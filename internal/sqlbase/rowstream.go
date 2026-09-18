// Package sqlbase 提供 database/sql 系驱动（MySQL、PostgreSQL、SQLite）的公共实现：
// 行流包装、类型归一化、值截断。
package sqlbase

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"tacivan/internal/dbx"
)

// RowStream 把 *sql.Rows 适配成 dbx.RowStream。
//
// 全程只持有当前一行：每次 Next 复用同一组扫描目标，转换完立即释放。
// 结果集有多大都不影响这里的内存占用。
type RowStream struct {
	rows *sql.Rows
	cols []dbx.ColumnMeta
	opts dbx.ScanOptions

	// raw 复用的扫描目标。
	raw  []sql.RawBytes
	dest []any
	// binary 每列是否按二进制处理。
	binary []bool

	closed bool
	// onClose 关闭时的额外清理（如归还连接、取消 context）。
	onClose func()
}

// NewRowStream 包装一个 *sql.Rows。
func NewRowStream(rows *sql.Rows, classify func(dbTypeName string) dbx.ValueClass, opts dbx.ScanOptions, onClose func()) (*RowStream, error) {
	opts = opts.Normalize()
	names, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		return nil, err
	}

	cols := make([]dbx.ColumnMeta, len(names))
	binary := make([]bool, len(names))
	for i := range names {
		dbType := ""
		if i < len(types) && types[i] != nil {
			dbType = types[i].DatabaseTypeName()
		}
		class := dbx.ClassUnknown
		if classify != nil {
			class = classify(dbType)
		}
		nullable := -1
		if i < len(types) && types[i] != nil {
			if n, ok := types[i].Nullable(); ok {
				if n {
					nullable = 1
				} else {
					nullable = 0
				}
			}
		}
		cols[i] = dbx.ColumnMeta{
			Name:     names[i],
			Type:     dbType,
			Class:    class,
			Nullable: nullable,
		}
		binary[i] = class == dbx.ClassBinary
	}

	s := &RowStream{
		rows:    rows,
		cols:    cols,
		opts:    opts,
		raw:     make([]sql.RawBytes, len(names)),
		dest:    make([]any, len(names)),
		binary:  binary,
		onClose: onClose,
	}
	for i := range s.raw {
		s.dest[i] = &s.raw[i]
	}
	return s, nil
}

// Columns 列元信息。
func (s *RowStream) Columns() []dbx.ColumnMeta { return s.cols }

// Next 读取下一行。
func (s *RowStream) Next() (dbx.Row, error) {
	if s.closed {
		return nil, io.EOF
	}
	if !s.rows.Next() {
		if err := s.rows.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	if err := s.rows.Scan(s.dest...); err != nil {
		return nil, err
	}
	row := make(dbx.Row, len(s.raw))
	for i := range s.raw {
		row[i] = s.makeCell(s.raw[i], s.binary[i])
	}
	return row, nil
}

// Close 关闭底层 rows。
func (s *RowStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.rows.Close()
	if s.onClose != nil {
		s.onClose()
	}
	return err
}

// makeCell 把原始字节转成 Cell，在这里完成截断。
//
// 截断发生在数据进入内存缓存之前，这是控制内存的关键：
// 一个 LONGBLOB 列即便有 200MB，进到结果集里也只有预览那几 KB。
func (s *RowStream) makeCell(b sql.RawBytes, isBinary bool) dbx.Cell {
	if b == nil {
		return dbx.Cell{Null: true}
	}
	size := len(b)
	if isBinary || !utf8.Valid(b) {
		limit := s.opts.BinaryPreviewLimit
		src := b
		truncated := false
		if len(src) > limit {
			src = src[:limit]
			truncated = true
		}
		return dbx.Cell{
			Text:      hex.EncodeToString(src),
			Binary:    true,
			Truncated: truncated,
			Size:      size,
		}
	}

	if size > s.opts.PreviewLimit {
		cut := truncateUTF8(b, s.opts.PreviewLimit)
		return dbx.Cell{Text: string(b[:cut]), Truncated: true, Size: size}
	}
	return dbx.Cell{Text: string(b), Size: size}
}

// truncateUTF8 在不切断多字节字符的前提下截断到不超过 limit 字节。
func truncateUTF8(b []byte, limit int) int {
	if len(b) <= limit {
		return len(b)
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(b[cut]) {
		cut--
	}
	return cut
}

// ScanValues 把一次性查询的小结果读成字符串矩阵，用于元数据查询。
// 仅供行数可控的内部查询使用，不要拿它读用户数据。
func ScanValues(rows *sql.Rows) ([][]sql.NullString, error) {
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]sql.NullString
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		dest := make([]any, len(cols))
		for i := range vals {
			dest[i] = &vals[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, vals)
	}
	return out, rows.Err()
}

// NS 取 NullString 的值，NULL 返回空串。
func NS(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

// QuoteIdentWith 用给定的引号字符引用标识符，内部同名字符双写转义。
func QuoteIdentWith(ident string, q byte) string {
	qs := string(q)
	return qs + strings.ReplaceAll(ident, qs, qs+qs) + qs
}

// QuoteStringLiteral 生成标准 SQL 字符串字面量。
func QuoteStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// FormatBytes 人类可读的字节数，用于对象树上的体积展示。
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

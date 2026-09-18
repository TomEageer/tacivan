package app

import (
	"bufio"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"tacivan/internal/dbx"
)

// ExportOptions 导出选项。
type ExportOptions struct {
	// Format: csv | tsv | json | sql | markdown
	Format string `json:"format"`
	// Path 目标文件路径。
	Path string `json:"path"`
	// IncludeHeader CSV/TSV 是否写表头。
	IncludeHeader bool `json:"includeHeader"`
	// Delimiter CSV 分隔符，默认逗号。
	Delimiter string `json:"delimiter"`
	// Encoding: utf8 | utf8bom
	Encoding string `json:"encoding"`
	// NullText NULL 值在文本格式中的写法。
	NullText string `json:"nullText"`
	// MaxRows 最多导出多少行，0 表示全部。
	MaxRows int64 `json:"maxRows"`
	// TableName SQL 格式的目标表名。
	TableName string `json:"tableName"`
	// BatchSize SQL 格式每条 INSERT 带多少行。
	BatchSize int `json:"batchSize"`
}

// ExportResult 导出结果。
type ExportResult struct {
	Path       string `json:"path"`
	Rows       int64  `json:"rows"`
	Bytes      int64  `json:"bytes"`
	DurationMS int64  `json:"durationMs"`
	// Truncated 因 MaxRows 被截断。
	Truncated bool `json:"truncated"`
}

// exportChunk 每次从结果集里取多少行写盘。
const exportChunk = 1000

// ExportResultSet 把一个结果集导出到文件。
//
// 全程流式：从游标按块取、立刻写盘、立刻丢弃，
// 导出一亿行和导出一千行的内存占用是一样的。
func (a *App) ExportResultSet(resultID string, opts ExportOptions) (*ExportResult, error) {
	c, ok := a.rs.Get(resultID)
	if !ok {
		return nil, fmt.Errorf("结果集不存在或已关闭")
	}
	if strings.TrimSpace(opts.Path) == "" {
		return nil, fmt.Errorf("请指定导出文件路径")
	}
	if opts.NullText == "" && opts.Format != "sql" {
		opts.NullText = ""
	}

	f, err := os.Create(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 256<<10)

	if opts.Encoding == "utf8bom" {
		// Excel 打开 UTF-8 CSV 需要 BOM，否则中文乱码。
		if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return nil, err
		}
	}

	cols := c.Columns()
	start := time.Now()
	res := &ExportResult{Path: opts.Path}

	var writeRows func(rows []dbx.Row, first bool) error
	var finish func() error

	switch strings.ToLower(opts.Format) {
	case "csv", "tsv":
		cw := csv.NewWriter(w)
		if opts.Format == "tsv" {
			cw.Comma = '\t'
		} else if opts.Delimiter != "" {
			cw.Comma = rune(opts.Delimiter[0])
		}
		if opts.IncludeHeader {
			head := make([]string, len(cols))
			for i, col := range cols {
				head[i] = col.Name
			}
			if err := cw.Write(head); err != nil {
				return nil, err
			}
		}
		buf := make([]string, len(cols))
		writeRows = func(rows []dbx.Row, _ bool) error {
			for _, r := range rows {
				for i := range buf {
					if i < len(r) {
						buf[i] = cellText(r[i], opts.NullText)
					} else {
						buf[i] = ""
					}
				}
				if err := cw.Write(buf); err != nil {
					return err
				}
			}
			return nil
		}
		finish = func() error { cw.Flush(); return cw.Error() }

	case "json":
		if _, err := w.WriteString("[\n"); err != nil {
			return nil, err
		}
		enc := json.NewEncoder(w)
		writeRows = func(rows []dbx.Row, first bool) error {
			for i, r := range rows {
				if !(first && i == 0) {
					if _, err := w.WriteString(",\n"); err != nil {
						return err
					}
				}
				obj := make(map[string]any, len(cols))
				for k, col := range cols {
					if k >= len(r) {
						continue
					}
					if r[k].Null {
						obj[col.Name] = nil
					} else {
						obj[col.Name] = r[k].Text
					}
				}
				if _, err := w.WriteString("  "); err != nil {
					return err
				}
				// Encoder 会自带换行，这里用它的输出直接续写。
				if err := enc.Encode(obj); err != nil {
					return err
				}
				// 去掉 Encode 追加的换行，交由下一轮的分隔符控制排版。
				if err := w.Flush(); err != nil {
					return err
				}
			}
			return nil
		}
		finish = func() error { _, err := w.WriteString("]\n"); return err }

	case "sql":
		table := opts.TableName
		if table == "" {
			table = c.Meta.Ref.Name
		}
		if table == "" {
			table = "exported_table"
		}
		batch := opts.BatchSize
		if batch <= 0 {
			batch = 100
		}
		names := make([]string, len(cols))
		for i, col := range cols {
			names[i] = "`" + strings.ReplaceAll(col.Name, "`", "``") + "`"
		}
		header := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES\n",
			strings.ReplaceAll(table, "`", "``"), strings.Join(names, ", "))
		pending := 0
		writeRows = func(rows []dbx.Row, _ bool) error {
			for _, r := range rows {
				if pending == 0 {
					if _, err := w.WriteString(header); err != nil {
						return err
					}
				} else {
					if _, err := w.WriteString(",\n"); err != nil {
						return err
					}
				}
				vals := make([]string, len(cols))
				for i := range cols {
					if i < len(r) {
						vals[i] = sqlLiteral(r[i], cols[i])
					} else {
						vals[i] = "NULL"
					}
				}
				if _, err := fmt.Fprintf(w, "  (%s)", strings.Join(vals, ", ")); err != nil {
					return err
				}
				pending++
				if pending >= batch {
					if _, err := w.WriteString(";\n\n"); err != nil {
						return err
					}
					pending = 0
				}
			}
			return nil
		}
		finish = func() error {
			if pending > 0 {
				_, err := w.WriteString(";\n")
				return err
			}
			return nil
		}

	case "markdown":
		head := make([]string, len(cols))
		sep := make([]string, len(cols))
		for i, col := range cols {
			head[i] = escapeMarkdown(col.Name)
			sep[i] = "---"
		}
		if _, err := fmt.Fprintf(w, "| %s |\n| %s |\n", strings.Join(head, " | "), strings.Join(sep, " | ")); err != nil {
			return nil, err
		}
		writeRows = func(rows []dbx.Row, _ bool) error {
			cells := make([]string, len(cols))
			for _, r := range rows {
				for i := range cols {
					if i < len(r) {
						cells[i] = escapeMarkdown(cellText(r[i], opts.NullText))
					} else {
						cells[i] = ""
					}
				}
				if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(cells, " | ")); err != nil {
					return err
				}
			}
			return nil
		}
		finish = func() error { return nil }

	default:
		return nil, fmt.Errorf("不支持的导出格式: %s", opts.Format)
	}

	var offset int64
	first := true
	for {
		limit := int64(exportChunk)
		if opts.MaxRows > 0 && offset+limit > opts.MaxRows {
			limit = opts.MaxRows - offset
		}
		if limit <= 0 {
			res.Truncated = true
			break
		}
		page, err := c.Fetch(offset, limit)
		if err != nil {
			return nil, err
		}
		if len(page.Rows) == 0 {
			break
		}
		if err := writeRows(page.Rows, first); err != nil {
			return nil, err
		}
		first = false
		offset += int64(len(page.Rows))
		res.Rows = offset
		if page.Complete && offset >= page.Loaded {
			break
		}
		// 每写完一批就检查一次全局水位，长时间导出不会把内存推高。
		a.rs.EnforceBudget()
	}

	if err := finish(); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil {
		res.Bytes = st.Size()
	}
	res.DurationMS = time.Since(start).Milliseconds()
	return res, nil
}

func cellText(c dbx.Cell, nullText string) string {
	if c.Null {
		return nullText
	}
	return c.Text
}

func sqlLiteral(c dbx.Cell, col dbx.ColumnMeta) string {
	if c.Null {
		return "NULL"
	}
	if c.Binary {
		// 预览已是十六进制文本，还原成 SQL 的二进制字面量。
		if _, err := hex.DecodeString(c.Text); err == nil {
			return "0x" + c.Text
		}
	}
	if col.Class == dbx.ClassNumber {
		if _, err := strconv.ParseFloat(c.Text, 64); err == nil {
			return c.Text
		}
	}
	var b strings.Builder
	b.Grow(len(c.Text) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(c.Text); i++ {
		switch ch := c.Text[i]; ch {
		case '\'':
			b.WriteString("''")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case 0:
			b.WriteString("\\0")
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", "<br>")
	return s
}

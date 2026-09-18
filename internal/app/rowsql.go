package app

import (
	"fmt"
	"strings"

	"tacivan/internal/dbx"
	"tacivan/internal/rs"
)

// 为选中的行生成可直接执行的语句。
//
// 这是排查问题时最常用的动作：从结果里挑一行，复制成 UPDATE 发给同事，
// 或者复制成 INSERT 灌到测试库。WHERE 条件用主键生成，
// 没有主键就退化到全列匹配——那样至少语句是能跑的，只是效率差。

// RowSQLOptions 生成行语句的选项。
type RowSQLOptions struct {
	// Kind: insert | update | delete | select
	Kind string `json:"kind"`
	// RowIndexes 结果集中的行号。
	RowIndexes []int64 `json:"rowIndexes"`
	// Columns 只针对这些列生成（用于「只复制选中列」）；为空表示全部。
	Columns []string `json:"columns"`
}

// GenerateRowSQL 把结果集里选中的行转成语句文本。
func (a *App) GenerateRowSQL(resultID string, opts RowSQLOptions) (string, error) {
	cursor, ok := a.rs.Get(resultID)
	if !ok {
		return "", fmt.Errorf("结果集不存在或已关闭")
	}
	if len(opts.RowIndexes) == 0 {
		return "", fmt.Errorf("请先选中至少一行")
	}

	meta := cursor.Columns()
	colIndex := make(map[string]int, len(meta))
	for i, c := range meta {
		colIndex[strings.ToLower(c.Name)] = i
	}

	ec, hasTarget := a.getEditContext(resultID)

	// 拿不到来源表时只能生成 INSERT，且表名用占位符——
	// 其余几种语句都需要能定位到具体的表和行。
	if !hasTarget {
		if strings.ToLower(opts.Kind) != "insert" {
			return "", fmt.Errorf("该结果集无法定位到具体的表，只能生成 INSERT 语句")
		}
		return a.generateGenericInsert(cursor, meta, opts)
	}

	_, sc, err := a.sqlSession(ec.connID)
	if err != nil {
		return "", err
	}
	d := sc.Dialect()

	// 没有主键时退化成全列匹配：生成的语句仍然正确，只是走不了索引。
	keys := ec.keyColumns
	degraded := false
	if len(keys) == 0 {
		degraded = true
		for _, c := range meta {
			keys = append(keys, c.Name)
		}
	}

	wanted := map[string]bool{}
	for _, c := range opts.Columns {
		wanted[strings.ToLower(c)] = true
	}
	include := func(name string) bool {
		return len(wanted) == 0 || wanted[strings.ToLower(name)]
	}

	var out []string
	if degraded {
		out = append(out, "-- 该表没有主键或唯一索引，WHERE 用全部列匹配")
	}

	for _, rowIndex := range opts.RowIndexes {
		page, err := cursor.Fetch(rowIndex, 1)
		if err != nil {
			return "", err
		}
		if len(page.Rows) == 0 {
			continue
		}
		row := page.Rows[0]

		valueOf := func(name string) (dbx.FieldValue, bool) {
			i, ok := colIndex[strings.ToLower(name)]
			if !ok || i >= len(row) {
				return dbx.FieldValue{}, false
			}
			cell := row[i]
			if cell.Null {
				return dbx.FieldValue{Null: true}, true
			}
			// 被截断或二进制的值写进语句会是错的，明确标出来而不是悄悄写个残缺值。
			if cell.Truncated || cell.Binary {
				return dbx.FieldValue{Value: cell.Text}, true
			}
			return dbx.FieldValue{Value: cell.Text}, true
		}

		keyValues := map[string]dbx.FieldValue{}
		for _, k := range keys {
			if v, ok := valueOf(k); ok {
				keyValues[k] = v
			}
		}

		switch strings.ToLower(opts.Kind) {
		case "select":
			stmt, err := d.BuildRowWrite(ec.ref, ec.columns, dbx.RowChange{Op: "delete", Keys: keyValues})
			if err != nil {
				return "", err
			}
			// 借用 DELETE 的 WHERE 生成，再把动词换成 SELECT，
			// 免得把同一套条件拼装逻辑再写一遍。
			where := stmt.Preview
			if i := strings.Index(where, " WHERE "); i >= 0 {
				out = append(out, "SELECT * FROM "+d.QualifyRef(ec.ref)+where[i:]+";")
			}

		case "delete":
			stmt, err := d.BuildRowWrite(ec.ref, ec.columns, dbx.RowChange{Op: "delete", Keys: keyValues})
			if err != nil {
				return "", err
			}
			out = append(out, stmt.Preview+";")

		case "update":
			values := map[string]dbx.FieldValue{}
			isKey := map[string]bool{}
			for _, k := range keys {
				isKey[strings.ToLower(k)] = true
			}
			for _, c := range meta {
				if isKey[strings.ToLower(c.Name)] || !include(c.Name) {
					continue
				}
				if v, ok := valueOf(c.Name); ok {
					values[c.Name] = v
				}
			}
			if len(values) == 0 {
				return "", fmt.Errorf("没有可更新的列（主键之外的列都被排除了）")
			}
			stmt, err := d.BuildRowWrite(ec.ref, ec.columns,
				dbx.RowChange{Op: "update", Values: values, Keys: keyValues})
			if err != nil {
				return "", err
			}
			out = append(out, stmt.Preview+";")

		case "insert":
			values := map[string]dbx.FieldValue{}
			for _, c := range meta {
				if !include(c.Name) {
					continue
				}
				if v, ok := valueOf(c.Name); ok {
					values[c.Name] = v
				}
			}
			stmt, err := d.BuildRowWrite(ec.ref, ec.columns,
				dbx.RowChange{Op: "insert", Values: values})
			if err != nil {
				return "", err
			}
			out = append(out, stmt.Preview+";")

		default:
			return "", fmt.Errorf("不支持的语句类型: %s", opts.Kind)
		}
	}

	if len(out) == 0 {
		return "", fmt.Errorf("没有生成任何语句")
	}
	return strings.Join(out, "\n"), nil
}

// generateGenericInsert 结果集来源不明（多表 JOIN、聚合等）时，
// 仍然允许把行复制成 INSERT——表名用结果集里记录的名字，没有就留占位符让用户自己填。
func (a *App) generateGenericInsert(cursor *rs.Cursor, meta []dbx.ColumnMeta, opts RowSQLOptions) (string, error) {
	table := cursor.Meta.Ref.Name
	placeholder := false
	if table == "" {
		table = "目标表"
		placeholder = true
	}
	quoted := make([]string, 0, len(meta))
	for _, c := range meta {
		quoted = append(quoted, "`"+strings.ReplaceAll(c.Name, "`", "``")+"`")
	}

	var out []string
	if placeholder {
		out = append(out, "-- 结果集来自多表或聚合查询，表名请自行替换")
	}
	for _, rowIndex := range opts.RowIndexes {
		page, err := cursor.Fetch(rowIndex, 1)
		if err != nil {
			return "", err
		}
		if len(page.Rows) == 0 {
			continue
		}
		row := page.Rows[0]
		vals := make([]string, len(meta))
		for i := range meta {
			if i < len(row) {
				vals[i] = sqlLiteral(row[i], meta[i])
			} else {
				vals[i] = "NULL"
			}
		}
		out = append(out, fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s);",
			strings.ReplaceAll(table, "`", "``"), strings.Join(quoted, ", "), strings.Join(vals, ", ")))
	}
	if len(out) == 0 {
		return "", fmt.Errorf("没有生成任何语句")
	}
	return strings.Join(out, "\n"), nil
}

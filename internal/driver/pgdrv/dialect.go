package pgdrv

import (
	"fmt"
	"strconv"
	"strings"

	"tacivan/internal/dbx"
	"tacivan/internal/sqlbase"
)

// Dialect PostgreSQL 方言。
type Dialect struct {
	version int
}

// NewDialect 创建方言实例。
func NewDialect(version int) *Dialect { return &Dialect{version: version} }

func (d *Dialect) Engine() dbx.Engine { return dbx.EnginePostgres }

func (d *Dialect) QuoteIdent(ident string) string {
	return sqlbase.QuoteIdentWith(ident, '"')
}

func (d *Dialect) QuoteString(s string) string { return sqlbase.QuoteStringLiteral(s) }

func (d *Dialect) PlaceholderStyle() string { return "$N" }

func (d *Dialect) QualifyRef(ref dbx.ObjectRef) string {
	schema := ref.Schema
	if schema == "" {
		schema = "public"
	}
	return d.QuoteIdent(schema) + "." + d.QuoteIdent(ref.Name)
}

func (d *Dialect) BuildSelect(ref dbx.ObjectRef, opt dbx.SelectOptions) string {
	var b strings.Builder
	b.WriteString("SELECT ")
	if len(opt.Columns) == 0 {
		b.WriteString("*")
	} else {
		for i, c := range opt.Columns {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(d.QuoteIdent(c))
		}
	}
	b.WriteString("\nFROM ")
	b.WriteString(d.QualifyRef(ref))
	if w := strings.TrimSpace(opt.Where); w != "" {
		b.WriteString("\nWHERE ")
		b.WriteString(w)
	}
	if len(opt.OrderBy) > 0 {
		b.WriteString("\nORDER BY ")
		for i, o := range opt.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(d.QuoteIdent(o.Column))
			if o.Desc {
				b.WriteString(" DESC")
			}
		}
	}
	if opt.Limit > 0 {
		fmt.Fprintf(&b, "\nLIMIT %d", opt.Limit)
		if opt.Offset > 0 {
			fmt.Fprintf(&b, " OFFSET %d", opt.Offset)
		}
	}
	return b.String()
}

func (d *Dialect) BuildCount(ref dbx.ObjectRef, where string) string {
	s := "SELECT COUNT(*) FROM " + d.QualifyRef(ref)
	if w := strings.TrimSpace(where); w != "" {
		s += " WHERE " + w
	}
	return s
}

func (d *Dialect) BuildRowWrite(ref dbx.ObjectRef, cols []dbx.Column, ch dbx.RowChange) (dbx.Statement, error) {
	byName := make(map[string]*dbx.Column, len(cols))
	for i := range cols {
		byName[cols[i].Name] = &cols[i]
	}
	table := d.QualifyRef(ref)
	// PostgreSQL 用 $1 $2 递增占位符，需要一个跨子句延续的计数器。
	n := 0
	next := func() string { n++; return "$" + strconv.Itoa(n) }

	switch strings.ToLower(ch.Op) {
	case "insert":
		if len(ch.Values) == 0 {
			return dbx.Statement{}, fmt.Errorf("插入操作没有任何列值")
		}
		names := orderedNames(cols, ch.Values)
		var sqlCols, marks, prevVals []string
		var args []any
		for _, k := range names {
			sqlCols = append(sqlCols, d.QuoteIdent(k))
			marks = append(marks, next())
			args = append(args, nullableArg(ch.Values[k]))
			prevVals = append(prevVals, d.literal(byName[k], ch.Values[k]))
		}
		return dbx.Statement{
			SQL:     fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(sqlCols, ", "), strings.Join(marks, ", ")),
			Args:    args,
			Preview: fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(sqlCols, ", "), strings.Join(prevVals, ", ")),
		}, nil

	case "update":
		if len(ch.Values) == 0 {
			return dbx.Statement{}, fmt.Errorf("更新操作没有任何变更列")
		}
		if len(ch.Keys) == 0 {
			return dbx.Statement{}, fmt.Errorf("无法定位目标行：该结果集没有可用的唯一键")
		}
		names := orderedNames(cols, ch.Values)
		var sets, prevSets []string
		var args []any
		for _, k := range names {
			sets = append(sets, d.QuoteIdent(k)+" = "+next())
			args = append(args, nullableArg(ch.Values[k]))
			prevSets = append(prevSets, d.QuoteIdent(k)+" = "+d.literal(byName[k], ch.Values[k]))
		}
		where, whereArgs, prevWhere := d.buildWhere(byName, cols, ch.Keys, next)
		args = append(args, whereArgs...)
		return dbx.Statement{
			SQL:     fmt.Sprintf("UPDATE %s SET %s WHERE %s", table, strings.Join(sets, ", "), where),
			Args:    args,
			Preview: fmt.Sprintf("UPDATE %s SET %s WHERE %s", table, strings.Join(prevSets, ", "), prevWhere),
		}, nil

	case "delete":
		if len(ch.Keys) == 0 {
			return dbx.Statement{}, fmt.Errorf("无法定位目标行：该结果集没有可用的唯一键")
		}
		where, args, prevWhere := d.buildWhere(byName, cols, ch.Keys, next)
		return dbx.Statement{
			SQL:     fmt.Sprintf("DELETE FROM %s WHERE %s", table, where),
			Args:    args,
			Preview: fmt.Sprintf("DELETE FROM %s WHERE %s", table, prevWhere),
		}, nil
	}
	return dbx.Statement{}, fmt.Errorf("不支持的行操作: %s", ch.Op)
}

func (d *Dialect) buildWhere(byName map[string]*dbx.Column, cols []dbx.Column, keys map[string]dbx.FieldValue, next func() string) (string, []any, string) {
	names := orderedNames(cols, keys)
	var parts, prevParts []string
	var args []any
	for _, k := range names {
		v := keys[k]
		if v.Null {
			parts = append(parts, d.QuoteIdent(k)+" IS NULL")
			prevParts = append(prevParts, d.QuoteIdent(k)+" IS NULL")
			continue
		}
		parts = append(parts, d.QuoteIdent(k)+" = "+next())
		args = append(args, v.Value)
		prevParts = append(prevParts, d.QuoteIdent(k)+" = "+d.literal(byName[k], v))
	}
	return strings.Join(parts, " AND "), args, strings.Join(prevParts, " AND ")
}

func orderedNames(cols []dbx.Column, m map[string]dbx.FieldValue) []string {
	var out []string
	seen := make(map[string]bool, len(m))
	for _, c := range cols {
		if _, ok := m[c.Name]; ok {
			out = append(out, c.Name)
			seen[c.Name] = true
		}
	}
	for n := range m {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}

func nullableArg(v dbx.FieldValue) any {
	if v.Null {
		return nil
	}
	return v.Value
}

func (d *Dialect) literal(col *dbx.Column, v dbx.FieldValue) string {
	if v.Null {
		return "NULL"
	}
	if col != nil && isNumericType(col.Type) {
		if _, err := strconv.ParseFloat(v.Value, 64); err == nil {
			return v.Value
		}
	}
	return sqlbase.QuoteStringLiteral(v.Value)
}

func isNumericType(t string) bool {
	switch strings.ToLower(t) {
	case "smallint", "integer", "int", "int2", "int4", "int8", "bigint",
		"decimal", "numeric", "real", "float4", "float8", "double precision", "money":
		return true
	}
	return false
}

func (d *Dialect) columnDef(c dbx.Column) string {
	var b strings.Builder
	b.WriteString(d.QuoteIdent(c.Name))
	b.WriteString(" ")
	b.WriteString(d.typeSpec(c))
	if c.Generated != "" {
		b.WriteString(" GENERATED ALWAYS AS (" + c.Generated + ") STORED")
	}
	if !c.Nullable {
		b.WriteString(" NOT NULL")
	}
	if c.HasDefault && c.Generated == "" && !c.AutoIncrement {
		b.WriteString(" DEFAULT " + d.defaultLiteral(c))
	}
	return b.String()
}

// typeSpec 生成类型定义；自增列在 PostgreSQL 里用 serial 家族表达。
func (d *Dialect) typeSpec(c dbx.Column) string {
	if c.AutoIncrement {
		switch strings.ToLower(c.Type) {
		case "smallint", "int2":
			return "smallserial"
		case "bigint", "int8":
			return "bigserial"
		default:
			return "serial"
		}
	}
	if ft := strings.TrimSpace(c.FullType); ft != "" {
		return ft
	}
	t := strings.ToLower(c.Type)
	switch {
	case c.Scale > 0:
		return fmt.Sprintf("%s(%d,%d)", t, c.Length, c.Scale)
	case c.Length > 0 && (t == "varchar" || t == "character varying" || t == "char" || t == "character" || t == "bit" || t == "numeric"):
		return fmt.Sprintf("%s(%d)", t, c.Length)
	}
	return t
}

func (d *Dialect) defaultLiteral(c dbx.Column) string {
	v := strings.TrimSpace(c.Default)
	if c.DefaultIsExpr || v == "" {
		return v
	}
	up := strings.ToUpper(v)
	// PostgreSQL 的 information_schema 里默认值通常已是完整表达式（含 ::type 强转）。
	if strings.Contains(v, "::") || strings.HasSuffix(v, ")") ||
		up == "NULL" || up == "CURRENT_TIMESTAMP" || up == "NOW()" || up == "TRUE" || up == "FALSE" {
		return v
	}
	if isNumericType(c.Type) {
		if _, err := strconv.ParseFloat(v, 64); err == nil {
			return v
		}
	}
	return sqlbase.QuoteStringLiteral(v)
}

func (d *Dialect) BuildCreateTable(def *dbx.TableDefinition) ([]string, error) {
	if def == nil || len(def.Columns) == 0 {
		return nil, fmt.Errorf("建表至少需要一个字段")
	}
	var parts []string
	for _, c := range def.Columns {
		parts = append(parts, "  "+d.columnDef(c))
	}
	var pkCols []string
	for _, idx := range def.Indexes {
		if idx.Primary {
			for _, c := range idx.Columns {
				pkCols = append(pkCols, c.Name)
			}
		}
	}
	if len(pkCols) == 0 {
		for _, c := range def.Columns {
			if c.PrimaryKey {
				pkCols = append(pkCols, c.Name)
			}
		}
	}
	if len(pkCols) > 0 {
		parts = append(parts, "  PRIMARY KEY ("+d.quoteList(pkCols)+")")
	}
	for _, fk := range def.ForeignKeys {
		parts = append(parts, "  "+d.foreignKeyDef(fk))
	}

	stmts := []string{"CREATE TABLE " + d.QualifyRef(def.Ref) + " (\n" + strings.Join(parts, ",\n") + "\n)"}

	// PostgreSQL 的索引是独立对象，不能写在 CREATE TABLE 里。
	for _, idx := range def.Indexes {
		if idx.Primary {
			continue
		}
		stmts = append(stmts, d.createIndex(def.Ref, idx))
	}
	if def.Comment != "" {
		stmts = append(stmts, "COMMENT ON TABLE "+d.QualifyRef(def.Ref)+" IS "+sqlbase.QuoteStringLiteral(def.Comment))
	}
	for _, c := range def.Columns {
		if c.Comment != "" {
			stmts = append(stmts, "COMMENT ON COLUMN "+d.QualifyRef(def.Ref)+"."+d.QuoteIdent(c.Name)+" IS "+sqlbase.QuoteStringLiteral(c.Comment))
		}
	}
	return stmts, nil
}

func (d *Dialect) createIndex(ref dbx.ObjectRef, idx dbx.Index) string {
	var b strings.Builder
	b.WriteString("CREATE ")
	if idx.Unique {
		b.WriteString("UNIQUE ")
	}
	b.WriteString("INDEX " + d.QuoteIdent(idx.Name) + " ON " + d.QualifyRef(ref))
	if idx.Type != "" && !strings.EqualFold(idx.Type, "btree") {
		b.WriteString(" USING " + idx.Type)
	}
	b.WriteString(" (")
	for i, c := range idx.Columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.QuoteIdent(c.Name))
		if strings.EqualFold(c.Order, "DESC") {
			b.WriteString(" DESC")
		}
	}
	b.WriteString(")")
	return b.String()
}

func (d *Dialect) foreignKeyDef(fk dbx.ForeignKey) string {
	var b strings.Builder
	if fk.Name != "" {
		b.WriteString("CONSTRAINT " + d.QuoteIdent(fk.Name) + " ")
	}
	b.WriteString("FOREIGN KEY (" + d.quoteList(fk.Columns) + ") REFERENCES ")
	schema := fk.ReferencedSchema
	if schema == "" {
		schema = "public"
	}
	b.WriteString(d.QuoteIdent(schema) + "." + d.QuoteIdent(fk.ReferencedTable))
	b.WriteString(" (" + d.quoteList(fk.ReferencedColumns) + ")")
	if fk.OnDelete != "" {
		b.WriteString(" ON DELETE " + fk.OnDelete)
	}
	if fk.OnUpdate != "" {
		b.WriteString(" ON UPDATE " + fk.OnUpdate)
	}
	return b.String()
}

func (d *Dialect) quoteList(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = d.QuoteIdent(n)
	}
	return strings.Join(out, ", ")
}

// BuildAlterTable PostgreSQL 的变更要拆成多条语句：改名、改类型、约束、索引各自独立。
func (d *Dialect) BuildAlterTable(old, newDef *dbx.TableDefinition) ([]string, error) {
	if old == nil || newDef == nil {
		return nil, fmt.Errorf("表定义不完整")
	}
	table := d.QualifyRef(old.Ref)
	var stmts []string

	oldCols := make(map[string]dbx.Column, len(old.Columns))
	for _, c := range old.Columns {
		oldCols[c.Name] = c
	}
	kept := make(map[string]bool, len(newDef.Columns))

	for _, c := range newDef.Columns {
		orig := c.OrigName
		if orig == "" {
			orig = c.Name
		}
		oc, existed := oldCols[orig]
		if !existed {
			stmts = append(stmts, "ALTER TABLE "+table+" ADD COLUMN "+d.columnDef(c))
			if c.Comment != "" {
				stmts = append(stmts, "COMMENT ON COLUMN "+table+"."+d.QuoteIdent(c.Name)+" IS "+sqlbase.QuoteStringLiteral(c.Comment))
			}
			continue
		}
		kept[orig] = true

		name := oc.Name
		if oc.Name != c.Name {
			stmts = append(stmts, "ALTER TABLE "+table+" RENAME COLUMN "+d.QuoteIdent(oc.Name)+" TO "+d.QuoteIdent(c.Name))
			name = c.Name
		}
		if !strings.EqualFold(strings.TrimSpace(oc.FullType), strings.TrimSpace(c.FullType)) {
			stmts = append(stmts, "ALTER TABLE "+table+" ALTER COLUMN "+d.QuoteIdent(name)+" TYPE "+d.typeSpec(c)+
				" USING "+d.QuoteIdent(name)+"::"+d.typeSpec(c))
		}
		if oc.Nullable != c.Nullable {
			if c.Nullable {
				stmts = append(stmts, "ALTER TABLE "+table+" ALTER COLUMN "+d.QuoteIdent(name)+" DROP NOT NULL")
			} else {
				stmts = append(stmts, "ALTER TABLE "+table+" ALTER COLUMN "+d.QuoteIdent(name)+" SET NOT NULL")
			}
		}
		if oc.HasDefault != c.HasDefault || oc.Default != c.Default {
			if c.HasDefault {
				stmts = append(stmts, "ALTER TABLE "+table+" ALTER COLUMN "+d.QuoteIdent(name)+" SET DEFAULT "+d.defaultLiteral(c))
			} else {
				stmts = append(stmts, "ALTER TABLE "+table+" ALTER COLUMN "+d.QuoteIdent(name)+" DROP DEFAULT")
			}
		}
		if oc.Comment != c.Comment {
			stmts = append(stmts, "COMMENT ON COLUMN "+table+"."+d.QuoteIdent(name)+" IS "+sqlbase.QuoteStringLiteral(c.Comment))
		}
	}
	for _, c := range old.Columns {
		if !kept[c.Name] {
			stmts = append(stmts, "ALTER TABLE "+table+" DROP COLUMN "+d.QuoteIdent(c.Name))
		}
	}

	// 索引
	oldIdx := map[string]dbx.Index{}
	for _, i := range old.Indexes {
		oldIdx[i.Name] = i
	}
	newIdx := map[string]dbx.Index{}
	for _, i := range newDef.Indexes {
		newIdx[i.Name] = i
	}
	for name, oi := range oldIdx {
		if oi.Primary {
			continue
		}
		ni, ok := newIdx[name]
		if !ok {
			stmts = append(stmts, "DROP INDEX "+d.QuoteIdent(indexSchema(old.Ref))+"."+d.QuoteIdent(name))
			continue
		}
		if !indexEqual(oi, ni) {
			stmts = append(stmts, "DROP INDEX "+d.QuoteIdent(indexSchema(old.Ref))+"."+d.QuoteIdent(name))
			stmts = append(stmts, d.createIndex(old.Ref, ni))
		}
	}
	for name, ni := range newIdx {
		if ni.Primary {
			continue
		}
		if _, ok := oldIdx[name]; !ok {
			stmts = append(stmts, d.createIndex(old.Ref, ni))
		}
	}

	// 外键
	oldFK := map[string]dbx.ForeignKey{}
	for _, f := range old.ForeignKeys {
		oldFK[f.Name] = f
	}
	newFK := map[string]dbx.ForeignKey{}
	for _, f := range newDef.ForeignKeys {
		newFK[f.Name] = f
	}
	for name, of := range oldFK {
		nf, ok := newFK[name]
		if !ok {
			stmts = append(stmts, "ALTER TABLE "+table+" DROP CONSTRAINT "+d.QuoteIdent(name))
			continue
		}
		if !fkEqual(of, nf) {
			stmts = append(stmts, "ALTER TABLE "+table+" DROP CONSTRAINT "+d.QuoteIdent(name))
			stmts = append(stmts, "ALTER TABLE "+table+" ADD "+d.foreignKeyDef(nf))
		}
	}
	for name, nf := range newFK {
		if _, ok := oldFK[name]; !ok {
			stmts = append(stmts, "ALTER TABLE "+table+" ADD "+d.foreignKeyDef(nf))
		}
	}

	if newDef.Comment != old.Comment {
		stmts = append(stmts, "COMMENT ON TABLE "+table+" IS "+sqlbase.QuoteStringLiteral(newDef.Comment))
	}
	if newDef.Ref.Name != "" && newDef.Ref.Name != old.Ref.Name {
		stmts = append(stmts, "ALTER TABLE "+table+" RENAME TO "+d.QuoteIdent(newDef.Ref.Name))
	}
	return stmts, nil
}

func indexSchema(ref dbx.ObjectRef) string {
	if ref.Schema == "" {
		return "public"
	}
	return ref.Schema
}

func indexEqual(a, b dbx.Index) bool {
	if a.Unique != b.Unique || len(a.Columns) != len(b.Columns) || !strings.EqualFold(a.Type, b.Type) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i].Name != b.Columns[i].Name || !strings.EqualFold(a.Columns[i].Order, b.Columns[i].Order) {
			return false
		}
	}
	return true
}

func fkEqual(a, b dbx.ForeignKey) bool {
	if a.ReferencedTable != b.ReferencedTable ||
		!strings.EqualFold(a.OnDelete, b.OnDelete) ||
		!strings.EqualFold(a.OnUpdate, b.OnUpdate) ||
		len(a.Columns) != len(b.Columns) ||
		len(a.ReferencedColumns) != len(b.ReferencedColumns) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return false
		}
	}
	for i := range a.ReferencedColumns {
		if a.ReferencedColumns[i] != b.ReferencedColumns[i] {
			return false
		}
	}
	return true
}

func (d *Dialect) BuildDropObject(ref dbx.ObjectRef) string {
	switch ref.Kind {
	case dbx.KindView:
		return "DROP VIEW " + d.QualifyRef(ref)
	case dbx.KindFunction:
		return "DROP FUNCTION " + d.QualifyRef(ref)
	case dbx.KindProcedure:
		return "DROP PROCEDURE " + d.QualifyRef(ref)
	case dbx.KindSequence:
		return "DROP SEQUENCE " + d.QualifyRef(ref)
	case dbx.KindTrigger:
		return "DROP TRIGGER " + d.QuoteIdent(ref.Name)
	}
	return "DROP TABLE " + d.QualifyRef(ref)
}

func (d *Dialect) BuildTruncate(ref dbx.ObjectRef) string {
	return "TRUNCATE TABLE " + d.QualifyRef(ref)
}

func (d *Dialect) BuildRenameObject(ref dbx.ObjectRef, newName string) string {
	kind := "TABLE"
	switch ref.Kind {
	case dbx.KindView:
		kind = "VIEW"
	case dbx.KindSequence:
		kind = "SEQUENCE"
	}
	return "ALTER " + kind + " " + d.QualifyRef(ref) + " RENAME TO " + d.QuoteIdent(newName)
}

func (d *Dialect) BuildExplain(query string, analyze bool) string {
	if analyze {
		// ANALYZE 会真的执行语句，调用方需要在只读连接上拦截。
		return "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) " + query
	}
	return "EXPLAIN (FORMAT TEXT) " + query
}

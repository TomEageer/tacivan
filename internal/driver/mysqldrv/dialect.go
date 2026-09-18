package mysqldrv

import (
	"fmt"
	"strconv"
	"strings"

	"tacivan/internal/dbx"
	"tacivan/internal/sqlbase"
)

// Dialect MySQL/MariaDB 方言。
type Dialect struct {
	engine dbx.Engine
	// version 服务器版本号，如 80035。影响部分语法（如 RENAME COLUMN 需要 8.0+）。
	version int
}

// NewDialect 创建方言实例。
func NewDialect(engine dbx.Engine, version int) *Dialect {
	return &Dialect{engine: engine, version: version}
}

func (d *Dialect) Engine() dbx.Engine { return d.engine }

func (d *Dialect) QuoteIdent(ident string) string {
	return sqlbase.QuoteIdentWith(ident, '`')
}

func (d *Dialect) QuoteString(s string) string { return quoteMySQLString(s) }

func (d *Dialect) PlaceholderStyle() string { return "?" }

func (d *Dialect) QualifyRef(ref dbx.ObjectRef) string {
	if ref.Database == "" {
		return d.QuoteIdent(ref.Name)
	}
	return d.QuoteIdent(ref.Database) + "." + d.QuoteIdent(ref.Name)
}

// quoteMySQLString 生成 MySQL 字符串字面量。
//
// MySQL 默认开启反斜杠转义，因此除了单引号双写，反斜杠与控制字符也必须处理，
// 否则预览出来的 SQL 复制到别处执行会与实际行为不一致。
func quoteMySQLString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'':
			b.WriteString("''")
		case '\\':
			b.WriteString("\\\\")
		case 0:
			b.WriteString("\\0")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case 26:
			b.WriteString("\\Z")
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('\'')
	return b.String()
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

	switch strings.ToLower(ch.Op) {
	case "insert":
		if len(ch.Values) == 0 {
			return dbx.Statement{}, fmt.Errorf("插入操作没有任何列值")
		}
		names := orderedNames(cols, ch.Values)
		var sqlCols, marks, prevVals []string
		var args []any
		for _, n := range names {
			sqlCols = append(sqlCols, d.QuoteIdent(n))
			marks = append(marks, "?")
			v := ch.Values[n]
			args = append(args, nullableArg(v))
			prevVals = append(prevVals, d.literal(byName[n], v))
		}
		body := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(sqlCols, ", "), strings.Join(marks, ", "))
		preview := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(sqlCols, ", "), strings.Join(prevVals, ", "))
		return dbx.Statement{SQL: body, Args: args, Preview: preview}, nil

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
		for _, n := range names {
			sets = append(sets, d.QuoteIdent(n)+" = ?")
			args = append(args, nullableArg(ch.Values[n]))
			prevSets = append(prevSets, d.QuoteIdent(n)+" = "+d.literal(byName[n], ch.Values[n]))
		}
		where, whereArgs, prevWhere := d.buildWhere(byName, cols, ch.Keys)
		args = append(args, whereArgs...)
		body := fmt.Sprintf("UPDATE %s SET %s WHERE %s", table, strings.Join(sets, ", "), where)
		preview := fmt.Sprintf("UPDATE %s SET %s WHERE %s", table, strings.Join(prevSets, ", "), prevWhere)
		return dbx.Statement{SQL: body, Args: args, Preview: preview}, nil

	case "delete":
		if len(ch.Keys) == 0 {
			return dbx.Statement{}, fmt.Errorf("无法定位目标行：该结果集没有可用的唯一键")
		}
		where, args, prevWhere := d.buildWhere(byName, cols, ch.Keys)
		body := fmt.Sprintf("DELETE FROM %s WHERE %s", table, where)
		preview := fmt.Sprintf("DELETE FROM %s WHERE %s", table, prevWhere)
		return dbx.Statement{SQL: body, Args: args, Preview: preview}, nil
	}
	return dbx.Statement{}, fmt.Errorf("不支持的行操作: %s", ch.Op)
}

// buildWhere 用定位键构造 WHERE 子句，NULL 值走 IS NULL。
func (d *Dialect) buildWhere(byName map[string]*dbx.Column, cols []dbx.Column, keys map[string]dbx.FieldValue) (string, []any, string) {
	names := orderedNames(cols, keys)
	var parts, prevParts []string
	var args []any
	for _, n := range names {
		v := keys[n]
		if v.Null {
			parts = append(parts, d.QuoteIdent(n)+" IS NULL")
			prevParts = append(prevParts, d.QuoteIdent(n)+" IS NULL")
			continue
		}
		parts = append(parts, d.QuoteIdent(n)+" = ?")
		args = append(args, v.Value)
		prevParts = append(prevParts, d.QuoteIdent(n)+" = "+d.literal(byName[n], v))
	}
	return strings.Join(parts, " AND "), args, strings.Join(prevParts, " AND ")
}

// orderedNames 按列定义顺序返回 m 中出现的列名，保证生成的 SQL 稳定可比对。
func orderedNames(cols []dbx.Column, m map[string]dbx.FieldValue) []string {
	var out []string
	seen := make(map[string]bool, len(m))
	for _, c := range cols {
		if _, ok := m[c.Name]; ok {
			out = append(out, c.Name)
			seen[c.Name] = true
		}
	}
	// 结果集里有而表定义里没有的列（理论上不该发生）兜底附在后面。
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

// literal 生成用于预览的字面量。
func (d *Dialect) literal(col *dbx.Column, v dbx.FieldValue) string {
	if v.Null {
		return "NULL"
	}
	if col != nil && isNumericType(col.Type) {
		if _, err := strconv.ParseFloat(v.Value, 64); err == nil {
			return v.Value
		}
	}
	return quoteMySQLString(v.Value)
}

func isNumericType(t string) bool {
	switch strings.ToLower(t) {
	case "tinyint", "smallint", "mediumint", "int", "integer", "bigint",
		"decimal", "numeric", "float", "double", "real", "bit", "year":
		return true
	}
	return false
}

// columnDef 生成一列的 DDL 片段。
func (d *Dialect) columnDef(c dbx.Column) string {
	var b strings.Builder
	b.WriteString(d.QuoteIdent(c.Name))
	b.WriteString(" ")
	b.WriteString(d.typeSpec(c))
	if c.Unsigned {
		b.WriteString(" UNSIGNED")
	}
	if c.Charset != "" {
		b.WriteString(" CHARACTER SET " + c.Charset)
	}
	if c.Collation != "" {
		b.WriteString(" COLLATE " + c.Collation)
	}
	if c.Generated != "" {
		b.WriteString(" GENERATED ALWAYS AS (" + c.Generated + ")")
	}
	if c.Nullable {
		b.WriteString(" NULL")
	} else {
		b.WriteString(" NOT NULL")
	}
	if c.HasDefault && c.Generated == "" {
		b.WriteString(" DEFAULT " + d.defaultLiteral(c))
	}
	if c.AutoIncrement {
		b.WriteString(" AUTO_INCREMENT")
	}
	if c.Comment != "" {
		b.WriteString(" COMMENT " + quoteMySQLString(c.Comment))
	}
	return b.String()
}

// typeSpec 生成类型定义，已含长度/精度的 FullType 优先使用。
func (d *Dialect) typeSpec(c dbx.Column) string {
	if ft := strings.TrimSpace(c.FullType); ft != "" {
		// FullType 来自 information_schema，可能已带 unsigned，去掉避免重复。
		ft = strings.TrimSpace(strings.ReplaceAll(strings.ToLower(ft), "unsigned", ""))
		ft = strings.TrimSpace(strings.ReplaceAll(ft, "zerofill", ""))
		return ft
	}
	t := strings.ToLower(c.Type)
	switch {
	case len(c.EnumValues) > 0 && (t == "enum" || t == "set"):
		vals := make([]string, len(c.EnumValues))
		for i, v := range c.EnumValues {
			vals[i] = quoteMySQLString(v)
		}
		return t + "(" + strings.Join(vals, ",") + ")"
	case c.Scale > 0:
		return fmt.Sprintf("%s(%d,%d)", t, c.Length, c.Scale)
	case c.Length > 0 && typeTakesLength(t):
		return fmt.Sprintf("%s(%d)", t, c.Length)
	}
	return t
}

func typeTakesLength(t string) bool {
	switch t {
	case "varchar", "char", "varbinary", "binary", "decimal", "numeric", "bit":
		return true
	}
	return false
}

func (d *Dialect) defaultLiteral(c dbx.Column) string {
	v := c.Default
	if c.DefaultIsExpr || strings.EqualFold(v, "NULL") {
		return v
	}
	up := strings.ToUpper(strings.TrimSpace(v))
	// CURRENT_TIMESTAMP 家族即便未被标记为表达式也不能加引号。
	if strings.HasPrefix(up, "CURRENT_TIMESTAMP") || up == "NOW()" || up == "CURRENT_DATE" || up == "CURRENT_TIME" {
		return v
	}
	if isNumericType(c.Type) {
		if _, err := strconv.ParseFloat(v, 64); err == nil {
			return v
		}
	}
	return quoteMySQLString(v)
}

func (d *Dialect) indexDef(idx dbx.Index) string {
	var b strings.Builder
	switch {
	case idx.Primary:
		b.WriteString("PRIMARY KEY")
	case idx.Unique:
		b.WriteString("UNIQUE KEY " + d.QuoteIdent(idx.Name))
	case strings.EqualFold(idx.Type, "FULLTEXT"):
		b.WriteString("FULLTEXT KEY " + d.QuoteIdent(idx.Name))
	case strings.EqualFold(idx.Type, "SPATIAL"):
		b.WriteString("SPATIAL KEY " + d.QuoteIdent(idx.Name))
	default:
		b.WriteString("KEY " + d.QuoteIdent(idx.Name))
	}
	b.WriteString(" (")
	for i, c := range idx.Columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(d.QuoteIdent(c.Name))
		if c.Length > 0 {
			fmt.Fprintf(&b, "(%d)", c.Length)
		}
		if strings.EqualFold(c.Order, "DESC") {
			b.WriteString(" DESC")
		}
	}
	b.WriteString(")")
	if idx.Type != "" && !idx.Primary && !strings.EqualFold(idx.Type, "BTREE") &&
		!strings.EqualFold(idx.Type, "FULLTEXT") && !strings.EqualFold(idx.Type, "SPATIAL") {
		b.WriteString(" USING " + idx.Type)
	}
	if idx.Comment != "" {
		b.WriteString(" COMMENT " + quoteMySQLString(idx.Comment))
	}
	return b.String()
}

func (d *Dialect) foreignKeyDef(fk dbx.ForeignKey) string {
	var b strings.Builder
	if fk.Name != "" {
		b.WriteString("CONSTRAINT " + d.QuoteIdent(fk.Name) + " ")
	}
	b.WriteString("FOREIGN KEY (")
	b.WriteString(d.quoteList(fk.Columns))
	b.WriteString(") REFERENCES ")
	if fk.ReferencedSchema != "" {
		b.WriteString(d.QuoteIdent(fk.ReferencedSchema) + ".")
	}
	b.WriteString(d.QuoteIdent(fk.ReferencedTable))
	b.WriteString(" (")
	b.WriteString(d.quoteList(fk.ReferencedColumns))
	b.WriteString(")")
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

func (d *Dialect) BuildCreateTable(def *dbx.TableDefinition) ([]string, error) {
	if def == nil || len(def.Columns) == 0 {
		return nil, fmt.Errorf("建表至少需要一个字段")
	}
	var parts []string
	for _, c := range def.Columns {
		parts = append(parts, "  "+d.columnDef(c))
	}
	// 主键从索引列表取；没有显式主键索引时，退回按列上的 PrimaryKey 标记收集。
	hasPK := false
	for _, idx := range def.Indexes {
		if idx.Primary {
			hasPK = true
		}
		parts = append(parts, "  "+d.indexDef(idx))
	}
	if !hasPK {
		var pkCols []string
		for _, c := range def.Columns {
			if c.PrimaryKey {
				pkCols = append(pkCols, c.Name)
			}
		}
		if len(pkCols) > 0 {
			parts = append(parts, "  PRIMARY KEY ("+d.quoteList(pkCols)+")")
		}
	}
	for _, fk := range def.ForeignKeys {
		parts = append(parts, "  "+d.foreignKeyDef(fk))
	}

	var b strings.Builder
	b.WriteString("CREATE TABLE " + d.QualifyRef(def.Ref) + " (\n")
	b.WriteString(strings.Join(parts, ",\n"))
	b.WriteString("\n)")
	if def.Engine != "" {
		b.WriteString(" ENGINE=" + def.Engine)
	}
	if def.Charset != "" {
		b.WriteString(" DEFAULT CHARSET=" + def.Charset)
	}
	if def.Collation != "" {
		b.WriteString(" COLLATE=" + def.Collation)
	}
	if def.AutoIncrement > 0 {
		fmt.Fprintf(&b, " AUTO_INCREMENT=%d", def.AutoIncrement)
	}
	if def.Comment != "" {
		b.WriteString(" COMMENT=" + quoteMySQLString(def.Comment))
	}
	return []string{b.String()}, nil
}

// BuildAlterTable 比对新旧表定义，生成变更语句。
//
// 列的对应关系靠 Column.OrigName 追踪：设计器里改名的列带着原名过来，
// 这样才能区分「把 a 改名成 b」和「删掉 a、新增 b」——后者会丢数据。
func (d *Dialect) BuildAlterTable(old, newDef *dbx.TableDefinition) ([]string, error) {
	if old == nil || newDef == nil {
		return nil, fmt.Errorf("表定义不完整")
	}
	var stmts []string
	table := d.QualifyRef(old.Ref)

	oldCols := make(map[string]dbx.Column, len(old.Columns))
	for _, c := range old.Columns {
		oldCols[c.Name] = c
	}

	// 新定义中仍保留的旧列名，用于识别被删除的列。
	kept := make(map[string]bool, len(newDef.Columns))
	var clauses []string

	for i, c := range newDef.Columns {
		orig := c.OrigName
		if orig == "" {
			orig = c.Name
		}
		oc, existed := oldCols[orig]
		if !existed {
			// 新增列：指定位置以保持设计器里的字段顺序。
			clause := "ADD COLUMN " + d.columnDef(c)
			if i == 0 {
				clause += " FIRST"
			} else {
				clause += " AFTER " + d.QuoteIdent(newDef.Columns[i-1].Name)
			}
			clauses = append(clauses, clause)
			continue
		}
		kept[orig] = true
		if columnChanged(oc, c) {
			if oc.Name != c.Name {
				// CHANGE 同时完成改名与重定义，8.0 以下也支持。
				clauses = append(clauses, "CHANGE COLUMN "+d.QuoteIdent(oc.Name)+" "+d.columnDef(c))
			} else {
				clauses = append(clauses, "MODIFY COLUMN "+d.columnDef(c))
			}
		}
	}
	for _, c := range old.Columns {
		if !kept[c.Name] {
			clauses = append(clauses, "DROP COLUMN "+d.QuoteIdent(c.Name))
		}
	}

	// 索引：按名字比对，内容变了就先删后加。
	oldIdx := indexMap(old.Indexes)
	newIdx := indexMap(newDef.Indexes)
	for name, oi := range oldIdx {
		ni, ok := newIdx[name]
		if !ok {
			clauses = append(clauses, d.dropIndexClause(oi))
			continue
		}
		if !indexEqual(oi, ni) {
			clauses = append(clauses, d.dropIndexClause(oi))
			clauses = append(clauses, "ADD "+d.indexDef(ni))
		}
	}
	for name, ni := range newIdx {
		if _, ok := oldIdx[name]; !ok {
			clauses = append(clauses, "ADD "+d.indexDef(ni))
		}
	}

	// 外键同理。
	oldFK := fkMap(old.ForeignKeys)
	newFK := fkMap(newDef.ForeignKeys)
	for name, of := range oldFK {
		nf, ok := newFK[name]
		if !ok {
			clauses = append(clauses, "DROP FOREIGN KEY "+d.QuoteIdent(name))
			continue
		}
		if !fkEqual(of, nf) {
			clauses = append(clauses, "DROP FOREIGN KEY "+d.QuoteIdent(name))
			clauses = append(clauses, "ADD "+d.foreignKeyDef(nf))
		}
	}
	for name, nf := range newFK {
		if _, ok := oldFK[name]; !ok {
			clauses = append(clauses, "ADD "+d.foreignKeyDef(nf))
		}
	}

	// 表级属性。
	if newDef.Comment != old.Comment {
		clauses = append(clauses, "COMMENT="+quoteMySQLString(newDef.Comment))
	}
	if newDef.Engine != "" && !strings.EqualFold(newDef.Engine, old.Engine) {
		clauses = append(clauses, "ENGINE="+newDef.Engine)
	}
	if newDef.Collation != "" && newDef.Collation != old.Collation {
		charset := newDef.Charset
		if charset == "" {
			charset = charsetFromCollation(newDef.Collation)
		}
		clauses = append(clauses, "DEFAULT CHARACTER SET "+charset+" COLLATE "+newDef.Collation)
	} else if newDef.Charset != "" && newDef.Charset != old.Charset {
		clauses = append(clauses, "DEFAULT CHARACTER SET "+newDef.Charset)
	}
	if newDef.AutoIncrement > 0 && newDef.AutoIncrement != old.AutoIncrement {
		clauses = append(clauses, fmt.Sprintf("AUTO_INCREMENT=%d", newDef.AutoIncrement))
	}

	if len(clauses) > 0 {
		stmts = append(stmts, "ALTER TABLE "+table+"\n  "+strings.Join(clauses, ",\n  "))
	}
	// 改表名放最后，前面的语句都还在用旧表名。
	if newDef.Ref.Name != "" && newDef.Ref.Name != old.Ref.Name {
		stmts = append(stmts, d.BuildRenameObject(old.Ref, newDef.Ref.Name))
	}
	return stmts, nil
}

func (d *Dialect) dropIndexClause(idx dbx.Index) string {
	if idx.Primary {
		return "DROP PRIMARY KEY"
	}
	return "DROP INDEX " + d.QuoteIdent(idx.Name)
}

func charsetFromCollation(collation string) string {
	if i := strings.Index(collation, "_"); i > 0 {
		return collation[:i]
	}
	return collation
}

func indexMap(list []dbx.Index) map[string]dbx.Index {
	m := make(map[string]dbx.Index, len(list))
	for _, i := range list {
		name := i.Name
		if i.Primary {
			name = "PRIMARY"
		}
		m[name] = i
	}
	return m
}

func fkMap(list []dbx.ForeignKey) map[string]dbx.ForeignKey {
	m := make(map[string]dbx.ForeignKey, len(list))
	for _, f := range list {
		m[f.Name] = f
	}
	return m
}

func columnChanged(a, b dbx.Column) bool {
	return a.Name != b.Name ||
		!strings.EqualFold(strings.TrimSpace(a.FullType), strings.TrimSpace(b.FullType)) ||
		a.Nullable != b.Nullable ||
		a.HasDefault != b.HasDefault ||
		a.Default != b.Default ||
		a.AutoIncrement != b.AutoIncrement ||
		a.Comment != b.Comment ||
		a.Unsigned != b.Unsigned ||
		a.Collation != b.Collation ||
		a.Generated != b.Generated
}

func indexEqual(a, b dbx.Index) bool {
	if a.Unique != b.Unique || a.Primary != b.Primary || len(a.Columns) != len(b.Columns) {
		return false
	}
	if !strings.EqualFold(a.Type, b.Type) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i].Name != b.Columns[i].Name ||
			a.Columns[i].Length != b.Columns[i].Length ||
			!strings.EqualFold(a.Columns[i].Order, b.Columns[i].Order) {
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
	case dbx.KindTrigger:
		return "DROP TRIGGER " + d.QualifyRef(ref)
	case dbx.KindEvent:
		return "DROP EVENT " + d.QualifyRef(ref)
	}
	return "DROP TABLE " + d.QualifyRef(ref)
}

func (d *Dialect) BuildTruncate(ref dbx.ObjectRef) string {
	return "TRUNCATE TABLE " + d.QualifyRef(ref)
}

func (d *Dialect) BuildRenameObject(ref dbx.ObjectRef, newName string) string {
	target := dbx.ObjectRef{Database: ref.Database, Name: newName, Kind: ref.Kind}
	if ref.Kind == dbx.KindView {
		return "RENAME TABLE " + d.QualifyRef(ref) + " TO " + d.QualifyRef(target)
	}
	return "RENAME TABLE " + d.QualifyRef(ref) + " TO " + d.QualifyRef(target)
}

func (d *Dialect) BuildExplain(query string, analyze bool) string {
	// EXPLAIN ANALYZE 需要 MySQL 8.0.18+，低版本退回普通 EXPLAIN 的 JSON 形式。
	if analyze && d.version >= 80018 {
		return "EXPLAIN ANALYZE " + query
	}
	return "EXPLAIN " + query
}

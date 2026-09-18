package sqlitedrv

import (
	"fmt"
	"strconv"
	"strings"

	"tacivan/internal/dbx"
	"tacivan/internal/sqlbase"
)

// Dialect SQLite 方言。
type Dialect struct{}

// NewDialect 创建方言实例。
func NewDialect() *Dialect { return &Dialect{} }

func (d *Dialect) Engine() dbx.Engine { return dbx.EngineSQLite }

func (d *Dialect) QuoteIdent(ident string) string { return sqlbase.QuoteIdentWith(ident, '"') }

func (d *Dialect) QuoteString(s string) string { return sqlbase.QuoteStringLiteral(s) }

func (d *Dialect) PlaceholderStyle() string { return "?" }

func (d *Dialect) QualifyRef(ref dbx.ObjectRef) string {
	if ref.Database != "" && ref.Database != "main" {
		return d.QuoteIdent(ref.Database) + "." + d.QuoteIdent(ref.Name)
	}
	return d.QuoteIdent(ref.Name)
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
	b.WriteString("\nFROM " + d.QualifyRef(ref))
	if w := strings.TrimSpace(opt.Where); w != "" {
		b.WriteString("\nWHERE " + w)
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
		for _, k := range names {
			sqlCols = append(sqlCols, d.QuoteIdent(k))
			marks = append(marks, "?")
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
			sets = append(sets, d.QuoteIdent(k)+" = ?")
			args = append(args, nullableArg(ch.Values[k]))
			prevSets = append(prevSets, d.QuoteIdent(k)+" = "+d.literal(byName[k], ch.Values[k]))
		}
		where, whereArgs, prevWhere := d.buildWhere(byName, cols, ch.Keys)
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
		where, args, prevWhere := d.buildWhere(byName, cols, ch.Keys)
		return dbx.Statement{
			SQL:     fmt.Sprintf("DELETE FROM %s WHERE %s", table, where),
			Args:    args,
			Preview: fmt.Sprintf("DELETE FROM %s WHERE %s", table, prevWhere),
		}, nil
	}
	return dbx.Statement{}, fmt.Errorf("不支持的行操作: %s", ch.Op)
}

func (d *Dialect) buildWhere(byName map[string]*dbx.Column, cols []dbx.Column, keys map[string]dbx.FieldValue) (string, []any, string) {
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
		parts = append(parts, d.QuoteIdent(k)+" = ?")
		args = append(args, v.Value)
		prevParts = append(prevParts, d.QuoteIdent(k)+" = "+d.literal(byName[k], v))
	}
	return strings.Join(parts, " AND "), args, strings.Join(prevParts, " AND ")
}

func orderedNames(cols []dbx.Column, m map[string]dbx.FieldValue) []string {
	var out []string
	seen := map[string]bool{}
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
	if col != nil && classify(col.FullType) == dbx.ClassNumber {
		if _, err := strconv.ParseFloat(v.Value, 64); err == nil {
			return v.Value
		}
	}
	return sqlbase.QuoteStringLiteral(v.Value)
}

func (d *Dialect) columnDef(c dbx.Column, singlePK bool) string {
	var b strings.Builder
	b.WriteString(d.QuoteIdent(c.Name))
	t := strings.TrimSpace(c.FullType)
	if t == "" {
		t = c.Type
	}
	if t != "" {
		b.WriteString(" " + t)
	}
	// SQLite 只在「单列 INTEGER 主键」这一形式下才有自增语义。
	if singlePK && c.PrimaryKey {
		b.WriteString(" PRIMARY KEY")
		if c.AutoIncrement {
			b.WriteString(" AUTOINCREMENT")
		}
	}
	if !c.Nullable && !(singlePK && c.PrimaryKey) {
		b.WriteString(" NOT NULL")
	}
	if c.HasDefault && c.Default != "" {
		b.WriteString(" DEFAULT " + c.Default)
	}
	if c.Collation != "" {
		b.WriteString(" COLLATE " + c.Collation)
	}
	return b.String()
}

func (d *Dialect) BuildCreateTable(def *dbx.TableDefinition) ([]string, error) {
	if def == nil || len(def.Columns) == 0 {
		return nil, fmt.Errorf("建表至少需要一个字段")
	}
	stmts := []string{d.createTableStmt(def, def.Ref.Name)}
	for _, idx := range def.Indexes {
		if idx.Primary {
			continue
		}
		stmts = append(stmts, d.createIndex(def.Ref, idx, idx.Name))
	}
	return stmts, nil
}

func (d *Dialect) createTableStmt(def *dbx.TableDefinition, tableName string) string {
	pkCols := primaryKeyColumns(def)
	singlePK := len(pkCols) == 1

	var parts []string
	for _, c := range def.Columns {
		parts = append(parts, "  "+d.columnDef(c, singlePK))
	}
	if len(pkCols) > 1 {
		parts = append(parts, "  PRIMARY KEY ("+d.quoteList(pkCols)+")")
	}
	for _, idx := range def.Indexes {
		if idx.Primary || !idx.Unique {
			continue
		}
		var cols []string
		for _, c := range idx.Columns {
			cols = append(cols, c.Name)
		}
		parts = append(parts, "  CONSTRAINT "+d.QuoteIdent(idx.Name)+" UNIQUE ("+d.quoteList(cols)+")")
	}
	for _, fk := range def.ForeignKeys {
		parts = append(parts, "  "+d.foreignKeyDef(fk))
	}
	ref := def.Ref
	ref.Name = tableName
	return "CREATE TABLE " + d.QualifyRef(ref) + " (\n" + strings.Join(parts, ",\n") + "\n)"
}

func primaryKeyColumns(def *dbx.TableDefinition) []string {
	for _, idx := range def.Indexes {
		if idx.Primary {
			var out []string
			for _, c := range idx.Columns {
				out = append(out, c.Name)
			}
			return out
		}
	}
	var out []string
	for _, c := range def.Columns {
		if c.PrimaryKey {
			out = append(out, c.Name)
		}
	}
	return out
}

func (d *Dialect) createIndex(ref dbx.ObjectRef, idx dbx.Index, name string) string {
	var b strings.Builder
	b.WriteString("CREATE ")
	if idx.Unique {
		b.WriteString("UNIQUE ")
	}
	b.WriteString("INDEX " + d.QuoteIdent(name) + " ON " + d.QuoteIdent(ref.Name) + " (")
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
	b.WriteString("FOREIGN KEY (" + d.quoteList(fk.Columns) + ") REFERENCES " +
		d.QuoteIdent(fk.ReferencedTable) + " (" + d.quoteList(fk.ReferencedColumns) + ")")
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

// BuildAlterTable SQLite 的 ALTER TABLE 只支持加列、删列、改列名、改表名。
//
// 只涉及这些操作时走原生 ALTER；一旦碰到改类型、改约束、改主键，
// 就退回官方推荐的「建新表 → 搬数据 → 换名」流程，全程包在事务里，
// 中途失败不会留下半张表。
func (d *Dialect) BuildAlterTable(old, newDef *dbx.TableDefinition) ([]string, error) {
	if old == nil || newDef == nil {
		return nil, fmt.Errorf("表定义不完整")
	}
	if simple, ok := d.trySimpleAlter(old, newDef); ok {
		return simple, nil
	}
	return d.rebuildTable(old, newDef)
}

func (d *Dialect) trySimpleAlter(old, newDef *dbx.TableDefinition) ([]string, bool) {
	if !indexesEqual(old.Indexes, newDef.Indexes) || !fksEqual(old.ForeignKeys, newDef.ForeignKeys) {
		return nil, false
	}
	table := d.QualifyRef(old.Ref)
	oldCols := map[string]dbx.Column{}
	for _, c := range old.Columns {
		oldCols[c.Name] = c
	}
	var stmts []string
	kept := map[string]bool{}

	for _, c := range newDef.Columns {
		orig := c.OrigName
		if orig == "" {
			orig = c.Name
		}
		oc, existed := oldCols[orig]
		if !existed {
			// 新列有 NOT NULL 但无默认值时 SQLite 会拒绝，这种情况必须走重建。
			if !c.Nullable && !c.HasDefault {
				return nil, false
			}
			stmts = append(stmts, "ALTER TABLE "+table+" ADD COLUMN "+d.columnDef(c, false))
			continue
		}
		kept[orig] = true
		if oc.Name != c.Name {
			stmts = append(stmts, "ALTER TABLE "+table+" RENAME COLUMN "+d.QuoteIdent(oc.Name)+" TO "+d.QuoteIdent(c.Name))
		}
		// 除改名外的任何列属性变化，SQLite 都无法就地修改。
		if !strings.EqualFold(strings.TrimSpace(oc.FullType), strings.TrimSpace(c.FullType)) ||
			oc.Nullable != c.Nullable || oc.Default != c.Default ||
			oc.HasDefault != c.HasDefault || oc.PrimaryKey != c.PrimaryKey {
			return nil, false
		}
	}
	for _, c := range old.Columns {
		if !kept[c.Name] {
			stmts = append(stmts, "ALTER TABLE "+table+" DROP COLUMN "+d.QuoteIdent(c.Name))
		}
	}
	if newDef.Ref.Name != "" && newDef.Ref.Name != old.Ref.Name {
		stmts = append(stmts, "ALTER TABLE "+table+" RENAME TO "+d.QuoteIdent(newDef.Ref.Name))
	}
	return stmts, true
}

func (d *Dialect) rebuildTable(old, newDef *dbx.TableDefinition) ([]string, error) {
	finalName := newDef.Ref.Name
	if finalName == "" {
		finalName = old.Ref.Name
	}
	tmpName := "navigo_tmp_" + finalName

	// 找出新旧表共有的列，只搬这部分数据。
	oldCols := map[string]bool{}
	for _, c := range old.Columns {
		oldCols[c.Name] = true
	}
	var newList, oldList []string
	for _, c := range newDef.Columns {
		orig := c.OrigName
		if orig == "" {
			orig = c.Name
		}
		if oldCols[orig] {
			newList = append(newList, d.QuoteIdent(c.Name))
			oldList = append(oldList, d.QuoteIdent(orig))
		}
	}

	stmts := []string{
		"PRAGMA foreign_keys = OFF",
		"BEGIN TRANSACTION",
		d.createTableStmt(newDef, tmpName),
	}
	if len(newList) > 0 {
		stmts = append(stmts, fmt.Sprintf("INSERT INTO %s (%s)\n  SELECT %s FROM %s",
			d.QuoteIdent(tmpName), strings.Join(newList, ", "),
			strings.Join(oldList, ", "), d.QuoteIdent(old.Ref.Name)))
	}
	stmts = append(stmts,
		"DROP TABLE "+d.QuoteIdent(old.Ref.Name),
		"ALTER TABLE "+d.QuoteIdent(tmpName)+" RENAME TO "+d.QuoteIdent(finalName),
	)
	finalRef := newDef.Ref
	finalRef.Name = finalName
	for _, idx := range newDef.Indexes {
		if idx.Primary || idx.Unique {
			// 主键与唯一约束已经写进建表语句里了。
			continue
		}
		stmts = append(stmts, d.createIndex(finalRef, idx, idx.Name))
	}
	stmts = append(stmts, "COMMIT", "PRAGMA foreign_keys = ON")
	return stmts, nil
}

func indexesEqual(a, b []dbx.Index) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]dbx.Index{}
	for _, i := range a {
		m[i.Name] = i
	}
	for _, i := range b {
		o, ok := m[i.Name]
		if !ok || o.Unique != i.Unique || o.Primary != i.Primary || len(o.Columns) != len(i.Columns) {
			return false
		}
		for k := range o.Columns {
			if o.Columns[k].Name != i.Columns[k].Name {
				return false
			}
		}
	}
	return true
}

func fksEqual(a, b []dbx.ForeignKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ReferencedTable != b[i].ReferencedTable ||
			len(a[i].Columns) != len(b[i].Columns) ||
			!strings.EqualFold(a[i].OnDelete, b[i].OnDelete) {
			return false
		}
		for k := range a[i].Columns {
			if a[i].Columns[k] != b[i].Columns[k] {
				return false
			}
		}
	}
	return true
}

func (d *Dialect) BuildDropObject(ref dbx.ObjectRef) string {
	switch ref.Kind {
	case dbx.KindView:
		return "DROP VIEW " + d.QualifyRef(ref)
	case dbx.KindTrigger:
		return "DROP TRIGGER " + d.QualifyRef(ref)
	case dbx.KindIndex:
		return "DROP INDEX " + d.QualifyRef(ref)
	}
	return "DROP TABLE " + d.QualifyRef(ref)
}

// BuildTruncate SQLite 没有 TRUNCATE，DELETE 无 WHERE 会走它的整表删除优化。
func (d *Dialect) BuildTruncate(ref dbx.ObjectRef) string {
	return "DELETE FROM " + d.QualifyRef(ref)
}

func (d *Dialect) BuildRenameObject(ref dbx.ObjectRef, newName string) string {
	return "ALTER TABLE " + d.QualifyRef(ref) + " RENAME TO " + d.QuoteIdent(newName)
}

func (d *Dialect) BuildExplain(query string, analyze bool) string {
	return "EXPLAIN QUERY PLAN " + query
}

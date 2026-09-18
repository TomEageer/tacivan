package mysqldrv

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
	"tacivan/internal/sqlbase"
)

// 元数据读取全部走 SHOW 系列命令，不碰 information_schema。
//
// 原因是实测出来的：在一个十几个库、每库上百张表的 MySQL 5.7 实例上，
// 读一张表的结构要二十秒以上直到超时。5.7 的 information_schema 是临时表实现，
// 查 COLUMNS / STATISTICS 需要逐个打开表定义，而
// KEY_COLUMN_USAGE JOIN REFERENTIAL_CONSTRAINTS 更要扫全库的约束。
//
// SHOW FULL COLUMNS / SHOW INDEX / SHOW CREATE TABLE 都只读目标表自己的定义，
// 同样的信息量，代价差了几个数量级。

// TableDefinition 读取表的完整结构。
//
// 三条 SHOW 命令并发发出，总耗时约等于一个网络往返：
//   - SHOW FULL COLUMNS  取列（含注释、字符集）
//   - SHOW INDEX         取索引
//   - SHOW CREATE TABLE  取外键、表选项与 DDL 原文
func (c *Conn) TableDefinition(ctx context.Context, ref dbx.ObjectRef) (*dbx.TableDefinition, error) {
	database := ref.Database
	if database == "" {
		database = c.cfg.Database
	}
	db, err := c.pool("")
	if err != nil {
		return nil, err
	}
	qualified := c.qualify(database, ref.Name)

	var (
		wg   sync.WaitGroup
		cols []dbx.Column
		idxs []dbx.Index
		ddl  string

		errCols, errIdx, errDDL error
	)
	wg.Add(3)
	go func() { defer wg.Done(); cols, errCols = c.showColumns(ctx, db, database, qualified) }()
	go func() { defer wg.Done(); idxs, errIdx = c.showIndexes(ctx, db, database, qualified) }()
	go func() { defer wg.Done(); ddl, errDDL = c.showCreate(ctx, db, database, qualified, ref.Kind) }()
	wg.Wait()

	if errCols != nil {
		return nil, errCols
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("对象不存在: %s.%s", database, ref.Name)
	}
	if errIdx != nil {
		return nil, errIdx
	}

	def := &dbx.TableDefinition{
		Ref:     ref,
		Columns: cols,
		Indexes: idxs,
	}
	if errDDL == nil {
		def.DDL = ddl
		// 外键与表选项都在建表语句里，顺手解析出来，省掉额外的往返。
		def.ForeignKeys = parseForeignKeys(ddl)
		opts := parseTableOptions(ddl)
		def.Engine = opts.engine
		def.Charset = opts.charset
		def.Collation = opts.collation
		def.Comment = opts.comment
		def.AutoIncrement = opts.autoInc
	}
	if def.Charset == "" && def.Collation != "" {
		def.Charset = charsetFromCollation(def.Collation)
	}
	return def, nil
}

// qualify 生成 `db`.`table` 形式。
func (c *Conn) qualify(database, name string) string {
	q := func(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
	if database == "" {
		return q(name)
	}
	return q(database) + "." + q(name)
}

// showColumns 用 SHOW FULL COLUMNS 读列定义。
func (c *Conn) showColumns(ctx context.Context, db *sql.DB, database, qualified string) ([]dbx.Column, error) {
	q := "SHOW FULL COLUMNS FROM " + qualified
	vals, err := c.metaQuery(ctx, db, "mysql.showColumns", database, q)
	if err != nil {
		return nil, err
	}

	// 列顺序：Field, Type, Collation, Null, Key, Default, Extra, Privileges, Comment
	out := make([]dbx.Column, 0, len(vals))
	for i, v := range vals {
		if len(v) < 7 {
			continue
		}
		name := sqlbase.NS(v[0])
		fullType := sqlbase.NS(v[1])
		extra := strings.ToLower(sqlbase.NS(v[6]))

		col := dbx.Column{
			Name:       name,
			OrigName:   name,
			Position:   i + 1,
			FullType:   fullType,
			Collation:  sqlbase.NS(v[2]),
			Nullable:   strings.EqualFold(sqlbase.NS(v[3]), "YES"),
			PrimaryKey: sqlbase.NS(v[4]) == "PRI",
			// SHOW COLUMNS 的 Default 为 NULL 时表示「没有默认值」，
			// 与「默认值是 NULL」在这里无法区分，按没有默认值处理。
			Default:       sqlbase.NS(v[5]),
			HasDefault:    v[5].Valid,
			AutoIncrement: strings.Contains(extra, "auto_increment"),
			DefaultIsExpr: strings.Contains(extra, "default_generated"),
		}
		if len(v) >= 9 {
			col.Comment = sqlbase.NS(v[8])
		}
		if strings.Contains(extra, "generated") && !strings.Contains(extra, "default_generated") {
			// 生成列的表达式不在 SHOW COLUMNS 里，留给 DDL 解析补充。
			col.Generated = " "
		}
		if col.Collation != "" {
			col.Charset = charsetFromCollation(col.Collation)
		}
		applyTypeSpec(&col, fullType)
		out = append(out, col)
	}
	return out, nil
}

// showIndexes 用 SHOW INDEX 读索引定义。
func (c *Conn) showIndexes(ctx context.Context, db *sql.DB, database, qualified string) ([]dbx.Index, error) {
	q := "SHOW INDEX FROM " + qualified
	vals, err := c.metaQuery(ctx, db, "mysql.showIndex", database, q)
	if err != nil {
		return nil, err
	}

	// 列顺序（5.7）：Table, Non_unique, Key_name, Seq_in_index, Column_name,
	// Collation, Cardinality, Sub_part, Packed, Null, Index_type, Comment, Index_comment
	const (
		iNonUnique  = 1
		iKeyName    = 2
		iColumn     = 4
		iCollation  = 5
		iSubPart    = 7
		iIndexType  = 10
		iIdxComment = 12
	)

	var order []string
	byName := map[string]*dbx.Index{}
	for _, v := range vals {
		if len(v) <= iIndexType {
			continue
		}
		name := sqlbase.NS(v[iKeyName])
		idx, ok := byName[name]
		if !ok {
			idx = &dbx.Index{
				Name:    name,
				Unique:  sqlbase.NS(v[iNonUnique]) == "0",
				Primary: name == "PRIMARY",
				Type:    sqlbase.NS(v[iIndexType]),
			}
			if len(v) > iIdxComment {
				idx.Comment = sqlbase.NS(v[iIdxComment])
			}
			byName[name] = idx
			order = append(order, name)
		}
		dir := "ASC"
		if sqlbase.NS(v[iCollation]) == "D" {
			dir = "DESC"
		}
		col := dbx.IndexColumn{Name: sqlbase.NS(v[iColumn]), Order: dir}
		if sub := sqlbase.NS(v[iSubPart]); sub != "" {
			col.Length, _ = strconv.ParseInt(sub, 10, 64)
		}
		idx.Columns = append(idx.Columns, col)
	}

	out := make([]dbx.Index, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	return out, nil
}

// showCreate 取建表语句原文。
func (c *Conn) showCreate(ctx context.Context, db *sql.DB, database, qualified string, kind dbx.ObjectKind) (string, error) {
	verb := "TABLE"
	switch kind {
	case dbx.KindView:
		verb = "VIEW"
	case dbx.KindFunction:
		verb = "FUNCTION"
	case dbx.KindProcedure:
		verb = "PROCEDURE"
	case dbx.KindTrigger:
		verb = "TRIGGER"
	case dbx.KindEvent:
		verb = "EVENT"
	}
	q := "SHOW CREATE " + verb + " " + qualified
	vals, err := c.metaQuery(ctx, db, "mysql.showCreate", database, q)
	if err != nil {
		return "", err
	}
	if len(vals) == 0 {
		return "", fmt.Errorf("未取得 DDL")
	}
	// SHOW CREATE 的语句列位置随对象类型不同，取第一个像 CREATE 开头的列。
	for _, v := range vals[0] {
		s := sqlbase.NS(v)
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "CREATE") {
			return s, nil
		}
	}
	return sqlbase.NS(vals[0][len(vals[0])-1]), nil
}

// --- 类型与 DDL 解析 ---

var (
	enumItemRe   = regexp.MustCompile(`'((?:[^']|'')*)'`)
	fkRe         = regexp.MustCompile("(?i)CONSTRAINT\\s+`([^`]+)`\\s+FOREIGN KEY\\s*\\(([^)]*)\\)\\s*REFERENCES\\s+(?:`([^`]+)`\\.)?`([^`]+)`\\s*\\(([^)]*)\\)([^,\\n]*)")
	identRe      = regexp.MustCompile("`([^`]+)`")
	engineRe     = regexp.MustCompile(`(?i)ENGINE=(\w+)`)
	charsetRe    = regexp.MustCompile(`(?i)(?:DEFAULT )?CHARSET=(\w+)`)
	collateRe    = regexp.MustCompile(`(?i)COLLATE=(\w+)`)
	autoIncRe    = regexp.MustCompile(`(?i)AUTO_INCREMENT=(\d+)`)
	tblCommentRe = regexp.MustCompile(`(?is)COMMENT='((?:[^']|'')*)'\s*$`)
)

// typeModifiers 跟在类型名后面的修饰符，它们不属于类型名本身。
var typeModifiers = map[string]bool{
	"unsigned": true, "zerofill": true, "binary": true, "ascii": true, "unicode": true,
}

// applyTypeSpec 把完整类型拆成基础类型、长度、精度、枚举值等。
//
// 要同时应付两种形态：MySQL 5.7 的 "int(10) unsigned" 和
// 8.0.19 起去掉显示宽度后的 "int unsigned"。早先用一个贪婪的
// ^([a-zA-Z ]+) 去取类型名，后者会把 unsigned 一并吃进去，
// 于是 int 列被当成非数字类型，生成的语句里数字都带上了引号。
func applyTypeSpec(col *dbx.Column, fullType string) {
	s := strings.TrimSpace(fullType)
	if s == "" {
		return
	}

	// 先摘出括号里的参数，剩下的就只有类型名与修饰符。
	var args string
	if i := strings.IndexByte(s, '('); i >= 0 {
		if j := strings.LastIndexByte(s, ')'); j > i {
			args = s[i+1 : j]
			s = strings.TrimSpace(s[:i] + " " + s[j+1:])
		}
	}

	lower := strings.ToLower(s)
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return
	}

	col.Type = fields[0]
	// "double precision" 是少数由两个词构成的类型名。
	if len(fields) > 1 && fields[0] == "double" && fields[1] == "precision" {
		col.Type = "double precision"
	}
	for _, f := range fields[1:] {
		if typeModifiers[f] {
			if f == "unsigned" {
				col.Unsigned = true
			}
		}
	}

	switch col.Type {
	case "enum", "set":
		for _, e := range enumItemRe.FindAllStringSubmatch(args, -1) {
			col.EnumValues = append(col.EnumValues, strings.ReplaceAll(e[1], "''", "'"))
		}
	default:
		if args == "" {
			return
		}
		parts := strings.SplitN(args, ",", 2)
		col.Length, _ = strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if len(parts) == 2 {
			scale, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			col.Scale = scale
		}
	}
}

// parseForeignKeys 从建表语句里解析外键。
func parseForeignKeys(ddl string) []dbx.ForeignKey {
	var out []dbx.ForeignKey
	for _, m := range fkRe.FindAllStringSubmatch(ddl, -1) {
		fk := dbx.ForeignKey{
			Name:              m[1],
			Columns:           parseIdentList(m[2]),
			ReferencedSchema:  m[3],
			ReferencedTable:   m[4],
			ReferencedColumns: parseIdentList(m[5]),
		}
		tail := strings.ToUpper(m[6])
		fk.OnDelete = parseRefAction(tail, "ON DELETE")
		fk.OnUpdate = parseRefAction(tail, "ON UPDATE")
		out = append(out, fk)
	}
	return out
}

func parseIdentList(s string) []string {
	var out []string
	for _, m := range identRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

func parseRefAction(tail, keyword string) string {
	i := strings.Index(tail, keyword)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(tail[i+len(keyword):])
	for _, a := range []string{"NO ACTION", "SET DEFAULT", "SET NULL", "RESTRICT", "CASCADE"} {
		if strings.HasPrefix(rest, a) {
			return a
		}
	}
	return ""
}

type tableOptions struct {
	engine    string
	charset   string
	collation string
	comment   string
	autoInc   int64
}

// parseTableOptions 解析建表语句尾部的表级选项。
func parseTableOptions(ddl string) tableOptions {
	// 只看最后一行的选项部分，避免把列定义里的 COMMENT 当成表注释。
	tail := ddl
	if i := strings.LastIndex(ddl, ")"); i >= 0 {
		tail = ddl[i:]
	}
	var o tableOptions
	if m := engineRe.FindStringSubmatch(tail); m != nil {
		o.engine = m[1]
	}
	if m := charsetRe.FindStringSubmatch(tail); m != nil {
		o.charset = m[1]
	}
	if m := collateRe.FindStringSubmatch(tail); m != nil {
		o.collation = m[1]
	}
	if m := autoIncRe.FindStringSubmatch(tail); m != nil {
		o.autoInc, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := tblCommentRe.FindStringSubmatch(strings.TrimSpace(tail)); m != nil {
		o.comment = strings.ReplaceAll(m[1], "''", "'")
	}
	return o
}

// --- 对象列表 ---

// Objects 列出库下的对象。
//
// 同样避开 information_schema：SHOW FULL TABLES 只读库的目录，
// 无论库里有多少张表都是毫秒级。行数与体积属于统计信息，
// 由 TableStats 按需单独取，不阻塞对象树的展开。
func (c *Conn) Objects(ctx context.Context, database, schema string, kinds []dbx.ObjectKind) ([]dbx.ObjectInfo, error) {
	if database == "" {
		database = c.cfg.Database
	}
	db, err := c.pool("")
	if err != nil {
		return nil, err
	}
	want := map[dbx.ObjectKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	if len(kinds) == 0 {
		want[dbx.KindTable] = true
	}
	qdb := "`" + strings.ReplaceAll(database, "`", "``") + "`"

	var out []dbx.ObjectInfo

	if want[dbx.KindTable] || want[dbx.KindView] {
		vals, err := c.metaQuery(ctx, db, "mysql.showTables", database, "SHOW FULL TABLES FROM "+qdb)
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			if len(v) < 2 {
				continue
			}
			kind := dbx.KindTable
			if strings.Contains(strings.ToUpper(sqlbase.NS(v[1])), "VIEW") {
				kind = dbx.KindView
			}
			if !want[kind] {
				continue
			}
			out = append(out, dbx.ObjectInfo{Name: sqlbase.NS(v[0]), Kind: kind})
		}
	}

	if want[dbx.KindFunction] || want[dbx.KindProcedure] {
		for _, spec := range []struct {
			kind dbx.ObjectKind
			verb string
		}{
			{dbx.KindProcedure, "PROCEDURE"},
			{dbx.KindFunction, "FUNCTION"},
		} {
			if !want[spec.kind] {
				continue
			}
			// SHOW PROCEDURE STATUS 的 WHERE 走的是结果集过滤，不扫 information_schema。
			q := "SHOW " + spec.verb + " STATUS WHERE Db = ?"
			vals, err := c.metaQuery(ctx, db, "mysql.showRoutines", database, q, database)
			if err != nil {
				continue
			}
			for _, v := range vals {
				if len(v) < 2 {
					continue
				}
				info := dbx.ObjectInfo{Name: sqlbase.NS(v[1]), Kind: spec.kind}
				if len(v) > 7 {
					info.Comment = sqlbase.NS(v[7])
				}
				out = append(out, info)
			}
		}
	}

	if want[dbx.KindTrigger] {
		vals, err := c.metaQuery(ctx, db, "mysql.showTriggers", database, "SHOW TRIGGERS FROM "+qdb)
		if err == nil {
			for _, v := range vals {
				if len(v) < 5 {
					continue
				}
				out = append(out, dbx.ObjectInfo{
					Name: sqlbase.NS(v[0]), Kind: dbx.KindTrigger,
					Comment: fmt.Sprintf("%s %s ON %s", sqlbase.NS(v[4]), sqlbase.NS(v[1]), sqlbase.NS(v[2])),
				})
			}
		}
	}

	if want[dbx.KindEvent] {
		vals, err := c.metaQuery(ctx, db, "mysql.showEvents", database, "SHOW EVENTS FROM "+qdb)
		if err == nil {
			for _, v := range vals {
				if len(v) < 2 {
					continue
				}
				out = append(out, dbx.ObjectInfo{Name: sqlbase.NS(v[1]), Kind: dbx.KindEvent})
			}
		}
	}
	return out, nil
}

// TableStats 取一个库里所有表的行数与体积。
//
// SHOW TABLE STATUS 需要 InnoDB 采样统计，在大库上有明显成本，
// 因此它是独立的一次调用：对象树先把表名列出来，统计信息随后再填。
func (c *Conn) TableStats(ctx context.Context, database string) (map[string]dbx.ObjectInfo, error) {
	if database == "" {
		database = c.cfg.Database
	}
	db, err := c.pool("")
	if err != nil {
		return nil, err
	}
	qdb := "`" + strings.ReplaceAll(database, "`", "``") + "`"
	vals, err := c.metaQuery(ctx, db, "mysql.showTableStatus", database, "SHOW TABLE STATUS FROM "+qdb)
	if err != nil {
		return nil, err
	}

	// 列顺序：Name, Engine, Version, Row_format, Rows, Avg_row_length,
	// Data_length, Max_data_length, Index_length, Data_free, Auto_increment,
	// Create_time, Update_time, Check_time, Collation, Checksum,
	// Create_options, Comment
	out := make(map[string]dbx.ObjectInfo, len(vals))
	for _, v := range vals {
		if len(v) < 12 {
			continue
		}
		info := dbx.ObjectInfo{
			Name:      sqlbase.NS(v[0]),
			Engine:    sqlbase.NS(v[1]),
			Rows:      parseInt64(sqlbase.NS(v[4])),
			DataSize:  parseInt64(sqlbase.NS(v[6])),
			IndexSize: parseInt64(sqlbase.NS(v[8])),
		}
		if len(v) > 14 {
			info.Collation = sqlbase.NS(v[14])
		}
		if len(v) > 17 {
			info.Comment = sqlbase.NS(v[17])
		}
		if len(v) > 12 {
			info.UpdatedAt = sqlbase.NS(v[12])
		}
		out[info.Name] = info
	}
	return out, nil
}

// ObjectDDL 取对象的建表语句。
func (c *Conn) ObjectDDL(ctx context.Context, ref dbx.ObjectRef) (string, error) {
	database := ref.Database
	if database == "" {
		database = c.cfg.Database
	}
	db, err := c.pool("")
	if err != nil {
		return "", err
	}
	return c.showCreate(ctx, db, database, c.qualify(database, ref.Name), ref.Kind)
}

// Databases 列出数据库。
//
// SHOW DATABASES 只读服务器目录，比 information_schema.SCHEMATA 快得多；
// 字符集属于库级属性，对象树上用不到，就不额外花一次往返去取。
func (c *Conn) Databases(ctx context.Context) ([]dbx.DatabaseInfo, error) {
	db, err := c.pool("")
	if err != nil {
		return nil, err
	}
	start := time.Now()
	vals, err := c.metaQuery(ctx, db, "mysql.showDatabases", "", "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	logx.Op("mysql.Databases", start, "count", len(vals))

	out := make([]dbx.DatabaseInfo, 0, len(vals))
	for _, v := range vals {
		if len(v) == 0 {
			continue
		}
		name := sqlbase.NS(v[0])
		out = append(out, dbx.DatabaseInfo{
			Name:    name,
			Current: name == c.cfg.Database,
			System:  systemSchemas[strings.ToLower(name)],
		})
	}
	return out, nil
}

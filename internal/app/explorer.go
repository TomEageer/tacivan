package app

import (
	"fmt"
	"strings"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
	"tacivan/internal/sqlbase"
)

// ListDatabases 列出连接下的数据库。
func (a *App) ListDatabases(connID string) ([]dbx.DatabaseInfo, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()

	start := time.Now()
	defer func() { logx.Op("ListDatabases", start, "conn", s.cfg.Name) }()

	// 自定义列表启用时直接返回，一条语句都不发。
	//
	// 这才是这个功能的意义所在：那台 3165 个库的 MySQL 5.7 上，
	// 光 SHOW DATABASES 就要 1.4 秒，回来还要渲染三千多个树节点，
	// 而真正会打开的不超过十个。过滤放在拿到结果之后做是没用的。
	if s.cfg.UseDatabaseList && len(s.cfg.DatabaseList) > 0 {
		out := make([]dbx.DatabaseInfo, 0, len(s.cfg.DatabaseList))
		for _, e := range s.cfg.DatabaseList {
			if e.Name == "" {
				continue
			}
			out = append(out, dbx.DatabaseInfo{Name: e.Name, AutoOpen: e.AutoOpen})
		}
		return out, nil
	}

	var list []dbx.DatabaseInfo
	switch c := s.conn.(type) {
	case dbx.SQLConn:
		list, err = c.Databases(ctx)
	case dbx.KVConn:
		list, err = c.Databases(ctx)
	default:
		return nil, fmt.Errorf("该连接不支持列出数据库")
	}
	if err != nil {
		return nil, err
	}
	if !a.store.Settings().ShowSystemObjects {
		kept := make([]dbx.DatabaseInfo, 0, len(list))
		for _, d := range list {
			if !d.System {
				kept = append(kept, d)
			}
		}
		list = kept
	}
	return list, nil
}

// ListAllDatabases 无视自定义列表，向服务端实拉一次全部库名。
// 只给「连接配置里挑库」这一个场景用——对象树永远不走这里。
func (a *App) ListAllDatabases(connID string) ([]dbx.DatabaseInfo, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(60)
	defer cancel()
	start := time.Now()
	defer func() { logx.Op("ListAllDatabases", start, "conn", s.cfg.Name) }()

	switch c := s.conn.(type) {
	case dbx.SQLConn:
		return c.Databases(ctx)
	case dbx.KVConn:
		return c.Databases(ctx)
	}
	return nil, fmt.Errorf("该连接不支持列出数据库")
}

// ListSchemas 列出库下的 schema（仅 PostgreSQL）。
func (a *App) ListSchemas(connID, database string) ([]dbx.SchemaInfo, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()
	list, err := sc.Schemas(ctx, database)
	if err != nil {
		return nil, err
	}
	if !a.store.Settings().ShowSystemObjects {
		kept := make([]dbx.SchemaInfo, 0, len(list))
		for _, s := range list {
			if !s.System {
				kept = append(kept, s)
			}
		}
		list = kept
	}
	return list, nil
}

// ObjectNode 对象树上的一个节点，附带格式化好的展示文本。
type ObjectNode struct {
	dbx.ObjectInfo
	// SizeText 人类可读的体积，如 12.4 MB。
	SizeText string `json:"sizeText"`
	// RowsText 估算行数的展示文本。
	RowsText string `json:"rowsText"`
}

// ListObjects 列出某个库/schema 下的对象。
func (a *App) ListObjects(connID, database, schema string, kinds []string) ([]ObjectNode, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return nil, err
	}
	ks := make([]dbx.ObjectKind, 0, len(kinds))
	for _, k := range kinds {
		ks = append(ks, dbx.ObjectKind(k))
	}
	ctx, cancel := a.requestCtx(60)
	defer cancel()
	start := time.Now()
	list, err := sc.Objects(ctx, database, schema, ks)
	logx.Op("ListObjects", start, "db", database, "kinds", fmt.Sprint(kinds), "count", len(list))
	if err != nil {
		return nil, err
	}
	out := make([]ObjectNode, 0, len(list))
	for _, o := range list {
		node := ObjectNode{ObjectInfo: o}
		if o.DataSize > 0 || o.IndexSize > 0 {
			node.SizeText = sqlbase.FormatBytes(o.DataSize + o.IndexSize)
		}
		if o.Rows > 0 {
			node.RowsText = formatCount(o.Rows)
		}
		out = append(out, node)
	}
	return out, nil
}

// GetTableStats 取某个库的表统计信息（行数、体积）。
//
// 对象树先用 ListObjects 把名字列出来，这个接口随后异步补上数字——
// 统计信息在大实例上可能要数秒，不该让用户盯着空树等。
func (a *App) GetTableStats(connID, database string) (map[string]ObjectNode, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return nil, err
	}
	provider, ok := s.conn.(dbx.StatsProvider)
	if !ok {
		return map[string]ObjectNode{}, nil
	}
	ctx, cancel := a.requestCtx(120)
	defer cancel()

	start := time.Now()
	raw, err := provider.TableStats(ctx, database)
	logx.Op("GetTableStats", start, "db", database, "count", len(raw))
	if err != nil {
		return nil, err
	}
	out := make(map[string]ObjectNode, len(raw))
	for name, o := range raw {
		node := ObjectNode{ObjectInfo: o}
		if o.DataSize > 0 || o.IndexSize > 0 {
			node.SizeText = sqlbase.FormatBytes(o.DataSize + o.IndexSize)
		}
		if o.Rows > 0 {
			node.RowsText = formatCount(o.Rows)
		}
		out[name] = node
	}
	return out, nil
}

func formatCount(n int64) string {
	switch {
	case n >= 100_000_000:
		return fmt.Sprintf("%.1f 亿", float64(n)/1e8)
	case n >= 10_000:
		return fmt.Sprintf("%.1f 万", float64(n)/1e4)
	}
	return fmt.Sprintf("%d", n)
}

// GetTableDefinition 返回表的完整定义，供表设计器使用。
func (a *App) GetTableDefinition(connID string, ref dbx.ObjectRef) (*dbx.TableDefinition, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()
	return sc.TableDefinition(ctx, ref)
}

// GetObjectDDL 返回对象的建表/建视图语句。
func (a *App) GetObjectDDL(connID string, ref dbx.ObjectRef) (string, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return "", err
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()
	return sc.ObjectDDL(ctx, ref)
}

// DDLPreview 一组待执行语句的预览，用于「先看 SQL 再执行」。
type DDLPreview struct {
	Statements []string `json:"statements"`
	// SQL 拼好的完整脚本，供复制。
	SQL string `json:"sql"`
}

func makePreview(stmts []string) DDLPreview {
	var b strings.Builder
	for _, s := range stmts {
		b.WriteString(s)
		if !strings.HasSuffix(strings.TrimSpace(s), ";") {
			b.WriteString(";")
		}
		b.WriteString("\n\n")
	}
	return DDLPreview{Statements: stmts, SQL: strings.TrimSpace(b.String())}
}

// PreviewCreateTable 生成建表语句但不执行。
func (a *App) PreviewCreateTable(connID string, def dbx.TableDefinition) (DDLPreview, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return DDLPreview{}, err
	}
	stmts, err := sc.Dialect().BuildCreateTable(&def)
	if err != nil {
		return DDLPreview{}, err
	}
	return makePreview(stmts), nil
}

// PreviewAlterTable 比对新旧定义生成变更语句但不执行。
func (a *App) PreviewAlterTable(connID string, oldDef, newDef dbx.TableDefinition) (DDLPreview, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return DDLPreview{}, err
	}
	stmts, err := sc.Dialect().BuildAlterTable(&oldDef, &newDef)
	if err != nil {
		return DDLPreview{}, err
	}
	if len(stmts) == 0 {
		return DDLPreview{Statements: []string{}, SQL: ""}, nil
	}
	return makePreview(stmts), nil
}

// CreateTable 执行建表。
func (a *App) CreateTable(connID string, def dbx.TableDefinition) error {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	stmts, err := sc.Dialect().BuildCreateTable(&def)
	if err != nil {
		return err
	}
	return a.execAll(connID, def.Ref.Database, stmts)
}

// AlterTable 执行表结构变更。
func (a *App) AlterTable(connID string, oldDef, newDef dbx.TableDefinition) error {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	stmts, err := sc.Dialect().BuildAlterTable(&oldDef, &newDef)
	if err != nil {
		return err
	}
	if len(stmts) == 0 {
		return nil
	}
	return a.execAll(connID, oldDef.Ref.Database, stmts)
}

// execAll 按顺序执行一组语句，任意一条失败即中止并带上位置信息。
func (a *App) execAll(connID, database string, stmts []string) error {
	s, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	if s.cfg.ReadOnly {
		return fmt.Errorf("当前连接为只读，已拦截结构变更操作")
	}
	ctx, cancel := a.requestCtx(120)
	defer cancel()
	for i, stmt := range stmts {
		if _, err := sc.Exec(ctx, database, stmt); err != nil {
			return fmt.Errorf("执行第 %d 条语句失败：%w\n\n%s", i+1, err, stmt)
		}
	}
	return nil
}

// DropObject 删除对象。
func (a *App) DropObject(connID string, ref dbx.ObjectRef) error {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	return a.execAll(connID, ref.Database, []string{sc.Dialect().BuildDropObject(ref)})
}

// TruncateTable 清空表。
func (a *App) TruncateTable(connID string, ref dbx.ObjectRef) error {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	return a.execAll(connID, ref.Database, []string{sc.Dialect().BuildTruncate(ref)})
}

// RenameObject 重命名对象。
func (a *App) RenameObject(connID string, ref dbx.ObjectRef, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("新名称不能为空")
	}
	if newName == ref.Name {
		return nil
	}
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	return a.execAll(connID, ref.Database, []string{sc.Dialect().BuildRenameObject(ref, newName)})
}

// CreateDatabase 新建数据库。
func (a *App) CreateDatabase(connID, name, charset, collation string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("数据库名不能为空")
	}
	s, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	d := sc.Dialect()
	stmt := "CREATE DATABASE " + d.QuoteIdent(name)
	switch s.cfg.Engine {
	case dbx.EngineMySQL, dbx.EngineMariaDB:
		if charset != "" {
			stmt += " DEFAULT CHARACTER SET " + charset
		}
		if collation != "" {
			stmt += " DEFAULT COLLATE " + collation
		}
	case dbx.EnginePostgres:
		if charset != "" {
			stmt += " ENCODING " + d.QuoteString(charset)
		}
	default:
		return fmt.Errorf("%s 不支持新建数据库", s.cfg.Engine.DisplayName())
	}
	return a.execAll(connID, "", []string{stmt})
}

// DropDatabase 删除数据库。
func (a *App) DropDatabase(connID, name string) error {
	s, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	if s.cfg.Engine == dbx.EngineSQLite {
		return fmt.Errorf("SQLite 的数据库就是文件本身，请直接删除文件")
	}
	return a.execAll(connID, "", []string{"DROP DATABASE " + sc.Dialect().QuoteIdent(name)})
}

// CreateSchema 新建 schema（PostgreSQL）。
func (a *App) CreateSchema(connID, database, name string) error {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("schema 名不能为空")
	}
	return a.execAll(connID, database, []string{"CREATE SCHEMA " + sc.Dialect().QuoteIdent(name)})
}

// DropSchema 删除 schema。
func (a *App) DropSchema(connID, database, name string, cascade bool) error {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return err
	}
	stmt := "DROP SCHEMA " + sc.Dialect().QuoteIdent(name)
	if cascade {
		stmt += " CASCADE"
	}
	return a.execAll(connID, database, []string{stmt})
}

// ServerVariables 返回服务器变量/状态，用于「服务器监控」面板。
func (a *App) ServerVariables(connID string) (map[string]string, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()

	if kv, ok := s.conn.(dbx.KVConn); ok {
		return kv.ServerStats(ctx)
	}
	sc, ok := s.conn.(dbx.SQLConn)
	if !ok {
		return nil, fmt.Errorf("该连接不支持查看服务器变量")
	}

	var query string
	switch s.cfg.Engine {
	case dbx.EngineMySQL, dbx.EngineMariaDB:
		query = "SHOW GLOBAL VARIABLES"
	case dbx.EnginePostgres:
		query = "SELECT name, setting FROM pg_settings ORDER BY name"
	case dbx.EngineSQLite:
		// SQLite 没有变量表，用一组关键 PRAGMA 代替。
		out := map[string]string{}
		for _, p := range []string{"journal_mode", "synchronous", "cache_size", "page_size", "foreign_keys", "temp_store", "auto_vacuum"} {
			st, err := sc.Query(ctx, "", "PRAGMA "+p, a.scanOptions())
			if err != nil {
				continue
			}
			if row, err := st.Next(); err == nil && len(row) > 0 {
				out[p] = row[0].Text
			}
			st.Close()
		}
		return out, nil
	default:
		return nil, fmt.Errorf("该连接不支持查看服务器变量")
	}

	stream, err := sc.Query(ctx, "", query, a.scanOptions())
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	out := map[string]string{}
	for {
		row, err := stream.Next()
		if err != nil {
			break
		}
		if len(row) >= 2 {
			out[row[0].Text] = row[1].Text
		}
	}
	return out, nil
}

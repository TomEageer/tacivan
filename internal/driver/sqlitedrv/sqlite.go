// Package sqlitedrv 实现 SQLite 驱动。
//
// 用 modernc.org/sqlite（纯 Go 实现，不依赖 CGO），这样整个程序仍是单一静态二进制，
// 交叉编译和分发都不需要额外的 C 工具链。
package sqlitedrv

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"tacivan/internal/dbx"
	"tacivan/internal/sqlbase"
)

func init() {
	dbx.Register(dialer{})
}

type dialer struct{}

func (dialer) Engine() dbx.Engine { return dbx.EngineSQLite }

func (dialer) Open(ctx context.Context, cfg dbx.ConnectionConfig) (dbx.Conn, error) {
	path := strings.TrimSpace(cfg.Database)
	if path == "" {
		return nil, fmt.Errorf("请指定 SQLite 数据库文件路径")
	}
	if path != ":memory:" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = abs
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("数据库文件不存在: %s", path)
			}
			return nil, err
		}
	}

	c := &Conn{cfg: cfg, path: path}
	if err := c.open(); err != nil {
		return nil, err
	}
	if err := c.Ping(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Conn 一个 SQLite 数据库文件。
type Conn struct {
	cfg  dbx.ConnectionConfig
	path string

	mu     sync.Mutex
	db     *sql.DB
	closed bool
}

func (c *Conn) open() error {
	dsn := c.path
	q := url.Values{}
	// WAL 让读不阻塞写；busy_timeout 避免并发下直接报 database is locked。
	q.Set("_pragma", "busy_timeout(5000)")
	if c.cfg.ReadOnly {
		q.Set("mode", "ro")
	}
	if len(q) > 0 && c.path != ":memory:" {
		dsn = "file:" + c.path + "?" + q.Encode()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("打开数据库文件失败: %w", err)
	}
	// SQLite 写入是单写者模型，连接数开大反而增加锁竞争。
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	c.db = db
	if !c.cfg.ReadOnly {
		_, _ = db.Exec("PRAGMA journal_mode=WAL")
	}
	_, _ = db.Exec("PRAGMA foreign_keys=ON")
	return nil
}

func (c *Conn) Engine() dbx.Engine { return dbx.EngineSQLite }

func (c *Conn) Capability() dbx.Capability {
	return dbx.Capability{
		SQL: true, Views: true, Triggers: true, ForeignKeys: true,
		Transactions: true, ExplainPlan: true,
	}
}

func (c *Conn) handle() (*sql.DB, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.db == nil {
		return nil, fmt.Errorf("连接已关闭")
	}
	return c.db, nil
}

func (c *Conn) Ping(ctx context.Context) error {
	db, err := c.handle()
	if err != nil {
		return err
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("无法打开 %s: %w", c.path, err)
	}
	return nil
}

func (c *Conn) ServerInfo(ctx context.Context) (dbx.ServerInfo, error) {
	db, err := c.handle()
	if err != nil {
		return dbx.ServerInfo{}, err
	}
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return dbx.ServerInfo{}, err
	}
	info := dbx.ServerInfo{
		Engine: dbx.EngineSQLite, Version: version, Charset: "UTF-8",
		Extra: map[string]string{"file": c.path},
	}
	if st, err := os.Stat(c.path); err == nil {
		info.Extra["size"] = sqlbase.FormatBytes(st.Size())
	}
	var journal string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err == nil {
		info.Extra["journal_mode"] = journal
	}
	return info, nil
}

func (c *Conn) Dialect() dbx.Dialect { return NewDialect() }

// Databases SQLite 一个连接就是一个文件，用 PRAGMA database_list 取已挂载的库。
func (c *Conn) Databases(ctx context.Context) ([]dbx.DatabaseInfo, error) {
	db, err := c.handle()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, err
	}
	vals, err := sqlbase.ScanValues(rows)
	if err != nil {
		return nil, err
	}
	out := make([]dbx.DatabaseInfo, 0, len(vals))
	for _, v := range vals {
		name := sqlbase.NS(v[1])
		out = append(out, dbx.DatabaseInfo{
			Name: name, Charset: "UTF-8",
			Current: name == "main",
			System:  name == "temp",
		})
	}
	if len(out) == 0 {
		out = append(out, dbx.DatabaseInfo{Name: "main", Charset: "UTF-8", Current: true})
	}
	return out, nil
}

func (c *Conn) Schemas(ctx context.Context, database string) ([]dbx.SchemaInfo, error) {
	return nil, nil
}

func (c *Conn) Objects(ctx context.Context, database, schema string, kinds []dbx.ObjectKind) ([]dbx.ObjectInfo, error) {
	db, err := c.handle()
	if err != nil {
		return nil, err
	}
	if database == "" {
		database = "main"
	}
	want := map[dbx.ObjectKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	if len(kinds) == 0 {
		want[dbx.KindTable] = true
	}

	// database 来自对象树的已知库名，仍用标识符引用防止拼接出问题。
	q := fmt.Sprintf(
		"SELECT name, type, sql FROM %s.sqlite_master WHERE type IN ('table','view','trigger','index') ORDER BY type, name",
		sqlbase.QuoteIdentWith(database, '"'))
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	vals, err := sqlbase.ScanValues(rows)
	if err != nil {
		return nil, err
	}

	var out []dbx.ObjectInfo
	for _, v := range vals {
		name := sqlbase.NS(v[0])
		var kind dbx.ObjectKind
		switch sqlbase.NS(v[1]) {
		case "table":
			kind = dbx.KindTable
		case "view":
			kind = dbx.KindView
		case "trigger":
			kind = dbx.KindTrigger
		case "index":
			kind = dbx.KindIndex
		default:
			continue
		}
		if !want[kind] {
			continue
		}
		// 自动创建的内部索引没有 sql，不展示。
		if kind == dbx.KindIndex && !v[2].Valid {
			continue
		}
		if strings.HasPrefix(name, "sqlite_") {
			continue
		}
		// 行数不在这里查：逐表 COUNT(*) 在表多或表大时会让展开卡住，
		// 交给 TableStats 按需取。
		out = append(out, dbx.ObjectInfo{Name: name, Kind: kind})
	}
	return out, nil
}

// TableStats 逐表统计行数。SQLite 没有现成的统计信息，只能实际数一遍，
// 因此它是独立调用，不阻塞对象树。
func (c *Conn) TableStats(ctx context.Context, database string) (map[string]dbx.ObjectInfo, error) {
	db, err := c.handle()
	if err != nil {
		return nil, err
	}
	if database == "" {
		database = "main"
	}
	qdb := sqlbase.QuoteIdentWith(database, '"')

	rows, err := db.QueryContext(ctx, fmt.Sprintf(
		"SELECT name FROM %s.sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%%'", qdb))
	if err != nil {
		return nil, err
	}
	names, err := sqlbase.ScanValues(rows)
	if err != nil {
		return nil, err
	}

	out := make(map[string]dbx.ObjectInfo, len(names))
	for _, n := range names {
		name := sqlbase.NS(n[0])
		var count int64
		q := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", qdb, sqlbase.QuoteIdentWith(name, '"'))
		if err := db.QueryRowContext(ctx, q).Scan(&count); err != nil {
			continue
		}
		out[name] = dbx.ObjectInfo{Name: name, Kind: dbx.KindTable, Rows: count}
	}
	return out, nil
}

func (c *Conn) TableDefinition(ctx context.Context, ref dbx.ObjectRef) (*dbx.TableDefinition, error) {
	db, err := c.handle()
	if err != nil {
		return nil, err
	}
	database := ref.Database
	if database == "" {
		database = "main"
	}
	qdb := sqlbase.QuoteIdentWith(database, '"')
	qname := sqlbase.QuoteIdentWith(ref.Name, '"')
	def := &dbx.TableDefinition{Ref: ref}

	// 列
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA %s.table_info(%s)", qdb, qname))
	if err != nil {
		return nil, err
	}
	vals, err := sqlbase.ScanValues(rows)
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("对象不存在: %s", ref.Name)
	}
	for i, v := range vals {
		declType := sqlbase.NS(v[2])
		col := dbx.Column{
			Name: sqlbase.NS(v[1]), OrigName: sqlbase.NS(v[1]),
			Position:   i + 1,
			Type:       baseTypeName(declType),
			FullType:   declType,
			Nullable:   sqlbase.NS(v[3]) == "0",
			Default:    sqlbase.NS(v[4]),
			HasDefault: v[4].Valid,
			PrimaryKey: sqlbase.NS(v[5]) != "0",
			// SQLite 的默认值就是表达式文本，原样写回。
			DefaultIsExpr: true,
		}
		def.Columns = append(def.Columns, col)
	}

	// INTEGER PRIMARY KEY 即 rowid 别名，等价于自增。
	if len(def.Columns) > 0 {
		pkCount := 0
		for _, col := range def.Columns {
			if col.PrimaryKey {
				pkCount++
			}
		}
		if pkCount == 1 {
			for i := range def.Columns {
				if def.Columns[i].PrimaryKey && strings.EqualFold(def.Columns[i].Type, "integer") {
					def.Columns[i].AutoIncrement = true
				}
			}
		}
	}

	// 索引
	rows, err = db.QueryContext(ctx, fmt.Sprintf("PRAGMA %s.index_list(%s)", qdb, qname))
	if err != nil {
		return nil, err
	}
	idxVals, err := sqlbase.ScanValues(rows)
	if err != nil {
		return nil, err
	}
	for _, v := range idxVals {
		idxName := sqlbase.NS(v[1])
		origin := sqlbase.NS(v[3]) // c=CREATE INDEX, u=UNIQUE 约束, pk=主键
		idx := dbx.Index{
			Name:    idxName,
			Unique:  sqlbase.NS(v[2]) == "1",
			Primary: origin == "pk",
			Type:    "BTREE",
		}
		ir, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA %s.index_info(%s)", qdb, sqlbase.QuoteIdentWith(idxName, '"')))
		if err != nil {
			return nil, err
		}
		iv, err := sqlbase.ScanValues(ir)
		if err != nil {
			return nil, err
		}
		for _, c2 := range iv {
			name := sqlbase.NS(c2[2])
			if name == "" {
				name = "(表达式)"
			}
			idx.Columns = append(idx.Columns, dbx.IndexColumn{Name: name, Order: "ASC"})
		}
		def.Indexes = append(def.Indexes, idx)
	}

	// INTEGER PRIMARY KEY 是 rowid 的别名，SQLite 不为它建独立索引，
	// PRAGMA index_list 里也就看不到。这里按列上的 pk 标志补一条主键索引出来，
	// 否则上层会认为该表没有主键，数据网格会退化成只读。
	hasPrimaryIndex := false
	for _, idx := range def.Indexes {
		if idx.Primary {
			hasPrimaryIndex = true
			break
		}
	}
	if !hasPrimaryIndex {
		var pkCols []dbx.IndexColumn
		for _, c := range def.Columns {
			if c.PrimaryKey {
				pkCols = append(pkCols, dbx.IndexColumn{Name: c.Name, Order: "ASC"})
			}
		}
		if len(pkCols) > 0 {
			def.Indexes = append([]dbx.Index{{
				Name: "PRIMARY", Primary: true, Unique: true, Type: "BTREE", Columns: pkCols,
			}}, def.Indexes...)
		}
	}

	// 外键
	rows, err = db.QueryContext(ctx, fmt.Sprintf("PRAGMA %s.foreign_key_list(%s)", qdb, qname))
	if err != nil {
		return nil, err
	}
	fkVals, err := sqlbase.ScanValues(rows)
	if err != nil {
		return nil, err
	}
	fkByID := map[string]*dbx.ForeignKey{}
	var fkOrder []string
	for _, v := range fkVals {
		id := sqlbase.NS(v[0])
		fk, ok := fkByID[id]
		if !ok {
			fk = &dbx.ForeignKey{
				Name:            fmt.Sprintf("fk_%s_%s", ref.Name, id),
				ReferencedTable: sqlbase.NS(v[2]),
				OnUpdate:        sqlbase.NS(v[5]),
				OnDelete:        sqlbase.NS(v[6]),
			}
			fkByID[id] = fk
			fkOrder = append(fkOrder, id)
		}
		fk.Columns = append(fk.Columns, sqlbase.NS(v[3]))
		fk.ReferencedColumns = append(fk.ReferencedColumns, sqlbase.NS(v[4]))
	}
	for _, id := range fkOrder {
		def.ForeignKeys = append(def.ForeignKeys, *fkByID[id])
	}

	if ddl, err := c.ObjectDDL(ctx, ref); err == nil {
		def.DDL = ddl
	}
	return def, nil
}

// baseTypeName 从声明类型里取出不含长度的基础类型名。
func baseTypeName(declType string) string {
	if i := strings.IndexByte(declType, '('); i > 0 {
		return strings.TrimSpace(declType[:i])
	}
	return strings.TrimSpace(declType)
}

func (c *Conn) ObjectDDL(ctx context.Context, ref dbx.ObjectRef) (string, error) {
	db, err := c.handle()
	if err != nil {
		return "", err
	}
	database := ref.Database
	if database == "" {
		database = "main"
	}
	q := fmt.Sprintf("SELECT sql FROM %s.sqlite_master WHERE name = ?", sqlbase.QuoteIdentWith(database, '"'))
	var ddl sql.NullString
	if err := db.QueryRowContext(ctx, q, ref.Name).Scan(&ddl); err != nil {
		return "", err
	}
	if !ddl.Valid {
		return "", fmt.Errorf("该对象没有可用的 DDL")
	}
	return ddl.String + ";", nil
}

// classify SQLite 是动态类型，按声明类型的 affinity 规则归类。
func classify(declType string) dbx.ValueClass {
	t := strings.ToUpper(declType)
	switch {
	case t == "":
		return dbx.ClassUnknown
	case strings.Contains(t, "INT"):
		return dbx.ClassNumber
	case strings.Contains(t, "CHAR"), strings.Contains(t, "CLOB"), strings.Contains(t, "TEXT"):
		return dbx.ClassString
	case strings.Contains(t, "BLOB"):
		return dbx.ClassBinary
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"),
		strings.Contains(t, "NUM"), strings.Contains(t, "DEC"):
		return dbx.ClassNumber
	case strings.Contains(t, "BOOL"):
		return dbx.ClassBool
	case strings.Contains(t, "DATE"), strings.Contains(t, "TIME"):
		return dbx.ClassTime
	case strings.Contains(t, "JSON"):
		return dbx.ClassJSON
	}
	return dbx.ClassUnknown
}

func (c *Conn) Query(ctx context.Context, database, query string, opts dbx.ScanOptions, args ...any) (dbx.RowStream, error) {
	db, err := c.handle()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return sqlbase.NewRowStream(rows, classify, opts, nil)
}

func (c *Conn) Exec(ctx context.Context, database, stmt string, args ...any) (dbx.ExecResult, error) {
	db, err := c.handle()
	if err != nil {
		return dbx.ExecResult{}, err
	}
	start := time.Now()
	res, err := db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return dbx.ExecResult{}, err
	}
	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	return dbx.ExecResult{
		RowsAffected: affected, LastInsertID: lastID,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.db != nil {
		err := c.db.Close()
		c.db = nil
		return err
	}
	return nil
}

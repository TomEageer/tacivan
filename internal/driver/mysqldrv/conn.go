// Package mysqldrv 实现 MySQL / MariaDB 驱动。
package mysqldrv

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	gomysql "github.com/go-sql-driver/mysql"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
	"tacivan/internal/sqlbase"
	"tacivan/internal/sshtun"
)

func init() {
	dbx.Register(dialer{engine: dbx.EngineMySQL})
	dbx.Register(dialer{engine: dbx.EngineMariaDB})
}

type dialer struct{ engine dbx.Engine }

func (d dialer) Engine() dbx.Engine { return d.engine }

func (d dialer) Open(ctx context.Context, cfg dbx.ConnectionConfig) (dbx.Conn, error) {
	c := &Conn{cfg: cfg, engine: d.engine, pools: map[string]*sql.DB{}}
	if cfg.SSH.Enabled {
		t, err := sshtun.Open(cfg.SSH, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
		if err != nil {
			return nil, err
		}
		c.tunnel = t
	}
	if cfg.TLS.Enabled {
		name, err := registerTLS(cfg)
		if err != nil {
			c.closeTunnel()
			return nil, err
		}
		c.tlsName = name
	}
	if err := c.Ping(ctx); err != nil {
		c.Close()
		return nil, err
	}
	info, err := c.ServerInfo(ctx)
	if err == nil {
		c.version = info.VersionNum
	}
	return c, nil
}

// Conn 一条 MySQL 连接。
//
// 每个数据库对应一个独立的连接池：切库不靠 USE 语句改变共享会话状态，
// 避免「A 标签页切库把 B 标签页的当前库也改了」这类串扰。
type Conn struct {
	cfg    dbx.ConnectionConfig
	engine dbx.Engine

	mu    sync.Mutex
	pools map[string]*sql.DB

	tunnel  *sshtun.Tunnel
	tlsName string
	version int
	closed  bool
}

func (c *Conn) Engine() dbx.Engine { return c.engine }

func (c *Conn) Capability() dbx.Capability {
	return dbx.Capability{
		SQL: true, MultiDatabase: true, Views: true, Functions: true,
		Procedures: true, Triggers: true, Events: true, ForeignKeys: true,
		Transactions: true, ExplainPlan: true, ServerVariable: true,
	}
}

// dsn 构造给定库的 DSN。
func (c *Conn) dsn(database string) string {
	cfg := gomysql.NewConfig()
	cfg.User = c.cfg.User
	cfg.Passwd = c.cfg.Password
	cfg.Net = "tcp"
	if c.tunnel != nil {
		cfg.Net = c.tunnel.NetName()
		cfg.Addr = c.tunnel.Target()
	} else {
		port := c.cfg.Port
		if port == 0 {
			port = 3306
		}
		cfg.Addr = net.JoinHostPort(c.cfg.Host, strconv.Itoa(port))
	}
	cfg.DBName = database
	// parseTime 必须关闭：结果集统一按原始字节扫描，交由前端按列类型渲染，
	// 打开后 DATETIME 会变成 time.Time，RawBytes 扫描会失败。
	cfg.ParseTime = false
	cfg.InterpolateParams = false
	cfg.MultiStatements = false
	cfg.ClientFoundRows = true
	// 不手写 charset 参数：DSN 参数会被 URL 编码，逗号变成 %2C 后
	// 会被 MySQL 当成非法的 SET NAMES 语句。驱动默认已是 utf8mb4。
	if len(c.cfg.Params) > 0 {
		cfg.Params = map[string]string{}
		for k, v := range c.cfg.Params {
			cfg.Params[k] = v
		}
	}
	timeout := time.Duration(c.cfg.ConnectTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cfg.Timeout = timeout
	// 协议级 zlib 压缩。宽表在慢链路上传输量大，开了能省不少；
	// 本机直连反而是白搭 CPU，所以交给连接配置决定，不默认开。
	if c.cfg.Compress {
		// compress 是私有字段，只能通过 Option 设进去。
		_ = cfg.Apply(gomysql.EnableCompression(true))
	}
	if c.tlsName != "" {
		cfg.TLSConfig = c.tlsName
	}
	return cfg.FormatDSN()
}

// pool 取得（或懒建）指定库的连接池。
func (c *Conn) pool(database string) (*sql.DB, error) {
	if database == "" {
		database = c.cfg.Database
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("连接已关闭")
	}
	if db, ok := c.pools[database]; ok {
		return db, nil
	}
	db, err := sql.Open("mysql", c.dsn(database))
	if err != nil {
		return nil, fmt.Errorf("打开连接失败: %w", err)
	}
	// 每个打开的结果集会独占一条连接直到关闭，池子要留出余量；
	// 同时让空闲连接自动回收，不像某些客户端一直挂着几十条会话。
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(2 * time.Hour)
	c.pools[database] = db
	return db, nil
}

func (c *Conn) Ping(ctx context.Context) error {
	db, err := c.pool("")
	if err != nil {
		return err
	}
	start := time.Now()
	perr := db.PingContext(ctx)
	logx.SQL("mysql.ping", c.cfg.Database, "PING", start, -1, perr)
	if err := perr; err != nil {
		return fmt.Errorf("无法连接到 %s: %w", c.cfg.Host, err)
	}
	return nil
}

var versionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func (c *Conn) ServerInfo(ctx context.Context) (dbx.ServerInfo, error) {
	db, err := c.pool("")
	if err != nil {
		return dbx.ServerInfo{}, err
	}
	var version, charset, tz string
	row := db.QueryRowContext(ctx, "SELECT VERSION(), @@character_set_server, @@time_zone")
	if err := row.Scan(&version, &charset, &tz); err != nil {
		return dbx.ServerInfo{}, err
	}
	info := dbx.ServerInfo{
		Engine:     c.engine,
		Version:    version,
		VersionNum: parseVersion(version),
		Charset:    charset,
		Timezone:   tz,
		Extra:      map[string]string{},
	}
	var uptime sql.NullString
	if err := db.QueryRowContext(ctx, "SHOW GLOBAL STATUS LIKE 'Uptime'").Scan(new(string), &uptime); err == nil && uptime.Valid {
		info.Extra["uptime"] = uptime.String
	}
	return info, nil
}

func parseVersion(v string) int {
	m := versionRe.FindStringSubmatch(v)
	if len(m) != 4 {
		return 0
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return maj*10000 + min*100 + patch
}

func (c *Conn) Dialect() dbx.Dialect { return NewDialect(c.engine, c.version) }

var systemSchemas = map[string]bool{
	"information_schema": true, "performance_schema": true, "mysql": true, "sys": true,
}

// metaQuery 执行一条元数据查询并记录耗时。
//
// 所有 information_schema 查询都走这里，日志里就能直接看出
// 打开一张表的时间到底花在哪一条语句上。
func (c *Conn) metaQuery(ctx context.Context, db *sql.DB, op, database, query string, args ...any) ([][]sql.NullString, error) {
	start := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		logx.SQL(op, database, query, start, -1, err)
		return nil, err
	}
	vals, err := sqlbase.ScanValues(rows)
	logx.SQL(op, database, query, start, len(vals), err)
	return vals, err
}

// Schemas MySQL 没有独立的 schema 层。
func (c *Conn) Schemas(ctx context.Context, database string) ([]dbx.SchemaInfo, error) {
	return nil, nil
}

func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

var enumValueRe = regexp.MustCompile(`'((?:[^']|'')*)'`)

// classify 把 MySQL 类型名归一到展示类别。
func classify(dbType string) dbx.ValueClass {
	switch strings.ToUpper(dbType) {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT",
		"DECIMAL", "FLOAT", "DOUBLE", "YEAR", "BIT", "UNSIGNED TINYINT",
		"UNSIGNED SMALLINT", "UNSIGNED MEDIUMINT", "UNSIGNED INT", "UNSIGNED BIGINT":
		return dbx.ClassNumber
	case "DATE", "DATETIME", "TIMESTAMP", "TIME":
		return dbx.ClassTime
	case "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY", "GEOMETRY":
		return dbx.ClassBinary
	case "JSON":
		return dbx.ClassJSON
	case "CHAR", "VARCHAR", "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT", "ENUM", "SET":
		return dbx.ClassString
	}
	return dbx.ClassUnknown
}

func (c *Conn) Query(ctx context.Context, database, query string, opts dbx.ScanOptions, args ...any) (dbx.RowStream, error) {
	db, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	rows, err := db.QueryContext(ctx, query, args...)
	// 这里记的是「语句发出到服务端返回首批结果」的耗时；
	// 行数据是流式读的，真正的读取耗时体现在后续的 fetch 上。
	logx.SQL("mysql.query", database, query, start, -1, err)
	if err != nil {
		return nil, err
	}
	return sqlbase.NewRowStream(rows, classify, opts, nil)
}

func (c *Conn) Exec(ctx context.Context, database, stmt string, args ...any) (dbx.ExecResult, error) {
	db, err := c.pool(database)
	if err != nil {
		return dbx.ExecResult{}, err
	}
	start := time.Now()
	res, err := db.ExecContext(ctx, stmt, args...)
	logx.SQL("mysql.exec", database, stmt, start, -1, err)
	if err != nil {
		return dbx.ExecResult{}, err
	}
	affected, _ := res.RowsAffected()
	lastID, _ := res.LastInsertId()
	return dbx.ExecResult{
		RowsAffected: affected,
		LastInsertID: lastID,
		DurationMS:   time.Since(start).Milliseconds(),
	}, nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	pools := make([]*sql.DB, 0, len(c.pools))
	for _, db := range c.pools {
		pools = append(pools, db)
	}
	c.pools = map[string]*sql.DB{}
	c.mu.Unlock()

	for _, db := range pools {
		_ = db.Close()
	}
	c.closeTunnel()
	return nil
}

func (c *Conn) closeTunnel() {
	if c.tunnel != nil {
		c.tunnel.Close()
		c.tunnel = nil
	}
}

var tlsSeq struct {
	sync.Mutex
	n int
}

// registerTLS 把连接的 TLS 配置注册到 go-sql-driver 的全局表并返回其名字。
func registerTLS(cfg dbx.ConnectionConfig) (string, error) {
	tlsCfg := &tls.Config{
		ServerName:         cfg.Host,
		InsecureSkipVerify: cfg.TLS.InsecureSkipVerify,
	}
	if cfg.TLS.CAFile != "" {
		pem, err := os.ReadFile(cfg.TLS.CAFile)
		if err != nil {
			return "", fmt.Errorf("读取 CA 证书失败: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return "", fmt.Errorf("CA 证书格式无效: %s", cfg.TLS.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return "", fmt.Errorf("读取客户端证书失败: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	tlsSeq.Lock()
	tlsSeq.n++
	name := fmt.Sprintf("navigo-%s-%d", cfg.ID, tlsSeq.n)
	tlsSeq.Unlock()
	if err := gomysql.RegisterTLSConfig(name, tlsCfg); err != nil {
		return "", err
	}
	return name, nil
}

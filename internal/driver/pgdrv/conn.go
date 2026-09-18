// Package pgdrv 实现 PostgreSQL 驱动。
//
// 这里直接用 pgx 原生接口而不是 database/sql：pgx 的 Rows.RawValues() 能拿到
// 服务端原样返回的字节，配合强制文本格式，结果集取数路径上零类型转换、零额外分配。
package pgdrv

import (
	"context"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tacivan/internal/dbx"
	"tacivan/internal/sshtun"
)

func init() {
	dbx.Register(dialer{})
}

type dialer struct{}

func (dialer) Engine() dbx.Engine { return dbx.EnginePostgres }

func (dialer) Open(ctx context.Context, cfg dbx.ConnectionConfig) (dbx.Conn, error) {
	c := &Conn{cfg: cfg, pools: map[string]*pgxpool.Pool{}}
	if cfg.SSH.Enabled {
		port := cfg.Port
		if port == 0 {
			port = 5432
		}
		t, err := sshtun.Open(cfg.SSH, net.JoinHostPort(cfg.Host, strconv.Itoa(port)))
		if err != nil {
			return nil, err
		}
		c.tunnel = t
	}
	if err := c.Ping(ctx); err != nil {
		c.Close()
		return nil, err
	}
	if info, err := c.ServerInfo(ctx); err == nil {
		c.version = info.VersionNum
	}
	return c, nil
}

// Conn 一条 PostgreSQL 连接。
type Conn struct {
	cfg dbx.ConnectionConfig

	mu    sync.Mutex
	pools map[string]*pgxpool.Pool

	tunnel  *sshtun.Tunnel
	version int
	closed  bool
}

func (c *Conn) Engine() dbx.Engine { return dbx.EnginePostgres }

func (c *Conn) Capability() dbx.Capability {
	return dbx.Capability{
		SQL: true, Schemas: true, MultiDatabase: true, Views: true,
		Functions: true, Procedures: true, Triggers: true, Sequences: true,
		ForeignKeys: true, Transactions: true, ExplainPlan: true, ServerVariable: true,
	}
}

func (c *Conn) connString(database string) string {
	port := c.cfg.Port
	if port == 0 {
		port = 5432
	}
	if database == "" {
		database = c.cfg.Database
	}
	if database == "" {
		database = "postgres"
	}
	timeout := c.cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = 10
	}
	sslmode := "prefer"
	if c.cfg.TLS.Enabled {
		if c.cfg.TLS.InsecureSkipVerify {
			sslmode = "require"
		} else if c.cfg.TLS.CAFile != "" {
			sslmode = "verify-full"
		} else {
			sslmode = "require"
		}
	} else {
		sslmode = "disable"
	}

	parts := []string{
		"host=" + c.cfg.Host,
		"port=" + strconv.Itoa(port),
		"dbname=" + database,
		"connect_timeout=" + strconv.Itoa(timeout),
		"sslmode=" + sslmode,
		"application_name=navigo",
	}
	if c.cfg.User != "" {
		parts = append(parts, "user="+c.cfg.User)
	}
	if c.cfg.Password != "" {
		parts = append(parts, "password="+escapeConnValue(c.cfg.Password))
	}
	if c.cfg.TLS.CAFile != "" {
		parts = append(parts, "sslrootcert="+c.cfg.TLS.CAFile)
	}
	if c.cfg.TLS.CertFile != "" {
		parts = append(parts, "sslcert="+c.cfg.TLS.CertFile)
	}
	if c.cfg.TLS.KeyFile != "" {
		parts = append(parts, "sslkey="+c.cfg.TLS.KeyFile)
	}
	for k, v := range c.cfg.Params {
		parts = append(parts, k+"="+escapeConnValue(v))
	}
	return strings.Join(parts, " ")
}

// escapeConnValue 关键字/值格式的连接串里，含空格或单引号的值要加引号。
func escapeConnValue(v string) string {
	if !strings.ContainsAny(v, " '\\") {
		return v
	}
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(v) + "'"
}

func (c *Conn) pool(database string) (*pgxpool.Pool, error) {
	if database == "" {
		database = c.cfg.Database
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("连接已关闭")
	}
	if p, ok := c.pools[database]; ok {
		return p, nil
	}
	pcfg, err := pgxpool.ParseConfig(c.connString(database))
	if err != nil {
		return nil, fmt.Errorf("连接参数无效: %w", err)
	}
	pcfg.MaxConns = 8
	pcfg.MinConns = 0
	pcfg.MaxConnIdleTime = 5 * time.Minute
	pcfg.MaxConnLifetime = 2 * time.Hour
	if c.tunnel != nil {
		t := c.tunnel
		pcfg.ConnConfig.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return t.DialContext(ctx, network, addr)
		}
	}
	p, err := pgxpool.NewWithConfig(context.Background(), pcfg)
	if err != nil {
		return nil, err
	}
	c.pools[database] = p
	return p, nil
}

func (c *Conn) Ping(ctx context.Context) error {
	p, err := c.pool("")
	if err != nil {
		return err
	}
	if err := p.Ping(ctx); err != nil {
		return fmt.Errorf("无法连接到 %s: %w", c.cfg.Host, err)
	}
	return nil
}

var pgVersionRe = regexp.MustCompile(`PostgreSQL (\d+)\.?(\d+)?`)

func (c *Conn) ServerInfo(ctx context.Context) (dbx.ServerInfo, error) {
	p, err := c.pool("")
	if err != nil {
		return dbx.ServerInfo{}, err
	}
	var version, encoding, tz string
	err = p.QueryRow(ctx, "SELECT version(), current_setting('server_encoding'), current_setting('TimeZone')").
		Scan(&version, &encoding, &tz)
	if err != nil {
		return dbx.ServerInfo{}, err
	}
	num := 0
	if m := pgVersionRe.FindStringSubmatch(version); len(m) >= 2 {
		maj, _ := strconv.Atoi(m[1])
		min := 0
		if len(m) > 2 && m[2] != "" {
			min, _ = strconv.Atoi(m[2])
		}
		num = maj*10000 + min*100
	}
	return dbx.ServerInfo{
		Engine: dbx.EnginePostgres, Version: version, VersionNum: num,
		Charset: encoding, Timezone: tz,
	}, nil
}

func (c *Conn) Dialect() dbx.Dialect { return NewDialect(c.version) }

func (c *Conn) Databases(ctx context.Context) ([]dbx.DatabaseInfo, error) {
	p, err := c.pool("")
	if err != nil {
		return nil, err
	}
	rows, err := p.Query(ctx, `
		SELECT d.datname, pg_encoding_to_char(d.encoding), d.datcollate, d.datistemplate
		FROM pg_database d
		WHERE d.datallowconn
		ORDER BY d.datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.DatabaseInfo
	for rows.Next() {
		var name, enc, coll string
		var isTemplate bool
		if err := rows.Scan(&name, &enc, &coll, &isTemplate); err != nil {
			return nil, err
		}
		out = append(out, dbx.DatabaseInfo{
			Name: name, Charset: enc, Collation: coll,
			Current: name == c.cfg.Database,
			System:  isTemplate || name == "postgres",
		})
	}
	return out, rows.Err()
}

func (c *Conn) Schemas(ctx context.Context, database string) ([]dbx.SchemaInfo, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	rows, err := p.Query(ctx, `
		SELECT n.nspname, pg_get_userbyid(n.nspowner)
		FROM pg_namespace n
		WHERE n.nspname NOT LIKE 'pg_temp%' AND n.nspname NOT LIKE 'pg_toast%'
		ORDER BY n.nspname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbx.SchemaInfo
	for rows.Next() {
		var name, owner string
		if err := rows.Scan(&name, &owner); err != nil {
			return nil, err
		}
		out = append(out, dbx.SchemaInfo{
			Name: name, Owner: owner,
			System: name == "information_schema" || strings.HasPrefix(name, "pg_"),
		})
	}
	return out, rows.Err()
}

func (c *Conn) Objects(ctx context.Context, database, schema string, kinds []dbx.ObjectKind) ([]dbx.ObjectInfo, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	if schema == "" {
		schema = "public"
	}
	want := map[dbx.ObjectKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	if len(kinds) == 0 {
		want[dbx.KindTable] = true
	}

	var out []dbx.ObjectInfo
	if want[dbx.KindTable] || want[dbx.KindView] || want[dbx.KindSequence] {
		rows, err := p.Query(ctx, `
			SELECT c.relname, c.relkind,
			       COALESCE(obj_description(c.oid, 'pg_class'), ''),
			       COALESCE(c.reltuples, 0)::bigint,
			       pg_table_size(c.oid), pg_indexes_size(c.oid)
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind = ANY($2)
			ORDER BY c.relname`, schema, []string{"r", "p", "v", "m", "S", "f"})
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, relkind, comment string
			var tuples, dataSize, idxSize int64
			if err := rows.Scan(&name, &relkind, &comment, &tuples, &dataSize, &idxSize); err != nil {
				return nil, err
			}
			var kind dbx.ObjectKind
			switch relkind {
			case "v", "m":
				kind = dbx.KindView
			case "S":
				kind = dbx.KindSequence
			default:
				kind = dbx.KindTable
			}
			if !want[kind] {
				continue
			}
			if tuples < 0 {
				tuples = 0
			}
			out = append(out, dbx.ObjectInfo{
				Name: name, Kind: kind, Schema: schema, Comment: comment,
				Rows: tuples, DataSize: dataSize, IndexSize: idxSize,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if want[dbx.KindFunction] || want[dbx.KindProcedure] {
		rows, err := p.Query(ctx, `
			SELECT p.proname, p.prokind, COALESCE(obj_description(p.oid, 'pg_proc'), ''),
			       pg_get_function_identity_arguments(p.oid)
			FROM pg_proc p
			JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = $1 AND p.prokind IN ('f','p')
			ORDER BY p.proname`, schema)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, prokind, comment, args string
			if err := rows.Scan(&name, &prokind, &comment, &args); err != nil {
				return nil, err
			}
			kind := dbx.KindFunction
			if prokind == "p" {
				kind = dbx.KindProcedure
			}
			if !want[kind] {
				continue
			}
			label := comment
			if args != "" {
				label = strings.TrimSpace("(" + args + ") " + comment)
			}
			out = append(out, dbx.ObjectInfo{Name: name, Kind: kind, Schema: schema, Comment: label})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if want[dbx.KindTrigger] {
		rows, err := p.Query(ctx, `
			SELECT t.tgname, c.relname
			FROM pg_trigger t
			JOIN pg_class c ON c.oid = t.tgrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND NOT t.tgisinternal
			ORDER BY t.tgname`, schema)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, table string
			if err := rows.Scan(&name, &table); err != nil {
				return nil, err
			}
			out = append(out, dbx.ObjectInfo{Name: name, Kind: dbx.KindTrigger, Schema: schema, Comment: "ON " + table})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *Conn) TableDefinition(ctx context.Context, ref dbx.ObjectRef) (*dbx.TableDefinition, error) {
	return c.tableDefinition(ctx, ref, true)
}

// tableDefinition 读取表结构。
//
// withDDL 用来打断递归：PostgreSQL 没有 SHOW CREATE TABLE，DDL 是由表结构反向
// 生成的，而 ObjectDDL 又需要表结构——生成 DDL 时必须走 withDDL=false 这一路。
func (c *Conn) tableDefinition(ctx context.Context, ref dbx.ObjectRef, withDDL bool) (*dbx.TableDefinition, error) {
	p, err := c.pool(ref.Database)
	if err != nil {
		return nil, err
	}
	schema := ref.Schema
	if schema == "" {
		schema = "public"
	}
	def := &dbx.TableDefinition{Ref: ref}

	rows, err := p.Query(ctx, `
		SELECT a.attname, a.attnum,
		       format_type(a.atttypid, NULL) AS base_type,
		       format_type(a.atttypid, a.atttypmod) AS full_type,
		       NOT a.attnotnull,
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), ''),
		       ad.adbin IS NOT NULL,
		       COALESCE(col_description(a.attrelid, a.attnum), ''),
		       a.attidentity <> '' OR COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '') LIKE 'nextval(%',
		       COALESCE(a.attgenerated, '') <> '',
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '')
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, schema, ref.Name)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, baseType, fullType, def0, comment, genExpr string
		var attnum int
		var nullable, hasDefault, isSerial, isGenerated bool
		if err := rows.Scan(&name, &attnum, &baseType, &fullType, &nullable, &def0, &hasDefault, &comment, &isSerial, &isGenerated, &genExpr); err != nil {
			rows.Close()
			return nil, err
		}
		col := dbx.Column{
			Name: name, OrigName: name, Position: attnum,
			Type: baseType, FullType: fullType,
			Nullable: nullable, Default: def0, HasDefault: hasDefault,
			// PostgreSQL 的默认值本来就是表达式文本，直接原样写回。
			DefaultIsExpr: true,
			AutoIncrement: isSerial,
			Comment:       comment,
		}
		if isGenerated {
			col.Generated = genExpr
		}
		if l := typeLength(fullType); l > 0 {
			col.Length = l
		}
		def.Columns = append(def.Columns, col)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(def.Columns) == 0 {
		return nil, fmt.Errorf("对象不存在: %s.%s", schema, ref.Name)
	}

	// 索引（主键也是索引）
	idxRows, err := p.Query(ctx, `
		SELECT i.relname, ix.indisunique, ix.indisprimary, am.amname,
		       a.attname, COALESCE(ix.indoption[k.ord - 1] & 1, 0) = 1
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_am am ON am.oid = i.relam
		JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		LEFT JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE n.nspname = $1 AND t.relname = $2
		ORDER BY i.relname, k.ord`, schema, ref.Name)
	if err != nil {
		return nil, err
	}
	idxOrder := []string{}
	idxByName := map[string]*dbx.Index{}
	for idxRows.Next() {
		var idxName, amName string
		var unique, primary, desc bool
		var colName *string
		if err := idxRows.Scan(&idxName, &unique, &primary, &amName, &colName, &desc); err != nil {
			idxRows.Close()
			return nil, err
		}
		idx, ok := idxByName[idxName]
		if !ok {
			idx = &dbx.Index{Name: idxName, Unique: unique, Primary: primary, Type: amName}
			idxByName[idxName] = idx
			idxOrder = append(idxOrder, idxName)
		}
		// 表达式索引的列名为 NULL，用占位符展示而不是丢掉整个索引。
		n := "(表达式)"
		if colName != nil {
			n = *colName
		}
		order := "ASC"
		if desc {
			order = "DESC"
		}
		idx.Columns = append(idx.Columns, dbx.IndexColumn{Name: n, Order: order})
	}
	idxRows.Close()
	if err := idxRows.Err(); err != nil {
		return nil, err
	}
	for _, n := range idxOrder {
		i := idxByName[n]
		if i.Primary {
			for k := range def.Columns {
				for _, ic := range i.Columns {
					if def.Columns[k].Name == ic.Name {
						def.Columns[k].PrimaryKey = true
					}
				}
			}
		}
		def.Indexes = append(def.Indexes, *i)
	}

	// 外键
	fkRows, err := p.Query(ctx, `
		SELECT con.conname,
		       (SELECT array_agg(att.attname ORDER BY k.ord)
		          FROM unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
		          JOIN pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = k.attnum),
		       fn.nspname, fc.relname,
		       (SELECT array_agg(att.attname ORDER BY k.ord)
		          FROM unnest(con.confkey) WITH ORDINALITY AS k(attnum, ord)
		          JOIN pg_attribute att ON att.attrelid = con.confrelid AND att.attnum = k.attnum),
		       con.confupdtype, con.confdeltype
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class fc ON fc.oid = con.confrelid
		JOIN pg_namespace fn ON fn.oid = fc.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND con.contype = 'f'
		ORDER BY con.conname`, schema, ref.Name)
	if err != nil {
		return nil, err
	}
	for fkRows.Next() {
		var name, refSchema, refTable string
		var cols, refCols []string
		var updRule, delRule string
		if err := fkRows.Scan(&name, &cols, &refSchema, &refTable, &refCols, &updRule, &delRule); err != nil {
			fkRows.Close()
			return nil, err
		}
		if refSchema == schema {
			refSchema = ""
		}
		def.ForeignKeys = append(def.ForeignKeys, dbx.ForeignKey{
			Name: name, Columns: cols,
			ReferencedSchema: refSchema, ReferencedTable: refTable, ReferencedColumns: refCols,
			OnUpdate: fkAction(updRule), OnDelete: fkAction(delRule),
		})
	}
	fkRows.Close()
	if err := fkRows.Err(); err != nil {
		return nil, err
	}

	_ = p.QueryRow(ctx, `
		SELECT COALESCE(obj_description(c.oid, 'pg_class'), '')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2`, schema, ref.Name).Scan(&def.Comment)

	if withDDL {
		if ddl, err := c.ObjectDDL(ctx, ref); err == nil {
			def.DDL = ddl
		}
	}
	return def, nil
}

func fkAction(code string) string {
	switch code {
	case "a":
		return "NO ACTION"
	case "r":
		return "RESTRICT"
	case "c":
		return "CASCADE"
	case "n":
		return "SET NULL"
	case "d":
		return "SET DEFAULT"
	}
	return ""
}

var typeLenRe = regexp.MustCompile(`\((\d+)`)

func typeLength(fullType string) int64 {
	if m := typeLenRe.FindStringSubmatch(fullType); len(m) == 2 {
		v, _ := strconv.ParseInt(m[1], 10, 64)
		return v
	}
	return 0
}

// ObjectDDL PostgreSQL 没有 SHOW CREATE TABLE，表的 DDL 由方言按定义反向生成。
func (c *Conn) ObjectDDL(ctx context.Context, ref dbx.ObjectRef) (string, error) {
	p, err := c.pool(ref.Database)
	if err != nil {
		return "", err
	}
	schema := ref.Schema
	if schema == "" {
		schema = "public"
	}
	switch ref.Kind {
	case dbx.KindView:
		var src string
		err := p.QueryRow(ctx, `SELECT pg_get_viewdef(($1 || '.' || $2)::regclass, true)`,
			quoteIdentForCast(schema), quoteIdentForCast(ref.Name)).Scan(&src)
		if err != nil {
			return "", err
		}
		d := NewDialect(c.version)
		return "CREATE OR REPLACE VIEW " + d.QualifyRef(ref) + " AS\n" + src, nil

	case dbx.KindFunction, dbx.KindProcedure:
		var src string
		err := p.QueryRow(ctx, `
			SELECT pg_get_functiondef(p.oid)
			FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = $1 AND p.proname = $2
			LIMIT 1`, schema, ref.Name).Scan(&src)
		if err != nil {
			return "", err
		}
		return src, nil
	}

	def, err := c.tableDefinition(ctx,
		dbx.ObjectRef{Database: ref.Database, Schema: schema, Name: ref.Name, Kind: dbx.KindTable}, false)
	if err != nil {
		return "", err
	}
	stmts, err := NewDialect(c.version).BuildCreateTable(def)
	if err != nil {
		return "", err
	}
	return strings.Join(stmts, ";\n\n") + ";", nil
}

func quoteIdentForCast(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// --- 结果集 ---

// rowStream 把 pgx.Rows 适配成 dbx.RowStream。
type rowStream struct {
	rows pgx.Rows
	cols []dbx.ColumnMeta
	opts dbx.ScanOptions
	// release 归还连接池中的连接。
	release func()
	closed  bool
	binary  []bool
}

func (s *rowStream) Columns() []dbx.ColumnMeta { return s.cols }

func (s *rowStream) Next() (dbx.Row, error) {
	if s.closed {
		return nil, io.EOF
	}
	if !s.rows.Next() {
		if err := s.rows.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	raw := s.rows.RawValues()
	row := make(dbx.Row, len(raw))
	for i, b := range raw {
		isBinary := i < len(s.binary) && s.binary[i]
		row[i] = makeCell(b, isBinary, s.opts)
	}
	return row, nil
}

func (s *rowStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.rows.Close()
	if s.release != nil {
		s.release()
	}
	return nil
}

func makeCell(b []byte, isBinary bool, opts dbx.ScanOptions) dbx.Cell {
	if b == nil {
		return dbx.Cell{Null: true}
	}
	size := len(b)
	if isBinary || !utf8.Valid(b) {
		limit := opts.BinaryPreviewLimit
		src := b
		truncated := false
		if len(src) > limit {
			src = src[:limit]
			truncated = true
		}
		return dbx.Cell{Text: hexEncode(src), Binary: true, Truncated: truncated, Size: size}
	}
	if size > opts.PreviewLimit {
		cut := opts.PreviewLimit
		for cut > 0 && !utf8.RuneStart(b[cut]) {
			cut--
		}
		return dbx.Cell{Text: string(b[:cut]), Truncated: true, Size: size}
	}
	return dbx.Cell{Text: string(b), Size: size}
}

const hexDigits = "0123456789abcdef"

func hexEncode(b []byte) string {
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexDigits[c>>4]
		out[i*2+1] = hexDigits[c&0x0f]
	}
	return string(out)
}

func (c *Conn) Query(ctx context.Context, database, query string, opts dbx.ScanOptions, args ...any) (dbx.RowStream, error) {
	p, err := c.pool(database)
	if err != nil {
		return nil, err
	}
	conn, err := p.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	// 强制文本格式：参数仍走扩展协议（保持参数化），但结果按服务端文本原样返回，
	// 省掉一整套二进制解码，RawValues 直接就是可展示的字节。
	rows, err := conn.Query(ctx, query, append([]any{pgx.QueryResultFormats{pgx.TextFormatCode}}, args...)...)
	if err != nil {
		conn.Release()
		return nil, err
	}

	opts = opts.Normalize()
	fds := rows.FieldDescriptions()
	cols := make([]dbx.ColumnMeta, len(fds))
	binary := make([]bool, len(fds))
	typeMap := conn.Conn().TypeMap()
	for i, fd := range fds {
		typeName := oidName(typeMap, fd.DataTypeOID)
		class := classifyPG(typeName)
		cols[i] = dbx.ColumnMeta{Name: fd.Name, Type: typeName, Class: class, Nullable: -1}
		binary[i] = class == dbx.ClassBinary
	}
	released := false
	return &rowStream{
		rows: rows, cols: cols, opts: opts, binary: binary,
		release: func() {
			if !released {
				released = true
				conn.Release()
			}
		},
	}, nil
}

func oidName(m *pgtype.Map, oid uint32) string {
	if t, ok := m.TypeForOID(oid); ok {
		return t.Name
	}
	return "oid:" + strconv.FormatUint(uint64(oid), 10)
}

func classifyPG(t string) dbx.ValueClass {
	switch strings.ToLower(t) {
	case "int2", "int4", "int8", "smallint", "integer", "bigint",
		"numeric", "decimal", "float4", "float8", "real", "money", "oid":
		return dbx.ClassNumber
	case "bool", "boolean":
		return dbx.ClassBool
	case "date", "time", "timetz", "timestamp", "timestamptz", "interval":
		return dbx.ClassTime
	case "bytea":
		return dbx.ClassBinary
	case "json", "jsonb":
		return dbx.ClassJSON
	case "text", "varchar", "bpchar", "char", "name", "uuid", "inet", "cidr", "macaddr", "xml":
		return dbx.ClassString
	}
	return dbx.ClassUnknown
}

func (c *Conn) Exec(ctx context.Context, database, stmt string, args ...any) (dbx.ExecResult, error) {
	p, err := c.pool(database)
	if err != nil {
		return dbx.ExecResult{}, err
	}
	start := time.Now()
	tag, err := p.Exec(ctx, stmt, args...)
	if err != nil {
		return dbx.ExecResult{}, err
	}
	return dbx.ExecResult{
		RowsAffected: tag.RowsAffected(),
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
	pools := make([]*pgxpool.Pool, 0, len(c.pools))
	for _, p := range c.pools {
		pools = append(pools, p)
	}
	c.pools = map[string]*pgxpool.Pool{}
	c.mu.Unlock()

	for _, p := range pools {
		p.Close()
	}
	if c.tunnel != nil {
		c.tunnel.Close()
		c.tunnel = nil
	}
	return nil
}

// Package plugindrv 把一个外部插件进程包装成 dbx.SQLConn。
//
// 插件负责「拿数据」和它自己的业务（对象树长什么样、节点是什么状态、右键有哪些动作）；
// 主程序这一层只做通用的事：进程管理、门禁、把结果包成流。所有安全相关的——
// 只读、单语句、强制 LIMIT、黑名单——都在这里做，插件自己再做一遍是加分项。
package plugindrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/driver/mysqldrv"
	"tacivan/internal/driver/pgdrv"
	"tacivan/internal/driver/sqlitedrv"
	"tacivan/internal/logx"
	"tacivan/internal/plugin"
	"tacivan/internal/sqlutil"
)

// 主程序注入的两样东西：反向调用的处理器、每个插件的数据目录。
var (
	hostHandler plugin.HostHandler
	dataDirFor  func(pluginID string) string
)

// SetHost 注入反向调用处理器与数据目录函数，由主程序启动时调用一次。
func SetHost(h plugin.HostHandler, dataDir func(pluginID string) string) {
	hostHandler, dataDirFor = h, dataDir
}

// Register 把一份清单注册成一个引擎。
func Register(m *plugin.Manifest) {
	dbx.Register(dialer{m: m})
}

type dialer struct{ m *plugin.Manifest }

func (d dialer) Engine() dbx.Engine { return dbx.Engine(d.m.Engine()) }

func (d dialer) Open(ctx context.Context, cfg dbx.ConnectionConfig) (dbx.Conn, error) {
	config := map[string]string{}
	for _, f := range d.m.Fields {
		v := cfg.Params[f.Key]
		if v == "" {
			v = f.Default
		}
		if f.Required && v == "" {
			return nil, fmt.Errorf("缺少必填项「%s」", f.Label)
		}
		config[f.Key] = v
	}
	config["__database"] = cfg.Database
	config["__connId"] = cfg.ID
	if dataDirFor != nil {
		config["__dataDir"] = dataDirFor(d.m.ID)
	}

	proc, err := plugin.Start(ctx, d.m, config,
		func(f string, a ...any) { logx.Info(fmt.Sprintf(f, a...)) }, hostHandler)
	if err != nil {
		return nil, err
	}
	return &Conn{m: d.m, proc: proc, cfg: cfg, dialect: newDialect(d.m)}, nil
}

// Conn 插件连接。
type Conn struct {
	m       *plugin.Manifest
	proc    *plugin.Process
	cfg     dbx.ConnectionConfig
	dialect dbx.Dialect
	once    sync.Once
}

func (c *Conn) Engine() dbx.Engine { return dbx.Engine(c.m.Engine()) }

func (c *Conn) Capability() dbx.Capability {
	return dbx.Capability{SQL: true, MultiDatabase: true, Views: true}
}

func (c *Conn) ServerInfo(ctx context.Context) (dbx.ServerInfo, error) {
	var out struct {
		Version string            `json:"version"`
		Extra   map[string]string `json:"extra"`
	}
	err := c.proc.Call(ctx, "serverInfo", nil, &out)
	if err != nil && !errors.Is(err, plugin.ErrMethodNotFound) {
		return dbx.ServerInfo{}, err
	}
	if out.Version == "" {
		out.Version = c.m.Name + " " + c.m.Version
	}
	return dbx.ServerInfo{Engine: c.Engine(), Version: out.Version, Extra: out.Extra}, nil
}

func (c *Conn) Ping(ctx context.Context) error {
	err := c.proc.Call(ctx, "ping", nil, nil)
	if errors.Is(err, plugin.ErrMethodNotFound) {
		return nil
	}
	return err
}

func (c *Conn) Close() error {
	var err error
	c.once.Do(func() { err = c.proc.Close() })
	return err
}

// nodeInfo 插件返回的树节点：名字之外还能带状态、说明和颜色。
type nodeInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Comment string `json:"comment"`
	Status  string `json:"status"`
	Detail  string `json:"detail"`
	Color   string `json:"color"`
}

func (c *Conn) Databases(ctx context.Context) ([]dbx.DatabaseInfo, error) {
	var out []nodeInfo
	if err := c.proc.Call(ctx, "databases", nil, &out); err != nil {
		if errors.Is(err, plugin.ErrMethodNotFound) {
			if c.cfg.Database != "" {
				return []dbx.DatabaseInfo{{Name: c.cfg.Database, Current: true}}, nil
			}
			return nil, nil
		}
		return nil, err
	}
	list := make([]dbx.DatabaseInfo, 0, len(out))
	for _, d := range out {
		list = append(list, dbx.DatabaseInfo{
			Name: d.Name, Current: d.Name == c.cfg.Database,
			Status: d.Status, Detail: d.Detail, Color: d.Color,
		})
	}
	return list, nil
}

func (c *Conn) Schemas(context.Context, string) ([]dbx.SchemaInfo, error) { return nil, nil }

func (c *Conn) Objects(ctx context.Context, database, schema string, kinds []dbx.ObjectKind) ([]dbx.ObjectInfo, error) {
	var out []nodeInfo
	ks := make([]string, 0, len(kinds))
	for _, k := range kinds {
		ks = append(ks, string(k))
	}
	err := c.proc.Call(ctx, "objects", map[string]any{"database": database, "kinds": ks}, &out)
	if err != nil {
		if errors.Is(err, plugin.ErrMethodNotFound) {
			return nil, nil
		}
		return nil, err
	}
	want := map[dbx.ObjectKind]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	list := make([]dbx.ObjectInfo, 0, len(out))
	for _, o := range out {
		k := dbx.ObjectKind(o.Kind)
		if k == "" {
			k = dbx.KindTable
		}
		if len(want) > 0 && !want[k] {
			continue
		}
		list = append(list, dbx.ObjectInfo{Name: o.Name, Kind: k, Comment: o.Comment,
			Status: o.Status, Detail: o.Detail, Color: o.Color})
	}
	return list, nil
}

func (c *Conn) TableDefinition(ctx context.Context, ref dbx.ObjectRef) (*dbx.TableDefinition, error) {
	var out struct {
		Columns []struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			Nullable   bool   `json:"nullable"`
			Comment    string `json:"comment"`
			PrimaryKey bool   `json:"primaryKey"`
			Default    string `json:"default"`
		} `json:"columns"`
		Comment string `json:"comment"`
		DDL     string `json:"ddl"`
	}
	if err := c.proc.Call(ctx, "tableDefinition", map[string]any{"database": ref.Database, "name": ref.Name}, &out); err != nil {
		return nil, err
	}
	def := &dbx.TableDefinition{Ref: ref, Comment: out.Comment, DDL: out.DDL}
	var pk []dbx.IndexColumn
	for i, col := range out.Columns {
		def.Columns = append(def.Columns, dbx.Column{
			Name: col.Name, Position: i + 1, Type: baseType(col.Type), FullType: col.Type,
			Nullable: col.Nullable, Comment: col.Comment, PrimaryKey: col.PrimaryKey,
			Default: col.Default, HasDefault: col.Default != "",
		})
		if col.PrimaryKey {
			pk = append(pk, dbx.IndexColumn{Name: col.Name})
		}
	}
	if len(pk) > 0 {
		def.Indexes = append(def.Indexes, dbx.Index{Name: "PRIMARY", Primary: true, Unique: true, Columns: pk})
	}
	return def, nil
}

func (c *Conn) ObjectDDL(ctx context.Context, ref dbx.ObjectRef) (string, error) {
	def, err := c.TableDefinition(ctx, ref)
	if err != nil {
		return "", err
	}
	return def.DDL, nil
}

// Query 把一条查询交给插件。真正执行前先过主程序这一道门禁。
func (c *Conn) Query(ctx context.Context, database, query string, opts dbx.ScanOptions, args ...any) (dbx.RowStream, error) {
	if len(args) > 0 {
		return nil, errors.New("插件连接不支持参数化查询")
	}
	sql, err := guard(c.m.Rules, query)
	if err != nil {
		return nil, err
	}
	if database == "" {
		database = c.cfg.Database
	}
	var out struct {
		Columns []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
		Rows [][]any `json:"rows"`
	}
	start := time.Now()
	err = c.proc.Call(ctx, "query", map[string]any{"database": database, "sql": sql, "limit": c.m.Rules.ForceLimit}, &out)
	logx.SQL("plugin."+c.m.ID+".query", database, sql, start, len(out.Rows), err)
	if err != nil {
		return nil, err
	}
	return newSliceStream(out.Columns, out.Rows, opts), nil
}

// Exec 永远失败：只读插件连接没有写路径，这不是配置，是结构。
func (c *Conn) Exec(context.Context, string, string, ...any) (dbx.ExecResult, error) {
	return dbx.ExecResult{}, errors.New("插件连接为只读，不支持写操作")
}

func (c *Conn) Dialect() dbx.Dialect { return c.dialect }

// --- 插件自定义动作 ---

// Node 右键时传给插件的节点定位：scope 是 connection | database | table。
type Node struct {
	Scope    string `json:"scope"`
	Database string `json:"database,omitempty"`
	Name     string `json:"name,omitempty"`
}

// Action 插件声明的一个动作；Fields 非空时主程序先弹表单收集参数。
type Action struct {
	ID     string         `json:"id"`
	Label  string         `json:"label"`
	Danger bool           `json:"danger"`
	Fields []plugin.Field `json:"fields,omitempty"`
}

// ActionResult 动作执行结果。Refresh 为真时主程序刷新该节点所在子树。
type ActionResult struct {
	Message string `json:"message"`
	Level   string `json:"level"` // success | info | warning | error
	Refresh bool   `json:"refresh"`
}

// Actions 问插件某个节点有哪些动作；插件没实现就是没有。
func (c *Conn) Actions(ctx context.Context, node Node) ([]Action, error) {
	var out []Action
	err := c.proc.Call(ctx, "actions", node, &out)
	if errors.Is(err, plugin.ErrMethodNotFound) {
		return nil, nil
	}
	return out, err
}

// RunAction 执行一个动作。
func (c *Conn) RunAction(ctx context.Context, id string, node Node, params map[string]string) (ActionResult, error) {
	var out ActionResult
	err := c.proc.Call(ctx, "action", map[string]any{"id": id, "node": node, "params": params}, &out)
	return out, err
}

// --- 门禁 ---

var (
	leadingWord = regexp.MustCompile(`^\s*([A-Za-z]+)`)
	limitRe     = regexp.MustCompile(`(?i)\blimit\s+(\d+)(?:\s*,\s*(\d+))?\s*;?\s*$`)
)

// guard 按清单规则检查并改写 SQL。
func guard(r plugin.Rules, sql string) (string, error) {
	body := sqlutil.StripComments(sql, sqlutil.MySQLDialect())
	body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), ";"))
	if body == "" {
		return "", errors.New("SQL 为空")
	}
	if r.SingleStatement {
		if stmts := sqlutil.Split(body, sqlutil.MySQLDialect()); len(stmts) > 1 {
			return "", errors.New("插件连接不允许多语句")
		}
	}
	if r.SelectOnly {
		m := leadingWord.FindStringSubmatch(body)
		if m == nil || !strings.EqualFold(m[1], "select") && !strings.EqualFold(m[1], "with") {
			return "", errors.New("插件连接只允许 SELECT 查询")
		}
	}
	lower := strings.ToLower(body)
	for _, p := range r.DenyPatterns {
		if p != "" && strings.Contains(lower, strings.ToLower(p)) {
			return "", fmt.Errorf("SQL 含有被禁止的内容：%s", p)
		}
	}
	if r.ForceLimit > 0 {
		if m := limitRe.FindStringSubmatch(body); m != nil {
			// LIMIT n 取 m[1]；LIMIT offset, n 时行数在 m[2]
			n, _ := strconv.Atoi(m[1])
			if m[2] != "" {
				n, _ = strconv.Atoi(m[2])
			}
			if n > r.ForceLimit {
				body = limitRe.ReplaceAllString(body, "LIMIT "+strconv.Itoa(r.ForceLimit))
			}
		} else {
			body += " LIMIT " + strconv.Itoa(r.ForceLimit)
		}
	}
	return body, nil
}

func baseType(t string) string {
	if i := strings.IndexAny(t, "( "); i > 0 {
		return strings.ToLower(t[:i])
	}
	return strings.ToLower(t)
}

// --- 结果流：插件一次性返回整页，这里包成 RowStream ---

type sliceStream struct {
	cols []dbx.ColumnMeta
	rows [][]any
	i    int
	opts dbx.ScanOptions
}

func newSliceStream(cols []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}, rows [][]any, opts dbx.ScanOptions) *sliceStream {
	metas := make([]dbx.ColumnMeta, 0, len(cols))
	for _, c := range cols {
		metas = append(metas, dbx.ColumnMeta{Name: c.Name, Type: c.Type, Class: classify(c.Type), Nullable: -1})
	}
	return &sliceStream{cols: metas, rows: rows, opts: opts.Normalize()}
}

func (s *sliceStream) Columns() []dbx.ColumnMeta { return s.cols }

func (s *sliceStream) Next() (dbx.Row, error) {
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	raw := s.rows[s.i]
	s.i++
	row := make(dbx.Row, len(s.cols))
	for j := range row {
		if j >= len(raw) || raw[j] == nil {
			row[j] = dbx.Cell{Null: true}
			continue
		}
		var text string
		switch v := raw[j].(type) {
		case float64:
			if v == float64(int64(v)) {
				text = strconv.FormatInt(int64(v), 10)
			} else {
				text = strconv.FormatFloat(v, 'f', -1, 64)
			}
		case string:
			text = v
		case map[string]any, []any:
			b, _ := json.Marshal(v)
			text = string(b)
		default:
			text = fmt.Sprint(v)
		}
		cell := dbx.Cell{Text: text, Size: len(text)}
		if limit := s.opts.PreviewLimit; limit > 0 && len(text) > limit {
			cell.Text, cell.Truncated = text[:limit], true
		}
		row[j] = cell
	}
	return row, nil
}

func (s *sliceStream) Close() error { s.rows = nil; return nil }

func classify(t string) dbx.ValueClass {
	switch baseType(t) {
	case "int", "integer", "bigint", "smallint", "tinyint", "decimal", "numeric", "float", "double", "number", "real":
		return dbx.ClassNumber
	case "datetime", "timestamp", "date", "time":
		return dbx.ClassTime
	case "bool", "boolean", "bit":
		return dbx.ClassBool
	case "json":
		return dbx.ClassJSON
	case "blob", "binary", "varbinary", "bytea":
		return dbx.ClassBinary
	}
	return dbx.ClassString
}

// --- 方言：插件背后是哪种库，标识符引用就跟谁 ---

type pluginDialect struct {
	dbx.Dialect
	engine dbx.Engine
}

func newDialect(m *plugin.Manifest) dbx.Dialect {
	var base dbx.Dialect
	switch m.Dialect {
	case "postgres":
		base = pgdrv.NewDialect(160000)
	case "sqlite":
		base = sqlitedrv.NewDialect()
	default:
		base = mysqldrv.NewDialect(dbx.EngineMySQL, 80000)
	}
	return pluginDialect{Dialect: base, engine: dbx.Engine(m.Engine())}
}

func (d pluginDialect) Engine() dbx.Engine { return d.engine }

// 语句里不带库名前缀：网关这类通道按请求参数里的库名路由，
// SQL 里再写 `逻辑库`.`表` 会被原样发到物理分片上，而分片的库名带年份，于是"表不存在"。
// 内嵌方言在自己的 BuildSelect 里调的是它自己的 QualifyRef，所以要在入口处把库名抹掉。
func (d pluginDialect) QualifyRef(ref dbx.ObjectRef) string { return d.Dialect.QuoteIdent(ref.Name) }

func (d pluginDialect) BuildSelect(ref dbx.ObjectRef, opt dbx.SelectOptions) string {
	ref.Database, ref.Schema = "", ""
	return d.Dialect.BuildSelect(ref, opt)
}

func (d pluginDialect) BuildCount(ref dbx.ObjectRef, where string) string {
	ref.Database, ref.Schema = "", ""
	return d.Dialect.BuildCount(ref, where)
}

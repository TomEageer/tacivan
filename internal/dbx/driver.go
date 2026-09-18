package dbx

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Cell 结果集中的单个值。
//
// 内存布局刻意保持扁平：一个 Cell 的常驻开销是 struct 本身 + Text 的字节数，
// 大字段在进入 Cell 之前就已被截断（见 ScanOptions.PreviewLimit），
// 完整值只存在于溢出文件中。这是不让大结果集吃爆内存的第一道闸。
type Cell struct {
	Null bool `json:"n,omitempty"`
	// Text 展示文本；Binary 为真时是十六进制预览。
	Text   string `json:"v,omitempty"`
	Binary bool   `json:"x,omitempty"`
	// Truncated 为真表示 Text 只是前缀，完整值需按需拉取。
	Truncated bool `json:"t,omitempty"`
	// Size 原始值的字节长度。
	Size int `json:"s,omitempty"`
}

// Row 一行数据。
type Row []Cell

// ApproxBytes 估算该行常驻内存字节数，供内存预算核算。
func (r Row) ApproxBytes() int64 {
	// 每个 Cell 的 struct 本身在 64 位平台约 40 字节（string header 16 + 3 bool + int + 对齐）。
	n := int64(len(r)) * 40
	for i := range r {
		n += int64(len(r[i].Text))
	}
	return n
}

// ScanOptions 控制驱动如何把原始值转成 Cell。
type ScanOptions struct {
	// PreviewLimit 单元格文本的最大字节数，超出部分截断。0 表示用默认值。
	PreviewLimit int
	// BinaryPreviewLimit 二进制列的十六进制预览上限（原始字节数）。
	BinaryPreviewLimit int
}

// DefaultScanOptions 返回默认扫描选项。
func DefaultScanOptions() ScanOptions {
	return ScanOptions{PreviewLimit: 4096, BinaryPreviewLimit: 256}
}

// Normalize 补齐零值字段。
func (o ScanOptions) Normalize() ScanOptions {
	if o.PreviewLimit <= 0 {
		o.PreviewLimit = 4096
	}
	if o.BinaryPreviewLimit <= 0 {
		o.BinaryPreviewLimit = 256
	}
	return o
}

// RowStream 单向行流。
//
// 驱动只负责顺序产出行，不做任何缓存——缓存、回退、溢出统统交给 rs 包。
// 这样驱动实现保持简单，内存策略只有一份、可统一调优。
type RowStream interface {
	Columns() []ColumnMeta
	// Next 返回下一行；流结束时返回 (nil, io.EOF)。
	Next() (Row, error)
	Close() error
}

// Canceller 行流可以在读取过程中被外部打断。
//
// 单独成一个接口而不是并进 RowStream，是因为打断必须能在
// 「另一个 goroutine 正卡在 Next() 上」的时候调用。
// Close 做不到这件事：它要等 Next 返回才能安全释放资源。
type Canceller interface {
	// Cancel 让正在进行和后续的 Next 尽快返回错误。可重复调用。
	Cancel()
}

// SelectOptions 数据网格取数时的排序与过滤条件。
type SelectOptions struct {
	// Columns 为空表示 SELECT *。
	Columns []string
	// Where 原始 WHERE 子句（不含 WHERE 关键字）。
	Where string
	// OrderBy 排序列。
	OrderBy []OrderTerm
	Limit   int
	Offset  int
}

// OrderTerm 一个排序项。
type OrderTerm struct {
	Column string `json:"column"`
	Desc   bool   `json:"desc"`
}

// Conn 所有引擎连接的公共部分。
type Conn interface {
	Engine() Engine
	Capability() Capability
	ServerInfo(ctx context.Context) (ServerInfo, error)
	Ping(ctx context.Context) error
	Close() error
}

// SQLConn 关系型引擎连接。
type SQLConn interface {
	Conn

	Databases(ctx context.Context) ([]DatabaseInfo, error)
	Schemas(ctx context.Context, database string) ([]SchemaInfo, error)
	Objects(ctx context.Context, database, schema string, kinds []ObjectKind) ([]ObjectInfo, error)

	TableDefinition(ctx context.Context, ref ObjectRef) (*TableDefinition, error)
	ObjectDDL(ctx context.Context, ref ObjectRef) (string, error)

	// Query 执行查询并返回流式游标；database 为空表示沿用连接当前库。
	Query(ctx context.Context, database, query string, opts ScanOptions, args ...any) (RowStream, error)
	// Exec 执行非查询语句。
	Exec(ctx context.Context, database, stmt string, args ...any) (ExecResult, error)

	// Dialect 返回方言，所有 SQL 拼装都走它。
	Dialect() Dialect
}

// KVConn 键值引擎连接（Redis）。
type KVConn interface {
	Conn

	Databases(ctx context.Context) ([]DatabaseInfo, error)
	// ScanKeys 以流的形式返回键列表，列为 key/type/ttl/size。
	ScanKeys(ctx context.Context, database, pattern string, opts ScanOptions) (RowStream, error)
	GetKey(ctx context.Context, database, key string) (*KeyValue, error)
	SetKey(ctx context.Context, database string, kv KeyValue) error
	DeleteKeys(ctx context.Context, database string, keys []string) (int64, error)
	RenameKey(ctx context.Context, database, from, to string) error
	ExpireKey(ctx context.Context, database, key string, ttlSeconds int64) error
	// Command 执行任意命令，供内置命令行面板使用。
	Command(ctx context.Context, database string, args []string) (string, error)
	ServerStats(ctx context.Context) (map[string]string, error)
}

// KeyValue Redis 键的完整视图。
type KeyValue struct {
	Key  string `json:"key"`
	Type string `json:"type"` // string | list | set | zset | hash | stream
	TTL  int64  `json:"ttl"`  // 秒，-1 表示永不过期
	Size int64  `json:"size"` // 元素个数或字节数
	// Value 字符串类型的值。
	Value string `json:"value,omitempty"`
	// Entries 集合类型的成员，最多返回一页。
	Entries []KeyEntry `json:"entries,omitempty"`
	// Truncated 为真表示 Entries 只是前一页。
	Truncated   bool   `json:"truncated,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	MemoryUsage int64  `json:"memoryUsage,omitempty"`
}

// KeyEntry 集合类型中的一个成员。
type KeyEntry struct {
	// Field hash 的字段名 / zset 的成员 / list 的下标。
	Field string `json:"field"`
	Value string `json:"value"`
	// Score zset 的分值。
	Score float64 `json:"score,omitempty"`
}

// Statement 一条待执行的参数化语句。
type Statement struct {
	// SQL 带占位符的语句，执行用。
	SQL string `json:"sql"`
	// Args 占位符对应的参数。
	Args []any `json:"-"`
	// Preview 参数已内联的可读形式，仅用于界面展示与复制，不参与执行。
	Preview string `json:"preview"`
}

// Dialect 把各引擎的 SQL 语法差异收敛到一处。
type Dialect interface {
	Engine() Engine
	// QuoteIdent 引用标识符，如 `col` / "col"。
	QuoteIdent(ident string) string
	// QuoteString 引用字符串字面量。
	QuoteString(s string) string
	// QualifyRef 生成对象的完全限定名。
	QualifyRef(ref ObjectRef) string

	BuildSelect(ref ObjectRef, opt SelectOptions) string
	BuildCount(ref ObjectRef, where string) string
	// BuildRowWrite 把一次行变更翻译成参数化语句。
	//
	// 返回参数化 SQL 而不是拼好的字面量：真正执行的永远是带占位符的语句，
	// 字面量形式只用于在「SQL 预览」里给用户看，两者分离才不会出现注入面。
	BuildRowWrite(ref ObjectRef, cols []Column, ch RowChange) (Statement, error)

	// BuildCreateTable/BuildAlterTable 供表设计器使用，返回可预览的语句列表。
	BuildCreateTable(def *TableDefinition) ([]string, error)
	BuildAlterTable(old, new *TableDefinition) ([]string, error)
	BuildDropObject(ref ObjectRef) string
	BuildTruncate(ref ObjectRef) string
	BuildRenameObject(ref ObjectRef, newName string) string

	// BuildExplain 返回执行计划语句。
	BuildExplain(query string, analyze bool) string
	// PlaceholderStyle 返回参数占位符样式，"?" 或 "$N"。
	PlaceholderStyle() string
}

// StatsProvider 是可选能力：能单独提供表统计信息的引擎实现它。
//
// 行数与体积属于统计信息，取它往往比列目录贵得多（MySQL 要采样 InnoDB 统计，
// SQLite 要逐表 COUNT）。把它与「列出有哪些表」分开，对象树才能秒开，
// 统计数字随后再填。
type StatsProvider interface {
	TableStats(ctx context.Context, database string) (map[string]ObjectInfo, error)
}

// Dialer 建立连接。
type Dialer interface {
	Engine() Engine
	// Open 建立一个新连接。
	Open(ctx context.Context, cfg ConnectionConfig) (Conn, error)
}

var (
	registryMu sync.RWMutex
	registry   = map[Engine]Dialer{}
)

// Register 注册一个引擎驱动，由各驱动包的 init 调用。
func Register(d Dialer) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[d.Engine()] = d
}

// Open 按配置建立连接。
func Open(ctx context.Context, cfg ConnectionConfig) (Conn, error) {
	registryMu.RLock()
	d, ok := registry[cfg.Engine]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("不支持的数据库引擎: %s", cfg.Engine)
	}
	return d.Open(ctx, cfg)
}

// RegisteredEngines 返回已注册的引擎列表。
func RegisteredEngines() []Engine {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Engine, 0, len(registry))
	for e := range registry {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Package dbx 定义数据库引擎抽象层。
//
// 设计原则：所有引擎（关系型 + KV）共用一套连接/对象树模型，
// 差异通过 Capability 位暴露给前端，避免 UI 层写 if engine == "mysql" 这类分支。
package dbx

import "time"

// Engine 数据库引擎类型。
type Engine string

const (
	EngineMySQL    Engine = "mysql"
	EngineMariaDB  Engine = "mariadb"
	EnginePostgres Engine = "postgres"
	EngineSQLite   Engine = "sqlite"
	EngineRedis    Engine = "redis"
	// EngineMCP 连的不是数据库，是一个 MCP server：工具和资源在树上展示，手动调用。
	EngineMCP Engine = "mcp"
)

// AllEngines 返回受支持的引擎清单，顺序即新建连接对话框中的展示顺序。
func AllEngines() []Engine {
	return []Engine{EngineMySQL, EngineMariaDB, EnginePostgres, EngineSQLite, EngineRedis, EngineMCP}
}

// DisplayName 引擎在界面上的展示名。
func (e Engine) DisplayName() string {
	switch e {
	case EngineMySQL:
		return "MySQL"
	case EngineMariaDB:
		return "MariaDB"
	case EnginePostgres:
		return "PostgreSQL"
	case EngineSQLite:
		return "SQLite"
	case EngineRedis:
		return "Redis"
	case EngineMCP:
		return "MCP 服务器"
	}
	return string(e)
}

// DefaultPort 引擎默认端口，SQLite 为 0（基于文件）。
func (e Engine) DefaultPort() int {
	switch e {
	case EngineMySQL, EngineMariaDB:
		return 3306
	case EnginePostgres:
		return 5432
	case EngineRedis:
		return 6379
	}
	return 0
}

// Capability 引擎能力位，前端据此决定显示哪些面板与菜单项。
type Capability struct {
	SQL            bool `json:"sql"`            // 支持 SQL 编辑器
	Schemas        bool `json:"schemas"`        // 库下还有 schema 层（PostgreSQL）
	MultiDatabase  bool `json:"multiDatabase"`  // 一个连接可切换多个库
	Views          bool `json:"views"`          //
	Functions      bool `json:"functions"`      //
	Procedures     bool `json:"procedures"`     //
	Triggers       bool `json:"triggers"`       //
	Events         bool `json:"events"`         //
	Sequences      bool `json:"sequences"`      //
	ForeignKeys    bool `json:"foreignKeys"`    //
	Transactions   bool `json:"transactions"`   //
	ExplainPlan    bool `json:"explainPlan"`    //
	KeyValue       bool `json:"keyValue"`       // Redis 式键值浏览器
	ServerVariable bool `json:"serverVariable"` // 支持查看服务器状态/变量
}

// SSHConfig SSH 隧道配置。
type SSHConfig struct {
	Enabled    bool   `json:"enabled"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	AuthMethod string `json:"authMethod"` // password | key | agent
	Password   string `json:"password"`
	KeyFile    string `json:"keyFile"`
	Passphrase string `json:"passphrase"`
}

// TLSConfig TLS/SSL 配置。
type TLSConfig struct {
	Enabled            bool   `json:"enabled"`
	CAFile             string `json:"caFile"`
	CertFile           string `json:"certFile"`
	KeyFile            string `json:"keyFile"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
}

// ConnectionConfig 一条连接的完整配置。
//
// Password 字段只在内存与安全存储（macOS Keychain）之间流转，
// 落盘的 JSON 中该字段恒为空。
type ConnectionConfig struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Engine Engine `json:"engine"`
	// Color 连接颜色标记，对齐主流客户端的连接色条，用于区分生产/测试环境。
	Color string `json:"color"`
	// Group 所属分组名，空表示不分组。分组本身只是个名字，见 store.Group。
	Group string `json:"group,omitempty"`

	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`
	// Database 默认数据库；SQLite 下为数据库文件路径；Redis 下为库序号的字符串。
	Database string            `json:"database"`
	Params   map[string]string `json:"params,omitempty"`

	TLS TLSConfig `json:"tls"`
	SSH SSHConfig `json:"ssh"`

	// ReadOnly 只读连接：拦截一切写语句，用于挂生产库。
	ReadOnly bool `json:"readOnly"`
	// UseDatabaseList 启用自定义数据库列表。
	//
	// 开了之后对象树只列 DatabaseList 里的库，并且**根本不发 SHOW DATABASES**。
	// 这是为超大实例准备的：实测有一台 MySQL 5.7 有 3165 个库，
	// 每次展开连接都要拉一遍三千多行再渲染，而实际要用的不超过十个。
	UseDatabaseList bool `json:"useDatabaseList"`
	// DatabaseList 自定义数据库列表，仅在 UseDatabaseList 为真时生效。
	DatabaseList []DatabaseEntry `json:"databaseList,omitempty"`
	// Compress 启用协议级压缩（目前仅 MySQL/MariaDB）。
	// 慢链路上读宽表时能显著减少传输量，本机或内网直连则没必要开。
	Compress bool `json:"compress"`
	// KeepAlive 心跳间隔（秒），0 表示关闭。
	KeepAlive int `json:"keepAlive"`
	// ConnectTimeout 连接超时（秒）。
	ConnectTimeout int `json:"connectTimeout"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// SortOrder 在连接树中的排序位置。
	SortOrder int `json:"sortOrder"`
}

// DatabaseEntry 自定义数据库列表中的一项。
type DatabaseEntry struct {
	Name string `json:"name"`
	// AutoOpen 连接建立后自动展开该库。
	AutoOpen bool `json:"autoOpen"`
}

// ObjectKind 数据库对象类型，对应对象树中的分组。
type ObjectKind string

const (
	KindTable     ObjectKind = "table"
	KindView      ObjectKind = "view"
	KindFunction  ObjectKind = "function"
	KindProcedure ObjectKind = "procedure"
	KindTrigger   ObjectKind = "trigger"
	KindEvent     ObjectKind = "event"
	KindSequence  ObjectKind = "sequence"
	KindIndex     ObjectKind = "index"
)

// ObjectRef 唯一定位一个数据库对象。
type ObjectRef struct {
	Database string     `json:"database"`
	Schema   string     `json:"schema"`
	Name     string     `json:"name"`
	Kind     ObjectKind `json:"kind"`
}

// DatabaseInfo 一个数据库/catalog。
type DatabaseInfo struct {
	Name      string `json:"name"`
	Charset   string `json:"charset"`
	Collation string `json:"collation"`
	// Current 是否为当前连接的默认库。
	Current bool `json:"current"`
	// System 系统库（information_schema 等），前端默认折叠/置灰。
	System bool `json:"system"`
	// AutoOpen 来自连接的自定义列表，标记连上后自动展开。
	AutoOpen bool `json:"autoOpen"`
	// Status/Detail/Color 由插件之类的来源附加：核验过、出错……树上按它着色。
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
	Color  string `json:"color,omitempty"`
}

// SchemaInfo PostgreSQL 的 schema。
type SchemaInfo struct {
	Name   string `json:"name"`
	Owner  string `json:"owner"`
	System bool   `json:"system"`
}

// ObjectInfo 对象树叶子节点。
type ObjectInfo struct {
	Name   string     `json:"name"`
	Kind   ObjectKind `json:"kind"`
	Schema string     `json:"schema"`
	// Status/Detail/Color 由插件之类的来源附加，树上按它着色。
	Status  string `json:"status,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Color   string `json:"color,omitempty"`
	Comment string `json:"comment"`
	// Rows 估算行数（来自统计信息，非精确值）。
	Rows int64 `json:"rows"`
	// DataSize/IndexSize 字节。
	DataSize  int64  `json:"dataSize"`
	IndexSize int64  `json:"indexSize"`
	Engine    string `json:"engine"`
	Collation string `json:"collation"`
	// UpdatedAt 最后更新时间的文本形式，各引擎口径不同，仅作展示。
	UpdatedAt string `json:"updatedAt"`
}

// Column 表/视图的列定义。
type Column struct {
	Name string `json:"name"`
	// OrigName 表设计器中该列改名前的名字；为空表示新增列。
	OrigName string `json:"origName,omitempty"`
	Position int    `json:"position"`
	// Type 引擎原生类型名（不含长度），如 varchar、int8。
	Type string `json:"type"`
	// FullType 完整类型定义，如 varchar(255) unsigned。
	FullType   string `json:"fullType"`
	Length     int64  `json:"length"`
	Scale      int    `json:"scale"`
	Nullable   bool   `json:"nullable"`
	Default    string `json:"default"`
	HasDefault bool   `json:"hasDefault"`
	// DefaultIsExpr 默认值是表达式（如 CURRENT_TIMESTAMP），生成 DDL 时不加引号。
	DefaultIsExpr bool   `json:"defaultIsExpr"`
	AutoIncrement bool   `json:"autoIncrement"`
	PrimaryKey    bool   `json:"primaryKey"`
	Unsigned      bool   `json:"unsigned"`
	Charset       string `json:"charset"`
	Collation     string `json:"collation"`
	Comment       string `json:"comment"`
	// Generated 生成列表达式，空表示普通列。
	Generated string `json:"generated"`
	// EnumValues 枚举/集合类型的候选值，供数据网格下拉编辑用。
	EnumValues []string `json:"enumValues,omitempty"`
}

// IndexColumn 索引中的一列。
type IndexColumn struct {
	Name   string `json:"name"`
	Order  string `json:"order"` // ASC | DESC
	Length int64  `json:"length"`
}

// Index 索引定义。
type Index struct {
	Name    string        `json:"name"`
	Columns []IndexColumn `json:"columns"`
	Unique  bool          `json:"unique"`
	Primary bool          `json:"primary"`
	Type    string        `json:"type"` // BTREE | HASH | FULLTEXT | GIN ...
	Comment string        `json:"comment"`
}

// ForeignKey 外键定义。
type ForeignKey struct {
	Name              string   `json:"name"`
	Columns           []string `json:"columns"`
	ReferencedSchema  string   `json:"referencedSchema"`
	ReferencedTable   string   `json:"referencedTable"`
	ReferencedColumns []string `json:"referencedColumns"`
	OnUpdate          string   `json:"onUpdate"`
	OnDelete          string   `json:"onDelete"`
}

// TableDefinition 表设计器的完整模型。
type TableDefinition struct {
	Ref         ObjectRef    `json:"ref"`
	Columns     []Column     `json:"columns"`
	Indexes     []Index      `json:"indexes"`
	ForeignKeys []ForeignKey `json:"foreignKeys"`
	Comment     string       `json:"comment"`
	Engine      string       `json:"engine"`
	Charset     string       `json:"charset"`
	Collation   string       `json:"collation"`
	// AutoIncrement 当前自增值。
	AutoIncrement int64 `json:"autoIncrement"`
	// DDL 建表语句原文。
	DDL string `json:"ddl"`
}

// ColumnMeta 结果集列的元信息。
type ColumnMeta struct {
	Name string `json:"name"`
	// Type 引擎侧类型名。
	Type string `json:"type"`
	// Class 归一化后的类型类别，前端据此决定对齐方式与编辑器。
	Class ValueClass `json:"class"`
	// Table/Column 指向的源表与源列；可编辑结果集才有值。
	Table  string `json:"table"`
	Column string `json:"column"`
	// Nullable -1 未知，0 否，1 是。
	Nullable int `json:"nullable"`
}

// ValueClass 归一化的值类别。
type ValueClass string

const (
	ClassString  ValueClass = "string"
	ClassNumber  ValueClass = "number"
	ClassBool    ValueClass = "bool"
	ClassTime    ValueClass = "time"
	ClassBinary  ValueClass = "binary"
	ClassJSON    ValueClass = "json"
	ClassUnknown ValueClass = "unknown"
)

// ServerInfo 连接成功后的服务器概要。
type ServerInfo struct {
	Engine  Engine `json:"engine"`
	Version string `json:"version"`
	// VersionNum 归一化版本号，如 80035 表示 8.0.35。
	VersionNum int    `json:"versionNum"`
	Charset    string `json:"charset"`
	Timezone   string `json:"timezone"`
	// Extra 额外的服务器信息，键值对形式展示。
	Extra map[string]string `json:"extra,omitempty"`
}

// ExecResult 非查询语句的执行结果。
type ExecResult struct {
	RowsAffected int64 `json:"rowsAffected"`
	LastInsertID int64 `json:"lastInsertId"`
	// DurationMS 执行耗时（毫秒）。
	DurationMS int64 `json:"durationMs"`
}

// FieldValue 一个列值。
//
// 用显式的 Null 标志而不是 *string：指针在跨语言绑定里会被翻译成难用的形状，
// 而「空字符串」与「NULL」在数据库里是两回事，必须能区分开。
type FieldValue struct {
	Null  bool   `json:"null,omitempty"`
	Value string `json:"value,omitempty"`
}

// RowChange 数据网格提交的一次行级变更。
type RowChange struct {
	// Op: insert | update | delete
	Op string `json:"op"`
	// Values 目标值，键为列名。insert/update 使用。
	Values map[string]FieldValue `json:"values"`
	// Keys 定位行的条件，键为列名。update/delete 使用。
	Keys map[string]FieldValue `json:"keys"`
}

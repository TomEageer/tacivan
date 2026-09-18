/**
 * 前端使用的数据结构。
 *
 * 这里手写与后端 JSON 对应的接口，而不是直接用 Wails 生成的 class：
 * 生成物是按 Go 类型机械翻译的，map[string]*string 这类会被翻成难用的形状，
 * 且带构造函数。前端只依赖自己的这份契约，出入口在 api.ts 做一次收敛。
 */

export type Engine = 'mysql' | 'mariadb' | 'postgres' | 'sqlite' | 'redis' | 'mcp'

export type ObjectKind =
  | 'tool'
  | 'resource'
  | 'table'
  | 'view'
  | 'function'
  | 'procedure'
  | 'trigger'
  | 'event'
  | 'sequence'
  | 'index'

export type ValueClass = 'string' | 'number' | 'bool' | 'time' | 'binary' | 'json' | 'unknown'

export interface Capability {
  sql: boolean
  schemas: boolean
  multiDatabase: boolean
  views: boolean
  functions: boolean
  procedures: boolean
  triggers: boolean
  events: boolean
  sequences: boolean
  foreignKeys: boolean
  transactions: boolean
  explainPlan: boolean
  keyValue: boolean
  serverVariable: boolean
}

export interface SSHConfig {
  enabled: boolean
  host: string
  port: number
  user: string
  authMethod: 'password' | 'key' | 'agent'
  password: string
  keyFile: string
  passphrase: string
}

export interface TLSConfig {
  enabled: boolean
  caFile: string
  certFile: string
  keyFile: string
  insecureSkipVerify: boolean
}

/** 全库搜索的条件。 */
export interface SearchRequest {
  connId: string
  databases: string[]
  keyword: string
  mode: 'object' | 'data'
  searchColumns: boolean
  caseSensitive: boolean
  maxRowsPerTable: number
  maxTables: number
  progressToken?: string
}

export interface SearchHit {
  database: string
  schema: string
  table: string
  column: string
  /** table | view | column | row */
  kind: string
  value: string
  comment: string
}

export interface SearchResult {
  hits: SearchHit[]
  scannedDatabases: number
  scannedTables: number
  /** 用户中途停了；已找到的仍然有效。 */
  stopped: boolean
  /** 因触达上限而没扫完。 */
  truncated: boolean
  durationMs: number
  note?: string
}

/** 查询编辑器的未保存草稿，用于崩溃/退出后恢复。 */
export interface Draft {
  id: string
  title: string
  connId: string
  connName: string
  database: string
  sql: string
  updatedAt?: string
}

/** 连接的自定义数据库列表中的一项。 */
export interface DatabaseEntry {
  name: string
  autoOpen: boolean
}

export interface ConnectionConfig {
  id: string
  name: string
  engine: Engine
  color: string
  /** 所属分组名，空表示不分组。 */
  group?: string
  host: string
  port: number
  user: string
  password?: string
  database: string
  params?: Record<string, string>
  tls: TLSConfig
  ssh: SSHConfig
  readOnly: boolean
  keepAlive: number
  connectTimeout: number
  /** 启用后对象树只列 databaseList 里的库，连 SHOW DATABASES 都不发。 */
  useDatabaseList: boolean
  databaseList: DatabaseEntry[]
  /** 协议级压缩，目前仅 MySQL/MariaDB。慢链路读宽表时有用。 */
  compress: boolean
  createdAt?: string
  updatedAt?: string
  sortOrder: number
}

export interface ServerInfo {
  engine: Engine
  version: string
  versionNum: number
  charset: string
  timezone: string
  extra?: Record<string, string>
}

export interface ConnectionState {
  config: ConnectionConfig
  open: boolean
  serverInfo: ServerInfo
  capability: Capability
  openedAt: string
  currentDb: string
}

export interface DatabaseInfo {
  name: string
  charset: string
  collation: string
  current: boolean
  system: boolean
  /** 来自连接的自定义列表，标记连上后自动展开。 */
  autoOpen?: boolean
  /** 插件之类的来源附加的状态：核验过、出错……树上按它着色。 */
  status?: string
  detail?: string
  color?: string
}

export interface SchemaInfo {
  name: string
  owner: string
  system: boolean
}

export interface ObjectRef {
  database: string
  schema: string
  name: string
  kind: ObjectKind
}

export interface ObjectNode {
  name: string
  kind: ObjectKind
  schema: string
  status?: string
  detail?: string
  color?: string
  comment: string
  rows: number
  dataSize: number
  indexSize: number
  engine: string
  collation: string
  updatedAt: string
  sizeText: string
  rowsText: string
}

export interface Column {
  name: string
  origName?: string
  position: number
  type: string
  fullType: string
  length: number
  scale: number
  nullable: boolean
  default: string
  hasDefault: boolean
  defaultIsExpr: boolean
  autoIncrement: boolean
  primaryKey: boolean
  unsigned: boolean
  charset: string
  collation: string
  comment: string
  generated: string
  enumValues?: string[]
}

export interface IndexColumn {
  name: string
  order: string
  length: number
}

export interface TableIndex {
  name: string
  columns: IndexColumn[]
  unique: boolean
  primary: boolean
  type: string
  comment: string
}

export interface ForeignKey {
  name: string
  columns: string[]
  referencedSchema: string
  referencedTable: string
  referencedColumns: string[]
  onUpdate: string
  onDelete: string
}

export interface TableDefinition {
  ref: ObjectRef
  columns: Column[]
  indexes: TableIndex[]
  foreignKeys: ForeignKey[]
  comment: string
  engine: string
  charset: string
  collation: string
  autoIncrement: number
  ddl: string
}

export interface ColumnMeta {
  name: string
  type: string
  class: ValueClass
  table: string
  column: string
  /** -1 未知，0 否，1 是 */
  nullable: number
}

/**
 * 单元格。字段名是压缩过的（后端为减小传输体积）：
 * n=null, v=value, x=binary, t=truncated, s=size。
 */
export interface Cell {
  n?: boolean
  v?: string
  x?: boolean
  t?: boolean
  s?: number
}

export type Row = Cell[]

export interface Page {
  columns: ColumnMeta[]
  rows: Row[]
  offset: number
  loaded: number
  /** 未知时为 -1 */
  total: number
  complete: boolean
  limitReached: boolean
  /** 用户主动中断了取数；已返回的行仍然有效。 */
  stopped?: boolean
  error?: string
}

export interface OrderTerm {
  column: string
  desc: boolean
}

/** 一个列值；null 与空字符串在数据库里是两回事，用显式标志区分。 */
export interface FieldValue {
  null?: boolean
  value?: string
}

export interface RowChange {
  op: 'insert' | 'update' | 'delete'
  values: Record<string, FieldValue>
  keys: Record<string, FieldValue>
}

export type FilterOp =
  | 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte'
  | 'contains' | 'notContains' | 'startsWith' | 'endsWith'
  | 'in' | 'notIn' | 'isNull' | 'isNotNull' | 'between' | 'isEmpty'

export interface FilterCondition {
  column: string
  op: FilterOp
  values: string[]
  enabled: boolean
}

export interface FilterGroup {
  conditions: FilterCondition[]
  /** and | or */
  conjunction: string
}

export interface FilterOpInfo {
  op: FilterOp
  label: string
  /** 需要几个值：0 无需输入，1 单值，2 区间，-1 任意多个 */
  arity: number
  classes?: string[]
}

export interface RowSQLOptions {
  /** insert | update | delete | select */
  kind: string
  rowIndexes: number[]
  columns: string[]
}

export interface ChangeSet {
  resultId: string
  changes: RowChange[]
}

export interface ChangePreview {
  statements: string[]
  sql: string
  warnings?: string[]
}

export interface ApplyResult {
  applied: number
  rowsAffected: number
  statements: string[]
  durationMs: number
}

export interface OpenTableOptions {
  where: string
  filter: FilterGroup
  orderBy: OrderTerm[]
  columns: string[]
  firstPageRows: number
  /** 对象树上已知的估算行数，避免后端重复查询统计信息。 */
  estimatedRows: number
  /** 取数上限；0 用设置默认值，-1 不限。 */
  limit: number
  /** 带上它就能在打开过程中收到分步进度事件。 */
  progressToken?: string
}

export interface TableDataResult {
  resultId: string
  page: Page
  sql: string
  columns: Column[]
  keyColumns: string[]
  editable: boolean
  editableReason?: string
  ref: ObjectRef
  durationMs: number
  estimatedRows: number
  limit: number
}

export interface ExecuteOptions {
  executionId: string
  stopOnError: boolean
  inTransaction: boolean
  firstPageRows: number
  timeoutSeconds: number
}

export interface StatementResult {
  index: number
  sql: string
  kind: string
  line: number
  resultId?: string
  page?: Page
  editable: boolean
  editableReason?: string
  keyColumns?: string[]
  tableColumns?: Column[]
  ref: ObjectRef
  rowsAffected: number
  lastInsertId: number
  durationMs: number
  error?: string
  skipped: boolean
}

export interface ExecuteResponse {
  statements: StatementResult[]
  totalDurationMs: number
  canceled: boolean
}

export interface CursorStats {
  id: string
  loaded: number
  memoryBytes: number
  peakMemoryBytes: number
  spillBytes: number
  residentChunks: number
  complete: boolean
  idleSeconds: number
}

export interface GlobalStats {
  openResultSets: number
  memoryBytes: number
  memoryLimit: number
  spillBytes: number
  totalRows: number
  trimCount: number
  cursors: CursorStats[]
}

export interface ResourceStats {
  processHeapMb: number
  processSysMb: number
  goroutines: number
  resultSets: GlobalStats
  openConnections: number
}

export interface AppInfo {
  name: string
  version: string
  goVersion: string
  platform: string
  configDir: string
  spillDir: string
  keyringOk: boolean
  startedAt: string
  logPath: string
}

export interface Settings {
  theme: 'light' | 'dark' | 'system'
  /** 界面语言；system 表示跟随系统。 */
  language: 'system' | 'zh-CN' | 'en-US'
  gridPageSize: number
  rowLimit: number
  memoryLimitMb: number
  cellPreviewLimit: number
  maxResultRows: number
  autoCommit: boolean
  confirmOnDelete: boolean
  fontSize: number
  editorFont: string
  showSystemObjects: boolean
  historyLimit: number
  diagnosticLog: boolean
  slowQueryMs: number
}

export interface HistoryEntry {
  id: string
  connectionId: string
  connection: string
  database: string
  sql: string
  at: string
  durationMs: number
  rowsAffected: number
  success: boolean
  error?: string
}

export interface SavedQuery {
  id: string
  name: string
  connectionId: string
  database: string
  sql: string
  createdAt: string
  updatedAt: string
}

/** 一条收藏；name 为空表示收藏整个库。 */
export interface Favorite {
  connectionId: string
  database: string
  schema: string
  name: string
  kind: string
  addedAt?: string
}

/** 插件声明的连接配置项。 */
export interface PluginField {
  key: string
  label: string
  type: 'text' | 'password' | 'number' | 'bool' | 'select' | 'textarea' | 'connection'
  required: boolean
  placeholder?: string
  default?: string
  hint?: string
  options?: string[]
  secret: boolean
}

/** MCP 工具与资源。 */
export interface McpTool {
  name: string
  title?: string
  description?: string
  inputSchema?: unknown
}
export interface McpResource {
  uri: string
  name: string
  title?: string
  description?: string
  mimeType?: string
}
export interface McpCallResult {
  text: string
  isError: boolean
  resultId?: string
  rows: number
  durationMs: number
}

/** 右键时传给插件的节点定位。 */
export interface PluginNode {
  scope: 'connection' | 'database' | 'table'
  database?: string
  name?: string
}
/** 插件声明的一个右键动作；fields 非空时先弹表单。 */
export interface PluginAction {
  id: string
  label: string
  danger?: boolean
  fields?: PluginField[]
}
export interface PluginActionResult {
  message: string
  level: 'success' | 'info' | 'warning' | 'error' | ''
  refresh: boolean
}

/** 已加载的插件。 */
export interface PluginInfo {
  id: string
  name: string
  version: string
  description: string
  engine: string
  readOnly: boolean
  dir: string
}

/** 连接分组。 */
export interface Group {
  name: string
  sortOrder: number
}

export interface EngineOption {
  engine: Engine
  displayName: string
  defaultPort: number
  fileBased: boolean
  /** 插件提供的引擎：表单按 fields 渲染，不走主机/端口。 */
  plugin?: boolean
  readOnly?: boolean
  description?: string
  fields?: PluginField[]
}

export interface DDLPreview {
  statements: string[]
  sql: string
}

export interface KeyEntry {
  field: string
  value: string
  score?: number
}

export interface KeyValue {
  key: string
  type: string
  ttl: number
  size: number
  value?: string
  entries?: KeyEntry[]
  truncated?: boolean
  encoding?: string
  memoryUsage?: number
}

export interface RedisScanResult {
  resultId: string
  page: Page
  pattern: string
}

export type ExportFormat = 'csv' | 'tsv' | 'json' | 'sql' | 'markdown'

export interface ExportOptions {
  format: ExportFormat
  path: string
  includeHeader: boolean
  delimiter: string
  encoding: 'utf8' | 'utf8bom'
  nullText: string
  maxRows: number
  tableName: string
  batchSize: number
}

export interface ExportResult {
  path: string
  rows: number
  bytes: number
  durationMs: number
  truncated: boolean
}

export interface SqlStatement {
  text: string
  offset: number
  end: number
  line: number
}

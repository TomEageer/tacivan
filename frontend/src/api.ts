/**
 * 后端 API 的类型化封装。
 *
 * Wails 生成的绑定是按 Go 类型机械翻译的，这里统一断言成 types.ts 里手写的契约，
 * 让整个前端只面对一份形状清晰的接口。运行时行为不变，只是换了类型标注。
 */
import type {
  Draft,
  Group,
  PluginInfo,
  McpTool,
  McpResource,
  McpCallResult,
  PluginAction,
  PluginActionResult,
  PluginNode,
  SearchRequest,
  SearchResult,
  ApplyResult,
  ChangePreview,
  ChangeSet,
  ConnectionConfig,
  ConnectionState,
  DatabaseInfo,
  DDLPreview,
  EngineOption,
  ExecuteOptions,
  ExecuteResponse,
  ExportOptions,
  Favorite,
  FilterGroup,
  FilterOpInfo,
  ExportResult,
  HistoryEntry,
  KeyValue,
  ObjectNode,
  ObjectRef,
  OpenTableOptions,
  Page,
  RedisScanResult,
  RowSQLOptions,
  ResourceStats,
  Row,
  SavedQuery,
  SchemaInfo,
  ServerInfo,
  Settings,
  SqlStatement,
  StatementResult,
  CursorStats,
  TableDataResult,
  TableDefinition,
  AppInfo,
} from './types'
import { tr } from './i18n'

/**
 * 后端调用代理。
 *
 * 刻意不 import Wails 生成的 TS 绑定：它按 Go 类型机械翻译，遇到 map 值为结构体
 * 这类形状会生成不合法的声明。运行时入口 window.go.app.App 是稳定的，
 * 方法签名由本文件显式声明，类型安全不受生成器缺陷影响。
 * 方法名写错会在调用时立即得到明确报错，而不是静默返回 undefined。
 */
const b: Record<string, (...args: any[]) => Promise<any>> = new Proxy(
  {},
  {
    get:
      (_target, name: string) =>
      (...args: any[]) => {
        const api = (window as any).go?.app?.App
        const fn = api?.[name]
        if (typeof fn !== 'function') {
          return Promise.reject(
            new Error(tr('后端方法不可用：{name}（若刚改过后端，请重新生成绑定并重启）', { name })),
          )
        }
        return fn(...args)
      },
  },
) as Record<string, (...args: any[]) => Promise<any>>

// --- 应用 ---
export const GetAppInfo = (): Promise<AppInfo> => b.GetAppInfo()
export const GetSettings = (): Promise<Settings> => b.GetSettings()
export const SaveSettings = (s: Settings): Promise<Settings> => b.SaveSettings(s)
export const GetResourceStats = (): Promise<ResourceStats> => b.GetResourceStats()
export const ReleaseMemory = (): Promise<ResourceStats> => b.ReleaseMemory()
export const ListFavorites = (): Promise<Favorite[]> => b.ListFavorites()
export const ToggleFavorite = (f: Favorite): Promise<boolean> => b.ToggleFavorite(f)
export const ListEngines = (): Promise<EngineOption[]> => b.ListEngines()
export const RevealLogFile = (): Promise<void> => b.RevealLogFile()
export const ReadLogTail = (maxBytes: number): Promise<string> => b.ReadLogTail(maxBytes)

// --- 连接 ---
export const ListConnections = (): Promise<ConnectionState[]> => b.ListConnections()
export const SaveConnection = (c: ConnectionConfig): Promise<ConnectionConfig> => b.SaveConnection(c)
export const DeleteConnection = (id: string): Promise<void> => b.DeleteConnection(id)
export const DuplicateConnection = (id: string): Promise<ConnectionConfig> => b.DuplicateConnection(id)
export const ReorderConnections = (ids: string[]): Promise<void> => b.ReorderConnections(ids)
export const TestConnection = (c: ConnectionConfig): Promise<ServerInfo> => b.TestConnection(c)
export const OpenConnection = (id: string): Promise<ConnectionState> => b.OpenConnection(id)
export const CloseConnection = (id: string): Promise<void> => b.CloseConnection(id)
export const SetCurrentDatabase = (connId: string, db: string): Promise<void> =>
  b.SetCurrentDatabase(connId, db)
export const GetServerInfo = (connId: string): Promise<ServerInfo> => b.GetServerInfo(connId)
export const PingConnection = (connId: string): Promise<void> => b.PingConnection(connId)

// --- 对象树 ---
export const ListDatabases = (connId: string): Promise<DatabaseInfo[]> => b.ListDatabases(connId)
/** 在一个连接的若干个库里搜对象名或数据。 */
export const Search = (req: SearchRequest): Promise<SearchResult> => b.Search(req)

/** 保存查询编辑器草稿；内容为空等同于删除。 */
export const SaveDraft = (d: Draft): Promise<void> => b.SaveDraft(d)
/** 丢弃草稿，标签页被正常关闭时调用。 */
export const DeleteDraft = (id: string): Promise<void> => b.DeleteDraft(id)
/** 列出上次留下的草稿。 */
export const ListDrafts = (): Promise<Draft[]> => b.ListDrafts()

/** 系统偏好里「双击标题栏」的动作：Maximize | Minimize | Fill | None。 */
export const TitleBarDoubleClickAction = (): Promise<string> => b.TitleBarDoubleClickAction()

/** MCP：工具、资源、调用、读取。 */
export const McpTools = (connId: string): Promise<McpTool[]> => b.McpTools(connId)
export const McpResources = (connId: string): Promise<McpResource[] | null> => b.McpResources(connId)
export const McpCallTool = (connId: string, name: string, args: Record<string, unknown>): Promise<McpCallResult> =>
  b.McpCallTool(connId, name, args)
export const McpReadResource = (connId: string, uri: string): Promise<string> => b.McpReadResource(connId, uri)

/** 插件在某个节点上提供的动作；不是插件连接则为空。 */
export const PluginNodeActions = (connId: string, node: PluginNode): Promise<PluginAction[] | null> =>
  b.PluginNodeActions(connId, node)
export const RunPluginAction = (
  connId: string,
  actionId: string,
  node: PluginNode,
  params: Record<string, string>,
): Promise<PluginActionResult> => b.RunPluginAction(connId, actionId, node, params)

/** 插件管理。 */
export const ListPlugins = (): Promise<PluginInfo[]> => b.ListPlugins()
export const ImportPluginDialog = (): Promise<PluginInfo | null> => b.ImportPluginDialog()
export const RemovePlugin = (id: string): Promise<void> => b.RemovePlugin(id)
export const RevealPlugin = (id: string): Promise<void> => b.RevealPlugin(id)

/** 连接分组。 */
export const ListGroups = (): Promise<Group[]> => b.ListGroups()
export const SaveGroup = (name: string): Promise<Group[]> => b.SaveGroup(name)
export const RenameGroup = (oldName: string, newName: string): Promise<void> => b.RenameGroup(oldName, newName)
export const DeleteGroup = (name: string): Promise<void> => b.DeleteGroup(name)
export const ReorderGroups = (names: string[]): Promise<void> => b.ReorderGroups(names)
export const MoveConnection = (id: string, group: string): Promise<void> => b.MoveConnection(id, group)

/** 中断一个还在进行的长操作；token 为发起时生成的进度标识。 */
export const CancelOperation = (token: string): Promise<void> => b.CancelOperation(token)
/** 中断某个结果集正在进行的取数，已读到的行仍然可翻看。 */
export const StopFetch = (resultId: string): Promise<void> => b.StopFetch(resultId)

/** 无视自定义列表，向服务端实拉全部库名；只给连接配置里挑库用。 */
export const ListAllDatabases = (connId: string): Promise<DatabaseInfo[]> =>
  b.ListAllDatabases(connId)
export const ListSchemas = (connId: string, db: string): Promise<SchemaInfo[]> =>
  b.ListSchemas(connId, db)
export const ListObjects = (
  connId: string,
  db: string,
  schema: string,
  kinds: string[],
): Promise<ObjectNode[]> => b.ListObjects(connId, db, schema, kinds)
export const GetTableDefinition = (connId: string, ref: ObjectRef): Promise<TableDefinition> =>
  b.GetTableDefinition(connId, ref)
export const GetObjectDDL = (connId: string, ref: ObjectRef): Promise<string> =>
  b.GetObjectDDL(connId, ref)
export const ListFilterOps = (): Promise<FilterOpInfo[]> => b.ListFilterOps()
export const BuildFilterSQL = (connId: string, group: FilterGroup): Promise<string> =>
  b.BuildFilterSQL(connId, group)
export const GetTableStats = (connId: string, db: string): Promise<Record<string, ObjectNode>> =>
  b.GetTableStats(connId, db)
export const ServerVariables = (connId: string): Promise<Record<string, string>> =>
  b.ServerVariables(connId)

// --- 结构变更 ---
export const PreviewCreateTable = (connId: string, def: TableDefinition): Promise<DDLPreview> =>
  b.PreviewCreateTable(connId, def)
export const PreviewAlterTable = (
  connId: string,
  oldDef: TableDefinition,
  newDef: TableDefinition,
): Promise<DDLPreview> => b.PreviewAlterTable(connId, oldDef, newDef)
export const CreateTable = (connId: string, def: TableDefinition): Promise<void> =>
  b.CreateTable(connId, def)
export const AlterTable = (
  connId: string,
  oldDef: TableDefinition,
  newDef: TableDefinition,
): Promise<void> => b.AlterTable(connId, oldDef, newDef)
export const DropObject = (connId: string, ref: ObjectRef): Promise<void> => b.DropObject(connId, ref)
export const TruncateTable = (connId: string, ref: ObjectRef): Promise<void> =>
  b.TruncateTable(connId, ref)
export const RenameObject = (connId: string, ref: ObjectRef, name: string): Promise<void> =>
  b.RenameObject(connId, ref, name)
export const CreateDatabase = (
  connId: string,
  name: string,
  charset: string,
  collation: string,
): Promise<void> => b.CreateDatabase(connId, name, charset, collation)
export const DropDatabase = (connId: string, name: string): Promise<void> =>
  b.DropDatabase(connId, name)
export const CreateSchema = (connId: string, db: string, name: string): Promise<void> =>
  b.CreateSchema(connId, db, name)
export const DropSchema = (
  connId: string,
  db: string,
  name: string,
  cascade: boolean,
): Promise<void> => b.DropSchema(connId, db, name, cascade)

// --- 查询 ---
export const ExecuteSQL = (
  connId: string,
  db: string,
  script: string,
  opts: ExecuteOptions,
): Promise<ExecuteResponse> => b.ExecuteSQL(connId, db, script, opts)
export const CancelExecution = (execId: string): Promise<void> => b.CancelExecution(execId)
export const FetchRows = (resultId: string, offset: number, limit: number): Promise<Page> =>
  b.FetchRows(resultId, offset, limit)
export const CountResultRows = (resultId: string): Promise<number> => b.CountResultRows(resultId)
export const GetResultStats = (resultId: string): Promise<CursorStats> => b.GetResultStats(resultId)
export const CloseResult = (resultId: string): Promise<void> => b.CloseResult(resultId)
export const GetCellValue = (
  resultId: string,
  rowIndex: number,
  column: string,
): Promise<string> => b.GetCellValue(resultId, rowIndex, column)
export const ExplainSQL = (
  connId: string,
  db: string,
  query: string,
  analyze: boolean,
): Promise<StatementResult> => b.ExplainSQL(connId, db, query, analyze)
export const FormatSQL = (script: string, engine: string): Promise<string> =>
  b.FormatSQL(script, engine)
export const SplitStatements = (script: string, engine: string): Promise<SqlStatement[]> =>
  b.SplitStatements(script, engine)
export const GetStatementAt = (
  script: string,
  offset: number,
  engine: string,
): Promise<SqlStatement> => b.GetStatementAt(script, offset, engine)

// --- 表数据 ---
export const OpenTableData = (
  connId: string,
  ref: ObjectRef,
  opts: OpenTableOptions,
): Promise<TableDataResult> => b.OpenTableData(connId, ref, opts)
export const CountTableRows = (connId: string, ref: ObjectRef, where: string): Promise<number> =>
  b.CountTableRows(connId, ref, where)
export const PreviewChanges = (cs: ChangeSet): Promise<ChangePreview> => b.PreviewChanges(cs)
export const ApplyChanges = (cs: ChangeSet): Promise<ApplyResult> => b.ApplyChanges(cs)
export const GenerateRowSQL = (resultId: string, opts: RowSQLOptions): Promise<string> =>
  b.GenerateRowSQL(resultId, opts)
export const RefreshRow = (resultId: string, rowIndex: number): Promise<Row> =>
  b.RefreshRow(resultId, rowIndex)
export const GenerateSelectSQL = (
  connId: string,
  ref: ObjectRef,
  limit: number,
): Promise<string> => b.GenerateSelectSQL(connId, ref, limit)
export const GenerateInsertSQL = (connId: string, ref: ObjectRef): Promise<string> =>
  b.GenerateInsertSQL(connId, ref)
export const GenerateUpdateSQL = (connId: string, ref: ObjectRef): Promise<string> =>
  b.GenerateUpdateSQL(connId, ref)

// --- Redis ---
export const RedisScanKeys = (
  connId: string,
  db: string,
  pattern: string,
  firstPageRows: number,
): Promise<RedisScanResult> => b.RedisScanKeys(connId, db, pattern, firstPageRows)
export const RedisGetKey = (connId: string, db: string, key: string): Promise<KeyValue> =>
  b.RedisGetKey(connId, db, key)
export const RedisSetKey = (connId: string, db: string, kv: KeyValue): Promise<void> =>
  b.RedisSetKey(connId, db, kv)
export const RedisDeleteKeys = (connId: string, db: string, keys: string[]): Promise<number> =>
  b.RedisDeleteKeys(connId, db, keys)
export const RedisRenameKey = (
  connId: string,
  db: string,
  from: string,
  to: string,
): Promise<void> => b.RedisRenameKey(connId, db, from, to)
export const RedisExpireKey = (
  connId: string,
  db: string,
  key: string,
  ttl: number,
): Promise<void> => b.RedisExpireKey(connId, db, key, ttl)
export const RedisCommand = (connId: string, db: string, line: string): Promise<string> =>
  b.RedisCommand(connId, db, line)

// --- 历史与保存的查询 ---
export const GetHistory = (limit: number): Promise<HistoryEntry[]> => b.GetHistory(limit)
export const ClearHistory = (): Promise<void> => b.ClearHistory()
export const GetSavedQueries = (): Promise<SavedQuery[]> => b.GetSavedQueries()
export const SaveQuery = (q: SavedQuery): Promise<SavedQuery> => b.SaveQuery(q)
export const DeleteSavedQuery = (id: string): Promise<void> => b.DeleteSavedQuery(id)

// --- 文件与导出 ---
export const ExportResultSet = (resultId: string, opts: ExportOptions): Promise<ExportResult> =>
  b.ExportResultSet(resultId, opts)
export const OpenDatabaseFileDialog = (): Promise<string> => b.OpenDatabaseFileDialog()
export const CreateDatabaseFileDialog = (): Promise<string> => b.CreateDatabaseFileDialog()
export const SaveFileDialogFor = (format: string, defaultName: string): Promise<string> =>
  b.SaveFileDialogFor(format, defaultName)
export const OpenSQLFileDialog = (): Promise<string> => b.OpenSQLFileDialog()
export const SaveSQLFileDialog = (defaultName: string): Promise<string> =>
  b.SaveSQLFileDialog(defaultName)
export const ReadTextFile = (path: string): Promise<string> => b.ReadTextFile(path)
export const WriteTextFile = (path: string, content: string): Promise<void> =>
  b.WriteTextFile(path, content)

export namespace app {
	
	export class AppInfo {
	    name: string;
	    version: string;
	    goVersion: string;
	    platform: string;
	    configDir: string;
	    spillDir: string;
	    keyringOk: boolean;
	    startedAt: string;
	    logPath: string;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.goVersion = source["goVersion"];
	        this.platform = source["platform"];
	        this.configDir = source["configDir"];
	        this.spillDir = source["spillDir"];
	        this.keyringOk = source["keyringOk"];
	        this.startedAt = source["startedAt"];
	        this.logPath = source["logPath"];
	    }
	}
	export class ApplyResult {
	    applied: number;
	    rowsAffected: number;
	    statements: string[];
	    durationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new ApplyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applied = source["applied"];
	        this.rowsAffected = source["rowsAffected"];
	        this.statements = source["statements"];
	        this.durationMs = source["durationMs"];
	    }
	}
	export class ChangePreview {
	    statements: string[];
	    sql: string;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ChangePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.statements = source["statements"];
	        this.sql = source["sql"];
	        this.warnings = source["warnings"];
	    }
	}
	export class ChangeSet {
	    resultId: string;
	    changes: dbx.RowChange[];
	
	    static createFrom(source: any = {}) {
	        return new ChangeSet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.resultId = source["resultId"];
	        this.changes = this.convertValues(source["changes"], dbx.RowChange);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConnectionState {
	    config: dbx.ConnectionConfig;
	    open: boolean;
	    serverInfo: dbx.ServerInfo;
	    capability: dbx.Capability;
	    openedAt: string;
	    currentDb: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], dbx.ConnectionConfig);
	        this.open = source["open"];
	        this.serverInfo = this.convertValues(source["serverInfo"], dbx.ServerInfo);
	        this.capability = this.convertValues(source["capability"], dbx.Capability);
	        this.openedAt = source["openedAt"];
	        this.currentDb = source["currentDb"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DDLPreview {
	    statements: string[];
	    sql: string;
	
	    static createFrom(source: any = {}) {
	        return new DDLPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.statements = source["statements"];
	        this.sql = source["sql"];
	    }
	}
	export class EngineOption {
	    engine: string;
	    displayName: string;
	    defaultPort: number;
	    fileBased: boolean;
	    plugin: boolean;
	    readOnly: boolean;
	    description?: string;
	    fields?: plugin.Field[];
	
	    static createFrom(source: any = {}) {
	        return new EngineOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine = source["engine"];
	        this.displayName = source["displayName"];
	        this.defaultPort = source["defaultPort"];
	        this.fileBased = source["fileBased"];
	        this.plugin = source["plugin"];
	        this.readOnly = source["readOnly"];
	        this.description = source["description"];
	        this.fields = this.convertValues(source["fields"], plugin.Field);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExecuteOptions {
	    executionId: string;
	    stopOnError: boolean;
	    inTransaction: boolean;
	    firstPageRows: number;
	    timeoutSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new ExecuteOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.executionId = source["executionId"];
	        this.stopOnError = source["stopOnError"];
	        this.inTransaction = source["inTransaction"];
	        this.firstPageRows = source["firstPageRows"];
	        this.timeoutSeconds = source["timeoutSeconds"];
	    }
	}
	export class StatementResult {
	    index: number;
	    sql: string;
	    kind: string;
	    line: number;
	    resultId?: string;
	    page?: rs.Page;
	    editable: boolean;
	    editableReason?: string;
	    keyColumns?: string[];
	    tableColumns?: dbx.Column[];
	    ref: dbx.ObjectRef;
	    rowsAffected: number;
	    lastInsertId: number;
	    durationMs: number;
	    error?: string;
	    skipped: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StatementResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.sql = source["sql"];
	        this.kind = source["kind"];
	        this.line = source["line"];
	        this.resultId = source["resultId"];
	        this.page = this.convertValues(source["page"], rs.Page);
	        this.editable = source["editable"];
	        this.editableReason = source["editableReason"];
	        this.keyColumns = source["keyColumns"];
	        this.tableColumns = this.convertValues(source["tableColumns"], dbx.Column);
	        this.ref = this.convertValues(source["ref"], dbx.ObjectRef);
	        this.rowsAffected = source["rowsAffected"];
	        this.lastInsertId = source["lastInsertId"];
	        this.durationMs = source["durationMs"];
	        this.error = source["error"];
	        this.skipped = source["skipped"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExecuteResponse {
	    statements: StatementResult[];
	    totalDurationMs: number;
	    canceled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExecuteResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.statements = this.convertValues(source["statements"], StatementResult);
	        this.totalDurationMs = source["totalDurationMs"];
	        this.canceled = source["canceled"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExportOptions {
	    format: string;
	    path: string;
	    includeHeader: boolean;
	    delimiter: string;
	    encoding: string;
	    nullText: string;
	    maxRows: number;
	    tableName: string;
	    batchSize: number;
	
	    static createFrom(source: any = {}) {
	        return new ExportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.path = source["path"];
	        this.includeHeader = source["includeHeader"];
	        this.delimiter = source["delimiter"];
	        this.encoding = source["encoding"];
	        this.nullText = source["nullText"];
	        this.maxRows = source["maxRows"];
	        this.tableName = source["tableName"];
	        this.batchSize = source["batchSize"];
	    }
	}
	export class ExportResult {
	    path: string;
	    rows: number;
	    bytes: number;
	    durationMs: number;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.rows = source["rows"];
	        this.bytes = source["bytes"];
	        this.durationMs = source["durationMs"];
	        this.truncated = source["truncated"];
	    }
	}
	export class FilterCondition {
	    column: string;
	    op: string;
	    values: string[];
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FilterCondition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column = source["column"];
	        this.op = source["op"];
	        this.values = source["values"];
	        this.enabled = source["enabled"];
	    }
	}
	export class FilterGroup {
	    conditions: FilterCondition[];
	    conjunction: string;
	
	    static createFrom(source: any = {}) {
	        return new FilterGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.conditions = this.convertValues(source["conditions"], FilterCondition);
	        this.conjunction = source["conjunction"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FilterOpInfo {
	    op: string;
	    label: string;
	    arity: number;
	    classes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new FilterOpInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.op = source["op"];
	        this.label = source["label"];
	        this.arity = source["arity"];
	        this.classes = source["classes"];
	    }
	}
	export class McpCallResult {
	    text: string;
	    isError: boolean;
	    resultId?: string;
	    rows: number;
	    durationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new McpCallResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.isError = source["isError"];
	        this.resultId = source["resultId"];
	        this.rows = source["rows"];
	        this.durationMs = source["durationMs"];
	    }
	}
	export class ObjectNode {
	    name: string;
	    kind: string;
	    schema: string;
	    status?: string;
	    detail?: string;
	    color?: string;
	    comment: string;
	    rows: number;
	    dataSize: number;
	    indexSize: number;
	    engine: string;
	    collation: string;
	    updatedAt: string;
	    sizeText: string;
	    rowsText: string;
	
	    static createFrom(source: any = {}) {
	        return new ObjectNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.schema = source["schema"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	        this.color = source["color"];
	        this.comment = source["comment"];
	        this.rows = source["rows"];
	        this.dataSize = source["dataSize"];
	        this.indexSize = source["indexSize"];
	        this.engine = source["engine"];
	        this.collation = source["collation"];
	        this.updatedAt = source["updatedAt"];
	        this.sizeText = source["sizeText"];
	        this.rowsText = source["rowsText"];
	    }
	}
	export class OpenTableOptions {
	    where: string;
	    filter: FilterGroup;
	    orderBy: dbx.OrderTerm[];
	    columns: string[];
	    firstPageRows: number;
	    estimatedRows: number;
	    limit: number;
	    progressToken: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenTableOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.where = source["where"];
	        this.filter = this.convertValues(source["filter"], FilterGroup);
	        this.orderBy = this.convertValues(source["orderBy"], dbx.OrderTerm);
	        this.columns = source["columns"];
	        this.firstPageRows = source["firstPageRows"];
	        this.estimatedRows = source["estimatedRows"];
	        this.limit = source["limit"];
	        this.progressToken = source["progressToken"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PluginInfo {
	    id: string;
	    name: string;
	    version: string;
	    description: string;
	    engine: string;
	    readOnly: boolean;
	    dir: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.description = source["description"];
	        this.engine = source["engine"];
	        this.readOnly = source["readOnly"];
	        this.dir = source["dir"];
	    }
	}
	export class RedisScanResult {
	    resultId: string;
	    page?: rs.Page;
	    pattern: string;
	
	    static createFrom(source: any = {}) {
	        return new RedisScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.resultId = source["resultId"];
	        this.page = this.convertValues(source["page"], rs.Page);
	        this.pattern = source["pattern"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ResourceStats {
	    processHeapMb: number;
	    processSysMb: number;
	    goroutines: number;
	    resultSets: rs.GlobalStats;
	    openConnections: number;
	
	    static createFrom(source: any = {}) {
	        return new ResourceStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.processHeapMb = source["processHeapMb"];
	        this.processSysMb = source["processSysMb"];
	        this.goroutines = source["goroutines"];
	        this.resultSets = this.convertValues(source["resultSets"], rs.GlobalStats);
	        this.openConnections = source["openConnections"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RowSQLOptions {
	    kind: string;
	    rowIndexes: number[];
	    columns: string[];
	
	    static createFrom(source: any = {}) {
	        return new RowSQLOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.rowIndexes = source["rowIndexes"];
	        this.columns = source["columns"];
	    }
	}
	export class SearchHit {
	    database: string;
	    schema: string;
	    table: string;
	    column: string;
	    kind: string;
	    value: string;
	    comment: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchHit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.database = source["database"];
	        this.schema = source["schema"];
	        this.table = source["table"];
	        this.column = source["column"];
	        this.kind = source["kind"];
	        this.value = source["value"];
	        this.comment = source["comment"];
	    }
	}
	export class SearchRequest {
	    connId: string;
	    databases: string[];
	    keyword: string;
	    mode: string;
	    searchColumns: boolean;
	    caseSensitive: boolean;
	    maxRowsPerTable: number;
	    maxTables: number;
	    progressToken: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connId = source["connId"];
	        this.databases = source["databases"];
	        this.keyword = source["keyword"];
	        this.mode = source["mode"];
	        this.searchColumns = source["searchColumns"];
	        this.caseSensitive = source["caseSensitive"];
	        this.maxRowsPerTable = source["maxRowsPerTable"];
	        this.maxTables = source["maxTables"];
	        this.progressToken = source["progressToken"];
	    }
	}
	export class SearchResult {
	    hits: SearchHit[];
	    scannedDatabases: number;
	    scannedTables: number;
	    stopped: boolean;
	    truncated: boolean;
	    durationMs: number;
	    note?: string;
	
	    static createFrom(source: any = {}) {
	        return new SearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hits = this.convertValues(source["hits"], SearchHit);
	        this.scannedDatabases = source["scannedDatabases"];
	        this.scannedTables = source["scannedTables"];
	        this.stopped = source["stopped"];
	        this.truncated = source["truncated"];
	        this.durationMs = source["durationMs"];
	        this.note = source["note"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TableDataResult {
	    resultId: string;
	    page?: rs.Page;
	    sql: string;
	    columns: dbx.Column[];
	    keyColumns: string[];
	    editable: boolean;
	    editableReason?: string;
	    ref: dbx.ObjectRef;
	    durationMs: number;
	    estimatedRows: number;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new TableDataResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.resultId = source["resultId"];
	        this.page = this.convertValues(source["page"], rs.Page);
	        this.sql = source["sql"];
	        this.columns = this.convertValues(source["columns"], dbx.Column);
	        this.keyColumns = source["keyColumns"];
	        this.editable = source["editable"];
	        this.editableReason = source["editableReason"];
	        this.ref = this.convertValues(source["ref"], dbx.ObjectRef);
	        this.durationMs = source["durationMs"];
	        this.estimatedRows = source["estimatedRows"];
	        this.limit = source["limit"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace dbx {
	
	export class Capability {
	    sql: boolean;
	    schemas: boolean;
	    multiDatabase: boolean;
	    views: boolean;
	    functions: boolean;
	    procedures: boolean;
	    triggers: boolean;
	    events: boolean;
	    sequences: boolean;
	    foreignKeys: boolean;
	    transactions: boolean;
	    explainPlan: boolean;
	    keyValue: boolean;
	    serverVariable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Capability(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sql = source["sql"];
	        this.schemas = source["schemas"];
	        this.multiDatabase = source["multiDatabase"];
	        this.views = source["views"];
	        this.functions = source["functions"];
	        this.procedures = source["procedures"];
	        this.triggers = source["triggers"];
	        this.events = source["events"];
	        this.sequences = source["sequences"];
	        this.foreignKeys = source["foreignKeys"];
	        this.transactions = source["transactions"];
	        this.explainPlan = source["explainPlan"];
	        this.keyValue = source["keyValue"];
	        this.serverVariable = source["serverVariable"];
	    }
	}
	export class Cell {
	    n?: boolean;
	    v?: string;
	    x?: boolean;
	    t?: boolean;
	    s?: number;
	
	    static createFrom(source: any = {}) {
	        return new Cell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.n = source["n"];
	        this.v = source["v"];
	        this.x = source["x"];
	        this.t = source["t"];
	        this.s = source["s"];
	    }
	}
	export class Column {
	    name: string;
	    origName?: string;
	    position: number;
	    type: string;
	    fullType: string;
	    length: number;
	    scale: number;
	    nullable: boolean;
	    default: string;
	    hasDefault: boolean;
	    defaultIsExpr: boolean;
	    autoIncrement: boolean;
	    primaryKey: boolean;
	    unsigned: boolean;
	    charset: string;
	    collation: string;
	    comment: string;
	    generated: string;
	    enumValues?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.origName = source["origName"];
	        this.position = source["position"];
	        this.type = source["type"];
	        this.fullType = source["fullType"];
	        this.length = source["length"];
	        this.scale = source["scale"];
	        this.nullable = source["nullable"];
	        this.default = source["default"];
	        this.hasDefault = source["hasDefault"];
	        this.defaultIsExpr = source["defaultIsExpr"];
	        this.autoIncrement = source["autoIncrement"];
	        this.primaryKey = source["primaryKey"];
	        this.unsigned = source["unsigned"];
	        this.charset = source["charset"];
	        this.collation = source["collation"];
	        this.comment = source["comment"];
	        this.generated = source["generated"];
	        this.enumValues = source["enumValues"];
	    }
	}
	export class ColumnMeta {
	    name: string;
	    type: string;
	    class: string;
	    table: string;
	    column: string;
	    nullable: number;
	
	    static createFrom(source: any = {}) {
	        return new ColumnMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.class = source["class"];
	        this.table = source["table"];
	        this.column = source["column"];
	        this.nullable = source["nullable"];
	    }
	}
	export class DatabaseEntry {
	    name: string;
	    autoOpen: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DatabaseEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.autoOpen = source["autoOpen"];
	    }
	}
	export class SSHConfig {
	    enabled: boolean;
	    host: string;
	    port: number;
	    user: string;
	    authMethod: string;
	    password: string;
	    keyFile: string;
	    passphrase: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.authMethod = source["authMethod"];
	        this.password = source["password"];
	        this.keyFile = source["keyFile"];
	        this.passphrase = source["passphrase"];
	    }
	}
	export class TLSConfig {
	    enabled: boolean;
	    caFile: string;
	    certFile: string;
	    keyFile: string;
	    insecureSkipVerify: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TLSConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.caFile = source["caFile"];
	        this.certFile = source["certFile"];
	        this.keyFile = source["keyFile"];
	        this.insecureSkipVerify = source["insecureSkipVerify"];
	    }
	}
	export class ConnectionConfig {
	    id: string;
	    name: string;
	    engine: string;
	    color: string;
	    group?: string;
	    host: string;
	    port: number;
	    user: string;
	    password?: string;
	    database: string;
	    params?: Record<string, string>;
	    tls: TLSConfig;
	    ssh: SSHConfig;
	    readOnly: boolean;
	    useDatabaseList: boolean;
	    databaseList?: DatabaseEntry[];
	    compress: boolean;
	    keepAlive: number;
	    connectTimeout: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.engine = source["engine"];
	        this.color = source["color"];
	        this.group = source["group"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.password = source["password"];
	        this.database = source["database"];
	        this.params = source["params"];
	        this.tls = this.convertValues(source["tls"], TLSConfig);
	        this.ssh = this.convertValues(source["ssh"], SSHConfig);
	        this.readOnly = source["readOnly"];
	        this.useDatabaseList = source["useDatabaseList"];
	        this.databaseList = this.convertValues(source["databaseList"], DatabaseEntry);
	        this.compress = source["compress"];
	        this.keepAlive = source["keepAlive"];
	        this.connectTimeout = source["connectTimeout"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.sortOrder = source["sortOrder"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class DatabaseInfo {
	    name: string;
	    charset: string;
	    collation: string;
	    current: boolean;
	    system: boolean;
	    autoOpen: boolean;
	    status?: string;
	    detail?: string;
	    color?: string;
	
	    static createFrom(source: any = {}) {
	        return new DatabaseInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.charset = source["charset"];
	        this.collation = source["collation"];
	        this.current = source["current"];
	        this.system = source["system"];
	        this.autoOpen = source["autoOpen"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	        this.color = source["color"];
	    }
	}
	export class FieldValue {
	    null?: boolean;
	    value?: string;
	
	    static createFrom(source: any = {}) {
	        return new FieldValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.null = source["null"];
	        this.value = source["value"];
	    }
	}
	export class ForeignKey {
	    name: string;
	    columns: string[];
	    referencedSchema: string;
	    referencedTable: string;
	    referencedColumns: string[];
	    onUpdate: string;
	    onDelete: string;
	
	    static createFrom(source: any = {}) {
	        return new ForeignKey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.columns = source["columns"];
	        this.referencedSchema = source["referencedSchema"];
	        this.referencedTable = source["referencedTable"];
	        this.referencedColumns = source["referencedColumns"];
	        this.onUpdate = source["onUpdate"];
	        this.onDelete = source["onDelete"];
	    }
	}
	export class IndexColumn {
	    name: string;
	    order: string;
	    length: number;
	
	    static createFrom(source: any = {}) {
	        return new IndexColumn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.order = source["order"];
	        this.length = source["length"];
	    }
	}
	export class Index {
	    name: string;
	    columns: IndexColumn[];
	    unique: boolean;
	    primary: boolean;
	    type: string;
	    comment: string;
	
	    static createFrom(source: any = {}) {
	        return new Index(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.columns = this.convertValues(source["columns"], IndexColumn);
	        this.unique = source["unique"];
	        this.primary = source["primary"];
	        this.type = source["type"];
	        this.comment = source["comment"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class KeyEntry {
	    field: string;
	    value: string;
	    score?: number;
	
	    static createFrom(source: any = {}) {
	        return new KeyEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field = source["field"];
	        this.value = source["value"];
	        this.score = source["score"];
	    }
	}
	export class KeyValue {
	    key: string;
	    type: string;
	    ttl: number;
	    size: number;
	    value?: string;
	    entries?: KeyEntry[];
	    truncated?: boolean;
	    encoding?: string;
	    memoryUsage?: number;
	
	    static createFrom(source: any = {}) {
	        return new KeyValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	        this.ttl = source["ttl"];
	        this.size = source["size"];
	        this.value = source["value"];
	        this.entries = this.convertValues(source["entries"], KeyEntry);
	        this.truncated = source["truncated"];
	        this.encoding = source["encoding"];
	        this.memoryUsage = source["memoryUsage"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ObjectRef {
	    database: string;
	    schema: string;
	    name: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new ObjectRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.database = source["database"];
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	    }
	}
	export class OrderTerm {
	    column: string;
	    desc: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OrderTerm(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column = source["column"];
	        this.desc = source["desc"];
	    }
	}
	export class RowChange {
	    op: string;
	    values: Record<string, FieldValue>;
	    keys: Record<string, FieldValue>;
	
	    static createFrom(source: any = {}) {
	        return new RowChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.op = source["op"];
	        this.values = this.convertValues(source["values"], FieldValue, true);
	        this.keys = this.convertValues(source["keys"], FieldValue, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SchemaInfo {
	    name: string;
	    owner: string;
	    system: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SchemaInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.owner = source["owner"];
	        this.system = source["system"];
	    }
	}
	export class ServerInfo {
	    engine: string;
	    version: string;
	    versionNum: number;
	    charset: string;
	    timezone: string;
	    extra?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.engine = source["engine"];
	        this.version = source["version"];
	        this.versionNum = source["versionNum"];
	        this.charset = source["charset"];
	        this.timezone = source["timezone"];
	        this.extra = source["extra"];
	    }
	}
	
	export class TableDefinition {
	    ref: ObjectRef;
	    columns: Column[];
	    indexes: Index[];
	    foreignKeys: ForeignKey[];
	    comment: string;
	    engine: string;
	    charset: string;
	    collation: string;
	    autoIncrement: number;
	    ddl: string;
	
	    static createFrom(source: any = {}) {
	        return new TableDefinition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ref = this.convertValues(source["ref"], ObjectRef);
	        this.columns = this.convertValues(source["columns"], Column);
	        this.indexes = this.convertValues(source["indexes"], Index);
	        this.foreignKeys = this.convertValues(source["foreignKeys"], ForeignKey);
	        this.comment = source["comment"];
	        this.engine = source["engine"];
	        this.charset = source["charset"];
	        this.collation = source["collation"];
	        this.autoIncrement = source["autoIncrement"];
	        this.ddl = source["ddl"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace mcp {
	
	export class Resource {
	    uri: string;
	    name: string;
	    title?: string;
	    description?: string;
	    mimeType?: string;
	
	    static createFrom(source: any = {}) {
	        return new Resource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uri = source["uri"];
	        this.name = source["name"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.mimeType = source["mimeType"];
	    }
	}
	export class Tool {
	    name: string;
	    title?: string;
	    description?: string;
	    inputSchema?: number[];
	
	    static createFrom(source: any = {}) {
	        return new Tool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.inputSchema = source["inputSchema"];
	    }
	}

}

export namespace plugin {
	
	export class Field {
	    key: string;
	    label: string;
	    type: string;
	    required: boolean;
	    placeholder?: string;
	    default?: string;
	    hint?: string;
	    options?: string[];
	    secret: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Field(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.placeholder = source["placeholder"];
	        this.default = source["default"];
	        this.hint = source["hint"];
	        this.options = source["options"];
	        this.secret = source["secret"];
	    }
	}

}

export namespace plugindrv {
	
	export class Action {
	    id: string;
	    label: string;
	    danger: boolean;
	    fields?: plugin.Field[];
	
	    static createFrom(source: any = {}) {
	        return new Action(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.danger = source["danger"];
	        this.fields = this.convertValues(source["fields"], plugin.Field);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ActionResult {
	    message: string;
	    level: string;
	    refresh: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	        this.level = source["level"];
	        this.refresh = source["refresh"];
	    }
	}
	export class Node {
	    scope: string;
	    database?: string;
	    name?: string;
	
	    static createFrom(source: any = {}) {
	        return new Node(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.database = source["database"];
	        this.name = source["name"];
	    }
	}

}

export namespace rs {
	
	export class Stats {
	    id: string;
	    loaded: number;
	    memoryBytes: number;
	    peakMemoryBytes: number;
	    spillBytes: number;
	    residentChunks: number;
	    complete: boolean;
	    idleSeconds: number;
	    readMs: number;
	    readBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.loaded = source["loaded"];
	        this.memoryBytes = source["memoryBytes"];
	        this.peakMemoryBytes = source["peakMemoryBytes"];
	        this.spillBytes = source["spillBytes"];
	        this.residentChunks = source["residentChunks"];
	        this.complete = source["complete"];
	        this.idleSeconds = source["idleSeconds"];
	        this.readMs = source["readMs"];
	        this.readBytes = source["readBytes"];
	    }
	}
	export class GlobalStats {
	    openResultSets: number;
	    memoryBytes: number;
	    memoryLimit: number;
	    spillBytes: number;
	    totalRows: number;
	    trimCount: number;
	    cursors: Stats[];
	
	    static createFrom(source: any = {}) {
	        return new GlobalStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openResultSets = source["openResultSets"];
	        this.memoryBytes = source["memoryBytes"];
	        this.memoryLimit = source["memoryLimit"];
	        this.spillBytes = source["spillBytes"];
	        this.totalRows = source["totalRows"];
	        this.trimCount = source["trimCount"];
	        this.cursors = this.convertValues(source["cursors"], Stats);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Page {
	    columns: dbx.ColumnMeta[];
	    rows: dbx.Cell[][];
	    offset: number;
	    loaded: number;
	    total: number;
	    complete: boolean;
	    limitReached: boolean;
	    stopped: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new Page(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = this.convertValues(source["columns"], dbx.ColumnMeta);
	        this.rows = this.convertValues(source["rows"], dbx.Cell);
	        this.offset = source["offset"];
	        this.loaded = source["loaded"];
	        this.total = source["total"];
	        this.complete = source["complete"];
	        this.limitReached = source["limitReached"];
	        this.stopped = source["stopped"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sqlutil {
	
	export class Statement {
	    text: string;
	    offset: number;
	    end: number;
	    line: number;
	
	    static createFrom(source: any = {}) {
	        return new Statement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.offset = source["offset"];
	        this.end = source["end"];
	        this.line = source["line"];
	    }
	}

}

export namespace store {
	
	export class Draft {
	    id: string;
	    title: string;
	    connId: string;
	    connName: string;
	    database: string;
	    sql: string;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Draft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.connId = source["connId"];
	        this.connName = source["connName"];
	        this.database = source["database"];
	        this.sql = source["sql"];
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Favorite {
	    connectionId: string;
	    database: string;
	    schema: string;
	    name: string;
	    kind: string;
	    // Go type: time
	    addedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Favorite(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connectionId = source["connectionId"];
	        this.database = source["database"];
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.addedAt = this.convertValues(source["addedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Group {
	    name: string;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.sortOrder = source["sortOrder"];
	    }
	}
	export class HistoryEntry {
	    id: string;
	    connectionId: string;
	    connection: string;
	    database: string;
	    sql: string;
	    // Go type: time
	    at: any;
	    durationMs: number;
	    rowsAffected: number;
	    success: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.connectionId = source["connectionId"];
	        this.connection = source["connection"];
	        this.database = source["database"];
	        this.sql = source["sql"];
	        this.at = this.convertValues(source["at"], null);
	        this.durationMs = source["durationMs"];
	        this.rowsAffected = source["rowsAffected"];
	        this.success = source["success"];
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SavedQuery {
	    id: string;
	    name: string;
	    connectionId: string;
	    database: string;
	    sql: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SavedQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.connectionId = source["connectionId"];
	        this.database = source["database"];
	        this.sql = source["sql"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Settings {
	    theme: string;
	    language: string;
	    gridPageSize: number;
	    rowLimit: number;
	    memoryLimitMb: number;
	    cellPreviewLimit: number;
	    maxResultRows: number;
	    autoCommit: boolean;
	    confirmOnDelete: boolean;
	    fontSize: number;
	    editorFont: string;
	    showSystemObjects: boolean;
	    historyLimit: number;
	    diagnosticLog: boolean;
	    slowQueryMs: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.language = source["language"];
	        this.gridPageSize = source["gridPageSize"];
	        this.rowLimit = source["rowLimit"];
	        this.memoryLimitMb = source["memoryLimitMb"];
	        this.cellPreviewLimit = source["cellPreviewLimit"];
	        this.maxResultRows = source["maxResultRows"];
	        this.autoCommit = source["autoCommit"];
	        this.confirmOnDelete = source["confirmOnDelete"];
	        this.fontSize = source["fontSize"];
	        this.editorFont = source["editorFont"];
	        this.showSystemObjects = source["showSystemObjects"];
	        this.historyLimit = source["historyLimit"];
	        this.diagnosticLog = source["diagnosticLog"];
	        this.slowQueryMs = source["slowQueryMs"];
	    }
	}

}


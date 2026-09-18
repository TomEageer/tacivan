package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
	"tacivan/internal/rs"
	"tacivan/internal/sqlutil"
	"tacivan/internal/store"
)

// editContext 一个可编辑结果集的写回上下文。
type editContext struct {
	ref dbx.ObjectRef
	// columns 目标表的完整列定义。
	columns []dbx.Column
	// keyColumns 用来唯一定位一行的列。
	keyColumns []string
	connID     string
	database   string
}

// ExecuteOptions 执行 SQL 时的选项。
type ExecuteOptions struct {
	// ExecutionID 前端生成的执行标识，用于中途取消。
	ExecutionID string `json:"executionId"`
	// StopOnError 遇到错误是否停止后续语句。
	StopOnError bool `json:"stopOnError"`
	// InTransaction 把整批语句包进一个事务。
	InTransaction bool `json:"inTransaction"`
	// FirstPageRows 首屏返回多少行，0 表示用设置里的值。
	FirstPageRows int `json:"firstPageRows"`
	// TimeoutSeconds 单条语句超时，0 表示用默认值。
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// StatementResult 一条语句的执行结果。
type StatementResult struct {
	Index int    `json:"index"`
	SQL   string `json:"sql"`
	Kind  string `json:"kind"`
	// Line 语句在编辑器中的起始行。
	Line int `json:"line"`

	// ResultID 查询类语句的结果集标识，用于后续分页取数。
	ResultID string `json:"resultId,omitempty"`
	// Page 首屏数据。
	Page *rs.Page `json:"page,omitempty"`
	// Editable 结果集是否可就地编辑。
	Editable bool `json:"editable"`
	// EditableReason 不可编辑的原因。
	EditableReason string `json:"editableReason,omitempty"`
	// KeyColumns 定位行所用的列。
	KeyColumns []string `json:"keyColumns,omitempty"`
	// TableColumns 来源表的列定义；有了它，查询结果的列头也能显示类型与注释。
	TableColumns []dbx.Column `json:"tableColumns,omitempty"`
	// Ref 结果集对应的表。
	Ref dbx.ObjectRef `json:"ref"`

	RowsAffected int64 `json:"rowsAffected"`
	LastInsertID int64 `json:"lastInsertId"`

	DurationMS int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
	// Skipped 因前序语句出错而未执行。
	Skipped bool `json:"skipped"`
}

// ExecuteResponse 一次执行请求的整体结果。
type ExecuteResponse struct {
	Statements []StatementResult `json:"statements"`
	// TotalDurationMS 整批语句的总耗时。
	TotalDurationMS int64 `json:"totalDurationMs"`
	// Canceled 用户中途取消。
	Canceled bool `json:"canceled"`
}

// running 正在执行的语句批次，用于取消。
type runningExec struct {
	cancel context.CancelFunc
}

var (
	runningMu sync.Mutex
	running   = map[string]*runningExec{}
)

// ExecuteSQL 执行一段 SQL 文本，支持多语句。
func (a *App) ExecuteSQL(connID, database, script string, opts ExecuteOptions) (*ExecuteResponse, error) {
	s, sc, err := a.sqlSession(connID)
	if err != nil {
		return nil, err
	}
	settings := a.store.Settings()
	pageRows := opts.FirstPageRows
	if pageRows <= 0 {
		pageRows = settings.GridPageSize
	}
	timeout := opts.TimeoutSeconds
	if timeout <= 0 {
		timeout = 300
	}

	stmts := sqlutil.Split(script, splitDialect(s.cfg.Engine))
	if len(stmts) == 0 {
		return nil, fmt.Errorf("没有可执行的语句")
	}

	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	defer cancel()
	if opts.ExecutionID != "" {
		runningMu.Lock()
		running[opts.ExecutionID] = &runningExec{cancel: cancel}
		runningMu.Unlock()
		defer func() {
			runningMu.Lock()
			delete(running, opts.ExecutionID)
			runningMu.Unlock()
		}()
	}

	resp := &ExecuteResponse{Statements: make([]StatementResult, 0, len(stmts))}
	batchStart := time.Now()

	if opts.InTransaction && s.conn.Capability().Transactions {
		if _, err := sc.Exec(ctx, database, "BEGIN"); err != nil {
			return nil, fmt.Errorf("开启事务失败: %w", err)
		}
	}
	failed := false

	for i, st := range stmts {
		res := StatementResult{
			Index: i,
			SQL:   st.Text,
			Kind:  string(sqlutil.Classify(st.Text)),
			Line:  st.Line,
		}

		if failed && opts.StopOnError {
			res.Skipped = true
			resp.Statements = append(resp.Statements, res)
			continue
		}
		if ctx.Err() != nil {
			res.Skipped = true
			resp.Statements = append(resp.Statements, res)
			resp.Canceled = true
			continue
		}
		// 只读连接是挂生产库的主要用法，拦截必须发生在发出请求之前。
		if s.cfg.ReadOnly && !sqlutil.IsReadOnly(st.Text) {
			res.Error = "当前连接为只读，已拦截该写操作"
			failed = true
			resp.Statements = append(resp.Statements, res)
			continue
		}

		stmtCtx, stmtCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		start := time.Now()

		if sqlutil.Classify(st.Text) == sqlutil.KindQuery {
			// 结果集游标要活到用户关闭标签页，不能绑在语句级的超时 context 上。
			stream, qerr := longLivedQuery(stmtCtx, func(qctx context.Context) (dbx.RowStream, error) {
				return sc.Query(qctx, database, st.Text, a.scanOptions())
			})
			stmtCancel()
			if qerr != nil {
				res.Error = qerr.Error()
				res.DurationMS = time.Since(start).Milliseconds()
				failed = true
			} else {
				meta := rs.CursorMeta{
					Query:        st.Text,
					ConnectionID: connID,
					Database:     database,
					DurationMS:   time.Since(start).Milliseconds(),
				}
				cursor := a.rs.Open(stream, meta)
				page, ferr := cursor.Fetch(0, int64(pageRows))
				res.DurationMS = time.Since(start).Milliseconds()
				if ferr != nil {
					res.Error = ferr.Error()
					_ = a.rs.Close(cursor.ID())
					failed = true
				} else {
					res.ResultID = cursor.ID()
					res.Page = page
					a.attachEditContext(connID, database, cursor, &res)
				}
			}
		} else {
			execRes, eerr := sc.Exec(stmtCtx, database, st.Text)
			res.DurationMS = time.Since(start).Milliseconds()
			stmtCancel()
			if eerr != nil {
				res.Error = eerr.Error()
				failed = true
			} else {
				res.RowsAffected = execRes.RowsAffected
				res.LastInsertID = execRes.LastInsertID
			}
		}

		a.store.AddHistory(store.HistoryEntry{
			ConnectionID: connID,
			Connection:   s.cfg.Name,
			Database:     database,
			SQL:          st.Text,
			At:           start,
			DurationMS:   res.DurationMS,
			RowsAffected: res.RowsAffected,
			Success:      res.Error == "",
			Error:        res.Error,
		})
		resp.Statements = append(resp.Statements, res)
	}

	if opts.InTransaction && s.conn.Capability().Transactions {
		stmt := "COMMIT"
		if failed {
			stmt = "ROLLBACK"
		}
		if _, err := sc.Exec(ctx, database, stmt); err != nil {
			return resp, fmt.Errorf("%s 失败: %w", stmt, err)
		}
	}

	resp.TotalDurationMS = time.Since(batchStart).Milliseconds()
	if ctx.Err() != nil {
		resp.Canceled = true
	}
	return resp, nil
}

// CancelExecution 取消一次正在进行的执行。
func (a *App) CancelExecution(executionID string) error {
	runningMu.Lock()
	r, ok := running[executionID]
	runningMu.Unlock()
	if !ok {
		return fmt.Errorf("没有正在执行的语句")
	}
	r.cancel()
	return nil
}

func splitDialect(e dbx.Engine) sqlutil.Dialect {
	switch e {
	case dbx.EnginePostgres:
		return sqlutil.PostgresDialect()
	case dbx.EngineSQLite:
		return sqlutil.SQLiteDialect()
	}
	return sqlutil.MySQLDialect()
}

// attachEditContext 判断结果集能否就地编辑，能则记录写回所需的上下文。
//
// 判定链路：语句是单表 SELECT → 表存在 → 表有主键或唯一非空索引 →
// 结果集里带齐了那些键列。任何一环不成立都退化为只读，并给出具体原因，
// 而不是让用户改完点保存才报错。
func (a *App) attachEditContext(connID, database string, cursor *rs.Cursor, res *StatementResult) {
	s, sc, err := a.sqlSession(connID)
	if err != nil {
		return
	}
	table, _, ok := sqlutil.TableSource(res.SQL, splitDialect(s.cfg.Engine))
	if !ok {
		res.EditableReason = "仅单表查询的结果可以直接编辑"
		return
	}
	qualifier, name := sqlutil.SplitQualifiedName(table)
	ref := dbx.ObjectRef{Name: name, Kind: dbx.KindTable}
	switch s.cfg.Engine {
	case dbx.EnginePostgres:
		ref.Database = database
		ref.Schema = qualifier
		if ref.Schema == "" {
			ref.Schema = "public"
		}
	default:
		ref.Database = qualifier
		if ref.Database == "" {
			ref.Database = database
		}
	}

	ctx, cancel := a.requestCtx(20)
	defer cancel()
	def, err := sc.TableDefinition(ctx, ref)
	if err != nil {
		res.EditableReason = "无法读取表结构：" + err.Error()
		return
	}

	keys := pickKeyColumns(def)
	if len(keys) == 0 {
		res.EditableReason = "该表没有主键或唯一索引，无法安全定位行"
		return
	}
	// 结果集里必须带齐全部键列，否则定位不到行。
	present := map[string]bool{}
	for _, c := range cursor.Columns() {
		present[strings.ToLower(c.Name)] = true
	}
	for _, k := range keys {
		if !present[strings.ToLower(k)] {
			res.EditableReason = fmt.Sprintf("结果集中缺少键列 %s，请把它加入查询", k)
			return
		}
	}

	res.Editable = true
	res.KeyColumns = keys
	res.TableColumns = def.Columns
	res.Ref = ref
	a.setEditContext(cursor.ID(), &editContext{
		ref: ref, columns: def.Columns, keyColumns: keys,
		connID: connID, database: database,
	})
}

// pickKeyColumns 选出定位一行用的列：优先主键，其次唯一且全部非空的索引。
func pickKeyColumns(def *dbx.TableDefinition) []string {
	nullable := map[string]bool{}
	for _, c := range def.Columns {
		nullable[c.Name] = c.Nullable
	}
	for _, idx := range def.Indexes {
		if !idx.Primary {
			continue
		}
		var out []string
		for _, c := range idx.Columns {
			out = append(out, c.Name)
		}
		if len(out) > 0 {
			return out
		}
	}
	var pkCols []string
	for _, c := range def.Columns {
		if c.PrimaryKey {
			pkCols = append(pkCols, c.Name)
		}
	}
	if len(pkCols) > 0 {
		return pkCols
	}
	for _, idx := range def.Indexes {
		if !idx.Unique {
			continue
		}
		var out []string
		usable := true
		for _, c := range idx.Columns {
			// 唯一索引里含可空列时，NULL 不参与唯一性约束，不能用来定位。
			if nullable[c.Name] {
				usable = false
				break
			}
			out = append(out, c.Name)
		}
		if usable && len(out) > 0 {
			return out
		}
	}
	return nil
}

var (
	editMu       sync.RWMutex
	editContexts = map[string]*editContext{}
)

func (a *App) setEditContext(resultID string, ec *editContext) {
	editMu.Lock()
	editContexts[resultID] = ec
	editMu.Unlock()
}

func (a *App) getEditContext(resultID string) (*editContext, bool) {
	editMu.RLock()
	ec, ok := editContexts[resultID]
	editMu.RUnlock()
	return ec, ok
}

// FetchRows 取结果集的一段行，供虚拟滚动使用。
func (a *App) FetchRows(resultID string, offset, limit int64) (*rs.Page, error) {
	start := time.Now()
	page, err := a.rs.Fetch(resultID, offset, limit)
	if err != nil {
		logx.Op("FetchRows", start, "rs", resultID, "offset", offset, "err", err.Error())
		return nil, err
	}
	logx.Op("FetchRows", start, "rs", resultID, "offset", offset, "rows", len(page.Rows), "loaded", page.Loaded)
	return page, nil
}

// StopFetch 中断某个结果集正在进行的取数。
//
// 已经读到的行仍然可以翻看，只是不再往下读了——慢链路上滚到某一段
// 突然卡住时，用户需要的是「停在这儿」，不是「全部作废」。
func (a *App) StopFetch(resultID string) error {
	c, ok := a.rs.Get(resultID)
	if !ok {
		return fmt.Errorf("结果集不存在或已关闭")
	}
	c.Cancel()
	return nil
}

// CountResultRows 把结果集读到底以取得精确总行数。
//
// 这是个显式动作而不是打开表就自动做：在大表上 COUNT 可能要跑很久，
// 用户点了「显示总行数」才值得付这个代价。
func (a *App) CountResultRows(resultID string) (int64, error) {
	c, ok := a.rs.Get(resultID)
	if !ok {
		return 0, fmt.Errorf("结果集不存在或已关闭")
	}
	n, _, err := c.CountRemaining()
	a.rs.EnforceBudget()
	return n, err
}

// GetResultStats 返回单个结果集的资源占用。
func (a *App) GetResultStats(resultID string) (rs.Stats, error) {
	c, ok := a.rs.Get(resultID)
	if !ok {
		return rs.Stats{}, fmt.Errorf("结果集不存在或已关闭")
	}
	return c.Stats(), nil
}

// CloseResult 关闭结果集并释放其内存与溢出文件。
func (a *App) CloseResult(resultID string) error {
	editMu.Lock()
	delete(editContexts, resultID)
	editMu.Unlock()
	return a.rs.Close(resultID)
}

// GetCellValue 取某个单元格的完整值。
//
// 网格里显示的是截断后的预览，完整值按主键回查——这样几百 MB 的 BLOB 列
// 既不会进内存缓存，也不会写进溢出文件，只有用户真的点开那一格才去取。
func (a *App) GetCellValue(resultID string, rowIndex int64, columnName string) (string, error) {
	ec, ok := a.getEditContext(resultID)
	if !ok {
		return "", fmt.Errorf("该结果集不支持查看完整值（只有单表查询可以回查原始数据）")
	}
	c, found := a.rs.Get(resultID)
	if !found {
		return "", fmt.Errorf("结果集不存在或已关闭")
	}
	page, err := c.Fetch(rowIndex, 1)
	if err != nil {
		return "", err
	}
	if len(page.Rows) == 0 {
		return "", fmt.Errorf("行不存在")
	}

	cols := c.Columns()
	idx := map[string]int{}
	for i, col := range cols {
		idx[strings.ToLower(col.Name)] = i
	}
	target, ok := idx[strings.ToLower(columnName)]
	if !ok {
		return "", fmt.Errorf("列不存在: %s", columnName)
	}
	cell := page.Rows[0][target]
	if !cell.Truncated {
		if cell.Null {
			return "", nil
		}
		return cell.Text, nil
	}

	_, sc, err := a.sqlSession(ec.connID)
	if err != nil {
		return "", err
	}
	d := sc.Dialect()
	var where []string
	var args []any
	n := 0
	for _, k := range ec.keyColumns {
		ki, ok := idx[strings.ToLower(k)]
		if !ok {
			return "", fmt.Errorf("结果集中缺少键列 %s", k)
		}
		kc := page.Rows[0][ki]
		if kc.Null {
			where = append(where, d.QuoteIdent(k)+" IS NULL")
			continue
		}
		n++
		if d.PlaceholderStyle() == "$N" {
			where = append(where, fmt.Sprintf("%s = $%d", d.QuoteIdent(k), n))
		} else {
			where = append(where, d.QuoteIdent(k)+" = ?")
		}
		args = append(args, kc.Text)
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		d.QuoteIdent(columnName), d.QualifyRef(ec.ref), strings.Join(where, " AND "))

	ctx, cancel := a.requestCtx(60)
	defer cancel()
	// 完整值单独扫描，把预览上限抬到足够大。
	stream, err := sc.Query(ctx, ec.database, query, dbx.ScanOptions{
		PreviewLimit:       64 << 20,
		BinaryPreviewLimit: 8 << 20,
	}, args...)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	row, err := stream.Next()
	if err != nil {
		return "", fmt.Errorf("未取到该行，数据可能已被其他会话修改")
	}
	if len(row) == 0 || row[0].Null {
		return "", nil
	}
	return row[0].Text, nil
}

// ExplainSQL 返回语句的执行计划。
func (a *App) ExplainSQL(connID, database, query string, analyze bool) (*StatementResult, error) {
	s, sc, err := a.sqlSession(connID)
	if err != nil {
		return nil, err
	}
	if analyze && s.cfg.ReadOnly {
		return nil, fmt.Errorf("EXPLAIN ANALYZE 会真正执行语句，只读连接下已拦截")
	}
	stmt := sc.Dialect().BuildExplain(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";")), analyze)

	ctx, cancel := a.requestCtx(120)
	defer cancel()
	start := time.Now()
	stream, err := longLivedQuery(ctx, func(qctx context.Context) (dbx.RowStream, error) {
		return sc.Query(qctx, database, stmt, a.scanOptions())
	})
	if err != nil {
		return nil, err
	}
	cursor := a.rs.Open(stream, rs.CursorMeta{
		Query: stmt, ConnectionID: connID, Database: database,
	})
	page, err := cursor.Fetch(0, 2000)
	if err != nil {
		_ = a.rs.Close(cursor.ID())
		return nil, err
	}
	return &StatementResult{
		SQL: stmt, Kind: string(sqlutil.KindQuery),
		ResultID: cursor.ID(), Page: page,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

// FormatSQL 对 SQL 做基础格式化（关键字换行 + 缩进）。
func (a *App) FormatSQL(script string, engine string) string {
	return sqlutil.Format(script, splitDialect(dbx.Engine(engine)))
}

// SplitStatements 把脚本切成语句列表，供编辑器高亮当前语句。
func (a *App) SplitStatements(script, engine string) []sqlutil.Statement {
	return sqlutil.Split(script, splitDialect(dbx.Engine(engine)))
}

// --- 历史与已保存查询 ---

// GetHistory 返回查询历史。
func (a *App) GetHistory(limit int) []store.HistoryEntry { return a.store.History(limit) }

// ClearHistory 清空查询历史。
func (a *App) ClearHistory() error { return a.store.ClearHistory() }

// GetSavedQueries 返回已保存的查询。
func (a *App) GetSavedQueries() []store.SavedQuery { return a.store.SavedQueries() }

// SaveQuery 保存一条查询。
func (a *App) SaveQuery(q store.SavedQuery) (store.SavedQuery, error) { return a.store.SaveQuery(q) }

// DeleteSavedQuery 删除一条已保存的查询。
func (a *App) DeleteSavedQuery(id string) error { return a.store.DeleteSavedQuery(id) }

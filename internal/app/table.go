package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
	"tacivan/internal/rs"
	"tacivan/internal/sqlutil"
	"tacivan/internal/store"
)

// OpenTableOptions 打开表数据时的条件。
type OpenTableOptions struct {
	// Where 手写的筛选条件（不含 WHERE 关键字）。
	Where string `json:"where"`
	// Filter 可视化筛选器构造的条件；与 Where 同时存在时用 AND 串起来。
	Filter FilterGroup `json:"filter"`
	// OrderBy 排序。
	OrderBy []dbx.OrderTerm `json:"orderBy"`
	// Columns 只取指定列，为空表示全部。
	Columns []string `json:"columns"`
	// FirstPageRows 首屏行数。
	FirstPageRows int `json:"firstPageRows"`
	// EstimatedRows 对象树上已知的估算行数，避免重复查询统计信息。
	EstimatedRows int64 `json:"estimatedRows"`
	// Limit 本次取数的行数上限；0 表示用设置里的默认值，-1 表示不加限制。
	Limit int `json:"limit"`
	// ProgressToken 前端生成的标识，带上它就能在打开过程中收到分步进度。
	ProgressToken string `json:"progressToken"`
}

// TableDataResult 打开表数据的结果。
type TableDataResult struct {
	ResultID string   `json:"resultId"`
	Page     *rs.Page `json:"page"`
	// SQL 实际执行的语句，显示在结果页底部。
	SQL string `json:"sql"`
	// Columns 表的完整列定义，网格据此决定编辑器类型。
	Columns        []dbx.Column  `json:"columns"`
	KeyColumns     []string      `json:"keyColumns"`
	Editable       bool          `json:"editable"`
	EditableReason string        `json:"editableReason,omitempty"`
	Ref            dbx.ObjectRef `json:"ref"`
	DurationMS     int64         `json:"durationMs"`
	// EstimatedRows 来自统计信息的估算行数，先于精确 COUNT 展示。
	EstimatedRows int64 `json:"estimatedRows"`
	// Limit 本次实际使用的行数上限，0 表示未加限制。
	Limit int `json:"limit"`
}

// OpenTableData 打开一张表的数据视图。
//
// 不带 LIMIT：取多少行由前端的滚动位置决定，后端只在流上按需推进。
// 这正是「打开一亿行的表也不会卡死」的原因——它和打开一千行的表做的事一样多。
func (a *App) OpenTableData(connID string, ref dbx.ObjectRef, opts OpenTableOptions) (*TableDataResult, error) {
	s, sc, err := a.sqlSession(connID)
	if err != nil {
		return nil, err
	}
	if ref.Database == "" {
		s.mu.Lock()
		ref.Database = s.currentDB
		s.mu.Unlock()
	}
	pageRows := opts.FirstPageRows
	if pageRows <= 0 {
		pageRows = a.store.Settings().GridPageSize
	}

	// 默认给查询加 LIMIT：没有上限的 SELECT 一旦配上 ORDER BY，
	// 服务端要为整张表排序，大表上会直接把会话拖死。
	limit := opts.Limit
	if limit == 0 {
		limit = a.store.Settings().RowLimit
	}
	if limit < 0 {
		limit = 0
	}

	d := sc.Dialect()
	where, err := combineWhere(d, opts.Where, opts.Filter)
	if err != nil {
		return nil, err
	}
	query := d.BuildSelect(ref, dbx.SelectOptions{
		Columns: opts.Columns,
		Where:   where,
		OrderBy: opts.OrderBy,
		Limit:   limit,
	})

	ctx, cancel := a.requestCtx(120)
	defer cancel()

	opStart := time.Now()
	rep := a.reporterFor(opts.ProgressToken)
	defer rep.done()

	// 「停止」从第一秒起就要能按。此刻游标还不存在，所以先绑住请求 context——
	// 它管着表结构读取和查询建立这两段；游标一建出来就补进来。
	// 注意取消 ctx 不会误伤结果集：查询流是用 WithoutCancel 派生的。
	var live atomic.Pointer[rs.Cursor]
	rep.bindCancel(func() {
		cancel()
		if c := live.Load(); c != nil {
			c.Cancel()
		}
	})

	// 表结构与数据并发取：结构只用来判断能否就地编辑，
	// 不该挡在用户和数据之间。读不到就降级成只读，而不是整个打不开。
	var (
		wg     sync.WaitGroup
		def    *dbx.TableDefinition
		defErr error
		defMS  int64
	)
	rep.stage("表结构", "SHOW COLUMNS / INDEX / CREATE")
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.Now()
		def, defErr = sc.TableDefinition(ctx, ref)
		defMS = time.Since(t).Milliseconds()
	}()

	rep.stage("执行查询", query)
	start := time.Now()
	stream, err := longLivedQuery(ctx, func(qctx context.Context) (dbx.RowStream, error) {
		return sc.Query(qctx, ref.Database, query, a.scanOptions())
	})
	queryMS := time.Since(start).Milliseconds()
	rep.stage("等待表结构", "")
	wg.Wait()
	if err != nil {
		logx.Op("OpenTableData", opStart, "table", ref.Name, "err", err.Error())
		return nil, err
	}
	cursor := a.rs.Open(stream, rs.CursorMeta{
		Query: query, Ref: ref, ConnectionID: connID, Database: ref.Database,
	})
	live.Store(cursor)
	rep.stage("读取数据", "")
	rep.attach(cursor, "读取数据")
	fetchStart := time.Now()
	page, err := cursor.Fetch(0, int64(pageRows))
	fetchMS := time.Since(fetchStart).Milliseconds()
	cursor.SetProgress(nil)
	if err != nil {
		_ = a.rs.Close(cursor.ID())
		logx.Op("OpenTableData", opStart, "table", ref.Name, "err", err.Error())
		return nil, err
	}

	res := &TableDataResult{
		ResultID:   cursor.ID(),
		Page:       page,
		SQL:        query,
		Limit:      limit,
		Ref:        ref,
		DurationMS: time.Since(start).Milliseconds(),
	}
	if def != nil {
		res.Columns = def.Columns
	}

	var keys []string
	if def != nil {
		keys = pickKeyColumns(def)
	}
	switch {
	case def == nil:
		res.EditableReason = "未能读取表结构（" + defErr.Error() + "），已降级为只读"
	case ref.Kind == dbx.KindView:
		res.EditableReason = "视图不支持直接编辑"
	case len(keys) == 0:
		res.EditableReason = "该表没有主键或唯一索引，无法安全定位行"
	case len(opts.Columns) > 0 && !containsAll(opts.Columns, keys):
		res.EditableReason = "当前显示的列中缺少键列，无法定位行"
	case s.cfg.ReadOnly:
		res.EditableReason = "当前连接为只读"
	default:
		res.Editable = true
		res.KeyColumns = keys
		a.setEditContext(cursor.ID(), &editContext{
			ref: ref, columns: def.Columns, keyColumns: keys,
			connID: connID, database: ref.Database,
		})
	}

	// waitMs 是总耗时里既不属于结构、也不属于查询和取数的那一段——
	// 它等于本次调用在各种内部排队上花掉的时间。之前有过一次
	// 三段加起来两秒多、总耗时三十一秒的记录，差额全在这里，
	// 顺着它才找到内存回收把游标锁给堵了。
	cs := cursor.Stats()
	logx.Op("OpenTableData", opStart,
		"table", ref.Name,
		"defMs", defMS,
		"queryMs", queryMS,
		"fetchMs", fetchMS,
		"readMs", cs.ReadMS,
		"readBytes", cs.ReadBytes,
		"waitMs", time.Since(opStart).Milliseconds()-defMS-queryMS-fetchMS,
		"firstRows", len(page.Rows),
		"editable", res.Editable,
	)

	// 估算行数由前端从对象树带过来（那里已经查过一次）。
	// 早先在这里又查一遍 information_schema 拿行数，在 MySQL 5.7 这种
	// information_schema 走临时表实现的版本上，大实例要额外等好几秒。
	res.EstimatedRows = opts.EstimatedRows
	return res, nil
}

// combineWhere 把手写条件与可视化筛选器的条件合并。
func combineWhere(d dbx.Dialect, manual string, group FilterGroup) (string, error) {
	built, err := buildFilterWhere(d, group)
	if err != nil {
		return "", err
	}
	manual = strings.TrimSpace(manual)
	switch {
	case manual == "" && built == "":
		return "", nil
	case manual == "":
		return built, nil
	case built == "":
		return manual, nil
	}
	return "(" + manual + ") AND (" + built + ")", nil
}

func containsAll(have, want []string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[strings.ToLower(h)] = true
	}
	for _, w := range want {
		if !set[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

// CountTableRows 返回带当前筛选条件的精确行数。
func (a *App) CountTableRows(connID string, ref dbx.ObjectRef, where string) (int64, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return 0, err
	}
	ctx, cancel := a.requestCtx(300)
	defer cancel()
	stream, err := sc.Query(ctx, ref.Database, sc.Dialect().BuildCount(ref, where), a.scanOptions())
	if err != nil {
		return 0, err
	}
	defer stream.Close()
	row, err := stream.Next()
	if err != nil || len(row) == 0 {
		return 0, fmt.Errorf("未取到行数")
	}
	var n int64
	fmt.Sscanf(row[0].Text, "%d", &n)
	return n, nil
}

// ChangeSet 数据网格一次提交的全部变更。
type ChangeSet struct {
	ResultID string          `json:"resultId"`
	Changes  []dbx.RowChange `json:"changes"`
}

// ChangePreview 变更的 SQL 预览。
type ChangePreview struct {
	Statements []string `json:"statements"`
	SQL        string   `json:"sql"`
	// Warnings 需要提醒用户的风险点。
	Warnings []string `json:"warnings,omitempty"`
}

// PreviewChanges 生成变更对应的 SQL，但不执行。
//
// 对齐 Navicat 的行为：保存之前用户能先看到将要跑的是哪几条语句。
func (a *App) PreviewChanges(cs ChangeSet) (*ChangePreview, error) {
	ec, sc, err := a.editTarget(cs.ResultID)
	if err != nil {
		return nil, err
	}
	d := sc.Dialect()
	out := &ChangePreview{}
	for i, ch := range cs.Changes {
		st, err := d.BuildRowWrite(ec.ref, ec.columns, ch)
		if err != nil {
			return nil, fmt.Errorf("第 %d 处变更无法生成语句：%w", i+1, err)
		}
		out.Statements = append(out.Statements, st.Preview)
		if w := warnForChange(ch); w != "" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("第 %d 处变更：%s", i+1, w))
		}
	}
	out.SQL = strings.Join(out.Statements, ";\n") + ";"
	return out, nil
}

func warnForChange(ch dbx.RowChange) string {
	if strings.EqualFold(ch.Op, "delete") && len(ch.Keys) == 0 {
		return "缺少定位条件，将影响整张表"
	}
	return ""
}

// ApplyResult 一次提交的结果。
type ApplyResult struct {
	// Applied 成功执行的语句数。
	Applied int `json:"applied"`
	// RowsAffected 累计影响行数。
	RowsAffected int64 `json:"rowsAffected"`
	// Statements 实际执行的语句预览。
	Statements []string `json:"statements"`
	DurationMS int64    `json:"durationMs"`
}

// ApplyChanges 提交数据网格的变更。
//
// 整批变更在一个事务里执行：要么全成功，要么一条都不落，
// 不会出现「改了 5 行，第 6 行报错，前 5 行已经写进去了」这种半截状态。
func (a *App) ApplyChanges(cs ChangeSet) (*ApplyResult, error) {
	ec, sc, err := a.editTarget(cs.ResultID)
	if err != nil {
		return nil, err
	}
	if len(cs.Changes) == 0 {
		return &ApplyResult{}, nil
	}
	s, err := a.lookup(ec.connID)
	if err != nil {
		return nil, err
	}
	if s.cfg.ReadOnly {
		return nil, fmt.Errorf("当前连接为只读，已拦截数据修改")
	}

	d := sc.Dialect()
	stmts := make([]dbx.Statement, 0, len(cs.Changes))
	for i, ch := range cs.Changes {
		st, err := d.BuildRowWrite(ec.ref, ec.columns, ch)
		if err != nil {
			return nil, fmt.Errorf("第 %d 处变更无法生成语句：%w", i+1, err)
		}
		stmts = append(stmts, st)
	}

	ctx, cancel := a.requestCtx(120)
	defer cancel()
	start := time.Now()

	useTx := s.conn.Capability().Transactions
	if useTx {
		if _, err := sc.Exec(ctx, ec.database, "BEGIN"); err != nil {
			return nil, fmt.Errorf("开启事务失败: %w", err)
		}
	}

	res := &ApplyResult{}
	for i, st := range stmts {
		r, err := sc.Exec(ctx, ec.database, st.SQL, st.Args...)
		if err != nil {
			if useTx {
				_, _ = sc.Exec(ctx, ec.database, "ROLLBACK")
			}
			return nil, fmt.Errorf("第 %d 处变更执行失败（已回滚全部变更）：%w\n\n%s", i+1, err, st.Preview)
		}
		// 影响 0 行说明目标行已被别人改掉或删掉，继续下去会写出错误的数据。
		if r.RowsAffected == 0 && !strings.HasPrefix(strings.ToUpper(st.SQL), "INSERT") {
			if useTx {
				_, _ = sc.Exec(ctx, ec.database, "ROLLBACK")
			}
			return nil, fmt.Errorf("第 %d 处变更没有匹配到任何行（已回滚全部变更）：\n"+
				"该行可能已被其他会话修改或删除，请刷新后重试\n\n%s", i+1, st.Preview)
		}
		res.Applied++
		res.RowsAffected += r.RowsAffected
		res.Statements = append(res.Statements, st.Preview)
	}

	if useTx {
		if _, err := sc.Exec(ctx, ec.database, "COMMIT"); err != nil {
			return nil, fmt.Errorf("提交事务失败: %w", err)
		}
	}
	res.DurationMS = time.Since(start).Milliseconds()

	a.store.AddHistory(store.HistoryEntry{
		ConnectionID: ec.connID,
		Connection:   s.cfg.Name,
		Database:     ec.database,
		SQL:          strings.Join(res.Statements, ";\n") + ";",
		At:           start,
		DurationMS:   res.DurationMS,
		RowsAffected: res.RowsAffected,
		Success:      true,
	})
	return res, nil
}

func (a *App) editTarget(resultID string) (*editContext, dbx.SQLConn, error) {
	ec, ok := a.getEditContext(resultID)
	if !ok {
		return nil, nil, fmt.Errorf("该结果集不可编辑")
	}
	_, sc, err := a.sqlSession(ec.connID)
	if err != nil {
		return nil, nil, err
	}
	return ec, sc, nil
}

// RefreshRow 重新读取某一行的最新值，用于「保存后刷新当前行」。
func (a *App) RefreshRow(resultID string, rowIndex int64) (dbx.Row, error) {
	c, ok := a.rs.Get(resultID)
	if !ok {
		return nil, fmt.Errorf("结果集不存在或已关闭")
	}
	page, err := c.Fetch(rowIndex, 1)
	if err != nil {
		return nil, err
	}
	if len(page.Rows) == 0 {
		return nil, fmt.Errorf("行不存在")
	}
	return page.Rows[0], nil
}

// GenerateSelectSQL 为对象生成一条 SELECT 模板，对应右键菜单里的「生成 SQL」。
func (a *App) GenerateSelectSQL(connID string, ref dbx.ObjectRef, limit int) (string, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = 100
	}
	def, err := a.GetTableDefinition(connID, ref)
	if err != nil {
		return sc.Dialect().BuildSelect(ref, dbx.SelectOptions{Limit: limit}), nil
	}
	cols := make([]string, 0, len(def.Columns))
	for _, c := range def.Columns {
		cols = append(cols, c.Name)
	}
	return sc.Dialect().BuildSelect(ref, dbx.SelectOptions{Columns: cols, Limit: limit}), nil
}

// GenerateInsertSQL 生成 INSERT 模板。
func (a *App) GenerateInsertSQL(connID string, ref dbx.ObjectRef) (string, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return "", err
	}
	def, err := a.GetTableDefinition(connID, ref)
	if err != nil {
		return "", err
	}
	d := sc.Dialect()
	var cols, vals []string
	for _, c := range def.Columns {
		if c.AutoIncrement || c.Generated != "" {
			continue
		}
		cols = append(cols, d.QuoteIdent(c.Name))
		vals = append(vals, placeholderFor(c))
	}
	return fmt.Sprintf("INSERT INTO %s\n  (%s)\nVALUES\n  (%s);",
		d.QualifyRef(ref), strings.Join(cols, ", "), strings.Join(vals, ", ")), nil
}

// GenerateUpdateSQL 生成 UPDATE 模板。
func (a *App) GenerateUpdateSQL(connID string, ref dbx.ObjectRef) (string, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return "", err
	}
	def, err := a.GetTableDefinition(connID, ref)
	if err != nil {
		return "", err
	}
	d := sc.Dialect()
	keys := pickKeyColumns(def)
	keySet := map[string]bool{}
	for _, k := range keys {
		keySet[k] = true
	}
	var sets []string
	for _, c := range def.Columns {
		if keySet[c.Name] || c.Generated != "" {
			continue
		}
		sets = append(sets, d.QuoteIdent(c.Name)+" = "+placeholderFor(c))
	}
	where := "<条件>"
	if len(keys) > 0 {
		var parts []string
		for _, k := range keys {
			parts = append(parts, d.QuoteIdent(k)+" = <值>")
		}
		where = strings.Join(parts, " AND ")
	}
	return fmt.Sprintf("UPDATE %s\nSET\n  %s\nWHERE %s;",
		d.QualifyRef(ref), strings.Join(sets, ",\n  "), where), nil
}

func placeholderFor(c dbx.Column) string {
	return "<" + c.Name + ":" + c.Type + ">"
}

// GetStatementAt 返回光标处的语句，供「执行当前语句」使用。
func (a *App) GetStatementAt(script string, offset int, engine string) (sqlutil.Statement, error) {
	st, ok := sqlutil.StatementAt(script, offset, splitDialect(dbx.Engine(engine)))
	if !ok {
		return sqlutil.Statement{}, fmt.Errorf("光标处没有可执行的语句")
	}
	return st, nil
}

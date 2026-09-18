package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
)

// SearchRequest 全库搜索的条件。
type SearchRequest struct {
	ConnID string `json:"connId"`
	// Databases 搜索范围；为空表示当前库。
	Databases []string `json:"databases"`
	// Keyword 关键字。对象模式按子串匹配名字，数据模式按 LIKE 匹配值。
	Keyword string `json:"keyword"`
	// Mode "object" 搜对象名与列名，"data" 搜表里的数据。
	Mode string `json:"mode"`
	// SearchColumns 对象模式下是否连列名一起搜。
	//
	// 单列出来是因为代价差一个数量级：表名一个库一次查询就够，
	// 列名则要一张表一次。慢链路上这个选项必须由用户明确打开。
	SearchColumns bool `json:"searchColumns"`
	// CaseSensitive 区分大小写。
	CaseSensitive bool `json:"caseSensitive"`
	// MaxRowsPerTable 数据模式下每张表最多返回几行。
	MaxRowsPerTable int `json:"maxRowsPerTable"`
	// MaxTables 数据模式下最多扫多少张表，防止一脚踩进几千张表里。
	MaxTables int `json:"maxTables"`
	// ProgressToken 带上就能收到进度并支持中断。
	ProgressToken string `json:"progressToken"`
}

// SearchHit 一条命中。
type SearchHit struct {
	Database string `json:"database"`
	Schema   string `json:"schema"`
	// Table 所属表；Kind 为 table/view 时即对象本身。
	Table string `json:"table"`
	// Column 命中的列，对象模式下为列名，数据模式下为命中值所在列。
	Column string `json:"column"`
	// Kind table | view | column | row
	Kind string `json:"kind"`
	// Value 数据模式下命中值的预览。
	Value string `json:"value"`
	// Comment 对象或列的注释，帮助判断是不是要找的那个。
	Comment string `json:"comment"`
}

// SearchResult 一次搜索的结果。
type SearchResult struct {
	Hits []SearchHit `json:"hits"`
	// Scanned 实际扫过的库/表数量。
	ScannedDatabases int `json:"scannedDatabases"`
	ScannedTables    int `json:"scannedTables"`
	// Stopped 用户中途停了；Hits 里已经找到的仍然有效。
	Stopped bool `json:"stopped"`
	// Truncated 因为触达上限而没扫完。
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"durationMs"`
	Note       string `json:"note,omitempty"`
}

// 搜索的并发度。
//
// 不开大：并发发查询会抢连接池里的连接，而结果集游标本身也要占连接。
// 开到跟池子一样大，用户在搜索期间就别想再打开任何表了。
const searchConcurrency = 4

// Search 在一个连接的若干个库里找东西。
//
// 全程可中断：慢实例上搜一个库要几十秒，用户随时可能发现范围选错了。
func (a *App) Search(req SearchRequest) (*SearchResult, error) {
	_, sc, err := a.sqlSession(req.ConnID)
	if err != nil {
		return nil, err
	}
	kw := strings.TrimSpace(req.Keyword)
	if kw == "" {
		return nil, fmt.Errorf("请输入要搜索的内容")
	}
	if len(req.Databases) == 0 {
		return nil, fmt.Errorf("请至少选择一个数据库")
	}
	if req.MaxRowsPerTable <= 0 {
		req.MaxRowsPerTable = 5
	}
	if req.MaxTables <= 0 {
		req.MaxTables = 200
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	rep := a.reporterFor(req.ProgressToken)
	defer rep.done()
	rep.bindCancel(cancel)

	res := &SearchResult{Hits: []SearchHit{}}
	if req.Mode == "data" {
		err = a.searchData(ctx, sc, req, kw, res, rep)
	} else {
		err = a.searchObjects(ctx, sc, req, kw, res, rep)
	}
	res.DurationMS = time.Since(start).Milliseconds()
	if ctx.Err() != nil {
		res.Stopped = true
		err = nil
	}
	if err != nil {
		return nil, err
	}
	sortHits(res.Hits)
	logx.Op("Search", start,
		"mode", req.Mode, "dbs", len(req.Databases),
		"tables", res.ScannedTables, "hits", len(res.Hits),
		"stopped", res.Stopped, "truncated", res.Truncated)
	return res, nil
}

func sortHits(hits []SearchHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Database != hits[j].Database {
			return hits[i].Database < hits[j].Database
		}
		if hits[i].Table != hits[j].Table {
			return hits[i].Table < hits[j].Table
		}
		return hits[i].Column < hits[j].Column
	})
}

func matches(hay, needle string, caseSensitive bool) bool {
	if caseSensitive {
		return strings.Contains(hay, needle)
	}
	return strings.Contains(strings.ToLower(hay), strings.ToLower(needle))
}

// searchObjects 搜对象名，可选连列名一起搜。
func (a *App) searchObjects(
	ctx context.Context, sc dbx.SQLConn, req SearchRequest, kw string,
	res *SearchResult, rep *reporter,
) error {
	kinds := []dbx.ObjectKind{dbx.KindTable, dbx.KindView}
	type tableRef struct{ db, schema, name string }
	var pending []tableRef

	for _, db := range req.Databases {
		if ctx.Err() != nil {
			return nil
		}
		rep.stage("列出对象", db)
		objs, err := sc.Objects(ctx, db, "", kinds)
		if err != nil {
			// 单个库读不到不该让整次搜索失败——权限不足、库刚被删，
			// 都属于"这个库跳过"而不是"搜索出错"。
			res.Note = fmt.Sprintf("%s：%v（已跳过）", db, err)
			continue
		}
		res.ScannedDatabases++
		for _, o := range objs {
			if matches(o.Name, kw, req.CaseSensitive) {
				res.Hits = append(res.Hits, SearchHit{
					Database: db, Schema: o.Schema, Table: o.Name,
					Kind: string(o.Kind), Comment: o.Comment,
				})
			}
			if req.SearchColumns && o.Kind == dbx.KindTable {
				pending = append(pending, tableRef{db, o.Schema, o.Name})
			}
		}
	}

	if !req.SearchColumns || len(pending) == 0 {
		return nil
	}
	if len(pending) > req.MaxTables {
		pending = pending[:req.MaxTables]
		res.Truncated = true
	}

	// 列名要一张表读一次结构，这里是整个搜索最贵的一段。
	var (
		mu   sync.Mutex
		done int
		wg   sync.WaitGroup
		sem  = make(chan struct{}, searchConcurrency)
	)
	for _, t := range pending {
		if ctx.Err() != nil {
			break
		}
		t := t
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			def, err := sc.TableDefinition(ctx, dbx.ObjectRef{
				Database: t.db, Schema: t.schema, Name: t.name, Kind: dbx.KindTable,
			})
			mu.Lock()
			defer mu.Unlock()
			done++
			res.ScannedTables++
			rep.tick("搜索列名", fmt.Sprintf("%s.%s（%d/%d）", t.db, t.name, done, len(pending)))
			if err != nil || def == nil {
				return
			}
			for _, c := range def.Columns {
				if matches(c.Name, kw, req.CaseSensitive) {
					res.Hits = append(res.Hits, SearchHit{
						Database: t.db, Schema: t.schema, Table: t.name,
						Column: c.Name, Kind: "column", Comment: c.Comment,
					})
				}
			}
		}()
	}
	wg.Wait()
	return nil
}

// searchData 在表里找匹配的值。
func (a *App) searchData(
	ctx context.Context, sc dbx.SQLConn, req SearchRequest, kw string,
	res *SearchResult, rep *reporter,
) error {
	type tableRef struct{ db, schema, name string }
	var pending []tableRef
	for _, db := range req.Databases {
		if ctx.Err() != nil {
			return nil
		}
		rep.stage("列出表", db)
		objs, err := sc.Objects(ctx, db, "", []dbx.ObjectKind{dbx.KindTable})
		if err != nil {
			res.Note = fmt.Sprintf("%s：%v（已跳过）", db, err)
			continue
		}
		res.ScannedDatabases++
		for _, o := range objs {
			pending = append(pending, tableRef{db, o.Schema, o.Name})
		}
	}
	if len(pending) > req.MaxTables {
		pending = pending[:req.MaxTables]
		res.Truncated = true
	}

	var (
		mu   sync.Mutex
		done int
		wg   sync.WaitGroup
		sem  = make(chan struct{}, searchConcurrency)
	)
	for _, t := range pending {
		if ctx.Err() != nil {
			break
		}
		t := t
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ref := dbx.ObjectRef{Database: t.db, Schema: t.schema, Name: t.name, Kind: dbx.KindTable}
			hits := a.searchOneTable(ctx, sc, ref, kw, req)
			mu.Lock()
			done++
			res.ScannedTables++
			res.Hits = append(res.Hits, hits...)
			rep.tick("搜索数据", fmt.Sprintf("%s.%s（%d/%d），已命中 %d",
				t.db, t.name, done, len(pending), len(res.Hits)))
			mu.Unlock()
		}()
	}
	wg.Wait()
	return nil
}

// searchOneTable 在一张表里找匹配的值。
//
// 出错一律当成"这张表没有命中"：几百张表里总会有几张因为权限、
// 字符集或者正在被 DDL 而读不了，为此中断整次搜索得不偿失。
func (a *App) searchOneTable(
	ctx context.Context, sc dbx.SQLConn, ref dbx.ObjectRef, kw string, req SearchRequest,
) []SearchHit {
	def, err := sc.TableDefinition(ctx, ref)
	if err != nil || def == nil {
		return nil
	}
	d := sc.Dialect()
	cols := searchableColumns(def.Columns)
	if len(cols) == 0 {
		return nil
	}

	// 值的转义交给方言，这里只负责挑列和拼条件——
	// 关键字里的 % 和 _ 是普通字符，不能当通配符。
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		parts = append(parts, matchPredicate(d, c, kw, req.CaseSensitive))
	}
	query := d.BuildSelect(ref, dbx.SelectOptions{
		Where: strings.Join(parts, " OR "),
		Limit: req.MaxRowsPerTable,
	})

	stream, err := sc.Query(ctx, ref.Database, query, a.scanOptions())
	if err != nil {
		return nil
	}
	defer func() { _ = stream.Close() }()

	meta := stream.Columns()
	var out []SearchHit
	for {
		row, err := stream.Next()
		if err != nil {
			break // 含 io.EOF
		}
		// 找出到底是哪一列命中的，直接告诉用户，省得他再去一列列看。
		col, val := firstMatch(meta, row, kw, req.CaseSensitive)
		out = append(out, SearchHit{
			Database: ref.Database, Schema: ref.Schema, Table: ref.Name,
			Column: col, Kind: "row", Value: val,
		})
	}
	return out
}

func firstMatch(meta []dbx.ColumnMeta, row dbx.Row, kw string, cs bool) (string, string) {
	for i, cell := range row {
		if cell.Null || i >= len(meta) {
			continue
		}
		if matches(cell.Text, kw, cs) {
			return meta[i].Name, cell.Text
		}
	}
	return "", ""
}

// searchableColumns 挑出值得做 LIKE 的列。
//
// 数字和时间列也能 LIKE（引擎会隐式转字符串），但那既慢又几乎全是误命中，
// 而二进制列 LIKE 出来的东西没法看。所以只搜文本和 JSON。
func searchableColumns(cols []dbx.Column) []dbx.Column {
	out := make([]dbx.Column, 0, len(cols))
	for _, c := range cols {
		if c.Generated != "" {
			continue
		}
		switch strings.ToLower(c.Type) {
		case "char", "varchar", "text", "tinytext", "mediumtext", "longtext",
			"json", "jsonb", "enum", "set", "nvarchar", "nchar", "character",
			"character varying", "citext", "uuid", "name":
			out = append(out, c)
		}
	}
	return out
}

// matchPredicate 为一列生成「包含关键字」的条件。
//
// 三个引擎的大小写语义各不相同，不能用同一个算子糊过去：
//   - MySQL    默认 LIKE 是否区分大小写取决于列的排序规则（*_ci 不分），
//     要强制区分得写 LIKE BINARY。
//   - Postgres LIKE 本来就区分大小写，不区分要用 ILIKE。
//   - SQLite   LIKE 对 ASCII 一律不区分大小写且关不掉，要区分只能换 instr()。
//
// json/jsonb/uuid 在 Postgres 里不能直接 LIKE，得先转成文本。
func matchPredicate(d dbx.Dialect, c dbx.Column, kw string, caseSensitive bool) string {
	col := d.QuoteIdent(c.Name)
	// 显式声明转义符。SQLite 的 LIKE 默认压根没有转义字符，
	// 不写这一段的话搜 "100%" 会把整张表都匹配上——那不是慢，是结果全错。
	like := d.QuoteString("%"+escapeLike(kw)+"%") + " ESCAPE " + d.QuoteString(`\`)
	switch d.Engine() {
	case dbx.EnginePostgres:
		switch strings.ToLower(c.Type) {
		case "json", "jsonb", "uuid":
			col += "::text"
		}
		if caseSensitive {
			return fmt.Sprintf("%s LIKE %s", col, like)
		}
		return fmt.Sprintf("%s ILIKE %s", col, like)
	case dbx.EngineSQLite:
		if caseSensitive {
			// instr 按字节比，天然区分大小写；关键字里的 % _ 也不再是通配符。
			return fmt.Sprintf("instr(%s, %s) > 0", col, d.QuoteString(kw))
		}
		return fmt.Sprintf("%s LIKE %s", col, like)
	default:
		if caseSensitive {
			return fmt.Sprintf("%s LIKE BINARY %s", col, like)
		}
		return fmt.Sprintf("%s LIKE %s", col, like)
	}
}

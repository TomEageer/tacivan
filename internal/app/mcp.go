package app

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/driver/mcpdrv"
	"tacivan/internal/logx"
	"tacivan/internal/mcp"
	"tacivan/internal/rs"
)

func (a *App) mcpConn(connID string) (*mcpdrv.Conn, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return nil, err
	}
	c, ok := s.conn.(*mcpdrv.Conn)
	if !ok {
		return nil, fmt.Errorf("不是 MCP 连接")
	}
	return c, nil
}

// McpTools 列出 MCP server 的工具。
func (a *App) McpTools(connID string) ([]mcp.Tool, error) {
	c, err := a.mcpConn(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(60)
	defer cancel()
	return c.Client().Tools(ctx)
}

// McpResources 列出 MCP server 的资源。
func (a *App) McpResources(connID string) ([]mcp.Resource, error) {
	c, err := a.mcpConn(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(60)
	defer cancel()
	return c.Client().Resources(ctx)
}

// McpCallResult 工具调用结果：文本总是有；结果是表格形状时另给一个结果集。
type McpCallResult struct {
	Text       string `json:"text"`
	IsError    bool   `json:"isError"`
	ResultID   string `json:"resultId,omitempty"`
	Rows       int    `json:"rows"`
	DurationMS int64  `json:"durationMs"`
}

// McpCallTool 调用一个工具。args 是按工具的 JSON Schema 填好的参数。
func (a *App) McpCallTool(connID, name string, args map[string]any) (*McpCallResult, error) {
	c, err := a.mcpConn(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(300)
	defer cancel()
	start := time.Now()
	res, err := c.Client().Call(ctx, name, args)
	logx.Op("McpCallTool", start, "conn", connID, "tool", name, "err", errText(err))
	if err != nil {
		return nil, err
	}
	out := &McpCallResult{Text: mcp.TextOf(res.Content), IsError: res.IsError, DurationMS: time.Since(start).Milliseconds()}
	// 结果长得像一张表（对象数组）就进网格：筛选、导出都能照常用
	if rows := tabular(res); len(rows) > 0 {
		cur := a.rs.Open(newRowsStream(rows), rs.CursorMeta{Query: "mcp:" + name, ConnectionID: connID})
		out.ResultID = cur.ID()
		out.Rows = len(rows)
	}
	return out, nil
}

// McpReadResource 读一个资源，返回文本。
func (a *App) McpReadResource(connID, uri string) (string, error) {
	c, err := a.mcpConn(connID)
	if err != nil {
		return "", err
	}
	ctx, cancel := a.requestCtx(120)
	defer cancel()
	cs, err := c.Client().Read(ctx, uri)
	if err != nil {
		return "", err
	}
	return mcp.TextOf(cs), nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// tabular 从结果里挖出"对象数组"：优先 structuredContent，其次每块文本试着当 JSON 解析。
func tabular(res *mcp.CallResult) []map[string]any {
	try := func(raw []byte) []map[string]any {
		var arr []map[string]any
		if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
			return arr
		}
		// {items:[...]} / {data:[...]} / {rows:[...]} 这种包了一层的也认
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) == nil {
			for _, k := range []string{"rows", "items", "data", "results", "list"} {
				if v, ok := obj[k]; ok {
					if json.Unmarshal(v, &arr) == nil && len(arr) > 0 {
						return arr
					}
				}
			}
		}
		return nil
	}
	if len(res.StructuredContent) > 0 {
		if rows := try(res.StructuredContent); rows != nil {
			return rows
		}
	}
	for _, c := range res.Content {
		if c.Type == "text" && strings.HasPrefix(strings.TrimSpace(c.Text), "[") || strings.HasPrefix(strings.TrimSpace(c.Text), "{") {
			if rows := try([]byte(c.Text)); rows != nil {
				return rows
			}
		}
	}
	return nil
}

// rowsStream 把对象数组包成结果集流；列取所有对象键的并集，按首次出现顺序。
type rowsStream struct {
	cols []dbx.ColumnMeta
	keys []string
	rows []map[string]any
	i    int
}

func newRowsStream(rows []map[string]any) *rowsStream {
	seen := map[string]bool{}
	var keys []string
	for _, r := range rows {
		ks := make([]string, 0, len(r))
		for k := range r {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		for _, k := range ks {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	cols := make([]dbx.ColumnMeta, 0, len(keys))
	for _, k := range keys {
		cols = append(cols, dbx.ColumnMeta{Name: k, Type: "json", Class: classOf(rows, k), Nullable: 1})
	}
	return &rowsStream{cols: cols, keys: keys, rows: rows}
}

func classOf(rows []map[string]any, k string) dbx.ValueClass {
	for _, r := range rows {
		switch r[k].(type) {
		case float64:
			return dbx.ClassNumber
		case bool:
			return dbx.ClassBool
		case string:
			return dbx.ClassString
		case map[string]any, []any:
			return dbx.ClassJSON
		}
	}
	return dbx.ClassString
}

func (s *rowsStream) Columns() []dbx.ColumnMeta { return s.cols }
func (s *rowsStream) Close() error              { s.rows = nil; return nil }
func (s *rowsStream) Next() (dbx.Row, error) {
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	r := s.rows[s.i]
	s.i++
	row := make(dbx.Row, len(s.keys))
	for j, k := range s.keys {
		v, ok := r[k]
		if !ok || v == nil {
			row[j] = dbx.Cell{Null: true}
			continue
		}
		var text string
		switch x := v.(type) {
		case string:
			text = x
		case float64:
			b, _ := json.Marshal(x)
			text = string(b)
		case map[string]any, []any:
			b, _ := json.Marshal(x)
			text = string(b)
		default:
			text = fmt.Sprint(x)
		}
		row[j] = dbx.Cell{Text: text, Size: len(text)}
	}
	return row, nil
}

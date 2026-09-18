// Package mcp 是一个最小的 MCP（Model Context Protocol）客户端。
//
// 只做数据库客户端用得上的那一小块：连上一个 MCP server，列出它的工具和资源，
// 调用工具、读取资源。不接大模型——工具由用户在界面上手动点，参数用表单填，
// 这样"查错库"的责任链是清楚的：每一次调用都是人按下去的。
//
// 两种传输：
//   - stdio：本机起一个子进程，stdin/stdout 上按行 JSON-RPC（Claude Desktop 那种配置）
//   - http：Streamable HTTP，POST JSON-RPC 到一个 URL，响应可能是 JSON 也可能是 SSE
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocolVersion 本客户端声明的协议版本。
const ProtocolVersion = "2025-06-18"

// Config 连接参数。
type Config struct {
	// Transport: stdio | http
	Transport string
	// Command 仅 stdio：argv。
	Command []string
	// Env 仅 stdio：附加环境变量 KEY=VALUE。
	Env []string
	// URL 仅 http。
	URL string
	// Headers 仅 http：附加请求头。
	Headers map[string]string
}

// Tool 一个工具的描述。InputSchema 是 JSON Schema 原文，前端据此渲染表单。
type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// Resource 一个资源的描述。
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Content 工具结果或资源内容里的一块。
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Data     string `json:"data,omitempty"`
}

// CallResult 工具调用结果。
type CallResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// ServerInfo 握手时 server 报的身份。
type ServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Client 一个已连接的 MCP server。
type Client struct {
	cfg  Config
	info ServerInfo
	seq  atomic.Int64

	// stdio
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	mu      sync.Mutex
	pending map[int64]chan rpcMsg
	dead    atomic.Bool
	logf    func(string, ...any)

	// http
	http      *http.Client
	sessionID string
}

// Dial 建立连接并完成 initialize 握手。
func Dial(ctx context.Context, cfg Config, logf func(string, ...any)) (*Client, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	c := &Client{cfg: cfg, pending: map[int64]chan rpcMsg{}, logf: logf}
	switch cfg.Transport {
	case "stdio":
		if len(cfg.Command) == 0 {
			return nil, errors.New("stdio 传输需要启动命令")
		}
		if err := c.spawn(); err != nil {
			return nil, err
		}
	case "http":
		if strings.TrimSpace(cfg.URL) == "" {
			return nil, errors.New("http 传输需要 URL")
		}
		c.http = &http.Client{Timeout: 120 * time.Second}
	default:
		return nil, fmt.Errorf("不支持的传输方式: %q", cfg.Transport)
	}

	var hello struct {
		ProtocolVersion string     `json:"protocolVersion"`
		ServerInfo      ServerInfo `json:"serverInfo"`
	}
	err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "Tacivan", "version": "0.1.0"},
	}, &hello)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("MCP 握手失败: %w", err)
	}
	c.info = hello.ServerInfo
	c.info.ProtocolVersion = hello.ProtocolVersion
	// 握手完成后的通知，不等回复
	_ = c.notify("notifications/initialized", nil)
	return c, nil
}

// Info server 身份。
func (c *Client) Info() ServerInfo { return c.info }

// Tools 列出全部工具（自动翻页）。
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	cursor := ""
	for {
		var page struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := c.call(ctx, "tools/list", params, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Tools...)
		if page.NextCursor == "" {
			return out, nil
		}
		cursor = page.NextCursor
	}
}

// Call 调用一个工具。
func (c *Client) Call(ctx context.Context, name string, args map[string]any) (*CallResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	var out CallResult
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Resources 列出资源。server 不支持资源时返回空而不是错。
func (c *Client) Resources(ctx context.Context) ([]Resource, error) {
	var out []Resource
	cursor := ""
	for {
		var page struct {
			Resources  []Resource `json:"resources"`
			NextCursor string     `json:"nextCursor"`
		}
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := c.call(ctx, "resources/list", params, &page); err != nil {
			if isMethodNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		out = append(out, page.Resources...)
		if page.NextCursor == "" {
			return out, nil
		}
		cursor = page.NextCursor
	}
}

// Read 读取一个资源的内容。
func (c *Client) Read(ctx context.Context, uri string) ([]Content, error) {
	var out struct {
		Contents []Content `json:"contents"`
	}
	if err := c.call(ctx, "resources/read", map[string]any{"uri": uri}, &out); err != nil {
		return nil, err
	}
	return out.Contents, nil
}

// Close 关闭连接。
func (c *Client) Close() error {
	if c.cmd != nil && !c.dead.Load() {
		c.mu.Lock()
		_ = c.stdin.Close()
		cmd := c.cmd
		c.mu.Unlock()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
		}
	}
	return nil
}

// --- 传输 ---

func (c *Client) spawn() error {
	argv := c.cfg.Command
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), c.cfg.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 MCP server 失败（%s）: %w", strings.Join(argv, " "), err)
	}
	c.cmd, c.stdin = cmd, stdin
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var m rpcMsg
			if json.Unmarshal(line, &m) != nil || m.ID == nil || m.Method != "" {
				continue // 通知、或 server 发来的请求：这个客户端不处理
			}
			c.mu.Lock()
			ch := c.pending[*m.ID]
			delete(c.pending, *m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}()
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			c.logf("mcp[%s] %s", argv[0], sc.Text())
		}
	}()
	go func() {
		_ = cmd.Wait()
		c.dead.Store(true)
		c.mu.Lock()
		for id, ch := range c.pending {
			ch <- rpcMsg{Error: &rpcErr{Code: -1, Message: "MCP server 进程已退出"}}
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()
	return nil
}

func (c *Client) notify(method string, params any) error {
	m := rpcMsg{JSONRPC: "2.0", Method: method, Params: params}
	if c.http != nil {
		_, err := c.httpPost(context.Background(), m)
		return err
	}
	b, _ := json.Marshal(m)
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.stdin.Write(append(b, '\n'))
	return err
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	id := c.seq.Add(1)
	m := rpcMsg{JSONRPC: "2.0", ID: &id, Method: method, Params: params}
	var resp rpcMsg
	if c.http != nil {
		r, err := c.httpPost(ctx, m)
		if err != nil {
			return err
		}
		resp = r
	} else {
		if c.dead.Load() {
			return errors.New("MCP server 进程已退出")
		}
		ch := make(chan rpcMsg, 1)
		c.mu.Lock()
		c.pending[id] = ch
		b, _ := json.Marshal(m)
		_, err := c.stdin.Write(append(b, '\n'))
		c.mu.Unlock()
		if err != nil {
			return err
		}
		select {
		case resp = <-ch:
		case <-ctx.Done():
			c.mu.Lock()
			delete(c.pending, id)
			c.mu.Unlock()
			return ctx.Err()
		}
	}
	if resp.Error != nil {
		return &Error{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Error server 返回的 JSON-RPC 错误。
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string { return e.Message }

func isMethodNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == -32601
}

// httpPost Streamable HTTP：一次 POST，响应是 JSON 或 SSE。
func (c *Client) httpPost(ctx context.Context, m rpcMsg) (rpcMsg, error) {
	body, _ := json.Marshal(m)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return rpcMsg{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return rpcMsg{}, err
	}
	defer res.Body.Close()
	if sid := res.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}
	if m.ID == nil {
		return rpcMsg{}, nil // 通知：202/204，没有内容
	}
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return rpcMsg{}, fmt.Errorf("MCP server 返回 HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	ct := res.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		// 取第一条带我们这个 id 的 message 事件
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var r rpcMsg
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &r) == nil && r.ID != nil && *r.ID == *m.ID {
				return r, nil
			}
		}
		return rpcMsg{}, errors.New("SSE 流里没有等到响应")
	}
	var r rpcMsg
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return rpcMsg{}, fmt.Errorf("解析 MCP 响应失败: %w", err)
	}
	return r, nil
}

// TextOf 把内容块里的文本拼起来，给不认识结构的地方兜底展示。
func TextOf(cs []Content) string {
	var b strings.Builder
	for _, c := range cs {
		if c.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

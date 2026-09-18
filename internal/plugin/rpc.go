package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 协议：每行一个 JSON-RPC 2.0 消息，双向。
//
// 主程序 → 插件（插件按需实现，没实现就返回 -32601，主程序会降级）：
//
//	initialize   {config}                  -> {name, version}
//	serverInfo   {}                        -> {version, extra}
//	databases    {}                        -> [{name, status, detail, color}]
//	objects      {database, kinds}         -> [{name, kind, comment, status, detail, color}]
//	tableDefinition {database, name}       -> {columns:[{name,type,nullable,comment,primaryKey}], ddl}
//	query        {database, sql, limit}    -> {columns:[{name,type}], rows:[[...]]}
//	actions      {scope, node}             -> [{id, label, danger, fields:[…]}]   插件自定义的右键动作
//	action       {id, node, params}        -> {message, level, refresh}
//	shutdown     {}                        -> null
//
// 插件 → 主程序（方法名以 host. 开头，主程序注册 HostHandler 处理）：
//
//	host.connections                       -> [{id, name, engine}]
//	host.favorites   {connId}              -> [{database, name, kind}]
//	host.databases   {connId}              -> [{name}]
//	host.objects     {connId, database}    -> [{name, kind}]
//	host.tableDefinition {connId, database, name} -> {columns:[…], ddl}
//
// 有了反向调用，"从另一条连接读收藏和结构"这类定制逻辑就能整个留在插件里，
// 主程序只提供通用能力，不知道任何业务。

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// ErrMethodNotFound 插件没实现该方法。调用方据此降级，而不是当成故障。
var ErrMethodNotFound = errors.New("插件未实现该方法")

// HostHandler 处理插件发来的 host.* 请求。返回值会被序列化成 result。
type HostHandler func(method string, params json.RawMessage) (any, error)

// Process 一个正在运行的插件进程。
type Process struct {
	manifest *Manifest
	config   map[string]string
	logf     func(string, ...any)
	host     HostHandler

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pending map[int64]chan message
	seq     atomic.Int64
	dead    atomic.Bool
}

// Start 启动插件进程并完成 initialize 握手。host 可为 nil（插件的反向调用会得到错误）。
func Start(ctx context.Context, m *Manifest, config map[string]string, logf func(string, ...any), host HostHandler) (*Process, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	p := &Process{manifest: m, config: config, logf: logf, host: host, pending: map[int64]chan message{}}
	if err := p.spawn(); err != nil {
		return nil, err
	}
	var hello struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := p.Call(ctx, "initialize", map[string]any{"config": config}, &hello); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("插件 %s 初始化失败: %w", m.ID, err)
	}
	return p, nil
}

func (p *Process) spawn() error {
	argv := p.manifest.Command()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = p.manifest.Dir
	cmd.Env = append(os.Environ(), "TACIVAN_PLUGIN=1")
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
		return fmt.Errorf("启动插件 %s 失败（%s）: %w", p.manifest.ID, strings.Join(argv, " "), err)
	}
	p.cmd, p.stdin = cmd, stdin
	p.dead.Store(false)

	go p.readLoop(stdout)
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			p.logf("plugin[%s] %s", p.manifest.ID, sc.Text())
		}
	}()
	go func() {
		_ = cmd.Wait()
		p.dead.Store(true)
		// 进程没了，挂着的调用全部叫醒，别让它们等到超时。
		p.mu.Lock()
		for id, ch := range p.pending {
			ch <- message{Error: &rpcError{Code: -1, Message: "插件进程已退出"}}
			delete(p.pending, id)
		}
		p.mu.Unlock()
	}()
	return nil
}

func (p *Process) readLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	// 一页结果可能很大，行缓冲给到 64MB。
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			p.logf("plugin[%s] 非法消息: %.200s", p.manifest.ID, line)
			continue
		}
		// 带 method 的是插件发来的请求（反向调用），否则是对我们请求的响应
		if msg.Method != "" {
			go p.serveHostRequest(msg)
			continue
		}
		if msg.ID == nil {
			continue
		}
		p.mu.Lock()
		ch := p.pending[*msg.ID]
		delete(p.pending, *msg.ID)
		p.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

// serveHostRequest 处理插件的 host.* 请求并把结果写回去。
func (p *Process) serveHostRequest(req message) {
	var reply message
	reply.JSONRPC = "2.0"
	reply.ID = req.ID
	switch {
	case p.host == nil || !strings.HasPrefix(req.Method, "host."):
		reply.Error = &rpcError{Code: -32601, Message: "主程序不提供该方法: " + req.Method}
	default:
		res, err := p.host(req.Method, req.Params)
		if err != nil {
			reply.Error = &rpcError{Code: -32000, Message: err.Error()}
		} else if b, err := json.Marshal(res); err != nil {
			reply.Error = &rpcError{Code: -32000, Message: err.Error()}
		} else {
			reply.Result = b
		}
	}
	p.write(reply)
}

func (p *Process) write(m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

// Call 发一个请求并等待结果；插件进程死了会自动拉起一次再试。
func (p *Process) Call(ctx context.Context, method string, params any, out any) error {
	if p.dead.Load() {
		if err := p.respawn(ctx); err != nil {
			return err
		}
	}
	id := p.seq.Add(1)
	ch := make(chan message, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()

	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	if err := p.write(message{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return fmt.Errorf("向插件 %s 发送请求失败: %w", p.manifest.ID, err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			if resp.Error.Code == -32601 {
				return ErrMethodNotFound
			}
			return fmt.Errorf("插件 %s: %s", p.manifest.ID, resp.Error.Message)
		}
		if out != nil && len(resp.Result) > 0 && string(resp.Result) != "null" {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("插件 %s 的 %s 返回了无法解析的结果: %w", p.manifest.ID, method, err)
			}
		}
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return ctx.Err()
	}
}

func (p *Process) respawn(ctx context.Context) error {
	p.logf("plugin[%s] 进程已退出，重新拉起", p.manifest.ID)
	if err := p.spawn(); err != nil {
		return err
	}
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return p.Call(initCtx, "initialize", map[string]any{"config": p.config}, nil)
}

// Close 通知插件退出并回收进程。
func (p *Process) Close() error {
	if p.dead.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.Call(ctx, "shutdown", nil, nil)
	p.mu.Lock()
	_ = p.stdin.Close()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
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

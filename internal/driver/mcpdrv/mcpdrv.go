// Package mcpdrv 把一个 MCP server 当成一种"连接"。
//
// 它不是数据库：没有库表，只有工具和资源。树上按「工具 / 资源」两组展示，
// 工具用表单填参数手动调用，结果按内容展示（表格形状的进网格，其余当文本）。
// 不接大模型——每一次调用都是人按下去的。
package mcpdrv

import (
	"context"
	"fmt"
	"strings"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
	"tacivan/internal/mcp"
)

func init() { dbx.Register(dialer{}) }

type dialer struct{}

func (dialer) Engine() dbx.Engine { return dbx.EngineMCP }

func (dialer) Open(ctx context.Context, cfg dbx.ConnectionConfig) (dbx.Conn, error) {
	mc := ConfigFrom(cfg)
	cli, err := mcp.Dial(ctx, mc, func(f string, a ...any) { logx.Info(fmt.Sprintf(f, a...)) })
	if err != nil {
		return nil, err
	}
	return &Conn{cli: cli}, nil
}

// ConfigFrom 从连接配置的 Params 里取 MCP 参数。
//
//	transport: stdio | http
//	command:   stdio 的启动命令，一行，按空白切分（含空格的路径用引号）
//	env:       每行一个 KEY=VALUE
//	url:       http 的地址
//	headers:   每行一个 Key: Value
func ConfigFrom(cfg dbx.ConnectionConfig) mcp.Config {
	p := cfg.Params
	mc := mcp.Config{Transport: strings.TrimSpace(p["transport"]), URL: strings.TrimSpace(p["url"]), Headers: map[string]string{}}
	if mc.Transport == "" {
		if mc.URL != "" {
			mc.Transport = "http"
		} else {
			mc.Transport = "stdio"
		}
	}
	mc.Command = splitCommand(p["command"])
	for _, line := range strings.Split(p["env"], "\n") {
		if line = strings.TrimSpace(line); line != "" && strings.Contains(line, "=") {
			mc.Env = append(mc.Env, line)
		}
	}
	for _, line := range strings.Split(p["headers"], "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) != "" {
			mc.Headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return mc
}

// splitCommand 按空白切 argv，支持单双引号包住含空格的参数。
func splitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	has := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				cur.WriteByte(ch)
			}
		case ch == '"' || ch == '\'':
			quote = ch
			has = true
		case ch == ' ' || ch == '\t' || ch == '\n':
			if has || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteByte(ch)
			has = true
		}
	}
	if has || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// Conn 一个已连接的 MCP server。
type Conn struct{ cli *mcp.Client }

func (c *Conn) Engine() dbx.Engine             { return dbx.EngineMCP }
func (c *Conn) Capability() dbx.Capability     { return dbx.Capability{} }
func (c *Conn) Close() error                   { return c.cli.Close() }
func (c *Conn) Ping(ctx context.Context) error { _, err := c.cli.Tools(ctx); return err }

func (c *Conn) ServerInfo(context.Context) (dbx.ServerInfo, error) {
	i := c.cli.Info()
	return dbx.ServerInfo{Engine: dbx.EngineMCP, Version: strings.TrimSpace(i.Name + " " + i.Version),
		Extra: map[string]string{"protocolVersion": i.ProtocolVersion}}, nil
}

// Client 暴露底层客户端给上层用。
func (c *Conn) Client() *mcp.Client { return c.cli }

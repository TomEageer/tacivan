package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"tacivan/internal/dbx"
	"tacivan/internal/driver/plugindrv"
	"tacivan/internal/logx"
	"tacivan/internal/plugin"
)

// loadPlugins 发现并注册插件。在 New() 里调用一次。
//
// 只认配置目录下的 plugins/。之前还按工作目录找仓库里的 plugins/，
// 结果从程序坞启动（工作目录是 /）和从终端启动看到的插件不一样，排查半天。
// 开发期把仓库里的插件目录导入即可；TACIVAN_PLUGINS 可以追加一个目录。
func (a *App) loadPlugins() {
	plugindrv.SetHost(a.hostHandler, func(id string) string {
		dir := filepath.Join(a.store.Dir(), "plugin-data", id)
		_ = os.MkdirAll(dir, 0o700)
		return dir
	})
	dirs := []string{filepath.Join(a.store.Dir(), "plugins")}
	if extra := os.Getenv("TACIVAN_PLUGINS"); extra != "" {
		dirs = append(dirs, extra)
	}
	list, errs := plugin.Discover(dirs...)
	for _, err := range errs {
		logx.Warn("插件加载失败", "err", err.Error())
	}
	for _, m := range list {
		plugindrv.Register(m)
		a.plugins = append(a.plugins, m)
		logx.Info(fmt.Sprintf("插件已注册: %s (%s) engine=%s", m.Name, m.Version, m.Engine()))
	}
}

// hostHandler 处理插件的反向调用（host.*）。
//
// 只暴露"读"能力：列连接、读收藏、读库表结构。插件永远拿不到别的连接的写入口，
// 也拿不到密码——它只能借主程序已经打开的会话看结构。
func (a *App) hostHandler(method string, raw json.RawMessage) (any, error) {
	var p struct {
		ConnID   string `json:"connId"`
		Database string `json:"database"`
		Name     string `json:"name"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	switch method {
	case "host.connections":
		type item struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Engine string `json:"engine"`
			Open   bool   `json:"open"`
		}
		var out []item
		for _, c := range a.store.Connections() {
			if strings.HasPrefix(string(c.Engine), "plugin:") {
				continue // 插件不能套插件
			}
			_, err := a.lookup(c.ID)
			out = append(out, item{ID: c.ID, Name: c.Name, Engine: string(c.Engine), Open: err == nil})
		}
		return out, nil
	case "host.favorites":
		type item struct {
			Database string `json:"database"`
			Schema   string `json:"schema"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		}
		var out []item
		for _, f := range a.store.Favorites() {
			if f.ConnectionID == p.ConnID {
				out = append(out, item{Database: f.Database, Schema: f.Schema, Name: f.Name, Kind: f.Kind})
			}
		}
		return out, nil
	case "host.databases":
		sc, err := a.sqlSessionOrOpen(p.ConnID)
		if err != nil {
			return nil, err
		}
		return sc.Databases(ctx)
	case "host.objects":
		sc, err := a.sqlSessionOrOpen(p.ConnID)
		if err != nil {
			return nil, err
		}
		return sc.Objects(ctx, p.Database, "", []dbx.ObjectKind{dbx.KindTable, dbx.KindView})
	case "host.tableDefinition":
		sc, err := a.sqlSessionOrOpen(p.ConnID)
		if err != nil {
			return nil, err
		}
		return sc.TableDefinition(ctx, dbx.ObjectRef{Database: p.Database, Name: p.Name, Kind: dbx.KindTable})
	}
	return nil, fmt.Errorf("主程序不提供该方法: %s", method)
}

// sqlSessionOrOpen 取一条关系型连接；没打开就替用户打开——
// 插件指定了这条连接当结构来源，就是要用它。
func (a *App) sqlSessionOrOpen(connID string) (dbx.SQLConn, error) {
	if connID == "" {
		return nil, fmt.Errorf("没有指定连接")
	}
	if strings.HasPrefix(connID, "plugin:") {
		return nil, fmt.Errorf("插件不能借用另一个插件连接")
	}
	if _, sc, err := a.sqlSession(connID); err == nil {
		return sc, nil
	}
	if _, err := a.OpenConnection(connID); err != nil {
		return nil, err
	}
	_, sc, err := a.sqlSession(connID)
	return sc, err
}

// --- 插件自定义动作：树上右键 → 问插件有哪些 → 弹表单 → 执行 ---

// PluginNode 前端传来的节点定位。
type PluginNode = plugindrv.Node

// PluginAction 插件声明的动作。
type PluginAction = plugindrv.Action

// PluginActionResult 动作结果。
type PluginActionResult = plugindrv.ActionResult

func (a *App) pluginConn(connID string) (*plugindrv.Conn, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return nil, err
	}
	pc, ok := s.conn.(*plugindrv.Conn)
	if !ok {
		return nil, fmt.Errorf("不是插件连接")
	}
	return pc, nil
}

// PluginNodeActions 某个节点上插件提供的动作；不是插件连接则为空。
func (a *App) PluginNodeActions(connID string, node PluginNode) ([]PluginAction, error) {
	pc, err := a.pluginConn(connID)
	if err != nil {
		return nil, nil
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()
	return pc.Actions(ctx, node)
}

// RunPluginAction 执行插件动作。
func (a *App) RunPluginAction(connID, actionID string, node PluginNode, params map[string]string) (PluginActionResult, error) {
	pc, err := a.pluginConn(connID)
	if err != nil {
		return PluginActionResult{}, err
	}
	ctx, cancel := a.requestCtx(120)
	defer cancel()
	start := time.Now()
	res, err := pc.RunAction(ctx, actionID, node, params)
	logx.Op("PluginAction", start, "conn", connID, "action", actionID, "scope", node.Scope, "db", node.Database, "name", node.Name)
	return res, err
}

// --- 插件的安装、删除 ---

// PluginField 连接对话框要渲染的一个插件字段。
type PluginField = plugin.Field

// PluginInfo 设置页里展示的一条插件。
type PluginInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Engine      string `json:"engine"`
	ReadOnly    bool   `json:"readOnly"`
	Dir         string `json:"dir"`
}

// ListPlugins 已加载的插件。
func (a *App) ListPlugins() []PluginInfo {
	out := make([]PluginInfo, 0, len(a.plugins))
	for _, m := range a.plugins {
		out = append(out, PluginInfo{ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description,
			Engine: m.Engine(), ReadOnly: m.ReadOnly, Dir: m.Dir})
	}
	return out
}

// ImportPluginDialog 让用户挑一个插件目录（里面得有 plugin.json）然后导入。
func (a *App) ImportPluginDialog() (*PluginInfo, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("界面尚未就绪")
	}
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: "选择插件目录（含 plugin.json）"})
	if err != nil || dir == "" {
		return nil, err
	}
	return a.ImportPlugin(dir)
}

// ImportPlugin 把一个插件目录复制进配置目录并立即注册，不用重启。
//
// 复制而不是引用原目录：用户从下载目录导入完顺手把源删了，插件不该跟着消失。
// 同 id 已存在则整体替换——升级插件就是再导一次。插件自己的数据（plugin-data/<id>）不动。
func (a *App) ImportPlugin(src string) (*PluginInfo, error) {
	src = strings.TrimSpace(src)
	if strings.HasSuffix(src, "plugin.json") {
		src = filepath.Dir(src)
	}
	m, err := plugin.Load(filepath.Join(src, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("不是有效的插件目录: %w", err)
	}
	dst := filepath.Join(a.store.Dir(), "plugins", m.ID)
	if same, _ := filepath.Abs(src); same != dst {
		if err := os.RemoveAll(dst); err != nil {
			return nil, err
		}
		if err := copyDir(src, dst); err != nil {
			return nil, fmt.Errorf("复制插件失败: %w", err)
		}
	}
	m, err = plugin.Load(filepath.Join(dst, "plugin.json"))
	if err != nil {
		return nil, err
	}
	a.registerPlugin(m)
	logx.Info(fmt.Sprintf("插件已导入: %s (%s) engine=%s", m.Name, m.Version, m.Engine()))
	info := a.ListPlugins()
	for i := range info {
		if info[i].ID == m.ID {
			return &info[i], nil
		}
	}
	return nil, nil
}

// registerPlugin 注册或替换同 id 的清单。
func (a *App) registerPlugin(m *plugin.Manifest) {
	plugindrv.Register(m)
	for i, old := range a.plugins {
		if old.ID == m.ID {
			a.plugins[i] = m
			return
		}
	}
	a.plugins = append(a.plugins, m)
}

// RemovePlugin 删除插件目录并从引擎列表里摘掉。
// 已经建好的连接配置保留，只是打不开了——删插件不该顺手删用户的连接。
// 插件自己的数据目录也保留，重新装回来东西还在。
func (a *App) RemovePlugin(id string) error {
	dir := filepath.Join(a.store.Dir(), "plugins", id)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	kept := a.plugins[:0]
	for _, m := range a.plugins {
		if m.ID != id {
			kept = append(kept, m)
		}
	}
	a.plugins = kept
	return nil
}

// RevealPlugin 在访达里打开插件目录。
func (a *App) RevealPlugin(id string) error {
	for _, m := range a.plugins {
		if m.ID == id {
			return exec.Command("open", m.Dir).Run()
		}
	}
	return fmt.Errorf("插件不存在")
}

// copyDir 递归复制目录；软链按其指向的内容复制。
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Name() == ".DS_Store" {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm()|0o600)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

// Package plugin 定义外部插件的清单格式与进程协议。
//
// 插件是一个独立进程，和主程序之间用 stdin/stdout 上按行分隔的 JSON-RPC 2.0 通信——
// 和 LSP、MCP 是同一种做法。不用 Go 的 plugin 包：它要求插件用完全相同的工具链
// 编译，在 macOS 上还常年不稳定；而进程协议让插件可以用任何语言写，
// 崩了也只是重启一个子进程，拖不死主程序。
package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Field 插件声明的一个连接配置项，连接对话框按它渲染表单。
type Field struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Type: text | password | number | bool | select | textarea
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Default     string   `json:"default,omitempty"`
	Hint        string   `json:"hint,omitempty"`
	Options     []string `json:"options,omitempty"`
	// Secret 为真的值进钥匙串而不是配置文件。
	Secret bool `json:"secret"`
}

// Rules 主程序在把 SQL 交给插件之前强制执行的查询规则。
//
// 插件自己也可以再校验一遍——生产库这种地方，门禁多一道不嫌多。
type Rules struct {
	// SelectOnly 只放行以 SELECT 开头的语句。
	SelectOnly bool `json:"selectOnly"`
	// SingleStatement 拒绝分号堆叠的多语句。
	SingleStatement bool `json:"singleStatement"`
	// ForceLimit 大于 0 时，没写 LIMIT 的查询自动补上；写了但更大的压到这个值。
	ForceLimit int `json:"forceLimit"`
	// DenyPatterns 命中即拒绝的子串（大小写不敏感），例如 information_schema。
	DenyPatterns []string `json:"denyPatterns,omitempty"`
}

// Manifest 插件清单（plugin.json）。
type Manifest struct {
	// ID 全局唯一，作为引擎名的一部分：engine = "plugin:" + ID。
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	// Exec 启动命令，argv 形式；相对路径相对于插件目录解析。
	Exec []string `json:"exec"`
	// Dialect 插件背后是哪种 SQL 方言，决定标识符引用与筛选器语法：mysql | postgres | sqlite。
	Dialect string `json:"dialect"`
	// ReadOnly 为真时连接强制只读：写路径根本不存在，不是拦一下。
	ReadOnly bool    `json:"readOnly"`
	Fields   []Field `json:"fields"`
	Rules    Rules   `json:"rules"`

	// Dir 清单所在目录，加载时填入。
	Dir string `json:"-"`
}

// Engine 返回该插件对应的引擎标识。
func (m *Manifest) Engine() string { return "plugin:" + m.ID }

// Validate 检查清单是否完整。
func (m *Manifest) Validate() error {
	if m.ID == "" || strings.ContainsAny(m.ID, " /:\\") {
		return fmt.Errorf("插件 id 非法: %q", m.ID)
	}
	if m.Name == "" {
		return fmt.Errorf("插件 %s 缺少 name", m.ID)
	}
	if len(m.Exec) == 0 {
		return fmt.Errorf("插件 %s 缺少 exec", m.ID)
	}
	switch m.Dialect {
	case "mysql", "postgres", "sqlite":
	case "":
		m.Dialect = "mysql"
	default:
		return fmt.Errorf("插件 %s 的 dialect 不支持: %s", m.ID, m.Dialect)
	}
	seen := map[string]bool{}
	for _, f := range m.Fields {
		if f.Key == "" || seen[f.Key] {
			return fmt.Errorf("插件 %s 的字段 key 重复或为空: %q", m.ID, f.Key)
		}
		seen[f.Key] = true
	}
	return nil
}

// Command 返回解析好的启动命令：第一个元素若是相对路径，按插件目录补全。
func (m *Manifest) Command() []string {
	argv := append([]string(nil), m.Exec...)
	for i, a := range argv {
		// 只补全看起来像文件的参数（含路径分隔符或以 . 开头），
		// "node"、"python3" 这类要留给 PATH 去找。
		if i == 0 && !strings.ContainsAny(a, "/.") {
			continue
		}
		if !filepath.IsAbs(a) && (strings.ContainsAny(a, "/") || strings.HasPrefix(a, ".")) {
			argv[i] = filepath.Join(m.Dir, a)
		}
	}
	return argv
}

// Discover 从若干目录里找插件：每个子目录一个插件，里面必须有 plugin.json。
//
// 坏掉的清单跳过并记进 errs，不让一个写错的插件挡住其它插件加载。
func Discover(dirs ...string) (list []*Manifest, errs []error) {
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			pdir := filepath.Join(dir, e.Name())
			m, err := Load(filepath.Join(pdir, "plugin.json"))
			if err != nil {
				if !os.IsNotExist(err) {
					errs = append(errs, fmt.Errorf("%s: %w", pdir, err))
				}
				continue
			}
			if seen[m.ID] {
				errs = append(errs, fmt.Errorf("%s: 插件 id %q 重复，已忽略", pdir, m.ID))
				continue
			}
			seen[m.ID] = true
			list = append(list, m)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, errs
}

// Load 读取并校验一份清单。
func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("解析 plugin.json 失败: %w", err)
	}
	m.Dir = filepath.Dir(path)
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

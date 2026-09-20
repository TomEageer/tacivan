// Package envpath 解决"从程序坞启动的应用找不到 node"的问题。
//
// Finder / 程序坞启动的 GUI 应用只继承 launchd 的 PATH（/usr/bin:/bin:/usr/sbin:/sbin），
// Homebrew、nvm、volta 装的 node、python、uvx 全都不在里面；同一个程序从终端启动却一切正常。
// 这里在首次需要时问一次登录 shell 的 PATH，并补上几个常见位置，供插件与 MCP 子进程解析可执行文件。
package envpath

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	once   sync.Once
	cached string
)

// Path 返回增强后的 PATH：当前进程 PATH + 登录 shell 的 PATH + 常见安装目录，去重。
func Path() string {
	once.Do(func() { cached = build() })
	return cached
}

// Look 按增强 PATH 查找可执行文件；带路径分隔符的名字原样交给 exec.LookPath。
func Look(name string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		return exec.LookPath(name)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range filepath.SplitList(Path()) {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// Env 返回把 PATH 替换成增强版之后的环境变量列表，给子进程用。
func Env(extra ...string) []string {
	out := make([]string, 0, len(os.Environ())+len(extra)+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	out = append(out, "PATH="+Path())
	return append(out, extra...)
}

func build() string {
	var dirs []string
	seen := map[string]bool{}
	add := func(list string) {
		for _, d := range filepath.SplitList(list) {
			if d == "" || seen[d] {
				continue
			}
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	add(os.Getenv("PATH"))
	add(loginShellPath())
	home, _ := os.UserHomeDir()
	if runtime.GOOS != "windows" {
		add("/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/local/sbin")
		if home != "" {
			add(filepath.Join(home, ".volta", "bin") + string(os.PathListSeparator) +
				filepath.Join(home, ".local", "bin") + string(os.PathListSeparator) +
				filepath.Join(home, ".cargo", "bin") + string(os.PathListSeparator) +
				filepath.Join(home, "go", "bin"))
			// nvm：每个版本一个目录，取存在的那些（最新版靠字典序不可靠，但至少能找到 node）
			if matches, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); len(matches) > 0 {
				for i := len(matches) - 1; i >= 0; i-- {
					add(matches[i])
				}
			}
		}
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

// loginShellPath 用用户的登录 shell 跑一次 `echo $PATH`。失败或超时就返回空，不影响启动。
func loginShellPath() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// -l 读登录配置（.zprofile / .profile），-i 读交互配置（.zshrc）——nvm 之类多半写在后者
	out, err := exec.CommandContext(ctx, shell, "-lic", `printf '%s' "$PATH"`).Output()
	if err != nil {
		out, err = exec.CommandContext(ctx, shell, "-lc", `printf '%s' "$PATH"`).Output()
		if err != nil {
			return ""
		}
	}
	return strings.TrimSpace(string(out))
}

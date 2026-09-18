package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Draft 一个未保存的查询编辑器草稿。
//
// 写在一个个独立文件里而不是汇总成一份 JSON：草稿是高频写入的东西
// （每次停止输入就落一次盘），一整份重写既浪费又把所有草稿的安全性
// 绑在同一次写入上——那正是"崩溃时别丢东西"这个功能最不该有的性质。
type Draft struct {
	// ID 与标签页 ID 相同，用来做幂等覆盖。
	ID string `json:"id"`
	// Title 标签页标题。
	Title string `json:"title"`
	// ConnID / ConnName / Database 恢复时用来把标签页接回原来的连接。
	ConnID   string `json:"connId"`
	ConnName string `json:"connName"`
	Database string `json:"database"`
	// SQL 编辑器里的全部内容。
	SQL string `json:"sql"`
	// UpdatedAt 最后一次落盘时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Store) draftDir() string { return s.path("drafts") }

// safeDraftName 把 ID 转成一个安全的文件名。
// ID 由前端生成，不能直接拼进路径——那等于把目录穿越交给了调用方。
func safeDraftName(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("草稿 ID 为空")
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := b.String()
	if name == "" || strings.Trim(name, "_") == "" {
		return "", fmt.Errorf("草稿 ID 非法: %q", id)
	}
	return name + ".json", nil
}

// SaveDraft 写入（或覆盖）一份草稿。
func (s *Store) SaveDraft(d Draft) error {
	name, err := safeDraftName(d.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.draftDir(), 0o700); err != nil {
		return fmt.Errorf("创建草稿目录失败: %w", err)
	}
	d.UpdatedAt = time.Now()
	return writeJSON(filepath.Join(s.draftDir(), name), &d)
}

// DeleteDraft 删除一份草稿。标签页被正常关掉时调用。
func (s *Store) DeleteDraft(id string) error {
	name, err := safeDraftName(id)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(s.draftDir(), name))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除草稿失败: %w", err)
	}
	return nil
}

// ListDrafts 读出全部草稿，按最后修改时间从新到旧。
//
// 单个文件坏了就跳过它，不让一份损坏的草稿挡住其余草稿的恢复。
func (s *Store) ListDrafts() []Draft {
	entries, err := os.ReadDir(s.draftDir())
	if err != nil {
		return nil
	}
	out := make([]Draft, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var d Draft
		if err := readJSON(filepath.Join(s.draftDir(), e.Name()), &d); err != nil {
			continue
		}
		if d.ID == "" || strings.TrimSpace(d.SQL) == "" {
			continue
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

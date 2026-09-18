package store

import (
	"fmt"
	"sort"
	"strings"

	"tacivan/internal/dbx"
)

// Group 连接分组。分组只是个带顺序的名字：连接通过 ConnectionConfig.Group 指向它。
//
// 不做多级嵌套：有的客户端支持，但实际用起来两层就够（生产 / 测试 / 本地），
// 再深就是在树里找东西比连库还慢。
type Group struct {
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

// Groups 返回全部分组，按顺序。连接上出现过但没登记的分组名也会补出来，
// 这样手改 connections.json 也不会让连接凭空消失。
func (s *Store) Groups() []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]bool{}
	out := make([]Group, 0, len(s.groups))
	for _, g := range s.groups {
		if g.Name == "" || seen[g.Name] {
			continue
		}
		seen[g.Name] = true
		out = append(out, g)
	}
	for _, c := range s.conns {
		if c.Group != "" && !seen[c.Group] {
			seen[c.Group] = true
			out = append(out, Group{Name: c.Group, SortOrder: len(out)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })
	return out
}

// SaveGroup 新建一个分组；已存在则原样返回。
func (s *Store) SaveGroup(name string) ([]Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("分组名不能为空")
	}
	s.mu.Lock()
	for _, g := range s.groups {
		if g.Name == name {
			s.mu.Unlock()
			return s.Groups(), nil
		}
	}
	s.groups = append(s.groups, Group{Name: name, SortOrder: len(s.groups)})
	groups := append([]Group(nil), s.groups...)
	s.mu.Unlock()
	if err := writeJSON(s.path("groups.json"), groups); err != nil {
		return nil, err
	}
	return s.Groups(), nil
}

// RenameGroup 改名，连接上的引用一并改。
func (s *Store) RenameGroup(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("分组名不能为空")
	}
	s.mu.Lock()
	for i := range s.groups {
		if s.groups[i].Name == oldName {
			s.groups[i].Name = newName
		}
	}
	for i := range s.conns {
		if s.conns[i].Group == oldName {
			s.conns[i].Group = newName
		}
	}
	groups := append([]Group(nil), s.groups...)
	conns := append([]dbx.ConnectionConfig(nil), s.conns...)
	s.mu.Unlock()
	if err := writeJSON(s.path("groups.json"), groups); err != nil {
		return err
	}
	return writeJSON(s.path("connections.json"), conns)
}

// DeleteGroup 删除分组；里面的连接回到根，不会跟着被删。
func (s *Store) DeleteGroup(name string) error {
	s.mu.Lock()
	kept := s.groups[:0]
	for _, g := range s.groups {
		if g.Name != name {
			kept = append(kept, g)
		}
	}
	s.groups = kept
	for i := range s.conns {
		if s.conns[i].Group == name {
			s.conns[i].Group = ""
		}
	}
	groups := append([]Group(nil), s.groups...)
	conns := append([]dbx.ConnectionConfig(nil), s.conns...)
	s.mu.Unlock()
	if err := writeJSON(s.path("groups.json"), groups); err != nil {
		return err
	}
	return writeJSON(s.path("connections.json"), conns)
}

// ReorderGroups 按给定名字顺序重排分组。
func (s *Store) ReorderGroups(names []string) error {
	s.mu.Lock()
	pos := map[string]int{}
	for i, n := range names {
		pos[n] = i
	}
	// 只在连接上出现、还没登记过的分组，此刻登记下来——
	// 否则它们永远排在已登记分组后面，用户怎么拖都拖不动。
	known := map[string]bool{}
	for _, g := range s.groups {
		known[g.Name] = true
	}
	for _, n := range names {
		if n != "" && !known[n] {
			s.groups = append(s.groups, Group{Name: n})
			known[n] = true
		}
	}
	for i := range s.groups {
		if p, ok := pos[s.groups[i].Name]; ok {
			s.groups[i].SortOrder = p
		}
	}
	sort.SliceStable(s.groups, func(i, j int) bool { return s.groups[i].SortOrder < s.groups[j].SortOrder })
	groups := append([]Group(nil), s.groups...)
	s.mu.Unlock()
	return writeJSON(s.path("groups.json"), groups)
}

// MoveConnection 把连接挪进某个分组（空串 = 根）。
func (s *Store) MoveConnection(id, group string) error {
	s.mu.Lock()
	found := false
	for i := range s.conns {
		if s.conns[i].ID == id {
			s.conns[i].Group = strings.TrimSpace(group)
			found = true
		}
	}
	conns := append([]dbx.ConnectionConfig(nil), s.conns...)
	s.mu.Unlock()
	if !found {
		return fmt.Errorf("连接不存在")
	}
	return writeJSON(s.path("connections.json"), conns)
}

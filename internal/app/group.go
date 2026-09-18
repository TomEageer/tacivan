package app

import "tacivan/internal/store"

// ListGroups 连接分组列表。
func (a *App) ListGroups() []store.Group { return a.store.Groups() }

// SaveGroup 新建分组。
func (a *App) SaveGroup(name string) ([]store.Group, error) { return a.store.SaveGroup(name) }

// RenameGroup 分组改名，连接上的引用一并改。
func (a *App) RenameGroup(oldName, newName string) error {
	return a.store.RenameGroup(oldName, newName)
}

// DeleteGroup 删除分组，里面的连接回到根。
func (a *App) DeleteGroup(name string) error { return a.store.DeleteGroup(name) }

// ReorderGroups 重排分组。
func (a *App) ReorderGroups(names []string) error { return a.store.ReorderGroups(names) }

// MoveConnection 把连接挪进分组；空串表示回到根。
func (a *App) MoveConnection(id, group string) error { return a.store.MoveConnection(id, group) }

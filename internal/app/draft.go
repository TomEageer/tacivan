package app

import "tacivan/internal/store"

// SaveDraft 保存查询编辑器的草稿。
//
// 由前端在停止输入后调用，是「编辑器自动恢复」的写入端。
// 内容为空时直接当成删除——空标签页没有恢复的价值，
// 留着只会让下次启动多出一个空窗口。
func (a *App) SaveDraft(d store.Draft) error {
	if d.SQL == "" {
		return a.store.DeleteDraft(d.ID)
	}
	return a.store.SaveDraft(d)
}

// DeleteDraft 丢弃草稿。标签页被用户正常关闭时调用。
func (a *App) DeleteDraft(id string) error { return a.store.DeleteDraft(id) }

// ListDrafts 列出上次留下的草稿，启动时用来把编辑器恢复回去。
func (a *App) ListDrafts() []store.Draft { return a.store.ListDrafts() }

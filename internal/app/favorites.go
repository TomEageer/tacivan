package app

import (
	"tacivan/internal/dbx"
	"tacivan/internal/store"
)

// 收藏用来在成百上千个库、上万张表里留住常用的那几个。
// 它是纯本地的界面状态，不往数据库里写任何东西。

// ListFavorites 返回全部收藏。
func (a *App) ListFavorites() []store.Favorite {
	return a.store.Favorites()
}

// ToggleFavorite 切换某个对象的收藏状态，返回切换后是否已收藏。
func (a *App) ToggleFavorite(f store.Favorite) (bool, error) {
	return a.store.ToggleFavorite(f)
}

// DeleteConnection 删除连接时，顺带清掉它名下的收藏，
// 免得留下一批指向不存在连接的孤儿条目。
func (a *App) deleteConnectionFavorites(connID string) {
	_ = a.store.RemoveFavoritesByConnection(connID)
}

var _ = dbx.EngineMySQL

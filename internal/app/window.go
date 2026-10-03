package app

import (
	"tacivan/internal/logx"
	"tacivan/internal/store"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 窗口尺寸：首次按屏幕的八成开，之后记住用户上次留下的位置和大小。
// 以前写死 1440×900，在 14 寸笔记本（逻辑 1512×982）上等于铺满整个桌面。

const (
	defaultWinW, defaultWinH = 1440, 900
	minWinW, minWinH         = 1024, 640
)

// InitialWindowSize 给 wails.Run 的初始尺寸：有记忆用记忆，否则用默认值（启动后再按屏幕收缩）。
func (a *App) InitialWindowSize() (int, int) {
	f := a.store.WindowFrame()
	if f.W >= minWinW && f.H >= minWinH {
		return f.W, f.H
	}
	return defaultWinW, defaultWinH
}

// restoreWindow 在 Startup 里调用：恢复位置，或按当前屏幕算一个不铺满的首开尺寸。
func (a *App) restoreWindow() {
	f := a.store.WindowFrame()
	sw, sh := a.currentScreenSize()
	logx.Info("窗口恢复", "saved", f, "screenW", sw, "screenH", sh)
	// 记忆的尺寸几乎铺满当前屏幕（比如记忆来自更大的外接屏）：当作没有记忆，按屏幕重新算
	if sw > 0 && !f.Maximised && f.W >= sw*9/10 && f.H >= sh*9/10 {
		f = store.WindowFrame{}
	}
	if f.W >= minWinW && f.H >= minWinH {
		if f.Maximised {
			wr.WindowMaximise(a.ctx)
			return
		}
		// 位置还在屏幕范围内才恢复，外接屏拔掉后别把窗口放到看不见的地方
		if sw == 0 || (f.X >= -f.W/2 && f.Y >= 0 && f.X < sw-100 && f.Y < sh-100) {
			wr.WindowSetPosition(a.ctx, f.X, f.Y)
		} else {
			wr.WindowCenter(a.ctx)
		}
		return
	}
	if sw == 0 {
		return
	}
	w := min(defaultWinW, sw*8/10)
	h := min(defaultWinH, sh*8/10)
	w, h = max(w, minWinW), max(h, minWinH)
	wr.WindowSetSize(a.ctx, w, h)
	wr.WindowCenter(a.ctx)
}

func (a *App) currentScreenSize() (int, int) {
	screens, err := wr.ScreenGetAll(a.ctx)
	if err != nil {
		return 0, 0
	}
	for _, s := range screens {
		if s.IsCurrent {
			return s.Size.Width, s.Size.Height
		}
	}
	for _, s := range screens {
		if s.IsPrimary {
			return s.Size.Width, s.Size.Height
		}
	}
	return 0, 0
}

// saveWindowFrame 退出前记一下窗口在哪、多大。
func (a *App) saveWindowFrame() {
	if a.ctx == nil {
		return
	}
	w, h := wr.WindowGetSize(a.ctx)
	x, y := wr.WindowGetPosition(a.ctx)
	if w < minWinW || h < minWinH {
		return
	}
	f := store.WindowFrame{X: x, Y: y, W: w, H: h, Maximised: wr.WindowIsMaximised(a.ctx)}
	if err := a.store.SaveWindowFrame(f); err != nil {
		logx.Warn("保存窗口位置失败", "err", err)
	}
}

// BeforeClose 由 Wails 在窗口关闭/退出前调用；返回 false 表示放行。
func (a *App) BeforeClose(ctx any) bool {
	a.saveWindowFrame()
	return false
}

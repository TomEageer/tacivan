package store

// WindowFrame 上次退出时的窗口位置与尺寸（逻辑像素）。单独一个文件，不和设置混在一起：
// 它每次退出都会写，而设置只在用户改的时候写。
type WindowFrame struct {
	X, Y, W, H int
	Maximised  bool
}

// WindowFrame 读上次保存的窗口框架；没有则 W/H 为 0。
func (s *Store) WindowFrame() WindowFrame {
	var f WindowFrame
	_ = readJSON(s.path("window.json"), &f)
	return f
}

// SaveWindowFrame 保存窗口框架。
func (s *Store) SaveWindowFrame(f WindowFrame) error {
	return writeJSON(s.path("window.json"), f)
}

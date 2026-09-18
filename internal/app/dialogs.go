package app

import (
	"fmt"
	"os"
	"path/filepath"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// OpenDatabaseFileDialog 弹出系统文件选择框，用于挑选 SQLite 数据库文件。
func (a *App) OpenDatabaseFileDialog() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面尚未就绪")
	}
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择 SQLite 数据库文件",
		Filters: []wruntime.FileFilter{
			{DisplayName: "SQLite 数据库 (*.db;*.sqlite;*.sqlite3;*.db3)", Pattern: "*.db;*.sqlite;*.sqlite3;*.db3"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
}

// CreateDatabaseFileDialog 选择新建 SQLite 数据库文件的保存位置，并创建空文件。
func (a *App) CreateDatabaseFileDialog() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面尚未就绪")
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "新建 SQLite 数据库",
		DefaultFilename: "database.db",
		Filters: []wruntime.FileFilter{
			{DisplayName: "SQLite 数据库 (*.db)", Pattern: "*.db"},
		},
	})
	if err != nil || path == "" {
		return path, err
	}
	// SQLite 会在首次写入时初始化文件头，这里只需保证文件存在且可写。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("创建数据库文件失败: %w", err)
	}
	_ = f.Close()
	return path, nil
}

// SaveFileDialogFor 选择导出文件的保存位置。
func (a *App) SaveFileDialogFor(format, defaultName string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面尚未就绪")
	}
	filters := map[string]wruntime.FileFilter{
		"csv":      {DisplayName: "CSV 文件 (*.csv)", Pattern: "*.csv"},
		"tsv":      {DisplayName: "TSV 文件 (*.tsv)", Pattern: "*.tsv"},
		"json":     {DisplayName: "JSON 文件 (*.json)", Pattern: "*.json"},
		"sql":      {DisplayName: "SQL 脚本 (*.sql)", Pattern: "*.sql"},
		"markdown": {DisplayName: "Markdown (*.md)", Pattern: "*.md"},
	}
	f, ok := filters[format]
	if !ok {
		f = wruntime.FileFilter{DisplayName: "所有文件 (*.*)", Pattern: "*.*"}
	}
	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "导出到文件",
		DefaultFilename: defaultName,
		Filters:         []wruntime.FileFilter{f},
	})
}

// OpenSQLFileDialog 选择要打开的 SQL 脚本文件。
func (a *App) OpenSQLFileDialog() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面尚未就绪")
	}
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "打开 SQL 脚本",
		Filters: []wruntime.FileFilter{
			{DisplayName: "SQL 脚本 (*.sql)", Pattern: "*.sql"},
			{DisplayName: "所有文件 (*.*)", Pattern: "*.*"},
		},
	})
}

// maxScriptBytes 允许在编辑器里打开的脚本大小上限。
//
// 超过这个尺寸的脚本塞进编辑器只会让界面卡死，
// 这类文件应该用命令行导入而不是在图形客户端里编辑。
const maxScriptBytes = 16 << 20

// ReadTextFile 读取文本文件内容（用于打开 SQL 脚本）。
func (a *App) ReadTextFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Size() > maxScriptBytes {
		return "", fmt.Errorf("文件过大（%.1f MB），超过 %d MB 的编辑器上限；"+
			"大脚本请用命令行导入", float64(st.Size())/(1<<20), maxScriptBytes>>20)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WriteTextFile 写入文本文件（用于保存 SQL 脚本）。
func (a *App) WriteTextFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// SaveSQLFileDialog 选择 SQL 脚本的保存位置。
func (a *App) SaveSQLFileDialog(defaultName string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面尚未就绪")
	}
	if defaultName == "" {
		defaultName = "query.sql"
	}
	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "保存 SQL 脚本",
		DefaultFilename: defaultName,
		Filters: []wruntime.FileFilter{
			{DisplayName: "SQL 脚本 (*.sql)", Pattern: "*.sql"},
		},
	})
}

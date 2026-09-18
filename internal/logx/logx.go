// Package logx 提供带耗时统计的诊断日志。
//
// 目的很具体：当用户说「打开一张表要等半天」时，能从日志里直接看出
// 时间花在哪一条语句上，而不是靠猜。因此每条语句都记录耗时、返回行数与参数。
package logx

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	mu      sync.Mutex
	file    *os.File
	logger  atomic.Pointer[slog.Logger]
	logPath string
	// enabled 关闭时所有记录函数直接返回，避免格式化开销。
	enabled atomic.Bool
	// slowMS 超过这个毫秒数的语句额外标记为 slow，便于筛选。
	slowMS atomic.Int64
)

// maxLogBytes 单个日志文件的上限，超过则轮转一次。
const maxLogBytes = 16 << 20

func init() {
	slowMS.Store(300)
	logger.Store(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

// Init 打开日志文件。dir 为空时使用 ~/Library/Logs/Tacivan（或对应平台位置）。
func Init(dir string, on bool) error {
	mu.Lock()
	defer mu.Unlock()

	enabled.Store(on)
	if !on {
		return nil
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, "Library", "Logs", "Tacivan")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	path := filepath.Join(dir, "tacivan.log")

	// 超过上限就把旧文件挪走，保留一份供事后排查。
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	if file != nil {
		_ = file.Close()
	}
	file = f
	logPath = path

	logger.Store(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// 时间精确到毫秒就够，完整 RFC3339 太长影响阅读。
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(time.Now().Format("15:04:05.000"))
			}
			return a
		},
	})))
	return nil
}

// Path 当前日志文件路径。
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return logPath
}

// Enabled 日志是否开启。
func Enabled() bool { return enabled.Load() }

// SetSlowThreshold 设置慢语句阈值（毫秒）。
func SetSlowThreshold(ms int64) { slowMS.Store(ms) }

// Close 关闭日志文件。
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		_ = file.Close()
		file = nil
	}
}

// compact 把 SQL 压成一行并截断，日志里保持可读。
func compact(q string, limit int) string {
	q = strings.Join(strings.Fields(q), " ")
	if limit > 0 && len(q) > limit {
		return q[:limit] + "…"
	}
	return q
}

// SQL 记录一条语句的执行情况。
//
// op 是调用点的标识（如 mysql.fetchColumns），用它能一眼看出
// 是哪个阶段在拖时间。
func SQL(op, database, query string, start time.Time, rows int, err error) {
	if !enabled.Load() {
		return
	}
	d := time.Since(start)
	attrs := []any{
		"op", op,
		"db", database,
		"ms", d.Milliseconds(),
		"sql", compact(query, 400),
	}
	if rows >= 0 {
		attrs = append(attrs, "rows", rows)
	}
	if d.Milliseconds() >= slowMS.Load() {
		attrs = append(attrs, "slow", true)
	}
	if err != nil {
		attrs = append(attrs, "err", err.Error())
		logger.Load().Error("sql", attrs...)
		return
	}
	logger.Load().Info("sql", attrs...)
}

// Op 记录一次高层操作的耗时与阶段分解。
func Op(name string, start time.Time, attrs ...any) {
	if !enabled.Load() {
		return
	}
	d := time.Since(start)
	all := append([]any{"op", name, "ms", d.Milliseconds()}, attrs...)
	if d.Milliseconds() >= slowMS.Load() {
		all = append(all, "slow", true)
	}
	logger.Load().Info("operation", all...)
}

// Info 记录一条普通信息。
func Info(msg string, attrs ...any) {
	if !enabled.Load() {
		return
	}
	logger.Load().Info(msg, attrs...)
}

// Warn 记录一条警告。
func Warn(msg string, attrs ...any) {
	if !enabled.Load() {
		return
	}
	logger.Load().Warn(msg, attrs...)
}

// Error 记录一条错误。
func Error(msg string, attrs ...any) {
	if !enabled.Load() {
		return
	}
	logger.Load().Error(msg, attrs...)
}

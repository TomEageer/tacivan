// Package app 是 Wails 绑定层：把后端能力以方法形式暴露给前端。
//
// 这一层只做编排与参数校验，不写 SQL 方言逻辑（在 dbx/driver），
// 也不写内存策略（在 rs）。
package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
	"tacivan/internal/plugin"
	"tacivan/internal/rs"
	"tacivan/internal/store"

	// 注册全部引擎驱动。
	_ "tacivan/internal/driver/mcpdrv"
	_ "tacivan/internal/driver/mysqldrv"
	_ "tacivan/internal/driver/pgdrv"
	_ "tacivan/internal/driver/redisdrv"
	_ "tacivan/internal/driver/sqlitedrv"
)

// session 一条已打开的连接。
type session struct {
	cfg      dbx.ConnectionConfig
	conn     dbx.Conn
	info     dbx.ServerInfo
	openedAt time.Time

	mu       sync.Mutex
	lastUsed time.Time
	// currentDB 界面上当前选中的库，仅用于展示与默认值。
	currentDB string
}

func (s *session) touch() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

// App Wails 绑定的根对象。
type App struct {
	ctx context.Context
	// plugins 启动时发现的插件清单。
	plugins []*plugin.Manifest
	store   *store.Store
	rs      *rs.Manager

	mu       sync.RWMutex
	sessions map[string]*session

	// startedAt 进程启动时间，供状态栏展示。
	startedAt time.Time
}

// New 创建应用实例。
func New() (*App, error) {
	st, err := store.New()
	if err != nil {
		return nil, err
	}
	settings := st.Settings()

	if err := logx.Init("", settings.DiagnosticLog); err != nil {
		// 日志开不起来不该挡着应用启动。
		fmt.Fprintf(os.Stderr, "诊断日志不可用: %v\n", err)
	}
	logx.SetSlowThreshold(settings.SlowQueryMS)
	logx.Info("应用启动", "version", Version, "platform", runtime.GOOS+"/"+runtime.GOARCH)

	opts := rs.DefaultOptions()
	opts.MaxRows = settings.MaxResultRows
	spillDir := filepath.Join(os.TempDir(), "tacivan-spill")
	mgr := rs.NewManager(int64(settings.MemoryLimitMB)<<20, spillDir, opts)

	a := &App{
		store:     st,
		rs:        mgr,
		sessions:  map[string]*session{},
		startedAt: time.Now(),
	}
	// 插件在这里注册：必须在任何连接打开之前，引擎表才查得到它们。
	a.loadPlugins()
	return a, nil
}

// Startup 由 Wails 在应用启动时调用。
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

// Shutdown 由 Wails 在应用退出时调用，负责关掉所有连接与结果集。
func (a *App) Shutdown(ctx context.Context) {
	logx.Info("应用退出")
	logx.Close()
	a.rs.Shutdown()
	a.mu.Lock()
	sessions := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.sessions = map[string]*session{}
	a.mu.Unlock()
	for _, s := range sessions {
		_ = s.conn.Close()
	}
}

// requestCtx 返回带超时的请求上下文。
//
// 一切数据库调用都带超时：卡死的网络不该让界面无限转圈。
func (a *App) requestCtx(seconds int) (context.Context, context.CancelFunc) {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	if seconds <= 0 {
		seconds = 30
	}
	return context.WithTimeout(base, time.Duration(seconds)*time.Second)
}

// lookup 取已打开的连接会话。
func (a *App) lookup(connID string) (*session, error) {
	a.mu.RLock()
	s, ok := a.sessions[connID]
	a.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("连接未打开，请先在左侧双击连接")
	}
	s.touch()
	return s, nil
}

// sqlSession 取已打开的关系型连接。
func (a *App) sqlSession(connID string) (*session, dbx.SQLConn, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return nil, nil, err
	}
	sc, ok := s.conn.(dbx.SQLConn)
	if !ok {
		return nil, nil, fmt.Errorf("%s 连接不支持 SQL 操作", s.cfg.Engine.DisplayName())
	}
	return s, sc, nil
}

// kvSession 取已打开的键值连接。
func (a *App) kvSession(connID string) (*session, dbx.KVConn, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return nil, nil, err
	}
	kc, ok := s.conn.(dbx.KVConn)
	if !ok {
		return nil, nil, fmt.Errorf("%s 连接不是键值型数据库", s.cfg.Engine.DisplayName())
	}
	return s, kc, nil
}

// scanOptions 按当前设置构造扫描选项。
func (a *App) scanOptions() dbx.ScanOptions {
	st := a.store.Settings()
	return dbx.ScanOptions{
		PreviewLimit:       st.CellPreviewLimit,
		BinaryPreviewLimit: 256,
	}.Normalize()
}

// --- 应用信息 ---

// EngineOption 新建连接对话框里的一个引擎选项。
type EngineOption struct {
	Engine      string `json:"engine"`
	DisplayName string `json:"displayName"`
	DefaultPort int    `json:"defaultPort"`
	// FileBased 为真表示该引擎连的是本地文件而不是主机端口。
	FileBased bool `json:"fileBased"`
	// Plugin 为真表示这是插件提供的引擎，表单按 Fields 渲染。
	Plugin      bool          `json:"plugin"`
	ReadOnly    bool          `json:"readOnly"`
	Description string        `json:"description,omitempty"`
	Fields      []PluginField `json:"fields,omitempty"`
}

// ListEngines 返回受支持的引擎列表。
func (a *App) ListEngines() []EngineOption {
	out := make([]EngineOption, 0, len(dbx.AllEngines())+len(a.plugins))
	for _, e := range dbx.AllEngines() {
		out = append(out, EngineOption{
			Engine:      string(e),
			DisplayName: e.DisplayName(),
			DefaultPort: e.DefaultPort(),
			FileBased:   e == dbx.EngineSQLite,
		})
	}
	// 插件引擎排在内置引擎后面；表单按清单里的字段渲染，不走主机/端口那套。
	for _, m := range a.plugins {
		out = append(out, EngineOption{
			Engine:      m.Engine(),
			DisplayName: m.Name,
			Plugin:      true,
			ReadOnly:    m.ReadOnly,
			Description: m.Description,
			Fields:      m.Fields,
		})
	}
	return out
}

// AppInfo 应用自身的信息，显示在「关于」对话框。
type AppInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
	ConfigDir string `json:"configDir"`
	SpillDir  string `json:"spillDir"`
	KeyringOK bool   `json:"keyringOk"`
	StartedAt string `json:"startedAt"`
	// LogPath 诊断日志文件路径，空表示未启用。
	LogPath string `json:"logPath"`
}

// Version 应用版本号，构建时可通过 ldflags 覆盖。
var Version = "0.1.0"

// GetAppInfo 返回应用信息。
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{
		Name:      "Tacivan",
		Version:   Version,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		ConfigDir: a.store.Dir(),
		SpillDir:  filepath.Join(os.TempDir(), "tacivan-spill"),
		KeyringOK: a.store.KeyringAvailable(),
		StartedAt: a.startedAt.Format(time.RFC3339),
		LogPath:   logx.Path(),
	}
}

// ResourceStats 资源占用快照，状态栏与「资源监视器」用。
type ResourceStats struct {
	// ProcessHeapMB 当前 Go 堆占用。
	ProcessHeapMB float64 `json:"processHeapMb"`
	// ProcessSysMB 向系统申请的总内存。
	ProcessSysMB float64 `json:"processSysMb"`
	Goroutines   int     `json:"goroutines"`
	// ResultSets 结果集引擎的统计。
	ResultSets rs.GlobalStats `json:"resultSets"`
	// OpenConnections 已打开的连接数。
	OpenConnections int `json:"openConnections"`
}

// GetResourceStats 返回资源占用快照。
//
// 这个面板是刻意做出来的：一个数据库客户端到底占了多少内存、
// 哪个结果集占的、有没有落盘，用户应该能直接看到而不是靠猜。
func (a *App) GetResourceStats() ResourceStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	a.mu.RLock()
	open := len(a.sessions)
	a.mu.RUnlock()

	return ResourceStats{
		ProcessHeapMB:   float64(ms.HeapAlloc) / (1 << 20),
		ProcessSysMB:    float64(ms.Sys) / (1 << 20),
		Goroutines:      runtime.NumGoroutine(),
		ResultSets:      a.rs.Stats(),
		OpenConnections: open,
	}
}

// ReleaseMemory 主动把闲置结果集压回磁盘并把内存还给系统。
func (a *App) ReleaseMemory() ResourceStats {
	a.rs.EnforceBudget()
	runtime.GC()
	// FreeOSMemory 把已释放的堆页真正交还操作系统，
	// 否则从活动监视器上看进程内存迟迟不下降。
	debugFreeOSMemory()
	return a.GetResourceStats()
}

// --- 设置 ---

// GetSettings 返回应用设置。
func (a *App) GetSettings() store.Settings { return a.store.Settings() }

// SaveSettings 保存应用设置，并把影响后端行为的项即时生效。
func (a *App) SaveSettings(s store.Settings) (store.Settings, error) {
	saved, err := a.store.SaveSettings(s)
	if err != nil {
		return saved, err
	}
	a.rs.SetMemoryLimit(int64(saved.MemoryLimitMB) << 20)
	a.rs.SetMaxRows(saved.MaxResultRows)
	// 语言落到系统层，让菜单栏与 AppKit 文案下次启动跟着变。
	applyLanguageToBundle(saved.Language)
	// 日志开关即时生效，排查问题时不必重启应用。
	if err := logx.Init("", saved.DiagnosticLog); err != nil {
		return saved, nil
	}
	logx.SetSlowThreshold(saved.SlowQueryMS)
	return saved, nil
}

// RevealLogFile 在访达里定位诊断日志文件。
func (a *App) RevealLogFile() error {
	path := logx.Path()
	if path == "" {
		return fmt.Errorf("诊断日志未启用")
	}
	return exec.Command("open", "-R", path).Start()
}

// ReadLogTail 返回日志末尾若干字节，便于在界面里直接查看。
func (a *App) ReadLogTail(maxBytes int64) (string, error) {
	path := logx.Path()
	if path == "" {
		return "", fmt.Errorf("诊断日志未启用")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if maxBytes <= 0 || maxBytes > 1<<20 {
		maxBytes = 256 << 10
	}
	offset := st.Size() - maxBytes
	if offset < 0 {
		offset = 0
	}
	buf := make([]byte, st.Size()-offset)
	if _, err := f.ReadAt(buf, offset); err != nil && len(buf) > 0 {
		return string(buf), nil
	}
	return string(buf), nil
}

// InitialAppearance 返回启动时应采用的外观：dark 或 light。
//
// 给窗口背景色用。Wails 的背景色只能在创建窗口时定死，如果固定成浅色，
// 深色模式下启动会先闪一下白底再被页面盖住。
func (a *App) InitialAppearance() string {
	switch a.store.Settings().Theme {
	case "dark":
		return "dark"
	case "light":
		return "light"
	}
	if systemPrefersDark() {
		return "dark"
	}
	return "light"
}

// ThemePreference 返回用户的外观设置：system / light / dark。
func (a *App) ThemePreference() string {
	t := a.store.Settings().Theme
	if t == "" {
		return "system"
	}
	return t
}

// systemPrefersDark 读取 macOS 的全局外观。
// 非深色时该键不存在，命令会返回错误——这正是「浅色」的信号。
func systemPrefersDark() bool {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "dark")
}

// Context 返回 Wails 运行时上下文，供 main 包发送事件。
func (a *App) Context() context.Context { return a.ctx }

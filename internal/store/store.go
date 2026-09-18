// Package store 负责连接配置、应用设置与查询历史的本地持久化。
//
// 密码从不写进配置文件：落盘的 JSON 里 password 恒为空，真正的口令存放在
// 系统钥匙串（macOS Keychain / Windows 凭据管理器 / Linux Secret Service）。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"

	"tacivan/internal/dbx"
)

const keyringService = "Tacivan"

// legacyKeyringService 是改名前的服务名。
// 钥匙串条目按服务名索引，改名等于所有已存密码一夜之间读不到了——
// 所以读不到新名字下的条目时要回头看旧名字，找到就搬过来并删掉旧的。
const legacyKeyringService = "Navigo"

// 钥匙串的三个操作用变量承接，测试里才能换成假实现验证迁移逻辑。
var (
	kGet = keyring.Get
	kSet = keyring.Set
	kDel = keyring.Delete
)

// Settings 应用级设置，对应「选项」对话框。
type Settings struct {
	// Theme: light | dark | system
	Theme string `json:"theme"`
	// Language: system | zh-CN | en-US。system 表示跟随系统语言。
	Language string `json:"language"`
	// GridPageSize 数据网格每次向后端请求的行数。
	GridPageSize int `json:"gridPageSize"`
	// RowLimit 打开表数据时给 SELECT 加的 LIMIT，0 表示不加。
	//
	// 与 MaxResultRows 的区别：这个是日常浏览的默认上限（对应其它客户端的
	// 「限制记录数」），MaxResultRows 是防止误操作打穿磁盘的最后一道闸。
	RowLimit int `json:"rowLimit"`
	// MemoryLimitMB 结果集全局常驻内存上限。
	MemoryLimitMB int `json:"memoryLimitMb"`
	// CellPreviewLimit 单元格预览字节上限。
	CellPreviewLimit int `json:"cellPreviewLimit"`
	// MaxResultRows 单个结果集最多拉取的行数，0 表示不限。
	MaxResultRows int64 `json:"maxResultRows"`
	// AutoCommit SQL 编辑器是否自动提交。
	AutoCommit bool `json:"autoCommit"`
	// ConfirmOnDelete 删除数据前是否二次确认。
	ConfirmOnDelete bool `json:"confirmOnDelete"`
	// FontSize 编辑器与网格字号。
	FontSize int `json:"fontSize"`
	// EditorFont 编辑器字体。
	EditorFont string `json:"editorFont"`
	// ShowSystemObjects 对象树是否显示系统库。
	ShowSystemObjects bool `json:"showSystemObjects"`
	// HistoryLimit 保留的查询历史条数。
	HistoryLimit int `json:"historyLimit"`
	// DiagnosticLog 是否记录带耗时的诊断日志。
	DiagnosticLog bool `json:"diagnosticLog"`
	// SlowQueryMS 超过该毫秒数的语句在日志里标记为 slow。
	SlowQueryMS int64 `json:"slowQueryMs"`
}

// DefaultSettings 返回默认设置。
func DefaultSettings() Settings {
	return Settings{
		Theme:             "system",
		Language:          "system",
		GridPageSize:      200,
		RowLimit:          1000,
		MemoryLimitMB:     512,
		CellPreviewLimit:  4096,
		MaxResultRows:     5_000_000,
		AutoCommit:        true,
		ConfirmOnDelete:   true,
		FontSize:          13,
		EditorFont:        defaultMonoFont(),
		ShowSystemObjects: false,
		HistoryLimit:      500,
		// 默认开着：每条语句只多一行文本的开销，
		// 但「为什么慢」这类问题能直接从日志得到答案而不是靠猜。
		DiagnosticLog: true,
		SlowQueryMS:   300,
	}
}

func defaultMonoFont() string {
	if runtime.GOOS == "darwin" {
		return "SF Mono, Menlo, monospace"
	}
	return "Consolas, 'Courier New', monospace"
}

// HistoryEntry 一条查询历史。
type HistoryEntry struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connectionId"`
	Connection   string    `json:"connection"`
	Database     string    `json:"database"`
	SQL          string    `json:"sql"`
	At           time.Time `json:"at"`
	DurationMS   int64     `json:"durationMs"`
	RowsAffected int64     `json:"rowsAffected"`
	Success      bool      `json:"success"`
	Error        string    `json:"error,omitempty"`
}

// SavedQuery 一条用户保存的查询。
type SavedQuery struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ConnectionID string    `json:"connectionId"`
	Database     string    `json:"database"`
	SQL          string    `json:"sql"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Favorite 一条收藏。
//
// Name 为空表示收藏整个库；否则是库里的某个对象。
type Favorite struct {
	ConnectionID string    `json:"connectionId"`
	Database     string    `json:"database"`
	Schema       string    `json:"schema"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	AddedAt      time.Time `json:"addedAt"`
}

// Key 收藏的唯一标识，与对象树节点一一对应。
func (f Favorite) Key() string {
	return strings.Join([]string{f.ConnectionID, f.Database, f.Schema, f.Name}, "\x1f")
}

// Store 本地配置存储。
type Store struct {
	dir string

	mu       sync.RWMutex
	conns    []dbx.ConnectionConfig
	settings Settings
	groups   []Group
	history  []HistoryEntry
	saved    []SavedQuery
	favs     []Favorite

	// keyringOK 为假表示系统钥匙串不可用（如无桌面会话的 Linux），
	// 此时口令只存在进程内存里，并在界面上提示。
	keyringOK bool
	secretsMu sync.RWMutex
	secrets   map[string]string
}

// New 打开（或初始化）配置目录。
func New() (*Store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}
	s := &Store{dir: dir, settings: DefaultSettings(), secrets: map[string]string{}}
	s.keyringOK = probeKeyring()
	if err := s.load(); err != nil {
		return nil, err
	}
	s.migrateSecrets()
	return s, nil
}

func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("无法确定配置目录: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "Tacivan")
	migrateConfigDir(filepath.Join(base, "Navigo"), dir)
	return dir, nil
}

// migrateConfigDir 把改名前的配置目录整体搬到新位置。
// 只在"新目录不存在、旧目录存在"时动手，重复执行不会覆盖任何东西。
func migrateConfigDir(oldDir, newDir string) {
	if _, err := os.Stat(newDir); err == nil {
		return
	}
	if _, err := os.Stat(oldDir); err != nil {
		return
	}
	_ = os.Rename(oldDir, newDir)
}

// Dir 配置目录路径。
func (s *Store) Dir() string { return s.dir }

// KeyringAvailable 系统钥匙串是否可用。
func (s *Store) KeyringAvailable() bool { return s.keyringOK }

func probeKeyring() bool {
	// 自动化环境（CI、集成测试）里访问钥匙串会弹出授权框并卡住，用开关跳过。
	if os.Getenv("TACIVAN_NO_KEYRING") != "" {
		return false
	}
	const probeKey = "__navigo_probe__"
	if err := keyring.Set(keyringService, probeKey, "1"); err != nil {
		return false
	}
	_ = keyring.Delete(keyringService, probeKey)
	return true
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

func (s *Store) load() error {
	if err := readJSON(s.path("connections.json"), &s.conns); err != nil {
		return err
	}
	settings := DefaultSettings()
	if err := readJSON(s.path("settings.json"), &settings); err != nil {
		return err
	}
	s.settings = settings
	if err := readJSON(s.path("history.json"), &s.history); err != nil {
		return err
	}
	if err := readJSON(s.path("saved_queries.json"), &s.saved); err != nil {
		return err
	}
	if err := readJSON(s.path("favorites.json"), &s.favs); err != nil {
		return err
	}
	if err := readJSON(s.path("groups.json"), &s.groups); err != nil {
		return err
	}
	sort.SliceStable(s.conns, func(i, j int) bool { return s.conns[i].SortOrder < s.conns[j].SortOrder })
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), err)
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", filepath.Base(path), err)
	}
	return nil
}

// writeJSON 原子写入：先写临时文件再改名，避免写一半断电留下损坏的配置。
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保存 %s 失败: %w", filepath.Base(path), err)
	}
	return nil
}

// --- 连接 ---

// Connections 返回全部连接配置（不含密码）。
func (s *Store) Connections() []dbx.ConnectionConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]dbx.ConnectionConfig, len(s.conns))
	copy(out, s.conns)
	for i := range out {
		out[i].Password = ""
		out[i].SSH.Password = ""
		out[i].SSH.Passphrase = ""
	}
	return out
}

// Connection 按 ID 取连接配置，密码从钥匙串补齐。
func (s *Store) Connection(id string) (dbx.ConnectionConfig, bool) {
	s.mu.RLock()
	var cfg dbx.ConnectionConfig
	found := false
	for _, c := range s.conns {
		if c.ID == id {
			cfg = c
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return cfg, false
	}
	cfg.Password = s.loadSecret(id, "password")
	cfg.SSH.Password = s.loadSecret(id, "ssh-password")
	cfg.SSH.Passphrase = s.loadSecret(id, "ssh-passphrase")
	return cfg, true
}

// SaveConnection 新增或更新一条连接。
func (s *Store) SaveConnection(cfg dbx.ConnectionConfig) (dbx.ConnectionConfig, error) {
	if cfg.Name == "" {
		return cfg, fmt.Errorf("连接名不能为空")
	}
	now := time.Now()

	s.mu.Lock()
	if cfg.ID == "" {
		cfg.ID = newID()
		cfg.CreatedAt = now
		cfg.SortOrder = len(s.conns)
	}
	cfg.UpdatedAt = now

	// 密码单独存钥匙串，配置里清空。
	password, sshPassword, sshPassphrase := cfg.Password, cfg.SSH.Password, cfg.SSH.Passphrase
	cfg.Password, cfg.SSH.Password, cfg.SSH.Passphrase = "", "", ""

	replaced := false
	for i := range s.conns {
		if s.conns[i].ID == cfg.ID {
			if cfg.CreatedAt.IsZero() {
				cfg.CreatedAt = s.conns[i].CreatedAt
			}
			s.conns[i] = cfg
			replaced = true
			break
		}
	}
	if !replaced {
		s.conns = append(s.conns, cfg)
	}
	conns := make([]dbx.ConnectionConfig, len(s.conns))
	copy(conns, s.conns)
	s.mu.Unlock()

	if err := writeJSON(s.path("connections.json"), conns); err != nil {
		return cfg, err
	}
	// 空字符串表示「清除已保存的密码」，与「保持不变」由前端显式区分。
	s.storeSecret(cfg.ID, "password", password)
	s.storeSecret(cfg.ID, "ssh-password", sshPassword)
	s.storeSecret(cfg.ID, "ssh-passphrase", sshPassphrase)
	return cfg, nil
}

// DeleteConnection 删除连接及其钥匙串条目。
func (s *Store) DeleteConnection(id string) error {
	s.mu.Lock()
	kept := s.conns[:0]
	for _, c := range s.conns {
		if c.ID != id {
			kept = append(kept, c)
		}
	}
	s.conns = kept
	conns := make([]dbx.ConnectionConfig, len(s.conns))
	copy(conns, s.conns)
	s.mu.Unlock()

	for _, field := range []string{"password", "ssh-password", "ssh-passphrase"} {
		_ = kDel(keyringService, id+":"+field)
		_ = kDel(legacyKeyringService, id+":"+field)
	}
	return writeJSON(s.path("connections.json"), conns)
}

// ReorderConnections 按给定的 ID 顺序重排连接树。
func (s *Store) ReorderConnections(ids []string) error {
	s.mu.Lock()
	pos := make(map[string]int, len(ids))
	for i, id := range ids {
		pos[id] = i
	}
	for i := range s.conns {
		if p, ok := pos[s.conns[i].ID]; ok {
			s.conns[i].SortOrder = p
		}
	}
	sort.SliceStable(s.conns, func(i, j int) bool { return s.conns[i].SortOrder < s.conns[j].SortOrder })
	conns := make([]dbx.ConnectionConfig, len(s.conns))
	copy(conns, s.conns)
	s.mu.Unlock()
	return writeJSON(s.path("connections.json"), conns)
}

func (s *Store) storeSecret(id, field, value string) {
	key := id + ":" + field
	if !s.keyringOK {
		// 钥匙串不可用时退回进程内存：口令在本次运行内有效，退出即失效，
		// 总好过静默写进明文文件。
		s.secretsMu.Lock()
		if value == "" {
			delete(s.secrets, key)
		} else {
			s.secrets[key] = value
		}
		s.secretsMu.Unlock()
		return
	}
	if value == "" {
		_ = kDel(keyringService, key)
		return
	}
	_ = kSet(keyringService, key, value)
}

func (s *Store) loadSecret(id, field string) string {
	key := id + ":" + field
	if !s.keyringOK {
		s.secretsMu.RLock()
		v := s.secrets[key]
		s.secretsMu.RUnlock()
		return v
	}
	return secretGet(key)
}

// secretGet 读一条钥匙串条目，新名字下没有就去旧名字下找并搬过来。
// 搬完删旧条目，下次就直接命中新名字，整个过程幂等。
func secretGet(key string) string {
	if v, err := kGet(keyringService, key); err == nil {
		return v
	}
	v, err := kGet(legacyKeyringService, key)
	if err != nil {
		return ""
	}
	if kSet(keyringService, key, v) == nil {
		_ = kDel(legacyKeyringService, key)
	}
	return v
}

// migrateSecrets 启动时把所有连接的旧钥匙串条目一次性搬到新服务名下。
// loadSecret 里的懒迁移已经能兜底，这里是为了让迁移一次做完，
// 不要拖成"哪个连接被点开哪个才搬"。
func (s *Store) migrateSecrets() {
	if !s.keyringOK {
		return
	}
	s.mu.RLock()
	ids := make([]string, 0, len(s.conns))
	for _, c := range s.conns {
		ids = append(ids, c.ID)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		for _, field := range []string{"password", "ssh-password", "ssh-passphrase"} {
			_ = secretGet(id + ":" + field)
		}
	}
}

// --- 设置 ---

// Settings 返回当前设置。
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// SaveSettings 保存设置。
func (s *Store) SaveSettings(v Settings) (Settings, error) {
	// 兜底非法值，避免前端传 0 把网格弄成取不到数。
	if v.GridPageSize <= 0 {
		v.GridPageSize = 200
	}
	if v.RowLimit < 0 {
		v.RowLimit = 0
	}
	if v.MemoryLimitMB < 64 {
		v.MemoryLimitMB = 64
	}
	if v.CellPreviewLimit < 256 {
		v.CellPreviewLimit = 256
	}
	if v.FontSize < 9 {
		v.FontSize = 9
	}
	if v.HistoryLimit <= 0 {
		v.HistoryLimit = 500
	}
	if v.SlowQueryMS <= 0 {
		v.SlowQueryMS = 300
	}
	s.mu.Lock()
	s.settings = v
	s.mu.Unlock()
	return v, writeJSON(s.path("settings.json"), v)
}

// --- 查询历史 ---

// AddHistory 追加一条查询历史。
func (s *Store) AddHistory(e HistoryEntry) {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	s.mu.Lock()
	limit := s.settings.HistoryLimit
	s.history = append([]HistoryEntry{e}, s.history...)
	if len(s.history) > limit {
		s.history = s.history[:limit]
	}
	snapshot := make([]HistoryEntry, len(s.history))
	copy(snapshot, s.history)
	s.mu.Unlock()
	// 历史属于「丢了也无所谓」的数据，写失败不打断用户操作。
	_ = writeJSON(s.path("history.json"), snapshot)
}

// History 返回查询历史。
func (s *Store) History(limit int) []HistoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.history) {
		limit = len(s.history)
	}
	out := make([]HistoryEntry, limit)
	copy(out, s.history[:limit])
	return out
}

// ClearHistory 清空查询历史。
func (s *Store) ClearHistory() error {
	s.mu.Lock()
	s.history = nil
	s.mu.Unlock()
	return writeJSON(s.path("history.json"), []HistoryEntry{})
}

// --- 已保存的查询 ---

// SavedQueries 返回全部已保存查询。
func (s *Store) SavedQueries() []SavedQuery {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SavedQuery, len(s.saved))
	copy(out, s.saved)
	return out
}

// SaveQuery 新增或更新一条已保存查询。
func (s *Store) SaveQuery(q SavedQuery) (SavedQuery, error) {
	if q.Name == "" {
		return q, fmt.Errorf("查询名称不能为空")
	}
	now := time.Now()
	s.mu.Lock()
	if q.ID == "" {
		q.ID = newID()
		q.CreatedAt = now
	}
	q.UpdatedAt = now
	replaced := false
	for i := range s.saved {
		if s.saved[i].ID == q.ID {
			s.saved[i] = q
			replaced = true
			break
		}
	}
	if !replaced {
		s.saved = append(s.saved, q)
	}
	snapshot := make([]SavedQuery, len(s.saved))
	copy(snapshot, s.saved)
	s.mu.Unlock()
	return q, writeJSON(s.path("saved_queries.json"), snapshot)
}

// DeleteSavedQuery 删除一条已保存查询。
func (s *Store) DeleteSavedQuery(id string) error {
	s.mu.Lock()
	kept := s.saved[:0]
	for _, q := range s.saved {
		if q.ID != id {
			kept = append(kept, q)
		}
	}
	s.saved = kept
	snapshot := make([]SavedQuery, len(s.saved))
	copy(snapshot, s.saved)
	s.mu.Unlock()
	return writeJSON(s.path("saved_queries.json"), snapshot)
}

// --- 收藏 ---

// Favorites 返回全部收藏。
func (s *Store) Favorites() []Favorite {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Favorite, len(s.favs))
	copy(out, s.favs)
	return out
}

// ToggleFavorite 切换收藏状态，返回切换后是否为已收藏。
func (s *Store) ToggleFavorite(f Favorite) (bool, error) {
	key := f.Key()
	s.mu.Lock()
	idx := -1
	for i, x := range s.favs {
		if x.Key() == key {
			idx = i
			break
		}
	}
	added := idx < 0
	if added {
		f.AddedAt = time.Now()
		s.favs = append(s.favs, f)
	} else {
		s.favs = append(s.favs[:idx], s.favs[idx+1:]...)
	}
	snapshot := make([]Favorite, len(s.favs))
	copy(snapshot, s.favs)
	s.mu.Unlock()

	return added, writeJSON(s.path("favorites.json"), snapshot)
}

// RemoveFavoritesByConnection 连接被删除时清掉它名下的收藏。
func (s *Store) RemoveFavoritesByConnection(connID string) error {
	s.mu.Lock()
	kept := s.favs[:0]
	for _, f := range s.favs {
		if f.ConnectionID != connID {
			kept = append(kept, f)
		}
	}
	s.favs = kept
	snapshot := make([]Favorite, len(s.favs))
	copy(snapshot, s.favs)
	s.mu.Unlock()
	return writeJSON(s.path("favorites.json"), snapshot)
}

var idSeq struct {
	sync.Mutex
	n int
}

func newID() string {
	idSeq.Lock()
	idSeq.n++
	n := idSeq.n
	idSeq.Unlock()
	return fmt.Sprintf("%x-%x", time.Now().UnixNano(), n)
}

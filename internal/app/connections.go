package app

import (
	"fmt"
	"strings"
	"time"

	"tacivan/internal/dbx"
	"tacivan/internal/logx"
)

// ConnectionState 一条连接在界面上的状态。
type ConnectionState struct {
	Config dbx.ConnectionConfig `json:"config"`
	// Open 是否已建立连接。
	Open       bool           `json:"open"`
	ServerInfo dbx.ServerInfo `json:"serverInfo"`
	Capability dbx.Capability `json:"capability"`
	OpenedAt   string         `json:"openedAt"`
	CurrentDB  string         `json:"currentDb"`
}

// ListConnections 返回全部已保存的连接及其打开状态。
func (a *App) ListConnections() []ConnectionState {
	configs := a.store.Connections()
	out := make([]ConnectionState, 0, len(configs))

	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, cfg := range configs {
		st := ConnectionState{Config: cfg}
		if s, ok := a.sessions[cfg.ID]; ok {
			st.Open = true
			st.ServerInfo = s.info
			st.Capability = s.conn.Capability()
			st.OpenedAt = s.openedAt.Format(time.RFC3339)
			s.mu.Lock()
			st.CurrentDB = s.currentDB
			s.mu.Unlock()
		}
		out = append(out, st)
	}
	return out
}

// SaveConnection 新增或更新一条连接配置。
func (a *App) SaveConnection(cfg dbx.ConnectionConfig) (dbx.ConnectionConfig, error) {
	if err := validateConfig(&cfg); err != nil {
		return cfg, err
	}
	saved, err := a.store.SaveConnection(cfg)
	if err != nil {
		return saved, err
	}
	saved.Password = ""
	return saved, nil
}

// validateConfig 在写库前做基本校验，把错误挡在连接对话框里。
func validateConfig(cfg *dbx.ConnectionConfig) error {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		return fmt.Errorf("请填写连接名")
	}
	if cfg.Engine == "" {
		return fmt.Errorf("请选择数据库类型")
	}
	if cfg.Engine == dbx.EngineSQLite {
		if strings.TrimSpace(cfg.Database) == "" {
			return fmt.Errorf("请选择 SQLite 数据库文件")
		}
		return nil
	}
	// 插件引擎没有主机/端口这套东西，它的配置在 Params 里，缺什么由插件驱动
	// 打开连接时按清单校验。这里再查端口就会把 0 当成错误，用户根本没地方填。
	if strings.HasPrefix(string(cfg.Engine), "plugin:") || cfg.Engine == dbx.EngineMCP {
		cfg.Host, cfg.Port = "", 0
		cfg.SSH.Enabled = false
		return nil
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Host == "" {
		return fmt.Errorf("请填写主机地址")
	}
	if cfg.Port == 0 {
		cfg.Port = cfg.Engine.DefaultPort()
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("端口号无效: %d", cfg.Port)
	}
	if cfg.SSH.Enabled {
		if strings.TrimSpace(cfg.SSH.Host) == "" {
			return fmt.Errorf("已启用 SSH 隧道，请填写跳板机地址")
		}
		if strings.TrimSpace(cfg.SSH.User) == "" {
			return fmt.Errorf("已启用 SSH 隧道，请填写 SSH 用户名")
		}
		if cfg.SSH.Port == 0 {
			cfg.SSH.Port = 22
		}
	}
	return nil
}

// DeleteConnection 删除连接配置；若已打开会先断开。
func (a *App) DeleteConnection(id string) error {
	_ = a.CloseConnection(id)
	// 顺带清掉它名下的收藏，免得留下指向不存在连接的孤儿条目。
	a.deleteConnectionFavorites(id)
	return a.store.DeleteConnection(id)
}

// DuplicateConnection 复制一条连接配置。
func (a *App) DuplicateConnection(id string) (dbx.ConnectionConfig, error) {
	cfg, ok := a.store.Connection(id)
	if !ok {
		return dbx.ConnectionConfig{}, fmt.Errorf("连接不存在")
	}
	cfg.ID = ""
	cfg.Name = cfg.Name + " 副本"
	saved, err := a.store.SaveConnection(cfg)
	saved.Password = ""
	return saved, err
}

// ReorderConnections 按给定顺序重排连接树。
func (a *App) ReorderConnections(ids []string) error {
	return a.store.ReorderConnections(ids)
}

// TestConnection 测试连接配置是否可用。
//
// 若密码字段为空且该连接已保存过，则沿用钥匙串里的口令，
// 这样用户编辑已有连接时不必重新输入密码就能点「测试」。
func (a *App) TestConnection(cfg dbx.ConnectionConfig) (dbx.ServerInfo, error) {
	if err := validateConfig(&cfg); err != nil {
		return dbx.ServerInfo{}, err
	}
	if cfg.Password == "" && cfg.ID != "" {
		if saved, ok := a.store.Connection(cfg.ID); ok {
			cfg.Password = saved.Password
			if cfg.SSH.Password == "" {
				cfg.SSH.Password = saved.SSH.Password
			}
			if cfg.SSH.Passphrase == "" {
				cfg.SSH.Passphrase = saved.SSH.Passphrase
			}
		}
	}

	timeout := cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = 15
	}
	ctx, cancel := a.requestCtx(timeout + 5)
	defer cancel()

	conn, err := dbx.Open(ctx, cfg)
	if err != nil {
		return dbx.ServerInfo{}, err
	}
	defer conn.Close()
	return conn.ServerInfo(ctx)
}

// OpenConnection 打开一条已保存的连接。
func (a *App) OpenConnection(id string) (ConnectionState, error) {
	a.mu.RLock()
	existing, ok := a.sessions[id]
	a.mu.RUnlock()
	if ok {
		return a.stateOf(existing), nil
	}

	cfg, found := a.store.Connection(id)
	if !found {
		return ConnectionState{}, fmt.Errorf("连接不存在")
	}

	timeout := cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = 15
	}
	ctx, cancel := a.requestCtx(timeout + 5)
	defer cancel()

	openStart := time.Now()
	conn, err := dbx.Open(ctx, cfg)
	logx.Op("OpenConnection", openStart, "conn", cfg.Name, "engine", string(cfg.Engine))
	if err != nil {
		return ConnectionState{}, err
	}
	info, err := conn.ServerInfo(ctx)
	if err != nil {
		conn.Close()
		return ConnectionState{}, err
	}

	now := time.Now()
	s := &session{
		cfg: cfg, conn: conn, info: info,
		openedAt: now, lastUsed: now,
		currentDB: cfg.Database,
	}

	a.mu.Lock()
	// 双检：两个并发的双击不该建出两条连接。
	if prev, dup := a.sessions[id]; dup {
		a.mu.Unlock()
		conn.Close()
		return a.stateOf(prev), nil
	}
	a.sessions[id] = s
	a.mu.Unlock()

	return a.stateOf(s), nil
}

func (a *App) stateOf(s *session) ConnectionState {
	cfg := s.cfg
	cfg.Password = ""
	cfg.SSH.Password = ""
	cfg.SSH.Passphrase = ""
	s.mu.Lock()
	current := s.currentDB
	s.mu.Unlock()
	return ConnectionState{
		Config:     cfg,
		Open:       true,
		ServerInfo: s.info,
		Capability: s.conn.Capability(),
		OpenedAt:   s.openedAt.Format(time.RFC3339),
		CurrentDB:  current,
	}
}

// CloseConnection 断开连接，并关闭其下全部结果集。
func (a *App) CloseConnection(id string) error {
	a.mu.Lock()
	s, ok := a.sessions[id]
	delete(a.sessions, id)
	a.mu.Unlock()
	if !ok {
		return nil
	}
	// 先关结果集：它们持有该连接上的游标，顺序反了会让驱动报「连接已关闭」。
	a.rs.CloseByConnection(id)
	err := s.conn.Close()
	debugFreeOSMemory()
	return err
}

// SetCurrentDatabase 记录界面上当前选中的库。
func (a *App) SetCurrentDatabase(connID, database string) error {
	s, err := a.lookup(connID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.currentDB = database
	s.mu.Unlock()
	return nil
}

// GetServerInfo 返回连接的服务器信息。
func (a *App) GetServerInfo(connID string) (dbx.ServerInfo, error) {
	s, err := a.lookup(connID)
	if err != nil {
		return dbx.ServerInfo{}, err
	}
	ctx, cancel := a.requestCtx(15)
	defer cancel()
	return s.conn.ServerInfo(ctx)
}

// PingConnection 检查连接是否仍然可用。
func (a *App) PingConnection(connID string) error {
	s, err := a.lookup(connID)
	if err != nil {
		return err
	}
	ctx, cancel := a.requestCtx(10)
	defer cancel()
	return s.conn.Ping(ctx)
}

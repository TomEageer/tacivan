package rs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tacivan/internal/dbx"
)

// Manager 管理进程内全部结果集的生命周期与内存预算。
//
// 每个结果集自己有软上限，Manager 再在其上加一道全局闸：
// 同时打开很多标签页时，最久未访问的结果集会被压回磁盘，
// 保证进程常驻内存不随标签页数量线性增长。
type Manager struct {
	mu      sync.Mutex
	cursors map[string]*Cursor
	order   []string // 按创建顺序，用于稳定的回收次序

	opts        Options
	globalLimit int64
	spillDir    string
	seq         atomic.Uint64

	stopCh chan struct{}
	once   sync.Once
}

// GlobalStats 全局资源快照。
type GlobalStats struct {
	OpenResultSets int   `json:"openResultSets"`
	MemoryBytes    int64 `json:"memoryBytes"`
	MemoryLimit    int64 `json:"memoryLimit"`
	SpillBytes     int64 `json:"spillBytes"`
	TotalRows      int64 `json:"totalRows"`
	// TrimCount 累计触发全局回收的次数。
	TrimCount int64   `json:"trimCount"`
	Cursors   []Stats `json:"cursors"`
}

// NewManager 创建管理器。globalLimit 为全局常驻内存上限（字节）。
func NewManager(globalLimit int64, spillDir string, opts Options) *Manager {
	if globalLimit <= 0 {
		globalLimit = 512 << 20
	}
	if spillDir == "" {
		spillDir = filepath.Join(os.TempDir(), "tacivan-spill")
	}
	opts = opts.normalize()
	opts.SpillDir = spillDir

	// 清掉上次进程留下的溢出文件（异常退出时可能残留）。
	cleanStaleSpill(spillDir)

	m := &Manager{
		cursors:     make(map[string]*Cursor),
		opts:        opts,
		globalLimit: globalLimit,
		spillDir:    spillDir,
		stopCh:      make(chan struct{}),
	}
	go m.janitor()
	return m
}

var trimCounter atomic.Int64

// Open 把一个行流包装成受管理的游标。
func (m *Manager) Open(stream dbx.RowStream, meta CursorMeta) *Cursor {
	id := fmt.Sprintf("rs%d-%d", time.Now().UnixNano()%1e9, m.seq.Add(1))
	c := NewCursor(id, stream, m.opts, meta)

	m.mu.Lock()
	m.cursors[id] = c
	m.order = append(m.order, id)
	m.mu.Unlock()

	// 新结果集进来先看一眼全局水位，必要时把旧的压回磁盘。
	m.EnforceBudget()
	return c
}

// Get 按 ID 取游标。
func (m *Manager) Get(id string) (*Cursor, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cursors[id]
	return c, ok
}

// Fetch 是 Get + Fetch 的便捷封装，并在取数后校验全局水位。
func (m *Manager) Fetch(id string, offset, limit int64) (*Page, error) {
	c, ok := m.Get(id)
	if !ok {
		return nil, fmt.Errorf("结果集不存在或已关闭: %s", id)
	}
	page, err := c.Fetch(offset, limit)
	if err != nil {
		return nil, err
	}
	m.EnforceBudget()
	return page, nil
}

// Close 关闭并移除一个结果集。
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	c := m.cursors[id]
	delete(m.cursors, id)
	for i, v := range m.order {
		if v == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Close()
}

// CloseByConnection 关闭某个连接下的全部结果集，连接断开时调用。
func (m *Manager) CloseByConnection(connID string) {
	m.mu.Lock()
	var victims []*Cursor
	for id, c := range m.cursors {
		if c.Meta.ConnectionID == connID {
			victims = append(victims, c)
			delete(m.cursors, id)
		}
	}
	kept := m.order[:0]
	for _, id := range m.order {
		if _, ok := m.cursors[id]; ok {
			kept = append(kept, id)
		}
	}
	m.order = kept
	m.mu.Unlock()

	for _, c := range victims {
		_ = c.Close()
	}
}

// EnforceBudget 检查全局内存水位，超限时按最久未访问的顺序把结果集压回磁盘。
func (m *Manager) EnforceBudget() {
	m.mu.Lock()
	defer m.mu.Unlock()

	var total int64
	type entry struct {
		c    *Cursor
		mem  int64
		seen time.Time
	}
	entries := make([]entry, 0, len(m.cursors))
	for _, c := range m.cursors {
		mem := c.memoryBytes()
		total += mem
		entries = append(entries, entry{c: c, mem: mem, seen: c.idleSince()})
	}
	if total <= m.globalLimit {
		return
	}

	// 目标压到 80% 水位，留出余量避免刚回收完又立刻触发。
	target := m.globalLimit * 8 / 10
	sort.Slice(entries, func(i, j int) bool { return entries[i].seen.Before(entries[j].seen) })
	for _, e := range entries {
		if total <= target {
			break
		}
		if e.mem == 0 {
			continue
		}
		e.c.Trim()
		freed := e.mem - e.c.memoryBytes()
		total -= freed
		trimCounter.Add(1)
	}
}

// Stats 全局快照。
func (m *Manager) Stats() GlobalStats {
	m.mu.Lock()
	cursors := make([]*Cursor, 0, len(m.cursors))
	for _, c := range m.cursors {
		cursors = append(cursors, c)
	}
	m.mu.Unlock()

	gs := GlobalStats{
		MemoryLimit: m.globalLimit,
		TrimCount:   trimCounter.Load(),
		Cursors:     make([]Stats, 0, len(cursors)),
	}
	for _, c := range cursors {
		s := c.Stats()
		gs.MemoryBytes += s.MemoryBytes
		gs.SpillBytes += s.SpillBytes
		gs.TotalRows += s.Loaded
		gs.Cursors = append(gs.Cursors, s)
	}
	gs.OpenResultSets = len(cursors)
	sort.Slice(gs.Cursors, func(i, j int) bool { return gs.Cursors[i].MemoryBytes > gs.Cursors[j].MemoryBytes })
	return gs
}

// janitor 定期把长时间闲置的结果集压回磁盘。
//
// 注意这里只 Trim 不 Close：用户切回旧标签页时数据仍在（从溢出文件重建），
// 只是不再白白占着内存。
func (m *Manager) janitor() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	const idleThreshold = 3 * time.Minute
	for {
		select {
		case <-m.stopCh:
			return
		case <-t.C:
			m.mu.Lock()
			var idle []*Cursor
			for _, c := range m.cursors {
				if time.Since(c.idleSince()) > idleThreshold && c.memoryBytes() > 0 {
					idle = append(idle, c)
				}
			}
			m.mu.Unlock()
			for _, c := range idle {
				c.Trim()
			}
			m.EnforceBudget()
		}
	}
}

// Shutdown 关闭全部结果集并停止后台任务。
func (m *Manager) Shutdown() {
	m.once.Do(func() { close(m.stopCh) })
	m.mu.Lock()
	cursors := make([]*Cursor, 0, len(m.cursors))
	for _, c := range m.cursors {
		cursors = append(cursors, c)
	}
	m.cursors = make(map[string]*Cursor)
	m.order = nil
	m.mu.Unlock()
	for _, c := range cursors {
		_ = c.Close()
	}
	cleanStaleSpill(m.spillDir)
}

func cleanStaleSpill(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".spill") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// SetMemoryLimit 调整全局内存上限，设置项改动后立即生效。
func (m *Manager) SetMemoryLimit(limit int64) {
	if limit < 64<<20 {
		limit = 64 << 20
	}
	m.mu.Lock()
	m.globalLimit = limit
	m.mu.Unlock()
	m.EnforceBudget()
}

// SetMaxRows 调整单个结果集的行数上限。只影响此后新建的结果集。
func (m *Manager) SetMaxRows(n int64) {
	m.mu.Lock()
	m.opts.MaxRows = n
	m.mu.Unlock()
}

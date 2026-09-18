package rs

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"tacivan/internal/dbx"
)

// Options 单个游标的行为参数。
type Options struct {
	// ChunkRows 每个内存块的行数。块太小则元数据开销大，太大则驱逐粒度粗。
	ChunkRows int
	// MemSoftLimit 本游标常驻内存的软上限（字节）。超过后启用磁盘溢出并开始驱逐旧块。
	MemSoftLimit int64
	// MinResidentChunks 无论预算多紧张都保留的块数，保证当前视窗不会反复抖动。
	MinResidentChunks int
	// MaxRows 单个结果集允许拉取的最大行数，0 表示不限。防止 SELECT 无 WHERE 打穿磁盘。
	MaxRows int64
	// SpillDir 溢出文件目录。
	SpillDir string
}

// DefaultOptions 返回一组经验默认值。
//
// 512 行/块 × 8 块常驻 ≈ 4096 行可视区，对任何屏幕尺寸都绰绰有余；
// 48MB 软上限意味着即便同时开十几个结果集标签页，进程内存仍在几百 MB 量级。
func DefaultOptions() Options {
	return Options{
		ChunkRows:         512,
		MemSoftLimit:      48 << 20,
		MinResidentChunks: 8,
		MaxRows:           5_000_000,
		SpillDir:          "",
	}
}

func (o Options) normalize() Options {
	if o.ChunkRows <= 0 {
		o.ChunkRows = 512
	}
	if o.MemSoftLimit <= 0 {
		o.MemSoftLimit = 48 << 20
	}
	if o.MinResidentChunks <= 0 {
		o.MinResidentChunks = 4
	}
	return o
}

// chunk 一段连续行的内存缓存。
type chunk struct {
	idx     int64
	rows    []dbx.Row
	bytes   int64
	lastUse uint64
}

// Cursor 可随机访问的结果集游标。
//
// 对上层来说它像一个可以任意 Fetch(offset, limit) 的数组，
// 对底层来说它只对驱动做单向顺序读取，且常驻内存有硬性上界。
type Cursor struct {
	mu sync.Mutex

	id     string
	stream dbx.RowStream
	cols   []dbx.ColumnMeta
	opts   Options

	// fetched 已从流中拉取的行数。
	fetched int64
	// eof 流已读完（正常结束或出错）。
	eof bool
	// streamErr 流中途出错时的错误。
	streamErr error
	// hitRowLimit 因触达 MaxRows 而主动停止。
	hitRowLimit bool
	// stopped 用户主动中断了取数，已读到的行仍然有效。
	stopped bool

	chunks   map[int64]*chunk
	memBytes int64
	clock    uint64

	spill *spillFile
	// locs 每行在溢出文件中的位置，仅在溢出启用后有效。
	locs    []rowLoc
	readBuf []byte

	// peakMem 峰值常驻内存，用于状态栏展示。
	peakMem int64
	// lastAccess 最后一次被访问的时间，Manager 据此回收闲置结果集。
	lastAccess time.Time
	createdAt  time.Time

	closed bool

	// --- 下面这组是给 Manager 和状态栏读的无锁快照 ---
	//
	// 它们必须独立于 c.mu 存在。Fetch 是在持有 c.mu 的状态下做网络读取的，
	// 慢链路上一次就能握住几十秒；观察者要是也去抢 c.mu，
	// 一个慢查询就会把内存回收、状态栏、以及其它标签页的打开全部拖死——
	// 实测过：一张表读了 87 秒，同时打开的另一张两行的表也跟着等了 29 秒。
	statMem    atomic.Int64
	statSpill  atomic.Int64
	statRows   atomic.Int64
	statPeak   atomic.Int64
	statChunks atomic.Int64
	statAccess atomic.Int64 // UnixNano
	statDone   atomic.Bool
	// readNanos 累计花在 stream.Next() 上的时间。
	// 把「慢」归因到网络还是本地处理，靠的就是它和总耗时的差。
	readNanos atomic.Int64
	// readBytes 累计从流里读到的字节数，用来区分「行大」还是「往返多」。
	readBytes atomic.Int64
	// cancelled 用户按了「停止」。必须是原子量：设它的时候
	// c.mu 正被那个要停的 Fetch 握着。
	cancelled atomic.Bool

	// onProgress 流式推进过程中的进度回调。在 c.mu 内调用，必须立刻返回。
	onProgress atomic.Pointer[func(ReadProgress)]

	// 只读元信息，供上层展示与「可编辑判定」。
	Meta CursorMeta
}

// ReadProgress 一次流式推进的中途快照。
type ReadProgress struct {
	// Rows 已读到的行数。
	Rows int64 `json:"rows"`
	// Want 本次推进的目标行数。
	Want int64 `json:"want"`
	// Bytes 已读到的字节数。
	Bytes int64 `json:"bytes"`
	// ReadMS 其中花在等服务端出数上的毫秒数。
	ReadMS int64 `json:"readMs"`
}

// CursorMeta 结果集的附带信息。
type CursorMeta struct {
	// Query 产生该结果集的语句。
	Query string `json:"query"`
	// Ref 若结果集直接来自单表，则记录该表，用于就地编辑。
	Ref dbx.ObjectRef `json:"ref"`
	// Editable 结果集是否可就地编辑。
	Editable bool `json:"editable"`
	// EditableReason 不可编辑时的原因说明。
	EditableReason string `json:"editableReason"`
	// DurationMS 语句执行耗时。
	DurationMS int64 `json:"durationMs"`
	// ConnectionID 所属连接。
	ConnectionID string `json:"connectionId"`
	// Database 执行时所在的库。
	Database string `json:"database"`
}

// Page 一次取数的结果。
type Page struct {
	Columns []dbx.ColumnMeta `json:"columns"`
	Rows    []dbx.Row        `json:"rows"`
	Offset  int64            `json:"offset"`
	// Loaded 当前已从服务端拉取的行数。
	Loaded int64 `json:"loaded"`
	// Total 总行数；未知时为 -1。
	Total int64 `json:"total"`
	// Complete 为真表示 Loaded 即为全部行数。
	Complete bool `json:"complete"`
	// LimitReached 触达 MaxRows 保护上限。
	LimitReached bool `json:"limitReached"`
	// Stopped 用户主动中断了取数；已返回的行仍然有效。
	Stopped bool `json:"stopped"`
	// Error 流式读取过程中的错误。
	Error string `json:"error,omitempty"`
}

// Stats 游标的资源占用快照，用于状态栏与诊断面板。
type Stats struct {
	ID string `json:"id"`
	// Loaded 已拉取行数。
	Loaded int64 `json:"loaded"`
	// MemoryBytes 当前常驻内存。
	MemoryBytes int64 `json:"memoryBytes"`
	// PeakMemoryBytes 峰值常驻内存。
	PeakMemoryBytes int64 `json:"peakMemoryBytes"`
	// SpillBytes 溢出文件大小，0 表示未发生溢出。
	SpillBytes int64 `json:"spillBytes"`
	// ResidentChunks 常驻块数。
	ResidentChunks int  `json:"residentChunks"`
	Complete       bool `json:"complete"`
	// IdleSeconds 闲置时长。
	IdleSeconds int64 `json:"idleSeconds"`
	// ReadMS 累计等服务端出数的毫秒数。
	ReadMS int64 `json:"readMs"`
	// ReadBytes 累计读到的字节数。
	ReadBytes int64 `json:"readBytes"`
}

// NewCursor 基于一个行流创建游标。
func NewCursor(id string, stream dbx.RowStream, opts Options, meta CursorMeta) *Cursor {
	opts = opts.normalize()
	now := time.Now()
	c := &Cursor{
		id:         id,
		stream:     stream,
		cols:       stream.Columns(),
		opts:       opts,
		chunks:     make(map[int64]*chunk),
		lastAccess: now,
		createdAt:  now,
		Meta:       meta,
	}
	c.statAccess.Store(now.UnixNano())
	return c
}

// Cancel 中断正在进行的取数。
//
// 全程不碰 c.mu——要停的那个 Fetch 正握着它。先立起标志位让循环退出，
// 再把底层流掐掉让卡住的 Next() 返回；两件事都必须能在持锁者之外做到。
func (c *Cursor) Cancel() {
	c.cancelled.Store(true)
	if cc, ok := c.stream.(dbx.Canceller); ok {
		cc.Cancel()
	}
}

// SetProgress 注册流式读取的进度回调，传 nil 取消。
func (c *Cursor) SetProgress(fn func(ReadProgress)) {
	if fn == nil {
		c.onProgress.Store(nil)
		return
	}
	c.onProgress.Store(&fn)
}

// syncStats 把锁内状态同步到无锁快照。调用时必须持有 c.mu。
func (c *Cursor) syncStats() {
	c.statMem.Store(c.memBytes)
	c.statRows.Store(c.fetched)
	c.statPeak.Store(c.peakMem)
	c.statChunks.Store(int64(len(c.chunks)))
	c.statDone.Store(c.eof && c.streamErr == nil && !c.hitRowLimit && !c.stopped)
	if c.spill != nil {
		c.statSpill.Store(c.spill.sizeOnDisk())
	} else {
		c.statSpill.Store(0)
	}
}

func (c *Cursor) touch() {
	now := time.Now()
	c.lastAccess = now
	c.statAccess.Store(now.UnixNano())
}

// ID 游标标识。
func (c *Cursor) ID() string { return c.id }

// Columns 结果集列定义。
func (c *Cursor) Columns() []dbx.ColumnMeta { return c.cols }

// Fetch 取一段行。offset 可以任意后退，已被驱逐的块会从溢出文件重建。
func (c *Cursor) Fetch(offset, limit int64) (*Page, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("结果集已关闭")
	}
	c.touch()
	defer c.syncStats()

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}

	// 只拉到需要的位置为止，后面的行永远不碰。
	if err := c.advanceTo(offset + limit); err != nil {
		return nil, err
	}

	end := offset + limit
	if end > c.fetched {
		end = c.fetched
	}

	page := &Page{
		Columns:      c.cols,
		Offset:       offset,
		Loaded:       c.fetched,
		Total:        -1,
		Complete:     c.eof && c.streamErr == nil && !c.hitRowLimit && !c.stopped,
		LimitReached: c.hitRowLimit,
		Stopped:      c.stopped,
	}
	if page.Complete {
		page.Total = c.fetched
	}
	if c.streamErr != nil {
		page.Error = c.streamErr.Error()
	}
	if offset >= end {
		page.Rows = []dbx.Row{}
		return page, nil
	}

	rows := make([]dbx.Row, 0, end-offset)
	for i := offset; i < end; {
		cIdx := i / int64(c.opts.ChunkRows)
		ck, err := c.ensureChunk(cIdx)
		if err != nil {
			return nil, err
		}
		base := cIdx * int64(c.opts.ChunkRows)
		from := int(i - base)
		to := len(ck.rows)
		if base+int64(to) > end {
			to = int(end - base)
		}
		rows = append(rows, ck.rows[from:to]...)
		i = base + int64(to)
	}
	page.Rows = rows
	return page, nil
}

// advanceTo 把流向前推进到至少 target 行。
//
// 这里逐行给 stream.Next() 单独计时。看上去啰嗦，但它是唯一能把
// 「慢」讲清楚的地方：同样是读 200 行花了 30 秒，readNanos 接近 30 秒说明
// 是服务端/链路在出数，远小于 30 秒则说明时间花在了本机的行处理上。
// 没有这个数，只能靠猜。
func (c *Cursor) advanceTo(target int64) error {
	if c.opts.MaxRows > 0 && target > c.opts.MaxRows {
		target = c.opts.MaxRows
	}
	var (
		startRows = c.fetched
		tick      = time.Now()
	)
	for c.fetched < target && !c.eof {
		if c.cancelled.Load() {
			c.stopped = true
			c.eof = true
			break
		}
		t0 := time.Now()
		row, err := c.stream.Next()
		c.readNanos.Add(int64(time.Since(t0)))
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.eof = true
				break
			}
			// 被停止时驱动一定会报错（context canceled 之类），
			// 那不是故障，不该以红色错误的形式甩给用户。
			if c.cancelled.Load() {
				c.stopped = true
				c.eof = true
				break
			}
			c.streamErr = err
			c.eof = true
			break
		}
		if err := c.appendRow(row); err != nil {
			c.streamErr = err
			c.eof = true
			return err
		}
		if c.opts.MaxRows > 0 && c.fetched >= c.opts.MaxRows {
			c.hitRowLimit = true
			c.eof = true
			break
		}
		// 慢链路上这一圈能跑几十秒，中途必须让上层知道进度，
		// 否则界面只能干转圈，用户不知道是在动还是卡死了。
		if time.Since(tick) >= progressInterval {
			tick = time.Now()
			c.syncStats()
			c.reportProgress(target)
		}
	}
	if c.fetched != startRows {
		c.syncStats()
		c.reportProgress(target)
	}
	return nil
}

// progressInterval 进度上报的最小间隔。太密会淹没事件通道，太疏则看着像卡住。
const progressInterval = 120 * time.Millisecond

func (c *Cursor) reportProgress(want int64) {
	fn := c.onProgress.Load()
	if fn == nil {
		return
	}
	(*fn)(ReadProgress{
		Rows:   c.fetched,
		Want:   want,
		Bytes:  c.readBytes.Load(),
		ReadMS: c.readNanos.Load() / int64(time.Millisecond),
	})
}

// appendRow 接纳一行新数据。
func (c *Cursor) appendRow(row dbx.Row) error {
	// 溢出已启用时，先落盘拿到位置，内存块随时可丢。
	if c.spill != nil {
		loc, err := c.spill.append(row)
		if err != nil {
			return err
		}
		c.locs = append(c.locs, loc)
	}

	idx := c.fetched / int64(c.opts.ChunkRows)
	ck := c.chunks[idx]
	if ck == nil {
		ck = &chunk{idx: idx, rows: make([]dbx.Row, 0, c.opts.ChunkRows)}
		c.chunks[idx] = ck
	}
	sz := row.ApproxBytes()
	c.readBytes.Add(sz)
	ck.rows = append(ck.rows, row)
	ck.bytes += sz
	c.clock++
	ck.lastUse = c.clock
	c.memBytes += sz
	if c.memBytes > c.peakMem {
		c.peakMem = c.memBytes
	}
	c.fetched++

	if c.memBytes > c.opts.MemSoftLimit {
		if err := c.enableSpill(); err != nil {
			return err
		}
		c.evictLocked(c.opts.MemSoftLimit)
	}
	return nil
}

// enableSpill 首次超限时启用磁盘溢出，并把内存中已有的行补写进文件。
//
// 在此之前一行都不写磁盘：绝大多数日常操作（看某张表的前几百行）
// 全程不产生任何 IO，只有真正的大结果集才会付出磁盘代价。
func (c *Cursor) enableSpill() error {
	if c.spill != nil {
		return nil
	}
	sf, err := newSpillFile(c.opts.SpillDir, c.id)
	if err != nil {
		return err
	}
	c.spill = sf
	c.locs = make([]rowLoc, 0, c.fetched+int64(c.opts.ChunkRows))

	// 已有的块必然是从 0 开始连续的（溢出前不驱逐），按行序补写。
	total := c.fetched
	for i := int64(0); i < total; i++ {
		ck := c.chunks[i/int64(c.opts.ChunkRows)]
		if ck == nil {
			c.spill.close()
			c.spill = nil
			c.locs = nil
			return errors.New("内部错误: 启用溢出时缺失内存块")
		}
		loc, err := c.spill.append(ck.rows[i%int64(c.opts.ChunkRows)])
		if err != nil {
			c.spill.close()
			c.spill = nil
			c.locs = nil
			return err
		}
		c.locs = append(c.locs, loc)
	}
	return nil
}

// ensureChunk 取得某个块，必要时从溢出文件重建。
func (c *Cursor) ensureChunk(idx int64) (*chunk, error) {
	if ck := c.chunks[idx]; ck != nil {
		c.clock++
		ck.lastUse = c.clock
		return ck, nil
	}
	if c.spill == nil {
		return nil, errors.New("内部错误: 请求的数据块既不在内存也无溢出副本")
	}

	base := idx * int64(c.opts.ChunkRows)
	end := base + int64(c.opts.ChunkRows)
	if end > int64(len(c.locs)) {
		end = int64(len(c.locs))
	}
	if base >= end {
		return nil, errors.New("请求的数据块超出已加载范围")
	}

	ck := &chunk{idx: idx, rows: make([]dbx.Row, 0, end-base)}
	for i := base; i < end; i++ {
		row, buf, err := c.spill.readAt(c.locs[i], c.readBuf)
		c.readBuf = buf
		if err != nil {
			return nil, err
		}
		ck.rows = append(ck.rows, row)
		ck.bytes += row.ApproxBytes()
	}
	c.clock++
	ck.lastUse = c.clock
	c.chunks[idx] = ck
	c.memBytes += ck.bytes
	if c.memBytes > c.peakMem {
		c.peakMem = c.memBytes
	}
	// 新块进来可能再次撑破预算，立刻回收最久未用的块。
	c.evictLocked(c.opts.MemSoftLimit)
	return ck, nil
}

// evictLocked 驱逐最久未使用的块直到内存回到 target 以下。
// 仅在溢出启用后可调用，否则会丢失唯一副本。
func (c *Cursor) evictLocked(target int64) {
	if c.spill == nil || c.memBytes <= target {
		return
	}
	// 块数很少，直接线性挑最旧的即可，无需维护额外的 LRU 链表。
	for c.memBytes > target && len(c.chunks) > c.opts.MinResidentChunks {
		var victim *chunk
		for _, ck := range c.chunks {
			if victim == nil || ck.lastUse < victim.lastUse {
				victim = ck
			}
		}
		if victim == nil {
			return
		}
		delete(c.chunks, victim.idx)
		c.memBytes -= victim.bytes
	}
}

// Trim 由 Manager 在全局内存吃紧时调用，把该游标压缩到尽可能小。
// 若尚未启用溢出，会先落盘再释放，保证数据不丢。
func (c *Cursor) Trim() {
	// 拿不到锁说明这个游标此刻正在读流——慢链路上那一握可能是几十秒。
	// 内存回收是尽力而为的事，为它去排队会把调用方（往往是另一个标签页的
	// 打开操作）一起堵死，所以直接跳过，等下一轮再说。
	if !c.mu.TryLock() {
		return
	}
	defer c.mu.Unlock()
	if c.closed || c.fetched == 0 {
		return
	}
	defer c.syncStats()
	if c.spill == nil {
		if err := c.enableSpill(); err != nil {
			// 落盘失败就保持现状，宁可占内存也不能丢数据。
			return
		}
	}
	c.evictLocked(0)
}

// CountRemaining 把流读到底以取得精确总行数。
// 数据仍然只进溢出文件，不会把内存拉爆。
func (c *Cursor) CountRemaining() (int64, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, false, errors.New("结果集已关闭")
	}
	c.touch()
	defer c.syncStats()
	limit := c.opts.MaxRows
	if limit <= 0 {
		limit = int64(1) << 62
	}
	if err := c.advanceTo(limit); err != nil {
		return c.fetched, false, err
	}
	return c.fetched, c.eof && !c.hitRowLimit && !c.stopped && c.streamErr == nil, c.streamErr
}

// Stats 资源占用快照。全程读原子量，不碰 c.mu——
// 状态栏每两秒刷一次，绝不能因为某个游标正在慢慢读流就跟着卡住。
func (c *Cursor) Stats() Stats {
	return Stats{
		ID:              c.id,
		Loaded:          c.statRows.Load(),
		MemoryBytes:     c.statMem.Load(),
		PeakMemoryBytes: c.statPeak.Load(),
		SpillBytes:      c.statSpill.Load(),
		ResidentChunks:  int(c.statChunks.Load()),
		Complete:        c.statDone.Load(),
		IdleSeconds:     int64(time.Since(time.Unix(0, c.statAccess.Load())).Seconds()),
		ReadMS:          c.readNanos.Load() / int64(time.Millisecond),
		ReadBytes:       c.readBytes.Load(),
	}
}

// memoryBytes 供 Manager 读取，同样不加锁。
func (c *Cursor) memoryBytes() int64 { return c.statMem.Load() }

func (c *Cursor) idleSince() time.Time { return time.Unix(0, c.statAccess.Load()) }

// Close 释放流、内存与溢出文件。
func (c *Cursor) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	err := c.stream.Close()
	c.chunks = nil
	c.memBytes = 0
	c.locs = nil
	c.readBuf = nil
	if c.spill != nil {
		c.spill.close()
		c.spill = nil
	}
	c.onProgress.Store(nil)
	c.syncStats()
	return err
}

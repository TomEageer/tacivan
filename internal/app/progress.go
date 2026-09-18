package app

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"tacivan/internal/rs"
)

// ProgressEvent 一次长操作的中途播报。
//
// 做这个是因为「转圈」这件事本身不含任何信息：用户看不出是在等服务端出数、
// 在等表结构，还是已经卡死了。慢实例上一次打开可能要几十秒，
// 这几十秒里界面必须能说清自己在干什么、干了多久。
type ProgressEvent struct {
	// Token 由前端生成，用来把事件对上发起它的那个标签页。
	Token string `json:"token"`
	// Stage 当前步骤的短名，例如「表结构」「查询」「取数」。
	Stage string `json:"stage"`
	// Detail 补充说明，例如「已读 120 行 / 1.4 MB」。
	Detail string `json:"detail,omitempty"`
	// ElapsedMS 从操作开始算起的毫秒数。
	ElapsedMS int64 `json:"elapsedMs"`
	// Done 为真表示该 Token 的操作已结束，前端可以收起进度。
	Done bool `json:"done"`
}

// progressEventName 所有长操作共用一个事件名，前端按 Token 过滤。
const progressEventName = "op:progress"

// inflight 按 Token 登记可被中断的长操作。
//
// 用 Token 而不是结果集 ID 做键，是因为界面在操作开始时还拿不到结果集 ID——
// 游标是在操作中途才建出来的，而「停止」按钮从第一秒起就得能按。
var (
	inflightMu sync.Mutex
	inflight   = map[string]func(){}
)

// CancelOperation 中断一个还在进行的长操作。token 由前端发起时生成。
//
// 已经读到的数据不丢：中断的是「继续往下读」，不是整个结果集。
func (a *App) CancelOperation(token string) error {
	inflightMu.Lock()
	fn := inflight[token]
	inflightMu.Unlock()
	if fn == nil {
		return fmt.Errorf("该操作已经结束")
	}
	fn()
	return nil
}

// reporter 一次长操作的进度播报器。Token 为空时全部调用都是空转，
// 这样调用方不需要到处写 if。
type reporter struct {
	app   *App
	token string
	start time.Time
	// last 上一次实际发出的时间，用于限流。
	last atomic.Int64
	// closed 防止 Done 之后还有迟到的流式回调再发事件。
	closed atomic.Bool
}

func (a *App) reporterFor(token string) *reporter {
	return &reporter{app: a, token: token, start: time.Now()}
}

// stage 播报进入了一个新步骤，不限流。
func (r *reporter) stage(name, detail string) {
	r.emit(name, detail, false, true)
}

// tick 播报同一步骤内的进展，会限流。
func (r *reporter) tick(name, detail string) {
	r.emit(name, detail, false, false)
}

// bindCancel 登记本次操作的中断方式，让界面上的「停止」能按下去。
func (r *reporter) bindCancel(fn func()) {
	if r == nil || r.token == "" || fn == nil {
		return
	}
	inflightMu.Lock()
	inflight[r.token] = fn
	inflightMu.Unlock()
}

// done 播报操作结束，并注销中断入口。
func (r *reporter) done() {
	if r == nil {
		return
	}
	if r.token != "" {
		inflightMu.Lock()
		delete(inflight, r.token)
		inflightMu.Unlock()
	}
	if r.closed.Swap(true) {
		return
	}
	r.emit("", "", true, true)
}

func (r *reporter) emit(stage, detail string, done, force bool) {
	if r == nil || r.token == "" || r.app == nil || r.app.ctx == nil {
		return
	}
	if !done && r.closed.Load() {
		return
	}
	now := time.Now()
	if !force {
		// 事件本身要过一次 JS 桥，发太密了反而拖慢被观测的操作。
		const minGap = int64(120 * time.Millisecond)
		prev := r.last.Load()
		if prev != 0 && now.UnixNano()-prev < minGap {
			return
		}
	}
	r.last.Store(now.UnixNano())
	wr.EventsEmit(r.app.ctx, progressEventName, ProgressEvent{
		Token:     r.token,
		Stage:     stage,
		Detail:    detail,
		ElapsedMS: now.Sub(r.start).Milliseconds(),
		Done:      done,
	})
}

// attach 把游标的流式读取进度接到播报器上。
func (r *reporter) attach(c *rs.Cursor, stage string) {
	if r == nil || r.token == "" {
		return
	}
	c.SetProgress(func(p rs.ReadProgress) {
		r.tick(stage, describeRead(p))
	})
}

func describeRead(p rs.ReadProgress) string {
	s := formatCount(p.Rows) + " 行"
	if p.Want > 0 && p.Want < 1<<62 {
		s += " / " + formatCount(p.Want)
	}
	s += "，" + formatSize(p.Bytes)
	if p.ReadMS > 0 {
		s += "，其中等服务端 " + formatMS(p.ReadMS)
	}
	return s
}

func formatSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	}
}

func formatMS(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%d ms", ms)
	}
	return fmt.Sprintf("%.1f s", float64(ms)/1000)
}

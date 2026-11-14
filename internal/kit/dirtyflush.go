package kit

import (
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================================
// 热路径变更的合批落盘器
//
// 各平台的 token 记账发生在每个请求的热路径上：就地改内存表很便宜，但每次都
// 重写整个配置文件（marshal + 临时文件 + rename）太重。记完只打一个脏标记，
// 由后台 goroutine 周期性写盘，是 cline 账号池 markPoolDirtyLocked +
// startPoolFlusher 的同一套思路，抽到这里给所有平台复用：
//
//   - Mark()：热路径调用，只置位，不写盘、不阻塞；
//   - 后台 goroutine：到点取走脏标记，有变更才调 save（save 由调用方注入，
//     必须自带串行保护与锁序，本工具不持任何调用方的锁）。
//
// 语义取舍：进程被 kill（没有优雅退出）时，最多丢最后 <every> 内的记账。这是
// 记账而非金融流水，接受这个窗口；不接受的是"重启后整列清零"。
// ============================================================================

// DirtyFlusher 一个配置文件的合批落盘器。零值不可用，用 NewDirtyFlusher 构造。
type DirtyFlusher struct {
	mu    sync.Mutex
	dirty bool
	save  func()
	every time.Duration
	once  sync.Once
}

// suspendedForTest 测试挂起开关：置位后 Mark 不再启动周期 goroutine，已在跑的
// 循环也不再写盘。测试二进制里包级 flusher 是泄漏 goroutine——首次 Mark 惰性
// 启动、无停止路径，测试结束后仍会按周期向「当前 DATA_DIR」（随 t.Setenv 逐
// 测试变化）写盘，与任何正在清理 t.TempDir 的测试竞态（RemoveAll: directory
// not empty，CI 2026-11-12 0aaefa91 与 2026-11-14 ea0c2374 同型两例）。测试
// 不依赖周期写盘语义——脏标记照置、显式 Flush 与同步保存点照写。
var suspendedForTest atomic.Bool

// SuspendFlushersForTest 挂起所有 DirtyFlusher 的周期写盘（仅测试用，
// 在 TestMain 里 m.Run() 之前调用）。
func SuspendFlushersForTest() { suspendedForTest.Store(true) }

// NewDirtyFlusher 构造落盘器。save 必须是"整体写盘"函数（内部自带串行化），
// 且调用时不能持有调用方的配置锁——它自己会去取。
func NewDirtyFlusher(every time.Duration, save func()) *DirtyFlusher {
	if every <= 0 {
		every = 10 * time.Second
	}
	return &DirtyFlusher{every: every, save: save}
}

// Mark 标记有未落盘变更，并惰性启动后台 goroutine（首次调用时启动一次）。
// 挂起态（SuspendFlushersForTest）下只置脏位，不启动周期写盘。
func (f *DirtyFlusher) Mark() {
	f.mu.Lock()
	f.dirty = true
	f.mu.Unlock()
	if !suspendedForTest.Load() {
		f.once.Do(func() { go f.loop() })
	}
}

// Flush 立即写盘（外部显式触发，如优雅退出/管理操作）。无变更时直接返回。
func (f *DirtyFlusher) Flush() {
	f.mu.Lock()
	dirty := f.dirty
	f.dirty = false
	f.mu.Unlock()
	if dirty && f.save != nil {
		f.save()
	}
}

func (f *DirtyFlusher) loop() {
	t := time.NewTicker(f.every)
	defer t.Stop()
	for range t.C {
		if suspendedForTest.Load() {
			continue // 挂起态只空转不写盘（对挂起前已启动的循环兜底）
		}
		f.Flush()
	}
}

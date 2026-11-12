// idle.go 流中空闲监控（网关 keypool 平台共享件）：活跃吐数据续命不掐，
// 静默超过阈值才 cancel 请求 context，中断阻塞中的 Read。
// 挂起形态实测：上游发完响应头后 body 永久停滞（2026-11-12 生产 amd 路径
// 22 分钟挂起、received=0），ResponseHeaderTimeout 只管首字节阶段管不到它。
// 语义与 internal/workbuddy/upstream/idle.go、internal/trae2api/traectl/idle.go
// 同源（vendor 树各自保留，非 vendor 的 keypool 四包共用本实现）。
package kit

import (
	"context"
	"io"
	"sync"
	"time"
)

// tailDrainMax / tailDrainWait Close 前读掉流尾的上限：读到 EOF 连接才回连接池。
// 读者在 [DONE] 处就停了，上游随后才发 chunked 结束块（几个字节）；直接取消
// context 会让 Transport 丢弃连接，流式请求每次都要重新 TCP+TLS（经代理时还要
// 再建一次隧道）。流没结束时最多多读 4KB、多等 100ms 就照旧取消、关闭。
const (
	tailDrainMax  = 4 << 10
	tailDrainWait = 100 * time.Millisecond
)

// idleBody 包在上游响应 body 外层：每次读到底层数据（n>0）就刷新 lastRead；
// 后台 goroutine 周期检查，静默超过 idle 就 cancel，打断阻塞中的 Read。
type idleBody struct {
	rc       io.ReadCloser
	mu       sync.Mutex
	lastRead time.Time
	stopCh   chan struct{}
	cancel   context.CancelFunc

	closeOnce sync.Once
	closeErr  error
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.mu.Lock()
		b.lastRead = time.Now()
		b.mu.Unlock()
	}
	return n, err
}

// Close 停掉后台 goroutine，读掉流尾让连接可复用，再取消请求 context、关闭底流，
// 保证无泄漏。幂等：只有第一次真正执行（调用链里常有两层各关一次）。
func (b *idleBody) Close() error {
	b.closeOnce.Do(func() { b.closeErr = b.close() })
	return b.closeErr
}

func (b *idleBody) close() error {
	close(b.stopCh)
	drained := make(chan struct{})
	go func() {
		_, _ = io.CopyN(io.Discard, b.rc, tailDrainMax)
		close(drained)
	}()
	t := time.NewTimer(tailDrainWait)
	select {
	case <-drained:
	case <-t.C:
	}
	t.Stop()
	b.cancel()
	return b.rc.Close()
}

func (b *idleBody) idleFor() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Since(b.lastRead)
}

// MonitorBody 若 idle<=0 或 cancel 为 nil 直接返回原底流（禁用空闲监控，零开销）；
// 否则包上流中空闲监控。计时从包装那一刻开始——响应头阶段由
// Transport.ResponseHeaderTimeout 管，这里不抢跑。
func MonitorBody(rc io.ReadCloser, idle time.Duration, cancel context.CancelFunc) io.ReadCloser {
	if idle <= 0 || cancel == nil {
		return rc
	}
	b := &idleBody{
		rc:       rc,
		lastRead: time.Now(),
		stopCh:   make(chan struct{}),
		cancel:   cancel,
	}
	go func() {
		t := time.NewTicker(idleTick(idle))
		defer t.Stop()
		for {
			select {
			case <-b.stopCh:
				return
			case <-t.C:
				if b.idleFor() > idle {
					cancel()
					return
				}
			}
		}
	}()
	return b
}

// idleTick 返回监控周期：idle/4，钳在 [10ms, 1s]。小 idle 也能快速发现，大值避免空转。
func idleTick(idle time.Duration) time.Duration {
	d := idle / 4
	if d > time.Second {
		d = time.Second
	}
	if d < 10*time.Millisecond {
		d = 10 * time.Millisecond
	}
	return d
}

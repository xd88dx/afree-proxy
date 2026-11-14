package app

import (
	"os"
	"testing"

	"afree-proxy/internal/kit"
)

// 包级合批落盘器（zen/amd/oprt/tkhb/other 的 *-state.json）首次 Mark 惰性启动
// 10s 周期 goroutine 且无停止路径：某测试 mark 过脏位后，这个泄漏 goroutine
// 会在其后任意测试的 t.Setenv("DATA_DIR", t.TempDir()) 窗口内向该 TempDir 写盘，
// 与 TempDir 清理竞态（RemoveAll: directory not empty）——CI 上失败被记到当时
// 正在清理的测试头上（TestServedModelGate，2026-11-12 0aaefa91 与 2026-11-14
// ea0c2374 同型两例）。测试不依赖周期写盘语义，整包挂起；显式 Flush 与同步
// 保存点不受影响。
func TestMain(m *testing.M) {
	kit.SuspendFlushersForTest()
	os.Exit(m.Run())
}

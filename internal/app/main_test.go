package app

import (
	"os"
	"testing"
	"time"

	"afree-proxy/internal/kit"
)

// 包级合批落盘器（zen/amd/oprt/tkhb/other 的 *-state.json）首次 Mark 惰性启动
// 10s 周期 goroutine 且无停止路径：某测试 mark 过脏位后，这个泄漏 goroutine
// 会在其后任意测试的 t.Setenv("DATA_DIR", testDataDir(t)) 窗口内向该 TempDir 写盘，
// 与 TempDir 清理竞态（RemoveAll: directory not empty）——CI 上失败被记到当时
// 正在清理的测试头上（TestServedModelGate，2026-11-12 0aaefa91 与 2026-11-14
// ea0c2374 同型两例）。测试不依赖周期写盘语义，整包挂起；显式 Flush 与同步
// 保存点不受影响。
func TestMain(m *testing.M) {
	kit.SuspendFlushersForTest()
	os.Exit(m.Run())
}

// testDataDir 替代 t.TempDir() 作为 DATA_DIR 载体：包内存在测试结束后仍在落盘
// 的泄漏 goroutine（reqlog 写协程的队列余量、zen 410 迁移持久化等），它们在
// 「解析路径（env 未还原）→ open 文件（清理进行中）」的微秒窗口里创建条目，
// 与 testing 对 TempDir 的一次性 RemoveAll 竞态（directory not empty）——CI
// 同型失败三例（2026-11-12 0aaefa91、2026-11-14 两次），失败都记在当刻正在
// 清理的测试头上（TestServedModelGate 中招两次）。自建目录 + 带重试的清理
// 绕开该竞态；重试耗尽则留目录打日志（CI runner 一次性环境，可容忍）。
func testDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "afree-data-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 10; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Logf("testDataDir: 清理重试耗尽（疑似仍有泄漏写者），目录保留: %s", dir)
	})
	return dir
}

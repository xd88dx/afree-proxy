package zen

import (
	"os"
	"testing"
	"time"

	"afree-proxy/internal/exitpool"
)

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

// ClearTestProxyPool 清空代理池（测试隔离；与 app clearTestProxyPool 同语义）。
func ClearTestProxyPool(t *testing.T) {
	t.Helper()
	saved := exitpool.GetPool()
	empty := *saved
	empty.Proxies = nil
	exitpool.ActivatePool(&empty)
	t.Cleanup(func() { exitpool.ActivatePool(saved) })
}

package openrouter

import (
	"path/filepath"
	"testing"
	"time"
)

// TestUncoolNoDeadlock 回归：Uncool 曾在持 mu.Lock 期间调用 Get()（RLock），
// 对不可重入的 RWMutex 自死锁，毒化整个包的锁（面板 test 按钮卡死）。
// 修复后直接删 cfg.Cool；本用例在 5s 内不返回即失败（go test 超时兜底）。
func TestUncoolNoDeadlock(t *testing.T) {
	Load(filepath.Join(t.TempDir(), ".cfg-test.json"))
	Set(&Config{Keys: []string{"k1"}})
	Cooldown("k1", time.Minute)
	done := make(chan struct{})
	go func() {
		Uncool("k1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Uncool deadlocked (Lock inside Lock?)")
	}
	if _, ok := CooldownUntil("k1"); ok {
		t.Fatal("cooldown should be cleared after Uncool")
	}
}

package other

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadWithExistingConfigDoesNotDeadlock 回归：Load 持 mu 写锁时若再调
// Get()（内部 mu.RLock）会在同一 goroutine 上自死锁——Go 的 RWMutex 不可重入。
// 症状是 initOther() 永久阻塞：进程存活但从不监听端口、启动横幅也永不打印。
//
// 旧实现里 rebuildAllSem() 走 Get()，只有 .other-config.json 已存在时才会
// 走到（文件不存在时 os.ReadFile 提前 return），所以空目录启动测不出来。
// 本测试写入真实配置文件后调 Load，必须在超时内返回。
func TestLoadWithExistingConfigDoesNotDeadlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".other-config.json")

	raw := Config{Platforms: []Platform{{
		Slug:           "acme",
		Name:           "Acme",
		Prefix:         "acme",
		BaseURL:        "http://127.0.0.1:9/v1",
		Keys:           []string{"sk-1"},
		MaxConcurrency: 8,
		Retries:        3,
	}}}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// 复位包级状态，保证 Load 真读到文件（loaded 标记是 once 语义）。
	mu.Lock()
	savedCfg, savedPath, savedLoaded := cfg, ConfigPath, loaded
	cfg = &Config{}
	ConfigPath = ""
	loaded = false
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		cfg, ConfigPath, loaded = savedCfg, savedPath, savedLoaded
		mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		Load(path)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Load deadlocked on an existing config file: mu is held for writing " +
			"and rebuildAllSem re-enters it via Get()'s RLock")
	}

	// 不只要返回，还要真的加载成功并重建出子平台信号量。
	got := Get()
	if len(got.Platforms) != 1 || got.Platforms[0].Slug != "acme" {
		t.Fatalf("config not loaded: %+v", got.Platforms)
	}
	semMu.Lock()
	defer semMu.Unlock()
	if _, ok := sems["acme"]; !ok {
		t.Fatal("per-platform semaphore not rebuilt after Load")
	}
}

// TestSetRebuildsSemaphore Set 路径同样要重建信号量，且不得死锁。
func TestSetRebuildsSemaphore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)

	mu.Lock()
	savedCfg, savedPath, savedLoaded := cfg, ConfigPath, loaded
	cfg = &Config{}
	ConfigPath = filepath.Join(dir, ".other-config.json")
	loaded = true
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		cfg, ConfigPath, loaded = savedCfg, savedPath, savedLoaded
		mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		Set(&Config{Platforms: []Platform{{
			Slug: "beta", Name: "Beta", Prefix: "beta", MaxConcurrency: 4,
		}}})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Set deadlocked")
	}

	semMu.Lock()
	defer semMu.Unlock()
	if _, ok := sems["beta"]; !ok {
		t.Fatal("per-platform semaphore not rebuilt after Set")
	}
}

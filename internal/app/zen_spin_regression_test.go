package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 2026-11-06 生产实证回归：绑定出口在请求中途进入冷却后，callZenAPI 的
// boundExitFor 失败分支没有清空 retryKey——下一轮跳过 pickZenKey 原样复用
// 同一个 key，无睡眠、无上限地自旋（生产观测：10 分钟出口冷却期内刷出
// 30 万+ 行 "bound proxies unavailable" 日志并打满一核）。callZenResponsesAPI
// 同分支已有 retryKey = ""，本测试把 chat 路径钉在同一语义上。
//
// 模拟生产序列：出口存活时选中 key → 两次拨号失败（2-strike 冷却出口）→
// 下一轮 boundExitFor 失败 → 修复后应冷却该 key 并换 key/快速报错返回，
// 而不是自旋。断言调用在有界时间内返回且上游零命中。
func TestZenChatBlockedExitSwitchesKeyInsteadOfSpinning(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	// 单 key，绑定出口指向一个必然拨号失败的地址（127.0.0.1:1 连接拒绝）。
	// 出口初始存活：两次拨号失败经 recordProxyOutcome 2-strike 冷却后，
	// boundExitFor 才开始失败——复现"选中后再坏"的生产时序。
	setupBindingTest(t, &zenTestConfig{
		Keys:           []string{"sk-1"},
		BaseURL:        upstream.URL,
		Retries:        3,
		Proxies:        []string{"http://127.0.0.1:1"},
		KeyBindings:    map[string]proxyBinding{"sk-1": {Main: "http://127.0.0.1:1"}},
		ProxyIsolation: boolPtr(true),
	})
	zenKeyMu.Lock()
	zenKeyIdx = 0
	zenKeyMu.Unlock()
	t.Cleanup(func() {
		zenKeyMu.Lock()
		zenKeyCool = map[string]time.Time{}
		zenKeyMu.Unlock()
	})

	setZenModelForTest("aaa-spin-model", "")

	params := map[string]any{
		"model":    "aaa-spin-model",
		"messages": []any{map[string]any{"role": "user", "content": "x"}},
	}

	type outcome struct {
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		_, _, err := callZenAPI(context.Background(), params, true)
		ch <- outcome{err: err}
	}()
	select {
	case got := <-ch:
		if got.err == nil {
			t.Fatal("expected an error (no routable exit), got nil")
		}
		t.Logf("callZenAPI returned as expected: %v", got.err)
	case <-time.After(45 * time.Second):
		t.Fatal("callZenAPI did not return within 45s — bound-exit-failure branch is spinning again")
	}

	if hits != 0 {
		t.Fatalf("upstream must never be reached (all attempts die at the dial), got %d hits", hits)
	}
	if !zenKeyCooling("sk-1") {
		t.Fatal("key must carry a 2m cooldown after its bound exit became unavailable")
	}
}

// 2026-11-06 生产实证回归：固定 ".tmp" 名下两个并发写者互踩——A rename 走了
// tmp，B 的 rename 报 ENOENT、快照丢失（生产日志 zen-state.json 两次
// "replace ... no such file or directory"）。修复后临时文件按调用唯一命名，
// 并发写互不相扰。断言：并发写全程无错误日志、最终文件必为某一次写入的
// 完整内容、目录无残留 tmp。
func TestAtomicWriteFileConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	var mu sync.Mutex
	var buf strings.Builder
	log.SetOutput(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.WriteString(string(p))
	}))
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	const n = 64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			atomicWriteFile(path, []byte(fmt.Sprintf(`{"w":%d}`, i)))
		}(i)
	}
	wg.Wait()

	mu.Lock()
	logged := buf.String()
	mu.Unlock()
	if logged != "" {
		t.Fatalf("atomicWriteFile must not log errors under concurrent writers, got:\n%s", logged)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	s := string(raw)
	if !strings.HasPrefix(s, `{"w":`) || !strings.HasSuffix(s, "}") {
		t.Fatalf("final file must be exactly one writer's payload, got %q", s)
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// writerFunc 让 strings.Builder 满足 io.Writer（log 输出捕获）。
type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

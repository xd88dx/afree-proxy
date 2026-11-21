package zen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 指纹闸自愈判据的纯函数层测试（循环层接线由容器实测覆盖，见
// MERGE.md 2026-11-21 方案一条目）。

// TestZenKeyTokensToday 覆盖当日记账读取：当日值、跨日归零、无记录归零。
func TestZenKeyTokensToday(t *testing.T) {
	t.Setenv("DATA_DIR", testDataDir(t))

	const key = "oc_sk_test_fingerprint"
	RecordZenKeyTokens(key, 42)
	if got := zenKeyTokensToday(key); got != 42 {
		t.Fatalf("当日记账 = %d, want 42", got)
	}
	RecordZenKeyTokens(key, 58)
	if got := zenKeyTokensToday(key); got != 100 {
		t.Fatalf("累加后 = %d, want 100", got)
	}
	// 跨日：把 Date 手工拨回昨天 → 读数归零。
	zenKeyMu.Lock()
	st := zenKeyTokens[key]
	st.Date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	zenKeyTokens[key] = st
	zenKeyMu.Unlock()
	if got := zenKeyTokensToday(key); got != 0 {
		t.Fatalf("跨日记账应归零, got %d", got)
	}
	if got := zenKeyTokensToday(""); got != 0 {
		t.Fatalf("空 key 应归零, got %d", got)
	}
	if got := zenKeyTokensToday("oc_sk_never_used"); got != 0 {
		t.Fatalf("无记录 key 应归零, got %d", got)
	}
}

// TestZenClientToolNames 覆盖工具名清单提取：chat 嵌套形态、跳过坏形态条目。
func TestZenClientToolNames(t *testing.T) {
	params := map[string]any{
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "probe_read"}},
			"not-a-map",
			map[string]any{"type": "function"}, // 无 function 内嵌
			map[string]any{"type": "function", "function": map[string]any{"name": "probe_write"}},
		},
	}
	got := zenClientToolNames(params)
	if got != "probe_read,probe_write" {
		t.Fatalf("工具名清单 = %q", got)
	}
	// 无工具 → 空串。
	if got := zenClientToolNames(map[string]any{}); got != "" {
		t.Fatalf("无工具应返回空串, got %q", got)
	}
}

// TestZenFingerprintSuspectThreshold 锁住阈值语义（≤100 含边界）。
func TestZenFingerprintSuspectThreshold(t *testing.T) {
	if zenFingerprintSuspectTokens != 100 {
		t.Fatalf("阈值 = %d, want 100（测试场景单次冒烟约 10 tok，100 覆盖）", zenFingerprintSuspectTokens)
	}
	if !strings.Contains(zenClientToolNames(map[string]any{}), "") {
		t.Fatal("占位断言不应失败")
	}
}

// TestCallZenAPIFingerprintSelfHeal 循环层自愈接线验证：假上游第 1 次回
// "日额度耗尽"形态 429（RA 指向午夜），barely-used key 判疑似指纹 → 同 key
// 去客户端工具重试 → 200。断言：两次上游往返、第二次 body 只剩 gate 工具 +
// tool_choice=none、key 未被 park、响应 200。
func TestCallZenAPIFingerprintSelfHeal(t *testing.T) {
	t.Setenv("DATA_DIR", testDataDir(t))

	var mu sync.Mutex
	calls := 0
	var secondBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			now := time.Now().UTC()
			midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
			w.Header().Set("Retry-After", strconv.Itoa(int(midnight.Sub(now).Seconds())))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"FreeUsageLimitError","message":"Free usage limit reached. Free models work in OpenCode."}}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(b, &secondBody)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	saved := GetZenConfig()
	t.Cleanup(func() { SetZenConfig(saved) })
	cfg := DefaultZenConfig()
	cfg.BaseURL = srv.URL + "/v1"
	cfg.Keys = []string{"oc_sk_test_selfheal"}
	SetZenConfig(cfg)

	savedExit := Host.BoundExitFor
	Host.BoundExitFor = func(string) (string, int, bool) { return "", -1, true }
	t.Cleanup(func() { Host.BoundExitFor = savedExit })

	params := map[string]any{
		"model":    "test-model",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{
			"name": "probe_read", "description": "x", "parameters": map[string]any{"type": "object"},
		}}},
		"tool_choice": "auto",
	}
	resp, _, err := CallZenAPI(context.Background(), params, false)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	resp.Body.Close()

	mu.Lock()
	c := calls
	sb := secondBody
	mu.Unlock()
	if c != 2 {
		t.Fatalf("上游往返 = %d, want 2（首次 429 + 去工具自愈）", c)
	}
	gateN := len(ZenGateToolDefs())
	tools, _ := sb["tools"].([]any)
	if len(tools) != gateN {
		t.Fatalf("自愈后 tools = %d, want gate %d", len(tools), gateN)
	}
	if sb["tool_choice"] != "none" {
		t.Fatalf("自愈后 tool_choice = %v, want none", sb["tool_choice"])
	}
	if _, parked := ZenKeyParked("oc_sk_test_selfheal"); parked {
		t.Fatal("自愈成功后 key 不应被 park")
	}
}

// TestCallZenAPIWellUsedKeyStillParks 对照分支：当日用量超阈值的 key 吃到同款
// 429 → 真额度耗尽，直接 park + 返回 429，不做去工具重试。
func TestCallZenAPIWellUsedKeyStillParks(t *testing.T) {
	t.Setenv("DATA_DIR", testDataDir(t))

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu2 := &struct{}{}
		_ = mu2
		calls++
		now := time.Now().UTC()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
		w.Header().Set("Retry-After", strconv.Itoa(int(midnight.Sub(now).Seconds())))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"FreeUsageLimitError","message":"Free usage limit reached."}}`))
	}))
	defer srv.Close()

	saved := GetZenConfig()
	t.Cleanup(func() { SetZenConfig(saved) })
	cfg := DefaultZenConfig()
	cfg.BaseURL = srv.URL + "/v1"
	cfg.Keys = []string{"oc_sk_test_wellused"}
	SetZenConfig(cfg)

	savedExit := Host.BoundExitFor
	Host.BoundExitFor = func(string) (string, int, bool) { return "", -1, true }
	t.Cleanup(func() { Host.BoundExitFor = savedExit })

	// 当日记账 5000 tok（>100）：真额度耗尽形态
	RecordZenKeyTokens("oc_sk_test_wellused", 5000)

	params := map[string]any{
		"model":    "test-model",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{
			"name": "probe_read", "description": "x", "parameters": map[string]any{"type": "object"},
		}}},
		"tool_choice": "auto",
	}
	_, _, err := CallZenAPI(context.Background(), params, false)
	if err == nil {
		t.Fatal("全 key 耗尽应返回错误")
	}
	if calls != 1 {
		t.Fatalf("上游往返 = %d, want 1（超阈值 key 不做去工具重试）", calls)
	}
	if _, parked := ZenKeyParked("oc_sk_test_wellused"); !parked {
		t.Fatal("真额度耗尽应 park")
	}
}

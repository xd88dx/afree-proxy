package freepi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ============ 会话粘性测试（2026-11-25）============
//
// 覆盖：会话标识双轨（显式 ID > 首条 user 指纹）、映射 TTL/LRU/清退、
// Chat 层 sticky 锁 key 与失败快速返回（不换 key、不打第二次上游）。
// 与 freepi_test.go 同风格：直接换包级 cfg/usage 并在 Cleanup 恢复。

func TestConversationKeyExplicitIDWins(t *testing.T) {
	params := map[string]any{
		"session_id":      "body-session",
		"conversation_id": "body-conv",
		"messages":        []any{map[string]any{"role": "user", "content": "hello"}},
	}
	if got := ConversationKey(WithStickySession(context.Background(), "header-session"), params, "caller"); got != "id:header-session" {
		t.Fatalf("ctx/header ID must win, got %q", got)
	}
	if got := ConversationKey(context.Background(), params, "caller"); got != "id:body-session" {
		t.Fatalf("body session_id must beat conversation_id/fingerprint, got %q", got)
	}
	p2 := map[string]any{"conversation_id": "body-conv", "messages": params["messages"]}
	if got := ConversationKey(context.Background(), p2, "caller"); got != "id:body-conv" {
		t.Fatalf("body conversation_id must beat fingerprint, got %q", got)
	}
}

func TestConversationKeyFingerprintStableAndScoped(t *testing.T) {
	msg := func(content string) map[string]any {
		return map[string]any{"messages": []any{map[string]any{"role": "user", "content": content}}}
	}
	a := ConversationKey(context.Background(), msg("write a report"), "caller-A")
	b := ConversationKey(context.Background(), msg("write a report"), "caller-A")
	if a == "" || a != b {
		t.Fatalf("same conversation must map to same key: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "fp:") {
		t.Fatalf("fingerprint must be namespaced, got %q", a)
	}
	if other := ConversationKey(context.Background(), msg("write a report"), "caller-B"); other == a {
		t.Fatal("different caller namespace must not collide")
	}
	if other := ConversationKey(context.Background(), msg("a different task"), "caller-A"); other == a {
		t.Fatal("different user content must not collide")
	}
	sysOnly := map[string]any{"messages": []any{map[string]any{"role": "system", "content": "x"}}}
	if got := ConversationKey(context.Background(), sysOnly, "caller"); got != "" {
		t.Fatalf("no user message must yield empty key, got %q", got)
	}
}

func TestStickyBindLookupAndForget(t *testing.T) {
	saved := usage
	t.Cleanup(func() { usage = saved })
	usage = &usageData{}
	valid := map[string]bool{"k1": true, "k2": true}

	stickyBind("id:s1", "k1")
	if got := stickyLookup("id:s1", valid); got != "k1" {
		t.Fatalf("bound key not returned: %q", got)
	}
	// 2026-11-25 起不再按空闲时长淘汰：用户实测同一会话隔一天换 key 仍报
	// 归属错误，时间淘汰无意义。只要 key 仍在池中，旧条目继续命中。
	usage.StickySessions["id:s1"] = StickyEntry{Key: "k1", LastSeen: time.Now().Add(-time.Hour)}
	if got := stickyLookup("id:s1", valid); got != "k1" {
		t.Fatalf("stale-time entry must still hit while key is valid: %q", got)
	}
	// key 已不在池内 → 映射作废并清理
	stickyBind("id:s2", "k2")
	if got := stickyLookup("id:s2", map[string]bool{"k1": true}); got != "" {
		t.Fatalf("entry for removed key must not be returned: %q", got)
	}
	if _, ok := usage.StickySessions["id:s2"]; ok {
		t.Fatal("entry for removed key must be deleted")
	}
	// 空标识/空 key 不建映射
	stickyBind("", "k1")
	stickyBind("id:s3", "")
	if _, ok := usage.StickySessions["id:s3"]; ok {
		t.Fatal("empty key must not be bound")
	}
	// PruneUsage 联动：移除 k2 后只保留指向 k1 的映射
	stickyBind("id:keep", "k1")
	stickyForgetKey(map[string]bool{"k1": true})
	if _, ok := usage.StickySessions["id:keep"]; !ok {
		t.Fatal("mapping for surviving key must stay")
	}
	if _, ok := usage.StickySessions["id:s2"]; ok {
		t.Fatal("mapping for removed key must be dropped")
	}
}

func TestStickyBindLRUCap(t *testing.T) {
	saved := usage
	t.Cleanup(func() { usage = saved })
	usage = &usageData{StickySessions: map[string]StickyEntry{}}
	base := time.Now().Add(-time.Hour)
	for i := 0; i < stickyMaxEntries+5; i++ {
		usage.StickySessions[fmt.Sprintf("id:s%d", i)] = StickyEntry{
			Key: "k", LastSeen: base.Add(time.Duration(i) * time.Second),
		}
	}
	stickyBind("id:new", "k")
	if len(usage.StickySessions) != stickyMaxEntries {
		t.Fatalf("LRU cap: got %d, want %d", len(usage.StickySessions), stickyMaxEntries)
	}
	if _, ok := usage.StickySessions["id:s0"]; ok {
		t.Fatal("oldest entry must be evicted")
	}
	if _, ok := usage.StickySessions["id:new"]; !ok {
		t.Fatal("new entry must survive")
	}
}

// ---------- Chat 层：锁 key 与快速失败 ----------

type stickyRT func(*http.Request) (*http.Response, error)

func (f stickyRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stickyResp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

// TestChatStickyLocksKeyNoRetry 同会话第二轮粘在同一把 key；上游 402 时
// 直接返回原错误，不换 key、不打第二次上游（用户 2026-11-25 定）。
func TestChatStickyLocksKeyNoRetry(t *testing.T) {
	savedCfg, savedUsage, savedProvider, savedPath := cfg, usage, clientProvider, UsagePath
	t.Cleanup(func() {
		cfg, usage, clientProvider, UsagePath = savedCfg, savedUsage, savedProvider, savedPath
	})
	UsagePath = ""
	usage = &usageData{}
	off := false
	cfg = &Config{Keys: []string{"k1", "k2"}, MaxConcurrency: 8, WeightOptimize: &off}

	calls := 0
	var keys []string
	clientProvider = func(string) *http.Client {
		return &http.Client{Transport: stickyRT(func(r *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
				return stickyResp(200, "{}"), nil // ads/session 端点 best-effort
			}
			calls++
			keys = append(keys, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if calls == 1 {
				return stickyResp(200, `{"choices":[]}`), nil
			}
			return stickyResp(402, `{"error":{"message":"paid only"}}`), nil
		})}
	}
	params := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "task"}}}
	ctx := WithStickySession(context.Background(), "agent-run-1")

	resp, _, err := Chat(ctx, params, "glm-5.3-flash", false, "fill", nil, nil)
	if err != nil || resp == nil {
		t.Fatalf("first chat must succeed: err=%v", err)
	}
	resp.Body.Close()
	if got := stickyLookup("id:agent-run-1", map[string]bool{"k1": true, "k2": true}); got == "" {
		t.Fatal("successful chat must bind sticky mapping")
	}

	_, _, err = Chat(ctx, params, "glm-5.3-flash", false, "fill", nil, nil)
	he, ok := err.(*HTTPError)
	if !ok || he.Status != 402 {
		t.Fatalf("want HTTPError 402, got %T %v", err, err)
	}
	if calls != 2 {
		t.Fatalf("sticky failure must not retry upstream: calls=%d, want 2", calls)
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatalf("sticky session must stay on one key: %v", keys)
	}
}

// TestChatStickyCoolingFailsWithoutUpstream 粘住的 key 冷却时立即失败，
// 一次上游都不打（换 key 只会再撞风控，白费调用）。
func TestChatStickyCoolingFailsWithoutUpstream(t *testing.T) {
	savedCfg, savedUsage, savedProvider, savedPath := cfg, usage, clientProvider, UsagePath
	t.Cleanup(func() {
		cfg, usage, clientProvider, UsagePath = savedCfg, savedUsage, savedProvider, savedPath
	})
	UsagePath = ""
	usage = &usageData{}
	off := false
	cfg = &Config{Keys: []string{"k1"}, MaxConcurrency: 8, WeightOptimize: &off}

	calls := 0
	var boundKey string
	clientProvider = func(string) *http.Client {
		return &http.Client{Transport: stickyRT(func(r *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
				return stickyResp(200, "{}"), nil
			}
			calls++
			boundKey = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			return stickyResp(200, `{"choices":[]}`), nil
		})}
	}
	params := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "task"}}}
	ctx := WithStickySession(context.Background(), "agent-run-2")
	resp, _, err := Chat(ctx, params, "glm-5.3-flash", false, "round_robin", nil, nil)
	if err != nil || resp == nil {
		t.Fatalf("first chat must succeed: err=%v", err)
	}
	resp.Body.Close()
	Cooldown(boundKey, time.Hour)

	_, _, err = Chat(ctx, params, "glm-5.3-flash", false, "round_robin", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "fail fast") {
		t.Fatalf("want fail-fast error for cooling sticky key, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("cooling sticky key must not touch upstream: calls=%d, want 1", calls)
	}
}

// TestChatStickyDisabledKeepsRotation 开关关闭时完全保留原轮转语义。
func TestChatStickyDisabledKeepsRotation(t *testing.T) {
	savedCfg, savedUsage, savedProvider, savedPath := cfg, usage, clientProvider, UsagePath
	// round_robin 游标是包级状态：固定起点并在结束时还原，避免污染 shuffle 下其他测试。
	keyIdxMu.Lock()
	savedIdx := keyIdx
	keyIdx = 0
	keyIdxMu.Unlock()
	t.Cleanup(func() {
		cfg, usage, clientProvider, UsagePath = savedCfg, savedUsage, savedProvider, savedPath
		keyIdxMu.Lock()
		keyIdx = savedIdx
		keyIdxMu.Unlock()
	})
	UsagePath = ""
	usage = &usageData{}
	off := false
	cfg = &Config{Keys: []string{"k1", "k2"}, MaxConcurrency: 8, WeightOptimize: &off, SessionSticky: &off}

	var keys []string
	clientProvider = func(string) *http.Client {
		return &http.Client{Transport: stickyRT(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/chat/completions") {
				keys = append(keys, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			}
			return stickyResp(200, `{"choices":[]}`), nil
		})}
	}
	params := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "task"}}}
	ctx := WithStickySession(context.Background(), "agent-run-3")
	for i := 0; i < 2; i++ {
		resp, _, err := Chat(ctx, params, "glm-5.3-flash", false, "round_robin", nil, nil)
		if err != nil || resp == nil {
			t.Fatalf("chat %d failed: %v", i, err)
		}
		resp.Body.Close()
	}
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("sticky disabled must keep round_robin rotation, got %v", keys)
	}
	if len(usage.StickySessions) != 0 {
		t.Fatalf("sticky disabled must not write mappings, got %d", len(usage.StickySessions))
	}
}

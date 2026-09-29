package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"afree-proxy/internal/workbuddy/auth"
	"afree-proxy/internal/workbuddy/pool"
	"afree-proxy/internal/workbuddy/upstream"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakePanelUpstream(status int, body string) *upstream.Client {
	return &upstream.Client{
		HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		})},
		ChatBaseCN:    "https://fake.example",
		BillingBaseCN: "https://fake.example",
	}
}

// TestModelsStaleServeOnProbeFailure 探测失败时回放最后完整快照
// （panelModelsStaleMax 内），面板模型列表不因一次拨号抖动静默清空；
// 无快照时维持既有 502 语义。
func TestModelsStaleServeOnProbeFailure(t *testing.T) {
	reset := func() {
		panelModelsSnapshot.mu.Lock()
		panelModelsSnapshot.entries = nil
		panelModelsSnapshot.fetched = time.Time{}
		panelModelsSnapshot.mu.Unlock()
	}
	reset()
	t.Cleanup(reset)

	const body = `{"code":0,"data":{"models":[` +
		`{"id":"glm-5.2","maxInputTokens":131072,"maxOutputTokens":32768},` +
		`{"id":"kimi-k3","maxInputTokens":262144,"maxOutputTokens":16384}` +
		`]}}`
	newPool := func() *pool.Pool {
		p := pool.New("")
		a := &auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}
		p.Add(a)
		p.SetCredits(a.UID, 1000, 0)
		return p
	}
	call := func(up *upstream.Client) (int, map[string]any) {
		p := New(Config{Version: "test", APIKey: "test-key", Pool: newPool(), Upstream: up})
		req := httptest.NewRequest("GET", "/panel/api/models", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		var resp map[string]any
		json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec.Code, resp
	}

	// 1) 全量成功：200 + 快照落库（响应不带 stale 标记）
	code, resp := call(fakePanelUpstream(200, body))
	if code != 200 {
		t.Fatalf("fresh: code=%d body=%v", code, resp)
	}
	if _, has := resp["stale"]; has {
		t.Fatalf("fresh response must not carry stale flag: %v", resp)
	}
	models, _ := resp["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("fresh: models=%d want 2", len(models))
	}

	// 2) 上游失败：回放快照（200 + stale 标记 + 完整条目），列表不清空
	code, resp = call(fakePanelUpstream(500, `boom`))
	if code != 200 {
		t.Fatalf("stale: code=%d body=%v", code, resp)
	}
	if resp["stale"] != true {
		t.Fatalf("stale response must carry stale=true: %v", resp)
	}
	models, _ = resp["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("stale: models=%d want 2 (last good snapshot)", len(models))
	}

	// 3) 无快照 + 上游失败：维持既有 502
	reset()
	code, _ = call(fakePanelUpstream(500, `boom`))
	if code != 502 {
		t.Fatalf("no snapshot: code=%d want 502", code)
	}
}

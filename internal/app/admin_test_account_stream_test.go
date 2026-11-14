package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestAccountMustStreamProbeRetriesWithStream 锁定 cline Test 探针的「必须流式」
// 自学习重试：字典序选中的探测模型一旦必须流式（当前目录全是此类），max_tokens=1
// 的非流式探针结构性 500 empty response content，健康账号全部假阴性（2026-11-13
// 审计实测 19/19 error）。探针须与请求路径（cline_stream.go）同语义：学到指纹 →
// 立即改 stream=true 重试 → SSE 200 即 active，并把模型记入学习表供后续直达。
func TestAccountMustStreamProbeRetriesWithStream(t *testing.T) {
	setupBindingTest(t, nil)
	t.Setenv("DATA_DIR", testDataDir(t))

	clineStreamMu.Lock()
	savedLearned := clineStreamLearned
	clineStreamLearned = nil
	clineStreamMu.Unlock()
	t.Cleanup(func() {
		clineStreamMu.Lock()
		clineStreamLearned = savedLearned
		clineStreamMu.Unlock()
	})

	var sawStream atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		stream, _ := body["stream"].(bool)
		sawStream.Store(stream)
		if !stream {
			// cline 对「必须流式」模型非流式调用的固定答复
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"empty response content","success":false}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	origBase := clineAPIBase
	clineAPIBase = upstream.URL
	t.Cleanup(func() { clineAPIBase = origBase })

	modelsMu.Lock()
	savedCache := modelsCache
	modelsCache = map[string]*ModelInfo{
		"cline-free/probe-model": {ID: "cline-free/probe-model", Status: ModelActive, RequiresStream: false},
	}
	modelsMu.Unlock()
	t.Cleanup(func() {
		modelsMu.Lock()
		modelsCache = savedCache
		modelsMu.Unlock()
	})

	swapTestPool(t, []*Account{{AccountID: "acc_probe", Email: "probe@test", APIToken: "sk-test-static-key"}})

	result, status := testAccount(context.Background(), &Account{AccountID: "acc_probe", Email: "probe@test", APIToken: "sk-test-static-key"})
	if status != "active" {
		t.Fatalf("status = %q (result=%v), want active", status, result)
	}
	if saw := sawStream.Load(); saw != true {
		t.Fatalf("retry probe must carry stream=true, got %v", saw)
	}
	if !clineStreamRequired("cline-free/probe-model") {
		t.Fatal("fingerprint model must be learned into the stream-required table")
	}
	if !modelNeedsStream("cline-free/probe-model") {
		t.Fatal("modelNeedsStream must hit the learned entry for later probes/requests")
	}
}

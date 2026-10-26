package trae

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// 模型级永久错误（HTTP 级 4023/1005）必须立即上抛：换账号、换网关候选都
// 不会变，掉进重试循环只会把一次失败放大成 (retries+1)×候选 次上游请求。
func TestChatNoRetryOnModelPermanentHTTPErrors(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":4023,"message":"model not found"}`))
	}))
	defer srv.Close()

	prev := Get()
	t.Cleanup(func() { Set(prev) })
	Set(&Config{Gateway: srv.URL, Retries: 3, Accounts: []*Account{{ID: "u1", Token: "tok"}}})

	_, _, err := Chat(context.Background(), ChatOpts{
		Model:  Model{ID: "gone"},
		Params: map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}},
	})
	var he *HTTPError
	if !errors.As(err, &he) || he.Code != "4023" {
		t.Fatalf("want HTTPError code 4023, got %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("model-level permanent error must not retry, got %d upstream request(s)", n)
	}
}

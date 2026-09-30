package upstream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"afree-proxy/internal/workbuddy/auth"
)

// afree 本地扩展测试（vendor 合并保留项）：ObserveProxy 出口成败观测钩子。
// 断言口径：真实走了代理才回报；直连不回报；传输层失败 ok=false，HTTP 4xx/5xx
//（响应头都拿到了）算传输层成功 ok=true。

type proxyObservation struct {
	url string
	ok  bool
	err error
}

func newObserveClient() (*Client, func() []proxyObservation) {
	c := New()
	var mu sync.Mutex
	var got []proxyObservation
	c.ObserveProxy = func(raw string, ok bool, err error) {
		mu.Lock()
		got = append(got, proxyObservation{raw, ok, err})
		mu.Unlock()
	}
	return c, func() []proxyObservation {
		mu.Lock()
		defer mu.Unlock()
		return append([]proxyObservation(nil), got...)
	}
}

func TestObserveProxyReportsProxySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
	}))
	defer srv.Close()

	c, obs := newObserveClient()
	c.ChatBaseCN = srv.URL
	// httptest server 同时充当 http 代理：对 http 目标，client 会把绝对形态
	// 的请求发给代理，服务器照常应答——足以验证真实代理传输路径。
	c.ProxyFor = func(*auth.Auth) (string, bool) { return srv.URL, true }

	a := &auth.Auth{AccessToken: "at", UID: "u1"}
	rc, status, _, err := c.ChatStream(a, []byte(`{"model":"glm-5.2","messages":[]}`), "", ChatMeta{})
	if err != nil || status != 200 {
		t.Fatalf("chat: status=%d err=%v", status, err)
	}
	if rc != nil {
		rc.Close()
	}

	got := obs()
	if len(got) == 0 {
		t.Fatal("proxied chat must report the exit outcome")
	}
	for _, o := range got {
		if o.url != srv.URL {
			t.Fatalf("observed url = %q, want %q", o.url, srv.URL)
		}
		if !o.ok {
			t.Fatalf("expected transport success, got err=%v", o.err)
		}
	}
}

func TestObserveProxyReportsTransportFailure(t *testing.T) {
	c, obs := newObserveClient()
	c.ChatBaseCN = "http://127.0.0.1:1/chat"
	c.ProxyFor = func(*auth.Auth) (string, bool) { return "http://127.0.0.1:1", true }

	a := &auth.Auth{AccessToken: "at", UID: "u1"}
	if _, _, _, err := c.ChatStream(a, []byte(`{"model":"glm-5.2","messages":[]}`), "", ChatMeta{}); err == nil {
		t.Fatal("dialing a dead proxy must fail")
	}

	got := obs()
	dead := false
	for _, o := range got {
		if o.url != "http://127.0.0.1:1" {
			t.Fatalf("observed url = %q", o.url)
		}
		if o.ok {
			t.Fatal("dial failure must report ok=false")
		}
		if o.err == nil {
			t.Fatal("failure observation must carry the error")
		}
		dead = true
	}
	if !dead {
		t.Fatal("proxied chat over a dead exit must report the failure")
	}
}

func TestObserveProxySkipsDirectExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
	}))
	defer srv.Close()

	c, obs := newObserveClient()
	c.ChatBaseCN = srv.URL
	c.ProxyFor = func(*auth.Auth) (string, bool) { return "", true } // 直连出口

	a := &auth.Auth{AccessToken: "at", UID: "u1"}
	rc, status, _, err := c.ChatStream(a, []byte(`{"model":"glm-5.2","messages":[]}`), "", ChatMeta{})
	if err != nil || status != 200 {
		t.Fatalf("direct chat: status=%d err=%v", status, err)
	}
	if rc != nil {
		rc.Close()
	}
	if got := obs(); len(got) != 0 {
		t.Fatalf("direct exit must not be observed, got %+v", got)
	}
}

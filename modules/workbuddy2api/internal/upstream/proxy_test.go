package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/misakano7545/workbuddy2api-panel/internal/auth"
)

func TestProxyTransportForBuildsAndCachesBoundExit(t *testing.T) {
	c := New()
	c.ProxyFor = func(*auth.Auth) (string, bool) {
		return "http://127.0.0.1:19080", true
	}

	tr, raw, err := c.proxyTransportFor(context.Background(), &auth.Auth{UID: "uid-1"})
	if err != nil {
		t.Fatalf("proxyTransportFor: %v", err)
	}
	if raw != "http://127.0.0.1:19080" {
		t.Fatalf("proxyTransportFor raw URL = %q", raw)
	}
	if tr == nil || tr.Proxy == nil {
		t.Fatal("bound exit did not produce a proxied transport")
	}
	got, err := tr.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}})
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if got.String() != "http://127.0.0.1:19080" {
		t.Fatalf("proxy URL = %q", got.String())
	}

	tr2, _, err := c.proxyTransportFor(context.Background(), &auth.Auth{UID: "uid-1"})
	if err != nil {
		t.Fatalf("second proxyTransportFor: %v", err)
	}
	if tr2 != tr {
		t.Fatal("same bound exit should reuse the cached transport")
	}
}

func TestProxyFuncFailClosed(t *testing.T) {
	c := New()
	c.ProxyFor = func(*auth.Auth) (string, bool) {
		return "", false
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if _, err := c.doHTTP(req, &auth.Auth{UID: "uid-1"}); !errors.Is(err, errNoUsableProxy) {
		t.Fatalf("unavailable bound exit must fail closed, got %v", err)
	}
}

func TestAccountCallPathsUseBoundExit(t *testing.T) {
	c := New()
	c.ProxyFor = func(*auth.Auth) (string, bool) {
		return "", false
	}
	a := &auth.Auth{UID: "uid-1", AccessToken: "at", RefreshToken: "rt"}

	if err := c.RefreshToken(a); !errors.Is(err, errNoUsableProxy) {
		t.Fatalf("RefreshToken bypassed the bound exit: %v", err)
	}
	if _, err := c.billingJSON(a, http.MethodGet, "/billing/meter", nil); !errors.Is(err, errNoUsableProxy) {
		t.Fatalf("billingJSON bypassed the bound exit: %v", err)
	}
	if _, _, _, err := c.ChatStream(a, []byte(`{"model":"glm-5.2","messages":[],"stream":true}`), "", ChatMeta{}); !errors.Is(err, errNoUsableProxy) {
		t.Fatalf("ChatStream bypassed the bound exit: %v", err)
	}
}

// afree 本地定制回归锁：失败类 WARN 的 60s 限频语义——首报必落、窗口内吞、
// 窗口外再落、不同键互不影响。
func TestWarnThrottledWindow(t *testing.T) {
	key := "test:uid-x"
	warnThrottleMu.Lock()
	delete(warnThrottleLast, key)
	warnThrottleMu.Unlock()

	base := time.Now()
	if !warnShouldLog(key, base) {
		t.Fatal("first occurrence must be logged")
	}
	if warnShouldLog(key, base.Add(30*time.Second)) {
		t.Fatal("second occurrence inside the window must be swallowed")
	}
	if !warnShouldLog(key, base.Add(90*time.Second)) {
		t.Fatal("occurrence past the window must be logged again")
	}
	other := "test:uid-y"
	if !warnShouldLog(other, base) {
		t.Fatal("a different key must not share the window")
	}
}

package tokenharbor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 目录同步的凭据口径必须与真实会话一致：跳过停用的 key，用第一个已启用的，
// 而不是无条件取 Keys[0]（停用 = 该 key 不在池里）。
func TestSyncCatalogUsesFirstEnabledKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model:free","context_length":32000}]}`))
	}))
	defer srv.Close()

	prev := Get()
	next := *prev
	next.BaseURL = srv.URL
	next.Keys = []string{"thk-disabled", "thk-live"}
	next.KeyRoutingEnabled = map[string]bool{"thk-disabled": false, "thk-live": true}
	Set(&next)
	t.Cleanup(func() { Set(prev) })

	if _, err := SyncCatalog(); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}
	if got := strings.TrimPrefix(gotAuth, "Bearer "); got != "thk-live" {
		t.Fatalf("目录同步使用了凭据 %q，期望 %q（应跳过停用 key）", got, "thk-live")
	}
}

// 全部 key 停用 = 池里没有可用凭据：跳过同步（保留现有目录），且不发上游请求。
func TestSyncCatalogSkipsWhenAllKeysDisabled(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model:free"}]}`))
	}))
	defer srv.Close()

	prev := Get()
	next := *prev
	next.BaseURL = srv.URL
	next.Keys = []string{"thk-a", "thk-b"}
	next.KeyRoutingEnabled = map[string]bool{"thk-a": false, "thk-b": false}
	Set(&next)
	t.Cleanup(func() { Set(prev) })

	n, err := SyncCatalog()
	if err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}
	if n != 0 {
		t.Fatalf("all keys disabled: changed %d entries, want 0", n)
	}
	if hits != 0 {
		t.Fatalf("all keys disabled: must not hit upstream, got %d requests", hits)
	}
}

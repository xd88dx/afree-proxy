package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// 面板 OpenCode 页每行的"删除"按钮契约（POST /opencode/keys/delete {index}）：
// 按索引移除 key，随行清掉代理绑定与路由参与记录；删空后自动回归匿名 public。

func deleteZenKeyAt(t *testing.T, index int) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"index":` + strconv.Itoa(index) + `}`)
	handleZenKeyDelete(rec, httptest.NewRequest("POST", "/admin/api/opencode/keys/delete", body))
	return rec
}

func TestZenKeyDeleteRemovesKeyAndItsState(t *testing.T) {
	setupZenProbeTest(t, func(w http.ResponseWriter, r *http.Request) {})

	cfg := *getZenConfig()
	cfg.KeyBindings = map[string]zenProxyBinding{"sk-bbb222": {Main: "http://p1:1"}}
	cfg.KeyRoutingEnabled = map[string]bool{"sk-bbb222": false}
	setZenConfig(&cfg)

	if rec := deleteZenKeyAt(t, 1); rec.Code != http.StatusOK {
		t.Fatalf("delete -> %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	got := getZenConfig()
	if strings.Join(got.Keys, ",") != "sk-aaa111,sk-pin333" {
		t.Fatalf("keys = %v, want [sk-aaa111 sk-pin333]", got.Keys)
	}
	if got.Key != got.Keys[0] {
		t.Fatalf("compat key = %q, want Keys[0] = %q", got.Key, got.Keys[0])
	}
	if _, ok := got.KeyBindings["sk-bbb222"]; ok {
		t.Fatal("deleted key left a proxy binding behind")
	}
	if _, ok := got.KeyRoutingEnabled["sk-bbb222"]; ok {
		t.Fatal("deleted key left a routing entry behind")
	}
}

// 删除最后一个真实 key 必须真的删掉：normalizeZenKeys 见到"空 Keys + 非空 Key"
// 会把 Key 灌回池子（旧单 key 迁移规则），若 handler 不一起清 Key，接口回 200
// 但 key 仍在，属于无声的假成功。
func TestZenKeyDeleteLastRealKeyFallsBackToPublic(t *testing.T) {
	setupZenProbeTest(t, func(w http.ResponseWriter, r *http.Request) {})

	cfg := *getZenConfig()
	cfg.Keys = []string{"sk-only-one"}
	cfg.Key = "sk-only-one"
	setZenConfig(&cfg)

	if rec := deleteZenKeyAt(t, 0); rec.Code != http.StatusOK {
		t.Fatalf("delete -> %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	got := getZenConfig()
	if strings.Join(got.Keys, ",") != "public" {
		t.Fatalf("keys = %v, want [public] (anonymous fallback)", got.Keys)
	}
	if got.Key != "public" {
		t.Fatalf("compat key = %q, want public", got.Key)
	}
}

func TestZenKeyDeleteRejectsBadIndex(t *testing.T) {
	setupZenProbeTest(t, func(w http.ResponseWriter, r *http.Request) {})

	if rec := deleteZenKeyAt(t, 99); rec.Code != http.StatusBadRequest {
		t.Fatalf("index out of range -> %d, want 400", rec.Code)
	}
	rec := httptest.NewRecorder()
	handleZenKeyDelete(rec, httptest.NewRequest("POST", "/admin/api/opencode/keys/delete", strings.NewReader(`{invalid`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON -> %d, want 400", rec.Code)
	}
	if rec := deleteZenKeyAt(t, 0); rec.Code != http.StatusOK {
		t.Fatalf("valid delete after failures -> %d, want 200", rec.Code)
	}
}

package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 面板 OpenCode 页拖拽排序契约（POST /opencode/keys/reorder {order:[oldIndex,...]}）：
// 按旧 index 置换重排 key 池，Key=Keys[0] 兼容字段随新首 key 同步；代理绑定与
// 路由参与按 key 明文存储，重排零影响；越界 index 整体拒绝。

func reorderZenKeysAt(t *testing.T, order string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handleZenKeysReorder(rec, httptest.NewRequest("POST", "/admin/api/opencode/keys/reorder", strings.NewReader(`{"order":[`+order+`]}`)))
	return rec
}

func TestZenKeysReorderPermutationAndCompat(t *testing.T) {
	setupZenProbeTest(t, func(w http.ResponseWriter, r *http.Request) {})

	cfg := *getZenConfig()
	cfg.Keys = []string{"sk-aaa111", "sk-bbb222", "sk-pin333"}
	cfg.Key = "sk-aaa111"
	cfg.KeyBindings = map[string]zenProxyBinding{"sk-bbb222": {Main: "http://p1:1"}}
	cfg.KeyRoutingEnabled = map[string]bool{"sk-bbb222": false}
	setZenConfig(&cfg)

	// 置换 [2,0,1]：原 index2 (pin) 提到最前
	if rec := reorderZenKeysAt(t, `2,0,1`); rec.Code != http.StatusOK {
		t.Fatalf("reorder -> %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	got := getZenConfig()
	if strings.Join(got.Keys, ",") != "sk-pin333,sk-aaa111,sk-bbb222" {
		t.Fatalf("keys = %v, want [sk-pin333 sk-aaa111 sk-bbb222]", got.Keys)
	}
	// 兼容字段 Key 同步为新的 Keys[0]
	if got.Key != "sk-pin333" {
		t.Fatalf("compat key = %q, want sk-pin333 (new Keys[0])", got.Key)
	}
	// 绑定/路由表按 key 明文存储，重排不得丢
	if b, ok := got.KeyBindings["sk-bbb222"]; !ok || b.Main != "http://p1:1" {
		t.Fatal("proxy binding lost or corrupted after reorder")
	}
	if routed, ok := got.KeyRoutingEnabled["sk-bbb222"]; !ok || routed {
		t.Fatal("routing entry lost or corrupted after reorder")
	}

	// 越界 index 整体拒绝，池保持原状
	if rec := reorderZenKeysAt(t, `0,9`); rec.Code != http.StatusBadRequest {
		t.Fatalf("out-of-range index -> %d, want 400", rec.Code)
	}
	if got := getZenConfig(); strings.Join(got.Keys, ",") != "sk-pin333,sk-aaa111,sk-bbb222" {
		t.Fatal("keys must stay untouched after a rejected reorder")
	}

	// 空顺序拒绝
	if rec := reorderZenKeysAt(t, ``); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty order -> %d, want 400", rec.Code)
	}

	// 部分置换：未提及的 key 按原相对顺序追加尾部
	if rec := reorderZenKeysAt(t, `1`); rec.Code != http.StatusOK {
		t.Fatalf("partial reorder -> %d, want 200", rec.Code)
	}
	if got := getZenConfig(); strings.Join(got.Keys, ",") != "sk-aaa111,sk-pin333,sk-bbb222" {
		t.Fatalf("partial keys = %v, want [sk-aaa111 sk-pin333 sk-bbb222]", got.Keys)
	}
}

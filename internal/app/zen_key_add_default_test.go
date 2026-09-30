package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// 面板保存 key 列表（整体替换）的"新增身份默认不启用"口径：
// 新引入的 key 默认不参与路由（覆盖陈旧表项），已有 key 保持显式状态，
// 缺表项的旧 key（兼容语义=启用）不受影响，被移除的 key 清理表项。
func TestZenConfigUpdateNewKeyDefaultsDisabled(t *testing.T) {
	setupBindingTest(t, &zenConfigData{
		Keys:              []string{"sk-off", "sk-legacy"},
		KeyRoutingEnabled: map[string]bool{"sk-off": false},
	})
	req := httptest.NewRequest("POST", "/admin/api/opencode/config/update",
		strings.NewReader(`{"keys":["sk-off","sk-legacy","sk-new"]}`))
	rec := httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update failed: %d %s", rec.Code, rec.Body.String())
	}
	cfg := getZenConfig()
	if cfg.KeyRoutingEnabled["sk-new"] != false {
		t.Fatal("newly added key must default to routing-disabled")
	}
	if v, ok := cfg.KeyRoutingEnabled["sk-off"]; !ok || v {
		t.Fatalf("existing key must keep explicit state, got v=%v ok=%v", v, ok)
	}
	if _, ok := cfg.KeyRoutingEnabled["sk-legacy"]; ok {
		t.Fatal("legacy key without entry must stay entry-less (absence = enabled)")
	}

	// 再保存相同列表：无新 key，状态零漂移
	req = httptest.NewRequest("POST", "/admin/api/opencode/config/update",
		strings.NewReader(`{"keys":["sk-off","sk-legacy","sk-new"]}`))
	rec = httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("re-save failed: %d", rec.Code)
	}
	cfg = getZenConfig()
	// 零漂移口径：sk-new 保持默认禁用，sk-off 保持显式禁用，sk-legacy 仍无表项。
	if cfg.KeyRoutingEnabled["sk-new"] || cfg.KeyRoutingEnabled["sk-off"] {
		t.Fatalf("re-save must not drift key states: %v", cfg.KeyRoutingEnabled)
	}
	if _, ok := cfg.KeyRoutingEnabled["sk-legacy"]; ok {
		t.Fatal("legacy key must stay entry-less after re-save")
	}

	// 移除 key：表项一并清理
	req = httptest.NewRequest("POST", "/admin/api/opencode/config/update",
		strings.NewReader(`{"keys":["sk-legacy"]}`))
	rec = httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("shrink failed: %d", rec.Code)
	}
	cfg = getZenConfig()
	if _, ok := cfg.KeyRoutingEnabled["sk-off"]; ok {
		t.Fatal("removed key routing entry must be cleaned")
	}

	// 单 key 兼容提交（key 字段）同样按"新增默认禁用"处理
	setupBindingTest(t, &zenConfigData{Keys: []string{"public"}})
	req = httptest.NewRequest("POST", "/admin/api/opencode/config/update",
		strings.NewReader(`{"key":"sk-single"}`))
	rec = httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("single-key update failed: %d", rec.Code)
	}
	if cfg = getZenConfig(); cfg.KeyRoutingEnabled["sk-single"] != false {
		t.Fatal("single-key compat submit must default to routing-disabled")
	}
}

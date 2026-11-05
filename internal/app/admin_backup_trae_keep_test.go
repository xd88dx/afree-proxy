package app

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigImportKeepEnabledTrae 导入弹窗「沿用启用状态」对 TraeWork 的豁免
// （2026-11-05 用户两轮实测）：trae 换线时前端弹窗漏了 trae 行（impKeepRowsFor
// 补），后端 KeepEnabled["trae"] 永远缺项 → trae2apiRestoreBackup 全部置 false，
// 备份里明明带着 routing=true 也恢复不了。本测试锁后端语义：
//   - keepEnabled.trae=true → routing 按备份原样恢复；
//   - 缺项/false → 隔离护栏（全部置 false，显式落表）。
func TestConfigImportKeepEnabledTrae(t *testing.T) {
	savedPath := proxyConfigPathOverride
	proxyConfigPathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { proxyConfigPathOverride = savedPath })
	savedSub := trae2apiSub
	trae2apiSub = nil // 未启动：restore 只落盘，启动装载后再套用（导入常态）
	t.Cleanup(func() { trae2apiSub = savedSub })

	const routingTrue = `{"trae-cn-1": true, "trae-cn-2": false}`
	seg := `{"config": {"trae": {"chat_base": "https://x"}, "pool": {}}, "routing": ` + routingTrue + `, "accounts": {"trae-cn-1.json": {"uid": "trae-cn-1"}}}`

	doImport := func(keep string) {
		body := `{"version": 3, "keepEnabled": ` + keep + `, "trae": ` + seg + `}`
		req := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(body))
		rr := httptest.NewRecorder()
		handleAdminConfigImport(rr, req)
		if rr.Code != 200 {
			t.Fatalf("import status = %d: %s", rr.Code, rr.Body.String())
		}
	}

	// 沿用：routing 原样（uid1=true, uid2=false）
	doImport(`{"trae": true}`)
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	got := map[string]bool(f.Trae2apiRouting)
	proxyFileMu.Unlock()
	if got["trae-cn-1"] != true || got["trae-cn-2"] != false {
		t.Fatalf("keepEnabled.trae=true must restore routing verbatim, got %v", got)
	}

	// 未勾选（缺项）：隔离护栏——显式 false 落表（不是查表缺失的兼容启用）
	doImport(`{}`)
	proxyFileMu.Lock()
	f2 := loadProxyConfigFileLocked()
	got2 := map[string]bool(f2.Trae2apiRouting)
	proxyFileMu.Unlock()
	if got2["trae-cn-1"] != false || got2["trae-cn-2"] != false {
		t.Fatalf("keepEnabled.trae missing must disable all explicitly, got %v", got2)
	}

	// 落盘确认：护栏值写进 proxy-config.json（重启后不复活）
	raw, err := os.ReadFile(proxyConfigPathOverride)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk struct {
		Routing map[string]bool `json:"trae2apiRouting"`
	}
	if json.Unmarshal(raw, &onDisk) != nil || onDisk.Routing["trae-cn-1"] {
		t.Fatalf("guard-rail false must be persisted, file: %s", raw)
	}
}

package app

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestConfigBackupTraeProxies 备份必须覆盖 TraeWork 账号出口绑定
// （proxy-config.json 的 trae2apiBindings 段）：2026-11-05 用户实测——生产
// 导出→docker rm 数据卷→导入后绑定丢失。wb 同类缺口 2026-10-14 已补，
// trae 换线时没跟。往返语义与 wb 同：导出携带 traeProxies 段；导入整体替换
// （本机多出的 uid 清掉，防跨机残留旧绑定）。
func TestConfigBackupTraeProxies(t *testing.T) {
	t.Run("export writes traeProxies", func(t *testing.T) {
		b := configBackup{
			Version: configBackupVersion,
			TraeProxies: map[string]trae2apiProxyBinding{
				"trae-cn-1": {Main: "socks5://127.0.0.1:1080", Backup: "socks5://127.0.0.1:1081"},
			},
		}
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["traeProxies"]; !ok {
			t.Error(`export must contain the "traeProxies" section`)
		}
	})

	t.Run("import replaces bindings", func(t *testing.T) {
		t.Setenv("DATA_DIR", testDataDir(t))
		t.Setenv("DATA_DIR", testDataDir(t)) // 同 wb 测试口径；proxy-config 路径经 override 下设
		savedPath := proxyConfigPathOverride
		proxyConfigPathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
		t.Cleanup(func() { proxyConfigPathOverride = savedPath })

		traeBindingsMu.Lock()
		traeBindingsLoaded = false
		traeBindings = map[string]trae2apiProxyBinding{
			"trae-cn-stale": {Main: "socks5://stale:1"},
		}
		traeBindingsMu.Unlock()

		got := importTrae2apiProxyBindings(map[string]trae2apiProxyBinding{
			"trae-cn-1": {Main: "socks5://127.0.0.1:1080", Backup: "socks5://127.0.0.1:1081"},
		})
		if got != 1 {
			t.Fatalf("imported %d bindings, want 1", got)
		}
		traeBindingsMu.Lock()
		defer traeBindingsMu.Unlock()
		if _, ok := traeBindings["trae-cn-stale"]; ok {
			t.Error("import must replace bindings wholesale, stale uid survived")
		}
		b := traeBindings["trae-cn-1"]
		if b.Main != "socks5://127.0.0.1:1080" || b.Backup != "socks5://127.0.0.1:1081" {
			t.Errorf("binding not restored: %+v", b)
		}
	})
}

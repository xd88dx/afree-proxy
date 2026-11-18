package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	"afree-proxy/internal/trae"
	"afree-proxy/internal/workbuddy"
	"afree-proxy/internal/zen"
)

// 2026-11-19 备份缺口补齐的导入验证：wkbd 三段（走池/账号意志/shareOptimize）、
// trae 走池+分域倍率、zen 走池此前不随备份迁移，跨机迁移后全部回落默认值。
func TestConfigImportPlatformKbdSegments(t *testing.T) {
	setupBindingTest(t, nil)

	// 先预置本机现值，验证「旧备份无新段 = 不动」；随后导入带新段的备份验证覆盖。
	workbuddy.MutateWkbdConfig(func(k *workbuddy.WkbdConfigData) {
		k.UseProxies = kbdBoolPtr(false)
		k.ShareOptimize = &workbuddy.WkbdShareOptimize{Enabled: true, Cron: "0 3 * * *"}
	})
	zen.MutateZenConfigFile(func(z *zen.ZenConfigFileData) { z.ZenUseProxies = kbdBoolPtr(true) })
	trae.MutateTraeConfig(func(tc *trae.TraeConfigData) { tc.ModelRateFilterCN = kbdFloatPtr(0.1) })

	t.Run("旧备份无新段：现值不动", func(t *testing.T) {
		body := `{"version":3,"accounts":[]}`
		req := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(body))
		rr := httptest.NewRecorder()
		handleAdminConfigImport(rr, req)
		if rr.Code != 200 {
			t.Fatalf("import status = %d: %s", rr.Code, rr.Body.String())
		}
		if v := workbuddy.WkbdConfigUseProxies(); v == nil || *v != false {
			t.Fatalf("useProxies 现值应保留 false, got %v", v)
		}
		if so := workbuddy.WkbdConfigShareOptimize(); so == nil || so.Cron != "0 3 * * *" {
			t.Fatalf("shareOptimize 现值应保留, got %v", so)
		}
		if v := zen.ZenConfigUseProxies(); v == nil || *v != true {
			t.Fatalf("zenUseProxies 现值应保留 true, got %v", v)
		}
	})

	t.Run("带新段备份：各域落盘生效", func(t *testing.T) {
		body := `{
			"version": 3,
			"workbuddyUseProxies": true,
			"workbuddyShareOptimize": {"enabled": false, "cron": "30 4 * * *"},
			"workbuddyAccountPrefs": {
				"uid-one": {"poolEnabled": true, "accountEnabled": false, "nicknameLocked": true}
			},
			"zenUseProxies": false,
			"traeUseProxies": false,
			"traeRateFilterCN": 0.3,
			"traeRateFilterIntl": 0.5
		}`
		req := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(body))
		rr := httptest.NewRecorder()
		handleAdminConfigImport(rr, req)
		if rr.Code != 200 {
			t.Fatalf("import status = %d: %s", rr.Code, rr.Body.String())
		}
		if v := workbuddy.WkbdConfigUseProxies(); v == nil || *v != true {
			t.Fatalf("workbuddyUseProxies 应为 true, got %v", v)
		}
		if so := workbuddy.WkbdConfigShareOptimize(); so == nil || so.Enabled || so.Cron != "30 4 * * *" {
			t.Fatalf("shareOptimize 应整体替换, got %+v", so)
		}
		pe, ae, nl := workbuddy.WkbdConfigAccountPref("uid-one")
		if pe == nil || !*pe || ae == nil || *ae || !nl {
			t.Fatalf("accountPrefs 应还原: pe=%v ae=%v nl=%v", pe, ae, nl)
		}
		if v := zen.ZenConfigUseProxies(); v == nil || *v != false {
			t.Fatalf("zenUseProxies 应为 false, got %v", v)
		}
		if v := trae.TraeConfigUseProxies(); v == nil || *v != false {
			t.Fatalf("traeUseProxies 应为 false, got %v", v)
		}
		cn, intl := trae.TraeConfigRateFilters()
		if cn == nil || *cn != 0.3 || intl == nil || *intl != 0.5 {
			t.Fatalf("trae 分域倍率应还原 cn=0.3 intl=0.5, got %v/%v", cn, intl)
		}
	})
}

func kbdBoolPtr(v bool) *bool        { return &v }
func kbdFloatPtr(v float64) *float64 { return &v }

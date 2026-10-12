package app

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/openrouter"
)

// 备份导入的隔离护栏：导入把一切上游身份置为禁用——Cline 账号 poolEnabled
// 显式 false、zen/or key 路由表逐 key 显式 false（缺项 = 参与路由，清表不算
// 禁用）。备份文件里写死的启用状态不允许穿透：导出方部署的绑定出口在本地
// 未必存在，原样启用等于把流量引向未经验证的身份。combos 平台白名单与
// 面板保存同口径（六平台）。
func TestConfigImportDisablesAllIdentities(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DATA_DIR", tmp)

	// poolPath 在 init() 时按当时的 DATA_DIR 解析，测试里改指临时目录
	savedPath, savedPool := poolPath, pool
	poolPath = filepath.Join(tmp, ".cline-accounts.json")
	pool = nil
	defer func() { poolPath, pool = savedPath, savedPool }()

	savedCombos, savedCombosLoaded := combosList, combosLoaded
	defer func() { combosList, combosLoaded = savedCombos, savedCombosLoaded }()

	// openrouter 配置路径指向临时文件（StartProxy 之外不会自动装载）
	openrouter.Load(filepath.Join(tmp, "or.json"))

	body := `{
	  "version": 2,
	  "accounts": [
	    {"accountID": "acc_t1", "refreshToken": "r", "apiToken": "sk_t", "status": "active", "poolEnabled": true}
	  ],
	  "zen": {"keys": ["zk1", "zk2"], "keyRoutingEnabled": {"zk1": true}},
	  "openrouter": {"keys": ["ork1"], "keyRoutingEnabled": {}},
	  "combos": [
	    {"id": "cfgtestcombo", "platform": "openrouter", "target": "oprt:some-model"},
	    {"id": "cfgtestbad", "platform": "bogus", "target": "x"}
	  ]
	}`
	req := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handleAdminConfigImport(rr, req)
	if rr.Code != 200 {
		t.Fatalf("import status = %d: %s", rr.Code, rr.Body.String())
	}

	// Cline 账号：文件里的 poolEnabled=true 必须被覆盖为禁用
	p := loadPool()
	if len(p.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(p.Accounts))
	}
	if p.Accounts[0].PoolEnabled == nil || *p.Accounts[0].PoolEnabled {
		t.Errorf("imported account must be pool-disabled, got %v", p.Accounts[0].PoolEnabled)
	}

	// zen key：两个 key 都必须显式 false（缺项 = 参与，不能靠清表）
	zc := getZenConfig()
	for _, k := range []string{"zk1", "zk2"} {
		if v, ok := zc.KeyRoutingEnabled[k]; !ok || v {
			t.Errorf("zen key %q routing = (%v,%v), want explicit false", k, v, ok)
		}
	}

	// openrouter key：整配置替换后全部禁用
	if v := openrouter.Get().KeyRoutingEnabled["ork1"]; v {
		t.Error("openrouter key routing = true, want false")
	}

	// combos：白名单外的条目丢弃，六平台白名单内的保留
	combosMu.Lock()
	n := len(combosList)
	combosMu.Unlock()
	if n != 1 {
		t.Fatalf("combos kept = %d, want 1 (six-platform whitelist)", n)
	}
}

func TestConfigBackupVersionOK(t *testing.T) {
	for _, v := range []int{1, 2} {
		if !configBackupVersionOK(v) {
			t.Errorf("version %d must be accepted", v)
		}
	}
	for _, v := range []int{0, 3, -1} {
		if configBackupVersionOK(v) {
			t.Errorf("version %d must be rejected", v)
		}
	}
}

// AMDCloud 备份段名 amd → amdcloud（随平台改名同步）。本项目无存量备份文件，
// 不保留旧键兼容：段名若再漂移，按段替换语义下旧文件会静默丢该平台配置。
func TestConfigBackupAMDCloudSectionName(t *testing.T) {
	t.Run("export writes amdcloud", func(t *testing.T) {
		b := configBackup{Version: configBackupVersion, AMDCloud: &amd.Config{Keys: []string{"rc-x"}}}
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["amdcloud"]; !ok {
			t.Error(`export must contain the "amdcloud" section`)
		}
	})

	t.Run("import reads amdcloud into the pool", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("DATA_DIR", tmp)

		savedPath, savedPool := poolPath, pool
		poolPath = filepath.Join(tmp, ".cline-accounts.json")
		pool = nil
		defer func() { poolPath, pool = savedPath, savedPool }()

		savedCombos, savedCombosLoaded := combosList, combosLoaded
		defer func() { combosList, combosLoaded = savedCombos, savedCombosLoaded }()

		amd.Load(filepath.Join(tmp, "amd.json"))

		body := `{"version":2,"amdcloud":{"keys":["rc-new"],"keyRoutingEnabled":{"rc-new":true}}}`
		req := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(body))
		rr := httptest.NewRecorder()
		handleAdminConfigImport(rr, req)
		if rr.Code != 200 {
			t.Fatalf("import status = %d: %s", rr.Code, rr.Body.String())
		}

		ks := amd.Keys()
		if len(ks) != 1 || ks[0] != "rc-new" {
			t.Fatalf("amd pool keys = %v, want [rc-new]", ks)
		}
		// 导入护栏不受影响：身份一律禁用
		if amd.RoutingEnabled("rc-new") {
			t.Error("imported AMDCloud key must be routing-disabled")
		}
	})
}

func TestComboPlatformValid(t *testing.T) {
	for _, p := range []string{"cline", "zen", "workbuddy", "openrouter", "amd", "tokenharbor"} {
		if !comboPlatformValid(p) {
			t.Errorf("platform %q must be valid", p)
		}
	}
	for _, p := range []string{"", "bogus", "Cline"} {
		if comboPlatformValid(p) {
			t.Errorf("platform %q must be invalid", p)
		}
	}
}

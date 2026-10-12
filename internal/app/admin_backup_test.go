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

// 走真实 import handler 的端到端护栏：单元测试直接调 normalizeAMDCloudSection，
// 护不住"handler 忘了调它"的回归（把调用去掉测试照样绿）。这里断言旧键 "amd"
// 的备份经 HTTP 导入后，key 真的落进 amd 池——静默丢失是这里要防的失败模式
// （按段替换语义下"没这段"合法且不报错）。
func TestConfigImportLegacyAMDSectionReachesPool(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DATA_DIR", tmp)

	savedPath, savedPool := poolPath, pool
	poolPath = filepath.Join(tmp, ".cline-accounts.json")
	pool = nil
	defer func() { poolPath, pool = savedPath, savedPool }()

	savedCombos, savedCombosLoaded := combosList, combosLoaded
	defer func() { combosList, combosLoaded = savedCombos, savedCombosLoaded }()

	amd.Load(filepath.Join(tmp, "amd.json"))

	body := `{"version":2,"amd":{"keys":["rc-legacy-e2e"],"keyRoutingEnabled":{"rc-legacy-e2e":true}}}`
	req := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handleAdminConfigImport(rr, req)
	if rr.Code != 200 {
		t.Fatalf("import status = %d: %s", rr.Code, rr.Body.String())
	}

	ks := amd.Keys()
	if len(ks) != 1 || ks[0] != "rc-legacy-e2e" {
		t.Fatalf("amd pool keys = %v, want [rc-legacy-e2e] (legacy \"amd\" section dropped)", ks)
	}
	// 护栏仍生效：导入的身份一律禁用
	if amd.RoutingEnabled("rc-legacy-e2e") {
		t.Error("imported AMDCloud key must be routing-disabled")
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

// 平台展示名改名（RadeonCloud → AMDCloud）同步改了备份段名 amd → amdcloud。
// 存量备份文件里仍是 "amd"，必须照样导入（否则整段 AMDCloud key 池静默丢失，
// 而按段替换语义下"没这段"是合法的、不报错的）。
func TestBackupAMDCloudSectionRenameCompatibility(t *testing.T) {
	t.Run("legacy amd key still imports", func(t *testing.T) {
		var b configBackup
		body := `{"version":2,"amd":{"keys":["rc-legacy"],"keyRoutingEnabled":{"rc-legacy":true}}}`
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			t.Fatal(err)
		}
		b.normalizeAMDCloudSection()
		if b.AMDCloud == nil {
			t.Fatal("legacy \"amd\" section must be normalised into AMDCloud")
		}
		if len(b.AMDCloud.Keys) != 1 || b.AMDCloud.Keys[0] != "rc-legacy" {
			t.Fatalf("AMDCloud.Keys = %v, want [rc-legacy]", b.AMDCloud.Keys)
		}
	})

	t.Run("new amdcloud key imports", func(t *testing.T) {
		var b configBackup
		body := `{"version":2,"amdcloud":{"keys":["rc-new"]}}`
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			t.Fatal(err)
		}
		b.normalizeAMDCloudSection()
		if b.AMDCloud == nil || len(b.AMDCloud.Keys) != 1 || b.AMDCloud.Keys[0] != "rc-new" {
			t.Fatalf("AMDCloud = %+v, want key rc-new", b.AMDCloud)
		}
	})

	t.Run("new key wins when both present", func(t *testing.T) {
		var b configBackup
		body := `{"version":2,"amdcloud":{"keys":["rc-new"]},"amd":{"keys":["rc-old"]}}`
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			t.Fatal(err)
		}
		b.normalizeAMDCloudSection()
		if len(b.AMDCloud.Keys) != 1 || b.AMDCloud.Keys[0] != "rc-new" {
			t.Fatalf("both present must prefer amdcloud, got %v", b.AMDCloud.Keys)
		}
	})

	t.Run("neither key stays nil", func(t *testing.T) {
		var b configBackup
		if err := json.Unmarshal([]byte(`{"version":2}`), &b); err != nil {
			t.Fatal(err)
		}
		b.normalizeAMDCloudSection()
		if b.AMDCloud != nil {
			t.Fatalf("absent AMDCloud section must stay nil (section untouched), got %+v", b.AMDCloud)
		}
	})
}

// 导出只写 amdcloud，永不再写旧键 amd——否则每次导出/导入往返都会让旧段复活。
func TestConfigBackupExportDropsLegacyAMDKey(t *testing.T) {
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
		t.Error("export must contain the amdcloud section")
	}
	if _, ok := m["amd"]; ok {
		t.Error("export must NOT contain the legacy amd section")
	}
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

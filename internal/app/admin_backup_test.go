package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	wbauth "afree-proxy/internal/workbuddy/auth"
	wbconfig "afree-proxy/internal/workbuddy/config"
	wbpool "afree-proxy/internal/workbuddy/pool"
	wbusage "afree-proxy/internal/workbuddy/usage"
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
	poolPath = filepath.Join(tmp, "cline-config.json")
	pool = nil
	defer func() { poolPath, pool = savedPath, savedPool }()

	savedCombos, savedCombosLoaded := combosList, combosLoaded
	defer func() { combosList, combosLoaded = savedCombos, savedCombosLoaded }()

	// openrouter 配置路径指向临时文件（StartProxy 之外不会自动装载）
	openrouter.Load(filepath.Join(tmp, "or.json"))

	body := `{
	  "version": 3,
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

// TestConfigImportKeepEnabled 导入弹窗"沿用启用状态"（KeepEnabled）：勾选的
// 平台保留备份内原状态，未勾选的仍走隔离护栏全部禁用。key 口径与前端对齐：
// cline / zen / oprt / amd / tkhb / wb / "other:<slug>"。
func TestConfigImportKeepEnabled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DATA_DIR", tmp)

	savedPath, savedPool := poolPath, pool
	poolPath = filepath.Join(tmp, "cline-config.json")
	pool = nil
	defer func() { poolPath, pool = savedPath, savedPool }()

	savedCombos, savedCombosLoaded := combosList, combosLoaded
	defer func() { combosList, combosLoaded = savedCombos, savedCombosLoaded }()

	openrouter.Load(filepath.Join(tmp, "or.json"))
	other.Load(filepath.Join(tmp, "other.json"))

	body := `{
	  "version": 3,
	  "keepEnabled": {"cline": true, "oprt": true, "other:alpha": true},
	  "accounts": [
	    {"accountID": "acc_on", "refreshToken": "r1", "status": "active", "poolEnabled": true},
	    {"accountID": "acc_off", "refreshToken": "r2", "status": "active", "poolEnabled": false}
	  ],
	  "zen": {"keys": ["zk1"], "keyRoutingEnabled": {"zk1": true}},
	  "openrouter": {"keys": ["ork1", "ork2"], "keyRoutingEnabled": {"ork1": true, "ork2": false}},
	  "other": {"platforms": [
	    {"slug": "alpha", "name": "A", "prefix": "alpha", "baseURL": "http://a/v1", "keys": ["oak"], "keyRoutingEnabled": {"oak": true}},
	    {"slug": "beta", "name": "B", "prefix": "beta", "baseURL": "http://b/v1", "keys": ["obk"], "keyRoutingEnabled": {"obk": true}}
	  ]}
	}`
	req := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handleAdminConfigImport(rr, req)
	if rr.Code != 200 {
		t.Fatalf("import status = %d: %s", rr.Code, rr.Body.String())
	}

	// cline：沿用（acc_on 启用、acc_off 禁用，与备份一致）
	p := loadPool()
	states := map[string]bool{}
	for _, a := range p.Accounts {
		if a.PoolEnabled == nil {
			t.Errorf("account %s poolEnabled nil, want explicit after keep-enabled import", a.AccountID)
			continue
		}
		states[a.AccountID] = *a.PoolEnabled
	}
	if !states["acc_on"] || states["acc_off"] {
		t.Errorf("cline states not preserved: %v", states)
	}

	// zen：未勾选 → 隔离护栏仍生效（显式 false）
	zc := getZenConfig()
	if v, ok := zc.KeyRoutingEnabled["zk1"]; !ok || v {
		t.Errorf("zen zk1 = (%v,%v), want explicit false (not kept)", v, ok)
	}

	// oprt：沿用（ork1 启用、ork2 禁用）
	or := openrouter.Get()
	if !or.KeyRoutingEnabled["ork1"] || or.KeyRoutingEnabled["ork2"] {
		t.Errorf("oprt states not preserved: %v", or.KeyRoutingEnabled)
	}

	// other：仅 alpha 沿用（启用），beta 未勾选（禁用）
	cfg := other.Get()
	for _, pl := range cfg.Platforms {
		for _, k := range pl.Keys {
			switch pl.Slug {
			case "alpha":
				if !pl.KeyRoutingEnabled[k] {
					t.Errorf("other/alpha key %q must stay enabled (kept)", k)
				}
			case "beta":
				if pl.KeyRoutingEnabled[k] {
					t.Errorf("other/beta key %q must be disabled (not kept)", k)
				}
			}
		}
	}
}

func TestConfigBackupVersionOK(t *testing.T) {
	// 仅接受当前版本 v3：旧 v1/v2 备份不再兼容（2026-11-05 清理），
	// 更新版本必须显式拒绝（未知段静默丢弃比报错危险）。
	if !configBackupVersionOK(3) {
		t.Error("version 3 must be accepted")
	}
	for _, v := range []int{0, 1, 2, 4, -1} {
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
		poolPath = filepath.Join(tmp, "cline-config.json")
		pool = nil
		defer func() { poolPath, pool = savedPath, savedPool }()

		savedCombos, savedCombosLoaded := combosList, combosLoaded
		defer func() { combosList, combosLoaded = savedCombos, savedCombosLoaded }()

		amd.Load(filepath.Join(tmp, "amd.json"))

		body := `{"version":3,"amdcloud":{"keys":["rc-new"],"keyRoutingEnabled":{"rc-new":true}}}`
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

// TestConfigBackupOtherSection 备份必须覆盖 Other 子平台聚合配置：段名 other，
// 导入时 key 一律禁用（与其它 key 池平台同口径）。
func TestConfigBackupOtherSection(t *testing.T) {
	t.Run("export writes other", func(t *testing.T) {
		b := configBackup{
			Version: configBackupVersion,
			Other: &other.Config{Platforms: []other.Platform{{
				Slug: "acme", Name: "Acme", Prefix: "acme", Keys: []string{"sk-1"},
			}}},
		}
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["other"]; !ok {
			t.Error(`export must contain the "other" section`)
		}
	})

	t.Run("import reads other and disables keys", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("DATA_DIR", tmp)

		savedPath, savedPool := poolPath, pool
		poolPath = filepath.Join(tmp, "cline-config.json")
		pool = nil
		defer func() { poolPath, pool = savedPath, savedPool }()

		savedCombos, savedCombosLoaded := combosList, combosLoaded
		defer func() { combosList, combosLoaded = savedCombos, savedCombosLoaded }()

		other.Load(filepath.Join(tmp, "other.json"))

		body := `{"version":3,"other":{"platforms":[{"slug":"acme","name":"Acme","prefix":"acme",` +
			`"baseURL":"http://x/v1","keys":["sk-1","sk-2"],` +
			`"keyRoutingEnabled":{"sk-1":true,"sk-2":true}}]}}`
		req := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(body))
		rr := httptest.NewRecorder()
		handleAdminConfigImport(rr, req)
		if rr.Code != 200 {
			t.Fatalf("import status = %d: %s", rr.Code, rr.Body.String())
		}

		cfg := other.Get()
		if len(cfg.Platforms) != 1 || cfg.Platforms[0].Slug != "acme" {
			t.Fatalf("other platforms not restored: %+v", cfg.Platforms)
		}
		p := cfg.Platforms[0]
		if len(p.Keys) != 2 {
			t.Fatalf("keys = %v, want 2", p.Keys)
		}
		for _, k := range p.Keys {
			if p.KeyRoutingEnabled[k] {
				t.Errorf("imported key %q must be disabled", k)
			}
		}
	})
}

// TestConfigBackupWorkBuddyProxies 备份必须覆盖 WorkBuddy 出口绑定
// （.workbuddy-proxies.json）：往返后主/备用出口原样还原。
func TestConfigBackupWorkBuddyProxies(t *testing.T) {
	t.Run("export writes workBuddyProxies", func(t *testing.T) {
		b := configBackup{
			Version: configBackupVersion,
			WorkBuddyProxies: map[string]workbuddyProxyBinding{
				"uid-1": {Main: "socks5://127.0.0.1:1080", Backup: "socks5://127.0.0.1:1081"},
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
		if _, ok := m["workBuddyProxies"]; !ok {
			t.Error(`export must contain the "workBuddyProxies" section`)
		}
	})

	t.Run("import replaces bindings", func(t *testing.T) {
		t.Setenv("DATA_DIR", testDataDir(t))

		wbProxyMu.Lock()
		wbProxyLoaded = false
		wbProxyBindings = map[string]workbuddyProxyBinding{
			"stale": {Main: "socks5://stale:1"},
		}
		wbProxyMu.Unlock()

		got := importWorkbuddyProxyBindings(map[string]workbuddyProxyBinding{
			"uid-1": {Main: "socks5://127.0.0.1:1080", Backup: "socks5://127.0.0.1:1081"},
		})
		if got != 1 {
			t.Fatalf("imported %d bindings, want 1", got)
		}
		wbProxyMu.Lock()
		defer wbProxyMu.Unlock()
		if _, ok := wbProxyBindings["stale"]; ok {
			t.Error("import must replace bindings wholesale, stale uid survived")
		}
		if b := wbProxyBindings["uid-1"]; b.Main != "socks5://127.0.0.1:1080" || b.Backup != "socks5://127.0.0.1:1081" {
			t.Errorf("binding not restored: %+v", b)
		}
	})
}

// TestConfigBackupWorkbuddyOrder 备份必须覆盖 WorkBuddy 账号的**展示顺序**：
// 顺序只存在 state.json 的 order 字段（不在 vendor config.json，也不在凭据
// 文件里），2026-10-21 实测"拖拽顺序 + 导出导入"时正是这一段丢失。展示顺序
// 随 state 快照的 order 字段迁移（2026-11-05 起独立 Order 段已删）：这里覆盖
// 三件事——导出快照携带实际展示顺序、JSON 键名 order 在 workbuddy.state 内、
// 导入经 ImportStateBackupJSON 按备份顺序套用。
func TestConfigBackupWorkbuddyOrder(t *testing.T) {
	tmp := t.TempDir()
	authDir := filepath.Join(tmp, "auths")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}

	p1 := wbpool.New(filepath.Join(tmp, "state.json"))
	defer p1.Close()
	for _, uid := range []string{"b", "a", "c"} {
		p1.Add(&wbauth.Auth{UID: uid})
	}
	if n := p1.SetOrder([]string{"c", "a", "b"}); n != 3 {
		t.Fatalf("SetOrder n = %d, want 3", n)
	}
	p1.Flush()

	savedSub := workbuddySub
	defer func() { workbuddySub = savedSub }()
	workbuddySub = &workbuddySubsystem{
		cfgPath: filepath.Join(tmp, "config.json"),
		cfg:     &wbconfig.Config{AuthDir: authDir},
		pool:    p1,
	}

	exp := exportWorkbuddyBackup()
	if exp == nil {
		t.Fatal("export returned nil while the subsystem is running")
	}
	if len(exp.State) == 0 {
		t.Fatal("exported backup must carry the state snapshot")
	}
	var sf struct {
		Order []string `json:"order"`
	}
	if err := json.Unmarshal(exp.State, &sf); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sf.Order, ","); got != "c,a,b" {
		t.Fatalf("exported state order = %q, want c,a,b", got)
	}
	// 线上键名必须是 workbuddy.state 内的 order
	raw, err := json.Marshal(exp)
	if err != nil {
		t.Fatal(err)
	}
	var wbm map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wbm); err != nil {
		t.Fatal(err)
	}
	var st map[string]json.RawMessage
	if err := json.Unmarshal(wbm["state"], &st); err != nil {
		t.Fatal(err)
	}
	if _, ok := st["order"]; !ok {
		t.Error(`export must contain the "order" key inside workbuddy.state`)
	}

	// 目标实例：账号集合相同但顺序被打乱（无自定义顺序 = uid 排序）
	p2 := wbpool.New(filepath.Join(tmp, "state2.json"))
	defer p2.Close()
	for _, uid := range []string{"a", "b", "c"} {
		p2.Add(&wbauth.Auth{UID: uid})
	}
	workbuddySub.pool = p2
	importWorkbuddyBackup(exp, true, true)
	uids := make([]string, 0, 3)
	for _, st2 := range p2.List() {
		uids = append(uids, st2.UID)
	}
	if got := strings.Join(uids, ","); got != "c,a,b" {
		t.Fatalf("order after import = %q, want c,a,b", got)
	}

	// 备份没带 state 时不碰本机顺序（缺字段 = 保持不动）
	p3 := wbpool.New(filepath.Join(tmp, "state3.json"))
	defer p3.Close()
	for _, uid := range []string{"b", "a", "c"} {
		p3.Add(&wbauth.Auth{UID: uid})
	}
	p3.SetOrder([]string{"a", "b", "c"})
	workbuddySub.pool = p3
	importWorkbuddyBackup(&workbuddyBackup{}, true, true)
	uids = uids[:0]
	for _, st3 := range p3.List() {
		uids = append(uids, st3.UID)
	}
	if got := strings.Join(uids, ","); got != "a,b,c" {
		t.Fatalf("local order must stay untouched when backup has no state, got %q", got)
	}
}

// TestConfigBackupWorkbuddyStateFacts wb 段三层口径（2026-11-03）：
//   - 导出 = 全量：state（accounts+scheduling+order，事实+统计都在内）与
//     usage（实测台账）都必须出现；
//   - 导入勾选统计：事实与统计都还原（credits/冷却回池、台账回填）；
//   - 导入不勾选统计：账号事实仍还原（余额回池），本机统计与台账保留本机现值。
func TestConfigBackupWorkbuddyStateFacts(t *testing.T) {
	tmp := t.TempDir()
	authDir := filepath.Join(tmp, "auths")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 导出方：账号 a 余额 500、实测成本观测（收费 0.1/1k），usage 台账有 bucket。
	p1 := wbpool.New(filepath.Join(tmp, "state.json"))
	defer p1.Close()
	p1.Add(&wbauth.Auth{UID: "a"})
	p1.Add(&wbauth.Auth{UID: "b"})
	p1.SetCredits("a", 500, 0)
	p1.NoteModelCost("a", "paid-model", 0.1, 1000)
	p1.Flush()

	usagePath := filepath.Join(tmp, "usage.json")
	rec1 := wbusage.New(usagePath)
	rec1.Start()
	defer rec1.Stop()
	rec1.Add(time.Now(), "cn", "a", "paid-model", wbusage.Delta{HasTotal: true, TotalTokens: 1234, HasCredit: true, Credit: 0.05}, true)
	rec1.Save()

	savedSub := workbuddySub
	defer func() { workbuddySub = savedSub }()
	workbuddySub = &workbuddySubsystem{
		cfgPath: filepath.Join(tmp, "config.json"),
		cfg:     &wbconfig.Config{AuthDir: authDir},
		pool:    p1,
		usage:   rec1,
	}

	exp := exportWorkbuddyBackup()
	if exp == nil {
		t.Fatal("export returned nil while the subsystem is running")
	}
	if len(exp.State) == 0 || !strings.Contains(string(exp.State), `"credits":500`) {
		t.Fatalf("export must carry full pool state with credits, got: %s", exp.State)
	}
	if len(exp.Usage) == 0 || !strings.Contains(string(exp.Usage), `"t":1234`) {
		t.Fatalf("export must carry usage ledger, got: %s", exp.Usage)
	}

	// 导入方：同名账号，台账先放一条本机现值（统计层必须能区分还原/保留）。
	p2 := wbpool.New(filepath.Join(tmp, "state2.json"))
	defer p2.Close()
	p2.Add(&wbauth.Auth{UID: "a"})
	p2.Add(&wbauth.Auth{UID: "b"})
	usagePath2 := filepath.Join(tmp, "usage2.json")
	rec2 := wbusage.New(usagePath2)
	rec2.Start()
	defer rec2.Stop()
	workbuddySub.pool = p2
	workbuddySub.usage = rec2

	// 勾选统计：余额、实测成本与台账都随备份还原。
	// （fake 子系统没有 vendor config 文件，cfgApplied=false 属预期——config
	// 段的应用有专门覆盖，这里只验 state/usage 分层。）
	importWorkbuddyBackup(exp, true, true)
	if st, ok := p2.Status("a"); !ok || st.Credits != 500 {
		t.Fatalf("credits must be restored on checked import: %+v ok=%v", st, ok)
	}
	if st, ok := p2.Status("a"); !ok || len(st.ModelCosts) == 0 || st.ModelCosts[0].Model != "paid-model" {
		t.Fatalf("model cost facts must be restored on checked import: %+v", st)
	}
	var led struct {
		Buckets []struct {
			TotalTokens int64 `json:"t"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(rec2.SnapshotRaw(), &led); err != nil {
		t.Fatal(err)
	}
	if len(led.Buckets) == 0 || led.Buckets[0].TotalTokens != 1234 {
		t.Fatalf("usage ledger must be restored on checked import: %s", rec2.SnapshotRaw())
	}

	// 不勾选统计：事实（余额）仍还原，台账保留本机现值。
	p3 := wbpool.New(filepath.Join(tmp, "state3.json"))
	defer p3.Close()
	p3.Add(&wbauth.Auth{UID: "a"})
	p3.Add(&wbauth.Auth{UID: "b"})
	p3.SetCredits("a", 1, 0)
	usagePath3 := filepath.Join(tmp, "usage3.json")
	rec3 := wbusage.New(usagePath3)
	rec3.Start()
	defer rec3.Stop()
	rec3.Add(time.Now(), "cn", "a", "local-model", wbusage.Delta{HasTotal: true, TotalTokens: 9}, true)
	rec3.Save()
	workbuddySub.pool = p3
	workbuddySub.usage = rec3

	importWorkbuddyBackup(exp, true, false)
	if st, ok := p3.Status("a"); !ok || st.Credits != 500 {
		t.Fatalf("account facts (credits) must be imported even without the statistics switch: %+v ok=%v", st, ok)
	}
	var led3 struct {
		Buckets []struct {
			TotalTokens int64 `json:"t"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(rec3.SnapshotRaw(), &led3); err != nil {
		t.Fatal(err)
	}
	if len(led3.Buckets) != 1 || led3.Buckets[0].TotalTokens != 9 {
		t.Fatalf("unchecking import statistics must keep local usage ledger: %s", rec3.SnapshotRaw())
	}
}

// TestConfigBackupStateRoundTrip state 段三层口径（2026-11-03）：导出 = 全量
// 快照（账号事实 + 本机统计都在内）；导入勾选统计（默认）= 事实与统计都随备份
// 还原；取消勾选 = 账号事实仍还原、本机统计保留本机现值（不再清零）。覆盖
// app 层 struct（cline）与平台包 JSON 往返（openrouter，StateSnapshotJSON/
// ImportStateJSON）两种形态。
func TestConfigBackupStateRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("DATA_DIR", tmp)
	savedPath, savedPool, savedUsagePath := poolPath, pool, poolUsagePath
	poolPath = filepath.Join(tmp, "cline-config.json")
	poolUsagePath = filepath.Join(tmp, "cline-state.json")
	pool = nil
	t.Cleanup(func() { poolPath, poolUsagePath = savedPath, savedUsagePath; pool = savedPool })

	// openrouter：配置与痕迹路径都指向临时目录
	openrouter.UsagePath = filepath.Join(tmp, "or-state.json")
	t.Cleanup(func() { openrouter.UsagePath = "" })
	openrouter.Load(filepath.Join(tmp, "or-config.json"))
	// 记账 key 必须在 key 池内：导入配置段时 pruneRemovedKeys 会按新 key 集合
	// 收敛痕迹（既有语义），池外 key 的记账条目会被清掉。
	orc := openrouter.DefaultConfig()
	orc.Keys = []string{"ork_s1"}
	orc.KeyRoutingEnabled = map[string]bool{"ork_s1": true}
	openrouter.Set(orc)

	// 造导出方运行态：cline 账号冷却中（事实）+ 3 次调用/300 tokens（统计）、
	// or key 记账 100（统计）。
	loadPool()
	poolMu.Lock()
	pool = &AccountPool{Accounts: []*Account{{AccountID: "acc_s1", Email: "s@x", RefreshToken: "r"}}}
	poolUsage = &poolUsageData{Accounts: map[string]*accountUsage{
		"acc_s1": {
			Status: "cooldown", CooldownUntil: time.Now().Add(time.Hour).Round(time.Second),
			LastReason: "429", UsageCount: 3, TokensTotal: 300,
		},
	}}
	poolMu.Unlock()
	openrouter.RecordKeyTokens("ork_s1", 100)

	// ---- 导出：state 段必须携带事实与统计（全量） ----
	expReq := httptest.NewRequest("GET", "/admin/api/config/export", nil)
	expRR := httptest.NewRecorder()
	handleAdminConfigExport(expRR, expReq)
	if expRR.Code != 200 {
		t.Fatalf("export status = %d", expRR.Code)
	}
	var backup struct {
		State struct {
			Cline struct {
				Accounts map[string]struct {
					Status        string    `json:"status"`
					CooldownUntil time.Time `json:"cooldownUntil"`
					LastReason    string    `json:"lastReason"`
					UsageCount    int64     `json:"usageCount"`
					TokensTotal   int64     `json:"tokensTotal"`
				} `json:"accounts"`
			} `json:"cline"`
			OpenRouter json.RawMessage `json:"openrouter"`
		} `json:"state"`
	}
	if err := json.Unmarshal(expRR.Body.Bytes(), &backup); err != nil {
		t.Fatalf("parse export: %v", err)
	}
	cl := backup.State.Cline.Accounts["acc_s1"]
	if cl.Status != "cooldown" || cl.LastReason != "429" || cl.UsageCount != 3 || cl.TokensTotal != 300 || cl.CooldownUntil.IsZero() {
		t.Fatalf("cline state missing in export (facts+stats): %+v", cl)
	}
	var orSnap struct {
		KeyTokens map[string]struct {
			Today int64 `json:"Today"`
		} `json:"keyTokens"`
	}
	if len(backup.State.OpenRouter) == 0 || json.Unmarshal(backup.State.OpenRouter, &orSnap) != nil || orSnap.KeyTokens["ork_s1"].Today != 100 {
		t.Fatalf("openrouter state missing in export: %s", backup.State.OpenRouter)
	}

	// ---- 导入（importState 缺省 = 还原全部）：事实与统计都回填 ----
	poolMu.Lock()
	poolUsage = &poolUsageData{}
	poolMu.Unlock()
	openrouter.ClearUsage()
	importBody := expRR.Body.String()
	importReq := httptest.NewRequest("POST", "/admin/api/config/import", strings.NewReader(importBody))
	importRR := httptest.NewRecorder()
	handleAdminConfigImport(importRR, importReq)
	if importRR.Code != 200 {
		t.Fatalf("import status = %d: %s", importRR.Code, importRR.Body.String())
	}
	poolMu.Lock()
	got := usagePeekLocked(&Account{AccountID: "acc_s1"})
	poolMu.Unlock()
	if got == nil || got.Status != "cooldown" || got.LastReason != "429" ||
		got.UsageCount != 3 || got.TokensTotal != 300 || got.CooldownUntil.IsZero() {
		t.Fatalf("cline facts+stats not restored on checked import: %+v", got)
	}
	var orBack struct {
		KeyTokens map[string]struct {
			Today int64 `json:"Today"`
		} `json:"keyTokens"`
	}
	if err := json.Unmarshal(openrouter.StateSnapshotJSON(), &orBack); err != nil {
		t.Fatal(err)
	}
	if orBack.KeyTokens["ork_s1"].Today != 100 {
		t.Fatalf("openrouter state not restored on checked import: %+v", orBack)
	}

	// ---- 导入（importState=false）：账号事实仍还原，本机统计保留本机现值 ----
	// 先造一组与备份不同的本机现值：cline 统计 7 次/700 tokens、or 记账 999。
	poolMu.Lock()
	poolUsage = &poolUsageData{Accounts: map[string]*accountUsage{
		"acc_s1": {Status: "active", UsageCount: 7, TokensTotal: 700},
	}}
	poolMu.Unlock()
	openrouter.ClearUsage()
	openrouter.RecordKeyTokens("ork_s1", 999)
	var payload map[string]any
	if err := json.Unmarshal([]byte(importBody), &payload); err != nil {
		t.Fatal(err)
	}
	payload["importState"] = false
	patch, _ := json.Marshal(payload)
	importReq2 := httptest.NewRequest("POST", "/admin/api/config/import", bytes.NewReader(patch))
	importRR2 := httptest.NewRecorder()
	handleAdminConfigImport(importRR2, importReq2)
	if importRR2.Code != 200 {
		t.Fatalf("import2 status = %d: %s", importRR2.Code, importRR2.Body.String())
	}
	// 事实：备份的冷却状态/原因必须覆盖本机（无论勾选与否都导入）。
	poolMu.Lock()
	got2 := usagePeekLocked(&Account{AccountID: "acc_s1"})
	poolMu.Unlock()
	if got2 == nil || got2.Status != "cooldown" || got2.LastReason != "429" || got2.CooldownUntil.IsZero() {
		t.Fatalf("cline facts must be imported even without the statistics switch: %+v", got2)
	}
	// 统计：保留本机现值（2026-11-03 起不再清零、也不被备份覆盖）。
	if got2.UsageCount != 7 || got2.TokensTotal != 700 {
		t.Fatalf("unchecking import statistics must keep local stats, got %+v", got2)
	}
	var orBack2 struct {
		KeyTokens map[string]struct {
			Today int64 `json:"Today"`
		} `json:"keyTokens"`
	}
	if err := json.Unmarshal(openrouter.StateSnapshotJSON(), &orBack2); err != nil {
		t.Fatal(err)
	}
	if orBack2.KeyTokens["ork_s1"].Today != 999 {
		t.Fatalf("unchecking import statistics must keep local or ledger: %+v", orBack2)
	}
}

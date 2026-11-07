package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// 模型黑名单行为回归：
//   - 权限判据只认 11158 / 「账号类型不支持」文案，普通 4xx/5xx 不误判；
//   - 拉黑 → /v1/models 过滤 → 解禁，状态机闭环；
//   - 同一模型重复命中只累加次数，不重置首次拉黑时间；
//   - 网关出口扫描：错误响应命中判据时自动拉黑，成功响应/流式不触发；
//   - 落盘文件可重启续读。

func resetModelBlacklistForTest(t *testing.T) {
	t.Helper()
	modelBlacklistMu.Lock()
	modelBlacklistItems = map[string]*modelBlacklistEntry{}
	modelBlacklistLoaded = true
	modelBlacklistMu.Unlock()
	t.Cleanup(func() {
		modelBlacklistMu.Lock()
		modelBlacklistItems = map[string]*modelBlacklistEntry{}
		modelBlacklistLoaded = false
		modelBlacklistMu.Unlock()
	})
}

func TestIsModelPermissionDenied(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{503, `{"code":11158,"msg":"model [gpt-6-luna] is not allowed for user type [personal_free_user]"}`, true},
		{400, `{"error":"This model does not support this account"}`, false}, // 文案不精确命中
		{400, `{"message":"The account type does not support this model"}`, true},
		{503, `{"error":"upstream overloaded"}`, false},
		{429, `{"code":14018,"msg":"credits exhausted"}`, false},
		{400, `{"code":11128,"msg":"request illegal"}`, false},
		{500, ``, false},
	}
	for _, c := range cases {
		if got := IsModelPermissionDenied(c.status, c.body); got != c.want {
			t.Errorf("IsModelPermissionDenied(%d, %q) = %v, want %v", c.status, c.body, got, c.want)
		}
	}
}

func TestPlatformOfModel(t *testing.T) {
	cases := map[string]string{
		"wbgb:gpt-6-luna":                       platWorkBuddy,
		"wbcn:step-5":                           platWorkBuddy,
		"global:default-model":                  platWorkBuddy,
		"zen:glm-5.3-flash-free":                platZen,
		"cline-free/muse-spark-1.3-contributor": platCline,
		"amd:Qwen3.8":                           platAMD,
		"oprt:vendor/model":                     platOpenRouter,
		"tkhb:deepseek:free":                    platTokenHarbor,
		"tacn:glm-5.2":                          platTrae,
		"tagb:glm-5.2":                          platTrae,
		"trae:glm-5.2":                          "", // 旧四前缀已废除（2026-11-07 tacn/tagb 合并域）
		"deepseek-v4.1-flash":                   "", // 裸名无平台信息：不猜，避免误拉黑
		"z-ai/glm-5.3-flash":                    "", // cline live feed 直出的裸名同样不猜
	}
	for in, want := range cases {
		if got := platformOfModel(in); got != want {
			t.Errorf("platformOfModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModelBlacklistLifecycle(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	if IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("fresh list must not contain the model")
	}
	BlacklistModel(platWorkBuddy, "wbgb:gpt-6-luna", "11158", BanReasonAuto, "not allowed for user type")
	if !IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("model should be blacklisted")
	}
	rows := modelBlacklistSnapshot()
	if len(rows) != 1 || rows[0]["platform"] != platWorkBuddy || rows[0]["model"] != "wbgb:gpt-6-luna" {
		t.Fatalf("snapshot = %+v", rows)
	}
	if rows[0]["reason"] != BanReasonAuto {
		t.Fatalf("reason = %v, want %q", rows[0]["reason"], BanReasonAuto)
	}
	firstCreated := rows[0]["createdAt"]

	// 重复命中：累加次数，不改首次拉黑时间。
	BlacklistModel(platWorkBuddy, "wbgb:gpt-6-luna", "11158", BanReasonAuto, "again")
	rows = modelBlacklistSnapshot()
	if rows[0]["hits"].(int) != 2 {
		t.Fatalf("hits = %v, want 2", rows[0]["hits"])
	}
	if rows[0]["createdAt"] != firstCreated {
		t.Fatalf("createdAt changed on re-hit: %v -> %v", firstCreated, rows[0]["createdAt"])
	}

	// 落盘可续读。
	if _, err := os.Stat(globalConfigPath()); err != nil {
		t.Fatalf("blacklist file not written: %v", err)
	}
	modelBlacklistMu.Lock()
	modelBlacklistItems = map[string]*modelBlacklistEntry{}
	modelBlacklistLoaded = false
	modelBlacklistMu.Unlock()
	if !IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("model should survive reload from disk")
	}

	// 解禁：移出名单，再拉黑能重新加入（不是永久豁免）。
	if !UnblockModel(platWorkBuddy, "wbgb:gpt-6-luna") {
		t.Fatal("unblock should report an existing entry")
	}
	if IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("model should be gone after unblock")
	}
	if UnblockModel(platWorkBuddy, "wbgb:gpt-6-luna") {
		t.Fatal("second unblock should report nothing removed")
	}
	BlacklistModel(platWorkBuddy, "wbgb:gpt-6-luna", "11158", BanReasonAuto, "again after unblock")
	if !IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("model should be re-blacklistable after unblock")
	}
}

// 手动封禁：原因统一「管理员手动封禁」；对已存在条目覆盖原因。
func TestBanModelManually(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	if !BanModel(platZen, "zen:big-pickle") {
		t.Fatal("first manual ban should report a new entry")
	}
	if !IsModelBlacklisted("zen:big-pickle") {
		t.Fatal("manually banned model should be blacklisted")
	}
	rows := modelBlacklistSnapshot()
	if rows[0]["reason"] != BanReasonManual || rows[0]["code"] != "" {
		t.Fatalf("manual ban entry = %+v", rows[0])
	}

	// 自动封禁后再手动封禁：原因被覆盖为手动封禁。
	BlacklistModel(platWorkBuddy, "wbgb:gpt-6-luna", "11158", BanReasonAuto, "not allowed for user type")
	BanModel(platWorkBuddy, "wbgb:gpt-6-luna")
	rows = modelBlacklistSnapshot()
	byModel := map[string]map[string]any{}
	for _, r := range rows {
		byModel[r["model"].(string)] = r
	}
	if got := byModel["wbgb:gpt-6-luna"]["reason"]; got != BanReasonManual {
		t.Fatalf("reason after manual ban = %v, want %q", got, BanReasonManual)
	}
	if BanModel(platZen, "zen:big-pickle") {
		t.Fatal("re-ban of existing entry must report not-new")
	}
}

// 网关出口扫描：错误响应命中判据 → 自动拉黑；200 响应不触发。
// 场景用 zen 平台（wb 目录来自 vendor server，测试环境无法注入条目；
// zen 目录可锁内注入，判据 11158 与平台无关）。
func TestWithModelBlacklistScan(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)
	// zen 平台可路由性闸要求有启用 key（zenKeyRoutingEnabled 缺省启用）。
	savedCfg := getZenConfig()
	cp := *savedCfg
	cp.Keys = []string{"scan-test-key"}
	setZenConfig(&cp)
	t.Cleanup(func() { setZenConfig(savedCfg) })
	// 服务闸要求模型在对外目录：锁内注入 zen seed 形态条目。
	zenModelsMu.Lock()
	savedZen := make(map[string]*ZenModel, len(zenModels))
	for k, v := range zenModels {
		savedZen[k] = v
	}
	zenModels["mimo-v2.5-free"] = &ZenModel{ID: "mimo-v2.5-free", Source: "seed"}
	zenModels["big-pickle"] = &ZenModel{ID: "big-pickle", Source: "seed"}
	zenModelsMu.Unlock()
	t.Cleanup(func() {
		zenModelsMu.Lock()
		zenModels = savedZen
		zenModelsMu.Unlock()
	})

	errBody := `{"error":{"message":"{\"code\":11158,\"msg\":\"model [mimo-v2.5-free] is not allowed for user type [personal_free_user]\"}"}}`
	h := withModelBlacklistScan(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(errBody))
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"zen:mimo-v2.5-free","messages":[]}`))
	rec := httptest.NewRecorder()
	h(rec, req)

	if !IsModelBlacklisted("zen:mimo-v2.5-free") {
		t.Fatal("permission error must auto-blacklist the model")
	}
	// 客户端拿到的响应体必须原样透传（包装层只 tee，不改写）。
	if !strings.Contains(rec.Body.String(), "11158") {
		t.Fatalf("body mutated: %s", rec.Body.String())
	}
	// 平台/业务码/统一原因落库正确。
	rows := modelBlacklistSnapshot()
	if rows[0]["platform"] != platZen || rows[0]["code"] != "11158" {
		t.Fatalf("entry = %+v", rows[0])
	}
	if rows[0]["reason"] != BanReasonAuto {
		t.Fatalf("reason = %v, want %q", rows[0]["reason"], BanReasonAuto)
	}
	if rows[0]["detail"] == "" || !strings.Contains(rows[0]["detail"].(string), "11158") {
		t.Fatalf("detail should keep the upstream body, got %+v", rows[0]["detail"])
	}

	// 成功响应（流式）不触发拉黑。
	hOk := withModelBlacklistScan(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n"))
	})
	rec2 := httptest.NewRecorder()
	hOk(rec2, httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"zen:big-pickle","messages":[]}`)))
	if IsModelBlacklisted("zen:big-pickle") {
		t.Fatal("successful response must not blacklist")
	}
}

// cline 免费目录残留在网关缓存里的死模型：上游固定回 404 model not found
// （官方 feed 把模型移入 clinePass 后，live too small 保护不修剪目录），
// 网关错误信封是 500 内嵌 "API 404: ..."。命中文案即拉黑，让 /v1/models
// 停止对外提供（客户端含 newapi 渠道映射，否则每次选中都白吃一轮失败）。
func TestClineModelNotFoundAutoBan(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)
	// 平台可路由性闸要求 cline 池有 active 账号，种一个。
	// 增删都必须持 poolMu：服务闸未命中会触发后台目录同步（pickAccountAny
	// 持锁读账号），锁外改池构成数据竞争（2026-11-05 race 实测）。清理按
	// AccountID 精确摘除——[:0] 会连别的测试种进来的账号一起清掉。
	// 痕迹表也要清：poolUsage 是进程内单例，-count>1 时上一轮迭代留给
	// scan-cline-t1 的 expired 痕迹会让本轮种下的账号直接不可路由。
	p := loadPool()
	poolMu.Lock()
	p.Accounts = append(p.Accounts, &Account{AccountID: "scan-cline-t1"})
	if poolUsage.Accounts != nil {
		delete(poolUsage.Accounts, "scan-cline-t1")
	}
	poolMu.Unlock()
	t.Cleanup(func() {
		poolMu.Lock()
		for i, a := range p.Accounts {
			if a != nil && a.AccountID == "scan-cline-t1" {
				p.Accounts = append(p.Accounts[:i], p.Accounts[i+1:]...)
				break
			}
		}
		if poolUsage.Accounts != nil {
			delete(poolUsage.Accounts, "scan-cline-t1")
		}
		poolMu.Unlock()
	})
	// 请求路径有服务闸（目录外一律拒）：注入被测模型让请求穿过闸。
	injectClineModels(t, "cline-free/muse-spark-1.3-contributor")

	// 网关信封形态（proxy.go 对 cline 上游错误统一 500 + api_error，
	// 上游 404 文案内嵌在 message 里）。
	errBody := `{"error":{"message":"API 404: {\"error\":\"model not found\",\"success\":false}\n","type":"api_error"}}`
	h := withModelBlacklistScan(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(errBody))
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"cline-free/muse-spark-1.3-contributor","messages":[]}`))
	rec := httptest.NewRecorder()
	h(rec, req)

	if !IsModelBlacklisted("cline-free/muse-spark-1.3-contributor") {
		t.Fatal("cline 404 model not found must auto-blacklist the model")
	}
	rows := modelBlacklistSnapshot()
	if rows[0]["platform"] != platCline {
		t.Fatalf("platform = %v, want %v", rows[0]["platform"], platCline)
	}
	// 原因必须是 FeedRemoved（官方下线），不是账号权限不足——两者处置语义不同。
	if rows[0]["reason"] != BanReasonFeedRemoved {
		t.Fatalf("reason = %v, want %q", rows[0]["reason"], BanReasonFeedRemoved)
	}
	// 客户端拿到的响应体原样透传。
	if !strings.Contains(rec.Body.String(), "model not found") {
		t.Fatalf("body mutated: %s", rec.Body.String())
	}

	// 同平台非"模型不存在"的错误（网络/过载信封）不拉黑。
	resetModelBlacklistForTest(t)
	hNet := withModelBlacklistScan(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"API 500: upstream overloaded","type":"api_error"}}`))
	})
	hNet(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"cline-free/deepseek-v4.1-flash","messages":[]}`)))
	if IsModelBlacklisted("cline-free/deepseek-v4.1-flash") {
		t.Fatal("non model-not-found cline errors must not blacklist")
	}
}

// 请求路径的模型服务闸（2026-10-31 用户定）：已拉黑模型拒、目录外模型拒、
// 目录内模型放行；空 model 放行（错误语义归入口校验）。
func TestServedModelGate(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	throttleGateRefreshForTest() // 未知模型分支不做 live 目录同步（不发外网、不写盘）
	resetModelBlacklistForTest(t)
	// apiModelList 的展示门控要求 cline 有可路由账号，种一个。
	p := loadPool()
	p.Accounts = append(p.Accounts, &Account{AccountID: "gate-cline-t1"})
	t.Cleanup(func() { p.Accounts = p.Accounts[:0] })
	injectClineModels(t, "z-ai/glm-5.3-flash")

	// 目录内模型放行（对外 ID 天然带前缀，精确匹配；空 model 放行归入口校验）。
	for _, m := range []string{"cline-free/z-ai/glm-5.3-flash", ""} {
		if status, _ := servedModelGate(m); status != 0 {
			t.Errorf("servedModelGate(%q) = %d, want pass", m, status)
		}
	}
	// 裸名（models api 输出里没有的拼写形态）同样拒：目录里能看到的才能请求到。
	if status, _ := servedModelGate("z-ai/glm-5.3-flash"); status != http.StatusNotFound {
		t.Errorf("servedModelGate(bare form) = %d, want 404", status)
	}
	// 目录外模型拒。
	if status, msg := servedModelGate("no/such-model"); status != http.StatusNotFound || msg == "" {
		t.Errorf("servedModelGate(unknown) = %d, want 404", status)
	}
	// 已拉黑模型拒（无论是否在目录）。
	BlacklistModel(platCline, "cline-free/z-ai/glm-5.3-flash", "", BanReasonAuto, "")
	if status, msg := servedModelGate("cline-free/z-ai/glm-5.3-flash"); status != http.StatusNotFound || !strings.Contains(msg, "banned") {
		t.Errorf("servedModelGate(banned) = %d msg=%q, want 404 banned", status, msg)
	}
}

// cline 计费哨兵：成功响应 usage.cost>0 自动拉黑（仅 cline 路径调用本函数）。
func TestClineAutoBanPaidUsage(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	// cost>0：拉黑，对外 ID 归一（裸名补前缀）。
	clineAutoBanPaidUsage(map[string]any{"model": "z-ai/glm-5.3-flash"},
		map[string]any{"cost": 0.0000071, "prompt_tokens": float64(10)})
	if !IsModelBlacklisted("cline-free/z-ai/glm-5.3-flash") {
		t.Fatal("cost>0 must blacklist the model with normalized external id")
	}
	rows := modelBlacklistSnapshot()
	if rows[0]["reason"] != BanReasonFeedRemoved || rows[0]["code"] != "cost" {
		t.Fatalf("entry = %+v", rows[0])
	}

	// cost=0 / 缺 cost / 非 cline 信封：不动。
	resetModelBlacklistForTest(t)
	clineAutoBanPaidUsage(map[string]any{"model": "z-ai/glm-5.3-flash"}, map[string]any{"cost": float64(0)})
	clineAutoBanPaidUsage(map[string]any{"model": "z-ai/glm-5.3-flash"}, map[string]any{})
	clineAutoBanPaidUsage(map[string]any{"model": "z-ai/glm-5.3-flash"}, nil)
	if len(modelBlacklistSnapshot()) != 0 {
		t.Fatal("zero/missing cost must not blacklist")
	}
}

// 批量封禁：平台目录 ID 大小写不敏感包含关键字即封；已封条目跳过不覆盖
// 原因；空关键字/未知平台拒绝（空关键字=全量匹配，危险操作）。
func TestBatchBanPlatformModels(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)
	p := loadPool()
	p.Accounts = append(p.Accounts, &Account{AccountID: "batch-cline-t1"})
	t.Cleanup(func() { p.Accounts = p.Accounts[:0] })
	injectClineModels(t,
		"z-ai/glm-5.3-flash", "cline-free/DOUBAO-Seed-2.1-Pro", "poolside/laguna-s-2.1:free")

	// 大小写不敏感：小写关键字命中大写 ID。
	matched, banned := batchBanPlatformModels(platCline, "doubao")
	if matched != 1 || len(banned) != 1 || banned[0] != "cline-free/DOUBAO-Seed-2.1-Pro" {
		t.Fatalf("matched=%d banned=%v, want 1 hit", matched, banned)
	}
	if !IsModelBlacklisted("cline-free/DOUBAO-Seed-2.1-Pro") {
		t.Fatal("matched model must be blacklisted")
	}
	rows := modelBlacklistSnapshot()
	if rows[0]["platform"] != platCline || rows[0]["reason"] != BanReasonManual {
		t.Fatalf("entry = %+v", rows[0])
	}

	// 幂等：再次批量不重复封禁、不新增条目。
	matched2, banned2 := batchBanPlatformModels(platCline, "doubao")
	if matched2 != 1 || len(banned2) != 0 {
		t.Fatalf("re-ban must skip existing: matched=%d banned=%v", matched2, banned2)
	}
	if n := len(modelBlacklistSnapshot()); n != 1 {
		t.Fatalf("snapshot = %d entries, want 1", n)
	}

	// 不含关键字的模型不受影响。
	if IsModelBlacklisted("z-ai/glm-5.3-flash") || IsModelBlacklisted("cline-free/poolside/laguna-s-2.1:free") {
		t.Fatal("non-matching models must stay unbanned")
	}
}

// Trae 模型级失败判据：1005/4023 直接封，4001 先复核，限流不封。
func TestTraeScopedBanIsolation(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	// trae 域封禁：不影响 solo 域
	BanModel("trae", "trae:glm-5.3-flash", "trae")
	if !TraeModelBannedIn("trae", "glm-5.3-flash") {
		t.Fatal("traecode scope must be banned")
	}
	if TraeModelBannedIn("solo", "glm-5.3-flash") || TraeModelBannedIn("trae-intl", "glm-5.3-flash") {
		t.Fatal("trae ban must not leak into solo/trae-intl scopes")
	}
	// 全档条目（scope 空）：所有档位命中
	BanModel("trae", "trae:glm-5.2")
	if !TraeModelBannedIn("trae", "glm-5.2") || !TraeModelBannedIn("solo", "glm-5.2") {
		t.Fatal("unscoped ban must apply to every scope")
	}
	// 按档位解禁只拆该档位
	UnblockModel("trae", "trae:glm-5.3-flash", "trae")
	if TraeModelBannedIn("trae", "glm-5.3-flash") {
		t.Fatal("scoped unblock must clear the scoped entry")
	}
	// 自动封禁按传输落档位：1005 → 只封 trae 域
	BlacklistModel("trae", "trae:kimi-k2.6", "1005", BanReasonAuto, "test", "trae")
	if !TraeModelBannedIn("trae", "kimi-k2.6") || TraeModelBannedIn("solo", "kimi-k2.6") {
		t.Fatal("auto ban must land on the failing transport's scope")
	}
}

// 面板「解禁」按钮不表达档位，语义是「这个模型别封了」：空 scope 解禁必须
// 把按档位落的条目一起清掉。只删全档键会留下 trae/solo/trae-intl 条目，
// 表现是点了「解禁」弹成功、刷新后条目原地不动（2026-10-28 修复）。
func TestUnblockModelWithoutScopeClearsScopedEntries(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	// 自动封禁按传输落档位（4023 + raw → traecode），与生产数据同形。
	BlacklistModel(platTrae, "trae:deepseek-v4.1-flash", "4023", BanReasonModelGone, "model not found", "trae")
	if !TraeModelBannedIn("trae", "deepseek-v4.1-flash") {
		t.Fatal("scoped auto ban must land on the traecode scope")
	}
	if !UnblockModel(platTrae, "trae:deepseek-v4.1-flash") {
		t.Fatal("unblock without scope must remove the scoped entry")
	}
	if IsModelBlacklisted("trae:deepseek-v4.1-flash") || TraeModelBannedIn("trae", "deepseek-v4.1-flash") {
		t.Fatal("model must be gone from every scope after an unscoped unblock")
	}
	if UnblockModel(platTrae, "trae:deepseek-v4.1-flash") {
		t.Fatal("second unblock must report nothing removed")
	}

	// 同模型多档位 + 全档条目并存：一次解禁全部清空。
	BanModel(platTrae, "trae:glm-5.3", "solo")
	BanModel(platTrae, "trae:glm-5.3", "trae-intl")
	BanModel(platTrae, "trae:glm-5.3")
	if !UnblockModel(platTrae, "trae:glm-5.3") {
		t.Fatal("unblock must report removal")
	}
	if TraeModelBannedIn("solo", "glm-5.3") || TraeModelBannedIn("trae-intl", "glm-5.3") ||
		TraeModelBannedIn("trae", "glm-5.3") {
		t.Fatal("unscoped unblock must clear every scoped entry")
	}

	// 带档位解禁仍只拆该档位（档位隔离语义不变）。
	BanModel(platTrae, "trae:kimi-k3", "solo")
	BanModel(platTrae, "trae:kimi-k3", "trae-intl")
	if !UnblockModel(platTrae, "trae:kimi-k3", "solo") {
		t.Fatal("scoped unblock must report removal")
	}
	if TraeModelBannedIn("solo", "kimi-k3") {
		t.Fatal("scoped unblock must clear only its own scope")
	}
	if !TraeModelBannedIn("trae-intl", "kimi-k3") {
		t.Fatal("other scopes must survive a scoped unblock")
	}
}

// 备份导入的封禁表合并语义（2026-11-05 用户定，取代整体替换）：
// 封禁记录是跨机各自增长的运维状态，备份互导是对齐手段——
//
//	本机独有条目必须保留（旧语义会抹掉另一侧较新的封禁，被抹的模型重回
//	/v1/models 并被 hermes 写回渠道，2026-11-04 生产实证）；
//	备份新增条目吸收；同 key 取 createdAt 较新的记录。
func TestImportModelBlacklistMerges(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	// 本机已有两条：旧记录 + 新记录。
	old := &modelBlacklistEntry{Platform: platAMD, Model: "amd:GLM-5.3-Flash",
		Reason: BanReasonManual, CreatedAt: time.Date(2026, 10, 27, 14, 10, 16, 0, time.UTC)}
	recent := &modelBlacklistEntry{Platform: platAMD, Model: "amd:MiMo-V2.6-Flash",
		Reason: BanReasonManual, CreatedAt: time.Date(2026, 11, 4, 23, 16, 18, 0, time.UTC)}
	modelBlacklistMu.Lock()
	modelBlacklistItems = map[string]*modelBlacklistEntry{
		blacklistKey(platAMD, "amd:GLM-5.3-Flash"):   old,
		blacklistKey(platAMD, "amd:MiMo-V2.6-Flash"): recent,
	}
	modelBlacklistMu.Unlock()

	// 备份带三条：GLM 同 key 但更旧（应被本机新记录保留）、MiMo 同 key 且更
	// 新（应覆盖本机）、wbgb 为备份新增（应吸收）。
	in := map[string]*modelBlacklistEntry{
		blacklistKey(platAMD, "amd:GLM-5.3-Flash"): {
			Platform: platAMD, Model: "amd:GLM-5.3-Flash", Reason: BanReasonManual,
			CreatedAt: time.Date(2026, 10, 27, 14, 10, 16, 0, time.UTC).Add(-time.Hour),
		},
		blacklistKey(platAMD, "amd:MiMo-V2.6-Flash"): {
			Platform: platAMD, Model: "amd:MiMo-V2.6-Flash", Reason: BanReasonManual,
			CreatedAt: recent.CreatedAt.Add(time.Hour),
		},
		blacklistKey(platWorkBuddy, "wbgb:gpt-6-luna"): {
			Platform: platWorkBuddy, Model: "wbgb:gpt-6-luna", Reason: BanReasonManual,
			CreatedAt: time.Date(2026, 11, 4, 23, 48, 46, 0, time.UTC),
		},
	}
	importModelBlacklist(in)

	if len(modelBlacklistItems) != 3 {
		t.Fatalf("merged list = %d entries, want 3", len(modelBlacklistItems))
	}
	if got := modelBlacklistItems[blacklistKey(platAMD, "amd:GLM-5.3-Flash")]; got != old {
		t.Fatal("local-only-newer GLM entry must survive an older backup record")
	}
	if got := modelBlacklistItems[blacklistKey(platAMD, "amd:MiMo-V2.6-Flash")]; got == recent {
		t.Fatal("backup's newer MiMo record must replace the local older one")
	}
	if got := modelBlacklistItems[blacklistKey(platWorkBuddy, "wbgb:gpt-6-luna")]; got == nil {
		t.Fatal("backup-only wbgb entry must be absorbed")
	}
	if !IsModelBlacklisted("amd:GLM-5.3-Flash") || !IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("all merged entries must be enforceable")
	}
}

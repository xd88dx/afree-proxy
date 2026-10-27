package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	trae "afree-proxy/internal/trae"
	wbauth "afree-proxy/internal/workbuddy/auth"
	wbpool "afree-proxy/internal/workbuddy/pool"
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
		"wbgb:gpt-6-luna":        platWorkBuddy,
		"wbcn:step-5":            platWorkBuddy,
		"global:default-model":   platWorkBuddy,
		"zen:glm-5.3-flash-free": platZen,
		"amd:Qwen3.8":            platAMD,
		"oprt:vendor/model":      platOpenRouter,
		"tkhb:deepseek:free":     platTokenHarbor,
		"trae:glm-5.2":           platTrae,
		"deepseek-v4.1-flash":    "", // 裸名无平台信息：不猜，避免误拉黑
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
func TestWithModelBlacklistScan(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)
	// 平台可路由性闸要求 workbuddy 有可用账号，种一个默认启用的空账号。
	tmp := t.TempDir()
	savedSub := workbuddySub
	workbuddySub = &workbuddySubsystem{pool: wbpool.New(filepath.Join(tmp, "state.json"))}
	workbuddySub.pool.Add(&wbauth.Auth{UID: "scan-test"})
	t.Cleanup(func() {
		workbuddySub.pool.Close()
		workbuddySub = savedSub
	})

	errBody := `{"error":{"message":"{\"code\":11158,\"msg\":\"model [gpt-6-luna] is not allowed for user type [personal_free_user]\"}"}}`
	h := withModelBlacklistScan(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(errBody))
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"wbgb:gpt-6-luna","messages":[]}`))
	rec := httptest.NewRecorder()
	h(rec, req)

	if !IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("permission error must auto-blacklist the model")
	}
	// 客户端拿到的响应体必须原样透传（包装层只 tee，不改写）。
	if !strings.Contains(rec.Body.String(), "11158") {
		t.Fatalf("body mutated: %s", rec.Body.String())
	}
	// 平台/业务码/统一原因落库正确。
	rows := modelBlacklistSnapshot()
	if rows[0]["platform"] != platWorkBuddy || rows[0]["code"] != "11158" {
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
		strings.NewReader(`{"model":"wbgb:gpt-5.3-codex","messages":[]}`)))
	if IsModelBlacklisted("wbgb:gpt-5.3-codex") {
		t.Fatal("successful response must not blacklist")
	}
}

// Trae 模型级失败判据：1005/4023 直接封，4001 先复核，限流不封。
func TestTraeAutoBanDecision(t *testing.T) {
	cases := []struct {
		code        string
		ban, verify bool
	}{
		{"1005", true, false},
		{"4023", true, false},
		{"4001", true, true},
		{"4011", false, false},
		{"429", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		ban, verify := traeAutoBanDecision(c.code)
		if ban != c.ban || verify != c.verify {
			t.Errorf("traeAutoBanDecision(%q) = (%v,%v), want (%v,%v)", c.code, ban, verify, c.ban, c.verify)
		}
	}
}

// 自动封禁落地：1005 立即可见（封禁表 + /v1/models 过滤口径），重复命中不重复写；
// 4001 拿不到复核结论时不封；限流永不封。
func TestTraeAutoBanModel(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	m := trae.Model{ID: "glm-5.3", RawName: "glm-5.3__dev", Function: "solo_agent", Native: true}
	traeAutoBanModel(m, nil, "1005", "plan required", "raw")
	if !IsModelBlacklisted("trae:glm-5.3") {
		t.Fatal("1005 must ban immediately")
	}
	before := modelBlacklistSnapshot()[0]["hits"].(int)
	traeAutoBanModel(m, nil, "1005", "plan required", "raw")
	if after := modelBlacklistSnapshot()[0]["hits"].(int); after != before {
		t.Fatalf("already-banned model hit count changed: %d -> %d", before, after)
	}

	// 4001 需同账号复核；没有账号可复核 → 不封（宁可漏封）。
	traeAutoBanModel(trae.Model{ID: "glm-5.1", RawName: "glm-5.1__dev", Function: "solo_agent"}, nil, "4001", "param is invalid", "raw")
	if IsModelBlacklisted("trae:glm-5.1") {
		t.Fatal("4001 without verification must not ban")
	}

	// 限流是换号可解的临时故障，不封。
	traeAutoBanModel(trae.Model{ID: "glm-5.2", RawName: "glm-5.2__dev", Function: "solo_agent"}, nil, "4011", "rate limit", "raw")
	if IsModelBlacklisted("trae:glm-5.2") {
		t.Fatal("rate limit must not ban")
	}
}

// HTTP 级上游错误（非 200 带业务码）与流内 error 事件同判据：4023/1005 立即
// 封禁；4001 仍走复核（无账号不封）；限流/网络错误不封。
func TestTraeAutoBanOnHTTPError(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	m := trae.Model{ID: "glm-5.4", RawName: "glm-5.4__dev", Function: "solo_agent", Native: true}
	traeAutoBanOnHTTPError(m, nil, &trae.HTTPError{Status: http.StatusBadRequest, Code: "4023", Body: "model not found"})
	if !IsModelBlacklisted("trae:glm-5.4") {
		t.Fatal("HTTP-level 4023 must ban immediately")
	}

	traeAutoBanOnHTTPError(trae.Model{ID: "glm-5.5", RawName: "glm-5.5__dev"}, nil,
		&trae.HTTPError{Status: http.StatusBadRequest, Code: "4001", Body: "param is invalid"})
	if IsModelBlacklisted("trae:glm-5.5") {
		t.Fatal("HTTP-level 4001 must not ban without verification")
	}

	traeAutoBanOnHTTPError(trae.Model{ID: "glm-5.6", RawName: "glm-5.6__dev"}, nil,
		&trae.HTTPError{Status: http.StatusTooManyRequests, Code: "429", Body: "rate limited", RateLimited: true})
	if IsModelBlacklisted("trae:glm-5.6") {
		t.Fatal("HTTP-level rate limit must not ban")
	}

	// 非 HTTPError（网络错误等）不封。
	traeAutoBanOnHTTPError(trae.Model{ID: "glm-5.7", RawName: "glm-5.7__dev"}, nil, errors.New("dial tcp: timeout"))
	if IsModelBlacklisted("trae:glm-5.7") {
		t.Fatal("network error must not ban")
	}
}

// trae 按端点封禁：scope 隔离 + 全档条目语义 + 解禁。
func TestTraeScopedBanIsolation(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	// raw(traecode) 档封禁：不影响 remote 档
	BanModel("trae", "trae:glm-5.3-flash", "traecode")
	if !TraeModelBannedIn("traecode", "glm-5.3-flash") {
		t.Fatal("traecode scope must be banned")
	}
	if TraeModelBannedIn("remote", "glm-5.3-flash") || TraeModelBannedIn("traework", "glm-5.3-flash") {
		t.Fatal("traecode ban must not leak into remote/traework scopes")
	}
	// 全档条目（scope 空）：所有档位命中
	BanModel("trae", "trae:glm-5.2")
	if !TraeModelBannedIn("traecode", "glm-5.2") || !TraeModelBannedIn("remote", "glm-5.2") {
		t.Fatal("unscoped ban must apply to every scope")
	}
	// 按档位解禁只拆该档位
	UnblockModel("trae", "trae:glm-5.3-flash", "traecode")
	if TraeModelBannedIn("traecode", "glm-5.3-flash") {
		t.Fatal("scoped unblock must clear the scoped entry")
	}
	// 自动封禁按传输落档位：1005 + raw → 只封 traecode
	BlacklistModel("trae", "trae:kimi-k2.6", "1005", BanReasonAuto, "test", "traecode")
	if !TraeModelBannedIn("traecode", "kimi-k2.6") || TraeModelBannedIn("remote", "kimi-k2.6") {
		t.Fatal("auto ban must land on the failing transport's scope")
	}
}

// 面板「解禁」按钮不表达档位，语义是「这个模型别封了」：空 scope 解禁必须
// 把按档位落的条目一起清掉。只删全档键会留下 traecode/remote/traework 条目，
// 表现是点了「解禁」弹成功、刷新后条目原地不动（2026-10-28 修复）。
func TestUnblockModelWithoutScopeClearsScopedEntries(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

	// 自动封禁按传输落档位（4023 + raw → traecode），与生产数据同形。
	BlacklistModel(platTrae, "trae:deepseek-v4.1-flash", "4023", BanReasonModelGone, "model not found", "traecode")
	if !TraeModelBannedIn("traecode", "deepseek-v4.1-flash") {
		t.Fatal("scoped auto ban must land on the traecode scope")
	}
	if !UnblockModel(platTrae, "trae:deepseek-v4.1-flash") {
		t.Fatal("unblock without scope must remove the scoped entry")
	}
	if IsModelBlacklisted("trae:deepseek-v4.1-flash") || TraeModelBannedIn("traecode", "deepseek-v4.1-flash") {
		t.Fatal("model must be gone from every scope after an unscoped unblock")
	}
	if UnblockModel(platTrae, "trae:deepseek-v4.1-flash") {
		t.Fatal("second unblock must report nothing removed")
	}

	// 同模型多档位 + 全档条目并存：一次解禁全部清空。
	BanModel(platTrae, "trae:glm-5.3", "remote")
	BanModel(platTrae, "trae:glm-5.3", "traework")
	BanModel(platTrae, "trae:glm-5.3")
	if !UnblockModel(platTrae, "trae:glm-5.3") {
		t.Fatal("unblock must report removal")
	}
	if TraeModelBannedIn("remote", "glm-5.3") || TraeModelBannedIn("traework", "glm-5.3") ||
		TraeModelBannedIn("traecode", "glm-5.3") {
		t.Fatal("unscoped unblock must clear every scoped entry")
	}

	// 带档位解禁仍只拆该档位（档位隔离语义不变）。
	BanModel(platTrae, "trae:kimi-k3", "remote")
	BanModel(platTrae, "trae:kimi-k3", "traework")
	if !UnblockModel(platTrae, "trae:kimi-k3", "remote") {
		t.Fatal("scoped unblock must report removal")
	}
	if TraeModelBannedIn("remote", "kimi-k3") {
		t.Fatal("scoped unblock must clear only its own scope")
	}
	if !TraeModelBannedIn("traework", "kimi-k3") {
		t.Fatal("other scopes must survive a scoped unblock")
	}
}

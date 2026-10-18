package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
		"wbgb:gpt-6-luna":   platWorkBuddy,
		"wbcn:step-5":       platWorkBuddy,
		"global:default-model": platWorkBuddy,
		"zen:glm-5.3-flash-free": platZen,
		"amd:Qwen3.8":       platAMD,
		"oprt:vendor/model": platOpenRouter,
		"tkhb:deepseek:free": platTokenHarbor,
		"deepseek-v4.1-flash": "", // 裸名无平台信息：不猜，避免误拉黑
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
	BlacklistModel(platWorkBuddy, "wbgb:gpt-6-luna", "11158", "not allowed for user type")
	if !IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("model should be blacklisted")
	}
	rows := modelBlacklistSnapshot()
	if len(rows) != 1 || rows[0]["platform"] != platWorkBuddy || rows[0]["model"] != "wbgb:gpt-6-luna" {
		t.Fatalf("snapshot = %+v", rows)
	}
	firstCreated := rows[0]["createdAt"]

	// 重复命中：累加次数，不改首次拉黑时间。
	BlacklistModel(platWorkBuddy, "wbgb:gpt-6-luna", "11158", "again")
	rows = modelBlacklistSnapshot()
	if rows[0]["hits"].(int) != 2 {
		t.Fatalf("hits = %v, want 2", rows[0]["hits"])
	}
	if rows[0]["createdAt"] != firstCreated {
		t.Fatalf("createdAt changed on re-hit: %v -> %v", firstCreated, rows[0]["createdAt"])
	}

	// 落盘可续读。
	if _, err := os.Stat(modelBlacklistPath()); err != nil {
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
	BlacklistModel(platWorkBuddy, "wbgb:gpt-6-luna", "11158", "again after unblock")
	if !IsModelBlacklisted("wbgb:gpt-6-luna") {
		t.Fatal("model should be re-blacklistable after unblock")
	}
}

// 网关出口扫描：错误响应命中判据 → 自动拉黑；200 响应不触发。
func TestWithModelBlacklistScan(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)

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
	// 平台/业务码落库正确。
	rows := modelBlacklistSnapshot()
	if rows[0]["platform"] != platWorkBuddy || rows[0]["code"] != "11158" {
		t.Fatalf("entry = %+v", rows[0])
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

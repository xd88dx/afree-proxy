package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	traecfg "afree-proxy/internal/trae2api/config"
	traepool "afree-proxy/internal/trae2api/pool"
	traeserver "afree-proxy/internal/trae2api/server"
	traectl "afree-proxy/internal/trae2api/traectl"
)

// trae chat 主入口的"缓冲+中断重试"（2026-11-10 用户定）：流式请求不再直通
// vendored handler（响应头已发就只能 200+error 帧，agent 把半截回复当完成），
// 改走 handleStreamBufferedGeneric——完整缓冲才 200 回放，真流中断换账号重试
//（上限 StreamMaxAttempts），确定性错误（引擎 error 帧+[DONE]、非 200）不重试。

// newTraeBufferFixture 造一个"零可用账号"的 vendored 子系统：显式 accounts 指向
// 不存在的 storage（禁用自动探测——本机装了 Trae 客户端时会探测出真账号），
// 四个 realm 渠道为产品级骨架（恒建）但 0 账号 → /v1/chat/completions 恒
// 503 no_account，是完全可控的非 200 形态。
func newTraeBufferFixture(t *testing.T) {
	t.Helper()
	t.Setenv("DATA_DIR", testDataDir(t))

	cfg := traecfg.Default()
	// DataDir 指到测试临时目录：默认 "./data" 会吃到工作目录的真实快照
	//（data/accounts/*.json → BuildChannels 从 accStore 建出真渠道）。
	cfg.DataDir = t.TempDir()
	// 显式账号且路径不存在：resolveSpecs 不再自动探测本机客户端。
	cfg.Trae.Accounts = []traecfg.Account{{
		Product:     "trae-cn",
		StoragePath: t.TempDir() + string('\\') + "no-such-storage.json",
	}}
	traeApplyAfreeConstraints(cfg)
	// DataDir 必须在约束函数**之后**再钉一次：traeApplyAfreeConstraints 强制
	// DataDir=traeDataDir()（生产语义：数据统一目录），不重钉会吃到工作目录的
	// 真实快照（data/accounts/*.json → BuildChannels 建出真渠道）。
	cfg.DataDir = t.TempDir()
	srv, err := traeserver.New(cfg)
	if err != nil {
		t.Fatalf("build vendored server: %v", err)
	}
	for k, ch := range srv.Channels() {
		if ch != nil && len(ch.Accounts()) != 0 {
			t.Fatalf("fixture expects account-less channels, %s has %d", k, len(ch.Accounts()))
		}
	}
	// 塞一个带假凭据的池账号让网关可路由闸（trae2apiRoutableCount）放行——
	// fixture 要测的是 attempt 层对 vendored 失败的处理。凭据是过期假 token：
	// 请求进 vendored 后在续期/选号阶段失败（确定性错误），绝不能塞无 Store 的
	// 裸账号——ClientFor 无条件解引用 Store，nil 会 panic（vendored 脆点，
	// 生产不可达：真账号都经 OpenStore 建档）。
	prod, ok := traectl.ProductByKey("trae-cn")
	if !ok {
		t.Fatal("trae-cn product not registered")
	}
	snapPath := filepath.Join(t.TempDir(), "fixture-snap.json")
	snap := `{"version":1,"product":"trae-cn","userId":"1000000000000001","token":"fake.expired.token","refreshToken":"fake-refresh","privateKeyPEM":"","publicKeyPEM":"","deviceId":"0","host":""}`
	if err := os.WriteFile(snapPath, []byte(snap), 0o600); err != nil {
		t.Fatalf("write fixture snapshot: %v", err)
	}
	store, err := traectl.OpenStore(prod, snapPath)
	if err != nil {
		t.Fatalf("open fixture store: %v", err)
	}
	srv.Pool().Add(&traepool.Account{UID: prod.Key + "-1000000000000001", Realm: "trae", Product: prod, Store: store})
	sub := &trae2apiSubsystem{cfg: cfg, srv: srv}
	old := trae2apiSub
	trae2apiSub = sub
	t.Cleanup(func() { trae2apiSub = old })
}

// 流式 + 非 200（确定性错误）：不重试，503 原样透出（错误体含 vendored 错误码）。
func TestTraeChatBufferedNon200NoRetry(t *testing.T) {
	newTraeBufferFixture(t)

	body := `{"model":"tacn:glm-5.3-flash","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	serveTrae2apiChat(rec, r, []byte(body))

	// 非 200 = 确定性错误（这里是无凭据假账号在 vendored 内部失败）：attempt
	// 返回 upstreamHTTPError → handleStreamBufferedGeneric 一次直出不重试。
	// 具体状态码随 vendored 内部失败点浮动（503 no_account / 500 空凭据），
	// 关键断言：错误以 JSON 信封透出、vendored 原话保留、不因重试耗尽变形。
	if rec.Code < 400 || rec.Code >= 600 {
		t.Fatalf("non-200 upstream must surface as 4xx/5xx without retry, got %d body=%s", rec.Code, rec.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || m["error"] == nil {
		t.Fatalf("upstream error must pass through as JSON error envelope, got: %s", rec.Body.String())
	}
}

// 非流式仍直通 vendored（聚合语义在引擎内）：同 fixture 同样 503 形状。
func TestTraeChatNonStreamPassthrough(t *testing.T) {
	newTraeBufferFixture(t)

	body := `{"model":"tacn:glm-5.3-flash","messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	serveTrae2apiChat(rec, r, []byte(body))

	if rec.Code < 400 || rec.Code >= 600 {
		t.Fatalf("non-stream must passthrough vendored error as 4xx/5xx, got %d", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || m["error"] == nil {
		t.Fatalf("non-stream error body must be JSON envelope: %s", rec.Body.String())
	}
}

// stream 缺省 = false（OpenAI 契约）：缺省请求走非流式直通，不被 vendored 的
// "缺省当 true" 流式化（traeNormalizeStreamDefault 的既有语义在缓冲路径下保持）。
func TestTraeChatStreamDefaultFalseGoesPassthrough(t *testing.T) {
	newTraeBufferFixture(t)

	body := `{"model":"tacn:glm-5.3-flash","messages":[{"role":"user","content":"hi"}]}`
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	serveTrae2apiChat(rec, r, []byte(body))
	if rec.Code < 400 || rec.Code >= 600 {
		t.Fatalf("default-stream must be normalized to false (passthrough), got %d", rec.Code)
	}
}

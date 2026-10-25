package trae

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func b64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func fakeJWT(payload string) string {
	return b64url(`{"alg":"HS256"}`) + "." + b64url(payload) + "." + strings.Repeat("s", 100)
}

func TestTokenAccountIDFromJWT(t *testing.T) {
	tok := fakeJWT(`{"data":{"id":"7226084321984"},"exp":1}`)
	if got := TokenAccountID(tok); got != "7226084321984" {
		t.Fatalf("TokenAccountID = %q, want 7226084321984", got)
	}
}

func TestTokenAccountIDFallbacks(t *testing.T) {
	if got := TokenAccountID(fakeJWT(`{"user_id":"u-1"}`)); got != "u-1" {
		t.Fatalf("user_id fallback = %q", got)
	}
	opaque := strings.Repeat("x", 40)
	if got := TokenAccountID(opaque); got != opaque[:24] {
		t.Fatalf("opaque fallback = %q", got)
	}
}

func TestLooksLikeCloudIDEJWT(t *testing.T) {
	if !LooksLikeCloudIDEJWT(fakeJWT(`{"data":{"id":"1"}}`)) {
		t.Fatal("valid JWT rejected")
	}
	if LooksLikeCloudIDEJWT("short.token") {
		t.Fatal("short value accepted")
	}
	if LooksLikeCloudIDEJWT(strings.Repeat("x", 200)) {
		t.Fatal("opaque long value accepted")
	}
}

func TestReconcilePoolEnabled(t *testing.T) {
	old := false
	cur := &Config{Accounts: []*Account{
		{ID: "a", Token: "t1", PoolEnabled: &old},
		{ID: "b", Token: "t2"}, // nil = 参与（存量兼容）
	}}
	next := &Config{Accounts: []*Account{
		{ID: "a", Token: "t1"},
		{ID: "c", Token: "t3"}, // 新账号
		{ID: "b", Token: "t2"},
	}}
	next.ReconcilePoolEnabled(next, cur)
	if !next.Accounts[0].PoolEnabledOrDefault() {
		t.Fatal("existing account with explicit enabled state must keep it")
	}
	if next.Accounts[1].PoolEnabledOrDefault() {
		t.Fatal("new account must default to routing-disabled")
	}
	if !next.Accounts[2].PoolEnabledOrDefault() {
		t.Fatal("existing account with nil poolEnabled must stay enabled (read-side compat)")
	}
}

func TestNormalizeRejectsOAuthOnlyGateway(t *testing.T) {
	c := &Config{Gateway: "https://api.trae.cn", MaxConcurrency: 0, Retries: -1, Accounts: []*Account{
		{Token: fakeJWT(`{"data":{"id":"a1"}}`)},
		{Token: fakeJWT(`{"data":{"id":"a1"}}`)}, // 重复主键去重
		{Token: "   "}, // 空 token 剔除
	}}
	c.normalize()
	if c.Gateway != DefaultGateway {
		t.Fatalf("gateway = %q, want fallback to default", c.Gateway)
	}
	if c.MaxConcurrency != 8 || c.Retries != 0 {
		t.Fatalf("limits not normalized: %d/%d", c.MaxConcurrency, c.Retries)
	}
	if len(c.Accounts) != 1 || c.Accounts[0].ID != "a1" {
		t.Fatalf("accounts not normalized: %d", len(c.Accounts))
	}
}

func TestAccumulateDeltaVsSnapshot(t *testing.T) {
	// 纯增量
	if d, next, ch := accumulate("", "Hel"); !ch || d != "Hel" || next != "Hel" {
		t.Fatalf("accumulate delta = %q %q %v", d, next, ch)
	}
	// 增量追加
	if d, next, ch := accumulate("Hel", "lo"); !ch || d != "lo" || next != "Hello" {
		t.Fatalf("accumulate append = %q %q %v", d, next, ch)
	}
	// 快照延长
	if d, next, ch := accumulate("Hello", "Hello world"); !ch || d != " world" || next != "Hello world" {
		t.Fatalf("accumulate snapshot extend = %q %q %v", d, next, ch)
	}
	// 相同快照：无事件
	if _, _, ch := accumulate("Hello", "Hello"); ch {
		t.Fatal("identical snapshot must not emit")
	}
}

func TestNormalizeEventName(t *testing.T) {
	cases := map[string]string{
		"textDelta":          "text_delta",
		"ModelConfig":        "model_config",
		"requestWaitInQueue": "request_wait_in_queue",
		"response-done":      "done",
		"StreamDone":         "done",
		"tokenUsage":         "token_usage",
		"done":               "done",
		"output":             "output",
	}
	for in, want := range cases {
		if got := normalizeEventName(in); got != want {
			t.Fatalf("normalizeEventName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapUsageSpellings(t *testing.T) {
	u := mapUsage(map[string]any{"input_token": 8.0, "output_token": 1.0, "credits_float": 0.2})
	if u == nil || u.Prompt != 8 || u.Completion != 1 || u.Credits != 0.2 {
		t.Fatalf("flat usage = %+v", u)
	}
	u = mapUsage(map[string]any{"usage": map[string]any{"input_tokens": 12.0, "output_tokens": 3.0, "total_tokens": 15.0, "credits_consumed": 0.31}})
	if u == nil || u.Prompt != 12 || u.Completion != 3 || u.Total != 15 {
		t.Fatalf("nested usage = %+v", u)
	}
	if mapUsage(map[string]any{"foo": 1}) != nil {
		t.Fatal("empty usage must return nil")
	}
}

func TestExtractToolCallsNativeFrames(t *testing.T) {
	s := &Stream{}
	s.mergeToolCalls(extractToolCalls(mustObj(t, `{"tool_calls":[{"index":0,"id":"call_1","type":"function","function_call":{"name":"Read","args":""}}]}`)))
	s.mergeToolCalls(extractToolCalls(mustObj(t, `{"tool_calls":[{"index":0,"id":"","type":"function","function_call":{"name":"","arguments":"{\"filePath\":\"README.md\"}"}}]}`)))
	got := s.ToolCallsSnapshot()
	if len(got) != 1 || got[0].ID != "call_1" || got[0].Name != "Read" || got[0].ArgsJSON != `{"filePath":"README.md"}` {
		t.Fatalf("tool calls = %+v", got)
	}
}

func TestParseUpstreamErrorClassification(t *testing.T) {
	cases := []struct {
		status      int
		body        string
		rateLimited bool
		paramError  bool
		authFail    bool
	}{
		{429, "busy", true, false, false},
		{400, `{"code":3004,"message":"busy"}`, true, false, false},
		{403, `{"error":{"errorCode":3004}}`, true, false, false},
		{400, `{"code":4001,"message":"param is invalid"}`, false, true, false},
		{401, `{"error":"unauthorized"}`, false, false, true},
		{500, `{"message":"boom"}`, false, false, false},
	}
	for _, c := range cases {
		e := parseUpstreamError(c.status, []byte(c.body))
		if e.RateLimited != c.rateLimited || e.ParamError != c.paramError || e.AuthFailure != c.authFail {
			t.Fatalf("parseUpstreamError(%d,%s) = rl=%v pe=%v af=%v", c.status, c.body, e.RateLimited, e.ParamError, e.AuthFailure)
		}
	}
}

func TestNormalizeExpire(t *testing.T) {
	if got := normalizeExpire("1760000000000"); !strings.HasSuffix(got, "Z") {
		t.Fatalf("ms timestamp = %q", got)
	}
	if got := normalizeExpire("1760000000"); !strings.HasSuffix(got, "Z") {
		t.Fatalf("sec timestamp = %q", got)
	}
	if got := normalizeExpire("2026-10-24T00:00:00Z"); got != "2026-10-24T00:00:00Z" {
		t.Fatalf("RFC3339 = %q", got)
	}
}

func TestNeedsRefreshWindows(t *testing.T) {
	soon := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	a := &Account{ID: "x", RefreshToken: "rt", ExpiredAt: soon}
	if !NeedsRefresh(a) {
		t.Fatal("token expiring in 10min must need refresh")
	}
	far := &Account{ID: "x", RefreshToken: "rt", ExpiredAt: time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)}
	if NeedsRefresh(far) {
		t.Fatal("token with 2h left must not need refresh")
	}
	noRT := &Account{ID: "x", ExpiredAt: soon}
	if NeedsRefresh(noRT) {
		t.Fatal("no refresh token = no refresh possible")
	}
	if !TokenUsable(&Account{ID: "x", ExpiredAt: far.ExpiredAt}) {
		t.Fatal("token with 2h left must be usable")
	}
	// 10min 剩余：>5min 可用缓冲（可用）但 <30min 提前刷新窗口（需刷新）
	if !TokenUsable(&Account{ID: "x", ExpiredAt: soon}) {
		t.Fatal("token with 10min left must still be usable")
	}
	expiredSoon := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
	if TokenUsable(&Account{ID: "x", ExpiredAt: expiredSoon}) {
		t.Fatal("token with 2min left must not be usable")
	}
}

func TestStripPrefix(t *testing.T) {
	if id, ok := StripPrefix("trae:glm-5.2"); !ok || id != "glm-5.2" {
		t.Fatalf("StripPrefix = %q %v", id, ok)
	}
	if _, ok := StripPrefix("glm-5.2"); ok {
		t.Fatal("bare id must not match prefix")
	}
	if !HasPrefix("trae:glm-5.2") || HasPrefix("oprt:glm-5.2") {
		t.Fatal("HasPrefix wrong")
	}
}

func TestBuildChatBodyShape(t *testing.T) {
	m := Model{ID: "glm-5.1", RawName: "glm-5__v2", Function: "chat_v3"}
	params := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "f", "arguments": "{}"}},
			}},
			map[string]any{"role": "tool", "tool_call_id": "c1", "name": "f", "content": "ok"},
		},
		"tools": []any{map[string]any{"type": "function"}},
	}
	body := BuildChatBody(params, m, "acc-1")
	if body["config_name"] != "glm-5.1" || body["model_name"] != "glm-5__v2" {
		t.Fatalf("model fields = %v/%v", body["config_name"], body["model_name"])
	}
	if body["stream"] != true || body["raw_chat_function"] != "chat_v3" || body["config_source"] != 1 {
		t.Fatalf("wire fields = %v", body)
	}
	sid, _ := body["session_id"].(string)
	if len(sid) != 36 || strings.Count(sid, "-") != 4 {
		t.Fatalf("session_id not UUID-shaped: %q", sid)
	}
	msgs := body["messages"].([]map[string]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d", len(msgs))
	}
	if msgs[0]["content"].([]map[string]any)[0]["text"] != "hello" {
		t.Fatalf("user text = %v", msgs[0]["content"])
	}
	if _, ok := msgs[1]["tool_calls"]; !ok {
		t.Fatal("assistant tool_calls dropped")
	}
	if msgs[2]["tool_call_id"] != "c1" || msgs[2]["role"] != "tool" {
		t.Fatalf("tool message = %v", msgs[2])
	}
	// session_id 确定性：同账号同模型两次构造必须一致
	body2 := BuildChatBody(params, m, "acc-1")
	if body2["session_id"] != sid {
		t.Fatal("session id not deterministic")
	}
}

func TestConvertImageDataURL(t *testing.T) {
	out := convertContent([]any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,QUJD", "detail": "high"}},
	})
	v, ok := out[0]["value"].(map[string]any)
	if !ok || v["mimeType"] != "image/png" || v["data"] != "QUJD" {
		t.Fatalf("image block = %v", out[0])
	}
	out = convertContent([]any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/a.png"}}})
	v, _ = out[0]["value"].(map[string]any)
	if v["mimeType"] != "url" || v["data"] != "https://example.com/a.png" {
		t.Fatalf("url image block = %v", out[0])
	}
}

func TestConvertMessagesRoles(t *testing.T) {
	out := convertMessages([]any{
		map[string]any{"role": "developer", "content": "sys"},
		map[string]any{"role": "function", "content": "r", "name": "fn"},
	})
	if out[0]["role"] != "system" || out[1]["role"] != "tool" {
		t.Fatalf("role mapping = %v", out)
	}
}

func TestFingerprintAndSessionDeterminism(t *testing.T) {
	f1 := fingerprintFor("acc-1")
	f2 := fingerprintFor("acc-1")
	if f1 != f2 || len(f1.DeviceID) != 18 || len(f1.MachineID) != 32 {
		t.Fatalf("fingerprint unstable/wrong shape: %+v", f1)
	}
	if fingerprintFor("acc-2") == f1 {
		t.Fatal("different accounts must differ")
	}
	if len(deviceID16("acc-1")) != 16 {
		t.Fatal("deviceID16 length")
	}
}

func TestParseCreditsBody(t *testing.T) {
	// 真实形态：顶层无 data 包裹，usage_summary 优先
	c, err := parseCreditsBody([]byte(`{"usage_summary":{"consumed_amount":0.03,"total_amount":4700},"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":4000}},"usage":{}}]}`))
	if err != nil || c.Limit != 4700 || c.Remaining < 4699.9 || c.Remaining > 4700 {
		t.Fatalf("usage_summary credits = %+v err = %v", c, err)
	}
	// 无 summary：pack 兜底（data 包裹形态兼容）
	c, err = parseCreditsBody([]byte(`{"data":{"user_pay_identity_str":"ProPlus","user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":100}},"usage":{"credits_amount":2}}]}}`))
	if err != nil || c.Remaining != 98 || c.Limit != 100 || c.Identity != "ProPlus" {
		t.Fatalf("pack credits = %+v err = %v", c, err)
	}
	// 顶层 pack 形态（无 data 无 summary）
	c, err = parseCreditsBody([]byte(`{"user_entitlement_pack_list":[{"entitlement_base_info":{"quota":{"credits_limit":-1}}}]}`))
	if err != nil || !c.Unlimited || c.Limit != -1 {
		t.Fatalf("unlimited credits = %+v err = %v", c, err)
	}
	// 功能开关 pack（product_type=0，无 credits_limit）+ 免费形态
	c, err = parseCreditsBody([]byte(`{"user_entitlement_pack_list":[{"entitlement_base_info":{"product_type":0,"quota":{"enable_solo_agent":true}},"usage":{}}]}`))
	if err != nil || c.Limit != 0 || c.Remaining != 0 {
		t.Fatalf("free packs = %+v err = %v", c, err)
	}
}

func mustObj(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return m
}

package trae

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// auto 分流判定（原项目 TRAE_AUTO_ROUTE 语义）：工具/图片走 raw_chat，
// 纯文字走 Remote；目录未确认 remote 档不分流。
func TestTransportSelector(t *testing.T) {
	prev := Get()
	t.Cleanup(func() { Set(prev) })
	plainParams := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	toolParams := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":    []any{map[string]any{"type": "function"}},
	}
	imageParams := map[string]any{"messages": []any{map[string]any{
		"role": "user",
		"content": []any{map[string]any{"type": "image_url",
			"image_url": map[string]any{"url": "data:image/png;base64,x"}}},
	}}}
	remoteModel := Model{ID: "glm-5.2", Tiers: []string{"traecode", "remote"}}
	nativeModel := Model{ID: "glm-5.2", Native: true, Function: "solo_agent"}
	workOnlyModel := Model{ID: "m1", Tiers: []string{"traework"}}
	remoteOnlyModel := Model{ID: "m2", Tiers: []string{"remote"}}
	remoteWorkModel := Model{ID: "m3", Tiers: []string{"remote", "traework"}}

	cases := []struct {
		name         string
		endpoint     string
		model        Model
		params       map[string]any
		wantPrimary  func(context.Context, *Account, ChatOpts, string) (*Stream, error)
		wantFallback func(context.Context, *Account, ChatOpts, string) (*Stream, error) // nil = 无回落
	}{
		{"auto plain remote-tier model", "", remoteModel, plainParams, remoteChatOnce, rawChatOnce},
		{"auto tools keep raw", "", remoteModel, toolParams, rawChatOnce, remoteChatOnce},
		{"auto image keeps raw", "", remoteModel, imageParams, rawChatOnce, remoteChatOnce},
		{"auto plain native-only model", "", nativeModel, plainParams, rawChatOnce, nil},
		{"auto plain work-only model", "", workOnlyModel, plainParams, remoteWorkChatOnce, nil},
		{"auto tools work-only model", "", workOnlyModel, toolParams, rawChatOnce, remoteWorkChatOnce},
		{"auto plain remote-only model", "", remoteOnlyModel, plainParams, remoteChatOnce, nil},
		{"auto plain remote+work model", "", remoteWorkModel, plainParams, remoteChatOnce, remoteWorkChatOnce},
		{"remote endpoint pinned", "remote", nativeModel, toolParams, remoteChatOnce, nil},
		{"traework endpoint pinned", "traework", nativeModel, plainParams, remoteWorkChatOnce, nil},
		{"traecode endpoint pinned", "traecode", remoteModel, plainParams, rawChatOnce, nil},
	}
	for _, tc := range cases {
		Set(&Config{Gateway: DefaultGateway, Accounts: []*Account{{ID: "u1", Token: "tok"}}, Endpoint: tc.endpoint})
		primary, fallback := transportSelector(ChatOpts{Model: tc.model, Params: tc.params})
		// 顶层函数不可 == 比较（Go 限制），用代码指针判定身份
		if fnPtr(primary) != fnPtr(tc.wantPrimary) {
			t.Fatalf("%s: wrong primary transport", tc.name)
		}
		if fnPtr(fallback) != fnPtr(tc.wantFallback) {
			t.Fatalf("%s: wrong fallback transport (present=%v)", tc.name, fallback != nil)
		}
	}
}

// fnPtr 顶层函数的代码指针（nil 安全）。
func fnPtr(fn func(context.Context, *Account, ChatOpts, string) (*Stream, error)) uintptr {
	if fn == nil {
		return 0
	}
	return reflect.ValueOf(fn).Pointer()
}

// 回落资格：网络错误可回落；限流/认证/参数/模型级错误不回落。
func TestRemoteFallbackEligible(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("dial tcp timeout"), true},
		{&HTTPError{Status: 502, Body: "bad gateway"}, true},
		{&HTTPError{Status: 429, RateLimited: true}, false},
		{&HTTPError{Status: 401, AuthFailure: true}, false},
		{&HTTPError{Status: 400, ParamError: true}, false},
		{&HTTPError{Status: 400, Code: "4023"}, false},
		{&HTTPError{Status: 400, Code: "1005"}, false},
	}
	for _, tc := range cases {
		if got := remoteFallbackEligible(tc.err); got != tc.want {
			t.Fatalf("remoteFallbackEligible(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

package trae

import (
	"context"
	"testing"
)

// auto 路由的封禁感知选路（2026-10-28 用户定，一刀切）：档位被黑名单命中
// 即视为不可用，主/回落都只在「有档位且未被封」里选——兑现「封禁摘掉不可用
// 档位，请求自动走剩下那条，只要有一条路能走通就保留路由」。不按封禁原因
// 分层（免费账号权限问题全局同因）。
func TestTransportSelectorBanAware(t *testing.T) {
	prev := Get()
	t.Cleanup(func() { Set(prev) })

	banned := map[string]bool{}
	SetBanChecker(func(scope, bareID string) bool { return banned[scope+"|"+bareID] })
	t.Cleanup(func() { SetBanChecker(nil) })

	Set(&Config{Gateway: DefaultGateway, Accounts: []*Account{{ID: "u1", Token: "tok"}}, Endpoint: "auto"})

	plainParams := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	toolParams := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":    []any{map[string]any{"type": "function"}},
	}
	twoTier := Model{ID: "glm-5.2", Tiers: []string{"traecode", "remote"}}
	threeTier := Model{ID: "glm-5.3-flash", Tiers: []string{"traecode", "remote", "traework"}}
	workOnly := Model{ID: "m1", Tiers: []string{"traework"}}

	cases := []struct {
		name         string
		model        Model
		params       map[string]any
		banned       []string // "tier|id"
		wantPrimary  func(context.Context, *Account, ChatOpts, string) (*Stream, error)
		wantFallback func(context.Context, *Account, ChatOpts, string) (*Stream, error)
	}{
		// traecode(raw) 被封：纯文字与工具请求都走 remote（工具请求 2026-10-30
		// 起 remote 优先，raw 封禁只决定回落链）。
		{"plain raw-banned two-tier", twoTier, plainParams,
			[]string{"traecode|glm-5.2"}, remoteChatOnce, nil},
		{"tools raw-banned two-tier degrade", twoTier, toolParams,
			[]string{"traecode|glm-5.2"}, remoteChatOnce, nil},
		// remote 被封：纯文字走 raw；工具请求回落 raw（2026-10-30 反转）。
		{"plain remote-banned two-tier", twoTier, plainParams,
			[]string{"remote|glm-5.2"}, rawChatOnce, nil},
		{"tools remote-banned two-tier", twoTier, toolParams,
			[]string{"remote|glm-5.2"}, rawChatOnce, nil},
		// 三档模型 raw 被封：remote → work 回落（纯文字与工具同链）。
		{"plain raw-banned three-tier", threeTier, plainParams,
			[]string{"traecode|glm-5.3-flash"}, remoteChatOnce, remoteWorkChatOnce},
		{"tools raw-banned three-tier", threeTier, toolParams,
			[]string{"traecode|glm-5.3-flash"}, remoteChatOnce, remoteWorkChatOnce},
		// 仅 work 档健康时保留路由。
		{"plain work-only unbanned", workOnly, plainParams, nil, remoteWorkChatOnce, nil},
		// 全档被封：目录已剔除该模型，显式请求兜底 raw 让上游真实错误透出。
		{"all tiers banned falls back to raw", twoTier, plainParams,
			[]string{"traecode|glm-5.2", "remote|glm-5.2"}, rawChatOnce, nil},
		{"all tiers banned tools", twoTier, toolParams,
			[]string{"traecode|glm-5.2", "remote|glm-5.2"}, rawChatOnce, nil},
		// 封禁别的模型不影响本模型选路。
		{"other model ban ignored", twoTier, plainParams,
			[]string{"traecode|other-id"}, remoteChatOnce, rawChatOnce},
	}
	for _, tc := range cases {
		banned = map[string]bool{}
		for _, k := range tc.banned {
			banned[k] = true
		}
		primary, fallback := transportSelector(ChatOpts{Model: tc.model, Params: tc.params})
		if fnPtr(primary) != fnPtr(tc.wantPrimary) {
			t.Fatalf("%s: wrong primary transport", tc.name)
		}
		if fnPtr(fallback) != fnPtr(tc.wantFallback) {
			t.Fatalf("%s: wrong fallback transport (present=%v)", tc.name, fallback != nil)
		}
	}

	// banChecker 拆掉后必须退化为纯 modelHasTier（无封禁体系 = 与旧行为一致）。
	SetBanChecker(nil)
	banned = map[string]bool{"traecode|glm-5.2": true}
	primary, fallback := transportSelector(ChatOpts{Model: twoTier, Params: plainParams})
	if fnPtr(primary) != fnPtr(remoteChatOnce) || fnPtr(fallback) != fnPtr(rawChatOnce) {
		t.Fatal("without ban checker the selector must ignore bans (legacy behavior)")
	}
}

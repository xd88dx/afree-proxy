package app

import (
	"fmt"
	"testing"
	"time"
)

// TestPlatformScopedCooldown 平台粒度冷却（2026-10-30 方案 A）：
// 个别平台封出口 IP（wb 对 codebuddy）造成的隧道后流错误，2-strike 达成后
// **只冷却该平台的视角**——绑同一出口的其它平台完全无感；而建连阶段失败
// （出口真死）仍然是全局冷却，所有平台一起避开。
func TestPlatformScopedCooldown(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{
		"socks5://u:p@10.0.0.1:11106#DDPHX",
		"socks5://u:p@10.0.0.1:11105#DDFRK",
	}})
	t.Cleanup(func() {
		proxyCooldownsMu.Lock()
		proxyPlatformCooldowns = map[string]map[int]time.Time{}
		proxyCooldowns = map[int]time.Time{}
		proxyStreamFails = map[string][]time.Time{}
		proxyCooldownsMu.Unlock()
	})

	banned := "socks5://u:p@10.0.0.1:11106#DDPHX"
	healthy := "socks5://u:p@10.0.0.1:11105#DDFRK"
	bannedIdx := proxyIdxInPool(banned)
	healthyIdx := proxyIdxInPool(healthy)
	if bannedIdx < 0 || healthyIdx < 0 {
		t.Fatalf("fixture proxies not in pool: %d/%d", bannedIdx, healthyIdx)
	}
	// codebuddy 封 IP 的形态：隧道建通后上游断流（Go 侧 url.Error EOF）
	streamErr := fmt.Errorf(`Post "https://www.codebuddy.ai/v2/chat/completions": EOF`)

	// ① 第一次隧道后流错误：不冷却，出口仍可用
	recordProxyOutcome(pkWb, banned, false, streamErr, 0)
	if !proxyAvailableFor(pkWb, bannedIdx) {
		t.Fatal("single upstream-side stream failure must not cool")
	}

	// ② 第二次（60s 窗口内）：只对 wb 冷却；amd 视角照常可用；无全局冷却
	recordProxyOutcome(pkWb, banned, false, streamErr, 0)
	if proxyAvailableFor(pkWb, bannedIdx) {
		t.Fatal("wb should see the exit cooled after 2 strikes")
	}
	if !proxyAvailableFor(pkAmd, bannedIdx) {
		t.Fatal("amd must NOT be affected by wb-scoped cooldown")
	}
	if len(proxyCooldowns) != 0 {
		t.Fatalf("platform-scoped failure must not write global cooldowns: %v", proxyCooldowns)
	}
	// 绑定选择同口径：wb 拿不到该出口，amd 照常拿到
	if _, _, ok := pickBoundProxyFor(pkWb, banned, ""); ok {
		t.Fatal("wb bound pick must skip the platform-cooled exit")
	}
	if u, _, ok := pickBoundProxyFor(pkAmd, banned, ""); !ok || u != banned {
		t.Fatalf("amd bound pick should still return the exit (got %q ok=%v)", u, ok)
	}
	// 兜底轮转同口径：wb 轮转跳过被封出口，落到健康出口
	if u, idx := pickUpstreamProxyFor(pkWb); u == banned {
		t.Fatalf("wb pool rotation must skip cooled exit (got idx=%d)", idx)
	}

	// ③ 建连阶段失败：全局冷却，所有平台一起避开
	recordProxyOutcome(pkAmd, healthy, false, fmt.Errorf("dial tcp 1.2.3.4:443: connect: connection refused"), 0)
	if proxyAvailableFor(pkAmd, healthyIdx) || proxyAvailableFor(pkWb, healthyIdx) {
		t.Fatal("establishment failure must cool the exit globally")
	}
	if len(proxyCooldowns) != 1 {
		t.Fatalf("establishment failure should write global cooldown: %v", proxyCooldowns)
	}

	// ④ strike 计数按平台隔离：wb 攒下的失败不能给 amd 凑数
	proxyStreamFailMu.Lock()
	proxyStreamFails = map[string][]time.Time{}
	proxyCooldownsMu.Lock()
	proxyPlatformCooldowns = map[string]map[int]time.Time{}
	proxyCooldownsMu.Unlock()
	proxyStreamFailMu.Unlock()
	recordProxyOutcome(pkWb, banned, false, streamErr, 0)
	if _, cool := shouldCooldownExit(pkAmd, banned, streamErr); cool {
		t.Fatal("amd strikes must not be satisfied by wb failures")
	}
}

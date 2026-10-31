package app

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestShouldCooldownExit 出口冷却的归因（2026-10-31）：
// 建连阶段失败与隧道后流错误都走 60s 窗口 2-strike——第一次不冷却，
// 窗口内第二次才冷却；上游响应类错误永不冷却。
// shouldCooldownExit 是**纯查询**（本次算第 N 次，不写计数）：cline/zen 请求
// 循环用它即时判定，真正的记账在 recordExitFailure（recordProxyOutcome 内）
// 单点完成。
func TestShouldCooldownExit(t *testing.T) {
	const p = "socks5://10.10.10.5:10106#DDPHX"
	reset := func() {
		proxyStreamFailMu.Lock()
		proxyStreamFails = map[string][]time.Time{}
		proxyStreamFailMu.Unlock()
	}

	// ① 建连阶段：第一次不冷却（2-strike，2026-10-31 用户定——单次网络抖动
	// 不再把唯一可用出口全局冷却）
	for _, err := range []error{
		fmt.Errorf(`Post "https://x/v1/chat": dial tcp 10.0.0.1:443: i/o timeout`),
		fmt.Errorf(`Post "https://x/v1/chat": proxyconnect tcp: dial tcp 10.0.0.1:1080: connect: connection refused`),
		fmt.Errorf(`Post "https://x/v1/chat": socks5: no acceptable methods`),
	} {
		reset()
		if scope, cool := shouldCooldownExit(pkWb, p, err); cool || scope != "" {
			t.Fatalf("single dial-stage failure must not cool (2-strike): %v (scope=%q cool=%v)", err, scope, cool)
		}
		// 记账一次后，查询端（本次算第 2 次）应判全局冷却
		if scope := recordExitFailure(pkWb, p, err); scope != "" {
			t.Fatalf("first recorded dial-stage failure must not fire: %v (scope=%q)", err, scope)
		}
		if scope, cool := shouldCooldownExit(pkWb, p, err); !cool || scope != "global" {
			t.Fatalf("second dial-stage failure should cool globally: %v (scope=%q cool=%v)", err, scope, cool)
		}
		// recordExitFailure 达阈值触发并清零
		if scope := recordExitFailure(pkWb, p, err); scope != "global" {
			t.Fatalf("second recorded dial-stage failure should fire global (scope=%q)", scope)
		}
		if scope, cool := shouldCooldownExit(pkWb, p, err); cool {
			t.Fatalf("strike counter should reset after firing: %v (scope=%q cool=%v)", err, scope, cool)
		}
	}

	// ② 隧道后流错误：单次不冷却（用户报的场景：codebuddy 上游 EOF 冷却了好出口），
	// 同平台同出口窗口内第二次才平台级冷却。TLS 端到端握手错误（发生在 CONNECT
	// 隧道已通之后，2026-10-31 用户定）也归平台级——iStore 生产日志里
	// `net/http: TLS handshake timeout` 单次即全局冷 2m 正是要修的场景。
	streams := []error{
		fmt.Errorf(`Post "https://www.codebuddy.ai/v2/chat/completions": EOF`),
		fmt.Errorf(`Post "https://www.workbuddy.cn/v2/chat/completions": net/http: TLS handshake timeout`),
		fmt.Errorf(`Post "https://x/v1/chat": remote error: tls: handshake failure`),
	}
	for _, stream := range streams {
		reset()
		if scope, cool := shouldCooldownExit(pkWb, p, streams[0]); cool || scope != "" {
			t.Fatalf("single upstream-side stream error must not cool a healthy exit (scope=%q cool=%v): %v", scope, cool, stream)
		}
		if scope := recordExitFailure(pkWb, p, stream); scope != "" {
			t.Fatalf("first recorded stream error must not fire (scope=%q): %v", scope, stream)
		}
		scope, cool := shouldCooldownExit(pkWb, p, streams[0])
		if !cool || scope != "platform" {
			t.Fatalf("repeated upstream-side stream errors in window should cool platform-scoped (scope=%q cool=%v): %v", scope, cool, stream)
		}
		if scope := recordExitFailure(pkWb, p, stream); scope != "platform" {
			t.Fatalf("second recorded stream error should fire platform-scoped (scope=%q): %v", scope, stream)
		}
		if scope, cool := shouldCooldownExit(pkWb, p, streams[0]); cool {
			t.Fatalf("strike counter should reset after firing (scope=%q cool=%v)", scope, cool)
		}
	}

	// ③ nil / 上游响应类错误：永不冷却
	if _, cool := shouldCooldownExit(pkWb, p, nil); cool {
		t.Fatal("nil error must not cool")
	}
	if scope := recordExitFailure(pkWb, p, nil); scope != "" {
		t.Fatalf("nil error must not record a strike (scope=%q)", scope)
	}

	// ④ 平台间不凑数：wb 记了 1 次平台层失败后，amd 的同出口查询不应达阈值
	dial := fmt.Errorf(`Post "https://x/v1/chat": dial tcp 10.0.0.1:443: i/o timeout`)
	reset()
	_ = recordExitFailure(pkWb, p, streams[0])
	if scope, cool := shouldCooldownExit(pkAmd, p, streams[0]); cool || scope != "" {
		t.Fatalf("other platform's failure must not inherit wb's strike (scope=%q cool=%v)", scope, cool)
	}
	// 全局层与平台层分开计数：全局层已记 1 次建连失败，不影响平台层判定
	reset()
	_ = recordExitFailure(pkWb, p, dial)
	if scope, cool := shouldCooldownExit(pkWb, p, streams[0]); cool || scope != "" {
		t.Fatalf("global-layer failure must not count toward platform strike (scope=%q cool=%v)", scope, cool)
	}
	// 反向同理：平台层记 1 次不影响全局层判定
	reset()
	_ = recordExitFailure(pkWb, p, streams[0])
	if scope, cool := shouldCooldownExit(pkWb, p, dial); cool || scope != "" {
		t.Fatalf("platform-layer failure must not count toward global strike (scope=%q cool=%v)", scope, cool)
	}
}

// TestClearProxyCooldowns 手动解禁（2026-10-31）：清全局冷却、平台级冷却与
// 全部失败 strike 计数。
func TestClearProxyCooldowns(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{
		"socks5://u:p@10.0.0.1:11199#UNBAN-TEST",
	}})
	t.Cleanup(func() {
		proxyCooldownsMu.Lock()
		proxyPlatformCooldowns = map[string]map[int]time.Time{}
		proxyCooldowns = map[int]time.Time{}
		proxyStreamFailMu.Lock()
		proxyStreamFails = map[string][]time.Time{}
		proxyStreamFailMu.Unlock()
		proxyCooldownsMu.Unlock()
	})

	const p = "socks5://u:p@10.0.0.1:11199#UNBAN-TEST"
	idx := proxyIdxInPool(p)
	if idx < 0 {
		t.Fatal("test proxy must be in pool")
	}
	// 布满三层状态：全局 + 两个平台冷却 + 全局/平台 strike 各 1 条
	cooldownUpstreamProxy(idx, time.Minute)
	cooldownUpstreamProxyFor(pkWb, idx, time.Minute)
	cooldownUpstreamProxyFor(pkZen, idx, time.Minute)
	proxyStreamFailMu.Lock()
	proxyStreamFails = map[string][]time.Time{
		"\x00" + p:        {time.Now()},
		pkWb + "\x00" + p: {time.Now()},
	}
	proxyStreamFailMu.Unlock()

	// 面板传打码 URL 也要能定位（健康表里只有打码 URL）
	clearProxyCooldowns(p)
	proxyStreamFailMu.Lock()
	proxyStreamFails = map[string][]time.Time{
		"\x00" + p:        {time.Now()},
		pkWb + "\x00" + p: {time.Now()},
	}
	proxyStreamFailMu.Unlock()
	cooldownUpstreamProxy(idx, time.Minute)
	cooldownUpstreamProxyFor(pkWb, idx, time.Minute)
	clearProxyCooldowns(maskProxyURL(p))

	proxyCooldownsMu.Lock()
	gCool := proxyCooldowns[idx]
	_, wbCool := proxyPlatformCooldowns[pkWb][idx]
	proxyCooldownsMu.Unlock()
	if gCool.After(time.Now()) {
		t.Fatal("global cooldown must be cleared")
	}
	if wbCool {
		t.Fatal("platform cooldowns must be cleared")
	}
	proxyStreamFailMu.Lock()
	n := len(proxyStreamFails)
	proxyStreamFailMu.Unlock()
	if n != 0 {
		t.Fatalf("strike counters must be cleared, %d left", n)
	}
}

// TestProxyCooldownSetClearAPI 手动置冷/解禁 API（2026-10-31）：置冷后面板行
// 处于 cooling，clear 后恢复；打码 URL 能定位回原始条目。
func TestProxyCooldownSetClearAPI(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{
		"socks5://u:p@10.0.0.1:11200#SET-TEST",
	}})
	t.Cleanup(func() {
		proxyCooldownsMu.Lock()
		proxyPlatformCooldowns = map[string]map[int]time.Time{}
		proxyCooldowns = map[int]time.Time{}
		proxyStreamFailMu.Lock()
		proxyStreamFails = map[string][]time.Time{}
		proxyStreamFailMu.Unlock()
		proxyCooldownsMu.Unlock()
	})
	const p = "socks5://u:p@10.0.0.1:11200#SET-TEST"
	masked := maskProxyURL(p)

	call := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		if path == "/set" {
			handleProxyCooldownSet(rec, req)
		} else {
			handleProxyCooldownClear(rec, req)
		}
		return rec
	}

	// ① 打码 URL 置冷（全局，10 分钟）
	rec := call("/set", `{"proxy":"`+masked+`","minutes":10}`)
	if rec.Code != 200 {
		t.Fatalf("set via masked URL failed: %d %s", rec.Code, rec.Body.String())
	}
	idx := proxyIdxInPool(p)
	proxyCooldownsMu.Lock()
	until := proxyCooldowns[idx]
	proxyCooldownsMu.Unlock()
	if !until.After(time.Now()) {
		t.Fatalf("set did not write global cooldown: %v", until)
	}

	// ② 打码 URL 解禁：冷却与 strike 计数全清
	proxyStreamFailMu.Lock()
	proxyStreamFails = map[string][]time.Time{"\x00" + p: {time.Now()}}
	proxyStreamFailMu.Unlock()
	rec = call("/clear", `{"proxy":"`+masked+`"}`)
	if rec.Code != 200 {
		t.Fatalf("clear via masked URL failed: %d %s", rec.Code, rec.Body.String())
	}
	proxyCooldownsMu.Lock()
	until = proxyCooldowns[idx]
	proxyCooldownsMu.Unlock()
	if until.After(time.Now()) {
		t.Fatal("clear must remove the global cooldown")
	}
	proxyStreamFailMu.Lock()
	n := len(proxyStreamFails)
	proxyStreamFailMu.Unlock()
	if n != 0 {
		t.Fatalf("clear must remove strike counters, %d left", n)
	}

	// ③ 平台级置冷 + 解禁；池外代理 404
	if rec := call("/set", `{"proxy":"`+masked+`","kind":"wb","minutes":10}`); rec.Code != 200 {
		t.Fatalf("platform set failed: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := (func() (time.Time, bool) {
		proxyCooldownsMu.Lock()
		defer proxyCooldownsMu.Unlock()
		t2, ok := proxyPlatformCooldowns[pkWb][idx]
		return t2, ok
	})(); !ok {
		t.Fatal("platform set did not write platform cooldown")
	}
	if rec := call("/clear", `{"proxy":"`+masked+`"}`); rec.Code != 200 {
		t.Fatalf("platform clear failed: %d", rec.Code)
	}
	if rec := call("/set", `{"proxy":"socks5://not-in-pool:1"}`); rec.Code != 404 {
		t.Fatalf("set for out-of-pool proxy must 404, got %d", rec.Code)
	}
}

// TestIsUpstreamSideStreamFailure 形态判定：方法+URL 前缀 + 流错误关键词才算
// "隧道后"；建连阶段关键词（dial/proxyconnect/socks）不算。TLS 端到端握手
// 错误（2026-10-31 用户定）归平台级——隧道已通，超时不是出口死了。
func TestIsUpstreamSideStreamFailure(t *testing.T) {
	yes := []string{
		`Post "https://a/b": EOF`,
		`Post "https://a/b": unexpected EOF`,
		`Get "https://a/b": read tcp 1.2.3.4:5->6.7.8.9:443: connection reset by peer`,
		`Post "https://a/b": http: server closed idle connection`,
		`Post "https://www.workbuddy.cn/v2/chat/completions": net/http: TLS handshake timeout`,
		`Post "https://a/b": remote error: tls: handshake failure`,
	}
	no := []string{
		`Post "https://a/b": dial tcp 1.2.3.4:443: connect: connection refused`,
		`Post "https://a/b": proxyconnect tcp: EOF`,
		`Post "https://a/b": proxyconnect: tls: handshake failure`, // 与代理建 TLS 仍属建连阶段
		`socks5: EOF`,
		`EOF`,
		`Post "https://a/b": context canceled`,
	}
	for _, s := range yes {
		if !isUpstreamSideStreamFailure(fmt.Errorf("%s", s)) {
			t.Fatalf("should be upstream-side stream failure: %s", s)
		}
	}
	for _, s := range no {
		if isUpstreamSideStreamFailure(fmt.Errorf("%s", s)) {
			t.Fatalf("should NOT be upstream-side stream failure: %s", s)
		}
	}
}

// TestProxyStreamFailStrikeWindow 窗口外的旧失败不累计。
func TestProxyStreamFailStrikeWindow(t *testing.T) {
	const p = "socks5://h:1#W"
	proxyStreamFailMu.Lock()
	proxyStreamFails = map[string][]time.Time{pkWb + "\x00" + p: {time.Now().Add(-2 * proxyStreamFailWindow)}}
	proxyStreamFailMu.Unlock()
	if proxyFailStrike(pkWb + "\x00" + p) {
		t.Fatal("stale failure outside window must not count toward the threshold")
	}
}

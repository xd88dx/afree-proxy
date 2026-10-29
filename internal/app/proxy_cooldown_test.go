package app

import (
	"fmt"
	"testing"
	"time"
)

// TestShouldCooldownExit 出口冷却的归因（2026-10-30）：
// 建连阶段失败立刻冷却；隧道已建通后的流错误（上游 EOF/reset）单次不冷却，
// 窗口内累计到阈值才冷却；上游响应类错误永不冷却。
func TestShouldCooldownExit(t *testing.T) {
	const p = "socks5://10.10.10.5:10106#DDPHX"
	reset := func() {
		proxyStreamFailMu.Lock()
		proxyStreamFails = map[string][]time.Time{}
		proxyStreamFailMu.Unlock()
	}

	// ① 建连阶段：立刻冷却
	for _, err := range []error{
		fmt.Errorf(`Post "https://x/v1/chat": dial tcp 10.0.0.1:443: i/o timeout`),
		fmt.Errorf(`Post "https://x/v1/chat": proxyconnect tcp: dial tcp 10.0.0.1:1080: connect: connection refused`),
		fmt.Errorf(`Post "https://x/v1/chat": socks5: no acceptable methods`),
		fmt.Errorf(`Post "https://x/v1/chat": remote error: tls: handshake failure`),
	} {
		reset()
		if !shouldCooldownExit(p, err) {
			t.Fatalf("dial-stage failure should cool: %v", err)
		}
	}

	// ② 隧道后流错误：单次不冷却（用户报的场景：codebuddy 上游 EOF 冷却了好出口）
	stream := fmt.Errorf(`Post "https://www.codebuddy.ai/v2/chat/completions": EOF`)
	reset()
	if shouldCooldownExit(p, stream) {
		t.Fatal("single upstream EOF must not cool a healthy exit")
	}
	// 同一出口窗口内第二次才冷却
	if !shouldCooldownExit(p, stream) {
		t.Fatal("repeated upstream-side stream failures in window should cool")
	}
	// 触发后清零：紧接着的第三次不应再次判为"达到阈值"以外的行为
	if shouldCooldownExit(p, stream) {
		t.Fatal("strike counter should reset after firing")
	}

	// ③ 上游响应类错误（各平台 HTTPError 语义）：永不冷却
	if shouldCooldownExit(p, nil) {
		t.Fatal("nil error must not cool")
	}
}

// TestIsUpstreamSideStreamFailure 形态判定：方法+URL 前缀 + 流错误关键词才算
// "隧道后"；建连阶段关键词（dial/proxyconnect/socks/tls）不算。
func TestIsUpstreamSideStreamFailure(t *testing.T) {
	yes := []string{
		`Post "https://a/b": EOF`,
		`Post "https://a/b": unexpected EOF`,
		`Get "https://a/b": read tcp 1.2.3.4:5->6.7.8.9:443: connection reset by peer`,
		`Post "https://a/b": http: server closed idle connection`,
	}
	no := []string{
		`Post "https://a/b": dial tcp 1.2.3.4:443: connect: connection refused`,
		`Post "https://a/b": proxyconnect tcp: EOF`,
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
	proxyStreamFails = map[string][]time.Time{p: {time.Now().Add(-2 * proxyStreamFailWindow)}}
	proxyStreamFailMu.Unlock()
	if proxyStreamFailStrike(p) {
		t.Fatal("stale failure outside window must not count toward the threshold")
	}
}

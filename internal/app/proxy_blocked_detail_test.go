package app

import (
	"strings"
	"testing"

	"afree-proxy/internal/kit"
)

// 面板 Test 在"绑定出口冷却/被移除"时会直接失败。旧文案只说结论，用户无法
// 区分"key 废了"和"出口挂了"，实测出现过把健康凭据误判成失效的情况。
// 这两个用例锁住：文案必须带上出口与冷却解除时刻，并显式声明凭据未被验证。

func TestBlockedExitMessageIncludesExitDetail(t *testing.T) {
	got := blockedExitMessage(
		"key#15 bound proxies unavailable (cooling or removed); probe aborted by proxy isolation",
		"socks5://10.10.10.5:10106#DDHK", "")
	if !strings.Contains(got, "main=") {
		t.Fatalf("缺少 main 出口说明: %q", got)
	}
	if !strings.Contains(got, "10106#DDHK") {
		t.Fatalf("缺少出口标识: %q", got)
	}
	if !strings.Contains(got, "backup=none") {
		t.Fatalf("缺少 backup=none: %q", got)
	}
	if !strings.Contains(got, "not verified") {
		t.Fatalf("缺少“凭据本身未验证”声明: %q", got)
	}
	// 前缀必须原样保留，调用方（日志/面板）依赖它
	if !strings.HasPrefix(got, "key#15 bound proxies unavailable") {
		t.Fatalf("前缀被改写: %q", got)
	}
}

func TestBlockedExitMessageUnboundKeepsPlainPrefix(t *testing.T) {
	plain := "account bound proxies unavailable (cooling or removed); probe skipped by proxy isolation"
	if got := blockedExitMessage(plain, "", ""); got != plain {
		t.Fatalf("未绑定身份不应追加出口详情: %q", got)
	}
}

func TestKitBoundProbeBlockedMsgDegradesWithoutInjection(t *testing.T) {
	// 子包单测里主包未注入详情钩子：文案必须降级为原句，不能报空、不能加噪。
	prefix := "key#12 bound proxies unavailable; probe aborted"
	if kit.ProxyBlockedDetail != nil {
		t.Skip("主包已注入，跳过降级路径")
	}
	if got := kit.BoundProbeBlockedMsg("amd", "rc-xxx", prefix); got != prefix {
		t.Fatalf("未注入时应原样返回: %q", got)
	}
}

func TestKitMaskProxyURL(t *testing.T) {
	cases := map[string]string{
		"socks5://user:pass@10.0.0.1:1080#a": "socks5://***@10.0.0.1:1080#a",
		"socks5://10.0.0.1:1080#a":           "socks5://10.0.0.1:1080#a",
		"":                                   "",
	}
	for in, want := range cases {
		if got := kit.MaskProxyURL(in); got != want {
			t.Fatalf("MaskProxyURL(%q)=%q, want %q", in, got, want)
		}
	}
}

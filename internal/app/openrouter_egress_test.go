package app

import (
	"testing"

	openrouter "afree-proxy/internal/openrouter"
)

// orGlobalEgressExit 是「未绑定 key / 无凭据场景」的全局出口单点（目录同步与
// 未绑定 key 的 chat 共用）：走池关 + DISABLE_DIRECT → fail-closed；走池关 +
// 未设 DISABLE_DIRECT → 显式直连放行。隔离口径（绝不隐式暴露直连）的回归锚点。
func TestOrGlobalEgressExitDirectModes(t *testing.T) {
	openrouter.SetUseProxiesResolver(func() *bool { p := false; return &p })
	t.Cleanup(func() { openrouter.SetUseProxiesResolver(nil) })

	t.Setenv("DISABLE_DIRECT", "true")
	if url, ok := orGlobalEgressExit(); ok || url != "" {
		t.Fatalf("DISABLE_DIRECT must fail closed, got (%q, %v)", url, ok)
	}

	t.Setenv("DISABLE_DIRECT", "")
	if url, ok := orGlobalEgressExit(); !ok || url != "" {
		t.Fatalf("explicit direct mode must pass through, got (%q, %v)", url, ok)
	}
}

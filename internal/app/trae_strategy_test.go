package app

import (
	"testing"

	trae "afree-proxy/internal/trae"
)

// TestTraeStrategyPriority 固定 Trae 选号策略的三级优先级：
// TRAE_STRATEGY env > 面板 Selection > 全局账号池策略。
// 回归点：面板「Selection strategy」曾完全不生效（app 层只传全局策略）。
func TestTraeStrategyPriority(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("TRAE_STRATEGY", "")

	savedTrae := trae.Get()
	t.Cleanup(func() { trae.Set(savedTrae) })
	savedCfg := getProxyConfig()
	t.Cleanup(func() { setProxyConfig(savedCfg) })

	setProxyConfig(&proxyConfigData{Strategy: "random", Headers: map[string]string{}})
	trae.Set(&trae.Config{Selection: ""})
	if got := traeStrategy(); got != "random" {
		t.Fatalf("未设置面板策略时应回落全局策略: got %q", got)
	}

	trae.Set(&trae.Config{Selection: "credit-priority"})
	if got := traeStrategy(); got != "credit-priority" {
		t.Fatalf("面板策略应覆盖全局策略: got %q", got)
	}

	t.Setenv("TRAE_STRATEGY", "fill")
	if got := traeStrategy(); got != "fill" {
		t.Fatalf("TRAE_STRATEGY 应覆盖面板策略: got %q", got)
	}

	t.Setenv("TRAE_STRATEGY", "bogus")
	if got := traeStrategy(); got != "credit-priority" {
		t.Fatalf("非法 TRAE_STRATEGY 应被忽略并回落面板策略: got %q", got)
	}
}

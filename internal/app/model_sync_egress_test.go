package app

import (
	"context"
	"strings"
	"testing"
)

// 目录同步出口解析与探测模型守卫的回归（2026-11-05 审计）：
//   - cline/zen 目录同步此前裸直连（机制存在但没接全）——直连不可达的部署上
//     目录自重启起每轮 EOF：cline 目录清空、/v1/models 摘除整段、探测模型
//     退化为空串打到上游变成不知所谓的 400 "invalid model format"；
//   - 修复后同步出口与会话请求同语义（绑定 > 走池 > 显式直连，DISABLE_DIRECT
//     恒拦），无可用出口 fail-closed 跳过本轮；
//   - 探测路径对空目录明确报因，不再把空 model 交给上游。

func TestClineSyncClientBoundExitUnavailableFailsClosed(t *testing.T) {
	setupBindingTest(t, nil)
	t.Setenv("DISABLE_DIRECT", "")
	ghost := "socks5://10.255.255.1:9#ghost-exit"
	swapTestPool(t, []*Account{
		{AccountID: "acc_1", Email: "a@x", APIToken: "sk_test", ProxyMain: ghost},
	})
	_, _, err := clineSyncClient()
	if err == nil {
		t.Fatal("bound exit removed from pool: want fail-closed error, got nil")
	}
	if !strings.Contains(err.Error(), "no routable exit for model sync") {
		t.Fatalf("error should name the sync exit failure, got: %v", err)
	}
}

func TestClineSyncClientUnboundPoolDirectGating(t *testing.T) {
	setupBindingTest(t, nil)
	swapTestPool(t, []*Account{{AccountID: "acc_1", Email: "a@x", APIToken: "sk_test"}})
	clearTestProxyPool(t)

	// 走池（默认开）+ 池空 + DISABLE_DIRECT：直连兜底不放行。
	t.Setenv("DISABLE_DIRECT", "true")
	if _, _, err := clineSyncClient(); err == nil {
		t.Fatal("pool empty + DISABLE_DIRECT: want error, got nil")
	}

	// 面板显式直连模式（走池关）+ DISABLE_DIRECT：直连是被明确选择的出口，
	// 但 DISABLE_DIRECT 语义是"直连全面失效"，同样不放行。
	disabled := false
	setPlatformUseProxies("cline", &disabled)
	if _, _, err := clineSyncClient(); err == nil {
		t.Fatal("explicit direct mode + DISABLE_DIRECT: want error, got nil")
	}

	// 面板显式直连模式 + 直连允许：返回直连客户端。
	t.Setenv("DISABLE_DIRECT", "")
	client, exitURL, err := clineSyncClient()
	if err != nil {
		t.Fatalf("explicit direct mode: %v", err)
	}
	if client == nil || exitURL != "" {
		t.Fatalf("explicit direct mode: want direct client with empty exit, got %v / %q", client, exitURL)
	}
}

func TestZenSyncClientUsesRoutingEnabledKeys(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:              []string{"zk_a", "zk_b"},
		KeyRoutingEnabled: map[string]bool{"zk_a": false, "zk_b": true},
		KeyBindings:       map[string]proxyBinding{"zk_b": {Main: "socks5://10.255.255.1:9#zen-exit"}},
		Proxies:           []string{"socks5://10.255.255.1:9#zen-exit"},
	})
	t.Setenv("DISABLE_DIRECT", "")
	client, err := zenSyncClient()
	if err != nil {
		t.Fatalf("routing-enabled key with available binding: %v", err)
	}
	if client == nil {
		t.Fatal("want non-nil client")
	}
}

func TestZenSyncClientAllKeysDownFallsToPoolGating(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:        []string{"zk_a"},
		KeyBindings: map[string]proxyBinding{"zk_a": {Main: "socks5://10.255.255.1:9#ghost"}},
	})
	t.Setenv("DISABLE_DIRECT", "true")
	if _, err := zenSyncClient(); err == nil {
		t.Fatal("all keys unroutable + pool empty + DISABLE_DIRECT: want error, got nil")
	}
}

func TestTestAccountEmptyCatalogReportsCause(t *testing.T) {
	setupBindingTest(t, nil)
	swapTestPool(t, []*Account{{AccountID: "acc_empty", Email: "empty@x", APIToken: "sk_test"}})
	modelsMu.Lock()
	modelsCache = map[string]*ModelInfo{}
	modelsMu.Unlock()
	t.Cleanup(func() {
		modelsMu.Lock()
		modelsCache = nil
		modelsMu.Unlock()
	})

	result, status := testAccount(context.Background(), &Account{AccountID: "acc_empty", Email: "empty@x", APIToken: "sk_test"})
	if status != "error" {
		t.Fatalf("status = %q, want error", status)
	}
	if reason, _ := result["reason"].(string); !strings.Contains(reason, "no probe model") {
		t.Fatalf("reason should name the empty catalog, got: %q", reason)
	}
}

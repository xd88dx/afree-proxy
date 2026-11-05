package openrouter

import (
	"testing"
	"time"
)

// swapExitResolver 换入测试 resolver 并注册恢复（包级变量带锁，测试直接读写）。
func swapExitResolver(t *testing.T, r ExitResolver) {
	t.Helper()
	exitResolverMu.Lock()
	saved := exitResolver
	exitResolver = r
	exitResolverMu.Unlock()
	t.Cleanup(func() {
		exitResolverMu.Lock()
		exitResolver = saved
		exitResolverMu.Unlock()
	})
}

// key 全禁用/无 key 时，目录同步退到平台全局出口规则（resolver 空 key 语义），
// 不再直接 fail——2026-11-05 生产空窗实证：备份导入后 key 全禁用，live 目录
// 同步持续失败，快照外新模型 404 达 5 分钟。
func TestCatalogClientFallsBackToGlobalExitWhenNoRoutableKey(t *testing.T) {
	swapExitResolver(t, func(key string) (string, bool) {
		if key == "" {
			return "socks5://127.0.0.1:19999", true
		}
		return "", false // 所有 key 的出口均不可用
	})

	c, err := catalogHTTPClient()
	if err != nil {
		t.Fatalf("global egress fallback must serve the sync, got err=%v", err)
	}
	if c == nil {
		t.Fatal("client must not be nil")
	}
}

// 全局出口被 DISABLE_DIRECT 判否（resolver 空 key 返回 !ok）→ fail-closed，
// 绝不隐式回落直连（2026-10-29 审计口径）。
func TestCatalogClientGlobalExitFailClosed(t *testing.T) {
	swapExitResolver(t, func(string) (string, bool) { return "", false })

	if _, err := catalogHTTPClient(); err == nil {
		t.Fatal("must fail closed when no key egress and global egress is blocked")
	}
}

// 全局规则给显式直连（空 URL）时，client 带 25s 超时（目录同步防悬挂）。
func TestCatalogClientDirectFallbackHasSyncTimeout(t *testing.T) {
	swapExitResolver(t, func(key string) (string, bool) {
		if key == "" {
			return "", true
		}
		return "", false
	})

	c, err := catalogHTTPClient()
	if err != nil {
		t.Fatalf("explicit direct fallback must work, got err=%v", err)
	}
	if c.Timeout != 25*time.Second {
		t.Fatalf("direct sync client timeout = %v, want 25s", c.Timeout)
	}
}

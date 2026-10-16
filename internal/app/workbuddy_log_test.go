package app

import (
	"context"
	"testing"

	wbauth "afree-proxy/internal/workbuddy/auth"
)

// TestWorkbuddyObserveAccountFillsLogColumns wbcn/wbgb 请求日志此前恒不带
// 账号与代理信息（上游账号的选择发生在 vendor 树内）。ObserveAccount 钩子
// 注入后，日志槽位必须拿到「账号/Key」与「代理」两列，且不碰无槽位的 ctx。
func TestWorkbuddyObserveAccountFillsLogColumns(t *testing.T) {
	t.Setenv("PROXY_ISOLATION", "true")
	wbProxyMu.Lock()
	prevLoaded, prevBindings := wbProxyLoaded, wbProxyBindings
	wbProxyLoaded = true // 跳过文件读取，直接注入绑定
	wbProxyBindings = map[string]workbuddyProxyBinding{
		"uid-main": {Main: "socks5://main:1080"},
	}
	wbProxyMu.Unlock()
	t.Cleanup(func() {
		wbProxyMu.Lock()
		wbProxyLoaded, wbProxyBindings = prevLoaded, prevBindings
		wbProxyMu.Unlock()
	})

	// 未绑定账号 + 直连出口：代理列记 direct（确实没经代理）。
	// workbuddySub 未初始化时序号恒 0（wb#0），真实序号只在网关运行时取池顺序。
	ctx, ui := withUpstreamInfo(context.Background())
	workbuddyObserveAccount(ctx, &wbauth.Auth{UID: "uid-plain", Nickname: "n1"}, "")
	if ui.label != "wb#0 n1" {
		t.Fatalf("upstream label = %q, want %q", ui.label, "wb#0 n1")
	}
	if ui.exit != "direct" {
		t.Fatalf("exit = %q, want direct", ui.exit)
	}

	// 绑定主代理且本次从主出网：代理列记 main。
	ctx2, ui2 := withUpstreamInfo(context.Background())
	workbuddyObserveAccount(ctx2, &wbauth.Auth{UID: "uid-main"}, "socks5://main:1080")
	if ui2.label != "wb#0 uid#uid-main" {
		t.Fatalf("upstream label = %q, want wb#0 uid#uid-main", ui2.label)
	}
	if ui2.exit != "main" {
		t.Fatalf("exit = %q, want main", ui2.exit)
	}

	// 长 UID 截断，防日志列被撑爆。
	ctx3, ui3 := withUpstreamInfo(context.Background())
	workbuddyObserveAccount(ctx3, &wbauth.Auth{UID: "0123456789abcdef"}, "")
	if ui3.label != "wb#0 uid#0123456789…" {
		t.Fatalf("truncated label = %q", ui3.label)
	}

	// 非网关请求（ctx 无日志槽位）与 nil 账号：不得 panic。
	workbuddyObserveAccount(context.Background(), nil, "")
	workbuddyObserveAccount(context.Background(), &wbauth.Auth{}, "")
	workbuddyObserveAccount(nil, &wbauth.Auth{UID: "uid-plain"}, "")
}

// TestKeyPoolAttemptObserverFillsLogColumns amd / tokenharbor / openrouter /
// other 共用注入：日志「账号/Key」列为 key#N，「代理」列按绑定归类。
func TestKeyPoolAttemptObserverFillsLogColumns(t *testing.T) {
	t.Setenv("PROXY_ISOLATION", "true")
	keys := []string{"sk-aaa", "sk-bbb"}
	bindings := map[string]workbuddyProxyBinding{}
	obs := scopedObserver(keyPoolAttemptObserver(
		func(key string) int {
			for i, k := range keys {
				if k == key {
					return i + 1 // 面板序号从 1 开始
				}
			}
			return 0
		},
		func(key string) (string, string, bool) {
			b := bindings[key]
			return b.Main, b.Backup, b.Main != "" || b.Backup != ""
		},
	))

	// 未绑定 + 直连 → direct；scope 形参被单平台适配器忽略。
	ctx, ui := withUpstreamInfo(context.Background())
	obs(ctx, "ignored-scope", "sk-aaa", "")
	if ui.label != "key#1" {
		t.Fatalf("upstream label = %q, want key#1", ui.label)
	}
	if ui.exit != "direct" {
		t.Fatalf("exit = %q, want direct", ui.exit)
	}

	// 绑定主代理并从主出网 → main。
	bindings["sk-bbb"] = workbuddyProxyBinding{Main: "socks5://main:1080"}
	ctx2, ui2 := withUpstreamInfo(context.Background())
	obs(ctx2, "", "sk-bbb", "socks5://main:1080")
	if ui2.label != "key#2" {
		t.Fatalf("upstream label = %q, want key#2", ui2.label)
	}
	if ui2.exit != "main" {
		t.Fatalf("exit = %q, want main", ui2.exit)
	}

	// 全局池轮转（非绑定、出口非空）→ 空串，面板显示 -。
	ctx3, ui3 := withUpstreamInfo(context.Background())
	obs(ctx3, "", "sk-aaa", "socks5://pool:1080")
	if ui3.exit != "" {
		t.Fatalf("exit = %q, want empty (global pool rotation)", ui3.exit)
	}

	// nil ctx / 空 key：不得 panic。
	obs(nil, "", "sk-aaa", "")
	obs(context.Background(), "", "", "")
}
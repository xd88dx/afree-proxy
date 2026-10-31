package trae

import (
	"path/filepath"
	"testing"
	"time"
)

// TestPickAccountCreditPriority 覆盖 panel「Selection strategy = credit-priority」
// 的选号语义：额度余量多者优先，unlimited 恒最优先，同额度按池序稳定。
// 回归点：该分支曾因 app 层只传全局账号池策略而完全不可达。
// 2026-10-31 拆分：额度快照住痕迹表（按账号 ID 键），经 UsageFor/导入函数设置。
func TestPickAccountCreditPriority(t *testing.T) {
	saved := Get()
	t.Cleanup(func() { Set(saved) })

	poor := &Account{ID: "poor", Token: "t1"}
	rich := &Account{ID: "rich", Token: "t2"}
	unlimited := &Account{ID: "unl", Token: "t3"}

	Set(&Config{Accounts: []*Account{poor, rich, unlimited}})
	setUsageCredits("poor", &Credits{Remaining: 5})
	setUsageCredits("rich", &Credits{Remaining: 500})
	setUsageCredits("unl", &Credits{Unlimited: true, Limit: -1})
	if got := PickAccount("credit-priority", nil); got == nil || got.ID != "unl" {
		t.Fatalf("unlimited account must win credit-priority: got %+v", got)
	}

	// 去掉 unlimited：应选额度最多的 rich，与池序无关（rich 在 poor 之后）。
	Set(&Config{Accounts: []*Account{poor, rich}})
	for i := 0; i < 3; i++ {
		if got := PickAccount("credit-priority", nil); got == nil || got.ID != "rich" {
			t.Fatalf("richest account must win credit-priority (iter %d): got %+v", i, got)
		}
	}

	// 未知额度按 0 计：有额度的账号优先于无额度快照的账号。
	unknown := &Account{ID: "unknown", Token: "t4"}
	Set(&Config{Accounts: []*Account{unknown, poor}})
	if got := PickAccount("credit-priority", nil); got == nil || got.ID != "poor" {
		t.Fatalf("known remaining credits must outrank unknown: got %+v", got)
	}
}

// setUsageCredits 测试辅助：直接写包级 usage 的额度快照（mu 下）。
func setUsageCredits(id string, c *Credits) {
	mu.Lock()
	usageForLocked(id).Credits = c
	mu.Unlock()
}

// TestPickAccountRoundRobinIgnoresCredits round_robin 不受额度影响，按池序轮转。
func TestPickAccountRoundRobinIgnoresCredits(t *testing.T) {
	saved := Get()
	t.Cleanup(func() { Set(saved) })

	// 额度故意与池序相反：round_robin 仍应先取池序第一个。
	rich := &Account{ID: "rich", Token: "t1"}
	poor := &Account{ID: "poor", Token: "t2"}
	Set(&Config{Accounts: []*Account{rich, poor}})
	setUsageCredits("rich", &Credits{Remaining: 900})
	setUsageCredits("poor", &Credits{Remaining: 1})

	got1 := PickAccount("round_robin", nil)
	got2 := PickAccount("round_robin", nil)
	if got1 == nil || got2 == nil || got1.ID == got2.ID {
		t.Fatalf("round_robin must rotate between accounts: %+v / %+v", got1, got2)
	}
}

// TestNormalizeKeepsRoundRobin 面板/env 显式写 round_robin 不能被清空。
func TestNormalizeKeepsRoundRobin(t *testing.T) {
	c := &Config{Selection: "round_robin"}
	c.normalize()
	if c.Selection != "round_robin" {
		t.Fatalf("round_robin must survive normalize, got %q", c.Selection)
	}
	c = &Config{Selection: "bogus"}
	c.normalize()
	if c.Selection != "" {
		t.Fatalf("unknown strategy must be cleared, got %q", c.Selection)
	}
}

// 全冷却默认拒绝（UNABLE_FORCE 未设，2026-10-28 用户定）；true 时恢复兜底。
func TestPickAccountAllCoolingFollowsUnableForce(t *testing.T) {
	saved := Get()
	t.Cleanup(func() { Set(saved) })
	Set(&Config{Gateway: DefaultGateway, Accounts: []*Account{
		{ID: "u1", Token: "tok"}, {ID: "u2", Token: "tok"},
	}, Endpoint: "auto"})
	Cooldown("u1", 5*time.Minute)
	Cooldown("u2", 5*time.Minute)
	if got := PickAccount("round_robin", nil); got != nil {
		t.Fatalf("default must reject when all accounts are cooling, got %+v", got)
	}
	t.Setenv("UNABLE_FORCE", "true")
	if got := PickAccount("round_robin", nil); got == nil {
		t.Fatal("UNABLE_FORCE=true must fall back to a cooling account")
	}
}

// 冷却落盘回读（2026-10-28 用户定）：Cooldown 后重新加载，冷却保留。
// 2026-10-31 拆分后持久化目标是独立的 trae-state.json。
func TestCooldownPersistedAcrossReload(t *testing.T) {
	dir := t.TempDir()
	saved := Get()
	t.Cleanup(func() { Set(saved) })
	ConfigPath = filepath.Join(dir, "trae-config.json")
	UsagePath = filepath.Join(dir, "trae-state.json")
	t.Cleanup(func() { ConfigPath = ""; UsagePath = "" })
	Set(&Config{Gateway: DefaultGateway, Accounts: []*Account{
		{ID: "u1", Token: "tok"}, {ID: "u2", Token: "tok"},
	}, Endpoint: "auto"})
	Cooldown("u1", 5*time.Minute)
	// 模拟重启：重新加载同一对文件
	Load(ConfigPath)
	until, ok := CooldownUntil("u1")
	if !ok || until.Before(time.Now()) {
		t.Fatalf("cooldown must survive reload, ok=%v until=%v", ok, until)
	}
	Uncool("u1")
	Load(ConfigPath)
	if _, ok := CooldownUntil("u1"); ok {
		t.Fatal("uncooled account must stay uncooled after reload")
	}
}

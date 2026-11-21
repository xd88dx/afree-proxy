package freepi

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReconcileRoutingNewKeysDefaultDisabled 新增 key 必须默认不参与路由
// （key 池平台统一口径），已有 key 保持勾选状态，被移除的 key 清理表项。
func TestReconcileRoutingNewKeysDefaultDisabled(t *testing.T) {
	cur := &Config{Keys: []string{}}
	next := &Config{Keys: []string{"k1", "k2"}}
	next.ReconcileRouting(cur)
	if next.RoutingEnabled("k1") || next.RoutingEnabled("k2") {
		t.Fatal("new keys should default routing-disabled")
	}

	next2 := &Config{Keys: []string{"k1", "k3"}, KeyRoutingEnabled: map[string]bool{"k1": true}}
	next2.ReconcileRouting(next)
	if !next2.RoutingEnabled("k1") {
		t.Fatal("existing key k1 should stay enabled")
	}
	if next2.RoutingEnabled("k3") {
		t.Fatal("newly added key k3 should default routing-disabled")
	}

	next3 := &Config{Keys: []string{"k1"}, KeyRoutingEnabled: map[string]bool{"k1": true, "k2": false}}
	next3.ReconcileRouting(next)
	if _, ok := next3.KeyRoutingEnabled["k2"]; ok {
		t.Fatal("removed key k2 should be dropped from the routing table")
	}
}

// TestValidateJWTAndCleanToken 粘贴凭据的清洗与形状校验（对齐 plugin
// cleanToken/jwtShape：env 行/引号/Bearer 剥离；三段、43 字符签名、全 ASCII）。
func TestValidateJWTAndCleanToken(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0." + strings.Repeat("a", 43)
	if got := CleanToken("FREEPI_TOKEN=" + jwt); got != jwt {
		t.Fatalf("env line not stripped: %q", got)
	}
	if got := CleanToken("export FREEPI_TOKEN=\"" + jwt + "\""); got != jwt {
		t.Fatalf("export+quotes not stripped: %q", got)
	}
	if got := CleanToken("Bearer " + jwt); got != jwt {
		t.Fatalf("bearer prefix not stripped: %q", got)
	}
	if got := CleanToken(jwt + "\n  \n"); got != jwt {
		t.Fatalf("trailing blank lines not stripped: %q", got)
	}
	if err := ValidateJWT(jwt); err != nil {
		t.Fatalf("valid JWT rejected: %v", err)
	}
	if err := ValidateJWT("a.b"); err == nil {
		t.Fatal("two-part token must be rejected")
	}
	short := "h.p." + strings.Repeat("a", 40)
	if err := ValidateJWT(short); err == nil {
		t.Fatal("short signature must be rejected")
	}
	if err := ValidateJWT("h.p." + strings.Repeat("a", 42) + "，"); err == nil {
		t.Fatal("non-ASCII character must be rejected")
	}
	if err := ValidateJWT(strings.Repeat("a", 43)); err == nil {
		t.Fatal("no-dot blob must be rejected")
	}
}

// TestPickKeyAllCoolingFollowsUnableForce 全冷却默认拒绝；UNABLE_FORCE=true
// 恢复兜底（2026-10-28 用户定，key 池平台统一口径）。
func TestPickKeyAllCoolingFollowsUnableForce(t *testing.T) {
	savedCfg := cfg
	t.Cleanup(func() { cfg = savedCfg })
	cfg = &Config{Keys: []string{"a", "b"}}
	Cooldown("a", 5*time.Minute)
	Cooldown("b", 5*time.Minute)
	if got := PickKey("round_robin", nil); got != "" {
		t.Fatalf("default must reject when all keys are cooling, got %q", got)
	}
	t.Setenv("UNABLE_FORCE", "true")
	if got := PickKey("round_robin", nil); got != "a" && got != "b" {
		t.Fatalf("UNABLE_FORCE=true must fall back to a cooling key, got %q", got)
	}
}

// TestPickKeySkipsInFlightThenRelaxes 在途排他：空闲 key 优先；全在途时放宽
// 排他继续前进（concurrent 闸门由上游裁决，好过硬失败——单 key 池场景）。
func TestPickKeySkipsInFlightThenRelaxes(t *testing.T) {
	savedCfg, savedUsage := cfg, usage
	t.Cleanup(func() { cfg, usage = savedCfg, savedUsage })
	cfg = &Config{Keys: []string{"a", "b"}}
	usage = &usageData{} // 隔离前测残留的冷却（包级痕迹表共享）
	AcquireKey("a")
	if got := PickKey("round_robin", nil); got != "b" {
		t.Fatalf("idle key b must be preferred over in-flight a, got %q", got)
	}
	AcquireKey("b")
	got := PickKey("round_robin", nil)
	if got != "a" && got != "b" {
		t.Fatalf("all in-flight must relax exclusion, got %q", got)
	}
	ReleaseKey("a")
	ReleaseKey("b")
	if inFlightOf("a") != 0 || inFlightOf("b") != 0 {
		t.Fatal("release must clear in-flight counters")
	}
}

// TestCooldownPersistedAcrossReload 冷却落盘回读（2026-10-28 用户定）：
// Cooldown 落盘后模拟重启（重置一次性 loaded 标志重新 Load），冷却保留。
func TestCooldownPersistedAcrossReload(t *testing.T) {
	dir := t.TempDir()
	savedCfg, savedPath, savedUsage := cfg, ConfigPath, UsagePath
	savedLoaded := loaded
	t.Cleanup(func() {
		cfg, ConfigPath, UsagePath, loaded = savedCfg, savedPath, savedUsage, savedLoaded
		usage = &usageData{}
	})
	cfg = &Config{Keys: []string{"a"}}
	ConfigPath = filepath.Join(dir, "freepi-config.json")
	UsagePath = filepath.Join(dir, "freepi-state.json")
	loaded = false
	Load(ConfigPath)
	Set(&Config{Keys: []string{"a"}})
	Cooldown("a", 5*time.Minute)
	// 模拟重启：重置一次性标志重新加载同一对文件
	loaded = false
	usage = &usageData{}
	Load(ConfigPath)
	until, ok := CooldownUntil("a")
	if !ok || until.Before(time.Now()) {
		t.Fatalf("cooldown must survive reload, ok=%v until=%v", ok, until)
	}
}

// TestKnownModelsSortedAndStaticFallback 目录 = Fetched ∪ 静态兜底，按 ID
// 升序恒序（/v1/models 段内恒序铁律的包内事实源）；静态表兜底保证空目录
// 期仍有完整名单。
func TestKnownModelsSortedAndStaticFallback(t *testing.T) {
	savedCfg := cfg
	t.Cleanup(func() { cfg = savedCfg })
	cfg = &Config{Models: ModelState{Fetched: []Model{{ID: "zzz-live", Name: "Live"}}}}
	list := KnownModels()
	if len(list) != len(staticModels)+1 {
		t.Fatalf("known = %d, want static %d + 1 live", len(list), len(staticModels))
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].ID >= list[i].ID {
			t.Fatalf("KnownModels not sorted: %q >= %q", list[i-1].ID, list[i].ID)
		}
	}
	if _, ok := ResolveModel("glm-5.3-flash"); !ok {
		t.Fatal("static fallback model must resolve")
	}
	if _, ok := ResolveModel("zzz-live"); !ok {
		t.Fatal("fetched model must resolve")
	}
	if _, ok := ResolveModel("totally-unknown"); ok {
		t.Fatal("unknown model must not resolve")
	}
}

// TestCatalogFailClosed 目录同步出口全不可用时 fail-closed（本轮跳过，
// 绝不隐式回落直连——2026-11-09 补齐的 other 同款判据）。
func TestCatalogFailClosed(t *testing.T) {
	savedCfg, savedExit := cfg, catalogExit
	t.Cleanup(func() { cfg, catalogExit = savedCfg, savedExit })
	cfg = &Config{Keys: []string{"a"}, KeyRoutingEnabled: map[string]bool{"a": true}}
	catalogExit = func(key string) (string, bool) { return "", false }
	if _, _, err := catalogClientFor([]string{"a"}); err == nil {
		t.Fatal("all exits unavailable must fail closed")
	}
	// 面板显式选直连（ok=true 且空 URL）才允许直连默认 client
	catalogExit = func(key string) (string, bool) { return "", true }
	k, _, err := catalogClientFor([]string{"a"})
	if err != nil || k != "a" {
		t.Fatalf("explicit direct must pass, key=%q err=%v", k, err)
	}
}

// TestCreditsSetAndPrune credits 快照随 /me 落表；key 移除时随 PruneUsage 收敛。
func TestCreditsSetAndPrune(t *testing.T) {
	savedUsage := usage
	t.Cleanup(func() { usage = savedUsage; UsagePath = "" })
	UsagePath = "" // 内存态即可
	SetCredits("a", CreditStats{Remaining: 12000, Allowance: 15000, Handle: "octo"})
	SetCredits("b", CreditStats{Remaining: 1, Allowance: 2})
	keyMu.Lock()
	cr := usage.Credits["a"]
	keyMu.Unlock()
	if cr.Remaining != 12000 || cr.Handle != "octo" {
		t.Fatalf("credits snapshot lost: %+v", cr)
	}
	// PruneUsage 的 keys = 当前 key 池：不在池内的条目收敛移除
	PruneUsage([]string{"a"})
	keyMu.Lock()
	_, stillA := usage.Credits["a"]
	_, stillB := usage.Credits["b"]
	keyMu.Unlock()
	if !stillA {
		t.Fatal("PruneUsage must keep in-pool key's credits")
	}
	if stillB {
		t.Fatal("PruneUsage must drop removed key's credits")
	}
}

// TestPickKeyCreditWeighted 额度加权（wb 权重同思想）：权重 10000:1 时高余额
// key 应以压倒性概率被选中（200 次抽样 ≥180）——「按剩余额度加权、额度趋平」。
func TestPickKeyCreditWeighted(t *testing.T) {
	savedCfg, savedUsage := cfg, usage
	t.Cleanup(func() { cfg, usage = savedCfg, savedUsage })
	cfg = &Config{Keys: []string{"a", "b"}}
	usage = &usageData{Credits: map[string]CreditStats{
		"a": {Remaining: 10000, Allowance: 15000},
		"b": {Remaining: 1, Allowance: 15000},
	}}
	aCount := 0
	const N = 200
	for i := 0; i < N; i++ {
		if PickKey("round_robin", nil) == "a" {
			aCount++
		}
	}
	if aCount < 180 {
		t.Fatalf("credit-weighted pick: want a >= 180/%d, got %d", N, aCount)
	}
}

package exitpool

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// TestRegionOf 地区代码提取（2026-11-24 用户定）：别名末尾 "-XX"（横杠后
// 恰好 2 个字母）；3 字母（USE）、无横杠（DDSG/Mikrus）= 无地区。
func TestRegionOf(t *testing.T) {
	cases := map[string]string{
		"socks5://h:1#elynode-FI":    "FI",
		"socks5://h:1#cloady-JP":     "JP",
		"socks5://h:1#zz-altrth4-TH": "TH",
		"socks5://h:1#cloady-USE":    "",
		"socks5://h:1#DDSG":          "",
		"socks5://h:1#Mikrus":        "",
		"socks5://h:1#zz-altrau1-AU": "AU",
	}
	for in, want := range cases {
		if got := regionOf(in); got != want {
			t.Fatalf("regionOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// setupFailoverPool 测试池：a-jp / x-us / y-jp / z-au，binding 段走临时文件。
func setupFailoverPool(t *testing.T) {
	PathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { PathOverride = "" })
	ActivatePool(&PoolConfig{Proxies: []string{
		"socks5://h:1#a-jp",
		"socks5://h:2#x-us",
		"socks5://h:3#y-jp",
		"socks5://h:4#z-au",
	}, Strategy: "round_robin"})
	t.Cleanup(func() { ActivatePool(&PoolConfig{Strategy: "round_robin"}) })
}

// TestPickReplacementRegionPriority 替补选择（2026-11-24 用户定）：从后往前
// 扫、同地区优先于遍历顺序——a-jp 故障时池序 x-us,y-jp,z-au 的末尾是 z-au，
// 但 y-jp 地区匹配，选 y-jp；无同地区时取从后往前第一个候选。
func TestPickReplacementRegionPriority(t *testing.T) {
	setupFailoverPool(t)
	got, ok := pickReplacement("", "socks5://h:1#a-jp", []string{"wb"})
	if !ok || got != "socks5://h:3#y-jp" {
		t.Fatalf("region priority: got %q ok=%v, want y-jp", got, ok)
	}
	// 无同地区：故障 x-us → 从后往前第一个候选 z-au。
	got, ok = pickReplacement("", "socks5://h:2#x-us", []string{"wb"})
	if !ok || got != "socks5://h:4#z-au" {
		t.Fatalf("no region match: got %q ok=%v, want z-au", got, ok)
	}
}

// TestPickReplacementSkipsPlatformCooled 平台级冷却的出口不进替补候选
// （2026-11-25 补审计 R1：空 kind 只看全局层，会漏掉仅对该平台冷却的出口）。
func TestPickReplacementSkipsPlatformCooled(t *testing.T) {
	setupFailoverPool(t)
	cooldownsMu.Lock()
	platformCooldowns = map[string]map[int]time.Time{}
	cooldownsMu.Unlock()
	t.Cleanup(func() {
		cooldownsMu.Lock()
		platformCooldowns = map[string]map[int]time.Time{}
		cooldownsMu.Unlock()
	})
	// y-jp（池内下标 2）对 wb 平台级冷却 → 同地区候选被跳过，落到 z-au。
	CooldownUpstreamProxyFor("wb", 2, 10*time.Minute)
	got, ok := pickReplacement("wb", "socks5://h:1#a-jp", []string{"wb"})
	if !ok || got != "socks5://h:4#z-au" {
		t.Fatalf("platform-cooled y-jp must be skipped: got %q ok=%v, want z-au", got, ok)
	}
	// 空 kind（无平台上下文的调用方）不受平台级冷却影响——语义不变。
	got, ok = pickReplacement("", "socks5://h:1#a-jp", []string{"wb"})
	if !ok || got != "socks5://h:3#y-jp" {
		t.Fatalf("empty kind must ignore platform cooldown: got %q ok=%v, want y-jp", got, ok)
	}
}

// boundFake 绑定适配器替身：Pairs 声明一批已绑定出口。
type boundFake struct{ bound []string }

func (f boundFake) Pairs() []Binding {
	var out []Binding
	for _, p := range f.bound {
		out = append(out, Binding{Main: p})
	}
	return out
}
func (boundFake) Rebind(from, to string) int    { return 0 }
func (boundFake) RebindAll(from, to string) int { return 0 }

// TestPickReplacementSkipsBoundAndExcluded 严格候选：被绑定/被排除的出口不
// 进候选；排除后从剩余候选按同地区优先挑选。
func TestPickReplacementSkipsBoundAndExcluded(t *testing.T) {
	setupFailoverPool(t)
	RegisterFailoverBindings("wb", boundFake{bound: []string{"socks5://h:3#y-jp"}})
	t.Cleanup(func() {
		failoverMu.Lock()
		delete(failoverBindings, "wb")
		failoverMu.Unlock()
	})
	// y-jp 已被绑定 → 跳过 → z-au。
	got, ok := pickReplacement("", "socks5://h:1#a-jp", []string{"wb"})
	if !ok || got != "socks5://h:4#z-au" {
		t.Fatalf("bound skipped: got %q ok=%v, want z-au", got, ok)
	}
	// 把 z-au 加入排除 → 只剩 x-us。
	addExclusion("wb", "socks5://h:4#z-au", "https://www.workbuddy.ai/")
	if !proxyExcludedFor("wb", "socks5://h:4#z-au") {
		t.Fatal("z-au must be excluded for wb after addExclusion")
	}
	got, ok = pickReplacement("", "socks5://h:1#a-jp", []string{"wb"})
	if !ok || got != "socks5://h:2#x-us" {
		t.Fatalf("excluded skipped: got %q ok=%v, want x-us", got, ok)
	}
}

// TestAutoFailoverDefaultOff 开关缺省关闭（nil = 关）。
func TestAutoFailoverDefaultOff(t *testing.T) {
	PathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { PathOverride = "" })
	if AutoFailoverEnabled() {
		t.Fatal("auto failover must default to off")
	}
	on := true
	SetBinding(&BindingConfig{ProxyAutoFailover: &on})
	if !AutoFailoverEnabled() {
		t.Fatal("auto failover must follow explicit true")
	}
}

// TestFailedTargetOf 从触发错误提取目标 base URL。
func TestFailedTargetOf(t *testing.T) {
	err := errFake(`Get "https://www.workbuddy.ai/v3/config": net/http: TLS handshake timeout`)
	if got := failedTargetOf(err); got != "https://www.workbuddy.ai/" {
		t.Fatalf("failedTargetOf = %q", got)
	}
	if got := failedTargetOf(errFake("plain error")); got != "" {
		t.Fatalf("plain error must yield empty, got %q", got)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

// rebindFake 记录 Rebind 调用的适配器替身。OnExitFailure 异步 goroutine
// 写 calls，测试主线程读，必须加锁（否则 -race DATA RACE，2026-11-24 CI）。
type rebindFake struct {
	mu    sync.Mutex
	bound []string
	calls []string
}

func (f *rebindFake) Pairs() []Binding {
	var out []Binding
	for _, p := range f.bound {
		out = append(out, Binding{Main: p})
	}
	return out
}
func (f *rebindFake) RebindAll(from, to string) int { return f.Rebind(from, to) }

func (f *rebindFake) Rebind(from, to string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, from+"->"+to)
	return 2
}

// Calls 返回调用记录的快照（供测试主线程并发安全读取）。
func (f *rebindFake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// waitCond 轮询等待条件成立（异步 goroutine 判定完成）。
func waitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		sleepMs(20)
	}
	t.Fatalf("timeout waiting for %s", what)
}

// waitFailoverIdle 等待所有在途 failover goroutine 收尾（替代固定 sleep，
// 避免跨场景的钩子/替身读写与上一场景异步任务重叠）。
func waitFailoverIdle(t *testing.T) {
	t.Helper()
	for i := 0; i < 250; i++ {
		failoverMu.Lock()
		n := len(failoverRunning)
		failoverMu.Unlock()
		if n == 0 {
			return
		}
		sleepMs(20)
	}
	t.Fatal("timeout waiting for failover goroutines to finish")
}

// TestOnPlatformScopedFailureEndToEnd 差分故障转移全链路（2026-11-24 用户定）：
// 开关开 + 三元组成立 → 故障出口进入该平台排除表 + 绑定它的凭据改绑到
// 同地区替补（a-jp → y-jp，不是池尾的 z-au）；三元组任一不成立则不动。
func TestOnPlatformScopedFailureEndToEnd(t *testing.T) {
	setupFailoverPool(t)
	// 复测确认延迟缩短到毫秒级（3 分钟是生产值，测试白盒缩短）。
	savedDelay := failoverRecheckDelay
	failoverRecheckDelay = 10 * time.Millisecond
	t.Cleanup(func() { failoverRecheckDelay = savedDelay })
	on := true
	SetBinding(&BindingConfig{ProxyAutoFailover: &on})
	rb := &rebindFake{bound: []string{"socks5://h:1#a-jp"}}
	RegisterFailoverBindings("wb", rb)
	t.Cleanup(func() {
		failoverMu.Lock()
		delete(failoverBindings, "wb")
		failoverMu.Unlock()
		setProbeHook(nil)
	})
	const failed = "socks5://h:1#a-jp"
	const wbErr = errFake(`Get "https://www.workbuddy.ai/v3/config": net/http: TLS handshake timeout`)

	// 场景 1：出口死（测速目标探测失败）→ 改绑但不排除（2026-11-24 用户定）。
	setProbeHook(func(_ context.Context, proxy, target string) error {
		if SameAlias(proxy, failed) {
			return errFake("socks connect: EOF")
		}
		return nil
	})
	OnExitFailure("wb", failed, wbErr)
	waitCond(t, "dead-exit rebind", func() bool { return len(rb.Calls()) == 1 })
	waitFailoverIdle(t)
	if proxyExcludedFor("wb", failed) {
		t.Fatal("dead exit must NOT be platform-excluded")
	}
	calls := rb.Calls()
	if calls[0] != failed+"->socks5://h:3#y-jp" {
		t.Fatalf("dead-exit rebind = %v, want a-jp->y-jp", calls)
	}

	// 场景 2：目标整体故障（旁证出口也超时）→ 不排除不改绑。
	setProbeHook(func(_ context.Context, proxy, target string) error {
		if SameAlias(proxy, failed) {
			return nil // 测速目标通
		}
		return errFake("net/http: TLS handshake timeout") // 旁证全挂
	})
	OnExitFailure("wb", failed, wbErr)
	waitFailoverIdle(t)
	if proxyExcludedFor("wb", failed) {
		t.Fatal("target-wide outage must not exclude the exit")
	}
	if len(rb.Calls()) != 1 {
		t.Fatalf("target-wide outage must not rebind, calls = %v", rb.Calls())
	}

	// 场景 3：复测自愈（transient path failure，2026-11-26 用户定）——测速
	// 目标通、旁证通，但 3 分钟后复测目标 host 已恢复 → 不排除不改绑。
	setProbeHook(func(_ context.Context, proxy, target string) error {
		return nil
	})
	OnExitFailure("wb", failed, wbErr)
	waitFailoverIdle(t)
	if proxyExcludedFor("wb", failed) {
		t.Fatal("transient failure must not be excluded after recheck")
	}
	if len(rb.Calls()) != 1 {
		t.Fatalf("transient failure must not rebind, calls = %v", rb.Calls())
	}

	// 场景 4：三元组成立 + 复测仍不通 → 排除 + 改绑到同地区替补 y-jp。
	// hook 按目标区分：测速目标（apple）通、目标 host（workbuddy）持续不通。
	setProbeHook(func(_ context.Context, proxy, target string) error {
		if SameAlias(proxy, failed) && strings.Contains(target, "workbuddy") {
			return errFake("net/http: TLS handshake timeout")
		}
		return nil
	})
	OnExitFailure("wb", failed, wbErr)
	waitCond(t, "exclusion", func() bool { return proxyExcludedFor("wb", failed) })
	waitFailoverIdle(t)
	calls = rb.Calls()
	if len(calls) != 2 || calls[1] != failed+"->socks5://h:3#y-jp" {
		t.Fatalf("rebind = %v, want second call a-jp->y-jp", calls)
	}

	// 场景 5：已排除过的出口再次触发 → 不重复写排除表，改绑仍执行（幂等
	// 补做——"仅排除未改绑"的卡死场景靠此自愈，2026-11-24 用户定）。
	OnExitFailure("wb", failed, wbErr)
	waitCond(t, "idempotent rebind", func() bool { return len(rb.Calls()) == 3 })
	waitFailoverIdle(t)
	if !proxyExcludedFor("wb", failed) {
		t.Fatal("exclusion must persist across retrigger")
	}

	// 场景 5：出口死且无绑定凭据 → 改绑 no-op（不排除、不调用）。
	empty := &rebindFake{}
	RegisterFailoverBindings("acc", empty)
	t.Cleanup(func() {
		failoverMu.Lock()
		delete(failoverBindings, "acc")
		failoverMu.Unlock()
	})
	setProbeHook(func(_ context.Context, proxy, target string) error {
		if SameAlias(proxy, failed) {
			return errFake("socks connect: EOF")
		}
		return nil
	})
	OnExitFailure("acc", failed, wbErr)
	waitFailoverIdle(t)
	if len(empty.Calls()) != 0 {
		t.Fatalf("no-bound-credentials dead exit must not rebind, got %v", empty.Calls())
	}
}

// TestRuleKindsFor 冷却 kind → 排除规则 kind 映射；未注册 other 枚举时不触发。
func TestRuleKindsFor(t *testing.T) {
	if ks, ok := ruleKindsFor(KindWb); !ok || len(ks) != 1 || ks[0] != "wb" {
		t.Fatalf("wb => %v ok=%v", ks, ok)
	}
	if ks, ok := ruleKindsFor(KindCline); !ok || ks[0] != "acc" {
		t.Fatalf("cline => %v ok=%v", ks, ok)
	}
	if _, ok := ruleKindsFor("nope"); ok {
		t.Fatal("unknown kind must not trigger")
	}
	failoverMu.Lock()
	saved := otherRuleKindsFn
	otherRuleKindsFn = nil
	failoverMu.Unlock()
	t.Cleanup(func() {
		failoverMu.Lock()
		otherRuleKindsFn = saved
		failoverMu.Unlock()
	})
	if _, ok := ruleKindsFor(KindOther); ok {
		t.Fatal("other without registered slugs must not trigger")
	}
}

// realmFake 带凭据域的适配器替身（zone 约束测试用）。
type realmFake struct{ entries []BindingEntry }

func (f realmFake) Pairs() []Binding {
	var out []Binding
	for _, e := range f.entries {
		out = append(out, Binding{Main: e.Main, Backup: e.Backup})
	}
	return out
}
func (f realmFake) Entries() []BindingEntry       { return f.entries }
func (f realmFake) Rebind(from, to string) int    { return 0 }
func (f realmFake) RebindAll(from, to string) int { return 0 }

// TestPickReplacementZoneRealm 国内出口改绑的域约束（2026-11-26 用户定语义）：
// 无域凭据绑定的国内出口故障 → 候选无 zone 约束（未声明平台把国内出口当
// 普通出口）；声明国际凭据在场 → 替补限定不限出口。国内域槽位由适配器
// Rebind 跳过（不参与改绑）。
func TestPickReplacementZoneRealm(t *testing.T) {
	PathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { PathOverride = "" })
	ActivatePool(&PoolConfig{Proxies: []string{
		"socks5://h:1#x-us", // 不限
		"socks5://h:2#f-jp", // 故障出口（不限）
		"socks5://h:3#c-jp", // 国内（扫描序最靠后 → 同地区优先会先碰到它）
	}, Zones: map[string]string{"socks5://h:3#c-jp": ZoneCN}, Strategy: "round_robin"})
	t.Cleanup(func() { ActivatePool(&PoolConfig{Strategy: "round_robin"}) })

	register := func(entries []BindingEntry) {
		RegisterFailoverBindings("wb", realmFake{entries: entries})
		t.Cleanup(func() {
			failoverMu.Lock()
			delete(failoverBindings, "wb")
			failoverMu.Unlock()
		})
	}

	// 场景 A：无域凭据绑 f-jp → c-jp（国内出口对无域凭据是普通候选，且同地区优先）。
	register([]BindingEntry{{Key: "k1", Main: "socks5://h:2#f-jp", Realm: ""}})
	got, ok := pickReplacement("", "socks5://h:2#f-jp", []string{"wb"})
	if !ok || got != "socks5://h:3#c-jp" {
		t.Fatalf("no-realm: got %q ok=%v, want c-jp", got, ok)
	}
	// 场景 B：声明国际凭据绑 f-jp → 候选限定不限出口，c-jp 跳过 → x-us。
	register([]BindingEntry{{Key: "k2", Main: "socks5://h:2#f-jp", Realm: "global"}})
	got, ok = pickReplacement("", "socks5://h:2#f-jp", []string{"wb"})
	if !ok || got != "socks5://h:1#x-us" {
		t.Fatalf("intl: got %q ok=%v, want x-us (cn exits excluded)", got, ok)
	}
}

// TestReviewExclusionsAutoUnban TTL 复核（2026-11-26 用户定：半小时一次、
// 探通 1 次即解禁）：差分排除的条目恢复可达 1 次自动移除；失败不计数；
// 无台账的手动条目不动。
func TestReviewExclusionsAutoUnban(t *testing.T) {
	setupFailoverPool(t)
	on := true
	SetBinding(&BindingConfig{ProxyAutoFailover: &on})
	t.Cleanup(func() { setProbeHook(nil) })

	// 差分写入一条（带台账）+ 手动写一条（无台账）。
	addExclusion("wb", "socks5://h:4#z-au", "https://www.workbuddy.ai/")
	SetBinding(func() *BindingConfig {
		cp := *GetBinding()
		next := make(map[string][]string, len(cp.ProxyExclusions)+1)
		for p, ks := range cp.ProxyExclusions {
			next[p] = append([]string(nil), ks...)
		}
		next["socks5://h:3#y-jp"] = []string{"wb"}
		cp.ProxyExclusions = next
		return &cp
	}())

	// 探活通过：okCount=1 即达阈值 → 差分条目立即自动移除；手动条目（无台账）保留。
	setProbeHook(func(_ context.Context, proxy, target string) error { return nil })
	reviewExclusions()
	if proxyExcludedFor("wb", "socks5://h:4#z-au") {
		t.Fatal("one recovery probe must auto-unban the diff-written entry")
	}
	if !proxyExcludedFor("wb", "socks5://h:3#y-jp") {
		t.Fatal("manual entry (no stamp) must never auto-unban")
	}
	// 台账已清。
	if _, ok := GetBinding().ProxyExclusionStamps[stampKey(RuleKeyOf("socks5://h:4#z-au"), "wb")]; ok {
		t.Fatal("stamp must be cleared after auto-unban")
	}

	// 探活失败 → 不计数，条目保持。
	addExclusion("wb", "socks5://h:2#x-us", "https://www.workbuddy.ai/")
	setProbeHook(func(_ context.Context, proxy, target string) error {
		return errFake("net/http: TLS handshake timeout")
	})
	reviewExclusions() // 失败 → 计数保持 0
	_, _, okCount := parseStamp(GetBinding().ProxyExclusionStamps[stampKey(RuleKeyOf("socks5://h:2#x-us"), "wb")])
	if okCount != 0 {
		t.Fatalf("failed probe must reset ok count, got %d", okCount)
	}
	if !proxyExcludedFor("wb", "socks5://h:2#x-us") {
		t.Fatal("entry must stay excluded while target unreachable")
	}
}

// reorderFake 定时重排的适配器替身：Entries 带 realm、RebindMain 记录移动。
type reorderFake struct {
	mu      sync.Mutex
	entries []BindingEntry
	moves   []string
}

func (f *reorderFake) Pairs() []Binding {
	var out []Binding
	for _, e := range f.entries {
		out = append(out, Binding{Main: e.Main, Backup: e.Backup})
	}
	return out
}
func (f *reorderFake) Entries() []BindingEntry       { return f.entries }
func (f *reorderFake) RebindAll(from, to string) int { return f.Rebind(from, to) }

func (f *reorderFake) Rebind(from, to string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, from+"->"+to)
	return 1
}
func (f *reorderFake) RebindMain(ruleKind, from, to string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, "main:"+from+"->"+to)
	return 1
}
func (f *reorderFake) Moves() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.moves...)
}

func setHS(t *testing.T, url string, hs int64) {
	EnsureHealthLoaded()
	healthMu.Lock()
	// 键 = 备注名（RuleKeyOf，2026-11-26 主键）——与生产写入路径
	// RecordProxyOutcome 同口径，保证 reorderHS 的查询键对齐被测试锁住。
	healthStats[RuleKeyOf(url)] = &HealthEntry{HSCount: 5, HSSumMs: hs * 5}
	healthMu.Unlock()
	t.Cleanup(func() {
		healthMu.Lock()
		delete(healthStats, RuleKeyOf(url))
		healthMu.Unlock()
	})
}

// TestRunReorderForKind 后端定时重排（2026-11-26 用户定）：cn 条目冻结且其
// 出口不进供给（占用保护）；无域慢条目修复到最优候选——国内出口对无域凭据
// 也是候选（新语义）；声明国际的快条目冻结。
func TestRunReorderForKind(t *testing.T) {
	PathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { PathOverride = "" })
	ActivatePool(&PoolConfig{Proxies: []string{
		"socks5://h:1#a-jp",
		"socks5://h:2#b-us",
		"socks5://h:3#c-fr",
		"socks5://h:4#z-au",
	}, Zones: map[string]string{
		"socks5://h:1#a-jp": ZoneCN,
		"socks5://h:4#z-au": ZoneCN,
	}, Strategy: "round_robin"})
	t.Cleanup(func() { ActivatePool(&PoolConfig{Strategy: "round_robin"}) })
	// 健康：a-jp/z-au 快（cn 保留），b-us 快，c-fr 慢（要被修复）。
	setHS(t, "socks5://h:1#a-jp", 800)
	setHS(t, "socks5://h:2#b-us", 1000)
	setHS(t, "socks5://h:3#c-fr", 20000)
	setHS(t, "socks5://h:4#z-au", 900)

	fk := &reorderFake{entries: []BindingEntry{
		{Key: "cn-cred", Main: "socks5://h:1#a-jp", Realm: "cn"},         // cn：冻结
		{Key: "slow-cred", Main: "socks5://h:3#c-fr", Realm: ""},         // 无域 + 慢 → 修复
		{Key: "global-cred", Main: "socks5://h:2#b-us", Realm: "global"}, // global + 快 → 冻结
	}}
	RegisterFailoverBindings("acc", fk)
	t.Cleanup(func() {
		failoverMu.Lock()
		delete(failoverBindings, "acc")
		failoverMu.Unlock()
	})

	n := RunReorderForKind("acc")
	if n != 1 {
		t.Fatalf("changed = %d, want 1", n)
	}
	// 无域慢条目 c-fr 的候选含国内出口 z-au（快 900ms，比 b-us 更快）→ 选 z-au。
	moves := fk.Moves()
	if len(moves) != 1 || moves[0] != "main:socks5://h:3#c-fr->socks5://h:4#z-au" {
		t.Fatalf("moves = %v, want c-fr -> z-au only", moves)
	}
}

// TestRunReorderForKindIntlBlock 声明国际的凭据不可被重排到国内出口
// （2026-11-26 用户定 B 语义：国际域唯一效果 = 不分配国内出口）。
func TestRunReorderForKindIntlBlock(t *testing.T) {
	PathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { PathOverride = "" })
	ActivatePool(&PoolConfig{Proxies: []string{
		"socks5://h:1#a-jp",
		"socks5://h:2#b-us",
		"socks5://h:3#c-fr",
	}, Zones: map[string]string{
		"socks5://h:1#a-jp": ZoneCN,
	}, Strategy: "round_robin"})
	t.Cleanup(func() { ActivatePool(&PoolConfig{Strategy: "round_robin"}) })
	setHS(t, "socks5://h:1#a-jp", 500)
	setHS(t, "socks5://h:2#b-us", 9000)  // 慢于阈值
	setHS(t, "socks5://h:3#c-fr", 10000) // 更慢

	fk := &reorderFake{entries: []BindingEntry{
		{Key: "k1", Main: "socks5://h:2#b-us", Realm: "global"},
		{Key: "k2", Main: "socks5://h:3#c-fr", Realm: "global"},
	}}
	RegisterFailoverBindings("key", fk)
	t.Cleanup(func() {
		failoverMu.Lock()
		delete(failoverBindings, "key")
		failoverMu.Unlock()
	})

	RunReorderForKind("key")
	// 全场最快的 a-jp 是国内出口，对国际凭据不兼容；其余候选同档慢不更优
	// → 无改动。
	if m := fk.Moves(); len(m) != 0 {
		t.Fatalf("cn exit must not be offered to intl rows, moves = %v", m)
	}
}

// TestRunReorderForKindDirect 直连行（2026-11-26 修）：显式直连（Main =
// direct 哨兵）在"排除直连"规则成立时被强制改绑到可用代理；未排除时照旧
// 完全不参与（既有口径）。此前 reorder 用 eurl=="" 判直连（错把全局行当
// 直连、漏掉 direct 行），且改绑传 "direct" 给适配器的别名匹配恒不中。
func TestRunReorderForKindDirect(t *testing.T) {
	PathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { PathOverride = "" })
	ActivatePool(&PoolConfig{Proxies: []string{
		"socks5://h:1#a-jp",
		"socks5://h:2#b-us",
	}, Strategy: "round_robin"})
	t.Cleanup(func() { ActivatePool(&PoolConfig{Strategy: "round_robin"}) })

	fk := &reorderFake{entries: []BindingEntry{
		{Key: "direct-cred", Main: DirectSentinel, Realm: "", Kind: "acc"},
	}}
	RegisterFailoverBindings("acc", fk)
	t.Cleanup(func() {
		failoverMu.Lock()
		delete(failoverBindings, "acc")
		failoverMu.Unlock()
	})

	// ① 直连未被该平台排除：不参与重排，无改动。
	SetBinding(&BindingConfig{})
	if n := RunReorderForKind("acc"); n != 0 || len(fk.Moves()) != 0 {
		t.Fatalf("direct row without __direct__ exclusion must stay, n=%d moves=%v", n, fk.Moves())
	}
	// ② 排除直连（__direct__ 键在排除表）：强制挪到池序第一候选。
	SetBinding(&BindingConfig{ProxyExclusions: map[string][]string{ProxyRuleDirectKey: {"acc"}}})
	t.Cleanup(func() { SetBinding(&BindingConfig{}) })
	n := RunReorderForKind("acc")
	moves := fk.Moves()
	if n != 1 || len(moves) != 1 || moves[0] != "main:"+DirectSentinel+"->socks5://h:1#a-jp" {
		t.Fatalf("direct-excluded row must rebind to first candidate, n=%d moves=%v", n, moves)
	}
}

// TestIsInterceptBody 拦截页特征判定（2026-11-27 probeExitAlive 判定核心）：
// 只认 <!doctype html / <html 两个标记，大小写不敏感；JSON/SSE/空体/纯文本
// 一律不命中——"没把握不排除"（2026-11-27 用户定），唯一判死信号是出口
// 劫持的高置信特征。apple 测试页（用户实测测速目标）以 <!DOCTYPE html
// 开头会命中本判定，这正是步骤① 测速目标探测不得换用 probeExitAlive 的
// 原因（见 runFailover ① 注释）。
func TestIsInterceptBody(t *testing.T) {
	cases := map[string]bool{
		"<!DOCTYPE html>":                   true,
		`<!doctype html><html lang="en">`:   true,
		"<html><body>blocked</body></html>": true,
		"  \n<HTML> uppercase":              true,
		`<Html Lang="zh">`:                  true,
		`{"choices":[]}`:                    false,
		"data: {\"usage\":{}}\n\n":          false,
		"":                                  false,
		"region not supported":              false,
		`{"error":"region not supported"}`:  false,
		"gateway timeout":                   false,
	}
	for body, want := range cases {
		if got := isInterceptBody(body); got != want {
			t.Fatalf("isInterceptBody(%q) = %v, want %v", body, got, want)
		}
	}
}

// TestProbeExitAliveRespectsHook probeExitAlive 必须尊重 probeHook：判定核心
// 由 TestIsInterceptBody 覆盖，接线行为（②旁证与③复测走拦截页感知口径）
// 依赖既有 hook 注入测试——hook 分支若被移除，本测试先挂，避免既有端到端
// 测试静默改走真实网络。
func TestProbeExitAliveRespectsHook(t *testing.T) {
	sentinel := errFake("hook: html intercept page")
	setProbeHook(func(_ context.Context, _, _ string) error { return sentinel })
	t.Cleanup(func() { setProbeHook(nil) })
	err := probeExitAlive(context.Background(), "socks5://h:1#a-jp", "https://www.workbuddy.ai/")
	if err != sentinel {
		t.Fatalf("probeExitAlive must delegate to probeHook, got %v", err)
	}
}

// TestWitnessExistsVerifyBodySplit witnessExists 的验体开关接线：true 走
// probeExitAlive（拦截页感知）、false 走 probeURL（收到响应即成立）——① 分支
// 对 HTML 测速目标必须传 false，否则健康出口的正常 HTML 响应被判拦截页、
// 旁证恒失败，出口死永不改绑（2026-11-28 生产实测死锁）。hook 层两函数共用
// 同一注入点，这里用 failHook 区分目标锁接线；两调用点（① false / ② true）
// 由 runFailover 内联保证。
func TestWitnessExistsVerifyBodySplit(t *testing.T) {
	setupFailoverPool(t)
	setProbeHook(func(_ context.Context, _, _ string) error { return nil })
	t.Cleanup(func() { setProbeHook(nil) })
	const kind = "wb"
	if !witnessExists(context.Background(), kind, "socks5://h:9#dead", "https://t.example/success.html", false) {
		t.Fatal("verifyBody=false must succeed when the target answers")
	}
	if !witnessExists(context.Background(), kind, "socks5://h:9#dead", "https://www.workbuddy.ai/", true) {
		t.Fatal("verifyBody=true must succeed when the target answers non-intercept")
	}
}

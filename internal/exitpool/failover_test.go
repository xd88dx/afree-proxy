package exitpool

import (
	"context"
	"path/filepath"
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
	got, ok := pickReplacement("socks5://h:1#a-jp", []string{"wb"})
	if !ok || got != "socks5://h:3#y-jp" {
		t.Fatalf("region priority: got %q ok=%v, want y-jp", got, ok)
	}
	// 无同地区：故障 x-us → 从后往前第一个候选 z-au。
	got, ok = pickReplacement("socks5://h:2#x-us", []string{"wb"})
	if !ok || got != "socks5://h:4#z-au" {
		t.Fatalf("no region match: got %q ok=%v, want z-au", got, ok)
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
func (boundFake) Rebind(from, to string) int { return 0 }

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
	got, ok := pickReplacement("socks5://h:1#a-jp", []string{"wb"})
	if !ok || got != "socks5://h:4#z-au" {
		t.Fatalf("bound skipped: got %q ok=%v, want z-au", got, ok)
	}
	// 把 z-au 加入排除 → 只剩 x-us。
	addExclusion("wb", "socks5://h:4#z-au")
	if !proxyExcludedFor("wb", "socks5://h:4#z-au") {
		t.Fatal("z-au must be excluded for wb after addExclusion")
	}
	got, ok = pickReplacement("socks5://h:1#a-jp", []string{"wb"})
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

// rebindFake 记录 Rebind 调用的适配器替身。
type rebindFake struct {
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
func (f *rebindFake) Rebind(from, to string) int {
	f.calls = append(f.calls, from+"->"+to)
	return 2
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

// TestOnPlatformScopedFailureEndToEnd 差分故障转移全链路（2026-11-24 用户定）：
// 开关开 + 三元组成立 → 故障出口进入该平台排除表 + 绑定它的凭据改绑到
// 同地区替补（a-jp → y-jp，不是池尾的 z-au）；三元组任一不成立则不动。
func TestOnPlatformScopedFailureEndToEnd(t *testing.T) {
	setupFailoverPool(t)
	on := true
	SetBinding(&BindingConfig{ProxyAutoFailover: &on})
	rb := &rebindFake{bound: []string{"socks5://h:1#a-jp"}}
	RegisterFailoverBindings("wb", rb)
	t.Cleanup(func() {
		failoverMu.Lock()
		delete(failoverBindings, "wb")
		probeHook = nil
		failoverMu.Unlock()
	})
	const failed = "socks5://h:1#a-jp"
	const wbErr = errFake(`Get "https://www.workbuddy.ai/v3/config": net/http: TLS handshake timeout`)

	// 场景 1：出口死（测速目标探测失败）→ 改绑但不排除（2026-11-24 用户定）。
	probeHook = func(_ context.Context, proxy, target string) error {
		if SameAlias(proxy, failed) {
			return errFake("socks connect: EOF")
		}
		return nil
	}
	OnExitFailure("wb", failed, wbErr)
	waitCond(t, "dead-exit rebind", func() bool { return len(rb.calls) == 1 })
	if proxyExcludedFor("wb", failed) {
		t.Fatal("dead exit must NOT be platform-excluded")
	}
	if rb.calls[0] != failed+"->socks5://h:3#y-jp" {
		t.Fatalf("dead-exit rebind = %v, want a-jp->y-jp", rb.calls)
	}

	// 场景 2：目标整体故障（旁证出口也超时）→ 不排除不改绑。
	probeHook = func(_ context.Context, proxy, target string) error {
		if SameAlias(proxy, failed) {
			return nil // 测速目标通
		}
		return errFake("net/http: TLS handshake timeout") // 旁证全挂
	}
	OnExitFailure("wb", failed, wbErr)
	sleepMs(300)
	if proxyExcludedFor("wb", failed) {
		t.Fatal("target-wide outage must not exclude the exit")
	}
	if len(rb.calls) != 1 {
		t.Fatalf("target-wide outage must not rebind, calls = %v", rb.calls)
	}

	// 场景 3：三元组成立 → 排除 + 改绑到同地区替补 y-jp。
	probeHook = func(_ context.Context, proxy, target string) error {
		return nil
	}
	OnExitFailure("wb", failed, wbErr)
	waitCond(t, "exclusion", func() bool { return proxyExcludedFor("wb", failed) })
	if len(rb.calls) != 2 || rb.calls[1] != failed+"->socks5://h:3#y-jp" {
		t.Fatalf("rebind = %v, want second call a-jp->y-jp", rb.calls)
	}

	// 场景 4：已排除过的出口再次触发 → 不重复写排除表，改绑仍执行（幂等
	// 补做——"仅排除未改绑"的卡死场景靠此自愈，2026-11-24 用户定）。
	OnExitFailure("wb", failed, wbErr)
	waitCond(t, "idempotent rebind", func() bool { return len(rb.calls) == 3 })
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
	probeHook = func(_ context.Context, proxy, target string) error {
		if SameAlias(proxy, failed) {
			return errFake("socks connect: EOF")
		}
		return nil
	}
	OnExitFailure("acc", failed, wbErr)
	sleepMs(300)
	if len(empty.calls) != 0 {
		t.Fatalf("no-bound-credentials dead exit must not rebind, got %v", empty.calls)
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

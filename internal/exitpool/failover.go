package exitpool

// ============================================================================
// 出口差分自动故障转移（2026-11-24 用户定）
//
// 背景：elynode-FI 对 gstatic 通、对 workbuddy 域名 TLS 握手超时，被平台级
// 冷却反复循环（到期复用→再超时→再冷却）。差分信号可高置信度区分"出口对
// 该平台故障"与"出口整体死亡"：
//
//   ① 故障出口对测速目标（gstatic）实时探测**通**——隧道与通用链路活着；
//   ② 其他出口对**目标 host**（从触发错误提取）实时探测**至少 1 个通**——
//     目标没有整体故障，是该出口被目标选择性阻断；
//   ③ 触发源：该出口对该平台 TLS 类失败达成平台级冷却（IsUpstreamSide
//     StreamFailure 归类）。
//
// 三元组成立 → 处置（全部持久化）：
//   - 故障出口加入该平台的排除规则（ProxyExclusions[proxy] += kind）；
//   - 从池内挑替补（严格候选：未被该平台排除、未被该平台任何凭据绑定、
//     不在冷却；**从后往前扫，同地区优先**——别名末尾 -XX 提取地区）；
//   - 把该平台所有绑定故障出口的凭据改绑到替补；无候选放弃改绑（仅排除）。
//
// 开关：proxyAutoFailover（面板"自动排除"，默认关）。触发挂在平台级与全局
// 冷却分支（strike 门控天然限频），异步执行不阻塞请求。
// ============================================================================

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"afree-proxy/internal/kit"
)

const (
	// failoverProbeTimeout 单次实时探测超时（覆盖拨号+隧道+TLS+请求全程）。
	// 10s：慢速测速目标（如 apple 测试页经远端节点 4s+）在 5s 下会被误判
	// "出口死"（2026-11-24 容器实测），差分判定需要宽裕的余量。
	failoverProbeTimeout = 10 * time.Second
	// failoverWitnessMax 旁证探测最多试用的其他出口数（任一通即成立）。
	failoverWitnessMax = 3
	// failoverOverall 差分判定全程兜底超时（信号 A + 最多 3 次旁证 + 复测
	// 等待与复测）。
	failoverOverall = 5 * time.Minute
)

// failoverRecheckDelay 复测确认延迟（2026-11-26 用户定）：差分成立后等 3
// 分钟复测目标 host——WB 抖动期的"暂时性路径故障"与真风控在单次判定中不可
// 区分，只有时间能区分（11-25 两起连环误判的根源）。复测通了 = 抖动自愈，
// 只冷却不排除。var 便于测试白盒缩短。
var failoverRecheckDelay = 3 * time.Minute

// BindingEntry 绑定条目（带凭据标识与域）：后端定时重排需要按凭据域跳过
// cn 条目（2026-11-26 用户定），差分改绑只看槽位（Pairs）。
type BindingEntry struct {
	Key    string // 凭据标识（uid/key/accountID，仅日志与日志域判定用）
	Main   string
	Backup string
	// Realm 凭据域：cn / global / intl / 空（无域）。wb = auth.Realm()、
	// trae = uid 前缀 trae-intl-、other = 子平台 Realm 字段、cline/zen/freepi = 空。
	Realm string
	// Kind 条目所属的排除规则 kind（2026-11-26 修）：other 子平台条目 =
	// "other:<slug>"，其余 = 注册键。定时重排按 ruleKind 过滤——other 的
	// 适配器聚合全部子平台绑定，不过滤会跨子平台互相洗牌（11-26 实测
	// 1480 处改动的根因）。
	Kind string
}

// FailoverBindings 平台绑定适配器：读该平台全部绑定对 + 把 from 槽位改绑 to。
// 由装配根（app）按排除规则 kind 注册；未注册的平台只排除、不改绑。
// Entries（带凭据域）为可选能力：类型断言探测，未实现时由 pairsEntries 兜底
// 降级（Realm 全空 = 无域，定时重排按无域凭据处理）。
type FailoverBindings interface {
	// Pairs 该平台当前全部绑定对（只读快照）。
	Pairs() []Binding
	// Rebind 把 from（别名等价匹配，绑定行可能只保留旧端口/凭据）的槽位
	// 改绑为 to（池内当前地址），返回改绑槽数；写回各自配置文件。
	// 国内域凭据的槽位跳过不改（2026-11-26 用户定：自动排除不参与国内凭据）。
	Rebind(from, to string) int
	// RebindAll 与 Rebind 同形但不做域过滤——订阅换端口的绑定行跟随迁移
	//（2026-11-26 用户定）：国内凭据的绑定也要跟到新 URL。
	RebindAll(from, to string) int
}

// EntriesProvider 可选接口：提供带凭据域的绑定条目（定时重排用）。
type EntriesProvider interface {
	Entries() []BindingEntry
}

// MainRebinder 可选接口：只改 Main 槽的改绑（定时重排用——前端重排只动
// main、backup 原样保留，Rebind 会把 backup 一起改走，语义不同）。ruleKind
// 供聚合型适配器（other）过滤子平台；单平台适配器忽略。
type MainRebinder interface {
	RebindMain(ruleKind, from, to string) int
}

// rebindMain 适配器 Main 槽改绑：实现 MainRebinder 用之；未实现退回 Rebind
// （此时 backup 可能被连带改走——调用方日志提示）。
func rebindMain(b FailoverBindings, ruleKind, from, to string) (int, bool) {
	if m, ok := b.(MainRebinder); ok {
		return m.RebindMain(ruleKind, from, to), true
	}
	return b.Rebind(from, to), false
}

// bindingsEntries 适配器条目读取：实现了 EntriesProvider 用之，否则从 Pairs
// 降级（Realm 全空）。
func bindingsEntries(b FailoverBindings) []BindingEntry {
	if p, ok := b.(EntriesProvider); ok {
		return p.Entries()
	}
	var out []BindingEntry
	for _, pair := range b.Pairs() {
		out = append(out, BindingEntry{Main: pair.Main, Backup: pair.Backup})
	}
	return out
}

var (
	failoverMu       sync.Mutex
	failoverBindings = map[string]FailoverBindings{} // 排除规则 kind → 适配器
	// otherRuleKindsFn other 子平台 slug 枚举（返回 "other:<slug>" 列表）。
	// other 平台的冷却 kind 不带 slug，差分触发时按"other 整体故障"处理。
	otherRuleKindsFn func() []string
	// failoverRunning 防抖：同一 (冷却 kind, 出口) 判定进行中就跳过。
	failoverRunning = map[string]bool{}
	// failoverWriteMu 串行化差分路径对 binding 段的写（与面板 patch 的
	// 整体替换存在丢更新窗口，此锁只保证差分侧自身串行；窗口极小可接受）。
	failoverWriteMu sync.Mutex
)

// RegisterFailoverBindings 装配根注册平台适配器（键 = 排除规则 kind）。
func RegisterFailoverBindings(ruleKind string, b FailoverBindings) {
	failoverMu.Lock()
	failoverBindings[ruleKind] = b
	failoverMu.Unlock()
}

// RegisterOtherRuleKinds 注册 other 子平台枚举回调（未注册 = other 不触发）。
func RegisterOtherRuleKinds(fn func() []string) {
	failoverMu.Lock()
	otherRuleKindsFn = fn
	failoverMu.Unlock()
}

// failoverBindingsFor 排除规则 kind 的适配器：other:<slug> 粒度回落到
// "other" 注册键（适配器内部覆盖全部子平台）；未注册返回 nil。
func failoverBindingsFor(ruleKind string) FailoverBindings {
	failoverMu.Lock()
	defer failoverMu.Unlock()
	if b, ok := failoverBindings[ruleKind]; ok {
		return b
	}
	if strings.HasPrefix(ruleKind, "other:") {
		return failoverBindings["other"]
	}
	return nil
}

// ruleKindsFor 冷却 kind → 排除规则 kind 列表。other 展开为全部子平台；
// 无法映射的 kind 返回 false（保守不触发）。
func ruleKindsFor(cooldownKind string) ([]string, bool) {
	switch cooldownKind {
	case KindCline:
		return []string{"acc"}, true
	case KindZen:
		return []string{"key"}, true
	case KindWb:
		return []string{"wb"}, true
	case KindTrae:
		return []string{"trae"}, true
	case KindFreepi:
		return []string{"freepi"}, true
	case KindOther:
		failoverMu.Lock()
		fn := otherRuleKindsFn
		failoverMu.Unlock()
		if fn == nil {
			return nil, false
		}
		slugs := fn()
		if len(slugs) == 0 {
			return nil, false
		}
		return slugs, true
	}
	return nil, false
}

// AutoFailoverEnabled 自动排除开关（proxyAutoFailover，nil = 默认关）。
func AutoFailoverEnabled() bool {
	v := GetBinding().ProxyAutoFailover
	return v != nil && *v
}

// OnExitFailure 出口冷却（平台级或全局）触发时的差分判定入口：开关关、
// kind 无映射、出口不在池内直接返回；同一出口判定进行中跳过（防抖）。
// 命中则异步执行，不阻塞请求路径。
func OnExitFailure(cooldownKind, poolURL string, err error) {
	if err == nil || poolURL == "" || !AutoFailoverEnabled() {
		return
	}
	ruleKinds, ok := ruleKindsFor(cooldownKind)
	if !ok || len(ruleKinds) == 0 {
		return
	}
	if IdxInPool(poolURL) < 0 {
		return
	}
	key := cooldownKind + "\x00" + poolURL
	failoverMu.Lock()
	if failoverRunning[key] {
		failoverMu.Unlock()
		return
	}
	failoverRunning[key] = true
	failoverMu.Unlock()
	go func() {
		defer func() {
			failoverMu.Lock()
			delete(failoverRunning, key)
			failoverMu.Unlock()
		}()
		runFailover(cooldownKind, ruleKinds, poolURL, err)
	}()
}

// ============================================================================
// 差分判定与处置
// ============================================================================

// runFailover 差分判定主体（2026-11-24 用户定三档处置）：
//   - 故障出口对测速目标**不通** = 出口整体死（会自愈）→ 仅改绑、不排除；
//   - 出口活 + 旁证出口对出错 host ≥1 通 = 目标选择性风控 → 排除 + 改绑
//     （排除幂等：已在表内跳过写入；改绑幂等：绑定槽已改走则 0）；
//   - 出口活 + 旁证全挂 = 目标整体故障 → 不排除不改绑（防改绑风暴）。
func runFailover(cooldownKind string, ruleKinds []string, failedURL string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), failoverOverall)
	defer cancel()
	masked := kit.MaskProxyURL(failedURL)
	// ① 差分信号 A：故障出口对测速目标探测。不通 = 出口整体死。
	target := EffectiveTestTarget()
	if perr := probeURL(ctx, failedURL, target); perr != nil {
		log.Printf("[exit-failover] %s exit %s: exit dead (probe %s failed: %v) — rebind-only, not excluded",
			cooldownKind, masked, kit.MaskProxyURL(target), perr)
		failoverWriteMu.Lock()
		defer failoverWriteMu.Unlock()
		rebindDeadExit(ruleKinds, failedURL, cooldownKind, masked)
		return
	}
	// ② 差分信号 B：目标 host 从触发错误提取；其他出口至少 1 个通。
	targetURL := failedTargetOf(err)
	if targetURL == "" {
		log.Printf("[exit-failover] %s exit %s: skip (cannot extract target URL from error)", cooldownKind, masked)
		return
	}
	if !witnessExists(ctx, cooldownKind, failedURL, targetURL) {
		log.Printf("[exit-failover] %s exit %s: skip (target %s unreachable via all witness exits — target-wide outage, not exit ban)",
			cooldownKind, masked, targetURL)
		return
	}
	// ③ 复测确认（2026-11-26 用户定）：等 failoverRecheckDelay 后对故障出口
	// 复测目标 host。仍不通 = 持续性目标侧阻断（真风控）→ 排除；通了 = 抖动
	// 自愈 → 只冷却不排除，把"暂时性路径故障"洗掉。
	time.Sleep(failoverRecheckDelay)
	if ctx.Err() != nil {
		log.Printf("[exit-failover] %s exit %s: skip (overall deadline during recheck wait)", cooldownKind, masked)
		return
	}
	if perr := probeURL(ctx, failedURL, targetURL); perr == nil {
		log.Printf("[exit-failover] %s exit %s: skip (recheck %s reachable after %v — transient path failure, not exit ban)",
			cooldownKind, masked, targetURL, failoverRecheckDelay)
		return
	}
	// ④ 处置：排除（已在表内跳过写入）+ 改绑。
	failoverWriteMu.Lock()
	defer failoverWriteMu.Unlock()
	alreadyExcluded := false
	for _, rk := range ruleKinds {
		if proxyExcludedFor(rk, failedURL) {
			alreadyExcluded = true
			continue
		}
		addExclusion(rk, failedURL, targetURL)
	}
	repl, ok := pickReplacement(cooldownKind, failedURL, ruleKinds)
	if !ok {
		log.Printf("[exit-failover] %s exit %s excluded (kind=%s%s); no replacement candidate (unbound+unexcluded), rebinding skipped",
			cooldownKind, masked, strings.Join(ruleKinds, ","), alreadyExcludedTag(alreadyExcluded))
		return
	}
	n := 0
	for _, rk := range ruleKinds {
		if b := failoverBindingsFor(rk); b != nil {
			n += b.Rebind(failedURL, repl)
		}
	}
	log.Printf("[exit-failover] %s exit %s excluded (kind=%s%s); %d slot(s) rebound %s -> %s (region %s)",
		cooldownKind, masked, strings.Join(ruleKinds, ","), alreadyExcludedTag(alreadyExcluded), n, masked, kit.MaskProxyURL(repl), regionOf(failedURL))
}

// alreadyExcludedTag 日志后缀：已排除过的重复触发标注。
func alreadyExcludedTag(already bool) string {
	if already {
		return ", already excluded"
	}
	return ""
}

// rebindDeadExit 出口整体死时的改绑：把该平台绑定它的凭据全部改到替补，
// 不排除——排除表是"被目标风控"的长期语义，节点死会自愈，恢复后人工绑回。
// 无绑定槽/无候选仅日志。调用方持 failoverWriteMu。
func rebindDeadExit(ruleKinds []string, failedURL, cooldownKind, masked string) {
	repl, ok := pickReplacement(cooldownKind, failedURL, ruleKinds)
	if !ok {
		log.Printf("[exit-failover] %s exit %s: dead, no replacement candidate — rebind skipped", cooldownKind, masked)
		return
	}
	n := 0
	for _, rk := range ruleKinds {
		if b := failoverBindingsFor(rk); b != nil {
			n += b.Rebind(failedURL, repl)
		}
	}
	if n > 0 {
		log.Printf("[exit-failover] %s exit %s: dead (not excluded); %d slot(s) rebound -> %s",
			cooldownKind, masked, n, kit.MaskProxyURL(repl))
	} else {
		log.Printf("[exit-failover] %s exit %s: dead (not excluded); no bound credentials to rebind", cooldownKind, masked)
	}
}

// proxyExcludedFor 出口是否已被该排除规则 kind（或全局集）排除。键为备注名
// （RuleKeyOf，2026-11-26 用户定主键）；存量 URL 键按别名等价兜底。
func proxyExcludedFor(ruleKind, proxyURL string) bool {
	cfg := GetBinding()
	for p := range cfg.ProxyExclusionsAll {
		if SameAlias(cfg.ProxyExclusionsAll[p], proxyURL) {
			return true
		}
	}
	kinds, ok := cfg.ProxyExclusions[RuleKeyOf(proxyURL)]
	if !ok {
		// 存量 URL 键兼容：按别名等价再对一遍键。
		for p, ks := range cfg.ProxyExclusions {
			if SameAlias(p, proxyURL) {
				kinds, ok = ks, true
				break
			}
		}
	}
	if !ok {
		return false
	}
	for _, k := range kinds {
		if k == ruleKind {
			return true
		}
	}
	return false
}

// addExclusion 把出口追加进排除规则 kind（已存在不重复）并登记 TTL 复核
// 台账（只有差分写入的条目有台账——手动条目不复核、永不自动移除）。
// target 用于后续复核探活。键为备注名（RuleKeyOf）。调用方持 failoverWriteMu。
func addExclusion(ruleKind, proxyURL, target string) {
	cfg := GetBinding()
	cp := *cfg
	key := RuleKeyOf(proxyURL)
	exists := false
	next := make(map[string][]string, len(cfg.ProxyExclusions)+1)
	for p, ks := range cfg.ProxyExclusions {
		next[RuleKeyOf(p)] = append([]string(nil), ks...)
	}
	ks := next[key]
	for _, k := range ks {
		if k == ruleKind {
			exists = true
			break
		}
	}
	if !exists {
		next[key] = append(ks, ruleKind)
	}
	cp.ProxyExclusions = next
	stamps := CloneStamps(cfg.ProxyExclusionStamps)
	stamps[stampKey(key, ruleKind)] = stampValue(time.Now(), target, 0)
	cp.ProxyExclusionStamps = stamps
	SetBinding(&cp)
}

// removeExclusion 把出口从排除规则 kind 移除（kind 列表空则删条目）并清
// 台账。只处理目标条目——其他出口的同 kind 排除不受影响（2026-11-26 修：
// 初版遍历全表删同 kind，TTL 解禁一个出口会连带误删其他出口的排除）。
// 供 TTL 复核自动解禁。调用方持 failoverWriteMu。
func removeExclusion(ruleKind, proxyURL string) {
	cfg := GetBinding()
	cp := *cfg
	next := make(map[string][]string, len(cfg.ProxyExclusions))
	for p, ks := range cfg.ProxyExclusions {
		if p != proxyURL && !SameAlias(p, proxyURL) {
			next[p] = append([]string(nil), ks...)
			continue
		}
		kept := make([]string, 0, len(ks))
		for _, k := range ks {
			if k != ruleKind {
				kept = append(kept, k)
			}
		}
		if len(kept) > 0 {
			next[p] = kept
		}
	}
	cp.ProxyExclusions = next
	stamps := CloneStamps(cfg.ProxyExclusionStamps)
	delete(stamps, stampKey(proxyURL, ruleKind))
	cp.ProxyExclusionStamps = stamps
	SetBinding(&cp)
	log.Printf("[exit-failover-ttl] exit %s auto-unbanned (kind=%s) after recovery probes", kit.MaskProxyURL(proxyURL), ruleKind)
}

// stampKey / stampValue / CloneStamps TTL 复核台账的键值编解码。
// 值形态 "since|target|okCount"（RFC3339）。
func stampKey(proxyURL, ruleKind string) string { return proxyURL + "\x00" + ruleKind }

func stampValue(since time.Time, target string, okCount int) string {
	return since.Format(time.RFC3339) + "|" + target + "|" + strconv.Itoa(okCount)
}

func parseStamp(v string) (since time.Time, target string, okCount int) {
	parts := strings.SplitN(v, "|", 3)
	if len(parts) == 3 {
		if t, err := time.Parse(time.RFC3339, parts[0]); err == nil {
			since = t
		}
		target = parts[1]
		okCount, _ = strconv.Atoi(parts[2])
	}
	return since, target, okCount
}

func CloneStamps(stamps map[string]string) map[string]string {
	out := make(map[string]string, len(stamps)+1)
	for k, v := range stamps {
		out[k] = v
	}
	return out
}

// witnessExists 旁证：从池尾往前找最多 failoverWitnessMax 个健康其他出口，
// 对目标 host 实时探测，任一收到 HTTP 响应即成立。kind = 触发平台的冷却
// kind，平台级冷却的出口同样不当旁证（2026-11-25 补 R1，与下方注释口径一致）。
func witnessExists(ctx context.Context, kind, failedURL, targetURL string) bool {
	cfg := GetPool()
	tried := 0
	for i := len(cfg.Proxies) - 1; i >= 0 && tried < failoverWitnessMax; i-- {
		p := cfg.Proxies[i]
		if SameAlias(p, failedURL) {
			continue
		}
		if !ProxyAvailableFor(kind, i) {
			continue // 冷却/手动摘除的出口不适合当旁证
		}
		tried++
		if perr := probeURL(ctx, p, targetURL); perr == nil {
			return true
		}
	}
	return false
}

// pickReplacement 选替补：严格候选 = 池内、非故障口、域约束（2026-11-26
// 用户定：改绑对象中若有声明国际的凭据（Realm=intl/global 绑在故障口），
// 替补限定不限出口；声明国内的凭据槽位由适配器 Rebind 跳过、根本不进改绑，
// 故无需"cn 故障口只挑 cn 候选"——剩余槽位的无域凭据用任意出口均符合
// zone.go 矩阵）、未被规则 kind 排除、
// 未被该平台任何凭据绑定（别名等价）、不在冷却（全局+平台级两层，2026-11-25
// 补 R1——空 kind 只看全局层，会漏掉仅对该平台冷却的出口）。从后往前扫，
// 同地区立即选中；无同地区取从后往前第一个候选。找不到返回 ("", false)。
func pickReplacement(kind, failedURL string, ruleKinds []string) (string, bool) {
	cfg := GetPool()
	reg := regionOf(failedURL)
	// 改绑对象的域约束（2026-11-26 用户定语义）：国内域凭据的槽位由适配器
	// Rebind 跳过（不参与自动排除）；剩余被改绑槽位中若有声明国际的凭据，
	// 替补必须是不限出口（国际凭据不分配国内出口）；纯无域凭据无约束
	//（未声明平台把国内出口当普通出口）。
	intlBound := false
	for _, rk := range ruleKinds {
		if b := failoverBindingsFor(rk); b == nil {
			continue
		}
		for _, e := range bindingsEntries(failoverBindingsFor(rk)) {
			if e.Realm == "cn" {
				continue
			}
			if SameAlias(e.Main, failedURL) || (e.Backup != "" && SameAlias(e.Backup, failedURL)) {
				if e.Realm == "global" || e.Realm == "intl" {
					intlBound = true
				}
			}
		}
	}
	// 该平台全部绑定槽位集合（别名等价匹配）。
	bound := map[string]bool{}
	for _, rk := range ruleKinds {
		if b := failoverBindingsFor(rk); b != nil {
			for _, pair := range b.Pairs() {
				for _, slot := range []string{pair.Main, pair.Backup} {
					if slot != "" {
						bound[slot] = true
					}
				}
			}
		}
	}
	var fallback string
	for i := len(cfg.Proxies) - 1; i >= 0; i-- {
		p := cfg.Proxies[i]
		if SameAlias(p, failedURL) {
			continue
		}
		// 声明国际凭据在场：替补限定不限出口。
		if intlBound && ZoneOf(p) != "" {
			continue
		}
		excluded := false
		for _, rk := range ruleKinds {
			if proxyExcludedFor(rk, p) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		if !ProxyAvailableFor(kind, i) {
			continue
		}
		if boundMatch(bound, p) {
			continue
		}
		if reg != "" && regionOf(p) == reg {
			return p, true
		}
		if fallback == "" {
			fallback = p
		}
	}
	return fallback, fallback != ""
}

// boundMatch 候选是否命中绑定槽位集合（别名等价）。
func boundMatch(bound map[string]bool, candidate string) bool {
	for slot := range bound {
		if SameAlias(slot, candidate) {
			return true
		}
	}
	return false
}

// regionOf 地区代码：别名末尾 "-XX"（横杠后恰好 2 个字母），大写返回；
// 无横杠或非 2 字母 = 无地区（空串）。
func regionOf(proxyURL string) string {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return ""
	}
	alias := strings.TrimSpace(u.Fragment)
	i := strings.LastIndex(alias, "-")
	// 横杠后恰好 2 个字符：len - (i+1) == 2。
	if i < 0 || len(alias)-(i+1) != 2 {
		return ""
	}
	s := alias[i+1:]
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return ""
		}
	}
	return strings.ToUpper(s)
}

// ============================================================================
// 实时探测与错误解析
// ============================================================================

// probeHook 测试注入点（白盒）：非 nil 时替代真实 probeURL，验证差分判定
// 全链路不需要真实网络。生产路径恒为 nil。
//
// 用 atomic.Pointer 承载：OnExitFailure 异步起 goroutine 执行 runFailover，
// 测试主线程会在各个场景间替换 hook，而上一场景的 goroutine 可能尚未退出
// （failoverRunning 只防同 key 重入，不保证跨场景串行）。普通函数变量读写
// 会触发 -race 的 DATA RACE（2026-11-24 CI 实测）。生产路径 Load 到 nil 即
// 走真实探测，无额外开销。
type probeFunc func(ctx context.Context, proxyURL, target string) error

var probeHook atomic.Pointer[probeFunc]

// setProbeHook 原子替换探测钩子；nil = 恢复真实探测。仅测试使用。
func setProbeHook(f probeFunc) {
	if f == nil {
		probeHook.Store(nil)
		return
	}
	probeHook.Store(&f)
}

// probeURL 经指定出口对目标发一次 GET（完整握手：拨号+隧道+TLS+请求），
// 收到任何 HTTP 响应（含 401/404/5xx）都算通——隧道与 TLS 已走通即有效，
// 与面板延迟测试 probeOnce 同口径。纯探测：不记统计、不触发冷却。
func probeURL(ctx context.Context, proxyURL, target string) error {
	if f := probeHook.Load(); f != nil {
		return (*f)(ctx, proxyURL, target)
	}
	transport := BuildTransport(func(c context.Context, network, addr string) (net.Conn, error) {
		return DialViaProxy(c, proxyURL, network, addr)
	})
	transport.DisableKeepAlives = true
	transport.ResponseHeaderTimeout = failoverProbeTimeout
	transport.TLSHandshakeTimeout = failoverProbeTimeout
	client := &http.Client{Transport: transport, Timeout: failoverProbeTimeout}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// failedTargetOf 从触发错误提取目标 base URL（scheme://host/）。错误形如
// `Get "https://www.workbuddy.ai/v3/config": net/http: TLS handshake timeout`
// ——URL 在第一对引号内。旁证探测用同 host 根路径：TLS 黑洞与 path 无关，
// 且收到任何状态码都算通。
func failedTargetOf(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	q1 := strings.IndexByte(msg, '"')
	if q1 < 0 {
		return ""
	}
	rest := msg[q1+1:]
	q2 := strings.IndexByte(rest, '"')
	if q2 < 0 {
		return ""
	}
	u, perr := url.Parse(rest[:q2])
	if perr != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/"
}

// ============================================================================
// 排除条目 TTL 复核（2026-11-26 用户定）：差分排除的条目每半小时探活一次，
// 恢复可达 1 次即自动移除该 kind——解决"判定当时正确、上游解封后条目永久
// 残留"（11 个 WB 已恢复条目的来源）。手动条目无台账，永不自动移除。
// ============================================================================

const (
	// failoverReviewInterval 复核周期（2026-11-26 用户定：每小时 → 每半小时）。
	failoverReviewInterval = 30 * time.Minute
	// failoverReviewOK 探活通过次数阈值：达到即自动解禁（2026-11-26 用户定：
	// 2 次 → 1 次——探通一次即说明上游已解禁，无需二次确认）。
	failoverReviewOK = 1
)

// StartFailoverReviewLoop 启动 TTL 复核循环（装配根调用一次；差分开关关闭
// 时循环空转——条目只增不减，开销为零）。
func StartFailoverReviewLoop() {
	go func() {
		for {
			time.Sleep(failoverReviewInterval)
			reviewExclusions()
		}
	}()
}

// reviewExclusions 一轮复核：逐条探活台账条目，恢复达标自动解禁。
func reviewExclusions() {
	if !AutoFailoverEnabled() {
		return
	}
	cfg := GetBinding()
	type item struct {
		proxyURL, kind, target string
		okCount                int
	}
	var items []item
	for key, v := range cfg.ProxyExclusionStamps {
		proxyURL, kind, ok := strings.Cut(key, "\x00")
		if !ok || proxyURL == "" || kind == "" {
			continue
		}
		since, target, okCount := parseStamp(v)
		if target == "" {
			// 旧格式/坏数据：按测速目标复核兜底。
			target = EffectiveTestTarget()
			_ = since
		}
		items = append(items, item{proxyURL, kind, target, okCount})
	}
	for _, it := range items {
		idx := IdxInPool(it.proxyURL)
		if idx < 0 {
			// 出口已不在池：条目失去意义，台账一并清（下一次 SetBinding 落盘）。
			failoverWriteMu.Lock()
			removeExclusion(it.kind, it.proxyURL)
			failoverWriteMu.Unlock()
			continue
		}
		// 键为备注名（2026-11-26）：探活前解析回池内当前 URL（端口随订阅更新）。
		probeRaw := GetPool().Proxies[idx]
		ctx, cancel := context.WithTimeout(context.Background(), failoverProbeTimeout)
		perr := probeURL(ctx, probeRaw, it.target)
		cancel()
		var okCount int
		if perr == nil {
			okCount = it.okCount + 1
		}
		failoverWriteMu.Lock()
		cfg := GetBinding()
		if okCount >= failoverReviewOK {
			removeExclusion(it.kind, it.proxyURL)
		} else {
			// 未达标：刷新台账（恢复计数或清零），排除条目保持。
			cp := *cfg
			stamps := CloneStamps(cfg.ProxyExclusionStamps)
			stamps[stampKey(it.proxyURL, it.kind)] = stampValue(time.Now(), it.target, okCount)
			cp.ProxyExclusionStamps = stamps
			SetBinding(&cp)
		}
		failoverWriteMu.Unlock()
		if perr == nil && okCount < failoverReviewOK {
			log.Printf("[exit-failover-ttl] exit %s recovered (probe %s ok, %d/%d) — keeping exclusion until threshold",
				kit.MaskProxyURL(it.proxyURL), it.target, okCount, failoverReviewOK)
		}
	}
}

// RebindAllEverywhere 全部已注册绑定适配器执行别名改写（订阅换端口的绑定行
// 跟随迁移，2026-11-26 用户定）：不做域过滤——国内凭据的绑定也要跟到新 URL
// （与差分 Rebind 跳过国内槽位是两回事）。
func RebindAllEverywhere(from, to string) {
	failoverMu.Lock()
	defer failoverMu.Unlock()
	for _, b := range failoverBindings {
		b.RebindAll(from, to)
	}
}

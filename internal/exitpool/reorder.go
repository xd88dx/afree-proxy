package exitpool

// ============================================================================
// 延迟重排后端化（2026-11-26 用户定：每天 cron 定时自动重排）。
//
// 算法与前端 applyReorderRules（html.go）同语义——冻结-供给-修复回填：
//   冻结段：绑定健康（快代理、未拉黑、未超上限）与 cn 域条目（2026-11-07/26
//     用户定：cn 凭据绑定不参与自动分配）原样不动；
//   修复段：慢 / 无记录 / 被拉黑 / 超上限 / 手动冷却的条目，从剩余供给按
//     快→无记录→慢 取最优；非强制行只在候选严格优于现状时才换（重排 diff 最小）。
//
// 与前端的差异（后端形态所然）：只动 Main 槽（MainRebinder），backup 原样；
// 凭据域从 BindingEntry.Realm 取（wb=auth.Realm、trae=uid 前缀、other=子平台
// Realm 字段、其余无域）；zone 兼容过滤复用 ZoneCompatible。
// ============================================================================

import (
	"log"
	"sort"

	"afree-proxy/internal/kit"
)

// reorderEntry 供给/修复段的单条目视图。
type reorderEntry struct {
	idx    int    // entries 下标（行序）
	url    string // 当前 Main（"" = 直连）
	realm  string // 凭据域
	forced bool   // 强制挪（被拉黑/手动冷却/超上限/直连被排除）
}

// reorderCapFor 每代理绑定上限（0 = 不限）。
func reorderCapFor(ruleKind string) int {
	v := GetBinding().ProxyBindCaps[ruleKind]
	if v < 0 {
		return 0
	}
	return v
}

// reorderHS 健康统计的平均握手 ms（无记录返回 0）。查询键 = 备注名
// （RuleKeyOf，与 RecordProxyOutcome 的写入键同口径——2026-11-26 备注名主键
// 后统计表里没有完整 URL 键，直接查 URL 会恒 miss、重排退化成池序）。
func reorderHS(proxyURL string) int64 {
	EnsureHealthLoaded()
	healthMu.Lock()
	defer healthMu.Unlock()
	e := healthStats[RuleKeyOf(proxyURL)]
	if e == nil || e.HSCount == 0 {
		return 0
	}
	return e.HSSumMs / e.HSCount
}

// reorderKeepMs 阈值（秒→毫秒；缺省回落 DefaultReorderKeepSec）。
func reorderKeepMs() int64 {
	sec := GetBinding().ReorderKeepSec
	if sec <= 0 {
		sec = DefaultReorderKeepSec
	}
	return int64(sec * 1000)
}

// RunReorderForKind 对单个平台规则 kind 执行一轮冻结-供给-修复重排。
// ruleKind 为排除规则口径（acc/trae/key/wb/freepi/other:<slug>）。
// 返回改动条目数。仅动 Main 槽；cn 域条目与直连条目冻结。
func RunReorderForKind(ruleKind string) int {
	b := failoverBindingsFor(ruleKind)
	if b == nil {
		return 0
	}
	entries := bindingsEntries(b)
	if len(entries) == 0 {
		return 0
	}
	cfg := GetPool()
	capN := reorderCapFor(ruleKind)
	keepMs := reorderKeepMs()
	directExcluded := proxyExcludedFor(ruleKind, ProxyRuleDirectKey)

	// --- 冻结判定与占用统计 ---
	var repair []reorderEntry
	cnt := map[string]int{} // 每出口占用（冻结 + 保留）
	cnOccupied := map[string]bool{}
	seen := map[string]int{}
	for i, e := range entries {
		// 规则 kind 过滤（2026-11-26 修）：other 适配器聚合全部子平台绑定，
		// 不过滤会把别的子平台绑定按当前子平台规则洗牌（跨子平台污染）。
		if e.Kind != "" && e.Kind != ruleKind {
			continue
		}
		eurl := e.Main
		if e.Realm == "cn" {
			// cn 域条目冻结（2026-11-07/26 用户定）：绑定原样保留，其出口
			// 不进供给（专属出口不得分给其他域凭据）。直连主槽不占代理位。
			if eurl != "" && !IsDirect(eurl) {
				cnt[eurl]++
				cnOccupied[eurl] = true
			}
			continue
		}
		if eurl == "" {
			continue // 全局主槽行（未绑定主出口）不参与重排
		}
		if IsDirect(eurl) {
			// 显式直连行（Main = direct 哨兵）：直连未被该平台排除时照旧
			// 不参与（保持既有口径）；被排除（__direct__ 键在排除表）时强制
			// 挪到可用代理。2026-11-26 修：此前用 eurl=="" 判直连（错把全局
			// 行当直连、漏掉显式直连行），且改绑传 "direct" 给适配器的别名
			// 匹配恒不中——排除直连的行每次 cron 都静默 no-op。
			if !directExcluded {
				continue
			}
			repair = append(repair, reorderEntry{i, eurl, e.Realm, true})
			continue
		}
		seen[eurl]++
		forced := proxyExcludedFor(ruleKind, eurl) || manualCooledAtURL(eurl) ||
			(capN > 0 && seen[eurl] > capN)
		hs := reorderHS(eurl)
		bad := forced || hs > keepMs || hs <= 0
		if bad {
			repair = append(repair, reorderEntry{i, eurl, e.Realm, forced})
			continue
		}
		// 冻结（健康）条目：占用计数。
		cnt[eurl]++
	}

	// --- 供给偏好序：快（延迟升序）→ 无记录（池序）→ 慢（延迟升序）---
	type supply struct {
		url  string
		tier int // 0=快 1=无记录 2=慢
		hs   int64
	}
	var pref []supply
	inPool := map[string]bool{}
	for _, p := range cfg.Proxies {
		inPool[p] = true
	}
	for _, p := range cfg.Proxies {
		if !inPool[p] || p == "" {
			continue
		}
		if proxyExcludedFor(ruleKind, p) || manualCooledAtURL(p) || cnOccupied[p] {
			continue
		}
		hs := reorderHS(p)
		tier := 1
		if hs > 0 {
			tier = map[bool]int{true: 0, false: 2}[hs <= keepMs]
		}
		pref = append(pref, supply{p, tier, hs})
	}
	tierRank := map[int]int{0: 0, 1: 1, 2: 2}
	sort.SliceStable(pref, func(i, j int) bool {
		if tierRank[pref[i].tier] != tierRank[pref[j].tier] {
			return tierRank[pref[i].tier] < tierRank[pref[j].tier]
		}
		if pref[i].tier == 1 {
			return poolIdx(cfg, pref[i].url) < poolIdx(cfg, pref[j].url)
		}
		return pref[i].hs < pref[j].hs
	})

	// --- 修复段：forced 优先（先抢候选，避免被非 forced 行占走——与前端
	// repair.sort 的 forced 置顶同口径），其余按行序 ---
	sort.SliceStable(repair, func(i, j int) bool {
		if repair[i].forced != repair[j].forced {
			return repair[i].forced
		}
		return repair[i].idx < repair[j].idx
	})

	changed := 0
	for _, r := range repair {
		forced := r.forced
		best := ""
		for _, s := range pref {
			if s.url == r.url {
				continue
			}
			if !ZoneCompatible(ZoneOf(s.url), r.realm) {
				continue
			}
			left := capN - cnt[s.url]
			if capN > 0 && left <= 0 {
				continue
			}
			best = s.url
			break
		}
		if best == "" {
			if r.url != "" {
				cnt[r.url]++ // 无候选：原样保留（保留也占容量）
			}
			continue
		}
		if !forced && tierRank[tierOfHS(reorderHS(best))] >= tierRank[tierOfHS(reorderHS(r.url))] {
			// 非强制行：候选不严格更优（同档或更差）→ 原样保留（与前端
			// applyReorderRules 的 >= 口径一致，重排 diff 最小）。
			if r.url != "" {
				cnt[r.url]++
			}
			continue
		}
		n, exact := rebindMain(b, ruleKind, r.url, best)
		if n > 0 {
			changed += n
			cnt[best] += n
			if !exact {
				log.Printf("[proxy-reorder] %s: adapter lacks RebindMain, backup slots may have moved with %s -> %s",
					ruleKind, kit.MaskProxyURL(r.url), kit.MaskProxyURL(best))
			}
		}
	}
	if changed > 0 {
		log.Printf("[proxy-reorder] %s: %d binding(s) reordered (keepMs=%d cap=%d)", ruleKind, changed, keepMs, capN)
	}
	return changed
}

func tierOfHS(hs int64) int {
	if hs <= 0 {
		return 1
	}
	if hs <= reorderKeepMs() {
		return 0
	}
	return 2
}

func poolIdx(cfg *PoolConfig, url string) int {
	for i, p := range cfg.Proxies {
		if p == url {
			return i
		}
	}
	return len(cfg.Proxies)
}

// manualCooledAtURL 出口是否处于手动冷却（无到期，仅解禁恢复）。
func manualCooledAtURL(proxyURL string) bool {
	idx := IdxInPool(proxyURL)
	if idx < 0 {
		return false
	}
	return proxyManualCooledAt(idx)
}

// RunReorderAll 全平台重排一轮：基础平台 + 逐 other 子平台（cap/排除按
// other:<slug> 粒度）。返回改动条目总数。
func RunReorderAll() int {
	total := 0
	for _, kind := range []string{"acc", "trae", "key", "wb", "freepi"} {
		total += RunReorderForKind(kind)
	}
	failoverMu.Lock()
	fn := otherRuleKindsFn
	failoverMu.Unlock()
	if fn != nil {
		for _, slug := range fn() {
			total += RunReorderForKind(slug)
		}
	}
	return total
}

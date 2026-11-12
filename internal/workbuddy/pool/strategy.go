package pool

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	WeightedRandom    = "weighted_random"
	RoundRobin        = "round_robin"
	SequentialExhaust = "sequential_exhaust"
)

// RegionStrategies 使用配置层的地区名；路由层仍使用现有 cn/global realm。
type RegionStrategies struct {
	Domestic      string `json:"domestic"`
	International string `json:"international"`
}

func (s RegionStrategies) Normalize() (RegionStrategies, error) {
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"domestic", &s.Domestic}, {"international", &s.International},
	} {
		*field.value = strings.ToLower(strings.TrimSpace(*field.value))
		if *field.value == "" {
			*field.value = WeightedRandom
		}
		switch *field.value {
		case WeightedRandom, RoundRobin, SequentialExhaust:
		default:
			return s, fmt.Errorf("%s: %q 不是合法策略（weighted_random / round_robin / sequential_exhaust）", field.name, *field.value)
		}
	}
	return s, nil
}

// RegionAccountOrder 指定 UID 优先顺序；未列出的账号按 UID 排序追加。
type RegionAccountOrder struct {
	Domestic      []string `json:"domestic,omitempty"`
	International []string `json:"international,omitempty"`
}

type scheduleCursor struct {
	Strategy string `json:"strategy"`
	UID      string `json:"uid"`
}

// SetScheduling 原子热更新两域策略。状态带策略标签，切换算法后忽略旧指针。
func (p *Pool) SetScheduling(s RegionStrategies, order RegionAccountOrder) error {
	s, err := s.Normalize()
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.strategies = s
	p.accountOrder = RegionAccountOrder{
		Domestic:      append([]string(nil), order.Domestic...),
		International: append([]string(nil), order.International...),
	}
	for key, cursor := range p.scheduling {
		realm, _, _ := strings.Cut(key, "\x1f")
		if cursor.Strategy != p.strategyForRealmLocked(realm) {
			delete(p.scheduling, key)
			p.dirty.Store(true)
		}
	}
	return nil
}

func (p *Pool) strategyForRealmLocked(realm string) string {
	s := ""
	switch realm {
	case "cn":
		s = p.strategies.Domestic
	case "global":
		s = p.strategies.International
	}
	if s == "" {
		return WeightedRandom
	} // 无 realm 的旧入口不混用地区策略。
	return s
}

// StrategyForRealm 供会话入口决定新会话是否应交由账号池分配。
func (p *Pool) StrategyForRealm(realm string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.strategyForRealmLocked(realm)
}

func (p *Pool) restoreSchedulingLocked(cursors map[string]scheduleCursor) {
	p.scheduling = make(map[string]scheduleCursor, len(cursors))
	for key, cursor := range cursors {
		realm, _, ok := strings.Cut(key, "\x1f")
		if ok && (realm == "cn" || realm == "global") &&
			(cursor.Strategy == RoundRobin || cursor.Strategy == SequentialExhaust) {
			p.scheduling[key] = cursor
		}
	}
}

// pickOrderedLocked 接收已完成健康、保底、在途与成本层过滤的全量候选。
// 临时 tried/在途占满/成本探索可借用其他账号，但不推进顺序耗尽的主账号。
// 调用方持 p.mu；指针按 (realm, model) 隔离，并通过既有快照机制持久化。
func (p *Pool) pickOrderedLocked(cands []*entry, tried map[string]bool, model, realm, strategy string, explored bool, now time.Time) *entry {
	order := p.accountOrder.Domestic
	if realm == "global" {
		order = p.accountOrder.International
	}
	rank := make(map[string]int, len(order))
	for _, uid := range order {
		if _, exists := rank[uid]; !exists {
			rank[uid] = len(rank)
		}
	}
	lessUID := func(a, b string) bool {
		ar, aok := rank[a]
		br, bok := rank[b]
		if aok != bok {
			return aok
		}
		if aok {
			return ar < br
		}
		return a < b
	}
	sort.Slice(cands, func(i, j int) bool { return lessUID(cands[i].a.UID, cands[j].a.UID) })
	key := realm + "\x1f" + model
	cursor := p.scheduling[key]
	if cursor.Strategy != strategy {
		cursor = scheduleCursor{Strategy: strategy}
	}
	var selected *entry
	if strategy == SequentialExhaust {
		for _, e := range cands {
			if e.a.UID == cursor.UID {
				selected = e
				break
			}
		}
	}
	if selected == nil {
		selected = cands[0]
		if cursor.UID != "" {
			for _, e := range cands {
				if lessUID(cursor.UID, e.a.UID) {
					selected = e
					break
				}
			}
		}
	}
	advance := true
	if strategy == SequentialExhaust && cursor.UID != "" {
		current := p.byUID[cursor.UID]
		if current != nil && current.a.Realm() == realm && current.healthyForModel(now, model) &&
			!p.floorBlockedForRealmModel(current, model, realm, now) &&
			(explored || tried[cursor.UID] || p.inFlightFull(current)) {
			advance = false
		}
	}
	if advance && cursor.UID != selected.a.UID {
		p.scheduling[key] = scheduleCursor{Strategy: strategy, UID: selected.a.UID}
		p.dirty.Store(true)
	}
	return selected
}

// ============================================================================
// 全局选号策略（上游 18a6776 移植）：与 linguo 线的地区策略并存。两套共用
// scheduling map——key 同形 (realm+"\x1f"+model)，Strategy 标签作命名空间
// （地区策略值带下划线，全局策略值不带），切策略时旧游标自动失效。
// ============================================================================

// 选号策略常量（config pool.selection_strategy 的取值）。
const (
	// StrategyWeighted 加权随机（缺省，既有行为）。
	StrategyWeighted = "weighted"
	// StrategyRoundRobin 轮询：每次请求换下一个号（按 UID 排序后的环）。
	StrategyRoundRobin = "round_robin"
	// StrategySequentialExhaust 顺序耗尽：当前号一直用到不可用才换下一个。
	StrategySequentialExhaust = "sequential"
)

// NormalizeStrategy 归一化策略名（空/未知 → weighted）。
func NormalizeStrategy(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case StrategyRoundRobin, "rr", "round-robin", "roundrobin":
		return StrategyRoundRobin
	case StrategySequentialExhaust, "sequential_exhaust", "seq", "exhaust", "sequentialexhaust":
		return StrategySequentialExhaust
	default:
		return StrategyWeighted
	}
}

// SetStrategy 设置选号策略（热生效）。未知值回落 weighted。
func (p *Pool) SetStrategy(raw string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.strategy = NormalizeStrategy(raw)
}

// Strategy 当前生效策略（面板展示用）。
func (p *Pool) Strategy() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.strategy == "" {
		return StrategyWeighted
	}
	return p.strategy
}

// scheduleKey 游标分桶键：按 realm + 模型。
func scheduleKey(realm, model string) string {
	return realm + "\x1f" + model
}

// strategyOrder 给策略用的确定性顺序：按 UID 升序（权重不稳定，不能作环）。
func strategyOrder(cands []*entry) []*entry {
	out := make([]*entry, len(cands))
	copy(out, cands)
	sort.SliceStable(out, func(i, j int) bool { return out[i].a.UID < out[j].a.UID })
	return out
}

// pickRoundRobinLocked 轮询：取环上游标的下一个。
func (p *Pool) pickRoundRobinLocked(order []*entry, key string) *entry {
	if len(order) == 0 {
		return nil
	}
	cur := p.scheduling[key]
	if cur.Strategy != StrategyRoundRobin {
		cur = scheduleCursor{Strategy: StrategyRoundRobin}
	}
	next := 0
	if cur.UID != "" {
		for i, e := range order {
			if e.a.UID == cur.UID {
				next = (i + 1) % len(order)
				break
			}
		}
	}
	selected := order[next]
	p.scheduling[key] = scheduleCursor{Strategy: StrategyRoundRobin, UID: selected.a.UID}
	p.dirty.Store(true)
	return selected
}

// pickSequentialLocked 顺序耗尽：当前号还能用就一直用它，否则换排序上的下一个。
func (p *Pool) pickSequentialLocked(order []*entry, key string) *entry {
	if len(order) == 0 {
		return nil
	}
	cur := p.scheduling[key]
	if cur.Strategy != StrategySequentialExhaust {
		cur = scheduleCursor{Strategy: StrategySequentialExhaust}
	}
	for _, e := range order {
		if cur.UID != "" && e.a.UID == cur.UID {
			p.scheduling[key] = scheduleCursor{Strategy: StrategySequentialExhaust, UID: cur.UID}
			p.dirty.Store(true)
			return e
		}
	}
	selected := order[0]
	for _, e := range order {
		if e.a.UID > cur.UID {
			selected = e
			break
		}
	}
	p.scheduling[key] = scheduleCursor{Strategy: StrategySequentialExhaust, UID: selected.a.UID}
	p.dirty.Store(true)
	return selected
}

// pickByStrategyLocked 按当前策略在候选里选一个；weighted 返回 nil（走加权随机）。
func (p *Pool) pickByStrategyLocked(cands []*entry, reqModel, realm string, now time.Time) *entry {
	if p.strategy == "" || p.strategy == StrategyWeighted {
		return nil
	}
	order := strategyOrder(cands)
	key := scheduleKey(realm, reqModel)
	var selected *entry
	switch p.strategy {
	case StrategyRoundRobin:
		selected = p.pickRoundRobinLocked(order, key)
	case StrategySequentialExhaust:
		selected = p.pickSequentialLocked(order, key)
	default:
		return nil
	}
	if selected == nil {
		return nil
	}
	// 防并发撞号：选中号刚被别的 goroutine 用掉时，在剩余候选里按同一策略再取一次。
	if selected.lastUsed.IsZero() || now.Sub(selected.lastUsed) >= minPickGap {
		return selected
	}
	for _, e := range order {
		if e == selected {
			continue
		}
		if e.lastUsed.IsZero() || now.Sub(e.lastUsed) >= minPickGap {
			return e
		}
	}
	return selected
}

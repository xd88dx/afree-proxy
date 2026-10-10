package amd

import (
	"strings"
	"sync"
	"time"
)

// ============ 多 key 轮转 ============
//
// 语义与 openrouter.PickKey 同构：round-robin 选未冷却且路由启用的 key；
// 绑定出口全部不可用的 key（调用方注入 boundRoutable）不参与候选；全部
// 不可用时返回 ""，调用方报"无可用 key"而不是静默直连。

var keyMu sync.Mutex
var keyIdx int

// PickKey 选取下一个可用 key。strategy 取自网关账号调度策略；boundRoutable
// 是调用方注入的"该 key 绑定出口是否可用"判定（隔离模式；nil = 全部可路由）。
func PickKey(strategy string, boundRoutable func(key string) bool) string {
	c := Get()
	keys := c.Keys
	if len(keys) == 0 {
		return ""
	}
	blocked := map[string]bool{}
	if boundRoutable != nil {
		for _, k := range keys {
			if !boundRoutable(k) {
				blocked[k] = true
			}
		}
	}
	routingOff := map[string]bool{}
	for _, k := range keys {
		if !c.RoutingEnabled(k) {
			routingOff[k] = true
		}
	}
	if len(blocked) == len(keys) || len(routingOff) == len(keys) {
		return ""
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	now := time.Now()
	for k, until := range c.Cool {
		if now.After(until) {
			delete(c.Cool, k)
		}
	}
	buildCandidates := func(ignoreCooling bool) []string {
		var out []string
		for i := 0; i < len(keys); i++ {
			k := keys[(keyIdx+i)%len(keys)]
			if blocked[k] || routingOff[k] {
				continue
			}
			if !ignoreCooling {
				if _, cooling := c.Cool[k]; cooling {
					continue
				}
			}
			out = append(out, k)
		}
		return out
	}
	candidates := buildCandidates(false)
	if len(candidates) == 0 {
		candidates = buildCandidates(true) // 全冷却时兜底（保持请求流动）
	}
	if len(candidates) == 0 {
		return ""
	}
	var picked string
	switch strategy {
	case "fill":
		set := map[string]bool{}
		for _, k := range candidates {
			set[k] = true
		}
		for _, k := range keys {
			if set[k] {
				picked = k
				break
			}
		}
	case "random":
		picked = candidates[randIntn(len(candidates))]
	default:
		// round_robin：轮转头的候选，游标推进到它之后
		picked = candidates[0]
		for i, k := range keys {
			if k == picked {
				keyIdx = (i + 1) % len(keys)
				break
			}
		}
	}
	return picked
}

// KeyIndex key 在池中的序号（1-based 展示用）；不存在返回 0。
func KeyIndex(key string) int {
	for i, k := range Get().Keys {
		if k == key {
			return i + 1
		}
	}
	return 0
}

// MarkSuccess 累计 key 的成功调用计数（内存态，面板展示）。
func MarkSuccess(key string) {
	if key == "" {
		return
	}
	c := Get()
	mu.Lock()
	if c.Usage == nil {
		c.Usage = map[string]int64{}
	}
	c.Usage[key]++
	mu.Unlock()
}

// Cooldown 把 key 置为冷却，冷却期内轮转跳过。返回实际生效的时长
// （0 = 未冷却：空 key）。上游并发上限 8/key，429 时按 Retry-After 冷却。
func Cooldown(key string, d time.Duration) time.Duration {
	if key == "" {
		return 0
	}
	if d <= 0 {
		d = time.Minute
	}
	if d > 24*time.Hour {
		d = 24 * time.Hour
	}
	c := Get()
	mu.Lock()
	if c.Cool == nil {
		c.Cool = map[string]time.Time{}
	}
	c.Cool[key] = time.Now().Add(d)
	mu.Unlock()
	return d
}

// Uncool 清除 key 的冷却（探测成功后调用：真实 2xx 是最强证据）。
func Uncool(key string) {
	if key == "" {
		return
	}
	mu.Lock()
	delete(Get().Cool, key)
	mu.Unlock()
}

// CooldownUntil 只读查询冷却截止时刻。
func CooldownUntil(key string) (time.Time, bool) {
	until, ok := Get().Cool[key]
	return until, ok
}

// RoutingEnabled key 是否参与路由。
func RoutingEnabled(key string) bool { return Get().RoutingEnabled(key) }

// Keys 返回 key 池副本。
func Keys() []string {
	c := Get()
	out := make([]string, len(c.Keys))
	copy(out, c.Keys)
	return out
}

// randIntn xorshift 轻量随机（与 openrouter 同实现）。
var randMu sync.Mutex
var randState uint64 = uint64(time.Now().UnixNano())

func randIntn(n int) int {
	if n <= 1 {
		return 0
	}
	randMu.Lock()
	randState ^= randState << 13
	randState ^= randState >> 7
	randState ^= randState << 17
	v := randState
	randMu.Unlock()
	return int(v % uint64(n))
}

// ============ 入口判定 ============

// StripPrefix 去掉 "amd:" 前缀，返回裸模型 ID；无前缀时返回原串与 false。
func StripPrefix(model string) (id string, ok bool) {
	m := strings.TrimSpace(model)
	if strings.HasPrefix(m, ModelPrefix) {
		return strings.TrimSpace(m[len(ModelPrefix):]), true
	}
	return m, false
}

// HasPrefix 判断模型 ID 是否带本平台前缀。
func HasPrefix(model string) bool {
	_, ok := StripPrefix(model)
	return ok
}

package freepi

import (
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 多 key 轮转 ============
//
// 与 other.PickKey 同构，差异：候选构建额外排除「在途」key——上游对单账号
// 同时只放行一个请求（429 concurrent），同 key 并发只会白吃一轮限流冷却。
// 在途计数是运行态（不落盘），请求结束后释放；全部 key 在途时放宽排他
// 继续前进（单 key 池场景硬失败好过静默，上游 concurrent 闸自会裁决）。
// 冷却表在 usage.go（keyMu 域）。

var (
	// keyMu 保护 usage 痕迹表（usage.go；与 other 同款锁域：save/KeyStatus/
	// PickKey/Cooldown 全部经它，锁序 saveMu → keyMu → mu.RLock）。
	keyMu sync.Mutex
	// keyIdxMu 保护轮转游标（运行态，不落盘）——仅权重优化关闭时的
	// round_robin 全局策略路径使用。
	keyIdxMu sync.Mutex
	keyIdx   int
)

var inFlightMu sync.Mutex
var inFlight = map[string]int{} // key 明文 -> 在途请求数

// AcquireKey 标记 key 进入在途（取得 key 之后、发起上游调用前调用；Chat 的
// defer 里 ReleaseKey）。concurrent 闸门对策：选号避开在途 key。
func AcquireKey(key string) {
	if key == "" {
		return
	}
	inFlightMu.Lock()
	inFlight[key]++
	inFlightMu.Unlock()
}

// ReleaseKey 释放 key 的在途计数。
func ReleaseKey(key string) {
	if key == "" {
		return
	}
	inFlightMu.Lock()
	if v := inFlight[key]; v <= 1 {
		delete(inFlight, key)
	} else {
		inFlight[key] = v - 1
	}
	inFlightMu.Unlock()
}

// inFlightOf key 当前在途请求数（0 = 空闲）。
func inFlightOf(key string) int {
	inFlightMu.Lock()
	defer inFlightMu.Unlock()
	return inFlight[key]
}

// PickKey 在 key 池中选取下一个可用 key。strategy 取自网关账号调度策略；
// boundRoutable 是调用方注入的"该 key 绑定出口是否可用"判定（隔离模式；
// nil = 全部可路由）。候选构建：空闲优先 → 全在途时放宽排他 → 全冷却时
// UNABLE_FORCE 兜底（保持请求流动，与其他 key 池平台同口径）。调度语义由
// WeightOptimize 开关决定（面板热改）：开 = 额度加权随机（effectiveWeight
// Locked，剩余额度越多概率越高，各 key 日额度消耗趋平）；关 = 网关全局
// 策略原语义（round_robin 游标 / random / fill）。
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
	for k, until := range usage.Cool {
		if now.After(until) {
			delete(usage.Cool, k)
		}
	}
	keyIdxMu.Lock()
	start := keyIdx
	keyIdxMu.Unlock()
	buildCandidates := func(ignoreCooling, ignoreInFlight bool) []string {
		var out []string
		for i := 0; i < len(keys); i++ {
			k := keys[(start+i)%len(keys)]
			if blocked[k] || routingOff[k] {
				continue
			}
			if !ignoreCooling {
				if _, cooling := usage.Cool[k]; cooling {
					continue
				}
			}
			if !ignoreInFlight && inFlightOf(k) > 0 {
				continue
			}
			out = append(out, k)
		}
		return out
	}
	candidates := buildCandidates(false, false)
	if len(candidates) == 0 {
		// 全部空闲 key 在途：放宽排他继续前进。上游 concurrent 闸对重复
		// 请求回 429，走既有限流路径（短冷却+换 key+退避），好过硬失败。
		candidates = buildCandidates(false, true)
	}
	if len(candidates) == 0 && kit.UnableForce() {
		candidates = buildCandidates(true, true) // 全冷却兜底（UNABLE_FORCE=true）
	}
	if len(candidates) == 0 {
		return ""
	}
	var picked string
	if c.WeightOptimizeEnabled() {
		// 额度加权随机（wb 凭据权重同思想，2026-11-21 用户定「尽可能让额度
		// 持平」）：剩余额度越多被抽中概率越高，消耗按余额比例摊、整体趋平；
		// credits 快照未知按权重 1。抽中失败兜底首候选（理论不可达）。
		// 加权启用时取代全局策略（fill/random 同样被加权取代——开关本身即
		// 策略选择）；关闭时下方 switch 全局策略原路径原地生效。
		var total int64
		ws := make([]int64, len(candidates))
		for i, k := range candidates {
			ws[i] = effectiveWeightLocked(k)
			total += ws[i]
		}
		pick := int64(randIntn(int(total)))
		var acc int64
		for i, w := range ws {
			acc += w
			if pick < acc {
				picked = candidates[i]
				break
			}
		}
		if picked == "" {
			picked = candidates[0]
		}
		return picked
	}
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
				keyIdxMu.Lock()
				keyIdx = (i + 1) % len(keys)
				keyIdxMu.Unlock()
				break
			}
		}
	}
	return picked
}

// effectiveWeightLocked key 的调度权重：credits 快照已知且 >0 时取剩余额度
// （余额越多被抽中概率越高，额度趋平——wb 权重列同思想）；未知/0 按 1 兜底
// （零余额 key 保留最低参与度防全池零余额锁死，上游 0 额度自会拒绝并走冷却）。
// 调用方必须已持 keyMu（PickKey/KeyStatus 路径）。
func effectiveWeightLocked(key string) int64 {
	if cr, ok := usage.Credits[key]; ok && cr.Remaining > 0 {
		return cr.Remaining
	}
	return 1
}

// RecordKeyTokens 记录 key 本次请求消耗的 token（prompt+completion），自动
// 处理跨日重置，累计值不重置；同时刷新最近使用时间。tokens<=0 时忽略。
func RecordKeyTokens(key string, tokens int64) {
	if key == "" || tokens <= 0 {
		return
	}
	keyMu.Lock()
	if usage.KeyTokens == nil {
		usage.KeyTokens = map[string]KeyTokenStats{}
	}
	today := time.Now().Format("2006-01-02")
	st := usage.KeyTokens[key]
	if st.Date != today {
		st.Date = today
		st.Today = 0
	}
	st.Today += tokens
	st.Total += tokens
	st.LastUsedAt = time.Now()
	usage.KeyTokens[key] = st
	keyMu.Unlock()
	markTokensDirty() // 合批落盘（键已不在表里时记账也不会丢）
}

// Cooldown 把 key 置为冷却，冷却期内轮转跳过。返回实际生效的时长
// （0 = 未冷却：空 key）。上游 429 时按 Retry-After 冷却。
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
	keyMu.Lock()
	if usage.Cool == nil {
		usage.Cool = map[string]time.Time{}
	}
	usage.Cool[key] = time.Now().Add(d)
	keyMu.Unlock()
	saveUsage() // 冷却落盘持久化：重启后冷却保留
	return d
}

// Uncool 清除 key 的冷却（探测成功后调用：真实 2xx 是最强证据）。
func Uncool(key string) {
	if key == "" {
		return
	}
	keyMu.Lock()
	delete(usage.Cool, key)
	keyMu.Unlock()
	saveUsage() // 解除冷却同步落盘
}

// CooldownUntil 只读查询冷却截止时刻。
func CooldownUntil(key string) (time.Time, bool) {
	keyMu.Lock()
	defer keyMu.Unlock()
	until, ok := usage.Cool[key]
	return until, ok
}

// SetCredits 落一份 /me 查询结果（Test 与刷新额度共用）。key 必须在池内
// （调用方保证），不在池内的条目由 PruneUsage 收敛。
func SetCredits(key string, cr CreditStats) {
	if key == "" {
		return
	}
	keyMu.Lock()
	if usage.Credits == nil {
		usage.Credits = map[string]CreditStats{}
	}
	usage.Credits[key] = cr
	keyMu.Unlock()
	markTokensDirty()
}

// KeyIndex key 在池中的序号（1-based 展示用）；不存在返回 0。
func KeyIndex(key string) int {
	c := Get()
	for i, k := range c.Keys {
		if k == key {
			return i + 1
		}
	}
	return 0
}

// randIntn xorshift 轻量随机。
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

// StripPrefix 去掉平台前缀，返回裸模型 ID；无前缀时返回原串与 false。
func StripPrefix(model string) (id string, ok bool) {
	m := strings.TrimSpace(model)
	pfx := ModelPrefix + ":"
	if strings.HasPrefix(m, pfx) {
		return strings.TrimSpace(m[len(pfx):]), true
	}
	return m, false
}

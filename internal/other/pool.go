package other

import (
	"strings"
	"sync"
	"time"
)

// ============ 多 key 轮转 ============
//
// 语义与 amd/openrouter.PickKey 同构，但池是 per-platform 的：调用方传
// *Platform，选 key 只在该子平台的池内进行。轮转游标是包级运行态
// （slug -> 下标），不落盘、不进配置结构。keyMu 同时保护各平台的
// Usage/Cool 运行时表（写路径只在请求与探测路径）。

var (
	keyMu     sync.Mutex
	keyIdxMu  sync.Mutex
	keyIdxMap = map[string]int{}
)

func keyCursor(slug string) int {
	keyIdxMu.Lock()
	defer keyIdxMu.Unlock()
	return keyIdxMap[slug]
}

func setKeyCursor(slug string, idx int) {
	keyIdxMu.Lock()
	keyIdxMap[slug] = idx
	keyIdxMu.Unlock()
}

// PickKey 在平台 key 池中选取下一个可用 key。strategy 取自网关账号调度
// 策略；boundRoutable 是调用方注入的"该 key 绑定出口是否可用"判定
// （隔离模式；nil = 全部可路由）。
func PickKey(p *Platform, strategy string, boundRoutable func(key string) bool) string {
	keys := p.Keys
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
		if !p.RoutingEnabled(k) {
			routingOff[k] = true
		}
	}
	if len(blocked) == len(keys) || len(routingOff) == len(keys) {
		return ""
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	now := time.Now()
	for k, until := range p.Cool {
		if now.After(until) {
			delete(p.Cool, k)
		}
	}
	keyIdx := keyCursor(p.Slug)
	buildCandidates := func(ignoreCooling bool) []string {
		var out []string
		for i := 0; i < len(keys); i++ {
			k := keys[(keyIdx+i)%len(keys)]
			if blocked[k] || routingOff[k] {
				continue
			}
			if !ignoreCooling {
				if _, cooling := p.Cool[k]; cooling {
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
				setKeyCursor(p.Slug, (i+1)%len(keys))
				break
			}
		}
	}
	return picked
}

// MarkSuccess 累计 key 的成功调用计数（内存态，面板展示）。
func MarkSuccess(p *Platform, key string) {
	if key == "" {
		return
	}
	keyMu.Lock()
	if p.Usage == nil {
		p.Usage = map[string]int64{}
	}
	p.Usage[key]++
	keyMu.Unlock()
}

// Cooldown 把 key 置为冷却，冷却期内轮转跳过。返回实际生效的时长
// （0 = 未冷却：空 key）。上游 429 时按 Retry-After 冷却。
func Cooldown(p *Platform, key string, d time.Duration) time.Duration {
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
	if p.Cool == nil {
		p.Cool = map[string]time.Time{}
	}
	p.Cool[key] = time.Now().Add(d)
	keyMu.Unlock()
	return d
}

// Uncool 清除 key 的冷却（探测成功后调用：真实 2xx 是最强证据）。
func Uncool(p *Platform, key string) {
	if key == "" {
		return
	}
	keyMu.Lock()
	delete(p.Cool, key)
	keyMu.Unlock()
}

// CooldownUntil 只读查询冷却截止时刻。
func CooldownUntil(p *Platform, key string) (time.Time, bool) {
	until, ok := p.Cool[key]
	return until, ok
}

// KeyIndex key 在池中的序号（1-based 展示用）；不存在返回 0。
func KeyIndex(p *Platform, key string) int {
	for i, k := range p.Keys {
		if k == key {
			return i + 1
		}
	}
	return 0
}

// randIntn xorshift 轻量随机（与 amd/openrouter 同实现）。
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

// ResolvePrefix 把对外模型 ID 解析到子平台与裸模型 ID：命中某子平台前缀
// （"<prefix>:..."）时返回该平台与剥前缀后的模型 ID。
func ResolvePrefix(model string) (*Platform, string, bool) {
	m := strings.TrimSpace(model)
	if m == "" {
		return nil, m, false
	}
	c := Get()
	for i := range c.Platforms {
		pl := &c.Platforms[i]
		if pl.Prefix == "" {
			continue
		}
		pfx := pl.Prefix + ":"
		if strings.HasPrefix(m, pfx) {
			return pl, strings.TrimSpace(m[len(pfx):]), true
		}
	}
	return nil, m, false
}

// StripPrefix 去掉本平台前缀，返回裸模型 ID；无前缀时返回原串与 false。
func StripPrefix(p *Platform, model string) (id string, ok bool) {
	m := strings.TrimSpace(model)
	pfx := p.Prefix + ":"
	if strings.HasPrefix(m, pfx) {
		return strings.TrimSpace(m[len(pfx):]), true
	}
	return m, false
}

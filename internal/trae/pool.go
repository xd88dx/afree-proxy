package trae

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 账号轮转 ============
//
// 语义与 openrouter.PickKey 同构：round-robin 选未冷却且路由启用的账号；
// 绑定出口全部不可用的账号（隔离判定，调用方注入 boundRoutable）不参与
// 候选；全部不可用返回 nil，调用方报"无可用账号"而不是静默直连。

var (
	keyMu  sync.Mutex
	accIdx int // round-robin 轮转游标
)

// PickAccount 选取下一个可用账号。boundRoutable 是调用方注入的"该账号绑定
// 出口是否可用"判定（隔离模式；nil = 全部可路由）。
func PickAccount(strategy string, boundRoutable func(*Account) bool) *Account {
	c := Get()
	accounts := c.Accounts
	if len(accounts) == 0 {
		return nil
	}
	blocked := map[string]bool{}
	if boundRoutable != nil {
		for _, a := range accounts {
			if !boundRoutable(a) {
				blocked[a.ID] = true
			}
		}
	}
	routingOff := map[string]bool{}
	for _, a := range accounts {
		if !a.Routable() {
			routingOff[a.ID] = true
		}
	}
	if len(blocked) == len(accounts) || len(routingOff) == len(accounts) {
		return nil
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	now := time.Now()
	for id, until := range c.Cool {
		if now.After(until) {
			delete(c.Cool, id)
		}
	}
	buildCandidates := func(ignoreCooling bool) []*Account {
		var out []*Account
		for i := 0; i < len(accounts); i++ {
			a := accounts[(accIdx+i)%len(accounts)]
			if blocked[a.ID] || routingOff[a.ID] {
				continue
			}
			if !ignoreCooling {
				if _, cooling := c.Cool[a.ID]; cooling {
					continue
				}
			}
			out = append(out, a)
		}
		return out
	}
	candidates := buildCandidates(false)
	if len(candidates) == 0 && kit.UnableForce() {
		candidates = buildCandidates(true) // 全冷却兜底（UNABLE_FORCE=true：保持请求流动）
	}
	if len(candidates) == 0 {
		return nil
	}
	var picked *Account
	switch strategy {
	case "fill":
		// 池序第一个候选（用满一个再换）
		set := map[string]bool{}
		for _, a := range candidates {
			set[a.ID] = true
		}
		for _, a := range accounts {
			if set[a.ID] {
				picked = a
				break
			}
		}
	case "random":
		picked = candidates[randIntn(len(candidates))]
	case "credit-priority":
		// 额度优先（上游原版 polling_mode=credit-priority 语义）：额度余量多者
		// 优先，unlimited 恒最优先；余量未知按 0；同额度按池序稳定。
		picked = candidates[0]
		best := creditSortKey(picked)
		for _, a := range candidates[1:] {
			if k := creditSortKey(a); k < best {
				picked = a
				best = k
			}
		}
	default:
		// round_robin：轮转头的候选，游标推进到它之后
		picked = candidates[0]
		for i, a := range accounts {
			if a.ID == picked.ID {
				accIdx = (i + 1) % len(accounts)
				break
			}
		}
	}
	return picked
}

// creditSortKey 额度优先排序键：unlimited=0 恒最小（最优先），其余按剩余额度
// 降序（取负），未知额度视为 0。
func creditSortKey(a *Account) float64 {
	if a.Credits == nil {
		return 0
	}
	if a.Credits.Unlimited {
		return -1e18
	}
	return -a.Credits.Remaining
}

// AccountIndex 账号在池中的序号（1-based 展示用）；不存在返回 0。
func AccountIndex(id string) int {
	for i, a := range Get().Accounts {
		if a.ID == id {
			return i + 1
		}
	}
	return 0
}

// RecordAccountTokens 记录账号本次请求消耗的 token（prompt+completion），
// 自动跨日重置，累计值不重置；同时刷新最近使用时间。tokens<=0 忽略。
func RecordAccountTokens(id string, tokens int64) {
	if id == "" || tokens <= 0 {
		return
	}
	c := Get()
	mu.Lock()
	for _, a := range c.Accounts {
		if a.ID == id {
			today := time.Now().Format("2006-01-02")
			if a.Tokens.Date != today {
				a.Tokens.Date = today
				a.Tokens.Today = 0
			}
			a.Tokens.Today += tokens
			a.Tokens.Total += tokens
			a.Tokens.LastUsedAt = time.Now()
			break
		}
	}
	mu.Unlock()
}

// Cooldown 把账号置为冷却，冷却期内轮转跳过。返回实际生效的时长。
func Cooldown(id string, d time.Duration) time.Duration {
	if id == "" {
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
	c.Cool[id] = time.Now().Add(d)
	mu.Unlock()
	save() // 冷却落盘持久化（2026-10-28）：重启后冷却保留
	return d
}

// Uncool 清除账号冷却（探测/刷新成功后调用：真实 2xx 是最强证据）。
func Uncool(id string) {
	if id == "" {
		return
	}
	mu.Lock()
	delete(cfg.Cool, id)
	mu.Unlock()
	save() // 解除冷却同步落盘
}

// CooldownUntil 只读查询冷却截止时刻。
func CooldownUntil(id string) (time.Time, bool) {
	until, ok := Get().Cool[id]
	return until, ok
}

// PoolEnabled 账号是否参与路由（完整判定：账号总闸 && 反代开关）。
func PoolEnabled(a *Account) bool { return a.Routable() }

// AccountIDs 返回账号池主键列表（副本）。
func AccountIDs() []string {
	accs := Get().Accounts
	out := make([]string, 0, len(accs))
	for _, a := range accs {
		out = append(out, a.ID)
	}
	return out
}

// HasAccount 判断主键是否已在池中。
func HasAccount(id string) bool {
	for _, a := range Get().Accounts {
		if a.ID == id {
			return true
		}
	}
	return false
}

// randIntn 轻量随机（crypto/rand，避开全局源并发锁）。
func randIntn(n int) int {
	if n <= 1 {
		return 0
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	v := int(hexToUint64(b[:]) % uint64(n))
	if v < 0 {
		return 0
	}
	return v
}

func hexToUint64(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}

// randHex 生成 n 字节的十六进制随机串（请求/设备指纹字段用）。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}

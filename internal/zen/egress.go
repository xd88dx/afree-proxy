package zen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/exitpool"
)

// ============ egress binding: one proxy per upstream identity ============
//
// 背景（2026-10-21 实测）：opencode zen 免费层现在按 api key 记录出口 IP，
// 一个 key 只允许一个"个人 IP"。共享代理池的 round-robin 让每个 key 轮流
// 从所有出口出去 —— 每个 key 都见过 N 个 IP，即使代理本身干净，上游也会
// 把整池 key 判为可疑。对策是把出口从"按请求轮转"改成"按 key 绑定"：
// 一个 key 永远从同一个出口出去。
//
// 容量规则：同一个 namespace 内一个代理只服务一个身份。代理数少于身份数时，
// 多出来的身份没有绑定 —— 它们被 PickZenKey 过滤掉，而不是退回直连或借用
// 别的出口（那正是要消除的指纹）。cline 账号不接本表：它的出口绑定走账号级
// ProxyMain/ProxyBackup 体系（proxy_binding.go + 面板绑定列），两套并存会让
// 同一账号在两个绑定表里各有一份出口，互相打架。
//
// 持久化：zen-config.json 的 EgressBindings 段（2026-11-17 配置重组迁入——凭据级出口绑定跟凭据所在平台文件走；2026-10-27 曾从独立的
// .egress-bindings.json 并入——它就是代理域的数据；旧文件的自动迁移已随
// 2026-10-27 死代码清理移除，部署目录若仍有残留旧文件需手工合并）。绑定必须跨重启保留——每次启动重新分配
// 等于让 key 换 IP，正好重新触发上游告警。zen key 在表里只以 sha256 前
// 8 字节的 hex 出现，所以不含明文凭据，可以安全地贴进 issue。

const (
	egressNSZen = "zen:"
)

// EgressBinding 单个身份绑定的出口。
type EgressBinding struct {
	Proxy string `json:"proxy"`
	Since int64  `json:"since"`
}

// BoolPtr 取指针（配置里用 *bool 区分"未提交"与"显式 false"）。
func BoolPtr(b bool) *bool { return &b }

var (
	EgressMu       sync.Mutex
	EgressBindings = map[string]EgressBinding{}
	EgressLoaded   bool
)

// EgressID 绑定表的键：namespace + 身份。zen key 是凭据，只写入 sha256
// 前 8 字节的 hex（16 字符），绑定表因此不含明文密钥。cline 账号 ID 不是
// 凭据，原样保留——面板与日志里能直接对上号。
func EgressID(ns, id string) string {
	if ns == egressNSZen {
		sum := sha256.Sum256([]byte(id))
		return hex.EncodeToString(sum[:8])
	}
	return id
}

func LoadEgressBindings() {
	EgressMu.Lock()
	defer EgressMu.Unlock()
	if EgressLoaded {
		return
	}
	EgressLoaded = true
	zenFileMu.Lock()
	m := LoadZenConfigFileLocked().EgressBindings
	zenFileMu.Unlock()
	if len(m) > 0 {
		EgressBindings = m
	}
}

// SaveEgressLocked 持久化绑定表（调用方持有 EgressMu；锁序 EgressMu 在外、
// zenFileMu 在内，与 zen 子系统其它段一致）。
func SaveEgressLocked() {
	next := make(map[string]EgressBinding, len(EgressBindings))
	for k, v := range EgressBindings {
		next[k] = v
	}
	MutateZenConfigFile(func(f *ZenConfigFileData) {
		f.EgressBindings = next
	})
}

// ReconcileEgressBindings 按当前 key/账号/代理列表重算绑定并落盘。
// 幂等：已有绑定只要代理还在池里就原样保留（这是"key 不换 IP"的全部意义）。
func ReconcileEgressBindings() {
	LoadEgressBindings()

	EgressMu.Lock()
	defer EgressMu.Unlock()
	// 只管 zen namespace：cline 账号级出口绑定走本地 ProxyMain/ProxyBackup
	// 体系（proxy_binding.go + 面板绑定/自动分配），两套并存会让同一账号在
	// 两个绑定表里各有一份出口，互相打架。上游的 cline 钉定不接入。
	zenAdded, zenDropped := AssignEgressLocked(egressNSZen, GetZenConfig().Keys, exitpool.GetPool().Proxies)
	if zenAdded+zenDropped > 0 {
		SaveEgressLocked()
	}
}

// AssignEgressLocked 为一个 namespace 重算绑定，返回 (新分配, 失效丢弃) 数量。
//
// 分配顺序：先按配置顺序为已绑定身份登记代理占用，再把未绑定身份依次填入
// 空闲代理。这样新增一个代理只会给下一个空身份用，既有 key 的出口不动。
func AssignEgressLocked(ns string, ids, proxies []string) (assigned, dropped int) {
	valid := make(map[string]bool, len(proxies))
	for _, p := range proxies {
		if p != "" {
			valid[p] = true
		}
	}
	// 1) 代理被移出池 → 该绑定失效（占位也已不存在）。
	for k, b := range EgressBindings {
		if !strings.HasPrefix(k, ns) {
			continue
		}
		if !valid[b.Proxy] {
			delete(EgressBindings, k)
			dropped++
		}
	}
	if len(valid) == 0 {
		// 无代理 = 直连模式：绑定没有意义，清空以免代理重新加回来时
		// 沿用一批陈旧（且可能已被别人占用）的分配。
		for k := range EgressBindings {
			if strings.HasPrefix(k, ns) {
				delete(EgressBindings, k)
			}
		}
		return 0, dropped
	}
	// 2) 已绑定身份先占位（按配置顺序，先到先得，结果稳定）。
	taken := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !EgressEligibleID(id) {
			continue
		}
		if b, ok := EgressBindings[ns+EgressID(ns, id)]; ok {
			taken[b.Proxy] = true
		}
	}
	// 3) 未绑定身份填空闲代理；池满则保持未绑定（= 该身份被过滤掉）。
	for _, id := range ids {
		if !EgressEligibleID(id) {
			continue
		}
		bk := ns + EgressID(ns, id)
		if _, ok := EgressBindings[bk]; ok {
			continue
		}
		next := ""
		for _, p := range proxies {
			if p == "" || taken[p] {
				continue
			}
			next = p
			break
		}
		if next == "" {
			continue
		}
		EgressBindings[bk] = EgressBinding{Proxy: next, Since: time.Now().Unix()}
		taken[next] = true
		assigned++
	}
	return assigned, dropped
}

// EgressEligibleID 该身份是否需要独占一个出口。
// 空 ID 与 zen 的匿名哨兵 "public" 不算身份：没有 key 就无法被上游关联，
// 给它们分配代理只会白占名额——恰好挤掉一个真实 key 的出口。
func EgressEligibleID(id string) bool {
	return id != "" && id != "public"
}

// EgressProxyFor 查一个身份绑定的出口。
func EgressProxyFor(ns, id string) (string, bool) {
	if !EgressEligibleID(id) {
		return "", false
	}
	LoadEgressBindings()
	EgressMu.Lock()
	defer EgressMu.Unlock()
	b, ok := EgressBindings[ns+EgressID(ns, id)]
	if !ok || b.Proxy == "" {
		return "", false
	}
	return b.Proxy, true
}

func EgressProxyForZen(key string) (string, bool) { return EgressProxyFor(egressNSZen, key) }

// PinProxyPerKeyEnabled 面板开关（默认开）。nil 表示旧配置没有这个键。
func PinProxyPerKeyEnabled() bool {
	p := GetZenConfig().PinProxyPerKey
	return p == nil || *p
}

// EgressBindingActive 绑定策略是否真的生效：必须同时"开启"且"有代理"。
// 没有代理时退回直连（历史行为）——否则默认配置（proxies 为空）会让每个
// zen key 都被判为"无可用出口"，整条 zen 链路直接失效。
func EgressBindingActive() bool {
	return PinProxyPerKeyEnabled() && len(exitpool.GetPool().Proxies) > 0
}

// ProxyIndex 返回代理在池中的下标（冷却表按索引记录）；不在池中返回 -1。
func ProxyIndex(p string) int {
	if p == "" {
		return -1
	}
	for i, x := range exitpool.GetPool().Proxies {
		if x == p {
			return i
		}
	}
	return -1
}

// EgressCapacity 返回 (已绑定身份数, 身份总数)。
func EgressCapacity(ns string, ids []string) (assigned, total int) {
	LoadEgressBindings()
	EgressMu.Lock()
	defer EgressMu.Unlock()
	for _, id := range ids {
		if _, ok := EgressBindings[ns+EgressID(ns, id)]; ok {
			assigned++
		}
	}
	return assigned, len(ids)
}

// EgressShortfallError 说明"为什么没有可用身份"以及怎么修。
// 这是绑定策略下最常见的问题（代理比 key 少），必须有可操作的提示，
// 否则用户只看到 500/503 而不知道为什么 key 突然不用了。
func EgressShortfallError(label, ns string, ids []string) error {
	assigned, total := EgressCapacity(ns, ids)
	return fmt.Errorf("%d of %d %s identities have an egress proxy assigned (one proxy serves one identity; %d proxies configured) — add proxies, or turn off pinProxyPerKey",
		assigned, total, label, len(exitpool.GetPool().Proxies))
}

// EgressKeyState 单个 key 的出口快照（面板用）。
type EgressKeyState struct {
	proxy string // 绑定的代理（未打码，调用方按需掩码）
	index int    // 池内下标，-1 表示无
	bound bool   // 绑定策略是否处于生效状态（用来区分"关闭"与"没分到"）
}

// ZenEgressSnapshotOf 批量取 key 的出口快照。在 zenKeyMu 之外调用。
func ZenEgressSnapshotOf(keys []string) []EgressKeyState {
	active := EgressBindingActive()
	out := make([]EgressKeyState, len(keys))
	for i, k := range keys {
		out[i].bound = active
		out[i].index = -1
		if !active || k == "" || k == "public" {
			continue
		}
		if p, ok := EgressProxyForZen(k); ok {
			out[i].proxy = p
			out[i].index = ProxyIndex(p)
		}
	}
	return out
}

// ZenEgressSnapshot 汇总接口：给面板判断"代理够不够分"。
func ZenEgressSnapshot() map[string]any {
	cfg := GetZenConfig()
	assigned, total := EgressCapacity(egressNSZen, cfg.Keys)
	return map[string]any{
		"enabled":    PinProxyPerKeyEnabled(),
		"active":     EgressBindingActive(),
		"proxies":    len(exitpool.GetPool().Proxies),
		"assigned":   assigned,
		"identities": total,
		"unassigned": total - assigned,
	}
}

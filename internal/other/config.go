package other

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ============ 配置与数据模型 ============
//
// Other 平台（自定义小企业上游的聚合）：一个 Other 页下挂多个子平台，每个
// 子平台是独立的 key 池（BaseURL + 多 key + 前缀 + 模型勾选），请求按
// "<prefix>:<model-id>" 路由。结构与 internal/amd / internal/tokenharbor
// 同构（key 轮转 / 401·429 冷却 / 主备代理绑定 / 面板暂存编辑），区别是
// 平台数量动态，全部状态落盘于 DATA_DIR/.other-config.json。

// Config 是 Other 平台的全部可配置状态。
type Config struct {
	// UseProxies 未绑定 key 是否走共享代理池（Proxy pool 页 Global policy
	// 的 Other 列）。nil = 缺省 = 走池。
	UseProxies *bool `json:"useProxies,omitempty"`
	// Platforms 子平台列表（有序）。
	Platforms []Platform `json:"platforms,omitempty"`
}

// Platform 一个子平台的全部状态。
type Platform struct {
	// Slug 内部标识（创建时生成，不可改；管理 API 用它定位平台）。
	Slug string `json:"slug"`
	// Name 面板显示名（可改）。
	Name string `json:"name"`
	// Prefix 对外模型前缀（规范化后的裸串，不含冒号），唯一。对外模型 ID
	// 形如 "<prefix>:<model-id>"。
	Prefix string `json:"prefix"`
	// BaseURL 上游 OpenAI 兼容端点根（去尾斜杠）。
	BaseURL string `json:"baseURL"`
	// Keys 多 key 池，请求按池调度策略轮转。
	Keys []string `json:"keys,omitempty"`
	// KeyRoutingEnabled 路由参与表：key 明文 -> 是否进入请求轮转。
	// nil/缺项 = 参与。新增 key 默认 false（各平台统一口径）。
	KeyRoutingEnabled map[string]bool `json:"keyRoutingEnabled,omitempty"`
	// KeyBindings 出口绑定表：key 明文 -> 主/备用代理。
	KeyBindings map[string]Binding `json:"keyBindings,omitempty"`
	// MaxConcurrency 上游最大并发，默认 8。
	MaxConcurrency int `json:"maxConcurrency"`
	// Retries 限流/网络错误重试次数，默认 3。
	Retries int `json:"retries"`
	// Models 模型目录状态（拉取/手动/勾选/探测模型）。
	Models ModelState `json:"models"`
	// KeyTokens token 记账（内存态，不落盘）：key 明文 -> 今日/累计消耗与最近使用。
	KeyTokens map[string]KeyTokenStats `json:"-"`
	// Cool 冷却表：key 明文 -> 冷却截止时刻。落盘持久化（2026-10-28 用户定）：
	// 重启后冷却保留，避免上游刚限流过的 key 立即回池被重试；选号时惰性清理过期项。
	Cool map[string]time.Time `json:"cool,omitempty"`
}

// Binding 一个 key 的出口绑定：主代理优先、备用代理兜底；双不可用 = 该 key
// 本轮跳过（隔离优先于可用性，绝不回退直连）。空值 = 未绑定 = 走全局规则。
type Binding struct {
	Main   string `json:"main,omitempty"`
	Backup string `json:"backup,omitempty"`
}

// KeyTokenStats 单个 key 的 token 记账：今日/累计消耗（prompt+completion）
// 与跨日日期、最近成功时间。内存态（不落盘）。
type KeyTokenStats struct {
	Today      int64     `json:"today,omitempty"`
	Total      int64     `json:"total,omitempty"`
	Date       string    `json:"date,omitempty"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}

// ModelState 模型目录：Fetched 为上游 /models 拉取结果（会随同步整体替换），
// Manual 为手动添加的模型 ID（常驻），Known = Fetched ∪ Manual；Selected 为
// 勾选表（对外暴露的模型，按模型 ID 记录，上游模型上下线不清勾选）。
type ModelState struct {
	Fetched  []Model         `json:"fetched,omitempty"`
	Manual   []string        `json:"manual,omitempty"`
	Selected map[string]bool `json:"selected,omitempty"`
	// Probe 探测模型 ID（Test 按钮用）；空 = 取勾选列表中排序第一个。
	Probe string `json:"probe,omitempty"`
}

// Model 一个模型条目（/v1/models 合并与面板展示用）。
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Context  int    `json:"context,omitempty"`
	ToolCall bool   `json:"toolCall,omitempty"`
}

// ReservedPrefixes 内置平台前缀（与各平台模块的 ModelPrefix 保持一致：
// zen / wbcn / wbgb / cline-free/ / amd / tkhb / oprt）。Other 子平台前缀
// 不得与之冲突，否则请求路由会被既有平台截走。
var ReservedPrefixes = []string{"zen:", "wbcn:", "wbgb:", "cline-free/", "amd:", "tkhb:", "oprt:"}

// DefaultConfig 出厂配置：无子平台、并发/重试默认值在平台 normalize 时补齐。
func DefaultConfig() *Config {
	return &Config{}
}

// ============ 规范化与校验 ============

// NormalizePrefix 规范化前缀：去空白、去尾部冒号（对外 ID 拼接时统一加冒号）。
func NormalizePrefix(p string) string {
	return strings.TrimRight(strings.TrimSpace(p), ":")
}

// ValidatePrefix 前缀格式：小写字母开头，后接小写字母/数字/下划线/连字符，
// 1-32 字符。不含冒号（对外 ID 的冒号由拼接层添加）。
func ValidatePrefix(p string) error {
	p = NormalizePrefix(p)
	if p == "" {
		return fmt.Errorf("prefix is required")
	}
	if len(p) > 32 {
		return fmt.Errorf("prefix %q too long (max 32 chars)", p)
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '-'):
		default:
			return fmt.Errorf("prefix %q invalid: lowercase letters, digits, _ and - only (start with a letter)", p)
		}
	}
	for _, r := range ReservedPrefixes {
		if p+":" == r || p+"/" == r {
			return fmt.Errorf("prefix %q is reserved by a built-in platform", p)
		}
	}
	return nil
}

// PrefixConflict 检查前缀是否与本配置内其他子平台冲突（不含自身，skipSlug
// 为空时检查全部）。冲突返回冲突平台的显示名。
func (c *Config) PrefixConflict(p, skipSlug string) (string, bool) {
	p = NormalizePrefix(p)
	for i := range c.Platforms {
		pl := &c.Platforms[i]
		if pl.Slug == skipSlug {
			continue
		}
		if pl.Prefix == p {
			return pl.Name, true
		}
	}
	return "", false
}

// normalize 常规化：每平台 key 去重、baseURL 去尾斜杠、并发/重试下限修正、
// 冷却表初始化、前缀规范化、勾选表补齐。
func (c *Config) normalize() {
	for i := range c.Platforms {
		p := &c.Platforms[i]
		normalizeKeys(p)
		p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		p.Prefix = NormalizePrefix(p.Prefix)
		if p.MaxConcurrency <= 0 {
			p.MaxConcurrency = 8
		}
		if p.Retries < 0 {
			p.Retries = 0
		}
		if p.Cool == nil {
			p.Cool = map[string]time.Time{}
		}
		if p.Models.Selected == nil {
			p.Models.Selected = map[string]bool{}
		}
	}
}

// normalizeKeys 规范 key 池：去空去重，保持顺序。
func normalizeKeys(p *Platform) {
	cleaned := make([]string, 0, len(p.Keys))
	seen := map[string]bool{}
	for _, k := range p.Keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		cleaned = append(cleaned, k)
	}
	p.Keys = cleaned
}

// ReconcileRouting 整体替换 key 池后对齐路由参与表：新引入的 key 默认不
// 参与路由（面板勾选"Pool"后才生效），已有 key 保持原状态，被移除的 key
// 清理表项。cur 为替换前的平台（携带旧 key 池），next 为替换后的平台。
func (next *Platform) ReconcileRouting(cur *Platform) {
	old := map[string]bool{}
	if cur != nil {
		for _, k := range cur.Keys {
			old[k] = true
		}
	}
	nextSet := map[string]bool{}
	for _, k := range next.Keys {
		nextSet[k] = true
	}
	routing := map[string]bool{}
	for k, v := range next.KeyRoutingEnabled {
		if nextSet[k] {
			routing[k] = v
		}
	}
	for _, k := range next.Keys {
		if !old[k] {
			routing[k] = false
		}
	}
	if len(routing) == 0 {
		next.KeyRoutingEnabled = nil
		return
	}
	next.KeyRoutingEnabled = routing
}

// RoutingEnabled key 是否参与请求路由。nil/缺项 = 参与。
func (p *Platform) RoutingEnabled(key string) bool {
	if v, ok := p.KeyRoutingEnabled[key]; ok {
		return v
	}
	return true
}

// maskKey 面板展示的 key 掩码（前 12 位 + 省略号，与 amd/openrouter 同口径）。
func maskKey(k string) string {
	switch {
	case k == "":
		return "-"
	case len(k) <= 12:
		return k[:4] + "…"
	default:
		return k[:12] + "…"
	}
}

// MaskKey 对外暴露的掩码函数（管理 API 用；key 明文永不出口）。
func MaskKey(k string) string { return maskKey(k) }

// ============ 存储 ============

var (
	mu     sync.RWMutex
	cfg    = DefaultConfig()
	saveMu sync.Mutex
	loaded bool
)

// ConfigPath 数据文件路径（由主包注入，指向 DATA_DIR/.other-config.json）。
var ConfigPath string

// Load 读取持久化配置（主包装配期调用一次）。文件不存在或损坏时保留默认值
// （损坏文件改名留档，不静默覆盖）。
func Load(path string) {
	ConfigPath = path
	mu.Lock()
	defer mu.Unlock()
	if loaded {
		return
	}
	loaded = true
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
			fmt.Printf("  other config corrupt JSON; moved to %s.corrupt-%s\n", path, stamp)
		}
		return
	}
	cfg.normalize()
	// 已持 mu 写锁：必须走不重入的 rebuildAllSemLocked（见其注释）。
	rebuildAllSemLocked()
}

// Get 返回活配置指针（调用方遵循写时复制：先拷贝平台再改再 Set）。
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return cfg
}

// PlatformBySlug 活配置里按 slug 找子平台（nil = 不存在）。
func PlatformBySlug(slug string) *Platform {
	c := Get()
	for i := range c.Platforms {
		if c.Platforms[i].Slug == slug {
			return &c.Platforms[i]
		}
	}
	return nil
}

// PlatformByPrefix 活配置里按前缀找子平台（nil = 不存在）。
func PlatformByPrefix(prefix string) *Platform {
	prefix = NormalizePrefix(prefix)
	c := Get()
	for i := range c.Platforms {
		if c.Platforms[i].Prefix == prefix {
			return &c.Platforms[i]
		}
	}
	return nil
}

// ClonePlatform 深拷贝一个子平台（写时复制用，运行时字段一并拷贝）。
// 拷贝 Cool 持 keyMu：源平台的 Cool 会被 Cooldown/Uncool 并发就地写。
func ClonePlatform(p *Platform) *Platform {
	keyMu.Lock()
	defer keyMu.Unlock()
	cp := *p
	cp.Keys = append([]string(nil), p.Keys...)
	cp.KeyRoutingEnabled = map[string]bool{}
	for k, v := range p.KeyRoutingEnabled {
		cp.KeyRoutingEnabled[k] = v
	}
	cp.KeyBindings = map[string]Binding{}
	for k, v := range p.KeyBindings {
		cp.KeyBindings[k] = v
	}
	cp.Models.Fetched = append([]Model(nil), p.Models.Fetched...)
	cp.Models.Manual = append([]string(nil), p.Models.Manual...)
	cp.Models.Selected = map[string]bool{}
	for k, v := range p.Models.Selected {
		cp.Models.Selected[k] = v
	}
	cp.Cool = map[string]time.Time{}
	for k, v := range p.Cool {
		cp.Cool[k] = v
	}
	return &cp
}

// Set 替换配置：normalize → 落盘 → 重建并发信号量。
func Set(next *Config) {
	next.normalize()
	mu.Lock()
	cfg = next
	mu.Unlock()
	save()
	// rebuildAllSemLocked 只读包级 cfg 与 semMu，不再取 mu，故无需持 mu。
	rebuildAllSemLocked()
}

// save 原子落盘（临时文件 + rename）。Cool（冷却表）随配置一并持久化。
// marshal 全程持 keyMu：Cool 在 Cooldown/Uncool/PickKey 下就地上写（keyMu 域），
// 无锁 marshal 会构成 "concurrent map iteration and map write" 进程级崩溃
// （2026-10-29 审计修复）； Platforms 切片本体另持 mu.RLock 防 Set 换指针。
// 锁序固定 keyMu → mu，与 KeyStatus 一致；无任何 mu → keyMu 反向路径。
func save() {
	saveMu.Lock()
	defer saveMu.Unlock()
	if ConfigPath == "" {
		return
	}
	keyMu.Lock()
	mu.RLock()
	disk := *cfg
	disk.Platforms = make([]Platform, len(cfg.Platforms))
	for i, p := range cfg.Platforms {
		cp := p
		disk.Platforms[i] = cp
	}
	mu.RUnlock()
	data, err := json.MarshalIndent(&disk, "", "  ")
	keyMu.Unlock()
	if err != nil {
		fmt.Printf("  other config marshal failed: %v\n", err)
		return
	}
	tmp := ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  other config save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, ConfigPath); err != nil {
		if werr := os.WriteFile(ConfigPath, data, 0600); werr != nil {
			fmt.Printf("  other config replace failed: %v\n", err)
			return
		}
		_ = os.Remove(tmp)
	}
}

// ProxiesEnabled 未绑定 key 是否走共享代理池：缺省（nil）默认走池。

// useProxiesResolver 由 app 层注入：走池开关存在 proxy-config.json 的代理池段
// （与池本体同处一个文件），平台配置文件不再保存它。返回 nil = 该平台未配置，
// 即缺省走池。
var useProxiesResolver func() *bool

// SetUseProxiesResolver 由 app 层在启动时注入读取器（见 app/platform_proxies.go）。
func SetUseProxiesResolver(f func() *bool) { useProxiesResolver = f }

// CurrentUseProxies 当前走池开关：优先全局代理配置，回落到本文件的历史字段。
// 面板回显与请求路径共用同一口径。
func CurrentUseProxies() *bool {
	if useProxiesResolver != nil {
		if v := useProxiesResolver(); v != nil {
			return v
		}
	}
	return Get().UseProxies
}

// ProxiesEnabled 未绑定凭据是否走共享代理池：缺省（nil）默认走池。
func ProxiesEnabled() bool {
	up := CurrentUseProxies()
	if up == nil {
		return true
	}
	return *up
}

// KeyStatus 面板展示用的单平台每 key 运行时状态（key 值打码，明文不出口）。
// KeyTokens/Cool 是 keyMu 域的就地写运行时表，读它们必须持 keyMu
// （Keys/KeyBindings 走写时复制，无锁读安全）。
func KeyStatus(p *Platform) []map[string]any {
	keyMu.Lock()
	defer keyMu.Unlock()
	now := time.Now()
	out := make([]map[string]any, 0, len(p.Keys))
	for i, k := range p.Keys {
		st := map[string]any{
			"index":          i,
			"keyMask":        maskKey(k),
			"routingEnabled": p.RoutingEnabled(k),
		}
		if ts, ok := p.KeyTokens[k]; ok {
			st["tokensToday"] = ts.Today
			st["tokensTotal"] = ts.Total
			if !ts.LastUsedAt.IsZero() {
				st["lastUsed"] = ts.LastUsedAt.Format(time.RFC3339)
			}
		}
		if b, ok := p.KeyBindings[k]; ok && (b.Main != "" || b.Backup != "") {
			st["proxyMain"] = b.Main
			st["proxyBackup"] = b.Backup
		}
		if until, ok := p.Cool[k]; ok && now.Before(until) {
			st["cooling"] = true
			st["cooldownUntil"] = until.Format(time.RFC3339)
		} else {
			st["cooling"] = false
		}
		out = append(out, st)
	}
	return out
}

// BindingOf key 的出口绑定；未绑定时 bound=false。
func BindingOf(p *Platform, key string) (main, backup string, bound bool) {
	b, ok := p.KeyBindings[key]
	if !ok || (b.Main == "" && b.Backup == "") {
		return "", "", false
	}
	return b.Main, b.Backup, true
}

// ============ 并发信号量 ============
//
// 每子平台独立信号量（MaxConcurrency 上游最大并发，防单平台瞬时打满上游）。
// Set 时统一重建；正在飞行的请求持旧信号量完成，新请求换新信号量。

var (
	semMu sync.Mutex
	sems  = map[string]chan struct{}{}
)

// rebuildAllSemLocked 重建全部子平台并发信号量。调用方必须已持有 mu 写锁
// 或根本未持锁——Go 的 RWMutex 不可重入，持锁者再调 Get() 会在 RLock
// 上自死锁（Load 走的就是这条路径）。
func rebuildAllSemLocked() {
	semMu.Lock()
	defer semMu.Unlock()
	c := cfg
	sems = make(map[string]chan struct{}, len(c.Platforms))
	for i := range c.Platforms {
		p := &c.Platforms[i]
		n := p.MaxConcurrency
		if n <= 0 {
			n = 8
		}
		sems[p.Slug] = make(chan struct{}, n)
	}
}

func semFor(slug string, n int) chan struct{} {
	semMu.Lock()
	defer semMu.Unlock()
	s, ok := sems[slug]
	if !ok || cap(s) != n {
		s = make(chan struct{}, n)
		sems[slug] = s
	}
	return s
}

// acquireSem 占一个并发槽位；ctx 取消时不再占用。返回 false 表示客户端已离开。
func acquireSem(ctx context.Context, slug string, n int) bool {
	s := semFor(slug, n)
	select {
	case s <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func releaseSem(slug string, n int) {
	semMu.Lock()
	s := sems[slug]
	semMu.Unlock()
	if s != nil {
		<-s
	}
}

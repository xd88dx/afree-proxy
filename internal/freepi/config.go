package freepi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ============ 配置与数据模型 ============
//
// FreePI（freepi.ai 免费日额度池）：单平台 key 池，凭据是 free-pi CLI 铸造
// 的 JWT（90 天有效，UTC 零点重置每日 credits）。与 internal/other 同构
// （key 轮转 / 401·429 冷却 / 主备出口绑定 / 暂存编辑 / 昵称表），差异：
// 无子平台维度（前缀与上游端点固定），痕迹表含 credits 余额（/me 免费查询，
// 不打对话额度）。上游三道闸门（ads_required / concurrent_session /
// concurrent）的处理在 client.go——这是与普通 key 池的本质差异。

// 平台常量：上游端点与 CLI 版本指纹，对齐 free-pi CLI 0.6.25（参考
// pingmike2/freepi-magpie-plugin——上游服务端有最低版本闸，版本随参考
// 插件的 CLIENT_VERSION 推进）。上游只有一个，BaseURL 不做成配置
// （死配置判据：没有理由改它；真门禁与出口都在网关层）。
const (
	APIOrigin     = "https://api.freepi.ai"
	APIBase       = APIOrigin + "/v1"
	ClientVersion = "0.6.25"
	// ModelPrefix 对外模型前缀：对外模型 ID 形如 "freepi:<model-id>"。
	ModelPrefix = "freepi"
)

// Config 是 FreePI 平台的全部可配置状态（单平台，无子平台维度）。
type Config struct {
	// UseProxies 未绑定 key 是否走共享代理池（Proxy pool 页 Global policy
	// 的 FreePI 列）。nil = 缺省 = 走池。
	UseProxies *bool `json:"useProxies,omitempty"`
	// Keys 多 key 池（JWT），请求按池调度策略轮转。
	Keys []string `json:"keys,omitempty"`
	// KeyRoutingEnabled 路由参与表：key 明文 -> 是否进入请求轮转。
	// nil/缺项 = 参与。新增 key 默认 false（各平台统一口径）。
	KeyRoutingEnabled map[string]bool `json:"keyRoutingEnabled,omitempty"`
	// KeyBindings 出口绑定表：key 明文 -> 主/备用代理。
	KeyBindings map[string]Binding `json:"keyBindings,omitempty"`
	// KeyNicknames key 昵称表（账号列堆叠展示，口径对齐 other 2026-11-19）：
	// key 明文 -> 显示名。仅影响面板展示，不参与路由；空/缺 = 显示掩码 key。
	KeyNicknames map[string]string `json:"keyNicknames,omitempty"`
	// MaxConcurrency 平台最大并发（信号量），默认 8（other 同款）。
	MaxConcurrency int `json:"maxConcurrency"`
	// Retries 限流/网络错误重试次数，默认 3。
	Retries int `json:"retries"`
	// Models 模型目录状态（/client-version 拉取结果整体替换；静态兜底表在
	// models.go）。other 的勾选暴露机制刻意没有：目录即上游真值，全量暴露。
	Models ModelState `json:"models"`
	// KeyTokens（token 记账）/Cool（冷却表）/Credits（余额）是使用痕迹，
	// 拆到 freepi-state.json（usage.go）：随请求/429/查询变化，留在配置
	// 文件会干扰外部对配置变更的监测（*-config 与 *-state 命名铁律）。
}

// Binding 一个 key 的出口绑定：主代理优先、备用代理兜底；双不可用 = 该 key
// 本轮跳过（隔离优先于可用性，绝不回退直连）。空值 = 未绑定 = 走全局规则。
type Binding struct {
	Main   string `json:"main,omitempty"`
	Backup string `json:"backup,omitempty"`
}

// KeyTokenStats 单个 key 的 token 记账：今日/累计消耗（prompt+completion）
// 与跨日日期、最近成功时间。
type KeyTokenStats struct {
	Today      int64     `json:"today,omitempty"`
	Total      int64     `json:"total,omitempty"`
	Date       string    `json:"date,omitempty"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}

// ModelState 模型目录：Fetched 为上游 GET /client-version 拉取结果（同步
// 成功时整体替换）。静态兜底表 staticModels 在 models.go，不落盘。
type ModelState struct {
	Fetched []Model `json:"fetched,omitempty"`
}

// Model 一个模型条目（/v1/models 合并与面板展示用）。Name 携带上游目录的
// 倍率标注（如 "Solar Pro 4 · ~0.6x (cheapest)"）。
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Context  int    `json:"context,omitempty"`
	ToolCall bool   `json:"toolCall,omitempty"`
}

// DefaultConfig 出厂配置：并发/重试默认值在 normalize 时补齐。
func DefaultConfig() *Config { return &Config{} }

// ============ 规范化 ============

// normalize 常规化：key 去重、并发/重试下限修正。
func (c *Config) normalize() {
	cleaned := make([]string, 0, len(c.Keys))
	seen := map[string]bool{}
	for _, k := range c.Keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		cleaned = append(cleaned, k)
	}
	c.Keys = cleaned
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = 8
	}
	if c.Retries < 0 {
		c.Retries = 0
	}
}

// ReconcileRouting 整体替换 key 池后对齐路由参与表：新引入的 key 默认不
// 参与路由（面板勾选 "Pool" 后才生效），已有 key 保持原状态，被移除的 key
// 清理表项。cur 为替换前的配置（携带旧 key 池），next 为替换后的配置。
func (next *Config) ReconcileRouting(cur *Config) {
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
func (c *Config) RoutingEnabled(key string) bool {
	if v, ok := c.KeyRoutingEnabled[key]; ok {
		return v
	}
	return true
}

// maskKey 面板展示的 key 掩码（前 12 位 + 省略号）。
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

// MaskKey 对外暴露的掩码函数（管理 API 用；key 明文永不出口，明文查询走
// app 层的 /keys/get 端点）。
func MaskKey(k string) string { return maskKey(k) }

// ============ 存储 ============

var (
	mu     sync.RWMutex
	cfg    = DefaultConfig()
	saveMu sync.Mutex
	loaded bool
)

// ConfigPath 数据文件路径（由主包注入，指向 DATA_DIR/freepi-config.json）。
var ConfigPath string

// Load 读取持久化配置（主包装配期调用一次）。文件不存在或损坏时保留默认值
// （损坏文件改名留档，不静默覆盖）。
func Load(path string) {
	ConfigPath = path
	mu.Lock()
	if loaded {
		mu.Unlock()
		return
	}
	loaded = true
	data, err := os.ReadFile(path)
	if err != nil {
		mu.Unlock()
		return
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
			fmt.Printf("  freepi config corrupt JSON; moved to %s.corrupt-%s\n", path, stamp)
		}
		mu.Unlock()
		return
	}
	cfg.normalize()
	// 已持 mu 写锁：必须走不重入的 rebuildSemLocked（RWMutex 不可重入）。
	rebuildSemLocked()
	mu.Unlock()
	// 痕迹加载在配置写锁外执行：锁序约定无 mu → keyMu 反向路径（other 同款）。
	loadUsage()
}

// Get 返回活配置指针（调用方遵循写时复制：先 CloneConfig 再改再 Set）。
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return cfg
}

// CloneConfig 深拷贝整个配置（写时复制用）。KeyRoutingEnabled/KeyBindings/
// KeyNicknames 无并发就地写、无锁拷贝安全；token 记账/冷却/credits 在痕迹
// 文件侧（usage.go），不随 Config 对象拷贝。
func CloneConfig(c *Config) *Config {
	cp := *c
	cp.Keys = append([]string(nil), c.Keys...)
	cp.KeyRoutingEnabled = map[string]bool{}
	for k, v := range c.KeyRoutingEnabled {
		cp.KeyRoutingEnabled[k] = v
	}
	cp.KeyBindings = map[string]Binding{}
	for k, v := range c.KeyBindings {
		cp.KeyBindings[k] = v
	}
	cp.KeyNicknames = map[string]string{}
	for k, v := range c.KeyNicknames {
		cp.KeyNicknames[k] = v
	}
	cp.Models.Fetched = append([]Model(nil), c.Models.Fetched...)
	return &cp
}

// Set 替换配置：normalize → 落盘 → 重建并发信号量。痕迹收敛（key 池缩小时
// 清理已移除 key 的记账/冷却/credits）在 save 之后做——PruneUsage 自拿
// keyMu，不能在 mu 内调（锁序约定：无 mu → keyMu）。
func Set(next *Config) {
	next.normalize()
	mu.Lock()
	cfg = next
	mu.Unlock()
	save()
	PruneUsage(next.Keys)
	// rebuildSemLocked 只读包级 cfg 与 semMu，不再取 mu，故无需持 mu。
	rebuildSemLocked()
}

// save 原子落盘（临时文件 + rename）。marshal 持 keyMu → mu.RLock（锁序
// 固定 keyMu → mu，与 KeyStatus 一致；无任何 mu → keyMu 反向路径）。
func save() {
	saveMu.Lock()
	defer saveMu.Unlock()
	if ConfigPath == "" {
		return
	}
	keyMu.Lock()
	mu.RLock()
	disk := *cfg
	mu.RUnlock()
	data, err := json.MarshalIndent(&disk, "", "  ")
	keyMu.Unlock()
	if err != nil {
		fmt.Printf("  freepi config marshal failed: %v\n", err)
		return
	}
	tmp := ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  freepi config save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, ConfigPath); err != nil {
		if werr := os.WriteFile(ConfigPath, data, 0600); werr != nil {
			fmt.Printf("  freepi config replace failed: %v\n", err)
			return
		}
		_ = os.Remove(tmp)
	}
}

// SetConfigUseProxies 改写配置级走池开关并落盘（2026-11-17 配置重组口径：
// 开关住平台自己的配置文件）。写时复制：拷贝顶层 Config 改字段后 Set。
func SetConfigUseProxies(v *bool) {
	next := CloneConfig(Get())
	next.UseProxies = v
	Set(next)
}

// CurrentUseProxies 当前走池开关。nil = 未配置 = 缺省走池。
func CurrentUseProxies() *bool { return Get().UseProxies }

// ProxiesEnabled 未绑定凭据是否走共享代理池：缺省（nil）默认走池。
func ProxiesEnabled() bool {
	up := CurrentUseProxies()
	if up == nil {
		return true
	}
	return *up
}

// KeyStatus 面板展示用的每 key 运行时状态（key 值打码，明文不出口）。
// token 记账/冷却/credits 在痕迹表（keyMu 域的就地写活 map），读它们必须
// 持 keyMu（Keys/KeyBindings 走写时复制，无锁读安全）。
func KeyStatus() []map[string]any {
	keyMu.Lock()
	defer keyMu.Unlock()
	now := time.Now()
	c := Get()
	u := usageLocked()
	out := make([]map[string]any, 0, len(c.Keys))
	for i, k := range c.Keys {
		st := map[string]any{
			"index":          i,
			"keyMask":        maskKey(k),
			"routingEnabled": c.RoutingEnabled(k),
			// 昵称（账号列堆叠展示）：空 = 显示掩码 key。
			"nickname": c.KeyNicknames[k],
		}
		// JWT 过期时间（状态列小字，照 TraeWork 的 token 过期行）
		if exp, ok := jwtExpFromToken(k); ok {
			st["jwtExpiresAt"] = exp.Format(time.RFC3339)
			st["jwtExpired"] = now.After(exp)
		}
		if u != nil {
			if ts, ok := u.KeyTokens[k]; ok {
				st["tokensToday"] = ts.Today
				st["tokensTotal"] = ts.Total
				if !ts.LastUsedAt.IsZero() {
					st["lastUsed"] = ts.LastUsedAt.Format(time.RFC3339)
				}
			}
			if cr, ok := u.Credits[k]; ok {
				st["creditsLeft"] = cr.Remaining
				st["creditsAllowance"] = cr.Allowance
				if cr.RefillAt != "" {
					st["creditsRefillAt"] = cr.RefillAt
				}
				if cr.Handle != "" {
					st["handle"] = cr.Handle
				}
			}
		}
		if b, ok := c.KeyBindings[k]; ok && (b.Main != "" || b.Backup != "") {
			st["proxyMain"] = b.Main
			st["proxyBackup"] = b.Backup
		}
		if u != nil {
			if until, ok := u.Cool[k]; ok && now.Before(until) {
				st["cooling"] = true
				st["cooldownUntil"] = until.Format(time.RFC3339)
			} else {
				st["cooling"] = false
			}
		} else {
			st["cooling"] = false
		}
		out = append(out, st)
	}
	return out
}

// BindingOf key 的出口绑定；未绑定时 bound=false。
func BindingOf(key string) (main, backup string, bound bool) {
	c := Get()
	b, ok := c.KeyBindings[key]
	if !ok || (b.Main == "" && b.Backup == "") {
		return "", "", false
	}
	return b.Main, b.Backup, true
}

// ============ 并发信号量 ============
//
// 平台级信号量（MaxConcurrency，防瞬时打满上游）。Set/Load 时重建；正在
// 飞行的请求持旧信号量完成，新请求换新信号量。per-key 的单请求排他
// （concurrent 闸门对策）在 pool.go 的 inFlight 表，与本信号量正交。

var semMu sync.Mutex
var sem chan struct{}

// rebuildSemLocked 重建平台并发信号量。调用方必须已持有 mu 写锁或根本未持
// 锁——Go 的 RWMutex 不可重入，持锁者再调 Get() 会在 RLock 上自死锁
// （Load 走的就是这条路径）。
func rebuildSemLocked() {
	semMu.Lock()
	defer semMu.Unlock()
	n := cfg.MaxConcurrency
	if n <= 0 {
		n = 8
	}
	sem = make(chan struct{}, n)
}

func semFor(n int) chan struct{} {
	semMu.Lock()
	defer semMu.Unlock()
	if sem == nil || cap(sem) != n {
		sem = make(chan struct{}, n)
	}
	return sem
}

// acquireSem 占一个并发槽位并返回该槽位所属信号量；ctx 取消时不占用。
// 释放必须用这里返回的 channel：Set 会整体重建，重查会释放到新表上永久阻塞。
func acquireSem(ctx context.Context, n int) (chan struct{}, bool) {
	s := semFor(n)
	select {
	case s <- struct{}{}:
		return s, true
	case <-ctx.Done():
		return nil, false
	}
}

func releaseSem(s chan struct{}) {
	if s != nil {
		<-s
	}
}

// ============ 凭据校验（参考 pingmike2/freepi-magpie-plugin 的 cleanToken/jwtShape）============

// CleanToken 清洗粘贴的凭据：剥离整行 env（FREEPI_TOKEN=eyJ…）、export 前缀、
// 引号、Bearer 前缀与空白，只留 JWT 本体。返回空串 = 没有可用的凭据。
func CleanToken(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			text = line
			break
		}
	}
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "export ")
	if i := strings.IndexByte(text, '='); i > 0 && isEnvName(text[:i]) {
		text = text[i+1:]
	}
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "Bearer ")
	text = strings.TrimPrefix(text, "bearer ")
	text = strings.Trim(text, "\"'`")
	text = strings.Join(strings.Fields(text), "")
	return text
}

// isEnvName 判定等号左侧是否形似环境变量名（FREEPI_TOKEN 等）。
func isEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// jwtExpFromToken 从 JWT payload 解析 exp（unix 秒）。解析失败/无 exp 返回
// false——过期时间是展示信息，缺失不影响路由，键的真伪仍由 /me 或首次请求裁决。
func jwtExpFromToken(key string) (time.Time, bool) {
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var payload struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(payload.Exp, 0), true
}

// ValidateJWT 粘贴时先报形状问题再发上游（参考 plugin jwtShape）：三段
// base64url、全 ASCII 可见、HS256 签名段 43 字符。返回 nil = 形状合法
// （内容真伪仍由 /me 或首次请求裁决）。
func ValidateJWT(key string) error {
	parts := strings.Split(key, ".")
	if len(parts) != 3 {
		return fmt.Errorf("that is not a JWT: it has %d parts, a token has 3", len(parts))
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return fmt.Errorf("that token has a non-ASCII character at position %d — a dash or quote was mangled on the way; copy it again straight from the CLI output", i+1)
		}
	}
	if len(parts[2]) != 43 {
		return fmt.Errorf("that token's signature is %d characters, a valid one is 43 — part of it was lost; copy it again straight from the CLI output", len(parts[2]))
	}
	return nil
}

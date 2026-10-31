package trae

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ============ 配置与数据模型 ============
//
// Trae 平台（trae: 前缀）：账号池形态（Cloud-IDE-JWT + refresh token，与
// Cline 账号同族），但自成独立池——凭据、token 记账、出口绑定都不与
// cline/zen/workbuddy 混用。复用主包的代理池与隔离语义：
//   - 每账号可绑主/备用代理，双不可用 = 本轮跳过（绝不回退直连）；
//   - 未绑定账号走全局规则（UseProxies，nil = 走池）；
//   - 新增账号默认不参与路由（ReconcilePoolEnabled，三平台统一口径）。

// Account 一个 Trae 账号。核心凭据是 Token（Cloud-IDE-JWT，三段式）；
// RefreshToken 用于过期前续期（POST {host}/cloudide/api/v3/trae/oauth/ExchangeToken）。
type Account struct {
	// ID 账号主键：JWT payload 内的用户 id（添加时服务端解析；解析失败用
	// token 前 24 字符兜底）。绑定/路由/冷却表都以它为键。
	ID string `json:"id"`
	// Label 面板显示名（可选，用户可改）。
	Label string `json:"label,omitempty"`
	// Token Cloud-IDE-JWT（核心凭据，请求头 Authorization: Cloud-IDE-JWT <token>）。
	Token string `json:"token"`
	// RefreshToken 刷新令牌（可为空：无则过期后只能人工换 token）。
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiredAt token 过期时间（RFC3339；空 = 未知，按长期有效处理）。
	ExpiredAt string `json:"expiredAt,omitempty"`
	// Host 该账号的 OAuth/账号域（刷新端点前缀；空 = 用网关域名）。
	Host string `json:"host,omitempty"`
	// Edition 版本：cn | sg（决定默认网关与国际区请求头），空 = cn。
	Edition string `json:"edition,omitempty"`
	// Enabled 账号总开关（面板"账号"列，对齐 WorkBuddy 双开关语义）：
	// false = 该账号视为不存在——不参与选号路由，签到/额度刷新等维护任务
	// 一并跳过。与 PoolEnabled 正交：PoolEnabled=false 只停会话路由，保号
	// 任务照跑。nil = 未记录 = 启用（存量兼容）。
	Enabled *bool `json:"enabled,omitempty"`
	// PoolEnabled 路由参与开关（面板"反代"列）：nil = 参与（存量兼容），
	// 新增账号落盘前置 false。约束：true 要求 Enabled=true（面板互锁）。
	PoolEnabled *bool `json:"poolEnabled,omitempty"`
	// ProxyMain / ProxyBackup 出口绑定（隔离模式：主优先备兜底，双不可用跳过）。
	ProxyMain   string `json:"proxyMain,omitempty"`
	ProxyBackup string `json:"proxyBackup,omitempty"`
	// Tokens（token 记账）/Credits（额度快照）/Checkin（签到状态）/
	// CheckinDeviceGen（签到设备代际）已拆出到独立 trae-state.json
	// （usage.go，2026-10-31，按账号 ID 键）：它们是随请求/签到/额度刷新变化
	// 的使用痕迹，留在配置文件里会干扰外部对配置变更的监测。凭据三件套
	//（Token/RefreshToken/ExpiredAt）保留在这里——续期回写属凭据轮转。
}

// CheckinState 每日签到状态（checkin_credits/status + claim 的投影）。
type CheckinState struct {
	// CheckedIn 今日是否已签到（9095 = 设备维度已签，同样视为已签）。
	CheckedIn bool `json:"checkedIn,omitempty"`
	// Date 签到归属日期（YYYY-MM-DD，跨日自动重签）。
	Date string `json:"date,omitempty"`
	// LastAt 最近一次签到动作时刻。
	LastAt time.Time `json:"lastAt,omitempty"`
	// Credits 签到得到的积分（本次 claim 返回值）。
	Credits float64 `json:"credits,omitempty"`
	// LastError 最近一次失败的文案（空 = 正常）。
	LastError string `json:"lastError,omitempty"`
	// Rotations 当日 9074 换设备次数（跨日清零；上限 checkin9074MaxRotations）。
	Rotations int `json:"rotations,omitempty"`
}

// TokenStats 单账号 token 记账。
type TokenStats struct {
	Today      int64     `json:"today,omitempty"`
	Total      int64     `json:"total,omitempty"`
	Date       string    `json:"date,omitempty"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}

// Credits 账号额度快照（ide_user_ent_usage 解析结果）。
type Credits struct {
	Remaining float64   `json:"remaining,omitempty"`
	Limit     float64   `json:"limit,omitempty"` // -1 = unlimited
	Unlimited bool      `json:"unlimited,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	// Identity 套餐身份（user_pay_identity_str，如 ProPlus）；空 = 未知。
	Identity string `json:"identity,omitempty"`
	// ParallelLimit 会话并发权益（entitlement pack quota 里的
	// solo_agent_parallel_limit，多个活跃 pack 取最大值；0 = 未知，闸门
	// 用保守默认）。对齐上游 trae_client.py 的 parallel_limits 聚合。
	ParallelLimit int `json:"parallelLimit,omitempty"`
}

// Config Trae 平台全部状态，落盘于 DATA_DIR/.trae-config.json。
type Config struct {
	// UseProxies 未绑定账号是否走共享代理池（代理池页下拉）。nil = 缺省 = 走池。
	UseProxies *bool `json:"useProxies,omitempty"`
	// Gateway 模型网关 base（raw_chat 端点前缀）。默认中国区网关；
	// api.trae.cn / trae-api.com.cn 等只有 OAuth 路由的域名会被 normalize 拒收。
	Gateway string `json:"gateway"`
	// Accounts 账号池（有序，面板按序号展示与重排）。
	Accounts []*Account `json:"accounts,omitempty"`
	// MaxConcurrency 上游最大并发，默认 8。
	MaxConcurrency int `json:"maxConcurrency"`
	// Retries 限流/网络错误重试次数（含账号轮换），默认 3。
	Retries int `json:"retries"`
	// Selection 选号策略：round_robin | credit-priority（额度多者优先，
	// unlimited 恒最优先；依赖后台额度刷新的数据）| fill | random。
	// 空 = 跟随全局账号池策略（POOL_STRATEGY，默认 round_robin）；TRAE_STRATEGY
	// 环境变量优先级更高（见 app.traeStrategy）。
	Selection string `json:"selection,omitempty"`	// Endpoint 上游端点（原项目 UPSTREAM_MODE 的三档收敛，面板下拉框）：
	//   - "" / auto（默认）：自动路由 = 合并目录 + 每模型自己的 Function 档
	//     走 raw_chat（TraeCode 桌面协议，历史行为不变）；
	//   - remote：chat_sessions 传输（solo_agent_remote 执行器，Agent 档）；
	//   - traecode：同 auto 的传输（raw_chat），仅模型目录按 TraeCode 档过滤；
	//   - traework：chat_sessions 传输（solo_work_remote 执行器，Work 档）。
	Endpoint string `json:"endpoint,omitempty"`
	// AutoCheckin 每日自动签到开关。nil = 关闭（与上游原版默认一致）。
	AutoCheckin *bool `json:"autoCheckin,omitempty"`
	// Cool 冷却表已拆出到独立 trae-state.json（usage.go，2026-10-31）：
	// 它是随 429/失败变化的使用痕迹，留在配置文件里会干扰外部对配置变更的
	// 监测。落盘持久化语义不变（2026-10-28 用户定）。
}

// DefaultGateway Trae 中国区模型网关（fallback api5-normal.mchost.guru 在
// client.go 的候选表里）。国际区账号（edition=sg）自动切 sgGateway。
const DefaultGateway = "https://trae-api-cn.mchost.guru"

// sgGateway 国际区（SG）模型网关。
const sgGateway = "https://a0ai-api-sg.byteintlapi.com"

// oauthOnlyHostFragments 只承载 OAuth/账号路由、不能作模型网关的域名片段
// （对这类 host 发 raw_chat 必失败，直接拒绝保存，免得整池静默 5xx）。
var oauthOnlyHostFragments = []string{"api.trae.cn", "api.trae.com.cn", "trae-api.com.cn", "oauth"}

// ModelPrefix 网关分发前缀：客户端以 "trae:<model-id>" 形式调用本平台模型。
const ModelPrefix = "trae:"

// DefaultConfig 出厂配置：空账号池、并发 8、重试 3、中国区网关。
func DefaultConfig() *Config {
	return &Config{
		Gateway:        DefaultGateway,
		MaxConcurrency: 8,
		Retries:        3,
	}
}

// normalize 常规化：网关去尾斜杠并拒收 OAuth-only 域、并发/重试下限修正、
// 账号字段清理（ID/token 空白修剪、ID 兜底）。
func (c *Config) normalize() {
	c.Gateway = strings.TrimRight(strings.TrimSpace(c.Gateway), "/")
	if c.Gateway == "" {
		c.Gateway = DefaultGateway
	}
	low := strings.ToLower(c.Gateway)
	for _, frag := range oauthOnlyHostFragments {
		if strings.Contains(low, frag) {
			c.Gateway = DefaultGateway
			break
		}
	}
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = 8
	}
	if c.Retries < 0 {
		c.Retries = 0
	}
	switch c.Selection {
	case "round_robin", "credit-priority", "fill", "random":
	default:
		c.Selection = ""
	}
	// 端点选择（面板下拉）：auto（空，默认）/ remote / traecode / traework。
	// 非法值回落 auto（原项目 UPSTREAM_MODE 校验同语义）。
	switch strings.ToLower(strings.TrimSpace(c.Endpoint)) {
	case "remote", "traecode", "traework":
		c.Endpoint = strings.ToLower(strings.TrimSpace(c.Endpoint))
	default:
		c.Endpoint = ""
	}
	cleaned := c.Accounts[:0]
	seen := map[string]bool{}
	for _, a := range c.Accounts {
		if a == nil {
			continue
		}
		a.Token = strings.TrimSpace(a.Token)
		a.RefreshToken = strings.TrimSpace(a.RefreshToken)
		a.Host = strings.TrimRight(strings.TrimSpace(a.Host), "/")
		a.ID = strings.TrimSpace(a.ID)
		if a.ID == "" {
			a.ID = TokenAccountID(a.Token)
		}
		if a.Token == "" || seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		cleaned = append(cleaned, a)
	}
	c.Accounts = cleaned
}

// ReconcilePoolEnabled 整体替换账号池后对齐路由参与表（保存账号列表时调用）：
// 新引入的账号默认不参与路由（面板勾选启用后生效），已有账号保持原状态，
// 被移除的账号由调用方清理表项。
func (c *Config) ReconcilePoolEnabled(next, cur *Config) {
	old := map[string]bool{}
	for _, a := range cur.Accounts {
		old[a.ID] = true
	}
	for _, a := range next.Accounts {
		if a.PoolEnabled == nil && !old[a.ID] {
			f := false
			a.PoolEnabled = &f
		}
	}
}

// PoolEnabledOrDefault 账号是否参与请求路由（仅看反代开关）。nil/缺字段 = 参与
// （存量兼容口径）。完整可路由判定用 Routable。
func (a *Account) PoolEnabledOrDefault() bool {
	return a.PoolEnabled == nil || *a.PoolEnabled
}

// EnabledOrDefault 账号总开关。nil/缺字段 = 启用（存量兼容口径）。
func (a *Account) EnabledOrDefault() bool {
	return a.Enabled == nil || *a.Enabled
}

// Routable 完整可路由判定：账号总闸开 且 反代开关开（对齐 WorkBuddy 的
// AccountEnabled() && PoolEnabled() 选号谓词）。
func (a *Account) Routable() bool {
	return a.EnabledOrDefault() && a.PoolEnabledOrDefault()
}

// TokenAccountID 从 JWT 提取账号主键：payload base64url 解码后优先 data.id，
// 其次 user_id/userId/sub；非 JWT 形状用 token 前 24 字符兜底。
func TokenAccountID(token string) string {
	t := strings.TrimSpace(token)
	parts := strings.Split(t, ".")
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" {
		if payload, err := jsonDecodeSegment(parts[1]); err == nil {
			for _, path := range []string{"data.id", "user_id", "userId", "sub"} {
				if v := digString(payload, strings.Split(path, ".")); v != "" {
					return v
				}
			}
		}
	}
	if len(t) > 24 {
		return t[:24]
	}
	return t
}

// LooksLikeCloudIDEJWT JWT 形状判别（三段式且足够长）。非 JWT 的 opaque
// 值不能用于模型网关，添加时直接拒绝。
func LooksLikeCloudIDEJWT(token string) bool {
	parts := strings.Split(strings.TrimSpace(token), ".")
	return len(strings.TrimSpace(token)) >= 128 && len(parts) == 3 &&
		parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// digString 按路径取嵌套 map 里的字符串。
func digString(m map[string]any, path []string) string {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	s, _ := cur.(string)
	return strings.TrimSpace(s)
}

// jsonDecodeSegment base64url 解码 JWT payload 段（自动补齐 padding）。
func jsonDecodeSegment(seg string) (map[string]any, error) {
	if b, err := base64RawURLDecode(seg); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			return m, nil
		}
	}
	return nil, fmt.Errorf("not a json segment")
}

// MaskToken 面板展示的凭据掩码（前 12 位 + 省略号；明文不出口）。
func MaskToken(t string) string {
	t = strings.TrimSpace(t)
	switch {
	case t == "":
		return "-"
	case len(t) <= 12:
		return t[:4] + "…"
	default:
		return t[:12] + "…"
	}
}

// ============ 存储 ============

var (
	mu     sync.RWMutex
	cfg    = DefaultConfig()
	saveMu sync.Mutex
	loaded bool
)

// ConfigPath 数据文件路径（主包装配期注入，指向 DATA_DIR/.trae-config.json）。
var ConfigPath string

// Load 读取持久化配置（装配期调用一次）。文件损坏改名留档，不静默覆盖。
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
			fmt.Printf("  trae config corrupt JSON; moved to %s.corrupt-%s\n", path, stamp)
		}
		mu.Unlock()
		return
	}
	cfg.normalize()
	mu.Unlock()
	// 痕迹加载/迁移在配置写锁外执行：迁移内部的 saveUsage 要拿 mu.RLock，
	// 写锁内调用会自死锁（2026-10-31 端到端实测发现并修复）。
	loadUsage()
}

// Get 返回活配置指针（写时复制语义由调用方保证：先拷贝再 Set）。
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return cfg
}

// Set 替换配置：normalize → 落盘 → 清理已移除账号的冷却/记账残留。
func Set(next *Config) {
	next.normalize()
	mu.Lock()
	cfg = next
	mu.Unlock()
	save()
	pruneRemovedAccounts(next)
}

// save 原子落盘（临时文件 + rename）。拆分后只写配置字段——Tokens/Credits/
// Checkin/Cool 等使用痕迹走 usage.go 的 saveUsage 独立落盘。
// marshal 全程持 mu.RLock：Accounts 是活指针切片，无锁 marshal 会与写路径
// 构成数据竞争（2026-10-29 审计修复口径）。
func save() {
	saveMu.Lock()
	defer saveMu.Unlock()
	if ConfigPath == "" {
		return
	}
	mu.RLock()
	data, err := json.MarshalIndent(cfg, "", "  ")
	mu.RUnlock()
	if err != nil {
		fmt.Printf("  trae config marshal failed: %v\n", err)
		return
	}
	tmp := ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  trae config save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, ConfigPath); err != nil {
		if werr := os.WriteFile(ConfigPath, data, 0600); werr != nil {
			fmt.Printf("  trae config replace failed: %v\n", err)
			return
		}
		_ = os.Remove(tmp)
	}
}

// pruneRemovedAccounts 清理不再在池中的账号的痕迹/冷却条目。
func pruneRemovedAccounts(c *Config) {
	valid := map[string]bool{}
	for _, a := range c.Accounts {
		valid[a.ID] = true
	}
	mu.Lock()
	defer mu.Unlock()
	pruneUsageLocked(valid)
}

// CloneConfig 写时复制的克隆对（app 层各池操作同模式）：cur 供 diff 基准，
// next 为待改副本。改 next 后调 Set 落盘。使用痕迹不随 Config 对象复制
//（跟账号 ID 走，住在痕迹文件侧）。
func CloneConfig() (cur, next *Config) {
	c := Get()
	cur = c
	next = &Config{
		UseProxies:     c.UseProxies,
		Gateway:        c.Gateway,
		Selection:      c.Selection,
		AutoCheckin:    c.AutoCheckin,
		MaxConcurrency: c.MaxConcurrency,
		Retries:        c.Retries,
	}
	if c.Accounts != nil {
		next.Accounts = make([]*Account, len(c.Accounts))
		for i, a := range c.Accounts {
			cp := *a
			next.Accounts[i] = &cp
		}
	}
	return cur, next
}

// FindAccount 按主键取账号（池内指针，调用方只读）。
func FindAccount(id string) *Account {
	for _, a := range Get().Accounts {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// ProxiesEnabled 未绑定账号是否走共享代理池：缺省（nil）默认走池。

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

// GatewayFor 账号使用的模型网关：账号 host 是模型网关域则优先（高级用法），
// 国际区账号用 SG 网关，其余用全局 Gateway 设置。
func GatewayFor(a *Account) string {
	if a != nil && a.Host != "" {
		low := strings.ToLower(a.Host)
		oauthOnly := false
		for _, frag := range oauthOnlyHostFragments {
			if strings.Contains(low, frag) {
				oauthOnly = true
				break
			}
		}
		if !oauthOnly {
			return strings.TrimRight(a.Host, "/")
		}
	}
	if a != nil && (a.Edition == "sg" || a.Edition == "solo-sg") {
		return sgGateway
	}
	return Get().Gateway
}

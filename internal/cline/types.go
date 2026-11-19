package cline

import "time"

// Account cline 账号的配置态（cline-config.json）。使用痕迹（Status/用量
// 计数/冷却等）住 cline-state.json 的 AccountUsage（pool_usage.go，按
// AccountID 键，2026-10-31 拆分）——配置文件恢复"仅面板操作/导入才变"，
// 外部监测配置变更不被高频记账噪声淹没。
type Account struct {
	AccountID string `json:"accountId"`
	// Nickname 可选显示名（面板账号列）：设置后账号列优先显示昵称、小字
	// 保留邮箱；空 = 未设置（显示邮箱）。仅影响面板展示与日志标签，不参与
	// 选号路由。随 cline-config.json 与配置备份一起持久化。
	Nickname     string `json:"nickname,omitempty"`
	RefreshToken string `json:"refreshToken"`
	// APIToken 静态 API key（sk_...）账号：设置后直接作为 Bearer 使用，
	// 不刷新（无 refresh token）；json 序列化持久化，重启后仍生效。
	// 优先级高于 RefreshToken/AccessToken。
	APIToken    string `json:"apiToken,omitempty"`
	AccessToken string `json:"-"`
	ExpiresAt   int64  `json:"-"`
	// 代理绑定（账号隔离，见 proxy_binding.go）：主代理优先，备用代理兜底；
	// 两者都不可用（冷却中/已从池中删除）时该账号在选号阶段被整体跳过 ——
	// 隔离优先于可用性，绝不退回其他出口。空 = 未绑定，沿用全局代理规则。
	// 隔离开关关闭（旧版全局轮转）时两个字段被完全忽略。
	ProxyMain   string `json:"proxyMain,omitempty"`
	ProxyBackup string `json:"proxyBackup,omitempty"`
	// PoolEnabled 是“参与轮换”的显式开关，与 active/cooldown/expired 状态
	// 分开表达。nil（旧数据或未选择）按启用处理；只有显式 false 才退出主选号，
	// 只读接口仍按 Status 使用账号。
	PoolEnabled *bool `json:"poolEnabled,omitempty"`
}

// AccountUsage 账号使用痕迹（cline-state.json，按 AccountID 键）：状态、
// 用量计数、冷却。与 Account 分离后配置文件不再随请求高频变化。
type AccountUsage struct {
	Status          string    `json:"status"` // active, cooldown, expired
	LastUsed        time.Time `json:"lastUsed"`
	UsageCount      int64     `json:"usageCount"`      // 本地累计成功调用次数
	UsageCountToday int64     `json:"usageCountToday"` // 本地今日成功调用次数
	UsageDate       string    `json:"usageDate"`       // 本地计数日期 YYYY-MM-DD，跨日自动重置
	TokensTotal     int64     `json:"tokensTotal"`     // 本地累计 token 消耗（prompt+completion）
	TokensToday     int64     `json:"tokensToday"`     // 本地今日 token 消耗
	TokensDate      string    `json:"tokensDate"`      // 今日 token 计数日期 YYYY-MM-DD，跨日自动重置
	CooldownUntil   time.Time `json:"cooldownUntil,omitempty"`
	LastReason      string    `json:"lastReason,omitempty"`
}

// PoolUsageData cline-state.json 的落盘形态：轮转游标 + 每账号痕迹表。
type PoolUsageData struct {
	// CurrentIdx round-robin 轮转游标（按 active 候选序）。随选号高频变化，
	// 拆出配置文件（2s flusher 只写本文件）。
	CurrentIdx int `json:"currentIdx"`
	// Accounts 账号 ID -> 痕迹表。
	Accounts map[string]*AccountUsage `json:"accounts,omitempty"`
}

// AccountView 面板账号列表行（GET /admin/api/accounts 输出）：Account 配置态
// 与 AccountUsage 痕迹的合成视图，JSON 键与拆分前 Account 序列化逐字一致
// （前端零改动）。
type AccountView struct {
	AccountID string `json:"accountId"`
	Nickname  string `json:"nickname,omitempty"`
	// 账号列改造（2026-11-19）：凭据类型与掩码随行下发。api=静态 key（sk_，
	// 面板可编辑明文）；oauth=refreshToken（只读展示、不可编辑）。掩码与
	// other.MaskKey 同口径（前 12 字符 + …），明文永不出网关。
	CredType string `json:"credType,omitempty"`
	CredMask string `json:"credMask,omitempty"`
	// 痕迹段（原 Account 字段原样搬移）
	Status          string    `json:"status"`
	LastUsed        time.Time `json:"lastUsed"`
	UsageCount      int64     `json:"usageCount"`
	UsageCountToday int64     `json:"usageCountToday"`
	UsageDate       string    `json:"usageDate"`
	TokensTotal     int64     `json:"tokensTotal"`
	TokensToday     int64     `json:"tokensToday"`
	TokensDate      string    `json:"tokensDate"`
	CooldownUntil   time.Time `json:"cooldownUntil,omitempty"`
	LastReason      string    `json:"lastReason,omitempty"`
	// 配置段
	ProxyMain   string `json:"proxyMain,omitempty"`
	ProxyBackup string `json:"proxyBackup,omitempty"`
	PoolEnabled *bool  `json:"poolEnabled,omitempty"`
}

type AccountPool struct {
	Accounts []*Account `json:"accounts"`
	// UseProxies 走池开关（2026-11-17 配置重组迁回平台文件：原 proxy-config.json
	// 的 clineUseProxies）。nil = 未配置 = 走池（与其他平台同约定）。
	UseProxies *bool `json:"useProxies,omitempty"`
	// StreamRequired cline「必须流式」自学习表（2026-11-17 从 afree-config.json
	// 迁入：平台私有数据归平台文件）。键 = 对外模型 ID，值 = 学习依据。
	StreamRequired map[string]string `json:"streamRequired,omitempty"`
	// CatalogFeedOnly cline 目录「仅展示插件免费」开关（面板显示偏好，
	// 2026-11-17 从 afree-config.json 迁入）。nil = 关闭（展示全部免费目录）。
	CatalogFeedOnly *bool `json:"catalogFeedOnly,omitempty"`
	// Headers 是模拟 Cline CLI 的自定义请求头（面板保存，重启回放）。
	// 账号调度策略、/admin 日志开关在 afree-config.json（跨平台语义）。
	// CurrentIdx 轮转游标已拆到 cline-state.json（2026-10-31），旧文件里的
	// currentIdx 键由 encoding/json 静默忽略。
	Headers map[string]string `json:"headers,omitempty"`
}

type LoginMethod int

const (
	MethodDeviceOAuth LoginMethod = iota
	MethodRefreshToken
	MethodSSOCookie
)

// maskCredential 凭据掩码（账号列堆叠展示用，2026-11-19）：前 12 字符 + …，
// 与 other.MaskKey 同口径；空串返回空。明文永不出网关。
func maskCredential(k string) string {
	switch {
	case k == "":
		return ""
	case len(k) <= 12:
		return k[:4] + "…"
	default:
		return k[:12] + "…"
	}
}

// AccountLabel 日志与错误文案里的账号标识（2026-11-20 Email 字段删除后的
// 替代）：昵称优先，否则账号 ID（截 12 位）。
func AccountLabel(acc *Account) string {
	if acc == nil {
		return "?"
	}
	if acc.Nickname != "" {
		return acc.Nickname
	}
	id := acc.AccountID
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

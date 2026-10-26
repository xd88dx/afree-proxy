package app

import "time"

type Account struct {
	AccountID string `json:"accountId"`
	Email     string `json:"email"`
	// Nickname 可选显示名（面板账号列）：设置后账号列优先显示昵称、小字
	// 保留邮箱；空 = 未设置（显示邮箱）。仅影响面板展示与日志标签，不参与
	// 选号路由。随 cline-config.json 与配置备份一起持久化。
	Nickname     string `json:"nickname,omitempty"`
	RefreshToken string `json:"refreshToken"`
	// APIToken 静态 API key（sk_...）账号：设置后直接作为 Bearer 使用，
	// 不刷新（无 refresh token）；json 序列化持久化，重启后仍生效。
	// 优先级高于 RefreshToken/AccessToken。
	APIToken        string    `json:"apiToken,omitempty"`
	AccessToken     string    `json:"-"`
	ExpiresAt       int64     `json:"-"`
	Status          string    `json:"status"` // active, cooldown, expired
	LastUsed        time.Time `json:"lastUsed"`
	UsageCount      int64     `json:"usageCount"`              // 本地累计成功调用次数
	UsageCountToday int64     `json:"usageCountToday"`         // 本地今日成功调用次数
	UsageDate       string    `json:"usageDate"`               // 本地计数日期 YYYY-MM-DD，跨日自动重置
	TokensTotal     int64     `json:"tokensTotal"`             // 本地累计 token 消耗（prompt+completion）
	TokensToday     int64     `json:"tokensToday"`             // 本地今日 token 消耗
	TokensDate      string    `json:"tokensDate"`              // 今日 token 计数日期 YYYY-MM-DD，跨日自动重置
	CooldownUntil   time.Time `json:"cooldownUntil,omitempty"` // 预计冷却结束时间
	LastReason      string    `json:"lastReason,omitempty"`    // 最后一次进入冷却/失效的原因
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
	// EgressProxy / NoEgress 仅供管理面板展示：该账号绑定的出口（已打码），
	// 或"代理池已用尽、本账号在代理模式下被跳过"。绑定本身持久化在
	// .egress-bindings.json，不进账号池文件。
	EgressProxy string `json:"egressProxy,omitempty"`
	NoEgress    bool   `json:"noEgress,omitempty"`
}

type AccountPool struct {
	Accounts   []*Account `json:"accounts"`
	CurrentIdx int        `json:"currentIdx"`
	// ClineUseProxies 只用于读旧 .cline-accounts.json 做一次性迁移；
	// 开关本体存在 proxy-config.json 的代理池段（见 platform_proxies.go），
	// 本字段永远以 nil 落盘，omitempty 下不会出现在 cline-config.json 里。
	ClineUseProxies *bool `json:"clineUseProxies,omitempty"`
	// Headers 是模拟 Cline CLI 的自定义请求头（面板保存，重启回放）。
	// 客户端 key、账号调度策略、/admin 日志开关都在 afree-config.json。
	Headers map[string]string `json:"headers,omitempty"`
}

type LoginMethod int

const (
	MethodDeviceOAuth LoginMethod = iota
	MethodRefreshToken
	MethodSSOCookie
)

package gwconfig

// types.go afree-config.json 全局段的数据形态（P4：数据模型归配置主人——
// Combo/黑名单条目类型迁本包，判定逻辑留各域包）。
import "time"

// Combo 仪表盘自定义的别名模型：/v1 请求 model 命中 combo ID 时，
// 上游改写为同平台的 target 模型。严格同平台：cline/zen/workbuddy
// 只能选各自平台当前模型列表中的模型，保存时校验。
type Combo struct {
	ID         string    `json:"id"`
	Platform   string    `json:"platform"` // cline | zen | workbuddy | trae
	Target     string    `json:"target"`
	UseProxies bool      `json:"useProxies,omitempty"` // 该别名的上游调用走出口代理池（round-robin）
	CreatedAt  time.Time `json:"createdAt"`
}

// ModelBlacklistEntry 一条拉黑记录。Model 存**对外模型 ID**（含前缀，
// 如 wbgb:gpt-6-luna），与 /v1/models 的 id 同口径，过滤时直接比对。
// 无 scope 维度（2026-11-16 wb 化：trae 合并域由 tacn:/tagb: 对外前缀区分）。
type ModelBlacklistEntry struct {
	Platform  string    `json:"platform"`
	Model     string    `json:"model"`
	Code      string    `json:"code,omitempty"`   // 上游业务码，如 11158
	Reason    string    `json:"reason"`           // 统一文案：自动/手动封禁
	Detail    string    `json:"detail,omitempty"` // 上游原始报文（悬停提示用，不进表格列）
	CreatedAt time.Time `json:"createdAt"`        // 加入黑名单时间
	LastAt    time.Time `json:"lastAt"`           // 最近一次命中
	Hits      int       `json:"hits"`             // 命中次数
}

type PlatformBanEntry struct {
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"createdAt"`
}

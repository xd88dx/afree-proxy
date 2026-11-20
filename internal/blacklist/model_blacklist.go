package blacklist

import (
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/workbuddy"

	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/kit"
	"afree-proxy/internal/other"
)

// ============================================================================
// 模型黑名单（afree 本地功能）
//
// 背景：上游会返回「账号类型不支持该模型」这类**权限级**错误（WorkBuddy global
// 实测 code=11158：「model [gpt-6-luna] is not allowed for user type
// [personal_free_user]」）。它的性质是：
//
//   - 不是账号坏了（余额/会话都健康），换号重试没有意义——同等级的号都没权限；
//   - 不是上游抖动，重试一百次也是同一个答复；
//   - 但 /v1/models 照样把它列出来，客户端（含 new-api 这类渠道映射）会反复
//     选到它，每次都吃一次 503 与一轮无谓的凭据轮转。
//
// 处理：命中判据后把「平台 + 模型」拉黑 —— /v1/models 不再对外提供（客户端
// 选不到），面板「收费模型」区可见、可人工测试/解禁。解禁只是移出名单，
// 程序下次遇到同样原因仍会再次拉黑（不是永久豁免）。
//
// 落盘 DATA_DIR/.model-blacklist.json：拉黑是低频事件，即时落盘（不设周期）。
// ============================================================================

// 封禁原因统一文案（面板「封禁原因」列直接展示，不再放上游原话——
// 上游原始报文保留在 Detail 字段，悬停提示可见）。英文源文 + 面板 I18N
// 词条双语（2026-10-29 汉化遗留修复）；存量中文文案由 normalizeBanReason
// 在加载/导入时归一。
const (
	BanReasonAuto   = "auto-banned: account tier lacks access to this model"
	BanReasonManual = "banned manually by admin"
	// BanReasonModelGone 模型名在 TraeCode 网关上不存在（4023）：不是权限问题，
	// 而是目录给的名字发不出请求（web-only/旧名），换任何账号同样报错。
	BanReasonModelGone = "auto-banned: model name not available on this gateway (4023)"
	// BanReasonFeedRemoved 官方免费目录下线：cline 的 recommended-models free
	// feed 已把该模型移走（2026-10-31 实测移入付费 clinePass 档），免费账号池
	// 请求固定 404 model not found。性质与 BanReasonAuto（账号档位不够）不同
	// ——换更高权益的账号池也救不回来，只能等官方把它放回免费档或人工解禁。
	BanReasonFeedRemoved = "auto-banned: model removed from official free feed"
)

// 平台标识（与对外模型前缀对应）。
const (
	PlatWorkBuddy = "workbuddy"
	PlatZen       = "zen"
	PlatCline     = "cline"
	PlatTrae      = "trae"
	PlatOther     = "other"
	PlatFreepi    = "freepi"
)

var (
	ModelBlacklistMu     sync.Mutex
	ModelBlacklistItems  = map[string]*gwconfig.ModelBlacklistEntry{}
	ModelBlacklistLoaded bool
)

func BlacklistKey(platform, model string) string {
	return platform + "|" + model
}

func EnsureModelBlacklistLoaded() {
	ModelBlacklistMu.Lock()
	defer ModelBlacklistMu.Unlock()
	if ModelBlacklistLoaded {
		return
	}
	ModelBlacklistLoaded = true
	gwconfig.GlobalCfgMu.Lock()
	g := gwconfig.LoadGlobalConfigLocked()
	if len(g.ModelBlacklist) > 0 {
		ModelBlacklistItems = make(map[string]*gwconfig.ModelBlacklistEntry, len(g.ModelBlacklist))
		for k, e := range g.ModelBlacklist {
			ModelBlacklistItems[k] = e
		}
	}
	gwconfig.GlobalCfgMu.Unlock()
	if n := len(ModelBlacklistItems); n > 0 {
		log.Printf("model blacklist: restored %d entry(ies) from %s", n, gwconfig.GlobalConfigPath())
	}
}

func FlushModelBlacklist() {
	ModelBlacklistMu.Lock()
	out := make(map[string]*gwconfig.ModelBlacklistEntry, len(ModelBlacklistItems))
	for k, e := range ModelBlacklistItems {
		cp := *e
		out[k] = &cp
	}
	ModelBlacklistMu.Unlock()

	gwconfig.MutateGlobalConfig(func(g *gwconfig.GlobalConfigData) { g.ModelBlacklist = out })
}

// BlacklistModel 把「平台 + 对外模型 ID」加入黑名单（自动拉黑路径，reason 传
// 统一文案 BanReasonAuto，detail 传上游原始报文）。已存在时只累加命中次数与
// 刷新最近时间（不重置 createdAt——面板展示的是首次被拉黑的时间）。
func BlacklistModel(platform, model, code, reason, detail string) {
	BlacklistHit(platform, model, code, reason, detail)
}

// BanModel 管理员手动封禁：原因统一为 BanReasonManual。已存在时覆盖原因
// （手动封禁表达的是管理员最新意志）并累加命中次数。返回是否为新加入。
func BanModel(platform, model string) bool {
	platform = strings.TrimSpace(platform)
	model = strings.TrimSpace(model)
	if platform == "" || model == "" {
		return false
	}
	EnsureModelBlacklistLoaded()
	ModelBlacklistMu.Lock()
	k := BlacklistKey(platform, model)
	isNew := ModelBlacklistItems[k] == nil
	BlacklistHitLocked(k, platform, model, "", BanReasonManual, "")
	ModelBlacklistMu.Unlock()
	FlushModelBlacklist()
	if isNew {
		log.Printf("model blacklist: +%s %s (manual ban)", platform, model)
	}
	return isNew
}

// BlacklistHit 写入命中（唯一写入口）。已存在时累加命中次数与刷新时间。
func BlacklistHit(platform, model, code, reason, detail string) {
	platform = strings.TrimSpace(platform)
	model = strings.TrimSpace(model)
	if platform == "" || model == "" {
		return
	}
	EnsureModelBlacklistLoaded()
	ModelBlacklistMu.Lock()
	BlacklistHitLocked(BlacklistKey(platform, model), platform, model, code, reason, detail)
	ModelBlacklistMu.Unlock()
	FlushModelBlacklist()
}

// BlacklistHitLocked 实际写入（调用方须持有 ModelBlacklistMu）。
func BlacklistHitLocked(k, platform, model, code, reason, detail string) {
	now := time.Now()
	if e := ModelBlacklistItems[k]; e != nil {
		e.Hits++
		e.LastAt = now
		if code != "" {
			e.Code = code
		}
		if reason != "" {
			e.Reason = kit.Truncate(reason, 300)
		}
		if detail != "" {
			e.Detail = kit.Truncate(detail, 2000)
		}
		return
	}
	ModelBlacklistItems[k] = &gwconfig.ModelBlacklistEntry{
		Platform:  platform,
		Model:     model,
		Code:      code,
		Reason:    kit.Truncate(reason, 300),
		Detail:    kit.Truncate(detail, 2000),
		CreatedAt: now,
		LastAt:    now,
		Hits:      1,
	}
	log.Printf("model blacklist: +%s %s (%s) %s", platform, model, code, kit.Truncate(reason, 120))
}

// IsModelBlacklisted 该对外模型 ID 是否已拉黑（按对外 ID 判，跨平台无歧义：
// 同一 ID 只可能属于一个平台）。
func IsModelBlacklisted(model string) bool {
	if model == "" {
		return false
	}
	EnsureModelBlacklistLoaded()
	ModelBlacklistMu.Lock()
	defer ModelBlacklistMu.Unlock()
	for _, e := range ModelBlacklistItems {
		if e.Model == model {
			return true
		}
	}
	return false
}

// UnblockModel 解禁：移出名单。仅此而已——后续若再次命中同样原因，会重新
// 被拉黑（不是永久豁免）。按 (platform, model) 精确键删除。
func UnblockModel(platform, model string) bool {
	EnsureModelBlacklistLoaded()
	ModelBlacklistMu.Lock()
	k := BlacklistKey(platform, model)
	_, existed := ModelBlacklistItems[k]
	delete(ModelBlacklistItems, k)
	ModelBlacklistMu.Unlock()
	if existed {
		log.Printf("model blacklist: -%s %s (manual unblock)", platform, model)
		FlushModelBlacklist()
	}
	return existed
}

// TraeModelBannedIn trae 平台模型在指定合并域（tacn/tagb）是否被封禁
// （/v1/models 摘除用）。封禁无 scope 维度（2026-11-16 wb 化）：对外 ID
// tacn:<bare>/tagb:<bare> 本身编码域，等值匹配即可区分——国内版拉黑不会
// 连带国际版同 bare 模型（两域上游独立供给）。
func TraeModelBannedIn(merged, bare string) bool {
	return IsModelBlacklisted(merged + ":" + bare)
}

// ModelBlacklistSnapshot 面板表格数据（按加入时间倒序）。
func ModelBlacklistSnapshot() []map[string]any {
	EnsureModelBlacklistLoaded()
	ModelBlacklistMu.Lock()
	rows := make([]map[string]any, 0, len(ModelBlacklistItems))
	for _, e := range ModelBlacklistItems {
		rows = append(rows, map[string]any{
			"platform":  e.Platform,
			"model":     e.Model,
			"code":      e.Code,
			"reason":    e.Reason,
			"detail":    e.Detail,
			"createdAt": e.CreatedAt.UTC().Format(time.RFC3339),
			"lastAt":    e.LastAt.UTC().Format(time.RFC3339),
			"hits":      e.Hits,
		})
	}
	ModelBlacklistMu.Unlock()
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i]["createdAt"].(string) > rows[j]["createdAt"].(string)
	})
	return rows
}

// ============================================================================
// 权限级错误判据
// ============================================================================

// ModelPermissionDeniedMarkers 上游「账号类型/等级不支持该模型」的确定性文案。
// 命中即拉黑：这类答复换号、重试都不会变（同等级凭据都没有权限）。
var ModelPermissionDeniedMarkers = []string{
	"is not allowed for user type",
	"does not support this model",
	"account type does not support",
	"not available for your account type",
}

// IsModelPermissionDenied 判断上游响应是否属于「账号类型不支持该模型」。
// 判据：业务码 11158（WorkBuddy 实测）或文案命中 marker。
func IsModelPermissionDenied(status int, body string) bool {
	if body == "" {
		return false
	}
	if strings.Contains(body, "11158") {
		return true
	}
	lower := strings.ToLower(body)
	for _, m := range ModelPermissionDeniedMarkers {
		if strings.Contains(lower, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// Flush 透传，保证流式响应行为不变（包装层不能把 Flusher 吃掉）。
// finish 请求结束后判定并拉黑（命中一次即够，重复命中只累加次数）。
// BatchBanPlatformModels 批量封禁：平台目录里 ID 包含关键字（大小写不敏感）
// 的模型全部加入黑名单。已在名单里的条目保持原样（不覆盖自动封禁原因）。
// 返回命中数与新封禁的 ID 列表。
func PlatformOfModel(model string) string {
	switch {
	case strings.HasPrefix(model, workbuddy.WbRealmPrefixCN), strings.HasPrefix(model, workbuddy.WbRealmPrefixGlobal),
		strings.HasPrefix(model, workbuddy.VendorRealmPrefixCN), strings.HasPrefix(model, workbuddy.VendorRealmPrefixGlobal):
		return PlatWorkBuddy
	case strings.HasPrefix(model, "zen:"):
		return PlatZen
	case strings.HasPrefix(model, "cline-free/"):
		// cline 免费目录的对外前缀（seed/live 目录里带前缀的条目）。裸名
		// （z-ai/glm-5.3-flash 等 live feed 直出的 ID）仍返回空串不猜。
		return PlatCline
	case trae.Trae2apiPlatformModel(model):
		return PlatTrae
	case strings.HasPrefix(model, "freepi:"):
		return PlatFreepi
	}
	if i := strings.IndexByte(model, ':'); i > 0 {
		if p := other.PlatformByPrefix(model[:i]); p != nil {
			return PlatOther
		}
	}
	return ""
}

// ModelErrorCapture 只捕获错误响应体的 ResponseWriter 包装。
func BusinessCodeOf(body string) string {
	candidates := []string{body, strings.ReplaceAll(body, `\"`, `"`)}
	for _, src := range candidates {
		if code := BusinessCodeIn(src); code != "" {
			return code
		}
	}
	return ""
}

func BusinessCodeIn(body string) string {
	for _, key := range []string{`"code":`, `"code" :`} {
		off := 0
		for {
			i := strings.Index(body[off:], key)
			if i < 0 {
				break
			}
			i += off
			rest := strings.TrimLeft(body[i+len(key):], " ")
			j := 0
			for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			if j > 0 {
				return rest[:j]
			}
			// 网关信封里先出现的是字符串型业务码（如 "code":"no_healthy_account"），
			// 继续往后找第一个数字型业务码（内嵌上游 JSON 里的 11158）。
			off = i + len(key)
		}
	}
	return ""
}

// WithModelBlacklistScan 在网关入口统一挂自动拉黑：先读出请求体取 model
// （用于判平台与记录），再把可捕获错误体的 writer 交给下游 handler。
// 同一入口顺带挂**封禁平台闸**（platform_blacklist.go）：平台被管理员封禁后
// 不参与任何会话路由，命中直接 503——放在读出 model 之后、分发之前，三个
// /chat/completions、/messages、/responses 入口一处收口。判定口径：combo 别名
// 按声明平台、带前缀按前缀平台、裸名按 Cline（RoutePlatformKeyOf；裸名 zen 兼容
// 解析是旧形态遗留，封 Cline 时一并被闸属 fail-closed 的可接受代价）。

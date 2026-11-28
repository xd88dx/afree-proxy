package gateway

// 平台路由分类（gateway 批自 app/blacklist_gateway.go 迁入）：模型将被路由到
// 哪个平台的判定（combo 别名按声明平台，其余按对外 ID 前缀）。封禁/闸面共用。

import (
	"strings"

	"afree-proxy/internal/blacklist"
	"afree-proxy/internal/other"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/workbuddy"
)

func RoutePlatformKeyOf(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		// 空模型名交给入口的 400（model is required）：落到这里会被误判成
		// Cline 裸名，再撞上可路由性闸报成"平台无可用凭据"，排查方向被带偏。
		return ""
	}
	switch {
	case strings.HasPrefix(model, workbuddy.WbRealmPrefixCN), strings.HasPrefix(model, workbuddy.WbRealmPrefixGlobal),
		strings.HasPrefix(model, workbuddy.VendorRealmPrefixCN), strings.HasPrefix(model, workbuddy.VendorRealmPrefixGlobal):
		return blacklist.PlatWorkBuddy
	case strings.HasPrefix(model, "opcd:"), strings.HasPrefix(model, "zen:"):
		// opcd: 现行前缀（2026-11-28 由 zen: 改名）；zen: 旧形态仅用于存量
		// 数据/日志分类，对外请求面由目录闸硬切。
		return blacklist.PlatZen
	case trae.Trae2apiPlatformModel(model):
		return blacklist.PlatTrae
	case strings.HasPrefix(model, "freepi:"):
		return blacklist.PlatFreepi
	case strings.HasPrefix(model, "cline:"):
		// cline: 新对外前缀（2026-11-28 由 cline-free/ 改名）。cline-free/
		// 旧形态与裸名一样落冒号分支之外的兜底（无冒号 → Cline）。
		return blacklist.PlatCline
	}
	if i := strings.IndexByte(model, ':'); i > 0 {
		if p := other.PlatformByPrefix(model[:i]); p != nil {
			return "other:" + p.Slug
		}
		return ""
	}
	// 裸名：Cline 免费目录（zen 裸名兼容解析是旧形态遗留，封 Cline 时一并
	// 被闸住是 fail-closed 的可接受代价，见 WithModelBlacklistScan 闸注释）。
	return blacklist.PlatCline
}

// ModelRoutePlatformKey 模型将被路由到的平台键：combo 别名按声明平台，
// 其余按对外 ID 前缀（裸名 = Cline）。网关入口的可路由性闸用它判定；
// 不用 RoutePlatformKeyOf 直接判 combo（别名不带前缀，会被误判成 Cline）。
func ModelRoutePlatformKey(model string) string {
	if c := resolveCombo(model); c != nil {
		return c.Platform
	}
	return RoutePlatformKeyOf(model)
}

// BannedPlatformOf 返回模型将被路由到的平台封禁键（combo 别名按其声明平台、
// 其余按模型 ID 前缀判，裸名 = Cline）；该平台未封禁或无法判定时返回 ""。
// /v1/models 过滤与网关入口闸共用本判定。
func BannedPlatformOf(model string) string {
	if model == "" {
		return ""
	}
	plat := ""
	if c := resolveCombo(model); c != nil {
		plat = c.Platform
	} else {
		plat = RoutePlatformKeyOf(model)
	}
	if plat == "" || !blacklist.IsPlatformBanned(plat) {
		return ""
	}
	return plat
}

// RoutePlatformKeyOf / ModelRoutePlatformKey / BannedPlatformOf 导出面
// （装配_root 封禁端点与备份消费）。
func RoutePlatformKeyOfExport(model string) string    { return RoutePlatformKeyOf(model) }
func ModelRoutePlatformKeyExport(model string) string { return ModelRoutePlatformKey(model) }
func BannedPlatformOfExport(model string) string      { return BannedPlatformOf(model) }

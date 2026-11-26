package exitpool

// ============================================================================
// 出口区域（zone，2026-11-26 用户定语义修正）
//
// 出口两个区域值：国内（cn）/ 不限（空）。"国内保留"只约束声明了国内域的
// 平台（zone.go 下半部声明表）：
//
//	凭据域 \ 出口区域 |  cn（国内） | 空（不限）
//	cn（声明国内）      ✓ 独占使用    手动可绑（不参与自动化）
//	intl（声明国际）    ✗ 不分配      ✓
//	无域（未声明平台）  ✓ 当普通出口  ✓
//
// 即：cline 等未声明平台把国内出口当普通出口正常用；只有声明了国内域的
// 凭据才专门使用国内出口（且完全不参与一键代理/重排/自动排除——手动绑定）。
// 手动绑定不受限（面板仍可把任意出口绑给任意凭据）。
//
// 2026-11-26 起 global（国际保留出口）语义废除：区域下拉只有 不限/国内 两档，
// 存量 global 值由 PruneZones 归空删除。
// ============================================================================

// ZoneCN 国内保留出口。
const ZoneCN = "cn"

// NormalizeZone 区域值收敛：仅 cn 有效；global 已废除（2026-11-26 用户定，
// 区域只有 不限/国内 两档），空串与其他值一律返回空（不限）。
func NormalizeZone(z string) string {
	if z == ZoneCN {
		return ZoneCN
	}
	return ""
}

// ZoneOf 出口的区域标签（未打标 = 空 = 不限）。键为备注名（订阅换端口
// 规则不失配，2026-11-26 用户定备注名主键）。
func ZoneOf(proxyURL string) string {
	return NormalizeZone(GetPool().Zones[RuleKeyOf(proxyURL)])
}

// ZoneCompatible 凭据域与出口区域是否兼容（自动分配候选判定）。
// credRealm：cn（声明国内）/ global 或 intl（声明国际）/ 空 = 无域凭据
//（未声明平台，cline/zen 等——国内出口对它们是普通出口）。
func ZoneCompatible(exitZone, credRealm string) bool {
	exitZone = NormalizeZone(exitZone)
	if exitZone == "" {
		return true
	}
	// 国内出口：声明国际的凭据不分配；声明国内与无域凭据可用。
	switch credRealm {
	case "global", "intl":
		return false
	}
	return true
}

// ============================================================================
// 平台域声明（2026-11-26 用户定）：哪些平台可作为国内平台，由面板声明菜单
// 勾选（参考网关设置"封禁平台"同款交互）。声明集是 zone 兼容判定中"凭据
// 有效域"的唯一来源——取代各平台 realm 的隐式写死判定。
//
// 声明键形态：
//   "acc"/"key"/"zen"/"freepi"             整平台勾选 = 全部凭据 cn 域
//   "wb:cn"/"wb:gb"                        wb 的 cn/global 区域凭据分别匹配
//   "trae:cn"/"trae:gb"                    trae 的 cn/intl 区域凭据分别匹配
//   "other:<slug>"                         other 子平台勾选 = 该子平台全部 key cn 域
//
// 未声明的平台 = 无域 = 国内出口当普通出口用。手动绑定不受限。
// ============================================================================

// DeclaredRealm 经平台域声明推导凭据有效域。platformKey 为排除规则口径
//（acc/trae/key/wb/freepi/other:<slug>）；credRealm 为凭据自身区域
//（wb=auth.Realm、trae=uid 前缀 cn/intl、其余空）。返回 "cn"/"global"/""。
func DeclaredRealm(platformKey, credRealm string) string {
	has := func(k string) bool {
		for _, d := range GetBinding().ZoneRealms {
			if d == k {
				return true
			}
		}
		return false
	}
	// 带区域子声明的平台（wb/trae）：凭据区域与声明键一一对应。
	sub := map[string][2]string{ // credRealm -> [cn声明键, gb声明键]
		"wb":   {"wb:cn", "wb:gb"},
		"trae": {"trae:cn", "trae:gb"},
	}[platformKey]
	if sub != ([2]string{}) {
		switch credRealm {
		case "cn":
			if has(sub[0]) {
				return "cn"
			}
		case "global", "intl":
			if has(sub[1]) {
				return "global"
			}
		}
		return ""
	}
	// 整平台声明（勾选 = 全部凭据 cn 域）。
	if has(platformKey) {
		return "cn"
	}
	return ""
}

// RuleKeyOf 代理的规则/统计主键：备注名（#fragment）优先——订阅换端口时
// 规则与统计不失配（2026-11-26 用户定备注名唯一主键）；无备注键保持原样
//（直连哨兵 __direct__、存量无备注行）。
func RuleKeyOf(proxyURL string) string {
	if a := AliasOf(proxyURL); a != "" {
		return a
	}
	return proxyURL
}

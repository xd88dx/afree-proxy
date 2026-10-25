package app

import (
	"afree-proxy/internal/amd"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/tokenharbor"
	"afree-proxy/internal/trae"
)

// ============================================================================
// 平台可路由性门（无启用凭据 = 整平台退出会话路由）
//
// 用户定（2026-10-26）：任何平台——哪怕没有封禁——只要没有任何启用凭据或
// 开了反代开关的凭据，会话就不该路由过去。与封禁平台同处一个闸
// （withModelBlacklistScan），挂在三端点入口；/v1/models 的展示门控由
// 各平台 ModelList 自己负责（trae/amd/wb 已各自实现）。
//
// 判定口径按平台各自的"凭据启用"语义：
//   - cline：账号池存在 Status=active 且 PoolEnabled 的账号
//   - trae：trae.HasEnabledAccount()（账号总闸开且持有 token）
//   - workbuddy：vendor 池存在可用账号（AvailableUIDs 非空，含总闸+反代
//     开关+健康判定）
//   - zen（OpenCode）：存在 KeyRoutingEnabled 的 key（匿名 public key 不算
//     —— 无凭据，恒不铸造，但 zen 免费层无凭据也可用；此处按"有启用的
//     具名 key"判，纯 public 部署视为不可路由是 fail-closed 的可接受代价）
//   - amd / openrouter / tokenharbor / other 子平台：存在 RoutingEnabled
//     的 key
// ============================================================================

// platformHasRoutableCredential 报告平台当前是否至少有一个可参与请求路由的
// 凭据。无法识别的平台（空串等）返回 true（不挡，交由后续正常路由报错）。
func platformHasRoutableCredential(platform string) bool {
	switch platform {
	case platCline:
		p := loadPool()
		poolMu.Lock()
		defer poolMu.Unlock()
		for _, a := range p.Accounts {
			if a != nil && a.Status == "active" && accountPoolEnabled(a) {
				return true
			}
		}
		return false
	case platTrae:
		return trae.HasEnabledAccount()
	case platWorkBuddy:
		if workbuddySub == nil || workbuddySub.pool == nil {
			return false
		}
		return len(workbuddySub.pool.AvailableUIDs()) > 0
	case platZen:
		cfg := getZenConfig()
		for _, k := range cfg.Keys {
			if k != "" && k != "public" && zenKeyRoutingEnabled(k) {
				return true
			}
		}
		return false
	case platAMD:
		return amd.HasEnabledKey()
	case platOpenRouter:
		for _, k := range openrouter.Keys() {
			if openrouter.RoutingEnabled(k) {
				return true
			}
		}
		return false
	case platTokenHarbor:
		for _, k := range tokenharbor.Keys() {
			if tokenharbor.RoutingEnabled(k) {
				return true
			}
		}
		return false
	default:
		if p := other.PlatformBySlug(trimOtherPrefix(platform)); p != nil {
			for _, k := range p.Keys {
				if p.RoutingEnabled(k) {
					return true
				}
			}
			return false
		}
		return true
	}
}

// trimOtherPrefix "other:<slug>" → "<slug>"；非 other 前缀原样返回。
func trimOtherPrefix(platform string) string {
	const prefix = "other:"
	if len(platform) > len(prefix) && platform[:len(prefix)] == prefix {
		return platform[len(prefix):]
	}
	return platform
}

package app

import (
	"afree-proxy/internal/other"
)

// ============================================================================
// 平台可路由性门（无启用凭据 = 整平台退出会话路由）
//
// 用户定（2026-10-26）：任何平台——哪怕没有封禁——只要没有任何启用凭据或
// 开了反代开关的凭据，会话就不该路由过去。与封禁平台同处一个闸
// （withModelBlacklistScan），挂在三端点入口；/v1/models 的展示门控与
// 会话闸同判定（trae 在各自 ModelList，cline/zen/
// other 在 app 层段生成处），保证"列出来的模型都调得通"。
//
// 判定口径按平台各自的"凭据启用"语义：
//   - cline：账号池存在 Status=active 且 PoolEnabled 的账号
//   - trae：trae2apiRoutableCount()>0（账号总闸开且反代开关开）
//   - workbuddy：vendor 池存在可用账号（AvailableUIDs 非空，含总闸+反代
//     开关+健康判定）
//   - zen（OpenCode）：存在参与路由的 key（空池 = 不可路由；匿名 "public"
//     哨兵已于 2026-10-26 移除，上游匿名层也已不可用）
//   - other 子平台：存在 RoutingEnabled
//     的 key
// ============================================================================

// platformHasRoutableCredential 报告平台当前是否至少有一个可参与请求路由的
// 凭据。2026-11-17 P3 起走 platform registry（各平台适配器见
// platform_registry.go，判定口径与下述注释一致）；未注册的平台（空串等）
// 返回 true（不挡，交由后续正常路由报错）。
func platformHasRoutableCredential(platform string) bool {
	if p := platformRegistry.Lookup(platform); p != nil {
		return p.Routable()
	}
	return true
}

// otherPlatformHasRoutableKey 子平台是否存在参与路由的启用 key。展示门控与
// 请求入口闸共用，避免"列出来却调不通"（与 trae 同口径）。
func otherPlatformHasRoutableKey(p *other.Platform) bool {
	if p == nil {
		return false
	}
	for _, k := range p.Keys {
		if p.RoutingEnabled(k) {
			return true
		}
	}
	return false
}

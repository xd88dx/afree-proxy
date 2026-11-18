package app

import (
	"afree-proxy/internal/gateway"
)

// 平台可路由性门（registry 查询）：gateway 批后网关经 PlatformRoutable 钩子
// 消费本判定（registry 单例在装配根）。

// platformHasRoutableCredential 报告平台当前是否至少有一个可参与请求路由的
// 凭据（各平台适配器见 platform_registry.go）；未注册的平台返回 true。
func platformHasRoutableCredential(platform string) bool {
	if p := platformRegistry.Lookup(platform); p != nil {
		return p.Routable()
	}
	return true
}

func init() {
	gateway.PlatformRoutable = platformHasRoutableCredential
}

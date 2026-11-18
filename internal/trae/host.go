package trae

// Host Trae 平台对宿主服务的依赖注入点（2026-11-18 结构重构 P3e）：出包后
// 平台不再 import 装配根——请求日志槽位（upstream label / 出口类型）、模型
// 黑名单判定经本结构注入（装配根 init 赋值；平台代码不判 nil——装配根契约：
// 注入即完整）。

import "context"

// Host 注入点集合。
var Host = struct {
	LogUpstreamInfo func(ctx context.Context, label string)
	LogUpstreamExit func(ctx context.Context, exitType string)
	ModelBannedIn   func(merged, bare string) bool
}{}

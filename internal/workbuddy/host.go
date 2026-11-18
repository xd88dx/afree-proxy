package workbuddy

// Host WorkBuddy 平台对宿主服务的依赖注入点（2026-11-18 结构重构 P3e）：
// 出包后平台不再 import 装配根——请求日志槽位、面板日志 sink 挂载、权重
// 优化定时循环的启动、出口观察经本结构注入（装配根 init 赋值；平台代码不
// 判 nil——装配根契约：注入即完整）。

import (
	"context"
	"io"
	"net/http"
)

// Host 注入点集合。
var Host = struct {
	WriteJSON              func(w http.ResponseWriter, status int, data any)
	LogUpstreamInfo        func(ctx context.Context, label string)
	LogUpstreamExit        func(ctx context.Context, exitType string)
	AttachLogSink          func(sink io.Writer)
	StartShareOptimizeLoop func(ctx context.Context)
}{}

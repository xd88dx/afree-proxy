package server

import "context"

// AfreeAccountObserver afree 桥接层的「账号选定」观测钩子（vendor 脚本注入点，
// 上游原样为 nil = 无行为）：vendored 每次真正用某账号发上游请求前回调一次，
// 失败换号的每次尝试都会回调（后写覆盖先写）。
//
// 为什么需要它：afree 的出口接管钩子只在账号**走代理**时才会被调用（直连分支
// 交还上游自带 client），直连绑定的账号因此拿不到任何回调，请求日志的
// 「账号/凭据」列恒空（2026-11-09 修复）。本钩子与出口无关，只回报"谁在服务"。
var AfreeAccountObserver func(ctx context.Context, uid string)

// afreeObserveAccount 供 chat/anthropic 注入点调用；无钩子/无 uid 时零成本。
func afreeObserveAccount(ctx context.Context, uid string) {
	if AfreeAccountObserver == nil || uid == "" || ctx == nil {
		return
	}
	AfreeAccountObserver(ctx, uid)
}

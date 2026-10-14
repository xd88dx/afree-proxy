package app

import (
	"context"
	"fmt"
)

// scopedObserver 把无 scope 的三参数注入适配成四个（含空 scope）的形式，
// 供单平台实现（amd / tokenharbor / openrouter）直接注册 SetAttemptObserver。
func scopedObserver(f func(ctx context.Context, key, proxyURL string)) func(ctx context.Context, scope, key, proxyURL string) {
	return func(ctx context.Context, _ string, key, proxyURL string) { f(ctx, key, proxyURL) }
}

// keyPoolAttemptObserver 是 amd / tokenharbor / openrouter / other 四个 key
// 池平台共用的请求日志注入（各平台的 SetAttemptObserver）。这些平台在各自
// 包内选 key，key 序号与绑定表都在包内，宿主只能拿到明文 key + 出口 URL，
// 因此把"转序号 + 查绑定 + 判出口类型"交给各平台闭包，主包只做日志填充。
//
// ctx 来自发起请求的 *http.Request：网关 /v1 请求带日志槽位，面板 Test
// 与定时任务的 ctx 没有槽位，setUpstreamInfo/setUpstreamExit 内部即 no-op。
func keyPoolAttemptObserver(keyIndex func(key string) int, bindingOf func(key string) (main, backup string, bound bool)) func(ctx context.Context, key, proxyURL string) {
	return func(ctx context.Context, key, proxyURL string) {
		if ctx == nil || key == "" {
			return
		}
		setUpstreamInfo(ctx, fmt.Sprintf("key#%d", keyIndex(key)))
		main, backup, bound := bindingOf(key)
		if !proxyIsolationEnabled() {
			// 隔离关闭时绑定不生效，出口按全局规则判定
			bound = false
			main, backup = "", ""
		}
		setUpstreamExit(ctx, proxyExitType(proxyURL, main, backup, bound))
	}
}

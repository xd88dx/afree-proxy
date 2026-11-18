package zen

// Host zen 平台对宿主服务的依赖注入点（2026-11-17 结构重构 P3d）：出包后
// 平台不再 import 装配根——模型目录判重、combo 平台解析、池快照、上游错误
// 分级、SSE 聚合、日志槽位全部经本结构注入（装配根 init 赋值；平台代码不判
// nil——装配根契约：注入即完整）。

import (
	"context"
	"net/http"
	"time"
)

// Ctx/HTTPResponse 类型别名（net/http 形态）。
type Ctx = context.Context
type HTTPResponse = http.Response

// Host 注入点集合。
var Host = struct {
	CollectStreamResponse   func(upstream *HTTPResponse, hintModel ...string) (map[string]any, error)
	IsDirect                func(v string) bool
	PassThroughKeys         []string
	RepairToolArguments     func(args string) string
	AsInt                   func(v any) int
	BoundExitFor            func(key string) (proxyURL string, idx int, ok bool)
	MaskProxyURLRaw         func(raw string) string
	LogUpstreamInfo         func(ctx Ctx, label string)
	LogUpstreamExitType     func(ctx Ctx, exitType string)
	ClineHeaderWatchdogFor  func(ctx Ctx, d time.Duration) (Ctx, func() bool)
	ZenHeaderTimeout        func(stream bool) time.Duration
	ProxyExitType           func(proxyURL, main, backup string, bound bool) string
	ShouldCooldownExit      func(kind, proxyURL string, err error) (string, bool)
	PlatformCooldown        time.Duration
	IsolationEnabled        func() bool
	AccountIDs              func() []string
	HasClineCatalogConflict func(zenID string) bool
	Strategy                func() string
	LogUpstream             func(ctx Ctx, label string)
	LogUpstreamExit         func(ctx Ctx, exit string)
	ComboPlatform           func(id string) (platform string, ok bool)
	HasModelEntry           func(id string) bool
	GetMsgCount             func(params map[string]any) int
	ZenRoutable             func() bool
	MaskProxyURL            func(raw string) string
	PoolStatusSnapshot      func() (total, active int)
	UpstreamErrorStatus     func(err error, fallback int) int
	KeyBindingOf            func(key string) (main, backup string, bound bool)
	BoundProxiesRoutableFor func(kind, main, backup string) bool
	BlockedExitMessage      func(prefix, main, backup string) string
}{
	CollectStreamResponse: func(upstream *HTTPResponse, hintModel ...string) (map[string]any, error) {
		return nil, nil
	},
	IsDirect:                func(v string) bool { return v == "direct" },
	RepairToolArguments:     func(args string) string { return args },
	AsInt:                   func(v any) int { return 0 },
	BoundExitFor:            func(string) (string, int, bool) { return "", -1, false },
	MaskProxyURLRaw:         func(raw string) string { return raw },
	LogUpstreamInfo:         func(Ctx, string) {},
	LogUpstreamExitType:     func(Ctx, string) {},
	ClineHeaderWatchdogFor:  func(ctx Ctx, d time.Duration) (Ctx, func() bool) { return ctx, func() bool { return false } },
	ZenHeaderTimeout:        func(bool) time.Duration { return 0 },
	ProxyExitType:           func(_, _, _ string, _ bool) string { return "" },
	ShouldCooldownExit:      func(string, string, error) (string, bool) { return "", false },
	PlatformCooldown:        10 * time.Minute,
	IsolationEnabled:        func() bool { return false },
	AccountIDs:              func() []string { return nil },
	HasClineCatalogConflict: func(string) bool { return false },
	Strategy:                func() string { return "" },
	LogUpstream:             func(Ctx, string) {},
	LogUpstreamExit:         func(Ctx, string) {},
	ComboPlatform:           func(string) (string, bool) { return "", false },
	HasModelEntry:           func(string) bool { return false },
	GetMsgCount:             func(map[string]any) int { return 0 },
	ZenRoutable:             func() bool { return false },
	MaskProxyURL:            func(raw string) string { return raw },
	PoolStatusSnapshot:      func() (int, int) { return 0, 0 },
	UpstreamErrorStatus:     func(err error, fallback int) int { return fallback },
	KeyBindingOf:            func(string) (string, string, bool) { return "", "", false },
	BoundProxiesRoutableFor: func(string, string, string) bool { return true },
	BlockedExitMessage:      func(prefix, main, backup string) string { return prefix },
}

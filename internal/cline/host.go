package cline

import (
	"context"
	"net/http"
)

// Host cline 平台对宿主服务的依赖注入点（2026-11-17 结构重构 P3c）：
// 出包后平台不再 import 装配根——全局配置读、走池开关分发、模型目录规范化、
// egress 重算、客户端指纹头升级全部经本结构注入；nil 字段 = 该能力不可用
//（装配根 init 必须注入全部字段）。上游调用入口 CallAPI 同为注入点

// Host 注入点集合。字段全部由装配根在 init 时赋值；平台代码不判 nil 直接调
// （宿主契约：注入即完整）。
// Kind 平台 kind（platform.KindCline 的本地常量——出口冷却/记账的平台维度）。
const KindCline = "cline"

var Host = struct {
	ClientKeys              func() []string
	Strategy                func() string
	AdminLogEnabled         func() bool
	UseProxies              func() *bool
	ReconcileEgress         func()
	NormalizeIdentityHeader func(map[string]string) int
	SyncPersistedHeaders    func(map[string]string)
	ZenModelExists          func(string) bool
	SyncZenCatalog          func() error
}{}

// Ctx/HTTPResponse 类型别名（net/http 形态，避免接口装箱）。
type Ctx = context.Context
type HTTPResponse = http.Response

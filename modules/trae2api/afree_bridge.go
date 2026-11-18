// Package trae2api 是 afree 桥接层（internal/app/trae*.go）消费 vendored
// trae2api 的唯一门面。vendor 树是独立 Go module（module 路径与上游一致，
// import 零改写），Go 的 internal 可见性规则禁止主 module 直接 import 本
// module 的 internal/*——本文件就是那个被刻意收窄的消费契约：桥接层需要的
// 类型/函数/钩子在此显式列出，上游重构漂移在编译期暴露。
//
// 本文件是 afree 自有文件（vendor 脚本 PRESERVE 登记项），上游没有它；
// re-vendor 不丢。新增消费面时先在这里补导出，再改桥接层。
package trae2api

import (
	"context"
	"net/http"

	"trae2api/internal/accountstore"
	"trae2api/internal/config"
	"trae2api/internal/metrics"
	"trae2api/internal/pool"
	"trae2api/internal/realm"
	"trae2api/internal/server"
	"trae2api/internal/traectl"
)

// ---- 类型别名（alias 携带全部方法与字段）----

type (
	// config 域
	Config        = config.Config
	ConfigAccount = config.Account
	// server 域
	Server = server.Server
	// pool 域
	Pool        = pool.Pool
	PoolAccount = pool.Account
	PoolOptions = pool.Options
	PoolStatus  = pool.Status
	// traectl 域
	Store              = traectl.Store
	Product            = traectl.Product
	CredentialSnapshot = traectl.CredentialSnapshot
	RefreshError       = traectl.RefreshError
	// metrics 域
	Sample = metrics.Sample
)

// ---- realm 域常量（上游为字符串常量）----

const (
	Trae     = realm.Trae
	Solo     = realm.Solo
	TraeIntl = realm.TraeIntl
	SoloIntl = realm.SoloIntl
)

// ---- 函数（var 别名保持签名逐字节精确）----

var (
	Load                = config.Load
	Default             = config.Default
	NewServer           = server.New
	NewPool             = pool.New
	OpenStore           = traectl.OpenStore
	ProductByKey        = traectl.ProductByKey
	SharedChatTransport = traectl.SharedChatTransport
)

// ---- 钩子注入口（vendor 侧 nil = 上游原样行为）----
//
// 这些钩子在 vendor 树内是包级 var，由桥接层进程启动时注入；门面用 setter
// 收口而不是重导出 var（var 别名会拷贝初值，后续赋值不回传）。

// SetStoreHTTPClient 注入出口接管钩子（traectl.StoreHTTPClient）：chat/续期/
// 额度/签到等全部对 Trae 上游的出站统一经宿主解析的 HTTP 客户端发出。
func SetStoreHTTPClient(f func(*Store) *http.Client) { traectl.StoreHTTPClient = f }

// SetRoutingAllowed 注入反代开关钩子（pool.RoutingAllowed）：返回 false 的
// 账号不参与选号（含兜底路径）。
func SetRoutingAllowed(f func(string) bool) { pool.RoutingAllowed = f }

// SetAccountAddedHook 注入新增身份钩子（accountstore.AccountAddedHook）：
// 快照落盘成功后回调宿主（新增身份默认不参与路由的初始落点）。
func SetAccountAddedHook(f func(*CredentialSnapshot)) { accountstore.AccountAddedHook = f }

// SetOnRecord 注入持久记账钩子（server.OnRecord）：每个完成的请求样本回传
// 宿主，维护跨重启的今日/累计 token 记账。
func SetOnRecord(f func(Sample)) { server.OnRecord = f }

// SetAccountObserver 注入「账号选定」观测钩子（server.AfreeAccountObserver）：
// 每次真正用某账号发上游请求前回调（直连绑定账号也能填请求日志两列）。
func SetAccountObserver(f func(context.Context, string)) { server.AfreeAccountObserver = f }

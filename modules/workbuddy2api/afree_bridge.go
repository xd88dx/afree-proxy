// Package wbbridge 是 afree 桥接层（internal/app/workbuddy*.go、admin_backup.go）
// 消费 vendored workbuddy2api-panel 的唯一门面。vendor 树是独立 Go module
// （module 路径与上游一致，import 零改写），Go 的 internal 可见性规则禁止主
// module 直接 import 本 module 的 internal/*——本文件就是那个被刻意收窄的
// 消费契约：桥接层需要的类型/函数/钩子在此显式列出，上游重构漂移在编译期
// 暴露。
//
// 命名规则：类型取「域前缀 + 符号」避免跨包撞名（config.Config→WBConfig），
// 函数统一 var 别名保持签名逐字节精确；钩子用 setter 收口（vendor 侧为包级
// var，var 别名会拷贝初值、后续赋值不回传）。
//
// 本文件是 afree 自有文件（vendor 脚本 PRESERVE 登记项），上游没有它；
// re-vendor 不丢。新增消费面时先在这里补导出，再改桥接层。
package wbbridge

import (
	"github.com/misakano7545/workbuddy2api-panel/internal/auth"
	"github.com/misakano7545/workbuddy2api-panel/internal/config"
	"github.com/misakano7545/workbuddy2api-panel/internal/livecfg"
	"github.com/misakano7545/workbuddy2api-panel/internal/panel"
	"github.com/misakano7545/workbuddy2api-panel/internal/pool"
	"github.com/misakano7545/workbuddy2api-panel/internal/redisstore"
	"github.com/misakano7545/workbuddy2api-panel/internal/reqlog"
	"github.com/misakano7545/workbuddy2api-panel/internal/scheduler"
	"github.com/misakano7545/workbuddy2api-panel/internal/server"
	"github.com/misakano7545/workbuddy2api-panel/internal/session"
	"github.com/misakano7545/workbuddy2api-panel/internal/upstream"
	"github.com/misakano7545/workbuddy2api-panel/internal/usage"
)

// ---- 类型别名（alias 携带全部方法与字段）----

type (
	Auth = auth.Auth
	// config 域
	WBConfig = config.Config
	// panel 域
	Panel       = panel.Panel
	PanelConfig = panel.Config
	// pool 域
	Pool             = pool.Pool
	PoolStatus       = pool.Status
	PoolExpiryBucket = pool.ExpiryBucket
	// session 域
	SessionRouter = session.Router
	SessionConfig = session.Config
	// usage 域
	UsageRecorder = usage.Recorder
	UsageDelta    = usage.Delta
	// livecfg 域
	LivecfgHolder   = livecfg.Holder
	LivecfgSnapshot = livecfg.Snapshot
	// redisstore 域
	RedisStore = redisstore.Store
	RedisNoop  = redisstore.Noop
	// reqlog 域
	ReqlogConfig   = reqlog.Config
	ReqlogRecorder = reqlog.Recorder
	// scheduler 域
	Scheduler      = scheduler.Scheduler
	SchedConfig    = scheduler.Config
	ScheduleParams = scheduler.ScheduleParams
	// server 域
	Handler      = server.Handler
	ServerConfig = server.Config
	// upstream 域
	UpClient     = upstream.Client
	EffortPolicy = upstream.EffortPolicy
)

// ---- 函数（var 别名保持签名逐字节精确）----

var (
	AuthLoadDir                   = auth.LoadDir
	AuthSetGlobalEnabled          = auth.SetGlobalEnabled
	ConfigLoad                    = config.Load
	ConfigDefault                 = config.Default
	ConfigParse                   = config.ParseConfig
	PanelNew                      = panel.New
	PoolNew                       = pool.New
	SessionNew                    = session.New
	UsageNew                      = usage.New
	LivecfgNew                    = livecfg.New
	LivecfgNormalizeAllowedModels = livecfg.NormalizeAllowedModels
	LivecfgNormalizeHiddenModels  = livecfg.NormalizeHiddenModels
	LivecfgNormalizeModelAliases  = livecfg.NormalizeModelAliases
	RedisNew                      = redisstore.New
	ReqlogNew                     = reqlog.New
	SchedNew                      = scheduler.New
	ServerNewHandler              = server.NewHandler
	ServerResolveModel            = server.ResolveModel
	ServerSetChatLogOutput        = server.SetChatLogOutput
	UpNew                         = upstream.New
	UpNewDNSDialer                = upstream.NewDNSDialer
	UpSetModelCatalogPath         = upstream.SetModelCatalogPath
)

// ---- 钩子注入口（vendor 侧 nil = 上游原样行为）----

// SetPoolUnableForce 注入全冷却兜底开关（pool.UnableForce）：UNABLE_FORCE=true
// 时才允许在「全部账号冷却」时按兜底策略选用冷却凭据（2026-10-28 用户定）。
func SetPoolUnableForce(f func() bool) { pool.UnableForce = f }

// SetServerOnlyRouter 注入维护任务开关（server.OnlyRouter）：ONLY_ROUTER=true
// 时面板「立即执行」的手动补跑以 503 拒绝（备用实例不得手动打上游）。
func SetServerOnlyRouter(f func() bool) { server.OnlyRouter = f }

// SetAuthAccountPrefProvider 注入账号意志回填钩子（auth.AccountPrefProvider）：
// auth 文件解析完成后按 UID 取宿主侧意志（反代/账号开关、昵称锁）——意志的
// 唯一持久层在宿主平台配置（wkbd-config.json accountPrefs），auth 文件回归
// 纯凭据（2026-11-17 外置）。
func SetAuthAccountPrefProvider(f func(uid string) (poolEnabled, accountEnabled *bool, nicknameLocked bool)) {
	auth.AccountPrefProvider = f
}

// SetAuthAccountPrefObserver 注入账号意志变更通知钩子（auth.AccountPrefObserver）：
// SetPoolEnabled/SetAccountEnabled/SetLocalNickname 改动内存后回调（锁外、
// 异步），宿主据此落自己的持久层。
func SetAuthAccountPrefObserver(f func(uid string)) {
	auth.AccountPrefObserver = f
}

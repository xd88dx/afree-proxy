package trae

// TraeWork 平台桥接层。
//
// 引擎 = vendored trae2api（XCool-603/trae2api，纯标准库 Go，internal/trae2api/）。
// 上游自带：OpenAI 兼容 /v1/chat/completions（SSE）、域前缀模型目录
// （对外合并域 tacn:/tagb:，内部四 realm）、多账号池（分级冷却/熔断/降权/会话粘性）、
// Web console、OAuth 登录与便携快照导入、自动签到。本文件只做三件事：
//
//  1. 装配：vendored server 进程内启动（不占额外端口），config 落
//     DATA_DIR/trae2api/config.json（进配置监测清单；2026-11-05 从顶层散文件
//     trae2api-config.json 迁入子系统目录，与 workbuddy/config.json 同构），
//     运行态落 DATA_DIR/trae2api/（State/metrics/accounts，使用痕迹不进配置监测口径）。
//  2. afree 特色接线（经 vendor 注入的函数变量，不产生 import 环）：
//     出口接管（主/备绑定 fail-closed + 走池开关，见 proxy_binding 约定）、
//     反代开关（RoutingAllowed 选号过滤）、新增身份默认禁用（AccountAddedHook）。
//  3. 网关分发与面板管理 API：tacn:/tagb: 前缀模型名经合并域翻译后进 vendored
//     引擎，/v1/models trae 段 + 费率过滤，/admin/api/trae/* 代理
//     到 vendored 管理端点。
//
// 鉴权分层：vendored 的 cfg.APIKey/webPassword 恒空（上游 withAuth 注入匿名
// 管理员），真正的门禁是 afree 的网关鉴权与 /admin 会话——vendored 引擎只从
// afree 内部可达（进程内 ServeHTTP / 管道 client），不监听任何端口。

import (
	"trae2api"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/kit"
)

// Trae2apiVersion vendored 引擎版本（与上游 internal/server Version 对齐，
// sync 脚本会提醒更新）。
const Trae2apiVersion = "0.2.6"

// TraeConfigPath 配置落 data/trae2api/config.json（2026-11-05 用户定：从顶层
// 散文件 trae2api-config.json 迁入子系统目录，与 workbuddy/config.json 同构）。
func TraeConfigPath() string { return filepath.Join(TraeDataDir(), "config.json") }
func TraeDataDir() string    { return kit.ResolveDataPath("trae2api") }

// EnsureTraeDataDir 子系统目录兜底（全新环境下 Load 的模板写入需要目录）。
// 旧布局顶层散文件的 rename 迁移已随 2026-11-05 清理移除——存量部署均已
// 迁移完毕，残留旧文件需手工搬入 trae2api/config.json。
func EnsureTraeDataDir() {
	_ = os.MkdirAll(TraeDataDir(), 0o700)
}

// ============ 子系统装配 ============

type Trae2apiSubsystem struct {
	Mu     sync.Mutex
	Cfg    *trae2api.Config
	Srv    *trae2api.Server
	Cancel context.CancelFunc
	BgDone chan struct{}
	// vendorDone vendored RunBackground 退出信号（ONLY_ROUTER 下立即关闭）：
	// 它在 ctx 取消时会做末次落盘，重启必须等它，否则旧实例的写盘可能晚于新
	// 实例启动、把新状态盖回去（2026-11-09）。等有上限，见 TraeRestartJoinTimeout。
	VendorDone chan struct{}
}

// TraeRestartJoinTimeout 重启等待旧实例退出的总预算：等到就是干净的单一写者，
// 等不到（vendor 卡住）也绝不能把面板保存拖成无限转圈——超时记日志后继续。
const TraeRestartJoinTimeout = 5 * time.Second

// WaitClosedUntil 等到 done 关闭或 by 到点，超时只记日志不阻塞（by 为过去时刻
// 即刻返回，故调用方可用同一个截止时刻串行等两个信号）。
func WaitClosedUntil(done <-chan struct{}, by time.Time, what string) {
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(time.Until(by)):
		log.Printf("trae2api restart: %s did not stop within %v; continuing without waiting", what, TraeRestartJoinTimeout)
	}
}

var (
	Trae2apiMu    sync.Mutex
	Trae2apiSub   *Trae2apiSubsystem
	TraeHooksOnce sync.Once
)

// Trae2apiCurrent 返回当前子系统快照（nil = 未装载，调用方按 503 处理）。
func Trae2apiCurrent() *Trae2apiSubsystem {
	Trae2apiMu.Lock()
	defer Trae2apiMu.Unlock()
	return Trae2apiSub
}

// InitTrae 启动 vendored trae2api 子系统。失败只记日志不影响既有平台
// （与 WorkBuddy 子系统同口径）。StartProxy 早期调用一次。
func InitTrae() {
	if err := Trae2apiStart(); err != nil {
		log.Printf("trae2api subsystem unavailable: %v", err)
	}
}

// Trae2apiLoadConfig 载入 vendored 配置并套上 afree 硬约束。配置文件缺失时
// 由 vendored Load 落出厂模板（网关默认 https://trae-api-cn.mchost.guru）。
func Trae2apiLoadConfig() (*trae2api.Config, error) {
	cfg, err := trae2api.Load(TraeConfigPath())
	if err != nil {
		return nil, err
	}
	TraeApplyAfreeConstraints(cfg)
	return cfg, nil
}

// TraeDefaultChatBase vendored 引擎的 chat 上游默认基址（traecfg 内部同值）。
const TraeDefaultChatBase = "https://trae-api-cn.mchost.guru"

// TraeApplyAfreeConstraints afree 侧硬约束（面板不可改，重启后强制生效）：
//   - 出口一律经 afree 解析（trae2api.SetStoreHTTPClient__ 接管），上游自带的全局
//     代理与 per-account proxy 字段是死配置，清空防止"面板改了不生效"。
//   - 数据目录钉到 DATA_DIR/trae2api/（State/metrics/accounts 归置一处）。
//   - 鉴权放空：匿名管理员由上游注入，门禁在 afree 网关层。
func TraeApplyAfreeConstraints(cfg *trae2api.Config) {
	// 网关空值兜底 + OAuth-only 域拒收：vendored client 内部有同值默认，但面板要
	// 显示真实值，配置文件也应携带它。手改配置文件把 chat_base 指到只承载 OAuth
	// 的域（api.trae.cn 等）会让整池静默 5xx——这里回退默认并告警（2026-11-09：
	// Trae2apiValidateGateway 此前没有任何生产调用点，文档承诺的拒收并不存在）。
	cfg.Trae.ChatBase = strings.TrimSpace(cfg.Trae.ChatBase)
	if cfg.Trae.ChatBase == "" {
		cfg.Trae.ChatBase = TraeDefaultChatBase
	} else if err := Trae2apiValidateGateway(cfg.Trae.ChatBase); err != nil {
		log.Printf("trae2api: %v; falling back to %s", err, TraeDefaultChatBase)
		cfg.Trae.ChatBase = TraeDefaultChatBase
	}
	// 跨域容灾恒关（上游 9eece39 的 cross_realm_failover 默认 true）：vendored
	// 引擎在 CN 域账号耗尽/冷却时自动切 Intl 域账号服务——这绕过本仓的域透传
	// 契约（tacn 恒 CN、tagb 恒 Intl，域级可路由过滤 + intl 关闭时 afree 层直接
	// 拒绝），Intl 额度会被 CN 请求悄悄消耗、域记账错位。需要 Intl 容灾时由
	// 客户端/下游显式调 tagb: 模型（newapi/hermes 渠道映射层做 failover），
	// 引擎内部绝不跨域。
	noFailover := false
	cfg.Trae.CrossRealmFailover = &noFailover
	cfg.DataDir = TraeDataDir()
	cfg.APIKey = ""
	cfg.WebPassword = ""
	cfg.Trae.Proxy = ""
	for i := range cfg.Trae.Accounts {
		cfg.Trae.Accounts[i].Proxy = ""
	}
}

func Trae2apiStart() error {
	Trae2apiMu.Lock()
	defer Trae2apiMu.Unlock()
	if Trae2apiSub != nil {
		return nil
	}
	EnsureTraeDataDir()
	cfg, err := Trae2apiLoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	srv, err := trae2api.NewServer(cfg)
	if err != nil {
		return fmt.Errorf("build server: %w", err)
	}
	TraeHooksOnce.Do(func() { TraeWireHooks(cfg.DataDir) })
	sub := &Trae2apiSubsystem{Cfg: cfg, Srv: srv, BgDone: make(chan struct{}), VendorDone: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	sub.Cancel = cancel
	go sub.RunBackground(ctx)
	Trae2apiSub = sub
	// 凭据展示顺序（afree-config.json 的 trae.displayOrder 段，2026-11-05
	// 用户定：顺序落盘并随备份迁移）——vendored 池的 order 是进程内新建的，
	// 每次启动/重启都要把保存的顺序重新套上。
	TraeDisplayOrderLoad()
	Trae2apiApplyDisplayOrder(sub)
	log.Printf("trae2api subsystem started (engine v%s, gateway=%s, data=%s)",
		Trae2apiVersion, cfg.Trae.ChatBase, cfg.DataDir)
	return nil
}

// Trae2apiRestart 配置保存后重建子系统：先摘除（请求闸 503），旧实例落盘
// 池状态再停，避免与 vendored 30s ticker 竞争写 State.json。
func Trae2apiRestart() error {
	Trae2apiMu.Lock()
	old := Trae2apiSub
	Trae2apiSub = nil
	Trae2apiMu.Unlock()
	if old != nil {
		if old.Srv != nil {
			_ = old.Srv.Pool().Save()
		}
		if old.Cancel != nil {
			old.Cancel()
			// 两个信号都要等：bgDone = 我们的 30s 落盘循环，vendorDone = vendored
			// 后台任务（它在 ctx 取消时也写盘）。共用同一个截止时刻，总预算封顶。
			stopBy := time.Now().Add(TraeRestartJoinTimeout)
			WaitClosedUntil(old.BgDone, stopBy, "state flusher")
			WaitClosedUntil(old.VendorDone, stopBy, "engine background tasks")
		}
	}
	return Trae2apiStart()
}

// RunBackground vendored 后台任务（额度刷新 30min/签到定时/metrics 落盘
// 2min）+ 池状态 30s 落盘（cmd/server main 的节奏）。ctx 取消时做末次落盘。
// ONLY_ROUTER=true（2026-10-28 用户定，2026-11-04 审计补接 trae）：仅会话
// 路由——vendored 的签到/额度刷新等维护任务不启动（面板自动签到开关被强制
// 覆盖）；metrics 落盘与池状态落盘是观测/持久化基础设施，照常运行。
func (sub *Trae2apiSubsystem) RunBackground(ctx context.Context) {
	defer close(sub.BgDone)
	if kit.OnlyRouter() {
		log.Printf("[only-router] ONLY_ROUTER=true: trae2api 签到/额度刷新等维护任务已禁用（仅会话路由）")
		close(sub.VendorDone) // 无 vendored 后台任务，重启无需等待
	} else {
		go func() {
			sub.Srv.RunBackground(ctx)
			close(sub.VendorDone)
		}()
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = sub.Srv.Pool().Save()
			return
		case <-ticker.C:
			_ = sub.Srv.Pool().Save()
		}
	}
}

// TraeWireHooks 把 afree 侧钩子注入 vendored 包级变量（进程级单例，上游原样
// 为 nil = 上游行为）。闭包全部经 Trae2apiCurrent() 动态取当前子系统，重启
// 后无需重挂。dataDir：记账文件路径的数据目录——本函数在 Trae2apiStart 持
// Trae2apiMu 期间执行，不能经 Trae2apiCurrent() 取（锁重入自死锁，
// 2026-11-05 部署实测抓出）。
func TraeWireHooks(dataDir string) {
	trae2api.SetStoreHTTPClient(TraeStoreHTTPClient)
	trae2api.SetRoutingAllowed(TraeRoutingAllowed)
	trae2api.SetAccountAddedHook(TraeAccountAdded)
	// 持久记账（今日/累计 token，tokens.json；2026-11-05 用户定：与其他平台
	// 同口径，弃用 metrics 流水重建）+ 合批落盘循环。
	trae2api.SetOnRecord(Trae2apiTokenRecorder)
	// 账号选定观测：填请求日志的「账号/凭据」与「出口」列。出口接管钩子只在
	// 账号走代理时才跑到，直连绑定的账号拿不到任何回调、两列恒空（2026-11-09
	// 修复；本钩子与出口无关，只回报"谁在服务"）。
	trae2api.SetAccountObserver(Trae2apiObserveAccount)
	Trae2apiTokensLoad(dataDir)
	StartTrae2apiTokenFlusher()
}

// ============ 出口接管（主/备绑定 + 走池开关） ============

// Trae2apiProxyBinding 与 workbuddyProxyBinding 同语义：主优先、备兜底，
// 双不可用 = 本次请求直接失败（隔离优先），绝不退回直连。
type Trae2apiProxyBinding struct {
	Main   string `json:"main,omitempty"`
	Backup string `json:"backup,omitempty"`
}

var (
	TraeBindingsMu     sync.Mutex
	TraeBindingsLoaded bool
	TraeBindings       = map[string]Trae2apiProxyBinding{}
)

func LoadTraeBindingsLocked() {
	if TraeBindingsLoaded {
		return
	}
	TraeBindingsLoaded = true
	// 配置无 bindings 键时返回 nil：保留已初始化空表，避免后续写入 panic。
	if b := TraeConfigBindings(); b != nil {
		TraeBindings = b
	}
}

func SaveTraeBindingsLocked() {
	cp := make(map[string]Trae2apiProxyBinding, len(TraeBindings))
	for uid, b := range TraeBindings {
		cp[uid] = b
	}
	MutateTraeConfig(func(t *TraeConfigData) { t.Bindings = cp })
}

// TraeBindingOf 账号的绑定出口（主/备）；未绑定时 bound=false。
func TraeBindingOf(uid string) (main, backup string, bound bool) {
	TraeBindingsMu.Lock()
	defer TraeBindingsMu.Unlock()
	LoadTraeBindingsLocked()
	b, ok := TraeBindings[uid]
	if !ok || (b.Main == "" && b.Backup == "") {
		return "", "", false
	}
	return b.Main, b.Backup, true
}

// TraeProxiesEnabled Trae 平台走池开关（trae-config.json 的 useProxies；
// nil = 未配置 = 走池，与其他平台同约定）。
func TraeProxiesEnabled() bool {
	if v := TraeConfigUseProxies(); v != nil {
		return *v
	}
	return true
}

// TraeGlobalExit 未绑定身份的全局出口：走池开关开 → 池内轮转；关 → 直连
// （DisableDirect 时不放行）。与 workbuddyGlobalExit 同口径。
func TraeGlobalExit() (string, bool) {
	if TraeProxiesEnabled() {
		url, _, ok := exitpool.UpstreamForKind(exitpool.KindTrae)
		if !ok {
			return "", false // 出口全被手动冷却：摘出轮转，不走直连兜底
		}
		if url == "" && kit.DisableDirect() {
			return "", false
		}
		return url, true
	}
	if kit.DisableDirect() {
		return "", false
	}
	return "", true
}

// TraeStoreHTTPClient vendored 出口接管钩子（trae2api.SetStoreHTTPClient__）。
// 语义与 workbuddyProxyFor 一致：
//   - 隔离关闭：忽略绑定，按全局走池开关。
//   - 隔离开启且有绑定：主→备 fail-closed，双不可用返回必然失败的客户端。
//   - 隔离开启且未绑定：全局规则；直连哨兵按 DisableDirect 判定。
//
// 返回 nil = 交给上游原生 client（仅 Store 身份缺失的退化路径）；
// 非 nil = 守卫包裹的出口（直连走原生 Transport + 守卫，代理走池客户端 + 观测）。
func TraeStoreHTTPClient(s *trae2api.Store) *http.Client {
	if s == nil {
		return nil
	}
	uid := TraeUIDOfStore(s)
	if uid == "" {
		return nil
	}
	if !exitpool.IsolationEnabled() {
		return TraeEgressClient(uid)
	}
	main, backup, bound := TraeBindingOf(uid)
	if !bound {
		return TraeEgressClient(uid)
	}
	url, _, ok := exitpool.PickBoundProxyFor(exitpool.KindTrae, main, backup)
	if !ok {
		return TraeFailClosedClient()
	}
	return TraePooledClient(url, uid)
}

// TraeEgressClient 全局规则出口（未绑定/隔离关闭时）。
func TraeEgressClient(uid string) *http.Client {
	url, ok := TraeGlobalExit()
	if !ok {
		return TraeFailClosedClient()
	}
	return TraePooledClient(url, uid)
}

// TraeDirectClient 直连出口客户端：包装 vendored 原生 chat Transport
// （trae2api.SharedChatTransport，连接参数不变），只叠加回退链守卫（死腿短路 +
// 非 2xx 观测）。此前直连返回 nil 交还原生 client，死腿 create_agent_task 会
// 真实出网白耗一个往返（2026-11-10 生产 tacn 案例：未绑定出口账号的第 1 轮
// 尝试实测 4s）。出口/账号列仍由账号 observer 补写 direct（见
// Trae2apiObserveAccount），与代理路径的 TraeObserveTransport 互补不重复。
func TraeDirectClient() *http.Client {
	return &http.Client{Transport: TraeFallbackGuardTransport{
		Inner: trae2api.SharedChatTransport(),
	}}
}

// TraePooledClient 把池代理地址换成带观测的客户端；空串 = 直连（原生
// Transport + 守卫，见 TraeDirectClient）。
// 最外层是回退链守卫（死腿短路 + 中间失败观测，见 trae_fallback_guard.go），
// 短路不出网、不计入出口健康统计。
// uid 是本次出站命中的账号（调用方已按 Store 反查）：vendored 每次发请求都
// 重新调 StoreHTTPClient(c.Store)，client 按请求构造，闭包捕获即精确归属，
// 无需从出站请求上下文反推（出口 client 不带账号身份，上下文里没有）。
func TraePooledClient(proxyURL, uid string) *http.Client {
	if proxyURL == "" {
		return TraeDirectClient()
	}
	base := exitpool.ClientFor(proxyURL)
	return &http.Client{Transport: TraeFallbackGuardTransport{
		Inner: TraeObserveTransport{Inner: base.Transport, Proxy: proxyURL, UID: uid},
	}}
}

// TraeFailClosedClient 出口解析失败（绑定全不可用/隔离禁直连）时返回的
// 客户端：每次 RoundTrip 直接报错，宁弃请求不暴露直连。
func TraeFailClosedClient() *http.Client {
	return &http.Client{Transport: TraeFailClosedTransport{}}
}

type TraeFailClosedTransport struct{}

func (TraeFailClosedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("trae2api: no usable egress (bound exits down or isolation denies direct)")
}

// TraeObserveTransport 出站观测：出口健康统计 + 请求日志「账号/出口」列。
// uid 由构造方闭包注入（TraeStoreHTTPClient 按 Store 反查，见 TraePooledClient）；
// vendored 的续期/签到/额度等后台出站同样经这里，但没有日志槽位时
// setUpstreamInfo 内部 no-op，不会污染。
type TraeObserveTransport struct {
	Inner http.RoundTripper
	Proxy string
	UID   string // 本次出站命中的账号 UID；空 = 未知（面板只填出口列）
}

func (t TraeObserveTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if ctx := r.Context(); ctx != nil && t.UID != "" {
		Host.LogUpstreamInfo(ctx, Trae2apiUpstreamLabel(t.UID))
		Host.LogUpstreamExit(ctx, Trae2apiExitType(t.UID, t.Proxy))
	}
	start := time.Now()
	resp, err := t.Inner.RoundTrip(r)
	exitpool.RecordProxyOutcome(exitpool.KindTrae, t.Proxy, err == nil, err, time.Since(start).Milliseconds())
	return resp, err
}

// Trae2apiUpstreamLabel 日志「账号/Key」列：account #N（与 wb 同口径，行号
// 取池内顺序）。
// Trae2apiObserveAccount 是 vendored AfreeAccountObserver 的实现:账号选定后
// 填请求日志两列(面板与运维排查用)。面板/定时任务的 ctx 没有日志槽位,
// setUpstreamInfo/Exit 内部即 no-op。
func Trae2apiObserveAccount(ctx context.Context, uid string) {
	if ctx == nil || uid == "" {
		return
	}
	Host.LogUpstreamInfo(ctx, Trae2apiUpstreamLabel(uid))
	// 出口列:走代理的路径由 TraeObserveTransport 写精确值(main/backup);
	// 直连路径没有观测传输,这里补齐 direct。只在"无需选池"的分支写,避免
	// observer 里调 pickUpstreamProxyFor 白耗一次轮转位。
	main, _, bound := TraeBindingOf(uid)
	if bound {
		if strings.EqualFold(strings.TrimSpace(main), "direct") {
			Host.LogUpstreamExit(ctx, "direct")
		}
		return
	}
	if !TraeProxiesEnabled() {
		Host.LogUpstreamExit(ctx, "direct")
	}
}
func Trae2apiUpstreamLabel(uid string) string {
	n := 0
	if sub := Trae2apiCurrent(); sub != nil {
		for _, a := range sub.Srv.Pool().All() {
			n++
			if a.UID == uid {
				return fmt.Sprintf("account #%d", n)
			}
		}
	}
	return fmt.Sprintf("account #%d", 0)
}

// Trae2apiExitType 与 TraeStoreHTTPClient 同口径判定出口类型（面板日志列）。
func Trae2apiExitType(uid, exitRaw string) string {
	if !exitpool.IsolationEnabled() {
		return exitpool.ProxyExitType(exitRaw, "", "", false)
	}
	main, backup, bound := TraeBindingOf(uid)
	if !bound {
		return exitpool.ProxyExitType(exitRaw, "", "", false)
	}
	return exitpool.ProxyExitType(exitRaw, main, backup, true)
}

// TraeUIDOfStore 由凭据存储反查池 UID（Store 指针比对，账号量级 = 几十，
// 每次出站扫一遍可忽略）。config 文件直挂的 storage_path 账号 afree 不使用
// （账号一律走 data/accounts 快照），识别不了时返回空串 = 不接管出口。
func TraeUIDOfStore(s *trae2api.Store) string {
	sub := Trae2apiCurrent()
	if sub == nil || s == nil {
		return ""
	}
	for _, ch := range sub.Srv.Channels() {
		for _, a := range ch.Accounts() {
			if a.Store == s {
				return a.UID
			}
		}
	}
	return ""
}

// ============ 反代开关（路由参与） ============

var (
	TraeRoutingMu     sync.Mutex
	TraeRoutingLoaded bool
	// TraeRouting uid → 反代开关（true=参与模型路由）。查表缺失 = 启用
	// （存量缺字段按启用的兼容边界）；新增身份经 TraeAccountAdded 显式落
	// false（新增身份默认禁用铁律）。
	TraeRouting = map[string]bool{}
)

func LoadTraeRoutingLocked() {
	if TraeRoutingLoaded {
		return
	}
	TraeRoutingLoaded = true
	// 配置无 routing 键时返回 nil：保留已初始化空表，避免后续写入 panic。
	if rt := TraeConfigRouting(); rt != nil {
		TraeRouting = rt
	}
}

func SaveTraeRoutingLocked() {
	cp := make(map[string]bool, len(TraeRouting))
	for uid, v := range TraeRouting {
		cp[uid] = v
	}
	MutateTraeConfig(func(t *TraeConfigData) { t.Routing = cp })
}

func TraeRoutingGet(uid string) bool {
	TraeRoutingMu.Lock()
	defer TraeRoutingMu.Unlock()
	LoadTraeRoutingLocked()
	v, ok := TraeRouting[uid]
	if !ok {
		return true
	}
	return v
}

func TraeRoutingSet(uid string, enabled bool) {
	TraeRoutingMu.Lock()
	defer TraeRoutingMu.Unlock()
	LoadTraeRoutingLocked()
	TraeRouting[uid] = enabled
	SaveTraeRoutingLocked()
}

// TraeRoutingAllowed vendored 反代开关钩子（trae2api.SetRoutingAllowed__）：关闭
// 的账号不参与模型路由选号；签到/额度刷新等维护任务不经 Pick，不受影响。
func TraeRoutingAllowed(uid string) bool {
	return TraeRoutingGet(uid)
}

// TraeAccountAdded vendored 新增身份钩子（trae2api.SetAccountAddedHook__）：
// 新增身份默认不参与路由（2026-10-12 铁律），落盘后由面板显式打开。
func TraeAccountAdded(snap *trae2api.CredentialSnapshot) {
	if snap == nil {
		return
	}
	uid := snap.Product + "-" + snap.UserID
	TraeRoutingSet(uid, false)
	log.Printf("trae2api: account %s imported (routing disabled by default)", uid)
}

// ============ 可路由性与模型目录 ============

// Trae2apiRoutableCount 有凭据、账号开关开且反代开关开的账号数。0 = 整平台
// 503 拒路由 + /v1/models 摘除（与 wb/cline 同闸）。
func Trae2apiRoutableCount() int {
	sub := Trae2apiCurrent()
	if sub == nil {
		return 0
	}
	n := 0
	for _, st := range sub.Srv.Pool().List() {
		if !st.Disabled && TraeRoutingGet(st.UID) {
			n++
		}
	}
	return n
}

// Trae2apiIntlEnabled 国际域（trae-intl:/solo-intl:）是否透出：跟随上游
// intl 配置（默认开）。关闭时这四个前缀在网关层直接拒绝，避免 vendored 的
// 跨域兜底把它们悄悄路由到 CN 账号。
func Trae2apiIntlEnabled() bool {
	sub := Trae2apiCurrent()
	if sub == nil {
		return false
	}
	return sub.Cfg.Intl.Enabled
}

// ============ 合并域（2026-11-07 用户定：池子对合并，形态向 wb 看齐）============
//
// 对外只有两个合并域：tacn（国内 = Trae CN + SOLO CN）、tagb（国际 = Trae Intl
// + SOLO Intl）。vendored 引擎的四 realm 原样运转（选号/冷却/并发/额度按内部
// realm 与账号体系），afree 层做「目录合并 + 前缀翻译」：对外模型 id =
// tacn:bare / tagb:bare，请求时按「交集以 trae 为主」翻译回内部 realm:bare。
// 旧四前缀（trae:/solo:/trae-intl:/solo-intl:）废除——不在目录 → strict-gate
// 404，客户端与下游渠道映射改用新前缀（hermes 自动跟随 /v1/models）。
const (
	TraeMergedCN   = "tacn"
	TraeMergedIntl = "tagb"
)

// Trae2apiMergedRealms 合并域的内部 realm 成员（有序：trae 在前 = 交集以 trae 为主）。
func Trae2apiMergedRealms(merged string) []string {
	if merged == TraeMergedCN {
		return []string{trae2api.Trae, trae2api.Solo}
	}
	if merged == TraeMergedIntl {
		return []string{trae2api.TraeIntl, trae2api.SoloIntl}
	}
	return nil
}

// Trae2apiMergedOfRealm 内部 realm → 合并域（非 trae 四域返回空串）。
func Trae2apiMergedOfRealm(realm string) string {
	if realm == trae2api.Trae || realm == trae2api.Solo {
		return TraeMergedCN
	}
	if realm == trae2api.TraeIntl || realm == trae2api.SoloIntl {
		return TraeMergedIntl
	}
	return ""
}

// Trae2apiSplitMerged 拆合并域前缀：返回 (merged, bare, 是否合并域模型)。
func Trae2apiSplitMerged(model string) (string, string, bool) {
	for _, m := range []string{TraeMergedCN, TraeMergedIntl} {
		if p := m + ":"; strings.HasPrefix(model, p) {
			return m, strings.TrimPrefix(model, p), true
		}
	}
	return "", "", false
}

// Trae2apiResolveMerged 合并域模型归属解析：bare 在哪个内部 realm 的渠道目录
// 里就归谁（成员顺序保证交集命中 trae 在先）。intl 关闭时 tagb 不解析。
// 返回空串 = 目录里不存在。
func Trae2apiResolveMerged(merged, bare string) string {
	sub := Trae2apiCurrent()
	if sub == nil {
		return ""
	}
	// 可路由性过滤（与 Trae2apiModelEntries 的目录归属同口径）：不可路由的
	// 渠道不承接翻译——否则「trae 渠道无账号、solo 承接渲染」的交集模型会被
	// 翻译回 trae，选号必 503。成员顺序保证 trae 可路由时交集仍以 trae 为主。
	routable := map[string]bool{}
	for _, st := range sub.Srv.Pool().List() {
		if !st.Disabled && TraeRoutingGet(st.UID) {
			routable[st.Realm] = true
		}
	}
	for _, realm := range Trae2apiMergedRealms(merged) {
		if !routable[realm] {
			continue
		}
		for _, ch := range sub.Srv.Channels() {
			if ch.Realm != realm {
				continue
			}
			for _, e := range ch.ModelEntries() {
				if id, _ := e["id"].(string); id == realm+":"+bare {
					return realm
				}
			}
		}
	}
	return ""
}

// Trae2apiTranslateMerged 把合并域模型名翻译成内部 realm:bare 透传给 vendored
// （交集以 trae 为主；solo 独有模型归 solo）。非合并域前缀原样返回。
func Trae2apiTranslateMerged(model string) string {
	merged, bare, ok := Trae2apiSplitMerged(model)
	if !ok {
		return model
	}
	if realm := Trae2apiResolveMerged(merged, bare); realm != "" {
		return realm + ":" + bare
	}
	// 目录里不存在：原样透传，vendored 按未知模型拒绝（与旧「不存在的模型」一致）。
	return model
}

// Trae2apiTranslateMergedBody 对请求体做合并域模型名翻译（ServeTrae2apiChat
// 的透传路径——vendored 只认内部 realm 前缀，对外 tacn:/tagb: 必须先翻译）。
func Trae2apiTranslateMergedBody(body []byte) []byte {
	var probe struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &probe) != nil || probe.Model == "" {
		return body
	}
	translated := Trae2apiTranslateMerged(probe.Model)
	if translated == probe.Model {
		return body
	}
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return body
	}
	m["model"] = translated
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// Trae2apiModelPrefixes 网关层认可的合并域前缀（对外契约）。tagb 仅在 intl
// 开启时认可（关闭时 tagb: 模型 strict-gate 404，与旧 trae-intl: 同语义）。
func Trae2apiModelPrefixes() []string {
	p := []string{TraeMergedCN + ":"}
	if Trae2apiIntlEnabled() {
		p = append(p, TraeMergedIntl+":")
	}
	return p
}

// IsTrae2apiModel 模型名是否归 TraeWork 平台（合并域前缀；tagb 仅在 intl
// 开启时认可）。旧四前缀（trae:/solo:/...）已废除，不再识别。
func IsTrae2apiModel(model string) bool {
	for _, p := range Trae2apiModelPrefixes() {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

// Trae2apiPlatformModel 静态前缀判定（不随 intl 开关变化）——封禁/路由归类
// 用：两个合并域前缀永远属于 TraeWork 平台命名空间。
func Trae2apiPlatformModel(model string) bool {
	_, _, ok := Trae2apiSplitMerged(model)
	return ok
}

// Trae2apiModelExists id 是否是 vendored 目录里的真实模型（combo 别名冲突
// 检查用；intl 关闭时其域渠道不存在 → 自然不存在）。
func Trae2apiModelExists(id string) bool {
	merged, bare, ok := Trae2apiSplitMerged(id)
	if !ok {
		return false
	}
	if merged == TraeMergedIntl && !Trae2apiIntlEnabled() {
		return false
	}
	return Trae2apiResolveMerged(merged, bare) != ""
}

// Trae2apiRateFilterThresholdCN / ...Intl 分域费率过滤阈值（2026-11-07 拆分）：
// CN 阈值管合并域 tacn（realm Trae/Solo），Intl 管 tagb（realm TraeIntl/
// SoloIntl）。nil = 未配置 = 不筛；显式 0 = 该域关。配置住 afree-config.json
// 的 trae 子域（2026-11-16 迁入）；旧单值 trae2apiModelRateFilter 已删除。
func Trae2apiRateFilterThresholdCN() float64 {
	cn, _ := TraeConfigRateFilters()
	if cn != nil {
		return *cn
	}
	return 0
}

func Trae2apiRateFilterThresholdIntl() float64 {
	_, intl := TraeConfigRateFilters()
	if intl != nil {
		return *intl
	}
	return 0
}

// Trae2apiModelEntries /v1/models 合并用的 Trae 段条目（2026-11-07 合并域化）：
// 对外 id = tacn:bare / tagb:bare。构建：遍历内部 realm 成员（trae 在前），
// **bare 名去重、交集以 trae 为主**——同一模型在 chat_v3 与 solo_work_lite
// 两池都出现时归 trae 渠道服务（solo 独有模型才落 solo）。过滤链：渠道级
// 可路由闸（交集模型的可达性跟着归属渠道走——「列出来的必须调得通」）→
// 分域费率阈值（tacn 全体走 CN 阈值、tagb 走 Intl，不管内部归属）→ 平台
// 模型黑名单（scope 合并域化，存量内部 realm 记录经 TraeModelBannedIn 兼容
// 判定）。owned_by 恒规范名 "trae"（hermes 分桶契约不变）。
func Trae2apiModelEntries() []map[string]any {
	out := make([]map[string]any, 0)
	sub := Trae2apiCurrent()
	if sub == nil || Trae2apiRoutableCount() == 0 {
		return out
	}
	// 渠道级可路由性（2026-11-04 审计铁律：列出来的必须调得通）。
	routableRealms := make(map[string]bool)
	for _, st := range sub.Srv.Pool().List() {
		if !st.Disabled && TraeRoutingGet(st.UID) {
			routableRealms[st.Realm] = true
		}
	}
	for _, merged := range []string{TraeMergedCN, TraeMergedIntl} {
		if merged == TraeMergedIntl && !Trae2apiIntlEnabled() {
			continue
		}
		threshold := Trae2apiRateFilterThresholdCN()
		if merged == TraeMergedIntl {
			threshold = Trae2apiRateFilterThresholdIntl()
		}
		seen := map[string]bool{}
		for _, realm := range Trae2apiMergedRealms(merged) {
			if !routableRealms[realm] {
				continue
			}
			ch := sub.Srv.Channels()[realm]
			if ch == nil {
				continue
			}
			for _, e := range ch.ModelEntries() {
				id, _ := e["id"].(string)
				if id == "" {
					continue
				}
				bare := strings.TrimPrefix(id, realm+":")
				if seen[bare] {
					continue // 交集以 trae 为主（成员顺序保证 trae 先入）
				}
				seen[bare] = true
				if threshold > 0 {
					if rate, ok := e["consumption_rate"].(float64); ok && rate > threshold {
						continue
					}
				}
				if Host.ModelBannedIn(merged, bare) {
					continue
				}
				entry := make(map[string]any, len(e)+4)
				for k, v := range e {
					entry[k] = v
				}
				entry["id"] = merged + ":" + bare
				entry["status"] = "active"
				entry["cost"] = "free"
				out = append(out, entry)
			}
		}
	}
	// 各平台段 ID 有序是面板/newapi 映射的既有约定（models_order_test）；
	// vendored 目录顺序随启动/intl 配置漂移，出口处统一排序。
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i]["id"].(string)
		b, _ := out[j]["id"].(string)
		return a < b
	})
	return out
}

// ============ 网关分发 ============

// ShouldServeTrae2api /v1 请求闸：子系统装载 + 模型带认可前缀。
func ShouldServeTrae2api(model string) bool {
	return Trae2apiCurrent() != nil && IsTrae2apiModel(model)
}

func (w *TraePipeWriter) Header() http.Header { return w.header }

func (w *TraePipeWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.pw.Write(p)
}

func (w *TraePipeWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.code <- code
}

// Flush 满足 http.Flusher：io.Pipe 是同步管道，Write 即到即达，无需额外动作。
func (w *TraePipeWriter) Flush() {}

// traeInprocessCaller 为平台 chat 调用器抽象（cross_endpoint）构造的进程内
// client：请求直接进 vendored Handler，响应以 *http.Response 形态交还外层
// 缓冲转发层（与 cline/zen 同一套流式/重试语义）。
var TraeInprocessHTTP = &http.Client{Transport: TraeInprocessRoundTripper{}}

const TraeInprocessBase = "http://trae2api.inprocess"

type TraeInprocessRoundTripper struct{}

// TraePipeWriter 桥接 http.ResponseWriter 与 io.Pipe：handler 的写入变成
// 响应体流，WriteHeader 的状态码经 channel 回传。
type TraePipeWriter struct {
	header http.Header
	pw     *io.PipeWriter
	code   chan int
	wrote  bool
}

func (TraeInprocessRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	sub := Trae2apiCurrent()
	if sub == nil || sub.Srv == nil {
		return nil, errors.New("trae2api subsystem unavailable")
	}
	handler := sub.Srv.Handler()
	pr, pw := io.Pipe()
	w := &TraePipeWriter{header: make(http.Header), pw: pw, code: make(chan int, 1)}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				if !w.wrote {
					w.WriteHeader(http.StatusInternalServerError)
				}
				_ = pw.CloseWithError(fmt.Errorf("trae2api handler panic: %v", rec))
			}
		}()
		handler.ServeHTTP(w, r)
		_ = pw.Close()
	}()
	code := <-w.code
	resp := &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Header:     w.header,
		Body:       pr,
		Request:    r,
	}
	if cl := w.header.Get("Content-Length"); cl != "" {
		var n int64
		if _, err := fmt.Sscan(cl, &n); err == nil {
			resp.ContentLength = n
		}
	} else {
		resp.ContentLength = -1
	}
	return resp, nil
}

func BytesReader(b []byte) io.Reader { return strings.NewReader(string(b)) }

// TraeNormalizeStreamDefault 对请求体补 OpenAI 缺省语义：无 stream 键 = false
// （显式 true/false 原样保留）。body 非 JSON 时原样返回。
func TraeNormalizeStreamDefault(body []byte) []byte {
	var params map[string]any
	if err := json.Unmarshal(body, &params); err != nil {
		return body
	}
	if _, ok := params["stream"]; ok {
		return body
	}
	params["stream"] = false
	nb, err := json.Marshal(params)
	if err != nil {
		return body
	}
	return nb
}

// ============ 管理面板 API（/admin/api/trae/*） ============

// Trae2apiAdminCall 进程内调用 vendored 管理端点（test/checkin/credits/
// clear-cooldown/delete），经同一管道 client，鉴权 = 匿名管理员。
func Trae2apiAdminCall(method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, TraeInprocessBase+path, BytesReader(body))
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := TraeInprocessHTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, data, err
}

func Trae2apiAccountStates(sub *Trae2apiSubsystem) []map[string]any {
	// 锁序铁律（2026-11-04 实测死锁）：绝不能持 TraeBindingsMu/TraeRoutingMu
	// 调 Pool().List()——List→Status→healthyLocked→RoutingAllowed 钩子→
	// TraeRoutingGet 会重入 TraeRoutingMu，同 goroutine 自锁把 /v1/models、
	// config 接口与后台 Pool.Save 全部堵死。先在各自锁内取快照，再查池。
	TraeBindingsMu.Lock()
	LoadTraeBindingsLocked()
	bindings := make(map[string]Trae2apiProxyBinding, len(TraeBindings))
	for uid, v := range TraeBindings {
		bindings[uid] = v
	}
	TraeBindingsMu.Unlock()

	statuses := sub.Srv.Pool().List()
	out := make([]map[string]any, 0, len(statuses))
	for i, st := range statuses {
		b := bindings[st.UID]
		row := map[string]any{
			// 0 基序号（前端统一 +1 显示，与其他平台一致；2026-11-04 用户报告
			// 首行显示 #2——此前这里 i+1 与前端 (index+1) 叠成双重偏移）。
			"index":         i,
			"uid":           st.UID,
			"label":         st.Label,
			"realm":         st.Realm,
			"product":       st.Product,
			"owner":         st.Owner,
			"disabled":      st.Disabled,
			"disableReason": st.DisableReason,
			"cooling":       st.Cooling,
			"coolKind":      st.CoolKind,
			"coolReason":    st.CoolReason,
			"coolRemain":    st.CoolRemainSec,
			"breaker":       st.Breaker,
			"breakerRemain": st.BreakerRemain,
			"degraded":      st.Degraded,
			"healthy":       st.Healthy,
			"inFlight":      st.InFlight,
			"maxInFlight":   st.MaxInFlight,
			"okCount":       st.OKCount,
			"errCount":      st.ErrCount,
			// routing 口径与实际路由判定一致（TraeRoutingGet：查表缺失=启用）
			"routing":       TraeRoutingGet(st.UID),
			"bindingMain":   b.Main,
			"bindingBackup": b.Backup,
		}
		if st.Credits != nil {
			row["credits"] = *st.Credits
			// 总额度（vendor Status.CreditsTotal，0=未知）——面板「剩余 / 总」
			if st.CreditsTotal > 0 {
				row["creditsTotal"] = st.CreditsTotal
			}
		}
		if st.ModelCooling != nil {
			row["modelCooling"] = st.ModelCooling
		}
		Trae2apiEnrichAccountRow(sub, st, row)
		out = append(out, row)
	}
	return out
}

// Trae2apiEnrichAccountRow 给账号行补齐旧引擎时代面板展示的四组字段（2026-11-04
// 用户要求恢复换线前的表格列序）。数据全部取自 vendored 引擎现成结构，不新增
// 持久化：
//   - token 过期：Store.Cred.ExpiredAt（快照自带，续期后引擎回写）；
//   - 今日/累计 token 与最近使用：metrics Collector 的当日分桶（byDay） +
//     账号聚合 LastSeen——Today 口径与控制台看板一致（本地自然日）；
//   - 签到状态：签到动作由 vendored 后台/手动端点完成，此处仅回显「今日已签」
//     标记（软状态，不真打上游——面板刷新频率下逐号打 UG 接口太重）。
func Trae2apiEnrichAccountRow(sub *Trae2apiSubsystem, st trae2api.PoolStatus, row map[string]any) {
	// token 过期（Store 可能未绑定：config accounts 形态；空值即不出口）
	if a := sub.Srv.Pool().Get(st.UID); a != nil && a.Store != nil {
		a.Store.Mu().Lock()
		exp := a.Store.Cred.ExpiredAt
		a.Store.Mu().Unlock()
		if t, err := time.Parse(time.RFC3339, exp); err == nil && !t.IsZero() {
			row["expiredAt"] = t.UTC().Format(time.RFC3339)
			row["tokenUsable"] = time.Until(t) > 5*time.Minute
		}
	}
	// 今日/累计 token + 最近使用：持久记账表（trae2api/tokens.json，经
	// server.OnRecord 钩子逐请求累加；2026-11-05 用户定）。跨重启不丢，跨日
	// 惰性清零（展示层），与其他七平台同口径——vendored metrics 近 100 条
	// 流水撑不起持久口径，弃用。
	today, total, lastUsed, _ := Trae2apiTokensSnapshot(st.UID)
	row["tokensToday"] = today
	row["tokensTotal"] = total
	if lastUsed != "" {
		row["lastUsed"] = lastUsed
	}
	// 签到状态：引擎持久化记录（CheckinAll 动作后 NoteCheckin 回写，随池
	// State.json 持久化；空日期 = 从未执行过签到，面板不显示徽章）
	if st.CheckinDate != "" {
		row["checkin"] = map[string]any{
			"checkedIn": st.CheckinChecked,
			"date":      st.CheckinDate,
			"lastError": st.CheckinError,
			"failed":    st.CheckinFailed,
		}
	}
}

// HandleTraeConfigUpdate POST /admin/api/trae/config/update —— vendored 配置
// 子集 + 费率过滤 patch；vendored 无热更新，落盘后整组重启（池状态经
// State.json 保留，冷却/熔断不丢）。useProxies 走 proxy-config.json 既有键。
// TraeApplyRateFilterPatch 倍率筛选 patch 应用：分域键各自独立生效，nil =
// 不改动。面板只发新键；旧单值 trae2apiModelRateFilter 已删除（2026-11-16），
// 无旧键兼容路径。抽出独立函数便于在子系统未启动的单测环境直接验证应用
// 逻辑（handler 侧有 503 守卫）。
// TraeRateFilterPatch 倍率筛选 patch 的两个键（从 HandleTraeConfigUpdate 的
// 大 patch struct 里独立出来，便于单测直调）。
type TraeRateFilterPatch struct {
	RateFilterCN   *float64 `json:"rateFilterCN"`
	RateFilterIntl *float64 `json:"rateFilterIntl"`
}

func TraeApplyRateFilterPatch(patch TraeRateFilterPatch) {
	if patch.RateFilterCN == nil && patch.RateFilterIntl == nil {
		return
	}
	MutateTraeConfig(func(t *TraeConfigData) {
		if patch.RateFilterCN != nil {
			t.ModelRateFilterCN = patch.RateFilterCN
		}
		if patch.RateFilterIntl != nil {
			t.ModelRateFilterIntl = patch.RateFilterIntl
		}
	})
}

func Trae2apiValidateGateway(gw string) error {
	if gw == "" {
		return nil
	}
	lower := strings.ToLower(gw)
	for _, frag := range []string{"api.trae.cn", "api.trae.com.cn", "trae-api.com.cn", "oauth"} {
		if strings.Contains(lower, frag) {
			return fmt.Errorf("gateway %q is an OAuth-only host and cannot serve model requests", gw)
		}
	}
	return nil
}

func ExportTrae2apiProxyBindings() map[string]Trae2apiProxyBinding {
	TraeBindingsMu.Lock()
	defer TraeBindingsMu.Unlock()
	LoadTraeBindingsLocked()
	if len(TraeBindings) == 0 {
		return nil
	}
	out := make(map[string]Trae2apiProxyBinding, len(TraeBindings))
	for k, v := range TraeBindings {
		out[k] = v
	}
	return out
}

// ImportTrae2apiProxyBindings 还原 TraeWork 出口绑定，返回写入条数。与 wb
// 同采用"整体替换"：备份里出现的 uid 覆盖本机值；本机多出来的 uid 一并清掉，
// 避免跨机迁移后残留旧机的绑定。
func ImportTrae2apiProxyBindings(in map[string]Trae2apiProxyBinding) int {
	TraeBindingsMu.Lock()
	defer TraeBindingsMu.Unlock()
	LoadTraeBindingsLocked()
	next := make(map[string]Trae2apiProxyBinding, len(in))
	for k, v := range in {
		next[k] = v
	}
	TraeBindings = next
	SaveTraeBindingsLocked()
	return len(next)
}

func VendoredErrCode(code int) string {
	if code < 400 {
		return ""
	}
	return fmt.Sprintf("vendored endpoint returned HTTP %d", code)
}

func VendoredErr(vendored map[string]any, code int) string {
	if code < 400 {
		return ""
	}
	if e, ok := vendored["error"].(map[string]any); ok {
		if m, ok := e["message"].(string); ok {
			return m
		}
	}
	return fmt.Sprintf("vendored endpoint returned HTTP %d", code)
}

// Trae2apiBackupPayload 备份的 trae 段形态（2026-11-04 起）。config =
// data/trae2api/config.json 原文；accounts = 快照文件名 → 快照 JSON；routing =
// 反代开关表。旧引擎（trae-config.json 账号池）的形态不再兼容。
type Trae2apiBackupPayload struct {
	Config   json.RawMessage            `json:"config,omitempty"`
	Routing  map[string]bool            `json:"routing,omitempty"`
	Accounts map[string]json.RawMessage `json:"accounts,omitempty"`
}

func Trae2apiBackupSnapshot() json.RawMessage {
	payload := Trae2apiBackupPayload{Routing: TraeRoutingCopy()}
	if raw, err := os.ReadFile(TraeConfigPath()); err == nil && json.Valid(raw) {
		payload.Config = json.RawMessage(raw)
	}
	acc := map[string]json.RawMessage{}
	entries, _ := os.ReadDir(filepath.Join(TraeDataDir(), "accounts"))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(TraeDataDir(), "accounts", e.Name()))
		if err == nil && json.Valid(raw) {
			acc[e.Name()] = json.RawMessage(raw)
		}
	}
	payload.Accounts = acc
	out, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return out
}

func TraeRoutingCopy() map[string]bool {
	TraeRoutingMu.Lock()
	defer TraeRoutingMu.Unlock()
	LoadTraeRoutingLocked()
	cp := make(map[string]bool, len(TraeRouting))
	for uid, v := range TraeRouting {
		cp[uid] = v
	}
	return cp
}

// Trae2apiRestoreBackup 备份导入：还原 config 与账号快照文件 + 路由表。
// 隔离护栏：未勾选"沿用启用状态"时全部身份路由置禁用（与六平台口径一致）。
// 旧引擎形态的 trae 段（无 config/accounts 键）报错跳过，不迁移。
// 返回还原的账号数。
func Trae2apiRestoreBackup(raw json.RawMessage, keepEnabled bool) (int, error) {
	var payload Trae2apiBackupPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, fmt.Errorf("not a trae2api backup segment (legacy engine shape?): %w", err)
	}
	if payload.Config == nil && len(payload.Accounts) == 0 {
		return 0, errors.New("empty trae2api backup segment")
	}
	if payload.Config != nil {
		// 形态判定只看顶层 trae 键**存在与否**（2026-11-04 用户定：不再兼容
		// 旧引擎格式，只考虑全新环境导出导入）。旧引擎 trae-config.json 顶层
		// 是 accounts/users/interval，没有 trae 键；新引擎配置必有 trae 键。
		// 不能用 chat_base 的值判定——afree 约束把网关空值兜底只做在启动装载
		// （TraeApplyAfreeConstraints），落盘文件里 chat_base 合法为空串，按值
		// 判会把新引擎自己的完整备份误判成 legacy 整段跳过（实测翻车）。
		var probe struct {
			Trae json.RawMessage `json:"trae"`
		}
		_ = json.Unmarshal(payload.Config, &probe)
		if len(probe.Trae) == 0 {
			return 0, errors.New("legacy trae config shape (pre-trae2api), not migrated")
		}
		// 全新环境（清卷后导入）子系统目录尚不存在——WriteFile 不会自建父目录，
		// 不补会让整段 restore 失败、routing/账号全部落空（2026-11-05 闭环测试抓出）。
		EnsureTraeDataDir()
		if err := os.WriteFile(TraeConfigPath(), payload.Config, 0o600); err != nil {
			return 0, err
		}
	}
	accountsDir := filepath.Join(TraeDataDir(), "accounts")
	if err := os.MkdirAll(accountsDir, 0o700); err != nil {
		return 0, err
	}
	n := 0
	for name, rawSnap := range payload.Accounts {
		// 文件名白名单（快照命名 = <product>-<userId>.json），防路径逃逸。
		if !strings.HasSuffix(name, ".json") || strings.ContainsAny(name, "/\\") ||
			strings.Contains(name, "..") || name != filepath.Base(name) {
			continue
		}
		if err := os.WriteFile(filepath.Join(accountsDir, name), rawSnap, 0o600); err != nil {
			continue
		}
		n++
		uid := strings.TrimSuffix(name, ".json")
		if keepEnabled {
			if v, ok := payload.Routing[uid]; ok {
				TraeRoutingSet(uid, v)
			}
		} else {
			TraeRoutingSet(uid, false)
		}
	}
	if n > 0 || payload.Config != nil {
		_ = Trae2apiRestart()
	}
	return n, nil
}

// Trae2apiProbeBlacklistedModel 黑名单页对 trae 封禁模型的定向探测：进程内
// 发一次最小 chat 请求（真实计费意图极小：max_tokens=16），返回上游真实反应
// 供人工判断"该不该解禁"。与旧引擎 traeProbeBlacklistedModel 同语义。
// model 是合并域对外形态（tacn:/tagb:），vendored 只认内部 realm 前缀，须先翻译
// （与 ServeTrae2apiChat/Trae2apiCaller 同口径，09589a0 漏接的第四条路径）。
func Trae2apiProbeBlacklistedModel(model string) (map[string]any, string) {
	params := map[string]any{
		"model":      Trae2apiTranslateMerged(model),
		"stream":     false,
		"max_tokens": 16,
		"messages":   []map[string]any{{"role": "user", "content": "Reply with exactly: OK"}},
	}
	body, err := json.Marshal(params)
	if err != nil {
		return map[string]any{"ok": false, "model": model, "error": err.Error()}, "error"
	}
	code, data, err := Trae2apiAdminCall(http.MethodPost, "/v1/chat/completions", body)
	if err != nil {
		return map[string]any{"ok": false, "model": model, "error": err.Error()}, "error"
	}
	var resp struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error map[string]any `json:"error"`
	}
	_ = json.Unmarshal(data, &resp)
	if code >= 400 || resp.Error != nil {
		msg := fmt.Sprintf("HTTP %d", code)
		if resp.Error != nil {
			if m, ok := resp.Error["message"].(string); ok {
				msg = m
			}
		}
		return map[string]any{"ok": false, "model": model, "status": code, "error": msg}, "error"
	}
	finish := ""
	if len(resp.Choices) > 0 {
		finish = resp.Choices[0].FinishReason
	}
	return map[string]any{"ok": true, "model": model, "finishReason": finish}, "ok"
}

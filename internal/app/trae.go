package app

// TraeWork 平台桥接层。
//
// 引擎 = vendored trae2api（XCool-603/trae2api，纯标准库 Go，internal/trae2api/）。
// 上游自带：OpenAI 兼容 /v1/chat/completions（SSE）、域前缀模型目录
// （trae:/solo:/trae-intl:/solo-intl:）、多账号池（分级冷却/熔断/降权/会话粘性）、
// Web console、OAuth 登录与便携快照导入、自动签到。本文件只做三件事：
//
//  1. 装配：vendored server 进程内启动（不占额外端口），config 落
//     DATA_DIR/trae2api/config.json（进配置监测清单；2026-11-05 从顶层散文件
//     trae2api-config.json 迁入子系统目录，与 workbuddy/config.json 同构），
//     运行态落 DATA_DIR/trae2api/（state/metrics/accounts，使用痕迹不进配置监测口径）。
//  2. afree 特色接线（经 vendor 注入的函数变量，不产生 import 环）：
//     出口接管（主/备绑定 fail-closed + 走池开关，见 proxy_binding 约定）、
//     反代开关（RoutingAllowed 选号过滤）、新增身份默认禁用（AccountAddedHook）。
//  3. 网关分发与面板管理 API：trae:/solo:/… 前缀模型名**透传**（vendored
//     原生解析域前缀），/v1/models trae 段 + 费率过滤，/admin/api/trae/* 代理
//     到 vendored 管理端点。
//
// 鉴权分层：vendored 的 cfg.APIKey/webPassword 恒空（上游 withAuth 注入匿名
// 管理员），真正的门禁是 afree 的网关鉴权与 /admin 会话——vendored 引擎只从
// afree 内部可达（进程内 ServeHTTP / 管道 client），不监听任何端口。

import (
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

	"afree-proxy/internal/kit"

	traeaccountstore "afree-proxy/internal/trae2api/accountstore"
	traecfg "afree-proxy/internal/trae2api/config"
	traepool "afree-proxy/internal/trae2api/pool"
	traerealm "afree-proxy/internal/trae2api/realm"
	traeserver "afree-proxy/internal/trae2api/server"
	traectl "afree-proxy/internal/trae2api/traectl"
)

// trae2apiVersion vendored 引擎版本（与上游 internal/server Version 对齐，
// sync 脚本会提醒更新）。
const trae2apiVersion = "0.2.6"

// traeConfigPath 配置落 data/trae2api/config.json（2026-11-05 用户定：从顶层
// 散文件 trae2api-config.json 迁入子系统目录，与 workbuddy/config.json 同构）。
func traeConfigPath() string { return filepath.Join(traeDataDir(), "config.json") }
func traeDataDir() string    { return kit.ResolveDataPath("trae2api") }

// ensureTraeDataDir 子系统目录兜底（全新环境下 Load 的模板写入需要目录）。
// 旧布局顶层散文件的 rename 迁移已随 2026-11-05 清理移除——存量部署均已
// 迁移完毕，残留旧文件需手工搬入 trae2api/config.json。
func ensureTraeDataDir() {
	_ = os.MkdirAll(traeDataDir(), 0o700)
}

// ============ 子系统装配 ============

type trae2apiSubsystem struct {
	mu     sync.Mutex
	cfg    *traecfg.Config
	srv    *traeserver.Server
	cancel context.CancelFunc
	bgDone chan struct{}
}

var (
	trae2apiMu    sync.Mutex
	trae2apiSub   *trae2apiSubsystem
	traeHooksOnce sync.Once
)

// trae2apiCurrent 返回当前子系统快照（nil = 未装载，调用方按 503 处理）。
func trae2apiCurrent() *trae2apiSubsystem {
	trae2apiMu.Lock()
	defer trae2apiMu.Unlock()
	return trae2apiSub
}

// initTrae 启动 vendored trae2api 子系统。失败只记日志不影响既有平台
// （与 WorkBuddy 子系统同口径）。StartProxy 早期调用一次。
func initTrae() {
	if err := trae2apiStart(); err != nil {
		log.Printf("trae2api subsystem unavailable: %v", err)
	}
}

// trae2apiLoadConfig 载入 vendored 配置并套上 afree 硬约束。配置文件缺失时
// 由 vendored Load 落出厂模板（网关默认 https://trae-api-cn.mchost.guru）。
func trae2apiLoadConfig() (*traecfg.Config, error) {
	cfg, err := traecfg.Load(traeConfigPath())
	if err != nil {
		return nil, err
	}
	traeApplyAfreeConstraints(cfg)
	return cfg, nil
}

// traeApplyAfreeConstraints afree 侧硬约束（面板不可改，重启后强制生效）：
//   - 出口一律经 afree 解析（traectl.StoreHTTPClient 接管），上游自带的全局
//     代理与 per-account proxy 字段是死配置，清空防止"面板改了不生效"。
//   - 数据目录钉到 DATA_DIR/trae2api/（state/metrics/accounts 归置一处）。
//   - 鉴权放空：匿名管理员由上游注入，门禁在 afree 网关层。
func traeApplyAfreeConstraints(cfg *traecfg.Config) {
	// 网关空值兜底：vendored client 内部有同值默认，但面板要显示真实值，
	// 配置文件也应携带它（用户在面板看到的与实际生效的一致）。
	if strings.TrimSpace(cfg.Trae.ChatBase) == "" {
		cfg.Trae.ChatBase = "https://trae-api-cn.mchost.guru"
	}
	// 跨域容灾恒关（上游 9eece39 的 cross_realm_failover 默认 true）：vendored
	// 引擎在 CN 域账号耗尽/冷却时自动切 Intl 域账号服务——这绕过本仓的域透传
	// 契约（trae: 恒 CN、trae-intl: 恒 Intl，0a584b6 域级可路由过滤 + intl 关闭
	// 时 afree 层直接拒绝），Intl 额度会被 CN 请求悄悄消耗、域记账错位。需要
	// Intl 容灾时由客户端/下游显式调 trae-intl: 模型（newapi/hermes 渠道映射
	// 层做 failover），引擎内部绝不跨域。
	noFailover := false
	cfg.Trae.CrossRealmFailover = &noFailover
	cfg.DataDir = traeDataDir()
	cfg.APIKey = ""
	cfg.WebPassword = ""
	cfg.Trae.Proxy = ""
	for i := range cfg.Trae.Accounts {
		cfg.Trae.Accounts[i].Proxy = ""
	}
}

func trae2apiStart() error {
	trae2apiMu.Lock()
	defer trae2apiMu.Unlock()
	if trae2apiSub != nil {
		return nil
	}
	ensureTraeDataDir()
	cfg, err := trae2apiLoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	srv, err := traeserver.New(cfg)
	if err != nil {
		return fmt.Errorf("build server: %w", err)
	}
	traeHooksOnce.Do(func() { traeWireHooks(cfg.DataDir) })
	sub := &trae2apiSubsystem{cfg: cfg, srv: srv, bgDone: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	sub.cancel = cancel
	go sub.runBackground(ctx)
	trae2apiSub = sub
	// 凭据展示顺序（proxy-config.json trae2apiDisplayOrder 段，2026-11-05
	// 用户定：顺序落盘并随备份迁移）——vendored 池的 order 是进程内新建的，
	// 每次启动/重启都要把保存的顺序重新套上。
	traeDisplayOrderLoad()
	trae2apiApplyDisplayOrder(sub)
	log.Printf("trae2api subsystem started (engine v%s, gateway=%s, data=%s)",
		trae2apiVersion, cfg.Trae.ChatBase, cfg.DataDir)
	return nil
}

// trae2apiRestart 配置保存后重建子系统：先摘除（请求闸 503），旧实例落盘
// 池状态再停，避免与 vendored 30s ticker 竞争写 state.json。
func trae2apiRestart() error {
	trae2apiMu.Lock()
	old := trae2apiSub
	trae2apiSub = nil
	trae2apiMu.Unlock()
	if old != nil {
		if old.srv != nil {
			_ = old.srv.Pool().Save()
		}
		if old.cancel != nil {
			old.cancel()
			<-old.bgDone
		}
	}
	return trae2apiStart()
}

// runBackground vendored 后台任务（额度刷新 30min/签到定时/metrics 落盘
// 2min）+ 池状态 30s 落盘（cmd/server main 的节奏）。ctx 取消时做末次落盘。
// ONLY_ROUTER=true（2026-10-28 用户定，2026-11-04 审计补接 trae）：仅会话
// 路由——vendored 的签到/额度刷新等维护任务不启动（面板自动签到开关被强制
// 覆盖）；metrics 落盘与池状态落盘是观测/持久化基础设施，照常运行。
func (sub *trae2apiSubsystem) runBackground(ctx context.Context) {
	defer close(sub.bgDone)
	if kit.OnlyRouter() {
		log.Printf("[only-router] ONLY_ROUTER=true: trae2api 签到/额度刷新等维护任务已禁用（仅会话路由）")
	} else {
		go sub.srv.RunBackground(ctx)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = sub.srv.Pool().Save()
			return
		case <-ticker.C:
			_ = sub.srv.Pool().Save()
		}
	}
}

// traeWireHooks 把 afree 侧钩子注入 vendored 包级变量（进程级单例，上游原样
// 为 nil = 上游行为）。闭包全部经 trae2apiCurrent() 动态取当前子系统，重启
// 后无需重挂。dataDir：记账文件路径的数据目录——本函数在 trae2apiStart 持
// trae2apiMu 期间执行，不能经 trae2apiCurrent() 取（锁重入自死锁，
// 2026-11-05 部署实测抓出）。
func traeWireHooks(dataDir string) {
	traectl.StoreHTTPClient = traeStoreHTTPClient
	traepool.RoutingAllowed = traeRoutingAllowed
	traeaccountstore.AccountAddedHook = traeAccountAdded
	// 持久记账（今日/累计 token，tokens.json；2026-11-05 用户定：与其他平台
	// 同口径，弃用 metrics 流水重建）+ 合批落盘循环。
	traeserver.OnRecord = trae2apiTokenRecorder
	trae2apiTokensLoad(dataDir)
	startTrae2apiTokenFlusher()
}

// ============ 出口接管（主/备绑定 + 走池开关） ============

// trae2apiProxyBinding 与 workbuddyProxyBinding 同语义：主优先、备兜底，
// 双不可用 = 本次请求直接失败（隔离优先），绝不退回直连。
type trae2apiProxyBinding struct {
	Main   string `json:"main,omitempty"`
	Backup string `json:"backup,omitempty"`
}

var (
	traeBindingsMu     sync.Mutex
	traeBindingsLoaded bool
	traeBindings       = map[string]trae2apiProxyBinding{}
)

func loadTraeBindingsLocked() {
	if traeBindingsLoaded {
		return
	}
	traeBindingsLoaded = true
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	bindings := f.Trae2apiBindings
	proxyFileMu.Unlock()
	if bindings != nil {
		traeBindings = bindings
	}
}

func saveTraeBindingsLocked() {
	cp := make(map[string]trae2apiProxyBinding, len(traeBindings))
	for uid, b := range traeBindings {
		cp[uid] = b
	}
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.Trae2apiBindings = cp
	})
}

// traeBindingOf 账号的绑定出口（主/备）；未绑定时 bound=false。
func traeBindingOf(uid string) (main, backup string, bound bool) {
	traeBindingsMu.Lock()
	defer traeBindingsMu.Unlock()
	loadTraeBindingsLocked()
	b, ok := traeBindings[uid]
	if !ok || (b.Main == "" && b.Backup == "") {
		return "", "", false
	}
	return b.Main, b.Backup, true
}

// traeProxiesEnabled Trae 平台走池开关（proxy-config.json 的 traeUseProxies；
// nil = 未配置 = 走池，与其他平台同约定）。
func traeProxiesEnabled() bool {
	if v := platformUseProxies("trae"); v != nil {
		return *v
	}
	return true
}

// traeGlobalExit 未绑定身份的全局出口：走池开关开 → 池内轮转；关 → 直连
// （DisableDirect 时不放行）。与 workbuddyGlobalExit 同口径。
func traeGlobalExit() (string, bool) {
	if traeProxiesEnabled() {
		url, _ := pickUpstreamProxyFor(pkTrae)
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

// traeStoreHTTPClient vendored 出口接管钩子（traectl.StoreHTTPClient）。
// 语义与 workbuddyProxyFor 一致：
//   - 隔离关闭：忽略绑定，按全局走池开关。
//   - 隔离开启且有绑定：主→备 fail-closed，双不可用返回必然失败的客户端。
//   - 隔离开启且未绑定：全局规则；直连哨兵按 DisableDirect 判定。
//
// 返回 nil = 交给上游原生 client（直连哨兵/全局直连）；非 nil = 钉定出口。
func traeStoreHTTPClient(s *traectl.Store) *http.Client {
	if s == nil {
		return nil
	}
	uid := traeUIDOfStore(s)
	if uid == "" {
		return nil
	}
	if !proxyIsolationEnabled() {
		return traeEgressClient(uid)
	}
	main, backup, bound := traeBindingOf(uid)
	if !bound {
		return traeEgressClient(uid)
	}
	url, _, ok := pickBoundProxyFor(pkTrae, main, backup)
	if !ok {
		return traeFailClosedClient()
	}
	return traePooledClient(url)
}

// traeEgressClient 全局规则出口（未绑定/隔离关闭时）。
func traeEgressClient(uid string) *http.Client {
	url, ok := traeGlobalExit()
	if !ok {
		return traeFailClosedClient()
	}
	return traePooledClient(url)
}

// traePooledClient 把池代理地址换成带观测的客户端；空串 = 直连（交还上游）。
// 最外层是回退链守卫（死腿短路 + 中间失败观测，见 trae_fallback_guard.go），
// 短路不出网、不计入出口健康统计。
func traePooledClient(proxyURL string) *http.Client {
	if proxyURL == "" {
		return nil
	}
	base := proxyClientFor(proxyURL)
	return &http.Client{Transport: traeFallbackGuardTransport{
		inner: traeObserveTransport{inner: base.Transport, proxy: proxyURL},
	}}
}

// traeFailClosedClient 出口解析失败（绑定全不可用/隔离禁直连）时返回的
// 客户端：每次 RoundTrip 直接报错，宁弃请求不暴露直连。
func traeFailClosedClient() *http.Client {
	return &http.Client{Transport: traeFailClosedTransport{}}
}

type traeFailClosedTransport struct{}

func (traeFailClosedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("trae2api: no usable egress (bound exits down or isolation denies direct)")
}

// traeObserveTransport 出站观测：出口健康统计 + 请求日志「账号/出口」列。
// ctx 取自 vendored handler 的请求链（afree 直转路径带着原始请求上下文，
// 面板/定时任务调用没有日志槽位时 setUpstreamInfo 内部 no-op）。
type traeObserveTransport struct {
	inner http.RoundTripper
	proxy string
}

func (t traeObserveTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if ctx := r.Context(); ctx != nil {
		uid := traeUIDFromCtx(r)
		if uid != "" {
			setUpstreamInfo(ctx, trae2apiUpstreamLabel(uid))
			setUpstreamExit(ctx, trae2apiExitType(uid, t.proxy))
		}
	}
	start := time.Now()
	resp, err := t.inner.RoundTrip(r)
	recordProxyOutcome(pkTrae, t.proxy, err == nil, err, time.Since(start).Milliseconds())
	return resp, err
}

// traeUIDFromCtx 从请求的 Authorization/vendored 侧不可行——出口 client 不带
// 账号身份，这里退化为按 proxy 槽位只填出口列。账号列由面板凭据页呈现。
func traeUIDFromCtx(*http.Request) string { return "" }

// trae2apiUpstreamLabel 日志「账号/Key」列：account #N（与 wb 同口径，行号
// 取池内顺序）。
func trae2apiUpstreamLabel(uid string) string {
	n := 0
	if sub := trae2apiCurrent(); sub != nil {
		for _, a := range sub.srv.Pool().All() {
			n++
			if a.UID == uid {
				return fmt.Sprintf("account #%d", n)
			}
		}
	}
	return fmt.Sprintf("account #%d", 0)
}

// trae2apiExitType 与 traeStoreHTTPClient 同口径判定出口类型（面板日志列）。
func trae2apiExitType(uid, exitRaw string) string {
	if !proxyIsolationEnabled() {
		return proxyExitType(exitRaw, "", "", false)
	}
	main, backup, bound := traeBindingOf(uid)
	if !bound {
		return proxyExitType(exitRaw, "", "", false)
	}
	return proxyExitType(exitRaw, main, backup, true)
}

// traeUIDOfStore 由凭据存储反查池 UID（Store 指针比对，账号量级 = 几十，
// 每次出站扫一遍可忽略）。config 文件直挂的 storage_path 账号 afree 不使用
// （账号一律走 data/accounts 快照），识别不了时返回空串 = 不接管出口。
func traeUIDOfStore(s *traectl.Store) string {
	sub := trae2apiCurrent()
	if sub == nil || s == nil {
		return ""
	}
	for _, ch := range sub.srv.Channels() {
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
	traeRoutingMu     sync.Mutex
	traeRoutingLoaded bool
	// traeRouting uid → 反代开关（true=参与模型路由）。查表缺失 = 启用
	// （存量缺字段按启用的兼容边界）；新增身份经 traeAccountAdded 显式落
	// false（新增身份默认禁用铁律）。
	traeRouting = map[string]bool{}
)

func loadTraeRoutingLocked() {
	if traeRoutingLoaded {
		return
	}
	traeRoutingLoaded = true
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	routing := f.Trae2apiRouting
	proxyFileMu.Unlock()
	if routing != nil {
		traeRouting = routing
	}
}

func saveTraeRoutingLocked() {
	cp := make(map[string]bool, len(traeRouting))
	for uid, v := range traeRouting {
		cp[uid] = v
	}
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.Trae2apiRouting = cp
	})
}

func traeRoutingGet(uid string) bool {
	traeRoutingMu.Lock()
	defer traeRoutingMu.Unlock()
	loadTraeRoutingLocked()
	v, ok := traeRouting[uid]
	if !ok {
		return true
	}
	return v
}

func traeRoutingSet(uid string, enabled bool) {
	traeRoutingMu.Lock()
	defer traeRoutingMu.Unlock()
	loadTraeRoutingLocked()
	traeRouting[uid] = enabled
	saveTraeRoutingLocked()
}

// traeRoutingAllowed vendored 反代开关钩子（traepool.RoutingAllowed）：关闭
// 的账号不参与模型路由选号；签到/额度刷新等维护任务不经 Pick，不受影响。
func traeRoutingAllowed(uid string) bool {
	return traeRoutingGet(uid)
}

// traeAccountAdded vendored 新增身份钩子（traeaccountstore.AccountAddedHook）：
// 新增身份默认不参与路由（2026-10-12 铁律），落盘后由面板显式打开。
func traeAccountAdded(snap *traectl.CredentialSnapshot) {
	if snap == nil {
		return
	}
	uid := snap.Product + "-" + snap.UserID
	traeRoutingSet(uid, false)
	log.Printf("trae2api: account %s imported (routing disabled by default)", uid)
}

// ============ 可路由性与模型目录 ============

// trae2apiRoutableCount 有凭据、账号开关开且反代开关开的账号数。0 = 整平台
// 503 拒路由 + /v1/models 摘除（与 wb/cline 同闸）。
func trae2apiRoutableCount() int {
	sub := trae2apiCurrent()
	if sub == nil {
		return 0
	}
	n := 0
	for _, st := range sub.srv.Pool().List() {
		if !st.Disabled && traeRoutingGet(st.UID) {
			n++
		}
	}
	return n
}

// trae2apiIntlEnabled 国际域（trae-intl:/solo-intl:）是否透出：跟随上游
// intl 配置（默认开）。关闭时这四个前缀在网关层直接拒绝，避免 vendored 的
// 跨域兜底把它们悄悄路由到 CN 账号。
func trae2apiIntlEnabled() bool {
	sub := trae2apiCurrent()
	if sub == nil {
		return false
	}
	return sub.cfg.Intl.Enabled
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
	traeMergedCN   = "tacn"
	traeMergedIntl = "tagb"
)

// trae2apiMergedRealms 合并域的内部 realm 成员（有序：trae 在前 = 交集以 trae 为主）。
func trae2apiMergedRealms(merged string) []string {
	if merged == traeMergedCN {
		return []string{traerealm.Trae, traerealm.Solo}
	}
	if merged == traeMergedIntl {
		return []string{traerealm.TraeIntl, traerealm.SoloIntl}
	}
	return nil
}

// trae2apiMergedOfRealm 内部 realm → 合并域（非 trae 四域返回空串）。
func trae2apiMergedOfRealm(realm string) string {
	if realm == traerealm.Trae || realm == traerealm.Solo {
		return traeMergedCN
	}
	if realm == traerealm.TraeIntl || realm == traerealm.SoloIntl {
		return traeMergedIntl
	}
	return ""
}

// trae2apiSplitMerged 拆合并域前缀：返回 (merged, bare, 是否合并域模型)。
func trae2apiSplitMerged(model string) (string, string, bool) {
	for _, m := range []string{traeMergedCN, traeMergedIntl} {
		if p := m + ":"; strings.HasPrefix(model, p) {
			return m, strings.TrimPrefix(model, p), true
		}
	}
	return "", "", false
}

// trae2apiResolveMerged 合并域模型归属解析：bare 在哪个内部 realm 的渠道目录
// 里就归谁（成员顺序保证交集命中 trae 在先）。intl 关闭时 tagb 不解析。
// 返回空串 = 目录里不存在。
func trae2apiResolveMerged(merged, bare string) string {
	sub := trae2apiCurrent()
	if sub == nil {
		return ""
	}
	// 可路由性过滤（与 trae2apiModelEntries 的目录归属同口径）：不可路由的
	// 渠道不承接翻译——否则「trae 渠道无账号、solo 承接渲染」的交集模型会被
	// 翻译回 trae，选号必 503。成员顺序保证 trae 可路由时交集仍以 trae 为主。
	routable := map[string]bool{}
	for _, st := range sub.srv.Pool().List() {
		if !st.Disabled && traeRoutingGet(st.UID) {
			routable[st.Realm] = true
		}
	}
	for _, realm := range trae2apiMergedRealms(merged) {
		if !routable[realm] {
			continue
		}
		for _, ch := range sub.srv.Channels() {
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

// trae2apiTranslateMerged 把合并域模型名翻译成内部 realm:bare 透传给 vendored
// （交集以 trae 为主；solo 独有模型归 solo）。非合并域前缀原样返回。
func trae2apiTranslateMerged(model string) string {
	merged, bare, ok := trae2apiSplitMerged(model)
	if !ok {
		return model
	}
	if realm := trae2apiResolveMerged(merged, bare); realm != "" {
		return realm + ":" + bare
	}
	// 目录里不存在：原样透传，vendored 按未知模型拒绝（与旧「不存在的模型」一致）。
	return model
}

// trae2apiTranslateMergedBody 对请求体做合并域模型名翻译（serveTrae2apiChat
// 的透传路径——vendored 只认内部 realm 前缀，对外 tacn:/tagb: 必须先翻译）。
func trae2apiTranslateMergedBody(body []byte) []byte {
	var probe struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &probe) != nil || probe.Model == "" {
		return body
	}
	translated := trae2apiTranslateMerged(probe.Model)
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

// trae2apiModelPrefixes 网关层认可的合并域前缀（对外契约）。tagb 仅在 intl
// 开启时认可（关闭时 tagb: 模型 strict-gate 404，与旧 trae-intl: 同语义）。
func trae2apiModelPrefixes() []string {
	p := []string{traeMergedCN + ":"}
	if trae2apiIntlEnabled() {
		p = append(p, traeMergedIntl+":")
	}
	return p
}

// isTrae2apiModel 模型名是否归 TraeWork 平台（合并域前缀；tagb 仅在 intl
// 开启时认可）。旧四前缀（trae:/solo:/...）已废除，不再识别。
func isTrae2apiModel(model string) bool {
	for _, p := range trae2apiModelPrefixes() {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

// trae2apiPlatformModel 静态前缀判定（不随 intl 开关变化）——封禁/路由归类
// 用：两个合并域前缀永远属于 TraeWork 平台命名空间。
func trae2apiPlatformModel(model string) bool {
	_, _, ok := trae2apiSplitMerged(model)
	return ok
}

// trae2apiModelExists id 是否是 vendored 目录里的真实模型（combo 别名冲突
// 检查用；intl 关闭时其域渠道不存在 → 自然不存在）。
func trae2apiModelExists(id string) bool {
	merged, bare, ok := trae2apiSplitMerged(id)
	if !ok {
		return false
	}
	if merged == traeMergedIntl && !trae2apiIntlEnabled() {
		return false
	}
	return trae2apiResolveMerged(merged, bare) != ""
}

// trae2apiRateFilterThreshold 费率过滤阈值（0 = 关）。存 proxy-config.json
// 的 trae2apiModelRateFilter（afree 侧配置，与 vendored 费率元数据同源比较）。
// 2026-11-07 拆分后保留作旧单值回落：分域键未配置（nil）时两域共用旧值。
func trae2apiRateFilterThreshold() float64 {
	proxyFileMu.Lock()
	defer proxyFileMu.Unlock()
	f := loadProxyConfigFileLocked()
	return f.Trae2apiModelRateFilter
}

// trae2apiRateFilterThresholdCN / ...Intl 分域费率过滤阈值（2026-11-07 拆分，
// 照 wb 79cc3d6c 模式）：CN 过滤 trae:/solo:（realm.Trae/Solo），Intl 过滤
// trae-intl:/solo-intl:（realm.TraeIntl/SoloIntl）。分域键未配置（nil，旧
// 配置文件形态）回落旧单值——行为等价 wb 的 migrateModelRateFilterSplit
// 「逐字段回落自动迁移」，只是回落放在读取函数而非解析入口，少一处字节级
// 预处理。显式 0 = 该域关。
func trae2apiRateFilterThresholdCN() float64 {
	proxyFileMu.Lock()
	defer proxyFileMu.Unlock()
	f := loadProxyConfigFileLocked()
	if f.Trae2apiModelRateFilterCN != nil {
		return *f.Trae2apiModelRateFilterCN
	}
	return f.Trae2apiModelRateFilter
}

func trae2apiRateFilterThresholdIntl() float64 {
	proxyFileMu.Lock()
	defer proxyFileMu.Unlock()
	f := loadProxyConfigFileLocked()
	if f.Trae2apiModelRateFilterIntl != nil {
		return *f.Trae2apiModelRateFilterIntl
	}
	return f.Trae2apiModelRateFilter
}

// trae2apiRateFilterForRealm 按域取阈值。
func trae2apiRateFilterForRealm(realm string) float64 {
	if realm == traerealm.TraeIntl || realm == traerealm.SoloIntl {
		return trae2apiRateFilterThresholdIntl()
	}
	return trae2apiRateFilterThresholdCN()
}

// trae2apiModelEntries /v1/models 合并用的 Trae 段条目（2026-11-07 合并域化）：
// 对外 id = tacn:bare / tagb:bare。构建：遍历内部 realm 成员（trae 在前），
// **bare 名去重、交集以 trae 为主**——同一模型在 chat_v3 与 solo_work_lite
// 两池都出现时归 trae 渠道服务（solo 独有模型才落 solo）。过滤链：渠道级
// 可路由闸（交集模型的可达性跟着归属渠道走——「列出来的必须调得通」）→
// 分域费率阈值（tacn 全体走 CN 阈值、tagb 走 Intl，不管内部归属）→ 平台
// 模型黑名单（scope 合并域化，存量内部 realm 记录经 TraeModelBannedIn 兼容
// 判定）。owned_by 恒规范名 "trae"（hermes 分桶契约不变）。
func trae2apiModelEntries() []map[string]any {
	out := make([]map[string]any, 0)
	sub := trae2apiCurrent()
	if sub == nil || trae2apiRoutableCount() == 0 {
		return out
	}
	// 渠道级可路由性（2026-11-04 审计铁律：列出来的必须调得通）。
	routableRealms := make(map[string]bool)
	for _, st := range sub.srv.Pool().List() {
		if !st.Disabled && traeRoutingGet(st.UID) {
			routableRealms[st.Realm] = true
		}
	}
	for _, merged := range []string{traeMergedCN, traeMergedIntl} {
		if merged == traeMergedIntl && !trae2apiIntlEnabled() {
			continue
		}
		threshold := trae2apiRateFilterThresholdCN()
		if merged == traeMergedIntl {
			threshold = trae2apiRateFilterThresholdIntl()
		}
		seen := map[string]bool{}
		for _, realm := range trae2apiMergedRealms(merged) {
			if !routableRealms[realm] {
				continue
			}
			ch := sub.srv.Channels()[realm]
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
				if TraeModelBannedIn(merged, bare) {
					continue
				}
				entry := make(map[string]any, len(e)+4)
				for k, v := range e {
					entry[k] = v
				}
				entry["id"] = merged + ":" + bare
				entry["source"] = "trae"
				entry["status"] = "active"
				entry["cost"] = "free"
				// owned_by 用规范名 "trae"（与 combo 别名、其他平台段口径一致）：
				// vendored 引擎透传的是产品 Label（"Trae CN"/"TRAE SOLO CN"），下游
				// newapi 渠道映射（hermes afree-remap）按 owned_by 分桶且只认规范名。
				entry["owned_by"] = "trae"
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

// shouldServeTrae2api /v1 请求闸：子系统装载 + 模型带认可前缀。
func shouldServeTrae2api(model string) bool {
	return trae2apiCurrent() != nil && isTrae2apiModel(model)
}

// serveTrae2apiChat 把请求原样交给 vendored /v1/chat/completions（进程内
// ServeHTTP，与 workbuddy serveWorkBuddyChat 同机制）。模型名**透传**——
// vendored 原生解析域前缀与 strict_models 校验，afree 不做任何翻译。
func serveTrae2apiChat(rw http.ResponseWriter, r *http.Request, body []byte) {
	sub := trae2apiCurrent()
	if sub == nil {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "TraeWork subsystem unavailable", "type": "api_error"},
		})
		return
	}
	if trae2apiRoutableCount() == 0 {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"message": "No routable Trae accounts. Enable an account (and its gateway switch) on the Trae page.",
				"type":    "api_error",
			},
		})
		return
	}
	// OpenAI 契约：stream 缺省 = false。vendored 引擎把缺省当 true（上游实现
	// `req.Stream == nil || *req.Stream`），按规范省略 stream 的客户端会收到
	// SSE 而不是 JSON（2026-11-05 审计实测）。只兜 /v1/chat/completions 入口：
	// 缺省时显式置 false；/v1/messages、/v1/responses 走 trae2apiCaller，由
	// 调用方参数决定流式语义，不经这里。
	body = traeNormalizeStreamDefault(body)
	// 合并域翻译（2026-11-07）：对外 tacn:/tagb: → 内部 realm:bare 透传给
	// vendored（交集以 trae 为主，solo 独有归 solo）。
	body = trae2apiTranslateMergedBody(body)
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	sub.srv.Handler().ServeHTTP(rw, r)
}

// traeInprocessCaller 为平台 chat 调用器抽象（cross_endpoint）构造的进程内
// client：请求直接进 vendored Handler，响应以 *http.Response 形态交还外层
// 缓冲转发层（与 cline/zen 同一套流式/重试语义）。
var traeInprocessHTTP = &http.Client{Transport: traeInprocessRoundTripper{}}

const traeInprocessBase = "http://trae2api.inprocess"

type traeInprocessRoundTripper struct{}

// traePipeWriter 桥接 http.ResponseWriter 与 io.Pipe：handler 的写入变成
// 响应体流，WriteHeader 的状态码经 channel 回传。
type traePipeWriter struct {
	header http.Header
	pw     *io.PipeWriter
	code   chan int
	wrote  bool
}

func (w *traePipeWriter) Header() http.Header { return w.header }

func (w *traePipeWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.pw.Write(p)
}

func (w *traePipeWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.code <- code
}

// Flush 满足 http.Flusher：io.Pipe 是同步管道，Write 即到即达，无需额外动作。
func (w *traePipeWriter) Flush() {}

func (traeInprocessRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	sub := trae2apiCurrent()
	if sub == nil || sub.srv == nil {
		return nil, errors.New("trae2api subsystem unavailable")
	}
	handler := sub.srv.Handler()
	pr, pw := io.Pipe()
	w := &traePipeWriter{header: make(http.Header), pw: pw, code: make(chan int, 1)}
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

// trae2apiCaller 平台 chat 调用器（anthropic 跨端点路径）：model 透传，
// 记账回调恒 nil（token/积分记账由 vendored metrics 按账号自行落账）。
func trae2apiCaller(model string) (platformChatCall, bool) {
	if !isTrae2apiModel(model) {
		return nil, false
	}
	if trae2apiCurrent() == nil {
		return rejectCaller("TraeWork subsystem unavailable", false), true
	}
	if trae2apiRoutableCount() == 0 {
		return rejectCaller("No routable Trae accounts. Add accounts on the Trae page.", true), true
	}
	return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, func(map[string]any), error) {
		// 流式语义以调用方参数为准（/v1/responses 恒 true 与其余平台对齐，
		// /v1/messages 跟随客户端意图），不依赖 body 里的 stream 键——vendored
		// 引擎把「缺省」当 true，与 OpenAI 缺省 false 相反，缺省请求会被错误
		// 流式化（2026-11-05 审计实测）。
		params["stream"] = stream
		// 合并域翻译（同 serveTrae2apiChat）：caller 服务跨端点路径。
		params["model"] = trae2apiTranslateMerged(model)
		body, err := json.Marshal(params)
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			traeInprocessBase+"/v1/chat/completions", bytesReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := traeInprocessHTTP.Do(req)
		if err != nil {
			return nil, nil, err
		}
		return resp, nil, nil
	}, true
}

func bytesReader(b []byte) io.Reader { return strings.NewReader(string(b)) }

// traeNormalizeStreamDefault 对请求体补 OpenAI 缺省语义：无 stream 键 = false
// （显式 true/false 原样保留）。body 非 JSON 时原样返回。
func traeNormalizeStreamDefault(body []byte) []byte {
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

// trae2apiAdminCall 进程内调用 vendored 管理端点（test/checkin/credits/
// clear-cooldown/delete），经同一管道 client，鉴权 = 匿名管理员。
func trae2apiAdminCall(method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, traeInprocessBase+path, bytesReader(body))
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := traeInprocessHTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, data, err
}

// handleTraeConfig GET /admin/api/trae/config —— 面板一次性取全：
// vendored 配置子集 + afree 域字段（费率过滤/隔离/备用）+ 账号状态表。
func handleTraeConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	sub := trae2apiCurrent()
	data := map[string]any{
		"engineVersion":           trae2apiVersion,
		"loaded":                  sub != nil,
		"useProxies":              traeProxiesEnabled(),
		"proxyIsolation":          proxyIsolationEnabled(),
		"proxyIsolationEnvLocked": proxyIsolationEnvLocked(),
		"backupProxyEnabled":      backupProxyEnabled(),
		"rateFilterCN":            trae2apiRateFilterThresholdCN(),
		"rateFilterIntl":          trae2apiRateFilterThresholdIntl(),
		"routable":                trae2apiRoutableCount(),
	}
	if sub != nil {
		cfg := sub.cfg
		data["gateway"] = cfg.Trae.ChatBase
		data["maxInFlight"] = cfg.Pool.MaxInFlight
		data["breakerThreshold"] = cfg.Pool.BreakerThreshold
		data["breakerCooldown"] = cfg.Pool.BreakerCooldown
		data["cooldownSoftRate"] = cfg.Cooldown.SoftRate
		data["cooldownSoftRateMax"] = cfg.Cooldown.SoftRateMax
		data["cooldownModel"] = cfg.Cooldown.ModelCooldown
		data["checkinEnabled"] = cfg.Schedule.CheckinEnabled
		data["checkinHours"] = cfg.Schedule.CheckinHours
		data["strictModels"] = cfg.Trae.StrictModels == nil || *cfg.Trae.StrictModels
		data["sessionSticky"] = cfg.SessionSticky.Enabled
		data["intlEnabled"] = cfg.Intl.Enabled
		data["idleTimeout"] = cfg.Trae.IdleTimeoutSeconds
		data["accounts"] = trae2apiAccountStates(sub)
		data["models"] = trae2apiModelEntries()
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// trae2apiAccountStates 账号表：vendored 池状态 + afree 双开关/绑定合并。
func trae2apiAccountStates(sub *trae2apiSubsystem) []map[string]any {
	// 锁序铁律（2026-11-04 实测死锁）：绝不能持 traeBindingsMu/traeRoutingMu
	// 调 Pool().List()——List→Status→healthyLocked→RoutingAllowed 钩子→
	// traeRoutingGet 会重入 traeRoutingMu，同 goroutine 自锁把 /v1/models、
	// config 接口与后台 Pool.Save 全部堵死。先在各自锁内取快照，再查池。
	traeBindingsMu.Lock()
	loadTraeBindingsLocked()
	bindings := make(map[string]trae2apiProxyBinding, len(traeBindings))
	for uid, v := range traeBindings {
		bindings[uid] = v
	}
	traeBindingsMu.Unlock()

	statuses := sub.srv.Pool().List()
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
			// routing 口径与实际路由判定一致（traeRoutingGet：查表缺失=启用）
			"routing":       traeRoutingGet(st.UID),
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
		trae2apiEnrichAccountRow(sub, st, row)
		out = append(out, row)
	}
	return out
}

// trae2apiEnrichAccountRow 给账号行补齐旧引擎时代面板展示的四组字段（2026-11-04
// 用户要求恢复换线前的表格列序）。数据全部取自 vendored 引擎现成结构，不新增
// 持久化：
//   - token 过期：Store.Cred.ExpiredAt（快照自带，续期后引擎回写）；
//   - 今日/累计 token 与最近使用：metrics Collector 的当日分桶（byDay） +
//     账号聚合 LastSeen——Today 口径与控制台看板一致（本地自然日）；
//   - 签到状态：签到动作由 vendored 后台/手动端点完成，此处仅回显「今日已签」
//     标记（软状态，不真打上游——面板刷新频率下逐号打 UG 接口太重）。
func trae2apiEnrichAccountRow(sub *trae2apiSubsystem, st traepool.Status, row map[string]any) {
	// token 过期（Store 可能未绑定：config accounts 形态；空值即不出口）
	if a := sub.srv.Pool().Get(st.UID); a != nil && a.Store != nil {
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
	today, total, lastUsed, _ := trae2apiTokensSnapshot(st.UID)
	row["tokensToday"] = today
	row["tokensTotal"] = total
	if lastUsed != "" {
		row["lastUsed"] = lastUsed
	}
	// 签到状态：引擎持久化记录（CheckinAll 动作后 NoteCheckin 回写，随池
	// state.json 持久化；空日期 = 从未执行过签到，面板不显示徽章）
	if st.CheckinDate != "" {
		row["checkin"] = map[string]any{
			"checkedIn": st.CheckinChecked,
			"date":      st.CheckinDate,
			"lastError": st.CheckinError,
			"failed":    st.CheckinFailed,
		}
	}
}

// handleTraeConfigUpdate POST /admin/api/trae/config/update —— vendored 配置
// 子集 + 费率过滤 patch；vendored 无热更新，落盘后整组重启（池状态经
// state.json 保留，冷却/熔断不丢）。useProxies 走 proxy-config.json 既有键。
// traeApplyRateFilterPatch 倍率筛选 patch 应用（2026-11-07 拆分）：分域键
// 独立于旧单键生效——面板只发新键（rateFilter 为 nil）时旧键 if 块绝不参与，
// 两者并存时按「新键优先、旧键双写兜底」收敛到一致。抽出独立函数便于在
// 子系统未启动的单测环境直接验证应用逻辑（handler 侧有 503 守卫）。
// traeRateFilterPatch 倍率筛选 patch 的三个键（从 handleTraeConfigUpdate 的
// 大 patch struct 里独立出来，便于单测直调）。
type traeRateFilterPatch struct {
	RateFilter     *float64 `json:"rateFilter"`
	RateFilterCN   *float64 `json:"rateFilterCN"`
	RateFilterIntl *float64 `json:"rateFilterIntl"`
}

func traeApplyRateFilterPatch(patch traeRateFilterPatch) {
	// 经 getProxyBindingConfig→setProxyBindingConfig 同源路径写：倍率已在
	// 隔离/绑定段（随备份迁移），直改文件会绕过 proxyBindingCfg 内存缓存，
	// 后续 setProxyBindingConfig 会拿缓存旧值把刚存的倍率抹回去。
	if patch.RateFilter != nil {
		// 旧单键兼容：双写两域（面板已改用新键，此路径仅供旧脚本/手工 API）。
		next := *getProxyBindingConfig()
		next.Trae2apiModelRateFilter = *patch.RateFilter
		next.Trae2apiModelRateFilterCN = patch.RateFilter
		next.Trae2apiModelRateFilterIntl = patch.RateFilter
		setProxyBindingConfig(&next)
	}
	if patch.RateFilterCN != nil || patch.RateFilterIntl != nil {
		next := *getProxyBindingConfig()
		if patch.RateFilterCN != nil {
			next.Trae2apiModelRateFilterCN = patch.RateFilterCN
		}
		if patch.RateFilterIntl != nil {
			next.Trae2apiModelRateFilterIntl = patch.RateFilterIntl
		}
		setProxyBindingConfig(&next)
	}
}

func handleTraeConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var patch struct {
		Gateway          *string  `json:"gateway"`
		MaxInFlight      *int     `json:"maxInFlight"`
		BreakerThreshold *int     `json:"breakerThreshold"`
		BreakerCooldown  *string  `json:"breakerCooldown"`
		CooldownSoftRate *string  `json:"cooldownSoftRate"`
		CooldownSoftMax  *string  `json:"cooldownSoftRateMax"`
		CooldownModel    *string  `json:"cooldownModel"`
		CheckinEnabled   *bool    `json:"checkinEnabled"`
		CheckinHours     *[]int   `json:"checkinHours"`
		StrictModels     *bool    `json:"strictModels"`
		SessionSticky    *bool    `json:"sessionSticky"`
		IdleTimeout      *int     `json:"idleTimeout"`
		IntlEnabled      *bool    `json:"intlEnabled"`
		RateFilter       *float64 `json:"rateFilter"`
		// 分域倍率筛选（2026-11-07 拆分）：新键优先；旧 rateFilter 键为兼容
		// 保留，非空时双写两域（回落语义与读取端一致）。
		RateFilterCN   *float64 `json:"rateFilterCN"`
		RateFilterIntl *float64 `json:"rateFilterIntl"`
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}

	sub := trae2apiCurrent()
	if sub == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	sub.mu.Lock()
	cfg := sub.cfg
	// gateway 不再接受面板修改（2026-11-04 用户定）：沿用上游 chat_base 默认
	// （traeApplyAfreeConstraints 兜底补值）。patch 里的 gateway 字段直接忽略
	// ——保持 API 兼容（老客户端不发新请求不会炸），但改了也不生效，防止出现
	// "面板上没有的开关却暗中生效"的悖离。trae2apiValidateGateway 保留给
	// config 文件校验路径。
	_ = patch.Gateway
	if patch.MaxInFlight != nil && *patch.MaxInFlight > 0 {
		cfg.Pool.MaxInFlight = *patch.MaxInFlight
	}
	if patch.BreakerThreshold != nil && *patch.BreakerThreshold > 0 {
		cfg.Pool.BreakerThreshold = *patch.BreakerThreshold
	}
	if patch.BreakerCooldown != nil {
		cfg.Pool.BreakerCooldown = strings.TrimSpace(*patch.BreakerCooldown)
	}
	if patch.CooldownSoftRate != nil {
		cfg.Cooldown.SoftRate = strings.TrimSpace(*patch.CooldownSoftRate)
	}
	if patch.CooldownSoftMax != nil {
		cfg.Cooldown.SoftRateMax = strings.TrimSpace(*patch.CooldownSoftMax)
	}
	if patch.CooldownModel != nil {
		cfg.Cooldown.ModelCooldown = strings.TrimSpace(*patch.CooldownModel)
	}
	if patch.CheckinEnabled != nil {
		cfg.Schedule.CheckinEnabled = *patch.CheckinEnabled
	}
	if patch.CheckinHours != nil {
		cfg.Schedule.CheckinHours = *patch.CheckinHours
	}
	if patch.StrictModels != nil {
		v := *patch.StrictModels
		cfg.Trae.StrictModels = &v
	}
	if patch.SessionSticky != nil {
		cfg.SessionSticky.Enabled = *patch.SessionSticky
	}
	if patch.IdleTimeout != nil && *patch.IdleTimeout > 0 {
		cfg.Trae.IdleTimeoutSeconds = *patch.IdleTimeout
	}
	if patch.IntlEnabled != nil {
		cfg.Intl.Enabled = *patch.IntlEnabled
	}
	// afree 硬约束在序列化前重新套上（面板改不动的字段恒定）。
	traeApplyAfreeConstraints(cfg)
	raw, err := json.MarshalIndent(cfg, "", "  ")
	sub.mu.Unlock()
	if err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	if err := os.WriteFile(traeConfigPath(), raw, 0o600); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	// 经 getProxyBindingConfig→setProxyBindingConfig 同源路径写：倍率已在
	// 隔离/绑定段（随备份迁移），直改文件会绕过 proxyBindingCfg 内存缓存，
	// 后续 setProxyBindingConfig 会拿缓存旧值把刚存的倍率抹回去。
	traeApplyRateFilterPatch(traeRateFilterPatch{
		RateFilter:     patch.RateFilter,
		RateFilterCN:   patch.RateFilterCN,
		RateFilterIntl: patch.RateFilterIntl,
	})
	if err := trae2apiRestart(); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: "restart: " + err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"restarted": true}})
}

// trae2apiValidateGateway 网关域校验（判据自旧引擎 oauthOnlyHostFragments
// 移植，2026-11-04 实测补缺）：OAuth/账号路由域作网关必失败——只承载 OAuth、
// 不能发模型请求的域名直接拒绝保存，免得整池静默 5xx。
func trae2apiValidateGateway(gw string) error {
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

// handleTraeModelsRefresh POST /admin/api/trae/models/refresh —— vendored
// 目录随启动/intl 配置静态成型，这里仅重读并回传（老面板刷新按钮的语义）。
func handleTraeModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	models := trae2apiModelEntries()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"models": models, "total": len(models), "added": 0}})
}

// handleTraeAccountSetEnabled POST /admin/api/trae/accounts/enabled —— 账号
// 总闸（vendored disabled 字段）。关闭时连带关掉反代开关（互锁，与 wb 同款）：
// 账号不存在了，路由参与位没有意义；重开账号不自动恢复路由（显式操作）。
func handleTraeAccountSetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID     string `json:"uid"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	if sub.srv.Pool().Get(p.UID) == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "account not found: " + p.UID})
		return
	}
	reason := ""
	if !p.Enabled {
		reason = "disabled from panel"
	}
	if sub.srv.Pool().SetDisabled(p.UID, !p.Enabled, reason) {
		_ = sub.srv.Pool().Save()
	}
	if !p.Enabled && traeRoutingGet(p.UID) {
		traeRoutingSet(p.UID, false)
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountSetRouting POST /admin/api/trae/accounts/routing —— 反代
// 开关（路由参与位）。账号总闸关闭时不可打开（互锁）。
func handleTraeAccountSetRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID     string `json:"uid"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	if a := sub.srv.Pool().Get(p.UID); a == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "account not found: " + p.UID})
		return
	} else if st := a.Status(time.Now()); st.Disabled && p.Enabled {
		writeAPI(w, http.StatusBadRequest, apiResponse{
			Error: "account switch is off; turn the account switch on first"})
		return
	}
	traeRoutingSet(p.UID, p.Enabled)
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountSetRoutingAll POST /admin/api/trae/accounts/routing/all ——
// 批量反代开关。enabled=false 全关；enabled=true 仅作用于"账号总闸开"的行。
func handleTraeAccountSetRoutingAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	for _, st := range sub.srv.Pool().List() {
		if !p.Enabled || !st.Disabled {
			traeRoutingSet(st.UID, p.Enabled)
		}
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountSetProxy POST /admin/api/trae/accounts/proxy —— 主/备
// 出口绑定（traeBindings 段，与 workbuddyBindings 同语义）。
func handleTraeAccountSetProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID    string `json:"uid"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	if sub.srv.Pool().Get(p.UID) == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "account not found: " + p.UID})
		return
	}
	if p.Main == "" && p.Backup == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "both main and backup are empty; use proxy/clear"})
		return
	}
	traeBindingsMu.Lock()
	loadTraeBindingsLocked()
	prev := traeBindings[p.UID]
	main := strings.TrimSpace(p.Main)
	backup := resolveBackupSlot(strings.TrimSpace(p.Backup), prev.Backup)
	if err := validateProxyBinding(main, backup); err != nil {
		traeBindingsMu.Unlock()
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	traeBindings[p.UID] = trae2apiProxyBinding{Main: main, Backup: backup}
	saveTraeBindingsLocked()
	traeBindingsMu.Unlock()
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// exportTrae2apiProxyBindings TraeWork 账号出口绑定（proxy-config.json 的
// trae2apiBindings 段）随配置备份迁移（2026-11-05 用户实测补缺：导出→删卷→
// 导入后绑定丢失，wb 同类缺口 2026-10-14 已补、trae 换线时没跟）。
func exportTrae2apiProxyBindings() map[string]trae2apiProxyBinding {
	traeBindingsMu.Lock()
	defer traeBindingsMu.Unlock()
	loadTraeBindingsLocked()
	if len(traeBindings) == 0 {
		return nil
	}
	out := make(map[string]trae2apiProxyBinding, len(traeBindings))
	for k, v := range traeBindings {
		out[k] = v
	}
	return out
}

// importTrae2apiProxyBindings 还原 TraeWork 出口绑定，返回写入条数。与 wb
// 同采用"整体替换"：备份里出现的 uid 覆盖本机值；本机多出来的 uid 一并清掉，
// 避免跨机迁移后残留旧机的绑定。
func importTrae2apiProxyBindings(in map[string]trae2apiProxyBinding) int {
	traeBindingsMu.Lock()
	defer traeBindingsMu.Unlock()
	loadTraeBindingsLocked()
	next := make(map[string]trae2apiProxyBinding, len(in))
	for k, v := range in {
		next[k] = v
	}
	traeBindings = next
	saveTraeBindingsLocked()
	return len(next)
}

// handleTraeAccountClearProxies POST /admin/api/trae/accounts/proxy/clear ——
// 「清空代理」语义与各平台一致：清绑定 + 该账号路由置禁用（二者永远绑一起）。
func handleTraeAccountClearProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	traeBindingsMu.Lock()
	loadTraeBindingsLocked()
	delete(traeBindings, p.UID)
	saveTraeBindingsLocked()
	traeBindingsMu.Unlock()
	traeRoutingSet(p.UID, false)
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountDelete POST /admin/api/trae/accounts/delete —— 删除快照
// 文件（vendored DELETE /admin/accounts/{name}），连带清理 afree 侧路由位。
func handleTraeAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	code, data, err := trae2apiAdminCall(http.MethodDelete, "/admin/accounts/"+p.UID, nil)
	if err != nil {
		writeAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	if code >= 400 {
		writeAPI(w, code, apiResponse{Error: strings.TrimSpace(string(data))})
		return
	}
	traeBindingsMu.Lock()
	loadTraeBindingsLocked()
	delete(traeBindings, p.UID)
	saveTraeBindingsLocked()
	traeBindingsMu.Unlock()
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountLabel POST /admin/api/trae/accounts/label —— 昵称写入
// vendored Store（Label 字段随快照持久化）。
func handleTraeAccountLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID   string `json:"uid"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	a := sub.srv.Pool().Get(p.UID)
	if a == nil || a.Store == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "account not found: " + p.UID})
		return
	}
	// 快照文件是标签的持久层（重启后渠道重建以文件为准），运行时标签经
	// Store.Load() + 池 SetLabel 同步（vendor 注入的并发安全写入口）。
	label := strings.TrimSpace(p.Label)
	path := a.Store.Path()
	raw, err := os.ReadFile(path)
	if err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	enc, _ := json.Marshal(label)
	m["label"] = enc
	out, err := json.Marshal(m)
	if err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	if err := a.Store.Load(); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	a.SetLabel(label)
	_ = sub.srv.Pool().Save()
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountsReorder POST /admin/api/trae/accounts/reorder body: { uids: [...] }
// 凭据表拖拽排序（2026-11-05 用户定）：仅改展示顺序（vendored 池 order），
// 选号是引擎加权随机与顺序无关——与 WorkBuddy SetOrder 同语义。
func handleTraeAccountsReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if trae2apiCurrent() == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	var req struct {
		UIDs []string `json:"uids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if len(req.UIDs) == 0 {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "uids is required"})
		return
	}
	n := trae2apiSetDisplayOrder(req.UIDs)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"ordered": n}})
}

// handleTraeAccountTest POST /admin/api/trae/accounts/test —— 沿用上游设计：
// 额度连通性验证（真实 UG 调用 + 失败自动续期一次），无模型探测。
func handleTraeAccountTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/accounts/"+p.UID+"/test", nil)
	if err != nil {
		writeAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	var vendored map[string]any
	_ = json.Unmarshal(data, &vendored)
	writeAPI(w, code, apiResponse{Success: code < 400, Data: vendored, Error: vendoredErr(vendored, code)})
}

// handleTraeCheckin POST /admin/api/trae/checkin —— 全量签到（vendored
// CheckinAll：跳过国际版/SOLO、9074 换设备退避重试，签到失败不罚账号）。
func handleTraeCheckin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/checkin", nil)
	if err != nil {
		writeAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	var vendored struct {
		Results []map[string]any `json:"results"`
	}
	_ = json.Unmarshal(data, &vendored)
	writeAPI(w, code, apiResponse{Success: code < 400, Data: vendored, Error: vendoredErrCode(code)})
}

// vendoredErrCode 无错误体时的兜底错误文案（HTTP 状态码）。
func vendoredErrCode(code int) string {
	if code < 400 {
		return ""
	}
	return fmt.Sprintf("vendored endpoint returned HTTP %d", code)
}

// 刷新（vendored RefreshCredits）。
func handleTraeCreditsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/credits/refresh", nil)
	if err != nil {
		writeAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	var vendored map[string]any
	_ = json.Unmarshal(data, &vendored)
	writeAPI(w, code, apiResponse{Success: code < 400, Data: vendored, Error: vendoredErr(vendored, code)})
}

// handleTraeCooldownClear POST /admin/api/trae/accounts/cooldown-clear ——
// 解除账号冷却（vendored clear-cooldown）。
func handleTraeCooldownClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/accounts/"+p.UID+"/clear-cooldown", nil)
	if err != nil {
		writeAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, code, apiResponse{Success: code < 400, Data: json.RawMessage(data)})
}

// vendoredErr 从 vendored OpenAI 风格错误体取可读消息。
func vendoredErr(vendored map[string]any, code int) string {
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

// ============ 原版 console 挂载（/admin/trae2api/） ============

// trae2apiConsoleHandler 原版入口：vendored console 挂在本仓 /admin/trae2api/ 下
// （新标签页打开，与 WorkBuddy「Original panel」同模式）。路径重写：
//
//	/admin/trae2api/api/X → /X  （console fetch 已被 vendor 改写到该基址）
//	/admin/trae2api/X     → /X  （页面本体 /console 与根路径资源）
//
// 门禁复用 afree 管理会话 cookie；vendored 侧匿名管理员（cfg.APIKey/webPassword
// 恒空），对公网不可达（本 handler 只挂在 afree /admin 鉴权之后）。
func trae2apiConsoleHandler() http.Handler {
	return adminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub := trae2apiCurrent()
		if sub == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "TraeWork subsystem unavailable"})
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/admin/trae2api")
		if rest == "" || rest == "/" {
			rest = "/console"
		}
		if strings.HasPrefix(rest, "/api/") {
			rest = strings.TrimPrefix(rest, "/api")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = rest
		sub.srv.Handler().ServeHTTP(w, r2)
	}))
}

// ============ 备份导出 / 导入 ============

// trae2apiBackupPayload 备份的 trae 段形态（2026-11-04 起）。config =
// data/trae2api/config.json 原文；accounts = 快照文件名 → 快照 JSON；routing =
// 反代开关表。旧引擎（trae-config.json 账号池）的形态不再兼容。
type trae2apiBackupPayload struct {
	Config   json.RawMessage            `json:"config,omitempty"`
	Routing  map[string]bool            `json:"routing,omitempty"`
	Accounts map[string]json.RawMessage `json:"accounts,omitempty"`
}

// trae2apiBackupSnapshot 备份导出：配置原文 + 全部账号快照 + 路由表。
// vendored state.json（冷却/熔断/额度缓存）属使用痕迹，不随备份迁移。
func trae2apiBackupSnapshot() json.RawMessage {
	payload := trae2apiBackupPayload{Routing: traeRoutingCopy()}
	if raw, err := os.ReadFile(traeConfigPath()); err == nil && json.Valid(raw) {
		payload.Config = json.RawMessage(raw)
	}
	acc := map[string]json.RawMessage{}
	entries, _ := os.ReadDir(filepath.Join(traeDataDir(), "accounts"))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(traeDataDir(), "accounts", e.Name()))
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

func traeRoutingCopy() map[string]bool {
	traeRoutingMu.Lock()
	defer traeRoutingMu.Unlock()
	loadTraeRoutingLocked()
	cp := make(map[string]bool, len(traeRouting))
	for uid, v := range traeRouting {
		cp[uid] = v
	}
	return cp
}

// trae2apiRestoreBackup 备份导入：还原 config 与账号快照文件 + 路由表。
// 隔离护栏：未勾选"沿用启用状态"时全部身份路由置禁用（与六平台口径一致）。
// 旧引擎形态的 trae 段（无 config/accounts 键）报错跳过，不迁移。
// 返回还原的账号数。
func trae2apiRestoreBackup(raw json.RawMessage, keepEnabled bool) (int, error) {
	var payload trae2apiBackupPayload
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
		// （traeApplyAfreeConstraints），落盘文件里 chat_base 合法为空串，按值
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
		ensureTraeDataDir()
		if err := os.WriteFile(traeConfigPath(), payload.Config, 0o600); err != nil {
			return 0, err
		}
	}
	accountsDir := filepath.Join(traeDataDir(), "accounts")
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
				traeRoutingSet(uid, v)
			}
		} else {
			traeRoutingSet(uid, false)
		}
	}
	if n > 0 || payload.Config != nil {
		_ = trae2apiRestart()
	}
	return n, nil
}

// trae2apiProbeBlacklistedModel 黑名单页对 trae 封禁模型的定向探测：进程内
// 发一次最小 chat 请求（真实计费意图极小：max_tokens=16），返回上游真实反应
// 供人工判断"该不该解禁"。与旧引擎 traeProbeBlacklistedModel 同语义。
func trae2apiProbeBlacklistedModel(model string) (map[string]any, string) {
	params := map[string]any{
		"model":      model,
		"stream":     false,
		"max_tokens": 16,
		"messages":   []map[string]any{{"role": "user", "content": "Reply with exactly: OK"}},
	}
	body, err := json.Marshal(params)
	if err != nil {
		return map[string]any{"ok": false, "model": model, "error": err.Error()}, "error"
	}
	code, data, err := trae2apiAdminCall(http.MethodPost, "/v1/chat/completions", body)
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

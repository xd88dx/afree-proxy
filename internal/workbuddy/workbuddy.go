package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"afree-proxy/internal/exitpool"

	"afree-proxy/internal/kit"
	wbbridge "github.com/misakano7545/workbuddy2api-panel"
)

// 版本口径：misakano 线不跟语义化版本（CI 用日期版本注入），此处记录上游来源
// 与同步日期；基线 misakano/main 5b40d46（2026-11-21 同步）。
const WorkbuddyVersion = "2026.11.21-misakano+afree"

// WorkbuddySubsystem 把源项目的账号池/熔断/调度/面板/兼容接口作为独立子系统
// 挂在现有 afree-proxy 上。账号凭证与状态落在 data/workbuddy/ 下，不与 Cline
// 池或 OpenCode key 混用；cn:/global: 路由由 WorkbuddySubsystem 接管。
type WorkbuddySubsystem struct {
	CfgPath string
	Dir     string
	Cfg     *wbbridge.WBConfig

	Pool       *wbbridge.Pool
	Upstream   *wbbridge.UpClient
	Scheduler  *wbbridge.Scheduler
	Panel      *wbbridge.Panel
	Server     WorkbuddyServer
	Usage      *wbbridge.UsageRecorder
	RequestLog *wbbridge.ReqlogRecorder
	Session    *wbbridge.SessionRouter
	Live       *wbbridge.LivecfgHolder
	Store      wbbridge.RedisStore

	// saveConfig 与面板 SaveConfig 同源（SaveWorkbuddyConfig 闭包，workbuddy.go
	// 构造时赋值）：权重优化写入 account_share 复用同一条落盘+热更新链，
	// 避免两处配置保存口径漂移。
	SaveConfig func(raw []byte) ([]string, error)

	// shareOptimizeSched 权重优化定时调度热值（schedule.share_optimize_*，
	// afree 补丁字段随 config.go PRESERVE 登记）：SaveWorkbuddyConfig 热应用段
	// 写入，调度 worker 30s tick 读——改 cron 无需重启。
	ShareOptimizeSched atomic.Value

	Cancel context.CancelFunc

	Mu      sync.Mutex
	Stopped bool
}

// WorkbuddyServer keeps the subsystem decoupled from the concrete
// workbuddy/wbbridge.Handler only far enough to exercise the gateway adapters
// in unit tests. Production still constructs *wbbridge.Handler.
type WorkbuddyServer interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
	ModelList() []map[string]any
}

var WorkbuddySub *WorkbuddySubsystem

// replayWbDisplayOrder 把 wkbd-config.json displayOrder 回放到 vendor 池
// （pool.SetOrder 仅影响展示顺序，不参与选号路由）。wkbd 侧未记录顺序时做
// 一次升级播种：把 vendor state.json 的 order（旧落盘位置，运行态不进同步
// 面）搬进 wkbd-config，此后顺序意志随配置同步面走。
func replayWbDisplayOrder(p *wbbridge.Pool, stateFile string) {
	order := WkbdConfigDisplayOrder()
	if len(order) == 0 {
		if seeded := wbStateOrderSeed(stateFile); len(seeded) > 0 {
			WkbdSetDisplayOrder(seeded)
			order = seeded
			log.Printf("workbuddy: display order seeded from state.json into wkbd-config (%d uids)", len(seeded))
		}
	}
	if len(order) > 0 {
		if n := p.SetOrder(order); n > 0 {
			log.Printf("workbuddy: display order replayed (%d accounts)", n)
		}
	}
}

// wbStateOrderSeed 从 vendor state.json 读取 order 字段（升级播种用）。
// 文件缺失、损坏或无 order 时返回 nil——不猜写。
func wbStateOrderSeed(stateFile string) []string {
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return nil
	}
	var snap struct {
		Order []string `json:"order"`
	}
	if json.Unmarshal(raw, &snap) != nil {
		return nil
	}
	return snap.Order
}

// StartWorkBuddy 装配子系统。返回错误时调用方只记录日志并继续启动 afree-proxy，
// 保证现有 Cline/OpenCode 链路不受 WorkBuddy 配置故障影响。
func StartWorkBuddy() (*WorkbuddySubsystem, error) {
	stateFile := kit.ResolveDataPath(filepath.Join("workbuddy", "state.json"))
	dir := filepath.Dir(stateFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir workbuddy dir: %w", err)
	}
	authDir := filepath.Join(dir, "auths")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir workbuddy auth dir: %w", err)
	}
	cfgPath := filepath.Join(dir, "config.json")

	cfg, err := EnsureWorkbuddyConfig(cfgPath, authDir, stateFile)
	if err != nil {
		return nil, err
	}
	// 面板认证由 afree-proxy 的 /admin/* 会话统一负责；WorkBuddy 自己的 api_key
	// 不参与鉴权，避免同一部署出现两套互不相干的密钥门。
	cfg.APIKey = ""
	cfg.AuthDir = authDir
	cfg.StateFile = stateFile
	cfg.Listen = ""

	wbbridge.AuthSetGlobalEnabled(cfg.Global.Enabled)
	wbbridge.UpSetModelCatalogPath(filepath.Join(dir, "model.json"))

	store := wbbridge.RedisNew(cfg.Upstash.URL, cfg.Upstash.Token)
	// 账号意志钩子必须在 AuthLoadDir（auth 文件解析）之前注册——解析完成时
	// AccountPrefProvider 回填内存意志（真身住 wkbd-config.json accountPrefs，
	// 2026-11-17 外置，auth 文件回归纯凭据）。
	wbbridge.SetAuthAccountPrefProvider(WkbdAccountPrefProvider)
	wbbridge.SetAuthAccountPrefObserver(WorkbuddyAccountPrefObserver)
	p := wbbridge.PoolNew(cfg.StateFile)
	p.SetStore(store)
	p.RestoreFromSnapshot()
	auths, err := wbbridge.AuthLoadDir(cfg.AuthDir)
	if err != nil {
		log.Printf("workbuddy: load auth dir %s: %v", cfg.AuthDir, err)
	}
	p.SyncToDir(auths)
	// 账号显示顺序回放（2026-11-21 顺序意志外置到 wkbd-config.json
	// displayOrder 段）：有自定义顺序时覆盖 RestoreFromSnapshot 带回的
	// state.json order（同步源 iStore 意志优先）；空 = 未记录 = 保持 vendor
	// 原序。首次升级时从 state.json order 播种一份进 wkbd-config，此后随
	// 配置同步面走，两环境不再各自漂移。
	replayWbDisplayOrder(p, stateFile)
	p.SetBreaker(cfg.Pool.BreakerThreshold, cfg.BreakerCooldownDur, cfg.BreakerCooldownMaxD)
	p.SetMaxInFlight(cfg.Pool.MaxInFlight)
	p.SetMaxInFlightGlobal(cfg.Pool.MaxInFlightGlobal)
	p.SetDegrade(cfg.Pool.DegradeThreshold, cfg.DegradeCooldownDur, cfg.DegradeCooldownMaxD)
	p.SetSoftRateMax(cfg.SoftRateMaxDur)
	p.SetCostExploreInterval(cfg.CostExploreIntervalDur)
	p.SetCreditFloor(cfg.Pool.CreditFloor)
	p.SetWeights(cfg.Pool.IdleWeightPerHour, cfg.Pool.IdleWeightMax)
	p.SetPreferExpiring(cfg.Pool.PreferExpiring, cfg.ExpiringSoonDur)
	_ = p.SetScheduling(cfg.Pool.Strategy, cfg.Pool.AccountOrder)
	// misakano 线新增：保留积分（normalize 保证非 nil 且 >=0）、账号优先级/占比
	//（issue #62）、保底倍率上限——与 SaveWorkbuddyConfig 热应用同键。
	p.SetReserveCredits(*cfg.Pool.ReserveCredits)
	p.SetReserveCreditsByRealm(cfg.Pool.ReserveCreditsByRealm) // 按域覆盖保留积分（issue #147）
	p.SetAccountPriority(cfg.Pool.AccountPriority)
	p.SetAccountShare(cfg.Pool.AccountShare)
	p.SetCreditFloorMaxRate(cfg.Pool.CreditFloorMaxRate)
	// 窗口用量调度（上游 40d98f9 / issue #112）：按 (账号, 模型) 窗口消费 token
	// 反比降权打散热点；缺省 1h，"0" 关停。热路径同键。
	p.SetBalanceWindow(cfg.BalanceWindowDur)

	up := wbbridge.UpNew()
	up.HTTP.Timeout = time.Duration(cfg.Upstream.TimeoutSeconds) * time.Second
	up.HeaderTimeout = time.Duration(cfg.Upstream.HeaderTimeoutSeconds) * time.Second
	if tr, ok := up.ChatHTTP.Transport.(*http.Transport); ok {
		tr.ResponseHeaderTimeout = up.HeaderTimeout
	}
	up.IdleTimeout = time.Duration(cfg.Upstream.IdleTimeoutSeconds) * time.Second
	up.SanitizeFingerprints.Store(cfg.Features.SanitizeBlacklistFingerprints)
	up.SanitizeBrandScope.Store(cfg.Features.SanitizeBrandScope)
	// 出站 TLS 指纹档位（上游 80e275e / PR #135）：缺省 "node" 复刻官方
	// ClientHello（JA3/JA4 对齐真机），空值等价 node；配错保持 node 档不致命。
	if err := up.SetTLSFingerprint(cfg.Upstream.TLSFingerprint); err != nil {
		log.Printf("workbuddy: tls_fingerprint 配置无效，保持 node 档: %v", err)
	}
	// keep_recent_rounds（上游 issue #99）：出站前只保留最近 N 轮对话，0=全量透传。
	up.KeepRecentRounds.Store(int64(cfg.Prompt.KeepRecentRounds))
	// 思考档位策略（上游 thinking 节 / issue #18、#21）：启动即装载，面板保存
	// 配置时经 SaveWorkbuddyConfig 热更新。缺省启用（default_effort=max），
	// 不接这一行则客户端缺档时网关不再补档——上游默认行为会静默丢失。
	up.SetEffortPolicy(EffortPolicyOf(cfg))
	if cfg.Thinking.Enabled {
		log.Printf("workbuddy: 思考档位策略开，默认档 %s（客户端优先=%v，作用域=%s）",
			cfg.Thinking.DefaultEffort, cfg.Thinking.ClientPriority, cfg.Thinking.OverrideScope)
	} else {
		log.Printf("workbuddy: 思考档位策略关（不补档、不改写客户端档位）")
	}
	up.UserAgent = cfg.Upstream.UserAgent
	up.ClientVersion = cfg.Upstream.ClientVersion
	up.CliVersion = cfg.Upstream.CliVersion
	up.ClientName = cfg.Upstream.ClientName
	up.DeviceToken = cfg.Upstream.DeviceToken
	up.DeviceTokenFile = cfg.Upstream.DeviceTokenFile
	up.PassthroughIP = cfg.Upstream.PassthroughIP
	up.GlobalEnabled = cfg.Global.Enabled
	up.ChatBaseGlobal = cfg.Global.ChatBase
	// cn.chat_base 国内版 base 覆盖（上游 7c30d43）：空 = 回落内置默认
	// copilot.tencent.com。海外出口被该域 TLS 复位时可改 www.workbuddy.cn。
	// 仅在显式非空时覆盖：vendored chatBase() **没有空值回退**（只有 globalChatBase 有），
	// 内置默认 copilot.tencent.com 由 wbbridge.UpNew() 写入 Client.ChatBaseCN，空串覆盖会让
	// CN 模型目录与对话全部报 "unsupported protocol scheme \"\""（2026-10-22 部署实测）。
	if cfg.CN.ChatBase != "" {
		up.ChatBaseCN = cfg.CN.ChatBase
	}
	// cn.billing_base 国内 billing/growth 上游 base 覆盖（上游 9ca34c2，5.9.3
	// 客户端默认 www.workbuddy.cn）：与 cn.chat_base 同口径——vendored
	// billingBase() 对 CN **没有空值回退**，仅在显式非空时覆盖，退回旧域
	// codebuddy.cn 时手工填该键。
	if cfg.CN.BillingBase != "" {
		up.BillingBaseCN = cfg.CN.BillingBase
	}
	// dns_servers / WB2A_DNS（上游 eacee47）：仅作用于 WorkBuddy 出站解析。
	// 不动 http.DefaultTransport——afree 多平台共进程，改全局会波及其他平台出站。
	if len(cfg.DNSServers) > 0 {
		up.SetDNSDialer(wbbridge.UpNewDNSDialer(cfg.DNSServers))
		log.Printf("workbuddy: DNS 出站解析用 %v", cfg.DNSServers)
	}
	up.BillingBaseGlobal = cfg.Global.BillingBase
	up.ProxyFor = WorkbuddyProxyFor
	// kit 全局开关进 vendor 的两个判据点改钩子注入（vendor 树零外部依赖，
	// 独立 module 化前置）：UNABLE_FORCE 全冷却兜底放行 / ONLY_ROUTER 维护
	// 任务禁用。nil = 上游语义（默认拒绝兜底 / 允许补跑）。
	wbbridge.SetPoolUnableForce(kit.UnableForce)
	wbbridge.SetServerOnlyRouter(kit.OnlyRouter)
	// 请求日志的「账号/Key」「代理」两列：vendor 选号后把命中的账号与
	// 实际出口回报，网关侧据此填日志槽位（workerbuddyObserveAccount）。
	up.ObserveAccount = WorkbuddyObserveAccount
	// 出口成败观测（代理在线率统计）：WB 路径的拨号在 vendor 树内完成，成败
	// 只能靠这个钩子透出。直连出口与 ctx 取消由钩子/filter 各自忽略。
	up.ObserveProxy = func(u string, ok bool, e error, ms int64) {
		exitpool.RecordProxyOutcome(exitpool.KindWb, u, ok, e, ms)
	}
	// 积分保底的「收费」兜底判据：接上游模型目录的积分倍率表。本地实测台账无
	// 观测时用它判收费；倍率表由探测下发，闭包每次调用读实时快照。
	p.SetModelRateOf(up.ModelRate)
	// 模型 exclusive 闸（上游 7a48d72）：目录 modelTiers accessMode=exclusive 且账号
	// 付费状态已知为免费时事前不派单。付费状态由 billing summary 自动记录
	// （NotePaidUser），两项注入任缺其一闸门即关闭（缺数据宁放行不误杀）。
	p.SetExclusiveModels(up.ModelExclusive, up.PaidUser)

	var sess *wbbridge.SessionRouter
	if cfg.SessionSticky.Enabled {
		sess = wbbridge.SessionNew(wbbridge.SessionConfig{
			TTL:        cfg.SessionTTL,
			GCInterval: cfg.SessionGCInterval,
			Store:      store,
			Available:  p.AvailableUIDs,
			AvailableForModel: func(model string) []string {
				realm, bare := wbbridge.ServerResolveModel(model)
				return p.WeightedAvailableUIDsForModelRealm(bare, realm)
			},
		})
		sess.LoadFromStore()
		sess.StartGC()
	}
	stickyCount := func() int {
		if sess != nil {
			return sess.Count()
		}
		return 0
	}

	sch := wbbridge.SchedNew(wbbridge.SchedConfig{
		Pool:               p,
		Upstream:           up,
		CheckinHours:       cfg.Schedule.CheckinHours,
		TravelHours:        cfg.Schedule.TravelHours,
		ActivityHours:      cfg.Schedule.ActivityHours,
		KeepaliveHours:     cfg.Schedule.KeepaliveHours,
		GrowthHours:        cfg.Schedule.GrowthHours,
		ExpiringSoonWindow: cfg.ExpiringSoonDur,
		CheckinDisabled:    !cfg.Schedule.CheckinEnabled,
		TravelDisabled:     !cfg.Schedule.TravelEnabled,
		ActivityDisabled:   !cfg.Schedule.ActivityEnabled,
		// 国际版「网页通道」每日活跃腿（上游 issue #111）：缺省关，显式 true 才跑。
		WebActivityDisabled: !cfg.Schedule.WebActivityEnabled,
		KeepaliveDisabled:   !cfg.Schedule.KeepaliveEnabled,
		GrowthDisabled:      !cfg.Schedule.GrowthEnabled,
		// 保号类四任务是否覆盖禁用账号（缺省 false = 禁用即跳过，与源项目同口径）。
		IncludeDisabledInTasks: cfg.Schedule.IncludeDisabledInTasks,
	})

	allowedModels, allowlistEnabled := wbbridge.LivecfgNormalizeAllowedModels(cfg.Models.Allowed)
	live := wbbridge.LivecfgNew(wbbridge.LivecfgSnapshot{
		APIKey:                "",
		SoftCooldown:          cfg.SoftRateDur,
		SanitizeFingerprints:  cfg.Features.SanitizeBlacklistFingerprints,
		RecordClientInfo:      cfg.Logging.RequestClientInfo,
		RecordContext:         cfg.Logging.RequestArchiveContext,
		ModelRateFilterCN:     cfg.Pool.ModelRateFilterCN,
		ModelRateFilterGlobal: cfg.Pool.ModelRateFilterGlobal,
		HiddenModels:          wbbridge.LivecfgNormalizeHiddenModels(cfg.Models.Hidden),
		// models.allowed 白名单（上游 issue #68，37b3f4c）：与 Hidden 对称的热字段。
		AllowedModels:    allowedModels,
		AllowlistEnabled: allowlistEnabled,
		// misakano 线新增热字段：模型名映射与 6004 降级路由表（见 wbbridge.LivecfgSnapshot）。
		ModelAliases:  wbbridge.LivecfgNormalizeModelAliases(cfg.Models.Aliases),
		ModelFallback: cfg.Pool.ModelFallback,
		// 思考档位策略镜像（上游 thinking 节）：模型页的「网关档位」列据此
		// 显示当前生效值，缺了这几项面板一律显示「策略关闭」。
		ThinkingEnabled:        cfg.Thinking.Enabled,
		ThinkingDefaultEffort:  cfg.Thinking.DefaultEffort,
		ThinkingClientPriority: cfg.Thinking.ClientPriority,
		ThinkingOverrideScope:  cfg.Thinking.OverrideScope,
		ThinkingModelDefaults:  cfg.Thinking.ModelDefaults,
	})
	// 用量台账落 wkbd-state.json（2026-11-17 配置重组：afree 自有状态迁出
	// vendor 子目录 workbuddy/usage.json 到 data 根，与 trae-state.json 同
	// 命名铁律——afree 层状态不落 vendor 目录）。
	rec := wbbridge.UsageNew(kit.ResolveDataPath("wkbd-state.json"))
	rec.Start()

	// 请求指标与脱敏 JSONL 归档（落 state.json 同级 request-logs/）。开关关闭时
	// 仍保留内存指标（面板摘要可用），只是不落盘；归档配置改动需重启生效。
	requestLog := wbbridge.ReqlogNew(wbbridge.ReqlogConfig{
		Dir:           filepath.Join(dir, "request-logs"),
		Enabled:       cfg.Logging.RequestArchiveEnabled,
		RetentionDays: cfg.Logging.RequestRetentionDays,
		MaxBytes:      int64(cfg.Logging.RequestArchiveMaxMB) << 20,
	})
	if cfg.Logging.RequestArchiveEnabled {
		log.Printf("[reqlog] 请求指标已启用;JSONL 归档 %s (保留 %d 天, 上限 %d MiB)",
			filepath.Join(dir, "request-logs"), cfg.Logging.RequestRetentionDays, cfg.Logging.RequestArchiveMaxMB)
	}

	redisMode := "noop"
	if _, ok := store.(wbbridge.RedisNoop); !ok {
		redisMode = "upstash"
	}
	// gwRef/pnRef 在创建后回填（SaveConfig 闭包定义早于 h/pn 声明完成，Go 闭包
	// 要求变量先声明）；SaveWorkbuddyConfig 里 gw/pn 为 nil 时跳过对应热应用段。
	var gwRef *wbbridge.Handler
	var pnRef *wbbridge.Panel
	pn := wbbridge.PanelNew(wbbridge.PanelConfig{
		Pool:        p,
		Usage:       rec,
		Upstream:    up,
		Scheduler:   sch,
		RequestLog:  requestLog,
		AuthDir:     cfg.AuthDir,
		APIKey:      "",
		RedisMode:   redisMode,
		StickyCount: stickyCount,
		Version:     WorkbuddyVersion,
		// 上游新增的 panel.poll_interval_sec（前端自动刷新间隔；0 = 缺省 5s，
		// 负值 = 关闭自动刷新）。panel.Config 是构造期静态值，与上游独立部署
		// 同语义：改动在重启后生效。
		PollIntervalSec: cfg.Panel.PollIntervalSec,
		Live:            live,
		ProbeFile:       filepath.Join(dir, "output_probes.json"),
		ConfigPath:      cfgPath,
		LoadConfig: func() (any, error) {
			c, err := EnsureWorkbuddyConfig(cfgPath, authDir, stateFile)
			if err != nil {
				return nil, err
			}
			c.APIKey = ""
			c.AuthDir = authDir
			c.StateFile = stateFile
			c.Listen = ""
			return c, nil
		},
		SaveConfig: func(raw []byte) ([]string, error) {
			return SaveWorkbuddyConfig(cfgPath, raw, live, p, up, sch, gwRef, pnRef, authDir, stateFile)
		},
	})
	pnRef = pn
	sch.SetGrowthRunner(pn.RunGrowthQueueNow)
	// 补领清扫（misakano 新增）：分钟级周期，只 accept 待接受的、只 claim 已
	// 达标未领的；执行体在面板，间隔由 config schedule.task_sweep_* 控制。
	sch.SetSweepRunner(pn.RunTaskSweepNow)
	pn.SetGrowthConcurrency(cfg.Schedule.GrowthConcurrency)

	h := wbbridge.ServerNewHandler(wbbridge.ServerConfig{
		Pool:         p,
		Upstream:     up,
		APIKey:       "",
		Session:      sess,
		StickyCount:  stickyCount,
		RedisMode:    redisMode,
		SoftCooldown: cfg.SoftRateDur,
		Panel:        nil,
		Live:         live,
		Usage:        rec,
		RequestLog:   requestLog,
		PromptMode:   cfg.Prompt.Mode,
		PromptText:   cfg.PromptText,
		// 来源记录开关经 livecfg 热生效；此处同时填静态字段，供 Live 为 nil 的
		// 裸用/测试路径拿到同一缺省值。
		RecordClientInfo: cfg.Logging.RequestClientInfo,
		RecordContext:    cfg.Logging.RequestArchiveContext,
		GlobalEnabled:    cfg.Global.Enabled,
		// 模型黑名单原始条目（config models.hidden）；运行期以 livecfg 快照为准
		//（热生效），此处供 Live 为 nil 的裸用/测试路径。
		ModelsHidden:  cfg.Models.Hidden,
		ModelsAliases: cfg.Models.Aliases,
		// 倍率筛选同样双填（2026-11-07 分域）：Live 热改优先，静态字段兜底（同款理由）。
		ModelRateFilterCN:     cfg.Pool.ModelRateFilterCN,
		ModelRateFilterGlobal: cfg.Pool.ModelRateFilterGlobal,
		// /metrics 端点开关（缺省关）：注册在装配期，改动需重启。
		MetricsEnabled: cfg.Metrics.Enabled,
		// 预算三闸（issue #229）初始值：SaveWorkbuddyConfig 后续热改。
		BudgetLimit:      cfg.Budget.DailyCreditLimit,
		BurstCreditLimit: cfg.Budget.BurstCreditLimit,
		CacheLowRate:     cfg.Budget.CacheLowRate,
	})
	gwRef = h

	ctx, cancel := context.WithCancel(context.Background())
	// ONLY_ROUTER=true（2026-10-28 用户定）：仅会话路由——签到/保活/余额巡检/
	// 补领清扫等维护任务一律不启动，面板调度开关被强制覆盖；token 续期、
	// 积分倍率预热等请求路径支撑保留。
	if kit.OnlyRouter() {
		log.Printf("[only-router] ONLY_ROUTER=true: workbuddy 签到/保活/余额巡检等维护任务已禁用（仅会话路由）")
	} else {
		go sch.Run(ctx)
		sch.StartBalanceRefresh(ctx, cfg.BalanceRefreshInterval)
		// 补领清扫周期启动（task_sweep_enabled=false 时 interval<=0，函数内部不启动）。
		sch.StartTaskSweep(ctx, cfg.TaskSweepInterval)
	}
	// 启动即预热模型积分倍率表（供积分保底的目录兜底判定）：倍率只在
	// FetchModels/FetchGlobalModelInfos 成功时填充且均为懒触发，重启后的空窗期
	// 里 ModelRate 恒为空串，触底号会被当成「收费未知」放行并打穿。异步执行，
	// 失败仅记日志。
	go WarmModelRates(ctx, up, p)
	Host.AttachLogSink(pn.Logs())

	sub := &WorkbuddySubsystem{
		CfgPath:    cfgPath,
		Dir:        dir,
		Cfg:        cfg,
		Pool:       p,
		Upstream:   up,
		Scheduler:  sch,
		Panel:      pn,
		Server:     h,
		Usage:      rec,
		RequestLog: requestLog,
		Session:    sess,
		Live:       live,
		Store:      store,
		Cancel:     cancel,
		// 与面板 SaveConfig 同源闭包：权重优化写入 account_share 复用同一条
		// 落盘+热更新链（SaveWorkbuddyConfig），两处保存口径不会漂移。
		SaveConfig: func(raw []byte) ([]string, error) {
			return SaveWorkbuddyConfig(cfgPath, raw, live, p, up, sch, gwRef, pnRef, authDir, stateFile)
		},
	}
	// 权重优化调度热值初值（住 wkbd-config.json shareOptimize 段，2026-11-17
	// 迁出 vendor config 的 schedule.share_optimize_* 补丁键）+ worker 启动
	// （随 ctx 取消，与 subsystem 同生命周期）。
	if so := WkbdConfigShareOptimize(); so != nil {
		sub.ShareOptimizeSched.Store(&WbShareSched{Enabled: so.Enabled, Cron: so.Cron})
	}
	Host.StartShareOptimizeLoop(ctx)
	log.Printf("WorkBuddy subsystem ready: accounts=%d auth_dir=%s", len(auths), cfg.AuthDir)
	return sub, nil
}

// WkbdAccountPrefProvider 账号意志回填钩子（vendor auth 解析完成时回调）：
// 从 wkbd-config.json accountPrefs 取该 UID 的反代/账号开关与昵称锁。
func WkbdAccountPrefProvider(uid string) (poolEnabled, accountEnabled *bool, nicknameLocked bool) {
	return WkbdConfigAccountPref(uid)
}

// WorkbuddyAccountPrefObserver 账号意志变更通知（vendor auth 三 setter 锁外
// 异步回调）：从池里取当前内存值落 wkbd-config.json accountPrefs——回调发生
// 时内存已改、凭据文件已落（SaveAtomic），这里只补意志持久化。
func WorkbuddyAccountPrefObserver(uid string) {
	if WorkbuddySub == nil || WorkbuddySub.Pool == nil {
		return
	}
	a := WorkbuddySub.Pool.AuthByUID(uid)
	if a == nil {
		return
	}
	poolEnabled := a.PoolEnabled()
	accountEnabled := a.AccountEnabled()
	locked := a.NicknameLocked()
	WkbdSetAccountPref(uid, &poolEnabled, &accountEnabled, locked)
}

// EnsureWorkbuddyConfig 首次运行时写一份默认配置；已存在时按源项目规则加载。
func EnsureWorkbuddyConfig(path, authDir, stateFile string) (*wbbridge.WBConfig, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		c := wbbridge.ConfigDefault()
		c.APIKey = ""
		c.Listen = ""
		c.AuthDir = authDir
		c.StateFile = stateFile
		raw, merr := json.MarshalIndent(c, "", "  ")
		if merr != nil {
			return nil, fmt.Errorf("marshal workbuddy default config: %w", merr)
		}
		if werr := os.WriteFile(path, raw, 0o600); werr != nil {
			return nil, fmt.Errorf("write workbuddy default config: %w", werr)
		}
	}
	cfg, err := wbbridge.ConfigLoad(path)
	if err != nil {
		return nil, fmt.Errorf("load workbuddy config %s: %w", path, err)
	}
	return cfg, nil
}
func (w *WorkbuddySubsystem) Stop() {
	if w == nil {
		return
	}
	w.Mu.Lock()
	if w.Stopped {
		w.Mu.Unlock()
		return
	}
	w.Stopped = true
	w.Mu.Unlock()

	if w.Cancel != nil {
		w.Cancel()
	}
	if w.Pool != nil {
		w.Pool.Flush()
		w.Pool.Close()
	}
	if w.Session != nil {
		w.Session.StopGC()
	}
	if w.Usage != nil {
		w.Usage.Stop()
	}
	if w.RequestLog != nil {
		w.RequestLog.Close()
	}
	if w.Store != nil {
		_ = w.Store.Close()
	}
}

// WorkBuddy 网关前缀（v2）：wbcn: → CN 域、wbgb: → global 域。
// 对应源项目 vendor 树内部的 cn:/global: realm 前缀 —— vendor 是同步上游的
// 代码，手改会被 re-vendor 冲掉，因此在 afree 隔离层做双向翻译：
//   - 出站（/v1/models 列表、日志分类）：vendor 的 cn:/global: → wbcn:/wbgb:；
//   - 入站（请求路由、combo target 校验）：wbcn:/wbgb: → vendor 的 cn:/global:。
//
// 旧的 cn:/global: 前缀同样接受（存量 IDE 配置与 combo target 不破坏）。
const (
	WbRealmPrefixCN     = "wbcn:"
	WbRealmPrefixGlobal = "wbgb:"
	// VendorRealmPrefixCN / VendorRealmPrefixGlobal 是源项目协议前缀，
	// 仅在翻译边界出现。
	VendorRealmPrefixCN     = "cn:"
	VendorRealmPrefixGlobal = "global:"
)

// WbRealmToVendor 把网关前缀翻译成 vendor realm 前缀（入站方向）。
// 无前缀（旧裸名按 vendor 语义 = cn 域）原样返回，由 vendor resolveModel 处理。
func WbRealmToVendor(model string) string {
	switch {
	case strings.HasPrefix(model, WbRealmPrefixCN):
		return VendorRealmPrefixCN + strings.TrimPrefix(model, WbRealmPrefixCN)
	case strings.HasPrefix(model, WbRealmPrefixGlobal):
		return VendorRealmPrefixGlobal + strings.TrimPrefix(model, WbRealmPrefixGlobal)
	}
	return model
}

// WbVendorToRealm 把 vendor realm 前缀翻译成网关前缀（出站方向）。
func WbVendorToRealm(model string) string {
	switch {
	case strings.HasPrefix(model, VendorRealmPrefixCN):
		return WbRealmPrefixCN + strings.TrimPrefix(model, VendorRealmPrefixCN)
	case strings.HasPrefix(model, VendorRealmPrefixGlobal):
		return WbRealmPrefixGlobal + strings.TrimPrefix(model, VendorRealmPrefixGlobal)
	}
	return model
}

// IsWorkBuddyModel 判定网关前缀形态：wbcn:/wbgb:（新）或 vendor 的 cn:/global:
// （旧，兼容）。裸模型名继续走 Cline/OpenCode，避免与上游目录静默冲突。
func IsWorkBuddyModel(model string) bool {
	model = strings.TrimSpace(model)
	return strings.HasPrefix(model, WbRealmPrefixCN) ||
		strings.HasPrefix(model, WbRealmPrefixGlobal) ||
		strings.HasPrefix(model, VendorRealmPrefixCN) ||
		strings.HasPrefix(model, VendorRealmPrefixGlobal)
}

// ShouldServeWorkBuddy 汇总路由前置条件：子系统已装载且模型带 WorkBuddy realm
// 前缀。把条件集中在这里，路由测试无需启动真实子系统的调度器与上游连接。
func ShouldServeWorkBuddy(model string) bool {
	return WorkbuddySub != nil && IsWorkBuddyModel(model)
}
func WorkbuddyRewriteModel(body []byte) []byte {
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Model == "" {
		return body
	}
	translated := WbRealmToVendor(probe.Model)
	if translated == probe.Model {
		return body
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return body
	}
	enc, err := json.Marshal(translated)
	if err != nil {
		return body
	}
	raw["model"] = enc
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

// workbuddyModelList 返回给 /v1/models 合并的 WorkBuddy 模型条目；无账号/拉取
// 失败时源实现返回空列表。vendor 输出的 cn:/global: 前缀在这里翻译成网关
// 前缀（wbcn:/wbgb:），客户端按列表 ID 直接调用。
func (w *WorkbuddySubsystem) ModelList() []map[string]any {
	if w == nil || w.Server == nil {
		return nil
	}
	src := w.Server.ModelList()
	out := make([]map[string]any, 0, len(src))
	for _, e := range src {
		if id, ok := e["id"].(string); ok {
			cp := make(map[string]any, len(e))
			for k, v := range e {
				cp[k] = v
			}
			cp["id"] = WbVendorToRealm(id)
			out = append(out, cp)
			continue
		}
		out = append(out, e)
	}
	return out
}

// WorkbuddyRateThresholdFor 返回模型条目适用的倍率筛选阈值（2026-11-07 分域）：
// pool.model_rate_filter_cn / _global，缺省各 0.2（与 vendor Load 的缺省一致）。
// 域由条目 id 前缀判定——wbgb:（vendor global:）走国际阈值，其余（wbcn: 或
// 裸 ID）走国内阈值（vendor 语义裸 ID = CN 域，见 panel.ProbeModel 同款判定）。
// cfg 未装配（部分测试桩）时同样回退缺省。
func (w *WorkbuddySubsystem) WorkbuddyRateThresholdFor(entryID string) float64 {
	if w == nil || w.Cfg == nil {
		return 0.2
	}
	if strings.HasPrefix(entryID, "wbgb:") || strings.HasPrefix(entryID, "global:") {
		return w.Cfg.Pool.ModelRateFilterGlobal
	}
	return w.Cfg.Pool.ModelRateFilterCN
}

// workbuddyAdminHandler 把源面板挂到 /admin/workbuddy/ 下：把外部路径重写到
// 面板内部固定的 /panel/ 前缀。挂在 /admin 下是为了让管理会话 Cookie
// （Path=/admin）随 iframe 与 api 请求一起发送；面板前端已改为相对 api 路径。
func (w *WorkbuddySubsystem) AdminHandler() http.HandlerFunc {
	if w == nil || w.Panel == nil {
		return func(rw http.ResponseWriter, r *http.Request) {
			http.NotFound(rw, r)
		}
	}
	h := w.Panel.ServeHTTP
	return func(rw http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/admin/workbuddy")
		if rest == "" {
			rest = "/"
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/panel" + rest
		r2.URL.RawPath = ""
		h(rw, r2)
	}
}

// WarmModelRates 启动预热各域模型积分倍率表（供积分保底的目录兜底判定）。
// 单域失败只记 WARN（不阻塞、不致命——后续懒触发仍会补上）；global 域仅在
// 路由开关开启时预热。ctx 取消（进程退出）时立刻放弃剩余域。
func WarmModelRates(ctx context.Context, up *wbbridge.UpClient, p *wbbridge.Pool) {
	if ctx.Err() != nil {
		return
	}
	// CN：有可用 CN 账号才拉（与面板 models 同口径，避免无谓上游调用）。
	if uids := p.AvailableUIDsForRealm("cn"); len(uids) > 0 {
		if a := p.AuthByUID(uids[0]); a != nil {
			if _, err := up.FetchModels(a); err != nil {
				log.Printf("WARN: [upstream] warm model rates (cn): %v", err)
			} else {
				log.Printf("[upstream] warm model rates: cn ok")
			}
		}
	}
	// global：独立目录端点，倍率按 "global" 域键存储。
	if up.GlobalEnabled && ctx.Err() == nil {
		if uids := p.AvailableUIDsForRealm("global"); len(uids) > 0 {
			if a := p.AuthByUID(uids[0]); a != nil {
				// FetchGlobalModelInfos 无错误返回（内部负缓存自行节流），
				// 仅按结果条数判断是否拿到目录。
				if infos := up.FetchGlobalModelInfos(a); len(infos) == 0 {
					log.Printf("WARN: [upstream] warm model rates (global): empty model list")
				} else {
					log.Printf("[upstream] warm model rates: global ok (%d models)", len(infos))
				}
			}
		}
	}
}

// ServeWorkBuddyChat 把已读取的请求体原样交给 WorkBuddy 兼容 handler。
// 请求体里的模型 ID 先从网关前缀翻译回 vendor realm 前缀（wbcn:/wbgb: →
// cn:/global:）——vendor 树只认自己的协议。
func (w *WorkbuddySubsystem) ServeWorkBuddyChat(rw http.ResponseWriter, r *http.Request, body []byte) {
	if w == nil || w.Server == nil {
		Host.WriteJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "WorkBuddy subsystem unavailable", "type": "api_error"},
		})
		return
	}
	body = WorkbuddyRewriteModel(body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	w.Server.ServeHTTP(rw, r)
}

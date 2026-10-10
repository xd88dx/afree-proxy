package app

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
	"time"

	"afree-proxy/internal/kit"
	wbauth "afree-proxy/internal/workbuddy/auth"
	wbconfig "afree-proxy/internal/workbuddy/config"
	"afree-proxy/internal/workbuddy/livecfg"
	"afree-proxy/internal/workbuddy/panel"
	wbpool "afree-proxy/internal/workbuddy/pool"
	"afree-proxy/internal/workbuddy/redisstore"
	"afree-proxy/internal/workbuddy/reqlog"
	"afree-proxy/internal/workbuddy/scheduler"
	"afree-proxy/internal/workbuddy/server"
	wbsession "afree-proxy/internal/workbuddy/session"
	"afree-proxy/internal/workbuddy/upstream"
	wbusage "afree-proxy/internal/workbuddy/usage"
)

const workbuddyVersion = "1.12.0-panel+afree"

// workbuddySubsystem 把源项目的账号池/熔断/调度/面板/兼容接口作为独立子系统
// 挂在现有 afree-proxy 上。账号凭证与状态落在 data/workbuddy/ 下，不与 Cline
// 池或 OpenCode key 混用；cn:/global: 路由由 workbuddySubsystem 接管。
type workbuddySubsystem struct {
	cfgPath string
	dir     string
	cfg     *wbconfig.Config

	pool       *wbpool.Pool
	upstream   *upstream.Client
	scheduler  *scheduler.Scheduler
	panel      *panel.Panel
	server     workbuddyServer
	usage      *wbusage.Recorder
	requestLog *reqlog.Recorder
	session    *wbsession.Router
	live       *livecfg.Holder
	store      redisstore.Store

	cancel context.CancelFunc

	mu      sync.Mutex
	stopped bool
}

// workbuddyServer keeps the subsystem decoupled from the concrete
// workbuddy/server.Handler only far enough to exercise the gateway adapters
// in unit tests. Production still constructs *server.Handler.
type workbuddyServer interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
	ModelList() []map[string]any
}

var workbuddySub *workbuddySubsystem

// startWorkBuddy 装配子系统。返回错误时调用方只记录日志并继续启动 afree-proxy，
// 保证现有 Cline/OpenCode 链路不受 WorkBuddy 配置故障影响。
func startWorkBuddy() (*workbuddySubsystem, error) {
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

	cfg, err := ensureWorkbuddyConfig(cfgPath, authDir, stateFile)
	if err != nil {
		return nil, err
	}
	// 面板认证由 afree-proxy 的 /admin/* 会话统一负责；WorkBuddy 自己的 api_key
	// 不参与鉴权，避免同一部署出现两套互不相干的密钥门。
	cfg.APIKey = ""
	cfg.AuthDir = authDir
	cfg.StateFile = stateFile
	cfg.Listen = ""

	wbauth.SetGlobalEnabled(cfg.Global.Enabled)
	upstream.SetModelCatalogPath(filepath.Join(dir, "model.json"))

	store := redisstore.New(cfg.Upstash.URL, cfg.Upstash.Token)
	p := wbpool.New(cfg.StateFile)
	p.SetStore(store)
	p.RestoreFromSnapshot()
	auths, err := wbauth.LoadDir(cfg.AuthDir)
	if err != nil {
		log.Printf("workbuddy: load auth dir %s: %v", cfg.AuthDir, err)
	}
	p.SyncToDir(auths)
	p.SetBreaker(cfg.Pool.BreakerThreshold, cfg.BreakerCooldownDur, cfg.BreakerCooldownMaxD)
	p.SetMaxInFlight(cfg.Pool.MaxInFlight)
	p.SetMaxInFlightGlobal(cfg.Pool.MaxInFlightGlobal)
	p.SetDegrade(cfg.Pool.DegradeThreshold, cfg.DegradeCooldownDur, cfg.DegradeCooldownMaxD)
	p.SetSoftRateMax(cfg.SoftRateMaxDur)
	p.SetCostExploreInterval(cfg.CostExploreIntervalDur)
	p.SetCreditFloor(cfg.Pool.CreditFloor)
	p.SetWeights(cfg.Pool.IdleWeightPerHour, cfg.Pool.IdleWeightMax)
	p.SetPreferExpiring(cfg.Pool.PreferExpiring)

	up := upstream.New()
	up.HTTP.Timeout = time.Duration(cfg.Upstream.TimeoutSeconds) * time.Second
	up.HeaderTimeout = time.Duration(cfg.Upstream.HeaderTimeoutSeconds) * time.Second
	if tr, ok := up.ChatHTTP.Transport.(*http.Transport); ok {
		tr.ResponseHeaderTimeout = up.HeaderTimeout
	}
	up.IdleTimeout = time.Duration(cfg.Upstream.IdleTimeoutSeconds) * time.Second
	up.SanitizeFingerprints.Store(cfg.Features.SanitizeBlacklistFingerprints)
	up.UserAgent = cfg.Upstream.UserAgent
	up.ClientVersion = cfg.Upstream.ClientVersion
	up.CliVersion = cfg.Upstream.CliVersion
	up.ClientName = cfg.Upstream.ClientName
	up.DeviceToken = cfg.Upstream.DeviceToken
	up.DeviceTokenFile = cfg.Upstream.DeviceTokenFile
	up.PassthroughIP = cfg.Upstream.PassthroughIP
	up.GlobalEnabled = cfg.Global.Enabled
	up.ChatBaseGlobal = cfg.Global.ChatBase
	up.BillingBaseGlobal = cfg.Global.BillingBase
	up.ProxyFor = workbuddyProxyFor
	// 出口成败观测（代理在线率统计）：WB 路径的拨号在 vendor 树内完成，成败
	// 只能靠这个钩子透出。直连出口与 ctx 取消由钩子/filter 各自忽略。
	up.ObserveProxy = recordProxyOutcome
	// 积分保底的「收费」兜底判据：接上游模型目录的积分倍率表。本地实测台账无
	// 观测时用它判收费；倍率表由探测下发，闭包每次调用读实时快照。
	p.SetModelRateOf(up.ModelRate)

	var sess *wbsession.Router
	if cfg.SessionSticky.Enabled {
		sess = wbsession.New(wbsession.Config{
			TTL:        cfg.SessionTTL,
			GCInterval: cfg.SessionGCInterval,
			Store:      store,
			Available:  p.AvailableUIDs,
			AvailableForModel: func(model string) []string {
				realm, bare := server.ResolveModel(model)
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

	sch := scheduler.New(scheduler.Config{
		Pool:               p,
		Upstream:           up,
		CheckinHours:       cfg.Schedule.CheckinHours,
		TravelHours:        cfg.Schedule.TravelHours,
		ActivityHours:      cfg.Schedule.ActivityHours,
		KeepaliveHours:     cfg.Schedule.KeepaliveHours,
		BlackcatHours:      cfg.Schedule.BlackcatHours,
		GrowthHours:        cfg.Schedule.GrowthHours,
		ExpiringSoonWindow: cfg.ExpiringSoonDur,
		CheckinDisabled:    !cfg.Schedule.CheckinEnabled,
		TravelDisabled:     !cfg.Schedule.TravelEnabled,
		ActivityDisabled:   !cfg.Schedule.ActivityEnabled,
		KeepaliveDisabled:  !cfg.Schedule.KeepaliveEnabled,
		BlackcatDisabled:   !cfg.Schedule.BlackcatEnabled,
		GrowthDisabled:     !cfg.Schedule.GrowthEnabled,
		// 保号类四任务是否覆盖禁用账号（缺省 false = 禁用即跳过，与源项目同口径）。
		IncludeDisabledInTasks: cfg.Schedule.IncludeDisabledInTasks,
	})

	live := livecfg.New(livecfg.Snapshot{
		APIKey:               "",
		SoftCooldown:         cfg.SoftRateDur,
		SanitizeFingerprints: cfg.Features.SanitizeBlacklistFingerprints,
		RecordClientInfo:     cfg.Logging.RequestClientInfo,
		ModelRateFilter:      cfg.Pool.ModelRateFilter,
	})
	rec := wbusage.New(filepath.Join(dir, "usage.json"))
	rec.Start()

	// 请求指标与脱敏 JSONL 归档（落 state.json 同级 request-logs/）。开关关闭时
	// 仍保留内存指标（面板摘要可用），只是不落盘；归档配置改动需重启生效。
	requestLog := reqlog.New(reqlog.Config{
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
	if _, ok := store.(redisstore.Noop); !ok {
		redisMode = "upstash"
	}
	pn := panel.New(panel.Config{
		Pool:        p,
		Usage:       rec,
		Upstream:    up,
		Scheduler:   sch,
		RequestLog:  requestLog,
		AuthDir:     cfg.AuthDir,
		APIKey:      "",
		RedisMode:   redisMode,
		StickyCount: stickyCount,
		Version:     workbuddyVersion,
		Live:        live,
		ProbeFile:   filepath.Join(dir, "output_probes.json"),
		ConfigPath:  cfgPath,
		LoadConfig: func() (any, error) {
			c, err := ensureWorkbuddyConfig(cfgPath, authDir, stateFile)
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
			return saveWorkbuddyConfig(cfgPath, raw, live, p, up, sch, authDir, stateFile)
		},
	})
	sch.SetGrowthHook(pn.RunGrowthQueueOnce)

	h := server.NewHandler(server.Config{
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
		GlobalEnabled:    cfg.Global.Enabled,
		// 倍率筛选同样双填：Live 热改优先，静态字段兜底（同款理由）。
		ModelRateFilter: cfg.Pool.ModelRateFilter,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go sch.Run(ctx)
	sch.StartBalanceRefresh(ctx, cfg.BalanceRefreshInterval)
	// 启动即预热模型积分倍率表（供积分保底的目录兜底判定）：倍率只在
	// FetchModels/FetchGlobalModelInfos 成功时填充且均为懒触发，重启后的空窗期
	// 里 ModelRate 恒为空串，触底号会被当成「收费未知」放行并打穿。异步执行，
	// 失败仅记日志。
	go warmModelRates(ctx, up, p)
	attachWorkbuddyLogSink(pn.Logs())

	sub := &workbuddySubsystem{
		cfgPath:    cfgPath,
		dir:        dir,
		cfg:        cfg,
		pool:       p,
		upstream:   up,
		scheduler:  sch,
		panel:      pn,
		server:     h,
		usage:      rec,
		requestLog: requestLog,
		session:    sess,
		live:       live,
		store:      store,
		cancel:     cancel,
	}
	log.Printf("WorkBuddy subsystem ready: accounts=%d auth_dir=%s", len(auths), cfg.AuthDir)
	return sub, nil
}

// ensureWorkbuddyConfig 首次运行时写一份默认配置；已存在时按源项目规则加载。
func ensureWorkbuddyConfig(path, authDir, stateFile string) (*wbconfig.Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		c := wbconfig.Default()
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
	cfg, err := wbconfig.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load workbuddy config %s: %w", path, err)
	}
	return cfg, nil
}

// attachWorkbuddyLogSink 把标准日志与 WorkBuddy chat 表格日志同时镜像进面板
// 环形缓冲；控制台/文件输出保持原样。
func attachWorkbuddyLogSink(sink io.Writer) {
	if sink == nil {
		return
	}
	writers := []io.Writer{os.Stderr}
	if proxyLogFile != nil {
		writers = append(writers, proxyLogFile)
	}
	writers = append(writers, sink)
	log.SetOutput(io.MultiWriter(writers...))
	server.SetChatLogOutput(io.MultiWriter(os.Stdout, sink))
}

// Stop 优雅停止 WorkBuddy 子系统：先落盘池状态，再停止调度/会话 GC/用量记录。
func (w *workbuddySubsystem) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	w.mu.Unlock()

	if w.cancel != nil {
		w.cancel()
	}
	if w.pool != nil {
		w.pool.Flush()
		w.pool.Close()
	}
	if w.session != nil {
		w.session.StopGC()
	}
	if w.usage != nil {
		w.usage.Stop()
	}
	if w.requestLog != nil {
		w.requestLog.Close()
	}
	if w.store != nil {
		_ = w.store.Close()
	}
}

// isWorkBuddyModel 只接受源项目定义的 realm 前缀（cn: / global:）。裸模型名
// 继续走 Cline/OpenCode，避免与现有上游目录发生静默冲突；/v1/models 会列出
// 带前缀的 WorkBuddy 模型 ID，客户端按该 ID 调用即可。
func isWorkBuddyModel(model string) bool {
	model = strings.TrimSpace(model)
	return strings.HasPrefix(model, "cn:") || strings.HasPrefix(model, "global:")
}

// shouldServeWorkBuddy 汇总路由前置条件：子系统已装载且模型带 WorkBuddy realm
// 前缀。把条件集中在这里，路由测试无需启动真实子系统的调度器与上游连接。
func shouldServeWorkBuddy(model string) bool {
	return workbuddySub != nil && isWorkBuddyModel(model)
}

// serveWorkBuddyChat 把已读取的请求体原样交给 WorkBuddy 兼容 handler。
func (w *workbuddySubsystem) serveWorkBuddyChat(rw http.ResponseWriter, r *http.Request, body []byte) {
	if w == nil || w.server == nil {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "WorkBuddy subsystem unavailable", "type": "api_error"},
		})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	w.server.ServeHTTP(rw, r)
}

// workbuddyModelList 返回给 /v1/models 合并的 WorkBuddy 模型条目；无账号/拉取
// 失败时源实现返回空列表。
func (w *workbuddySubsystem) modelList() []map[string]any {
	if w == nil || w.server == nil {
		return nil
	}
	return w.server.ModelList()
}

// workbuddyAdminHandler 把源面板挂到 /admin/workbuddy/ 下：把外部路径重写到
// 面板内部固定的 /panel/ 前缀。挂在 /admin 下是为了让管理会话 Cookie
// （Path=/admin）随 iframe 与 api 请求一起发送；面板前端已改为相对 api 路径。
func (w *workbuddySubsystem) adminHandler() http.HandlerFunc {
	if w == nil || w.panel == nil {
		return func(rw http.ResponseWriter, r *http.Request) {
			http.NotFound(rw, r)
		}
	}
	h := w.panel.ServeHTTP
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

// warmModelRates 启动预热各域模型积分倍率表（供积分保底的目录兜底判定）。
// 单域失败只记 WARN（不阻塞、不致命——后续懒触发仍会补上）；global 域仅在
// 路由开关开启时预热。ctx 取消（进程退出）时立刻放弃剩余域。
func warmModelRates(ctx context.Context, up *upstream.Client, p *wbpool.Pool) {
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

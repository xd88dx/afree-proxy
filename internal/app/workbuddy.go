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
	"afree-proxy/internal/workbuddy/scheduler"
	"afree-proxy/internal/workbuddy/server"
	wbsession "afree-proxy/internal/workbuddy/session"
	"afree-proxy/internal/workbuddy/upstream"
	wbusage "afree-proxy/internal/workbuddy/usage"
)

const workbuddyVersion = "1.11.7-panel+afree"

// workbuddySubsystem 把源项目的账号池/熔断/调度/面板/兼容接口作为独立子系统
// 挂在现有 afree-proxy 上。账号凭证与状态落在 data/workbuddy/ 下，不与 Cline
// 池或 OpenCode key 混用；cn:/global: 路由由 workbuddySubsystem 接管。
type workbuddySubsystem struct {
	cfgPath string
	dir     string
	cfg     *wbconfig.Config

	pool      *wbpool.Pool
	upstream  *upstream.Client
	scheduler *scheduler.Scheduler
	panel     *panel.Panel
	server    workbuddyServer
	usage     *wbusage.Recorder
	session   *wbsession.Router
	live      *livecfg.Holder
	store     redisstore.Store

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
	p.SetWeights(cfg.Pool.IdleWeightPerHour, cfg.Pool.IdleWeightMax)

	up := upstream.New()
	up.HTTP.Timeout = time.Duration(cfg.Upstream.TimeoutSeconds) * time.Second
	up.HeaderTimeout = time.Duration(cfg.Upstream.HeaderTimeoutSeconds) * time.Second
	if tr, ok := up.ChatHTTP.Transport.(*http.Transport); ok {
		tr.ResponseHeaderTimeout = up.HeaderTimeout
	}
	up.IdleTimeout = time.Duration(cfg.Upstream.IdleTimeoutSeconds) * time.Second
	up.SanitizeFingerprints = cfg.Features.SanitizeBlacklistFingerprints
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

	var sess *wbsession.Router
	if cfg.SessionSticky.Enabled {
		sess = wbsession.New(wbsession.Config{
			TTL:        cfg.SessionTTL,
			GCInterval: cfg.SessionGCInterval,
			Store:      store,
			Available:  p.AvailableUIDs,
			AvailableForModel: func(model string) []string {
				realm, bare := server.ResolveModel(model)
				return p.AvailableUIDsForModelRealm(bare, realm)
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
	})

	live := livecfg.New(livecfg.Snapshot{
		APIKey:               "",
		SoftCooldown:         cfg.SoftRateDur,
		SanitizeFingerprints: cfg.Features.SanitizeBlacklistFingerprints,
	})
	rec := wbusage.New(filepath.Join(dir, "usage.json"))
	rec.Start()

	redisMode := "noop"
	if _, ok := store.(redisstore.Noop); !ok {
		redisMode = "upstash"
	}
	pn := panel.New(panel.Config{
		Pool:        p,
		Usage:       rec,
		Upstream:    up,
		Scheduler:   sch,
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
		Pool:          p,
		Upstream:      up,
		APIKey:        "",
		Session:       sess,
		StickyCount:   stickyCount,
		RedisMode:     redisMode,
		SoftCooldown:  cfg.SoftRateDur,
		Panel:         nil,
		Live:          live,
		Usage:         rec,
		PromptMode:    cfg.Prompt.Mode,
		PromptText:    cfg.PromptText,
		GlobalEnabled: cfg.Global.Enabled,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go sch.Run(ctx)
	sch.StartBalanceRefresh(ctx, cfg.BalanceRefreshInterval)
	attachWorkbuddyLogSink(pn.Logs())

	sub := &workbuddySubsystem{
		cfgPath:   cfgPath,
		dir:       dir,
		cfg:       cfg,
		pool:      p,
		upstream:  up,
		scheduler: sch,
		panel:     pn,
		server:    h,
		usage:     rec,
		session:   sess,
		live:      live,
		store:     store,
		cancel:    cancel,
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

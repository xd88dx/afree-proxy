package app

import (
	"afree-proxy/internal/cline"
	"afree-proxy/internal/gateway"
	"afree-proxy/internal/panel"
	"afree-proxy/internal/reqlog"

	"afree-proxy/internal/kit"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

var proxyListenAddress = "0.0.0.0:3457"

// proxyLogFile 由 initLogFile 持有，WorkBuddy 面板镜像日志时复用同一文件句柄，
// 避免二次打开造成 Windows 文件锁冲突或重复写入。
var proxyLogFile io.Writer

func StartProxy(host string, port int) error {
	if strings.TrimSpace(host) == "" {
		host = "0.0.0.0"
	}
	ApplyEnvConfig()

	// 公网暴露 fail-closed 检查：非回环监听必须有 API_KEY 与 ADMIN_PASSWORD
	if err := CheckPublicExposure(host); err != nil {
		return err
	}

	initLogFile()

	p := loadPool()
	activeCount := 0
	for _, a := range p.Accounts {
		// 启动预热单线程（flusher/请求路径尚未启动），裸读痕迹表安全；
		// accountStatusLocked 的约定锁 poolMu 在此不适用（refreshAccountToken
		// 内部会拿锁，这里不能先持着）。
		if accountStatusLocked(a) == "active" {
			// 停用（poolEnabled=false）的账号相当于不在池里：不做启动预热，
			// 也不占 activeCount。重新启用后由请求路径的 ensureAccountToken
			// 惰性刷新，不会被“没预热过”卡住。
			if !accountPoolEnabled(a) {
				continue
			}
			// 预热 token：APIToken 账号 token 恒有效（ensureAccountToken 直接返回），
			// OAuth 账号仅在缺失/过期时刷新
			if a.APIToken == "" && (a.AccessToken == "" || time.Now().UnixMilli() >= a.ExpiresAt) {
				if err := refreshAccountToken(a); err != nil {
					log.Printf("  Pre-warm failed for %s: %v", cline.AccountLabel(a), err)
					continue
				}
			}
			activeCount++
		}
	}
	log.Printf("Loaded %d active accounts from pool", activeCount)

	freePort(port)

	startModelsRefresher()
	startZenModelsRefresher()
	startPoolFlusher()
	// 配置指纹清单：外部监测只盯 config-manifest.json 一个文件（glob 收集
	// 全部 *-config.json + wb 配置/凭据，新平台自动纳入）。
	startConfigManifestWatcher()
	// 健康统计在启动期加载（先于任何 mutateProxyConfigFile 写回）：拆出独立
	// proxy-state.json 后两文件互不干扰，这里只是保证统计在任何请求前就位。
	ensureProxyHealthLoaded()
	loadZenEndpoints()
	// 出口绑定必须在首次请求之前就位：否则第一批请求会用轮转出来的
	// 出口发出，恰好是上游记录 IP 的那一刻。
	loadEgressBindings()
	reconcileEgressBindings()
	loadClineStreamLearned()
	initOther()
	initTrae()
	reqlog.LoadRequestLogsFromFile()
	go cleanupCompactStates()
	StartProxySubscriptionWorker() // 代理订阅自动更新（cron 驱动，配置热生效）

	// WorkBuddy 子系统：独立账号池 + 面板 + OpenAI 兼容 handler。装配失败只
	// 禁用该子系统，不影响既有 Cline/OpenCode 链路。
	if wb, err := startWorkBuddy(); err != nil {
		log.Printf("  WorkBuddy subsystem disabled: %v", err)
	} else {
		*workbuddySub = wb
		defer wb.Stop()
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})

	mux.HandleFunc("/v1/health", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"version":        "go-1.1",
			"activeAccounts": liveActiveAccountCount(),
		})
	}))
	mux.HandleFunc("/health", corsHandler(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"version":        "go-1.1",
			"activeAccounts": liveActiveAccountCount(),
		})
	}))

	// Admin API (frontend + REST)
	registerAdminRoutes(mux)
	if workbuddySub != nil {
		// WorkBuddy 面板挂在 /admin/workbuddy/：复用管理会话 Cookie（Path=/admin）
		// 统一鉴权；面板自身 api_key 置空，避免出现第二套密钥门。
		mux.Handle("/admin/workbuddy/", panel.AdminAuthMiddleware((*workbuddySub).AdminHandler()))
		mux.HandleFunc("/admin/api/workbuddy/proxy", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyProxyList)))
		mux.HandleFunc("/admin/api/workbuddy/proxy/set", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyProxySet)))
		mux.HandleFunc("/admin/api/workbuddy/proxy/clear", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyProxyClear)))
		mux.HandleFunc("/admin/api/workbuddy/enabled", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyPoolSetEnabled)))
		mux.HandleFunc("/admin/api/workbuddy/account-enabled", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyAccountSetEnabled)))
		mux.HandleFunc("/admin/api/workbuddy/reorder", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyPoolReorder)))
		mux.HandleFunc("/admin/api/workbuddy/nickname", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyNickname)))
		// 凭据权重优化（2026-11-15）：按快过期积分日均需耗计算占比写入
		// pool.account_share（见 workbuddy_share.go）。当前占比不设查询端点：
		// overview 的 accounts[].share 已透出（Status.Share 配置自证透出），
		// 权重列直接用行数据。
		mux.HandleFunc("/admin/api/workbuddy/share-optimize", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyShareOptimize)))
		mux.HandleFunc("/admin/api/workbuddy/account-share", panel.AdminCORS(panel.AdminAuthMiddleware(handleWorkbuddyShareSet)))
		// 凭据权重优化定时任务（2026-11-15 立项，2026-11-17 配置迁 wkbd-config.json）：
		// 开关与 cron 住 afree 侧 wkbd 段。此前 handler 存在但路由从未注册——面板
		// GET 回显 404 被前端静默吞掉（显示为未配置）、POST 保存恒 404，配置从未
		// 成功落盘过（2026-11-19 用户实测「保存失败 HTTP 404」暴露）。
		mux.HandleFunc("/admin/api/workbuddy/wkbd/share-optimize", panel.AdminCORS(panel.AdminAuthMiddleware(handleWkbdShareOptimize)))
	}
	// TraeWork 原版入口（vendored trae2api console）：新标签页打开 /admin/trae2api/，
	// 复用管理会话 Cookie；console fetch 基址已在 vendor 阶段改写。
	mux.Handle("/admin/trae2api/", trae2apiConsoleHandler())

	// modelsHandler：/v1/models 输出（servedModelEntries + 字段归一化）。
	mux.HandleFunc("/v1/models", gateway.APIKeyAuth(gateway.HandleModels))
	mux.HandleFunc("/models", gateway.APIKeyAuth(gateway.HandleModels))

	// 自动拉黑扫描：上游回「账号类型不支持该模型」时把该平台模型加入黑名单，
	// /v1/models 随即不再对外提供（面板「收费模型」区可见/可测试/可解禁）。
	mux.HandleFunc("/v1/chat/completions", withModelBlacklistScan(gateway.APIKeyAuth(gateway.HandleChat)))
	mux.HandleFunc("/chat/completions", withModelBlacklistScan(gateway.APIKeyAuth(gateway.HandleChat)))

	// Anthropic Messages API support
	mux.HandleFunc("/v1/messages", withModelBlacklistScan(gateway.APIKeyAuth(gateway.HandleAnthropic)))
	mux.HandleFunc("/messages", withModelBlacklistScan(gateway.APIKeyAuth(gateway.HandleAnthropic)))

	// OpenAI Responses API
	responsesHandler := gateway.APIKeyAuth(gateway.HandleResponses)
	mux.HandleFunc("/v1/responses", withModelBlacklistScan(responsesHandler))
	mux.HandleFunc("/responses", withModelBlacklistScan(responsesHandler))

	// Trae 本机授权助手对接：凭据入库（原版 helper 转发路径）+ 助手脚本下载
	mux.HandleFunc("/api/web-auth", handleTraeWebAuth)
	mux.HandleFunc("/trae/oauth/helper", handleTraeHelperDownload)

	addr := fmt.Sprintf("%s:%d", host, port)
	proxyListenAddress = addr
	server := &http.Server{
		Addr: addr,
		// 公网暴露必备：慢速头攻击（slowloris）防护与空闲连接回收。
		// 不设全局 ReadTimeout/WriteTimeout —— SSE 流式响应是长连接，
		// 超时由请求 ctx（客户端断开即取消）和请求体上限控制。
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		Handler:           reqlog.RecoverMiddleware(reqlog.RequestLogMiddleware(mux, gateway.ClassifyReqRoute)),
	}

	fmt.Println("")
	fmt.Println(strings.Repeat("=", 58))
	fmt.Println("  AFree Proxy v1.0 - No CLI Required")
	fmt.Println(strings.Repeat("=", 58))
	fmt.Printf("  http://%s\n", addr)
	fmt.Printf("  http://%s/v1\n", addr)
	// API key 状态要真实反映配置：硬编码的 "any value" 曾让部署者误判
	// 环境变量没生效（fail-closed 已通过 ≠ 未配置）
	switch {
	case APIKeyEnv() != "":
		fmt.Println("  API Key: set via API_KEY env")
	case len(globalClientKeys()) > 0:
		fmt.Println("  API Key: panel-generated keys in use")
	default:
		fmt.Println("  API Key: any value (no key configured — local use only)")
	}
	fmt.Printf("  Accounts: %d total, %d active\n", len(loadPool().Accounts), activeCount)
	fmt.Println(strings.Repeat("=", 58))

	// 优雅停机：docker stop 发 SIGTERM，等待在途请求（含 SSE 流）最多
	// 10s 后退出，而不是直接掐断
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	select {
	case err := <-errCh:
		return err
	case <-stop:
		log.Printf("  shutdown signal received, draining connections...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		flushPoolNow()
		log.Printf("  shutdown complete")
		return nil
	}
}

// rotatingLogWriter 给日志文件加大小上限：超过 maxBytes 就把当前文件轮转为
// .1（覆盖上一个 .1），再重新打开主文件继续追加。log.SetOutput 只在启动时
// 设置一次，因此轮转必须发生在 Writer 内部，不能靠外部定期检查。
//
// 与 requests.jsonl 的"超限即清空"不同，日志文件用轮转保留上一份历史
// （.1），排查"崩溃前发生了什么"时不至于丢失全部上下文。
type rotatingLogWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	f        *os.File
	size     int64
}

func newRotatingLogWriter(path string, maxBytes int64) (*rotatingLogWriter, error) {
	w := &rotatingLogWriter{path: path, maxBytes: maxBytes}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingLogWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	var size int64
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	w.f, w.size = f, size
	return nil
}

func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return len(p), nil // 打开失败时静默丢弃文件副本（stderr 仍有日志）
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	if w.maxBytes > 0 && w.size >= w.maxBytes {
		w.rotateLocked()
	}
	return n, err
}

// rotateLocked 关闭当前文件、轮转为 .1、重开主文件。调用方须持锁。
func (w *rotatingLogWriter) rotateLocked() {
	_ = w.f.Close()
	rotated := w.path + ".1"
	_ = os.Remove(rotated)
	_ = os.Rename(w.path, rotated)
	if err := w.open(); err != nil {
		w.f = nil
	}
}

// logFileMaxBytes 日志文件轮转阈值：LOG_FILE_MAX_MB 同样适用于 afree-proxy.log
// （默认 10MB）。轮转保留一份 .1 历史。
func logFileMaxBytes() int64 {
	return LogFileMaxBytes()
}

// initLogFile 将日志同时输出到控制台与 afree-proxy.log（追加模式），
// 控制台窗口滚动内容有限，文件可完整保留所有日志。
// 文件侧带大小上限（见 rotatingLogWriter）：长跑实例不会撑爆磁盘。
func initLogFile() {
	path := kit.ResolveDataPath("afree-proxy.log")
	rw, err := newRotatingLogWriter(path, logFileMaxBytes())
	if err != nil {
		log.Printf("  open log file failed: %v", err)
		return
	}
	proxyLogFile = rw
	log.SetOutput(io.MultiWriter(os.Stderr, rw))
	log.Printf("========== proxy started, log file: %s ==========", path)
}

// 健康检查永远显示旧值）。
func liveActiveAccountCount() int {
	_, active := poolStatusSnapshot()
	return active
}

// 429/5xx 以 *clineAPIError 返回，交由 callClineAPI 决定是否换账号。

// usageFromChatPayload 从已读入内存的上游响应体提取 usage（chat SSE 流或普通
// JSON 双形态，与 collectStreamResponse 同判据但不做内容聚合——调用方只要
// usage，如 Test 探测的记账口）。无 usage 返回 nil。
// reqIDSeq 配合纳秒时间戳生成短请求 id，贯穿一次请求的全部日志（归因用）。
// 未知进程是危险的（可能干掉合法持有端口的服务）。
func freePort(port int) {
	if v, ok := kit.EnvBool("KILL_PORT_ON_START"); !ok || !v {
		return
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return // port is free
	}
	conn.Close()

	// Try to kill the process using the port
	cmd := kit.ExecCommand("powershell", "-Command",
		fmt.Sprintf(`$p=Get-NetTCPConnection -LocalPort %d -ErrorAction SilentlyContinue; if($p){$p.OwningProcess | Sort-Object -Unique | ForEach-Object {Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue}}`, port))
	_ = cmd.Run()
	// 杀进程后确认端口确实释放，避免旧进程尚未退出时立刻竞争监听。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
}

// cline429CooldownDuration 429 冷却时长分流：优先上游明确时长（Try again in /
// Retry-After），都解析不出时按是否带配额耗尽信号取 18h / 20min。请求路径与
// 面板 Test 共用——绝不能落到 markAccountCooldown 的 18h 兜底，那只有明确的
// 配额信号才配得上，误判会把健康账号雪藏一整天。

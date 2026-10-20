package app

import (
	"afree-proxy/internal/amd"
	"afree-proxy/internal/cline"
	"afree-proxy/internal/kit"
	openrouter "afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/tokenharbor"
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
)

// defaultModel 默认模型偏好：空 = 未设置，由 getDefaultModel 从 live 免费列表
// 里取排序最小的可用项（不再硬编码某个可能下线的模型 id）。面板设置的
// DefaultModel 会覆盖它。
var defaultModel = ""

var proxyListenAddress = "0.0.0.0:3457"

// proxyLogFile 由 initLogFile 持有，WorkBuddy 面板镜像日志时复用同一文件句柄，
// 避免二次打开造成 Windows 文件锁冲突或重复写入。
var proxyLogFile io.Writer

const (
	defaultMaxTokens       = 128000
	defaultReasoningEffort = "high"
)

var passThroughKeys = []string{
	"tools", "tool_choice", "parallel_tool_calls", "functions", "function_call",
	"temperature", "top_p", "top_k", "stop", "presence_penalty", "frequency_penalty",
	"response_format", "user", "n", "logit_bias", "seed", "logprobs", "top_logprobs",
	"stream_options", "metadata",
}

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

	// 池为空时从种子文件导入cline账号（容器无状态重建场景）
	seedAccountsFromFile()

	p := loadPool()
	activeCount := 0
	for _, a := range p.Accounts {
		if a.Status == "active" {
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
					log.Printf("  Pre-warm failed for %s: %v", a.Email, err)
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
	startZenSessionRotator()
	startPoolFlusher()
	loadZenEndpoints()
	loadClineStreamLearned()
	initOpenRouter()
	initAMD()
	initTokenHarbor()
	initOther()
	LoadRequestLogsFromFile()
	go cleanupCompactStates()
	StartProxySubscriptionWorker() // 代理订阅自动更新（cron 驱动，配置热生效）

	// WorkBuddy 子系统：独立账号池 + 面板 + OpenAI 兼容 handler。装配失败只
	// 禁用该子系统，不影响既有 Cline/OpenCode 链路。
	if wb, err := startWorkBuddy(); err != nil {
		log.Printf("  WorkBuddy subsystem disabled: %v", err)
	} else {
		workbuddySub = wb
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
		mux.Handle("/admin/workbuddy/", adminAuthMiddleware(workbuddySub.adminHandler()))
		mux.HandleFunc("/admin/api/workbuddy/proxy", adminCORS(adminAuthMiddleware(handleWorkbuddyProxyList)))
		mux.HandleFunc("/admin/api/workbuddy/proxy/set", adminCORS(adminAuthMiddleware(handleWorkbuddyProxySet)))
		mux.HandleFunc("/admin/api/workbuddy/proxy/clear", adminCORS(adminAuthMiddleware(handleWorkbuddyProxyClear)))
		mux.HandleFunc("/admin/api/workbuddy/enabled", adminCORS(adminAuthMiddleware(handleWorkbuddyPoolSetEnabled)))
		mux.HandleFunc("/admin/api/workbuddy/reorder", adminCORS(adminAuthMiddleware(handleWorkbuddyPoolReorder)))
	}

	apiKeyHandler := func(next http.HandlerFunc) http.HandlerFunc {
		return corsHandler(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("x-api-key")
			if key == "" {
				// RFC 7235: "Bearer" 前缀大小写不敏感（客户端可能发 "bearer"）。
				if b := r.Header.Get("Authorization"); len(b) > 7 && strings.EqualFold(b[:7], "Bearer ") {
					key = b[7:]
				}
			}

			// API_KEY 环境变量优先：设置后它是 /v1 唯一有效凭证（无状态部署模式）
			if envKey := APIKeyEnv(); envKey != "" {
				if subtle.ConstantTimeCompare([]byte(key), []byte(envKey)) != 1 {
					writeJSON(w, http.StatusUnauthorized, map[string]any{
						"error": map[string]string{
							"message": "invalid API key (server requires the API_KEY env value)",
							"type":    "auth_error",
						},
					})
					return
				}
				next(w, r)
				return
			}

			// 未设置 API_KEY：沿用管理面板生成的动态 key 列表；列表为空则放行（本地模式）
			keys := poolKeysSnapshot()
			if len(keys) == 0 {
				next(w, r)
				return
			}

			valid := false
			for _, k := range keys {
				if subtle.ConstantTimeCompare([]byte(key), []byte(k)) == 1 {
					valid = true
					break
				}
			}

			if !valid {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]string{
						"message": "invalid API key. Generate one at /admin/ or set x-api-key header",
						"type":    "auth_error",
					},
				})
				return
			}
			next(w, r)
		})
	}

	modelsHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		ensureModelsFresh()
		data := apiModelList()
		// 合并 zen 免费模型（OpenCode 上游常开，无独立启停开关）
		for _, zm := range zenModelList() {
			data = append(data, map[string]any{
				"id":       zm["id"],
				"object":   "model",
				"created":  time.Now().UnixMilli(),
				"owned_by": "opencode-zen",
				"source":   "zen-free",
				"status":   "active",
				"cost":     "free",
				"context":  zm["context"],
				"output":   zm["output"],
			})
		}
		// 合并 WorkBuddy 模型（cn:/global: 前缀，源项目路由协议）。
		if workbuddySub != nil {
			data = append(data, workbuddySub.modelList()...)
		}
		// 合并 OpenRouter 免费模型（oprt: 前缀）。
		data = append(data, orModelList()...)
		// 合并 AMDCloud 免费模型（amd: 前缀）。
		data = append(data, amdModelList()...)
		// 合并 TokenHarbor 免费模型（tkhb: 前缀）。
		data = append(data, thModelList()...)
		// 合并 Other 子平台勾选模型（各子平台自定义前缀）。
		data = append(data, otherModelList()...)
		// combo 别名模型（仪表盘自定义的虚拟模型 ID）
		for _, c := range listCombos() {
			owned := "cline"
			if c.Platform == "zen" {
				owned = "opencode-zen"
			} else if c.Platform == "workbuddy" {
				owned = "workbuddy"
			} else if c.Platform == "openrouter" {
				owned = "openrouter"
			} else if c.Platform == "amd" {
				owned = "amd-cloud"
			} else if c.Platform == "tokenharbor" {
				owned = "tokenharbor"
			}
			data = append(data, map[string]any{
				"id":       c.ID,
				"object":   "model",
				"created":  c.CreatedAt.UnixMilli(),
				"owned_by": owned,
				"source":   "combo",
				"status":   "active",
				"cost":     "free",
			})
		}
		// 黑名单过滤：已确认「上游不允许该账号类型调用」的模型不再对外提供，
		// 否则客户端（含 new-api 渠道映射）会反复选中它、每次都吃一轮无谓失败。
		// 面板「收费模型」区仍可见，可测试/解禁。
		kept := data[:0]
		for _, m := range data {
			if id, _ := m["id"].(string); id != "" && IsModelBlacklisted(id) {
				continue
			}
			kept = append(kept, m)
		}
		data = kept
		// 字段归一化（归纳、统一、透传）：业内 OpenAI 官方 /v1/models 只有
		// id/object/created/owned_by，同类聚合器 OpenRouter 每条带 context_length
		//（Gemini 叫 inputTokenLimit）。各来源字段名不一，统一到标准形态：
		//
		//	context_length   ← WorkBuddy 已用该名，原样透传；
		//	                   ← zen/oprt/amd/tkhb 的 context，统一补齐；
		//	max_output_tokens ← WorkBuddy 已用该名，原样透传；
		//	                   ← zen 的 output（最大输出 tokens），统一补齐。
		//
		// 零值不透出（与 WorkBuddy「不透出假 131072」同哲学，误导客户端提前
		// 截断）；cline/other 上游无数据不编造。原字段保留（向后兼容）；
		// source/cost/requiresStream/toolCall 等为 afree 自有元数据，原样透传。
		for _, m := range data {
			normalizeModelEntryFields(m)
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
	})
	mux.HandleFunc("/v1/models", modelsHandler)
	mux.HandleFunc("/models", modelsHandler)

	chatHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"error": map[string]string{"message": "method not allowed", "type": "invalid_request_error"},
			})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}

		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}

		isStream, _ := params["stream"].(bool)
		toolCount := 0
		if tools, ok := params["tools"]; ok {
			if t, ok := tools.([]any); ok {
				toolCount = len(t)
			}
		}
		model, _ := params["model"].(string)
		log.Printf("  client: stream=%v tools=%d model=%s", isStream, toolCount, model)

		// WorkBuddy 路由：只接管源项目定义的 realm 前缀（cn:/global:），
		// 裸模型名仍走现有 Cline/OpenCode 目录，避免静默改路由。
		if shouldServeWorkBuddy(model) {
			workbuddySub.serveWorkBuddyChat(w, r, body)
			return
		}

		// combo 别名模型：改写为平台上游真实模型后按平台路由;
		// combo 可声明 useProxies 让该别名走出口代理池
		useProxies := clineProxiesEnabled()
		if c := resolveCombo(model); c != nil {
			log.Printf("  combo %q -> %s model %q (useProxies=%v)", model, c.Platform, c.Target, c.UseProxies)
			params["model"] = c.Target
			model = c.Target
			if c.UseProxies {
				useProxies = true
			}
		}
		if shouldServeWorkBuddy(model) {
			rewritten, err := json.Marshal(params)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"message": err.Error(), "type": "api_error"}})
				return
			}
			workbuddySub.serveWorkBuddyChat(w, r, rewritten)
			return
		}

		// OpenRouter 路由：oprt:<model-id> 前缀（纯 key 池平台，无账号 OAuth）
		if openrouter.HasPrefix(model) {
			handleOrChat(w, r, params)
			return
		}

		// AMDCloud 路由：amd:<model-id> 前缀（Public Free Model APIs key 池）
		if amd.HasPrefix(model) {
			handleAMDChat(w, r, params)
			return
		}

		// TokenHarbor 路由：tkhb:<model-id> 前缀（聚合网关 key 池，:free 模型）
		if tokenharbor.HasPrefix(model) {
			handleTHChat(w, r, params)
			return
		}

		// Other 路由：<prefix>:<model-id>（动态子平台 key 池，小企业上游）
		if p, _, ok := other.ResolvePrefix(model); ok && p != nil {
			handleOtherChat(w, r, params)
			return
		}

		// Override system prompt from override.md for OpenAI format
		applyOverride(params)

		// zen 免费模型路由
		if route := routeModel(model); route == "zen" {
			handleZenChat(w, r, params)
			return
		} else if route == "reject" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": fmt.Sprintf("model %q is a paid zen model; only free zen models are proxied", model), "type": "invalid_request_error"},
			})
			return
		}

		// 走到这说明是 cline 路由：未知模型名显式拒绝，而不是静默替换成默认模型
		if msg := strictModelGate(model); msg != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]string{"message": msg, "type": "invalid_request_error"},
			})
			return
		}

		upstreamStream := isStream
		if !isStream {
			model := getDefaultModel()
			if m, ok := params["model"].(string); ok && m != "" {
				model = normalizeRequestModel(m)
			}
			if upstreamNeedsStream(model) {
				upstreamStream = true
				// 我们替客户端开了流式：补 include_usage，否则聚合后 usage 缺失
				withStreamUsage(params)
				log.Printf("  model %s requires stream: forcing upstream stream, will aggregate", model)
			}
		}

		// cline 路由需要账号池；zen 免费模型不需要（纯 ZEN_KEYS 部署也能用），
		// 因此该检查放在路由之后而不是函数开头
		if poolAccountCount() == 0 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": map[string]string{
					"message": "No accounts in pool. Run with --add-account or add via /admin/.",
					"type":    "auth_error",
				},
			})
			return
		}

		// 客户端要流式：走"缓冲+中断重试"路径（上游断流对客户端不可见，
		// 杜绝下游把截断流当正常结束的"伪完成"）。必须放在 callClineAutoStream
		// 之前——每次重试都要重新调它。非流式路径保持原样。
		if isStream {
			handleClineStreamBuffered(w, r, params, useProxies)
			return
		}

		resp, acc, streamed, err := callClineAutoStream(r.Context(), params, upstreamStream, useProxies)
		if err != nil {
			log.Printf("  api error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		defer resp.Body.Close()

		// 自学习重试过：上游那份是 SSE，即便客户端要的是非流式也要走聚合
		upstreamStream = upstreamStream || streamed

		usageFn := accountUsageFn(acc, params)

		if upstreamStream {
			out, err := collectStreamResponse(resp)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error": map[string]string{"message": err.Error(), "type": "parse_error"},
				})
				return
			}
			if u, ok := out["usage"].(map[string]any); ok && len(u) > 0 {
				usageFn(u)
			}
			out = normalizeOpenAIResponse(out)
			truncateAtStopSequences(out, stopSequencesFrom(params))
			contentStr, _ := getNested(out, "choices", 0, "message", "content").(string)
			finishReason := getNested(out, "choices", 0, "finish_reason")
			log.Printf("  nonstream (aggregated): model=%v content_len=%d finish=%v",
				out["model"], len(contentStr), finishReason)
			writeJSON(w, http.StatusOK, out)
			return
		}

		handleNonStreamResponseWithUsage(w, resp, usageFn, stopSequencesFrom(params))
	})
	// 自动拉黑扫描：上游回「账号类型不支持该模型」时把该平台模型加入黑名单，
	// /v1/models 随即不再对外提供（面板「收费模型」区可见/可测试/可解禁）。
	mux.HandleFunc("/v1/chat/completions", withModelBlacklistScan(chatHandler))
	mux.HandleFunc("/chat/completions", withModelBlacklistScan(chatHandler))

	// Anthropic Messages API support
	anthropicHandler := apiKeyHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"error": map[string]string{"message": "method not allowed", "type": "invalid_request_error"},
			})
			return
		}
		handleAnthropicMessages(w, r)
	})
	mux.HandleFunc("/v1/messages", withModelBlacklistScan(anthropicHandler))
	mux.HandleFunc("/messages", withModelBlacklistScan(anthropicHandler))

	// OpenAI Responses API
	responsesHandler := apiKeyHandler(handleResponses)
	mux.HandleFunc("/v1/responses", withModelBlacklistScan(responsesHandler))
	mux.HandleFunc("/responses", withModelBlacklistScan(responsesHandler))

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
		Handler:           recoverMiddleware(requestLogMiddleware(mux)),
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
	case len(loadPool().Keys) > 0:
		fmt.Println("  API Key: panel-generated keys in use")
	default:
		fmt.Println("  API Key: any value (no key configured — local use only)")
	}
	fmt.Printf("  Model:   %s (auto-detected)\n", getDefaultModel())
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
//（.1），排查"崩溃前发生了什么"时不至于丢失全部上下文。
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
//（默认 10MB）。轮转保留一份 .1 历史。
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

func corsHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version, anthropic-beta")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// applyOverride 用 override.md 替换系统提示词。默认关闭：需设置
// APPLY_SYSTEM_PROMPT_OVERRIDE=true 才启用（编码 IDE / Agent 保留自己的提示词）。
func applyOverride(params map[string]any) {
	if !SystemPromptOverrideEnabled() {
		return
	}
	override := loadOverrideContent()
	if override == "" {
		return
	}
	if msgs, ok := params["messages"].([]any); ok {
		found := false
		for _, m := range msgs {
			if mm, ok := m.(map[string]any); ok {
				if mm["role"] == "system" {
					mm["content"] = override
					found = true
					break
				}
			}
		}
		if !found {
			params["messages"] = append([]any{map[string]any{"role": "system", "content": override}}, msgs...)
		}
	}
}

// handleZenChat opencode zen 免费模型分支: 压缩 -> 上游 -> 透传,并记录统计
func handleZenChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	model, _ := params["model"].(string)
	zm, ok := resolveZenFreeModel(model)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not a free zen model", model), "type": "invalid_request_error"},
		})
		return
	}
	isStream, _ := params["stream"].(bool)

	sid := requestSessionID(params, r.Header)
	out := maybeCompact(params, zm, sid)
	if out.changed {
		log.Printf("  zen: %s", out.note)
	}

	// 端点自适应：Upstream=="" 的模型先走 chat/completions；若上游报
	// "wrong-endpoint"（500/400 系列 + 端点错误特征），学习为 responses 并
	// 本次直接改走原生 responses 路径重试。muse-spark 等已知 responses 模型
	// 仍由 Upstream 字段直达，无额外探测开销。
	// （trade-off 记录：曾考虑按目录 provider.npm 推断端点，但 29 个免费模型
	// 中 23 个无 npm 字段、无规律可循——运行时探测是唯一可靠信号。）
	if zm.Upstream == "responses" {
		_ = handleZenResponsesNative(w, r, params, zm)
		return
	}

	resp, _, err := callZenAPI(r.Context(), params, true)
	if err != nil && isWrongEndpoint(err) {
		// 只在另一个端点真的成功后才持久化"该模型走 responses"。
		//
		// 裸 500 既是"走错端点"的信号，也是上游瞬时故障的形态（实测 muse-spark
		// 在正确端点上也会偶发 {"error":"Internal server error"}）。未经验证就落盘
		// 会把 chat 原生模型永久翻到 responses —— 目录同步不会改写既有条目的
		// Upstream，只能靠人工删文件恢复。先重试、成功才学。
		log.Printf("  zen endpoint probe: model=%s chat/completions rejected (%v), retrying responses",
			zm.ID, kit.Truncate(err.Error(), 120))
		if handleZenResponsesNative(w, r, params, zm) {
			learnZenEndpoint(zm.ID, "responses")
		} else {
			log.Printf("  zen endpoint probe: model=%s responses retry also failed, NOT learning", zm.ID)
		}
		return
	}
	if err != nil {
		log.Printf("  zen api error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return
	}
	defer resp.Body.Close()

	usedKey := takeUsedKey(resp)
	usageFn := func(u map[string]any) {
		recordZenKeyTokens(usedKey, usageTokens(u, params))
	}
	if isStream {
		// 首次尝试用上面已建立的响应（含端点自适应结果）；重试重新 callZenAPI
		//（端点已由本次调用验证/学习，重试不会走错端点）。
		firstUsed := false
		handleStreamBufferedGeneric(w, r, "zen", params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
			if !firstUsed {
				firstUsed = true
				return resp, usageFn, nil
			}
			r2, _, err := callZenAPI(ctx, params, true)
			if err != nil {
				return nil, nil, err
			}
			uk := takeUsedKey(r2)
			return r2, func(u map[string]any) { recordZenKeyTokens(uk, usageTokens(u, params)) }, nil
		})
		return
	}
	// FreeTier gate 要求上游恒 stream=true（buildZenBody 恒注入）；非流式
	// 客户端在此把上游 chat SSE 聚合回 chat JSON，语义与直连非流式一致。
	finishZenChatNonStream(w, resp, zm, params, usageFn)
}

// handleZenChatDirect chat/completions 直调（端点自适应的反向纠错口：
// responses 路径上报"该用 chat"时，学回 chat 后经此重试，避免递归回主入口）。
func handleZenChatDirect(w http.ResponseWriter, r *http.Request, params map[string]any, zm *ZenModel) {
	isStream, _ := params["stream"].(bool)
	resp, _, err := callZenAPI(r.Context(), params, true)
	if err != nil {
		log.Printf("  zen api error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return
	}
	defer resp.Body.Close()
	usedKey := takeUsedKey(resp)
	usageFn := func(u map[string]any) {
		recordZenKeyTokens(usedKey, usageTokens(u, params))
	}
	if isStream {
		firstUsed := false
		handleStreamBufferedGeneric(w, r, "zen", params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
			if !firstUsed {
				firstUsed = true
				return resp, usageFn, nil
			}
			r2, _, err := callZenAPI(ctx, params, true)
			if err != nil {
				return nil, nil, err
			}
			uk := takeUsedKey(r2)
			return r2, func(u map[string]any) { recordZenKeyTokens(uk, usageTokens(u, params)) }, nil
		})
		return
	}
	finishZenChatNonStream(w, resp, zm, params, usageFn)
}

// handleZenResponsesNative 原生 responses 端点模型（Upstream=="responses"）的
// chat 入口：上游走 callZenResponsesAPI，流式时把原生 SSE 转成 chat SSE
// 即时下发，非流式时把聚合好的 chat 直接呈现。统计/日志语义与 chat 路径一致。
// handleZenResponsesNative 走 zen 原生 responses 端点。返回 true 表示本次请求
// 已被成功应答（2xx 已写出）—— 端点学习的调用方据此判断"该模型真的该走 responses"，
// 避免把上游瞬时 500 当成端点信号持久化。
func handleZenResponsesNative(w http.ResponseWriter, r *http.Request, params map[string]any, zm *ZenModel) bool {
	clientStream, _ := params["stream"].(bool)
	// spark 等原生 responses 模型只接受 stream=true（官方 CLI 恒发 true；
	// stream=false（即使显式传）上游按"非 CLI"请求 403）。网关非流式请求
	// 在上游侧强制 stream=true 再聚合（与 cline 路由 force-stream 聚合约
	// 定一致），客户端仍按非流式接收。
	resp, _, err := callZenResponsesAPI(r.Context(), params, true)
	if err != nil {
		// 反向纠错：若 responses 路径上报"该用 chat"（未来反向模型），
		// 学回 chat 并改走 chat 路径重试一次。
		if isWrongEndpointResponses(err) {
			learnZenEndpoint(zm.ID, "")
			log.Printf("  zen endpoint auto-learn: model=%s responses rejected (%v), retrying chat",
				zm.ID, kit.Truncate(err.Error(), 120))
			handleZenChatDirect(w, r, params, zm)
			return true
		}
		log.Printf("  zen responses api error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return false
	}
	defer resp.Body.Close()
	usedKey := takeUsedKey(resp)
	usageFn := func(u map[string]any) {
		recordZenKeyTokens(usedKey, usageTokens(u, params))
	}

	if clientStream {
		// 原生 responses SSE 需要先转成 chat SSE 再下发：
		// 先聚合（保持 tool_calls/reasoning/usage 完整），再以 chat
		// chunk 形态逐块发出（长文本仍保持流式体感）
		chat, aerr := responsesSSEToChat(resp)
		if aerr != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": map[string]string{"message": aerr.Error(), "type": "api_error"},
			})
			return false
		}
		chat["model"] = zm.ID
		emitChatAsSSE(w, chat, usageFn)
		return true
	}
	// 非流式客户端：上游已强制 stream=true，resp.Body 是原生 responses SSE，
	// 先聚合成 chat 再按非流式呈现。
	chat, aerr := responsesSSEToChat(resp)
	if aerr != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": aerr.Error(), "type": "api_error"},
		})
		return false
	}
	chat["model"] = zm.ID
	data, merr := json.Marshal(chat)
	if merr != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": merr.Error(), "type": "api_error"},
		})
		return false
	}
	handleNonStreamResponseWithUsage(w, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}, usageFn, nil)
	return true
}

// emitChatAsSSE 把聚合好的 chat completions 按标准 chat SSE chunk 下发：
// role 首块 + 内容分片（每 ~2KB 一块）+ tool_calls（如有）+ usage 尾块 + [DONE]。
func emitChatAsSSE(w http.ResponseWriter, chat map[string]any, usageFn func(map[string]any)) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}
	emit := func(obj map[string]any) {
		data, _ := json.Marshal(obj)
		w.Write(append(append([]byte("data: "), data...), '\n', '\n'))
		flush()
	}
	model, _ := chat["model"].(string)
	msg, _ := getNested(chat, "choices", 0, "message").(map[string]any)
	if msg == nil {
		msg = map[string]any{}
	}
	finish, _ := getNested(chat, "choices", 0, "finish_reason").(string)
	chunk := func(delta map[string]any, fr any) {
		emit(map[string]any{
			"id":      fmt.Sprintf("chatcmpl-%x", time.Now().UnixNano()),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         delta,
				"finish_reason": fr,
			}},
		})
	}
	chunk(map[string]any{"role": "assistant"}, nil)
	content, _ := msg["content"].(string)
	for len(content) > 0 {
		n := 2048
		if len(content) < n {
			n = len(content)
		} else {
			// 不能在多字节字符中间切断：SSE 消费端逐块做 UTF-8 解码，半个字符会被
			// 替换成 U+FFFD 永久污染正文（中文/emoji 尤甚）。退到完整 rune 边界。
			for n > 0 && !utf8.RuneStart(content[n]) {
				n--
			}
			if n == 0 {
				// UTF-8 单字符最多 4 字节，正常不会走到；兜底避免死循环
				n = 2048
			}
		}
		chunk(map[string]any{"content": content[:n]}, nil)
		content = content[n:]
	}
	if tcs, ok := msg["tool_calls"].([]any); ok && len(tcs) > 0 {
		for i, tc := range tcs {
			tcm, _ := tc.(map[string]any)
			if tcm == nil {
				continue
			}
			fn, _ := tcm["function"].(map[string]any)
			if fn == nil {
				fn = map[string]any{}
			}
			id, _ := tcm["id"].(string)
			name, _ := fn["name"].(string)
			args, _ := fn["arguments"].(string)
			chunk(map[string]any{"tool_calls": []any{map[string]any{
				"index": i,
				"id":    id,
				"type":  "function",
				"function": map[string]any{
					"name":      name,
					"arguments": args,
				},
			}}}, nil)
		}
	}
	if u, ok := chat["usage"].(map[string]any); ok && len(u) > 0 {
		if usageFn != nil {
			usageFn(u)
		}
		chunk(map[string]any{}, nil)
		last := map[string]any{
			"id":      fmt.Sprintf("chatcmpl-%x", time.Now().UnixNano()),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": finish,
			}},
			"usage": u,
		}
		emit(last)
	} else {
		chunk(map[string]any{}, finish)
	}
	w.Write([]byte("data: [DONE]\n\n"))
	flush()
}

// asInt 把数值型 any 转为 int（客户端 JSON 解码得到 float64，
// anthropic/responses 内部转换路径写入 int/int64）。
func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

// liveActiveAccountCount /health 的实时活跃账号数（启动时算一次会让
// 健康检查永远显示旧值）。
func liveActiveAccountCount() int {
	_, active := poolStatusSnapshot()
	return active
}

func buildUpstreamBody(params map[string]any, stream bool) map[string]any {
	sessionID := "sess_" + kit.RandHex(8)

	maxTokens := defaultMaxTokens
	// max_tokens 可能是 float64（客户端 JSON 解码）或 int（anthropic/responses
	// 内部转换路径写入）—— 两种都要接受，否则转换路径永远回退到 128000
	if mt := asInt(params["max_tokens"]); mt > 0 {
		maxTokens = mt
	} else if mt := asInt(params["max_completion_tokens"]); mt > 0 {
		maxTokens = mt
	}

	model := getDefaultModel()
	if m, ok := params["model"].(string); ok && m != "" {
		model = normalizeRequestModel(m)
	}

	body := map[string]any{
		"model":            model,
		"max_tokens":       maxTokens,
		"session_id":       sessionID,
		"reasoning_effort": defaultReasoningEffort,
	}

	if msgsRaw, ok := params["messages"]; ok {
		body["messages"] = msgsRaw
	}

	if stream {
		body["stream"] = true
	}

	if re, ok := params["reasoning_effort"].(string); ok && re != "" {
		body["reasoning_effort"] = re
	} else if re, ok := params["reasoningEffort"].(string); ok && re != "" {
		body["reasoning_effort"] = re
	}

	for _, key := range passThroughKeys {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}

	enforceToolChoiceNone(body)

	return body
}

// enforceToolChoiceNone 保证 tool_choice:"none" 的语义。部分上游模型
// （尤其 zen 免费档）会忽略该指令，仍然输出 tool_call —— 而客户端
// （IDE）发出 none 时明确表示这一轮不要工具调用。最可靠的做法是
// 网关侧直接把 tools 从上游请求中移除：模型看不到工具，只能文本回答。
func enforceToolChoiceNone(body map[string]any) {
	if tc, ok := body["tool_choice"].(string); ok && tc == "none" {
		delete(body, "tools")
		delete(body, "tool_choice")
	}
}

// Cline 上游客户端指纹版本。官方 CLI 3.0.73（X-CLIENT-TYPE: cline-cli，
// core 0.0.95）。2026-10-19 真机抓包核对（cline@3.0.73 本地 mock 捕获
// POST /api/v1/chat/completions），不再依赖 sdk 源码推断。
// Cline 网关对被限制在 Cline 产品面的免费模型会校验这些身份头，缺失可能 403。
const (
	clineClientVersion = "3.0.73"
	clineCoreVersion   = "0.0.95"
	// clineUASuffix 官方 CLI 的 UA 后缀：ai-sdk provider 层在应用版本号之后
	// 追加 provider / provider-utils / runtime 三段。真机实测（同上抓包）：
	//   Cline/3.0.73 ai-sdk/openai-compatible/3.0.58 ai-sdk/provider-utils/5.0.50 runtime/bun/1.4.2
	// 只发 "Cline/<ver>" 会少掉整段后缀——这是与真实客户端最大的结构性偏差。
	clineUASuffix = " ai-sdk/openai-compatible/3.0.58 ai-sdk/provider-utils/5.0.50 runtime/bun/1.4.2"
)

// clineUserAgent 官方 CLI 的完整 User-Agent。
func clineUserAgent() string { return "Cline/" + clineClientVersion + clineUASuffix }

// clineTaskID 把任意会话标识规范成官方 CLI 的 X-Task-ID 形态
// <13 位毫秒>_<5 位 base36（真机抓包样本：1792426668112_96vx7）。
// 真实客户端在任务创建时生成该值、任务内恒定；网关按会话 ID 派生，
// 保证同一会话（含重试）稳定、不同会话不同。已是官方形态则原样透传。
func clineTaskID(sessionID string) string {
	if isClineTaskID(sessionID) {
		return sessionID
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(sessionID))
	sum := h.Sum64()
	// 毫秒段落在 2025-10-01 起的 90 天窗口内：恒定可复现，且永远是过去时间
	// （真实客户端的时间戳只会是任务创建时刻，未来时间戳反而可疑）。
	const base = int64(1759276800000)
	const window = uint64(90 * 24 * 3600 * 1000)
	return fmt.Sprintf("%d_%s", base+int64(sum%window), base36Fixed(sum>>17, 5))
}

// isClineTaskID 判断是否已是官方形态：13 位数字 + '_' + 5 位小写字母数字。
func isClineTaskID(s string) bool {
	if len(s) != 19 || s[13] != '_' {
		return false
	}
	for i := 0; i < 13; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	for i := 14; i < 19; i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// base36Fixed 把 n 编码成固定 width 位的 base36（小写，高位补 0）。
func base36Fixed(n uint64, width int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = digits[n%36]
		n /= 36
	}
	return string(out)
}

// clineHeaders 构造上游请求头。用 setRawHeader（而非 Header.Set）保留官方
// 客户端的精确大小写（Set 会把键规范化成 X-Task-Id / Http-Referer 等），
// 字节级对齐客户端指纹；同时删除同名的其它大小写变体，避免 map 里同时存在
// "X-CLIENT-VERSION" 与 "X-Client-Version" 两个键、上线发出重复头。
// cfg.Headers 仍可覆盖（管理员自定义优先）。
func clineHeaders(token, sessionID string) http.Header {
	h := http.Header{}
	setRawHeader(h, "Authorization", "Bearer "+token)
	setRawHeader(h, "Content-Type", "application/json")
	// Go 默认 UA（Go-http-client/1.1）是指纹异常点，容易被上游风控拦截；
	// 与管理面板探测请求保持一致的客户端标识。cfg.Headers 里可覆盖。
	setRawHeader(h, "User-Agent", clineUserAgent())
	setRawHeader(h, "HTTP-Referer", "https://cline.bot")
	setRawHeader(h, "X-Title", "Cline")
	setRawHeader(h, "X-IS-MULTIROOT", "false")
	setRawHeader(h, "X-CLIENT-TYPE", "cline-cli")
	setRawHeader(h, "X-CLIENT-VERSION", clineClientVersion)
	setRawHeader(h, "X-PLATFORM", "cli")
	setRawHeader(h, "X-PLATFORM-VERSION", clineClientVersion)
	setRawHeader(h, "X-CORE-VERSION", clineCoreVersion)
	if sessionID != "" {
		setRawHeader(h, "X-Task-ID", clineTaskID(sessionID))
	}

	cfg := getProxyConfig()
	for k, v := range cfg.Headers {
		setRawHeader(h, k, v)
	}

	return h
}

// setRawHeader 以给定 key 原样写入（保留大小写），并删除同名的其它大小写
// 变体——HTTP 头大小写不敏感，但 Go 的 http.Header 是 map[大小写敏感 key]，
// 同时存在两个变体时网络层会把两个键都发出去（重复头）。
func setRawHeader(h http.Header, key, value string) {
	canon := textproto.CanonicalMIMEHeaderKey(key)
	for existing := range h {
		if existing != key && textproto.CanonicalMIMEHeaderKey(existing) == canon {
			delete(h, existing)
		}
	}
	h[key] = []string{value}
}

// clineAPIBase cline 上游基址。包级变量便于测试把真实调用链指向本地假上游
// （cline.ClineAPIBase 是常量、不可注入）；生产恒为 cline.ClineAPIBase。
var clineAPIBase = cline.ClineAPIBase

// callClineAPI 调用 cline 上游，带跨账号 failover。
// ctx 来自客户端请求: IDE abort/取消时立即终止,不冷却账号。
// 账号有绑定出口（隔离模式）时永远只从绑定出口发出,绑定优先于 useProxies;
// 未绑定账号在 useProxies 为 true 时走共享出口代理池（每次尝试轮转挑选）;
// 代理路径上的网络错误只冷却代理本身,绝不冷却账号 —— 代理故障不污染账号池。
//
// failover（P2 修复）：429/5xx 时换下一个账号重试（至多 clineFailoverAttempts
// 个账号）。此前只 pick 一次账号，一个账号撞上 429 即整请求 500，池子里其它
// 健康账号救不了本次请求——N 个账号的可用性退化成 1。429 会先把该账号冷却，
// 因此换到的必然是别的账号；5xx 不冷却（可能是上游瞬时故障），但也换账号试。
// 只在尚未向客户端流出任何内容时发生（本函数在流式转发之前），因此重试安全。
func callClineAPI(ctx context.Context, params map[string]any, stream bool, useProxies bool) (*http.Response, *Account, error) {
	var lastErr error
	var lastAcc *Account
	tried := map[string]bool{}
	for i := 0; i < clineFailoverAttempts; i++ {
		acc := pickAccountExcluding(tried)
		if acc == nil {
			if lastErr != nil {
				// 所有可用账号都试过了：返回最后一次的真实错误（比"无账号"更有信息量）
				return nil, lastAcc, lastErr
			}
			return nil, nil, fmt.Errorf("no active accounts available: %s", describePoolStatus())
		}
		tried[acc.AccountID] = true
		lastAcc = acc

		resp, err := clineAPIWithAccount(ctx, params, stream, useProxies, acc)
		if err == nil {
			return resp, acc, nil
		}
		lastErr = err
		var ce *clineAPIError
		if !errors.As(err, &ce) {
			// 网络/令牌/取消等错误：不换账号（换账号无助于网络层故障，且
			// 取消必须立即终止）。直接返回。
			return nil, acc, err
		}
		// 只对 429 / 5xx 换账号；4xx（400/401/403 等）重试无意义，原样返回。
		if ce.Status != http.StatusTooManyRequests && ce.Status < 500 {
			return nil, acc, err
		}
		// "500 empty response content" 是模型属性（该模型必须流式），不是账号
		// 故障：换账号徒劳，且会拖慢 callClineAutoStream 的"学习+改流式重试"。
		// 原样返回，让上层去学。
		if clineEmptyStreamErr(ce.Status, []byte(ce.Body)) {
			return nil, acc, err
		}
		log.Printf("  cline failover: account=%s got %d, trying another account (%d/%d)",
			truncateEmail(acc.Email), ce.Status, i+1, clineFailoverAttempts)
	}
	return nil, lastAcc, lastErr
}

// clineFailoverAttempts 单次请求最多尝试的账号数（429/5xx 换账号）。
const clineFailoverAttempts = 3

// clineAPIWithAccount 用指定账号执行一次上游调用（含代理轮转与 401 刷新）。
// 429/5xx 以 *clineAPIError 返回，交由 callClineAPI 决定是否换账号。
func clineAPIWithAccount(ctx context.Context, params map[string]any, stream bool, useProxies bool, acc *Account) (*http.Response, error) {
	token, err := ensureAccountToken(acc)
	if err != nil {
		// Try other accounts
		return nil, fmt.Errorf("account %s token failed: %w", acc.Email, err)
	}

	body := buildUpstreamBody(params, stream)
	sessionID, _ := body["session_id"].(string)

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}

	toolCount := 0
	if tools, ok := params["tools"]; ok {
		if t, ok := tools.([]any); ok {
			toolCount = len(t)
		}
	}
	// 出口选择（见 proxy_binding.go）：
	//   - 隔离模式且账号有绑定：只在 {主, 辅} 内尝试（至多 2 次），绑定出口
	//     全不可用时报错且不冷却账号 —— 绝不退回其他出口或直连（pickAccount
	//     已过滤掉双不可用的账号，这里 !ok 只剩选号与拨号之间的窄竞态窗口）。
	//   - 未绑定 + useProxies：旧规则，每次尝试全局轮转挑代理（至多 3 次）。
	//   - 未绑定 + 直连：1 次，网络错误 5 分钟短冷却（原语义）。
	// 代理路径上的网络错误只冷却代理本身,绝不冷却账号 —— 代理故障不污染账号池。
	main, backup, bound := accountProxyBinding(acc)
	log.Printf("  upstream: account=%s stream=%v tools=%d msgs=%d max_tokens=%v effort=%v useProxies=%v boundExit=%v",
		truncateEmail(acc.Email), stream, toolCount, getMsgCount(params), body["max_tokens"], body["reasoning_effort"], useProxies, bound)
	setUpstreamInfo(ctx, fmt.Sprintf("账号#%d", accountNumber(acc)))
	client := kit.HTTPClient
	attempts := 1
	switch {
	case bound:
		attempts = 2
	case useProxies:
		attempts = 3
	}
	var resp *http.Response
	var lastErr error
	var exitURL string // 401 重试仍用最后一个成功出口的 client，成败按它归属
	for i := 0; i < attempts; i++ {
		proxyURL, pidx := "", -1
		if bound {
			var ok bool
			proxyURL, pidx, ok = pickBoundProxy(main, backup)
			if !ok {
				return nil, fmt.Errorf("account %s bound proxies unavailable (cooling or removed); account skipped by proxy isolation", truncateEmail(acc.Email))
			}
			client = proxyClientFor(proxyURL)
			if proxyURL == "" {
				log.Printf("  cline upstream via bound exit direct (no proxy)")
			} else {
				log.Printf("  cline upstream via bound exit %s", maskProxyURL(proxyURL))
			}
		} else if useProxies {
			proxyURL, pidx = pickUpstreamProxy()
			client = proxyClientFor(proxyURL)
			log.Printf("  cline upstream via %s", maskProxyURL(proxyURL))
		}
		// 每次尝试重建请求: bytes.Reader 只能读一次,
		// 复用已发送的 req 会以 "ContentLength=N with Body length 0" 失败
		req, rerr := http.NewRequestWithContext(ctx, "POST", clineAPIBase+"/chat/completions", bytes.NewReader(bodyJSON))
		if rerr != nil {
			return nil, fmt.Errorf("create request: %w", rerr)
		}
	// hsStart 拨号前打点：Do 返回即隧道建立（首字节未回），差值=握手延迟。
		req.Header = clineHeaders(token, sessionID)
		hsStart := time.Now()
		wctx, stopWatch := clineHeaderWatchdog(ctx)
		resp, lastErr = client.Do(req.WithContext(wctx))
		if stopWatch() {
			// 黑洞出口（TCP 通、响应头静默丢失）：按网络错误处理，冷却该出口并换下一个
			log.Printf("  cline upstream: response header watchdog fired after %v (exit unresponsive, failing over)", ClineHeaderTimeout())
		}
		if lastErr == nil {
			setUpstreamExit(ctx, proxyExitType(proxyURL, main, backup, bound))
			recordProxyOutcome(proxyURL, true, nil, time.Since(hsStart).Milliseconds())
			exitURL = proxyURL
			break
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("client aborted: %w", lastErr)
		}
		if bound || useProxies {
			// 隧道层失败才冷却,且只冷 2 分钟: 上游过载也会表现为连接重置,
			// 长冷却会让几次慢请求毒化整个池;2 分钟能跳过真死代理又快速自愈。
			// 绑定账号（隔离模式）同样适用：全局开关为直连时，绑定代理的网络
			// 错误也绝不毒化账号（pidx=-1 的直连兜底冷却为 no-op）。
			cooldownUpstreamProxy(pidx, 2*time.Minute)
			recordProxyOutcome(proxyURL, false, lastErr, 0)
			log.Printf("  cline proxy failed (%v), cooldown exit 2m, retrying on next", lastErr)
			continue
		}
		// 直连网络错误：临时短冷却 5 分钟
		markAccountCooldown(acc, "network error: "+lastErr.Error(), 5*time.Minute)
		return nil, fmt.Errorf("upstream request: %w", lastErr)
	}
	if lastErr != nil {
		// 所有代理出口都失败（useProxies 时）——不冷却账号
		return nil, fmt.Errorf("upstream request (all proxy exits failed): %w", lastErr)
	}

	if resp.StatusCode == 401 {
		resp.Body.Close()
		// Refresh token and retry（重建请求: 上一次 Do 已消费请求体）
		if rerr := refreshAccountToken(acc); rerr == nil {
			token = acc.AccessToken
			req2, cerr := http.NewRequestWithContext(ctx, "POST", clineAPIBase+"/chat/completions", bytes.NewReader(bodyJSON))
			if cerr != nil {
				return nil, fmt.Errorf("create request: %w", cerr)
			}
			req2.Header = clineHeaders(token, sessionID)
			// 注意: 不能用 := —— 那会在 if 块内遮蔽外层 resp，重试成功的
			// 响应被丢弃（body 泄漏）而调用方仍拿到旧的 401
			hsStart2 := time.Now()
			wctx2, stopWatch2 := clineHeaderWatchdog(ctx)
			resp2, derr := client.Do(req2.WithContext(wctx2))
			stopWatch2()
			if derr != nil {
				recordProxyOutcome(exitURL, false, derr, 0)
				return nil, fmt.Errorf("upstream retry: %w", derr)
			}
			recordProxyOutcome(exitURL, true, nil, time.Since(hsStart2).Milliseconds())
			if resp2.StatusCode == 401 {
				resp2.Body.Close()
				poolMu.Lock()
				acc.Status = "expired"
				savePoolLocked()
				poolMu.Unlock()
				return nil, fmt.Errorf("account %s token expired permanently", truncateEmail(acc.Email))
			}
			resp = resp2
		} else {
			poolMu.Lock()
			acc.Status = "expired"
			savePoolLocked()
			poolMu.Unlock()
			return nil, fmt.Errorf("account %s refresh failed: %w", truncateEmail(acc.Email), rerr)
		}
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// Mark account on cooldown on rate limits
		if resp.StatusCode == 429 {
			reason := kit.Truncate(string(bodyBytes), 500)
			duration := parseInferenceCapDuration(string(bodyBytes))
			if duration <= 0 {
				duration = parseRetryAfter(resp.Header.Get("Retry-After"))
			}
			if duration <= 0 {
				// 429 但既无 "Try again in ..." 也无 Retry-After: 无法区分
				// 配额耗尽与瞬时限流。绝不落到 markAccountCooldown 的 18h 兜底——
				// 那只有明确的配额信号才配得上，误判会把健康账号雪藏一整天。
				if isClineQuotaExhausted(string(bodyBytes)) {
					duration = 18 * time.Hour
				} else {
					duration = 20 * time.Minute
				}
				log.Printf("  429 without parseable wait on %s: defaulting to %v (quota=%v)",
					truncateEmail(acc.Email), duration, isClineQuotaExhausted(string(bodyBytes)))
			}
			markAccountCooldown(acc, "429: "+reason, duration)
			log.Printf("  account %s cooldown %v (reason: %s)", truncateEmail(acc.Email), duration, reason)
		}
		// 非 200 一律返回 error（body 已读尽并关闭），但状态码 + 错误体本身有价值：
		// 类型化错误把它带出来，供 callClineAutoStream 识别"该模型必须流式"的指纹。
		return nil, &clineAPIError{Status: resp.StatusCode, Body: kit.Truncate(string(bodyBytes), 500)}
	}

	bumpUsage(acc)
	return resp, nil
}

// accountUsageFn 构造账号 token 记账回调：从上游 usage 提取
// prompt_tokens + completion_tokens，计入该账号今日/累计消耗。
func accountUsageFn(acc *Account, params map[string]any) func(map[string]any) {
	return func(u map[string]any) {
		recordAccountTokens(acc, usageTokens(u, params))
	}
}

// usageTokens 从上游 usage 包提取 prompt+completion 消耗；
// 上游未返回 usage 时用入站请求估算兜底（与 zen 统计一致）。
func usageTokens(u map[string]any, params map[string]any) int64 {
	var pt, ct float64
	if v, ok := u["prompt_tokens"].(float64); ok {
		pt = v
	}
	if v, ok := u["completion_tokens"].(float64); ok {
		ct = v
	}
	tokens := int64(pt + ct)
	if tokens <= 0 && params != nil {
		tokens = int64(estimateJSON(params))
	}
	return tokens
}

// takeUsedKey 从上游 resp 取出实际服务的 key 并删头（防透传客户端）。
func takeUsedKey(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	k := resp.Header.Get("X-Afree-Used-Key")
	resp.Header.Del("X-Afree-Used-Key")
	return k
}

func truncateEmail(email string) string {
	if len(email) <= 12 {
		return email
	}
	parts := splitEmail(email)
	if len(parts) == 2 && len(parts[0]) > 3 {
		return parts[0][:3] + "***@" + parts[1]
	}
	if len(email) > 12 {
		return email[:8] + "..."
	}
	return email
}

func splitEmail(email string) []string {
	for i := 0; i < len(email); i++ {
		if email[i] == '@' {
			return []string{email[:i], email[i+1:]}
		}
	}
	return []string{email}
}

func getMsgCount(params map[string]any) int {
	if msgs, ok := params["messages"].([]any); ok {
		return len(msgs)
	}
	return 0
}

// ---- cline 流式缓冲转发 + 中断重试 ----
// 背景：上游 cline 流中断率极高（实测 24h 内 61%），旧直通路径把截断流发
// 给下游后，error 事件被 newapi→openai SDK 链路吞掉，客户端（hermes 等
// agent）把半截文本当正常完成 → "伪完成"、任务静默停止。
// 方案：上游 SSE 先完整缓冲、拿到 finish/[DONE] 才整体回放；中途断流则
// 换账号重试整个请求（沿用现有 failover 通道，每账号仍走自己的绑定出口，
// 隔离语义不变）。代价：免费账号 prompt 重复计费 + 每次重试数秒延迟。

// reqIDSeq 配合纳秒时间戳生成短请求 id，贯穿一次请求的全部日志（归因用）。
var reqIDSeq atomic.Uint64

func newReqID() string {
	return fmt.Sprintf("%x-%d", time.Now().UnixNano()&0xffffff, reqIDSeq.Add(1))
}

// sseBufferLimit 单次尝试的上游 SSE 缓冲上限。cline 响应通常 <100KB，
// 16MB 是极端防御值：超限说明响应异常巨大，重试无意义。
const sseBufferLimit = 16 << 20

// bufferUpstreamSSE 完整读一份上游 SSE，不向客户端写任何字节。
// 返回：原始行（保持逐字透传的回放保真度）、usage（成功时记账用）、
// complete（看到 finish_reason 非空或 [DONE]）、中断错误。
func bufferUpstreamSSE(body io.Reader) (lines []string, usage map[string]any, complete bool, brk error) {
	total := 0
	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// 干净 EOF：若已看到 finish/[DONE] 即完整；否则视为中断
				if strings.TrimSpace(line) != "" {
					lines = append(lines, line)
					total += len(line)
				}
				if !complete {
					brk = io.ErrUnexpectedEOF
				}
				return lines, usage, complete, brk
			}
			// 真实读错误（连接重置/取消等）
			return lines, usage, false, err
		}
		total += len(line)
		if total > sseBufferLimit {
			return lines, usage, false, fmt.Errorf("sse buffer limit %d exceeded", sseBufferLimit)
		}
		lines = append(lines, line)

		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		payload := strings.TrimSpace(trimmed[5:])
		switch {
		case payload == "[DONE]":
			complete = true
		case payload != "":
			var obj map[string]any
			if json.Unmarshal([]byte(payload), &obj) != nil {
				continue
			}
			// 部分 cline 响应包 {data:{...}}，与直通路径同款解包
			if d, ok := obj["data"].(map[string]any); ok {
				if _, has := d["choices"]; has {
					obj = d
				}
			}
			if u, ok := obj["usage"].(map[string]any); ok && len(u) > 0 {
				usage = u
			}
			if choices, ok := obj["choices"].([]any); ok {
				for _, c := range choices {
					cm, ok := c.(map[string]any)
					if !ok {
						continue
					}
					if fr := cm["finish_reason"]; fr != nil {
						complete = true
					}
				}
			}
		}
	}
}

// streamAttemptFn 一次上游尝试：返回响应与该次尝试的记账回调（绑定本次
// 实际服务的账号/key，由各平台 attempt 闭包构造）。
type streamAttemptFn func(ctx context.Context) (*http.Response, func(map[string]any), error)

// handleStreamBufferedGeneric 全平台通用的流式"缓冲+中断重试"路径：
// 上游 SSE 先完整缓冲、拿到 finish/[DONE] 才整体回放；中途断流则重新执行
// attempt（平台内部自选新账号/key，出口绑定语义不变）。ctx 取消（客户端
// 真断开）不重试。
func handleStreamBufferedGeneric(w http.ResponseWriter, r *http.Request, plat string, params map[string]any, attempt streamAttemptFn) {
	rid := newReqID()
	toolCount := 0
	if tools, ok := params["tools"].([]any); ok {
		toolCount = len(tools)
	}
	model, _ := params["model"].(string)
	log.Printf("  [%s] client(%s): stream=true (buffered) tools=%d model=%s", rid, plat, toolCount, model)

	var lastBrk error
	for n := 1; n <= StreamMaxAttempts(); n++ {
		if r.Context().Err() != nil {
			log.Printf("  [%s] client gone before attempt %d, abort", rid, n)
			return
		}
		resp, usageFn, err := attempt(r.Context())
		if err != nil {
			log.Printf("  [%s] %s attempt %d/%d api error: %v", rid, plat, n, StreamMaxAttempts(), err)
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "api_error"},
			})
			return
		}
		lines, usage, complete, brk := bufferUpstreamSSE(resp.Body)
		resp.Body.Close()
		if complete {
			// 只把成功这次的上游消耗记账（中断的尝试不重复记账）
			if usage != nil && usageFn != nil {
				usageFn(usage)
			}
			if n > 1 {
				log.Printf("  [%s] %s attempt %d/%d recovered after %d break(s)", rid, plat, n, StreamMaxAttempts(), n-1)
				setUpstreamNote(r.Context(), fmt.Sprintf("%d次中断已重试", n-1))
			}
			replaySSE(w, lines)
			return
		}
		// 中断归因：ctx 已取消 = 下游先断开连带取消上游读，非上游故障
		if r.Context().Err() != nil {
			log.Printf("  [%s] %s attempt %d/%d stream broken, cause=client_gone (ctx canceled), %d lines buffered, no retry",
				rid, plat, n, StreamMaxAttempts(), len(lines))
			setUpstreamNote(r.Context(), "客户端断开")
			return
		}
		log.Printf("  [%s] %s attempt %d/%d stream broken, cause=upstream_break (%v), %d lines buffered, retrying",
			rid, plat, n, StreamMaxAttempts(), brk, len(lines))
		lastBrk = brk
	}
	// 全部尝试中断：尚未向客户端写过任何字节（缓冲模式），直接 502——
	// 语义正确（newapi 记为真实错误、openai SDK 抛异常走客户端重试）、
	// 面板 Error 列可见，不再伪造 200 + SSE error 事件。
	log.Printf("  [%s] %s all %d attempts broken, returning 502", rid, plat, StreamMaxAttempts())
	setUpstreamNote(r.Context(), fmt.Sprintf("%d次尝试均中断", StreamMaxAttempts()))
	msg := "upstream stream interrupted"
	if lastBrk != nil {
		msg = kit.Truncate("upstream stream interrupted: "+lastBrk.Error(), 300)
	}
	writeJSON(w, http.StatusBadGateway, map[string]any{
		"error": map[string]any{"message": msg, "type": "upstream_error"},
	})
}

// handleClineStreamBuffered cline 路由入口（账号 failover 在 callClineAPI 内，
// 每次 attempt 由 callClineAPI 自选账号并走其绑定出口）。
func handleClineStreamBuffered(w http.ResponseWriter, r *http.Request, params map[string]any, useProxies bool) {
	handleStreamBufferedGeneric(w, r, "cline", params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
		resp, acc, _, err := callClineAutoStream(ctx, params, true, useProxies)
		if err != nil {
			return nil, nil, err
		}
		// 注意：callClineAutoStream 对流式请求恒返回 streamed=false（直通语义），
		// 不能用它判断响应体形态——stream=true 时上游永远回 SSE，直接缓冲。
		return resp, accountUsageFn(acc, params), nil
	})
}

// replaySSE 逐字回放缓冲的上游 SSE（保真度与旧直通路径一致）。
func replaySSE(w http.ResponseWriter, lines []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	var sb strings.Builder
	for _, ln := range lines {
		sb.WriteString(ln)
		if !strings.HasSuffix(ln, "\n") {
			sb.WriteString("\n")
		}
	}
	w.Write([]byte(sb.String()))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func handleNonStreamResponseWithUsage(w http.ResponseWriter, upstream *http.Response, onUsage func(map[string]any), stops []string) {
	var raw map[string]any
	if err := json.NewDecoder(upstream.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	if onUsage != nil {
		if u, ok := raw["usage"].(map[string]any); ok && len(u) > 0 {
			onUsage(u)
		}
	}

	// Some Cline responses wrap in {data: {...}}
	out := raw
	if data, ok := raw["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			out = d
		}
	}

	out = normalizeOpenAIResponse(out)
	truncateAtStopSequences(out, stops)

	if msg, ok := getNested(out, "choices", 0, "message").(map[string]any); ok {
		tc, _ := msg["tool_calls"].([]any)
		content, _ := msg["content"].(string)
		log.Printf("  nonstream finish=%v tool_calls=%d content_len=%d",
			getNested(out, "choices", 0, "finish_reason"),
			len(tc), len(content))
	}

	writeJSON(w, http.StatusOK, out)
}

// stopSequencesFrom 从客户端请求中提取 stop 序列（OpenAI 允许 string 或 string[]）。
// json.RawMessage 也是 string 的底层类型但不能直接断言成 string —— Anthropic
// 转换路径写入的 stop 就是 RawMessage，漏掉它会让 /v1/messages 的
// stop_sequences 截断兜底整个失效。
func stopSequencesFrom(params map[string]any) []string {
	var stops []string
	addString := func(s string) {
		if s != "" {
			stops = append(stops, s)
		}
	}
	switch v := params["stop"].(type) {
	case string:
		addString(v)
	case json.RawMessage:
		var one string
		if json.Unmarshal(v, &one) == nil {
			addString(one)
			break
		}
		var many []string
		if json.Unmarshal(v, &many) == nil {
			for _, s := range many {
				addString(s)
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				addString(s)
			}
		}
	}
	return stops
}

// truncateAtStopSequences 在网关侧兜底执行 OpenAI 语义的 stop 截断：
// cline 上游对 stop 的执行不可靠（实测时而不截断、时而整体吞掉内容），
// 因此对非流式响应在返回前做确定性截断（截到最早出现的 stop 序列之前）。
// 流式透传路径不做缓冲截断，避免破坏 SSE 的逐块转发。
func truncateAtStopSequences(out map[string]any, stops []string) {
	if len(stops) == 0 {
		return
	}
	choices, ok := out["choices"].([]any)
	if !ok || len(choices) == 0 {
		return
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return
	}
	msg, ok := choice["message"].(map[string]any)
	if !ok {
		return
	}
	content, ok := msg["content"].(string)
	if !ok || content == "" {
		return
	}
	cut := -1
	for _, s := range stops {
		if i := strings.Index(content, s); i >= 0 && (cut < 0 || i < cut) {
			cut = i
		}
	}
	if cut >= 0 {
		msg["content"] = content[:cut]
	}
}

// toolAccum 流式工具调用累积器（按上游 index 分桶）
type toolAccum struct {
	call map[string]any
	args strings.Builder
}

// isSSELine 判断一行是否属于 SSE 语法（字段名 + 冒号，或注释行 ":..."）。
// 上游心跳（": keep-alive"）、event:/id:/retry: 行都与 data: 同属一个流。
func isSSELine(line string) bool {
	if strings.HasPrefix(line, ":") {
		return true // 注释/心跳
	}
	for _, f := range []string{"data:", "event:", "id:", "retry:"} {
		if strings.HasPrefix(line, f) {
			return true
		}
	}
	return false
}

func collectStreamResponse(upstream *http.Response) (map[string]any, error) {
	// 自己关掉响应体：本函数把 body 整个读完，所有权理应在此结束。兄弟函数
	// responsesSSEToChat 一直这么做；这里不关会让连接既不回收也不复用，在长跑
	// 容器里按请求数泄漏（压缩路径每次摘要都走这条）。
	defer upstream.Body.Close()
	var (
		model        string
		content      strings.Builder
		finishReason string
		usage        map[string]any
		// 并行工具调用按上游 index 分桶累积（与 chatStreamToResponses 一致），
		// 上游交错下发 index 0/1/0/1 片段时单桶会互相污染
		pendingTools = map[int]*toolAccum{}
	)

	// 整段缓冲再逐行解析：能区分"真 SSE 流"与"上游 200 但返回 JSON 错误体"
	//（后者以单行 JSON 到达，无 data: 前缀；按 SSE 解析会得到空 200，把
	// 上游错误伪装成空成功）。
	raw, readErr := io.ReadAll(upstream.Body)
	if readErr != nil {
		return nil, fmt.Errorf("upstream read failed: %w", readErr)
	}
	body := string(raw)
	firstNonEmpty := ""
	for _, l := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			firstNonEmpty = t
			break
		}
	}
	// 非 SSE：整段按 JSON 错误体处理（error/data/普通对象都透传给调用方）。
	// 判据是"首个非空行是否为 SSE 行"，而不是只看 data: —— 上游常在首个
	// data 块之前发心跳注释（mimo 实测 ": keep-alive"），把它当非 SSE 会让
	// 正常流被误判成 500 parse_error。
	if firstNonEmpty != "" && !isSSELine(firstNonEmpty) {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err == nil {
			// 带 error 的 JSON 是上游错误体：必须上抛。原样透传会让调用方把它
			// 当成功响应下发（无 choices → 客户端收到"空 200"，错误被吞掉）。
			if e, ok := obj["error"]; ok && e != nil {
				msg := ""
				if em, ok := e.(map[string]any); ok {
					msg, _ = em["message"].(string)
				}
				if msg == "" {
					msg = kit.Truncate(fmt.Sprint(e), 300)
				}
				return nil, fmt.Errorf("upstream error: %s", msg)
			}
			return obj, nil
		}
		if strings.Contains(strings.ToLower(body), "error") {
			return nil, fmt.Errorf("upstream returned non-SSE error body: %s", kit.Truncate(body, 300))
		}
		return nil, fmt.Errorf("upstream returned non-SSE body: %s", kit.Truncate(body, 300))
	}

	reader := bufio.NewReader(strings.NewReader(body))
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			// 真实读错误（连接中断等）：把部分内容当成功返回会让客户端拿到
			// 截断的代码/工具参数还毫无察觉 —— 上抛由调用方返回 500 重试
			return nil, fmt.Errorf("upstream stream read failed after %d bytes: %w", content.Len(), err)
		}
		line = strings.TrimRight(line, "\r\n")

		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(line[5:])
			if payload == "" || payload == "[DONE]" {
				if err == io.EOF {
					break
				}
				continue
			}
			var obj map[string]any
			if json.Unmarshal([]byte(payload), &obj) != nil {
				continue
			}
			if data, ok := obj["data"]; ok {
				if d, ok := data.(map[string]any); ok {
					obj = d
				}
			}
			if m, ok := obj["model"].(string); ok && m != "" {
				model = m
			}
			if u, ok := obj["usage"].(map[string]any); ok && len(u) > 0 {
				usage = u
			}
			// SSE 帧里的 error 事件必须上抛：只凭"没有 choices"就 continue 会把
			// 上游故障静默成空成功（客户端收到 200 + 空内容）。无 choices 本身是
			// 合法的（纯 usage 收尾帧），所以判定条件是 error 键存在且渲染后非空。
			if e, ok := obj["error"]; ok && e != nil {
				msg := ""
				if em, ok := e.(map[string]any); ok {
					msg, _ = em["message"].(string)
				}
				if msg == "" {
					msg = strings.TrimSpace(fmt.Sprint(e))
				}
				if msg != "" && msg != "<nil>" {
					return nil, fmt.Errorf("upstream stream error after %d bytes: %s", content.Len(), msg)
				}
			}
			choices, _ := getNested(obj, "choices").([]any)
			if len(choices) == 0 {
				continue
			}
			choice, _ := choices[0].(map[string]any)
			if choice == nil {
				continue
			}
			delta, _ := choice["delta"].(map[string]any)
			if delta == nil {
				delta = choice
			}
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				finishReason = fr
			}
			if c, ok := delta["content"].(string); ok && c != "" {
				content.WriteString(c)
			}
			if tcRaw, ok := delta["tool_calls"].([]any); ok {
				for _, tc := range tcRaw {
					tcMap, _ := tc.(map[string]any)
					if tcMap == nil {
						continue
					}
					idx := 0
					if i, ok := tcMap["index"].(float64); ok {
						idx = int(i)
					}
					acc := pendingTools[idx]
					if acc == nil {
						acc = &toolAccum{call: map[string]any{
							"id":       tcMap["id"],
							"type":     "function",
							"function": map[string]any{"name": "", "arguments": ""},
						}}
						pendingTools[idx] = acc
					} else if id, ok := tcMap["id"].(string); ok && id != "" {
						// 后续分片才带到 id（部分上游首片 id 为空）——补齐，
						// 否则工具结果回传时 tool_call_id 对不上
						acc.call["id"] = id
					}
					if fn, ok := tcMap["function"].(map[string]any); ok {
						if n, ok := fn["name"].(string); ok && n != "" {
							acc.call["function"].(map[string]any)["name"] = n
						}
						// cline 上游在工具调用的起始分片发送 "arguments":""（空字符串）。
						// 空串必须整体跳过：若走对象分支会把空串序列化成字面量 ""
						// 追加进参数，最终得到 "\"\"\"\"{\"city\":...}" 这样的坏 JSON。
						if a, ok := fn["arguments"].(string); ok {
							if a != "" {
								acc.args.WriteString(a)
							}
						} else if aRaw, ok := fn["arguments"]; ok && aRaw != nil {
							// 某些上游一次性下发完整对象 —— 序列化后追加，不能丢弃
							if bts, merr := json.Marshal(aRaw); merr == nil {
								acc.args.WriteString(string(bts))
							}
						}
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
	}
	// 按 index 顺序汇出工具调用。按 map 键排序而不是 i++ 递增：上游 index
	// 从 1 起跳（或缺号）时，递增式遍历会当场断链，后面的调用全部丢失。
	toolCalls := make([]any, 0, len(pendingTools))
	if len(pendingTools) > 0 {
		idxs := make([]int, 0, len(pendingTools))
		for i := range pendingTools {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		for _, i := range idxs {
			acc := pendingTools[i]
			acc.call["function"].(map[string]any)["arguments"] = chatToolArguments(acc.args.String())
			if id, ok := acc.call["id"].(string); !ok || id == "" {
				acc.call["id"] = fmt.Sprintf("call_%x_%d", time.Now().UnixNano(), i)
			}
			toolCalls = append(toolCalls, acc.call)
		}
	}

	message := map[string]any{
		"role":    "assistant",
		"content": content.String(),
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	if finishReason == "" {
		// 流以 EOF 结束但从未收到 finish_reason 块 —— 补一个合法值，
		// 空串对 OpenAI 方言客户端是非法 finish_reason
		finishReason = "stop"
	}
	if len(toolCalls) > 0 && finishReason == "stop" {
		// 有工具调用却报 stop：agent 循环会把它当普通回复收尾，不再执行工具。
		// length（被截断）保持原样，客户端应丢弃半截调用。
		finishReason = "tool_calls"
	}
	choice := map[string]any{
		"index":         0,
		"message":       message,
		"finish_reason": finishReason,
	}
	out := map[string]any{
		"id":      "chatcmpl_" + fmt.Sprintf("%x", time.Now().UnixMilli()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{choice},
	}
	if usage != nil {
		out["usage"] = usage
	}
	return out, nil
}

// clineHeaderWatchdog 给单次上游尝试加“响应头看门狗”：派生一个可取消 ctx + 定时
// 取消。返回的 stop 必须在 client.Do 返回后立刻调用（成功/失败都要），它停掉定时器
// 并回报看门狗是否已经触发。
//
// 为什么不能直接用 context.WithTimeout：请求 ctx 控制整个请求（含 body 读取），
// 超时到达会把已建立的长流也掐断。此处的定时器只在“响应头还没到”时取消，头一到
// 就 Stop，SSE 之后跑多久都不受它影响。
func clineHeaderWatchdog(ctx context.Context) (context.Context, func() bool) {
	return clineHeaderWatchdogFor(ctx, ClineHeaderTimeout())
}

// clineHeaderWatchdogFor 是 clineHeaderWatchdog 的时长显式版本（便于单测）。
func clineHeaderWatchdogFor(ctx context.Context, d time.Duration) (context.Context, func() bool) {
	if d <= 0 {
		return ctx, func() bool { return false }
	}
	wctx, cancel := context.WithCancel(ctx)
	var fired atomic.Bool
	timer := time.AfterFunc(d, func() {
		fired.Store(true)
		cancel()
	})
	return wctx, func() bool {
		timer.Stop()
		return fired.Load()
	}
}

// modelNeedsStream 该模型是否必须让上游走流式：模型元数据（同步层按命名约定
// 推断的 RequiresStream）或上游错误自学习命中即 true。
func modelNeedsStream(modelID string) bool {
	initModelsCache()
	modelsMu.Lock()
	needs := false
	if m, ok := modelsCache[modelID]; ok && m.RequiresStream {
		needs = true
	}
	modelsMu.Unlock()
	if needs {
		return true
	}
	// 命名约定漏判的模型由上游错误自学习得到（见 cline_stream.go）
	return clineStreamRequired(modelID)
}

// upstreamNeedsStream 上游是否该走流式：CLINE_FORCE_STREAM 全局开关优先，其次
// 按模型判定。所有"客户端要非流式"的 cline 入口都必须问这个函数，别再直接调
// modelNeedsStream —— 否则全局开关会漏掉部分 API 形态（chat/messages/responses）。
func upstreamNeedsStream(modelID string) bool {
	if ClineForceStream() {
		return true
	}
	return modelNeedsStream(modelID)
}

// withStreamUsage 我们（而非客户端）替上游打开流式时补 stream_options
// .include_usage：不带该参数的上游 SSE 不吐 usage，聚合回 JSON 后 usage 缺失，
// 账号记账与请求日志的 token 列会整体空掉。客户端自己带了 stream_options 时
// 原样尊重，不覆盖。
func withStreamUsage(params map[string]any) {
	if params == nil {
		return
	}
	if _, ok := params["stream_options"]; ok {
		return
	}
	params["stream_options"] = map[string]any{"include_usage": true}
}

// finishZenChatNonStream 非流式 zen 应答收尾（chat 形态）：上游已被强制
// stream=true，此处把 chat SSE 聚合回 chat JSON 再呈现。handleZenChat /
// handleZenChatDirect 共用同一序列，避免 copy-paste 漂移。
func finishZenChatNonStream(w http.ResponseWriter, resp *http.Response, zm *ZenModel, params map[string]any, usageFn func(map[string]any)) {
	chat, aerr := collectStreamResponse(resp)
	if aerr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": aerr.Error(), "type": "parse_error"},
		})
		return
	}
	if usageFn != nil {
		if u, ok := chat["usage"].(map[string]any); ok && len(u) > 0 {
			usageFn(u)
		}
	}
	chat["model"] = zm.ID
	chat = normalizeOpenAIResponse(chat)
	truncateAtStopSequences(chat, stopSequencesFrom(params))
	writeJSON(w, http.StatusOK, chat)
}

// Anthropic Messages API support
type anthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type toolAccumulator struct {
	index   int
	id      string
	name    string
	args    string
	emitted bool
}

type anthropicReq struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Messages    []anthropicMsg  `json:"messages"`
	System      json.RawMessage `json:"system,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	TopK        *int            `json:"top_k,omitempty"`
	Stop        json.RawMessage `json:"stop_sequences,omitempty"`
	Tools       json.RawMessage `json:"tools,omitempty"`
	ToolChoice  json.RawMessage `json:"tool_choice,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	Extra       map[string]any  `json:"-"`
}

func loadOverrideContent() string {
	data, err := os.ReadFile("override.md")
	if err != nil {
		// override.md 是可选功能，文件不存在时静默使用客户端自带提示词
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content != "" {
		log.Printf("  using override.md as system prompt (%d bytes)", len(content))
	} else {
		log.Printf("  override.md is empty, using client system prompt")
	}
	return content
}

func extractStringContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string first
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Try array of content blocks
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		parts := []string{}
		for _, b := range blocks {
			if b["type"] == "text" {
				if t, ok := b["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func anthropicToolsToOpenAI(tools []any) []any {
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		if tMap, ok := t.(map[string]any); ok {
			// Already in OpenAI format
			if tMap["type"] == "function" {
				out = append(out, t)
				continue
			}
			// Convert Anthropic format to OpenAI
			oai := map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tMap["name"],
					"description": tMap["description"],
					"parameters":  tMap["input_schema"],
				},
			}
			out = append(out, oai)
		}
	}
	return out
}

func anthropicToOpenAI(req anthropicReq) map[string]any {
	openAI := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     req.Stream,
		"messages":   []any{},
	}
	// 指针字段：temperature=0（确定性输出）与未设置必须区分开
	if req.Temperature != nil {
		openAI["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		openAI["top_p"] = *req.TopP
	}
	// stop_sequences 是合法的 OpenAI stop 参数，之前被静默丢弃会导致
	// 依赖停止序列的 Agent 生成失控
	if len(req.Stop) > 0 && string(req.Stop) != "null" {
		openAI["stop"] = json.RawMessage(req.Stop)
	}
	// Convert Anthropic tools to OpenAI format
	if req.Tools != nil {
		var toolsArr []any
		if err := json.Unmarshal(req.Tools, &toolsArr); err == nil {
			openAI["tools"] = anthropicToolsToOpenAI(toolsArr)
		}
	}
	if req.ToolChoice != nil {
		// Anthropic 形状 {"type":"any"}/{"type":"tool","name":x} 映射为
		// OpenAI 形状 "required"/{"type":"function",...}，原样透传会被上游 400
		var tc map[string]any
		if json.Unmarshal(req.ToolChoice, &tc) == nil {
			switch tc["type"] {
			case "auto":
				openAI["tool_choice"] = "auto"
			case "none":
				openAI["tool_choice"] = "none"
			case "any":
				openAI["tool_choice"] = "required"
			case "tool":
				if n, _ := tc["name"].(string); n != "" {
					openAI["tool_choice"] = map[string]any{
						"type":     "function",
						"function": map[string]any{"name": n},
					}
				}
			default:
				openAI["tool_choice"] = req.ToolChoice
			}
		}
	}

	msgs := []any{}

	// System prompt: 默认用请求自带的 system；仅 APPLY_SYSTEM_PROMPT_OVERRIDE=true
	// 时才用 override.md 替换（与 chat 路径 applyOverride 行为一致）
	sysContent := ""
	if SystemPromptOverrideEnabled() {
		sysContent = loadOverrideContent()
	}
	if sysContent == "" && req.System != nil {
		sysContent = extractStringContent(req.System)
	}
	if sysContent != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": sysContent})
	}

	for _, m := range req.Messages {
		switch c := m.Content.(type) {
		case string:
			msgs = append(msgs, map[string]any{"role": m.Role, "content": c})
		case []any:
			textParts := []string{}
			var toolCalls []any
			var toolResults []map[string]any

			for _, block := range c {
				if b, ok := block.(map[string]any); ok {
					switch b["type"] {
					case "text":
						if t, ok := b["text"].(string); ok {
							textParts = append(textParts, t)
						}
					case "image":
						// skip images
					case "tool_use":
						argsStr := "{}"
						if input, ok := b["input"]; ok && input != nil {
							if s, ok := input.(string); ok {
								argsStr = s
							} else if bts, err := json.Marshal(input); err == nil {
								argsStr = string(bts)
							}
						}
						tc := map[string]any{
							"id":   b["id"],
							"type": "function",
							"function": map[string]any{
								"name":      b["name"],
								"arguments": argsStr,
							},
						}
						toolCalls = append(toolCalls, tc)
					case "tool_result":
						toolCallID, _ := b["tool_use_id"].(string)
						if toolCallID == "" {
							continue
						}
						toolResults = append(toolResults, map[string]any{
							"role":         "tool",
							"content":      anthropicContentToString(b["content"]),
							"tool_call_id": toolCallID,
						})
					}
				}
			}

			if m.Role == "assistant" && len(toolCalls) > 0 {
				msg := map[string]any{
					"role":       "assistant",
					"content":    strings.Join(textParts, "\n"),
					"tool_calls": toolCalls,
				}
				msgs = append(msgs, msg)
				log.Printf("  anthropic req: assistant tool_calls=%d", len(toolCalls))
			} else if m.Role == "user" && len(toolResults) > 0 {
				for _, tr := range toolResults {
					msgs = append(msgs, tr)
					content, _ := tr["content"].(string)
					id, _ := tr["tool_call_id"].(string)
					log.Printf("  anthropic req: tool_result id=%s content_len=%d prefix=%s", id, len(content), kit.Truncate(content, 400))
				}
				// 混合块中 tool_result 与 text 并存时，text 不能丢
				//（Claude Code 在工具执行期间允许用户输入）
				if len(textParts) > 0 {
					msgs = append(msgs, map[string]any{"role": "user", "content": strings.Join(textParts, "\n")})
				}
			} else {
				content := strings.Join(textParts, "\n")
				msgs = append(msgs, map[string]any{"role": m.Role, "content": content})
			}
		}
	}

	openAI["messages"] = msgs
	return openAI
}

// repairToolArguments 修复上游偶发的损坏 tool arguments（杂散 "" 前缀、
// 整体被字符串包裹、截断 JSON —— parseToolArgs 注释记录的已知怪癖）。
// 参数本身合法时原样返回；可修复时重新序列化为规范 JSON 字符串；
// 修复失败时原样返回（不吞掉上游原始值）。
func repairToolArguments(args string) string {
	if args == "" || json.Valid([]byte(args)) {
		return args
	}
	v, err := parseToolArgs(args)
	if err != nil {
		return args
	}
	b, err := json.Marshal(v)
	if err != nil {
		return args
	}
	return string(b)
}

// chatToolArguments chat 方言 tool_calls.arguments 归一化：空串补 "{}"
// （OpenAI 方言要求合法 JSON 字符串，空串会让严格客户端/上游解析失败），
// 其余交给 repairToolArguments。
func chatToolArguments(args string) string {
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return repairToolArguments(args)
}

// mergeConcatenatedJSON 合并粘连的多个 JSON 对象（上游把两次工具调用的参数
// 串成一段时产生）。全部为对象时按键合并，后到的非空值覆盖；出现数组/非
// 对象/截断则失败，交回上层兜底。
func mergeConcatenatedJSON(raw string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(raw))
	merged := map[string]any{}
	count := 0
	for {
		var v any
		err := dec.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		for k, val := range m {
			if s, isStr := val.(string); isStr && s == "" {
				continue // 空值不覆盖已累积内容
			}
			merged[k] = val
		}
		count++
	}
	if count < 2 {
		return nil, false // 单个对象不归这里管
	}
	return merged, true
}

// parseToolArgs 解析工具调用参数 JSON，带容错修复
func parseToolArgs(raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	// 清理杂散引号前缀：上游流式输出偶发 "" 前缀（如 ""{"file_path":...}）
	for strings.HasPrefix(raw, `""`) {
		raw = strings.TrimPrefix(raw, `""`)
	}
	raw = strings.TrimSpace(raw)
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		if v == nil {
			return map[string]any{}, nil
		}
		return v, nil
	}
	// 整体被 JSON 字符串包裹（"{\"file_path\": ...}"）时，解包字符串后再解析
	if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) && len(raw) >= 2 {
		var s string
		if json.Unmarshal([]byte(raw), &s) == nil {
			var v2 any
			if json.Unmarshal([]byte(s), &v2) == nil && v2 != nil {
				return v2, nil
			}
		}
	}
	fixed := raw
	if strings.HasPrefix(fixed, "{") && !strings.HasSuffix(fixed, "}") {
		fixed += "}"
	} else if strings.HasPrefix(fixed, "[") && !strings.HasSuffix(fixed, "]") {
		fixed += "]"
	}
	if strings.HasSuffix(fixed, ",") {
		fixed = strings.TrimRight(fixed, ",") + "}"
	}
	if err := json.Unmarshal([]byte(fixed), &v); err == nil && v != nil {
		return v, nil
	}
	// 粘连的多对象：上游把两次工具调用的参数串成一段
	//（实测 spark 并行调用产出 {"query":...}{"url":...}）。
	// 按键合并 —— 按"取首个对象"的兜底会把后一个调用的参数整段丢掉。
	if v2, ok := mergeConcatenatedJSON(raw); ok {
		return v2, nil
	}
	// 最终兜底：从杂散内容中提取首个 JSON 对象/数组
	if i := strings.IndexAny(raw, "{["); i >= 0 {
		openCh := raw[i]
		closeCh := byte('}')
		if openCh == '[' {
			closeCh = ']'
		}
		if j := strings.LastIndex(raw, string(closeCh)); j > i {
			sub := raw[i : j+1]
			if json.Unmarshal([]byte(sub), &v) == nil && v != nil {
				return v, nil
			}
		}
	}
	return nil, fmt.Errorf("invalid json: %s", kit.Truncate(raw, 120))
}

// extractToolSchemas 从 Anthropic 请求的 tools 定义中解析每个工具的 input_schema 属性集合，
// 用于转发 tool_use 时裁剪 input，避免多余字段触发客户端校验失败。
func extractToolSchemas(tools json.RawMessage) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if len(tools) == 0 {
		return out
	}
	var arr []map[string]any
	if err := json.Unmarshal(tools, &arr); err != nil {
		return out
	}
	for _, t := range arr {
		name, _ := t["name"].(string)
		if name == "" {
			continue
		}
		schema, _ := t["input_schema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		var propNames []string
		for k := range props {
			propNames = append(propNames, k)
		}
		var required []string
		if req, ok := schema["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}
		log.Printf("  tool schema: name=%s properties=%v required=%v", name, propNames, required)
		if len(props) == 0 {
			continue
		}
		allowed := map[string]bool{}
		for k := range props {
			allowed[k] = true
		}
		out[name] = allowed
	}
	return out
}

// filterToolInput 将工具参数裁剪到客户端 schema 允许的字段内；
// 找不到 schema 或过滤后为空时保留原参数，避免丢参数。
func filterToolInput(name string, input map[string]any, schemas map[string]map[string]bool) map[string]any {
	allowed, ok := schemas[name]
	if !ok || len(allowed) == 0 {
		return input
	}
	out := map[string]any{}
	for k, v := range input {
		if allowed[k] {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return input
	}
	return out
}

// anthropicContentToString 将 Anthropic content（字符串或块数组）转为纯文本
func anthropicContentToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if arr, ok := v.([]any); ok {
		parts := []string{}
		for _, it := range arr {
			if b, ok := it.(map[string]any); ok {
				if t, ok := b["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func openAIToAnthropic(openAI map[string]any) map[string]any {
	out := map[string]any{
		"id":    "msg_" + fmt.Sprintf("%x", time.Now().UnixMilli()),
		"type":  "message",
		"role":  "assistant",
		"model": getNested(openAI, "model"),
	}

	choices := getNested(openAI, "choices")
	if choices == nil {
		out["content"] = []any{map[string]any{"type": "text", "text": ""}}
		out["stop_reason"] = "end_turn"
		out["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0}
		return out
	}

	text := ""
	choice0, ok := getNested(openAI, "choices", 0).(map[string]any)
	if !ok {
		out["content"] = []any{map[string]any{"type": "text", "text": text}}
		out["stop_reason"] = "end_turn"
		out["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0}
		return out
	}
	msg, _ := choice0["message"].(map[string]any)
	if msg == nil {
		msg, _ = choice0["delta"].(map[string]any)
	}
	if msg != nil {
		if c, ok := msg["content"].(string); ok {
			text = c
		}
	}

	contentBlocks := []any{map[string]any{"type": "text", "text": text}}

	// Convert tool_calls to Anthropic tool_use blocks
	if msg != nil {
		if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
			contentBlocks = []any{}
			if text != "" {
				contentBlocks = append(contentBlocks, map[string]any{"type": "text", "text": text})
			}
			for _, tcItem := range tc {
				if tcMap, ok := tcItem.(map[string]any); ok {
					funcData, _ := tcMap["function"].(map[string]any)
					if funcData == nil {
						continue
					}
					input := funcData["arguments"]
					// OpenAI arguments is a JSON string; Anthropic expects an object
					if argsStr, ok := input.(string); ok {
						var argsObj any
						if json.Unmarshal([]byte(argsStr), &argsObj) == nil {
							input = argsObj
						}
					}
					if input == nil {
						input = map[string]any{}
					}
					id, _ := tcMap["id"].(string)
					if id == "" {
						id = fmt.Sprintf("toolu_%x_%d", time.Now().UnixMilli(), len(contentBlocks))
					}
					name, _ := funcData["name"].(string)
					if name == "" {
						continue
					}
					block := map[string]any{
						"type":  "tool_use",
						"id":    id,
						"name":  name,
						"input": input,
					}
					contentBlocks = append(contentBlocks, block)
				}
			}
		}
	}

	out["content"] = contentBlocks

	switch getNested(openAI, "choices", 0, "finish_reason") {
	case "stop":
		out["stop_reason"] = "end_turn"
	case "length":
		out["stop_reason"] = "max_tokens"
	case "tool_calls":
		out["stop_reason"] = "tool_use"
	default:
		out["stop_reason"] = "end_turn"
	}

	// usage 字段必须有确定数值：缺 key 时置 0，null 会炸掉 Anthropic 客户端
	// 的整数断言（上下文预估 / 费用统计）
	usage := map[string]any{"input_tokens": 0, "output_tokens": 0}
	if u := getNested(openAI, "usage"); u != nil {
		if um, ok := u.(map[string]any); ok {
			if pt, ok := um["prompt_tokens"].(float64); ok {
				usage["input_tokens"] = int(pt)
			}
			if ct, ok := um["completion_tokens"].(float64); ok {
				usage["output_tokens"] = int(ct)
			}
		}
	}
	out["usage"] = usage

	return out
}

func handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	var req anthropicReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}

	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": "messages is required", "type": "parse_error"},
		})
		return
	}

	toolSchemas := extractToolSchemas(req.Tools)
	if len(toolSchemas) > 0 {
		log.Printf("  anthropic tools: %d schemas", len(toolSchemas))
	}

	if req.MaxTokens == 0 {
		req.MaxTokens = defaultMaxTokens
	}

	// combo 别名模型：改写为目标上游模型（anthropicToOpenAI 取 req.Model）
	useProxies := clineProxiesEnabled()
	if c := resolveCombo(req.Model); c != nil {
		log.Printf("  anthropic combo %q -> %s model %q (useProxies=%v)", req.Model, c.Platform, c.Target, c.UseProxies)
		req.Model = c.Target
		if c.UseProxies {
			useProxies = true
		}
	}

	openAIReq := anthropicToOpenAI(req)

	log.Printf("  anthropic: model=%s stream=%v msgs=%d", req.Model, req.Stream, len(req.Messages))

	if shouldServeWorkBuddy(req.Model) {
		workbuddySub.serveAnthropicMessages(w, r, req, openAIReq, toolSchemas)
		return
	}

	// zen 免费模型路由
	if route := routeModel(req.Model); route == "zen" {
		handleZenAnthropic(w, r, req, openAIReq, toolSchemas)
		return
	} else if route == "reject" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is a paid zen model; only free zen models are proxied", req.Model), "type": "invalid_request_error"},
		})
		return
	}

	// cline 路由：未知模型名显式拒绝（与 chat 路径一致）
	if msg := strictModelGate(req.Model); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": msg, "type": "invalid_request_error"},
		})
		return
	}

	total, activeCount := poolStatusSnapshot()

	if total == 0 && activeCount == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{
				"message": "No accounts in pool",
				"type":    "auth_error",
			},
		})
		return
	}

	upstreamStream := req.Stream
	if !req.Stream && upstreamNeedsStream(normalizeRequestModel(req.Model)) {
		upstreamStream = true
		withStreamUsage(openAIReq)
		log.Printf("  anthropic model %s requires stream: forcing upstream stream, will aggregate", req.Model)
	}

	resp, acc, streamed, err := callClineAutoStream(r.Context(), openAIReq, upstreamStream, useProxies)
	if err != nil {
		log.Printf("  anthropic api error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return
	}
	defer resp.Body.Close()

	// 自学习重试过：上游那份是 SSE，即便客户端要的是非流式也要走聚合
	upstreamStream = upstreamStream || streamed

	usageFn := accountUsageFn(acc, openAIReq)

	if req.Stream {
		handleAnthropicStreamWithUsage(w, resp, normalizeRequestModel(req.Model), toolSchemas, stopSequencesFrom(openAIReq), usageFn)
		return
	}

	if upstreamStream {
		out, err := collectStreamResponse(resp)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}
		if u, ok := out["usage"].(map[string]any); ok && len(u) > 0 {
			usageFn(u)
		}
		out = normalizeOpenAIResponse(out)
		truncateAtStopSequences(out, stopSequencesFrom(openAIReq))
		anthropicResp := openAIToAnthropic(out)
		if tc, ok := getNested(out, "choices", 0, "message", "tool_calls").([]any); ok && len(tc) > 0 {
			anthropicResp["stop_reason"] = "tool_use"
		}
		writeJSON(w, http.StatusOK, anthropicResp)
		return
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "parse_error"},
		})
		return
	}
	if u, ok := raw["usage"].(map[string]any); ok && len(u) > 0 {
		usageFn(u)
	}
	out := raw
	if data, ok := raw["data"]; ok {
		if d, ok := data.(map[string]any); ok {
			out = d
		}
	}
	out = normalizeOpenAIResponse(out)
	truncateAtStopSequences(out, stopSequencesFrom(openAIReq))
	anthropicResp := openAIToAnthropic(out)

	if tc, ok := getNested(out, "choices", 0, "message", "tool_calls").([]any); ok && len(tc) > 0 {
		anthropicResp["stop_reason"] = "tool_use"
	}

	writeJSON(w, http.StatusOK, anthropicResp)
}

// handleZenAnthropic Anthropic Messages 请求路由到 zen 免费模型上游
// finishZenAnthropicNonStream 聚合好的 responses chat → Anthropic 非流式应答：
// usage 提取 → model 回填 → normalize → stop 截断 → openAIToAnthropic →
// tool_calls 时 stop_reason=tool_use。handleZenAnthropic 的 responses 直达分支
// 与 auto-learn 重试分支共用，避免 copy-paste 漂移。
func finishZenAnthropicNonStream(w http.ResponseWriter, chat map[string]any, openAIReq map[string]any, zm *ZenModel) {
	chat["model"] = zm.ID
	chat = normalizeOpenAIResponse(chat)
	truncateAtStopSequences(chat, stopSequencesFrom(openAIReq))
	anthropicResp := openAIToAnthropic(chat)
	if tc, ok := getNested(chat, "choices", 0, "message", "tool_calls").([]any); ok && len(tc) > 0 {
		anthropicResp["stop_reason"] = "tool_use"
	}
	writeJSON(w, http.StatusOK, anthropicResp)
}

func handleZenAnthropic(w http.ResponseWriter, r *http.Request, req anthropicReq, openAIReq map[string]any, toolSchemas map[string]map[string]bool) {
	zm, ok := resolveZenFreeModel(req.Model)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not a free zen model", req.Model), "type": "invalid_request_error"},
		})
		return
	}
	isStream := req.Stream

	sid := requestSessionID(openAIReq, r.Header)
	out := maybeCompact(openAIReq, zm, sid)
	if out.changed {
		log.Printf("  anthropic zen: %s", out.note)
	}

	// 原生 responses 端点模型：先经 responses 原生调用拿到 chat 形态，
	// 再走与 chat 路径完全相同的 anthropic 转换。上游恒 stream=true
	//（spark 只接受 stream=true；false 会 403），客户端形态由 isStream 决定。
	if zm.Upstream == "responses" {
		resp, _, rerr := callZenResponsesAPI(r.Context(), openAIReq, true)
		if rerr != nil {
			log.Printf("  anthropic zen responses api error: %v", rerr)
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": map[string]string{"message": rerr.Error(), "type": "api_error"},
			})
			return
		}
		defer resp.Body.Close()
		// 上游恒 stream=true：resp.Body 是原生 responses SSE，先聚合
		var chat map[string]any
		if chat, rerr = responsesSSEToChat(resp); rerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": rerr.Error(), "type": "parse_error"},
			})
			return
		}
		// 聚合结果不含上游 model（responsesSSEToChat 置空），两种客户端
		// 形态都要回填网关模型 ID，避免 Anthropic 应答 model 为空。
		chat["model"] = zm.ID
		if isStream {
			// responses 原生流已聚合为 chat：对 Anthropic 流式客户端按
			// Anthropic SSE 事件序列重发（message_start → 文本/工具块 →
			// message_delta → message_stop），不能回非流式 JSON。
			emitAnthropicSSEFromChat(w, normalizeOpenAIResponse(chat), nil)
			return
		}
		finishZenAnthropicNonStream(w, chat, openAIReq, zm)
		return
	}

	resp, _, err := callZenAPI(r.Context(), openAIReq, true)
	if err != nil && isWrongEndpoint(err) {
		learnZenEndpoint(zm.ID, "responses")
		log.Printf("  anthropic zen endpoint auto-learn: model=%s chat/completions rejected (%v), retrying responses",
			zm.ID, kit.Truncate(err.Error(), 120))
		resp2, _, rerr := callZenResponsesAPI(r.Context(), openAIReq, true)
		if rerr != nil {
			log.Printf("  anthropic zen responses api error: %v", rerr)
			writeJSON(w, http.StatusBadGateway, map[string]any{
				"error": map[string]string{"message": rerr.Error(), "type": "api_error"},
			})
			return
		}
		defer resp2.Body.Close()
		chat, aerr := responsesSSEToChat(resp2)
		if aerr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": aerr.Error(), "type": "parse_error"},
			})
			return
		}
		chat["model"] = zm.ID
		if isStream {
			// 流式客户端：与原生分支一致地重发 Anthropic SSE 事件序列
			//（回非流式 JSON 会让 SDK 的流解析器直接报错）
			emitAnthropicSSEFromChat(w, normalizeOpenAIResponse(chat), nil)
			return
		}
		finishZenAnthropicNonStream(w, chat, openAIReq, zm)
		return
	}
	if err != nil {
		log.Printf("  anthropic zen api error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return
	}
	defer resp.Body.Close()

	if isStream {
		handleAnthropicStreamWithUsage(w, resp, zm.ID, toolSchemas, stopSequencesFrom(openAIReq), nil)
		return
	}

	// FreeTier gate 要求上游恒 stream=true；非流式客户端聚合上游 chat SSE
	// 后再转 anthropic 形态。
	chatOut, aerr := collectStreamResponse(resp)
	if aerr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{"message": aerr.Error(), "type": "parse_error"},
		})
		return
	}
	chatOut["model"] = zm.ID
	chatOut = normalizeOpenAIResponse(chatOut)
	// 与 finishZenAnthropicNonStream 保持一致：上游对 stop 的执行不可靠，
	// 非流式应答统一在网关侧兜底截断（漏掉这里会让 stop_sequences 静默失效）
	truncateAtStopSequences(chatOut, stopSequencesFrom(openAIReq))
	anthropicResp := openAIToAnthropic(chatOut)
	if tc, ok := getNested(chatOut, "choices", 0, "message", "tool_calls").([]any); ok && len(tc) > 0 {
		anthropicResp["stop_reason"] = "tool_use"
	}
	writeJSON(w, http.StatusOK, anthropicResp)
}

func handleAnthropicStreamWithUsage(w http.ResponseWriter, upstream *http.Response, modelName string, toolSchemas map[string]map[string]bool, stops []string, onUsage func(map[string]any)) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("  anthropic stream: streaming not supported for client, falling back to non-stream aggregation")
		out, err := collectStreamResponse(upstream)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{"message": err.Error(), "type": "parse_error"},
			})
			return
		}
		if u, ok := out["usage"].(map[string]any); ok && len(u) > 0 {
			onUsage(u)
		}
		out = normalizeOpenAIResponse(out)
		// 非流式回退仍需网关侧 stop 兜底（与其余非流式出口一致）
		truncateAtStopSequences(out, stops)
		anthropicResp := openAIToAnthropic(out)
		writeJSON(w, http.StatusOK, anthropicResp)
		return
	}

	log.Printf("  anthropic stream: starting real-time forward")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	// 原始 SSE 落盘（完整对话内容，无上限）仅在 STREAM_LOG=true 时开启 ——
	// 公网长跑部署默认关闭，避免磁盘无限增长与对话内容留存
	var streamLog *os.File
	if StreamLogEnabled() {
		if sf, err := os.OpenFile(kit.ResolveDataPath("afree-proxy-stream.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
			streamLog = sf
		}
	}
	defer func() {
		if streamLog != nil {
			streamLog.Close()
		}
	}()

	emit := func(event string, data any) {
		d, _ := json.Marshal(data)
		line := fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(d))
		w.Write([]byte(line))
		if streamLog != nil {
			streamLog.WriteString(line)
		}
		flusher.Flush()
	}

	msgID := "msg_" + fmt.Sprintf("%x", time.Now().UnixMilli())
	stopReason := "end_turn"
	// lastUsage 记录最近一次上游 usage 块，message_delta 时回传给客户端
	var lastUsage map[string]any
	emit("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":      msgID,
			"type":    "message",
			"role":    "assistant",
			"content": []any{},
			"model":   modelName,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
			"stop_reason": nil,
		},
	})

	textIndex := new(int)
	*textIndex = -1
	hasText := false
	pendingTools := map[int]*toolAccumulator{}
	emitIndex := 0
	nextIndex := func() int {
		i := emitIndex
		emitIndex++
		return i
	}

	emitToolBlock := func(acc *toolAccumulator) {
		acc.emitted = true
		if acc.name == "" {
			log.Printf("  tool_use missing name, skipping (id=%s)", acc.id)
			return
		}
		idx := nextIndex()
		id := acc.id
		if id == "" {
			id = fmt.Sprintf("toolu_%x_%d", time.Now().UnixMilli(), idx)
			log.Printf("  tool_use missing id, generated %s", id)
		}
		argsObj, err := parseToolArgs(acc.args)
		if err != nil {
			log.Printf("  tool args parse failed for %s: %v (raw: %s)", acc.name, err, kit.Truncate(acc.args, 300))
			argsObj = map[string]any{}
		}
		if inputMap, ok := argsObj.(map[string]any); ok {
			argsObj = filterToolInput(acc.name, inputMap, toolSchemas)
		}
		parsed, _ := json.Marshal(argsObj)
		// 只记名字/长度: 完整工具入参（文件内容等）不该无条件进日志
		log.Printf("  tool_use emit: name=%s id=%s input_len=%d", acc.name, id, len(parsed))
		emit("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": idx,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    id,
				"name":  acc.name,
				"input": map[string]any{},
			},
		})
		emit("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": idx,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": string(parsed),
			},
		})
		emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": idx,
		})
	}

	processSSELine := func(line string) {
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "data:") {
			return
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			return
		}

		var obj map[string]any
		if err := json.Unmarshal([]byte(payload), &obj); err != nil {
			return
		}
		if onUsage != nil {
			if u, ok := obj["usage"].(map[string]any); ok && len(u) > 0 {
				lastUsage = u
				onUsage(u)
			}
		}
		if data, ok := obj["data"]; ok {
			if d, ok := data.(map[string]any); ok {
				obj = d
			}
		}

		if errPayload, ok := obj["error"]; ok {
			errBody, _ := json.Marshal(errPayload)
			log.Printf("  upstream SSE error: %s", string(errBody))
			emit("error", map[string]any{"type": "error", "error": errPayload})
			return
		}

		choices, _ := getNested(obj, "choices").([]any)
		if len(choices) == 0 {
			return
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			return
		}

		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			delta = choice
		}

		if c, ok := delta["content"].(string); ok && c != "" {
			if !hasText {
				hasText = true
				*textIndex = nextIndex()
				emit("content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": *textIndex,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				})
			}
			emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": *textIndex,
				"delta": map[string]any{
					"type": "text_delta",
					"text": c,
				},
			})
		}

		if tcRaw, ok := delta["tool_calls"].([]any); ok {
			for _, tc := range tcRaw {
				tcMap, _ := tc.(map[string]any)
				if tcMap == nil {
					continue
				}
				idx := 0
				if i, ok := tcMap["index"].(float64); ok {
					idx = int(i)
				}
				acc, exists := pendingTools[idx]
				if !exists {
					acc = &toolAccumulator{index: idx}
					pendingTools[idx] = acc
				}
				if id, ok := tcMap["id"].(string); ok && id != "" {
					acc.id = id
				}
				if fn, ok := tcMap["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok && name != "" {
						acc.name = name
					}
					// 空字符串分片整体跳过（否则序列化成字面量 "" 拼进参数，见
					// collectStreamResponse 同注）；非字符串对象序列化后追加
					if args, ok := fn["arguments"].(string); ok {
						if args != "" {
							acc.args += args
						}
					} else if argsRaw, ok := fn["arguments"]; ok && argsRaw != nil {
						if bts, err := json.Marshal(argsRaw); err == nil {
							acc.args += string(bts)
						}
					}
				}
			}
		}

		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			switch fr {
			case "length":
				stopReason = "max_tokens"
			case "tool_calls":
				stopReason = "tool_use"
			}
		}
	}

	var streamErr error
	reader := bufio.NewReader(upstream.Body)

	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			processSSELine(line)
		}
		if err != nil {
			if err != io.EOF {
				streamErr = err
			}
			break
		}
	}

	// Stop text block if active
	if hasText {
		emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": *textIndex,
		})
	}

	// Emit any remaining un-emitted tool blocks（按 index 升序 —— map 遍历
	// 顺序随机，会让相同请求每次得到不同的工具块顺序）
	idxs := make([]int, 0, len(pendingTools))
	for i := range pendingTools {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	emittedTools := 0
	for _, i := range idxs {
		if acc := pendingTools[i]; !acc.emitted {
			emitToolBlock(acc)
		}
		emittedTools++
	}
	if emittedTools > 0 && stopReason == "end_turn" {
		// 上游 finish_reason 常为 stop 但确实发了工具调用；报 end_turn 会让
		// 客户端收尾而不执行工具（agent 循环当场断掉）
		stopReason = "tool_use"
	}

	// 上游中途断流（非 EOF 的真实读错误）: 不能继续伪造 message_delta/end_turn
	// + message_stop —— 那会把截断的响应当成完整成功消息交给客户端
	//（Responses 路径对此发 response.failed，这里按 Anthropic 规范发 error 事件）
	if streamErr != nil {
		log.Printf("  anthropic stream upstream error: %v", streamErr)
		emit("error", map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "api_error", "message": kit.Truncate("upstream stream interrupted: "+streamErr.Error(), 300)},
		})
		return
	}

	// 上游 usage 透传给 Anthropic 客户端（Claude Code / ZCode 用它做
	// 上下文预估与自动 compact；一直报 0 会让长会话撑爆上下文才报错）
	usageOut := map[string]any{"output_tokens": 0}
	if lastUsage != nil {
		if ct, ok := lastUsage["completion_tokens"].(float64); ok {
			usageOut["output_tokens"] = int(ct)
		}
		if pt, ok := lastUsage["prompt_tokens"].(float64); ok {
			usageOut["input_tokens"] = int(pt)
		}
	}
	emit("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usageOut,
	})

	emit("message_stop", map[string]any{"type": "message_stop"})
	log.Printf("  anthropic stream done: hasText=%v tools=%d reason=%s", hasText, len(pendingTools), stopReason)
}

// emitAnthropicSSEFromChat 把聚合好的 chat completions 应答按 Anthropic SSE
// 事件序列重发：message_start → text 块（content_block_start/delta/stop）→
// tool_use 块（如有）→ message_delta（stop_reason + usage）→ message_stop。
// 用于 responses 路径的流式 Anthropic 客户端：上游原生 responses SSE 已
// 被聚合为 chat（handleZenAnthropic responses 分支），再以 Anthropic 形态
// 逐事件下发，避免对 stream=true 客户端回非流式 JSON。
func emitAnthropicSSEFromChat(w http.ResponseWriter, chat map[string]any, usageFn func(map[string]any)) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	emit := func(event string, data any) {
		d, _ := json.Marshal(data)
		w.Write([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(d))))
		if flusher != nil {
			flusher.Flush()
		}
	}

	modelName, _ := chat["model"].(string)
	msgID := "msg_" + fmt.Sprintf("%x", time.Now().UnixMilli())
	stopReason := "end_turn"
	switch getNested(chat, "choices", 0, "finish_reason") {
	case "length":
		stopReason = "max_tokens"
	case "tool_calls":
		stopReason = "tool_use"
	}

	emit("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":      msgID,
			"type":    "message",
			"role":    "assistant",
			"content": []any{},
			"model":   modelName,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
			"stop_reason": nil,
		},
	})

	// text 块：整段内容一次下发（上游已聚合，无逐 token 增量）
	text, _ := getNested(chat, "choices", 0, "message", "content").(string)
	if text != "" {
		emit("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
		emit("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": text,
			},
		})
		emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
	}

	// tool_use 块（如有）。块索引必须连续：无文本块时必须从 0 起，
	// 否则 index 跳号会被客户端当成非法流丢弃工具块。
	toolUseEmitted := false
	if tcs, ok := getNested(chat, "choices", 0, "message", "tool_calls").([]any); ok {
		idx := 0
		if text != "" {
			idx = 1
		}
		for _, tc := range tcs {
			tcm, _ := tc.(map[string]any)
			if tcm == nil {
				continue
			}
			fn, _ := tcm["function"].(map[string]any)
			name := ""
			if fn != nil {
				name, _ = fn["name"].(string)
			}
			if name == "" {
				continue
			}
			id, _ := tcm["id"].(string)
			if id == "" {
				id = fmt.Sprintf("toolu_%x_%d", time.Now().UnixMilli(), idx)
			}
			argsObj := map[string]any{}
			if fn != nil {
				if argsStr, ok := fn["arguments"].(string); ok {
					if json.Unmarshal([]byte(argsStr), &argsObj) != nil {
						argsObj = map[string]any{}
					}
				}
			}
			emit("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": idx,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    id,
					"name":  name,
					"input": map[string]any{},
				},
			})
			parsed, _ := json.Marshal(argsObj)
			emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": string(parsed),
				},
			})
			emit("content_block_stop", map[string]any{
				"type":  "content_block_stop",
				"index": idx,
			})
			toolUseEmitted = true
			idx++
		}
	}
	if toolUseEmitted && stopReason == "end_turn" {
		// 有工具块却报 end_turn：客户端会直接收尾、不执行工具（上游
		// finish_reason 常为 stop，工具块却是真的）
		stopReason = "tool_use"
	}

	// usage 透传 + 结束事件
	usageOut := map[string]any{"output_tokens": 0}
	if u, ok := chat["usage"].(map[string]any); ok {
		if ct, ok := u["completion_tokens"].(float64); ok {
			usageOut["output_tokens"] = int(ct)
		}
		if pt, ok := u["prompt_tokens"].(float64); ok {
			usageOut["input_tokens"] = int(pt)
		}
		if usageFn != nil {
			usageFn(u)
		}
	}
	emit("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": usageOut,
	})
	emit("message_stop", map[string]any{"type": "message_stop"})
}

func normalizeOpenAIResponse(obj map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range obj {
		if k == "provider_metadata" || k == "proxy_metadata" {
			continue
		}
		out[k] = v
	}

	if choices, ok := out["choices"].([]any); ok {
		normalized := make([]any, 0, len(choices))
		for _, ch := range choices {
			if c, ok := ch.(map[string]any); ok {
				nc := make(map[string]any)
				for k, v := range c {
					if k == "provider_metadata" || k == "proxy_metadata" {
						continue
					}
					nc[k] = v
				}
				if msg, ok := nc["message"].(map[string]any); ok {
					nc["message"] = normalizeMessage(msg)
				}
				if delta, ok := nc["delta"].(map[string]any); ok {
					nd := make(map[string]any)
					for k, v := range delta {
						if k == "provider_metadata" || k == "proxy_metadata" {
							continue
						}
						nd[k] = v
					}
					if tc, ok := nd["tool_calls"].([]any); ok && len(tc) > 0 {
						if nd["content"] == nil {
							nd["content"] = ""
						}
					}
					nc["delta"] = nd
				}
				normalized = append(normalized, nc)
			} else {
				normalized = append(normalized, ch)
			}
		}
		out["choices"] = normalized
	}

	return out
}

func normalizeMessage(msg map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range msg {
		if k == "provider_metadata" || k == "proxy_metadata" {
			continue
		}
		out[k] = v
	}
	if tc, ok := out["tool_calls"].([]any); ok && len(tc) > 0 {
		if out["content"] == nil {
			out["content"] = ""
		}
		// 非流式聚合路径的兜底修复：上游偶发损坏 arguments 在这里恢复；
		// 空串补 "{}"（严格客户端/上游不接受空 arguments）
		for _, t := range tc {
			if tm, ok := t.(map[string]any); ok {
				if fn, ok := tm["function"].(map[string]any); ok {
					if a, ok := fn["arguments"].(string); ok {
						fn["arguments"] = chatToolArguments(a)
					} else if fn["arguments"] == nil {
						fn["arguments"] = "{}"
					}
				}
			}
		}
	}
	if c, ok := out["content"].(string); ok {
		out["content"] = c
	}
	return out
}

func getNested(obj map[string]any, keys ...any) any {
	current := any(obj)
	for _, key := range keys {
		switch k := key.(type) {
		case string:
			if m, ok := current.(map[string]any); ok {
				current = m[k]
			} else {
				return nil
			}
		case int:
			// k 必须同时满足上下界：只判 k < len(arr) 时，负数下标会直接 panic
			//（数组越界），而本函数是"取不到就 nil"的宽容语义
			if arr, ok := current.([]any); ok && k >= 0 && k < len(arr) {
				current = arr[k]
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return current
}

// freePort 开发便利：Windows 上强杀占用端口的进程。仅当显式设置
// KILL_PORT_ON_START=true 时启用 —— 默认关闭，生产环境无差别强杀
// 未知进程是危险的（可能干掉合法持有端口的服务）。
func freePort(port int) {
	if v, ok := envBool("KILL_PORT_ON_START"); !ok || !v {
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

// parseInferenceCapDuration 从 Cline 429 错误体中解析 "Try again in 17h 59m" 形式的等待时长。
// 支持 "17h 59m"、"17h"、"59m"、"30s"、"1d 2h 30m" 等组合。
func parseInferenceCapDuration(body string) time.Duration {
	// 在错误体中查找 "Try again in ..." 子串
	idx := strings.Index(body, "Try again in")
	if idx < 0 {
		return 0
	}
	rest := body[idx+len("Try again in"):]
	// 截取到下一个引号或换行
	end := len(rest)
	if i := strings.IndexAny(rest, "\"\n\r}"); i >= 0 {
		end = i
	}
	segment := strings.TrimSpace(rest[:end])
	return parseHumanDuration(segment)
}

// parseHumanDuration 解析 "17h 59m" / "2h" / "59m" / "30s" / "1d 2h" 之类的时长。
func parseHumanDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	var total time.Duration
	num := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			num = num*10 + int(c-'0')
		case c == 'd':
			total += time.Duration(num) * 24 * time.Hour
			num = 0
		case c == 'h':
			total += time.Duration(num) * time.Hour
			num = 0
		case c == 'm' && i+1 < len(s) && s[i+1] == 's':
			total += time.Duration(num) * time.Millisecond
			num = 0
			i++
		case c == 'm':
			total += time.Duration(num) * time.Minute
			num = 0
		case c == 's':
			total += time.Duration(num) * time.Second
			num = 0
		case c == ' ':
			// 分隔符
		default:
			// 未知字符，重置
			num = 0
		}
	}
	if total <= 0 {
		return 0
	}
	return total
}

// isClineQuotaExhausted 从 429 错误体判断是否为配额耗尽（而非瞬时限流）。
// 只有明确的配额措辞才返回 true；429 缺少可解析时长时用它决定兜底冷却长度：
// 配额耗尽 → 18h，其余 → 20min 短冷却。
func isClineQuotaExhausted(body string) bool {
	low := strings.ToLower(body)
	for _, kw := range []string{
		"quota", "exceeded", "insufficient", "out of credit", "no credits",
		"daily limit", "monthly limit", "usage limit", "credit limit", "billing",
	} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// parseRetryAfter 解析 HTTP Retry-After 头（秒数或 HTTP 日期）。
func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	// 尝试秒数
	if secs, err := parseIntSafe(header); err == nil {
		return time.Duration(secs) * time.Second
	}
	// 尝试 HTTP 日期
	if t, err := http.ParseTime(header); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

func parseIntSafe(s string) (int, error) {
	var n int
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, nil
}

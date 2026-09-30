package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"afree-proxy/internal/kit"
)

// routeStat 网关请求计数（进程生命周期，仅 /v1/ 路径）：请求数、错误数、
// 累计耗时。落内存原子计数，供仪表盘统计区展示。
type routeStat struct {
	requests atomic.Int64
	errors   atomic.Int64
	durMS    atomic.Int64
}

var (
	routeStats    = map[string]*routeStat{"cline": {}, "zen": {}, "workbuddy": {}}
	reqStatsStart = time.Now()
)

// recordRouteStat 按路由累计一次网关请求；admin 与未归入三平台的流量
// （模型列表拉取、提取不到模型名的请求等）不计数。
func recordRouteStat(route string, status int, took time.Duration) {
	rs := routeStats[route]
	if rs == nil {
		return
	}
	rs.requests.Add(1)
	if status >= 400 {
		rs.errors.Add(1)
	}
	rs.durMS.Add(took.Milliseconds())
}

// RequestLog 单条代理请求记录（对话/API 调用历史）
type RequestLog struct {
	Time      time.Time `json:"time"`
	Client    string    `json:"client"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Model     string    `json:"model,omitempty"`
	Route     string    `json:"route"` // zen | cline | admin | other
	Status    int       `json:"status"`
	Duration  int64     `json:"duration_ms"`
	Upstream  string    `json:"upstream,omitempty"`  // 命中的上游：账号#N 或 key#N
	ProxyType string    `json:"proxyType,omitempty"` // 出口代理类型：main|backup|direct（绑定主/辅/直连），空=全局池轮转/未命中上游
	Note      string    `json:"note,omitempty"`
}

// upstreamInfo 挂在请求 context 上的可写槽位：处理链深处（选号/选 key 处）
// 写入命中的账号/key 序号，请求日志中间件在请求收尾时读出。槽位本身随
// context 传递，不引入全局状态，也不改变任何函数签名。
type upstreamInfo struct {
	label string
	exit  string // 出口代理类型：main | backup | ""（直连/全局池）
}

type upstreamCtxKey struct{}

func withUpstreamInfo(ctx context.Context) (context.Context, *upstreamInfo) {
	ui := &upstreamInfo{}
	return context.WithValue(ctx, upstreamCtxKey{}, ui), ui
}

// setUpstreamInfo 记录本次请求命中的上游（cline 账号序号 / zen key 序号）。
// 同一请求内重试、换号时后写覆盖先写 —— 展示最终实际服务的上游。
// ctx 里没有槽位（面板 Test、内部压缩等非日志请求路径）时是空操作。
func setUpstreamInfo(ctx context.Context, label string) {
	if label == "" || ctx == nil {
		return
	}
	if ui, ok := ctx.Value(upstreamCtxKey{}).(*upstreamInfo); ok {
		ui.label = label
	}
}

// setUpstreamExit 记录本次请求实际使用的出口代理类型（绑定主/辅）。
// 覆盖规则同 setUpstreamInfo：多次尝试时后写覆盖先写。
func setUpstreamExit(ctx context.Context, exit string) {
	if exit == "" || ctx == nil {
		return
	}
	if ui, ok := ctx.Value(upstreamCtxKey{}).(*upstreamInfo); ok {
		ui.exit = exit
	}
}

const (
	maxReqLogs = 500
)

var (
	reqLogsMu sync.Mutex
	reqLogs   []RequestLog
)

var reqLogsFile = kit.ResolveDataPath("requests.jsonl")

// reqLogPath 取当前落盘路径。reqLogsFile 是包级变量（测试会整体替换它来
// 避免污染真实的 data/requests.jsonl），而 AppendReqLog 的落盘 goroutine
// 与替换方并发，裸读会构成数据竞争。
func reqLogPath() string {
	reqLogsMu.Lock()
	defer reqLogsMu.Unlock()
	return reqLogsFile
}

// AppendReqLog 记录一条请求日志：内存环形保留 + 异步追加落盘。
// LOG_REQUESTS=false 时完全关闭（调用方已跳过，这里兜底）。
func AppendReqLog(l RequestLog) {
	if !LogRequestsEnabled() {
		return
	}
	reqLogsMu.Lock()
	reqLogs = append(reqLogs, l)
	if len(reqLogs) > maxReqLogs {
		reqLogs = reqLogs[len(reqLogs)-maxReqLogs:]
	}
	reqLogsMu.Unlock()
	go func() {
		data, _ := json.Marshal(l)
		path := reqLogPath()
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return
		}
		f.Write(append(data, '\n'))
		f.Close()
		// 大小上限（LOG_FILE_MAX_MB，默认 10MB）：超出清空落盘文件（内存仍保留最近 500 条）
		if st, err := os.Stat(path); err == nil && st.Size() > LogFileMaxBytes() {
			os.WriteFile(path, nil, 0600)
		}
	}()
}

// LoadRequestLogs 返回最近的请求日志（内存优先，启动后从落盘文件补载）。
// 必须返回副本：调用方（admin 面板）会原地反转切片，共享底层数组会
// 与 AppendReqLog 的 append 产生数据竞争。
func LoadRequestLogs() []RequestLog {
	reqLogsMu.Lock()
	defer reqLogsMu.Unlock()
	out := make([]RequestLog, len(reqLogs))
	copy(out, reqLogs)
	return out
}

// LoadRequestLogsFromFile 启动时从落盘文件读取尾部记录（日志关闭时跳过）。
// 开关关闭时不回载 admin 行与非 /v1/ 噪声行（favicon 等）：盘上还留着开关
// 打开期间写下的记录，无条件回载会让面板日志页看起来"开关没生效"。
func LoadRequestLogsFromFile() {
	if !LogRequestsEnabled() {
		return
	}
	raw, err := os.ReadFile(reqLogPath())
	if err != nil {
		return
	}
	includeAdmin := poolAdminLogEnabled()
	lines := splitLinesSafe(string(raw))
	reqLogsMu.Lock()
	defer reqLogsMu.Unlock()
	for _, line := range lines {
		if line == "" {
			continue
		}
		var l RequestLog
		if json.Unmarshal([]byte(line), &l) != nil {
			continue
		}
		if !includeAdmin && (l.Route == "admin" || !strings.HasPrefix(l.Path, "/v1/")) {
			continue
		}
		reqLogs = append(reqLogs, l)
	}
	if len(reqLogs) > maxReqLogs {
		reqLogs = reqLogs[len(reqLogs)-maxReqLogs:]
	}
}

func splitLinesSafe(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// ============ 请求日志中间件 ============

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush 透传底层 Flusher，保证 SSE 流式响应不被中间件吞掉
func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// requestLogMiddleware 记录所有进入代理的请求（API 调用与调用历史）。
// LOG_REQUESTS=false 时跳过日志，但仍包一层 statusWriter —— 它实现了
// http.Flusher，SSE 流式响应依赖它透传 Flush。
// 所有请求体先经 http.MaxBytesReader 限幅（MAX_BODY_MB，默认 32MB），
// 公网匿名请求的超大 body 在读入内存前即被拒绝。
func requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		upCtx, upstream := withUpstreamInfo(r.Context())
		r = r.WithContext(upCtx)

		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes())

		logEnabled := LogRequestsEnabled()
		// /admin 请求体里没有 model 字段，探测纯属浪费：面板的 JSON 请求
		// （含大体积配置导入）不必为提取模型名而整体读入再重放。体量限幅
		// 语义不变 —— MaxBytesReader 仍包在 Body 上，超限读失败即 413。
		adminPath := strings.HasPrefix(r.URL.Path, "/admin")
		model := ""
		if logEnabled {
			// 读取请求体提取模型，并放回，避免影响后续处理
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				// 超限（或读失败）：直接 413，不进入业务处理
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
					"error": map[string]string{
						"message": fmt.Sprintf("request body too large (limit %d MB)", MaxRequestBodyBytes()>>20),
						"type":    "invalid_request_error",
					},
				})
				return
			}
			if len(bodyBytes) > 0 {
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				if !adminPath {
					var probe struct {
						Model string `json:"model"`
					}
					if json.Unmarshal(bodyBytes, &probe) == nil {
						model = probe.Model
					}
				}
			}
		}

		next.ServeHTTP(sw, r)

		if !logEnabled {
			return
		}
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		route := "other"
		switch {
		case strings.HasPrefix(r.URL.Path, "/admin") || strings.Contains(r.URL.Path, "health"):
			route = "admin"
		case strings.HasPrefix(model, "zen/") || strings.HasPrefix(model, "opencode/"):
			route = "zen"
		case shouldServeWorkBuddy(model):
			route = "workbuddy"
		case model != "":
			route = "cline"
		case strings.Contains(r.URL.Path, "models"):
			route = "meta"
		}
		// 网关请求计数：仅 /v1/ 下已归入三平台的会话流量，模型列表拉取、
		// 面板与健康检查不计入。与日志同窗口 —— LOG_REQUESTS 关闭时模型名
		// 不再提取，分类失真，故计数放在 logEnabled 分支内随之停摆。
		if logEnabled && strings.HasPrefix(r.URL.Path, "/v1/") {
			recordRouteStat(route, sw.status, time.Since(start))
		}
		// 面板自身的页面/接口访问、/health 健康检查，以及 /favicon.ico 等
		// 浏览器/扫描器噪声（非 /v1/ 服务路径）：默认不记录（面板轮询会把
		// 日志刷满噪声行）。网关设置的"记录管理日志"开关打开后才落盘 ——
		// 关闭时只记录 /v1/ 网关流量（会话与模型列表），模型会话请求始终
		// 在其中。
		if !poolAdminLogEnabled() && (route == "admin" || !strings.HasPrefix(r.URL.Path, "/v1/")) {
			return
		}
		client := r.RemoteAddr
		if host, _, err := net.SplitHostPort(client); err == nil {
			client = host
		}
		AppendReqLog(RequestLog{
			Time:      time.Now(),
			Client:    client,
			Method:    r.Method,
			Path:      r.URL.Path,
			Model:     model,
			Route:     route,
			Status:    sw.status,
			Duration:  time.Since(start).Milliseconds(),
			Upstream:  upstream.label,
			ProxyType: upstream.exit,
		})
	})
}

// handleRequestStats GET /admin/api/request-stats —— 仪表盘统计区数据：
// 各路由进程生命周期内的请求数/错误数/平均耗时 + 计数起点。
func handleRequestStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	routes := make(map[string]any, len(routeStats))
	for name, rs := range routeStats {
		reqs := rs.requests.Load()
		var avg int64
		if reqs > 0 {
			avg = rs.durMS.Load() / reqs
		}
		routes[name] = map[string]any{
			"requests": reqs,
			"errors":   rs.errors.Load(),
			"avg_ms":   avg,
		}
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"since":  reqStatsStart.UnixMilli(),
		"routes": routes,
	}})
}

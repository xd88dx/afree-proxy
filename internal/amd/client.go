package amd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ============ 上游调用 ============
//
// 与 internal/openrouter 同构：请求体白名单透传，key 选择与出口解析由主包
// 注入，本包负责"给定 key + 出口"发一次请求 + 重试。上游限流语义：
// 429（Retry-After）/ 401（key 失效或已轮换）/ 8 并发每 key。

// HTTPError 类型化上游错误（携带状态码与响应体片段）。
type HTTPError struct {
	Status      int
	Body        string
	RateLimited bool
}

func (e *HTTPError) Error() string { return fmt.Sprintf("amd api %d: %s", e.Status, e.Body) }

// CallOpts 单次调用的可选参数。
type CallOpts struct {
	// PinKey 固定本次调用只用一个 key（面板 Test 探测）；空 = 正常轮转。
	PinKey string
}

// ExitResolver 出站解析回调（由主包注入）：给定 key 返回该次尝试的出口
// URL 与是否继续（隔离模式下绑定双不可用时 ok=false）。
type ExitResolver func(key string) (proxyURL string, ok bool)

var (
	exitResolverMu sync.RWMutex
	exitResolver   ExitResolver
	clientProvider func(proxyURL string) *http.Client

	// attemptObserver 请求日志观测钩子（主包注入）：仅观测，不参与路由。
	attemptObserverMu sync.RWMutex
	attemptObserver   func(ctx context.Context, scope, key, proxyURL string)

	// proxyObserver 出口成败观测钩子（afree 本地扩展，主包注入）：把代理出口
	// 的成败与握手耗时回传给网关的代理健康统计（在线率 / 平均握手延迟）。
	// 仅观测，不参与路由。re-vendor 同步上游代码时如被冲掉，需重新加回。
	proxyObserverMu sync.RWMutex
	proxyObserver   func(proxyURL string, ok bool, err error, hsMs int64)
)

// SetExitResolver 注入出站解析（主包装配期调用）。
func SetExitResolver(r ExitResolver) {
	exitResolverMu.Lock()
	exitResolver = r
	exitResolverMu.Unlock()
}

// SetClientProvider 注入按代理 URL 取 HTTP client 的工厂（复用主包的
// proxyClientFor 缓存，含代理冷却/健康统计钩子）。
func SetClientProvider(f func(proxyURL string) *http.Client) {
	clientProvider = f
}

// SetAttemptObserver 注入「命中 key + 出口」观测钩子（主包装配期调用）。
// Chat 每次选定 key 并解析出出口后回调一次（scope 恒为空串：单平台实现无子
// 平台概念），供网关填请求日志的「账号/Key」「代理」两列。key 为明文，由
// 注入方自行转序号与查绑定。仅观测，不参与任何路由决策。
func SetAttemptObserver(f func(ctx context.Context, scope, key, proxyURL string)) {
	attemptObserverMu.Lock()
	attemptObserver = f
	attemptObserverMu.Unlock()
}

// observeAttempt 回报一次 key 命中（无钩子时零成本跳过）。
func observeAttempt(ctx context.Context, key, proxyURL string) {
	attemptObserverMu.RLock()
	obs := attemptObserver
	attemptObserverMu.RUnlock()
	if obs != nil {
		obs(ctx, "", key, proxyURL)
	}
}

// SetProxyObserver 注入出口成败观测钩子（afree 本地扩展，主包装配期调用）。
// Chat 每次真实 Do 之后回调一次（成败都回调）；proxyURL 为空表示直连出口，
// 由实现方自行忽略。hsMs = TCP+TLS+CONNECT 握手耗时（毫秒）。
func SetProxyObserver(f func(proxyURL string, ok bool, err error, hsMs int64)) {
	proxyObserverMu.Lock()
	proxyObserver = f
	proxyObserverMu.Unlock()
}

// observeProxy 回报一次出口成败（无钩子时零成本跳过）。
func observeProxy(proxyURL string, ok bool, err error, hsMs int64) {
	proxyObserverMu.RLock()
	obs := proxyObserver
	proxyObserverMu.RUnlock()
	if obs != nil {
		obs(proxyURL, ok, err, hsMs)
	}
}

func resolveExit(key string) (string, bool) {
	exitResolverMu.RLock()
	r := exitResolver
	exitResolverMu.RUnlock()
	if r == nil {
		return "", true
	}
	return r(key)
}

func httpClientFor(proxyURL string) *http.Client {
	if clientProvider != nil {
		return clientProvider(proxyURL)
	}
	return http.DefaultClient
}

// chatBodyKeys 只保留 OpenAI 兼容白名单字段（上游文档声明的参数集）。
var chatBodyKeys = []string{
	"tools", "tool_choice", "parallel_tool_calls",
	"temperature", "top_p", "stop", "presence_penalty", "frequency_penalty",
	"response_format", "user", "n", "seed", "stream_options",
}

// BuildBody 构造 chat/completions 请求体：白名单字段透传 + model/messages/max_tokens，
// model 改写为目标模型 ID。reasoning_effort 映射为上游 reasoning 字段。
func BuildBody(params map[string]any, modelID string, stream bool) map[string]any {
	body := map[string]any{}
	for _, k := range chatBodyKeys {
		if v, ok := params[k]; ok {
			body[k] = v
		}
	}
	if v, ok := params["messages"]; ok {
		body["messages"] = v
	}
	if v, ok := params["max_tokens"]; ok {
		body["max_tokens"] = v
	}
	if v, ok := params["max_completion_tokens"]; ok {
		body["max_completion_tokens"] = v
	}
	body["model"] = modelID
	body["stream"] = stream
	if eff, ok := params["reasoning_effort"].(string); ok && eff != "" {
		body["reasoning"] = map[string]any{"effort": eff}
	} else if eff, ok := params["reasoningEffort"].(string); ok && eff != "" {
		body["reasoning"] = map[string]any{"effort": eff}
	}
	return body
}

// Chat 发一次 chat/completions 请求（内部按配置重试：限流换 key、网络错误退避）。
// 返回原始上游响应（流式 SSE 或聚合 JSON，由调用方处理）。
func Chat(ctx context.Context, params map[string]any, modelID string, stream bool, strategy string, boundRoutable func(string) bool) (*http.Response, int, error) {
	c := Get()
	bodyJSON, err := json.Marshal(BuildBody(params, modelID, stream))
	if err != nil {
		return nil, 0, fmt.Errorf("marshal body: %w", err)
	}
	endpoint := c.BaseURL + "/chat/completions"
	// 并发上限：拿到槽位才进入重试循环（槽位持有到本次调用返回，与 zen 同口径）
	if !acquireSem(ctx) {
		return nil, 0, fmt.Errorf("client aborted: %w", ctx.Err())
	}
	defer releaseSem()
	retries := c.Retries
	if retries <= 0 {
		retries = 3
	}
	delay := time.Second
	rateLimited := 0
	var pin string
	if v, ok := ctx.Value(pinKeyCtx{}).(string); ok {
		pin = v
	}
	for attempt := 0; ; attempt++ {
		var key string
		if pin != "" {
			key = pin
		} else {
			key = PickKey(strategy, boundRoutable)
		}
		if key == "" {
			return nil, rateLimited, fmt.Errorf("no amd key routable: all configured keys are cooling, routing-disabled or bound-exit-blocked")
		}
		proxyURL, ok := resolveExit(key)
		if !ok {
			if pin != "" {
				return nil, rateLimited, fmt.Errorf("key#%d bound proxies unavailable (cooling or removed); probe aborted by proxy isolation", KeyIndex(key))
			}
			Cooldown(key, 2*time.Minute)
			continue
		}
		observeAttempt(ctx, key, proxyURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, rateLimited, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		// hsStart 拨号前打点（与 cline/zen/workbuddy 同口径）：Do 返回即隧道
		// 建立（首字节未回），差值=TCP+TLS+CONNECT 握手耗时。此前本平台没有
		// 出口观测钩子，代理健康面板的在线率与握手延迟永远无样本。
		hsStart := time.Now()
		resp, err := httpClientFor(proxyURL).Do(req)
		observeProxy(proxyURL, err == nil, err, time.Since(hsStart).Milliseconds())
		if err != nil {
			if ctx.Err() != nil {
				return nil, rateLimited, fmt.Errorf("client aborted: %w", err)
			}
			if attempt < retries {
				if !sleepCtx(ctx, jitter(delay)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = nextDelay(delay)
				continue
			}
			return nil, rateLimited, fmt.Errorf("amd request: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			resp.Header.Set("X-Afree-Used-Key", key)
			return resp, rateLimited, nil
		}
		bodyBytes := readBody(resp)
		resp.Body.Close()
		apiErr := &HTTPError{Status: resp.StatusCode, Body: truncate(string(bodyBytes), 500)}
		if isRateLimited(resp.StatusCode, string(bodyBytes)) {
			rateLimited++
			apiErr.RateLimited = true
			raw := resp.Header.Get("Retry-After")
			_ = Cooldown(key, parseRetryAfter(raw))
			if pin != "" {
				return nil, rateLimited, apiErr
			}
			// 换一个未冷却的 key 立即重试（不睡眠）
			if next := PickKey(strategy, boundRoutable); next != "" && next != key {
				continue
			}
			if attempt < retries {
				wait := delay
				if ra := parseRetryAfter(raw); ra > wait {
					wait = ra
				}
				if wait > 30*time.Second {
					wait = 30 * time.Second
				}
				if !sleepCtx(ctx, jitter(wait)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = nextDelay(delay)
				continue
			}
			return nil, rateLimited, apiErr
		}
		// 401/403：key 失效/已轮换，冷却该 key 后换 key 重试
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			if pin != "" {
				return nil, rateLimited, apiErr
			}
			Cooldown(key, time.Hour)
			if next := PickKey(strategy, boundRoutable); next != "" && next != key {
				continue
			}
			return nil, rateLimited, apiErr
		}
		return nil, rateLimited, apiErr
	}
}

type pinKeyCtx struct{}

// WithPinKey 把固定 key 放进 ctx（面板 Test 用）。
func WithPinKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, pinKeyCtx{}, key)
}

func isRateLimited(status int, body string) bool {
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return true
	}
	if status == http.StatusBadGateway {
		low := strings.ToLower(body)
		for _, kw := range []string{"rate limit", "too many", "overloaded", "busy", "limit reached"} {
			if strings.Contains(low, kw) {
				return true
			}
		}
	}
	return false
}

func parseRetryAfter(header string) time.Duration {
	v := strings.TrimSpace(header)
	if v == "" {
		return time.Minute
	}
	var secs int64
	if _, err := fmt.Sscanf(v, "%d", &secs); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return time.Minute
}

func readBody(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return b
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// jitter 0~25% 抖动，避免并发请求同时重试。
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d + time.Duration(float64(d)*float64(randIntn(26))/100)
}

func nextDelay(d time.Duration) time.Duration {
	if d >= 8*time.Second {
		return 8 * time.Second
	}
	return d * 2
}

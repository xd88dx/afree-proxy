package tokenharbor

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
// 与 internal/amd 同构：请求体白名单透传，key 选择与出口解析由主包注入。
// 上游限流：免费账号 60 req/min（账号）、100 req/min（IP），429 带
// Retry-After；无并发限制。401/403 = key 失效或已轮换。

// HTTPError 类型化上游错误（携带状态码与响应体片段）。
type HTTPError struct {
	Status      int
	Body        string
	RateLimited bool
}

func (e *HTTPError) Error() string { return fmt.Sprintf("tokenharbor api %d: %s", e.Status, e.Body) }

// ExitResolver 出站解析回调（由主包注入）：给定 key 返回该次尝试的出口
// URL 与是否继续（隔离模式下绑定双不可用时 ok=false）。
type ExitResolver func(key string) (proxyURL string, ok bool)

var (
	exitResolverMu sync.RWMutex
	exitResolver   ExitResolver
	clientProvider func(proxyURL string) *http.Client
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

// chatBodyKeys 只保留 OpenAI 兼容白名单字段。
var chatBodyKeys = []string{
	"tools", "tool_choice", "parallel_tool_calls",
	"temperature", "top_p", "stop", "presence_penalty", "frequency_penalty",
	"response_format", "user", "n", "seed", "stream_options", "metadata",
}

// BuildBody 构造 chat/completions 请求体：白名单字段透传 + model/messages/max_tokens，
// model 改写为目标模型 ID。reasoning_effort 映射为上游 reasoning 字段。
// 直连模型调用上游不加不改（byte-for-byte 透传语义），此处只做字段收敛。
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
			return nil, rateLimited, fmt.Errorf("no tokenharbor key routable: all configured keys are cooling, routing-disabled or bound-exit-blocked")
		}
		proxyURL, ok := resolveExit(key)
		if !ok {
			if pin != "" {
				return nil, rateLimited, fmt.Errorf("key#%d bound proxies unavailable (cooling or removed); probe aborted by proxy isolation", KeyIndex(key))
			}
			Cooldown(key, 2*time.Minute)
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, rateLimited, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClientFor(proxyURL).Do(req)
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
			return nil, rateLimited, fmt.Errorf("tokenharbor request: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			MarkSuccess(key)
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

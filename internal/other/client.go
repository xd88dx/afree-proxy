package other

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// ============ 上游调用 ============
//
// 与 internal/amd 同构：请求体白名单透传，key 选择在平台池内轮转，出口
// （直连/代理池/绑定主辅）由主包每次调用通过 exitFn 注入（闭包捕获平台，
// 避免多平台下 key 不唯一带来的全局解析歧义）。上游限流语义：
// 429（Retry-After）/ 401（key 失效或已轮换）。

// HTTPError 类型化上游错误（携带状态码与响应体片段）。
type HTTPError struct {
	Status      int
	Body        string
	RateLimited bool
}

func (e *HTTPError) Error() string { return fmt.Sprintf("other api %d: %s", e.Status, e.Body) }

type pinKeyCtx struct{}

// WithPinKey 把固定 key 放进 ctx（面板 Test 用）。
func WithPinKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, pinKeyCtx{}, key)
}

// chatBodyKeys 只保留 OpenAI 兼容白名单字段（上游文档声明的参数集）。
var chatBodyKeys = []string{
	"tools", "tool_choice", "parallel_tool_calls",
	"temperature", "top_p", "stop", "presence_penalty", "frequency_penalty",
	"response_format", "user", "n", "seed", "stream_options",
}

// BuildBody 构造 chat/completions 请求体：白名单字段透传 + model/messages/
// max_tokens，model 改写为目标模型 ID。reasoning_effort 映射为上游
// reasoning 字段（与 amd 同口径）。
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

// clientProvider 按代理 URL 取 HTTP client 的工厂（由主包注入，复用主包的
// proxyClientFor 缓存，含代理冷却/健康统计钩子）。
var clientProvider func(proxyURL string) *http.Client

// SetClientProvider 注入 HTTP client 工厂（主包装配期调用）。
func SetClientProvider(f func(proxyURL string) *http.Client) {
	clientProvider = f
}

func httpClientFor(proxyURL string) *http.Client {
	if clientProvider != nil {
		return clientProvider(proxyURL)
	}
	return http.DefaultClient
}

// Chat 发一次 chat/completions 请求（内部按配置重试：限流换 key、网络错误
// 退避）。exitFn 给定 key 返回该次尝试的出口 URL 与是否继续（隔离模式下
// 绑定双不可用时 ok=false）；nil = 直连。boundRoutable 同 amd 语义。
func Chat(ctx context.Context, p *Platform, params map[string]any, modelID string, stream bool, strategy string, boundRoutable func(string) bool, exitFn func(string) (string, bool)) (*http.Response, int, error) {
	bodyJSON, err := json.Marshal(BuildBody(params, modelID, stream))
	if err != nil {
		return nil, 0, fmt.Errorf("marshal body: %w", err)
	}
	endpoint := p.BaseURL + "/chat/completions"
	// 并发上限：拿到槽位才进入重试循环（槽位持有到本次调用返回，与 amd 同口径）
	if !acquireSem(ctx, p.Slug, p.MaxConcurrency) {
		return nil, 0, fmt.Errorf("client aborted: %w", ctx.Err())
	}
	defer releaseSem(p.Slug, p.MaxConcurrency)
	retries := p.Retries
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
			key = PickKey(p, strategy, boundRoutable)
		}
		if key == "" {
			return nil, rateLimited, fmt.Errorf("no key routable on platform %q: all configured keys are cooling, routing-disabled or bound-exit-blocked", p.Name)
		}
		proxyURL := ""
		if exitFn != nil {
			url, ok := exitFn(key)
			if !ok {
				if pin != "" {
					return nil, rateLimited, fmt.Errorf("key#%d bound proxies unavailable (cooling or removed); probe aborted by proxy isolation", KeyIndex(p, key))
				}
				Cooldown(p, key, 2*time.Minute)
				continue
			}
			proxyURL = url
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
			return nil, rateLimited, fmt.Errorf("other request: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			MarkSuccess(p, key)
			return resp, rateLimited, nil
		}
		bodyBytes := readBody(resp)
		resp.Body.Close()
		apiErr := &HTTPError{Status: resp.StatusCode, Body: truncate(string(bodyBytes), 500)}
		if isRateLimited(resp.StatusCode, string(bodyBytes)) {
			rateLimited++
			apiErr.RateLimited = true
			raw := resp.Header.Get("Retry-After")
			_ = Cooldown(p, key, parseRetryAfter(raw))
			if pin != "" {
				return nil, rateLimited, apiErr
			}
			// 换一个未冷却的 key 立即重试（不睡眠）
			if next := PickKey(p, strategy, boundRoutable); next != "" && next != key {
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
			Cooldown(p, key, time.Hour)
			if next := PickKey(p, strategy, boundRoutable); next != "" && next != key {
				continue
			}
			return nil, rateLimited, apiErr
		}
		return nil, rateLimited, apiErr
	}
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
	return d + time.Duration(float64(d)*rand.Float64()*0.25)
}

func nextDelay(d time.Duration) time.Duration {
	if d >= 8*time.Second {
		return 8 * time.Second
	}
	return d * 2
}

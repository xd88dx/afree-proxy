package freepi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	mathrand "math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 上游调用（含三道闸门处理）============
//
// 请求体白名单透传与轮转骨架与 internal/other 同构（exitFn 注入出口、
// 限流换 key、网络错误退避）。FreePI 特有：每次上游尝试前打广告曝光对
// （CLI 行为），并按闸门自愈——
//
//	ads_required (403)：账号必须先看完一次广告曝光。重打曝光对后重试一次。
//	concurrent_session (409/429)：四会话槽占满（30 分钟自动释放）。
//		POST /session/reset 后按 250/600/1200/2400ms（±25% 抖动）退避重试。
//	concurrent (429)：单账号同时一个请求。走既有限流路径（短冷却+换 key），
//		选号侧另有在途排他（pool.go）避免无谓 429。
//
// 闸门语义依据 pingmike2/freepi-magpie-plugin（0.3.16 实测注释）。

// HTTPError 类型化上游错误（携带状态码与响应体片段）。
type HTTPError struct {
	Status      int
	Body        string
	RateLimited bool
	// RetryAfter 上游 Retry-After 头的解析结果（0 = 未给，外层用缺省）。
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("freepi api %d: %s", e.Status, e.Body) }

// HTTPStatus 让网关统一提取上游状态码（429 需原样透传给客户端）。
func (e *HTTPError) HTTPStatus() int { return e.Status }

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
// reasoning 字段（与 key 池平台同口径）。
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

// clientProvider 按代理 URL 取 HTTP client 的工厂（由主包注入：Chrome
// 指纹栈 + 出口冷却/健康统计钩子，other 同款口径）。
var clientProvider func(proxyURL string) *http.Client

// attemptObserver 请求日志观测钩子（主包注入）：仅观测，不参与路由。
var (
	attemptObserverMu sync.RWMutex
	attemptObserver   func(ctx context.Context, key, proxyURL string)
)

// proxyObserver 出口成败观测钩子（主包注入）：把代理出口的成败与握手耗时
// 回传给网关的代理健康统计（在线率/平均握手延迟）。仅观测，不参与路由。
var (
	proxyObserverMu sync.RWMutex
	proxyObserver   func(proxyURL string, ok bool, err error, hsMs int64)
)

// SetClientProvider 注入 HTTP client 工厂（主包装配期调用）。
func SetClientProvider(f func(proxyURL string) *http.Client) { clientProvider = f }

// SetAttemptObserver 注入「命中 key + 出口」观测钩子（主包装配期调用）。
// Chat 每次选定 key 并解析出出口后回调一次，供网关填请求日志的
// 「账号/Key」「代理」两列。key 为明文，由注入方自行转序号与查绑定。
func SetAttemptObserver(f func(ctx context.Context, key, proxyURL string)) {
	attemptObserverMu.Lock()
	attemptObserver = f
	attemptObserverMu.Unlock()
}

// SetProxyObserver 注入出口成败观测钩子（主包装配期调用）。
func SetProxyObserver(f func(proxyURL string, ok bool, err error, hsMs int64)) {
	proxyObserverMu.Lock()
	proxyObserver = f
	proxyObserverMu.Unlock()
}

// observeAttempt 回报一次 key 命中（无钩子时零成本跳过）。
func observeAttempt(ctx context.Context, key, proxyURL string) {
	attemptObserverMu.RLock()
	obs := attemptObserver
	attemptObserverMu.RUnlock()
	if obs != nil {
		obs(ctx, key, proxyURL)
	}
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

func httpClientFor(proxyURL string) *http.Client {
	if clientProvider != nil {
		return clientProvider(proxyURL)
	}
	return http.DefaultClient
}

// streamIdleTimeout 上游响应体流中空闲上限（kit.MonitorBody，300s 同 other；
// 响应头阶段由 client 工厂的 ResponseHeaderTimeout 管，两者不重叠）。
var streamIdleTimeout = 300 * time.Second

// uuid4 铸 RFC 4122 v4 随机 UUID（session/turn 标识，对齐 plugin 的
// crypto.randomUUID）。
func uuid4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%036x", mathrand.Int63())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// fingerprintHeaders 广告请求的 CLI 指纹头（plugin 原样：os/arch 固定
// linux/x64——真 CLI 即 linux 容器形态，locale/timezone 取宿主）。
// 只有 /ads/next 携带它们。
func fingerprintHeaders(extra map[string]string) map[string]string {
	tz, locale := "UTC", "en-US"
	if zone, _ := time.Now().Zone(); zone != "" {
		tz = zone
	}
	h := map[string]string{
		"x-client-os":       "linux",
		"x-client-arch":     "x64",
		"x-client-locale":   locale,
		"x-client-timezone": tz,
	}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

// warmAds 打一次广告曝光对（CLI 每次前台 chat 前的 banner 广告，保持账号
// 广告窗口活着）：GET /ads/next → GET 曝光像素（trygravity）→ POST
// /ads/impression。全程 best-effort：闸门的 403 会点名 ads_required，请求
// 路径据此自愈。曝光对与 chat 共享 session/turn id——闸门按 turn id 对账
// （plugin 0.6.19 实测注释）。
func warmAds(client *http.Client, token, sessionID, turnID string) {
	defer func() { _ = recover() }() // 纯预热：任何异常不影响主请求
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIOrigin+"/ads/next?slot=banner", nil)
	if err != nil {
		return
	}
	for k, v := range fingerprintHeaders(map[string]string{
		"authorization": "Bearer " + token,
		"x-session-id":  sessionID,
		"x-turn-id":     turnID,
	}) {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	var ad struct {
		AdID       string `json:"ad_id"`
		ClickToken string `json:"click_token"`
		ImpURL     string `json:"imp_url"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&ad)
	resp.Body.Close()
	if err != nil || ad.AdID == "" || ad.ClickToken == "" {
		return
	}
	// CLI 在 POST 前先打 gravity 曝光像素；0.6.19 闸门校验会话确实加载了广告
	if ad.ImpURL != "" && strings.Contains(ad.ImpURL, "trygravity.") {
		preq, perr := http.NewRequestWithContext(ctx, http.MethodGet, ad.ImpURL, nil)
		if perr == nil {
			if pres, perr := client.Do(preq); perr == nil {
				pres.Body.Close()
			}
		}
	}
	body, _ := json.Marshal(map[string]any{
		"ad_id": ad.AdID, "click_token": ad.ClickToken, "imp_fired": true,
	})
	ireq, err := http.NewRequestWithContext(ctx, http.MethodPost, APIOrigin+"/ads/impression", bytes.NewReader(body))
	if err != nil {
		return
	}
	ireq.Header.Set("Authorization", "Bearer "+token)
	ireq.Header.Set("Content-Type", "application/json")
	ireq.Header.Set("x-session-id", sessionID)
	if iresp, err := client.Do(ireq); err == nil {
		iresp.Body.Close()
	}
}

// resetSessions 重置账号的会话槽（concurrent_session 闸门的解法；槽 30 分钟
// 也会自动释放）。best-effort。
func resetSessions(client *http.Client, token string) {
	defer func() { _ = recover() }()
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIOrigin+"/session/reset", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}

// MeInfo /me 的余额视图（GET {APIOrigin}/me，免费，不打对话额度）。
type MeInfo struct {
	Handle    string
	Remaining int64
	Allowance int64
	RefillAt  string
}

// Me 查询账号信息与今日 credits（Test 验证与刷新额度共用）。token 无效时
// 返回类型化 HTTPError（401）。
func Me(ctx context.Context, client *http.Client, token string) (*MeInfo, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIOrigin+"/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{Status: resp.StatusCode, Body: truncate(string(body), 300)}
	}
	var payload struct {
		Handle                string `json:"handle"`
		RemainingCreditsToday int64  `json:"remaining_credits_today"`
		AllowanceCredits      int64  `json:"allowance_credits"`
		RefillAt              string `json:"refill_at"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("freepi /me decode: %w", err)
	}
	return &MeInfo{
		Handle:    payload.Handle,
		Remaining: payload.RemainingCreditsToday,
		Allowance: payload.AllowanceCredits,
		RefillAt:  payload.RefillAt,
	}, nil
}

// resetWaits 会话闸重试间隔（对齐 plugin RESET_WAITS，总预算约 9s；超出
// 后闸门错误按原样交回外层冷却换 key）。
var resetWaits = []time.Duration{
	250 * time.Millisecond,
	600 * time.Millisecond,
	1200 * time.Millisecond,
	2400 * time.Millisecond,
}

// gateCooldown 闸门重试穷尽后的 key 冷却：会话槽 30 分钟自动释放、ads 窗口
// "停一会儿"即自愈——10 分钟冷却足以等到自愈窗口，不长冷却误伤可用凭据。
const gateCooldown = 10 * time.Minute

// isSessionGate 判定拒绝是否为四会话槽闸（409/429 + concurrent_session；
// 不带 session 的 concurrent 是真排队，reset 无用）。
func isSessionGate(status int, text string) bool {
	if status != http.StatusConflict && status != http.StatusTooManyRequests {
		return false
	}
	return strings.Contains(text, "concurrent_session")
}

// chatAttempt sendChat 的结果包：200 时 resp 非 nil（body 未读，监控由
// attemptWithGates 挂）；非 200 时 status/body/retryIn 携带裁决材料。
type chatAttempt struct {
	resp    *http.Response
	status  int
	body    []byte
	retryIn time.Duration
	err     error
}

// sendChat 发一次 chat 请求（闸门自愈的最小单元）。请求头对齐 free-pi CLI：
// Bearer / x-session-id / x-client-version / x-turn-id（曝光对与 chat 共享
// 同一组 id——闸门按 turn id 对账）。
func sendChat(ctx context.Context, client *http.Client, endpoint string, bodyJSON []byte, key, sessionID, turnID string) chatAttempt {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyJSON))
	if err != nil {
		return chatAttempt{err: fmt.Errorf("create request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-session-id", sessionID)
	req.Header.Set("x-client-version", ClientVersion)
	req.Header.Set("x-turn-id", turnID)
	resp, err := client.Do(req)
	if err != nil {
		return chatAttempt{err: err}
	}
	if resp.StatusCode == http.StatusOK {
		return chatAttempt{resp: resp}
	}
	body := readBody(resp)
	resp.Body.Close()
	return chatAttempt{status: resp.StatusCode, body: body, retryIn: parseRetryAfter(resp.Header.Get("Retry-After"))}
}

// attemptWithGates 单次 key 内的闸门感知尝试链：铸 session/turn → 曝光对 →
// 发请求 → 按闸门自愈（ads_required 重打曝光重试一次；concurrent_session
// reset 后按退避链重试，每轮新 session/turn + 新曝光对）。返回 200 响应
// （body 已挂流中空闲监控，attemptCancel 所有权随 body 转移给调用方，
// Close 即取消）或错误：网络/取消原样上抛，闸门穷尽后返回携带状态码与
// 体片段的 *HTTPError（外层据此冷却换 key）。
func attemptWithGates(ctx context.Context, endpoint string, bodyJSON []byte, key, proxyURL string) (*http.Response, error) {
	client := httpClientFor(proxyURL)
	attemptCtx, attemptCancel := context.WithCancel(ctx)
	sessionID, turnID := uuid4(), uuid4()
	warmAds(client, key, sessionID, turnID)
	adsLeft := 1
	sessTry := 0
	for {
		at := sendChat(attemptCtx, client, endpoint, bodyJSON, key, sessionID, turnID)
		if at.err != nil {
			attemptCancel()
			return nil, at.err
		}
		if at.resp != nil {
			at.resp.Body = kit.MonitorBody(at.resp.Body, streamIdleTimeout, attemptCancel)
			return at.resp, nil
		}
		text := string(at.body)
		if at.status == http.StatusForbidden && strings.Contains(text, "ads_required") && adsLeft > 0 {
			adsLeft--
			// 曝光对与 chat 共享 session/turn：同 id 重打一次再试
			warmAds(client, key, sessionID, turnID)
			continue
		}
		if isSessionGate(at.status, text) && sessTry < len(resetWaits) {
			resetSessions(client, key)
			if !sleepCtx(attemptCtx, jitter(resetWaits[sessTry])) {
				attemptCancel()
				return nil, fmt.Errorf("client aborted during gate retry wait")
			}
			sessTry++
			sessionID, turnID = uuid4(), uuid4()
			warmAds(client, key, sessionID, turnID)
			continue
		}
		attemptCancel()
		return nil, &HTTPError{Status: at.status, Body: truncate(text, 500), RetryAfter: at.retryIn}
	}
}

// Chat 发一次 chat/completions 请求（内部按配置重试：限流换 key、网络错误
// 退避、闸门自愈在 attemptWithGates 内）。exitFn 给定 key 返回该次尝试的
// 出口 URL 与是否继续（隔离模式下绑定双不可用时 ok=false）。boundRoutable
// 同 key 池平台语义。请求结束后 key 的在途标记经 defer 释放（concurrent
// 闸门对策，pool.go）。
func Chat(ctx context.Context, params map[string]any, modelID string, stream bool, strategy string, boundRoutable func(string) bool, exitFn func(string) (string, bool)) (*http.Response, int, error) {
	bodyJSON, err := json.Marshal(BuildBody(params, modelID, stream))
	if err != nil {
		return nil, 0, fmt.Errorf("marshal body: %w", err)
	}
	endpoint := APIBase + "/chat/completions"
	// 并发上限：拿到槽位才进入重试循环（槽位持有到本次调用返回，other 同口径）
	sem, ok := acquireSem(ctx, Get().MaxConcurrency)
	if !ok {
		return nil, 0, fmt.Errorf("client aborted: %w", ctx.Err())
	}
	defer releaseSem(sem)
	retries := Get().Retries
	if retries <= 0 {
		retries = 3
	}
	delay := time.Second
	rateLimited := 0
	var pin string
	if v, ok := ctx.Value(pinKeyCtx{}).(string); ok {
		pin = v
	}
	held := ""
	defer func() {
		if held != "" {
			ReleaseKey(held)
		}
	}()

	// 会话粘性（2026-11-25）：同一对话多轮锁定同一把 key，避免"同 messages
	// 换 Bearer"被上游判为账号共享/轮转。pin（面板 Test 固定 key）不走粘性。
	cfg := Get()
	stickyOn := cfg.StickyEnabled() && pin == ""
	convKey := ""
	if stickyOn {
		convKey = ConversationKey(ctx, params, "")
	}
	stuckKey := ""
	if convKey != "" {
		valid := map[string]bool{}
		for _, k := range cfg.Keys {
			if cfg.RoutingEnabled(k) {
				valid[k] = true
			}
		}
		stuckKey = stickyLookup(convKey, valid)
		// 粘住的 key 正在冷却：本次立即失败，不打上游也不换 key——换 key
		// 只会再撞上游风控，白费一次调用（2026-11-25 用户定）。
		if stuckKey != "" {
			if until, cooling := CooldownUntil(stuckKey); cooling && time.Now().Before(until) {
				return nil, rateLimited, fmt.Errorf("freepi sticky session key#%d cooling until %s; fail fast without switching key", KeyIndex(stuckKey), until.Format(time.RFC3339))
			}
		}
	}
	stickyLocked := stuckKey != ""

	for attempt := 0; ; attempt++ {
		var key string
		switch {
		case stickyLocked:
			key = stuckKey
		case pin != "":
			key = pin
		default:
			key = PickKey(strategy, boundRoutable)
		}
		if key == "" {
			return nil, rateLimited, fmt.Errorf("no freepi key routable: all configured keys are cooling, routing-disabled or bound-exit-blocked")
		}
		if key != held {
			if held != "" {
				ReleaseKey(held)
			}
			AcquireKey(key)
			held = key
		}
		proxyURL := ""
		if exitFn != nil {
			url, ok := exitFn(key)
			if !ok {
				// sticky 与 pin 同语义：不换 key、立即失败。
				if pin != "" || stickyLocked {
					return nil, rateLimited, fmt.Errorf("%s", kit.BoundProbeBlockedMsg(ModelPrefix, key,
						fmt.Sprintf("key#%d bound proxies unavailable (cooling or removed); probe aborted by proxy isolation", KeyIndex(key))))
				}
				Cooldown(key, 2*time.Minute)
				continue
			}
			proxyURL = url
		}
		observeAttempt(ctx, key, proxyURL)
		resp, gerr := attemptWithGates(ctx, endpoint, bodyJSON, key, proxyURL)
		if gerr == nil {
			resp.Header.Set("X-Afree-Used-Key", key)
			if stickyOn && convKey != "" {
				stickyBind(convKey, key) // 首轮绑定 / 后续刷新 LastSeen
			}
			return resp, rateLimited, nil
		}
		apiErr, isAPI := gerr.(*HTTPError)
		if !isAPI {
			// 网络错误/取消：sticky 会话快速失败，不等待、不换 key。
			if ctx.Err() != nil {
				return nil, rateLimited, fmt.Errorf("client aborted: %w", gerr)
			}
			if stickyLocked {
				return nil, rateLimited, gerr
			}
			// 非 sticky：交回外层退避矩阵（other 同款）
			if attempt < retries {
				if !sleepCtx(ctx, jitter(delay)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = nextDelay(delay)
				continue
			}
			return nil, rateLimited, gerr
		}
		text := apiErr.Body
		switch {
		// ads_required / concurrent_session：闸门预算在 attemptWithGates 内已
		// 穷尽——短冷却（自愈窗口内）后换 key，不长冷却误伤可用凭据。
		case (apiErr.Status == http.StatusForbidden && strings.Contains(text, "ads_required")) ||
			strings.Contains(text, "concurrent_session"):
			if pin != "" {
				return nil, rateLimited, apiErr
			}
			Cooldown(key, gateCooldown)
			if stickyLocked {
				return nil, rateLimited, apiErr // 冷却让其他会话避开，本会话不换 key
			}
			if next := PickKey(strategy, boundRoutable); next != "" && next != key {
				continue
			}
			if attempt < retries {
				if !sleepCtx(ctx, jitter(delay)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = nextDelay(delay)
				continue
			}
			return nil, rateLimited, apiErr
		}
		// 限流（含 concurrent 排队与 5xx 限流形态）：短冷却换 key 立即重试
		if isRateLimited(apiErr.Status, text) {
			rateLimited++
			apiErr.RateLimited = true
			d := apiErr.RetryAfter
			if d <= 0 {
				d = time.Minute
			}
			_ = Cooldown(key, d)
			if pin != "" || stickyLocked {
				return nil, rateLimited, apiErr
			}
			// 换一个未冷却的 key 立即重试（不睡眠）
			if next := PickKey(strategy, boundRoutable); next != "" && next != key {
				continue
			}
			if attempt < retries {
				wait := d
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
		// 401/403（非 ads 形态）：key 失效/已轮换，冷却该 key 后换 key 重试
		if apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden {
			if pin != "" {
				return nil, rateLimited, apiErr
			}
			Cooldown(key, time.Hour)
			if stickyLocked {
				return nil, rateLimited, apiErr
			}
			if next := PickKey(strategy, boundRoutable); next != "" && next != key {
				continue
			}
			return nil, rateLimited, apiErr
		}
		return nil, rateLimited, apiErr
	}
}

// isRateLimited 限流判定（other 同款：429/503 直接成立；502 按关键词）。
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
	return d + time.Duration(float64(d)*mathrand.Float64()*0.25)
}

func nextDelay(d time.Duration) time.Duration {
	if d >= 8*time.Second {
		return 8 * time.Second
	}
	return d * 2
}

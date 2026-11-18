package cline

// cline 上游调用域（2026-11-18 gateway 批前置补完，自 app/proxy.go 迁入）：
// 上游请求构造（BuildUpstreamBody）、CLI 指纹头族、跨账号 failover 调用
//（CallAPI）、usage 记账回调与 429 退避解析。原 Host.CallAPI/NormalizeModel
// 钩子随迁退役——上游调用与模型规范化已是包内能力。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/textproto"
	"strings"
	"sync/atomic"
	"time"

	"afree-proxy/internal/blacklist"
	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/reqlog"
)

// proxyPlatformCooldown 平台级出口冷却时长（2026-10-30 用户定 10 分钟）。
const proxyPlatformCooldown = 10 * time.Minute

// accountProxyBinding 账号绑定出口读取（隔离模式）。
func accountProxyBinding(acc *Account) (main, backup string, bound bool) {
	if acc == nil || !exitpool.IsolationEnabled() {
		return "", "", false
	}
	PoolMu.Lock()
	main, backup = acc.ProxyMain, acc.ProxyBackup
	PoolMu.Unlock()
	if main == "" && backup == "" {
		return "", "", false
	}
	return main, backup, true
}

// AccountProxyBinding 账号绑定出口读取（导出：admin 探测路径消费）。
func AccountProxyBinding(acc *Account) (main, backup string, bound bool) {
	return accountProxyBinding(acc)
}

const (
	DefaultMaxTokens       = 128000
	DefaultReasoningEffort = "high"
)

var PassThroughKeys = []string{
	"tools", "tool_choice", "parallel_tool_calls", "functions", "function_call",
	"temperature", "top_p", "top_k", "stop", "presence_penalty", "frequency_penalty",
	"response_format", "user", "n", "logit_bias", "seed", "logprobs", "top_logprobs",
	"stream_options", "metadata",
}

// liveActiveAccountCount /health 的实时活跃账号数（启动时算一次会让

func BuildUpstreamBody(params map[string]any, stream bool) map[string]any {
	sessionID := "sess_" + kit.RandHex(8)

	maxTokens := DefaultMaxTokens
	// max_tokens 可能是 float64（客户端 JSON 解码）或 int（anthropic/responses
	// 内部转换路径写入）—— 两种都要接受，否则转换路径永远回退到 128000
	if mt := kit.AsInt(params["max_tokens"]); mt > 0 {
		maxTokens = mt
	} else if mt := kit.AsInt(params["max_completion_tokens"]); mt > 0 {
		maxTokens = mt
	}

	model := NormalizeRequestModel(params["model"].(string))

	body := map[string]any{
		"model":            model,
		"max_tokens":       maxTokens,
		"session_id":       sessionID,
		"reasoning_effort": DefaultReasoningEffort,
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

	for _, key := range PassThroughKeys {
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
	ClineClientVersion = "3.0.73"
	ClineCoreVersion   = "0.0.95"
	// clineUASuffix 官方 CLI 的 UA 后缀：ai-sdk provider 层在应用版本号之后
	// 追加 provider / provider-utils / runtime 三段。真机实测（同上抓包）：
	//   Cline/3.0.73 ai-sdk/openai-compatible/3.0.58 ai-sdk/provider-utils/5.0.50 runtime/bun/1.4.2
	// 只发 "Cline/<ver>" 会少掉整段后缀——这是与真实客户端最大的结构性偏差。
	clineUASuffix = " ai-sdk/openai-compatible/3.0.58 ai-sdk/provider-utils/5.0.50 runtime/bun/1.4.2"
)

// ClineUserAgent 官方 CLI 的完整 User-Agent。
func ClineUserAgent() string { return "Cline/" + ClineClientVersion + clineUASuffix }

// ClineTaskID 把任意会话标识规范成官方 CLI 的 X-Task-ID 形态
// <13 位毫秒>_<5 位 base36（真机抓包样本：1792426668112_96vx7）。
// 真实客户端在任务创建时生成该值、任务内恒定；网关按会话 ID 派生，
// 保证同一会话（含重试）稳定、不同会话不同。已是官方形态则原样透传。
func ClineTaskID(sessionID string) string {
	if IsClineTaskID(sessionID) {
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

// IsClineTaskID 判断是否已是官方形态：13 位数字 + '_' + 5 位小写字母数字。
func IsClineTaskID(s string) bool {
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

// customHeaders 面板自定义请求头覆盖（cline-config.json headers 段，
// SetHeaders 写入 / LoadPool 回放）。
func customHeaders() map[string]string {
	p := LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()
	return p.Headers
}

// ClineHeaders 构造上游请求头。用 setRawHeader（而非 Header.Set）保留官方
// 客户端的精确大小写（Set 会把键规范化成 X-Task-Id / Http-Referer 等），
// 字节级对齐客户端指纹；同时删除同名的其它大小写变体，避免 map 里同时存在
// "X-CLIENT-VERSION" 与 "X-Client-Version" 两个键、上线发出重复头。
// cfg.Headers 仍可覆盖（管理员自定义优先）。
func ClineHeaders(token, sessionID string) http.Header {
	h := http.Header{}
	setRawHeader(h, "Authorization", "Bearer "+token)
	setRawHeader(h, "Content-Type", "application/json")
	// Go 默认 UA（Go-http-client/1.1）是指纹异常点，容易被上游风控拦截；
	// 与管理面板探测请求保持一致的客户端标识。cfg.Headers 里可覆盖。
	setRawHeader(h, "User-Agent", ClineUserAgent())
	setRawHeader(h, "HTTP-Referer", "https://cline.bot")
	setRawHeader(h, "X-Title", "Cline")
	setRawHeader(h, "X-IS-MULTIROOT", "false")
	setRawHeader(h, "X-CLIENT-TYPE", "cline-cli")
	setRawHeader(h, "X-CLIENT-VERSION", ClineClientVersion)
	setRawHeader(h, "X-PLATFORM", "cli")
	setRawHeader(h, "X-PLATFORM-VERSION", ClineClientVersion)
	setRawHeader(h, "X-CORE-VERSION", ClineCoreVersion)
	if sessionID != "" {
		setRawHeader(h, "X-Task-ID", ClineTaskID(sessionID))
	}

	for k, v := range customHeaders() {
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

// APIBase cline 上游基址。包级变量便于测试把真实调用链指向本地假上游
// （ClineAPIBase 是常量、不可注入）；生产恒为 ClineAPIBase。
var APIBase = ClineAPIBase

// CallAPI 调用 cline 上游，带跨账号 failover。
// ctx 来自客户端请求: IDE abort/取消时立即终止,不冷却账号。
// 账号有绑定出口（隔离模式）时永远只从绑定出口发出,绑定优先于 useProxies;
// 未绑定账号在 useProxies 为 true 时走共享出口代理池（每次尝试轮转挑选;
// cline 不接上游的 egress 自动钉定——账号级绑定走本地 ProxyMain/ProxyBackup
// 体系与面板自动分配,见 egress.go 整合注释）
// 代理路径上的网络错误只冷却代理本身,绝不冷却账号 —— 代理故障不污染账号池。
//
// failover（P2 修复）：429/5xx 时换下一个账号重试（至多 clineFailoverAttempts
// 个账号）。此前只 pick 一次账号，一个账号撞上 429 即整请求 500，池子里其它
// 健康账号救不了本次请求——N 个账号的可用性退化成 1。429 会先把该账号冷却，
// 因此换到的必然是别的账号；5xx 不冷却（可能是上游瞬时故障），但也换账号试。
// 只在尚未向客户端流出任何内容时发生（本函数在流式转发之前），因此重试安全。
func CallAPI(ctx context.Context, params map[string]any, stream bool, useProxies bool) (*http.Response, *Account, error) {
	var lastErr error
	var lastAcc *Account
	tried := map[string]bool{}
	for i := 0; i < clineFailoverAttempts; i++ {
		acc := PickAccountExcluding(tried)
		if acc == nil {
			if lastErr != nil {
				// 所有可用账号都试过了：返回最后一次的真实错误（比"无账号"更有信息量）
				return nil, lastAcc, lastErr
			}
			// 绑定策略下"没有可用账号"通常是代理不够：明确说出来，
			// 否则用户只看到 "no active accounts" 而池子里其实有 11 个。
			// cline 不接 egress 自动钉定表（zen 专属），2026-10-29 移除按该表
			// 误诊"代理没分够"的分支。
			return nil, nil, fmt.Errorf("no cline account routable: all configured accounts are cooling, routing-disabled or bound-exit-blocked (%s)", DescribePoolStatus())
		}
		tried[acc.AccountID] = true
		lastAcc = acc

		resp, err := callAPIWithAccount(ctx, params, stream, useProxies, acc)
		if err == nil {
			// 注意：这里**不能**记 success。上游常常先回 200 再在 SSE 帧里
			// 报错（provider 挂掉时就是这样），此时 API 层是"成功"的，真正的
			// 失败要等调用方聚合流才知道。在这里清零会让 failStreak 永远
			// 在 1↔2 之间抖动、达不到阈值（实测踩过）。
			// 成功的清零放在真正消费完流之后，见 markModelServed。
			return resp, acc, nil
		}
		lastErr = err
		var ce *APIError
		if errors.As(err, &ce) {
			// 模型级错误（404 model not found / 500 inference failed /
			// stream_initialization_failed）与账号无关：换账号救不回来，但反复
			// 出现说明这个免费模型确实坏了 —— 记一次，累计到阈值就下架冷却。
			// 429 / 鉴权类不算模型故障，isModelLevelFailure 会滤掉。
			if id, _ := params["model"].(string); id != "" {
				RecordModelFailure(id, ce.Status, ce.Body)
			}
		}
		if !errors.As(err, &ce) {
			// 网络/令牌/取消等错误：不换账号（换账号无助于网络层故障，且
			// 取消必须立即终止）。直接返回。
			return nil, acc, err
		}
		// 只对 429 / 5xx 换账号；4xx（400/401/403 等）重试无意义，原样返回。
		// 例外（2026-11-05 生产实证）：403 呈 Google GFE HTML 拒绝页指纹 =
		// 出口 IP 被上游前置拒收，非账号/key 问题（同一出口稍后即自愈：
		// 19:47 被 GFE 403、20:34 同出口成功），换账号（隔离模式=换绑定出口）
		// 可救活本次请求。
		if ce.Status != http.StatusTooManyRequests && ce.Status < 500 {
			if ce.Status != http.StatusForbidden || !clineGFEProxyForbidden(ce.Body) {
				return nil, acc, err
			}
		}
		// "500 empty response content" 是模型属性（该模型必须流式），不是账号
		// 故障：换账号徒劳，且会拖慢 callClineAutoStream 的"学习+改流式重试"。
		// 原样返回，让上层去学。
		if EmptyStreamErr(ce.Status, []byte(ce.Body)) {
			return nil, acc, err
		}
		log.Printf("  cline failover: account=%s got %d, trying another account (%d/%d)",
			TruncateEmail(acc.Email), ce.Status, i+1, clineFailoverAttempts)
	}
	return nil, lastAcc, lastErr
}

// clineFailoverAttempts 单次请求最多尝试的账号数（429/5xx 换账号）。
const clineFailoverAttempts = 3

// clineGFEProxyForbidden 403 响应体是否为 Google 前端（GFE）的 IP 拒绝页而非
// cline API 的业务 403：HTML、Forbidden、"does not have permission" 三指纹同时
// 命中才判。cline API 的业务 403 是 JSON（权益/会话类），换账号无意义；GFE
// 页面拒的是出口 IP，换出口即可恢复。
func clineGFEProxyForbidden(body string) bool {
	low := strings.ToLower(body)
	return strings.Contains(low, "<html") &&
		strings.Contains(low, "forbidden") &&
		strings.Contains(low, "does not have permission")
}

// callAPIWithAccount 用指定账号执行一次上游调用（含代理轮转与 401 刷新）。

func callAPIWithAccount(ctx context.Context, params map[string]any, stream bool, useProxies bool, acc *Account) (*http.Response, error) {
	token, err := EnsureToken(acc)
	if err != nil {
		// Try other accounts
		return nil, fmt.Errorf("account %s token failed: %w", acc.Email, err)
	}

	body := BuildUpstreamBody(params, stream)
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
	//   - 隔离模式且账号有绑定：只在 {主, 备} 内尝试（至多 2 次），绑定出口
	//     全不可用时报错且不冷却账号 —— 绝不退回其他出口或直连
	//     （PickAccountExcluding 已过滤掉双不可用的账号，这里 !ok 只剩选号与
	//     拨号之间的窄竞态窗口）。
	//   - 未绑定 + useProxies：旧规则，每次尝试全局轮转挑代理（至多 3 次）。
	//   - 未绑定 + 直连：1 次，网络错误 5 分钟短冷却（原语义）。
	// 代理路径上的网络错误只冷却代理本身,绝不冷却账号 —— 代理故障不污染账号池。
	main, backup, bound := accountProxyBinding(acc)
	log.Printf("  upstream: account=%s stream=%v tools=%d msgs=%d max_tokens=%v effort=%v useProxies=%v boundExit=%v",
		TruncateEmail(acc.Email), stream, toolCount, GetMsgCount(params), body["max_tokens"], body["reasoning_effort"], useProxies, bound)
	reqlog.SetUpstreamInfo(ctx, fmt.Sprintf("account #%d", AccountNumber(acc)))
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
			proxyURL, pidx, ok = exitpool.PickBoundProxyFor(exitpool.KindCline, main, backup)
			if !ok {
				return nil, fmt.Errorf("account %s bound proxies unavailable (cooling or removed); account skipped by proxy isolation", TruncateEmail(acc.Email))
			}
			client = exitpool.ClientFor(proxyURL)
			if proxyURL == "" {
				log.Printf("  cline upstream via bound exit direct (no proxy)")
			} else {
				log.Printf("  cline upstream via bound exit %s", kit.MaskProxyURL(proxyURL))
			}
		} else if useProxies {
			var exitOK bool
			proxyURL, pidx, exitOK = exitpool.UpstreamForKind(exitpool.KindCline)
			if !exitOK {
				return nil, fmt.Errorf("cline account %s: every pool exit is manually cooled", TruncateEmail(acc.Email))
			}
			if proxyURL == "" && kit.DisableDirect() {
				return nil, fmt.Errorf("cline account %s has no proxy exit: direct egress is disabled by DISABLE_DIRECT", TruncateEmail(acc.Email))
			}
			client = exitpool.ClientFor(proxyURL)
			log.Printf("  cline upstream via %s", kit.MaskProxyURL(proxyURL))
		} else if kit.DisableDirect() {
			// 未绑定 + 面板直连模式：DISABLE_DIRECT=true 时凭据失效（2026-10-28）。
			return nil, fmt.Errorf("cline account %s has no proxy exit: direct egress is disabled by DISABLE_DIRECT", TruncateEmail(acc.Email))
		}
		// 每次尝试重建请求: bytes.Reader 只能读一次,
		// 复用已发送的 req 会以 "ContentLength=N with Body length 0" 失败
		req, rerr := http.NewRequestWithContext(ctx, "POST", APIBase+"/chat/completions", bytes.NewReader(bodyJSON))
		if rerr != nil {
			return nil, fmt.Errorf("create request: %w", rerr)
		}
		// hsStart 拨号前打点：Do 返回即隧道建立（首字节未回），差值=握手延迟。
		req.Header = ClineHeaders(token, sessionID)
		hsStart := time.Now()
		wctx, stopWatch := clineHeaderWatchdog(ctx)
		resp, lastErr = client.Do(req.WithContext(wctx))
		if stopWatch() {
			// 黑洞出口（TCP 通、响应头静默丢失）：按网络错误处理，冷却该出口并换下一个
			log.Printf("  cline upstream: response header watchdog fired after %v (exit unresponsive, failing over)", gwconfig.ClineHeaderTimeout())
		}
		if lastErr == nil {
			reqlog.SetUpstreamExit(ctx, exitpool.ProxyExitType(proxyURL, main, backup, bound))
			exitpool.RecordProxyOutcome(exitpool.KindCline, proxyURL, true, nil, time.Since(hsStart).Milliseconds())
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
			// exitpool.ShouldCooldownExit 纯查询（不写计数），真正的 strike 记账与冷却
			// 在下面的 exitpool.RecordProxyOutcome 单点完成——两处结论一致，动作幂等。
			// 建连失败与隧道后流错误都是 60s 窗口 N-strike（N 由
			// PROXY_COOLDOWN_STRIKES 控制，默认 3，2026-11-08）：
			// 单次网络抖动不再全局冷却唯一可用出口。
			if scope, cool := exitpool.ShouldCooldownExit(exitpool.KindCline, proxyURL, lastErr); cool {
				if scope == "global" {
					exitpool.CooldownUpstreamProxy(pidx, 2*time.Minute)
				} else {
					exitpool.CooldownUpstreamProxyFor(exitpool.KindCline, pidx, proxyPlatformCooldown)
				}
				log.Printf("  cline proxy failed (%v), cooldown exit %s 2m, retrying on next", lastErr, scope)
			} else {
				log.Printf("  cline upstream-side stream error (%v) on exit — not cooling the exit", lastErr)
			}
			exitpool.RecordProxyOutcome(exitpool.KindCline, proxyURL, false, lastErr, 0)
			continue
		}
		// 直连网络错误：临时短冷却 5 分钟
		MarkAccountCooldown(acc, "network error: "+lastErr.Error(), 5*time.Minute)
		return nil, fmt.Errorf("upstream request: %w", lastErr)
	}
	if lastErr != nil {
		// 所有代理出口都失败（useProxies 时）——不冷却账号
		return nil, fmt.Errorf("upstream request (all proxy exits failed): %w", lastErr)
	}

	if resp.StatusCode == 401 {
		resp.Body.Close()
		// Refresh token and retry（重建请求: 上一次 Do 已消费请求体）
		if rerr := RefreshAccountToken(acc); rerr == nil {
			token = acc.AccessToken
			req2, cerr := http.NewRequestWithContext(ctx, "POST", APIBase+"/chat/completions", bytes.NewReader(bodyJSON))
			if cerr != nil {
				return nil, fmt.Errorf("create request: %w", cerr)
			}
			req2.Header = ClineHeaders(token, sessionID)
			// 注意: 不能用 := —— 那会在 if 块内遮蔽外层 resp，重试成功的
			// 响应被丢弃（body 泄漏）而调用方仍拿到旧的 401
			hsStart2 := time.Now()
			wctx2, stopWatch2 := clineHeaderWatchdog(ctx)
			resp2, derr := client.Do(req2.WithContext(wctx2))
			stopWatch2()
			if derr != nil {
				exitpool.RecordProxyOutcome(exitpool.KindCline, exitURL, false, derr, 0)
				return nil, fmt.Errorf("upstream retry: %w", derr)
			}
			exitpool.RecordProxyOutcome(exitpool.KindCline, exitURL, true, nil, time.Since(hsStart2).Milliseconds())
			if resp2.StatusCode == 401 {
				resp2.Body.Close()
				PoolMu.Lock()
				UsageForLocked(acc).Status = "expired"
				SaveUsageLocked()
				PoolMu.Unlock()
				return nil, fmt.Errorf("account %s token expired permanently", TruncateEmail(acc.Email))
			}
			resp = resp2
		} else {
			PoolMu.Lock()
			UsageForLocked(acc).Status = "expired"
			SaveUsageLocked()
			PoolMu.Unlock()
			return nil, fmt.Errorf("account %s refresh failed: %w", TruncateEmail(acc.Email), rerr)
		}
	}

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// Mark account on cooldown on rate limits
		if resp.StatusCode == 429 {
			reason := kit.Truncate(string(bodyBytes), 500)
			duration := Cline429CooldownDuration(acc.Email, string(bodyBytes), resp.Header.Get("Retry-After"))
			MarkAccountCooldown(acc, "429: "+reason, duration)
			log.Printf("  account %s cooldown %v (reason: %s)", TruncateEmail(acc.Email), duration, reason)
		}
		// 非 200 一律返回 error（body 已读尽并关闭），但状态码 + 错误体本身有价值：
		// 类型化错误把它带出来，供 callClineAutoStream 识别"该模型必须流式"的指纹。
		return nil, &APIError{Status: resp.StatusCode, Body: kit.Truncate(string(bodyBytes), 500)}
	}

	BumpUsage(acc)
	return resp, nil
}

// AccountUsageFn 构造账号 token 记账回调：从上游 usage 提取
// prompt_tokens + completion_tokens，计入该账号今日/累计消耗。
// 仅 cline 路径使用（chat / 流式缓冲 / anthropic / responses 四个入口），
// 因此顺带做 cline 专属的计费哨兵：usage.cost > 0 说明官方已把该模型移出
// 免费档开始计费（2026-10-31 实测 free 组模型 cost 恒为 0，z-ai/glm 转计费
// 后 cost>0）——自动拉黑摘出目录，避免"能出字但在烧账号额度"的静默漏洞。
func AccountUsageFn(acc *Account, params map[string]any) func(map[string]any) {
	return func(u map[string]any) {
		RecordAccountTokens(acc, kit.UsageTokens(u, params))
		ClineAutoBanPaidUsage(params, u)
	}
}

// ClineAutoBanPaidUsage cline 计费哨兵：成功响应的 usage.cost > 0 即拉黑。
// 只认 cline 形态的模型（本回调只在 cline 路径被调用）；cost 取 float64，
// free 模型上游恒报 0，任何正值都是计费信号。判据独立于 404 拉黑（那是
// 模型彻底下线），原因同用 blacklist.BanReasonFeedRemoved——性质都是"官方不再免费"。
func ClineAutoBanPaidUsage(params map[string]any, u map[string]any) {
	if u == nil {
		return
	}
	cost, ok := u["cost"].(float64)
	if !ok || cost <= 0 || params == nil {
		return
	}
	model, _ := params["model"].(string)
	id := displayClineModelID(model)
	if id == "" || blacklist.IsModelBlacklisted(id) {
		return
	}
	detail := fmt.Sprintf("usage.cost=%g (paid model served to free account)", cost)
	blacklist.BlacklistModel(blacklist.PlatCline, id, "cost", blacklist.BanReasonFeedRemoved, detail)
	log.Printf("  cline paid-usage ban: %s cost=%g", id, cost)
}

func TruncateEmail(email string) string {
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

func GetMsgCount(params map[string]any) int {
	if msgs, ok := params["messages"].([]any); ok {
		return len(msgs)
	}
	return 0
}

// SetAPIBase 测试注入上游基址（app 探测/e2e 测试把调用链指向本地假上游）。
func SetAPIBase(u string) { APIBase = u }

func clineHeaderWatchdog(ctx context.Context) (context.Context, func() bool) {
	return ClineHeaderWatchdogFor(ctx, gwconfig.ClineHeaderTimeout())
}

// ClineHeaderWatchdogFor 是 clineHeaderWatchdog 的时长显式版本（便于单测）。
func ClineHeaderWatchdogFor(ctx context.Context, d time.Duration) (context.Context, func() bool) {
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

// ModelNeedsStream 该模型是否必须让上游走流式：模型元数据（同步层按命名约定
// 推断的 RequiresStream）或上游错误自学习命中即 true。
func ModelNeedsStream(modelID string) bool {
	InitModelsCache()
	ModelsMu.Lock()
	needs := false
	if m, ok := modelsCache[modelID]; ok && m.RequiresStream {
		needs = true
	}
	ModelsMu.Unlock()
	if needs {
		return true
	}
	// 命名约定漏判的模型由上游错误自学习得到（见 cline_stream.go）
	return StreamRequired(modelID)
}

// UpstreamNeedsStream 上游是否该走流式：FORCE_STREAM 总闸优先，其次按模型判定。
// 所有"客户端要非流式"的 cline 入口都必须问这个函数，别再直接调
// ModelNeedsStream —— 否则总闸会漏掉部分 API 形态（chat/messages/responses）。
func UpstreamNeedsStream(modelID string) bool {
	if gwconfig.ForceStream() {
		return true
	}
	return ModelNeedsStream(modelID)
}

func Cline429CooldownDuration(email, body, retryAfter string) time.Duration {
	if d := parseInferenceCapDuration(body); d > 0 {
		return d
	}
	if d := kit.ParseRetryAfter(retryAfter); d > 0 {
		return d
	}
	d, quota := 20*time.Minute, IsClineQuotaExhausted(body)
	if quota {
		d = 18 * time.Hour
	}
	log.Printf("  429 without parseable wait on %s: defaulting to %v (quota=%v)", TruncateEmail(email), d, quota)
	return d
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

// IsClineQuotaExhausted 从 429 错误体判断是否为配额耗尽（而非瞬时限流）。
// 只有明确的配额措辞才返回 true；429 缺少可解析时长时用它决定兜底冷却长度：
// 配额耗尽 → 18h，其余 → 20min 短冷却。
//
// 关键词必须**足够具体**：早先的列表里有裸的 "exceeded"，而瞬时限流的典型
// 正文就是 "Rate limit exceeded" —— 于是一次没带 Retry-After 的普通限流会把
// 健康账号冷却 18 小时，每个账号来一发整个池子就空了。只认同时表达"额度"
// 语义的组合，不认任何单独的 "limit"/"exceeded"。
func IsClineQuotaExhausted(body string) bool {
	low := strings.ToLower(body)
	for _, kw := range []string{
		"quota exceeded", "quota exhausted", "exceeded your quota",
		"insufficient quota", "insufficient credit", "insufficient balance",
		"out of credit", "out of credits", "no credits", "no credit left",
		"daily limit reach", "daily limit exceed", "monthly limit reach", "monthly limit exceed",
		"usage limit reach", "usage limit exceed", "credit limit reach",
		"billing hard limit", "free tier limit",
	} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// sortModelEntriesByID 按 id 字典序稳定排序模型条目(就地)。
//
// /v1/models 的「段内恒序」统一口径:各平台段内部一律确定有序(cline 按裸
// feed ID 升序、其余按输出 id 升序)。凡是段内顺序来自上游返回序(WorkBuddy
// cn/global/图像)、map 迭代序(zen)或文件存储序之外任何不确定来源的地方,
// 输出前都要过一遍——否则下游 new-api 等按序消费方每次看到的顺序都可能不同。
// 稳定排序保证同 id 条目相对次序不变。

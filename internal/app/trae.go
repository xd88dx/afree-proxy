package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/kit"
	trae "afree-proxy/internal/trae"
)

// ============ Trae 平台装配 ============
//
// Trae 账号池平台（trae: 前缀，Cloud-IDE-JWT + refresh token）：上游为
// TraeCode raw_chat 自有协议（internal/trae 实现线协议），本文件做管理
// API、账号池写时复制操作与网关分发的 SSE 翻译（Trae 事件流 → OpenAI
// chunk 流）。代理隔离语义与 key 池平台同构：每账号主/备出口绑定，双
// 不可用整体跳过，绝不直连回退。

// traeConfigPath Trae 配置文件路径（DATA_DIR/trae-config.json）。
func traeConfigPath() string { return kit.ResolveDataPath("trae-config.json") }

// initTrae 加载配置 + 注入出站解析/HTTP client 工厂。StartProxy 早期调用
// 一次；失败只记日志不影响既有平台（与 WorkBuddy 子系统同口径）。
func initTrae() {
	trae.UsagePath = kit.ResolveDataPath("trae-state.json")
	trae.Load(traeConfigPath())
	trae.SetExitResolver(func(acc *trae.Account) (string, bool) {
		if acc == nil {
			return "", true
		}
		main, backup, bound := traeBindingOf(acc.ID)
		if !bound {
			// 未绑定 = 全局规则：面板选直连时不走池
			if !trae.ProxiesEnabled() {
				if kit.DisableDirect() {
					return "", false // 直连被禁：凭据失效（2026-10-28）
				}
				return "", true
			}
			url, _ := pickUpstreamProxyFor(pkTrae)
			if url == "" && kit.DisableDirect() {
				return "", false // 代理池空兜底的直连同样不放行
			}
			return url, true
		}
		url, _, ok := pickBoundProxyFor(pkTrae, main, backup)
		return url, ok
	})
	trae.SetClientProvider(proxyClientFor)
	trae.SetProxyObserver(func(u string, ok bool, e error, ms int64) {
		recordProxyOutcome(pkTrae, u, ok, e, ms)
	})
	// 按端点档位的封禁查询（2026-10-27）：trae.ModelList 展示过滤时回查
	// app 层黑名单（scope 隔离：raw/remote/work 互不影响）。
	trae.SetBanChecker(func(scope, bareID string) bool {
		return TraeModelBannedIn(scope, bareID)
	})
	trae.SetAttemptObserver(func(ctx context.Context, acc *trae.Account, proxyURL string) {
		if ctx == nil || acc == nil {
			return
		}
		setUpstreamInfo(ctx, fmt.Sprintf("account#%d", trae.AccountIndex(acc.ID)))
		main, backup, bound := traeBindingOf(acc.ID)
		if !proxyIsolationEnabled() {
			bound = false
			main, backup = "", ""
		}
		setUpstreamExit(ctx, proxyExitType(proxyURL, main, backup, bound))
	})
	// 启动即预热模型目录：先同步一次，再进 5 分钟轮询。展示层只列原生档
	// 确认过的条目，首次同步完成前列表为空，不能等轮询第一跳（5 分钟）。
	trae.InitCatalog()
	go func() {
		syncTraeCatalog(1)
		startTraeModelsRefresher()
	}()
	// 额度定期刷新 + 自动签到巡检
	startTraeJobs()
}

// startTraeModelsRefresher 每 5 分钟同步一次 live 模型目录（需要至少一个
// 启用账号的 JWT；失败下一轮自然重试）。
func startTraeModelsRefresher() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		syncTraeCatalog(1)
	}
}

// syncTraeCatalog 同步目录；attempts 为额外重试次数（不含首次）。
// 没有启用账号时跳过（展示门控下同步也无意义）并返回错误供调用方判别。
// 2026-10-31：同步失败不再静默吞掉，手动刷新端点据此报真实失败。
func syncTraeCatalog(attempts int) error {
	if !trae.HasEnabledAccount() {
		return errors.New("no enabled Trae account")
	}
	var lastErr error
	for i := 0; i <= attempts; i++ {
		changed, err := trae.SyncCatalog()
		if err == nil {
			if changed > 0 {
				log.Printf("trae model sync: %d models changed", changed)
			}
			return nil
		}
		lastErr = err
		if i < attempts {
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		} else {
			log.Printf("trae model sync failed: %v", err)
		}
	}
	return lastErr
}

// traeModelList /v1/models 合并用的模型条目（trae: 前缀对外暴露）。
func traeModelList() []map[string]any {
	out := make([]map[string]any, 0)
	for _, m := range trae.ModelList() {
		entry := map[string]any{
			"id":       trae.ModelPrefix + m.ID,
			"object":   "model",
			"created":  time.Now().UnixMilli(),
			"owned_by": "trae",
			"source":   "trae",
			"status":   "active",
			"cost":     "free",
		}
		if m.Context > 0 {
			entry["context"] = m.Context
		}
		out = append(out, entry)
	}
	return out
}

// traeBindingOf 账号的绑定出口（主/备）；未绑定时 bound=false。
func traeBindingOf(id string) (main, backup string, bound bool) {
	a := trae.FindAccount(id)
	if a == nil {
		return "", "", false
	}
	if a.ProxyMain == "" && a.ProxyBackup == "" {
		return "", "", false
	}
	return a.ProxyMain, a.ProxyBackup, true
}

// traeBoundRoutable 该账号绑定出口是否可路由（隔离判定，注入 trae.PickAccount）。
// 未绑定账号恒可路由（走全局规则）；绑定代理已从池中删除/冷却中则跳过。
func traeBoundRoutable(acc *trae.Account) bool {
	if !proxyIsolationEnabled() {
		return true
	}
	main, backup, bound := traeBindingOf(acc.ID)
	if !bound {
		return true
	}
	return boundProxiesRoutableFor(pkTrae, main, backup)
}

// ============ Trae SSE → OpenAI 翻译 ============

// traeLengthFinish 终止原因里的"截断"族（sse.py _finish_reason 映射）。
var traeLengthFinish = map[string]bool{
	"length": true, "max_tokens": true, "max_output_tokens": true, "max_token": true,
	"token_limit": true, "model_context_window_exceeded": true,
	"context_window_exceeded": true, "context_length_exceeded": true,
}

// traeMapFinishReason 上游原始 finish_reason → OpenAI 标准值。
func traeMapFinishReason(fr string, hasTools bool) string {
	low := strings.ToLower(strings.TrimSpace(fr))
	switch {
	case hasTools:
		return "tool_calls"
	case traeLengthFinish[low]:
		return "length"
	case strings.Contains(low, "content_filter"), strings.Contains(low, "safety"):
		return "content_filter"
	default:
		return "stop"
	}
}

// traeAgg 一次尝试的 OpenAI SSE 累积器。
type traeAgg struct {
	buf    bytes.Buffer
	model  string
	accID  string
	usage  *trae.Usage
	tools  []trae.ToolCall
	finish string
	accTok int64
}

func (a *traeAgg) chunk(delta map[string]any, finish any) {
	chunk := map[string]any{
		"id":      "chatcmpl-trae-" + a.accID,
		"object":  "chat.completion.chunk",
		"created": time.Now().UnixMilli(),
		"model":   a.model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finish,
		}},
	}
	if a.usage != nil && finish != nil {
		chunk["usage"] = map[string]any{
			"prompt_tokens":     a.usage.Prompt,
			"completion_tokens": a.usage.Completion,
			"total_tokens":      a.usage.Total,
		}
	}
	b, _ := json.Marshal(chunk)
	a.buf.WriteString("data: ")
	a.buf.Write(b)
	a.buf.WriteString("\n\n")
}

// traeStrategy 返回 Trae 的选号策略。优先级（高 → 低）：
//  1. TRAE_STRATEGY 环境变量（Trae 独立策略，显式设置即覆盖）
//  2. 面板「Selection strategy」（trae.Config.Selection）
//  3. 全局账号池策略 proxyConfig.Strategy（POOL_STRATEGY，默认 round_robin）
//
// 取值 round_robin / fill / random / credit-priority。credit-priority 是
// Trae 独有能力（额度余量多者优先），其它平台没有对应语义。
func traeStrategy() string {
	if s := strings.TrimSpace(envStr("TRAE_STRATEGY")); s != "" {
		switch s {
		case "round_robin", "fill", "random", "credit-priority":
			return s
		default:
			log.Printf("invalid TRAE_STRATEGY %q, ignored (round_robin / fill / random / credit-priority)", s)
		}
	}
	if s := strings.TrimSpace(trae.Get().Selection); s != "" {
		return s
	}
	return globalStrategy()
}

// traeStreamOutcome 一次流消费的结果分类（缓冲层重试与 Work 回落的判据）。
type traeStreamOutcome int

const (
	traeOutcomeComplete          traeStreamOutcome = iota // 收到 done，完整
	traeOutcomeBrokenNoModelEvent                         // 断流且未出任何模型事件（可 Work 回落）
	traeOutcomeBrokenPartial                              // 出过模型事件后断流（回放部分内容，缓冲层重试）
	traeOutcomeRateLimited                                // 流内 error 事件且限流（缓冲层换号重试）
)

// isTraeModelEvent 计入"模型事件"的事件类型（对齐上游 observed_model_event；
// queue 是排队提示不算）。
func isTraeModelEvent(t string) bool {
	switch t {
	case "text", "reasoning", "usage", "done":
		return true
	}
	return false
}

// aggregateTraeSSE 运行一次 trae.Chat 并把事件流翻译成完整 OpenAI SSE 字节。
// 流中途断开/收到 error 事件：返回已生成部分（无 [DONE]，缓冲层据此重试）；
// 上游在首字节前失败：返回 err。usage 随返回值带回（记账用）。
// 断流未出任何模型事件时先尝试同账号 Work 回落（上游 work fallback 语义）。
func aggregateTraeSSE(ctx context.Context, params map[string]any, m trae.Model) (data []byte, usage *trae.Usage, accID string, err error) {
	opts := trae.ChatOpts{
		Model:         m,
		Params:        params,
		Strategy:      traeStrategy(),
		BoundRoutable: traeBoundRoutable,
	}
	stream, acc, err := trae.Chat(ctx, opts)
	if err != nil {
		traeAutoBanOnHTTPError(m, acc, err)
		return nil, nil, "", err
	}
	data, usage, outcome, cerr := consumeTraeAgg(ctx, stream, m, acc)
	if cerr != nil {
		return nil, usage, acc.ID, cerr
	}
	switch outcome {
	case traeOutcomeComplete:
		return data, usage, acc.ID, nil
	case traeOutcomeBrokenNoModelEvent:
		// 首个模型事件前流结束：对齐上游 work fallback（main.py:8940，
		// !observed_model_event && retryable → 换 solo_work_remote 会话重发）。
		// 同时给该账号 30s 短冷却（2026-10-29 排查：EOF 成簇出现时该账号上游
		// 并发坑位被活会话/僵尸会话占满，继续重试只会再吃一个会话）——缓冲层
		// 下一次 Chat 自动轮换到其他账号；Work 回落不受影响（同账号直达）。
		trae.Cooldown(acc.ID, 30*time.Second)
		if wdata, wusage, wok := traeWorkFallbackOnce(ctx, acc, stream, opts, m); wok {
			return wdata, wusage, acc.ID, nil
		}
		return data, usage, acc.ID, nil
	default:
		// Partial / RateLimited：交缓冲层按断流重试（原语义）
		return data, usage, acc.ID, nil
	}
}

// traeWorkFallbackOnce 同账号 Work 传输回落一次：仅在断流发生于 remote 档
//（agent 执行器）且未出模型事件时触发；Work 侧也断/出错则放弃回落，
// 交缓冲层重试。
func traeWorkFallbackOnce(ctx context.Context, acc *trae.Account, first *trae.Stream, opts trae.ChatOpts, m trae.Model) (data []byte, usage *trae.Usage, ok bool) {
	if first == nil || first.Tier != "remote" {
		return nil, nil, false
	}
	wok, wstream, err := trae.ChatWorkFallback(ctx, acc, opts)
	if !wok {
		return nil, nil, false
	}
	if err != nil {
		log.Printf("  trae work fallback failed: %v", err)
		return nil, nil, false
	}
	defer wstream.Close()
	log.Printf("  trae work fallback: remote ended before first model event, account=%q model=%s", acc.Label, m.ID)
	data, usage, outcome, cerr := consumeTraeAgg(ctx, wstream, m, acc)
	if cerr != nil || outcome != traeOutcomeComplete {
		return nil, nil, false
	}
	return data, usage, true
}

// consumeTraeAgg 消费一条 trae 流并聚合为 OpenAI SSE 字节（含 Close）。
// 断流（读错误 / EOF 未见到 done）时返回已聚合的部分内容，由调用方按
// outcome 决定重试方式。
func consumeTraeAgg(ctx context.Context, stream *trae.Stream, m trae.Model, acc *trae.Account) (data []byte, usage *trae.Usage, outcome traeStreamOutcome, err error) {
	defer stream.Close()
	agg := &traeAgg{model: trae.ModelPrefix + m.ID, accID: acc.ID}
	agg.chunk(map[string]any{"role": "assistant"}, nil)
	sawModelEvent := false
	for {
		ev, rerr := stream.Next()
		if rerr != nil {
			// 上游读错误：部分内容 + 让缓冲层按断流重试
			if sawModelEvent {
				return agg.buf.Bytes(), agg.usage, traeOutcomeBrokenPartial, nil
			}
			return agg.buf.Bytes(), agg.usage, traeOutcomeBrokenNoModelEvent, nil
		}
		if ev == nil {
			// 上游 EOF 未见到 done：视为断流（缓冲层重试 / Work 回落）
			if sawModelEvent {
				return agg.buf.Bytes(), agg.usage, traeOutcomeBrokenPartial, nil
			}
			return agg.buf.Bytes(), agg.usage, traeOutcomeBrokenNoModelEvent, nil
		}
		if isTraeModelEvent(ev.Type) {
			sawModelEvent = true
		}
		switch ev.Type {
		case "text", "queue":
			if ev.Text != "" {
				agg.chunk(map[string]any{"content": ev.Text}, nil)
			}
		case "reasoning":
			if ev.Text != "" {
				agg.chunk(map[string]any{"reasoning_content": ev.Text}, nil)
			}
		case "usage":
			agg.usage = ev.Usage
		case "done":
			agg.finish = ev.FinishReason
			if agg.usage == nil {
				agg.usage = stream.Usage
			}
			agg.tools = stream.ToolCallsSnapshot()
			// 工具调用整块输出（OpenAI 增量形态：先 id/name/空参，再全参）
			for i, tc := range agg.tools {
				args := tc.ArgsJSON
				if args == "" {
					args = "{}"
				}
				id := tc.ID
				if id == "" {
					id = fmt.Sprintf("call_trae_%d_%s", i, agg.accID)
				}
				agg.chunk(map[string]any{"tool_calls": []any{map[string]any{
					"index": i, "id": id, "type": "function",
					"function": map[string]any{"name": tc.Name, "arguments": args},
				}}}, nil)
			}
			reason := traeMapFinishReason(agg.finish, len(agg.tools) > 0)
			agg.chunk(map[string]any{}, reason)
			agg.buf.WriteString("data: [DONE]\n\n")
			return agg.buf.Bytes(), agg.usage, traeOutcomeComplete, nil
		case "error":
			// 上游业务错误：限流/配额换账号可能修复，保留断流语义让缓冲层
			// 重试；其余（模型不存在 4023、参数/权益 4001/1005 等）换号无用，
			// 直接上抛真实原因——不要再退化成"空流"。
			if stream.Err != nil && stream.Err.RateLimited {
				return agg.buf.Bytes(), agg.usage, traeOutcomeRateLimited, nil
			}
			traeAutoBanModel(m, acc, ev.Code, traeEventErrorText(ev), stream.Transport)
			return nil, agg.usage, traeOutcomeBrokenPartial, fmt.Errorf("trae upstream error: %s", traeEventErrorText(ev))
		}
	}
}

// traeEventErrorText 上游 error 事件的可读文案（带业务码，便于面板排障）。
func traeEventErrorText(ev *trae.StreamEvent) string {
	msg := strings.TrimSpace(ev.Message)
	if msg == "" {
		msg = "upstream error event"
	}
	if ev.Code != "" {
		return msg + " (code " + ev.Code + ")"
	}
	return msg
}

// ============ 模型级失败自动封禁 ============
//
// 上游这几类答复换账号、重试都不会变，却只有真调一次才知道：
//   - 1005 套餐/权益不足（实测 extra 带 {"plan":3}）
//   - 4023 模型名未知（目录里的 web-only/旧名）
//   - 4001 参数/权益错误（实测 glm-5.3 最小请求也 4001）
//
// 处理：命中后加入「平台+模型」黑名单（与面板「封禁模型」同表、同解禁
// 入口），/v1/models 立刻不再列出，客户端不会反复选中它吃 502。
//
// 4001 的文案与"客户端传了上游不接受的东西"同形，直接封会误伤，故先复核：
// 用**同一个账号**发一个纯文本最小请求，能出字说明是本次请求形状问题，不封；
// 照样 4001 才封。复核异步执行，不拖慢客户端响应。

// traeAutoBanDecision 错误码 → (是否封禁, 是否需复核)。
func traeAutoBanDecision(code string) (ban, verify bool) {
	switch strings.TrimSpace(code) {
	case "1005", "4023":
		return true, false
	case "4001":
		return true, true
	}
	return false, false
}

// traeAutoBanInflight 同一模型的复核去重（并发 502 不叠加探测请求）。
var traeAutoBanInflight sync.Map

// traeAutoBanOnHTTPError HTTP 级上游错误与流内 error 事件同判据自动封禁：
// 上游以非 200 返回业务码时错误走 trae.Chat 的返回值（而非流内 error 事件），
// Chat 已对 1005/4023/4001 不再换号重试，封禁入口也要对齐——否则同样的
// 模型级失败每次都只报错不进黑名单，/v1/models 继续列出。
func traeAutoBanOnHTTPError(m trae.Model, acc *trae.Account, err error, transport ...string) {
	var he *trae.HTTPError
	if !errors.As(err, &he) {
		return
	}
	t := ""
	if len(transport) > 0 && transport[0] != "" {
		t = transport[0]
	} else if he.Transport != "" {
		t = he.Transport
	}
	traeAutoBanModel(m, acc, he.Code, fmt.Sprintf("HTTP %d: %s", he.Status, kit.Truncate(he.Body, 300)), t)
}

// traeTransportScope 传输名 → 封禁档位（remote/traecode/traework）。
// 空/未知 → 空串（全档封禁，保守兜底）。agent 类型名（solo_agent_remote /
// solo_work_remote）是 Stream/HTTPError.Transport 的原始取值，一并归一，
// 避免 remote 流的失败落成"全档封禁"（2026-10-29 排查发现）。
func traeTransportScope(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "remote", "solo_agent_remote":
		return "remote"
	case "work", "solo_work_remote":
		return "traework"
	case "raw":
		return "traecode"
	}
	return ""
}

// traeAutoBanModel 命中判据后封禁；4001 走异步复核。封禁按失败发生的
// 传输落端点档位（2026-10-27 用户定）：raw(trae-b) 的失败不影响
// remote(trae-a) / work(trae-c) 的同 ID 模型。transport 空 = 未知传输，
// 全档封禁（保守兜底）。
func traeAutoBanModel(m trae.Model, acc *trae.Account, code, detail, transport string) {
	ban, verify := traeAutoBanDecision(code)
	if !ban {
		return
	}
	id := trae.ModelPrefix + m.ID
	scope := traeTransportScope(transport)
	if scope != "" && TraeModelBannedIn(scope, m.ID) {
		return
	}
	if scope == "" && IsModelBlacklisted(id) {
		return
	}
	reason := BanReasonAuto
	if strings.TrimSpace(code) == "4023" {
		reason = BanReasonModelGone
	}
	if !verify {
		BlacklistModel(platTrae, id, code, reason, detail, scope)
		return
	}
	if _, loaded := traeAutoBanInflight.LoadOrStore(id+"@"+scope, struct{}{}); loaded {
		return
	}
	go func() {
		defer traeAutoBanInflight.Delete(id + "@" + scope)
		if !traeProbeFailsModel(m, acc) {
			return
		}
		BlacklistModel(platTrae, id, code, reason, detail, scope)
	}()
}

// traeProbeFailsModel 同账号纯文本复核：仍然得到模型级失败返回 true；
// 能出字 / 限流 / 读不出结论一律 false（宁可漏封，不误封）。
func traeProbeFailsModel(m trae.Model, acc *trae.Account) bool {
	if acc == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	params := map[string]any{
		"model":      trae.ModelPrefix + m.ID,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with exactly: OK"}},
		"max_tokens": 16,
	}
	stream, _, err := trae.Chat(ctx, trae.ChatOpts{Model: m, Params: params, PinAccount: acc})
	if err != nil {
		var he *trae.HTTPError
		if errors.As(err, &he) && !he.RateLimited {
			ban, _ := traeAutoBanDecision(he.Code)
			return ban
		}
		return false
	}
	defer stream.Close()
	for {
		ev, rerr := stream.Next()
		if rerr != nil || ev == nil {
			return false // 断流/EOF 读不出结论
		}
		switch ev.Type {
		case "text", "reasoning", "done":
			return false // 能产出内容 = 模型本身可用
		case "error":
			if stream.Err != nil && stream.Err.RateLimited {
				return false
			}
			ban, _ := traeAutoBanDecision(ev.Code)
			return ban
		}
	}
}

// aggregateTraeCompletion 非流式：聚合事件流为一份 chat.completion JSON。
// 同时返回服务账号 ID（token 记账归属）。
func aggregateTraeCompletion(ctx context.Context, params map[string]any, m trae.Model) (map[string]any, string, error) {
	stream, acc, err := trae.Chat(ctx, trae.ChatOpts{
		Model:         m,
		Params:        params,
		Strategy:      traeStrategy(),
		BoundRoutable: traeBoundRoutable,
	})
	if err != nil {
		traeAutoBanOnHTTPError(m, acc, err)
		return nil, "", err
	}
	completion, cerr := consumeTraeCompletion(ctx, stream, m, acc)
	if cerr == nil {
		return completion, acc.ID, nil
	}
	// 空流（remote 档首事件前断流）→ Work 回落一次，与流式路径同口径
	if errors.Is(cerr, errTraeEmptyStream) {
		if wcompletion, wok := traeWorkFallbackCompletion(ctx, acc, stream, trae.ChatOpts{
			Model: m, Params: params, Strategy: traeStrategy(), BoundRoutable: traeBoundRoutable,
		}, m); wok {
			return wcompletion, acc.ID, nil
		}
	}
	return nil, acc.ID, cerr
}

// errTraeEmptyStream 流未产出任何内容即结束（触发 Work 回落判据）。
var errTraeEmptyStream = errors.New("trae upstream returned an empty stream")

// traeWorkFallbackCompletion 非流式路径的 Work 回落（对齐 traeWorkFallbackOnce）。
func traeWorkFallbackCompletion(ctx context.Context, acc *trae.Account, first *trae.Stream, opts trae.ChatOpts, m trae.Model) (map[string]any, bool) {
	if first == nil || first.Tier != "remote" {
		return nil, false
	}
	wok, wstream, err := trae.ChatWorkFallback(ctx, acc, opts)
	if !wok {
		return nil, false
	}
	if err != nil {
		log.Printf("  trae work fallback failed: %v", err)
		return nil, false
	}
	defer wstream.Close()
	log.Printf("  trae work fallback (non-stream): remote ended before first model event, account=%q model=%s", acc.Label, m.ID)
	completion, cerr := consumeTraeCompletion(ctx, wstream, m, acc)
	if cerr != nil {
		return nil, false
	}
	return completion, true
}

// consumeTraeCompletion 非流式消费一条 trae 流（含 Close），聚合为 completion。
func consumeTraeCompletion(ctx context.Context, stream *trae.Stream, m trae.Model, acc *trae.Account) (map[string]any, error) {
	defer stream.Close()
	var content, reasoning strings.Builder
	var usage *trae.Usage
	for {
		ev, rerr := stream.Next()
		if rerr != nil {
			// 读错误：零内容视为空流（可 Work 回落，对齐上游 read-timeout
			// retryable 语义）；已出内容按错误上抛
			if content.Len() == 0 && reasoning.Len() == 0 && len(stream.ToolCallsSnapshot()) == 0 {
				return nil, fmt.Errorf("trae upstream returned an empty stream (%w)", errTraeEmptyStream)
			}
			return nil, fmt.Errorf("trae upstream stream error: %w", rerr)
		}
		if ev == nil {
			break
		}
		switch ev.Type {
		case "text", "queue":
			content.WriteString(ev.Text)
		case "reasoning":
			reasoning.WriteString(ev.Text)
		case "usage":
			usage = ev.Usage
		case "error":
			traeAutoBanModel(m, acc, ev.Code, traeEventErrorText(ev), stream.Transport)
			return nil, fmt.Errorf("trae upstream error: %s", traeEventErrorText(ev))
		case "done":
			if usage == nil {
				usage = stream.Usage
			}
			return buildTraeCompletion(m, content.String(), reasoning.String(), stream.ToolCallsSnapshot(), usage, ev.FinishReason), nil
		}
	}
	// 无 done 终止：有内容就按现状返回，否则报错（空响应不伪装成功）
	if content.Len() > 0 || reasoning.Len() > 0 || len(stream.ToolCallsSnapshot()) > 0 {
		return buildTraeCompletion(m, content.String(), reasoning.String(), stream.ToolCallsSnapshot(), usage, ""), nil
	}
	return nil, errTraeEmptyStream
}

// buildTraeCompletion 组装 OpenAI chat.completion 响应体。
func buildTraeCompletion(m trae.Model, content, reasoning string, tools []trae.ToolCall, usage *trae.Usage, finish string) map[string]any {
	msg := map[string]any{"role": "assistant", "content": content}
	if reasoning != "" {
		msg["reasoning_content"] = reasoning
	}
	if len(tools) > 0 {
		tcs := make([]any, 0, len(tools))
		for i, tc := range tools {
			args := tc.ArgsJSON
			if args == "" {
				args = "{}"
			}
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_trae_%d", i)
			}
			tcs = append(tcs, map[string]any{
				"id": id, "type": "function",
				"function": map[string]any{"name": tc.Name, "arguments": args},
			})
		}
		msg["tool_calls"] = tcs
	}
	resp := map[string]any{
		"id":      "chatcmpl-trae",
		"object":  "chat.completion",
		"created": time.Now().UnixMilli(),
		"model":   trae.ModelPrefix + m.ID,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       msg,
			"finish_reason": traeMapFinishReason(finish, len(tools) > 0),
		}},
	}
	if usage != nil {
		resp["usage"] = map[string]any{
			"prompt_tokens":     usage.Prompt,
			"completion_tokens": usage.Completion,
			"total_tokens":      usage.Total,
		}
	}
	return resp
}

// handleTraeChat Trae chat/completions 转发（网关命中 trae: 前缀时调用）。
// 上游永远流式：流式路径走"缓冲+中断重试"（与 cline/zen 同语义）；非流式
// 由聚合器拼出完整 chat.completion。
func handleTraeChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	bare, _ := trae.StripPrefix(mustString(params["model"]))
	m, ok := trae.Resolve(bare)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not a known trae model", bare), "type": "invalid_request_error"},
		})
		return
	}
	if len(trae.AccountIDs()) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"message": "No Trae accounts configured. Add accounts on the Trae page.", "type": "auth_error"},
		})
		return
	}
	isStream, _ := params["stream"].(bool)
	if isStream {
		handleStreamBufferedGeneric(w, r, "trae", params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
			data, usage, accID, err := aggregateTraeSSE(ctx, params, m)
			if err != nil {
				return nil, nil, err
			}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(bytes.NewReader(data)),
			}
			usageFn := func(map[string]any) {
				if usage != nil {
					trae.RecordAccountTokens(accID, usage.Prompt+usage.Completion)
				}
			}
			return resp, usageFn, nil
		})
		return
	}
	completion, accID, err := aggregateTraeCompletion(r.Context(), params, m)
	if err != nil {
		log.Printf("  trae api error: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return
	}
	if usage, ok := completion["usage"].(map[string]any); ok && accID != "" {
		trae.RecordAccountTokens(accID, int64(modelEntryInt(usage["prompt_tokens"])+modelEntryInt(usage["completion_tokens"])))
	}
	writeJSON(w, http.StatusOK, completion)
}

// ============ Trae 管理 API ============

// traeAccountStates 面板展示用的每账号运行时状态（凭据打码，明文不出口）。
func traeAccountStates() []map[string]any {
	c := trae.Get()
	now := time.Now()
	out := make([]map[string]any, 0, len(c.Accounts))
	for i, a := range c.Accounts {
		st := map[string]any{
			"index":          i,
			"id":             a.ID,
			"label":          a.Label,
			"tokenMask":      trae.MaskToken(a.Token),
			"hasRefresh":     a.RefreshToken != "",
			"accountEnabled": a.EnabledOrDefault(),
			// 原始路由意愿（不含总闸与运算；前端按 双开关合成显示态）
			"routingEnabled": a.PoolEnabledOrDefault(),
		}
		if exp := traeTokenExpire(a); !exp.IsZero() {
			st["expiredAt"] = exp.UTC().Format(time.RFC3339)
			st["tokenUsable"] = time.Until(exp) > 5*time.Minute
		}
		if a.Edition != "" {
			st["edition"] = a.Edition
		}
		// 使用痕迹（记账/签到/额度/冷却）住 trae-state.json，按账号 ID 取副本
		u := trae.UsageFor(a.ID)
		if u.Tokens.Today > 0 || u.Tokens.Total > 0 || !u.Tokens.LastUsedAt.IsZero() {
			st["tokensToday"] = u.Tokens.Today
			st["tokensTotal"] = u.Tokens.Total
			if !u.Tokens.LastUsedAt.IsZero() {
				st["lastUsed"] = u.Tokens.LastUsedAt.Format(time.RFC3339)
			}
		}
		if ck := u.Checkin; ck != nil {
			st["checkin"] = map[string]any{
				"checkedIn": ck.CheckedIn,
				"date":      ck.Date,
				"lastError": ck.LastError,
			}
		}
		if cr := u.Credits; cr != nil {
			st["credits"] = map[string]any{
				"remaining": cr.Remaining, "limit": cr.Limit,
				"unlimited": cr.Unlimited, "identity": cr.Identity,
			}
		}
		if a.ProxyMain != "" || a.ProxyBackup != "" {
			st["proxyMain"] = a.ProxyMain
			st["proxyBackup"] = a.ProxyBackup
		}
		if until, cooling := trae.CooldownUntil(a.ID); cooling && now.Before(until) {
			st["cooling"] = true
			st["cooldownUntil"] = until.Format(time.RFC3339)
		} else {
			st["cooling"] = false
		}
		out = append(out, st)
	}
	return out
}

// traeTokenExpire 账号 token 过期时刻（未设置返回零值）。
func traeTokenExpire(a *trae.Account) time.Time {
	if a.ExpiredAt == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, a.ExpiredAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// GET /admin/api/trae/config
func handleTraeConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	c := trae.Get()
	data := map[string]any{
		"useProxies":              trae.ProxiesEnabled(),
		"gateway":                 c.Gateway,
		"selection":               c.Selection,
		"endpoint":                c.Endpoint,
		"autoCheckin":             c.AutoCheckin != nil && *c.AutoCheckin,
		"accounts":                traeAccountStates(),
		"maxConcurrency":          c.MaxConcurrency,
		"retries":                 c.Retries,
		"models":                  traeModelList(),
		"proxyIsolation":          proxyIsolationEnabled(),
		"proxyIsolationEnvLocked": proxyIsolationEnvLocked(),
		"backupProxyEnabled":      backupProxyEnabled(),
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// POST /admin/api/trae/config/update
func handleTraeConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var patch struct {
		UseProxies  *bool   `json:"useProxies"`
		Gateway     *string `json:"gateway"`
		MaxConc     *int    `json:"maxConcurrency"`
		Retries     *int    `json:"retries"`
		Selection   *string `json:"selection"`
		Endpoint    *string `json:"endpoint"`
		AutoCheckin *bool   `json:"autoCheckin"`
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	_, next := traeCloneConfig()
	// 走池开关存在 proxy-config.json 的代理池段，不写进平台文件。
	if patch.UseProxies != nil {
		setPlatformUseProxies("trae", patch.UseProxies)
	}
	if patch.Gateway != nil {
		next.Gateway = strings.TrimRight(strings.TrimSpace(*patch.Gateway), "/")
	}
	if patch.MaxConc != nil && *patch.MaxConc > 0 {
		next.MaxConcurrency = *patch.MaxConc
	}
	if patch.Retries != nil && *patch.Retries >= 0 {
		next.Retries = *patch.Retries
	}
	if patch.Selection != nil {
		next.Selection = strings.TrimSpace(*patch.Selection)
	}
	if patch.Endpoint != nil {
		// normalize() 内校验：非法值回落 auto（空）
		next.Endpoint = strings.TrimSpace(*patch.Endpoint)
	}
	if patch.AutoCheckin != nil {
		next.AutoCheckin = patch.AutoCheckin
	}
	trae.Set(next)
	trae.SetMaxConcurrency()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Trae config saved"})
}

// POST /admin/api/trae/accounts/add  body: { tokens: [JWT,...] }
// 每行一个 Cloud-IDE-JWT；新账号默认不参与路由（三平台统一口径）。
func handleTraeAccountAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Tokens []string `json:"tokens"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	added, skipped := 0, 0
	for _, line := range req.Tokens {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !trae.LooksLikeCloudIDEJWT(line) {
			skipped++
			continue
		}
		id := trae.TokenAccountID(line)
		if trae.HasAccount(id) {
			skipped++
			continue
		}
		cur, next := traeCloneConfig()
		next.Accounts = append(next.Accounts, &trae.Account{ID: id, Token: line})
		next.ReconcilePoolEnabled(next, cur)
		trae.Set(next)
		added++
	}
	msg := fmt.Sprintf("added %d account(s)", added)
	if skipped > 0 {
		msg += fmt.Sprintf(", skipped %d (duplicate or not a Cloud-IDE-JWT)", skipped)
	}
	log.Printf("trae accounts add: %s", msg)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: msg, Data: map[string]any{"added": added, "skipped": skipped}})
}

// POST /admin/api/trae/accounts/routing  body: { index, enabled }
func handleTraeAccountSetRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index   int  `json:"index"`
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := traeSetAccountRouting(req.Index, req.Enabled); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("account #%d routing enabled=%v", req.Index+1, req.Enabled)})
}

// POST /admin/api/trae/accounts/routing/all  body: { enabled }
func handleTraeAccountSetRoutingAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	n := traeSetAllAccountsRouting(req.Enabled)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("routing enabled=%v on %d account(s)", req.Enabled, n)})
}

// POST /admin/api/trae/accounts/reorder  body: { order: [oldIndex,...] }
func handleTraeAccountsReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Order []int `json:"order"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := traeReorderAccounts(req.Order); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"updated": len(req.Order)}})
}

// POST /admin/api/trae/accounts/delete  body: { index }
func handleTraeAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := traeDeleteAccount(req.Index); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("account #%d deleted", req.Index+1)})
}

// POST /admin/api/trae/accounts/label  body: { index, label }
func handleTraeAccountLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index int    `json:"index"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := traeSetAccountLabel(req.Index, strings.TrimSpace(req.Label)); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Label saved"})
}

// POST /admin/api/trae/accounts/proxy  body: { index, main, backup }
func handleTraeAccountSetProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index  int    `json:"index"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := traeSetAccountProxy(req.Index, req.Main, req.Backup); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Account proxy binding saved"})
}

// POST /admin/api/trae/accounts/proxy/clear
func handleTraeAccountClearProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	n := traeClearAllAccountProxies()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"cleared": n}})
}

// POST /admin/api/trae/accounts/test  body: { index }
// GetUserInfo 验凭据（X-Cloudide-Token），成功顺带抓一次额度快照；首测且
// 无备注时用 ScreenName 自动补显示名。
func handleTraeAccountTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	result, status := traeTestAccount(req.Index)
	result["status"] = status
	log.Printf("Test trae account #%d: status=%s reason=%v", req.Index+1, status, result["reason"])
	writeAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// POST /admin/api/trae/models/refresh
// 2026-10-31：改走全平台统一的刷新契约（见 admin_models_refresh.go）。
func handleTraeModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if !trae.HasEnabledAccount() {
		writeCatalogNoCredential(w, platTrae)
		return
	}
	before := len(trae.ModelList())
	if err := syncTraeCatalog(1); err != nil {
		writeCatalogSyncFailed(w, platTrae, err)
		return
	}
	total := len(trae.ModelList())
	writeCatalogResult(w, platTrae, total-before, total)
}

// ============ Trae 账号池操作（写时复制，与 openrouter 同口径） ============

func traeCloneConfig() (*trae.Config, *trae.Config) {
	cur := trae.Get()
	next := &trae.Config{
		Gateway:        cur.Gateway,
		Selection:      cur.Selection,
		AutoCheckin:    cur.AutoCheckin,
		MaxConcurrency: cur.MaxConcurrency,
		Retries:        cur.Retries,
	}
	if cur.Accounts != nil {
		next.Accounts = make([]*trae.Account, len(cur.Accounts))
		for i, a := range cur.Accounts {
			cp := *a
			next.Accounts[i] = &cp
		}
	}
	return cur, next
}

// traeAccountAt 按索引取账号（越界报错）。
func traeAccountAt(index int) (*trae.Config, *trae.Account, error) {
	cur := trae.Get()
	if index < 0 || index >= len(cur.Accounts) {
		return nil, nil, fmt.Errorf("account index out of range")
	}
	_, next := traeCloneConfig()
	return next, next.Accounts[index], nil
}

func traeSetAccountRouting(index int, enabled bool) error {
	next, a, err := traeAccountAt(index)
	if err != nil {
		return err
	}
	// 与 WorkBuddy 同款互锁：账号总闸关闭时反代开关不得单独打开
	if enabled && !a.EnabledOrDefault() {
		return fmt.Errorf("account switch is off: enable the account first, then the gateway switch")
	}
	f := enabled
	a.PoolEnabled = &f
	trae.Set(next)
	return nil
}

// traeSetAccountEnabled 账号总闸（对齐 WorkBuddy account-enabled）：关闭 = 该
// 账号不参与路由且签到/额度刷新等维护任务一并跳过；开启不联动改写路由开关
// （两持久化值独立，面板重开后自动恢复路由意愿）。
func traeSetAccountEnabled(index int, enabled bool) error {
	next, a, err := traeAccountAt(index)
	if err != nil {
		return err
	}
	f := enabled
	a.Enabled = &f
	trae.Set(next)
	return nil
}

func traeSetAllAccountsRouting(enabled bool) int {
	_, next := traeCloneConfig()
	n := 0
	for _, a := range next.Accounts {
		f := enabled
		a.PoolEnabled = &f
		n++
	}
	trae.Set(next)
	return n
}

func traeReorderAccounts(order []int) error {
	cur := trae.Get()
	n := len(cur.Accounts)
	if len(order) == 0 {
		return fmt.Errorf("order is required")
	}
	nextAccounts := make([]*trae.Account, 0, n)
	seen := make(map[int]bool, n)
	for _, idx := range order {
		if idx < 0 || idx >= n {
			return fmt.Errorf("account index out of range: %d", idx)
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		nextAccounts = append(nextAccounts, cur.Accounts[idx])
	}
	if len(nextAccounts) != n {
		for i, a := range cur.Accounts {
			if !seen[i] {
				nextAccounts = append(nextAccounts, a)
			}
		}
	}
	_, next := traeCloneConfig()
	next.Accounts = nextAccounts
	trae.Set(next)
	return nil
}

func traeDeleteAccount(index int) error {
	cur := trae.Get()
	if index < 0 || index >= len(cur.Accounts) {
		return fmt.Errorf("account index out of range")
	}
	_, next := traeCloneConfig()
	next.Accounts = append(next.Accounts[:index], next.Accounts[index+1:]...)
	trae.Set(next) // Set 内部 pruneRemovedAccounts 同步清理痕迹表的冷却/记账条目
	return nil
}

func traeSetAccountLabel(index int, label string) error {
	next, a, err := traeAccountAt(index)
	if err != nil {
		return err
	}
	a.Label = label
	trae.Set(next)
	return nil
}

func traeSetAccountProxy(index int, main, backup string) error {
	main = strings.TrimSpace(main)
	backup = strings.TrimSpace(backup)
	cur := trae.Get()
	if index < 0 || index >= len(cur.Accounts) {
		return fmt.Errorf("account index out of range")
	}
	acc := cur.Accounts[index]
	backup = resolveBackupSlot(backup, acc.ProxyBackup)
	if err := validateProxyBinding(main, backup); err != nil {
		return err
	}
	next, a, err := traeAccountAt(index)
	if err != nil {
		return err
	}
	if main == "" && backup == "" {
		a.ProxyMain = ""
		a.ProxyBackup = ""
	} else {
		a.ProxyMain = main
		a.ProxyBackup = backup
	}
	trae.Set(next)
	return nil
}

func traeClearAllAccountProxies() int {
	n := 0
	_, next := traeCloneConfig()
	for _, a := range next.Accounts {
		if a.ProxyMain != "" || a.ProxyBackup != "" {
			n++
		}
		a.ProxyMain = ""
		a.ProxyBackup = ""
	}
	trae.Set(next)
	return n
}

// traeTestAccount 单账号探测：GetUserInfo 验凭据 + 顺手抓额度快照。
// status ∈ active/cooldown/error。
func traeTestAccount(index int) (map[string]any, string) {
	cur := trae.Get()
	result := map[string]any{"index": index}
	if index < 0 || index >= len(cur.Accounts) {
		result["reason"] = "account index out of range"
		return result, "error"
	}
	acc := cur.Accounts[index]
	result["tokenMask"] = trae.MaskToken(acc.Token)
	if !acc.EnabledOrDefault() {
		result["reason"] = "account switch is off: enable the account first"
		return result, "error"
	}
	if !trae.TokenUsable(acc) {
		result["reason"] = "token expired and no refresh credential"
		return result, "error"
	}
	start := time.Now()
	screen, err := trae.GetUserInfo(acc)
	result["latencyMs"] = time.Since(start).Milliseconds()
	if err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "status 429") || trae.ContainsQuotaText(err.Error()) {
			if d := trae.Cooldown(acc.ID, time.Minute); d > 0 {
				if until, ok := trae.CooldownUntil(acc.ID); ok {
					result["cooldownUntil"] = until.UTC().Format(time.RFC3339)
					result["remaining"] = formatDuration(time.Until(until))
				}
			}
			result["reason"] = "rate limited: " + err.Error()
			return result, "cooldown"
		}
		result["reason"] = err.Error()
		return result, "error"
	}
	trae.Uncool(acc.ID)
	if screen != "" {
		result["screenName"] = screen
		// 首测且无备注：用上游 ScreenName 自动补显示名（与 cline 昵称同思路）
		if acc.Label == "" {
			_ = traeSetAccountLabel(index, screen)
			result["label"] = screen
		}
	}
	// GetUserInfo 只证明凭据活着，不证明模型可调——部分模型对低套餐账号
	// 返回 200 目录但请求必败（用户实测）。补发一条真实最小模型请求：
	// 能出字才算 active；模型级失败（1005/4023/4001）原样回报并走自动封禁判据。
	m, ok := trae.ProbeModel()
	if !ok {
		result["reason"] = "ok (userinfo) but no model available to probe; catalog empty"
		return result, "active"
	}
	pstart := time.Now()
	perr := traeProbeModelOnAccount(acc, m)
	result["probeModel"] = m.ID
	result["probeLatencyMs"] = time.Since(pstart).Milliseconds()
	if perr != nil {
		var he *trae.HTTPError
		if errors.As(perr, &he) {
			if he.RateLimited {
				result["reason"] = "userinfo ok, model probe rate limited (HTTP %d): %s"
				result["reason"] = fmt.Sprintf("userinfo ok, model probe rate limited (HTTP %d): %s", he.Status, kit.Truncate(he.Body, 200))
				return result, "cooldown"
			}
			traeAutoBanOnHTTPError(m, acc, perr)
			result["reason"] = fmt.Sprintf("userinfo ok but model probe failed (HTTP %d, code %s): %s", he.Status, he.Code, kit.Truncate(he.Body, 200))
			return result, "error"
		}
		// 网络错误：凭据本身没问题，仍算 active 但注明
		result["reason"] = "ok (userinfo); model probe network error: " + kit.Truncate(perr.Error(), 160)
		return result, "active"
	}
	result["reason"] = "ok"
	// 额度快照（best-effort，失败不影响探测结论）
	if cr, err := trae.FetchCredits(acc); err == nil && cr != nil {
		result["credits"] = map[string]any{
			"remaining": cr.Remaining, "limit": cr.Limit,
			"unlimited": cr.Unlimited, "identity": cr.Identity,
		}
	}
	return result, "active"
}

// traeProbeModelOnAccount 用固定账号发一条 "Reply with exactly: OK" 最小模型
// 请求（Test 探测的第二段）：与自动封禁复核同一路径。返回上游错误原样上抛；
// 能读到任何文本事件即视为可用。这也让 Test 的消耗进入 per-账号 token 记账
// （traeProbeFailsModel 内部走 trae.Chat 的记账通道之外，这里显式记录）。
func traeProbeModelOnAccount(acc *trae.Account, m trae.Model) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	params := map[string]any{
		"model":      trae.ModelPrefix + m.ID,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with exactly: OK"}},
		"max_tokens": 16,
	}
	stream, _, err := trae.Chat(ctx, trae.ChatOpts{Model: m, Params: params, PinAccount: acc})
	if err != nil {
		return err
	}
	defer stream.Close()
	for {
		ev, rerr := stream.Next()
		if ev != nil && ev.Type == "usage" && ev.Usage != nil && acc.ID != "" {
			trae.RecordAccountTokens(acc.ID, ev.Usage.Prompt+ev.Usage.Completion)
		}
		if rerr != nil || ev == nil {
			return nil // EOF/断流：请求本身通了（HTTP 200），按可用处理
		}
		switch ev.Type {
		case "text", "reasoning", "done":
			return nil
		case "error":
			if stream.Err != nil {
				return stream.Err
			}
			return fmt.Errorf("trae probe error event: %s", traeEventErrorText(ev))
		}
	}
}

// aggregateTraeCompletionResponse 非流式聚合并包成 JSON 伪响应
// （cross_endpoint 的 trae 非流式调用器用）。
func aggregateTraeCompletionResponse(ctx context.Context, params map[string]any, m trae.Model) (*http.Response, error) {
	completion, _, err := aggregateTraeCompletion(ctx, params, m)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(completion)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(b)),
	}, nil
}

// traeProbeBlacklistedModel 模型黑名单页的 trae 探测：取第一个可用账号对
// 指定模型发一个极小请求，验证模型是否真实可调。
func traeProbeBlacklistedModel(bare string) (map[string]any, string) {
	result := map[string]any{"ok": false, "model": trae.ModelPrefix + bare}
	m, ok := trae.Resolve(bare)
	if !ok {
		result["error"] = "unknown trae model: " + bare
		return result, "error"
	}
	params := map[string]any{
		"model":      trae.ModelPrefix + m.ID,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with exactly: OK"}},
		"max_tokens": 16,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	stream, acc, err := trae.Chat(ctx, trae.ChatOpts{
		Model: m, Params: params, Strategy: "fill", BoundRoutable: traeBoundRoutable,
	})
	if err != nil {
		result["error"] = err.Error()
		return result, "error"
	}
	defer stream.Close()
	sawDone := false
	for {
		ev, rerr := stream.Next()
		if rerr != nil || ev == nil {
			break
		}
		if ev.Type == "done" {
			sawDone = true
			break
		}
		if ev.Type == "error" {
			result["error"] = ev.Message
			return result, "error"
		}
	}
	result["latencyMs"] = time.Since(start).Milliseconds()
	if !sawDone {
		result["error"] = "upstream stream ended without a done event"
		return result, "error"
	}
	if acc != nil {
		trae.Uncool(acc.ID)
	}
	result["ok"] = true
	result["error"] = "ok"
	return result, "active"
}

// clearTraeAccountsForDataDelete 清空 Trae 账号池（保留网关等平台设置）；
// 使用痕迹（记账/签到/额度/冷却）一并清空（ClearUsage 即时落盘痕迹文件）。
func clearTraeAccountsForDataDelete() int {
	_, next := traeCloneConfig()
	n := len(next.Accounts)
	next.Accounts = nil
	trae.Set(next)
	trae.ClearUsage()
	return n
}

// ============ 账号总闸（对齐 WorkBuddy 双开关） ============

// POST /admin/api/trae/accounts/enabled  body: { index, enabled }
func handleTraeAccountSetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index   int  `json:"index"`
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := traeSetAccountEnabled(req.Index, req.Enabled); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("account #%d enabled=%v", req.Index+1, req.Enabled)})
}

// ============ 本机授权助手对接（对齐上游原版 web_login.py 架构） ============
//
// 授权页强依赖本机 helper（127.0.0.1:8765，脚本由本网关提供下载）。helper
// 收到授权回调后把凭据 POST 到本网关 /api/web-auth 入库（默认不参与路由）；
// 面板轮询最近入库状态展示结果。三个端点：
//   POST /api/web-auth            公开——helper 转发凭据入库（原版同路径）
//   GET  /trae/oauth/helper       公开——下载按本部署预填 relay 的助手脚本
//   GET  /admin/api/trae/helper/status  管理——最近一次入库结果

type traeHelperIngest struct {
	At      time.Time `json:"at"`
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Label   string    `json:"label"`
	Added   bool      `json:"added"`
}

var (
	traeLastIngestMu sync.Mutex
	traeLastIngest   *traeHelperIngest
)

// webAuthSourceAllowed /api/web-auth 的来源判定：回环、内网（RFC1918/ULA）、
// 链路本地放行；公网来源拒绝。RemoteAddr 无法解析时按拒绝处理（fail-closed）。
// CLIENT_IP_HEADER 反代头不采信——凭据写入端点信任的只能是 TCP 对端。
func webAuthSourceAllowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// POST /api/web-auth  body: helper 解析后的凭据 dict（token/refreshToken/userId/...）
// 响应契约与原版一致：{"success":true} / {"success":false,"error":"..."}。
//
// 源限制（2026-10-29 审计修复）：本端点无认证（helper 是裸脚本，加认证会破坏
// 与上游 web_login.py 的行级基线等价），但凭据写入绝不能对公网开放。设计场景
// 是 helper 与用户浏览器同网段：本机部署=127.0.0.1，NAS/LAN 部署=内网 PC 的
// RFC1918/ULA 地址——因此只放行回环+内网源，公网/反代外来源一律 403 fail-closed。
func handleTraeWebAuth(w http.ResponseWriter, r *http.Request) {
	if !webAuthSourceAllowed(r) {
		log.Printf("trae web-auth rejected: non-local source %s", r.RemoteAddr)
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": "web-auth endpoint only accepts local/LAN sources"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	var creds map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&creds); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	acc, added, err := trae.AddFromHelperCreds(creds)
	label := ""
	if acc != nil {
		label = acc.Label
		if label == "" {
			label = acc.ID
		}
	}
	rec := &traeHelperIngest{At: time.Now().UTC(), Success: err == nil, Label: label, Added: added}
	if err != nil {
		rec.Message = err.Error()
	} else if added {
		// 英文源文 + 面板 T() 词条（I18N_ZH）双语；这两条是固定串，可整句词条。
		rec.Message = "Credentials saved to server"
	} else {
		rec.Message = "Account already exists"
	}
	traeLastIngestMu.Lock()
	traeLastIngest = rec
	traeLastIngestMu.Unlock()
	if err != nil {
		log.Printf("trae web-auth ingest failed: %v", err)
	} else {
		log.Printf("trae web-auth ingest: account %q (%s), added=%v, routing disabled by default", label, acc.ID, added)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": err == nil, "error": rec.Message, "label": label})
}

// GET /trae/oauth/helper?as=py —— 下载按本部署预填 relay 的授权助手脚本。
// relay 取请求自身 Host（浏览器能打开面板就能运行助手连通网关）。
func handleTraeHelperDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	base := "http://" + r.Host
	// 反向代理/HTTPS 部署：X-Forwarded-Proto 优先
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		base = "https://" + r.Host
	}
	script := trae.HelperScript(base)
	w.Header().Set("Content-Type", "text/x-python; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=web_login.py")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

// GET /admin/api/trae/helper/status —— 最近一次 helper 入库结果。
func handleTraeHelperStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	traeLastIngestMu.Lock()
	rec := traeLastIngest
	traeLastIngestMu.Unlock()
	var data map[string]any
	if rec == nil {
		data = map[string]any{"seen": false}
	} else {
		data = map[string]any{
			"seen":    true,
			"at":      rec.At.Format(time.RFC3339),
			"success": rec.Success,
			"message": rec.Message,
			"label":   rec.Label,
			"added":   rec.Added,
		}
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// ============ 签到与额度后台任务 ============

// startTraeJobs 后台任务：额度定期刷新（30 分钟一轮）+ 自动签到巡检
// （每 30 分钟检查，AutoCheckin 开启时才真正执行）。首轮错开启动高峰。
// ONLY_ROUTER=true（2026-10-28 用户定）时整体不启动：仅会话路由，面板的
// 自动签到开关被强制覆盖；模型目录同步与请求路径不受影响。
func startTraeJobs() {
	if kit.OnlyRouter() {
		log.Printf("[only-router] ONLY_ROUTER=true: trae 额度刷新与自动签到任务已禁用（仅会话路由）")
		return
	}
	go func() {
		time.Sleep(2 * time.Minute)
		for {
			refreshAllTraeCredits()
			time.Sleep(30 * time.Minute)
		}
	}()
	go func() {
		time.Sleep(5 * time.Minute)
		for {
			if trae.Get().AutoCheckin != nil && *trae.Get().AutoCheckin {
				res := runTraeCheckinAll()
				if res.total > 0 {
					log.Printf("trae auto checkin: %d/%d ok, %d failed", res.ok, res.total, res.failed)
				}
			}
			time.Sleep(30 * time.Minute)
		}
	}()
}

// refreshAllTraeCredits 刷新全部账号总闸开启账号的额度快照（额度列与
// credit-priority 选号的数据源）。静默：失败只记日志摘要。
func refreshAllTraeCredits() int {
	accs := trae.Get().Accounts
	ok := 0
	for _, a := range accs {
		if a == nil || !a.EnabledOrDefault() || a.Token == "" {
			continue
		}
		if _, err := trae.FetchCredits(a); err == nil {
			ok++
		}
	}
	return ok
}

type traeCheckinResult struct {
	total   int
	ok      int
	already int
	failed  int
}

// runTraeCheckinAll 对账号总闸开启且今天未签的账号执行签到（串行）。
func runTraeCheckinAll() traeCheckinResult {
	var res traeCheckinResult
	for _, a := range trae.Get().Accounts {
		if a == nil || !a.EnabledOrDefault() || a.Token == "" {
			continue
		}
		res.total++
		if !trae.NeedsCheckin(a) {
			res.already++
			continue
		}
		if err := traeDoCheckin(a); err != nil {
			res.failed++
		} else {
			res.ok++
		}
	}
	return res
}

// traeDoCheckin 单账号签到：先 status 探测（上游 enable=false 说明活动未开），
// 再 claim。错误上抛给调用方计数。
func traeDoCheckin(a *trae.Account) error {
	enable, checkedIn, credits, err := trae.CheckinStatus(a)
	if err != nil {
		return err
	}
	if !enable {
		// 活动未开启：记状态（不算失败，但要清掉历史失败徽章，否则面板
		// 永远挂着上一次的"签到失败"）
		trae.RecordCheckinResult(a, false, 0, "check-in campaign not enabled for this account")
		return nil
	}
	if checkedIn {
		// 账号维度今天已签（status 口径）：落成功状态，清掉历史失败徽章。
		// 此前不落记录，导致上午已签成功、下午手动 claim 撞 9074 后徽章
		// 永远显示"签到失败"（2026-10-27 实测）。
		trae.RecordCheckinResult(a, true, credits, "")
		return nil
	}
	_, _, err = trae.CheckinClaim(a)
	return err
}

// POST /admin/api/trae/checkin  body: {} —— 手动全部签到
func handleTraeCheckin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	// ONLY_ROUTER=true：仅会话路由，手动签到同样拒绝（2026-10-28 用户定）。
	if kit.OnlyRouter() {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "maintenance tasks are disabled by ONLY_ROUTER (session routing only)"})
		return
	}
	res := runTraeCheckinAll()
	log.Printf("trae manual checkin: %d/%d ok, %d already, %d failed", res.ok, res.total, res.already, res.failed)
	writeAPI(w, http.StatusOK, apiResponse{
		Success: res.failed == 0,
		Message: fmt.Sprintf("checked in %d/%d", res.ok, res.total),
		Data:    map[string]any{"total": res.total, "ok": res.ok, "already": res.already, "failed": res.failed},
	})
}

// POST /admin/api/trae/credits/refresh —— 手动刷新全部余额
func handleTraeCreditsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	// ONLY_ROUTER=true：仅会话路由，手动额度刷新同样拒绝（2026-10-28 用户定）。
	if kit.OnlyRouter() {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "maintenance tasks are disabled by ONLY_ROUTER (session routing only)"})
		return
	}
	n := refreshAllTraeCredits()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("refreshed %d account(s)", n), Data: map[string]any{"refreshed": n}})
}

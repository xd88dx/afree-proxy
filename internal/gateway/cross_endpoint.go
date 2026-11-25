package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"afree-proxy/internal/freepi"
	"afree-proxy/internal/other"
)

// ============ /v1/responses 与 /v1/messages 的全平台接入 ============
//
// 此前这两个端点只路由 zen/workbuddy/cline，前缀平台（trae:/
// Other 子平台）进不去。本文件把"带平台前缀的模型"统一归口到一个
// chat 调用器：输入已归一的 OpenAI chat params，输出 OpenAI chat 形态响应
// （流式=完整 SSE、非流式=JSON），再经既有的 chat→responses / chat→anthropic
// 转换件回写给客户端。zen/workbuddy 仍走各自的专用路径（协议差异大）。

// platformChatCall 平台统一 chat 调用器：stream=true 时 Body 为 chat SSE，
// stream=false 时为聚合 JSON。调用器内部负责选号/出口解析/重试。
// 第二返回值是该次上游尝试的记账回调（key 已捕获在闭包里，传 usage 帧即记
// token；trae 在聚合内部自行记账，回调恒 nil）。stream=true 时由
// bufferedStreamCaller 在最终成功那次以末帧调用后不再外传（重试的中间尝试
// 不记账，回放路径也不逐帧重复记账）。
type platformChatCall func(ctx context.Context, params map[string]any, stream bool) (*http.Response, func(map[string]any), error)

// platformModelErr 未命中模型/未配置凭据类错误：对客户端 400/401 而非 502。
type platformModelErr struct {
	msg       string
	authError bool
}

func (e *platformModelErr) Error() string { return e.msg }

// resolvePlatformChatCaller 识别带平台前缀的模型并返回其 chat 调用器
// （已包缓冲重试语义）。未命中（走 zen/cline 既有路径）返回 ok=false。
func resolvePlatformChatCaller(model string) (platformChatCall, bool) {
	caller, ok := resolvePlatformChatCallerRaw(model)
	if !ok {
		return nil, false
	}
	return bufferedStreamCaller(caller), true
}

// resolvePlatformChatCallerRaw 未包缓冲的平台调用器（trae 聚合产物可能缺
// [DONE]，由外层 bufferedStreamCaller 统一判完成并重试）。
func resolvePlatformChatCallerRaw(model string) (platformChatCall, bool) {
	// TraeWork 路由（vendored trae2api）：域前缀模型名透传，进程内进
	// vendored /v1/chat/completions（OpenAI 原生 SSE，无需聚合翻译）。
	if caller, ok := trae2apiCaller(model); ok {
		return caller, true
	}
	// FreePI 路由（freepi: 前缀，单平台 key 池）：目录成员判定 + 有凭据闸。
	if bare, ok := freepi.StripPrefix(model); ok {
		if _, known := freepi.ResolveModel(bare); !known {
			return rejectCaller(fmt.Sprintf("model %q is not in the FreePI catalog — refresh the catalog on the FreePI page", bare), false), true
		}
		if len(freepi.Get().Keys) == 0 {
			return rejectCaller("No keys configured for FreePI. Add keys on the FreePI page.", true), true
		}
		return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, func(map[string]any), error) {
			return freepiChatCall(ctx, params, bare, stream)
		}, true
	}
	if p, bare, ok := other.ResolvePrefix(model); ok && p != nil {
		m, resolved := other.ResolveSelected(p, bare)
		if !resolved {
			return rejectCaller(fmt.Sprintf("model %q is not enabled on platform %q — tick it in the model selector on the Other page", bare, p.Name), false), true
		}
		if len(p.Keys) == 0 {
			return rejectCaller(fmt.Sprintf("No keys configured for platform %q. Add keys on the Other page.", p.Name), true), true
		}
		return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, func(map[string]any), error) {
			resp, _, err := other.Chat(ctx, p, params, m.ID, stream, globalStrategy(), otherBoundRoutable(p), otherExitFn(p))
			if err != nil {
				return nil, nil, err
			}
			usedKey := takeUsedKey(resp)
			return resp, func(u map[string]any) { other.RecordKeyTokens(p, usedKey, usageTokens(u, params)) }, nil
		}, true
	}
	return nil, false
}

// rejectCaller 固定失败的调用器（模型未命中/凭据未配置）。
func rejectCaller(msg string, authError bool) platformChatCall {
	return func(context.Context, map[string]any, bool) (*http.Response, func(map[string]any), error) {
		return nil, nil, &platformModelErr{msg: msg, authError: authError}
	}
}

// bufferedStreamCaller 流式调用加"缓冲+中断重试"（与 handleStreamBufferedGeneric
// 同口径）：完整缓冲上游 chat SSE，未见 finish/[DONE] 整次重试（平台内部
// 自选新账号/key，出口绑定语义不变）。非流式直通。ctx 取消不重试。
// 记账口径：只有最终成功这次以末帧 usage 调一次记账回调并置空外传——
// 重试的中间尝试不重复记账，回放路径（chatStreamToResponses 等）拿到的
// 回调为 nil，也不会逐帧重复记账。
func bufferedStreamCaller(caller platformChatCall) platformChatCall {
	return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, func(map[string]any), error) {
		if !stream {
			return caller(ctx, params, false)
		}
		var lastBrk error
		for n := 1; n <= StreamMaxAttempts(); n++ {
			resp, usageFn, err := caller(ctx, params, true)
			if err != nil {
				return nil, nil, err
			}
			lines, usage, complete, brk := bufferUpstreamSSE(resp.Body)
			resp.Body.Close()
			if complete {
				if usageFn != nil && usage != nil {
					usageFn(usage)
				}
				return sseLinesResponse(lines), nil, nil
			}
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			lastBrk = brk
		}
		if lastBrk != nil {
			return nil, nil, fmt.Errorf("upstream stream interrupted: %v", lastBrk)
		}
		return nil, nil, errors.New("upstream stream interrupted")
	}
}

// sseBytesResponse 把一段 OpenAI SSE 字节包成伪响应。
func sseBytesResponse(data []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

// sseLinesResponse 把缓冲的 SSE 行包成伪响应（保真回放）。
func sseLinesResponse(lines []string) *http.Response {
	var sb strings.Builder
	for _, ln := range lines {
		sb.WriteString(ln)
		if !strings.HasSuffix(ln, "\n") {
			sb.WriteString("\n")
		}
	}
	return sseBytesResponse([]byte(sb.String()))
}

// writePlatformCallError 平台调用错误的统一出口。
func writePlatformCallError(w http.ResponseWriter, err error) {
	var pme *platformModelErr
	if errors.As(err, &pme) {
		status := http.StatusBadRequest
		if pme.authError {
			status = http.StatusUnauthorized
		}
		writeJSON(w, status, map[string]any{
			"error": map[string]string{"message": pme.msg, "type": "invalid_request_error"},
		})
		return
	}
	log.Printf("  cross-endpoint chat error: %v", err)
	writeUpstreamAPIError(w, "", err, upstreamErrorStatus(err, http.StatusBadGateway))
}

// guardBufferedChatSSE 在向客户端写出 200 之前检查已缓冲的 OpenAI chat SSE。
// /v1/messages 与 /v1/responses 的流式出口此前没有这道闸——上游返回的坏
// tool_call（缺 command / file_path 等 required 字段）以 200 放行后，
// Claude Code 本地校验抛 InputValidationError，agent 任务静默终止。
// 上游响应已由 bufferedStreamCaller 完整缓冲，读走检查后还原 body 即可，
// 不改变后续实时转发语义。命中写 502 并返回 true。
func guardBufferedChatSSE(w http.ResponseWriter, resp *http.Response, params map[string]any) bool {
	if resp == nil || resp.Body == nil {
		return false
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return false
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	lines := strings.SplitAfter(string(body), "\n")
	bad := guardBadToolCalls(lines, params)
	if bad == "" {
		// freepi 账号共享/轮转拒绝是 200 SSE 正文，结构性检测抓不到；
		// 复用已缓冲的 lines 按文案判一次。
		bad = guardFreepiRejectionSSE(lines, params)
	}
	if bad == "" {
		return false
	}
	log.Printf("  cross-endpoint rejected, returning 502: %s", bad)
	writeJSON(w, http.StatusBadGateway, map[string]any{
		"error": map[string]any{"message": bad, "type": "upstream_error"},
	})
	return true
}

// ============ /v1/responses ============

// handleResponsesViaChat 前缀平台模型的 /v1/responses 入口：上游恒流式拿
// chat SSE（调用器已含缓冲重试），流式客户端经 chatStreamToResponses 重放，
// 非流式聚合后 chatToResponses 输出（与 zen 路径同一套转换件）。
// 记账：流式的已在 bufferedStreamCaller 以末帧完成（回调不外传）；非流式从
// 聚合 chat JSON 提 usage 记账。
func handleResponsesViaChat(w http.ResponseWriter, r *http.Request, chat map[string]any, model string, isStream bool, caller platformChatCall) {
	resp, usageFn, err := caller(r.Context(), chat, true)
	if err != nil {
		writePlatformCallError(w, err)
		return
	}
	defer resp.Body.Close()
	if isStream {
		if guardBufferedChatSSE(w, resp, chat) {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		chatStreamToResponses(w, resp, nil)
		return
	}
	raw, cerr := collectStreamResponse(resp)
	if cerr != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": cerr.Error(), "type": "api_error"},
		})
		return
	}
	if bad := guardBadToolCallsChat(raw, chat); bad != "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]any{"message": bad, "type": "upstream_error"},
		})
		return
	}
	if bad := guardFreepiRejectionChat(raw, chat); bad != "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]any{"message": bad, "type": "upstream_error"},
		})
		return
	}
	raw["model"] = model
	recordChatUsage(usageFn, raw)
	writeJSON(w, http.StatusOK, chatToResponses(normalizeOpenAIResponse(raw)))
}

// ============ /v1/messages ============

// handleAnthropicViaChat 前缀平台模型的 /v1/messages 入口：openAIReq 已由
// anthropicToOpenAI 归一；流式经 handleAnthropicStreamWithUsage 转发，非流式
// 聚合后 openAIToAnthropic 输出。记账口径与 handleResponsesViaChat 相同。
func handleAnthropicViaChat(w http.ResponseWriter, r *http.Request, req anthropicReq, openAIReq map[string]any, toolSchemas map[string]map[string]bool, caller platformChatCall) {
	if req.Stream {
		resp, _, err := caller(r.Context(), openAIReq, true)
		if err != nil {
			writePlatformCallError(w, err)
			return
		}
		defer resp.Body.Close()
		if guardBufferedChatSSE(w, resp, openAIReq) {
			return
		}
		// 末参 onUsage 传 nil：记账已在 bufferedStreamCaller 以末帧完成，
		// 回放路径不逐帧重复记账。
		handleAnthropicStreamWithUsage(w, resp, req.Model, toolSchemas, stopSequencesFrom(openAIReq), nil)
		return
	}
	resp, usageFn, err := caller(r.Context(), openAIReq, false)
	if err != nil {
		writePlatformCallError(w, err)
		return
	}
	defer resp.Body.Close()
	var chat map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": "upstream returned invalid JSON: " + err.Error(), "type": "parse_error"},
		})
		return
	}
	if bad := guardBadToolCallsChat(chat, openAIReq); bad != "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]any{"message": bad, "type": "upstream_error"},
		})
		return
	}
	if bad := guardFreepiRejectionChat(chat, openAIReq); bad != "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]any{"message": bad, "type": "upstream_error"},
		})
		return
	}
	recordChatUsage(usageFn, chat)
	finishAnthropicNonStreamFromChat(w, chat, req.Model, openAIReq)
}

// recordChatUsage 非流式路径记账：usageFn 由平台调用器给出，usage 从聚合的
// chat JSON 提取；两者任一缺失即跳过（trae 内部已记账，usageFn 恒 nil）。
func recordChatUsage(usageFn func(map[string]any), chat map[string]any) {
	if usageFn == nil || chat == nil {
		return
	}
	if u, ok := chat["usage"].(map[string]any); ok {
		usageFn(u)
	}
}

// finishAnthropicNonStreamFromChat 聚合 chat JSON → Anthropic 响应（与
// finishZenAnthropicNonStream 同一套转换，模型 ID 由调用方给定）。
func finishAnthropicNonStreamFromChat(w http.ResponseWriter, chat map[string]any, model string, openAIReq map[string]any) {
	chat["model"] = model
	chat = normalizeOpenAIResponse(chat)
	truncateAtStopSequences(chat, stopSequencesFrom(openAIReq))
	anthropicResp := openAIToAnthropic(chat)
	if tc, ok := getNested(chat, "choices", 0, "message", "tool_calls").([]any); ok && len(tc) > 0 {
		anthropicResp["stop_reason"] = "tool_use"
	}
	writeJSON(w, http.StatusOK, anthropicResp)
}

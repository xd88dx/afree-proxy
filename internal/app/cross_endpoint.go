package app

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

	"afree-proxy/internal/amd"
	openrouter "afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/tokenharbor"
)

// ============ /v1/responses 与 /v1/messages 的全平台接入 ============
//
// 此前这两个端点只路由 zen/workbuddy/cline，前缀平台（oprt:/amd:/tkhb:/
// trae:/Other 子平台）进不去。本文件把"带平台前缀的模型"统一归口到一个
// chat 调用器：输入已归一的 OpenAI chat params，输出 OpenAI chat 形态响应
// （流式=完整 SSE、非流式=JSON），再经既有的 chat→responses / chat→anthropic
// 转换件回写给客户端。zen/workbuddy 仍走各自的专用路径（协议差异大）。

// platformChatCall 平台统一 chat 调用器：stream=true 时 Body 为 chat SSE，
// stream=false 时为聚合 JSON。调用器内部负责选号/出口解析/重试。
type platformChatCall func(ctx context.Context, params map[string]any, stream bool) (*http.Response, error)

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
	if id, ok := openrouter.StripPrefix(model); ok {
		m, resolved := openrouter.Resolve(id)
		if !resolved {
			return rejectCaller(fmt.Sprintf("model %q is not a free openrouter model", id), false), true
		}
		return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, error) {
			resp, _, err := openrouter.Chat(ctx, params, m.ID, stream, getProxyConfig().Strategy, orBoundRoutable, nil)
			return resp, err
		}, true
	}
	if id, ok := amd.StripPrefix(model); ok {
		m, resolved := amd.Resolve(id)
		if !resolved {
			return rejectCaller(fmt.Sprintf("model %q is not an AMDCloud model", id), false), true
		}
		return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, error) {
			resp, _, err := amd.Chat(ctx, params, m.ID, stream, getProxyConfig().Strategy, amdBoundRoutable)
			return resp, err
		}, true
	}
	if id, ok := tokenharbor.StripPrefix(model); ok {
		m, resolved := tokenharbor.Resolve(id)
		if !resolved {
			return rejectCaller(fmt.Sprintf("model %q is not a free TokenHarbor model", id), false), true
		}
		return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, error) {
			resp, _, err := tokenharbor.Chat(ctx, params, m.ID, stream, getProxyConfig().Strategy, thBoundRoutable)
			return resp, err
		}, true
	}
	if id, ok := trae.StripPrefix(model); ok {
		m, resolved := trae.Resolve(id)
		if !resolved {
			return rejectCaller(fmt.Sprintf("model %q is not a known trae model", id), false), true
		}
		if len(trae.AccountIDs()) == 0 {
			return rejectCaller("No Trae accounts configured. Add accounts on the Trae page.", true), true
		}
		return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, error) {
			// 上游恒流式：聚合为 OpenAI chat SSE；缺 done 的断流产物原样
			// 返回（无 error），由 bufferedStreamCaller 判完成并整体重试。
			if !stream {
				return aggregateTraeCompletionResponse(ctx, params, m)
			}
			data, _, _, err := aggregateTraeSSE(ctx, params, m)
			if err != nil {
				return nil, err
			}
			return sseBytesResponse(data), nil
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
		return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, error) {
			resp, _, err := other.Chat(ctx, p, params, m.ID, stream, getProxyConfig().Strategy, otherBoundRoutable(p), otherExitFn(p))
			return resp, err
		}, true
	}
	return nil, false
}

// rejectCaller 固定失败的调用器（模型未命中/凭据未配置）。
func rejectCaller(msg string, authError bool) platformChatCall {
	return func(context.Context, map[string]any, bool) (*http.Response, error) {
		return nil, &platformModelErr{msg: msg, authError: authError}
	}
}

// bufferedStreamCaller 流式调用加"缓冲+中断重试"（与 handleStreamBufferedGeneric
// 同口径）：完整缓冲上游 chat SSE，未见 finish/[DONE] 整次重试（平台内部
// 自选新账号/key，出口绑定语义不变）。非流式直通。ctx 取消不重试。
func bufferedStreamCaller(caller platformChatCall) platformChatCall {
	return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, error) {
		if !stream {
			return caller(ctx, params, false)
		}
		var lastBrk error
		for n := 1; n <= StreamMaxAttempts(); n++ {
			resp, err := caller(ctx, params, true)
			if err != nil {
				return nil, err
			}
			lines, _, complete, brk := bufferUpstreamSSE(resp.Body)
			resp.Body.Close()
			if complete {
				return sseLinesResponse(lines), nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastBrk = brk
		}
		if lastBrk != nil {
			return nil, fmt.Errorf("upstream stream interrupted: %v", lastBrk)
		}
		return nil, errors.New("upstream stream interrupted")
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
	writeJSON(w, http.StatusBadGateway, map[string]any{
		"error": map[string]string{"message": err.Error(), "type": "api_error"},
	})
}

// ============ /v1/responses ============

// handleResponsesViaChat 前缀平台模型的 /v1/responses 入口：上游恒流式拿
// chat SSE（调用器已含缓冲重试），流式客户端经 chatStreamToResponses 重放，
// 非流式聚合后 chatToResponses 输出（与 zen 路径同一套转换件）。
func handleResponsesViaChat(w http.ResponseWriter, r *http.Request, chat map[string]any, model string, isStream bool, caller platformChatCall) {
	resp, err := caller(r.Context(), chat, true)
	if err != nil {
		writePlatformCallError(w, err)
		return
	}
	defer resp.Body.Close()
	if isStream {
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
	raw["model"] = model
	writeJSON(w, http.StatusOK, chatToResponses(normalizeOpenAIResponse(raw)))
}

// ============ /v1/messages ============

// handleAnthropicViaChat 前缀平台模型的 /v1/messages 入口：openAIReq 已由
// anthropicToOpenAI 归一；流式经 handleAnthropicStreamWithUsage 转发，非流式
// 聚合后 openAIToAnthropic 输出。
func handleAnthropicViaChat(w http.ResponseWriter, r *http.Request, req anthropicReq, openAIReq map[string]any, toolSchemas map[string]map[string]bool, caller platformChatCall) {
	if req.Stream {
		resp, err := caller(r.Context(), openAIReq, true)
		if err != nil {
			writePlatformCallError(w, err)
			return
		}
		defer resp.Body.Close()
		handleAnthropicStreamWithUsage(w, resp, req.Model, toolSchemas, stopSequencesFrom(openAIReq), nil)
		return
	}
	resp, err := caller(r.Context(), openAIReq, false)
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
	finishAnthropicNonStreamFromChat(w, chat, req.Model, openAIReq)
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

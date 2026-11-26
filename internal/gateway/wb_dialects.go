package gateway

// WorkBuddy 方言域（gateway 批自 app/wb_handlers.go 迁入）：vendored 子系统的
// anthropic/responses 方言适配；面板 handler 留装配根。

import (
	"encoding/json"
	"io"
	"net/http"

	"afree-proxy/internal/kit"
	"afree-proxy/internal/workbuddy"
)

func writeWorkbuddyAnthropicError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "api_error",
			"message": message,
		},
	})
}

// wbDialects 承载 anthropic/responses 方言适配方法（依赖网关翻译域符号，
// 留装配根；WorkbuddySubsystem 嵌入保方法形态与调用点最小改动）。
type wbDialects struct{ *workbuddy.WorkbuddySubsystem }

func (h wbDialects) ServeAnthropicMessages(rw http.ResponseWriter, r *http.Request, req anthropicReq, openAIReq map[string]any, toolSchemas map[string]map[string]bool) {
	body, err := json.Marshal(openAIReq)
	if err != nil {
		writeWorkbuddyAnthropicError(rw, http.StatusBadRequest, "encode request: "+err.Error())
		return
	}
	resp, cleanup, err := h.ServeWorkBuddyChatHTTP(r.Context(), r, body)
	if err != nil {
		writeWorkbuddyAnthropicError(rw, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer cleanup()
	if resp.StatusCode >= http.StatusBadRequest {
		writeWorkbuddyAnthropicError(rw, resp.StatusCode, workbuddy.WorkbuddyErrorText(resp.Body))
		return
	}

	if req.Stream {
		// 实时转发（2026-11-27 审计 G3 注记）：vendor SSE 逐帧直转，结构性
		// 无法在写 200 前过坏 tool_call 闸；非流式分支下方已接闸。
		handleAnthropicStreamWithUsage(rw, resp, req.Model, toolSchemas, stopSequencesFrom(openAIReq), nil)
		return
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		writeWorkbuddyAnthropicError(rw, http.StatusBadGateway, "decode WorkBuddy response: "+err.Error())
		return
	}
	out = normalizeOpenAIResponse(out)
	truncateAtStopSequences(out, stopSequencesFrom(openAIReq))
	// 坏 tool_call 闸（2026-11-27 审计 G3）：wb /v1/messages 非流式补接
	//（openAIReq 已在入口就地改名，传 nil）。
	if bad := guardBadToolCallsChat(out, openAIReq, nil); bad != "" {
		writeWorkbuddyAnthropicError(rw, http.StatusBadGateway, bad)
		return
	}
	anthropicResp := openAIToAnthropic(out)
	if tc, ok := getNested(out, "choices", 0, "message", "tool_calls").([]any); ok && len(tc) > 0 {
		anthropicResp["stop_reason"] = "tool_use"
	}
	writeJSON(rw, http.StatusOK, anthropicResp)
}
func (h wbDialects) ServeResponses(rw http.ResponseWriter, r *http.Request, chat map[string]any, isStream bool) {
	body, err := json.Marshal(chat)
	if err != nil {
		writeJSON(rw, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	resp, cleanup, err := h.ServeWorkBuddyChatHTTP(r.Context(), r, body)
	if err != nil {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	defer cleanup()
	if resp.StatusCode >= http.StatusBadRequest {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(rw, resp.Body)
		return
	}

	if isStream {
		// 实时转发（2026-11-27 审计 G3 注记）：vendor SSE 逐帧转换，结构性
		// 无法在写 200 前过坏 tool_call 闸；非流式分支下方已接闸。
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.Header().Set("Cache-Control", "no-cache")
		rw.Header().Set("Connection", "keep-alive")
		rw.Header().Set("Access-Control-Allow-Origin", "*")
		rw.WriteHeader(http.StatusOK)
		chatStreamToResponses(rw, resp, nil)
		return
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		writeJSON(rw, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if data, ok := out["data"].(map[string]any); ok {
		out = data
	}
	// 坏 tool_call 闸（2026-11-27 审计 G3）：wb /v1/responses 非流式补接
	//（chat 参数已在入口就地改名，传 nil）。
	if bad := guardBadToolCallsChat(out, chat, nil); bad != "" {
		writeJSON(rw, http.StatusBadGateway, map[string]any{
			"error": map[string]any{"message": kit.Truncate(bad, 300), "type": "upstream_error"},
		})
		return
	}
	writeJSON(rw, http.StatusOK, chatToResponses(normalizeOpenAIResponse(out)))
}

// freepi_error.go 识别 freepi 上游的「账号共享/轮转」拒绝文案并改判 502。
//
// 背景（2026-11-25）：freepi 上游把「同一 messages 换 Bearer」判为账号共享
// 时，返回的是 HTTP 200 的正常 SSE 正文（delta.content 里一段英文句子），
// 不带 error 键、没有非 2xx 状态码。网关现有 error 检测全是结构性的
// （状态码 / error 帧 / 工具参数形状），这段文本能穿透到客户端，agent 把它
// 当一次正常回答，任务就此意外结束。
//
// 判据刻意收窄：两句固定文案必须同时命中，且只在 freepi 平台启用——避免
// 正文里泛泛讨论 "account" / "sharing" 的正常回答被误判。命中在写 200 之前，
// 网关尚未提交任何字节，语义干净。
package gateway

import (
	"encoding/json"
	"strings"

	"afree-proxy/internal/freepi"
)

const (
	freepiShareMarker1 = "this conversation belongs to another free-pi account"
	freepiShareMarker2 = "sharing or rotating accounts isn't allowed"
)

// isFreepiAccountSharingRejection 判断聚合文本是否为 freepi 账号共享/轮转拒绝。
// 两句固定文案同时命中才判，大小写不敏感。
func isFreepiAccountSharingRejection(text string) bool {
	if text == "" {
		return false
	}
	low := strings.ToLower(text)
	return strings.Contains(low, freepiShareMarker1) && strings.Contains(low, freepiShareMarker2)
}

// freepiModelParams 判断请求是否走 freepi 平台（model 带 freepi: 前缀）。
// 跨端点 /v1/messages、/v1/responses 的 openAIReq 由 req.Model 转换而来，
// 前缀保留，可据此收窄判据，不误伤其它平台。
func freepiModelParams(params map[string]any) bool {
	if params == nil {
		return false
	}
	model, _ := params["model"].(string)
	return strings.HasPrefix(strings.TrimSpace(model), freepi.ModelPrefix+":")
}

// guardFreepiRejectionChat 对聚合后的 chat 对象检查 freepi 拒绝文案。
// 非流式出口在写 200 之前调用；命中返回对外可见原因。
func guardFreepiRejectionChat(chat map[string]any, params map[string]any) string {
	if !freepiModelParams(params) {
		return ""
	}
	msg, _ := getNested(chat, "choices", 0, "message").(map[string]any)
	if msg == nil {
		return ""
	}
	text, _ := msg["content"].(string)
	if isFreepiAccountSharingRejection(text) {
		return "上游 freepi 拒绝：账号共享/轮转（conversation belongs to another free-pi account）"
	}
	return ""
}

// guardFreepiRejectionSSE 对已缓冲的 SSE lines 检查 freepi 拒绝文案。
// 文案可能被上游切成多个 delta.content 分片，须拼接后整体判定。
func guardFreepiRejectionSSE(lines []string, params map[string]any) string {
	if !freepiModelParams(params) || len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(ln, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(payload), &obj) != nil {
			continue
		}
		choices, _ := obj["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			continue
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		if c, ok := delta["content"].(string); ok {
			b.WriteString(c)
		}
	}
	if isFreepiAccountSharingRejection(b.String()) {
		return "上游 freepi 拒绝：账号共享/轮转（conversation belongs to another free-pi account）"
	}
	return ""
}

package app

// 统一上下文溢出报错（2026-11-05 用户定）：
//
// 各上游对"输入超过模型上下文窗口"的报错五花八门（OpenAI 系 "maximum
// context length"、Anthropic 系 "prompt is too long"、413、中文文案……），
// 且网关此前一律包成 502 api_error——IDE/agent 无法区分"网关坏了"（要重试）
// 和"上下文满了"（要压缩后继续），用户也看不出该做什么。
//
// 这里在错误出口做签名识别：命中溢出签名的上游错误改写为统一信封——
//
//	HTTP 400
//	{"error":{"type":"invalid_request_error","code":"context_length_exceeded",
//	          "message":"Context length exceeded: ... Compress or truncate ...",
//	          "upstream_message":"<上游原文截断>"}}
//
// message 保留 "context length" / "compress" 等标准关键词：主流客户端
// （Cursor / Cline / Continue 等）按这些词正则触发自动压缩续跑；`code` 供
// 程序化判别。其余错误维持各调用点原有状态码与文案，行为不变。

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"afree-proxy/internal/kit"
)

// contextOverflowPatterns 上下文溢出签名（对上游错误全文做不区分大小写匹配）。
// 覆盖 OpenAI 兼容系、Anthropic 系、HTTP 413 文案与中文上游；场景矩阵实测
// 发现新形态时在此追加。
var contextOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)maximum context length`),
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),
	regexp.MustCompile(`(?i)context (length|window) (is )?(exceeded|too (long|large)|overflow)`),
	regexp.MustCompile(`(?i)(input|prompt).{0,40}(exceeds?|too long).{0,40}(context|token)`),
	regexp.MustCompile(`(?i)exceeds? the (maximum )?(context|token)`),
	regexp.MustCompile(`(?i)exceeds?.{0,40}context (window|length|limit)`),
	regexp.MustCompile(`(?i)(context|prompt) is too long`),
	regexp.MustCompile(`(?i)too many (input |prompt )?tokens`),
	regexp.MustCompile(`(?i)request entity too large`),
	regexp.MustCompile(`(?i)payload too large`),
	regexp.MustCompile(`(?i)reduce the (length|number) (of|…)`),
	regexp.MustCompile(`超过.{0,12}上下文`),
	regexp.MustCompile(`上下文.{0,8}(超限|过长|已满|超出|长度上限)`),
	regexp.MustCompile(`(?i)exceeds? .*context limit`),
	regexp.MustCompile(`(?i)exceeds? .*token limit`),
}

// isContextOverflow 上游错误文本是否命中上下文溢出签名。
func isContextOverflow(msg string) bool {
	for _, re := range contextOverflowPatterns {
		if re.MatchString(msg) {
			return true
		}
	}
	return false
}

// contextOverflowTruncate 上游原文进 upstream_message 前的截断上限。
const contextOverflowTruncate = 500

// contextOverflowEnvelope 生成统一溢出信封。model 用于文案指认；可为空
// （部分路径拿不到模型名）。
func contextOverflowEnvelope(model, upstreamMsg string) map[string]any {
	msg := "Context length exceeded"
	if model != "" {
		msg += fmt.Sprintf(": the request for model %q is larger than the model's context window", model)
	}
	msg += ". Compress or truncate the conversation (summarize or drop old messages / tool results) and retry."
	return map[string]any{
		"error": map[string]any{
			"message":          msg,
			"type":             "invalid_request_error",
			"code":             "context_length_exceeded",
			"upstream_message": kit.Truncate(strings.TrimSpace(upstreamMsg), contextOverflowTruncate),
		},
	}
}

// writeContextOverflowError 输出统一溢出信封（HTTP 400）。
func writeContextOverflowError(w http.ResponseWriter, model, upstreamMsg string) {
	writeJSON(w, http.StatusBadRequest, contextOverflowEnvelope(model, upstreamMsg))
}

// writeUpstreamAPIError 上游调用错误的统一出口：溢出签名改写为 400
// context_length_exceeded，其余按 fallbackStatus 原样透传（api_error）。
// 各调用点原状态码语义不变，仅溢出形态被归一。
// upstreamErrorStatus 上游错误的对外状态码：错误自带 429 时原样透传，其余按
// fallback（各调用点既有口径）。限流是"该凭据用完了，等会儿再来"，客户端会按
// Retry-After 退避重试；包成 502 会让 IDE 判定网关故障并标记渠道不可用（zen 侧
// 早已如此，2026-11-09 收口到全平台）。提取靠上游错误类型的 HTTPStatus() 方法
// （amd/openrouter/tokenharbor/other/zen/cline 与跨端点调用器通用）。
// upstreamHTTPError 带状态码的上游错误：供调用器把非 200 响应转成 error（而不是
// 让伪成功的响应体流进正常解析路径），状态码经 HTTPStatus() 被统一提取。
type upstreamHTTPError struct {
	Status int
	Body   string
}

func (e *upstreamHTTPError) Error() string {
	return fmt.Sprintf("upstream API %d: %s", e.Status, kit.Truncate(e.Body, 500))
}

func (e *upstreamHTTPError) HTTPStatus() int { return e.Status }

func upstreamErrorStatus(err error, fallback int) int {
	var sc interface{ HTTPStatus() int }
	if errors.As(err, &sc) && sc.HTTPStatus() == http.StatusTooManyRequests {
		return http.StatusTooManyRequests
	}
	return fallback
}

func writeUpstreamAPIError(w http.ResponseWriter, model string, err error, fallbackStatus int) {
	msg := err.Error()
	if isContextOverflow(msg) {
		log.Printf("  context overflow normalized (model=%q): %s", model, kit.Truncate(msg, 200))
		writeContextOverflowError(w, model, msg)
		return
	}
	writeJSON(w, fallbackStatus, map[string]any{
		"error": map[string]any{"message": msg, "type": "api_error"},
	})
}

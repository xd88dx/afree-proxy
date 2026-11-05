package app

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// 统一上下文溢出报错回归（2026-11-05 用户定）：
//   - 各上游溢出文案（OpenAI/Anthropic/413/中文）命中签名 → 统一改写
//     400 + code=context_length_exceeded，message 带压缩提示关键词；
//   - 非溢出错误维持原状态码与文案（api_error）透传，行为不变。

func TestIsContextOverflowSignatures(t *testing.T) {
	yes := []string{
		`API 400: {"error":{"message":"This model's maximum context length is 131072 tokens. However, you requested 200000 tokens (150000 in the messages, 50000 in the completion).","code":"context_length_exceeded"}}`,
		`API 400: {"error":{"message":"prompt is too long: 250000 tokens > 200000 maximum"}}`,
		`API 413: request entity too large`,
		`API 400: {"error":"输入超过模型上下文长度上限，请压缩后重试"}`,
		`API 400: {"error":{"message":"Input length and max_tokens exceed context limit"}}`,
		`API 400: {"error":{"message":"Request too large: too many input tokens"}}`,
		`API 400: {"error":{"message":"your request exceeds the model's context window"}}`,
	}
	for _, m := range yes {
		if !isContextOverflow(m) {
			t.Errorf("want overflow match: %.80s", m)
		}
	}
	no := []string{
		`API 403: {"error":{"message":"Inference is blocked on this account"}}`,
		`API 400: {"error":"invalid model format. Expected format: modelType/model"}`,
		`API 429: rate limited`,
		`API 503: upstream overloaded`,
		`network error: EOF`,
		`API 400: {"error":{"message":"max_tokens is too large: 32"}}`, // max_tokens 边界 ≠ 上下文溢出
	}
	for _, m := range no {
		if isContextOverflow(m) {
			t.Errorf("want NO overflow match: %.80s", m)
		}
	}
}

func TestWriteUpstreamAPIErrorNormalizesOverflow(t *testing.T) {
	w := httptest.NewRecorder()
	writeUpstreamAPIError(w, "oprt:deepseek-v4.1:free",
		errors.New(`API 400: {"error":{"message":"This model's maximum context length is 131072 tokens"}}`),
		502)
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`"code":"context_length_exceeded"`, "Compress or truncate", "oprt:deepseek-v4.1:free", "maximum context length"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

func TestWriteUpstreamAPIErrorPassesThroughOthers(t *testing.T) {
	w := httptest.NewRecorder()
	writeUpstreamAPIError(w, "amd:X", errors.New(`API 403: {"error":{"message":"blocked"}}`), 502)
	if w.Code != 502 {
		t.Fatalf("status = %d, want 502 (unchanged)", w.Code)
	}
	if !strings.Contains(w.Body.String(), `blocked`) {
		t.Fatalf("upstream message should pass through: %s", w.Body.String())
	}
}

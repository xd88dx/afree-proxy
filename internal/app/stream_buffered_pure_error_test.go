package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 纯 error 帧缓冲改判 502（2026-11-11 用户定）：缓冲完整但无任何正文时，
// afree 尚未向客户端提交字节——返回真实 502 而非"200+error 帧"伪成功，
// 日志状态列与 agent 侧失败语义对齐。
func TestBufferedPureErrorFrameBecomes502(t *testing.T) {
	sse := strings.Join([]string{
		": keepalive",
		`data: {"error":{"message":"traectl: 上游 /api/agent/v3/create_agent_task HTTP 400: {\"code\":4001}","type":"upstream_error"}}`,
		"data: [DONE]",
		"",
	}, "\n")
	attempt := func(ctx context.Context) (*http.Response, func(map[string]any), error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(sse))}, nil, nil
	}
	rec := httptest.NewRecorder()
	handleStreamBufferedGeneric(rec, httptest.NewRequest("POST", "/v1/chat/completions", nil), "trae",
		map[string]any{"model": "tacn:glm-5.3-flash", "stream": true}, attempt)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("纯 error 帧缓冲应改判 502，得到 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "4001") {
		t.Fatalf("502 体应携带帧内错误信息: %s", body)
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "event-stream") {
		t.Fatalf("改判后不应回 SSE: %s", ct)
	}
}

// 残余形态：先吐过正文才报错——仍回放 200 + error 帧（半截结果对客户端有效），
// 面板以琥珀色区分。
func TestBufferedPartialContentWithErrorStillReplayed(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"部分正文"}}]}`,
		`data: {"error":{"message":"mid-stream failure"}}`,
		"data: [DONE]",
		"",
	}, "\n")
	attempt := func(ctx context.Context) (*http.Response, func(map[string]any), error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(sse))}, nil, nil
	}
	rec := httptest.NewRecorder()
	handleStreamBufferedGeneric(rec, httptest.NewRequest("POST", "/v1/chat/completions", nil), "trae",
		map[string]any{"model": "m", "stream": true}, attempt)
	if rec.Code != http.StatusOK {
		t.Fatalf("有正文的缓冲应回放 200，得到 %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "event-stream") {
		t.Fatalf("应回 SSE: %s", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "部分正文") || !strings.Contains(body, "mid-stream failure") {
		t.Fatalf("正文与 error 帧都应原样回放: %s", body)
	}
}

// role 占位块（content 为空串）与 usage 帧不算正文：纯 error 帧前带这些
// 修饰帧时仍应改判 502。
func TestBufferedRoleAndUsageFramesDoNotCountAsContent(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","content":""}}]}`,
		`data: {"usage":{"prompt_tokens":10,"completion_tokens":0}}`,
		`data: {"error":{"message":"context too long"}}`,
		"data: [DONE]",
		"",
	}, "\n")
	attempt := func(ctx context.Context) (*http.Response, func(map[string]any), error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(sse))}, nil, nil
	}
	rec := httptest.NewRecorder()
	handleStreamBufferedGeneric(rec, httptest.NewRequest("POST", "/v1/chat/completions", nil), "trae",
		map[string]any{"model": "m", "stream": true}, attempt)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("role/usage 修饰帧不算正文，应改判 502，得到 %d", rec.Code)
	}
}

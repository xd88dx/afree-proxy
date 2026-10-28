package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// usageTokens：上游给了 usage 按它记；上游没给（含 nil）时退回入站请求估算，
// 不能记成 0（2026-10-29）。
func TestUsageTokensFallsBackToRequestEstimate(t *testing.T) {
	params := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello world"}}}
	if got := usageTokens(map[string]any{"prompt_tokens": float64(18), "completion_tokens": float64(16)}, params); got != 34 {
		t.Fatalf("usage from upstream = %d, want 34", got)
	}
	if got := usageTokens(nil, params); got <= 0 {
		t.Fatal("nil usage must fall back to the inbound request estimate")
	}
	if got := usageTokens(map[string]any{}, params); got <= 0 {
		t.Fatal("empty usage must fall back to the inbound request estimate")
	}
}

// 非流式响应为 {data:{...usage}} 包装形态时，必须解包后再记账（2026-10-29）。
// 修复前在解包前取 raw["usage"]，包装形态下取不到 → 该次消耗恒为 0。
func TestNonStreamUsageRecordsWrappedData(t *testing.T) {
	body, err := json.Marshal(map[string]any{"data": map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
		"usage":   map[string]any{"prompt_tokens": 18, "completion_tokens": 16},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var got int64
	upstream := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	handleNonStreamResponseWithUsage(httptest.NewRecorder(), upstream, func(u map[string]any) {
		got = usageTokens(u, map[string]any{"messages": []any{map[string]any{"role": "user", "content": "x"}}})
	}, nil)
	if got != 34 {
		t.Fatalf("wrapped usage must be recorded, got %d, want 34", got)
	}
}

// 上游完全没给 usage：记账回调仍要被调用，走估算兜底而不是记 0。
func TestNonStreamUsageFallsBackWhenAbsent(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got int64
	called := false
	upstream := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	handleNonStreamResponseWithUsage(httptest.NewRecorder(), upstream, func(u map[string]any) {
		called = true
		got = usageTokens(u, map[string]any{"messages": []any{map[string]any{"role": "user", "content": "x"}}})
	}, nil)
	if !called {
		t.Fatal("usage callback must be invoked even when upstream sends no usage")
	}
	if got <= 0 {
		t.Fatalf("absent usage must fall back to the estimate, got %d", got)
	}
}

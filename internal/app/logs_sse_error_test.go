package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 伪成功治理回归（2026-11-10）：trae 流中断等形态 HTTP 层恒 200，失败信息
// 藏在 SSE error 帧里——日志如果只看状态码就是"成功"，与 agent 实际失败
// 对不上。statusWriter 扫描 error 帧填 Error 列 + errors 计数。
func TestStatusWriterCapturesSSEErrorFrame(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "chat error frame (trae mid-stream abort)",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n" +
				"data: {\"error\":{\"message\":\"upstream stream interrupted: connection reset\",\"type\":\"upstream_error\"}}\n\n" +
				"data: [DONE]\n\n",
			want: true,
		},
		{
			name: "anthropic error event",
			body: "event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
				"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"overloaded\"}}\n\n",
			want: true,
		},
		{
			name: "clean stream with usage tail",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1}}\n\ndata: [DONE]\n\n",
			want: false,
		},
		{
			name: "error frame split across writes",
			body: "data: {\"error\":{\"mess",
			want: false, // 残行未闭合且没到 \n：不误判（等下一片，本例没有下一片）
		},
	}
	for _, c := range cases {
		sw := &statusWriter{ResponseWriter: httptest.NewRecorder(), start: time.Now()}
		// 模拟真实链路：先 WriteHeader(200)（流式响应头），再分两次写 body
		//（错误帧可能跨 Write 到达，验证残行拼接）。
		sw.WriteHeader(http.StatusOK)
		half := len(c.body) / 2
		sw.Write([]byte(c.body[:half]))
		sw.Write([]byte(c.body[half:]))
		got := sw.errorReason() != ""
		if got != c.want {
			t.Errorf("%s: errorReason=%q, want captured=%v", c.name, sw.errorReason(), c.want)
		}
	}
}

// 200+error 帧计入 errors 计数（route stats）。
func TestSSEErrorCountsAsError(t *testing.T) {
	route := "trae"

	recordRouteStat(route, http.StatusOK, 100)
	sw := &statusWriter{ResponseWriter: httptest.NewRecorder(), start: time.Now()}
	sw.WriteHeader(http.StatusOK)
	sw.Write([]byte("data: {\"error\":{\"message\":\"mid-stream failure\"}}\n\n"))
	if sw.sseError == "" {
		t.Fatal("error frame not captured")
	}
	if sw.status == http.StatusOK && sw.sseError != "" {
		recordRouteStat(route, sw.status, 100)
		// 伪成功治理的计数补路（与 requestLogMiddleware 同逻辑）
		if rs := routeStats[route]; rs != nil {
			rs.errors.Add(1)
		}
	}
	rs := routeStats[route]
	if rs.requests.Load() != 2 || rs.errors.Load() != 1 {
		t.Fatalf("requests=%d errors=%d, want 2/1", rs.requests.Load(), rs.errors.Load())
	}
}

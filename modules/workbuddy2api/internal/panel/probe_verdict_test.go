package panel

import (
	"strings"
	"testing"
)

// TestProbeBodyVerdict wb 面板探测响应体判定单测（2026-11-26 afree 主仓凭据 11
// 事故加固的本 module 副本）：出口拦截页返回 200 + HTML 时，探测必须报错而非
// 假阳性 NoteSuccess/清冷却。
func TestProbeBodyVerdict(t *testing.T) {
	sseOK := "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n" +
		"data: [DONE]\n\n"

	cases := []struct {
		name    string
		body    string
		wantOK  bool
		wantSub string
	}{
		{"HTML 拦截页", "<!DOCTYPE HTML>\n<html><body>nginx</body></html>", false, "HTML intercept page"},
		{"正常 SSE 流", sseOK, true, ""},
		{"心跳打头 SSE", ": keep-alive\n\n" + sseOK, true, ""},
		{"只有 [DONE]", "data: [DONE]\n\n", false, "without model output"},
		{"聚合 JSON", "{\"choices\":[{\"message\":{\"content\":\"OK\"}}],\"usage\":{\"total_tokens\":6}}", true, ""},
		{"data 包裹 JSON", "{\"data\":{\"choices\":[{\"delta\":{}}]}}", true, ""},
		{"JSON 错误体", "{\"error\":{\"message\":\"quota exceeded\"}}", false, "upstream error"},
		{"无证据 JSON", "{\"id\":\"c1\"}", false, "no model output"},
		{"空 body", "", false, "empty body"},
		{"非 SSE 非 JSON", "blocked by firewall", false, "non-API body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := probeBodyVerdict([]byte(tc.body))
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v (reason=%q)", ok, tc.wantOK, reason)
			}
			if !tc.wantOK && tc.wantSub != "" && !strings.Contains(reason, tc.wantSub) {
				t.Fatalf("reason=%q, want substring %q", reason, tc.wantSub)
			}
		})
	}
}

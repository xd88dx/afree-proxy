package gateway

import (
	"strings"
	"testing"
)

// TestProbeBodyVerdict 2026-11-26 凭据 11 事故加固的判定单测：
// 出口故障时中间层返回 200 + HTML 拦截页，只看状态码会把探测误判成功。
func TestProbeBodyVerdict(t *testing.T) {
	sseOK := "data: {\"id\":\"c1\",\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n" +
		"data: {\"id\":\"c1\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}}\n\n" +
		"data: [DONE]\n\n"
	jsonOK := "{\"id\":\"c1\",\"object\":\"chat.completion\",\"choices\":[{\"message\":{\"content\":\"OK\"}}],\"usage\":{\"total_tokens\":6}}"
	wrapOK := "{\"data\":{\"choices\":[{\"message\":{\"content\":\"OK\"}}],\"usage\":{\"total_tokens\":6}}}"
	usageOnlyOK := "{\"usage\":{\"prompt_tokens\":5,\"total_tokens\":5}}"

	cases := []struct {
		name    string
		body    string
		wantOK  bool
		wantSub string // 失败时 reason 应包含的子串
	}{
		{"HTML 拦截页（凭据 11 实案）",
			"<!DOCTYPE HTML>\n<html><head><title>Welcome</title></head><body>nginx</body></html>",
			false, "HTML intercept page"},
		{"HTML 大小写变体", "<HTML><BODY>blocked</BODY></HTML>", false, "HTML intercept page"},
		{"正常 SSE 流", sseOK, true, ""},
		{"心跳注释打头的 SSE（mimo 实测形态）",
			": keep-alive\n\n" + sseOK, true, ""},
		{"CRLF SSE", "data: {\"choices\":[{\"delta\":{}}]}\r\ndata: [DONE]\r\n", true, ""},
		{"只有 [DONE] 的空 SSE", "data: [DONE]\n\n", false, "without model output"},
		{"SSE 帧全部解析失败", "data: not-json\ndata: also-bad\n", false, "without model output"},
		{"正常聚合 JSON", jsonOK, true, ""},
		{"responses 完成帧形态", "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"usage\":{\"total_tokens\":6}}}\n\n", true, ""},
		{"{data:{...}} 包裹 JSON", wrapOK, true, ""},
		{"仅 usage 无 choices", usageOnlyOK, true, ""},
		{"JSON 错误体", "{\"error\":{\"message\":\"quota exceeded\",\"code\":402}}", false, "upstream error"},
		{"error 字段为字符串形态", "{\"error\":\"invalid api key\"}", false, "upstream error"},
		{"无证据 JSON", "{\"object\":\"chat.completion\",\"id\":\"c1\"}", false, "no model output"},
		{"空 body", "   \n  ", false, "empty body"},
		{"非 SSE 非 JSON", "gateway timeout text page", false, "non-API body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := ProbeBodyVerdict(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v (reason=%q)", ok, tc.wantOK, reason)
			}
			if !tc.wantOK && tc.wantSub != "" && !strings.Contains(reason, tc.wantSub) {
				t.Fatalf("reason=%q, want substring %q", reason, tc.wantSub)
			}
		})
	}
}

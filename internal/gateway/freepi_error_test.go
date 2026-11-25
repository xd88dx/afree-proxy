package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"afree-proxy/internal/freepi"
)

// freepi 归属错误文案回归：这句 200 + SSE 正文曾穿透到 agent，被当成
// 正常回答导致任务意外结束。两句固定文案必须同时命中，且仅在 freepi
// 平台生效，避免误伤其它平台正文里泛泛讨论 account/sharing 的回答。
const (
	testShareSentence1 = "This conversation belongs to another free-pi account."
	testShareSentence2 = "Sharing or rotating accounts isn't allowed; use one account per person."
)

func freepiTestParams(model string) map[string]any {
	return map[string]any{"model": model}
}

func freepiTestChat(content string) map[string]any {
	return map[string]any{
		"choices": []any{
			map[string]any{"message": map[string]any{"content": content}},
		},
	}
}

// freepiTestSSELines 模拟上游把拒绝文案切成多个 delta.content 分片。
func freepiTestSSELines(parts ...string) []string {
	out := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		obj := map[string]any{
			"choices": []any{
				map[string]any{"delta": map[string]any{"content": p}},
			},
		}
		b, _ := json.Marshal(obj)
		out = append(out, "data: "+string(b))
	}
	out = append(out, "data: [DONE]")
	return out
}

func TestIsFreepiAccountSharingRejection(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"两句同时命中", testShareSentence1 + " " + testShareSentence2, true},
		{"大小写不敏感", strings.ToUpper(testShareSentence1) + " " + strings.ToLower(testShareSentence2), true},
		{"只有第一句", testShareSentence1, false},
		{"只有第二句", testShareSentence2, false},
		{"空文本", "", false},
		{"正常回答", "Sure, here is the code you asked for.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isFreepiAccountSharingRejection(tc.text); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGuardFreepiRejectionChat(t *testing.T) {
	rejectText := testShareSentence1 + " " + testShareSentence2
	freepiModel := freepi.ModelPrefix + ":gemini-2.5-flash"

	if got := guardFreepiRejectionChat(freepiTestChat(rejectText), freepiTestParams(freepiModel)); got == "" {
		t.Fatal("freepi 平台命中拒绝文案应返回原因")
	}
	if got := guardFreepiRejectionChat(freepiTestChat("hello"), freepiTestParams(freepiModel)); got != "" {
		t.Fatalf("正常正文不应误伤: %s", got)
	}
	// 非 freepi 平台即使正文含同样文案也不拦，避免误伤其它上游。
	if got := guardFreepiRejectionChat(freepiTestChat(rejectText), freepiTestParams("zen:deepseek")); got != "" {
		t.Fatalf("非 freepi 平台不应误伤: %s", got)
	}
	if got := guardFreepiRejectionChat(map[string]any{}, freepiTestParams(freepiModel)); got != "" {
		t.Fatalf("无 choices 不应命中: %s", got)
	}
}

func TestGuardFreepiRejectionSSE(t *testing.T) {
	freepiModel := freepi.ModelPrefix + ":gemini-2.5-flash"
	// 关键：文案被切成两片，必须拼接后整体判定。
	lines := freepiTestSSELines(testShareSentence1+" ", testShareSentence2)

	if got := guardFreepiRejectionSSE(lines, freepiTestParams(freepiModel)); got == "" {
		t.Fatal("分片拒绝文案应拼接后命中")
	}
	if got := guardFreepiRejectionSSE(lines, freepiTestParams("zen:deepseek")); got != "" {
		t.Fatalf("非 freepi 平台不应误伤: %s", got)
	}
	if got := guardFreepiRejectionSSE(freepiTestSSELines("normal answer"), freepiTestParams(freepiModel)); got != "" {
		t.Fatalf("正常正文不应误伤: %s", got)
	}
	if got := guardFreepiRejectionSSE(nil, freepiTestParams(freepiModel)); got != "" {
		t.Fatalf("空 lines 不应命中: %s", got)
	}
}

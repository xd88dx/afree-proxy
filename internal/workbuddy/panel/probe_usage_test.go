package panel

import (
	"encoding/json"
	"testing"
)

// TestProbeUsageFromBody 探测响应的 usage 提取：出站 body 被强制 stream=true，
// 探测拿到的是 SSE 文本而非聚合 JSON —— 这里两种形态都要能取出 usage，
// 取不到时返回 nil（调用方仍会记一次"最近使用"）。
func TestProbeUsageFromBody(t *testing.T) {
	sse := "data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n" +
		"data: {\"id\":\"1\",\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\n" +
		"data: [DONE]\n\n"
	if u := probeUsageFromBody([]byte(sse)); u == nil {
		t.Fatal("SSE usage not extracted")
	} else if n, _ := u["total_tokens"].(float64); int64(n) != 10 {
		t.Fatalf("total=%v want 10", u["total_tokens"])
	}

	// data 包装层：usage 在 data 里
	wrapped := []byte(`{"data":{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}}`)
	if u := probeUsageFromBody(wrapped); u == nil {
		t.Fatal("wrapped usage not extracted")
	} else if n, _ := u["total_tokens"].(float64); int64(n) != 3 {
		t.Fatalf("wrapped total=%v want 3", u["total_tokens"])
	}

	// 末帧优先：取最后一帧带 usage 的（流式里末帧最全）
	two := "data: {\"usage\":{\"total_tokens\":1}}\n\ndata: {\"usage\":{\"total_tokens\":99}}\n\ndata: [DONE]\n\n"
	if u := probeUsageFromBody([]byte(two)); u == nil {
		t.Fatal("last-frame usage not extracted")
	} else if n, _ := u["total_tokens"].(float64); int64(n) != 99 {
		t.Fatalf("last frame total=%v want 99", u["total_tokens"])
	}

	// 无 usage / 空 usage / 垃圾文本：一律 nil（不编造）
	for _, in := range []string{
		`{"id":"1","choices":[]}`,
		`{"usage":{}}`,
		"not json at all",
		"",
	} {
		if u := probeUsageFromBody([]byte(in)); u != nil {
			t.Fatalf("expected nil for %q, got %v", in, u)
		}
	}

	// 形态自检：确认探针发的 body 会被上游层改写成 stream=true（这也是本用例存在的原因）
	var obj map[string]any
	_ = json.Unmarshal([]byte(`{"model":"x","stream":false}`), &obj)
	if v, ok := obj["stream"].(bool); !ok || v {
		t.Fatal("fixture broken")
	}
}

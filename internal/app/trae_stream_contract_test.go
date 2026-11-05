package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// Trae 流式契约回归（2026-11-05 审计）：vendored 引擎把「stream 缺省」当
// true（`req.Stream == nil || *req.Stream`），违反 OpenAI 缺省 false——
// 按规范省略 stream 的 /v1/chat/completions 客户端会收到 SSE 而非 JSON。
// 修复：chat 入口缺省补 false（traeNormalizeStreamDefault）；cross_endpoint
// 调用器以调用方 stream 参数显式落 body。

func TestTraeNormalizeStreamDefault(t *testing.T) {
	// 缺省 → 显式 false
	out := traeNormalizeStreamDefault([]byte(`{"model":"trae:x","messages":[]}`))
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if v, ok := m["stream"].(bool); !ok || v {
		t.Fatalf("absent stream must become explicit false, got %v", m["stream"])
	}
	// 显式 true / false 原样保留
	for _, want := range []string{`"stream":true`, `"stream":false`} {
		in := []byte(`{"model":"trae:x","stream":` + strings.TrimPrefix(want, `"stream":`) + `}`)
		out := traeNormalizeStreamDefault(in)
		if !strings.Contains(string(out), want) {
			t.Fatalf("explicit %s must be preserved, got %s", want, out)
		}
	}
	// 非 JSON 原样返回（防御）
	if got := traeNormalizeStreamDefault([]byte(`not-json`)); string(got) != `not-json` {
		t.Fatalf("non-JSON body must pass through unchanged, got %s", got)
	}
}

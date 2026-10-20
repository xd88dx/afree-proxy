package app

import "testing"

// CLINE_FORCE_STREAM 关闭（默认）时，判定仍按模型：未知模型不强制流式。
func TestUpstreamNeedsStream_DefaultOff(t *testing.T) {
	t.Setenv("CLINE_FORCE_STREAM", "")
	if upstreamNeedsStream("definitely-not-a-known-model-xyz") {
		t.Fatal("force-off 时未知模型不应要求流式")
	}
}

// 开关打开后，任何模型都要走上游流式（客户端非流式由网关聚合）。
func TestUpstreamNeedsStream_ForceOn(t *testing.T) {
	t.Setenv("CLINE_FORCE_STREAM", "true")
	if !upstreamNeedsStream("definitely-not-a-known-model-xyz") {
		t.Fatal("force-on 时任何模型都应要求流式")
	}
}

// 我们替客户端开流式时必须补 include_usage，否则 SSE 不带 usage、聚合后记账为空。
func TestWithStreamUsage_InjectsWhenAbsent(t *testing.T) {
	params := map[string]any{"model": "m"}
	withStreamUsage(params)
	so, ok := params["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("stream_options 未注入: %#v", params["stream_options"])
	}
	if v, _ := so["include_usage"].(bool); !v {
		t.Fatalf("include_usage 应为 true: %#v", so)
	}
}

// 客户端自己带了 stream_options 时原样尊重，不覆盖。
func TestWithStreamUsage_PreservesClientValue(t *testing.T) {
	custom := map[string]any{"include_usage": false}
	params := map[string]any{"model": "m", "stream_options": custom}
	withStreamUsage(params)
	got, _ := params["stream_options"].(map[string]any)
	if got == nil || got["include_usage"] != false {
		t.Fatalf("客户端 stream_options 被改写: %#v", params["stream_options"])
	}
}

// nil 参数不能 panic（非流式入口可能传空 map/nil）。
func TestWithStreamUsage_NilSafe(t *testing.T) {
	withStreamUsage(nil)
}

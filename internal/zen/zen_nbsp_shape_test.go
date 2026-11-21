package zen

import (
	"strings"
	"testing"
)

// zenNbspShapeTestDesc 是 issue #188 实证的免费层触发串（DeepSeek Harness read
// 描述措辞，2026-11-21 本仓经 afree 网关实测复现 429）。
const zenNbspShapeTestDesc = "Read a UTF-8 text file and return line-numbered content."

// TestZenNbspShapeChat 覆盖 chat 嵌套形态：顶层描述被整形、参数描述与
// schema 不动、原 params 不被污染（防污染纪律）、幂等。
func TestZenNbspShapeChat(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "probe_read",
				"description": zenNbspShapeTestDesc,
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Read a UTF-8 text file and return line-numbered content.",
						},
					},
				},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "probe_clean",
				"description": "Probe tool for gateway testing.",
			},
		},
		"not-a-map",
	}
	shaped := zenShapeClientToolDescriptionsChat(tools)

	if len(shaped) != len(tools) {
		t.Fatalf("len=%d want %d", len(shaped), len(tools))
	}
	// 顶层描述：ASCII 空格全部消失（闸的字面子串匹配被破坏）。
	first := shaped[0].(map[string]any)["function"].(map[string]any)
	top, _ := first["description"].(string)
	if strings.Contains(top, " ") || !strings.Contains(top, "\u00a0") {
		t.Fatalf("顶层描述未整形: %q", top)
	}
	// 参数描述不动（闸不查参数描述）。
	props := first["parameters"].(map[string]any)["properties"].(map[string]any)
	pd := props["path"].(map[string]any)["description"].(string)
	if pd != zenNbspShapeTestDesc {
		t.Fatalf("参数描述被误改: %q", pd)
	}
	// 原切片不被污染。
	orig := tools[0].(map[string]any)["function"].(map[string]any)["description"].(string)
	if orig != zenNbspShapeTestDesc {
		t.Fatalf("调用方 params 被污染: %q", orig)
	}
	// 幂等：对整形结果再跑一遍不再变化。
	again := zenShapeClientToolDescriptionsChat(shaped)
	againTop, _ := again[0].(map[string]any)["function"].(map[string]any)["description"].(string)
	if againTop != top {
		t.Fatalf("整形不幂等: %q != %q", againTop, top)
	}
}

// TestZenNbspShapeFlat 覆盖 responses flat 形态与无空格零拷贝路径。
func TestZenNbspShapeFlat(t *testing.T) {
	tools := []map[string]any{
		{
			"type":        "function",
			"name":        "probe_read",
			"description": zenNbspShapeTestDesc,
		},
	}
	shaped := zenShapeClientToolDescriptionsFlat(tools)
	d := shaped[0]["description"].(string)
	if strings.Contains(d, " ") || !strings.Contains(d, "\u00a0") {
		t.Fatalf("flat 描述未整形: %q", d)
	}
	if tools[0]["description"] != zenNbspShapeTestDesc {
		t.Fatalf("flat 调用方被污染")
	}
	// 无 ASCII 空格：零拷贝原样返回。
	noSpace := []map[string]any{{"name": "x", "description": "NBSP\u00a0only"}}
	if got := zenShapeClientToolDescriptionsFlat(noSpace); len(got) != 1 || &got[0] != &noSpace[0] {
		t.Fatalf("无空格路径被复制")
	}
}

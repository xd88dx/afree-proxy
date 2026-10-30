package trae

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---- 请求侧：消息文本化 ----

func TestRawMessagesNoToolProtocolNoRuntimePrompt(t *testing.T) {
	out := buildRawMessages([]any{
		map[string]any{"role": "user", "content": "123"},
	}, nil, false, nil)
	if len(out) != 1 || out[0]["role"] != "user" {
		t.Fatalf("plain request must pass through unchanged: %v", out)
	}
	if text := out[0]["content"].([]map[string]any)[0]["text"]; text != "123" {
		t.Fatalf("text = %q", text)
	}
}

func TestRawMessagesToolDefsInjectRuntimePrompt(t *testing.T) {
	defs := []map[string]any{
		{"name": "read_file", "description": "read a file", "input_schema": map[string]any{
			"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []any{"path"},
		}},
	}
	out := buildRawMessages([]any{
		map[string]any{"role": "user", "content": "hi"},
	}, defs, true, nil)
	if len(out) != 2 {
		t.Fatalf("messages = %d", len(out))
	}
	sys := out[0]["content"].([]map[string]any)[0]["text"].(string)
	for _, want := range []string{
		"Caller client context (JSON):",
		"<opencode_tool_call>",
		`Available client tool definitions and input schemas (JSON):`,
		"read_file",
	} {
		if !strings.Contains(sys, want) {
			t.Fatalf("runtime prompt missing %q:\n%s", want, sys)
		}
	}
}

func TestRawMessagesEmptyToolsKeyNoPrompt(t *testing.T) {
	// tools 键存在但为空列表：不算工具协议（与上游空目录同判）
	out := buildRawMessages([]any{map[string]any{"role": "user", "content": "hi"}},
		nil, true, nil)
	if len(out) != 1 || out[0]["role"] != "user" {
		t.Fatalf("empty tools must not inject prompt: %v", out)
	}
}

func TestRawMessagesHistoryCompaction(t *testing.T) {
	t.Setenv("TRAE_RAW_MAX_MESSAGES", "3")
	var msgs []any
	for i := 0; i < 10; i++ {
		msgs = append(msgs, map[string]any{"role": "user", "content": strings.Repeat("x", 10)})
		msgs = append(msgs, map[string]any{"role": "assistant", "content": strings.Repeat("y", 10)})
	}
	out := buildRawMessages(msgs, nil, false, nil)
	// 非系统消息保留最近 3 条（截断后 20 条 → 3 条），unique system 无
	if len(out) != 3 {
		t.Fatalf("compacted to %d, want 3", len(out))
	}
}

func TestRawMessagesHistoryCharBudget(t *testing.T) {
	t.Setenv("TRAE_RAW_MAX_HISTORY_CHARS", "60")
	msgs := []any{
		map[string]any{"role": "user", "content": strings.Repeat("a", 50)},
		map[string]any{"role": "assistant", "content": strings.Repeat("b", 50)},
		map[string]any{"role": "user", "content": strings.Repeat("c", 10)},
	}
	out := buildRawMessages(msgs, nil, false, nil)
	// 60 字符预算：从尾往前装 10(c)+50(b)=60 恰好不超，50(a) 放不下
	if len(out) != 2 {
		t.Fatalf("char budget compacted to %d, want 2: %v", len(out), out)
	}
	if text, _ := out[0]["content"].([]map[string]any)[0]["text"].(string); !strings.HasPrefix(text, "b") {
		t.Fatalf("newest-first retention broken, first text = %q", text)
	}
}

func TestRawMessagesOrphanToolResultKeepsAssistantCall(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "list files"},
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "c9", "type": "function", "function": map[string]any{"name": "ls", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "c9", "name": "ls", "content": "a.txt"},
	}
	t.Setenv("TRAE_RAW_MAX_MESSAGES", "1")
	out := buildRawMessages(msgs, nil, false, nil)
	// 消息有工具痕迹 → runtime system 注入；保留 1 条时是孤儿 tool result
	// → 守卫补回紧邻 assistant 调用：[system, assistant, tool→user]
	if len(out) != 3 {
		t.Fatalf("orphan guard kept %d messages, want 3", len(out))
	}
	if out[0]["role"] != "system" || out[1]["role"] != "assistant" || out[2]["role"] != "user" {
		t.Fatalf("roles = %v/%v/%v", out[0]["role"], out[1]["role"], out[2]["role"])
	}
}

func TestRawMessagesImageBlocksPreserved(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "看图"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,QUJD"}},
		}},
	}
	out := buildRawMessages(msgs, nil, false, nil)
	blocks := out[0]["content"].([]map[string]any)
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want text+image", len(blocks))
	}
	if blocks[1]["type"] != "image_url" {
		t.Fatalf("image block missing: %v", blocks)
	}
}

func TestToolSchemaBudgetCompaction(t *testing.T) {
	t.Setenv("TRAE_RAW_MAX_TOOL_SCHEMA_CHARS", "1024")
	big := strings.Repeat("x", 500)
	defs := make([]map[string]any, 0, 6)
	for i := 0; i < 6; i++ {
		defs = append(defs, map[string]any{
			"name": strings.Repeat("t", 1) + string(rune('a'+i)), "description": big,
			"input_schema": map[string]any{"type": "object", "properties": map[string]any{"p": map[string]any{"type": "string"}}},
		})
	}
	payload, compacted, omitted := toolDefinitionsPrompt(defs)
	if !compacted {
		t.Fatalf("expected compacted payload")
	}
	if omitted < 0 {
		t.Fatalf("negative omitted")
	}
	if len(payload) > 1024 {
		t.Fatalf("payload %d exceeds budget 1024", len(payload))
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(payload), &obj); err != nil {
		t.Fatalf("compact payload not JSON: %v", err)
	}
}

// ---- 响应侧：文本工具块 ----

func TestExtractTextToolCallsBasic(t *testing.T) {
	raw := `好的。` + "\n" + `<opencode_tool_call>{"name":"read_file","input":{"path":"a.txt"}}</opencode_tool_call>` + "\n" + `完成。`
	visible, calls := extractTextToolCalls(raw)
	if len(calls) != 1 || calls[0].Name != "read_file" {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].ArgsJSON != `{"path":"a.txt"}` {
		t.Fatalf("args = %q", calls[0].ArgsJSON)
	}
	if strings.Contains(visible, "opencode_tool_call") {
		t.Fatalf("block leaked into visible text: %q", visible)
	}
	if !strings.Contains(visible, "好的。") || !strings.Contains(visible, "完成。") {
		t.Fatalf("prose lost: %q", visible)
	}
}

func TestExtractTextToolCallsArgumentsKey(t *testing.T) {
	_, calls := extractTextToolCalls(`<tool_call>{"name":"exec","arguments":{"cmd":"dir"}}</tool_call>`)
	if len(calls) != 1 || calls[0].Name != "exec" || calls[0].ArgsJSON != `{"cmd":"dir"}` {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestExtractTextToolCallsIncompleteHeld(t *testing.T) {
	// 流式中块尚未闭合：保持隐藏，不产调用
	raw := `answer` + "\n" + `<opencode_tool_call>{"name":"read_file","input":{"path":"a`
	visible, calls := extractTextToolCalls(raw)
	if len(calls) != 0 {
		t.Fatalf("incomplete block produced calls: %+v", calls)
	}
	if strings.Contains(visible, "opencode_tool_call") || strings.Contains(visible, "read_file") {
		t.Fatalf("incomplete block leaked: %q", visible)
	}
	if !strings.Contains(visible, "answer") {
		t.Fatalf("prose lost: %q", visible)
	}
}

func TestExtractTextToolCallsCloseThenOpen(t *testing.T) {
	raw := `<opencode_tool_call>{"name":"a","input":{}}</opencode_tool_call>` +
		`<opencode_tool_call>{"name":"b","input":{}}` // 第二个未闭合
	visible, calls := extractTextToolCalls(raw)
	if len(calls) != 1 || calls[0].Name != "a" {
		t.Fatalf("calls = %+v", calls)
	}
	if strings.Contains(visible, "opencode_tool_call") {
		t.Fatalf("unclosed tail leaked: %q", visible)
	}
}

func TestExtractTextToolCallsNamedVariant(t *testing.T) {
	visible, calls := extractTextToolCalls(`<invoke name="search">{"query":"golang"}</invoke>`)
	if len(calls) != 1 || calls[0].Name != "search" || calls[0].ArgsJSON != `{"query":"golang"}` {
		t.Fatalf("calls = %+v", calls)
	}
	if strings.Contains(visible, "<invoke") {
		t.Fatalf("variant block leaked: %q", visible)
	}
}

func TestExtractTextToolCallsNoToolsPlain(t *testing.T) {
	raw := "普通中文回复,没有 < 任何尖括号协议。"
	visible, calls := extractTextToolCalls(raw)
	if len(calls) != 0 || visible != raw {
		t.Fatalf("plain text must pass through: %q %+v", visible, calls)
	}
}

func TestStripTextToolBlocks(t *testing.T) {
	out := stripTextToolBlocks(`pre<opencode_tool_call>{"name":"x","input":{}}</opencode_tool_call>post`)
	if out != "prepost" {
		t.Fatalf("strip = %q", out)
	}
}

// ---- Stream 集成 ----

func TestStreamProtoConsumeToolCalls(t *testing.T) {
	s := &Stream{Transport: "raw"}
	s.addText("先回答:")
	if ev, ok := s.protoConsume(); ok && ev.Text != "先回答:" {
		t.Fatalf("first delta = %+v", ev)
	}
	s.addText(`<opencode_tool_call>{"name":"read","input":{"p":"x"}}</opencode_tool_call>`)
	ev, _ := s.protoConsume()
	if ev != nil && ev.Text != "" {
		t.Fatalf("tool block must not become text: %+v", ev)
	}
	if got := s.ToolCallsSnapshot(); len(got) != 1 || got[0].Name != "read" {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestStreamProtoDisabledForRemote(t *testing.T) {
	// remote 传输不走文本协议（SOLO 执行器有自己的 tool_call_info）
	s := &Stream{Transport: "solo_agent_remote"}
	s.addText(`<opencode_tool_call>{"name":"x","input":{}}</opencode_tool_call>`)
	delta, _ := s.addText("")
	if delta != "" {
		t.Fatalf("unexpected delta")
	}
}

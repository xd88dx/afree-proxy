package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testDSMLPipe = "\uFF5C\uFF5C"

// dsmlTestBlock 用测试常量拼一个完整 DSML 块（invoke 内容由调用方给）。
func dsmlTestBlock(invoke string) string {
	return "<" + testDSMLPipe + "DSML" + testDSMLPipe + " calls>" +
		invoke +
		"</" + testDSMLPipe + "DSML" + testDSMLPipe + " calls>"
}

func dsmlTestBashInvoke() string {
	return "<" + testDSMLPipe + "DSML" + testDSMLPipe + ` invoke name="Bash">` +
		"<" + testDSMLPipe + "DSML" + testDSMLPipe + ` parameter name="command" string="true">ls -la</` +
		testDSMLPipe + "DSML" + testDSMLPipe + " parameter>" +
		"</" + testDSMLPipe + "DSML" + testDSMLPipe + " invoke>"
}

func testBashTools() map[string]any {
	return map[string]any{
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "Bash",
					"parameters": map[string]any{
						"type":     "object",
						"required": []any{"command"},
					},
				},
			},
		},
	}
}

func sseLine(v any) string {
	b, _ := json.Marshal(v)
	return "data: " + string(b) + "\n\n"
}

func TestDSMLRepairCompleteBlock(t *testing.T) {
	text := "前缀" + dsmlTestBlock(dsmlTestBashInvoke()) + "后缀"
	r := newDSMLRepair(map[string]bool{"Bash": true}, false)
	prose, calls := r.feed(text)
	prose += r.flush()
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d (prose=%q)", len(calls), prose)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("want name Bash, got %q", calls[0].Name)
	}
	if calls[0].Arguments != `{"command":"ls -la"}` {
		t.Fatalf("want args, got %q", calls[0].Arguments)
	}
	if prose != "前缀后缀" {
		t.Fatalf("want prose 前缀后缀, got %q", prose)
	}
	if r.seenBlocks() != 1 || r.convertedCalls() != 1 || r.rejectedBlocks() != 0 {
		t.Fatalf("stats seen=%d converted=%d rejected=%d", r.seenBlocks(), r.convertedCalls(), r.rejectedBlocks())
	}
}

func TestDSMLRepairRejectedKeepsText(t *testing.T) {
	// 块完整闭合但内容不是 invoke → 识别到但判定失败，必须原文回吐、不吞字节。
	text := dsmlTestBlock("这里不是工具调用")
	r := newDSMLRepair(map[string]bool{"Bash": true}, false)
	prose, calls := r.feed(text)
	prose += r.flush()
	if len(calls) != 0 {
		t.Fatalf("want 0 call, got %d", len(calls))
	}
	if prose != text {
		t.Fatalf("原文未回吐: got %q", prose)
	}
	if r.seenBlocks() != 1 || r.rejectedBlocks() != 1 {
		t.Fatalf("stats seen=%d rejected=%d", r.seenBlocks(), r.rejectedBlocks())
	}
}

func TestGuardBadToolCallsEmptyArgs(t *testing.T) {
	// 结构化 tool_call arguments 为空，工具 schema 声明了 required → 必须拦下。
	lines := []string{
		sseLine(map[string]any{
			"choices": []any{map[string]any{
				"delta": map[string]any{"tool_calls": []any{map[string]any{
					"index": float64(0), "id": "call_1", "type": "function",
					"function": map[string]any{"name": "Bash", "arguments": ""},
				}}},
			}},
		}),
		sseLine(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
		}),
		"data: [DONE]\n\n",
	}
	if bad := guardBadToolCalls(lines, testBashTools()); bad == "" {
		t.Fatal("空参数工具调用未被识别为错误")
	}
}

func TestGuardBadToolCallsBrokenDSML(t *testing.T) {
	text := dsmlTestBlock("这里不是工具调用")
	lines := []string{
		sseLine(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{"content": text}}},
		}),
		sseLine(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
		}),
		"data: [DONE]\n\n",
	}
	if bad := guardBadToolCalls(lines, testBashTools()); bad == "" {
		t.Fatal("破损 DSML 块未被识别为错误")
	}
}

func TestGuardBadToolCallsNormalNoFalsePositive(t *testing.T) {
	lines := []string{
		sseLine(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{"content": "hello"}}},
		}),
		sseLine(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
		}),
		"data: [DONE]\n\n",
	}
	if bad := guardBadToolCalls(lines, testBashTools()); bad != "" {
		t.Fatalf("正常响应被误伤: %s", bad)
	}
}

func TestGuardBadToolCallsChat(t *testing.T) {
	// 空参数（结构化）+ required schema → 拦。
	empty := map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id": "call_1", "type": "function",
					"function": map[string]any{"name": "Bash", "arguments": "{}"},
				}},
			},
			"finish_reason": "tool_calls",
		}},
	}
	if bad := guardBadToolCallsChat(empty, testBashTools()); bad == "" {
		t.Fatal("非流式：空参数工具调用未被拦下")
	}

	// 破损 DSML 块 → 拦。
	broken := map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": dsmlTestBlock("这里不是工具调用")},
			"finish_reason": "stop",
		}},
	}
	if bad := guardBadToolCallsChat(broken, testBashTools()); bad == "" {
		t.Fatal("非流式：破损 DSML 块未被拦下")
	}

	// 完整 DSML 块 → 放行给 textCallGW 修复，不能在守门层拦掉。
	complete := map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": dsmlTestBlock(dsmlTestBashInvoke())},
			"finish_reason": "stop",
		}},
	}
	if bad := guardBadToolCallsChat(complete, testBashTools()); bad != "" {
		t.Fatalf("非流式：完整 DSML 块应放行给出口修复，却被拦: %s", bad)
	}

	// 正常正文 → 不误伤。
	normal := map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": "hello"},
			"finish_reason": "stop",
		}},
	}
	if bad := guardBadToolCallsChat(normal, testBashTools()); bad != "" {
		t.Fatalf("非流式：正常响应被误伤: %s", bad)
	}
}

// TestGuardBadToolCallsMissingRequiredFields 截图场景（2026-11-25）：Write
// 的 arguments 非空但缺 content、Bash 缺 command——旧判据只拦 "" / "{}"，
// 部分缺字段会漏判成 200，Claude Code 校验失败后任务静默终止。
func TestGuardBadToolCallsMissingRequiredFields(t *testing.T) {
	writeTools := map[string]any{"tools": []any{map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":       "Write",
			"parameters": map[string]any{"type": "object", "required": []any{"file_path", "content"}},
		},
	}}}
	chat := func(args string) map[string]any {
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
				"id": "call_1", "type": "function",
				"function": map[string]any{"name": "Write", "arguments": args},
			}}},
			"finish_reason": "tool_calls",
		}}}
	}
	bad := guardBadToolCallsChat(chat(`{"file_path":"slack.py"}`), writeTools)
	if bad == "" || !strings.Contains(bad, "content") {
		t.Fatalf("缺 content 的部分参数必须拦下且点名缺失字段, got %q", bad)
	}
	if bad := guardBadToolCallsChat(chat(`{"file_path":"slack.py","content":"print(1)"}`), writeTools); bad != "" {
		t.Fatalf("完整参数被误伤: %s", bad)
	}
}

// TestGuardBadToolCallsAnthropicSchema new-api 半转换时 tools 是 Anthropic
// {name, input_schema} 形态，required 也必须被认出（否则空参数漏判）。
func TestGuardBadToolCallsAnthropicSchema(t *testing.T) {
	tools := map[string]any{"tools": []any{map[string]any{
		"name":         "Bash",
		"input_schema": map[string]any{"type": "object", "required": []any{"command"}},
	}}}
	chat := map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
			"id": "call_1", "type": "function",
			"function": map[string]any{"name": "Bash", "arguments": "{}"},
		}}},
		"finish_reason": "tool_calls",
	}}}
	if bad := guardBadToolCallsChat(chat, tools); bad == "" {
		t.Fatal("Anthropic input_schema 形态的空参数也必须拦下")
	}
}

func TestFinishZenChatNonStreamBadToolCall502(t *testing.T) {
	body := strings.Join([]string{
		sseLine(map[string]any{
			"choices": []any{map[string]any{
				"delta": map[string]any{"tool_calls": []any{map[string]any{
					"index": float64(0), "id": "call_1", "type": "function",
					"function": map[string]any{"name": "Bash", "arguments": ""},
				}}},
			}},
		}),
		sseLine(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
		}),
		"data: [DONE]\n\n",
	}, "")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	rec := httptest.NewRecorder()
	finishZenChatNonStream(rec, resp, &ZenModel{ID: "m"}, testBashTools(), nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `upstream_error`) {
		t.Fatalf("错误类型不对: %s", rec.Body.String())
	}
}

func TestTextCallGWDSMLStream(t *testing.T) {
	rec := httptest.NewRecorder()
	gw := newTextCallGateway(rec, []string{"Bash"}, "m")
	gw.Header().Set("Content-Type", "text/event-stream")
	gw.WriteHeader(http.StatusOK)
	writeChunk := func(delta map[string]any, fr any) {
		gw.Write([]byte(sseLine(map[string]any{
			"choices": []any{map[string]any{"index": float64(0), "delta": delta, "finish_reason": fr}},
		})))
	}
	writeChunk(map[string]any{"role": "assistant"}, nil)
	writeChunk(map[string]any{"content": dsmlTestBlock(dsmlTestBashInvoke())}, nil)
	writeChunk(map[string]any{}, "stop")
	gw.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, `"tool_calls"`) {
		t.Fatalf("未生成 tool_calls: %s", body)
	}
	if strings.Contains(body, testDSMLPipe) {
		t.Fatalf("DSML 标记泄漏到正文: %s", body)
	}
	if !strings.Contains(body, "ls -la") {
		t.Fatalf("工具参数丢失: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("finish_reason 未改写为 tool_calls: %s", body)
	}
}

func TestTextCallGWDSMLNonStream(t *testing.T) {
	rec := httptest.NewRecorder()
	gw := newTextCallGateway(rec, []string{"Bash"}, "m")
	gw.Header().Set("Content-Type", "application/json")
	gw.WriteHeader(http.StatusOK)
	payload := map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": dsmlTestBlock(dsmlTestBashInvoke())},
			"finish_reason": "stop",
		}},
	}
	b, _ := json.Marshal(payload)
	gw.Write(b)

	body := rec.Body.String()
	if !strings.Contains(body, `"tool_calls"`) {
		t.Fatalf("未生成 tool_calls: %s", body)
	}
	if strings.Contains(body, testDSMLPipe) {
		t.Fatalf("DSML 标记泄漏到正文: %s", body)
	}
	if !strings.Contains(body, "ls -la") {
		t.Fatalf("工具参数丢失: %s", body)
	}
}

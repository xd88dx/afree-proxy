package gateway

import (
	"context"
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

// TestGuardBufferedChatSSEMessagesEndpoint 回归（2026-11-25）：Claude Code 走
// /v1/messages，其流式出口此前没接 guard，缺 command 的 tool_call 以 200 放行，
// 客户端本地抛 InputValidationError。这道闸必须在写 200 前改判 502；未命中
// 时必须把已读走的 body 完整还原，否则后续实时转发拿到空流。
func TestGuardBufferedChatSSEMessagesEndpoint(t *testing.T) {
	sseWithArgs := func(args string) string {
		return strings.Join([]string{
			sseLine(map[string]any{
				"choices": []any{map[string]any{
					"delta": map[string]any{"tool_calls": []any{map[string]any{
						"index": float64(0), "id": "call_1", "type": "function",
						"function": map[string]any{"name": "Bash", "arguments": args},
					}}},
				}},
			}),
			sseLine(map[string]any{
				"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
			}),
			"data: [DONE]\n\n",
		}, "")
	}
	badBody := sseWithArgs(`{"description":"list"}`)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(badBody))}
	rec := httptest.NewRecorder()
	if !guardBufferedChatSSE(rec, resp, testBashTools()) {
		t.Fatal("缺 command 的 tool_call 必须命中 guard")
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d; body=%s", rec.Code, rec.Body.String())
	}

	goodBody := sseWithArgs(`{"command":"ls -la"}`)
	resp2 := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(goodBody))}
	rec2 := httptest.NewRecorder()
	if guardBufferedChatSSE(rec2, resp2, testBashTools()) {
		t.Fatalf("完整参数被误伤: %s", rec2.Body.String())
	}
	got, err := io.ReadAll(resp2.Body)
	if err != nil || string(got) != goodBody {
		t.Fatalf("未命中时 body 必须完整还原, err=%v len=%d", err, len(got))
	}
}

// TestHandleAnthropicViaChatBadToolCall502 端到端回归：Claude Code 的
// /v1/messages 流式请求经前缀平台调用器返回缺 required 字段的 tool_call 时，
// 出口必须 502，不能再以 200 放行让客户端本地抛 InputValidationError。
func TestHandleAnthropicViaChatBadToolCall502(t *testing.T) {
	req := anthropicReq{
		Model:    "other:test",
		Stream:   true,
		Messages: []anthropicMsg{{Role: "user", Content: "list files"}},
		Tools:    json.RawMessage(`[{"name":"Bash","description":"run shell","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}]`),
	}
	openAIReq := anthropicToOpenAI(req)
	if got := toolRequiredFields(openAIReq["tools"])["Bash"]; len(got) == 0 {
		t.Fatalf("anthropicToOpenAI 丢了 required, tools=%v", openAIReq["tools"])
	}
	badSSE := strings.Join([]string{
		sseLine(map[string]any{
			"choices": []any{map[string]any{
				"delta": map[string]any{"tool_calls": []any{map[string]any{
					"index": float64(0), "id": "call_1", "type": "function",
					"function": map[string]any{"name": "Bash", "arguments": `{"description":"list"}`},
				}}},
			}},
		}),
		sseLine(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
		}),
		"data: [DONE]\n\n",
	}, "")
	caller := func(ctx context.Context, params map[string]any, stream bool) (*http.Response, func(map[string]any), error) {
		return sseBytesResponse([]byte(badSSE)), nil, nil
	}
	rec := httptest.NewRecorder()
	handleAnthropicViaChat(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", nil), req, openAIReq, nil, caller)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "upstream_error") {
		t.Fatalf("错误类型不对: %s", rec.Body.String())
	}
}

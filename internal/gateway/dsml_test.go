package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"afree-proxy/internal/gwconfig"
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
	if bad := guardBadToolCalls(lines, testBashTools(), nil); bad == "" {
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
	if bad := guardBadToolCalls(lines, testBashTools(), nil); bad == "" {
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
	if bad := guardBadToolCalls(lines, testBashTools(), nil); bad != "" {
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
	if bad := guardBadToolCallsChat(empty, testBashTools(), nil); bad == "" {
		t.Fatal("非流式：空参数工具调用未被拦下")
	}

	// 破损 DSML 块 → 拦。
	broken := map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": dsmlTestBlock("这里不是工具调用")},
			"finish_reason": "stop",
		}},
	}
	if bad := guardBadToolCallsChat(broken, testBashTools(), nil); bad == "" {
		t.Fatal("非流式：破损 DSML 块未被拦下")
	}

	// 完整 DSML 块 → 放行给 textCallGW 修复，不能在守门层拦掉。
	complete := map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": dsmlTestBlock(dsmlTestBashInvoke())},
			"finish_reason": "stop",
		}},
	}
	if bad := guardBadToolCallsChat(complete, testBashTools(), nil); bad != "" {
		t.Fatalf("非流式：完整 DSML 块应放行给出口修复，却被拦: %s", bad)
	}

	// 正常正文 → 不误伤。
	normal := map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": "hello"},
			"finish_reason": "stop",
		}},
	}
	if bad := guardBadToolCallsChat(normal, testBashTools(), nil); bad != "" {
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
	bad := guardBadToolCallsChat(chat(`{"file_path":"slack.py"}`), writeTools, nil)
	if bad == "" || !strings.Contains(bad, "content") {
		t.Fatalf("缺 content 的部分参数必须拦下且点名缺失字段, got %q", bad)
	}
	if bad := guardBadToolCallsChat(chat(`{"file_path":"slack.py","content":"print(1)"}`), writeTools, nil); bad != "" {
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
	if bad := guardBadToolCallsChat(chat, tools, nil); bad == "" {
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
	finishZenChatNonStream(rec, resp, &ZenModel{ID: "m"}, testBashTools(), nil, nil)
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

// TestGuardBadToolCallsLeakVariants 覆盖 2026-11-25 生产复现暴露的三个漏判口径：
// 1) schema 只有 properties 没有 required（new-api 转换丢 required）；
// 2) 必需字段值为 null；
// 3) 参数非空但不是合法 JSON（交给 new-api 会回退成空对象）。
// 三者都曾让坏 tool_call 以 200 放行，客户端本地抛 InputValidationError。
func TestGuardBadToolCallsLeakVariants(t *testing.T) {
	badChat := func(args string) map[string]any {
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"tool_calls": []any{map[string]any{
				"id": "call_x", "type": "function",
				"function": map[string]any{"name": "Bash", "arguments": args},
			}}},
			"finish_reason": "tool_calls",
		}}}
	}
	propsOnly := map[string]any{"tools": []any{map[string]any{"type": "function", "function": map[string]any{
		"name": "Bash", "parameters": map[string]any{
			"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}},
		},
	}}}}
	withRequired := testBashTools()

	cases := []struct {
		name   string
		chat   map[string]any
		params map[string]any
	}{
		{"props-only null 值", badChat(`{"command":null}`), propsOnly},
		{"required null 值", badChat(`{"command":null}`), withRequired},
		{"非法 JSON 未转义引号", badChat(`{"command":"cd "D:/x" && ls"}`), withRequired},
		{"非法 JSON 未转义换行", badChat("{\"command\":\"echo hi\nls\"}"), withRequired},
	}
	for _, tc := range cases {
		if bad := guardBadToolCallsChat(tc.chat, tc.params, nil); bad == "" {
			t.Errorf("%s: 应判坏但放行", tc.name)
		}
	}
	// 合法调用不能误伤（2026-11-27 S1 口径对齐：props-only 的零参调用合法，
	// 只拦显式 null）：空 {}、空串、部分可选字段且无显式 null 三形态全放行。
	for _, args := range []string{"{}", "", `{"description":"list files"}`} {
		if bad := guardBadToolCallsChat(badChat(args), propsOnly, nil); bad != "" {
			t.Errorf("props-only 零参调用 %q 被误判: %s", args, bad)
		}
	}
}

// TestGuardBadToolCallsMultiCallEndToEnd 端到端复现 17:03 场景：同一响应里
// 两个 Bash tool_call，index 0 空参数（坏）、index 1 正常。只要 tools 声明了
// required 或 properties，守卫就必须在回放前拦下整个响应。
func TestGuardBadToolCallsMultiCallEndToEnd(t *testing.T) {
	frames := []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_bad","type":"function","function":{"name":"Bash","arguments":""}}]},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_ok","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}}]},"finish_reason":null}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	var lines []string
	for _, f := range frames {
		lines = append(lines, sseLine(json.RawMessage(f)))
	}
	lines = append(lines, "data: [DONE]\n\n")
	for _, params := range []map[string]any{
		testBashTools(),
		{"tools": []any{map[string]any{"name": "Bash", "input_schema": map[string]any{
			"type": "object", "required": []any{"command"},
		}}}},
	} {
		if bad := guardBadToolCalls(lines, params, nil); bad == "" {
			t.Errorf("端到端多调用未拦下坏 tool_call: tools=%v", params["tools"])
		}
	}
}

// withZenRenameConfig 测试期启用 zen 出站改名（隔离到临时配置文件，形态与
// toolrename 包测试一致）。
func withZenRenameConfig(t *testing.T, prefix string) {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/afree-config.json"
	_ = os.WriteFile(path, []byte("{}"), 0600)
	saved := gwconfig.GlobalConfigPathOverride
	gwconfig.GlobalConfigPathOverride = path
	savedCfg := *gwconfig.GetToolRename()
	t.Cleanup(func() {
		gwconfig.GlobalConfigPathOverride = saved
		_ = gwconfig.SetToolRename(savedCfg)
	})
	_ = os.Remove(path)
	if err := gwconfig.SetToolRename(gwconfig.ToolRenameConfig{Prefix: prefix, Platforms: map[string]bool{"zen": true}}); err != nil {
		t.Fatalf("set config: %v", err)
	}
}

// TestGuardBadToolCallsZenRenameAware 锁定 2026-11-27 审计 G2 的双向修复：
// zen 出站改名为 body 级（params 真名、响应假名），闸判据必须同时认假名——
// 结构化坏调用要拦得住（此前被假名致盲），DSML 好调用不能误判 502（此前
// 真名名单 strict 拒收假名块）。
func TestGuardBadToolCallsZenRenameAware(t *testing.T) {
	withZenRenameConfig(t, "zkx")
	params := testBashTools()
	ren := zenGuardMapping(params)
	if ren == nil {
		t.Fatal("zen 改名映射应生效")
	}
	fake := ren.Rename("Bash")
	if fake == "Bash" {
		t.Fatal("假名应与真名不同")
	}

	// ① 结构化坏调用（假名 + 空参数）：映射感知后必须判坏。
	bad := map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"tool_calls": []any{map[string]any{
			"id": "call_x", "type": "function",
			"function": map[string]any{"name": fake, "arguments": ""},
		}}},
		"finish_reason": "tool_calls",
	}}}
	if got := guardBadToolCallsChat(bad, params, ren); got == "" {
		t.Fatal("假名坏 tool_call 应判坏（闸被致盲）")
	}

	// ② DSML 好调用（假名块 + 合法参数）：映射感知后不得误判 502。
	dsmled := map[string]any{"choices": []any{map[string]any{
		"message": map[string]any{"content": dsmlTestBlock(strings.ReplaceAll(dsmlTestBashInvoke(), `name="Bash"`, `name="`+fake+`"`))},
	}}}
	if got := guardBadToolCallsChat(dsmled, params, ren); got != "" {
		t.Fatalf("假名 DSML 好调用被误判 502: %s", got)
	}
	// 反向对照：不传映射时该形态确实误判（防测试同义反复）。
	if got := guardBadToolCallsChat(dsmled, params, nil); got == "" {
		t.Fatal("无映射时假名 DSML 块应被判败（锁定旧行为存在过）")
	}
}

// TestFinishClineChatNonStreamWritesResponse 锁定 2026-11-27 审计 G1：cline 纯
// 非流式分支必须有写出（d8fbb8f1 出包重构曾丢失尾行 → 隐式 200 空响应体），
// 且写前过坏 tool_call 闸。
func TestFinishClineChatNonStreamWritesResponse(t *testing.T) {
	okBody := `{"choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(okBody))}
	rec := httptest.NewRecorder()
	finishClineChatNonStream(rec, resp, map[string]any{"model": "m"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("非流式应有 200 写出, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "hello") {
		t.Fatalf("响应体应含上游内容: %s", rec.Body.String())
	}

	badBody := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"Bash","arguments":""}}]},"finish_reason":"tool_calls"}]}`
	resp2 := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(badBody))}
	rec2 := httptest.NewRecorder()
	finishClineChatNonStream(rec2, resp2, testBashTools(), nil)
	if rec2.Code != http.StatusBadGateway {
		t.Fatalf("坏 tool_call 应改判 502, got %d", rec2.Code)
	}
}

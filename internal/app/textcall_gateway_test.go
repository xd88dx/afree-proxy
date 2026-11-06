package app

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func gwChunk(model string, delta map[string]any, fr any) []byte {
	obj := map[string]any{
		"id": "chatcmpl-x", "object": "chat.completion.chunk", "created": 1, "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": fr}},
	}
	b, _ := json.Marshal(obj)
	return append(append([]byte("data: "), b...), '\n', '\n')
}

// 主路径：正文里的模仿调用被转换，正文其余部分无损，finish_reason 改写。
func TestTextCallGWSSEConverts(t *testing.T) {
	rec := httptest.NewRecorder()
	gw := newTextCallGateway(rec, []string{"Bash"}, "test-model")
	rec.Header().Set("Content-Type", "text/event-stream")
	gw.WriteHeader(200)

	gw.Write(gwChunk("test-model", map[string]any{"role": "assistant", "content": ""}, nil))
	gw.Write(gwChunk("test-model", map[string]any{"content": "好的，执行"}, nil))
	gw.Write(gwChunk("test-model", map[string]any{"content": `[调用工具 bash({"command": "ls -la"})]`}, nil))
	gw.Write(gwChunk("test-model", map[string]any{}, "stop"))
	gw.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if strings.Contains(body, "[调用工具") {
		t.Fatalf("模仿调用不应残留在正文: %s", body)
	}
	if !strings.Contains(body, `"tool_calls"`) || !strings.Contains(body, `"name":"Bash"`) {
		t.Fatalf("应包含转换出的 tool_calls 块（工具名归一）: %s", body)
	}
	if !strings.Contains(body, `"arguments":"{\"command\": \"ls -la\"}"`) {
		t.Fatalf("参数应原样保留（JSON 字符串转义形态）: %s", body)
	}
	if !strings.Contains(body, `"id":"call_`) {
		t.Fatalf("转换出的调用应带生成的 id: %s", body)
	}
	if !strings.Contains(body, "好的，执行") {
		t.Fatalf("正文前缀不应丢失: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("finish_reason 应改写为 tool_calls: %s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Fatalf("[DONE] 应在最后: %s", body)
	}
}

// 原生 tool_calls 在前、转换调用在后：index 必须接续，不冲突。
func TestTextCallGWNativeThenConvertedIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	gw := newTextCallGateway(rec, []string{"Bash"}, "m")
	rec.Header().Set("Content-Type", "text/event-stream")
	gw.WriteHeader(200)

	gw.Write(gwChunk("m", map[string]any{"content": "查 [调用工具 Bash({\"command\": \"pwd\"})] 一下"}, nil))
	gw.Write(gwChunk("m", map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": "call_native", "type": "function",
		"function": map[string]any{"name": "Bash", "arguments": "{\"command\": \"date\"}"},
	}}}, nil))
	gw.Write(gwChunk("m", map[string]any{}, "stop"))
	gw.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	var converted map[string]any
	found := false
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(line[6:]), &obj) != nil {
			continue
		}
		chs, _ := obj["choices"].([]any)
		if len(chs) == 0 {
			continue
		}
		ch, _ := chs[0].(map[string]any)
		delta, _ := ch["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		if tcs, ok := delta["tool_calls"].([]any); ok {
			for _, e := range tcs {
				m, _ := e.(map[string]any)
				if m["id"] != "call_native" {
					converted = m
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("应存在转换出的调用块: %s", body)
	}
	if converted["index"].(float64) != 1 {
		t.Fatalf("转换调用 index 应为 1（接续原生 0），得到 %v", converted["index"])
	}
}

// ③ 单响应上限：超过部分按正文放行，不再无限转换。
func TestTextCallGWCap(t *testing.T) {
	t.Setenv("TEXTCALL_MAX_PER_RESPONSE", "2")
	rec := httptest.NewRecorder()
	gw := newTextCallGateway(rec, []string{"Bash"}, "m")
	rec.Header().Set("Content-Type", "text/event-stream")
	gw.WriteHeader(200)

	var body []byte
	for i := 0; i < 6; i++ {
		body = append(body, gwChunk("m", map[string]any{
			"content": `[调用工具 Bash({"command": "echo ` + string(rune('a'+i)) + `"})]`,
		}, nil)...)
	}
	body = append(body, gwChunk("m", map[string]any{}, "stop")...)
	body = append(body, []byte("data: [DONE]\n\n")...)
	gw.Write(body)

	out := rec.Body.String()
	// 只数转换块（finish_reason 的 "tool_calls" 值会干扰朴素计数，用 delta 形态锚定）
	if n := strings.Count(out, `"delta":{"tool_calls"`); n != 2 {
		t.Fatalf("上限 2 应只转换 2 个，得到 %d", n)
	}
	if n := strings.Count(out, "[调用工具 Bash("); n != 4 {
		t.Fatalf("溢出 4 个应按正文放行，得到 %d", n)
	}
	if !strings.Contains(out, `"finish_reason":"tool_calls"`) {
		t.Fatalf("有转换调用时 finish_reason 仍应改写: %s", out)
	}
}

// finish 之后到达的 usage 块：顺序保持（调用块 → finish → usage → [DONE]）。
func TestTextCallGWUsageAfterFinishOrder(t *testing.T) {
	rec := httptest.NewRecorder()
	gw := newTextCallGateway(rec, []string{"Bash"}, "m")
	rec.Header().Set("Content-Type", "text/event-stream")
	gw.WriteHeader(200)

	gw.Write(gwChunk("m", map[string]any{"content": `[调用工具 Bash({"command": "pwd"})]`}, nil))
	gw.Write(gwChunk("m", map[string]any{}, "stop"))
	usage := gwChunk("m", map[string]any{}, nil)
	usage = []byte(strings.Replace(string(usage), `"delta":{}`, `"delta":{},"usage":{"total_tokens":9}`, 1))
	// usage 块按惯例无 choices 或 choices 为空——这里直接构造空 choices 形态
	usage = []byte(`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"total_tokens":9}}` + "\n\n")
	gw.Write(usage)
	gw.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	iCalls := strings.Index(body, `"tool_calls"`)
	iFinish := strings.Index(body, `"finish_reason":"tool_calls"`)
	iUsage := strings.Index(body, `"total_tokens":9`)
	iDone := strings.Index(body, "data: [DONE]")
	if iCalls < 0 || iFinish < 0 || iUsage < 0 || iDone < 0 {
		t.Fatalf("缺少关键块: %s", body)
	}
	if !(iCalls < iFinish && iFinish < iUsage && iUsage < iDone) {
		t.Fatalf("块顺序错误: calls=%d finish=%d usage=%d done=%d", iCalls, iFinish, iUsage, iDone)
	}
}

// 非流式 JSON：content 改写 + message.tool_calls 追加 + finish 修正。
func TestTextCallGWJSONNonStream(t *testing.T) {
	rec := httptest.NewRecorder()
	gw := newTextCallGateway(rec, []string{"Bash"}, "m")
	rec.Header().Set("Content-Type", "application/json")
	gw.WriteHeader(200)

	resp := map[string]any{
		"id": "chatcmpl-y", "object": "chat.completion", "model": "m",
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": `执行 [调用工具 Bash({"command": "pwd"})] 完毕`,
			},
			"finish_reason": "stop",
		}},
	}
	b, _ := json.Marshal(resp)
	gw.Write(b)

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应应为合法 JSON: %v", err)
	}
	ch := got["choices"].([]any)[0].(map[string]any)
	msg := ch["message"].(map[string]any)
	if msg["content"] != "执行  完毕" {
		t.Fatalf("content 应剥离调用: %v", msg["content"])
	}
	tcs, ok := msg["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("应有 1 个转换出的 tool_calls: %v", msg["tool_calls"])
	}
	if ch["finish_reason"] != "tool_calls" {
		t.Fatalf("finish_reason 应改写: %v", ch["finish_reason"])
	}
	if _, hasIndex := tcs[0].(map[string]any)["index"]; hasIndex {
		t.Fatal("非流式 tool_calls 不应携带 index 字段")
	}
}

// reasoning 块透传不参与转换；无 pattern 的内容逐字节透传。
func TestTextCallGWPassthrough(t *testing.T) {
	rec := httptest.NewRecorder()
	gw := newTextCallGateway(rec, []string{"Bash"}, "m")
	rec.Header().Set("Content-Type", "text/event-stream")
	gw.WriteHeader(200)

	reasoning := gwChunk("m", map[string]any{"reasoning_content": "思考里有 [调用工具 Bash({})] 示例"}, nil)
	gw.Write(reasoning)
	plain := gwChunk("m", map[string]any{"content": "普通回答，没有调用"}, nil)
	gw.Write(plain)
	gw.Write(gwChunk("m", map[string]any{}, "stop"))
	gw.Write([]byte("data: [DONE]\n\n"))

	body := rec.Body.String()
	if !strings.Contains(body, "思考里有 [调用工具 Bash({})] 示例") {
		t.Fatalf("reasoning 块应原样透传: %s", body)
	}
	if !strings.Contains(body, "普通回答，没有调用") {
		t.Fatalf("普通内容应透传: %s", body)
	}
	if strings.Contains(body, `"tool_calls"`) {
		t.Fatalf("无转换时不应出现 tool_calls: %s", body)
	}
}

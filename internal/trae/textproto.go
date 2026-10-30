package trae

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ============ TraeCode raw 文本工具协议 ============
//
// llm_raw_chat 的 raw 端点只接受文本消息（raw_client.py build_raw_chat_body
// docstring："Sending OpenAI tools or generation options beside these fields
// makes the raw endpoint reject the request before the model can produce any
// output"）。工具定义必须文本化进 system 消息，模型以 <opencode_tool_call>
// 文本块发起调用，响应侧再把文本块解析回 OpenAI tool_calls。
//
// 上游对应物（2026-10-30 对照 .tmp/trae-up）：
//   - build_raw_messages + _compact_raw_history（请求侧消息文本化/历史压缩）
//   - build_runtime_system_prompt native_tools=False（运行时 system 提示）
//   - _tool_definitions_prompt + _tool_schema_budget（schema 48000 字符预算，
//     超出降级紧凑签名）
//   - ProtocolTextAccumulator/_hold_incomplete_tool_block + extract_tool_calls
//     cli_client.py（响应侧工具块提取/未闭合块隐藏）
//
// 有意不移植：response_style_instruction（简洁回答指令是上游 relay 的 UX
// 偏好而非协议必需，afree 是多客户端网关，不该替客户端决定输出风格）；
// _internal_wait_*（上游内部 wait 帧）与 ZedeX/Kimi 私有容错路径。

// 文本工具块的标签族（cli_client.py _TEXT_TOOL_BLOCK_RE）。opencode_tool_call
// 是系统提示教给模型的协议形态，其余是模型常见变体。
var textToolTags = []string{
	"opencode_tool_call", "toolcall", "tool_use", "tool_call", "tool_cell", "invoke",
}

const (
	defaultMaxRawMessages     = 80
	defaultMaxRawHistoryChars = 120000
	defaultToolSchemaBudget   = 48000
)

// rawHistoryLimit env 覆盖（对齐 _raw_history_limit：<=0 = 关闭该限制）。
func rawHistoryLimit(envName string, def int) int {
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n < 0 {
				return 0
			}
			return n
		}
	}
	return def
}

// toolSchemaBudget TRAE_RAW_MAX_TOOL_SCHEMA_CHARS（最小 1024）。
func toolSchemaBudget() int {
	if v := strings.TrimSpace(os.Getenv("TRAE_RAW_MAX_TOOL_SCHEMA_CHARS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1024 {
			return n
		}
	}
	return defaultToolSchemaBudget
}

// ============ 请求侧：运行时 system 提示 ============

// normToolDef 客户端工具定义 → {name,description,input_schema} 归一形态
// （_normalized_tools；OpenAI function 包裹与 Anthropic input_schema 都收）。
func normToolDef(raw map[string]any) (map[string]any, bool) {
	fn, _ := raw["function"].(map[string]any)
	name, desc, schema := "", "", any(nil)
	if fn != nil {
		name, _ = fn["name"].(string)
		desc, _ = fn["description"].(string)
		schema = fn["parameters"]
		if schema == nil {
			schema = fn["input_schema"]
		}
	} else {
		name, _ = raw["name"].(string)
		desc, _ = raw["description"].(string)
		schema = raw["input_schema"]
		if schema == nil {
			schema = raw["inputSchema"]
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	if schema == nil {
		schema = map[string]any{}
	}
	return map[string]any{"name": name, "description": desc, "input_schema": schema}, true
}

// buildToolDiscovery 工具名/命名空间摘要（build_tool_discovery）。
func buildToolDiscovery(defs []map[string]any) map[string]any {
	names := make([]string, 0, len(defs))
	namespaces := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, d := range defs {
		name := d["name"].(string)
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
		var ns string
		if i := strings.LastIndex(name, "__"); i > 0 {
			ns = strings.Trim(name[:i], "_")
		} else if i := strings.LastIndex(name, "."); i > 0 {
			ns = name[:i]
		}
		if ns != "" && !seen["\x00ns"+ns] {
			seen["\x00ns"+ns] = true
			namespaces = append(namespaces, ns)
		}
	}
	search := make([]string, 0, 2)
	envTools := make([]string, 0, 2)
	for _, name := range names {
		low := strings.ToLower(name)
		switch {
		case low == "tool_search" || low == "search_tools" || low == "list_tools" ||
			low == "plugin_search" || strings.Contains(low, "tool_search"):
			search = append(search, name)
		}
		for _, marker := range []string{"shell", "exec", "terminal", "environment", "workspace", "filesystem", "read_file", "list_dir", "glob"} {
			if strings.Contains(low, marker) {
				envTools = append(envTools, name)
				break
			}
		}
	}
	return map[string]any{
		"mode":                    "automatic",
		"declared_tools":          names,
		"plugin_namespaces":       namespaces,
		"tool_search_available":   len(search) > 0,
		"tool_search_tools":       search,
		"environment_probe_tools": envTools,
	}
}

// compactToolSignature 超预算降级用的紧凑签名（_compact_tool_signature）。
func compactToolSignature(d map[string]any) map[string]any {
	schema, _ := d["input_schema"].(map[string]any)
	fields := map[string]any{}
	if props, ok := schema["properties"].(map[string]any); ok {
		for name, rawHint := range props {
			hint := map[string]any{}
			if hm, ok := rawHint.(map[string]any); ok {
				if t, ok := hm["type"].(string); ok {
					hint["type"] = t
				}
				if e, ok := hm["enum"].([]any); ok && len(e) <= 20 {
					hint["enum"] = e
				}
				if t, _ := hm["type"].(string); t == "array" {
					if items, ok := hm["items"].(map[string]any); ok {
						if it, ok := items["type"].(string); ok {
							hint["items"] = it
						}
					}
				}
			}
			fields[name] = hint
		}
	}
	sig := map[string]any{"name": d["name"], "fields": fields}
	if req, ok := schema["required"].([]any); ok {
		names := make([]string, 0, len(req))
		for _, v := range req {
			if s, ok := v.(string); ok {
				names = append(names, s)
			}
		}
		sig["required"] = names
	}
	return sig
}

// toolDefinitionsPrompt 工具 schema 文本载荷（_tool_definitions_prompt）：
// 预算内全量 JSON；超出降级紧凑签名（discovery/execution 工具优先）。
func toolDefinitionsPrompt(defs []map[string]any) (payload string, compacted bool, omitted int) {
	full, _ := json.Marshal(defs)
	budget := toolSchemaBudget()
	if len(full) <= budget {
		return string(full), false, 0
	}
	discoveryNames := map[string]bool{
		"tool_search": true, "search_tools": true, "list_tools": true,
		"plugin_search": true, "environment": true, "get_environment": true,
	}
	executionNames := map[string]bool{
		"shell": true, "shell_exec": true, "exec": true, "exec_command": true,
		"download": true, "download_file": true, "write": true, "write_file": true,
		"file_write": true, "edit": true, "edit_file": true, "apply_patch": true,
	}
	priority := func(sig map[string]any) int {
		name := strings.ToLower(sig["name"].(string))
		base := name
		if i := strings.LastIndex(name, "__"); i >= 0 {
			base = name[i+2:]
		}
		if discoveryNames[name] || discoveryNames[base] {
			return 0
		}
		if executionNames[name] || executionNames[base] {
			return 1
		}
		return 2
	}
	sigs := make([]map[string]any, 0, len(defs))
	for _, d := range defs {
		sigs = append(sigs, compactToolSignature(d))
	}
	// 稳定排序：优先级升序，同级保持原有顺序
	for i := 1; i < len(sigs); i++ {
		for j := i; j > 0 && priority(sigs[j]) < priority(sigs[j-1]); j-- {
			sigs[j], sigs[j-1] = sigs[j-1], sigs[j]
		}
	}
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d["name"].(string))
	}
	payloadObj := map[string]any{
		"available_tools":    names,
		"signatures":         []any{},
		"omitted_signatures": len(sigs),
	}
	for _, sig := range sigs {
		cand := map[string]any{
			"available_tools":    names,
			"signatures":         append(append([]any{}, payloadObj["signatures"].([]any)...), sig),
			"omitted_signatures": len(sigs) - len(payloadObj["signatures"].([]any)) - 1,
		}
		enc, _ := json.Marshal(cand)
		if len(enc) > budget {
			continue
		}
		payloadObj = cand
	}
	enc, _ := json.Marshal(payloadObj)
	return string(enc), true, payloadObj["omitted_signatures"].(int)
}

// runtimeSystemPrompt 运行时 system 提示（build_runtime_system_prompt
// native_tools=False 分支；client_context 用请求自动发现形态）。
func runtimeSystemPrompt(defs []map[string]any, toolChoice any, parallel *bool) string {
	discovery := buildToolDiscovery(defs)
	context := map[string]any{
		"system_type": "Windows",
		"terminal_context": []any{map[string]any{
			"shell": "PowerShell", "cwd": ".", "source": "request-inferred",
		}},
		"client_name":    "afree-proxy",
		"context_source": "request-auto-discovery",
	}
	if len(defs) > 0 {
		delete(discovery, "declared_tools") // schema 载荷已带全部工具名
	}
	context["tool_discovery"] = discovery
	ctxJSON, _ := json.Marshal(context)

	lines := []string{
		"You are Trae Code connected to the user's local application.",
		"The external Trae-compatible terminal client application that started this conversation provides the client tools below.",
		"Caller client context (JSON):",
		string(ctxJSON),
		"Discover the local environment through the available client tools when the request requires it.",
		"If a tool can answer the question, call it proactively and wait for the external client result.",
		"Remote or server-side tools cannot write into the caller workspace. Never claim that a client file was downloaded, created, edited, patched, or saved unless a later matching client tool result with the same call id confirms success.",
		"A tool request, emitted tool-call block, plan, or assistant narration is not execution proof. If the matching client result reports failure, state the failure or request a corrective client tool; never report the operation as completed.",
		"Never repeat a completed tool call; do not describe that server's Linux filesystem as the caller workspace.",
		"Client tools are NOT native/server functions: never invoke them through function calling or any built-in tool, and never fall back to server tools (LS, Glob, Read, terminal) to reach the caller's files. The only way to call a client tool is the text block below; a native call reports 'not available' because the tool lives on the client.",
		`To use tools, emit only one or more JSON blocks and include no prose or final answer in that turn:`,
		`<opencode_tool_call>{"name":"tool_name","input":{}}</opencode_tool_call>`,
		"Do not invent an id field; the client assigns call ids. Keep file paths exactly as the user or tool result gave them.",
		"Fill input according to the selected tool schema and wait for the client result before continuing.",
	}
	if len(defs) > 0 {
		payload, compacted, omitted := toolDefinitionsPrompt(defs)
		schemaLabel := "Available client tool definitions and input schemas (JSON):"
		if compacted {
			schemaLabel = "Available client tools and compact input signatures (JSON). The full schema catalog exceeded the relay input budget"
			if omitted > 0 {
				schemaLabel += fmt.Sprintf("; %d signature(s) are name-only. Use an available tool_search/plugin discovery tool before guessing arguments", omitted)
			}
			schemaLabel += ":"
		}
		lines = append(lines,
			schemaLabel,
			payload,
			`To call a tool, emit one block per call in this format and no prose or final answer in the same turn: <opencode_tool_call>{"name":"tool_name","input":{}}</opencode_tool_call>. Omit the id field (the client assigns it), use an exact declared tool name, and fill input according to that tool's schema.`,
			"After the client returns a tool result, continue from that result until the user's request is complete.",
		)
	} else {
		lines = append(lines, "No client tools are available for this request; answer directly.")
	}
	switch tc := toolChoice.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(tc)) {
		case "none":
			lines = append(lines, "Tool choice is none: do not request a tool.")
		case "required":
			lines = append(lines, "Tool choice is required: request an available client tool before answering.")
		}
	case map[string]any:
		fn, _ := tc["function"].(map[string]any)
		if fn != nil {
			if name, _ := fn["name"].(string); name != "" {
				lines = append(lines, fmt.Sprintf("Tool choice requires the client tool named %s.", name))
			}
		}
	}
	if parallel != nil && !*parallel {
		lines = append(lines, "Request at most one tool per turn.")
	}
	return strings.Join(lines, "\n")
}

// ============ 请求侧：消息文本化 ============

// rawContentText content → 纯文本（_content_to_text：字符串直取；块数组取
// text/value 文本 join "\n"；对象 JSON 序列化）。
func rawContentText(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, block := range c {
			if s, ok := block.(string); ok {
				parts = append(parts, s)
				continue
			}
			bm, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := bm["text"].(string); ok && t != "" {
				parts = append(parts, t)
				continue
			}
			if t, ok := bm["content"].(string); ok && t != "" {
				parts = append(parts, t)
				continue
			}
			if bt, _ := bm["type"].(string); bt == "image_url" || bt == "image" {
				continue // 图片走原生 media 通道，不落文本占位
			}
			if bt, _ := bm["type"].(string); bt == "file" {
				parts = append(parts, "[Unsupported file input omitted]")
				continue
			}
			if v, ok := bm["value"]; ok && v != nil {
				if nested := rendererValueText(v); nested != "" {
					parts = append(parts, nested)
				}
				continue
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

// rendererValueText Trae 渲染器 value 载荷文本（renderer_block_text：
// value 可能是字符串或 [{type:text,value/text:...}] 块数组）。
func rendererValueText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, block := range c {
			bm, ok := block.(map[string]any)
			if !ok {
				if s, ok := block.(string); ok {
					parts = append(parts, s)
				}
				continue
			}
			if t, ok := bm["text"].(string); ok && t != "" {
				parts = append(parts, t)
			} else if t, ok := bm["value"].(string); ok && t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// nativeImageBlocks 抽出 content 里的图片块（_native_image_blocks：renderer
// image_url 形态；raw 传输把媒体放进块数组内容）。
func nativeImageBlocks(content any) []map[string]any {
	blocks, ok := content.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, b := range blocks {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		bt, _ := bm["type"].(string)
		if bt != "image_url" && bt != "image" {
			continue
		}
		if bt == "image" {
			// Anthropic image 块 → renderer image_url
			out = append(out, convertImageBlock(anthropicImageDataURL(bm), bm))
			continue
		}
		out = append(out, convertImageBlock(imageURLString(bm), bm))
	}
	return out
}

// imageURLString OpenAI image_url 块取 URL。
func imageURLString(bm map[string]any) string {
	if iu, ok := bm["image_url"].(map[string]any); ok {
		u, _ := iu["url"].(string)
		return u
	}
	u, _ := bm["url"].(string)
	return u
}

// anthropicImageDataURL Anthropic image 块 → data URL（base64 源）。
func anthropicImageDataURL(bm map[string]any) string {
	src, _ := bm["source"].(map[string]any)
	if src == nil {
		return ""
	}
	st, _ := src["type"].(string)
	if st == "base64" {
		mt, _ := src["media_type"].(string)
		data, _ := src["data"].(string)
		return "data:" + mt + ";base64," + data
	}
	if st == "url" {
		u, _ := src["url"].(string)
		return u
	}
	return ""
}

// serializeToolCallContext assistant 历史工具调用文本化
// （_serialize_tool_call_context）。
func serializeToolCallContext(v any) string {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	lines := []string{
		"Client tool calls already issued in this conversation " +
			"(history only; use the matching results below and do not repeat them):",
	}
	count := 0
	for _, raw := range list {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		if fn == nil {
			fn = call
		}
		name, _ := fn["name"].(string)
		if name == "" {
			name = "tool"
		}
		id, _ := call["id"].(string)
		if id == "" {
			id, _ = call["tool_call_id"].(string)
		}
		if id == "" {
			id = "unknown"
		}
		args := ""
		switch a := fn["arguments"].(type) {
		case string:
			args = a
		case nil:
			if inp, ok := fn["input"]; ok && inp != nil {
				b, _ := json.Marshal(inp)
				args = string(b)
			}
		default:
			b, _ := json.Marshal(a)
			args = string(b)
		}
		lines = append(lines, fmt.Sprintf("Client tool call [%s] %s; arguments: %s", id, name, args))
		count++
	}
	if count == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// rawHistoryMessageSize 历史消息尺寸（_raw_history_message_size：文本 +
// 工具调用 JSON）。
func rawHistoryMessageSize(m map[string]any) int {
	size := len(rawContentText(m["content"]))
	if tcs, ok := m["tool_calls"].([]any); ok && len(tcs) > 0 {
		b, _ := json.Marshal(tcs)
		size += len(b)
	}
	if fc, ok := m["function_call"].(map[string]any); ok {
		b, _ := json.Marshal(fc)
		size += len(b)
	}
	return size
}

// compactRawHistory 历史压缩（_compact_raw_history）：unique system 全保，
// 非系统消息保留最近 N 条并受字符预算约束；孤儿 tool result 前补回紧邻的
// assistant 调用消息。
func compactRawHistory(msgs []map[string]any) []map[string]any {
	maxMessages := rawHistoryLimit("TRAE_RAW_MAX_MESSAGES", defaultMaxRawMessages)
	maxChars := rawHistoryLimit("TRAE_RAW_MAX_HISTORY_CHARS", defaultMaxRawHistoryChars)
	if len(msgs) == 0 || (len(msgs) <= maxMessages && maxChars == 0) {
		return msgs
	}
	systemIdx := map[int]bool{}
	var nonSystem []int
	seenSystem := map[string]bool{}
	for i, m := range msgs {
		role, _ := m["role"].(string)
		if role == "system" {
			key := rawContentText(m["content"])
			if seenSystem[key] {
				continue
			}
			seenSystem[key] = true
			systemIdx[i] = true
			continue
		}
		nonSystem = append(nonSystem, i)
	}
	if maxMessages > 0 && len(nonSystem) > maxMessages {
		nonSystem = nonSystem[len(nonSystem)-maxMessages:]
	}
	if maxChars > 0 {
		remaining := maxChars
		for i := range msgs {
			if systemIdx[i] {
				remaining -= rawHistoryMessageSize(msgs[i])
			}
		}
		kept := make([]int, 0, len(nonSystem))
		used := 0
		for j := len(nonSystem) - 1; j >= 0; j-- {
			i := nonSystem[j]
			size := rawHistoryMessageSize(msgs[i])
			if len(kept) > 0 && used+size > remaining {
				break
			}
			kept = append([]int{i}, kept...)
			used += size
		}
		nonSystem = kept
	}
	// 不让保留段以孤儿 tool result 开头
	if len(nonSystem) > 0 {
		first := nonSystem[0]
		if role, _ := msgs[first]["role"].(string); role == "tool" && first > 0 {
			if prevRole, _ := msgs[first-1]["role"].(string); prevRole == "assistant" {
				if tcs, ok := msgs[first-1]["tool_calls"].([]any); ok && len(tcs) > 0 {
					nonSystem = append([]int{first - 1}, nonSystem...)
				}
			}
		}
	}
	keep := map[int]bool{}
	for i := range systemIdx {
		keep[i] = true
	}
	for _, i := range nonSystem {
		keep[i] = true
	}
	out := make([]map[string]any, 0, len(keep))
	for i, m := range msgs {
		if keep[i] {
			out = append(out, m)
		}
	}
	return out
}

// messageHasToolTrace 消息是否带工具痕迹（role=tool / tool_calls /
// function_call；renderer tool_use/tool_result 块）。
func messageHasToolTrace(m map[string]any) bool {
	if role, _ := m["role"].(string); role == "tool" {
		return true
	}
	if tcs, ok := m["tool_calls"].([]any); ok && len(tcs) > 0 {
		return true
	}
	if _, ok := m["function_call"].(map[string]any); ok {
		return true
	}
	if blocks, ok := m["content"].([]any); ok {
		for _, b := range blocks {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			switch bt, _ := bm["type"].(string); bt {
			case "tool_use", "tool_call", "function_call", "function", "tool_result":
				return true
			}
		}
	}
	return false
}

// buildRawMessages OpenAI messages → raw_chat 文本消息
// （build_raw_messages：历史压缩 → 运行时 system 注入 → 逐条文本化）。
func buildRawMessages(msgs []any, toolDefs []map[string]any, hasToolsKey bool, params map[string]any) []map[string]any {
	norm := make([]map[string]any, 0, len(msgs))
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		switch role {
		case "developer":
			role = "system"
		case "function":
			role = "tool"
		}
		cp := map[string]any{"role": role, "content": m["content"]}
		if tcs, ok := m["tool_calls"].([]any); ok && len(tcs) > 0 {
			cp["tool_calls"] = tcs
		}
		if fc, ok := m["function_call"].(map[string]any); ok {
			cp["function_call"] = fc
		}
		if id, ok := m["tool_call_id"].(string); ok && id != "" {
			cp["tool_call_id"] = id
		}
		if name, ok := m["name"].(string); ok && name != "" {
			cp["name"] = name
		}
		if b, ok := m["is_error"].(bool); ok {
			cp["is_error"] = b
		}
		norm = append(norm, cp)
	}
	norm = compactRawHistory(norm)

	hasToolProtocol := hasToolsKey && len(toolDefs) > 0
	if !hasToolProtocol {
		for _, m := range norm {
			if messageHasToolTrace(m) {
				hasToolProtocol = true
				break
			}
		}
	}
	var toolChoice any
	var parallel *bool
	if hasToolProtocol {
		toolChoice = params["tool_choice"]
		if ptc, ok := params["parallel_tool_calls"].(bool); ok {
			parallel = &ptc
		}
	}

	out := make([]map[string]any, 0, len(norm)+1)
	if hasToolProtocol {
		out = append(out, map[string]any{
			"role":    "system",
			"content": []map[string]any{{"type": "text", "text": runtimeSystemPrompt(toolDefs, toolChoice, parallel)}},
		})
	}
	for _, m := range norm {
		role, _ := m["role"].(string)
		content := m["content"]
		text := rawContentText(content)
		images := nativeImageBlocks(content)
		if text == "" && len(images) == 0 {
			// content 空时回退常见替代键（parts/text/prompt/message/input）
			for _, key := range []string{"parts", "text", "prompt", "message", "input"} {
				if cand, ok := m[key]; ok && cand != nil {
					if s := rawContentText(cand); s != "" {
						text = s
						break
					}
				}
			}
		}
		switch role {
		case "assistant":
			text = stripTextToolBlocks(text)
			if ctx := serializeToolCallContext(m["tool_calls"]); ctx != "" {
				if text != "" {
					text += "\n\n"
				}
				text += ctx
			}
			if text == "" {
				continue // 空 assistant 回合（无调用）丢弃
			}
		case "tool":
			id, _ := m["tool_call_id"].(string)
			if id == "" {
				id = "unknown"
			}
			name, _ := m["name"].(string)
			if name == "" {
				name = "tool"
			}
			status := "succeeded"
			if b, ok := m["is_error"].(bool); ok && b {
				status = "failed"
			}
			text = fmt.Sprintf("Client tool result [%s] %s status=%s:\n%s", id, name, status, text)
			role = "user"
		case "system", "user":
			if text == "" && len(images) == 0 {
				continue // 空 system/user 丢弃
			}
		default:
			role = "user"
		}
		entry := map[string]any{"role": role}
		blocks := make([]map[string]any, 0, 1+len(images))
		if text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
		blocks = append(blocks, images...)
		entry["content"] = blocks
		out = append(out, entry)
	}
	return out
}

// ============ 响应侧：文本工具块提取 ============

var (
	textToolOpenRe  = regexp.MustCompile(`(?i)<(opencode_tool_call|toolcall|tool_use|tool_call|tool_cell|invoke)(\s[^>]*)?>`)
	textToolNameRe  = regexp.MustCompile(`(?i)name\s*=\s*"([^"]*)"|name\s*=\s*'([^']*)'`)
	argKeyValueRe   = regexp.MustCompile(`(?is)(?:<arg_key>\s*)?([^<>\r\n]+?)\s*</arg_key>\s*<arg_value(?:\s[^>]*)?>\s*([\s\S]*?)\s*</arg_value>`)
	toolNameXMLRe   = regexp.MustCompile(`(?is)<(?:tool_name|server_name|name)>\s*([^<]+?)\s*</(?:tool_name|server_name|name)>`)
	toolInputXMLRe  = regexp.MustCompile(`(?is)<input>\s*([\s\S]*?)\s*</input>`)
	toolAttrPairsRe = regexp.MustCompile(`(?i)([A-Za-z_][A-Za-z0-9_.:-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

// tagCloseAt 在 text 中查找 tag 的闭合标签，返回起始下标（-1 = 无）。
func tagCloseAt(text, tag string, from int) int {
	for _, variant := range []string{"</" + tag + ">", "</" + tag + " >"} {
		if i := indexFold(text, variant, from); i >= 0 {
			return i
		}
	}
	return -1
}

// indexFold 大小写无关查找（块内体量小，朴素实现足够）。
func indexFold(text, sub string, from int) int {
	n := len(text) - len(sub)
	for i := from; i <= n; i++ {
		if strings.EqualFold(text[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

// span 占用区间（工具块在原文中的范围）。
type span struct{ start, end int }

// extractTextToolCalls 从累计原始文本提取文本协议工具调用与可见文本。
// 返回 visible（完整块剥离、未闭合块及疑似前缀截掉的可见文本）与 calls
// （按出现顺序；ID 留空由消费层兜底生成）。
func extractTextToolCalls(raw string) (string, []ToolCall) {
	if !strings.Contains(raw, "<") {
		return raw, nil
	}
	var occupied []span
	var calls []ToolCall

	pos := 0
	for {
		m := textToolOpenRe.FindStringIndex(raw[pos:])
		if m == nil {
			break
		}
		start := pos + m[0]
		openEnd := pos + m[1]
		tag := strings.ToLower(strings.TrimPrefix(raw[start:openEnd], "<"))
		if i := strings.IndexAny(tag, " \t\r\n>"); i >= 0 {
			tag = tag[:i]
		}
		closeIdx := tagCloseAt(raw, tag, openEnd)
		if closeIdx < 0 {
			// 未闭合块：从这里截掉全部剩余（流式中块尚未写完）
			break
		}
		openingTag := raw[start:openEnd]
		body := raw[openEnd:closeIdx]
		call, ok := parseTextToolBody(body, openingTag)
		if ok {
			calls = append(calls, call)
		}
		occupied = append(occupied, span{start, closeIdx + len(tag) + 3})
		pos = closeIdx + len(tag) + 3
	}
	visible := cutSpans(raw, occupied)
	// 尾部疑似未闭合块前缀（如 "<opencode_tool_c"）：截掉
	if idx := strings.LastIndexByte(visible, '<'); idx >= 0 {
		tail := strings.ToLower(visible[idx:])
		for _, tag := range textToolTags {
			if strings.HasPrefix("<"+tag, tail) || strings.HasPrefix(tail, "<"+tag) {
				visible = visible[:idx]
				break
			}
		}
	}
	// 去重（同名同参的重复块只留首个）
	seen := map[string]bool{}
	dedup := make([]ToolCall, 0, len(calls))
	for _, c := range calls {
		key := c.Name + "\x00" + c.ArgsJSON
		if seen[key] {
			continue
		}
		seen[key] = true
		dedup = append(dedup, c)
	}
	return visible, dedup
}

// cutSpans 按占用区间裁剪文本（区间按序合并后拼接剩余部分）。
func cutSpans(text string, spans []span) string {
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	pos := 0
	for _, s := range spans {
		if s.start < pos {
			if s.end > pos {
				pos = s.end
			}
			continue
		}
		if s.start > pos {
			b.WriteString(text[pos:s.start])
		}
		if s.end > pos {
			pos = s.end
		}
	}
	if pos < len(text) {
		b.WriteString(text[pos:])
	}
	return b.String()
}

// parseTextToolBody 解析工具块 body（_parse_text_tool_body 务实子集）：
// JSON 对象（name+input/arguments/...）→ arg_key/value XML → name 属性形态。
func parseTextToolBody(body, openingTag string) (ToolCall, bool) {
	tc := ToolCall{}
	// name 属性（<invoke name="X"> 等）
	if m := textToolNameRe.FindStringSubmatch(openingTag); m != nil {
		if m[1] != "" {
			tc.Name = m[1]
		} else {
			tc.Name = m[2]
		}
	}
	raw := strings.TrimSpace(body)
	if raw == "" {
		if tc.Name != "" {
			return tc, true
		}
		return ToolCall{}, false
	}
	if obj, ok := firstJSONObject(raw); ok {
		name, _ := obj["name"].(string)
		if name == "" {
			name, _ = obj["tool_name"].(string)
		}
		args := ""
		hasArgs := false
		for _, key := range []string{"input", "arguments", "args", "params", "parameters"} {
			if v, present := obj[key]; present && v != nil {
				args = stringifyToolArgs(v)
				hasArgs = true
				break
			}
		}
		if !hasArgs {
			// 参数平铺在顶层（{"query":"golang"}）：剥协议字段后整包当参数
			//（对齐 normalize_tool_call 顶层余量语义）
			flat := map[string]any{}
			for k, v := range obj {
				switch strings.ToLower(k) {
				case "name", "tool_name", "toolcallname", "tool_call_name", "server_name",
					"id", "type", "index", "function", "function_call", "functioncall",
					"tool_call_id", "toolcallid", "input", "arguments", "args", "params", "parameters":
					continue
				}
				flat[k] = v
			}
			if len(flat) > 0 {
				args = marshalCompact(flat)
				hasArgs = true
			}
		}
		// JSON 显式名优先；无名但有参数时用开标签属性名兜底
		if name != "" || (hasArgs && tc.Name != "") {
			if name != "" {
				tc.Name = name
			}
			tc.ArgsJSON = args
			return tc, true
		}
	}
	// arg_key/arg_value 形态
	if matches := argKeyValueRe.FindAllStringSubmatch(raw, -1); len(matches) > 0 {
		fields := map[string]any{}
		for _, m := range matches {
			key := strings.TrimSpace(m[1])
			if key == "" {
				continue
			}
			fields[key] = strings.TrimSpace(m[2])
		}
		if tc.Name == "" && len(fields) > 0 {
			// "ToolName arg" 首键形态
			for k := range fields {
				if i := strings.IndexByte(k, ' '); i > 0 {
					tc.Name = k[:i]
				}
				break
			}
			if tc.Name == "" {
				prefix := strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(raw[:argKeyValueRe.FindStringIndex(raw)[0]], " "))
				if words := strings.Fields(prefix); len(words) > 0 {
					tc.Name = words[0]
				}
			}
		}
		if tc.Name != "" {
			tc.ArgsJSON = marshalCompact(fields)
			return tc, true
		}
	}
	// <tool_name>X</tool_name><input>{...}</input> XML 形态
	if m := toolNameXMLRe.FindStringSubmatch(raw); m != nil {
		tc.Name = strings.TrimSpace(m[1])
	}
	if m := toolInputXMLRe.FindStringSubmatch(raw); m != nil {
		tc.ArgsJSON = strings.TrimSpace(m[1])
		if tc.Name != "" {
			return tc, true
		}
	}
	// 开标签属性字段（name="Read" path="a.txt"）
	if tc.Name == "" {
		pairs := toolAttrPairs(toolAttrPairsRe, openingTag)
		if n, ok := pairs["name"].(string); ok && n != "" {
			tc.Name = n
		}
	}
	if tc.Name != "" && tc.ArgsJSON == "" {
		if pairs := toolAttrPairs(toolAttrPairsRe, raw); len(pairs) > 0 {
			delete(pairs, "name")
			delete(pairs, "id")
			delete(pairs, "type")
			if len(pairs) > 0 {
				tc.ArgsJSON = marshalCompact(pairs)
			}
		}
	}
	if tc.Name != "" {
		return tc, true
	}
	return ToolCall{}, false
}

// toolAttrPairs 属性串 → map。
func toolAttrPairs(re *regexp.Regexp, s string) map[string]any {
	out := map[string]any{}
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		key := strings.ToLower(strings.TrimSpace(m[1]))
		val := m[2]
		if val == "" {
			val = m[3]
		}
		if val == "" {
			val = m[4]
		}
		out[key] = val
	}
	return out
}

// firstJSONObject 提取文本中首个平衡的 {...} 并解析（容错：前后有杂讯）。
func firstJSONObject(raw string) (map[string]any, bool) {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return nil, false
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if esc {
			esc = false
			continue
		}
		switch ch {
		case '\\':
			if inStr {
				esc = true
			}
		case '"':
			inStr = !inStr
		case '{':
			if !inStr {
				depth++
			}
		case '}':
			if !inStr {
				depth--
				if depth == 0 {
					var obj map[string]any
					if err := json.Unmarshal([]byte(raw[start:i+1]), &obj); err == nil {
						return obj, true
					}
					// 尾逗号等宽松重试
					fixed := strings.TrimSpace(raw[start : i+1])
					fixed = strings.TrimSuffix(fixed, ",") + "}"
					if err := json.Unmarshal([]byte(fixed), &obj); err == nil {
						return obj, true
					}
					return nil, false
				}
			}
		}
	}
	return nil, false
}

// stringifyToolArgs 参数归一为 JSON 字符串（对象/数组紧凑序列化；字符串
// 原样；其他 JSON 化）。
func stringifyToolArgs(v any) string {
	switch a := v.(type) {
	case string:
		if strings.TrimSpace(a) == "" {
			return "{}"
		}
		return a
	case nil:
		return "{}"
	default:
		return marshalCompact(a)
	}
}

func marshalCompact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// stripTextToolBlocks 只剥离工具块（历史 sanitize 用，不提取调用）。
func stripTextToolBlocks(text string) string {
	visible, _ := extractTextToolCalls(text)
	return visible
}

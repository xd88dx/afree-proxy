// 网关层「文本形式工具调用兜底转换」的响应出口接线。
//
// ## 为什么放在网关层
//
// 各上游引擎都会把 assistant 历史 tool_calls 编码成文本 `[调用工具 name(args)]`
// 混进对话，模型会模仿这个格式在正文里"输出调用"，客户端拿到纯文本不执行。
// 引擎内转换（vendored trae2api server/textcall.go）只护住 Trae 一家；wbcn 等
// 其他引擎没有兜底（2026-11-06 实测：wbcn 会话模仿轮次直接漏成字符串）。
// 本文件把转换上移到 /v1/chat/completions 的唯一出口 chatHandler：包一层
// ResponseWriter，**所有引擎、所有模型**的响应统一过闸，一处接入全量生效。
//
// ## 工作方式
//
// 流式（text/event-stream）：逐事件解析引擎写出的 `data: {chunk}`——
//   - delta.content → 喂给过滤器，用释放的正文重写该块（暂扣/剥离时整块不发）；
//   - delta.tool_calls（原生）→ 透传，并记录 index 上限（转换调用接在其后，避免冲突）；
//   - finish_reason 块 → 暂扣；[DONE] 时统一收尾：转换出的调用块先发，
//     finish_reason 为 stop 时改写为 tool_calls，再补发 finish 后暂存的事件（usage 等）；
//   - 其余（role 空块、reasoning 块、usage、注释行、非 choices 事件）原样透传。
//
// 非流式（application/json）：聚合完整 JSON 后对 message.content 做一次性解析，
// 改写 content / 追加 message.tool_calls / 修正 finish_reason。
//
// trae 引擎内部已有一层转换（vendored server/textcall.go）；转过的流到这里无
// 文本可转，外层自然空转，两层互不干扰——内层是第一道保险，本层兜住其余所有引擎。
package app

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"afree-proxy/internal/textcall"
)

// textcallMaxPerResponse 读取单响应转换上限（环境变量可覆盖，默认 30）。
func textcallMaxPerResponse() int {
	if v := os.Getenv("TEXTCALL_MAX_PER_RESPONSE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return textcall.DefaultMaxPerResponse
}

// textcallRandHex 生成 tool_call id 用的随机 hex（不引入第三方依赖）。
func textcallRandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type textCallGW struct {
	http.ResponseWriter
	filter    *textcall.Filter
	names     []string
	model     string
	mode      string // "sse" | "json" | "raw"；"" = 尚未嗅探
	sseBuf    []byte
	jsonBuf   []byte
	jsonDone  bool
	nextIdx   int      // 下一个可用 tool_calls index（原生调用推进它，转换调用消费它）
	finishObj map[string]any
	finishEvt []byte
	haveFin   bool
	heldTail  [][]byte // finish 之后、[DONE] 之前的事件（usage 等）
}

// newTextCallGateway 包装引擎可见的 ResponseWriter。names 为请求声明的工具名
// 清单；为空时调用方应直接跳过包装（零开销）。
func newTextCallGateway(w http.ResponseWriter, names []string, model string) *textCallGW {
	return &textCallGW{
		ResponseWriter: w,
		filter:         textcall.New(names, textcallMaxPerResponse()),
		names:          names,
		model:          model,
	}
}

func (g *textCallGW) WriteHeader(code int) {
	if g.mode == "" {
		ct := g.Header().Get("Content-Type")
		switch {
		case strings.Contains(ct, "text/event-stream"):
			g.mode = "sse"
		case strings.Contains(ct, "application/json"):
			g.mode = "json"
		default:
			g.mode = "raw"
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *textCallGW) Flush() {
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *textCallGW) Write(p []byte) (int, error) {
	if g.mode == "" {
		// 引擎未显式 WriteHeader（隐式 200）：按首块内容嗅探
		t := bytes.TrimLeft(p, " \t\r\n")
		switch {
		case bytes.HasPrefix(t, []byte("data: ")) || bytes.HasPrefix(t, []byte("event: ")):
			g.mode = "sse"
		case bytes.HasPrefix(t, []byte("{")):
			g.mode = "json"
		default:
			g.mode = "raw"
		}
	}
	switch g.mode {
	case "sse":
		g.sseBuf = append(g.sseBuf, p...)
		for {
			i := bytes.Index(g.sseBuf, []byte("\n\n"))
			if i < 0 {
				break
			}
			evt := g.sseBuf[:i+2]
			g.sseBuf = g.sseBuf[i+2:]
			g.handleSSEEvent(evt)
		}
		return len(p), nil
	case "json":
		return g.handleJSONWrite(p)
	default:
		return g.ResponseWriter.Write(p)
	}
}

func (g *textCallGW) rawWrite(p []byte) {
	_, _ = g.ResponseWriter.Write(p)
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// handleSSEEvent 处理一个完整的 SSE 事件（含结尾空行）。
func (g *textCallGW) handleSSEEvent(evt []byte) {
	if !bytes.HasPrefix(evt, []byte("data: ")) {
		g.rawWrite(evt) // 注释行 / event: 行等
		return
	}
	payload := strings.TrimSpace(string(bytes.TrimPrefix(evt, []byte("data: "))))
	if payload == "[DONE]" {
		g.finalize()
		g.rawWrite(evt)
		return
	}
	var obj map[string]any
	if json.Unmarshal([]byte(payload), &obj) != nil {
		g.rawWrite(evt) // 非 JSON 负载：透传
		return
	}
	chs, _ := obj["choices"].([]any)
	if len(chs) == 0 {
		g.holdOrPass(evt) // usage 尾块等
		return
	}
	ch, _ := chs[0].(map[string]any)
	if ch == nil {
		g.holdOrPass(evt)
		return
	}
	if fr := ch["finish_reason"]; fr != nil {
		// 结束块：暂扣。收尾时先补发转换出的调用块，再发它（必要时改写 stop）。
		g.finishEvt = evt
		g.finishObj = obj
		g.haveFin = true
		return
	}
	delta, _ := ch["delta"].(map[string]any)
	if delta == nil {
		g.holdOrPass(evt)
		return
	}
	if tcs, ok := delta["tool_calls"].([]any); ok {
		// 原生工具调用：透传；推进 index 游标，转换调用接在其后避免冲突
		for _, e := range tcs {
			if m, ok := e.(map[string]any); ok {
				if idx, ok := m["index"].(float64); ok && int(idx) >= g.nextIdx {
					g.nextIdx = int(idx) + 1
				}
			}
		}
		g.holdOrPass(evt)
		return
	}
	content, has := delta["content"].(string)
	if !has || content == "" {
		// role 首块（content:""）、reasoning 块等：透传（reasoning 不参与转换）
		g.holdOrPass(evt)
		return
	}
	// 正文增量：喂过滤器，用释放的正文重写该块
	released := g.filter.Feed(content)
	if released == "" {
		return // 全部被暂扣/剥离：该块不下发
	}
	delta["content"] = released
	g.emitChunkObj(obj)
}

// holdOrPass finish 之后到达的杂项事件先暂存，收尾时按原顺序补发；
// 否则原样透传。
func (g *textCallGW) holdOrPass(evt []byte) {
	if g.haveFin {
		g.heldTail = append(g.heldTail, evt)
		return
	}
	g.rawWrite(evt)
}

// emitChunkObj 把改写后的 chunk 对象序列化下发。
func (g *textCallGW) emitChunkObj(obj map[string]any) {
	data, err := json.Marshal(obj)
	if err != nil {
		return
	}
	g.rawWrite(append(append([]byte("data: "), data...), '\n', '\n'))
}

// emitToolCallChunk 以 finish 块的信封下发一个转换出的工具调用块。
// id 为空时补生成——OpenAI 规范 tool_calls[].id 必填，缺 id 会被严格客户端拒收。
func (g *textCallGW) emitToolCallChunk(index int, c textcall.Call) {
	id := c.ID
	if id == "" {
		id = "call_" + textcallRandHex(8)
	}
	env := map[string]any{
		// 注意：这是响应 id（chatcmpl-*），不是 tool_call id；后者已由 id 变量带入
		"id":      fmt.Sprintf("chatcmpl-%x", time.Now().UnixNano()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   g.model,
	}
	if g.finishObj != nil {
		for _, k := range []string{"id", "created", "model"} {
			if v, ok := g.finishObj[k]; ok {
				env[k] = v
			}
		}
	}
	env["choices"] = []any{map[string]any{
		"index":         0,
		"delta": map[string]any{"tool_calls": []any{map[string]any{
			"index": index,
			"id":    id,
			"type":  "function",
			"function": map[string]any{
				"name":      c.Name,
				"arguments": c.Args,
			},
		}}},
		"finish_reason": nil,
	}}
	g.emitChunkObj(env)
}

// finalize 在 [DONE] 前统一收尾：补发剩余正文、转换出的调用块、被暂扣的
// finish 块（必要时改写 finish_reason）与 finish 后暂存的事件。
func (g *textCallGW) finalize() {
	tail, calls := g.filter.Finish()
	if tail != "" {
		env := map[string]any{
			"id":      fmt.Sprintf("chatcmpl-%x", time.Now().UnixNano()),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   g.model,
		}
		if g.finishObj != nil {
			for _, k := range []string{"id", "created", "model"} {
				if v, ok := g.finishObj[k]; ok {
					env[k] = v
				}
			}
		}
		env["choices"] = []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"content": tail},
			"finish_reason": nil,
		}}
		g.emitChunkObj(env)
	}
	for i, c := range calls {
		g.emitToolCallChunk(g.nextIdx+i, c)
	}
	g.nextIdx += len(calls)
	if g.haveFin {
		if len(calls) > 0 {
			// 有转换出的调用：finish_reason 为 stop 时必须改写，否则客户端
			// 不会执行工具（上游引擎逐层透传时常见 stop）
			if chs, ok := g.finishObj["choices"].([]any); ok && len(chs) > 0 {
				if ch, ok := chs[0].(map[string]any); ok {
					if fr, _ := ch["finish_reason"].(string); fr == "" || fr == "stop" {
						ch["finish_reason"] = "tool_calls"
					}
				}
			}
			if data, err := json.Marshal(g.finishObj); err == nil {
				g.rawWrite(append(append([]byte("data: "), data...), '\n', '\n'))
			} else {
				g.rawWrite(g.finishEvt)
			}
		} else {
			g.rawWrite(g.finishEvt)
		}
		for _, e := range g.heldTail {
			g.rawWrite(e)
		}
		g.heldTail = nil
		g.haveFin = false
	}
	// 观测：转换/溢出/拒绝情况进日志，出问题可从容器日志直接定位
	if n := g.filter.Calls(); n > 0 || g.filter.Dropped() > 0 {
		log.Printf("[textcall] 网关层转换 %d 个文本形式工具调用为原生 tool_calls（model=%s）", n, g.model)
		if d := g.filter.Dropped(); d > 0 {
			log.Printf("[textcall] WARN: %d 个调用超过单响应转换上限，按正文放行（model=%s）——模型疑似病态模仿，建议开新会话", d, g.model)
		}
	}
	for _, r := range g.filter.Rejections() {
		log.Printf("[textcall] WARN: 疑似文本工具调用未转换: %s（model=%s）", r, g.model)
	}
}

// handleJSONWrite 处理非流式 JSON 响应：聚合完整 JSON 后一次性转换。
func (g *textCallGW) handleJSONWrite(p []byte) (int, error) {
	if g.jsonDone {
		return g.ResponseWriter.Write(p)
	}
	g.jsonBuf = append(g.jsonBuf, p...)
	var obj map[string]any
	if err := json.Unmarshal(g.jsonBuf, &obj); err != nil {
		if len(g.jsonBuf) > 16<<20 { // 兜底：始终解析不了的原样放行
			g.jsonDone = true
			return g.ResponseWriter.Write(g.jsonBuf)
		}
		return len(p), nil // 还没攒齐，等下一个增量
	}
	g.jsonDone = true
	if chs, ok := obj["choices"].([]any); ok && len(chs) > 0 {
		if ch, ok := chs[0].(map[string]any); ok {
			if msg, ok := ch["message"].(map[string]any); ok {
				if content, _ := msg["content"].(string); content != "" {
					prose, calls, changed, capped := textcall.ExtractTextToolCalls(
						content, g.names, textcallMaxPerResponse())
					if changed {
						msg["content"] = prose
						existing, _ := msg["tool_calls"].([]any)
						for _, c := range calls {
							c.ID = "call_" + textcallRandHex(8)
							// 非流式 message.tool_calls 无 index 字段（OpenAI 规范仅在流式分片用）
							existing = append(existing, map[string]any{
								"id":   c.ID,
								"type": "function",
								"function": map[string]any{
									"name":      c.Name,
									"arguments": c.Args,
								},
							})
						}
						msg["tool_calls"] = existing
						if fr, _ := ch["finish_reason"].(string); (fr == "" || fr == "stop") && len(calls) > 0 {
							ch["finish_reason"] = "tool_calls"
						}
						log.Printf("[textcall] 网关层转换 %d 个文本形式工具调用为原生 tool_calls（非流式，model=%s）", len(calls), g.model)
						if capped > 0 {
							log.Printf("[textcall] WARN: 非流式响应 %d 个调用超过单响应转换上限，按正文放行（model=%s）", capped, g.model)
						}
					}
				}
			}
		}
	}
	if data, err := json.Marshal(obj); err == nil {
		g.jsonBuf = nil
		return g.ResponseWriter.Write(data)
	}
	g.jsonDone = true
	return g.ResponseWriter.Write(g.jsonBuf)
}

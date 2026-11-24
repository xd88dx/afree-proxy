// dsml.go 通用网关侧的「DeepSeek 系模型原生工具调用标记」还原层。
//
// 背景（2026-11-25 排查 Claude Code InputValidationError 时确认）：
// 链路是 Claude Code → new-api(Claude Messages→OpenAI Compatible) → afree /v1/chat/completions
// → tkhb/zen → deepseek-v4.1-flash。DeepSeek 系模型把工具调用写成带全角双竖线的
// 标记文本（DSML），而不是 OpenAI 的 JSON tool_calls。afree 的通用出口此前只识别
// `[调用工具 name(args)]`，不认识 DSML，于是标记文本以正文泄漏给客户端； ClaudCode
// 把它当普通助手文本输出，工具一次都没跑。
//
// 本文件从 modules/workbuddy2api/internal/upstream/dsml.go 移植核心解析逻辑到 root
// module（独立 module 的 internal 包无法被 root import），供统一出口 textCallGW 使用。
//
// 判定原则与来源实现一致， conservative 优先：
//   - 只有块结构完美闭合、块内除 invoke/parameter 外无正文、工具名在请求声明的 tools
//     清单里，才还原成 tool_calls；
//   - 任何拿不准的分支原文回吐，绝不吞字节；
//   - 已存在结构化 tool_calls 或 finish_reason=length 的响应不参与还原（非流式）。
package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// dsmlPipe 是分隔符：两个全角竖线 U+FF5C。
const dsmlPipe = "\uFF5C\uFF5C"

const (
	dsmlOpen        = "<" + dsmlPipe + "DSML" + dsmlPipe + " calls>"
	dsmlClose       = "</" + dsmlPipe + "DSML" + dsmlPipe + " calls>"
	dsmlInvokeClose = "</" + dsmlPipe + "DSML" + dsmlPipe + " invoke>"
	dsmlParamClose  = "</" + dsmlPipe + "DSML" + dsmlPipe + " parameter>"
)

var (
	dsmlTagPrefix = regexp.QuoteMeta("<" + dsmlPipe + "DSML" + dsmlPipe)
	// dsmlInvokeRe 匹配 invoke 头：<｜｜DSML｜｜ invoke name="NAME">。
	dsmlInvokeRe = regexp.MustCompile(dsmlTagPrefix + `\s+invoke\s+name="([^"]*)"\s*>`)
	// dsmlParamRe 匹配 parameter 头，string 属性可选（缺省按 JSON 解析取值）。
	dsmlParamRe = regexp.MustCompile(dsmlTagPrefix + `\s+parameter\s+name="([^"]*)"(?:\s+string="(true|false)")?\s*>`)
	// 工具名合理形态：名单缺失时才用它兜底，挡住「讨论这段语法」时写出的占位符。
	dsmlToolNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:-]{0,63}$`)
)

// dsmlMaxBlockBytes 单块累积上限。超长未闭合说明不是工具调用（例如正文在讲解
// 这段语法），立即原文回吐并退出块态，避免无限缓冲。
const dsmlMaxBlockBytes = 256 * 1024

// dsmlCall 是从正文还原出的一次工具调用。
type dsmlCall struct {
	Name      string
	Arguments string // JSON 对象文本
}

// dsmlRepair 是流式/非流式共用的还原状态机。非并发安全：一条响应流一个实例。
type dsmlRepair struct {
	allowed      map[string]bool
	allowUnknown bool // 名单为空时启用弱判定（形态检查）；默认 false
	hold         string
	inBlock      bool
	seen         int
	rejected     int
	converted    int
}

func newDSMLRepair(allowed map[string]bool, allowUnknown bool) *dsmlRepair {
	return &dsmlRepair{allowed: allowed, allowUnknown: allowUnknown}
}

func (r *dsmlRepair) enabled() bool {
	return r != nil && (len(r.allowed) > 0 || r.allowUnknown)
}

func (r *dsmlRepair) seenBlocks() int    { if r == nil { return 0 }; return r.seen }
func (r *dsmlRepair) rejectedBlocks() int { if r == nil { return 0 }; return r.rejected }
func (r *dsmlRepair) convertedCalls() int { if r == nil { return 0 }; return r.converted }

// feed 吃进一段正文增量，返回「应作为正文透出的部分」与「还原出的工具调用」。
// 不变量：所有 feed 入参拼接 == 所有返回正文 + flush() + 被转换块的原文。
func (r *dsmlRepair) feed(text string) (string, []dsmlCall) {
	if !r.enabled() {
		return text, nil
	}
	s := r.hold + text
	r.hold = ""
	var emit strings.Builder
	var calls []dsmlCall
	for {
		if !r.inBlock {
			i := dsmlStartCandidate(s)
			if i < 0 {
				emit.WriteString(s)
				break
			}
			if len(s)-i < len(dsmlOpen) {
				// 起点被增量边界截断：留到下一片再判定。
				emit.WriteString(s[:i])
				r.hold = s[i:]
				break
			}
			emit.WriteString(s[:i])
			s = s[i+len(dsmlOpen):]
			r.inBlock = true
			continue
		}
		k := strings.Index(s, dsmlClose)
		if k < 0 {
			if len(s) > dsmlMaxBlockBytes {
				// 超长未闭合：不是标记，原文回吐并退出块态。
				emit.WriteString(dsmlOpen)
				emit.WriteString(s)
				r.inBlock = false
				break
			}
			r.hold = s
			break
		}
		inner := s[:k]
		s = s[k+len(dsmlClose):]
		r.inBlock = false
		r.seen++
		if got, ok := parseDSMLBlock(inner, r.allowed, r.allowUnknown); ok {
			calls = append(calls, got...)
			r.converted += len(got)
			continue
		}
		// 块内不是纯调用：原文回吐，绝不吞字节。
		r.rejected++
		emit.WriteString(dsmlOpen)
		emit.WriteString(inner)
		emit.WriteString(dsmlClose)
	}
	return emit.String(), calls
}

// flush 在流结束时把未判定的尾部原文交还（未闭合块连同起始标记一起回吐）。
func (r *dsmlRepair) flush() string {
	if !r.enabled() {
		return ""
	}
	out := r.hold
	if r.inBlock {
		out = dsmlOpen + r.hold
	}
	r.hold = ""
	r.inBlock = false
	return out
}

// dsmlStartCandidate 返回 s 中最早可能是 dsmlOpen 起点的下标（含被增量截断的前缀）。
func dsmlStartCandidate(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		rest := s[i:]
		if len(rest) >= len(dsmlOpen) {
			if rest[:len(dsmlOpen)] == dsmlOpen {
				return i
			}
			continue
		}
		if strings.HasPrefix(dsmlOpen, rest) {
			return i
		}
	}
	return -1
}

// parseDSMLBlock 解析 calls 块内部。成功要求：块内除 invoke 与空白外无别的字节；
// 每个 invoke 闭合且参数合法；工具名判定通过（名单非空走严格比对，名单为空走形态检查）。
func parseDSMLBlock(inner string, allowed map[string]bool, allowUnknown bool) ([]dsmlCall, bool) {
	strict := len(allowed) > 0
	var out []dsmlCall
	pos := 0
	for {
		loc := dsmlInvokeRe.FindStringSubmatchIndex(inner[pos:])
		if loc == nil {
			break
		}
		if strings.TrimSpace(inner[pos:pos+loc[0]]) != "" {
			return nil, false // invoke 之前夹带正文
		}
		name := inner[pos+loc[2] : pos+loc[3]]
		bodyStart := pos + loc[1]
		end := strings.Index(inner[bodyStart:], dsmlInvokeClose)
		if end < 0 {
			return nil, false // invoke 未闭合
		}
		body := inner[bodyStart : bodyStart+end]
		pos = bodyStart + end + len(dsmlInvokeClose)
		args, ok := parseDSMLParams(body)
		if !ok {
			return nil, false
		}
		if strict {
			if !allowed[name] {
				return nil, false // 工具名不在声明的 tools 里
			}
		} else if !allowUnknown || !dsmlToolNameRe.MatchString(name) {
			return nil, false
		}
		out = append(out, dsmlCall{Name: name, Arguments: args})
	}
	if len(out) == 0 {
		return nil, false
	}
	if strings.TrimSpace(inner[pos:]) != "" {
		return nil, false // 最后一个 invoke 之后夹带正文
	}
	return out, true
}

// parseDSMLParams 解析一个 invoke 内部的 parameter 列表，产出 JSON 对象文本。
// string="true" 的取值按原文字符串处理（走 JSON 转义，命令里的换行/引号/反斜杠原样保留）；
// 其余先按 JSON 解析，失败退化为字符串。无参数产出 "{}"。
func parseDSMLParams(body string) (string, bool) {
	args := map[string]any{}
	pos := 0
	for {
		loc := dsmlParamRe.FindStringSubmatchIndex(body[pos:])
		if loc == nil {
			break
		}
		if strings.TrimSpace(body[pos:pos+loc[0]]) != "" {
			return "", false
		}
		name := body[pos+loc[2] : pos+loc[3]]
		isString := loc[4] >= 0 && body[pos+loc[4]:pos+loc[5]] == "true"
		valStart := pos + loc[1]
		end := strings.Index(body[valStart:], dsmlParamClose)
		if end < 0 {
			return "", false
		}
		raw := body[valStart : valStart+end]
		pos = valStart + end + len(dsmlParamClose)
		if isString {
			args[name] = raw
			continue
		}
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(raw)), &v) == nil {
			args[name] = v
		} else {
			args[name] = raw
		}
	}
	if strings.TrimSpace(body[pos:]) != "" {
		return "", false
	}
	out, err := json.Marshal(args)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// dsmlHasMarkup 快速判断文本中是否出现 DSML 分隔符（热路径零开销筛选）。
func dsmlHasMarkup(s string) bool {
	return strings.Contains(s, dsmlPipe)
}

// dsmlToolNameAllowlist 从 OpenAI 形态 tools 里收集工具名集合。
func dsmlToolNameAllowlist(v any) map[string]bool {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	names := map[string]bool{}
	for _, t := range arr {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if fn, ok := tm["function"].(map[string]any); ok {
			if n, ok := fn["name"].(string); ok && n != "" {
				names[n] = true
				continue
			}
		}
		if n, ok := tm["name"].(string); ok && n != "" {
			names[n] = true
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// dsmlMessageToolCalls 把还原出的调用转成非流式 message.tool_calls（无 index）。
func dsmlMessageToolCalls(calls []dsmlCall) []any {
	out := make([]any, 0, len(calls))
	for i, call := range calls {
		out = append(out, map[string]any{
			"id":   dsmlCallID(i),
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	return out
}

// dsmlToolCalls 把还原出的调用转成流式 delta.tool_calls。index 用 float64：
// 上游帧经 json.Unmarshal 进来数值一律 float64，下游按 float64 读。
func dsmlToolCalls(calls []dsmlCall, base int) []any {
	out := make([]any, 0, len(calls))
	for i, call := range calls {
		out = append(out, map[string]any{
			"index": float64(base + i),
			"id":    dsmlCallID(base + i),
			"type":  "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	return out
}

// dsmlCallID 生成形态接近 OpenAI 的 tool_call id，序号保证同一响应内互不相同。
func dsmlCallID(idx int) string {
	return fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), idx)
}

// guardBadToolCalls 在响应回放前检查两类会导致客户端 schema 校验失败的坏工具调用：
//  1. DSML 块识别到但无法安全还原（破损/未闭合/工具名不在清单），或还原出的参数为空；
//  2. 结构化 tool_call 的参数是空对象，而该工具 schema 声明了 required 字段
//     （典型：Claude Code 的 Bash.command / Write.file_path —— new-api 转出
//     input={}，Claude Code 校验失败 → InputValidationError，任务终止）。
//
// 返回非空字符串表示应改判 502，内容为对外可见的错误原因。
//
// 注意：本函数只做「识别」，绝不修复——修复由 textCallGW 在回放时完成。两者职责
// 分离：坏到无法修复的在这里拦下（写 200 之前），可修复的放行给出口转换。
func guardBadToolCalls(lines []string, params map[string]any) string {
	if len(lines) == 0 {
		return ""
	}
	// 热路径快速跳过：逐行扫描，无 DSML 分隔符也无工具调用的响应连 Join 都不做。
	signal := false
	for _, ln := range lines {
		if strings.Contains(ln, dsmlPipe) || strings.Contains(ln, `"tool_calls"`) {
			signal = true
			break
		}
	}
	if !signal {
		return ""
	}
	body := strings.Join(lines, "")
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	chat, err := collectStreamResponse(resp)
	if err != nil || chat == nil {
		return "" // 解析失败不误判，交回原有回放逻辑
	}
	return guardBadToolCallsChat(chat, params)
}

// guardBadToolCallsChat 对聚合后的 chat 对象执行坏工具调用判定，与流式路径共用同一套判据。
// 非流式出口在写 200 之前直接调用本函数；流式路径由 guardBadToolCalls 聚合 SSE 后转调。
func guardBadToolCallsChat(chat map[string]any, params map[string]any) string {
	msg, _ := getNested(chat, "choices", 0, "message").(map[string]any)
	if msg == nil {
		return ""
	}
	tcs, _ := msg["tool_calls"].([]any)

	// 1) DSML 破损 / 空参数（只在没有结构化调用时才判，避免误伤正文里讨论语法）
	text, _ := msg["content"].(string)
	if len(tcs) == 0 && dsmlHasMarkup(text) {
		allowed := dsmlToolNameAllowlist(params["tools"])
		r := newDSMLRepair(allowed, false)
		_, calls := r.feed(text)
		_ = r.flush()
		if r.seenBlocks() > 0 && r.convertedCalls() == 0 {
			return "上游 DSML 工具调用标记无法安全还原（块结构破损或工具名未声明）"
		}
		for _, c := range calls {
			if c.Arguments == "" || c.Arguments == "{}" {
				return "上游 DSML 工具调用参数为空: " + c.Name
			}
		}
	}

	// 2) 结构化空 tool_call（arguments 空对象，但 schema 声明了必需字段）
	required := toolRequiredFields(params["tools"])
	if len(required) == 0 {
		return ""
	}
	for _, tcRaw := range tcs {
		tc, ok := tcRaw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tc["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		req, has := required[name]
		if !has || len(req) == 0 {
			continue
		}
		args, _ := fn["arguments"].(string)
		a := strings.TrimSpace(args)
		if a == "" || a == "{}" {
			return fmt.Sprintf("上游工具 %s 的调用参数为空（缺少必需字段 %s）", name, strings.Join(req, ","))
		}
	}
	return ""
}

// toolRequiredFields 从 OpenAI 形态 tools 提取各工具的 required 字段名。
func toolRequiredFields(v any) map[string][]string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := map[string][]string{}
	for _, t := range arr {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tm["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		if name == "" {
			continue
		}
		ps, _ := fn["parameters"].(map[string]any)
		reqRaw, _ := ps["required"].([]any)
		var req []string
		for _, r := range reqRaw {
			if s, ok := r.(string); ok && s != "" {
				req = append(req, s)
			}
		}
		if len(req) > 0 {
			out[name] = req
		}
	}
	return out
}

// dsmlRepairAggregated 在非流式聚合响应里把正文中的标记还原为 tool_calls。
// 只在「该 choice 没有结构化 tool_calls」且「finish_reason 为 stop」时生效。
// 返回使用的修复器，调用方据 seen/rejected/converted 记观测。
func dsmlRepairAggregated(chat map[string]any, allowed map[string]bool, allowUnknown bool) *dsmlRepair {
	r := newDSMLRepair(allowed, allowUnknown)
	if !r.enabled() {
		return r
	}
	choices, ok := chat["choices"].([]any)
	if !ok {
		return r
	}
	for _, ci := range choices {
		c, ok := ci.(map[string]any)
		if !ok {
			continue
		}
		msg, ok := c["message"].(map[string]any)
		if !ok {
			continue
		}
		if tcs, ok := msg["tool_calls"].([]any); ok && len(tcs) > 0 {
			continue
		}
		if fr, _ := c["finish_reason"].(string); fr != "stop" {
			continue
		}
		text, _ := msg["content"].(string)
		if text == "" || !dsmlHasMarkup(text) {
			continue
		}
		emit, calls := r.feed(text)
		emit += r.flush()
		if len(calls) == 0 {
			continue
		}
		msg["content"] = emit
		msg["tool_calls"] = dsmlMessageToolCalls(calls)
		c["finish_reason"] = "tool_calls"
	}
	return r
}

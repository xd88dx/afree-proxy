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
// 同步纪律（2026-11-27 复审 F1）：这是 vendor 解析器的**移植副本**，vendor 侧
// 修复必须同步回本文件。本次已回移 142790b3 的单竖线漂移双形态兼容（loose 族
// 常量、dsmlStartCandidate 双族、正则前缀放宽、inBlockOpen 同族回吐、闭合同
// 族取最早、dsmlHasMarkup 单竖线放行）。
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

	"afree-proxy/internal/toolrename"
)

// dsmlPipe 是分隔符：两个全角竖线 U+FF5C。
const dsmlPipe = "\uFF5C\uFF5C"

// dsmlPipeLoose 单全角竖线：2026-11-27 生产实证的漂移形态（vendor
// modules/workbuddy2api/internal/upstream/dsml.go 142790b3 同源）——模型端
// 序列化从 <｜｜DSML｜｜ X> 漂移为 <｜DSML｜ X>，两形态并存兼容，不替换旧形态。
// 本文件是 vendor dsml.go 的移植副本：vendor 侧解析器再变更时本文件必须跟进。
const dsmlPipeLoose = "\uFF5C"

const (
	dsmlOpen        = "<" + dsmlPipe + "DSML" + dsmlPipe + " calls>"
	dsmlClose       = "</" + dsmlPipe + "DSML" + dsmlPipe + " calls>"
	dsmlInvokeClose = "</" + dsmlPipe + "DSML" + dsmlPipe + " invoke>"
	dsmlParamClose  = "</" + dsmlPipe + "DSML" + dsmlPipe + " parameter>"

	dsmlOpenLoose        = "<" + dsmlPipeLoose + "DSML" + dsmlPipeLoose + " calls>"
	dsmlCloseLoose       = "</" + dsmlPipeLoose + "DSML" + dsmlPipeLoose + " calls>"
	dsmlInvokeCloseLoose = "</" + dsmlPipeLoose + "DSML" + dsmlPipeLoose + " invoke>"
	dsmlParamCloseLoose  = "</" + dsmlPipeLoose + "DSML" + dsmlPipeLoose + " parameter>"
)

// dsmlCloseFor 按块开头形态取同族的块尾——块的收尾与开头同一序列化族
// （模型一次生成一个族），配对不跨族。
func dsmlCloseFor(open string) string {
	if open == dsmlOpenLoose {
		return dsmlCloseLoose
	}
	return dsmlClose
}

var (
	// dsmlTagPrefixRe 两族开始标记公共前缀（双竖线 | 单竖线），后接空白。
	dsmlTagPrefixRe = `(?:` + regexp.QuoteMeta("<"+dsmlPipe+"DSML"+dsmlPipe) +
		`|` + regexp.QuoteMeta("<"+dsmlPipeLoose+"DSML"+dsmlPipeLoose) + `)\s+`
	// dsmlInvokeRe 匹配 invoke 头：<｜｜DSML｜｜ invoke name="NAME">（含单竖线漂移形态）。
	dsmlInvokeRe = regexp.MustCompile(dsmlTagPrefixRe + `invoke\s+name="([^"]*)"\s*>`)
	// dsmlParamRe 匹配 parameter 头，string 属性可选（缺省按 JSON 解析取值）。
	dsmlParamRe = regexp.MustCompile(dsmlTagPrefixRe + `parameter\s+name="([^"]*)"(?:\s+string="(true|false)")?\s*>`)
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
	inBlockOpen  string // 当前块的开头形态（dsmlOpen / dsmlOpenLoose）——拒绝回吐/超长回吐/flush 必须用原形态，否则污染正文
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

func (r *dsmlRepair) seenBlocks() int {
	if r == nil {
		return 0
	}
	return r.seen
}
func (r *dsmlRepair) rejectedBlocks() int {
	if r == nil {
		return 0
	}
	return r.rejected
}
func (r *dsmlRepair) convertedCalls() int {
	if r == nil {
		return 0
	}
	return r.converted
}

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
			i, open := dsmlStartCandidate(s)
			if i < 0 {
				emit.WriteString(s)
				break
			}
			if open == "" {
				// 起点被增量边界截断：留到下一片再判定。
				emit.WriteString(s[:i])
				r.hold = s[i:]
				break
			}
			emit.WriteString(s[:i])
			s = s[i+len(open):]
			r.inBlock = true
			r.inBlockOpen = open
			continue
		}
		k := strings.Index(s, dsmlCloseFor(r.inBlockOpen))
		if k < 0 {
			if len(s) > dsmlMaxBlockBytes {
				// 超长未闭合：不是标记，原文回吐并退出块态。
				emit.WriteString(r.inBlockOpen)
				emit.WriteString(s)
				r.inBlock = false
				break
			}
			r.hold = s
			break
		}
		inner := s[:k]
		s = s[k+len(dsmlCloseFor(r.inBlockOpen)):]
		r.inBlock = false
		r.seen++
		if got, ok := parseDSMLBlock(inner, r.allowed, r.allowUnknown); ok {
			calls = append(calls, got...)
			r.converted += len(got)
			continue
		}
		// 块内不是纯调用：原文回吐，绝不吞字节。
		r.rejected++
		emit.WriteString(r.inBlockOpen)
		emit.WriteString(inner)
		emit.WriteString(dsmlCloseFor(r.inBlockOpen))
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
		out = r.inBlockOpen + r.hold
	}
	r.hold = ""
	r.inBlock = false
	return out
}

// dsmlStartCandidate 返回 s 中最早「可能是任一族 dsmlOpen 起点」的下标与命中的
// open：该位置之后要么正好是完整的某个 open，要么是某个 open 的前缀（被增量边界
// 截断——此时 open 返回空串，调用方只 hold 不消耗）。没有候选返回 -1。
func dsmlStartCandidate(s string) (int, string) {
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		rest := s[i:]
		for _, open := range []string{dsmlOpen, dsmlOpenLoose} {
			if len(rest) >= len(open) {
				if rest[:len(open)] == open {
					return i, open
				}
				continue
			}
			if strings.HasPrefix(open, rest) {
				// 截断态：rest 是 open 的前缀。两族前缀共享 <｜DSML 时按更长的
				// 族等下一片——不必区分，hold 语义本来就等完整数据。
				return i, ""
			}
		}
	}
	return -1, ""
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
		// invoke 闭合形态与本块开头同族（单/双竖线），两种都找取最早，防模型混排。
		iS := strings.Index(inner[bodyStart:], dsmlInvokeClose)
		iL := strings.Index(inner[bodyStart:], dsmlInvokeCloseLoose)
		end, closeLen := -1, 0
		switch {
		case iS >= 0 && (iL < 0 || iS < iL):
			end, closeLen = iS, len(dsmlInvokeClose)
		case iL >= 0:
			end, closeLen = iL, len(dsmlInvokeCloseLoose)
		default:
			return nil, false // invoke 未闭合
		}
		body := inner[bodyStart : bodyStart+end]
		pos = bodyStart + end + closeLen
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
		// parameter 闭合同 invoke：两族都找取最早，防混排。
		iS := strings.Index(body[valStart:], dsmlParamClose)
		iL := strings.Index(body[valStart:], dsmlParamCloseLoose)
		end, closeLen := -1, 0
		switch {
		case iS >= 0 && (iL < 0 || iS < iL):
			end, closeLen = iS, len(dsmlParamClose)
		case iL >= 0:
			end, closeLen = iL, len(dsmlParamCloseLoose)
		default:
			return "", false
		}
		raw := body[valStart : valStart+end]
		pos = valStart + end + closeLen
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
// 含单个全角竖线即放行（2026-11-27 单竖线漂移形态，见 dsmlPipeLoose）——解析器
// 全程保守，误入成本只是一次无命中的状态机扫描；双竖线子串判据会让漂移形态
// 在门口直接漏过。
func dsmlHasMarkup(s string) bool {
	return strings.Contains(s, dsmlPipeLoose)
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
		// Anthropic 形态 tools（new-api 半转换时）：顶层 name + input_schema。
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
func guardBadToolCalls(lines []string, params map[string]any, ren *toolrename.Mapping) string {
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
	return guardBadToolCallsChat(chat, params, ren)
}

// guardBadToolCallsChat 对聚合后的 chat 对象执行坏工具调用判定，与流式路径共用同一套判据。
// 非流式出口在写 200 之前直接调用本函数；流式路径由 guardBadToolCalls 聚合 SSE 后转调。
// ren 为出站工具改名映射（zen 走域包出站 body 改写、params 保持真名，响应里是
// 假名——判据须同时认假名才能命中 schema 表且不把 DSML 好调用误判 502，
// 2026-11-27 审计 G2；其余平台 params 已就地改写，传 nil）。
func guardBadToolCallsChat(chat map[string]any, params map[string]any, ren *toolrename.Mapping) string {
	msg, _ := getNested(chat, "choices", 0, "message").(map[string]any)
	if msg == nil {
		return ""
	}
	tcs, _ := msg["tool_calls"].([]any)

	// 1) DSML 破损 / 空参数（只在没有结构化调用时才判，避免误伤正文里讨论语法）
	text, _ := msg["content"].(string)
	if len(tcs) == 0 && dsmlHasMarkup(text) {
		allowed := dsmlToolNameAllowlist(params["tools"])
		if ren != nil {
			// 响应正文里的 DSML 块用假名：名单同时收录改写形态，否则好调用
			// 被 strict 判败 → seen>0 && converted==0 → 误判 502。
			for real := range allowed {
				allowed[ren.Rename(real)] = true
			}
		}
		r := newDSMLRepair(allowed, false)
		_, calls := r.feed(text)
		_ = r.flush()
		if r.seenBlocks() > 0 && r.convertedCalls() == 0 {
			return "上游 DSML 工具调用标记无法安全还原（块结构破损或工具名未声明）"
		}
		for _, c := range calls {
			if c.Arguments == "" || c.Arguments == "{}" {
				return "上游 DSML 工具调用参数为空: " + ren.Restore(c.Name)
			}
		}
	}

	// 2) 结构化坏 tool_call：参数空对象 / 缺必需字段 / 必需字段为 null /
	//    参数不是合法 JSON（后两者经 new-api 转换会回退成 input={}）。
	//    判据用 toolSchemas：required 缺失但声明了 properties 的工具，空 {}
	//    同样判坏——2026-11-25 生产复现证明 new-api 转换会丢 required。
	schemas := toolSchemas(params["tools"])
	if len(schemas) == 0 {
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
		// zen 出站改名为 body 级（params 保持真名）：响应 tool_calls 是假名，
		// 先还原回真名再查 schema 表，否则闸被假名整体致盲（2026-11-27 审计 G2）。
		key := name
		if _, ok := schemas[key]; !ok && ren != nil {
			key = ren.Restore(name)
		}
		sch, has := schemas[key]
		if !has {
			continue
		}
		args, _ := fn["arguments"].(string)
		a := strings.TrimSpace(args)
		// 空参数：required 在身一律判坏（2026-11-25 生产复现：new-api 转换丢
		// required 后空 {} 直达客户端）。props-only（无 required 有 properties）
		// 的 ""/{}/零参调用是合法形态——只拦显式 null，与提交口径一致
		//（2026-11-27 审计 S1：原实现连 {} 一并拦属过度误伤）。
		if a == "" || a == "{}" || a == "null" {
			if len(sch.required) > 0 {
				return fmt.Sprintf("上游工具 %s 的调用参数为空（缺少必需字段 %s）", key, strings.Join(sch.required, ","))
			}
			if a == "null" {
				return fmt.Sprintf("上游工具 %s 的调用参数为 null（声明参数 %s）", key, strings.Join(sch.properties, ","))
			}
			continue
		}
		// 非空必须是合法 JSON 对象；非法参数交给 new-api 会被回退成 {}。
		var obj map[string]any
		if json.Unmarshal([]byte(a), &obj) != nil {
			return fmt.Sprintf("上游工具 %s 的调用参数不是合法 JSON", key)
		}
		// required 字段：缺失或值为 null 都让客户端 schema 校验失败。
		// props-only（无 required）的字段是可选的，只拦显式 null——
		// 可选字段未提供是合法调用，不能误判。
		var missing []string
		for _, field := range sch.required {
			if v, exists := obj[field]; !exists || v == nil {
				missing = append(missing, field)
			}
		}
		if len(missing) == 0 && len(sch.required) == 0 {
			for _, field := range sch.properties {
				if v, exists := obj[field]; exists && v == nil {
					missing = append(missing, field)
				}
			}
		}
		if len(missing) > 0 {
			return fmt.Sprintf("上游工具 %s 的调用参数缺少必需字段 %s", key, strings.Join(missing, ","))
		}
	}
	return ""
}

// toolSchema 是一个工具声明的字段约束：required 为必需字段名，properties 为
// 声明的全部参数名。required 缺失（部分上游转换会丢 required）时空参数一律
// 判坏；props-only 的空 {} 是合法零参调用，只拦显式 null（2026-11-27 S1）。
type toolSchema struct {
	required   []string
	properties []string
}

// toolSchemas 从 OpenAI / Anthropic 两种形态的 tools 提取字段约束。
func toolSchemas(v any) map[string]toolSchema {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := map[string]toolSchema{}
	for _, t := range arr {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		// OpenAI 形态：{type:function, function:{name, parameters}}；
		// Anthropic 形态（new-api 未转换或半转换时）：{name, input_schema}。
		// 两种都要认出，否则空参数调用漏判回 200（2026-11-25）。
		var name string
		var ps map[string]any
		if fn, ok := tm["function"].(map[string]any); ok {
			name, _ = fn["name"].(string)
			ps, _ = fn["parameters"].(map[string]any)
		} else {
			name, _ = tm["name"].(string)
			ps, _ = tm["input_schema"].(map[string]any)
		}
		if name == "" || ps == nil {
			continue
		}
		var req []string
		if reqRaw, ok := ps["required"].([]any); ok {
			for _, r := range reqRaw {
				if s, ok := r.(string); ok && s != "" {
					req = append(req, s)
				}
			}
		}
		var props []string
		if pmap, ok := ps["properties"].(map[string]any); ok {
			for k := range pmap {
				props = append(props, k)
			}
		}
		if len(req) > 0 || len(props) > 0 {
			out[name] = toolSchema{required: req, properties: props}
		}
	}
	return out
}

// toolRequiredFields 从 OpenAI 形态 tools 提取各工具的 required 字段名。
func toolRequiredFields(v any) map[string][]string {
	schemas := toolSchemas(v)
	out := make(map[string][]string, len(schemas))
	for name, s := range schemas {
		if len(s.required) > 0 {
			out[name] = s.required
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

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"afree-proxy/internal/trae2api/traectl"
)

// 本文件实现「文本形式工具调用的兜底解析」。
//
// ## 为什么需要
//
// 上游 Trae 协议没有工具类消息（消息只有 system/user/assistant + text 片段，
// 见 traectl.traePart），多轮工具对话中网关只能把 assistant 的历史 tool_calls
// 编码成文本 `[调用工具 name(args)]`（见 toTraeRequest）。实测这带来一个严重
// 副作用：**从第二轮起，模型会模仿这个文本格式在正文里"输出工具调用"**，而
// 不是走原生 function_call 通道 —— glm-5.3-flash / Doubao-Seed-Code /
// deepseek-v4.1-flash 三个模型全部复现，且在 system prompt 里明令禁止也拦不住。
// 客户端收到的只是普通文本，工具不会被执行，agent 的回合就此终止
// （表象是"一旦调工具任务就直接结束"）。
//
// ## 思路
//
// 编码格式是网关自己选的，模型模仿的正是它 —— 那就把它解析回来：检测正文中的
// `[调用工具 name(json)]`，命中时把它从正文剥离、转换成结构化 tool_calls
// （finish_reason 一并改写为 tool_calls）。这样即使模型继续模仿，客户端拿到的
// 仍是可执行的原生调用，编码格式从"陷阱"变成"往返一致"的传输层。
//
// ## 误伤防护
//
// 正文可能出现讲解式的示例文本（例如用户问"网关怎么编码工具调用"）。因此只有
// 同时满足以下条件才转换，否则一律按普通正文原样下发（与旧行为一致，不会更糟）：
//   - 本次请求声明了 tools（没有工具定义就不可能构成真实调用，整体直通）；
//   - 解析出的工具名在 tools 清单里；
//   - 参数是合法 JSON（空参数合法，与 isTruncatedArguments 的截断判定对齐）。
//
// 解析覆盖 OpenAI 入口的流式与非流式两条路径；Anthropic 入口暂不接入。

// patOpen 是文本工具调用的起始标记 —— 必须与 toTraeRequest 的历史编码严格一致。
const patOpen = "[调用工具"

// extractedCall 是从正文里解析出的一个工具调用。
type extractedCall struct {
	name string
	args string
	// repaired 表示 args 原文含非法 JSON 转义、经修复后才通过校验。
	// 模型模仿历史编码格式时会手写参数 JSON，命令里的 grep BRE 转义（如
	// `\|`）直接照抄进字符串就是非法转义 —— 不修复的话这类调用会被当成
	// 正文放行，客户端看到的又是"字符串工具调用"（2026-11-04 实测踩中）。
	repaired bool
}

// acceptCallArgs 校验并按需修复工具参数。
//
// 返回 (修复后的参数, 是否修复过, 是否可用)。原文合法时原样返回；
// 近似 JSON 经 repairJSONLenient 修复后重试；修完仍不合法才判不可用。
func acceptCallArgs(raw string) (fixed string, repaired bool, ok bool) {
	var v any
	if json.Unmarshal([]byte(raw), &v) == nil {
		return raw, false, true
	}
	fixed = repairJSONLenient(raw)
	if fixed == raw {
		return "", false, false
	}
	if json.Unmarshal([]byte(fixed), &v) != nil {
		return "", false, false
	}
	return fixed, true, true
}

// repairJSONLenient 尽力把"模型手写的近似 JSON"修补成合法 JSON。
//
// 三类修复（只作用于字符串字面量内部，外部结构原样保留）：
//   - 非法反斜杠转义翻倍（`\|` → `\\|`，grep BRE 等场景，2026-11-04 实测）
//   - 字符串内的裸双引号（ssh 命令嵌套引号，2026-11-04 实测）：仅当下一个
//     非空白字节是 `,` `}` `]` `:` 之一（或到达结尾）才视为字符串收尾，
//     否则补 `\` 转义
//   - 裸控制字符（换行/制表等）转义
//
// 这是启发式修复：修完仍不合法就整体放弃（调用方回落为正文放行），
// 最坏情况与不修复一致，不会更糟。
func repairJSONLenient(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 16)
	inStr := false
	for i := 0; i < len(s); {
		c := s[i]
		if !inStr {
			if c == '"' {
				inStr = true
			}
			sb.WriteByte(c)
			i++
			continue
		}
		switch {
		case c == '\\':
			if i+1 >= len(s) { // 结尾悬挂反斜杠
				sb.WriteString(`\\`)
				i++
				continue
			}
			n := s[i+1]
			switch n {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				sb.WriteByte(c)
				sb.WriteByte(n)
				i += 2
			case 'u':
				if i+6 <= len(s) && isHex4(s[i+2:i+6]) {
					sb.WriteString(s[i : i+6])
					i += 6
				} else {
					sb.WriteString(`\\`)
					i++
				}
			default:
				sb.WriteString(`\\`)
				i++
			}
		case c == '"':
			// 终止符判定：后随非空白 ∈ {,} ] : （或结尾）才收字符串，
			// 否则视为命令里未转义的内层引号，补转义
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j >= len(s) || s[j] == ',' || s[j] == '}' || s[j] == ']' || s[j] == ':' {
				inStr = false
				sb.WriteByte(c)
			} else {
				sb.WriteString(`\"`)
			}
			i++
		case c == '\n':
			sb.WriteString(`\n`)
			i++
		case c == '\r':
			sb.WriteString(`\r`)
			i++
		case c == '\t':
			sb.WriteString(`\t`)
			i++
		case c < 0x20:
			sb.WriteString(fmt.Sprintf(`\u%04x`, c))
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String()
}

func isHex4(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// ---- 增量解析（流式路径用） ----

// textCallFilter 在流式过程中增量检测文本工具调用。
//
// 正文增量先进入 pending 缓冲：可能是模式前缀的尾巴会被暂扣（最多扣
// len(patOpen)-1 字节，正常对话几乎无感）；确认是调用后整段剥离不再下发，
// 转而记入 calls；确认不是调用（或截断）的文本在流结束时原样冲刷 ——
// 内容不丢，只是晚一点到达。
type textCallFilter struct {
	known    map[string]bool
	canon    map[string]string // lower(name) -> 请求声明的规范名
	out      func(string)
	pending  []byte
	capture  bool // pending 是否正处于"从 '[' 开始的调用捕获"状态
	calls    []extractedCall
	rejected []string // 诊断：疑似调用但未转换的原因（含片段）
	disabled bool
}

// newTextCallFilter 构造解析器。tools 为空时整体直通（零开销）。
// out 是"可安全下发的正文"出口，不得为 nil。
func newTextCallFilter(tools []traectl.Tool, out func(string)) *textCallFilter {
	known := make(map[string]bool, len(tools))
	canon := make(map[string]string, len(tools))
	for _, t := range tools {
		known[t.Name] = true
		canon[strings.ToLower(t.Name)] = t.Name
	}
	return &textCallFilter{known: known, canon: canon, out: out, disabled: len(known) == 0}
}

// lookup 解析工具名：精确命中优先，其次大小写不敏感地映射到请求声明的规范名
// （模型手写模仿时大小写可能走样；客户端执行按声明名精确匹配，必须归一）。
func (f *textCallFilter) lookup(name string) (string, bool) {
	if f.known[name] {
		return name, true
	}
	if c, ok := f.canon[strings.ToLower(name)]; ok {
		return c, true
	}
	return "", false
}

// reject 记录一条"疑似调用但未转换"的诊断（用于事后从日志定位漏转换原因）。
func (f *textCallFilter) reject(reason, name, args string) {
	f.rejected = append(f.rejected, fmt.Sprintf("%s name=%q args=%.120q", reason, name, args))
}

// takeRejections 取走诊断记录。
func (f *textCallFilter) takeRejections() []string {
	r := f.rejected
	f.rejected = nil
	return r
}

// feed 处理一个正文增量。
func (f *textCallFilter) feed(delta string) {
	if f.disabled {
		f.emit(delta)
		return
	}
	f.pending = append(f.pending, delta...)
	for {
		if !f.capture {
			i := bytes.Index(f.pending, []byte(patOpen))
			if i < 0 {
				// 没有完整标记：只扣住可能是前缀的尾巴，其余立刻下发
				hold := prefixHold(f.pending)
				f.emitB(f.pending[:len(f.pending)-hold])
				f.pending = f.pending[len(f.pending)-hold:]
				return
			}
			f.emitB(f.pending[:i])
			f.pending = f.pending[i:]
			f.capture = true
		}
		raw, name, args, consumed, st := parseTextCall(f.pending)
		switch st {
		case tcIncomplete:
			return // 数据还没到齐，等下一个增量
		case tcMalformed:
			// 不是调用：按普通正文原样下发（内容无损，只是晚了一点）。
			// 必须留痕：这类"模型手写走样"的调用若静默放行，客户端就表现为
			// "只显示字符串不执行"，而无从定位（2026-11-04 实测教训）。
			f.reject("结构畸形（名字/收尾不符）", "", string(f.pending))
			f.emitB(f.pending)
			f.pending = nil
			f.capture = false
			return
		default: // tcComplete
			f.pending = f.pending[consumed:]
			f.capture = false
			if canonical, ok := f.lookup(name); ok {
				if fixed, rep, rok := acceptCallArgs(args); rok {
					f.calls = append(f.calls, extractedCall{name: canonical, args: fixed, repaired: rep})
				} else {
					f.reject("args 不可修复", name, args)
					f.emit(raw)
				}
			} else {
				f.reject("工具名不在清单", name, args)
				f.emit(raw)
			}
		}
	}
}

// finish 在流结束时冲刷残余正文并返回解析出的调用列表。
//
// 未闭合的捕获（流被截断）按正文放行：半截调用原样下发会让客户端解析失败，
// 但静默丢弃违背"内容不丢"原则 —— 与旧行为保持一致，交给客户端容错。
func (f *textCallFilter) finish() []extractedCall {
	if len(f.pending) > 0 {
		if f.capture {
			f.reject("调用未闭合（流截断）", "", string(f.pending))
		}
		f.emitB(f.pending)
		f.pending = nil
	}
	return f.calls
}

func (f *textCallFilter) emit(s string) {
	if s != "" && f.out != nil {
		f.out(s)
	}
}

func (f *textCallFilter) emitB(b []byte) { f.emit(string(b)) }

// prefixHold 返回 pending 尾部可能是 patOpen 前缀的最长字节数。
// 匹配按字节进行：命中即意味着这些字节与模式前缀逐字节相同，天然对齐 UTF-8 边界。
func prefixHold(p []byte) int {
	max := len(patOpen) - 1 // 完整模式走 Index 分支，这里只扣前缀
	if len(p) < max {
		max = len(p)
	}
	for k := max; k > 0; k-- {
		if bytes.Equal(p[len(p)-k:], []byte(patOpen[:k])) {
			return k
		}
	}
	return 0
}

// ---- 一次性解析（非流式路径用） ----

// extractTextToolCalls 对完整正文做一次解析，返回剥离调用后的正文。
// changed 为 true 表示正文发生了改写（调用被剥离）。
func extractTextToolCalls(content string, tools []traectl.Tool) (prose string, calls []extractedCall, changed bool) {
	if len(tools) == 0 || !bytes.Contains([]byte(content), []byte(patOpen)) {
		return content, nil, false
	}
	known := make(map[string]bool, len(tools))
	canon := make(map[string]string, len(tools))
	for _, t := range tools {
		known[t.Name] = true
		canon[strings.ToLower(t.Name)] = t.Name
	}
	var sb strings.Builder
	rest := []byte(content)
	for {
		i := bytes.Index(rest, []byte(patOpen))
		if i < 0 {
			sb.Write(rest)
			break
		}
		sb.Write(rest[:i])
		rest = rest[i:]
		raw, name, args, consumed, st := parseTextCall(rest)
		switch st {
		case tcComplete:
			canonical, knownOK := "", false
			if known[name] {
				canonical, knownOK = name, true
			} else if c, ok := canon[strings.ToLower(name)]; ok {
				canonical, knownOK = c, true
			}
			if knownOK {
				if fixed, rep, ok := acceptCallArgs(args); ok {
					calls = append(calls, extractedCall{name: canonical, args: fixed, repaired: rep})
					changed = true
				} else {
					sb.WriteString(raw)
				}
			} else {
				sb.WriteString(raw)
			}
			rest = rest[consumed:]
		default:
			// incomplete / malformed：这个 '[' 不是标记起点，跳过继续找
			sb.WriteByte(rest[0])
			rest = rest[1:]
		}
	}
	return sb.String(), calls, changed
}

// ---- 语法解析 ----

type tcState int

const (
	tcIncomplete tcState = iota // 数据未到齐，等待更多输入
	tcMalformed                 // 结构确定不是调用
	tcComplete                  // 完整解析出一个调用
)

// parseTextCall 尝试从 buf 开头解析 `[调用工具 name(args)]`。
//
// 返回 raw（原始片段，供校验失败时按正文放行）、name、args（JSON 文本，
// 无参调用为空串）、consumed（消耗的字节数）与解析状态。
//
// args 支持三种形态：空 `()`、JSON 对象 `{...}`、JSON 数组 `[...]`。
// 配平扫描跟踪字符串与转义，因此参数 JSON 里出现括号、引号不会截断解析
// （PowerShell 命令里常带这些字符）。
func parseTextCall(buf []byte) (raw, name, args string, consumed int, st tcState) {
	fail := func(s tcState) (string, string, string, int, tcState) {
		return "", "", "", 0, s
	}
	p := len(patOpen)
	if len(buf) < p || string(buf[:p]) != patOpen {
		return fail(tcMalformed)
	}
	isWS := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	isNameStop := func(c byte) bool { return isWS(c) || c == '(' || c == ')' || c == '[' || c == ']' }

	for p < len(buf) && (buf[p] == ' ' || buf[p] == '\t') {
		p++
	}
	ns := p
	for p < len(buf) && !isNameStop(buf[p]) {
		p++
	}
	if p >= len(buf) {
		return fail(tcIncomplete)
	}
	if p-ns == 0 || p-ns > 128 {
		return fail(tcMalformed) // 空名或超长：几乎不可能是真实调用
	}
	name = string(buf[ns:p])
	for p < len(buf) && (buf[p] == ' ' || buf[p] == '\t') {
		p++
	}
	if p >= len(buf) {
		return fail(tcIncomplete)
	}
	if buf[p] != '(' {
		return fail(tcMalformed)
	}
	p++
	for p < len(buf) && isWS(buf[p]) {
		p++
	}
	if p >= len(buf) {
		return fail(tcIncomplete)
	}

	if buf[p] == ')' { // 无参调用：name()
		p++
		for p < len(buf) && isWS(buf[p]) {
			p++
		}
		if p >= len(buf) {
			return fail(tcIncomplete)
		}
		if buf[p] != ']' {
			return fail(tcMalformed)
		}
		return string(buf[:p+1]), name, "", p + 1, tcComplete
	}

	if buf[p] != '{' && buf[p] != '[' {
		return fail(tcMalformed)
	}
	start := p
	depth, inStr, esc := 0, false, false
	for p < len(buf) {
		c := buf[p]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			p++
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				return fail(tcMalformed)
			}
			if depth == 0 {
				args = string(buf[start : p+1])
				p++
				for p < len(buf) && isWS(buf[p]) {
					p++
				}
				if p >= len(buf) {
					return fail(tcIncomplete)
				}
				// 收尾的 `)` 允许省略：模型手写模仿时常漏写（2026-11-04 实测
				// `[调用工具 Bash({...}]`），`]` 才是格式锚点，仍必须存在。
				if buf[p] == ')' {
					p++
					for p < len(buf) && isWS(buf[p]) {
						p++
					}
					if p >= len(buf) {
						return fail(tcIncomplete)
					}
				}
				if buf[p] != ']' {
					return fail(tcMalformed)
				}
				return string(buf[:p+1]), name, args, p + 1, tcComplete
			}
		}
		p++
	}
	return fail(tcIncomplete) // 配平未完成：等更多数据或流结束
}

// （参数校验由 acceptCallArgs 承担：合法放行、非法转义修复后放行、
// 结构坏到修复不了才拒绝。空参数是合法的"无参工具"。）

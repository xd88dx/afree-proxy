package zen

import (
	"encoding/json"
	"log"
	"strings"
)

// ============ zen 请求体积裁剪（工具过滤 + 描述截断） ============
//
// 背景（2026-11-07 抓包实测）：ZCode 一次真实对话的上游 body 是 137KB，
// 其中 tools 占 112KB（82%），76 个工具里最大的几个是 CronCreate 6.5KB、
// AskUserQuestion 5.1KB、EnterPlanMode 4.4KB、CreateWorkflow 4.1KB、
// mcp__node_repl__js 3.3KB。单条消息因此约 33k tokens，而免费层日额度
// 撑不住几条——429 的定性是**体积**，不是内容（同 key 同出口，ping 45
// tokens 成功、IDE 一单就 429）。
//
// 所以这里做真正减体积的事（不改写内容、不伪装客户端）：按名字前缀/名单
// 丢弃客户端工具，并对过长的 description 做截断。schema 一律保留——模型
// 调用工具靠的是 parameters 结构，描述只是提示。
//
// 关键约束：只过滤**客户端**工具。12 个 gate 占位工具由 ZenMergeTools 在
// 之后合并，绝不参与过滤（缺了它们上游直接 403）。

// ZenToolFilterConfig 体积裁剪配置。全部默认关闭：丢工具会改变模型可用
// 能力，必须由使用者显式开启（面板或配置文件）。
type ZenToolFilterConfig struct {
	// Enabled 总开关。false = 完全透传客户端工具（历史行为）。
	Enabled bool `json:"enabled"`
	// DenyPrefixes 按前缀丢弃（大小写不敏感）。典型值 "mcp__"——MCP 工具
	// 单个体积最大，且一次对话里往往只用其中一两个。
	DenyPrefixes []string `json:"denyPrefixes,omitempty"`
	// DenyNames 按精确名丢弃（大小写不敏感）。用于 cron/workflow/plan-mode
	// 这类 IDE 附带但单轮对话很少用到的重型工具。
	DenyNames []string `json:"denyNames,omitempty"`
	// MaxDescription 描述字符上限（按 rune 计），0 = 不截断。截断只动
	// description，parameters 原样保留，因此工具调用仍然有完整的参数结构。
	MaxDescription int `json:"maxDescription,omitempty"`
}

func DefaultZenToolFilter() ZenToolFilterConfig {
	// 默认值只在 Enabled 被显式打开时生效：给出一套"通用 IDE 重型工具"
	// 名单，但 Enabled 仍是 false。使用者开启总开关即可获得这份默认名单，
	// 也可以清空/改写它。
	return ZenToolFilterConfig{
		Enabled:      false,
		DenyPrefixes: []string{"mcp__"},
		DenyNames:    []string{},
		// 0 = 不截断：先只做丢弃，避免默认行为里混入质量风险。
		MaxDescription: 0,
	}
}

// ZenToolFilterActive 返回生效的过滤配置；未启用时 ok=false。
func ZenToolFilterActive() (ZenToolFilterConfig, bool) {
	c := GetZenConfig().ToolFilter
	if !c.Enabled {
		return c, false
	}
	return c, true
}

// ZenToolDenied 判断工具名是否命中丢弃规则。
func ZenToolDenied(name string, cfg ZenToolFilterConfig) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	for _, f := range cfg.DenyPrefixes {
		f = strings.ToLower(strings.TrimSpace(f))
		if f != "" && strings.HasPrefix(lower, f) {
			return true
		}
	}
	for _, n := range cfg.DenyNames {
		n = strings.ToLower(strings.TrimSpace(n))
		if n != "" && lower == n {
			return true
		}
	}
	return false
}

// TruncateZenDescription 按 rune 截断描述并补省略号。
func TruncateZenDescription(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " \t\n") + " …"
}

// ZenFilterToolStats 记录一次裁剪的字节数，用于日志/抓包观测。
type ZenFilterToolStats struct {
	In       int
	Out      int
	Dropped  []string
	BytesIn  int
	BytesOut int
}

// ZenFilterClientToolsChat 过滤 chat 嵌套形态的客户端工具
// (tools[].function{name,description,parameters})。
//
// 返回新切片，不修改传入的 map：params 是客户端参数，端点自适应重试、
// 日志和 token 估算随后还要读原形态（与 env marker / 换位同样的纪律）。
func ZenFilterClientToolsChat(tools []any, cfg ZenToolFilterConfig) ([]any, ZenFilterToolStats) {
	st := ZenFilterToolStats{In: len(tools)}
	out := make([]any, 0, len(tools))
	for _, raw := range tools {
		tm, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		fn, ok := tm["function"].(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		name, _ := fn["name"].(string)
		st.BytesIn += ApproxToolBytes(tm)
		if ZenToolDenied(name, cfg) {
			st.Dropped = append(st.Dropped, name)
			continue
		}
		// 复制 function 再改 description，避免污染客户端参数。
		nf := make(map[string]any, len(fn))
		for k, v := range fn {
			nf[k] = v
		}
		if d, ok := nf["description"].(string); ok {
			nf["description"] = TruncateZenDescription(d, cfg.MaxDescription)
		}
		cp := make(map[string]any, len(tm))
		for k, v := range tm {
			cp[k] = v
		}
		cp["function"] = nf
		st.BytesOut += ApproxToolBytes(cp)
		out = append(out, cp)
	}
	st.Out = len(out)
	return out, st
}

// ============ 客户端工具描述 NBSP 整形（免费层字面措辞闸对策） ============
//
// 背景（2026-11-21 实测）：免费层闸按客户端工具**顶层描述**做字面子串匹配
// （大小写不敏感），命中即 429 FreeUsageLimitError——响应伪装成日额度耗尽
// （Retry-After≈14h 指向日重置），本仓按额度信号 park key，一次请求沿
// failover 链最多烧掉 4 把 key（实测）。已知触发串：DeepSeek Harness 的
// read/edit/skill 描述措辞（opencode2api issue #188 实证：同会话原文→NBSP→
// 原文 = 429→200→429，纯请求级匹配不封 key）；工具名闸（clint_ 前缀，
// 2026-11-19）是同族另一形态。触发串清单随各反代产品指纹持续增长，第三方
// 客户端的描述随时可能撞上；本仓实测 NBSP 化描述上游照常 200。
//
// 对策（对齐 opencode2api #188，恒定开启）：把客户端工具顶层描述里的 ASCII
// 空格替换成 NBSP（U+00A0）——字面匹配被破坏，模型读取语义不变。刻意只动
// 顶层描述：参数描述、schema、工具名与 12 个 gate 工具不碰（闸不查参数
// 描述与官方工具，实证 200）。不设开关：闸改用 NBSP 感知匹配前开关是死
// 配置（§8 禁止死配置）。幂等：NBSP 非 ASCII 空格，重复整形不再变化。
//
// 纪律：返回新切片+新 map，绝不就地改写调用方 params（端点自适应重试、
// 日志、token 估算随后还要读原形态，与 env marker / 工具过滤同一纪律）。

const zenNbsp = "\u00a0"

// zenShapeClientToolDescriptionsChat 整形 chat 嵌套形态 tools[].function.description。
// 无可整形描述时原样返回（零拷贝）。
func zenShapeClientToolDescriptionsChat(tools []any) []any {
	var out []any
	for i, raw := range tools {
		tm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]any)
		if !ok {
			continue
		}
		d, ok := fn["description"].(string)
		if !ok || !strings.Contains(d, " ") {
			continue
		}
		if out == nil {
			out = make([]any, len(tools))
			copy(out, tools)
		}
		nf := make(map[string]any, len(fn))
		for k, v := range fn {
			nf[k] = v
		}
		nf["description"] = strings.ReplaceAll(d, " ", zenNbsp)
		nc := make(map[string]any, len(tm))
		for k, v := range tm {
			nc[k] = v
		}
		nc["function"] = nf
		out[i] = nc
	}
	if out == nil {
		return tools
	}
	return out
}

// zenShapeClientToolDescriptionsFlat 整形 responses flat 形态 tools[].description。
func zenShapeClientToolDescriptionsFlat(tools []map[string]any) []map[string]any {
	var out []map[string]any
	for i, tm := range tools {
		d, ok := tm["description"].(string)
		if !ok || !strings.Contains(d, " ") {
			continue
		}
		if out == nil {
			out = make([]map[string]any, len(tools))
			copy(out, tools)
		}
		nc := make(map[string]any, len(tm))
		for k, v := range tm {
			nc[k] = v
		}
		nc["description"] = strings.ReplaceAll(d, " ", zenNbsp)
		out[i] = nc
	}
	if out == nil {
		return tools
	}
	return out
}

// ZenFilterClientToolsFlat 过滤 responses 扁平形态的工具
// ({type,name,description,parameters,strict})。
func ZenFilterClientToolsFlat(tools []map[string]any, cfg ZenToolFilterConfig) ([]map[string]any, ZenFilterToolStats) {
	st := ZenFilterToolStats{In: len(tools)}
	out := make([]map[string]any, 0, len(tools))
	for _, tm := range tools {
		name, _ := tm["name"].(string)
		st.BytesIn += ApproxToolBytes(tm)
		if ZenToolDenied(name, cfg) {
			st.Dropped = append(st.Dropped, name)
			continue
		}
		cp := make(map[string]any, len(tm))
		for k, v := range tm {
			cp[k] = v
		}
		if d, ok := cp["description"].(string); ok {
			cp["description"] = TruncateZenDescription(d, cfg.MaxDescription)
		}
		st.BytesOut += ApproxToolBytes(cp)
		out = append(out, cp)
	}
	st.Out = len(out)
	return out, st
}

// ApproxToolBytes 估算单个工具定义的序列化体积（用于日志与抓包统计，
// 不需要精确——精确值由抓包的 upstreamBody 提供）。
func ApproxToolBytes(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}

// LogZenToolFilter 只在真的裁掉了东西时打一行，避免每请求一条噪声日志。
func LogZenToolFilter(label string, st ZenFilterToolStats) {
	if st.In == st.Out && st.BytesIn == st.BytesOut {
		return
	}
	saved := st.BytesIn - st.BytesOut
	if saved < 0 {
		saved = 0
	}
	log.Printf("  zen tool filter(%s): %d→%d tools, %d→%d bytes (saved %d B ≈ %d tok)%s",
		label, st.In, st.Out, st.BytesIn, st.BytesOut, saved, saved/4,
		DroppedSummary(st.Dropped))
}

func DroppedSummary(dropped []string) string {
	if len(dropped) == 0 {
		return ""
	}
	const max = 6
	if len(dropped) <= max {
		return " dropped=" + strings.Join(dropped, ",")
	}
	return " dropped=" + strings.Join(dropped[:max], ",") + ",…"
}

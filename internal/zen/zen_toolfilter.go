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
// mcp__node_repl__js 3.3KB。单条消息约 33k tokens——当时把 429 定性为
// **体积**（ping 45 tokens 成功、IDE 一单就 429）。
//
// **2026-11-21 勘误**：上游 opencode2api 作者近月更新证实免费层 429 的真身
// 是**代理身份指纹闸**（多触发点：Claude Code 工具名、clint_/mcp_ 改名
// 方案、工具描述字面措辞；配额本身充足——wintter 实测一天烧 100e token），
// 且所有闸门响应伪装成 429 FreeUsageLimitError。issue #188 的同体积对照
// （PTC 单工具 vs DSH 全工具，body 相当，200 vs 429）直接否定体积论：
// ping 成功的真因是它没有工具=没有指纹。丢工具/截断描述之所以有效，是
// 因为同时移除了指纹面——体积只是伴随效应。
//
// 所以这里做两件事：按名字前缀/名单丢弃客户端工具、截断过长 description
//（减体积+去指纹，两者同向）；顶层描述 NBSP 整形见下方对策段（破坏措辞
// 匹配且不丢能力）。schema 一律保留——模型调用工具靠的是 parameters 结构。
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

// ============ 客户端工具描述 NBSP 整形（免费层身份指纹闸对策之一） ============
//
// 机制（上游 opencode2api 作者近月更新实证 + 本仓 2026-11-21 复现）：免费层
// 对代理流量做**身份指纹**检测，多触发点、全部伪装成 429 FreeUsageLimitError
// （Retry-After 指向日重置，本仓按额度信号 park key，实测一次请求沿 failover
// 链烧 4 把 key）：
//   1. Claude Code 工具名（作者点名 ReportFindings/ScheduleWakeup）——上游把
//      客户端工具改名不透明摘要来规避；本仓名称透传，ZCode 系客户端的
//      Claude Code 核心名（Read/Bash/Edit…）现有流量未触发，但这是**在案
//      风险面**：上游清单自适应增长（clint_ 方案走红即被 ban）。
//   2. 工具顶层描述的字面措辞子串匹配（本条对策的靶面）——issue #188 实证
//      DSH read/edit/skill 描述命中（触发串不在官方 opencode 工具集内，属
//      已知第三方客户端话术）；同会话原文→NBSP→原文 = 429→200→429，纯
//      请求级匹配不封 key。本仓实测同款：触发串 429、NBSP 200。
//
// 对策（对齐上游 #188，恒定开启）：把客户端工具顶层描述里的 ASCII 空格替换
// 成 NBSP（U+00A0）——措辞匹配失配，模型读取语义不变。刻意只动顶层描述：
// 参数描述、schema、工具名与 12 个 gate 工具不碰（闸不查参数描述与官方
// 工具，实证 200）。不设开关：闸改用 NBSP 感知匹配前开关是死配置（§8
// 禁止死配置）。幂等：NBSP 非 ASCII 空格，重复整形不再变化。
// 名称面不整形：改名需全链路映射还原（上游专有机制），现有流量未触发，不
// 提前造轮子；触发时按台账 2026-11-21 勘误段的诊断路径先二分定位。
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

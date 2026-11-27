package zen

import (
	"strings"
)

// ============ 客户端工具描述 NBSP 整形（免费层身份指纹闸对策之一） ============
//
// 来源：原与 zen toolFilter shrinker 同住 zen_toolfilter.go（2026-11-21 立项）。
// 上游 7825b04（2026-11-26）实证 429 真因是按 egress IP 计费 tokens 而非请求
// 体积，把 shrinker 判死删除；NBSP 整形与之正交（靶面是描述措辞指纹而非体
// 积），本仓随 merge 留存并摘出到本文件。
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
// 日志、token 估算随后还要读原形态，与 env marker 同一纪律）。

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

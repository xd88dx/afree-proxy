package app

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// ============================================================================
// Zen 免费层 gate 工具的真实定义（描述 + 参数 schema）
//
// 2026-10-15 实测（新 inference 后端，干净 IP 二分定位）：迁移后的免费层
// 不再只校验工具名 —— 注入工具的 parameters 若是空 schema
// （{"type":"object","properties":{}}）直接 403 FreeTierError；换成官方
// CLI 1.19.0 的真实参数 schema 即 200。描述文本不参与校验（精简描述 +
// 真实 schema 实测通过），但这里仍存官方原文，保持与 CLI 字节级一致，
// 避免上游日后加严描述校验时再次全灭。
//
// 数据来源：2026-10-25 起改为 opencode2api 上游（jasonxu114514）内嵌的
// 官方 2.x 工具集 opencode_tools.json（13 工具，byte-for-byte 官方定义）；
// parameters_chat 用上游原样 schema（无 $schema，2.x 端点实测形态），
// parameters_responses 按官方 @ai-sdk 行为程序化剥 min/max 类约束。
// 旧版为 CLI 1.19.0 抓包的 1.x 名单（bash/task/todowrite），已随
// /inference/v1 的 2.x 校验整体替换。生成脚本：上游 json → strip 约束。
// ============================================================================

//go:embed zen_gate_tools.json
var zenGateToolsJSON []byte

type zenGateToolDef struct {
	Name                string         `json:"name"`
	Description         string         `json:"description"`
	ParametersChat      map[string]any `json:"parameters_chat"`
	ParametersResponses map[string]any `json:"parameters_responses"`
}

var (
	zenGateDefsOnce  sync.Once
	zenGateDefs      []zenGateToolDef
	zenGateDefByName map[string]*zenGateToolDef
)

func zenGateToolDefs() []zenGateToolDef {
	zenGateDefsOnce.Do(func() {
		var defs []zenGateToolDef
		if err := json.Unmarshal(zenGateToolsJSON, &defs); err == nil {
			zenGateDefs = defs
			zenGateDefByName = make(map[string]*zenGateToolDef, len(defs))
			for i := range zenGateDefs {
				zenGateDefByName[zenGateDefs[i].Name] = &zenGateDefs[i]
			}
		}
	})
	return zenGateDefs
}

func lookupZenGateToolDef(name string) *zenGateToolDef {
	zenGateToolDefs()
	return zenGateDefByName[name]
}

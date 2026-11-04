package app

import (
	"encoding/json"
)

// ============================================================================
// 平台 state 快照的「账号事实合并」（2026-11-03 三层口径）
//
// 备份的 state 段是全量快照（配置态 + 账号事实 + 本机统计，2026-11-01 起与
// *-state.json 同形态）。导入时按三层分流：
//
//   - 配置态段（各平台 *-config 对应段）：总是整体替换（与本开关无关）；
//   - 账号事实（key/账号冷却表）：总是应用；
//   - 本机统计（keyTokens 记账、请求计数、轮转游标、出口健康）：仅
//     导入方勾选「导入统计数据」时应用；不勾选时**保留本机现值**。
//
// 对 amd/oprt/tkhb/other 这四个以 JSON 往返（不暴露包内类型）的平台，合并
// 在 JSON 层做：键面约定 cool = 事实、keyTokens/tokens = 统计。trae 段不走
// 本文件（2026-11-04 起由 trae2apiRestoreBackup 单独处理 config/routing/
// accounts 全段）。旧版备份带统计字段时同样被本合并忽略（统计不迁移）。
// ============================================================================

// stateFactsTopKeys amd/oprt/tkhb/other 形态的账号事实键（其余键一律保留本机）。
const stateFactsTopKeys = "cool"

// mergeStateFactsJSON 把备份 state 快照里的账号事实合并进本机快照，返回合并
// 结果（可直接交给对应平台的 ImportStateJSON 整体落盘）。
//   - local / backup 均为平台 StateSnapshotJSON 的同形态 JSON。
//
// 任一侧解析失败返回错误、调用方放弃本次合并（宁可不动也不误写）。
func mergeStateFactsJSON(local, backup json.RawMessage) (json.RawMessage, error) {
	var lm, bm map[string]json.RawMessage
	if err := json.Unmarshal(local, &lm); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(backup, &bm); err != nil {
		return nil, err
	}
	if lm == nil {
		lm = map[string]json.RawMessage{}
	}
	// 冷却表：备份携带即整体替换（与 wb 的 ImportStateBackupJSON 同语义：
	// 备份没带 = 保持不动）。
	if v, ok := bm[stateFactsTopKeys]; ok {
		lm[stateFactsTopKeys] = v
	}
	return json.Marshal(lm)
}

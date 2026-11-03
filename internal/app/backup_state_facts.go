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
//   - 账号事实（key/账号冷却表、trae 的 credits/签到状态）：总是应用；
//   - 本机统计（keyTokens/tokens 记账、请求计数、轮转游标、出口健康）：仅
//     导入方勾选「导入统计数据」时应用；不勾选时**保留本机现值**。
//
// 对 amd/oprt/tkhb/other/trae 这五个以 JSON 往返（不暴露包内类型）的平台，
// 合并在 JSON 层做：键面约定 cool = 事实、keyTokens/tokens = 统计。旧版备份
// 带统计字段时同样被本合并忽略（统计不迁移）。
// ============================================================================

// stateFactsTopKeys amd/oprt/tkhb/other 形态的账号事实键（其余键一律保留本机）。
const stateFactsTopKeys = "cool"

// mergeStateFactsJSON 把备份 state 快照里的账号事实合并进本机快照，返回合并
// 结果（可直接交给对应平台的 ImportStateJSON 整体落盘）。
//   - local / backup 均为平台 StateSnapshotJSON 的同形态 JSON；
//   - traeAccounts=true 时按 trae 形态处理 accounts.*（credits/checkin/
//     checkinDeviceGen 是事实、tokens 是统计），否则按 {cool,keyTokens} 形态。
//
// 任一侧解析失败返回错误、调用方放弃本次合并（宁可不动也不误写）。
func mergeStateFactsJSON(local, backup json.RawMessage, traeAccounts bool) (json.RawMessage, error) {
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
	if traeAccounts {
		if err := mergeTraeAccountFacts(lm, bm); err != nil {
			return nil, err
		}
	}
	return json.Marshal(lm)
}

// mergeTraeAccountFacts trae 的 accounts.* 按账号合并：credits/checkin/
// checkinDeviceGen（账号事实）从备份覆盖；tokens（本机 token 记账）永远保留
// 本机值；备份有而本机没有的账号建条目（无 tokens——本机统计不迁移）。
// lm/bm 已在 mergeStateFactsJSON 解包，本函数只动 "accounts" 键。
func mergeTraeAccountFacts(lm, bm map[string]json.RawMessage) error {
	bRaw, ok := bm["accounts"]
	if !ok {
		return nil // 备份没带账号段 = 保持不动（按段替换语义）
	}
	var bAccs map[string]json.RawMessage
	if err := json.Unmarshal(bRaw, &bAccs); err != nil {
		return err
	}
	var lAccs map[string]json.RawMessage
	if len(lm["accounts"]) > 0 {
		if err := json.Unmarshal(lm["accounts"], &lAccs); err != nil {
			return err
		}
	}
	if lAccs == nil {
		lAccs = map[string]json.RawMessage{}
	}
	for id, bObjRaw := range bAccs {
		var bObj map[string]json.RawMessage
		if err := json.Unmarshal(bObjRaw, &bObj); err != nil {
			return err
		}
		delete(bObj, "tokens") // 本机统计不迁移
		if lRaw, ok := lAccs[id]; ok && len(lRaw) > 0 {
			var lObj map[string]json.RawMessage
			if err := json.Unmarshal(lRaw, &lObj); err != nil {
				return err
			}
			// 事实字段逐键覆盖（缺键 = 备份没有该事实 = 保持本机）。
			for _, k := range []string{"credits", "checkin", "checkinDeviceGen"} {
				if v, ok := bObj[k]; ok {
					lObj[k] = v
				}
			}
			out, err := json.Marshal(lObj)
			if err != nil {
				return err
			}
			lAccs[id] = out
			continue
		}
		out, err := json.Marshal(bObj)
		if err != nil {
			return err
		}
		lAccs[id] = out
	}
	accs, err := json.Marshal(lAccs)
	if err != nil {
		return err
	}
	lm["accounts"] = accs
	return nil
}

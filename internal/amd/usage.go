package amd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 使用痕迹落盘（amd-state.json）============
//
// KeyTokens（token 记账）与 Cool（冷却表）是随请求/429 变化的使用痕迹，
// 2026-10-31 从 amd-config.json 拆出独立落盘：配置文件恢复"仅面板操作才变"，
// 外部监测配置变更不再被记账噪声淹没。冷却落盘持久化语义不变（2026-10-28
// 用户定：重启后冷却保留）。
//
// 数据真身是包级 usage，仍由 config.go 的 mu 保护（读写点与拆分前完全同锁，
// 只是落盘目标换成独立文件）；记账合批 10s，冷却/解除即时落盘。

type usageData struct {
	// KeyTokens token 记账：key 明文 -> 今日/累计 prompt+completion 消耗与
	// 最近成功时间（面板"今日/累计 token"与"最近使用"列）。
	KeyTokens map[string]KeyTokenStats `json:"keyTokens,omitempty"`
	// Cool 冷却表：key 明文 -> 冷却截止时刻。选号时惰性清理过期项。
	Cool map[string]time.Time `json:"cool,omitempty"`
}

var (
	// usage 痕迹真身（mu 保护）。usageData 本身即落盘形态。
	usage = &usageData{}
	// UsagePath 痕迹文件路径（app 层注入 DATA_DIR/amd-state.json）；空 = 不落盘
	// （测试场景），内存记账照常。
	UsagePath string
)

// usageFlush token 记账的合批落盘器：记账后只打脏标记，由后台 goroutine 周期
// 写盘；saveUsage 自带串行保护，调用方持锁期间不要 Mark。
var usageFlush = kit.NewDirtyFlusher(10*time.Second, saveUsage)

// markTokensDirty 记账后打脏标记（RecordKeyTokens 调用）。
func markTokensDirty() { usageFlush.Mark() }

// saveUsage 痕迹文件原子落盘（tmp + rename，失败降级直写，与 config.go 的
// save 同款）。marshal 全程持 mu.RLock：两个 map 都是就地上写的活 map，无锁
// marshal 会与 RecordKeyTokens/Cooldown 的写入构成并发崩溃（2026-10-29 审计
// 修复口径在痕迹侧同样适用）。
func saveUsage() {
	saveMu.Lock()
	defer saveMu.Unlock()
	if UsagePath == "" {
		return
	}
	mu.RLock()
	data, err := json.MarshalIndent(usage, "", "  ")
	mu.RUnlock()
	if err != nil {
		fmt.Printf("  amd usage marshal failed: %v\n", err)
		return
	}
	tmp := UsagePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  amd usage save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, UsagePath); err != nil {
		if werr := os.WriteFile(UsagePath, data, 0600); werr != nil {
			fmt.Printf("  amd usage replace failed: %v\n", err)
			return
		}
		_ = os.Remove(tmp)
	}
}

// renameLegacyUsageState 命名统一迁移（2026-11-01）：状态文件统一为
// *-state.json；旧 *-usage.json 存在且新名缺失时改名续用（两段式迁移的
// 第二段，第一段是"config 痕迹键 → 独立文件"）。
func renameLegacyUsageState() {
	legacy := strings.TrimSuffix(UsagePath, "-state.json") + "-usage.json"
	if legacy == UsagePath {
		return
	}
	if _, err := os.Stat(legacy); err != nil {
		return
	}
	if _, err := os.Stat(UsagePath); err == nil {
		return // 新名已在：旧名留档不动
	}
	if err := os.Rename(legacy, UsagePath); err == nil {
		fmt.Printf("  amd state: renamed legacy %s -> %s\n", legacy, UsagePath)
	}
}

// loadUsage 读取痕迹文件（Load 尾部调用一次）。新文件缺失时从旧
// amd-config.json 一次性迁移 keyTokens/cool 键（幂等：新文件一旦存在即不再
// 迁移）；解析失败改名留档（与 config 同款），空表启动。
func loadUsage() {
	renameLegacyUsageState() // 2026-11-01 命名统一：旧 *-usage.json 改名续用
	if UsagePath == "" {
		return
	}
	data, err := os.ReadFile(UsagePath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("  amd usage read %s: %v (starting empty)\n", UsagePath, err)
			return
		}
		migrateUsageFromConfig()
		return
	}
	if err := json.Unmarshal(data, usage); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(UsagePath, UsagePath+".corrupt-"+stamp); renErr == nil {
			fmt.Printf("  amd usage corrupt JSON; moved to %s.corrupt-%s\n", UsagePath, stamp)
		}
		return
	}
	if usage.Cool == nil {
		usage.Cool = map[string]time.Time{}
	}
}

// migrateUsageFromConfig 从旧 amd-config.json 原文提取 keyTokens/cool 并写入
// 独立文件。必须在任何 amd-config.json 写回之前执行（initAMD 里 SetUsagePath
// 先于 Load，Load 之内先 loadUsage 后任何 save，天然满足）。
func migrateUsageFromConfig() {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		return // 没有旧文件 = 没有可迁移的数据
	}
	var shim usageData
	if err := json.Unmarshal(data, &shim); err != nil {
		return
	}
	if shim.Cool == nil {
		shim.Cool = map[string]time.Time{}
	}
	usage.KeyTokens = shim.KeyTokens
	usage.Cool = shim.Cool
	if len(shim.KeyTokens) == 0 && len(shim.Cool) == 0 {
		return
	}
	saveUsage()
	// 顺手重写配置文件剥离旧痕迹键（Config 已无对应字段，序列化自然不含）。
	save()
	fmt.Printf("  amd usage: migrated %d token entry(s) and %d cooldown(s) from %s\n",
		len(shim.KeyTokens), len(shim.Cool), ConfigPath)
}

// StateSnapshotJSON 统计快照（配置备份 state 段，2026-11-01）：与落盘文件
// 同形态的 JSON；marshal 全程持 mu.RLock（活 map 就地上写，与 saveUsage 同款）。
func StateSnapshotJSON() json.RawMessage {
	mu.RLock()
	data, err := json.Marshal(usage)
	mu.RUnlock()
	if err != nil {
		return nil
	}
	return data
}

// ImportStateJSON 统计导入（配置备份还原）：整体替换真身并即时落盘。
// 键随备份走，不在此处清理孤儿（pruneRemovedKeys 的既有时机会按 key 池收敛）。
func ImportStateJSON(raw json.RawMessage) error {
	var next usageData
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if next.Cool == nil {
		next.Cool = map[string]time.Time{}
	}
	mu.Lock()
	usage = &next
	mu.Unlock()
	saveUsage()
	return nil
}

// ClearUsage 清空全部使用痕迹（token 记账 + 冷却表）并即时落盘。清空数据/
// 备份导入按新 key 集合重建时调用。
func ClearUsage() {
	mu.Lock()
	usage.KeyTokens = nil
	usage.Cool = map[string]time.Time{}
	mu.Unlock()
	saveUsage()
}

// pruneUsage 清理不再存在于 key 池的痕迹条目（pruneRemovedKeys 的痕迹侧）。
// 调用方必须持有 mu。
func pruneUsageLocked(valid map[string]bool) {
	for k := range usage.KeyTokens {
		if !valid[k] {
			delete(usage.KeyTokens, k)
		}
	}
	for k := range usage.Cool {
		if !valid[k] {
			delete(usage.Cool, k)
		}
	}
	if len(usage.KeyTokens) == 0 {
		usage.KeyTokens = nil
	}
}

package app

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"afree-proxy/internal/kit"
)

// ============================================================================
// zen 使用痕迹落盘（zen-state.json）
//
// Cool（key 冷却表）与 KeyTokens（key token 记账）是随请求/429 变化的使用
// 痕迹，2026-10-31 从 zen-config.json 拆出独立落盘：配置文件恢复"仅面板操作
// 才变"，外部监测配置变更不再被记账噪声淹没。冷却落盘持久化语义不变
//（2026-10-28 用户定：重启后冷却保留）。
//
// 数据真身仍是包级 zenKeyCool/zenKeyTokens（zenKeyMu 保护，见 zen.go），
// 本文件只负责它们的落盘形态与读写；记账合批 10s（zenTokensFlush），冷却/
// 解除即时落盘。
// ============================================================================

// zenUsageFile zen-state.json 的落盘形态。内层 stats 沿用 zenKeyTokenStats
// 的默认键名（与拆分前 zen-config.json 里的形态一致）。
type zenUsageFile struct {
	Cool      map[string]time.Time        `json:"cool,omitempty"`
	KeyTokens map[string]zenKeyTokenStats `json:"keyTokens,omitempty"`
}

var (
	// zenUsagePathOverride 测试钩子：非空时覆盖痕迹文件路径。
	zenUsagePathOverride string
)

func zenUsagePath() string {
	if zenUsagePathOverride != "" {
		return zenUsagePathOverride
	}
	return kit.ResolveDataPath("zen-state.json")
}

// saveZenUsage 快照 zenKeyCool/zenKeyTokens 原子落盘（tmp+rename）。
func saveZenUsage() {
	zenKeyMu.Lock()
	out := zenUsageFile{
		Cool:      make(map[string]time.Time, len(zenKeyCool)),
		KeyTokens: make(map[string]zenKeyTokenStats, len(zenKeyTokens)),
	}
	for k, v := range zenKeyCool {
		out.Cool[k] = v
	}
	for k, v := range zenKeyTokens {
		out.KeyTokens[k] = v
	}
	zenKeyMu.Unlock()
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Printf("zen usage: marshal: %v", err)
		return
	}
	atomicWriteFile(zenUsagePath(), raw)
}

// renameLegacyZenState 命名统一迁移（2026-11-01）：状态文件统一为
// *-state.json；旧 *-usage.json 存在且新名缺失时改名续用（两段式迁移的
// 第二段，第一段是"config 痕迹键 → 独立文件"）。
func renameLegacyZenState() {
	path := zenUsagePath()
	legacy := strings.TrimSuffix(path, "-state.json") + "-usage.json"
	if legacy == path {
		return
	}
	if _, err := os.Stat(legacy); err != nil {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return // 新名已在：旧名留档不动
	}
	if err := os.Rename(legacy, path); err == nil {
		log.Printf("zen state: renamed legacy %s -> %s", legacy, path)
	}
}

// loadZenUsage 启动加载痕迹文件回填真身（init 调用，替代原 zenConfig 回填）。
// 新文件缺失时从旧 zen-config.json 一次性迁移 cool/keyTokens 键（幂等：新
// 文件一旦存在即不再迁移）；解析失败改名留档，空表启动。
func loadZenUsage() {
	renameLegacyZenState()
	path := zenUsagePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("zen usage: read %s: %v (starting empty)", path, err)
			return
		}
		migrateZenUsageFromConfig()
		return
	}
	var f zenUsageFile
	if err := json.Unmarshal(raw, &f); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
			log.Printf("zen usage: corrupt JSON at %s; moved to %s.corrupt-%s", path, path, stamp)
		}
		log.Printf("zen usage: parse failed: %v (starting empty)", err)
		return
	}
	zenKeyMu.Lock()
	if f.Cool != nil {
		zenKeyCool = f.Cool
	}
	if f.KeyTokens != nil {
		zenKeyTokens = f.KeyTokens
	}
	zenKeyMu.Unlock()
	log.Printf("zen usage: restored %d cooldown(s) and %d token entry(s) from %s",
		len(f.Cool), len(f.KeyTokens), path)
}

// legacyZenConfigShim 从旧 zen-config.json 原文提取 cool/keyTokens 的一次性
// 迁移载体（与 zenConfigData 解耦）。
type legacyZenConfigShim struct {
	Cool      map[string]time.Time        `json:"cool,omitempty"`
	KeyTokens map[string]zenKeyTokenStats `json:"keyTokens,omitempty"`
}

// migrateZenUsageFromConfig 从旧 zen-config.json 提取痕迹并写入独立文件。
func migrateZenUsageFromConfig() {
	raw, err := os.ReadFile(kit.ResolveDataPath("zen-config.json"))
	if err != nil {
		return
	}
	var shim legacyZenConfigShim
	if err := json.Unmarshal(raw, &shim); err != nil {
		return
	}
	if len(shim.Cool) == 0 && len(shim.KeyTokens) == 0 {
		return
	}
	zenKeyMu.Lock()
	zenKeyCool = shim.Cool
	zenKeyTokens = shim.KeyTokens
	zenKeyMu.Unlock()
	saveZenUsage()
	// 顺手重写配置文件剥离旧 cool/keyTokens 键（zenConfigData 已无对应字段）。
	saveZenConfig()
	log.Printf("zen usage: migrated %d cooldown(s) and %d token entry(s) from zen-config.json",
		len(shim.Cool), len(shim.KeyTokens))
}

// zenStateBackupSnapshot 统计快照（配置备份 state 段，2026-11-01）：真身
// zenKeyCool/zenKeyTokens 在 zenKeyMu 下深拷。
func zenStateBackupSnapshot() *zenUsageFile {
	zenKeyMu.Lock()
	out := zenUsageFile{
		Cool:      make(map[string]time.Time, len(zenKeyCool)),
		KeyTokens: make(map[string]zenKeyTokenStats, len(zenKeyTokens)),
	}
	for k, v := range zenKeyCool {
		out.Cool[k] = v
	}
	for k, v := range zenKeyTokens {
		out.KeyTokens[k] = v
	}
	zenKeyMu.Unlock()
	return &out
}

// importZenStateBackup 统计导入（配置备份还原）：整体替换真身并即时落盘。
func importZenStateBackup(f *zenUsageFile) {
	zenKeyMu.Lock()
	if f.Cool != nil {
		zenKeyCool = f.Cool
	}
	if f.KeyTokens != nil {
		zenKeyTokens = f.KeyTokens
	}
	zenKeyMu.Unlock()
	saveZenUsage()
}

// ClearZenUsage 清空全部 zen key 痕迹（冷却 + 记账）并即时落盘。清空数据时调用。
func ClearZenUsage() {
	zenKeyMu.Lock()
	zenKeyCool = map[string]time.Time{}
	zenKeyTokens = map[string]zenKeyTokenStats{}
	zenKeyMu.Unlock()
	saveZenUsage()
}

package cline

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"afree-proxy/internal/kit"
)

// ============================================================================
// cline 使用痕迹落盘（cline-state.json）
//
// Status/用量计数/冷却（per-account）与 CurrentIdx（轮转游标）是随请求变化
// 的使用痕迹，2026-10-31 从 cline-config.json 拆出独立落盘：配置文件恢复
// "仅面板操作/备份导入才变"，外部监测配置变更不再被 2s flusher 的记账噪声
// 淹没。冷却/判死状态落盘持久化语义不变（即时写痕迹文件）。
//
// 数据真身是包级 poolUsage，仍由 PoolMu 保护（读写点与拆分前痕迹写在
// Account 上完全同锁）；落盘走 poolSaveMu 串行 + tmp/rename，热路径打
// poolDirty 脏标记由 2s flusher 合批（pool.go）。
// ============================================================================

var (
	poolUsage = &PoolUsageData{}
)

// UsagePath cline 痕迹文件路径，运行时求值（不走 init 缓存）：测试经
// t.Setenv("DATA_DIR", …) 切换数据目录后必须生效，理由同 DataPath
// （2026-11-17）。
// UsagePathOverride 测试钩子：非空时覆盖痕迹文件路径。
var UsagePathOverride string

// SetUsagePathOverride 测试钩子：覆盖痕迹文件路径（"" = 清除）。
func SetUsagePathOverride(v string) { UsagePathOverride = v }

// GetUsagePathOverride 当前 override（测试恢复用）。
func GetUsagePathOverride() string { return UsagePathOverride }

// Usage 真身指针（测试经 SetUsageForTest 替换；不要在包外直接改写）。
func Usage() *PoolUsageData { return poolUsage }

// SetUsageForTest 测试替换痕迹真身（备份导入路径的白盒验证用）。
func SetUsageForTest(u *PoolUsageData) { poolUsage = u }

func UsagePath() string {
	if UsagePathOverride != "" {
		return UsagePathOverride
	}
	return kit.ResolveDataPath("cline-state.json")
}

// UsageForLocked 返回（必要时创建）账号痕迹表。调用方必须持有 PoolMu。
func UsageForLocked(acc *Account) *AccountUsage {
	if poolUsage.Accounts == nil {
		poolUsage.Accounts = map[string]*AccountUsage{}
	}
	u := poolUsage.Accounts[acc.AccountID]
	if u == nil {
		u = &AccountUsage{Status: "active"}
		poolUsage.Accounts[acc.AccountID] = u
	}
	return u
}

// UsagePeekLocked 只读查询账号痕迹（不创建，nil = 无记录）。
// 调用方必须持有 PoolMu（或至少 PoolMu.RLock 语义下只读）。
func UsagePeekLocked(acc *Account) *AccountUsage {
	if acc == nil || poolUsage.Accounts == nil {
		return nil
	}
	return poolUsage.Accounts[acc.AccountID]
}

// AccountStatusLocked 账号状态读取（默认 active：新账号无痕迹记录即 active，
// 与拆分前零值行为一致）。调用方必须持有 PoolMu。
func AccountStatusLocked(acc *Account) string {
	if u := UsagePeekLocked(acc); u != nil {
		return u.Status
	}
	return "active"
}

// AccountStatus 导出的账号状态读取（CLI/诊断用）：自拿 PoolMu 的安全副本读。
func AccountStatus(acc *Account) string {
	if acc == nil {
		return ""
	}
	LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()
	return AccountStatusLocked(acc)
}

// SaveUsageLocked 痕迹文件原子落盘；调用方必须持有 PoolMu（快照一致性）
// —— 与 SavePoolLocked 同款但只写痕迹文件。
func SaveUsageLocked() {
	poolSaveMu.Lock()
	defer poolSaveMu.Unlock()
	data, err := json.MarshalIndent(poolUsage, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal pool usage: %v", err)
		return
	}
	tmp := UsagePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		log.Printf("Failed to save pool usage: %v", err)
		return
	}
	if err := os.Rename(tmp, UsagePath()); err != nil {
		log.Printf("Failed to save pool usage (rename): %v", err)
	}
}

// SaveUsage 痕迹落盘（锁外入口）。
func SaveUsage() {
	PoolMu.Lock()
	defer PoolMu.Unlock()
	SaveUsageLocked()
}

// loadPoolUsage 启动加载痕迹文件（LoadPool 尾部调用，PoolMu 已持有）。
// 缺失即空表启动；解析失败改名留档（与池文件同款）。旧格式的一次性迁移已随
// 2026-11-01 死代码清理移除——升级前先在旧版导出含统计快照的备份，新版导入还原。
func loadPoolUsage() {
	raw, err := os.ReadFile(UsagePath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("pool usage: read %s: %v (starting empty)", UsagePath(), err)
			return
		}
	}
	var f PoolUsageData
	if err := json.Unmarshal(raw, &f); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(UsagePath(), UsagePath()+".corrupt-"+stamp); renErr == nil {
			log.Printf("pool usage: corrupt JSON at %s; moved to %s.corrupt-%s", UsagePath(), UsagePath(), stamp)
		}
		log.Printf("pool usage: parse failed: %v (starting empty)", err)
		return
	}
	if f.Accounts == nil {
		f.Accounts = map[string]*AccountUsage{}
	}
	poolUsage = &f
	log.Printf("pool usage: restored %d account trace(s), cursor=%d from %s",
		len(f.Accounts), f.CurrentIdx, UsagePath())
} // poolStateBackupSnapshot 统计快照（配置备份 state 段，2026-11-01）。
// 调用方必须持有 PoolMu。
func StateBackupSnapshotLocked() *PoolUsageData {
	out := &PoolUsageData{CurrentIdx: poolUsage.CurrentIdx}
	if len(poolUsage.Accounts) > 0 {
		out.Accounts = make(map[string]*AccountUsage, len(poolUsage.Accounts))
		for id, u := range poolUsage.Accounts {
			if u == nil {
				continue
			}
			cp := *u
			out.Accounts[id] = &cp
		}
	}
	return out
}

// ImportStateBackupLocked 统计导入（配置备份还原）：整体替换真身。
// 调用方必须持有 PoolMu；落盘由调用方随后统一执行（导入路径本就接 SavePool）。
func ImportStateBackupLocked(f *PoolUsageData) {
	next := &PoolUsageData{CurrentIdx: f.CurrentIdx}
	if f.Accounts != nil {
		next.Accounts = make(map[string]*AccountUsage, len(f.Accounts))
		for id, u := range f.Accounts {
			if u == nil {
				continue
			}
			cp := *u
			next.Accounts[id] = &cp
		}
	}
	poolUsage = next
}

// ImportStateFactsLocked 账号事实合并（2026-11-03 三层口径）：status/
// cooldownUntil/lastReason 是账号事实（账号在上游的客观状态），勾选与否都
// 应用；lastUsed/用量计数/token 记账/轮转游标是本机统计，保留本机现值。
// 备份没有的账号不创建空痕迹（不凭空造状态）。调用方必须持有 PoolMu。
func ImportStateFactsLocked(f *PoolUsageData) {
	if f == nil || len(f.Accounts) == 0 {
		return
	}
	if poolUsage.Accounts == nil {
		poolUsage.Accounts = map[string]*AccountUsage{}
	}
	for id, u := range f.Accounts {
		if u == nil {
			continue
		}
		cur := poolUsage.Accounts[id]
		if cur == nil {
			cur = &AccountUsage{Status: u.Status}
			poolUsage.Accounts[id] = cur
		}
		if u.Status != "" {
			cur.Status = u.Status
		}
		cur.CooldownUntil = u.CooldownUntil
		cur.LastReason = u.LastReason
	}
}

// clearPoolUsage 清空全部账号痕迹与游标并即时落盘（清空数据/导入重建时调用）。
// 调用方必须持有 PoolMu。
func ClearUsageLocked() {
	poolUsage = &PoolUsageData{}
	SaveUsageLocked()
}

// PruneUsageLocked 清理不再在池中的账号的痕迹条目。调用方必须持有 PoolMu。
func PruneUsageLocked(valid map[string]bool) {
	for id := range poolUsage.Accounts {
		if !valid[id] {
			delete(poolUsage.Accounts, id)
		}
	}
}

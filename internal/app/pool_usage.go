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
// cline 使用痕迹落盘（cline-state.json）
//
// Status/用量计数/冷却（per-account）与 CurrentIdx（轮转游标）是随请求变化
// 的使用痕迹，2026-10-31 从 cline-config.json 拆出独立落盘：配置文件恢复
// "仅面板操作/备份导入才变"，外部监测配置变更不再被 2s flusher 的记账噪声
// 淹没。冷却/判死状态落盘持久化语义不变（即时写痕迹文件）。
//
// 数据真身是包级 poolUsage，仍由 poolMu 保护（读写点与拆分前痕迹写在
// Account 上完全同锁）；落盘走 poolSaveMu 串行 + tmp/rename，热路径打
// poolDirty 脏标记由 2s flusher 合批（pool.go）。
// ============================================================================

var (
	poolUsage     = &poolUsageData{}
	poolUsagePath string
)

func init() {
	poolUsagePath = kit.ResolveDataPath("cline-state.json")
}

// usageForLocked 返回（必要时创建）账号痕迹表。调用方必须持有 poolMu。
func usageForLocked(acc *Account) *accountUsage {
	if poolUsage.Accounts == nil {
		poolUsage.Accounts = map[string]*accountUsage{}
	}
	u := poolUsage.Accounts[acc.AccountID]
	if u == nil {
		u = &accountUsage{Status: "active"}
		poolUsage.Accounts[acc.AccountID] = u
	}
	return u
}

// usagePeekLocked 只读查询账号痕迹（不创建，nil = 无记录）。
// 调用方必须持有 poolMu（或至少 poolMu.RLock 语义下只读）。
func usagePeekLocked(acc *Account) *accountUsage {
	if acc == nil || poolUsage.Accounts == nil {
		return nil
	}
	return poolUsage.Accounts[acc.AccountID]
}

// accountStatusLocked 账号状态读取（默认 active：新账号无痕迹记录即 active，
// 与拆分前零值行为一致）。调用方必须持有 poolMu。
func accountStatusLocked(acc *Account) string {
	if u := usagePeekLocked(acc); u != nil {
		return u.Status
	}
	return "active"
}

// AccountStatus 导出的账号状态读取（CLI/诊断用）：自拿 poolMu 的安全副本读。
func AccountStatus(acc *Account) string {
	if acc == nil {
		return ""
	}
	loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	return accountStatusLocked(acc)
}

// savePoolUsageLocked 痕迹文件原子落盘；调用方必须持有 poolMu（快照一致性）
// —— 与 savePoolLocked 同款但只写痕迹文件。
func savePoolUsageLocked() {
	poolSaveMu.Lock()
	defer poolSaveMu.Unlock()
	data, err := json.MarshalIndent(poolUsage, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal pool usage: %v", err)
		return
	}
	tmp := poolUsagePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		log.Printf("Failed to save pool usage: %v", err)
		return
	}
	if err := os.Rename(tmp, poolUsagePath); err != nil {
		log.Printf("Failed to save pool usage (rename): %v", err)
	}
}

// savePoolUsage 痕迹落盘（锁外入口）。
func savePoolUsage() {
	poolMu.Lock()
	defer poolMu.Unlock()
	savePoolUsageLocked()
}

// renameLegacyPoolState 命名统一迁移（2026-11-01）：状态文件统一为
// *-state.json；旧 *-usage.json 存在且新名缺失时改名续用（两段式迁移的
// 第二段，第一段是"config 痕迹键 → 独立文件"）。调用方持有 poolMu 的
// loadPool 时序内执行，rename 只动盘上文件不触碰内存态。
func renameLegacyPoolState() {
	legacy := strings.TrimSuffix(poolUsagePath, "-state.json") + "-usage.json"
	if legacy == poolUsagePath {
		return
	}
	if _, err := os.Stat(legacy); err != nil {
		return
	}
	if _, err := os.Stat(poolUsagePath); err == nil {
		return // 新名已在：旧名留档不动
	}
	if err := os.Rename(legacy, poolUsagePath); err == nil {
		log.Printf("pool state: renamed legacy %s -> %s", legacy, poolUsagePath)
	}
}

// loadPoolUsage 启动加载痕迹文件（loadPool 尾部调用，poolMu 已持有）。
// 新文件缺失时从旧 cline-config.json 一次性迁移账号痕迹与游标（幂等：新文件
// 一旦存在即不再迁移）；解析失败改名留档（与池文件同款），空表启动。
func loadPoolUsage() {
	renameLegacyPoolState()
	raw, err := os.ReadFile(poolUsagePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("pool usage: read %s: %v (starting empty)", poolUsagePath, err)
			return
		}
		migratePoolUsageFromConfig()
		return
	}
	var f poolUsageData
	if err := json.Unmarshal(raw, &f); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(poolUsagePath, poolUsagePath+".corrupt-"+stamp); renErr == nil {
			log.Printf("pool usage: corrupt JSON at %s; moved to %s.corrupt-%s", poolUsagePath, poolUsagePath, stamp)
		}
		log.Printf("pool usage: parse failed: %v (starting empty)", err)
		return
	}
	if f.Accounts == nil {
		f.Accounts = map[string]*accountUsage{}
	}
	poolUsage = &f
	log.Printf("pool usage: restored %d account trace(s), cursor=%d from %s",
		len(f.Accounts), f.CurrentIdx, poolUsagePath)
}

// legacyAccountShim/legacyPoolShim 从旧 cline-config.json 原文提取痕迹的
// 一次性迁移载体（与 Account/AccountPool 解耦）。
type legacyAccountShim struct {
	AccountID       string    `json:"accountId"`
	Status          string    `json:"status"`
	LastUsed        time.Time `json:"lastUsed"`
	UsageCount      int64     `json:"usageCount"`
	UsageCountToday int64     `json:"usageCountToday"`
	UsageDate       string    `json:"usageDate"`
	TokensTotal     int64     `json:"tokensTotal"`
	TokensToday     int64     `json:"tokensToday"`
	TokensDate      string    `json:"tokensDate"`
	CooldownUntil   time.Time `json:"cooldownUntil,omitempty"`
	LastReason      string    `json:"lastReason,omitempty"`
}

type legacyPoolShim struct {
	CurrentIdx int                  `json:"currentIdx"`
	Accounts   []*legacyAccountShim `json:"accounts"`
}

// migratePoolUsageFromConfig 从旧 cline-config.json 提取痕迹并写入独立文件。
// 必须在任何 cline-config.json 写回之前执行（StartProxy 的 loadPool 在一切
// 保存之前，天然满足）。
func migratePoolUsageFromConfig() {
	raw, err := os.ReadFile(poolPath)
	if err != nil {
		return
	}
	var shim legacyPoolShim
	if err := json.Unmarshal(raw, &shim); err != nil {
		return
	}
	next := &poolUsageData{CurrentIdx: shim.CurrentIdx, Accounts: map[string]*accountUsage{}}
	n := 0
	for _, la := range shim.Accounts {
		if la == nil || la.AccountID == "" {
			continue
		}
		next.Accounts[la.AccountID] = &accountUsage{
			Status:          la.Status,
			LastUsed:        la.LastUsed,
			UsageCount:      la.UsageCount,
			UsageCountToday: la.UsageCountToday,
			UsageDate:       la.UsageDate,
			TokensTotal:     la.TokensTotal,
			TokensToday:     la.TokensToday,
			TokensDate:      la.TokensDate,
			CooldownUntil:   la.CooldownUntil,
			LastReason:      la.LastReason,
		}
		n++
	}
	if n == 0 && shim.CurrentIdx == 0 {
		return
	}
	poolUsage = next
	savePoolUsageLocked()
	// 顺手重写配置文件剥离旧痕迹键（pool 结构已无对应字段，序列化自然不含）：
	// 升级即完成清理，配置文件立刻达到"无痕迹键"的监测口径。poolMu 由
	// loadPool 调用方持有，savePoolLocked 可直接调。
	savePoolLocked()
	log.Printf("pool usage: migrated %d account trace(s), cursor=%d from cline-config.json",
		n, shim.CurrentIdx)
}

// poolStateBackupSnapshot 统计快照（配置备份 state 段，2026-11-01）。
// 调用方必须持有 poolMu。
func poolStateBackupSnapshotLocked() *poolUsageData {
	out := &poolUsageData{CurrentIdx: poolUsage.CurrentIdx}
	if len(poolUsage.Accounts) > 0 {
		out.Accounts = make(map[string]*accountUsage, len(poolUsage.Accounts))
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

// importPoolStateBackupLocked 统计导入（配置备份还原）：整体替换真身。
// 调用方必须持有 poolMu；落盘由调用方随后统一执行（导入路径本就接 savePool）。
func importPoolStateBackupLocked(f *poolUsageData) {
	next := &poolUsageData{CurrentIdx: f.CurrentIdx}
	if f.Accounts != nil {
		next.Accounts = make(map[string]*accountUsage, len(f.Accounts))
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

// clearPoolUsage 清空全部账号痕迹与游标并即时落盘（清空数据/导入重建时调用）。
// 调用方必须持有 poolMu。
func clearPoolUsageLocked() {
	poolUsage = &poolUsageData{}
	savePoolUsageLocked()
}

// prunePoolUsageLocked 清理不再在池中的账号的痕迹条目。调用方必须持有 poolMu。
func prunePoolUsageLocked(valid map[string]bool) {
	for id := range poolUsage.Accounts {
		if !valid[id] {
			delete(poolUsage.Accounts, id)
		}
	}
}

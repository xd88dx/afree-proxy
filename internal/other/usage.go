package other

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 使用痕迹落盘（other-state.json）============
//
// KeyTokens（token 记账）与 Cool（冷却表）是随请求/429 变化的使用痕迹，
// 2026-10-31 从 other-config.json 拆出独立落盘：配置文件恢复"仅面板操作才变"，
// 外部监测配置变更不再被记账噪声淹没。冷却落盘持久化语义不变（2026-10-28
// 用户定：重启后冷却保留）。
//
// 结构差异（相对 amd/or/th）：子平台列表是动态的，痕迹按 slug 分键，跟
// slug 走而不随 Platform 对象拷贝（ClonePlatform/写时复制不涉及痕迹）。
//
// 锁：痕迹真身由 keyMu 保护（与拆分前痕迹就地写在活 Platform 上的锁域一致），
// saveUsage 锁序 saveMu → keyMu，与 config.go 的 save（saveMu → keyMu → mu）
// 同向；无任何 mu → keyMu 反向路径的既有约定不破坏。

type platformUsage struct {
	// KeyTokens token 记账：key 明文 -> 今日/累计 prompt+completion 消耗与
	// 最近成功时间（面板"今日/累计 token"与"最近使用"列）。
	KeyTokens map[string]KeyTokenStats `json:"keyTokens,omitempty"`
	// Cool 冷却表：key 明文 -> 冷却截止时刻。选号时惰性清理过期项。
	Cool map[string]time.Time `json:"cool,omitempty"`
}

type usageData struct {
	// Platforms slug -> 子平台痕迹表。
	Platforms map[string]*platformUsage `json:"platforms,omitempty"`
}

var (
	// usage 痕迹真身（keyMu 保护）。usageData 本身即落盘形态。
	usage = &usageData{Platforms: map[string]*platformUsage{}}
	// UsagePath 痕迹文件路径（app 层注入 DATA_DIR/other-state.json）；空 = 不落盘
	// （测试场景），内存记账照常。
	UsagePath string
)

// usageFlush token 记账的合批落盘器：记账后只打脏标记，由后台 goroutine 周期
// 写盘；saveUsage 自带串行保护，调用方持锁期间不要 Mark。
var usageFlush = kit.NewDirtyFlusher(10*time.Second, saveUsage)

// markTokensDirty 标记 token 记账有未落盘变更。
func markTokensDirty() { usageFlush.Mark() }

// usageForLocked 返回（必要时创建）slug 的痕迹表。调用方必须持 keyMu。
func usageForLocked(slug string) *platformUsage {
	if usage.Platforms == nil {
		usage.Platforms = map[string]*platformUsage{}
	}
	u := usage.Platforms[slug]
	if u == nil {
		u = &platformUsage{}
		usage.Platforms[slug] = u
	}
	return u
}

// usageBySlugLocked 只读查询 slug 的痕迹表（不创建，nil = 无记录）。
// 调用方必须持 keyMu。
func usageBySlugLocked(slug string) *platformUsage { return usage.Platforms[slug] }

// saveUsage 痕迹文件原子落盘（tmp + rename，失败降级直写）。marshal 持 keyMu
// （痕迹表是 keyMu 域的就地写活 map），文件 IO 在锁外（与 save 同款）。
func saveUsage() {
	saveMu.Lock()
	defer saveMu.Unlock()
	if UsagePath == "" {
		return
	}
	keyMu.Lock()
	data, err := json.MarshalIndent(usage, "", "  ")
	keyMu.Unlock()
	if err != nil {
		fmt.Printf("  other usage marshal failed: %v\n", err)
		return
	}
	tmp := UsagePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  other usage save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, UsagePath); err != nil {
		if werr := os.WriteFile(UsagePath, data, 0600); werr != nil {
			fmt.Printf("  other usage replace failed: %v\n", err)
			return
		}
		_ = os.Remove(tmp)
	}
}

// loadUsage 读取痕迹文件（Load 尾部调用一次）。新文件缺失时从旧
// other-config.json 一次性迁移各子平台的 keyTokens/cool 键（幂等：新文件一旦
// 存在即不再迁移）；解析失败改名留档（与 config 同款），空表启动。
func loadUsage() {
	if UsagePath == "" {
		return
	}
	data, err := os.ReadFile(UsagePath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("  other usage read %s: %v (starting empty)\n", UsagePath, err)
			return
		}
		return
	}
	if err := json.Unmarshal(data, usage); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(UsagePath, UsagePath+".corrupt-"+stamp); renErr == nil {
			fmt.Printf("  other usage corrupt JSON; moved to %s.corrupt-%s\n", UsagePath, stamp)
		}
		return
	}
	if usage.Platforms == nil {
		usage.Platforms = map[string]*platformUsage{}
	}
}
// StateSnapshotJSON 统计快照（配置备份 state 段，2026-11-01）：与落盘文件
// 同形态的 JSON；痕迹真身由 keyMu 保护，快照深拷（含 per-slug 表）。
func StateSnapshotJSON() json.RawMessage {
	keyMu.Lock()
	out := usageData{Platforms: make(map[string]*platformUsage, len(usage.Platforms))}
	for slug, u := range usage.Platforms {
		if u == nil {
			continue
		}
		cp := *u
		if u.KeyTokens != nil {
			m := make(map[string]KeyTokenStats, len(u.KeyTokens))
			for k, v := range u.KeyTokens {
				m[k] = v
			}
			cp.KeyTokens = m
		}
		if u.Cool != nil {
			m := make(map[string]time.Time, len(u.Cool))
			for k, v := range u.Cool {
				m[k] = v
			}
			cp.Cool = m
		}
		out.Platforms[slug] = &cp
	}
	keyMu.Unlock()
	data, err := json.Marshal(&out)
	if err != nil {
		return nil
	}
	return data
}

// ImportStateJSON 统计导入（配置备份还原）：整体替换真身并即时落盘。
// 子平台集合随备份走，孤儿条目由既有的 Set/prune 时机收敛。
func ImportStateJSON(raw json.RawMessage) error {
	var next usageData
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if next.Platforms == nil {
		next.Platforms = map[string]*platformUsage{}
	}
	keyMu.Lock()
	usage = &next
	keyMu.Unlock()
	saveUsage()
	return nil
}

// ClearAllUsage 清空全部子平台的使用痕迹并即时落盘。清空数据时调用。
func ClearAllUsage() {
	keyMu.Lock()
	usage.Platforms = map[string]*platformUsage{}
	keyMu.Unlock()
	saveUsage()
}

// PrunePlatformUsage 清理子平台里不再存在于 key 池的痕迹条目（key 池随
// 面板保存收敛时调用，保持与拆分前 normalizeKeys 的记账收敛同口径）。
func PrunePlatformUsage(slug string, keys []string) {
	valid := map[string]bool{}
	for _, k := range keys {
		valid[k] = true
	}
	keyMu.Lock()
	u := usageBySlugLocked(slug)
	if u != nil {
		for k := range u.KeyTokens {
			if !valid[k] {
				delete(u.KeyTokens, k)
			}
		}
		if len(u.KeyTokens) == 0 {
			u.KeyTokens = nil
		}
		for k := range u.Cool {
			if !valid[k] {
				delete(u.Cool, k)
			}
		}
	}
	keyMu.Unlock()
	markTokensDirty()
}

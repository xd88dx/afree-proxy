package freepi

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 使用痕迹落盘（freepi-state.json）============
//
// KeyTokens（token 记账）/Cool（冷却表）/Credits（余额快照）是随请求/429/
// 查询变化的使用痕迹，独立于 freepi-config.json 落盘（*-config 与 *-state
// 命名铁律：配置文件恢复"仅面板操作才变"，外部监测不被记账噪声淹没）。
// 冷却落盘持久化语义与其他 key 池一致（2026-10-28 用户定：重启后保留）。
//
// 锁：痕迹真身由 keyMu 保护（pool.go 的轮转游标另有 keyIdxMu）；saveUsage
// 锁序 saveMu → keyMu，与 config.go 的 save（saveMu → keyMu → mu.RLock）
// 同向；无任何 mu → keyMu 反向路径的既有约定不破坏。

// CreditStats 单个 key 的 credits 余额快照（GET /me 免费查询，不打对话
// 额度）：今日剩余/总额度与 UTC 补充时刻、账号 GitHub handle、最近查询时间。
type CreditStats struct {
	Remaining int64     `json:"remaining,omitempty"`
	Allowance int64     `json:"allowance,omitempty"`
	RefillAt  string    `json:"refillAt,omitempty"`
	Handle    string    `json:"handle,omitempty"`
	MeAt      time.Time `json:"meAt,omitempty"`
}

// usageData 即落盘形态（单平台，无分键维度）。
type usageData struct {
	// KeyTokens token 记账：key 明文 -> 今日/累计消耗与最近成功时间。
	KeyTokens map[string]KeyTokenStats `json:"keyTokens,omitempty"`
	// Cool 冷却表：key 明文 -> 冷却截止时刻。选号时惰性清理过期项。
	Cool map[string]time.Time `json:"cool,omitempty"`
	// Credits credits 余额快照：key 明文 -> /me 查询结果（Test/刷新额度落）。
	Credits map[string]CreditStats `json:"credits,omitempty"`
}

var (
	// usage 痕迹真身（keyMu 保护）。usageData 本身即落盘形态。
	usage = &usageData{}
	// UsagePath 痕迹文件路径（app 层注入 DATA_DIR/freepi-state.json）；
	// 空 = 不落盘（测试场景），内存记账照常。
	UsagePath string
)

// usageFlush 记账的合批落盘器：记账后只打脏标记，后台 goroutine 周期写盘；
// saveUsage 自带串行保护，调用方持锁期间不要 Mark。
var usageFlush = kit.NewDirtyFlusher(10*time.Second, saveUsage)

// markTokensDirty 标记痕迹有未落盘变更。
func markTokensDirty() { usageFlush.Mark() }

// usageLocked 返回痕迹表（调用方必须持 keyMu）。
func usageLocked() *usageData { return usage }

// saveUsage 痕迹文件原子落盘（tmp + rename，失败降级直写）。marshal 持 keyMu
// （痕迹表是 keyMu 域的就地写活 map），文件 IO 在锁外（与 config save 同款）。
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
		fmt.Printf("  freepi usage marshal failed: %v\n", err)
		return
	}
	tmp := UsagePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  freepi usage save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, UsagePath); err != nil {
		if werr := os.WriteFile(UsagePath, data, 0600); werr != nil {
			fmt.Printf("  freepi usage replace failed: %v\n", err)
			return
		}
		_ = os.Remove(tmp)
	}
}

// loadUsage 读取痕迹文件（Load 尾部调用一次）。解析失败改名留档（与 config
// 同款），空表启动。
func loadUsage() {
	if UsagePath == "" {
		return
	}
	data, err := os.ReadFile(UsagePath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("  freepi usage read %s: %v (starting empty)\n", UsagePath, err)
		}
		return
	}
	if err := json.Unmarshal(data, usage); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(UsagePath, UsagePath+".corrupt-"+stamp); renErr == nil {
			fmt.Printf("  freepi usage corrupt JSON; moved to %s.corrupt-%s\n", UsagePath, stamp)
		}
	}
}

// StateSnapshotJSON 统计快照（配置备份 state 段）：与落盘文件同形态的 JSON；
// 痕迹真身由 keyMu 保护，快照深拷。
func StateSnapshotJSON() json.RawMessage {
	keyMu.Lock()
	out := usageData{
		KeyTokens: make(map[string]KeyTokenStats, len(usage.KeyTokens)),
		Cool:      make(map[string]time.Time, len(usage.Cool)),
		Credits:   make(map[string]CreditStats, len(usage.Credits)),
	}
	for k, v := range usage.KeyTokens {
		out.KeyTokens[k] = v
	}
	for k, v := range usage.Cool {
		out.Cool[k] = v
	}
	for k, v := range usage.Credits {
		out.Credits[k] = v
	}
	keyMu.Unlock()
	data, err := json.Marshal(&out)
	if err != nil {
		return nil
	}
	return data
}

// ImportStateJSON 统计导入（配置备份还原）：整体替换真身并即时落盘。
// key 集合随备份走，孤儿条目由既有的 Set/PruneUsage 时机收敛。
func ImportStateJSON(raw json.RawMessage) error {
	var next usageData
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	keyMu.Lock()
	usage = &next
	keyMu.Unlock()
	saveUsage()
	return nil
}

// ClearUsage 清空全部使用痕迹并即时落盘。清空数据时调用。
func ClearUsage() {
	keyMu.Lock()
	usage = &usageData{}
	keyMu.Unlock()
	saveUsage()
}

// PruneUsage 清理不再存在于 key 池的痕迹条目（key 池随面板保存收敛时调用，
// 保持与其他 key 池平台的记账收敛同口径）。
func PruneUsage(keys []string) {
	valid := map[string]bool{}
	for _, k := range keys {
		valid[k] = true
	}
	keyMu.Lock()
	for k := range usage.KeyTokens {
		if !valid[k] {
			delete(usage.KeyTokens, k)
		}
	}
	if len(usage.KeyTokens) == 0 {
		usage.KeyTokens = nil
	}
	for k := range usage.Cool {
		if !valid[k] {
			delete(usage.Cool, k)
		}
	}
	for k := range usage.Credits {
		if !valid[k] {
			delete(usage.Credits, k)
		}
	}
	if len(usage.Credits) == 0 {
		usage.Credits = nil
	}
	keyMu.Unlock()
	markTokensDirty()
}

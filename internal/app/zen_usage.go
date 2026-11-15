package app

import (
	"encoding/json"
	"log"
	"os"
	"sync"
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
	// Sessions 会话铸态表（2026-11-15 从 zen-config.json 迁入，同域运行时
	// 状态）：403 被动换新闸门的支撑数据，见 zen_session.go。真身
	// zenSessions 在 zenSessMu 下；本文件写入统一走 zenStateWriteMu。
	Sessions map[string]*zenSessionEntry `json:"sessions,omitempty"`
}

var (
	// zenUsagePathOverride 测试钩子：非空时覆盖痕迹文件路径。
	zenUsagePathOverride string
	// zenStateWriteMu zen-state.json 文件级写互斥：sessions（403 换新）与
	// usage（冷却/token 记账）两个写方共用同一文件，避免互相整段覆盖。
	zenStateWriteMu sync.Mutex
)

func zenUsagePath() string {
	if zenUsagePathOverride != "" {
		return zenUsagePathOverride
	}
	return kit.ResolveDataPath("zen-state.json")
}

// saveZenUsage 快照 zenKeyCool/zenKeyTokens 原子落盘（tmp+rename）。
// readZenUsageFile 只读加载 zen-state.json（不回填真身，供迁移/定向写段）。
// 文件缺失返回零值结构；损坏改名留档（调用方按 zenSessSaveBlocked 语义处理）。
func readZenUsageFile() zenUsageFile {
	path := zenUsagePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return zenUsageFile{}
	}
	var f zenUsageFile
	if err := json.Unmarshal(raw, &f); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
			log.Printf("zen usage: corrupt JSON at %s; moved to %s.corrupt-%s", path, path, stamp)
		}
		log.Printf("zen usage: parse failed: %v", err)
		return zenUsageFile{}
	}
	return f
}

func saveZenUsage() {
	zenStateWriteMu.Lock()
	defer zenStateWriteMu.Unlock()
	// Sessions 快照（zenSessMu）与 Cool/KeyTokens 快照（zenKeyMu）分开取，
	// 两把锁不嵌套——锁顺序恒为 zenSessMu→zenKeyMu，反向持有者只等待不回拿。
	zenSessMu.Lock()
	sessions := make(map[string]*zenSessionEntry, len(zenSessions))
	for k, v := range zenSessions {
		sessions[k] = v
	}
	zenSessMu.Unlock()
	zenKeyMu.Lock()
	out := zenUsageFile{
		Cool:      make(map[string]time.Time, len(zenKeyCool)),
		KeyTokens: make(map[string]zenKeyTokenStats, len(zenKeyTokens)),
		Sessions:  sessions,
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

// loadZenUsage 启动加载痕迹文件回填真身（init 调用）。缺失即空表启动；
// 解析失败改名留档。旧格式的一次性迁移已随 2026-11-01 死代码清理移除——
// 升级前先在旧版导出含统计快照的备份，新版导入还原。
func loadZenUsage() {
	path := zenUsagePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		// 两个分支都直接返回：文件不存在时若继续走 Unmarshal(nil) 会打出
		// 误导性的 "parse failed"，还会白试一次 rename。
		if !os.IsNotExist(err) {
			log.Printf("zen usage: read %s: %v (starting empty)", path, err)
		}
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
	zenSessMu.Lock()
	if f.Sessions != nil {
		zenSessions = f.Sessions
	}
	zenSessMu.Unlock()
	zenKeyMu.Lock()
	if f.Cool != nil {
		zenKeyCool = f.Cool
	}
	if f.KeyTokens != nil {
		zenKeyTokens = f.KeyTokens
	}
	zenKeyMu.Unlock()
	log.Printf("zen usage: restored %d cooldown(s), %d token entry(s) and %d session(s) from %s",
		len(f.Cool), len(f.KeyTokens), len(f.Sessions), path)
} // zenStateBackupSnapshot 统计快照（配置备份 state 段，2026-11-01）：真身
// zenKeyCool/zenKeyTokens 在 zenKeyMu 下深拷。
func zenStateBackupSnapshot() *zenUsageFile {
	zenSessMu.Lock()
	sessions := make(map[string]*zenSessionEntry, len(zenSessions))
	for k, v := range zenSessions {
		sessions[k] = v
	}
	zenSessMu.Unlock()
	zenKeyMu.Lock()
	out := zenUsageFile{
		Cool:      make(map[string]time.Time, len(zenKeyCool)),
		KeyTokens: make(map[string]zenKeyTokenStats, len(zenKeyTokens)),
		Sessions:  sessions,
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

// importZenStateFacts 账号事实合并（2026-11-03 三层口径）：key 冷却表是账号
// 事实（key 在上游被限流的截止时刻），勾选与否都应用；token 记账是本机统计，
// 保留本机现值。
func importZenStateFacts(f *zenUsageFile) {
	if f == nil || f.Cool == nil {
		return
	}
	zenKeyMu.Lock()
	zenKeyCool = f.Cool
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

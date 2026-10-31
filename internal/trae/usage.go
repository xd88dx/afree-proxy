package trae

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 使用痕迹落盘（trae-state.json）============
//
// Tokens/Credits/Checkin/CheckinDeviceGen（per-account）与 Cool（账号冷却表）
// 是随请求/签到/额度刷新变化的使用痕迹，2026-10-31 从 trae-config.json 拆出
// 独立落盘：配置文件恢复"仅面板操作/凭据续期才变"，外部监测配置变更不再被
// 噪声淹没。冷却落盘持久化语义不变（2026-10-28 用户定：重启后冷却保留）；
// Token/RefreshToken/ExpiredAt 凭据本体仍随配置落盘（续期回写属凭据轮转）。
//
// 数据真身是包级 usage（账号 ID 键），由 config.go 的 mu 保护（与拆分前痕迹
// 就地写在活 Account 上的锁域一致）；记账合批 10s，冷却/解除/签到/额度快照
// 即时落盘。

type accountUsage struct {
	// Tokens token 记账（今日/累计 prompt+completion，跨日重置）。
	Tokens TokenStats `json:"tokens,omitempty"`
	// Credits 最近一次额度查询快照（后台定期刷新 + Test 即时刷新）。
	Credits *Credits `json:"credits,omitempty"`
	// Checkin 最近一次签到状态（status/claim 结果投影，面板"状态"列展示）。
	Checkin *CheckinState `json:"checkin,omitempty"`
	// CheckinDeviceGen 签到设备 ID 代际（9074 换设备后 +1，持久化）：
	// 设备 ID = deviceID16Gen(账号ID, 代际)，代际 0 为初始派生。
	CheckinDeviceGen int `json:"checkinDeviceGen,omitempty"`
}

type usageData struct {
	// Accounts 账号 ID -> 痕迹表。
	Accounts map[string]*accountUsage `json:"accounts,omitempty"`
	// Cool 冷却表：账号 ID -> 冷却截止时刻。选号时惰性清理过期项。
	Cool map[string]time.Time `json:"cool,omitempty"`
}

var (
	// usage 痕迹真身（mu 保护）。usageData 本身即落盘形态；Accounts 冷启动
	// 即初始化——所有写入路径（迁移/记账/导入）都对它就地上写，nil map 会
	// 直接 panic（2026-11-01 本机容器实测踩过，勿回退为零值初始化）。
	usage = &usageData{Accounts: map[string]*accountUsage{}}
	// UsagePath 痕迹文件路径（app 层注入 DATA_DIR/trae-state.json）；空 = 不落盘
	// （测试场景），内存记账照常。
	UsagePath string
)

// usageFlush token 记账的合批落盘器：记账后只打脏标记，由后台 goroutine 周期
// 写盘；saveUsage 自带串行保护，调用方持锁期间不要 Mark。
var usageFlush = kit.NewDirtyFlusher(10*time.Second, saveUsage)

// markTokensDirty 记账后打脏标记（RecordAccountTokens 调用）。
func markTokensDirty() { usageFlush.Mark() }

// usageForLocked 返回（必要时创建）账号痕迹表。调用方必须持 mu（写锁语义，
// 会创建条目）。
func usageForLocked(id string) *accountUsage {
	if usage.Accounts == nil {
		usage.Accounts = map[string]*accountUsage{}
	}
	u := usage.Accounts[id]
	if u == nil {
		u = &accountUsage{}
		usage.Accounts[id] = u
	}
	return u
}

// usagePeekLocked 只读查询账号痕迹表（不创建，nil = 无记录）。
// 调用方必须持 mu（读或写锁均可）。
func usagePeekLocked(id string) *accountUsage { return usage.Accounts[id] }

// UsageFor 导出的痕迹读取（副本语义，锁外安全）：面板列表/路由热路径用。
func UsageFor(id string) accountUsage {
	mu.Lock()
	defer mu.Unlock()
	if u := usage.Accounts[id]; u != nil {
		return *u
	}
	return accountUsage{}
}

// saveUsage 痕迹文件原子落盘（tmp + rename，失败降级直写，与 config.go 的
// save 同款）。marshal 全程持 mu.RLock：痕迹结构都是就地上写的活对象。
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
		fmt.Printf("  trae usage marshal failed: %v\n", err)
		return
	}
	tmp := UsagePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  trae usage save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, UsagePath); err != nil {
		if werr := os.WriteFile(UsagePath, data, 0600); werr != nil {
			fmt.Printf("  trae usage replace failed: %v\n", err)
			return
		}
		_ = os.Remove(tmp)
	}
}

// loadUsage 读取痕迹文件（Load 尾部调用一次）。缺失即空表启动；解析失败
// 改名留档（与 config 同款）。旧格式的一次性迁移已随 2026-11-01 死代码清理
// 移除——升级前先在旧版导出含统计快照的备份，新版导入还原。
func loadUsage() {
	if UsagePath == "" {
		return
	}
	data, err := os.ReadFile(UsagePath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("  trae usage read %s: %v (starting empty)\n", UsagePath, err)
			return
		}
		return
	}
	if err := json.Unmarshal(data, usage); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(UsagePath, UsagePath+".corrupt-"+stamp); renErr == nil {
			fmt.Printf("  trae usage corrupt JSON; moved to %s.corrupt-%s\n", UsagePath, stamp)
		}
		return
	}
	if usage.Cool == nil {
		usage.Cool = map[string]time.Time{}
	}
}
// StateSnapshotJSON 统计快照（配置备份 state 段，2026-11-01）：与落盘文件
// 同形态的 JSON。Credits/Checkin 深拷——marshal 在锁外进行，不与额度刷新/
// 签到写路径竞争。
func StateSnapshotJSON() json.RawMessage {
	mu.RLock()
	out := usageData{
		Accounts: make(map[string]*accountUsage, len(usage.Accounts)),
		Cool:     make(map[string]time.Time, len(usage.Cool)),
	}
	for id, u := range usage.Accounts {
		if u == nil {
			continue
		}
		cp := *u
		if u.Credits != nil {
			c := *u.Credits
			cp.Credits = &c
		}
		if u.Checkin != nil {
			c := *u.Checkin
			cp.Checkin = &c
		}
		out.Accounts[id] = &cp
	}
	for k, v := range usage.Cool {
		out.Cool[k] = v
	}
	mu.RUnlock()
	data, err := json.Marshal(&out)
	if err != nil {
		return nil
	}
	return data
}

// ImportStateJSON 统计导入（配置备份还原）：整体替换真身并即时落盘。
func ImportStateJSON(raw json.RawMessage) error {
	var next usageData
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if next.Cool == nil {
		next.Cool = map[string]time.Time{}
	}
	if next.Accounts == nil {
		next.Accounts = map[string]*accountUsage{}
	}
	mu.Lock()
	usage = &next
	mu.Unlock()
	saveUsage()
	return nil
}

// ClearUsage 清空全部使用痕迹（账号痕迹 + 冷却表）并即时落盘。清空数据时调用。
func ClearUsage() {
	mu.Lock()
	usage = &usageData{Cool: map[string]time.Time{}}
	mu.Unlock()
	saveUsage()
}

// pruneUsage 清理不再在池中的账号的痕迹/冷却条目（pruneRemovedAccounts 的
// 痕迹侧）。调用方必须持有 mu。
func pruneUsageLocked(valid map[string]bool) {
	for id := range usage.Accounts {
		if !valid[id] {
			delete(usage.Accounts, id)
		}
	}
	for id := range usage.Cool {
		if !valid[id] {
			delete(usage.Cool, id)
		}
	}
	if len(usage.Accounts) == 0 {
		usage.Accounts = nil
	}
}

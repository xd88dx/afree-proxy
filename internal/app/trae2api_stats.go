package app

// TraeWork（vendored trae2api）持久记账与显示排序（2026-11-05 用户定）：
//
//   - 今日/累计 token 记账：vendored metrics 只有近 100 条流水与 byDay 聚合，
//     撑不起"跨重启的持久今日/累计"口径。经 vendor 注入的 server.OnRecord
//     钩子（上游原样 nil）把每个完成请求的 usage 回传到这里，落独立文件
//     trae2api/tokens.json（与 metrics.json 同目录同为可丢观测语义，不进
//     配置监测口径）。跨日惰性清零（Date != 今天时重置 Today），与其他
//     七个平台同口径；累计值永不自动清零。
//   - 显示顺序：vendored 池的 p.order 本就是 All()/List() 的输出序（只是
//     上游从不改它，恒为 UID 排序）。SetDisplayOrder 持锁重排 + Pool().Save()
//     落盘，仅影响面板/状态列表展示顺序——选号是引擎加权随机，与顺序无关
//     （与 WorkBuddy SetOrder 同语义，用户确认过的口径）。

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"afree-proxy/internal/trae2api/metrics"
)

type trae2apiTokenStats struct {
	Today     int64  `json:"today"`
	Total     int64  `json:"total"`
	Date      string `json:"date"` // Today 归属的自然日（本地时区）
	LastUsed  string `json:"last_used,omitempty"`
	LastModel string `json:"last_model,omitempty"`
}

type trae2apiTokenState struct {
	Version int                           `json:"version"`
	SavedAt string                        `json:"saved_at"`
	Items   map[string]trae2apiTokenStats `json:"items"`
}

var (
	traeTokensMu    sync.Mutex
	traeTokens      = map[string]trae2apiTokenStats{}
	traeTokensPath  string // 启动时按子系统数据目录解析；空 = 未初始化（仅内存）
	traeTokensDirty bool

	// traeDisplayOrder TraeWork 凭据展示顺序的内存副本（真身在
	// proxy-config.json 的 trae2apiDisplayOrder 段，与 trae2apiBindings
	// 同文件同锁管理）。启动装载、SetDisplayOrder/导入时同步。
	traeOrderMu      sync.Mutex
	traeDisplayOrder []string
)

// traeDisplayOrderLoadLocked 从 proxy-config 装载顺序副本（调用方须已持
// proxyFileMu 或在启动单线程路径）。
func traeDisplayOrderLoad() {
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	order := append([]string(nil), f.Trae2apiDisplayOrder...)
	proxyFileMu.Unlock()
	traeOrderMu.Lock()
	traeDisplayOrder = order
	traeOrderMu.Unlock()
}

// trae2apiTokensLoad 初始化记账表路径并加载存量。dataDir 由调用方显式传入
// （trae2apiStart 在持有 trae2apiMu 期间经 traeWireHooks 调到这里——不能走
// trae2apiCurrent()，否则锁重入自死锁，2026-11-05 部署实测抓出）。
func trae2apiTokensLoad(dataDir string) {
	traeTokensMu.Lock()
	defer traeTokensMu.Unlock()
	if dataDir == "" {
		traeTokensPath = ""
		return
	}
	traeTokensPath = dataDir + "/tokens.json"
	raw, err := os.ReadFile(traeTokensPath)
	if err != nil {
		return // 全新环境/文件缺失 = 空表起步
	}
	var st trae2apiTokenState
	if json.Unmarshal(raw, &st) != nil {
		return // 损坏文件按空表起步，下次保存覆盖
	}
	traeTokens = st.Items
	if traeTokens == nil {
		traeTokens = map[string]trae2apiTokenStats{}
	}
}

func trae2apiTokensSaveLocked() {
	if traeTokensPath == "" {
		return
	}
	b, err := json.MarshalIndent(trae2apiTokenState{
		Version: 1,
		SavedAt: time.Now().Format(time.RFC3339),
		Items:   traeTokens,
	}, "", "  ")
	if err != nil {
		return
	}
	atomicWriteFile(traeTokensPath, b)
}

// trae2apiTokenRecorder 挂到 vendored server.OnRecord 的持久记账回调。
// 每次 chatStat.done()（含流式与非流式）都会调用；表随 60s 合批落盘（与
// proxy-state 同节拍），进程退出由子系统重启前的 Pool().Save() 序列兜底。
func trae2apiTokenRecorder(s metrics.Sample) {
	if s.Account == "" || s.Account == "-" || !s.HasUsage {
		return // 无账号归属或无 usage 观测：与 metrics 聚合同判据，不记 0
	}
	traeTokensMu.Lock()
	defer traeTokensMu.Unlock()
	today := time.Now().Format("2006-01-02")
	st := traeTokens[s.Account]
	if st.Date != today {
		st.Date = today
		st.Today = 0
	}
	st.Today += s.PromptTokens + s.CompletionTokens
	st.Total += s.PromptTokens + s.CompletionTokens
	st.LastUsed = time.Now().Format(time.RFC3339)
	if s.Model != "" {
		st.LastModel = s.Model
	}
	traeTokens[s.Account] = st
	traeTokensDirty = true
}

// startTrae2apiTokenFlusher 记账合批落盘循环（60s，与 proxy-state 同节拍）。
func startTrae2apiTokenFlusher() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			traeTokensMu.Lock()
			if traeTokensDirty {
				traeTokensDirty = false
				trae2apiTokensSaveLocked()
			}
			traeTokensMu.Unlock()
		}
	}()
}

// trae2apiTokensSnapshot 单账号记账视图（跨日惰性清零的展示口径：条目的
// Date 不是今天就返回今日 0——与 cline/zen 等 Record 侧惰性重置同效果，
// 但只影响展示，不改记账真身）。
func trae2apiTokensSnapshot(uid string) (today, total int64, lastUsed, lastModel string) {
	traeTokensMu.Lock()
	defer traeTokensMu.Unlock()
	st, ok := traeTokens[uid]
	if !ok {
		return 0, 0, "", ""
	}
	today = st.Today
	if st.Date != time.Now().Format("2006-01-02") {
		today = 0
	}
	return today, st.Total, st.LastUsed, st.LastModel
}

// trae2apiSetDisplayOrder 重排 vendored 池的展示顺序（仅面板/状态列表，
// 不改加权选号）。顺序落 proxy-config.json 的 trae2apiDisplayOrder 段
// （2026-11-05 用户定：顺序改动必须落盘、随配置备份迁移——此前是进程内
// 展示态，重启即回 UID 序，实测丢）。未提及/未知 uid 排尾按 UID；备份导出
// /导入经 TraeDisplayOrder 段（admin_backup.go）。
func trae2apiSetDisplayOrder(uids []string) int {
	sub := trae2apiCurrent()
	if sub == nil {
		return 0
	}
	n := sub.srv.Pool().SetOrder(uids)
	if n > 0 {
		traeDisplayOrderSave(uids)
	}
	return n
}

// traeDisplayOrderSave 把展示顺序落 proxy-config.json（与 trae2apiBindings
// 同文件同锁），并同步内存副本。
func traeDisplayOrderSave(uids []string) {
	traeOrderMu.Lock()
	traeDisplayOrder = append([]string(nil), uids...)
	traeOrderMu.Unlock()
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.Trae2apiDisplayOrder = append([]string(nil), uids...)
	})
}

// trae2apiApplyDisplayOrder 把已保存的展示顺序套到给定池。sub 由调用方显式
// 传入——启动路径（trae2apiStart）持 trae2apiMu 期间调用，不能经
// trae2apiCurrent() 取（锁重入自死锁；同一坑第二次，2026-11-05 部署实测）。
// 未提及/未知 uid 排尾按 UID（SetOrder 语义）。
func trae2apiApplyDisplayOrder(sub *trae2apiSubsystem) {
	if sub == nil {
		return
	}
	traeOrderMu.Lock()
	order := append([]string(nil), traeDisplayOrder...)
	traeOrderMu.Unlock()
	if len(order) > 0 {
		sub.srv.Pool().SetOrder(order)
	}
}

// traeDisplayOrderForBackup 导出用副本（admin_backup.go TraeDisplayOrder 段）。
func traeDisplayOrderForBackup() []string {
	traeOrderMu.Lock()
	defer traeOrderMu.Unlock()
	if len(traeDisplayOrder) == 0 {
		return nil
	}
	return append([]string(nil), traeDisplayOrder...)
}

// importTrae2apiDisplayOrder 备份导入：落 proxy-config 段 + 同步内存副本 +
// 套到当前池（子系统未启动时仅落盘，启动装载会再套）。与绑定段同"整体替换"
// 语义；返回写入的 UID 数。
func importTrae2apiDisplayOrder(uids []string) int {
	traeOrderMu.Lock()
	traeDisplayOrder = append([]string(nil), uids...)
	traeOrderMu.Unlock()
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.Trae2apiDisplayOrder = append([]string(nil), uids...)
	})
	trae2apiApplyDisplayOrder(trae2apiCurrent())
	return len(uids)
}

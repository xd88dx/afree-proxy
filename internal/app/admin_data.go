package app

import (
	"log"
	"net/http"
	"os"

	"afree-proxy/internal/kit"
)

// POST /admin/api/data/delete-all
// 删除全部持久化数据：Cline 账号与客户端 key、默认模型、请求头、调度策略、
// OpenCode key/绑定/代理池、OpenRouter key/绑定、zen 粘性会话与端点学习结果、
// WorkBuddy 账号与代理绑定、自定义别名、cline 流式自学习结果、请求日志与用量统计。
// 仅保留运行基础设施（.session-secret、运行日志），保证面板会话不被踢出。
func handleAdminDeleteAllData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}

	clineAccounts, clientKeys := clearPoolForDataDelete()
	zenKeys := clearOpenCodeKeysForDataDelete()
	orKeys := clearOpenrouterKeysForDataDelete()
	amdKeys := clearAMDKeysForDataDelete()
	thKeys := clearTokenHarborKeysForDataDelete()
	sessions := clearZenSessionsForDataDelete()
	endpoints := clearZenEndpointsForDataDelete()
	workbuddyAccounts := clearWorkbuddyAccountsForDataDelete()
	proxies := clearProxyPoolForDataDelete()
	combos := clearCombosForDataDelete()
	streamLearned := clearClineStreamLearnedForDataDelete()
	clearRequestLogsForDataDelete()

	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "All persisted data deleted",
		Data: map[string]any{
		"clineAccounts":     clineAccounts,
		"clientKeys":        clientKeys,
		"opencodeKeys":      zenKeys,
		"openrouterKeys":    orKeys,
		"amdKeys":           amdKeys,
		"tokenharborKeys":   thKeys,
		"sessions":          sessions,
			"endpoints":         endpoints,
			"workbuddyAccounts": workbuddyAccounts,
			"proxies":           proxies,
			"combos":            combos,
			"streamLearned":     streamLearned,
		},
	})
}

// clearPoolForDataDelete 清空账号池文件并重置其中的全局配置段：客户端 key、
// 默认模型、cline 走代理池开关、调度策略与请求头全部回到出厂值。内存中的
// 调度配置与默认模型一并复位，避免清完盘上数据后被内存旧值回写。
func clearPoolForDataDelete() (accounts, clientKeys int) {
	old := loadPool()
	poolMu.Lock()
	accounts = len(old.Accounts)
	clientKeys = len(old.Keys)
	pool = &AccountPool{
		Accounts:        []*Account{},
		CurrentIdx:      0,
		Keys:            []string{},
		DefaultModel:    "",
		ClineUseProxies: nil, // 重置为默认（开启）
		AdminLogEnabled: nil, // 重置为默认（不记录 /admin 访问日志）
		Strategy:        "round_robin",
		Headers:         defaultProxyConfig().Headers,
	}
	poolDirty = false
	poolMu.Unlock()
	savePool()

	setProxyConfig(defaultProxyConfig())
	defaultModel = ""
	// 旧账号的刷新单飞句柄一并作废
	refreshFlightMu.Lock()
	refreshFlight = map[*Account]*refreshFlightCall{}
	refreshFlightMu.Unlock()
	return accounts, clientKeys
}

func clearOpenCodeKeysForDataDelete() int {
	cfg := getZenConfig()
	n := len(cfg.Keys)
	next := *cfg
	// normalizeZenKeys 会把空列表规范回匿名 public key，保留其余 OpenCode
	// 配置（baseURL、压缩和代理策略）。
	next.Key = ""
	next.Keys = nil
	next.KeyBindings = nil
	setZenConfig(&next)
	return n
}

// clearZenSessionsForDataDelete 清空 zen 粘性会话（内存 + 落盘文件）。
func clearZenSessionsForDataDelete() int {
	loadZenSessions()
	zenSessMu.Lock()
	n := len(zenSessions)
	zenSessions = map[string]*zenSessionEntry{}
	zenSessLoaded = true
	saveZenSessionsLocked()
	zenSessMu.Unlock()
	return n
}

// clearZenEndpointsForDataDelete 删除端点学习结果文件，并把内存模型表中被
// 学习改写的 Upstream 回退到种子目录的原值（种子目录本身就是 responses 的
// 模型不受影响）。
func clearZenEndpointsForDataDelete() int {
	learned := loadZenEndpointsFile()
	if err := os.Remove(zenEndpointFile()); err != nil && !os.IsNotExist(err) {
		log.Printf("data delete: remove endpoints file failed: %v", err)
	}
	if len(learned) == 0 {
		return 0
	}
	seedUpstream := map[string]string{}
	for _, m := range zenSeedModels {
		seedUpstream[m.ID] = m.Upstream
	}
	initZenModels()
	zenModelsMu.Lock()
	n := 0
	for id, up := range learned {
		m, ok := zenModels[id]
		if !ok || m == nil || up != "responses" || m.Upstream != "responses" {
			continue
		}
		base := seedUpstream[id]
		if base == "responses" {
			continue
		}
		next := *m
		next.Upstream = base
		zenModels[id] = &next
		n++
	}
	zenModelsMu.Unlock()
	return n
}

func clearWorkbuddyAccountsForDataDelete() int {
	if workbuddySub == nil || workbuddySub.pool == nil {
		return 0
	}
	p := workbuddySub.pool
	statuses := p.List()
	if len(statuses) == 0 {
		return 0
	}
	auths := make([]struct {
		filePath string
	}, 0, len(statuses))
	for _, st := range statuses {
		if a := p.AuthByUID(st.UID); a != nil {
			auths = append(auths, struct{ filePath string }{filePath: a.FilePath})
		}
	}
	p.SyncToDir(nil)
	for _, a := range auths {
		if a.filePath == "" {
			continue
		}
		if err := os.Remove(a.filePath); err != nil && !os.IsNotExist(err) {
			log.Printf("workbuddy data delete: remove %s failed: %v", a.filePath, err)
		}
	}
	clearAllWorkbuddyProxyBindings()
	return len(statuses)
}

// clearProxyPoolForDataDelete 把 OpenCode 配置整体重置为出厂值（key、绑定、
// 代理池与别名全部清空），并清掉 cline 账号侧可能残留的代理绑定引用。
func clearProxyPoolForDataDelete() int {
	cfg := getZenConfig()
	n := len(cfg.Proxies)
	setZenConfig(defaultZenConfig())
	clearAllAccountProxies()
	clearAllZenKeyProxies()
	return n
}

// clearCombosForDataDelete 清空全部自定义别名（内存 + combos.json）。
func clearCombosForDataDelete() int {
	combosMu.Lock()
	defer combosMu.Unlock()
	loadCombosLocked()
	n := len(combosList)
	combosList = nil
	combosLoaded = true
	saveCombosLocked()
	return n
}

// clearClineStreamLearnedForDataDelete 清空 cline "必须流式"自学习结果
// （内存表 + data/.cline-stream-required.json）。
func clearClineStreamLearnedForDataDelete() int {
	clineStreamMu.Lock()
	n := len(clineStreamLearned)
	clineStreamLearned = nil
	clineStreamMu.Unlock()
	path := kit.ResolveDataPath(".cline-stream-required.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("data delete: remove %s failed: %v", path, err)
	}
	return n
}

// clearRequestLogsForDataDelete 清空请求日志与用量统计（内存 + 落盘文件）。
// 两个文件都是按次追加打开的，直接截断即可，无需处理打开句柄。
func clearRequestLogsForDataDelete() {
	reqLogsMu.Lock()
	reqLogs = nil
	reqLogsMu.Unlock()
	path := reqLogPath()
	if err := os.WriteFile(path, nil, 0600); err != nil {
		log.Printf("data delete: truncate %s failed: %v", path, err)
	}
	statsFile := kit.ResolveDataPath("zen-stats.jsonl")
	if err := os.WriteFile(statsFile, nil, 0600); err != nil {
		log.Printf("data delete: truncate %s failed: %v", statsFile, err)
	}
}

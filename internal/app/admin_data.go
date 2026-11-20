package app

import (
	"afree-proxy/internal/gateway"
	"afree-proxy/internal/reqlog"

	"afree-proxy/internal/cline"
	"afree-proxy/internal/zen"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// POST /admin/api/data/delete-all
// 删除全部持久化数据：Cline 账号与客户端 key、默认模型、请求头、调度策略、
// OpenCode key/绑定/代理池、zen 粘性会话与端点学习结果、
// WorkBuddy 账号与代理绑定、自定义别名、cline 流式自学习结果、请求日志与用量统计。
// 仅保留运行基础设施（.Session-secret、运行日志），保证面板会话不被踢出。
func handleAdminDeleteAllData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}

	clineAccounts, clientKeys := clearPoolForDataDelete()
	zenKeys := clearOpenCodeKeysForDataDelete()
	otherKeys := clearOtherKeysForDataDelete()
	freepiKeys := clearFreepiKeysForDataDelete()
	traeAccounts := clearTrae2apiAccountsForDataDelete()
	sessions := clearZenSessionsForDataDelete()
	endpoints := clearZenEndpointsForDataDelete()
	workbuddyAccounts := clearWorkbuddyAccountsForDataDelete()
	proxies := clearProxyPoolForDataDelete()
	combos := clearCombosForDataDelete()
	streamLearned := clearClineStreamLearnedForDataDelete()
	clearRequestLogsForDataDelete()

	WriteAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "All persisted data deleted",
		Data: map[string]any{
			"clineAccounts":     clineAccounts,
			"clientKeys":        clientKeys,
			"opencodeKeys":      zenKeys,
			"otherKeys":         otherKeys,
			"freepiKeys":        freepiKeys,
			"traeAccounts":      traeAccounts,
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
	accounts = cline.ResetPoolForDataDelete(defaultProxyConfig().Headers)
	clientKeys = len(globalClientKeys())

	setProxyConfig(defaultProxyConfig())
	setGlobalClientKeys(nil)
	setGlobalAdminLogEnabled(false)
	setGlobalStrategy("round_robin")
	// 旧账号的刷新单飞句柄一并作废
	cline.ResetRefreshFlight()
	return accounts, clientKeys
}

func clearOpenCodeKeysForDataDelete() int {
	cfg := getZenConfig()
	n := len(cfg.Keys)
	next := *cfg
	// 清空 key 池即清空 zen 凭据：保留其余 OpenCode 配置（baseURL、压缩和
	// 代理策略），平台转为不可路由（目录隐藏、请求明确报错）。
	// 注意：整个"清空全部数据"流程随后还会跑 clearProxyPoolForDataDelete，那里把
	// zen 配置整体重置为出厂值——最终连 baseURL/压缩设置一并复位，与面板文案
	// 「Deletes ALL persisted data (... config)」一致；上面的"保留"语义只在
	// 单独调用本函数时成立。
	next.Keys = nil
	// key 池已清空，绑定表随之失效（绑定按 key 明文存，留着就是垃圾配置）。
	next.KeyBindings = nil
	setZenConfig(&next)
	// 冷却/记账痕迹一并清空（痕迹文件即时落盘）。
	zen.ClearZenUsage()
	return n
}

// clearZenSessionsForDataDelete 清空 zen 粘性会话（内存 + 落盘文件）。
func clearZenSessionsForDataDelete() int {
	return zen.ClearSessionsForDataDelete()
}

// clearZenEndpointsForDataDelete 删除端点学习结果文件，并把内存模型表中
// 学习器写下的 responses 定论回退到默认 chat（Upstream 清空）。目录 npm
// 播种的 responses（Source!="learned"）是目录固有决策，不回退——取代旧版
// 的"种子目录本身就是 responses 的模型不受影响"基线（种子快照已于
// 2026-11-11 整机制删除）。
func clearZenEndpointsForDataDelete() int {
	return zen.ClearEndpointsForDataDelete()
}

func clearWorkbuddyAccountsForDataDelete() int {
	if *workbuddySub == nil || (*workbuddySub).Pool == nil {
		return 0
	}
	p := (*workbuddySub).Pool
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
	n := len(getProxyPool().Proxies)
	setProxyPool(defaultProxyPoolConfig())
	setZenConfig(defaultZenConfig())
	clearAllAccountProxies()
	clearAllKeyProxies()
	// 出口健康统计（独立 proxy-state.json）随代理池一并清空：列表已换，
	// 旧出口的成败样本没有保留价值（与编辑池即清零同一决策口径）。
	resetProxyHealthStats()
	return n
}

// clearCombosForDataDelete 清空全部自定义别名（内存 + combos.json）。
func clearCombosForDataDelete() int {
	return gateway.ClearCombosForDataDelete()
}

// clearClineStreamLearnedForDataDelete 清空 cline "必须流式"自学习结果
// （内存表 + cline-config.json 的 streamRequired 段，2026-11-17 迁入）。
func clearClineStreamLearnedForDataDelete() int {
	return cline.ClearStreamLearned()
}

// clearRequestLogsForDataDelete 清空请求日志与用量统计（内存 + 落盘文件）。
// 两个文件都是按次追加打开的，直接截断即可，无需处理打开句柄。
func clearRequestLogsForDataDelete() {
	reqlog.ClearForDataDelete()
}

// clearTrae2apiAccountsForDataDelete 清空 TraeWork 账号：删除全部快照文件 +
// 清空路由参与表与出口绑定，然后重启 vendored 子系统（渠道按空账号重建）。
func clearTrae2apiAccountsForDataDelete() int {
	accountsDir := filepath.Join(traeDataDir(), "accounts")
	entries, _ := os.ReadDir(accountsDir)
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(accountsDir, e.Name())); err != nil && !os.IsNotExist(err) {
			log.Printf("trae2api data delete: remove %s failed: %v", e.Name(), err)
			continue
		}
		n++
	}
	traeBindingsMu.Lock()
	loadTraeBindingsLocked()
	if len(*traeBindings) > 0 {
		*traeBindings = map[string]trae2apiProxyBinding{}
		saveTraeBindingsLocked()
	}
	traeBindingsMu.Unlock()
	traeRoutingMu.Lock()
	loadTraeRoutingLocked()
	if len(*traeRouting) > 0 {
		*traeRouting = map[string]bool{}
		saveTraeRoutingLocked()
	}
	traeRoutingMu.Unlock()
	if n > 0 {
		_ = trae2apiRestart()
	}
	return n
}

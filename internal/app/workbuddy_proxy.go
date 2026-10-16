package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"afree-proxy/internal/kit"
	wbauth "afree-proxy/internal/workbuddy/auth"
)

// workbuddyProxyBinding 是 WorkBuddy 账号的出口绑定。值语义与现有 Cline/zen
// 绑定一致：主优先、辅兜底；两者都不可用 = 该账号本次请求直接失败（隔离优先），
// 绝不退回直连或其他出口。空绑定 = 尚未分配；代理池为空时等价于直连。
type workbuddyProxyBinding struct {
	Main   string `json:"main,omitempty"`
	Backup string `json:"backup,omitempty"`
}

var (
	wbProxyMu       sync.Mutex
	wbProxyLoaded   bool
	wbProxyBindings = map[string]workbuddyProxyBinding{}
)

func workbuddyProxyPath() string {
	return kit.ResolveDataPath(".workbuddy-proxies.json")
}

func loadWorkbuddyProxyBindingsLocked() {
	if wbProxyLoaded {
		return
	}
	wbProxyLoaded = true
	raw, err := os.ReadFile(workbuddyProxyPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("workbuddy proxy bindings: read %s: %v", workbuddyProxyPath(), err)
		}
		return
	}
	var bindings map[string]workbuddyProxyBinding
	if err := json.Unmarshal(raw, &bindings); err != nil {
		log.Printf("workbuddy proxy bindings: corrupt JSON at %s: %v (keeping empty bindings; file not overwritten until next assignment)", workbuddyProxyPath(), err)
		return
	}
	if bindings != nil {
		wbProxyBindings = bindings
	}
}

func saveWorkbuddyProxyBindingsLocked() {
	raw, err := json.MarshalIndent(wbProxyBindings, "", "  ")
	if err != nil {
		log.Printf("workbuddy proxy bindings: marshal: %v", err)
		return
	}
	path := workbuddyProxyPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		log.Printf("workbuddy proxy bindings: write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("workbuddy proxy bindings: replace %s: %v", path, err)
		_ = os.Remove(tmp)
	}
}

// workbuddyProxyFor 是 upstream.ProxyFor 的实现。语义与 Cline/OpenCode 一致：
//   - 隔离关闭：忽略绑定，未绑定出口按全局走池开关轮转（开关关 = 直连）。
//   - 隔离开启且账号有绑定：主代理优先、辅代理兜底，双不可用 fail-closed。
//   - 隔离开启且账号未绑定：沿用全局走池开关；显式绑定 "direct" 才强制直连。
func workbuddyProxyFor(a *wbauth.Auth) (string, bool) {
	if a == nil {
		return "", true
	}
	if !proxyIsolationEnabled() {
		return workbuddyGlobalExit()
	}
	uid := strings.TrimSpace(a.UID)
	if uid == "" {
		return "", true
	}

	wbProxyMu.Lock()
	loadWorkbuddyProxyBindingsLocked()
	b, bound := wbProxyBindings[uid]
	wbProxyMu.Unlock()

	// 未绑定 = 全局策略，不是永久直连；代理池为空时 pickUpstreamProxy 返回直连。
	if !bound || (b.Main == "" && b.Backup == "") {
		return workbuddyGlobalExit()
	}
	url, _, usable := pickBoundProxy(b.Main, b.Backup)
		return url, usable
	}

	// workbuddyObserveAccount 是 upstream.ObserveAccount 的实现：vendor 选号
	// 之后把命中的账号与本次实际出口回报，网关侧据此填请求日志的「账号/Key」
	// 与「代理」两列（此前只有 Cline 账号与 zen key 有这两列，WorkBuddy 恒空）。
	// ctx 取自发起请求的 *http.Request：网关 /v1 请求带着日志槽位，面板请求与
	// 定时任务的 ctx 没有槽位，setUpstreamInfo/setUpstreamExit 内部即 no-op。
	func workbuddyObserveAccount(ctx context.Context, a *wbauth.Auth, exitRaw string) {
		if ctx == nil || a == nil {
			return
		}
		uid := strings.TrimSpace(a.UID)
		if uid == "" {
			return
		}
		setUpstreamInfo(ctx, workbuddyUpstreamLabel(uid, strings.TrimSpace(a.Nickname)))
		setUpstreamExit(ctx, workbuddyExitType(uid, exitRaw))
	}

	// workbuddyUpstreamLabel 日志「账号/Key」列的账号标识：与面板 # 列同口径的
	// 展示行号 —— wb#N（N = 当前 List 展示顺序中的行号，拖拽重排后按新顺序
	// 重新编号），无昵称时附 uid#短 UID 区分。
	func workbuddyUpstreamLabel(uid, nick string) string {
		uid = strings.TrimSpace(uid)
		n := 0
		if workbuddySub != nil && workbuddySub.pool != nil {
			n = workbuddySub.pool.IndexOf(uid)
		}
		label := fmt.Sprintf("wb#%d", n)
		short := uid
		if len(short) > 10 {
			short = short[:10] + "…"
		}
		if nick != "" && nick != uid {
			if len(nick) > 24 {
				nick = nick[:24] + "…"
			}
			label += " " + nick
		} else {
			label += " uid#" + short
		}
		return label
	}

	// workbuddyExitType 与 workbuddyProxyFor 同口径判定出口类型：隔离开启且
	// 账号有绑定时按绑定归类 main/backup/direct，否则走全局池轮转（返回空串，
	// 面板显示 -）。fail-closed（绑定出口全不可用、未出网）按直连记 direct。
	func workbuddyExitType(uid, exitRaw string) string {
		if !proxyIsolationEnabled() {
			return proxyExitType(exitRaw, "", "", false)
		}
		wbProxyMu.Lock()
		loadWorkbuddyProxyBindingsLocked()
		b, bound := wbProxyBindings[uid]
		wbProxyMu.Unlock()
		if !bound || (b.Main == "" && b.Backup == "") {
			return proxyExitType(exitRaw, "", "", false)
		}
		return proxyExitType(exitRaw, b.Main, b.Backup, true)
	}

// workbuddyGlobalExit 未绑定身份的全局出口：面板开关选直连时即使代理列表
// 非空也不走池（与 OpenCode 的 zenAttemptExit 同语义）。
func workbuddyGlobalExit() (string, bool) {
	if !workbuddyProxiesEnabled() {
		return "", true
	}
	url, _ := pickUpstreamProxy()
	return url, true
}

// workbuddyProxyBindingsSnapshot 返回代理池与当前绑定，供管理 API 排障。
func workbuddyProxyBindingsSnapshot() ([]string, map[string]workbuddyProxyBinding) {
	cfg := getZenConfig()
	proxies := append([]string(nil), cfg.Proxies...)
	wbProxyMu.Lock()
	defer wbProxyMu.Unlock()
	loadWorkbuddyProxyBindingsLocked()
	out := make(map[string]workbuddyProxyBinding, len(wbProxyBindings))
	for uid, b := range wbProxyBindings {
		out[uid] = b
	}
	return proxies, out
}

func setWorkbuddyProxyBinding(uid, main, backup string) error {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return fmt.Errorf("uid is required")
	}
	main = strings.TrimSpace(main)
	backup = strings.TrimSpace(backup)
	wbProxyMu.Lock()
	defer wbProxyMu.Unlock()
	loadWorkbuddyProxyBindingsLocked()
	backup = resolveBackupSlot(backup, wbProxyBindings[uid].Backup)
	if err := validateProxyBinding(main, backup); err != nil {
		return err
	}
	if main == "" && backup == "" {
		delete(wbProxyBindings, uid)
	} else {
		wbProxyBindings[uid] = workbuddyProxyBinding{Main: main, Backup: backup}
	}
	saveWorkbuddyProxyBindingsLocked()
	return nil
}

func clearAllWorkbuddyProxyBindings() int {
	wbProxyMu.Lock()
	defer wbProxyMu.Unlock()
	loadWorkbuddyProxyBindingsLocked()
	n := len(wbProxyBindings)
	wbProxyBindings = map[string]workbuddyProxyBinding{}
	saveWorkbuddyProxyBindingsLocked()
	return n
}

// handleWorkbuddyProxyList GET /admin/api/workbuddy/proxy
func handleWorkbuddyProxyList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	proxies, bindings := workbuddyProxyBindingsSnapshot()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"proxies":  proxies,
		"bindings": bindings,
	}})
}

// handleWorkbuddyProxySet POST /admin/api/workbuddy/proxy {uid, main, backup}
func handleWorkbuddyProxySet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		UID    string `json:"uid"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid json: " + err.Error()})
		return
	}
	if err := setWorkbuddyProxyBinding(req.UID, req.Main, req.Backup); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleWorkbuddyProxyClear POST /admin/api/workbuddy/proxy/clear
func handleWorkbuddyProxyClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	n := clearAllWorkbuddyProxyBindings()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"cleared": n}})
}

// handleWorkbuddyPoolSetEnabled POST /admin/api/workbuddy/enabled  body: { uid, enabled }
// 保存账号的"启用"开关（是否进入账号池参与选号）。开关落在 auth 凭证文件
// （pool_enabled 键）并原子写回，重启不丢；不改冷却/熔断状态机，也不影响
// 签到/保活等账号维护任务。
func handleWorkbuddyPoolSetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddySub == nil || workbuddySub.pool == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	var req struct {
		UID     string `json:"uid"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid json: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.UID) == "" || req.Enabled == nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "uid and enabled are required"})
		return
	}
	a := workbuddySub.pool.AuthByUID(strings.TrimSpace(req.UID))
	if a == nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown uid: " + req.UID})
		return
	}
	a.SetPoolEnabled(*req.Enabled)
	if err := a.SaveAtomic(); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"uid": req.UID, "enabled": *req.Enabled}})
}

// handleWorkbuddyPoolReorder POST /admin/api/workbuddy/reorder  body: { uids: [uid,...] }
// 管理面板拖拽排序：保存账号显示顺序（池内 order 字段持久化，state.json 落盘）。
// **仅影响面板/状态列表的展示顺序**——WorkBuddy 选号是加权路由（积分/到期/闲置），
// 与列表顺序无关（用户确认过的语义）。
func handleWorkbuddyPoolReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddySub == nil || workbuddySub.pool == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		UIDs []string `json:"uids"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if len(req.UIDs) == 0 {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "uids is required"})
		return
	}
	n := workbuddySub.pool.SetOrder(req.UIDs)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"ordered": n}})
}

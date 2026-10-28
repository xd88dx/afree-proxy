package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"afree-proxy/internal/kit"
	wbauth "afree-proxy/internal/workbuddy/auth"
)

// workbuddyProxyBinding 是 WorkBuddy 账号的出口绑定。值语义与现有 Cline/zen
// 绑定一致：主优先、备兜底；两者都不可用 = 该账号本次请求直接失败（隔离优先），
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

// loadWorkbuddyProxyBindingsLocked 从 proxy-config.json 的 workbuddyBindings
// 段载入绑定。调用方必须持有 wbProxyMu。
func loadWorkbuddyProxyBindingsLocked() {
	if wbProxyLoaded {
		return
	}
	wbProxyLoaded = true
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	bindings := f.WorkbuddyBindings
	proxyFileMu.Unlock()
	if bindings != nil {
		wbProxyBindings = bindings
	}
}

// saveWorkbuddyProxyBindingsLocked 只覆盖 proxy-config.json 的
// workbuddyBindings 段。调用方必须持有 wbProxyMu。
func saveWorkbuddyProxyBindingsLocked() {
	cp := make(map[string]workbuddyProxyBinding, len(wbProxyBindings))
	for uid, b := range wbProxyBindings {
		cp[uid] = b
	}
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.WorkbuddyBindings = cp
	})
}

// workbuddyProxyFor 是 upstream.ProxyFor 的实现。语义与 Cline/OpenCode 一致：
//   - 隔离关闭：忽略绑定，未绑定出口按全局走池开关轮转（开关关 = 直连）。
//   - 隔离开启且账号有绑定：主代理优先、备用代理兜底，双不可用 fail-closed。
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
	setUpstreamInfo(ctx, workbuddyUpstreamLabel(uid))
	setUpstreamExit(ctx, workbuddyExitType(uid, exitRaw))
}

// workbuddyUpstreamLabel 日志「账号/Key」列的账号标识：与面板 # 列同口径的
// 展示行号 —— account #N（N = 当前 List 展示顺序中的行号，拖拽重排后按新顺序
// 重新编号；英文源文，面板渲染时经 T('account #') 词条中文化）。昵称不参与
// （面板账号列已展示，日志只留序号方便排查）。
func workbuddyUpstreamLabel(uid string) string {
	uid = strings.TrimSpace(uid)
	n := 0
	if workbuddySub != nil && workbuddySub.pool != nil {
		n = workbuddySub.pool.IndexOf(uid)
	}
	return fmt.Sprintf("account #%d", n)
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
// 非空也不走池（与 OpenCode 的 boundExitFor 同语义）。
func workbuddyGlobalExit() (string, bool) {
	if !workbuddyProxiesEnabled() {
		if kit.DisableDirect() {
			return "", false // 直连被禁：凭据失效（2026-10-28）
		}
		return "", true
	}
	url, _ := pickUpstreamProxy()
	if url == "" && kit.DisableDirect() {
		return "", false // 代理池空兜底的直连同样不放行
	}
	return url, true
}

// workbuddyProxyBindingsSnapshot 返回代理池与当前绑定，供管理 API 排障。
func workbuddyProxyBindingsSnapshot() ([]string, map[string]workbuddyProxyBinding) {
	cfg := getProxyPool()
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
// 保存账号的"反代"开关（是否进入账号池参与会话选号）。开关落在 auth 凭证文件
// （pool_enabled 键）并原子写回，重启不丢；不改冷却/熔断状态机，也不影响
// 签到/保活等账号维护任务。互锁：账号总开关关闭时禁止单独开启反代——
// 不存在的账号没有"参与路由"可言。
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
	if *req.Enabled && !a.AccountEnabled() {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "account switch is off: enable the account first, then the gateway switch"})
		return
	}
	a.SetPoolEnabled(*req.Enabled)
	if err := a.SaveAtomic(); err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"uid": req.UID, "enabled": *req.Enabled}})
}

// handleWorkbuddyAccountSetEnabled POST /admin/api/workbuddy/account-enabled
// body: { uid, enabled }
// 保存账号的"账号"总开关（afree 本地功能）：false = 该账号视为不存在——不参与
// 会话选号，签到/旅行/活跃/保活/夜猫子/连登/余额刷新等全部维护任务也跳过
// （vendor scheduler/pool 按 Status.AccountEnabled 统一跳过）。开关落在 auth
// 凭证文件（account_enabled 键）并原子写回，重启不丢；缺键 = 启用（存量账号
// 不受影响）。反代开关（pool_enabled）不在此联动改写，两开关持久化值独立。
func handleWorkbuddyAccountSetEnabled(w http.ResponseWriter, r *http.Request) {
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
	a.SetAccountEnabled(*req.Enabled)
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

// handleWorkbuddyNickname POST /admin/api/workbuddy/nickname  body: { uid, nickname }
// 设置 WorkBuddy 账号的显示昵称（面板账号列）。仅影响展示与日志标签，不参与
// 选号路由。语义（2026-10-24 用户定）：
//   - nickname 非空：设为本地昵称并加锁（auth 文件 nickname_locked=true），
//     之后面板手动「全量刷新余额」的上游昵称同步（panel syncNicknames）跳过
//     该号——手改值不被覆盖。
//   - nickname 空：解锁并从上游拉当前昵称恢复（FetchAccountProfile），回到
//     "跟随上游"状态；上游拉取失败则整个请求失败，不做半更新。
func handleWorkbuddyNickname(w http.ResponseWriter, r *http.Request) {
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
		UID      string `json:"uid"`
		Nickname string `json:"nickname"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if req.UID == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "uid is required"})
		return
	}
	a := workbuddySub.pool.AuthByUID(req.UID)
	if a == nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown uid: " + req.UID})
		return
	}
	nick := strings.TrimSpace(req.Nickname)
	if nick != "" && len(nick) > 64 {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "nickname too long (max 64)"})
		return
	}
	if nick == "" {
		// 清空 = 解锁 + 恢复上游昵称。拉取失败整体失败：宁可让用户重试，
		// 也不留下"锁没了、昵称却是旧手改值"的中间态。
		if workbuddySub.upstream == nil {
			writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy upstream unavailable"})
			return
		}
		upstreamNick, ferr := workbuddySub.upstream.FetchAccountProfile(a)
		if ferr != nil {
			writeAPI(w, http.StatusBadGateway, apiResponse{Error: "fetch upstream nickname failed: " + ferr.Error()})
			return
		}
		if strings.TrimSpace(upstreamNick) == "" {
			writeAPI(w, http.StatusBadGateway, apiResponse{Error: "upstream returned empty nickname; keep your local name or retry later"})
			return
		}
		if serr := a.SetLocalNickname(upstreamNick, false); serr != nil {
			writeAPI(w, http.StatusInternalServerError, apiResponse{Error: "nickname save failed: " + serr.Error()})
			return
		}
		writeAPI(w, http.StatusOK, apiResponse{
			Success: true,
			Message: "Nickname restored from upstream",
			Data:    map[string]any{"uid": req.UID, "nickname": upstreamNick, "synced": true},
		})
		return
	}
	// 手改：设昵称 + 加锁，一次落盘。同值重复提交幂等（SetLocalNickname 无变化
	// 也会重写文件，但内容一致无害；锁状态反正要落盘）。
	if serr := a.SetLocalNickname(nick, true); serr != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: "nickname save failed: " + serr.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Nickname saved",
		Data:    map[string]any{"uid": req.UID, "nickname": nick, "locked": true},
	})
}

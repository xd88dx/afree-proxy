package app

// wb_handlers.go —— WorkBuddy 面板 handler、网关分发壳与 anthropic/responses
// 方言适配（2026-11-18 P3e 从 workbuddy*.go 拆回：panel 域响应封装、网关翻译
// 与 cron 基建属装配根职责；域逻辑在 internal/workbuddy，经 wb_bridge.go 的
// 小写别名消费。

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	wbbridge "github.com/misakano7545/workbuddy2api-panel"

	"afree-proxy/internal/workbuddy"
)

// AttachWorkbuddyLogSink 把标准日志与 WorkBuddy chat 表格日志同时镜像进面板
// 环形缓冲；控制台/文件输出保持原样。
func AttachWorkbuddyLogSink(sink io.Writer) {
	if sink == nil {
		return
	}
	writers := []io.Writer{os.Stderr}
	if proxyLogFile != nil {
		writers = append(writers, proxyLogFile)
	}
	writers = append(writers, sink)
	log.SetOutput(io.MultiWriter(writers...))
	wbbridge.ServerSetChatLogOutput(io.MultiWriter(os.Stdout, sink))
}

// Stop 优雅停止 WorkBuddy 子系统：先落盘池状态，再停止调度/会话 GC/用量记录。

// WorkbuddyRewriteModel 把请求体 JSON 的 model 字段从网关前缀翻译回 vendor
// realm 前缀。解析失败/无 model 字段时原样返回（由 vendor 自己报错）。

// handleWorkbuddyProxyList GET /admin/api/workbuddy/proxy
func handleWorkbuddyProxyList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	proxies, bindings := workbuddy.WorkbuddyProxyBindingsSnapshot()
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"proxies":  proxies,
		"bindings": bindings,
	}})
}

// handleWorkbuddyProxySet POST /admin/api/workbuddy/proxy {uid, main, backup}
func handleWorkbuddyProxySet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		UID    string `json:"uid"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid json: " + err.Error()})
		return
	}
	if err := workbuddy.SetWorkbuddyProxyBinding(req.UID, req.Main, req.Backup); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleWorkbuddyProxyClear POST /admin/api/workbuddy/proxy/clear
func handleWorkbuddyProxyClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	n := workbuddy.ClearAllWorkbuddyProxyBindings()
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"cleared": n}})
}

// handleWorkbuddyPoolSetEnabled POST /admin/api/workbuddy/enabled  body: { uid, enabled }
// 保存账号的"反代"开关（是否进入账号池参与会话选号）。开关落在 auth 凭证文件
// （pool_enabled 键）并原子写回，重启不丢；不改冷却/熔断状态机，也不影响
// 签到/保活等账号维护任务。互锁：账号总开关关闭时禁止单独开启反代——
// 不存在的账号没有"参与路由"可言。
func handleWorkbuddyPoolSetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddy.WorkbuddySub == nil || (*workbuddy.WorkbuddySub).Pool == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	var req struct {
		UID     string `json:"uid"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid json: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.UID) == "" || req.Enabled == nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "uid and enabled are required"})
		return
	}
	a := (*workbuddy.WorkbuddySub).Pool.AuthByUID(strings.TrimSpace(req.UID))
	if a == nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown uid: " + req.UID})
		return
	}
	if *req.Enabled && !a.AccountEnabled() {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "account switch is off: enable the account first, then the gateway switch"})
		return
	}
	a.SetPoolEnabled(*req.Enabled)
	if err := a.SaveAtomic(); err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"uid": req.UID, "enabled": *req.Enabled}})
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
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddy.WorkbuddySub == nil || (*workbuddy.WorkbuddySub).Pool == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	var req struct {
		UID     string `json:"uid"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid json: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.UID) == "" || req.Enabled == nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "uid and enabled are required"})
		return
	}
	a := (*workbuddy.WorkbuddySub).Pool.AuthByUID(strings.TrimSpace(req.UID))
	if a == nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown uid: " + req.UID})
		return
	}
	a.SetAccountEnabled(*req.Enabled)
	if err := a.SaveAtomic(); err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"uid": req.UID, "enabled": *req.Enabled}})
}

// handleWorkbuddyPoolReorder POST /admin/api/workbuddy/reorder  body: { uids: [uid,...] }
// 管理面板拖拽排序：保存账号显示顺序（池内 order 字段持久化，state.json 落盘）。
// **仅影响面板/状态列表的展示顺序**——WorkBuddy 选号是加权路由（积分/到期/闲置），
// 与列表顺序无关（用户确认过的语义）。
func handleWorkbuddyPoolReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddy.WorkbuddySub == nil || (*workbuddy.WorkbuddySub).Pool == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		UIDs []string `json:"uids"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if len(req.UIDs) == 0 {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "uids is required"})
		return
	}
	n := (*workbuddy.WorkbuddySub).Pool.SetOrder(req.UIDs)
	// 顺序意志唯一持久层在 wkbd-config.json displayOrder（2026-11-21 外置）：
	// 进配置同步面，重启由 StartWorkBuddy 回放。上面的 Pool.SetOrder 是 vendor
	// 原行为（state.json 落盘，本机即时生效），保留。
	workbuddy.WkbdSetDisplayOrder(req.UIDs)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"ordered": n}})
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
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddy.WorkbuddySub == nil || (*workbuddy.WorkbuddySub).Pool == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		UID      string `json:"uid"`
		Nickname string `json:"nickname"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if req.UID == "" {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "uid is required"})
		return
	}
	a := (*workbuddy.WorkbuddySub).Pool.AuthByUID(req.UID)
	if a == nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown uid: " + req.UID})
		return
	}
	nick := strings.TrimSpace(req.Nickname)
	if nick != "" && len(nick) > 64 {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "nickname too long (max 64)"})
		return
	}
	if nick == "" {
		// 清空 = 解锁 + 恢复上游昵称。拉取失败整体失败：宁可让用户重试，
		// 也不留下"锁没了、昵称却是旧手改值"的中间态。
		if (*workbuddy.WorkbuddySub).Upstream == nil {
			WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy upstream unavailable"})
			return
		}
		upstreamNick, ferr := (*workbuddy.WorkbuddySub).Upstream.FetchAccountProfile(a)
		if ferr != nil {
			WriteAPI(w, http.StatusBadGateway, apiResponse{Error: "fetch upstream nickname failed: " + ferr.Error()})
			return
		}
		if strings.TrimSpace(upstreamNick) == "" {
			WriteAPI(w, http.StatusBadGateway, apiResponse{Error: "upstream returned empty nickname; keep your local name or retry later"})
			return
		}
		if serr := a.SetLocalNickname(upstreamNick, false); serr != nil {
			WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: "nickname save failed: " + serr.Error()})
			return
		}
		WriteAPI(w, http.StatusOK, apiResponse{
			Success: true,
			Message: "Nickname restored from upstream",
			Data:    map[string]any{"uid": req.UID, "nickname": upstreamNick, "synced": true},
		})
		return
	}
	// 手改：设昵称 + 加锁，一次落盘。同值重复提交幂等（SetLocalNickname 无变化
	// 也会重写文件，但内容一致无害；锁状态反正要落盘）。
	if serr := a.SetLocalNickname(nick, true); serr != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: "nickname save failed: " + serr.Error()})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Nickname saved",
		Data:    map[string]any{"uid": req.UID, "nickname": nick, "locked": true},
	})
}

// handleWorkbuddyShareOptimize GET：按当前池状态计算建议占比（只算不写）。
// 响应 {suggestions, warnings, priority_active, has_pressure}。
func handleWorkbuddyShareOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddy.WorkbuddySub == nil || (*workbuddy.WorkbuddySub).Pool == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	// 一次 List 同时供计算与 priority 检测（List 是快照，两次调用间可能变化）。
	list := (*workbuddy.WorkbuddySub).Pool.List()
	sugs, warnings := workbuddy.ComputeShareSuggestions(list, time.Now())
	var hasPressure bool
	for _, s := range sugs {
		if !s.Reserved {
			hasPressure = true
			break
		}
	}
	priorityActive := workbuddy.AnyPriorityActive(list)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"suggestions":     sugs,
		"warnings":        warnings,
		"priority_active": priorityActive,
		"has_pressure":    hasPressure,
	}})
}

// handleWorkbuddyShareSet POST：写入/清空 pool.account_share。请求体
// {shares: {uid: 百分比权重}} 或 {clear: true}（清空回落普通加权路由）。
// 落盘走 SaveWorkbuddyConfig 同一条链（深合并保留手工段 + 原子落盘 + 热生效）。
func handleWorkbuddyShareSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddy.WorkbuddySub == nil || (*workbuddy.WorkbuddySub).SaveConfig == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Shares map[string]float64 `json:"shares"`
		Clear  bool               `json:"clear"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	shares := req.Shares
	if req.Clear {
		shares = map[string]float64{}
	}
	if shares == nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "shares or clear is required"})
		return
	}
	// 校验：uid 必须在池内（防手滑打错号）；值必须为正（占比语义）。
	known := (*workbuddy.WorkbuddySub).Pool
	for uid, v := range shares {
		if known.AuthByUID(uid) == nil {
			WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown uid: " + uid})
			return
		}
		if !(v > 0) {
			WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "share value must be positive: " + uid})
			return
		}
	}
	raw, err := json.Marshal(map[string]any{"pool": map[string]any{"account_share": shares}})
	if err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	restart, err := (*workbuddy.WorkbuddySub).SaveConfig(raw)
	if err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	log.Printf("workbuddy: account_share 已更新（%d 个账号，clear=%v）", len(shares), req.Clear)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"done": true, "count": len(shares), "restart_required": restart,
	}})
}

// handleWorkbuddyShareGet 已移除（2026-11-15）：当前占比经 overview 的
// accounts[].share 透出（Status.Share），无独立查询端点的必要——避免死端点。

// ============================================================================
// 权重优化定时任务（2026-11-15）：配置住 wkbd-config.json shareOptimize 段
// （2026-11-17 迁出 vendor config 的 schedule.share_optimize_* 补丁键——afree
// 增量配置归平台文件，vendor 配置自管）。保存链 = 面板 wkbd 配置段写
// atomic 热值（改 cron 无需重启）；worker 照订阅自动更新
// （StartProxySubscriptionWorker）形态：30s tick + cronMatches
// 命中 + (last, nowMin] 区间防漏防重，命中即执行与手动按钮同一套计算+写入。
// ============================================================================

// handleWkbdShareOptimize GET/POST /admin/api/workbuddy/wkbd/share-optimize：
// 权重优化定时任务开关与 cron（2026-11-17 起住 wkbd-config.json shareOptimize
// 段——增量配置归平台文件，vendor 配置自管）。POST 热应用 atomic 热值，改
// cron 无需重启。GET 返回 {shareOptimize:{enabled,cron}}。
func handleWkbdShareOptimize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		so := workbuddy.WkbdConfigShareOptimize()
		out := map[string]any{"shareOptimize": so}
		if so == nil {
			out["shareOptimize"] = map[string]any{}
		}
		WriteAPI(w, http.StatusOK, apiResponse{Data: out})
	case http.MethodPost:
		var patch struct {
			ShareOptimize *workbuddy.WkbdShareOptimize `json:"shareOptimize"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&patch); err != nil || patch.ShareOptimize == nil {
			WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid body: shareOptimize required"})
			return
		}
		// 面板保存即校验（2026-11-19）：坏表达式不再静默落盘靠 worker 跳过，
		// 当场 400 让面板提示。空 cron 合法（= 不定时）。
		if expr := strings.TrimSpace(patch.ShareOptimize.Cron); expr != "" {
			if err := validateCronExpr(expr); err != nil {
				WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid cron: " + err.Error()})
				return
			}
		}
		workbuddy.MutateWkbdConfig(func(k *workbuddy.WkbdConfigData) { k.ShareOptimize = patch.ShareOptimize })
		if workbuddy.WorkbuddySub != nil {
			(*workbuddy.WorkbuddySub).ShareOptimizeSched.Store(&workbuddy.WbShareSched{
				Enabled: patch.ShareOptimize.Enabled,
				Cron:    patch.ShareOptimize.Cron,
			})
		}
		WriteAPI(w, http.StatusOK, apiResponse{Data: map[string]any{"saved": true}})
	default:
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
	}
}

// workbuddy.WbShareSched 权重优化调度热值快照（atomic.Value 持有）。

// StartWorkbuddyShareOptimizeLoop 启动调度 worker（进程级单实例，随 subsystem）。
func StartWorkbuddyShareOptimizeLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for now := range ticker.C {
			if workbuddy.WorkbuddySub == nil || (*workbuddy.WorkbuddySub).SaveConfig == nil {
				continue
			}
			sched, ok := (*workbuddy.WorkbuddySub).ShareOptimizeSched.Load().(*workbuddy.WbShareSched)
			if !ok || sched == nil || !sched.Enabled {
				continue
			}
			expr := strings.TrimSpace(sched.Cron)
			if expr == "" {
				continue
			}
			if validateCronExpr(expr) != nil {
				continue // 面板已校验；手改配置文件的坏表达式静默跳过
			}
			nowMin := int(now.Unix() / 60)
			workbuddy.WbShareOptMu.Lock()
			last := workbuddy.WbShareLastMin
			workbuddy.WbShareOptMu.Unlock()
			// 检查 (last, nowMin] 区间内是否有命中的整分钟（30s tick 防漏防重）
			fired := false
			for m := last + 1; m <= nowMin; m++ {
				if cronMatches(expr, time.Unix(int64(m)*60, 0).Local()) {
					fired = true
					break
				}
			}
			if !fired || last == nowMin {
				continue
			}
			workbuddy.WbShareOptMu.Lock()
			workbuddy.WbShareLastMin = nowMin
			workbuddy.WbShareOptMu.Unlock()
			workbuddy.RunWorkbuddyShareOptimize("cron " + expr)
		}
	}()
}

// workbuddy.RunWorkbuddyShareOptimize 执行一次权重优化：计算建议 → 与当前 pool 占比比对
// → 有变化才写入（幂等，避免重复落盘）。手动按钮与定时触发共用。

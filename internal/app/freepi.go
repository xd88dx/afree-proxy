package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/freepi"
	"afree-proxy/internal/gateway"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/panel"
)

// ============ FreePI（freepi.ai 免费日额度 key 池）============
//
// 单平台 key 池（凭据 = free-pi CLI 铸造的 JWT，90 天有效），装配与 handler
// 骨架与 internal/app/other.go 同构（无子平台维度）。上游三道闸门
// （ads_required/concurrent_session/concurrent）的自愈在 internal/freepi
// 包内，本层只做装配与面板编排。

func freepiConfigPath() string { return kit.ResolveDataPath("freepi-config.json") }

// freepiVersionRe x-client-version 覆盖值的形状闸：x.y.z（各段 1-3 位数字）。
// 上游最低版本闸按 semver 比较，非此形状的值没有意义；提交入口先拦（400），
// 不给坏指纹落盘的机会。
var freepiVersionRe = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}$`)

// initFreepi 加载配置 + 注入四钩子（client 工厂/目录出口/出口观测/请求日志）
// + 启动周期目录同步。StartProxy 早期调用一次（other.initOther 同款顺序）。
func initFreepi() {
	freepi.UsagePath = kit.ResolveDataPath("freepi-state.json")
	freepi.Load(freepiConfigPath())
	freepi.SetClientProvider(freepiClientFor)
	freepi.SetCatalogExitResolver(func(key string) (string, bool) {
		return gateway.FreepiExitFn()(key)
	})
	freepi.SetProxyObserver(func(u string, ok bool, e error, ms int64) {
		recordProxyOutcome(pkFreepi, u, ok, e, ms)
	})
	freepi.SetAttemptObserver(freepiAttemptObserver)
	go startFreepiModelsRefresher()
	go startFreepiCreditsRefresher()
}

// freepiClientFor FreePI 出站客户端工厂：Chrome 指纹（2026-11-19 用户定口径：
// 普通 OpenAI 兼容上游用 Chrome，Bun 指纹只对 zen/opencode 上游有风控意义；
// 也规避 agnes 换证书后 Bun ClientHello 的 TLS 兼容缺陷重演）。
func freepiClientFor(proxyURL string) *http.Client {
	return exitpool.ClientForProfile(proxyURL, exitpool.ProfileChrome)
}

// freepiAttemptObserver 请求日志注入（key 明文 → 序号 + 绑定出口类型）。
func freepiAttemptObserver(ctx context.Context, key, proxyURL string) {
	if ctx == nil || key == "" {
		return
	}
	keyPoolAttemptObserver(
		func(k string) int { return freepi.KeyIndex(k) },
		func(k string) (string, string, bool) { return freepi.BindingOf(k) },
	)(ctx, key, proxyURL)
}

// startFreepiModelsRefresher 周期目录同步（5 分钟，other 同款节奏；无启用
// key 时 TrySync 内部静默跳过）。
func startFreepiModelsRefresher() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		if len(freepi.Get().Keys) > 0 {
			_, _ = freepi.TrySync()
		}
	}
}

// startFreepiCreditsRefresher 周期刷新 credits 快照（间隔面板可配，默认 30
// 分钟；启动抖动错峰；/me 免费只读、量极小）：额度加权调度的权重数据源
// 保鲜——日常消耗不更新快照，陈旧权重会让差异化额度（邀请加成号）的趋平
// 退化。间隔热改在当前周期结束后生效。与目录同步 refresher 同口径不判
// ONLY_ROUTER（免费只读轻量，非营销类任务）。
// 2026-11-25 用户定：未勾选「启用权重优化」时不再后台刷新（权重列表保持冻结）。
// 手动刷新按钮走独立 handler，不受此开关约束。
func startFreepiCreditsRefresher() {
	time.Sleep(time.Duration(kit.RandIntn(freepi.Get().WeightInterval()*60)) * time.Second) // 启动抖动：错开其他周期任务节拍
	for {
		time.Sleep(time.Duration(freepi.Get().WeightInterval()) * time.Minute)
		if !freepi.Get().WeightOptimizeEnabled() {
			continue
		}
		if len(freepi.Get().Keys) > 0 {
			if refreshed, failed := freepiRefreshEnabledCredits(); failed > 0 {
				log.Printf("freepi credits auto-refresh: %d ok, %d failed", refreshed, failed)
			}
		}
	}
}

// freepiSetNext 写时复制编排：深拷配置 → mutate → Set（normalize+落盘+重建
// 信号量+痕迹收敛）。各 key 管理 handler 共用。
func freepiSetNext(mutate func(next *freepi.Config)) {
	next := freepi.CloneConfig(freepi.Get())
	mutate(next)
	freepi.Set(next)
}

// GET /admin/api/freepi/config —— 平台配置与每 key 运行时状态（面板主加载）。
func handleFreepiConfig(w http.ResponseWriter, r *http.Request) {
	c := freepi.Get()
	models := freepi.KnownModels()
	modelViews := make([]map[string]any, 0, len(models))
	for _, m := range models {
		mv := map[string]any{"id": m.ID, "name": m.Name}
		if m.Context > 0 {
			mv["context"] = m.Context
		}
		modelViews = append(modelViews, mv)
	}
	data := map[string]any{
		"useProxies":              freepi.ProxiesEnabled(),
		"maxConcurrency":          c.MaxConcurrency,
		"retries":                 c.Retries,
		"weightOptimize":          c.WeightOptimizeEnabled(),
		"weightInterval":          c.WeightInterval(),
		"sessionSticky":           c.StickyEnabled(),
		"clientVersion":          c.ClientVersion,
		"clientVersionEffective": c.EffectiveClientVersion(),
		"clientVersionDefault":   freepi.ClientVersion,
		"routedModels":            c.RoutedModels,
		"keyStates":               freepi.KeyStatus(),
		"models":                  modelViews,
	}
	// 目录同步时刻（pill「官方源同步于」）：零值时省略字段，前端显示空 pill
	if t := freepi.CatalogSyncedAt(); !t.IsZero() {
		data["modelsSyncedAt"] = t.UTC().Format(time.RFC3339)
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// POST /admin/api/freepi/config/update —— patch 更新（全指针字段，nil=未提交）。
// 走池开关与并发/重试全部热生效（key 池无重启项概念）。
func handleFreepiConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var patch struct {
		UseProxies     *bool `json:"useProxies"`
		MaxConcurrency *int  `json:"maxConcurrency"`
		Retries        *int  `json:"retries"`
		WeightOptimize *bool `json:"weightOptimize"`
		WeightInterval *int  `json:"weightInterval"`
		// 会话粘性（2026-11-25）：nil = 未提交。
		SessionSticky *bool `json:"sessionSticky"`
		// 路由白名单（2026-11-24）：nil = 未提交；非 nil = 整表替换（空表 =
		// 全部路由）。指针区分语义与面板"即点即存"一致。
		RoutedModels *[]string `json:"routedModels"`
		// x-client-version 覆盖（2026-11-27）：nil = 未提交；非 nil 空串 = 清除
		// 覆盖回落默认；非空必须 x.y.z 形状（上游最低版本闸按 semver 比较，
		// 乱填形状等于把指纹写坏）。
		ClientVersion *string `json:"clientVersion"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&patch); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if patch.UseProxies != nil {
		freepi.SetConfigUseProxies(patch.UseProxies)
	}
	// 版本覆盖形状校验先行（400 在落盘前）：空串=清除覆盖；非空必须 x.y.z。
	if patch.ClientVersion != nil {
		v := strings.TrimSpace(*patch.ClientVersion)
		if v != "" && !freepiVersionRe.MatchString(v) {
			WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "clientVersion must look like x.y.z (e.g. 0.6.28), or empty to reset"})
			return
		}
	}
	if patch.MaxConcurrency != nil || patch.Retries != nil ||
		patch.WeightOptimize != nil || patch.WeightInterval != nil ||
		patch.SessionSticky != nil || patch.RoutedModels != nil ||
		patch.ClientVersion != nil {
		freepiSetNext(func(next *freepi.Config) {
			if patch.MaxConcurrency != nil && *patch.MaxConcurrency > 0 {
				next.MaxConcurrency = *patch.MaxConcurrency
			}
			if patch.Retries != nil && *patch.Retries >= 0 {
				next.Retries = *patch.Retries
			}
			if patch.WeightOptimize != nil {
				next.WeightOptimize = patch.WeightOptimize
			}
			// 0 = 恢复默认间隔（WeightInterval 访问器把 <=0 回落为 30）
			if patch.WeightInterval != nil && *patch.WeightInterval >= 0 {
				next.WeightIntervalMinutes = *patch.WeightInterval
			}
			if patch.SessionSticky != nil {
				next.SessionSticky = patch.SessionSticky
			}
			// 白名单整表替换（trim/去重在 normalize），空表 = 全部路由
			if patch.RoutedModels != nil {
				next.RoutedModels = *patch.RoutedModels
			}
			// 版本覆盖：normalize 会 trim，空串即清除（EffectiveClientVersion
			// 空值回落包级默认）
			if patch.ClientVersion != nil {
				next.ClientVersion = *patch.ClientVersion
			}
		})
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Config saved"})
}

// POST /admin/api/freepi/keys/update —— 添加凭据（JWT 形状校验先行：坏 JWT
// 在落盘前就报清楚，不让上游 401 说话）+ 昵称/改名（账号列编辑，other 同款
// 语义：renames 迁移路由/绑定/昵称表；冷却与 token 记账刻意不迁移——新 key
// 是上游新身份，旧 key 统计归档）。body: { add, nicknames, nickEdits, renames }
// freepiKeysMutMu 串行化「Get→改→Set」的 key 池读改写段（2026-11-27 审计
// S12）：设备码 done 并发完成、或与 keys/update 粘贴添加并发时，后 Set 者以
// 自己的快照整体覆盖前者，先入池的 token/改动丢失。窗口虽窄但属丢失更新；
// 锁内只有纯内存校验与一次落盘，无长阻塞。
var freepiKeysMutMu sync.Mutex

func handleFreepiKeysUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Add       []string          `json:"add"`
		Nicknames map[string]string `json:"nicknames"`
		NickEdits []struct {
			Index    int    `json:"index"`
			Nickname string `json:"nickname"`
		} `json:"nickEdits"`
		Renames []struct {
			Index int    `json:"index"`
			To    string `json:"to"`
		} `json:"renames"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	freepiKeysMutMu.Lock()
	defer freepiKeysMutMu.Unlock()
	cur := freepi.Get()
	hadKeys := len(cur.Keys) > 0
	// 添加行先清洗+形状校验（对齐 plugin cleanToken/jwtShape：env 行/引号/
	// Bearer 前缀剥离；三段 base64url、签名 43 字符、全 ASCII 可见）
	addedKeys := make([]string, 0, len(req.Add))
	for _, raw := range req.Add {
		k := freepi.CleanToken(raw)
		if k == "" {
			continue
		}
		if shapeErr := freepi.ValidateJWT(k); shapeErr != nil {
			WriteAPI(w, http.StatusBadRequest, apiResponse{Error: freepi.MaskKey(k) + ": " + shapeErr.Error()})
			return
		}
		addedKeys = append(addedKeys, k)
	}
	next := freepi.CloneConfig(cur)
	// key 改名先于追加：迁移路由/绑定/昵称表，再按新明文参与去重
	for _, rn := range req.Renames {
		to := strings.TrimSpace(rn.To)
		if rn.Index < 0 || rn.Index >= len(next.Keys) || to == "" {
			continue
		}
		from := next.Keys[rn.Index]
		if from == to {
			continue
		}
		next.Keys[rn.Index] = to
		if en, ok := next.KeyRoutingEnabled[from]; ok {
			next.KeyRoutingEnabled[to] = en
			delete(next.KeyRoutingEnabled, from)
		}
		if b, ok := next.KeyBindings[from]; ok {
			next.KeyBindings[to] = b
			delete(next.KeyBindings, from)
		}
		if nick, ok := next.KeyNicknames[from]; ok {
			next.KeyNicknames[to] = nick
			delete(next.KeyNicknames, from)
		}
	}
	for _, ne := range req.NickEdits {
		if ne.Index < 0 || ne.Index >= len(next.Keys) {
			continue
		}
		k := next.Keys[ne.Index]
		if v := strings.TrimSpace(ne.Nickname); v != "" {
			next.KeyNicknames[k] = v
		} else {
			delete(next.KeyNicknames, k)
		}
	}
	added := 0
	existing := map[string]bool{}
	for _, k := range next.Keys {
		existing[k] = true
	}
	for _, k := range addedKeys {
		if existing[k] {
			continue
		}
		existing[k] = true
		next.Keys = append(next.Keys, k)
		added++
	}
	if req.Nicknames != nil {
		for k, v := range req.Nicknames {
			if v = strings.TrimSpace(v); v != "" && existing[k] {
				next.KeyNicknames[k] = v
			}
		}
	}
	next.ReconcileRouting(cur)
	freepi.Set(next)
	// 首次配 key 后立即同步目录（失败静默：周期刷新与手动按钮仍可重试）
	if !hadKeys && added > 0 {
		go func() { _, _ = freepi.TrySync() }()
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Keys saved", Data: map[string]any{"added": added}})
}

// POST /admin/api/freepi/keys/get —— 编辑弹窗眼睛/复制用：按 index 返回 key
// 明文（面板信任模型与代理池页同口径，2026-11-20）。body: { index }
func handleFreepiKeyGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	c := freepi.Get()
	if req.Index < 0 || req.Index >= len(c.Keys) {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"key": c.Keys[req.Index]}})
}

// POST /admin/api/freepi/keys/routing  body: { index, enabled }
func handleFreepiKeySetRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index   int  `json:"index"`
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	c := freepi.Get()
	if req.Index < 0 || req.Index >= len(c.Keys) {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	next := freepi.CloneConfig(c)
	next.KeyRoutingEnabled[c.Keys[req.Index]] = req.Enabled
	freepi.Set(next)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d routing enabled=%v", req.Index+1, req.Enabled)})
}

// POST /admin/api/freepi/keys/routing/all  body: { enabled }
func handleFreepiKeySetRoutingAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	n := 0
	freepiSetNext(func(next *freepi.Config) {
		for _, k := range next.Keys {
			next.KeyRoutingEnabled[k] = req.Enabled
			n++
		}
	})
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("routing enabled=%v on %d key(s)", req.Enabled, n)})
}

// POST /admin/api/freepi/keys/reorder  body: { order: [oldIndex,...] }
func handleFreepiKeysReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Order []int `json:"order"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	cur := freepi.Get()
	n := len(cur.Keys)
	if len(req.Order) == 0 {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "order is required"})
		return
	}
	nextKeys := make([]string, 0, n)
	seen := make(map[int]bool, n)
	for _, idx := range req.Order {
		if idx < 0 || idx >= n {
			WriteAPI(w, http.StatusBadRequest, apiResponse{Error: fmt.Sprintf("key index out of range: %d", idx)})
			return
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		nextKeys = append(nextKeys, cur.Keys[idx])
	}
	if len(nextKeys) != n {
		for i, k := range cur.Keys {
			if !seen[i] {
				nextKeys = append(nextKeys, k)
			}
		}
	}
	next := freepi.CloneConfig(cur)
	next.Keys = nextKeys
	freepi.Set(next)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"updated": len(req.Order)}})
}

// POST /admin/api/freepi/keys/delete  body: { index }
func handleFreepiKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := freepi.Get()
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	next := freepi.CloneConfig(cur)
	key := next.Keys[req.Index]
	next.Keys = append(next.Keys[:req.Index], next.Keys[req.Index+1:]...)
	delete(next.KeyBindings, key)
	delete(next.KeyRoutingEnabled, key)
	delete(next.KeyNicknames, key)
	freepi.Set(next)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d deleted", req.Index+1)})
}

// POST /admin/api/freepi/keys/proxy  body: { index, main, backup }
func handleFreepiKeySetProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index  int    `json:"index"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	main := strings.TrimSpace(req.Main)
	backup := strings.TrimSpace(req.Backup)
	cur := freepi.Get()
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cur.Keys[req.Index]
	backup = resolveBackupSlot(backup, cur.KeyBindings[key].Backup)
	if err := validateProxyBinding(main, backup); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	next := freepi.CloneConfig(cur)
	if main == "" && backup == "" {
		delete(next.KeyBindings, key)
	} else {
		next.KeyBindings[key] = freepi.Binding{Main: main, Backup: backup}
	}
	freepi.Set(next)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Key proxy binding saved"})
}

// POST /admin/api/freepi/keys/proxy/clear —— 清绑定（不含"全部置禁用"：
// 置禁用走暂存启停按钮，二者前端绑 together，见 stageClearFreepiKeyProxies）。
// body: {}
func handleFreepiKeyClearProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	n := len(freepi.Get().KeyBindings)
	freepiSetNext(func(next *freepi.Config) {
		next.KeyBindings = map[string]freepi.Binding{}
	})
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"cleared": n}})
}

// POST /admin/api/freepi/keys/test  body: { index }
//
// 与其他 key 池平台的 Test 语义刻意不同：这里打 GET /me 而不是最小 chat。
// 理由：/me 免费（对话按 credits 计费，探测白烧日额度）、同样强验证 JWT
// 真伪（plugin 的添加路径即以 /me 验证），顺带把 credits 余额落进痕迹表。
// 成功即清除冷却（真实 2xx 是最强证据）。
func handleFreepiKeyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	result, status := freepiTestKey(req.Index)
	result["status"] = status
	log.Printf("Test freepi key #%d: status=%s reason=%v", req.Index+1, status, result["reason"])
	WriteAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// freepiTestKey 单 key 探测（/me）：出口解析 fail-closed（绑定双不可用即中止，
// 绝不直连兜底）；成功把 handle/credits 快照落进痕迹表。
func freepiTestKey(index int) (map[string]any, string) {
	result := map[string]any{"index": index}
	c := freepi.Get()
	if index < 0 || index >= len(c.Keys) {
		result["reason"] = "key index out of range"
		return result, "error"
	}
	key := c.Keys[index]
	result["keyMask"] = freepi.MaskKey(key)
	exitURL, ok := gateway.FreepiExitFn()(key)
	if !ok {
		result["reason"] = kit.BoundProbeBlockedMsg(freepi.ModelPrefix, key,
			fmt.Sprintf("key#%d bound proxies unavailable (cooling or removed); probe aborted by proxy isolation", freepi.KeyIndex(key)))
		return result, "error"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	info, err := freepi.Me(ctx, freepiClientFor(exitURL), key)
	result["latencyMs"] = time.Since(start).Milliseconds()
	var he *freepi.HTTPError
	if err != nil && errors.As(err, &he) {
		result["httpStatus"] = he.Status
		if he.Status == http.StatusTooManyRequests {
			result["reason"] = fmt.Sprintf("rate limited (HTTP %d): %s", he.Status, he.Body)
			if until, ok := freepi.CooldownUntil(key); ok {
				result["cooldownUntil"] = until.UTC().Format(time.RFC3339)
				result["remaining"] = formatDuration(time.Until(until))
			}
			return result, "cooldown"
		}
		result["reason"] = fmt.Sprintf("API %d: %s", he.Status, he.Body)
		return result, "error"
	}
	if err != nil {
		result["reason"] = "upstream call failed: " + err.Error()
		return result, "error"
	}
	freepi.Uncool(key)
	freepi.SetCredits(key, freepi.CreditStats{
		Remaining: info.Remaining,
		Allowance: info.Allowance,
		RefillAt:  info.RefillAt,
		Handle:    info.Handle,
		MeAt:      time.Now(),
	})
	result["reason"] = "ok"
	if info.Handle != "" {
		result["handle"] = info.Handle
	}
	result["creditsLeft"] = info.Remaining
	result["creditsAllowance"] = info.Allowance
	return result, "active"
}

// POST /admin/api/freepi/models/refresh —— 全平台统一刷新契约
// （panel.WriteCatalogResult；目录是易失缓存，TrySync 只改内存）。
func handleFreepiModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	// 无启用 key 时拒绝同步并给出提示（前端同判定，此处兜底防直连 API）。
	c := freepi.Get()
	enabled := false
	for _, k := range c.Keys {
		if c.RoutingEnabled(k) {
			enabled = true
			break
		}
	}
	if !enabled {
		panel.WriteCatalogNoCredential(w, platFreepi)
		return
	}
	before := len(freepi.KnownModels())
	added, err := freepi.TrySync()
	if err != nil {
		panel.WriteCatalogSyncFailed(w, platFreepi, err)
		return
	}
	total := len(freepi.KnownModels())
	if added <= 0 {
		added = total - before
	}
	panel.WriteCatalogResult(w, platFreepi, added, total)
}

// freepiRefreshEnabledCredits 串行刷新全部启用 key 的 /me 余额快照（免费；
// 账号间不并发，防同一上游瞬时连查——trae「刷新余额」节奏）。返回 (成功, 失败)。
// 额度刷新端点与权重优化端点共用。
func freepiRefreshEnabledCredits() (int, int) {
	c := freepi.Get()
	refreshed, failed := 0, 0
	for i, key := range c.Keys {
		if !c.RoutingEnabled(key) {
			continue
		}
		exitURL, ok := gateway.FreepiExitFn()(key)
		if !ok {
			failed++
			log.Printf("freepi credits refresh key#%d: no usable exit (isolation)", i+1)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		info, err := freepi.Me(ctx, freepiClientFor(exitURL), key)
		cancel()
		if err != nil {
			failed++
			log.Printf("freepi credits refresh key#%d: %v", i+1, err)
			continue
		}
		freepi.SetCredits(key, freepi.CreditStats{
			Remaining: info.Remaining,
			Allowance: info.Allowance,
			RefillAt:  info.RefillAt,
			Handle:    info.Handle,
			MeAt:      time.Now(),
		})
		refreshed++
	}
	return refreshed, failed
}

// POST /admin/api/freepi/credits/refresh —— 批量刷新每日 credits。响应
// { refreshed, failed }。
func handleFreepiCreditsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	refreshed, failed := freepiRefreshEnabledCredits()
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"refreshed": refreshed, "failed": failed}})
}

// clearFreepiKeysForDataDelete 清空 key 池与绑定/路由/昵称表（保留并发/重试
// 等平台设置）；token 记账/冷却/credits 一并清空（ClearUsage 即时落盘）。
func clearFreepiKeysForDataDelete() int {
	next := freepi.CloneConfig(freepi.Get())
	n := len(next.Keys)
	next.Keys = nil
	next.KeyBindings = map[string]freepi.Binding{}
	next.KeyRoutingEnabled = map[string]bool{}
	next.KeyNicknames = map[string]string{}
	freepi.Set(next)
	freepi.ClearUsage()
	return n
}

// freepiDeviceExitURL 设备码流程出站出口（无 key 可绑定，走全局口径，与
// freepiExitFn 未绑定分支同语义）：走池开关开 → freepi 池轮转；关 → 直连；
// DISABLE_DIRECT 时直连不放行；出口全被手动冷却 → fail-closed。
func freepiDeviceExitURL() (string, bool) {
	if !freepi.ProxiesEnabled() {
		if kit.DisableDirect() {
			return "", false
		}
		return "", true
	}
	url, _, ok := exitpool.UpstreamForKind(exitpool.KindFreepi)
	if !ok {
		return "", false
	}
	if url == "" && kit.DisableDirect() {
		return "", false
	}
	return url, true
}

// POST /admin/api/freepi/device/start —— GitHub 设备码授权第一步：申请会话
// + consent（5xx/网络抖动自动重试 3 次——上游偶发 500 实测）。响应
// { sessionId, userCode, url, interval, expiresIn }；轮询由面板 JS 驱动。
func handleFreepiDeviceStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	exitURL, ok := freepiDeviceExitURL()
	if !ok {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "all pool exits are manually cooled; device sign-in aborted (fail-closed)"})
		return
	}
	client := freepiClientFor(exitURL)
	var sess *freepi.DeviceSession
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		sess, lastErr = freepi.DeviceStart(client)
		if lastErr == nil {
			break
		}
		log.Printf("freepi device start attempt %d: %v", attempt, lastErr)
		if attempt < 3 {
			time.Sleep(3 * time.Second)
		}
	}
	if sess == nil {
		WriteAPI(w, http.StatusBadGateway, apiResponse{Error: lastErr.Error()})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"sessionId": sess.SessionID,
		"userCode":  sess.UserCode,
		"url":       sess.URL,
		"interval":  sess.Interval,
		"expiresIn": sess.ExpiresIn,
	}})
}

// POST /admin/api/freepi/device/poll body { sessionId } —— 轮询一步。上游出
// token 即就地入池（与粘贴路径同款 CleanToken/ValidateJWT 校验与去重；昵称
// = GitHub handle；ReconcileRouting 新 key 默认不参与路由），并落 credits
// 快照。响应 { status: pending|done|failed, handle, nickname, added, message }。
func handleFreepiDevicePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" || len(sessionID) > 64 {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "sessionId is required"})
		return
	}
	exitURL, ok := freepiDeviceExitURL()
	if !ok {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "all pool exits are manually cooled; poll aborted (fail-closed)"})
		return
	}
	client := freepiClientFor(exitURL)
	res, err := freepi.DevicePoll(client, sessionID)
	if err != nil {
		// transport 抖动：按 pending 继续，err 只进日志
		log.Printf("freepi device poll: %v", err)
	}
	if res.Status != "done" {
		data := map[string]any{"status": res.Status}
		if res.Message != "" {
			data["message"] = res.Message
		}
		WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
		return
	}
	token := freepi.CleanToken(res.Token)
	if token == "" {
		WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"status": "failed", "message": "empty token from upstream"}})
		return
	}
	if shapeErr := freepi.ValidateJWT(token); shapeErr != nil {
		WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"status": "failed", "message": "token failed validation: " + shapeErr.Error()}})
		return
	}
	// /me 取 handle 昵称与 credits 快照（失败不阻断入池——/auth/token 交换
	// 本身已验证授权；handle 缺失时昵称留空，面板可用编辑弹窗补）
	handle := ""
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	info, meErr := freepi.Me(ctx, freepiClientFor(exitURL), token)
	cancel()
	if meErr == nil {
		handle = info.Handle
		freepi.SetCredits(token, freepi.CreditStats{
			Remaining: info.Remaining,
			Allowance: info.Allowance,
			RefillAt:  info.RefillAt,
			Handle:    info.Handle,
			MeAt:      time.Now(),
		})
	}
	// 入池：与 handleFreepiKeysUpdate 添加路径同款（去重 + ReconcileRouting）。
	// 读改写段必须互斥（2026-11-27 审计 S12）：两个 GitHub 账号并发授权完成时，
	// 后 Set 者覆盖前者，先入池 token 丢失。
	freepiKeysMutMu.Lock()
	cur := freepi.Get()
	hadKeys := len(cur.Keys) > 0
	added := false
	dup := false
	for _, k := range cur.Keys {
		if k == token {
			dup = true
			break
		}
	}
	if !dup {
		next := freepi.CloneConfig(cur)
		next.Keys = append(next.Keys, token)
		if handle != "" {
			next.KeyNicknames[token] = handle
		}
		next.ReconcileRouting(cur)
		freepi.Set(next)
		added = true
		if !hadKeys {
			go func() { _, _ = freepi.TrySync() }()
		}
	}
	freepiKeysMutMu.Unlock()
	log.Printf("freepi device poll: token accepted handle=%s added=%v", handle, added)
	data := map[string]any{"status": "done", "handle": handle, "added": added}
	if handle != "" {
		data["nickname"] = handle
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

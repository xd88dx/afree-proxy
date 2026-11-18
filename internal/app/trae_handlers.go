package app

// trae_handlers.go —— TraeWork 面板 handler 与网关分发壳（2026-11-18 P3e
// 从 trae.go 拆回：panel 域响应封装（panel.WriteAPI/panel.ApiResponse/panel.AdminAuthMiddleware）
// 与网关分发（handleStreamBufferedGeneric/rejectCaller）属装配根职责；域逻辑
// 在 internal/trae，经 trae_bridge.go 的小写别名消费。vendored 引引擎经门面
// trae2api 引用。

import (
	"afree-proxy/internal/panel"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// handleTraeConfig GET /admin/api/trae/config —— 面板一次性取全：
// vendored 配置子集 + afree 域字段（费率过滤/隔离/备用）+ 账号状态表。
func handleTraeConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	sub := trae2apiCurrent()
	data := map[string]any{
		"engineVersion":           trae2apiVersion,
		"loaded":                  sub != nil,
		"useProxies":              traeProxiesEnabled(),
		"proxyIsolation":          proxyIsolationEnabled(),
		"proxyIsolationEnvLocked": proxyIsolationEnvLocked(),
		"backupProxyEnabled":      backupProxyEnabled(),
		"rateFilterCN":            trae2apiRateFilterThresholdCN(),
		"rateFilterIntl":          trae2apiRateFilterThresholdIntl(),
		"routable":                trae2apiRoutableCount(),
	}
	if sub != nil {
		cfg := sub.Cfg
		data["gateway"] = cfg.Trae.ChatBase
		data["maxInFlight"] = cfg.Pool.MaxInFlight
		data["breakerThreshold"] = cfg.Pool.BreakerThreshold
		data["breakerCooldown"] = cfg.Pool.BreakerCooldown
		data["cooldownSoftRate"] = cfg.Cooldown.SoftRate
		data["cooldownSoftRateMax"] = cfg.Cooldown.SoftRateMax
		data["cooldownModel"] = cfg.Cooldown.ModelCooldown
		data["checkinEnabled"] = cfg.Schedule.CheckinEnabled
		data["checkinHours"] = cfg.Schedule.CheckinHours
		data["strictModels"] = cfg.Trae.StrictModels == nil || *cfg.Trae.StrictModels
		data["sessionSticky"] = cfg.SessionSticky.Enabled
		data["intlEnabled"] = cfg.Intl.Enabled
		data["idleTimeout"] = cfg.Trae.IdleTimeoutSeconds
		data["accounts"] = trae2apiAccountStates(sub)
		data["models"] = trae2apiModelEntries()
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// trae2apiAccountStates 账号表：vendored 池状态 + afree 双开关/绑定合并。

func handleTraeConfigUpdate(w http.ResponseWriter, r *http.Request) {
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
	var patch struct {
		Gateway          *string `json:"gateway"`
		MaxInFlight      *int    `json:"maxInFlight"`
		BreakerThreshold *int    `json:"breakerThreshold"`
		BreakerCooldown  *string `json:"breakerCooldown"`
		CooldownSoftRate *string `json:"cooldownSoftRate"`
		CooldownSoftMax  *string `json:"cooldownSoftRateMax"`
		CooldownModel    *string `json:"cooldownModel"`
		CheckinEnabled   *bool   `json:"checkinEnabled"`
		CheckinHours     *[]int  `json:"checkinHours"`
		StrictModels     *bool   `json:"strictModels"`
		SessionSticky    *bool   `json:"sessionSticky"`
		IdleTimeout      *int    `json:"idleTimeout"`
		IntlEnabled      *bool   `json:"intlEnabled"`
		UseProxies       *bool   `json:"useProxies"`
		// 分域倍率筛选：各自独立生效，nil = 不改动（旧单值 rateFilter 键已删）。
		RateFilterCN   *float64 `json:"rateFilterCN"`
		RateFilterIntl *float64 `json:"rateFilterIntl"`
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}

	sub := trae2apiCurrent()
	if sub == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	// 走池开关（Global policy）不属于 vendored 配置：落 proxy-config.json 的
	// traeUseProxies（与其它七个平台同一归属）。此前 patch 结构缺该字段，
	// 面板保存后回弹、开关恒为默认（2026-11-09 审计修复）。
	if patch.UseProxies != nil {
		setPlatformUseProxies("trae", patch.UseProxies)
	}
	// 时长字段必须先校验再落盘：vendor 的 traecfg.normalize 对非法时长是硬失败
	// （Load 报错），而本函数是"先写文件再重启"——一次笔误（如 "30" 缺单位）就会
	// 让引擎装不起来、子系统变 nil，连生成本次故障的 config/update 自身都被 503
	// 守卫挡住，面板无法自愈（2026-11-09 审计修复）。
	for name, v := range map[string]*string{
		"breaker_cooldown":   patch.BreakerCooldown,
		"cooldown_soft_rate": patch.CooldownSoftRate,
		"cooldown_soft_max":  patch.CooldownSoftMax,
		"cooldown_model":     patch.CooldownModel,
	} {
		if v == nil || strings.TrimSpace(*v) == "" {
			continue
		}
		if _, err := time.ParseDuration(strings.TrimSpace(*v)); err != nil {
			WriteAPI(w, http.StatusBadRequest, apiResponse{Error: fmt.Sprintf("%s: invalid duration %q (use forms like 30m or 2h)", name, *v)})
			return
		}
	}
	sub.Mu.Lock()
	cfg := sub.Cfg
	// gateway 不再接受面板修改（2026-11-04 用户定）：沿用上游 chat_base 默认
	// （traeApplyAfreeConstraints 兜底补值）。patch 里的 gateway 字段直接忽略
	// ——保持 API 兼容（老客户端不发新请求不会炸），但改了也不生效，防止出现
	// "面板上没有的开关却暗中生效"的悖离。gateway 的手改值由装载路径
	// traeApplyAfreeConstraints 调 trae2apiValidateGateway 把关（非法域回退默认）。
	_ = patch.Gateway
	if patch.MaxInFlight != nil && *patch.MaxInFlight > 0 {
		cfg.Pool.MaxInFlight = *patch.MaxInFlight
	}
	if patch.BreakerThreshold != nil && *patch.BreakerThreshold > 0 {
		cfg.Pool.BreakerThreshold = *patch.BreakerThreshold
	}
	if patch.BreakerCooldown != nil {
		cfg.Pool.BreakerCooldown = strings.TrimSpace(*patch.BreakerCooldown)
	}
	if patch.CooldownSoftRate != nil {
		cfg.Cooldown.SoftRate = strings.TrimSpace(*patch.CooldownSoftRate)
	}
	if patch.CooldownSoftMax != nil {
		cfg.Cooldown.SoftRateMax = strings.TrimSpace(*patch.CooldownSoftMax)
	}
	if patch.CooldownModel != nil {
		cfg.Cooldown.ModelCooldown = strings.TrimSpace(*patch.CooldownModel)
	}
	if patch.CheckinEnabled != nil {
		cfg.Schedule.CheckinEnabled = *patch.CheckinEnabled
	}
	if patch.CheckinHours != nil {
		cfg.Schedule.CheckinHours = *patch.CheckinHours
	}
	if patch.StrictModels != nil {
		v := *patch.StrictModels
		cfg.Trae.StrictModels = &v
	}
	if patch.SessionSticky != nil {
		cfg.SessionSticky.Enabled = *patch.SessionSticky
	}
	if patch.IdleTimeout != nil && *patch.IdleTimeout > 0 {
		cfg.Trae.IdleTimeoutSeconds = *patch.IdleTimeout
	}
	if patch.IntlEnabled != nil {
		cfg.Intl.Enabled = *patch.IntlEnabled
	}
	// afree 硬约束在序列化前重新套上（面板改不动的字段恒定）。
	traeApplyAfreeConstraints(cfg)
	raw, err := json.MarshalIndent(cfg, "", "  ")
	sub.Mu.Unlock()
	if err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	if err := os.WriteFile(traeConfigPath(), raw, 0o600); err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	traeApplyRateFilterPatch(traeRateFilterPatch{
		RateFilterCN:   patch.RateFilterCN,
		RateFilterIntl: patch.RateFilterIntl,
	})
	if err := trae2apiRestart(); err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: "restart: " + err.Error()})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"restarted": true}})
}

// trae2apiValidateGateway 网关域校验（判据自旧引擎 oauthOnlyHostFragments
// 移植，2026-11-04 实测补缺）：OAuth/账号路由域作网关必失败——只承载 OAuth、
// 不能发模型请求的域名被判非法。调用点 = traeApplyAfreeConstraints（装载期
// 回退默认并告警，不让引擎因此装不起来）。

// handleTraeModelsRefresh POST /admin/api/trae/models/refresh —— vendored
// 目录随启动/intl 配置静态成型，这里仅重读并回传（老面板刷新按钮的语义）。
func handleTraeModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	models := trae2apiModelEntries()
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"models": models, "total": len(models), "added": 0}})
}

// handleTraeAccountSetEnabled POST /admin/api/trae/accounts/enabled —— 账号
// 总闸（vendored disabled 字段）。关闭时连带关掉反代开关（互锁，与 wb 同款）：
// 账号不存在了，路由参与位没有意义；重开账号不自动恢复路由（显式操作）。

// handleTraeAccountSetEnabled POST /admin/api/trae/accounts/enabled —— 账号
// 总闸（vendored disabled 字段）。关闭时连带关掉反代开关（互锁，与 wb 同款）：
// 账号不存在了，路由参与位没有意义；重开账号不自动恢复路由（显式操作）。
func handleTraeAccountSetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID     string `json:"uid"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	if sub.Srv.Pool().Get(p.UID) == nil {
		WriteAPI(w, http.StatusNotFound, apiResponse{Error: "account not found: " + p.UID})
		return
	}
	reason := ""
	if !p.Enabled {
		reason = "disabled from panel"
	}
	if sub.Srv.Pool().SetDisabled(p.UID, !p.Enabled, reason) {
		_ = sub.Srv.Pool().Save()
	}
	if !p.Enabled && traeRoutingGet(p.UID) {
		traeRoutingSet(p.UID, false)
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountSetRouting POST /admin/api/trae/accounts/routing —— 反代
// 开关（路由参与位）。账号总闸关闭时不可打开（互锁）。

// handleTraeAccountSetRouting POST /admin/api/trae/accounts/routing —— 反代
// 开关（路由参与位）。账号总闸关闭时不可打开（互锁）。
func handleTraeAccountSetRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID     string `json:"uid"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	if a := sub.Srv.Pool().Get(p.UID); a == nil {
		WriteAPI(w, http.StatusNotFound, apiResponse{Error: "account not found: " + p.UID})
		return
	} else if st := a.Status(time.Now()); st.Disabled && p.Enabled {
		WriteAPI(w, http.StatusBadRequest, apiResponse{
			Error: "account switch is off; turn the account switch on first"})
		return
	}
	traeRoutingSet(p.UID, p.Enabled)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountSetRoutingAll POST /admin/api/trae/accounts/routing/all ——
// 批量反代开关。enabled=false 全关；enabled=true 仅作用于"账号总闸开"的行。

// handleTraeAccountSetRoutingAll POST /admin/api/trae/accounts/routing/all ——
// 批量反代开关。enabled=false 全关；enabled=true 仅作用于"账号总闸开"的行。
func handleTraeAccountSetRoutingAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	for _, st := range sub.Srv.Pool().List() {
		if !p.Enabled || !st.Disabled {
			traeRoutingSet(st.UID, p.Enabled)
		}
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountSetProxy POST /admin/api/trae/accounts/proxy —— 主/备
// 出口绑定（traeBindings 段，与 workbuddyBindings 同语义）。

// handleTraeAccountSetProxy POST /admin/api/trae/accounts/proxy —— 主/备
// 出口绑定（traeBindings 段，与 workbuddyBindings 同语义）。
func handleTraeAccountSetProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID    string `json:"uid"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	if sub.Srv.Pool().Get(p.UID) == nil {
		WriteAPI(w, http.StatusNotFound, apiResponse{Error: "account not found: " + p.UID})
		return
	}
	if p.Main == "" && p.Backup == "" {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "both main and backup are empty; use proxy/clear"})
		return
	}
	traeBindingsMu.Lock()
	loadTraeBindingsLocked()
	prev := (*traeBindings)[p.UID]
	main := strings.TrimSpace(p.Main)
	backup := resolveBackupSlot(strings.TrimSpace(p.Backup), prev.Backup)
	if err := validateProxyBinding(main, backup); err != nil {
		traeBindingsMu.Unlock()
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	(*traeBindings)[p.UID] = trae2apiProxyBinding{Main: main, Backup: backup}
	saveTraeBindingsLocked()
	traeBindingsMu.Unlock()
	WriteAPI(w, http.StatusOK, apiResponse{Success: true})
}

// exportTrae2apiProxyBindings TraeWork 账号出口绑定（afree-config.json 的
// trae.bindings 段）随配置备份迁移（2026-11-05 用户实测补缺：导出→删卷→
// 导入后绑定丢失，wb 同类缺口 2026-10-14 已补、trae 换线时没跟）。

// handleTraeAccountClearProxies POST /admin/api/trae/accounts/proxy/clear ——
// 「清空代理」语义与各平台一致：清绑定 + 该账号路由置禁用（二者永远绑一起）。
func handleTraeAccountClearProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	traeBindingsMu.Lock()
	loadTraeBindingsLocked()
	delete(*traeBindings, p.UID)
	saveTraeBindingsLocked()
	traeBindingsMu.Unlock()
	traeRoutingSet(p.UID, false)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountDelete POST /admin/api/trae/accounts/delete —— 删除快照
// 文件（vendored DELETE /admin/accounts/{name}），连带清理 afree 侧路由位。

// handleTraeAccountDelete POST /admin/api/trae/accounts/delete —— 删除快照
// 文件（vendored DELETE /admin/accounts/{name}），连带清理 afree 侧路由位。
func handleTraeAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	code, data, err := trae2apiAdminCall(http.MethodDelete, "/admin/accounts/"+p.UID, nil)
	if err != nil {
		WriteAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	if code >= 400 {
		WriteAPI(w, code, apiResponse{Error: strings.TrimSpace(string(data))})
		return
	}
	traeBindingsMu.Lock()
	loadTraeBindingsLocked()
	delete(*traeBindings, p.UID)
	saveTraeBindingsLocked()
	traeBindingsMu.Unlock()
	WriteAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountLabel POST /admin/api/trae/accounts/label —— 昵称写入
// vendored Store（Label 字段随快照持久化）。

// handleTraeAccountLabel POST /admin/api/trae/accounts/label —— 昵称写入
// vendored Store（Label 字段随快照持久化）。
func handleTraeAccountLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID   string `json:"uid"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	sub := trae2apiCurrent()
	if sub == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	a := sub.Srv.Pool().Get(p.UID)
	if a == nil || a.Store == nil {
		WriteAPI(w, http.StatusNotFound, apiResponse{Error: "account not found: " + p.UID})
		return
	}
	// 快照文件是标签的持久层（重启后渠道重建以文件为准），运行时标签经
	// Store.Load() + 池 SetLabel 同步（vendor 注入的并发安全写入口）。
	label := strings.TrimSpace(p.Label)
	path := a.Store.Path()
	raw, err := os.ReadFile(path)
	if err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	enc, _ := json.Marshal(label)
	m["label"] = enc
	out, err := json.Marshal(m)
	if err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	if err := a.Store.Load(); err != nil {
		WriteAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	a.SetLabel(label)
	_ = sub.Srv.Pool().Save()
	WriteAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleTraeAccountsReorder POST /admin/api/trae/accounts/reorder body: { uids: [...] }
// 凭据表拖拽排序（2026-11-05 用户定）：仅改展示顺序（vendored 池 order），
// 选号是引擎加权随机与顺序无关——与 WorkBuddy SetOrder 同语义。

// handleTraeAccountsReorder POST /admin/api/trae/accounts/reorder body: { uids: [...] }
// 凭据表拖拽排序（2026-11-05 用户定）：仅改展示顺序（vendored 池 order），
// 选号是引擎加权随机与顺序无关——与 WorkBuddy SetOrder 同语义。
func handleTraeAccountsReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if trae2apiCurrent() == nil {
		WriteAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "TraeWork subsystem unavailable"})
		return
	}
	var req struct {
		UIDs []string `json:"uids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if len(req.UIDs) == 0 {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "uids is required"})
		return
	}
	n := trae2apiSetDisplayOrder(req.UIDs)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"ordered": n}})
}

// handleTraeAccountTest POST /admin/api/trae/accounts/test —— 沿用上游设计：
// 额度连通性验证（真实 UG 调用 + 失败自动续期一次），无模型探测。

// handleTraeAccountTest POST /admin/api/trae/accounts/test —— 沿用上游设计：
// 额度连通性验证（真实 UG 调用 + 失败自动续期一次），无模型探测。
func handleTraeAccountTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/accounts/"+p.UID+"/test", nil)
	if err != nil {
		WriteAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	var vendored map[string]any
	_ = json.Unmarshal(data, &vendored)
	WriteAPI(w, code, apiResponse{Success: code < 400, Data: vendored, Error: vendoredErr(vendored, code)})
}

// handleTraeCheckin POST /admin/api/trae/checkin —— 全量签到（vendored
// CheckinAll：跳过国际版/SOLO、9074 换设备退避重试，签到失败不罚账号）。

// handleTraeCheckin POST /admin/api/trae/checkin —— 全量签到（vendored
// CheckinAll：跳过国际版/SOLO、9074 换设备退避重试，签到失败不罚账号）。
func handleTraeCheckin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/checkin", nil)
	if err != nil {
		WriteAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	var vendored struct {
		Results []map[string]any `json:"results"`
	}
	_ = json.Unmarshal(data, &vendored)
	WriteAPI(w, code, apiResponse{Success: code < 400, Data: vendored, Error: vendoredErrCode(code)})
}

// vendoredErrCode 无错误体时的兜底错误文案（HTTP 状态码）。

// 刷新（vendored RefreshCredits）。
func handleTraeCreditsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/credits/refresh", nil)
	if err != nil {
		WriteAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	var vendored map[string]any
	_ = json.Unmarshal(data, &vendored)
	WriteAPI(w, code, apiResponse{Success: code < 400, Data: vendored, Error: vendoredErr(vendored, code)})
}

// handleTraeCooldownClear POST /admin/api/trae/accounts/cooldown-clear ——
// 解除账号冷却（vendored clear-cooldown）。

// handleTraeCooldownClear POST /admin/api/trae/accounts/cooldown-clear ——
// 解除账号冷却（vendored clear-cooldown）。
func handleTraeCooldownClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var p struct {
		UID string `json:"uid"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/accounts/"+p.UID+"/clear-cooldown", nil)
	if err != nil {
		WriteAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	WriteAPI(w, code, apiResponse{Success: code < 400, Data: json.RawMessage(data)})
}

// vendoredErr 从 vendored OpenAI 风格错误体取可读消息。

// ============ 原版 console 挂载（/admin/trae2api/） ============

// trae2apiConsoleHandler 原版入口：vendored console 挂在本仓 /admin/trae2api/ 下
// （新标签页打开，与 WorkBuddy「Original panel」同模式）。路径重写：
//
//	/admin/trae2api/api/X → /X  （console fetch 已被 vendor 改写到该基址）
//	/admin/trae2api/X     → /X  （页面本体 /console 与根路径资源）
//
// 门禁复用 afree 管理会话 cookie；vendored 侧匿名管理员（cfg.APIKey/webPassword
// 恒空），对公网不可达（本 handler 只挂在 afree /admin 鉴权之后）。
func trae2apiConsoleHandler() http.Handler {
	return panel.AdminAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sub := trae2apiCurrent()
		if sub == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "TraeWork subsystem unavailable"})
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/admin/trae2api")
		if rest == "" || rest == "/" {
			rest = "/console"
		}
		if strings.HasPrefix(rest, "/api/") {
			rest = strings.TrimPrefix(rest, "/api")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = rest
		sub.Srv.Handler().ServeHTTP(w, r2)
	}))
}

// ============ 备份导出 / 导入 ============

// trae2apiBackupPayload 备份的 trae 段形态（2026-11-04 起）。config =
// data/trae2api/config.json 原文；accounts = 快照文件名 → 快照 JSON；routing =
// 反代开关表。旧引擎（trae-config.json 账号池）的形态不再兼容。

// POST /api/web-auth  body: helper 解析后的凭据 dict（token/refreshToken/userId/...）
// 响应契约与原版一致：{"success":true} / {"success":false,"error":"..."}。
//
// 源限制（2026-10-29 审计修复）：本端点无认证（helper 是裸脚本，加认证会破坏
// 与上游 web_login.py 的行级基线等价），但凭据写入绝不能对公网开放。设计场景
// 是 helper 与用户浏览器同网段：本机部署=127.0.0.1，NAS/LAN 部署=内网 PC 的
// RFC1918/ULA 地址——因此只放行回环+内网源，公网/反代外来源一律 403 fail-closed。
func handleTraeWebAuth(w http.ResponseWriter, r *http.Request) {
	if !webAuthSourceAllowed(r) {
		log.Printf("trae web-auth rejected: non-local source %s", r.RemoteAddr)
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": "web-auth endpoint only accepts local/LAN sources"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	var creds map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&creds); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	uid, label, added, err := trae2apiIngestCreds(creds)
	rec := &traeHelperIngest{At: time.Now().UTC(), Success: err == nil, Label: label, Added: added}
	if err != nil {
		rec.Message = err.Error()
	} else if added {
		// 英文源文 + 面板 T() 词条（I18N）双语；这两条是固定串，可整句词条。
		rec.Message = "Credentials saved to server"
	} else {
		rec.Message = "Account already exists"
	}
	traeLastIngestMu.Lock()
	setTraeLastIngest(rec)
	traeLastIngestMu.Unlock()
	if err != nil {
		log.Printf("trae web-auth ingest failed: %v", err)
	} else {
		log.Printf("trae web-auth ingest: account %q (%s), added=%v, routing disabled by default", label, uid, added)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": err == nil, "error": rec.Message, "label": label})
}

// GET /trae/oauth/helper?as=py —— 下载按本部署预填 relay 的授权助手脚本。
// relay 取请求自身 Host（浏览器能打开面板就能运行助手连通网关）。

// GET /trae/oauth/helper?as=py —— 下载按本部署预填 relay 的授权助手脚本。
// relay 取请求自身 Host（浏览器能打开面板就能运行助手连通网关）。
func handleTraeHelperDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	base := "http://" + r.Host
	// 反向代理/HTTPS 部署：X-Forwarded-Proto 优先
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		base = "https://" + r.Host
	}
	script := helperScript(base)
	w.Header().Set("Content-Type", "text/x-python; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=web_login.py")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

// GET /admin/api/trae/helper/status —— 最近一次 helper 入库结果。

// GET /admin/api/trae/helper/status —— 最近一次 helper 入库结果。
func handleTraeHelperStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	traeLastIngestMu.Lock()
	rec := traeLastIngest()
	traeLastIngestMu.Unlock()
	var data map[string]any
	if rec == nil {
		data = map[string]any{"seen": false}
	} else {
		data = map[string]any{
			"seen":    true,
			"at":      rec.At.Format(time.RFC3339),
			"success": rec.Success,
			"message": rec.Message,
			"label":   rec.Label,
			"added":   rec.Added,
		}
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// ============ 凭据入库 ============

// trae2apiIngestCreds 把 helper/面板粘贴的凭据转成完整便携快照并导入：
// 进程内 POST /admin/accounts/import（vendored ParseSnapshots→Validate→
// accountstore 落盘→registerAccount 热加池）。重复导入由 vendored 覆盖语义
// 处理（同名快照覆盖，uid 不变则 added=false 由调用方判）。
// 返回 (uid, label, added, err)。

// handleTraeAccountAdd POST /admin/api/trae/accounts/add —— 手动粘贴凭据。
// 支持行格式 `token----refreshToken`（与快照行格式一致）与 JSON
// {"token":...,"refreshToken":...}；token-only 拒绝（见文件头说明）。
func handleTraeAccountAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Tokens []string `json:"tokens"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	added, failed := 0, 0
	lastErr := ""
	for _, line := range req.Tokens {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		creds := map[string]any{}
		// 行格式 token----refreshToken（refreshToken 里不会出现 "----"）。
		if !strings.HasPrefix(line, "{") && strings.Contains(line, "----") {
			parts := strings.SplitN(line, "----", 2)
			creds["token"] = strings.TrimSpace(parts[0])
			creds["refreshToken"] = strings.TrimSpace(parts[1])
		} else if strings.HasPrefix(line, "{") {
			if err := json.Unmarshal([]byte(line), &creds); err != nil {
				failed++
				lastErr = err.Error()
				continue
			}
		} else {
			creds["token"] = line
		}
		if _, _, _, err := trae2apiIngestCreds(creds); err != nil {
			failed++
			lastErr = err.Error()
			continue
		}
		added++
	}
	msg := fmt.Sprintf("added %d account(s)", added)
	if failed > 0 {
		msg += fmt.Sprintf(", failed %d: %s", failed, lastErr)
	}
	log.Printf("trae accounts add: %s", msg)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: msg,
		Data: map[string]any{"added": added, "failed": failed, "error": lastErr}})
}

// ============ 设备身份铸造 ============

// trae2apiMintDeviceKeyPair 现场铸造设备 EC P-256 密钥对（PKCS#8 私钥 PEM +
// SPKI 公钥 PEM，与 CredentialSnapshot 的格式约定一致）。

// serveTrae2apiChat 把请求原样交给 vendored /v1/chat/completions（进程内
// ServeHTTP，与 workbuddy serveWorkBuddyChat 同机制）。模型名**透传**——
// vendored 原生解析域前缀与 strict_models 校验，afree 不做任何翻译。
func serveTrae2apiChat(rw http.ResponseWriter, r *http.Request, body []byte) {
	sub := trae2apiCurrent()
	if sub == nil {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "TraeWork subsystem unavailable", "type": "api_error"},
		})
		return
	}
	if trae2apiRoutableCount() == 0 {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"message": "No routable Trae accounts. Enable an account (and its gateway switch) on the Trae page.",
				"type":    "api_error",
			},
		})
		return
	}
	// OpenAI 契约：stream 缺省 = false。vendored 引擎把缺省当 true（上游实现
	// `req.Stream == nil || *req.Stream`），按规范省略 stream 的客户端会收到
	// SSE 而不是 JSON（2026-11-05 审计实测）。只兜 /v1/chat/completions 入口：
	// 缺省时显式置 false；/v1/messages、/v1/responses 走 trae2apiCaller，由
	// 调用方参数决定流式语义，不经这里。
	body = traeNormalizeStreamDefault(body)
	// 合并域翻译（2026-11-07）：对外 tacn:/tagb: → 内部 realm:bare 透传给
	// vendored（交集以 trae 为主，solo 独有归 solo）。
	body = trae2apiTranslateMergedBody(body)

	// 流式请求走"缓冲+中断重试"（与 cline/zen 同语义）：上游 SSE 先完整缓冲、
	// 拿到 finish/[DONE] 才向客户端发 200 并回放；只有**缓冲不完整**（真流中断：
	// EOF/连接重置/idle 断流）才换账号重试整个请求（每次 attempt 由引擎自选新
	// 账号，重试上限 StreamMaxAttempts、ctx 取消不重试——不存在无限重试）。
	// 引擎级 error 帧（上下文超长/4001/权限类确定性错误）后跟 [DONE]，缓冲完整、
	// 不重试，error 帧原样回放给客户端——换账号救不回的错误不白打上游。
	// 2026-11-10 用户定：trae 上游（IDE 逆向接口+恒流式+WAF）中断率高，直通流
	// 时代"响应头已发就只能 200+error 帧"，agent 把半截回复当完成（伪成功）。
	// 非流式保持直通：引擎聚合语义已在 vendored 内，无需外层包装。
	var params map[string]any
	_ = json.Unmarshal(body, &params)
	isStream, _ := params["stream"].(bool)
	if isStream {
		handleStreamBufferedGeneric(rw, r, "trae", params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
			reqBody, err := json.Marshal(params)
			if err != nil {
				return nil, nil, err
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost,
				traeInprocessBase+"/v1/chat/completions", bytesReader(reqBody))
			if err != nil {
				return nil, nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := traeInprocessHTTP.Do(req)
			if err != nil {
				return nil, nil, err
			}
			if resp.StatusCode != http.StatusOK {
				eb, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
				resp.Body.Close()
				return nil, nil, &upstreamHTTPError{Status: resp.StatusCode, Body: string(eb)}
			}
			// 记账回调恒 nil：token/积分记账由 vendored metrics 按账号自行落账。
			return resp, nil, nil
		})
		return
	}

	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	sub.Srv.Handler().ServeHTTP(rw, r)
}

// traeInprocessCaller 为平台 chat 调用器抽象（cross_endpoint）构造的进程内
// client：请求直接进 vendored Handler，响应以 *http.Response 形态交还外层
// 缓冲转发层（与 cline/zen 同一套流式/重试语义）。

// trae2apiCaller 平台 chat 调用器（anthropic 跨端点路径）：model 透传，
// 记账回调恒 nil（token/积分记账由 vendored metrics 按账号自行落账）。
func trae2apiCaller(model string) (platformChatCall, bool) {
	if !isTrae2apiModel(model) {
		return nil, false
	}
	if trae2apiCurrent() == nil {
		return rejectCaller("TraeWork subsystem unavailable", false), true
	}
	if trae2apiRoutableCount() == 0 {
		return rejectCaller("No routable Trae accounts. Add accounts on the Trae page.", true), true
	}
	return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, func(map[string]any), error) {
		// 流式语义以调用方参数为准（/v1/responses 恒 true 与其余平台对齐，
		// /v1/messages 跟随客户端意图），不依赖 body 里的 stream 键——vendored
		// 引擎把「缺省」当 true，与 OpenAI 缺省 false 相反，缺省请求会被错误
		// 流式化（2026-11-05 审计实测）。
		params["stream"] = stream
		// 合并域翻译（同 serveTrae2apiChat）：caller 服务跨端点路径。
		params["model"] = trae2apiTranslateMerged(model)
		body, err := json.Marshal(params)
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			traeInprocessBase+"/v1/chat/completions", bytesReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := traeInprocessHTTP.Do(req)
		if err != nil {
			return nil, nil, err
		}
		// 非 200 必须是 error：vendored 的错误体是普通 JSON，让调用方拿 200 假设
		// 去解析（流式路径按 SSE 收、非流式路径按 chat JSON 解）会把它变成
		// "upstream stream interrupted"/解析失败，真实状态码（400/429/5xx）与上游
		// 原话全丢（2026-11-09 修复；429 经 upstreamErrorStatus 原样透传）。
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			return nil, nil, &upstreamHTTPError{Status: resp.StatusCode, Body: string(body)}
		}
		return resp, nil, nil
	}, true
}

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"afree-proxy/internal/kit"
)

// ============ Zen 免费模型管理 API ============

// GET /admin/api/zen/config
func handleZenConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: zenConfigPayload()})
}

// zenConfigPayload 是面板「OpenCode + 代理池」页的合并视图。代理池配置已拆到
// proxy-config.json 的出口池段（proxy_pool_config.go），但面板契约的 JSON 键名
// 保持不变，前端无需改动。
func zenConfigPayload() map[string]any {
	cfg := getZenConfig()
	binding := getProxyBindingConfig()
	pool := getProxyPool()
	return map[string]any{
		"zenUseProxies":       zenProxiesEnabled(),
		"workbuddyUseProxies": workbuddyProxiesEnabled(),
		"key":                 cfg.Key,
		"keys":                cfg.Keys,
		"keyStates":           zenKeyStatus(),
		"baseURL":             cfg.BaseURL,
		"proxies":             pool.Proxies,
		"proxyStrategy":       pool.Strategy,
		"proxySubURL":         pool.SubURL,
		"proxySubCron":        pool.SubCron,
		"maxConcurrency":      cfg.MaxConcurrency,
		"retries":             cfg.Retries,
		"compaction":          cfg.Compaction,
		// 代理隔离（账号/key 绑定出口）：生效值 + 是否被 env 钉死（面板据此
		// 禁用开关并提示）。
		"proxyIsolation":          proxyIsolationEnabled(),
		"proxyIsolationEnvLocked": proxyIsolationEnvLocked(),
		// 备用代理开关：关闭时主代理不通不再尝试备用代理（绑定退化为只看主槽）。
		"backupProxyEnabled": backupProxyEnabled(),
		// 自动分配规则（平台级代理黑名单 / 每代理绑定上限），代理池页编辑。
		"proxyExclusions": binding.ProxyExclusions,
		"proxyBindCaps":   binding.ProxyBindCaps,
		// 延迟重排的"保持不变"阈值（秒，生效值——未配置时为默认 8s）。
		"reorderKeepSec": reorderKeepSec(),
		// egress 自动钉定层（3758eb7）：开关状态与容量快照（代理够不够分）。
		"pinProxyPerKey": pinProxyPerKeyEnabled(),
		"egress":         zenEgressSnapshot(),
		// OAuth 账户组织 ID（上游 571f9b9）：面板可编辑，空 = 不发该头。
		"orgID": cfg.OrgID,
		"runtime": map[string]any{
			"proxyCooldowns": proxyCooldownStatus(),
			// 每代理在线率统计（纯被动：只记真实请求的出口成败）。URL 已打码。
			"proxyHealth": proxyHealthSnapshot(),
		},
	}
}

// POST /admin/api/zen/config/update
func handleZenConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()

	cur := getZenConfig()
	// 代理池字段走独立配置（proxy-config.json 的出口池段，proxy_pool_config.go）；
	// 只有本次提交真的带了代理池字段才回写，避免每次保存 zen 配置都无谓重写
	// 代理文件。
	poolNext := *getProxyPool()
	poolChanged := false
	// 隔离/绑定段同样独立落盘（proxy-config.json），只在本次真的带了相关字段
	// 或代理池变动需要清理排除表时才回写。
	bindingNext := *getProxyBindingConfig()
	bindingChanged := false
	var patch struct {
		ZenUseProxies       *bool    `json:"zenUseProxies"`
		WorkbuddyUseProxies *bool    `json:"workbuddyUseProxies"`
		Key                 *string  `json:"key"`
		Keys                []string `json:"keys"`
		BaseURL             *string  `json:"baseURL"`
		Proxies             []string `json:"proxies"`
		ProxyStrategy       *string  `json:"proxyStrategy"`
		ProxySubURL         *string  `json:"proxySubURL"`
		ProxySubCron        *string  `json:"proxySubCron"`
		MaxConcurrency      *int     `json:"maxConcurrency"`
		Retries             *int     `json:"retries"`
		ProxyIsolation      *bool    `json:"proxyIsolation"`
		BackupProxyEnabled  *bool    `json:"backupProxyEnabled"`
		// 自动分配规则：nil = 不改动；非 nil = 整表替换（空表 = 全部清除）。
		ProxyExclusions map[string][]string `json:"proxyExclusions"`
		ProxyBindCaps   map[string]int      `json:"proxyBindCaps"`
		// 延迟重排阈值（秒）：指针区分"未提交"与显式 0（0 = 所有有记录的
		// 代理都算慢、全部进入修复段）。
		ReorderKeepSec *float64 `json:"reorderKeepSec"`
		// OAuth 账户组织 ID（x-opencode-org-id，上游 571f9b9）：空串 = 清除该头。
		OrgID *string `json:"orgID"`
		// 每 key 固定出口代理（egress 层，3758eb7）：指针区分"未提交"与显式 false。
		PinProxyPerKey *bool `json:"pinProxyPerKey"`
		Compaction     *struct {
			Auto         *bool   `json:"auto"`
			Buffer       *int    `json:"buffer"`
			KeepTokens   *int    `json:"keepTokens"`
			SummaryModel *string `json:"summaryModel"`
			MaxSummary   *int    `json:"maxSummary"`
		} `json:"compaction"`
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	next := &zenConfigData{
		Key:            cur.Key,
		Keys:           cur.Keys,
		BaseURL:        cur.BaseURL,
		MaxConcurrency: cur.MaxConcurrency,
		Retries:        cur.Retries,
		// KeyBindings 必须随 next 保留：config/update 是全量替换语义，漏拷会把
		// 每个 key 的主/备出口绑定整体清掉——绑定按 key 明文存，是代理隔离的
		// 载体，丢了隔离就退化成走共享池。
		KeyBindings: cur.KeyBindings,
		// KeyRoutingEnabled 必须随 next 保留：config/update 是全量替换语义，
		// 漏拷会把面板勾选的路由参与表整体清掉（缺项 = 全部参与）。
		KeyRoutingEnabled: cur.KeyRoutingEnabled,
		Compaction:        cur.Compaction,
		// OrgID 必须随 next 保留（全量替换语义，漏拷 = 保存面板即清掉 org 头）。
		OrgID:          cur.OrgID,
		PinProxyPerKey: cur.PinProxyPerKey,
	}
	if patch.ZenUseProxies != nil {
		poolNext.ZenUseProxies = patch.ZenUseProxies
		poolChanged = true
	}
	if patch.WorkbuddyUseProxies != nil {
		poolNext.WorkbuddyUseProxies = patch.WorkbuddyUseProxies
		poolChanged = true
	}
	if patch.Keys != nil {
		// 多 key 池整体替换；兼容旧单 key 字段（key 非空时视为单元素列表）
		if len(patch.Keys) == 0 && (patch.Key == nil || *patch.Key == "") {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "keys list is empty"})
			return
		}
		if len(patch.Keys) > 0 {
			next.Keys = patch.Keys
		}
	}
	if patch.Key != nil && *patch.Key != "" && patch.Keys == nil {
		// 旧客户端单 key 提交 → 单元素池
		next.Keys = []string{*patch.Key}
	}
	if patch.Keys != nil || (patch.Key != nil && *patch.Key != "") {
		// key 池发生变动：新 key 默认禁用路由（面板勾选启用后生效），移除的 key
		// 清理表项——与 Cline/WorkBuddy 的新增身份默认不启用统一口径。
		reconcileZenKeyRouting(next, cur)
	}
	if patch.BaseURL != nil && *patch.BaseURL != "" {
		next.BaseURL = strings.TrimRight(*patch.BaseURL, "/")
	}
	if patch.Proxies != nil {
		// 每行先规范化（支持订阅分享格式 socks://b64(user:pass)@host:port#别名
		// 等，自动拆出别名），再按标准格式校验入库。别名（#fragment）随行
		// 保留 —— 别名就是代理行的一部分，随行保存/回显/编辑，无需单独同步。
		canonical := make([]string, 0, len(patch.Proxies))
		for _, line := range patch.Proxies {
			parsed, _, err := parseProxyLine(line)
			if err != nil {
				writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
				return
			}
			if parsed == "" {
				continue
			}
			canonical = append(canonical, parsed)
		}
		if err := validateProxyList(canonical); err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
			return
		}
		poolNext.Proxies = canonical
		poolChanged = true
		// 池变动后同步清理排除表：已不在池内的条目永远不会被自动分配命中，
		// 留着只是垃圾配置（订阅整池替换路径 proxy_subscription.go 复用本
		// 结构体浅拷贝，无需重复处理）。
		bindingNext.ProxyExclusions = pruneProxyExclusions(bindingNext.ProxyExclusions, canonical)
		bindingChanged = true
	}
	if patch.ProxyStrategy != nil && *patch.ProxyStrategy != "" {
		poolNext.Strategy = *patch.ProxyStrategy
		poolChanged = true
	}
	if patch.ProxySubURL != nil {
		poolNext.SubURL = strings.TrimSpace(*patch.ProxySubURL)
		poolChanged = true
	}
	if patch.ProxySubCron != nil {
		expr := strings.TrimSpace(*patch.ProxySubCron)
		if expr != "" {
			if err := validateCronExpr(expr); err != nil {
				writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid cron expression: " + err.Error()})
				return
			}
		}
		poolNext.SubCron = expr
		poolChanged = true
	}
	if patch.MaxConcurrency != nil && *patch.MaxConcurrency > 0 {
		next.MaxConcurrency = *patch.MaxConcurrency
	}
	if patch.Retries != nil && *patch.Retries >= 0 {
		next.Retries = *patch.Retries
	}
	if patch.ProxyIsolation != nil {
		// 仅 PROXY_ISOLATION=true 锁定面板（强制开启）；env=false 或未设置时
		// 面板可正常修改。锁定时直接拒绝并说明，免得用户以为保存成功了。
		if proxyIsolationEnvLocked() {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "PROXY_ISOLATION=true forces isolation on; unset the env var (or set it to false) to control isolation from the panel"})
			return
		}
		bindingNext.ProxyIsolation = patch.ProxyIsolation
		bindingChanged = true
	}
	if patch.BackupProxyEnabled != nil {
		bindingNext.BackupProxyEnabled = patch.BackupProxyEnabled
		bindingChanged = true
	}
	if patch.ProxyExclusions != nil {
		// 整表替换语义：逐条校验平台 kind，空代理行/空平台列表丢弃，全空归 nil
		clean := make(map[string][]string, len(patch.ProxyExclusions))
		for p, kinds := range patch.ProxyExclusions {
			if p == "" || len(kinds) == 0 {
				continue
			}
			for _, k := range kinds {
				if !proxyRuleKindValid(k) {
					writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid platform kind: " + k})
					return
				}
			}
			clean[p] = kinds
		}
		if len(clean) == 0 {
			clean = nil
		}
		bindingNext.ProxyExclusions = clean
		bindingChanged = true
	}
	if patch.ProxyBindCaps != nil {
		// 整表替换语义：只收正值，全空归 nil（缺项/0 = 不限）
		clean := make(map[string]int, len(patch.ProxyBindCaps))
		for k, v := range patch.ProxyBindCaps {
			if !proxyRuleKindValid(k) {
				writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid platform kind: " + k})
				return
			}
			if v > 0 {
				clean[k] = v
			}
		}
		if len(clean) == 0 {
			clean = nil
		}
		bindingNext.ProxyBindCaps = clean
		bindingChanged = true
	}
	if patch.ReorderKeepSec != nil {
		// 负数无意义（延迟不会为负），上限只做宽松护栏（600s = 10 分钟，
		// 再大等于"所有代理都保持不变"，重排就没意义了）。
		v := *patch.ReorderKeepSec
		if v < 0 || v > 600 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid reorderKeepSec: must be between 0 and 600 seconds"})
			return
		}
		bindingNext.ReorderKeepSec = v
		bindingChanged = true
	}
	if patch.PinProxyPerKey != nil {
		next.PinProxyPerKey = patch.PinProxyPerKey
	}
	if patch.OrgID != nil {
		// 空串即清除（不编造 org id：原始 API key 流量本来就没有）。
		next.OrgID = strings.TrimSpace(*patch.OrgID)
	}
	if patch.Compaction != nil {
		base := cur.Compaction
		if patch.Compaction.Buffer != nil {
			base.Buffer = *patch.Compaction.Buffer
		}
		if patch.Compaction.KeepTokens != nil {
			base.KeepTokens = *patch.Compaction.KeepTokens
		}
		if patch.Compaction.SummaryModel != nil {
			base.SummaryModel = *patch.Compaction.SummaryModel
		}
		if patch.Compaction.MaxSummary != nil {
			base.MaxSummary = *patch.Compaction.MaxSummary
		}
		if patch.Compaction.Auto != nil {
			base.Auto = *patch.Compaction.Auto
		}
		next.Compaction = base
	}
	setZenConfig(next)
	if bindingChanged {
		setProxyBindingConfig(&bindingNext)
	}
	if poolChanged {
		setProxyPool(&poolNext)
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: zenConfigPayload()})
}

// GET /admin/api/opencode/models — 只返回免费模型
func handleZenModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	initZenModels()
	// 展示门控（与 amd/tokenharbor/trae 同口径，2026-10-27 用户定）：没有
	// 任何参与路由的启用 key 时目录不对外提供——匿名 "public" 哨兵已移除，
	// 上游匿名层也不可用，列出来只会让用户选中后调不通。
	// credentialsEnabled 供前端区分「无凭据」（引导启用）与「目录为空」。
	credentialsEnabled := zenHasRoutableKey()
	zenModelsMu.RLock()
	models := make([]map[string]any, 0, len(zenModels))
	if credentialsEnabled {
		for _, m := range zenModels {
			if !isZenFreeModel(m) {
				continue
			}
			models = append(models, map[string]any{
				// 网关前缀（zen:<id>）：与 /v1/models 输出一致，面板即所见即所调。
				// 裸 ID 仍可解析（存量 IDE 配置与 combo target 兼容）。
				"id":        zenPrefixedID(m.ID),
				"aliases":   m.Aliases,
				"context":   m.Context,
				"output":    m.Output,
				"source":    m.Source,
				"upstream":  m.Upstream,
				"toolCall":  m.ToolCall,
				"reasoning": m.Reasoning,
				"attach":    m.Attach,
			})
		}
	}
	zenModelsMu.RUnlock()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"models": models, "count": len(models), "credentialsEnabled": credentialsEnabled}})
}

// POST /admin/api/zen/models/refresh
func handleZenModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	// 与 AMD/TokenHarbor 同口径：无启用 key 时拒绝同步并给出提示。
	if !zenHasRoutableKey() {
		writeAPI(w, http.StatusOK, apiResponse{
			Success: false,
			Message: "no enabled OpenCode key — enable a key first, then refresh",
		})
		return
	}
	added, err := syncZenModels()
	if err != nil {
		writeAPI(w, http.StatusBadGateway, apiResponse{Error: "sync failed: " + err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("synced, %d new models", added)})
}

// GET /admin/api/zen/sessions
// 每个 key 的会话状态（本地铸造，无 mint 任务进度）。
func handleZenSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	keys := getZenConfig().Keys
	sessions := make([]map[string]any, 0, len(keys))
	live := 0
	for i, k := range keys {
		s := zenSessionSnapshotOf(k)
		if s.Live {
			live++
		}
		entry := map[string]any{
			"index":     i,
			"keyMask":   maskZenKey(k),
			"noKey":     k == "",
			"live":      s.Live,
			"minted":    s.Minted,
			"session":   s.Session,
			"createdAt": "",
		}
		if s.CreatedAt > 0 {
			entry["createdAt"] = time.Unix(s.CreatedAt, 0).Format(time.RFC3339)
		}
		sessions = append(sessions, entry)
	}
	data := map[string]any{
		"liveCount": live,
		"total":     len(keys),
		"sessions":  sessions,
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// POST /admin/api/zen/keys/test   body: {"index": 0}
// 对单个 zen key 发一个极小探测请求（与 cline 账号的 Test 按钮同语义）。
// 整个探测固定在该 key 上（zenCallOpts.pinKey），绝不影响正常轮转；成功即
// 清除该 key 的冷却——真实 2xx 是"该 key 现在可用"的最强证据，比干等上游的
// Retry-After 更可信。429 原样上报冷却与预计恢复时间（探测本身会让 key 重新
// 进入冷却，时长来自上游 Retry-After，上限 24h）；403 = 会话已死，探测已顺带
// 触发收割机，提示去 mint。返回的 status: active / cooldown / error。
func handleZenKeyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Index int    `json:"index"`
		Model string `json:"model"` // 可选：指定探测模型；空 = 自动（big-pickle → live → 种子）
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cfg := getZenConfig()
	if req.Index < 0 || req.Index >= len(cfg.Keys) {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "key index out of range"})
		return
	}
	key := cfg.Keys[req.Index]
	if key == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "nothing to test: empty key"})
		return
	}

	result, status := testZenKey(key, req.Index, req.Model)
	// status 必须进 Data：面板的 testZenKey JS 读的是 r.status（与 cline 的
	// testAccount 相同的契约）。只放在 Message 里的话，每个 toast 都会渲染成
	// "— undefined" 并套上错误样式。
	result["status"] = status
	log.Printf("Test zen key #%d (%s): status=%s model=%s reason=%v",
		req.Index+1, maskZenKey(key), status, result["model"], result["reason"])

	writeAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// testZenKey 执行单 key 探测："Reply with exactly: OK"，按模型的 Upstream
// 字段走 chat 或原生 responses 上游（与正常请求同一条路，含 FreeTier gate、
// 会话粘性与冷却副作用）。modelID 为空时用 zenProbeModel() 自动选择
// （big-pickle → live 最小 id → 种子兜底）；非空时必须能解析为一个 free
// zen 模型，否则报错——探测不允许拿付费/不存在的模型当探针。
// 返回 (结果, 状态)。
func testZenKey(key string, index int, modelID string) (map[string]any, string) {
	result := map[string]any{
		"index":   index,
		"keyMask": maskZenKey(key),
	}
	var zm *ZenModel
	if strings.TrimSpace(modelID) != "" {
		// 支持别名与 opencode/ 前缀（与正常请求的解析规则一致）。
		m, ok := resolveZenFreeModel(modelID)
		if !ok {
			result["reason"] = fmt.Sprintf("unknown or non-free zen model: %s", modelID)
			return result, "error"
		}
		zm = m
	} else {
		zm = zenProbeModel()
	}
	if zm == nil {
		result["reason"] = "no zen model available to probe (model catalog empty)"
		return result, "error"
	}
	result["model"] = zm.ID

	params := map[string]any{
		"model":    zm.ID,
		"messages": []any{map[string]any{"role": "user", "content": "Reply with exactly: OK"}},
		// 上游 2xx 即探测成功；不追求可读内容，但太小会撞上推理模型的
		// 隐性预算（_finish_reason=length），64 足够容纳一个词。
		"max_tokens": 64,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	var (
		resp *http.Response
		err  error
	)
	opts := []zenCallOpts{{pinKey: key}}
	if zm.Upstream == "responses" {
		resp, _, err = callZenResponsesAPI(ctx, params, true, opts...)
	} else {
		resp, _, err = callZenAPI(ctx, params, true, opts...)
	}
	result["latencyMs"] = time.Since(start).Milliseconds()

	he := (*zenHTTPError)(nil)
	if err != nil && errors.As(err, &he) {
		// 先看分支再看状态码：isRateLimited 判定的 403/502/503（错误体带限流
		// 关键词）走的是限流分支——key 刚被冷却、收割机根本没跑，绝不能说
		// "会话已死/收割机已触发"。只有"干净"的 FreeTier 403 才是会话死亡。
		if he.RateLimited {
			result["httpStatus"] = he.Status
			result["reason"] = fmt.Sprintf("rate limited (HTTP %d): %s", he.Status, kit.Truncate(he.Body, 300))
			if until, ok := zenKeyCooldownUntil(key); ok {
				result["cooldownUntil"] = until.UTC().Format(time.RFC3339)
				result["remaining"] = formatDuration(time.Until(until))
			}
			return result, "cooldown"
		}
		switch he.Status {
		case http.StatusForbidden:
			result["httpStatus"] = he.Status
			result["reason"] = "session rejected (403): upstream refused this key+exit (probe: no state change) — check the exit IP/key, refreshing the session is unlikely to help"
			return result, "error"
		default:
			result["httpStatus"] = he.Status
			result["reason"] = fmt.Sprintf("API %d: %s", he.Status, kit.Truncate(he.Body, 300))
			return result, "error"
		}
	}
	if err != nil {
		// 网络/超时：不冷却、不改状态（与 cline 的网络错误分支语义一致地保守）
		result["reason"] = "upstream call failed: " + err.Error()
		return result, "error"
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		result["httpStatus"] = resp.StatusCode
		result["reason"] = fmt.Sprintf("API %d: %s", resp.StatusCode, kit.Truncate(string(bodyBytes), 300))
		return result, "error"
	}

	// Test 也是真实上游消耗：从响应体（zen 恒 SSE，usage 在收尾帧）提取 usage
	// 记账，与 /v1 业务流量统一口径（2026-10-27 用户定）。解析失败不影响结论。
	if u := usageFromChatPayload(string(bodyBytes)); len(u) > 0 {
		if n := usageTokens(u, nil); n > 0 {
			recordZenKeyTokens(key, n)
		}
	}

	// 成功：清除冷却。403 会话状态复位（zenSessionMarkSuccess，zen.go 两条
	// 调用路径各一处）已在上游 200 路径里做过，这里只复位冷却
	// （与 cline 的 Test 按钮同语义）。
	uncoolZenKey(key)
	result["reason"] = "ok"
	return result, "active"
}

// POST /admin/api/opencode/keys/routing  body: { index, enabled }
// 设置单个 zen key 的路由参与状态（面板"启用"勾选列）。禁用的 key 不进入
// 请求轮转（选号时跳过）。
func handleZenKeySetRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 512))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Index   int  `json:"index"`
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := setZenKeyRoutingEnabled(req.Index, req.Enabled); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d routing enabled=%v", req.Index+1, req.Enabled)})
}

// POST /admin/api/opencode/keys/routing/all  body: { enabled }
// 一键启用/停用全部 key 的路由参与（面板批量入口）。
func handleZenKeySetRoutingAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 512))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	n := setAllZenKeysRoutingEnabled(req.Enabled)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("routing enabled=%v on %d key(s)", req.Enabled, n)})
}

// POST /admin/api/opencode/keys/reorder  body: { order: [oldIndex,...] }
// 管理面板拖拽排序：按旧 index 置换重排 zen key 池（面板只见掩码不见明文，
// 协议用 index 置换）。持久化、Key=Keys[0] 兼容同步、轮转游标归零由
// setZenConfig 统一处理；冷却/token/粘性按 key 明文存储，与顺序无关。
func handleZenKeysReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Order []int `json:"order"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := reorderZenKeys(req.Order); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"updated": len(req.Order)}})
}

// POST /admin/api/opencode/keys/delete  body: { index }
// 从 key 池删除单个 key（按索引定位）：随行清掉代理绑定与路由参与记录；
// 会话/收割状态由 setZenConfig 的 normalize 按存活 key 列表裁剪。删空后
// key 池为空，平台转为不可路由（不再回退匿名 key）。
func handleZenKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 512))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cfg := getZenConfig()
	if req.Index < 0 || req.Index >= len(cfg.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cfg.Keys[req.Index]
	next := *cfg
	keys := make([]string, 0, len(cfg.Keys)-1)
	keys = append(keys, cfg.Keys[:req.Index]...)
	keys = append(keys, cfg.Keys[req.Index+1:]...)
	next.Keys = keys
	// Key 兼容字段必须一起清空：normalizeZenKeys 见到空 Keys 且 Key 非空时会
	// 把 Key 重新灌回池子（旧单 key 配置的迁移规则），删最后一个 key 就成了
	// 无声的假成功——key 还在，接口却回 200 deleted。
	if len(keys) == 0 {
		next.Key = ""
	}
	next.KeyRoutingEnabled = withoutKey(cfg.KeyRoutingEnabled, key)
	// 该 key 的代理绑定一并删除（绑定按 key 明文存，key 不在了绑定就是垃圾）。
	next.KeyBindings = withoutKey(cfg.KeyBindings, key)
	setZenConfig(&next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d deleted", req.Index+1)})
}

// withoutKey 复制删掉指定 key 条目的 map（nil 安全）。
func withoutKey[V any](m map[string]V, key string) map[string]V {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// POST /admin/api/opencode/keys/proxy  body: { index, main, backup }
// 设置/清除单个 zen key 的绑定出口（代理隔离）。main/backup 传空串即清除；
// 两者都为空 = 解除绑定，key 回到全局代理规则。key 按索引定位（与 keys/test
// 相同的契约），绑定表持久化到 zen 配置。
func handleZenKeySetProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Index  int    `json:"index"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if err := setKeyProxyBinding(req.Index, req.Main, req.Backup); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Key proxy binding saved"})
}

package app

import (
	"afree-proxy/internal/kit"
	"afree-proxy/internal/tokenharbor"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ============ TokenHarbor 平台装配 ============
//
// tokenharbor.ai 聚合网关（thk_live_… key，免费模型 :free 后缀）：多 key
// 轮转、同款隔离语义、tkhb: 前缀路由。模型目录同步需要 key（/v1/models 认证），
// 未配置 key 时以页面快照种子服务。

// thConfigPath TokenHarbor 配置文件路径（DATA_DIR/.th-config.json）。
func thConfigPath() string { return kit.ResolveDataPath(".th-config.json") }

// initTokenHarbor 加载配置 + 注入出站解析/HTTP client 工厂。StartProxy 早期调用一次。
func initTokenHarbor() {
	tokenharbor.Load(thConfigPath())
	tokenharbor.SetExitResolver(func(key string) (string, bool) {
		if key == "" {
			return "", true
		}
		main, backup, bound := tokenharbor.BindingOf(key)
		if !bound {
			// 未绑定 = 全局规则：面板选直连时不走池
			if !tokenharbor.ProxiesEnabled() {
				return "", true
			}
			url, _ := pickUpstreamProxy()
			return url, true
		}
		url, _, ok := pickBoundProxy(main, backup)
		return url, ok
	})
	tokenharbor.SetClientProvider(proxyClientFor)
	// 启动即预热模型目录（异步；无 key 时保持种子）
	tokenharbor.InitCatalog()
	go startTHModelsRefresher()
}

// startTHModelsRefresher 每 5 分钟同步一次模型目录（需 key；无 key 静默跳过）。
// 上游偶发 TLS EOF（与 amd/openrouter 同现象），失败重试两次再放弃本轮。
func startTHModelsRefresher() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		syncTHCatalog(2)
	}
}

// syncTHCatalog 同步目录；attempts 为额外重试次数（不含首次）。
func syncTHCatalog(attempts int) {
	for i := 0; i <= attempts; i++ {
		changed, err := tokenharbor.SyncCatalog()
		if err == nil {
			if changed > 0 {
				log.Printf("tokenharbor model sync: %d model(s) updated", changed)
			}
			return
		}
		if i < attempts {
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		} else {
			log.Printf("tokenharbor model sync failed: %v", err)
		}
	}
}

// thModelList /v1/models 合并用的模型条目。
func thModelList() []map[string]any {
	out := make([]map[string]any, 0)
	for _, m := range tokenharbor.ModelList() {
		entry := map[string]any{
			"id":       tokenharbor.ModelPrefix + m.ID,
			"object":   "model",
			"created":  time.Now().UnixMilli(),
			"owned_by": "tokenharbor",
			"source":   "tokenharbor-free",
			"status":   "active",
			"cost":     "free",
		}
		if m.Context > 0 {
			entry["context"] = m.Context
		}
		out = append(out, entry)
	}
	return out
}

// thBoundRoutable 该 key 绑定出口是否可路由（隔离判定，注入 tokenharbor.PickKey）。
func thBoundRoutable(key string) bool {
	if !proxyIsolationEnabled() {
		return true
	}
	main, backup, bound := tokenharbor.BindingOf(key)
	if !bound {
		return true
	}
	return boundProxiesRoutable(main, backup)
}

// handleTHChat TokenHarbor chat/completions 转发（网关命中 tkhb: 前缀时调用）。
func handleTHChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	bare, _ := tokenharbor.StripPrefix(mustString(params["model"]))
	m, ok := tokenharbor.Resolve(bare)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not a free TokenHarbor model", bare), "type": "invalid_request_error"},
		})
		return
	}
	if len(tokenharbor.Keys()) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"message": "No TokenHarbor keys configured. Add keys on the TokenHarbor page.", "type": "auth_error"},
		})
		return
	}
	isStream, _ := params["stream"].(bool)
	tracker := newZenStatsTracker(zenStatsRecord{
		TS:           time.Now().UnixMilli(),
		Upstream:     "tokenharbor",
		Model:        m.ID,
		Stream:       isStream,
		PromptTokens: estimateJSON(params),
	})
	usageFn := func(u map[string]any) {
		if pt, ok := u["prompt_tokens"].(float64); ok {
			tracker.rec.CompletionTokens += int(pt) - tracker.rec.PromptTokens
			if tracker.rec.CompletionTokens < 0 {
				tracker.rec.CompletionTokens = 0
			}
		}
		if ct, ok := u["completion_tokens"].(float64); ok {
			tracker.rec.CompletionTokens = int(ct)
		}
	}
	resp, rateLimited, err := tokenharbor.Chat(r.Context(), params, m.ID, true, getProxyConfig().Strategy, thBoundRoutable)
	if err != nil {
		log.Printf("  tokenharbor api error: %v", err)
		tracker.rec.RateLimited = rateLimited
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		tracker.finish(false, http.StatusBadGateway)
		return
	}
	tracker.rec.RateLimited = rateLimited
	defer resp.Body.Close()
	tracker.rec.Status = resp.StatusCode
	if isStream {
		handleStreamResponseWithUsage(w, resp, usageFn)
		tracker.finish(true, resp.StatusCode)
		return
	}
	finishZenChatNonStream(w, resp, &ZenModel{ID: m.ID}, params, usageFn, tracker)
}

// ============ TokenHarbor 管理 API ============

// GET /admin/api/tokenharbor/config
func handleTHConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	c := tokenharbor.Get()
	data := map[string]any{
		"useProxies":              tokenharbor.ProxiesEnabled(),
		"baseURL":                 c.BaseURL,
		"keys":                    tokenharbor.Keys(),
		"keyStates":               tokenharbor.KeyStatus(),
		"maxConcurrency":          c.MaxConcurrency,
		"retries":                 c.Retries,
		"models":                  thModelList(),
		"proxyIsolation":          proxyIsolationEnabled(),
		"proxyIsolationEnvLocked": proxyIsolationEnvLocked(),
		"backupProxyEnabled":      backupProxyEnabled(),
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// thCloneConfig 写时复制：拷贝运行时字段（路由表/绑定/用量/冷却）到新配置。
func thCloneConfig() (*tokenharbor.Config, *tokenharbor.Config) {
	cur := tokenharbor.Get()
	next := &tokenharbor.Config{
		UseProxies:        cur.UseProxies,
		BaseURL:           cur.BaseURL,
		Keys:              append([]string(nil), cur.Keys...),
		KeyRoutingEnabled: map[string]bool{},
		KeyBindings:       map[string]tokenharbor.Binding{},
		MaxConcurrency:    cur.MaxConcurrency,
		Retries:           cur.Retries,
		Usage:             map[string]int64{},
		Cool:              map[string]time.Time{},
	}
	for k, v := range cur.KeyRoutingEnabled {
		next.KeyRoutingEnabled[k] = v
	}
	for k, v := range cur.KeyBindings {
		next.KeyBindings[k] = v
	}
	for k, v := range cur.Usage {
		next.Usage[k] = v
	}
	for k, v := range cur.Cool {
		next.Cool[k] = v
	}
	return cur, next
}

// POST /admin/api/tokenharbor/config/update
func handleTHConfigUpdate(w http.ResponseWriter, r *http.Request) {
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
	var patch struct {
		UseProxies     *bool    `json:"useProxies"`
		BaseURL        *string  `json:"baseURL"`
		Keys           []string `json:"keys"`
		MaxConcurrency *int     `json:"maxConcurrency"`
		Retries        *int     `json:"retries"`
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	cur, next := thCloneConfig()
	if patch.UseProxies != nil {
		next.UseProxies = patch.UseProxies
	}
	if patch.BaseURL != nil {
		next.BaseURL = strings.TrimRight(strings.TrimSpace(*patch.BaseURL), "/")
	}
	if patch.MaxConcurrency != nil && *patch.MaxConcurrency > 0 {
		next.MaxConcurrency = *patch.MaxConcurrency
	}
	if patch.Retries != nil && *patch.Retries >= 0 {
		next.Retries = *patch.Retries
	}
	if patch.Keys != nil {
		if len(patch.Keys) == 0 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "keys list is empty"})
			return
		}
		next.Keys = patch.Keys
		// key 池整体替换：新 key 默认不参与路由（面板勾选启用后生效），
		// 移除的 key 清理表项（各平台统一口径）。
		next.ReconcileRouting(next, cur)
	}
	tokenharbor.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "TokenHarbor config saved"})
}

// POST /admin/api/tokenharbor/keys/routing  body: { index, enabled }
func handleTHKeySetRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index   int  `json:"index"`
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := tokenharbor.Get()
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cur.Keys[req.Index]
	_, next := thCloneConfig()
	next.KeyRoutingEnabled[key] = req.Enabled
	tokenharbor.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d routing enabled=%v", req.Index+1, req.Enabled)})
}

// POST /admin/api/tokenharbor/keys/routing/all  body: { enabled }
func handleTHKeySetRoutingAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur, next := thCloneConfig()
	n := 0
	for _, k := range cur.Keys {
		next.KeyRoutingEnabled[k] = req.Enabled
		n++
	}
	tokenharbor.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("routing enabled=%v on %d key(s)", req.Enabled, n)})
}

// POST /admin/api/tokenharbor/keys/reorder  body: { order: [oldIndex,...] }
func handleTHKeysReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Order []int `json:"order"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := tokenharbor.Get()
	n := len(cur.Keys)
	if len(req.Order) == 0 {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "order is required"})
		return
	}
	nextKeys := make([]string, 0, n)
	seen := make(map[int]bool, n)
	for _, idx := range req.Order {
		if idx < 0 || idx >= n {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: fmt.Sprintf("key index out of range: %d", idx)})
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
	_, next := thCloneConfig()
	next.Keys = nextKeys
	tokenharbor.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"updated": len(req.Order)}})
}

// POST /admin/api/tokenharbor/keys/delete  body: { index }
func handleTHKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := tokenharbor.Get()
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cur.Keys[req.Index]
	_, next := thCloneConfig()
	next.Keys = append(next.Keys[:req.Index], next.Keys[req.Index+1:]...)
	delete(next.KeyBindings, key)
	delete(next.KeyRoutingEnabled, key)
	tokenharbor.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d deleted", req.Index+1)})
}

// POST /admin/api/tokenharbor/keys/proxy  body: { index, main, backup }
func handleTHKeySetProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index  int    `json:"index"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	main := strings.TrimSpace(req.Main)
	backup := normalizeBackupSlot(strings.TrimSpace(req.Backup))
	if err := validateProxyBinding(main, backup); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	cur := tokenharbor.Get()
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cur.Keys[req.Index]
	_, next := thCloneConfig()
	if main == "" && backup == "" {
		delete(next.KeyBindings, key)
	} else {
		next.KeyBindings[key] = tokenharbor.Binding{Main: main, Backup: backup}
	}
	tokenharbor.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Key proxy binding saved"})
}

// POST /admin/api/tokenharbor/keys/proxy/clear
func handleTHKeyClearProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	cur, next := thCloneConfig()
	n := len(cur.KeyBindings)
	next.KeyBindings = map[string]tokenharbor.Binding{}
	tokenharbor.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"cleared": n}})
}

// POST /admin/api/tokenharbor/keys/test  body: { index }
func handleTHKeyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Index int    `json:"index"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	result, status := thTestKey(req.Index, req.Model)
	result["status"] = status
	log.Printf("Test tokenharbor key #%d: status=%s model=%v reason=%v", req.Index+1, status, result["model"], result["reason"])
	writeAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// POST /admin/api/tokenharbor/models/refresh
func handleTHModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	syncTHCatalog(2)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("synced, %d free models", len(tokenharbor.ModelList()))})
}

// thTestKey 对单个 key 发一个极小探测请求（与其他平台的 Test 同语义）：
// 真实 2xx = 成功并清除冷却；401/403 = key 失效；429 = 限流。
func thTestKey(index int, modelID string) (map[string]any, string) {
	cur := tokenharbor.Get()
	result := map[string]any{"index": index}
	if index < 0 || index >= len(cur.Keys) {
		result["reason"] = "key index out of range"
		return result, "error"
	}
	key := cur.Keys[index]
	result["keyMask"] = tokenharbor.MaskKey(key)
	bare := strings.TrimSpace(modelID)
	if bare != "" {
		if _, ok := tokenharbor.Resolve(bare); !ok {
			result["reason"] = fmt.Sprintf("unknown tokenharbor free model: %s", bare)
			return result, "error"
		}
	} else if pm, ok := tokenharbor.ProbeModel(); ok {
		bare = pm.ID
	} else {
		result["reason"] = "no tokenharbor free model available to probe"
		return result, "error"
	}
	result["model"] = bare

	params := map[string]any{
		"model":      tokenharbor.ModelPrefix + bare,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with exactly: OK"}},
		"max_tokens": 16,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = tokenharbor.WithPinKey(ctx, key)
	start := time.Now()
	resp, _, err := tokenharbor.Chat(ctx, params, bare, true, "fill", nil)
	result["latencyMs"] = time.Since(start).Milliseconds()
	var he *tokenharbor.HTTPError
	if err != nil && errors.As(err, &he) {
		result["httpStatus"] = he.Status
		if he.RateLimited {
			result["reason"] = fmt.Sprintf("rate limited (HTTP %d): %s", he.Status, he.Body)
			if until, ok := tokenharbor.CooldownUntil(key); ok {
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
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		result["httpStatus"] = resp.StatusCode
		result["reason"] = fmt.Sprintf("API %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		return result, "error"
	}
	tokenharbor.Uncool(key)
	result["reason"] = "ok"
	return result, "active"
}

// clearTokenHarborKeysForDataDelete 清空 TokenHarbor key 池与绑定/路由/用量表
// （保留 baseURL 与平台设置）。
func clearTokenHarborKeysForDataDelete() int {
	cur, next := thCloneConfig()
	n := len(cur.Keys)
	next.Keys = nil
	next.KeyBindings = map[string]tokenharbor.Binding{}
	next.KeyRoutingEnabled = map[string]bool{}
	next.Usage = map[string]int64{}
	next.Cool = map[string]time.Time{}
	tokenharbor.Set(next)
	return n
}

package app

import (
	"afree-proxy/internal/amd"
	"afree-proxy/internal/kit"
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

// ============ AMD Radeon Cloud 平台装配 ============
//
// Public Free Model APIs（key 由 developer.amd.com.cn/radeon/tokenfactory
// 签发，rc- 前缀）：多 key 轮转、or 同款隔离语义、amd: 前缀路由。模型目录
// 免认证同步（/v1/models 无需 key）。

// amdConfigPath AMD 配置文件路径（DATA_DIR/.amd-config.json）。
func amdConfigPath() string { return kit.ResolveDataPath(".amd-config.json") }

// initAMD 加载配置 + 注入出站解析/HTTP client 工厂。StartProxy 早期调用一次。
func initAMD() {
	amd.Load(amdConfigPath())
	amd.SetExitResolver(func(key string) (string, bool) {
		if key == "" {
			return "", true
		}
		main, backup, bound := amd.BindingOf(key)
		if !bound {
			// 未绑定 = 全局规则：面板选直连时不走池
			if !amd.ProxiesEnabled() {
				return "", true
			}
			url, _ := pickUpstreamProxy()
			return url, true
		}
		url, _, ok := pickBoundProxy(main, backup)
		return url, ok
	})
	amd.SetClientProvider(proxyClientFor)
	// 启动即预热模型目录（异步，失败保留种子）
	amd.InitCatalog()
	go startAMDModelsRefresher()
}

// startAMDModelsRefresher 每 5 分钟同步一次模型目录（免认证）。上游偶发
// TLS EOF（与 openrouter/zen 同现象），失败重试两次再放弃本轮。
func startAMDModelsRefresher() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		syncAMDCatalog(2)
	}
}

// syncAMDCatalog 同步目录；attempts 为额外重试次数（不含首次）。
func syncAMDCatalog(attempts int) {
	for i := 0; i <= attempts; i++ {
		changed, err := amd.SyncCatalog()
		if err == nil {
			if changed > 0 {
				log.Printf("amd model sync: %d model(s) updated", changed)
			}
			return
		}
		if i < attempts {
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		} else {
			log.Printf("amd model sync failed: %v", err)
		}
	}
}

// amdModelList /v1/models 合并用的模型条目。
func amdModelList() []map[string]any {
	out := make([]map[string]any, 0)
	for _, m := range amd.ModelList() {
		entry := map[string]any{
			"id":       amd.ModelPrefix + m.ID,
			"object":   "model",
			"created":  time.Now().UnixMilli(),
			"owned_by": "amd-radeon",
			"source":   "amd-free",
			"status":   "active",
			"cost":     "free-tier",
		}
		if m.Context > 0 {
			entry["context"] = m.Context
		}
		out = append(out, entry)
	}
	return out
}

// amdBoundRoutable 该 key 绑定出口是否可路由（隔离判定，注入 amd.PickKey）。
func amdBoundRoutable(key string) bool {
	if !proxyIsolationEnabled() {
		return true
	}
	main, backup, bound := amd.BindingOf(key)
	if !bound {
		return true
	}
	return boundProxiesRoutable(main, backup)
}

// handleAMDChat AMD chat/completions 转发（网关命中 amd: 前缀时调用）。
func handleAMDChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	bare, _ := amd.StripPrefix(mustString(params["model"]))
	m, ok := amd.Resolve(bare)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not an AMD Radeon Cloud model", bare), "type": "invalid_request_error"},
		})
		return
	}
	if len(amd.Keys()) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"message": "No AMD keys configured. Add keys on the AMD page.", "type": "auth_error"},
		})
		return
	}
	isStream, _ := params["stream"].(bool)
	tracker := newZenStatsTracker(zenStatsRecord{
		TS:           time.Now().UnixMilli(),
		Upstream:     "amd",
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
	resp, rateLimited, err := amd.Chat(r.Context(), params, m.ID, true, getProxyConfig().Strategy, amdBoundRoutable)
	if err != nil {
		log.Printf("  amd api error: %v", err)
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

// ============ AMD 管理 API ============

// GET /admin/api/amd/config
func handleAMDConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	c := amd.Get()
	data := map[string]any{
		"useProxies":              amd.ProxiesEnabled(),
		"baseURL":                 c.BaseURL,
		"keys":                    amd.Keys(),
		"keyStates":               amd.KeyStatus(),
		"maxConcurrency":          c.MaxConcurrency,
		"retries":                 c.Retries,
		"models":                  amdModelList(),
		"proxyIsolation":          proxyIsolationEnabled(),
		"proxyIsolationEnvLocked": proxyIsolationEnvLocked(),
		"backupProxyEnabled":      backupProxyEnabled(),
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// POST /admin/api/amd/config/update
func handleAMDConfigUpdate(w http.ResponseWriter, r *http.Request) {
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
	cur, next := amdCloneConfig()
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
	amd.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "AMD config saved"})
}

// amdCloneConfig 写时复制：拷贝运行时字段（路由表/绑定/用量/冷却）到新配置。
func amdCloneConfig() (*amd.Config, *amd.Config) {
	cur := amd.Get()
	next := &amd.Config{
		UseProxies:        cur.UseProxies,
		BaseURL:           cur.BaseURL,
		Keys:              append([]string(nil), cur.Keys...),
		KeyRoutingEnabled: map[string]bool{},
		KeyBindings:       map[string]amd.Binding{},
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

// POST /admin/api/amd/keys/routing  body: { index, enabled }
func handleAMDKeySetRouting(w http.ResponseWriter, r *http.Request) {
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
	cur := amd.Get()
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cur.Keys[req.Index]
	_, next := amdCloneConfig()
	next.KeyRoutingEnabled[key] = req.Enabled
	amd.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d routing enabled=%v", req.Index+1, req.Enabled)})
}

// POST /admin/api/amd/keys/routing/all  body: { enabled }
func handleAMDKeySetRoutingAll(w http.ResponseWriter, r *http.Request) {
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
	cur, next := amdCloneConfig()
	n := 0
	for _, k := range cur.Keys {
		next.KeyRoutingEnabled[k] = req.Enabled
		n++
	}
	amd.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("routing enabled=%v on %d key(s)", req.Enabled, n)})
}

// POST /admin/api/amd/keys/reorder  body: { order: [oldIndex,...] }
func handleAMDKeysReorder(w http.ResponseWriter, r *http.Request) {
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
	cur := amd.Get()
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
	_, next := amdCloneConfig()
	next.Keys = nextKeys
	amd.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"updated": len(req.Order)}})
}

// POST /admin/api/amd/keys/delete  body: { index }
func handleAMDKeyDelete(w http.ResponseWriter, r *http.Request) {
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
	cur := amd.Get()
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cur.Keys[req.Index]
	_, next := amdCloneConfig()
	next.Keys = append(next.Keys[:req.Index], next.Keys[req.Index+1:]...)
	delete(next.KeyBindings, key)
	delete(next.KeyRoutingEnabled, key)
	amd.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d deleted", req.Index+1)})
}

// POST /admin/api/amd/keys/proxy  body: { index, main, backup }
func handleAMDKeySetProxy(w http.ResponseWriter, r *http.Request) {
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
	cur := amd.Get()
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cur.Keys[req.Index]
	_, next := amdCloneConfig()
	if main == "" && backup == "" {
		delete(next.KeyBindings, key)
	} else {
		next.KeyBindings[key] = amd.Binding{Main: main, Backup: backup}
	}
	amd.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Key proxy binding saved"})
}

// POST /admin/api/amd/keys/proxy/clear
func handleAMDKeyClearProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	cur, next := amdCloneConfig()
	n := len(cur.KeyBindings)
	next.KeyBindings = map[string]amd.Binding{}
	amd.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"cleared": n}})
}

// POST /admin/api/amd/keys/test  body: { index }
func handleAMDKeyTest(w http.ResponseWriter, r *http.Request) {
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
	result, status := amdTestKey(req.Index, req.Model)
	result["status"] = status
	log.Printf("Test amd key #%d: status=%s model=%v reason=%v", req.Index+1, status, result["model"], result["reason"])
	writeAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// POST /admin/api/amd/models/refresh
func handleAMDModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	syncAMDCatalog(2)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("synced, %d models", len(amd.ModelList()))})
}

// amdTestKey 对单个 key 发一个极小探测请求（与 cline/zen/openrouter 的 Test
// 同语义）：真实 2xx = 成功并清除冷却；401/403 = key 失效；429 = 限流。
func amdTestKey(index int, modelID string) (map[string]any, string) {
	cur := amd.Get()
	result := map[string]any{"index": index}
	if index < 0 || index >= len(cur.Keys) {
		result["reason"] = "key index out of range"
		return result, "error"
	}
	key := cur.Keys[index]
	result["keyMask"] = amd.MaskKey(key)
	bare := strings.TrimSpace(modelID)
	if bare != "" {
		if _, ok := amd.Resolve(bare); !ok {
			result["reason"] = fmt.Sprintf("unknown amd model: %s", bare)
			return result, "error"
		}
	} else if pm, ok := amd.ProbeModel(); ok {
		bare = pm.ID
	} else {
		result["reason"] = "no amd model available to probe"
		return result, "error"
	}
	result["model"] = bare

	params := map[string]any{
		"model":      amd.ModelPrefix + bare,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with exactly: OK"}},
		"max_tokens": 16,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = amd.WithPinKey(ctx, key)
	start := time.Now()
	resp, _, err := amd.Chat(ctx, params, bare, true, "fill", nil)
	result["latencyMs"] = time.Since(start).Milliseconds()
	var he *amd.HTTPError
	if err != nil && errors.As(err, &he) {
		result["httpStatus"] = he.Status
		if he.RateLimited {
			result["reason"] = fmt.Sprintf("rate limited (HTTP %d): %s", he.Status, he.Body)
			if until, ok := amd.CooldownUntil(key); ok {
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
	amd.Uncool(key)
	result["reason"] = "ok"
	return result, "active"
}

// clearAMDKeysForDataDelete 清空 AMD key 池与绑定/路由/用量表（保留 baseURL
// 与平台设置）。
func clearAMDKeysForDataDelete() int {
	cur, next := amdCloneConfig()
	n := len(cur.Keys)
	next.Keys = nil
	next.KeyBindings = map[string]amd.Binding{}
	next.KeyRoutingEnabled = map[string]bool{}
	next.Usage = map[string]int64{}
	next.Cool = map[string]time.Time{}
	amd.Set(next)
	return n
}

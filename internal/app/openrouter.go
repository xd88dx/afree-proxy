package app

import (
	"afree-proxy/internal/kit"
	openrouter "afree-proxy/internal/openrouter"
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

// ============ OpenRouter 平台装配 ============
//
// OpenRouter 是纯 key 池平台（与 zen 同形态，无账号 OAuth）：多个 sk-or-v1-…
// key 按池调度策略轮转，未绑定 key 走共享代理池（代理池页下拉），隔离模式下
// 每 key 可绑主/辅出口。模型目录同步 openrouter.ai/api/v1/models 的免费
// 子集，客户端以 or:<model-id> 形式调用。

// orConfigPath OpenRouter 配置文件路径（DATA_DIR/.or-config.json）。
func orConfigPath() string { return kit.ResolveDataPath(".or-config.json") }

// initOpenRouter 加载配置 + 注入出站解析/HTTP client 工厂。StartProxy 早期
// 调用一次；失败只记日志不影响既有平台（与 WorkBuddy 子系统同口径）。
func initOpenRouter() {
	openrouter.Load(orConfigPath())
	openrouter.SetExitResolver(func(key string) (string, bool) {
		if key == "" {
			return "", true
		}
		main, backup, bound := openrouter.BindingOf(key)
		if !bound {
			// 未绑定 = 全局规则：面板选直连时不走池
			if !openrouter.ProxiesEnabled() {
				return "", true
			}
			url, _ := pickUpstreamProxy()
			return url, true
		}
		url, _, ok := pickBoundProxy(main, backup)
		return url, ok
	})
	openrouter.SetClientProvider(proxyClientFor)
	// 启动即预热模型目录（异步，失败保留种子）
	openrouter.InitCatalog()
	go startOrModelsRefresher()
}

// startOrModelsRefresher 每 5 分钟同步一次免费模型目录。上游偶发 TLS EOF
// （CF 对 uTLS 指纹连接的间歇重置，与 zen 注册表同现象），单次失败下一轮
// 自然重试，这里再补一次立即重试提高恢复速度。
func startOrModelsRefresher() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		syncOrCatalog(2)
	}
}

// syncOrCatalog 同步目录；attempts 为额外重试次数（不含首次）。
func syncOrCatalog(attempts int) {
	for i := 0; i <= attempts; i++ {
		added, err := openrouter.SyncCatalog()
		if err == nil {
			if added > 0 {
				log.Printf("openrouter model sync: %d new free models", added)
			}
			return
		}
		if i < attempts {
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		} else {
			log.Printf("openrouter model sync failed: %v", err)
		}
	}
}

// orModelList /v1/models 合并用的模型条目。
func orModelList() []map[string]any {
	out := make([]map[string]any, 0)
	for _, m := range openrouter.ModelList() {
		entry := map[string]any{
			"id":       openrouter.ModelPrefix + m.ID,
			"object":   "model",
			"created":  time.Now().UnixMilli(),
			"owned_by": "openrouter",
			"source":   "openrouter-free",
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

// orBoundRoutable 该 key 绑定出口是否可路由（隔离判定，注入 openrouter.PickKey）。
// 未绑定 key 恒可路由（走全局规则）；绑定代理已从池中删除/冷却中则跳过。
func orBoundRoutable(key string) bool {
	if !proxyIsolationEnabled() {
		return true
	}
	main, backup, bound := openrouter.BindingOf(key)
	if !bound {
		return true
	}
	return boundProxiesRoutable(main, backup)
}

// handleOrChat OpenRouter chat/completions 转发（网关 /v1/chat/completions 命中
// or: 前缀时调用）。非流式聚合 SSE、流式透传，统计与 cline/zen 同口径。
func handleOrChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	bare, _ := openrouter.StripPrefix(mustString(params["model"]))
	m, ok := openrouter.Resolve(bare)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not a free openrouter model", bare), "type": "invalid_request_error"},
		})
		return
	}
	if len(openrouter.Keys()) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"message": "No OpenRouter keys configured. Add keys on the OpenRouter page.", "type": "auth_error"},
		})
		return
	}
	isStream, _ := params["stream"].(bool)
	tracker := newZenStatsTracker(zenStatsRecord{
		TS:           time.Now().UnixMilli(),
		Upstream:     "openrouter",
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
	resp, rateLimited, err := openrouter.Chat(r.Context(), params, m.ID, true, getProxyConfig().Strategy, orBoundRoutable, nil)
	if err != nil {
		log.Printf("  openrouter api error: %v", err)
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

func mustString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ============ OpenRouter 管理 API ============

// GET /admin/api/openrouter/config
func handleOrConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	c := openrouter.Get()
	data := map[string]any{
		"useProxies":              openrouter.ProxiesEnabled(),
		"baseURL":                 c.BaseURL,
		"keys":                    openrouter.Keys(),
		"keyStates":               openrouter.KeyStatus(),
		"maxConcurrency":          c.MaxConcurrency,
		"retries":                 c.Retries,
		"models":                  orModelList(),
		"proxyIsolation":          proxyIsolationEnabled(),
		"proxyIsolationEnvLocked": proxyIsolationEnvLocked(),
		"backupProxyEnabled":      backupProxyEnabled(),
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// POST /admin/api/openrouter/config/update
func handleOrConfigUpdate(w http.ResponseWriter, r *http.Request) {
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
		UseProxies  *bool    `json:"useProxies"`
		BaseURL     *string  `json:"baseURL"`
		Keys        []string `json:"keys"`
		MaxConc     *int     `json:"maxConcurrency"`
		Retries     *int     `json:"retries"`
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	cur := openrouter.Get()
	next := &openrouter.Config{
		UseProxies:        cur.UseProxies,
		BaseURL:           cur.BaseURL,
		Keys:              append([]string(nil), cur.Keys...),
		KeyRoutingEnabled: map[string]bool{},
		KeyBindings:       map[string]openrouter.Binding{},
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
	if patch.UseProxies != nil {
		next.UseProxies = patch.UseProxies
	}
	if patch.BaseURL != nil {
		next.BaseURL = strings.TrimRight(strings.TrimSpace(*patch.BaseURL), "/")
	}
	if patch.MaxConc != nil && *patch.MaxConc > 0 {
		next.MaxConcurrency = *patch.MaxConc
	}
	if patch.Retries != nil && *patch.Retries >= 0 {
		next.Retries = *patch.Retries
	}
	keysChanged := false
	if patch.Keys != nil {
		if len(patch.Keys) == 0 {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "keys list is empty"})
			return
		}
		next.Keys = patch.Keys
		keysChanged = true
	}
	if keysChanged {
		// key 池整体替换：新 key 默认不参与路由（面板勾选启用后生效），
		// 移除的 key 清理表项（三平台统一口径）。
		next.ReconcileRouting(next, cur)
	}
	openrouter.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "OpenRouter config saved"})
}

// POST /admin/api/openrouter/keys/routing  body: { index, enabled }
func handleOrKeySetRouting(w http.ResponseWriter, r *http.Request) {
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
	if err := orSetKeyRouting(req.Index, req.Enabled); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d routing enabled=%v", req.Index+1, req.Enabled)})
}

// POST /admin/api/openrouter/keys/routing/all  body: { enabled }
func handleOrKeySetRoutingAll(w http.ResponseWriter, r *http.Request) {
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
	n := orSetAllKeysRouting(req.Enabled)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("routing enabled=%v on %d key(s)", req.Enabled, n)})
}

// POST /admin/api/openrouter/keys/reorder  body: { order: [oldIndex,...] }
func handleOrKeysReorder(w http.ResponseWriter, r *http.Request) {
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
	if err := orReorderKeys(req.Order); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"updated": len(req.Order)}})
}

// POST /admin/api/openrouter/keys/delete  body: { index }
func handleOrKeyDelete(w http.ResponseWriter, r *http.Request) {
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
	if err := orDeleteKey(req.Index); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d deleted", req.Index+1)})
}

// POST /admin/api/openrouter/keys/proxy  body: { index, main, backup }
func handleOrKeySetProxy(w http.ResponseWriter, r *http.Request) {
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
	if err := orSetKeyProxy(req.Index, req.Main, req.Backup); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Key proxy binding saved"})
}

// POST /admin/api/openrouter/keys/proxy/clear
func handleOrKeyClearProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	n := orClearAllKeyProxies()
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"cleared": n}})
}

// POST /admin/api/openrouter/keys/test  body: { index }
func handleOrKeyTest(w http.ResponseWriter, r *http.Request) {
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
	result, status := orTestKey(req.Index, req.Model)
	result["status"] = status
	log.Printf("Test openrouter key #%d: status=%s model=%v reason=%v", req.Index+1, status, result["model"], result["reason"])
	writeAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// POST /admin/api/openrouter/models/refresh
func handleOrModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	syncOrCatalog(2)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("synced, %d free models", len(openrouter.ModelList()))})
}

// ============ OpenRouter key 池操作（写时复制，与 zen 同口径） ============

func orCloneConfig() (*openrouter.Config, *openrouter.Config) {
	cur := openrouter.Get()
	next := &openrouter.Config{
		UseProxies:        cur.UseProxies,
		BaseURL:           cur.BaseURL,
		Keys:              append([]string(nil), cur.Keys...),
		KeyRoutingEnabled: map[string]bool{},
		KeyBindings:       map[string]openrouter.Binding{},
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

func orSetKeyRouting(index int, enabled bool) error {
	cur := openrouter.Get()
	if index < 0 || index >= len(cur.Keys) {
		return fmt.Errorf("key index out of range")
	}
	key := cur.Keys[index]
	_, next := orCloneConfig()
	next.KeyRoutingEnabled[key] = enabled
	openrouter.Set(next)
	return nil
}

func orSetAllKeysRouting(enabled bool) int {
	cur, next := orCloneConfig()
	n := 0
	for _, k := range cur.Keys {
		next.KeyRoutingEnabled[k] = enabled
		n++
	}
	openrouter.Set(next)
	return n
}

func orReorderKeys(order []int) error {
	cur := openrouter.Get()
	n := len(cur.Keys)
	if len(order) == 0 {
		return fmt.Errorf("order is required")
	}
	nextKeys := make([]string, 0, n)
	seen := make(map[int]bool, n)
	for _, idx := range order {
		if idx < 0 || idx >= n {
			return fmt.Errorf("key index out of range: %d", idx)
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
	_, next := orCloneConfig()
	next.Keys = nextKeys
	openrouter.Set(next)
	return nil
}

func orDeleteKey(index int) error {
	cur := openrouter.Get()
	if index < 0 || index >= len(cur.Keys) {
		return fmt.Errorf("key index out of range")
	}
	key := cur.Keys[index]
	_, next := orCloneConfig()
	next.Keys = append(next.Keys[:index], next.Keys[index+1:]...)
	delete(next.KeyBindings, key)
	delete(next.KeyRoutingEnabled, key)
	openrouter.Set(next)
	return nil
}

func orSetKeyProxy(index int, main, backup string) error {
	main = strings.TrimSpace(main)
	backup = normalizeBackupSlot(strings.TrimSpace(backup))
	if err := validateProxyBinding(main, backup); err != nil {
		return err
	}
	cur := openrouter.Get()
	if index < 0 || index >= len(cur.Keys) {
		return fmt.Errorf("key index out of range")
	}
	key := cur.Keys[index]
	_, next := orCloneConfig()
	if main == "" && backup == "" {
		delete(next.KeyBindings, key)
	} else {
		next.KeyBindings[key] = openrouter.Binding{Main: main, Backup: backup}
	}
	openrouter.Set(next)
	return nil
}

func orClearAllKeyProxies() int {
	cur, next := orCloneConfig()
	n := len(cur.KeyBindings)
	next.KeyBindings = map[string]openrouter.Binding{}
	openrouter.Set(next)
	return n
}

// orTestKey 对单个 key 发一个极小探测请求（与 cline/zen 的 Test 同语义）：
// 真实 2xx = 成功并清除冷却；401/403 = key 失效；429 = 限流（上报冷却与
// 预计恢复时刻）。返回 (结果, status)，status ∈ active/cooldown/error。
func orTestKey(index int, modelID string) (map[string]any, string) {
	cur := openrouter.Get()
	result := map[string]any{"index": index}
	if index < 0 || index >= len(cur.Keys) {
		result["reason"] = "key index out of range"
		return result, "error"
	}
	key := cur.Keys[index]
	result["keyMask"] = openrouter.MaskKey(key)
	bare := strings.TrimSpace(modelID)
	if bare != "" {
		if _, ok := openrouter.Resolve(bare); !ok {
			result["reason"] = fmt.Sprintf("unknown openrouter free model: %s", bare)
			return result, "error"
		}
	} else if pm, ok := openrouter.ProbeModel(); ok {
		bare = pm.ID
	} else {
		result["reason"] = "no openrouter free model available to probe"
		return result, "error"
	}
	result["model"] = bare

	params := map[string]any{
		"model":      openrouter.ModelPrefix + bare,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with exactly: OK"}},
		"max_tokens": 16,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = openrouter.WithPinKey(ctx, key)
	start := time.Now()
	resp, _, err := openrouter.Chat(ctx, params, bare, true, "fill", nil, nil)
	result["latencyMs"] = time.Since(start).Milliseconds()
	var he *openrouter.HTTPError
	if err != nil && errors.As(err, &he) {
		result["httpStatus"] = he.Status
		if he.RateLimited {
			result["reason"] = fmt.Sprintf("rate limited (HTTP %d): %s", he.Status, he.Body)
			if until, ok := openrouter.CooldownUntil(key); ok {
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
	openrouter.Uncool(key)
	result["reason"] = "ok"
	return result, "active"
}

// clearOpenrouterKeysForDataDelete 清空 OpenRouter key 池与绑定/路由/用量表
// （保留 baseURL 与平台设置）。
func clearOpenrouterKeysForDataDelete() int {
	cur, next := orCloneConfig()
	n := len(cur.Keys)
	next.Keys = nil
	next.KeyBindings = map[string]openrouter.Binding{}
	next.KeyRoutingEnabled = map[string]bool{}
	next.Usage = map[string]int64{}
	next.Cool = map[string]time.Time{}
	openrouter.Set(next)
	return n
}

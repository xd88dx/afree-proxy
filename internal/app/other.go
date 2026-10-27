package app

import (
	"afree-proxy/internal/kit"
	"afree-proxy/internal/other"
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

// ============ Other 平台装配 ============
//
// Other 页下挂多个子平台（小企业上游，自定义前缀）：每子平台独立 key 池 +
// 模型勾选目录，请求按 "<prefix>:<model-id>" 路由到对应 BaseURL。与 amd /
// tokenharbor 同构（key 轮转 / 401·429 冷却 / 主备代理绑定 / 面板暂存编辑），
// 区别是平台数量动态，管理 API 全部带 slug。

// otherConfigPath Other 配置文件路径（DATA_DIR/other-config.json）。
func otherConfigPath() string { return kit.ResolveDataPath("other-config.json") }

// initOther 加载配置 + 注入 HTTP client 工厂 + 启动周期同步。StartProxy 早期调用一次。
func initOther() {
	other.Load(otherConfigPath())
	other.SetClientProvider(proxyClientFor)
	// 出口成败/握手延迟回传（同 AMDCloud：此前无任何观测钩子）。
	other.SetProxyObserver(recordProxyOutcome)
	other.SetAttemptObserver(otherAttemptObserver)
	go startOtherModelsRefresher()
}

// otherAttemptObserver 是 other 的 SetAttemptObserver 实现。多子平台共用一
// 个注入点，故按 scope（子平台 slug）现查平台再走 keyPoolAttemptObserver 的
// 同款逻辑。slug 查不到（请求途中被删）时只记 key 序号，代理列留空。
func otherAttemptObserver(ctx context.Context, scope, key, proxyURL string) {
	if ctx == nil || key == "" {
		return
	}
	p := other.PlatformBySlug(scope)
	if p == nil {
		setUpstreamInfo(ctx, fmt.Sprintf("key#%d", other.KeyIndex(&other.Platform{}, key)))
		return
	}
	keyPoolAttemptObserver(
		func(k string) int { return other.KeyIndex(p, k) },
		func(k string) (string, string, bool) { return other.BindingOf(p, k) },
	)(ctx, key, proxyURL)
}

// startOtherModelsRefresher 每 5 分钟同步一次全部有 key 的子平台目录。上游偶发
// TLS EOF（与 amd/openrouter/tokenharbor 同现象），失败重试两次再放弃本轮。
func startOtherModelsRefresher() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		for _, p := range other.Get().Platforms {
			if len(p.Keys) > 0 {
				syncOtherPlatform(p.Slug, 2)
			}
		}
	}
}

// syncOtherPlatform 同步单个子平台目录；attempts 为额外重试次数（不含首次）。
func syncOtherPlatform(slug string, attempts int) {
	for i := 0; i <= attempts; i++ {
		p := other.PlatformBySlug(slug)
		if p == nil {
			return
		}
		changed, err := other.TrySync(p)
		if err == nil {
			if changed > 0 {
				log.Printf("other model sync (%s): %d model(s) updated", slug, changed)
			}
			return
		}
		if i < attempts {
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		} else {
			log.Printf("other model sync (%s) failed: %v", slug, err)
		}
	}
}

// ============ 网关侧 ============

// otherModelList /v1/models 合并用的模型条目（全部子平台勾选的模型，
// "<prefix>:<model-id>" 形式）。
func otherModelList() []map[string]any {
	out := make([]map[string]any, 0)
	for _, p := range other.Get().Platforms {
		// 展示门控：没有参与路由的启用 key 的子平台不出目录（与请求入口
		// 的可路由性闸同判定），否则客户端选中后必然 401/403。
		if !otherPlatformHasRoutableKey(&p) {
			continue
		}
		for _, m := range other.SelectedModels(&p) {
			out = append(out, map[string]any{
				"id":       p.Prefix + ":" + m.ID,
				"object":   "model",
				"created":  time.Now().UnixMilli(),
				"owned_by": "other-" + p.Slug,
				"source":   "other-" + p.Prefix,
				"status":   "active",
				"cost":     "free",
			})
		}
	}
	return out
}

// otherExitFn 平台未绑定 key 的出口解析（与 amd/tokenharbor 同语义）：
// 未绑定 = 全局规则（Other 全局开关关则直连，否则共享代理池轮转）；
// 绑定 = 主/备用出口，双不可用 = 本轮跳过（隔离优先于可用性）。
func otherExitFn(p *other.Platform) func(key string) (string, bool) {
	return func(key string) (string, bool) {
		if key == "" {
			return "", true
		}
		main, backup, bound := other.BindingOf(p, key)
		if !bound {
			if !other.ProxiesEnabled() {
				return "", true
			}
			url, _ := pickUpstreamProxy()
			return url, true
		}
		url, _, ok := pickBoundProxy(main, backup)
		return url, ok
	}
}

// otherBoundRoutable 该 key 绑定出口是否可路由（隔离判定）。
func otherBoundRoutable(p *other.Platform) func(key string) bool {
	return func(key string) bool {
		if !proxyIsolationEnabled() {
			return true
		}
		main, backup, bound := other.BindingOf(p, key)
		if !bound {
			return true
		}
		return boundProxiesRoutable(main, backup)
	}
}

// handleOtherChat Other chat/completions 转发（网关命中 <prefix>: 时调用）。
func handleOtherChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	model, _ := params["model"].(string)
	p, bare, ok := other.ResolvePrefix(model)
	if !ok || p == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not an Other platform model", model), "type": "invalid_request_error"},
		})
		return
	}
	m, ok := other.ResolveSelected(p, bare)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not enabled on platform %q — tick it in the model selector on the Other page", bare, p.Name), "type": "invalid_request_error"},
		})
		return
	}
	if len(p.Keys) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("No keys configured for platform %q. Add keys on the Other page.", p.Name), "type": "auth_error"},
		})
		return
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("Base URL is not configured for platform %q. Set it in the settings area on the Other page.", p.Name), "type": "api_error"},
		})
		return
	}
	isStream, _ := params["stream"].(bool)
	if isStream {
		handleStreamBufferedGeneric(w, r, "other:"+p.Slug, params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
			resp, _, err := other.Chat(ctx, p, params, m.ID, true, globalStrategy(), otherBoundRoutable(p), otherExitFn(p))
			if err != nil {
				return nil, nil, err
			}
			usedKey := takeUsedKey(resp)
			return resp, func(u map[string]any) { other.RecordKeyTokens(p, usedKey, usageTokens(u, params)) }, nil
		})
		return
	}
	resp, _, err := other.Chat(r.Context(), p, params, m.ID, true, globalStrategy(), otherBoundRoutable(p), otherExitFn(p))
	if err != nil {
		log.Printf("  other api error (%s): %v", p.Slug, err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": err.Error(), "type": "api_error"},
		})
		return
	}
	defer resp.Body.Close()
	usedKey := takeUsedKey(resp)
	usageFn := func(u map[string]any) {
		other.RecordKeyTokens(p, usedKey, usageTokens(u, params))
	}
	finishZenChatNonStream(w, resp, &ZenModel{ID: m.ID}, params, usageFn)
}

// ============ 管理 API ============

// GET /admin/api/other/config —— 全量状态（含每子平台 key 状态与模型目录）。
func handleOtherConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	platforms := make([]map[string]any, 0)
	for _, p := range other.Get().Platforms {
		platforms = append(platforms, platformAPI(&p))
	}
	data := map[string]any{
		"useProxies":              other.ProxiesEnabled(),
		"platforms":               platforms,
		"proxyIsolation":          proxyIsolationEnabled(),
		"proxyIsolationEnvLocked": proxyIsolationEnvLocked(),
		"backupProxyEnabled":      backupProxyEnabled(),
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// platformAPI 单个子平台的面板视图（key 明文永不出口；keyStates 用 index
// 定位，与 AMDCloud/TokenHarbor 的协议一致）。
func platformAPI(p *other.Platform) map[string]any {
	models := make([]string, 0)
	for _, m := range other.KnownModels(p) {
		models = append(models, m.ID)
	}
	return map[string]any{
		"slug":           p.Slug,
		"name":           p.Name,
		"prefix":         p.Prefix,
		"baseURL":        p.BaseURL,
		"keyStates":      other.KeyStatus(p),
		"maxConcurrency": p.MaxConcurrency,
		"retries":        p.Retries,
		"models":         models,
		"selectedModels": other.SelectedModelIDs(p),
		"manualModels":   append([]string(nil), p.Models.Manual...),
		"probeModel":     p.Models.Probe,
	}
}

// POST /admin/api/other/config/update —— 全局开关或平台字段补丁。
// body: { useProxies? } 或 { slug, name?, prefix?, baseURL?, maxConcurrency?, retries? }
func handleOtherConfigUpdate(w http.ResponseWriter, r *http.Request) {
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
		UseProxies     *bool   `json:"useProxies"`
		Slug           string  `json:"slug"`
		Name           *string `json:"name"`
		Prefix         *string `json:"prefix"`
		BaseURL        *string `json:"baseURL"`
		MaxConcurrency *int    `json:"maxConcurrency"`
		Retries        *int    `json:"retries"`
		Probe          *string `json:"probe"`
		ProbeModel     *string `json:"probeModel"` // probeModel 与 probe 同义（面板传 probeModel）
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if patch.Slug == "" {
		// 全局开关（Proxy pool 页 Other 列）
		if patch.UseProxies == nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "slug or useProxies is required"})
			return
		}
		// 走池开关存在 proxy-config.json 的代理池段，不写进平台文件。
		setPlatformUseProxies("other", patch.UseProxies)
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Other config saved"})
		return
	}
	cur := other.PlatformBySlug(patch.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + patch.Slug})
		return
	}
	next := other.ClonePlatform(cur)
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "name is required"})
			return
		}
		next.Name = name
	}
	if patch.Prefix != nil {
		p := other.NormalizePrefix(*patch.Prefix)
		if err := other.ValidatePrefix(p); err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
			return
		}
		c := other.Get()
		if name, conflict := c.PrefixConflict(p, next.Slug); conflict {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: fmt.Sprintf("prefix %q conflicts with platform %q", p, name)})
			return
		}
		next.Prefix = p
	}
	if patch.BaseURL != nil {
		base := strings.TrimRight(strings.TrimSpace(*patch.BaseURL), "/")
		if base == "" {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "base URL is required"})
			return
		}
		next.BaseURL = base
	}
	if patch.MaxConcurrency != nil && *patch.MaxConcurrency > 0 {
		next.MaxConcurrency = *patch.MaxConcurrency
	}
	if patch.Retries != nil && *patch.Retries >= 0 {
		next.Retries = *patch.Retries
	}
	if patch.Probe != nil {
		probe := strings.TrimSpace(*patch.Probe)
		if probe != "" && !other.ModelSelected(next, probe) {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "probe model must be an enabled model"})
			return
		}
		next.Models.Probe = probe
	} else if patch.ProbeModel != nil {
		probe := strings.TrimSpace(*patch.ProbeModel)
		if probe != "" && !other.ModelSelected(next, probe) {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "probe model must be an enabled model"})
			return
		}
		next.Models.Probe = probe
	}
	_, full := otherCloneConfig()
	for i := range full.Platforms {
		if full.Platforms[i].Slug == next.Slug {
			full.Platforms[i] = *next
			break
		}
	}
	other.Set(full)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Platform config saved"})
}

// POST /admin/api/other/platforms —— 新增子平台。
// body: { name, prefix, baseURL }
func handleOtherPlatformCreate(w http.ResponseWriter, r *http.Request) {
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
		Name    string `json:"name"`
		Prefix  string `json:"prefix"`
		BaseURL string `json:"baseURL"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "name is required"})
		return
	}
	prefix := other.NormalizePrefix(req.Prefix)
	if err := other.ValidatePrefix(prefix); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	// baseURL 允许创建后补配（配置区可改）；空值时同步/转发会显式报错
	base := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	cur := other.Get()
	if _, conflict := cur.PrefixConflict(prefix, ""); conflict {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: fmt.Sprintf("prefix %q already in use", prefix)})
		return
	}
	_, next := otherCloneConfig()
	next.Platforms = append(next.Platforms, other.Platform{
		Slug:           "op_" + kit.RandHex(4),
		Name:           name,
		Prefix:         prefix,
		BaseURL:        base,
		MaxConcurrency: 8,
		Retries:        3,
	})
	other.Set(next)
	var created *other.Platform
	if len(next.Platforms) > 0 {
		created = &next.Platforms[len(next.Platforms)-1] // 刚追加的那个
	}
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Platform created",
		Data:    map[string]any{"platform": platformAPI(created)},
	})
}

// POST /admin/api/other/platforms/delete —— 删除子平台（连带 key/绑定/目录）。
// body: { slug }
func handleOtherPlatformDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur, next := otherCloneConfig()
	idx := -1
	for i := range cur.Platforms {
		if cur.Platforms[i].Slug == req.Slug {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	name := cur.Platforms[idx].Name
	keys := len(cur.Platforms[idx].Keys)
	next.Platforms = append(next.Platforms[:idx], next.Platforms[idx+1:]...)
	other.Set(next)
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: fmt.Sprintf("Platform %q deleted (%d key(s) removed)", name, keys),
	})
}

// POST /admin/api/other/platforms/reorder —— 按给定 slug 顺序重排子平台。
// body: { slugs: ["op_a", "op_b", ...] }
// slug 集合必须与当前子平台集合完全一致（不新增、不遗漏、不重复），否则 400。
// 顺序直接落在 Config.Platforms 数组里，落盘即持久，无需额外 order 字段。
func handleOtherPlatformReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slugs []string `json:"slugs"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur, next := otherCloneConfig()
	if len(req.Slugs) != len(cur.Platforms) {
		writeAPI(w, http.StatusBadRequest, apiResponse{
			Error: fmt.Sprintf("slug count mismatch: got %d, want %d", len(req.Slugs), len(cur.Platforms)),
		})
		return
	}
	bySlug := make(map[string]other.Platform, len(cur.Platforms))
	for i := range cur.Platforms {
		bySlug[cur.Platforms[i].Slug] = cur.Platforms[i]
	}
	ordered := make([]other.Platform, 0, len(req.Slugs))
	seen := make(map[string]bool, len(req.Slugs))
	for _, s := range req.Slugs {
		p, ok := bySlug[s]
		if !ok || seen[s] {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown or duplicate slug: " + s})
			return
		}
		seen[s] = true
		ordered = append(ordered, p)
	}
	next.Platforms = ordered
	other.Set(next)
	apis := make([]map[string]any, 0, len(ordered))
	for i := range ordered {
		apis = append(apis, platformAPI(&next.Platforms[i]))
	}
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Platform order saved",
		Data:    map[string]any{"platforms": apis},
	})
}

// POST /admin/api/other/keys/update —— 追加 API key（面板"添加凭据"弹窗：
// 每行一个，按明文去重；新 key 默认不参与路由）。body: { slug, add: [key,...] }
// 也兼容整体替换的旧形态 { slug, keys: [...] }（keys 非空时优先整体替换）。
func handleOtherKeysUpdate(w http.ResponseWriter, r *http.Request) {
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
		Slug string   `json:"slug"`
		Add  []string `json:"add"`
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	hadKeys := len(cur.Keys) > 0
	next := other.ClonePlatform(cur)
	added := 0
	if len(req.Keys) > 0 {
		next.Keys = req.Keys // 整体替换（旧形态）
	} else {
		existing := map[string]bool{}
		for _, k := range next.Keys {
			existing[k] = true
		}
		for _, k := range req.Add {
			k = strings.TrimSpace(k)
			if k == "" || existing[k] {
				continue
			}
			existing[k] = true
			next.Keys = append(next.Keys, k)
			added++
		}
	}
	next.ReconcileRouting(cur)
	otherReplacePlatform(next)
	// 首次配 key 后立即同步目录（面板不必再点 Refresh；失败静默，5 分钟
	// 周期刷新与手动按钮仍可重试）
	if !hadKeys && len(next.Keys) > 0 {
		go syncOtherPlatform(req.Slug, 2)
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Keys saved", Data: map[string]any{"added": added}})
}

// POST /admin/api/other/keys/routing  body: { slug, index, enabled }
func handleOtherKeySetRouting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug    string `json:"slug"`
		Index   int    `json:"index"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	next := other.ClonePlatform(cur)
	next.KeyRoutingEnabled[cur.Keys[req.Index]] = req.Enabled
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d routing enabled=%v", req.Index+1, req.Enabled)})
}

// POST /admin/api/other/keys/routing/all  body: { slug, enabled }
func handleOtherKeySetRoutingAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug    string `json:"slug"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	next := other.ClonePlatform(cur)
	n := 0
	for _, k := range next.Keys {
		next.KeyRoutingEnabled[k] = req.Enabled
		n++
	}
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("routing enabled=%v on %d key(s)", req.Enabled, n)})
}

// POST /admin/api/other/keys/reorder  body: { slug, order: [oldIndex,...] }
func handleOtherKeysReorder(w http.ResponseWriter, r *http.Request) {
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
		Slug  string `json:"slug"`
		Order []int  `json:"order"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
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
	next := other.ClonePlatform(cur)
	next.Keys = nextKeys
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Order saved", Data: map[string]any{"updated": len(req.Order)}})
}

// POST /admin/api/other/keys/delete  body: { slug, index }
func handleOtherKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug  string `json:"slug"`
		Index int    `json:"index"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	next := other.ClonePlatform(cur)
	key := next.Keys[req.Index]
	next.Keys = append(next.Keys[:req.Index], next.Keys[req.Index+1:]...)
	delete(next.KeyBindings, key)
	delete(next.KeyRoutingEnabled, key)
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: fmt.Sprintf("key #%d deleted", req.Index+1)})
}

// POST /admin/api/other/keys/proxy  body: { slug, index, main, backup }
func handleOtherKeySetProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug   string `json:"slug"`
		Index  int    `json:"index"`
		Main   string `json:"main"`
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	main := strings.TrimSpace(req.Main)
	backup := strings.TrimSpace(req.Backup)
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	if req.Index < 0 || req.Index >= len(cur.Keys) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "key index out of range"})
		return
	}
	key := cur.Keys[req.Index]
	backup = resolveBackupSlot(backup, cur.KeyBindings[key].Backup)
	if err := validateProxyBinding(main, backup); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	next := other.ClonePlatform(cur)
	if main == "" && backup == "" {
		delete(next.KeyBindings, key)
	} else {
		next.KeyBindings[key] = other.Binding{Main: main, Backup: backup}
	}
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "Key proxy binding saved"})
}

// POST /admin/api/other/keys/proxy/clear  body: { slug }
func handleOtherKeyClearProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	next := other.ClonePlatform(cur)
	n := len(next.KeyBindings)
	next.KeyBindings = map[string]other.Binding{}
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"cleared": n}})
}

// POST /admin/api/other/keys/test  body: { slug, index, model }
func handleOtherKeyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug  string `json:"slug"`
		Index int    `json:"index"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	result, status := otherTestKey(req.Slug, req.Index, req.Model)
	result["status"] = status
	log.Printf("Test other key %s#%d: status=%s model=%v reason=%v", req.Slug, req.Index+1, status, result["model"], result["reason"])
	writeAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// POST /admin/api/other/models/refresh  body: { slug }
func handleOtherModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	p := other.PlatformBySlug(req.Slug)
	if p == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	// 与 AMD/TokenHarbor 同口径：该子平台无启用 key 时拒绝同步并给出提示
	//（前端 refreshOtherModels 已有同判定，此处兜底防直连 API）。
	if !otherPlatformHasRoutableKey(p) {
		writeAPI(w, http.StatusOK, apiResponse{
			Success: false,
			Message: "no enabled key on this platform — enable a key first, then refresh",
		})
		return
	}
	_, err := other.TrySync(p)
	if err != nil {
		writeAPI(w, http.StatusBadGateway, apiResponse{Error: err.Error()})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Catalog refreshed",
		Data:    map[string]any{"platform": platformAPI(other.PlatformBySlug(req.Slug))},
	})
}

// POST /admin/api/other/models/manual  body: { slug, model }
func handleOtherModelsManualAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug  string `json:"slug"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "model id is required"})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	next := other.ClonePlatform(cur)
	if !other.AddManual(next, model) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "model already known: " + model})
		return
	}
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Model added and enabled",
		Data:    map[string]any{"platform": platformAPI(other.PlatformBySlug(req.Slug))},
	})
}

// POST /admin/api/other/models/manual/delete  body: { slug, model }
func handleOtherModelsManualDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug  string `json:"slug"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	next := other.ClonePlatform(cur)
	if !other.RemoveManual(next, strings.TrimSpace(req.Model)) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "not a manual model"})
		return
	}
	delete(next.Models.Selected, strings.TrimSpace(req.Model))
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Manual model removed",
		Data:    map[string]any{"platform": platformAPI(other.PlatformBySlug(req.Slug))},
	})
}

// POST /admin/api/other/models/selected  body: { slug, selected: [modelId,...] }
func handleOtherModelsSelected(w http.ResponseWriter, r *http.Request) {
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
		Slug     string   `json:"slug"`
		Selected []string `json:"selected"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	cur := other.PlatformBySlug(req.Slug)
	if cur == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + req.Slug})
		return
	}
	next := other.ClonePlatform(cur)
	other.SetSelected(next, req.Selected)
	if next.Models.Probe != "" && !other.ModelSelected(next, next.Models.Probe) {
		next.Models.Probe = ""
	}
	otherReplacePlatform(next)
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Model selection saved",
		Data:    map[string]any{"platform": platformAPI(other.PlatformBySlug(req.Slug))},
	})
}

// ============ 备助 ============

// otherCloneConfig 写时复制：深拷贝全部子平台（含运行时表）。
func otherCloneConfig() (*other.Config, *other.Config) {
	cur := other.Get()
	next := &other.Config{}
	for i := range cur.Platforms {
		next.Platforms = append(next.Platforms, *other.ClonePlatform(&cur.Platforms[i]))
	}
	return cur, next
}

// otherReplacePlatform 把改过的平台克隆体放回全量配置并 Set。
func otherReplacePlatform(p *other.Platform) {
	_, full := otherCloneConfig()
	for i := range full.Platforms {
		if full.Platforms[i].Slug == p.Slug {
			full.Platforms[i] = *p
			break
		}
	}
	other.Set(full)
}

// otherTestKey 对单个 key 发一个极小探测请求（与 amd 的 Test 同语义）：
// 真实 2xx = 成功并清除冷却；401/403 = key 失效；429 = 限流。
func otherTestKey(slug string, index int, modelID string) (map[string]any, string) {
	result := map[string]any{"slug": slug, "index": index}
	p := other.PlatformBySlug(slug)
	if p == nil {
		result["reason"] = "platform not found"
		return result, "error"
	}
	if index < 0 || index >= len(p.Keys) {
		result["reason"] = "key index out of range"
		return result, "error"
	}
	key := p.Keys[index]
	result["keyMask"] = other.MaskKey(key)
	bare := strings.TrimSpace(modelID)
	if bare != "" {
		if _, ok := other.ResolveSelected(p, bare); !ok {
			result["reason"] = "model not enabled on this platform: " + bare
			return result, "error"
		}
	} else if pm, ok := other.ProbeModel(p); ok {
		bare = pm.ID
	} else {
		result["reason"] = "no enabled model to probe — tick models in the selector first"
		return result, "error"
	}
	result["model"] = bare

	params := map[string]any{
		"model":      p.Prefix + ":" + bare,
		"messages":   []any{map[string]any{"role": "user", "content": "Reply with exactly: OK"}},
		"max_tokens": 16,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx = other.WithPinKey(ctx, key)
	start := time.Now()
	resp, _, err := other.Chat(ctx, p, params, bare, true, "fill", otherBoundRoutable(p), otherExitFn(p))
	result["latencyMs"] = time.Since(start).Milliseconds()
	var he *other.HTTPError
	if err != nil && errors.As(err, &he) {
		result["httpStatus"] = he.Status
		if he.RateLimited {
			result["reason"] = fmt.Sprintf("rate limited (HTTP %d): %s", he.Status, he.Body)
			if until, ok := other.CooldownUntil(p, key); ok {
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
	other.Uncool(p, key)
	// Test 也是真实上游消耗：记账口径与 /v1 业务流量统一（2026-10-27 用户定）。
	probeRespTokens(resp, func(n int64) { other.RecordKeyTokens(p, key, n) })
	result["reason"] = "ok"
	return result, "active"
}

// clearOtherKeysForDataDelete 清空全部子平台的 key 池与绑定/路由表
// （保留平台本身、baseURL、前缀与模型勾选配置）。
func clearOtherKeysForDataDelete() int {
	_, next := otherCloneConfig()
	n := 0
	for i := range next.Platforms {
		n += len(next.Platforms[i].Keys)
		next.Platforms[i].Keys = nil
		next.Platforms[i].KeyBindings = map[string]other.Binding{}
		next.Platforms[i].KeyRoutingEnabled = map[string]bool{}
		next.Platforms[i].KeyTokens = map[string]other.KeyTokenStats{}
		next.Platforms[i].Cool = map[string]time.Time{}
	}
	other.Set(next)
	return n
}

// ============ 真实会话（Other 页"真实请求"按钮） ============
//
// 与 keys/test 的区别：test 固定 pin 单个 key、只发一句 "Reply with exactly:
// OK"、只看成功与否；这里用第一个已启用凭据发用户自定义内容，并把上游
// 回复文本回传给面板，便于人工确认模型行为。

// handleOtherLiveRequest POST /admin/api/other/chat  body: { slug, model?, prompt }
func handleOtherLiveRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Slug   string `json:"slug"`
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	otherLiveRequest(w, req.Slug, req.Model, req.Prompt)
}

// otherLiveRequest 执行一次真实会话并写回 JSON。三类前置条件缺失各自返回
// 明确 code，供面板给出对应中文提示（而不是笼统的失败）。
func otherLiveRequest(w http.ResponseWriter, slug, modelID, prompt string) {
	p := other.PlatformBySlug(slug)
	if p == nil {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "platform not found: " + slug, Data: map[string]any{"code": "no_platform"}})
		return
	}
	// 模型：显式指定则须已勾选，否则取第一个已勾选模型。
	bare := strings.TrimSpace(modelID)
	if bare != "" {
		if _, ok := other.ResolveSelected(p, bare); !ok {
			writeAPI(w, http.StatusBadRequest, apiResponse{
				Error: "model not enabled on this platform: " + bare,
				Data:  map[string]any{"code": "no_model"},
			})
			return
		}
	} else if pm, ok := other.ProbeModel(p); ok {
		bare = pm.ID
	} else {
		writeAPI(w, http.StatusBadRequest, apiResponse{
			Error: "no enabled model — tick models in the selector first",
			Data:  map[string]any{"code": "no_model"},
		})
		return
	}
	// 凭据：第一个已启用（参与路由）的 key。
	key := ""
	for i, k := range p.Keys {
		if p.RoutingEnabled(k) {
			key = k
			_ = i
			break
		}
	}
	if key == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{
			Error: "no enabled credential — enable a key in the pool first",
			Data:  map[string]any{"code": "no_key"},
		})
		return
	}

	msg := strings.TrimSpace(prompt)
	if msg == "" {
		msg = "Reply with exactly: OK"
	}
	params := map[string]any{
		"model":      p.Prefix + ":" + bare,
		"messages":   []any{map[string]any{"role": "user", "content": msg}},
		"max_tokens": 256,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	ctx = other.WithPinKey(ctx, key)
	start := time.Now()
	resp, _, err := other.Chat(ctx, p, params, bare, false, "fill", otherBoundRoutable(p), otherExitFn(p))
	latency := time.Since(start).Milliseconds()

	result := map[string]any{
		"model": bare, "keyMask": other.MaskKey(key), "latencyMs": latency,
	}
	if err != nil {
		var he *other.HTTPError
		if errors.As(err, &he) {
			result["httpStatus"] = he.Status
			result["reason"] = fmt.Sprintf("API %d: %s", he.Status, he.Body)
		} else {
			result["reason"] = "upstream call failed: " + err.Error()
		}
		log.Printf("Other live request %s model=%s key=%s: failed: %v", p.Slug, bare, other.MaskKey(key), err)
		writeAPI(w, http.StatusBadGateway, apiResponse{Success: false, Error: "upstream call failed", Data: result})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		result["httpStatus"] = resp.StatusCode
		result["reason"] = fmt.Sprintf("API %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		writeAPI(w, http.StatusBadGateway, apiResponse{Success: false, Error: "upstream call failed", Data: result})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	_ = json.Unmarshal(body, &parsed)
	text := ""
	if len(parsed.Choices) > 0 {
		text = parsed.Choices[0].Message.Content
		if text == "" {
			text = parsed.Choices[0].Delta.Content
		}
	}
	other.Uncool(p, key)
	result["reason"] = "ok"
	result["reply"] = strings.TrimSpace(text)
	log.Printf("Other live request %s model=%s key=%s: ok in %dms", p.Slug, bare, other.MaskKey(key), latency)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "ok", Data: result})
}

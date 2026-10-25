package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/tokenharbor"
)

// ============================================================================
// 模型黑名单（afree 本地功能）
//
// 背景：上游会返回「账号类型不支持该模型」这类**权限级**错误（WorkBuddy global
// 实测 code=11158：「model [gpt-6-luna] is not allowed for user type
// [personal_free_user]」）。它的性质是：
//
//   - 不是账号坏了（余额/会话都健康），换号重试没有意义——同等级的号都没权限；
//   - 不是上游抖动，重试一百次也是同一个答复；
//   - 但 /v1/models 照样把它列出来，客户端（含 new-api 这类渠道映射）会反复
//     选到它，每次都吃一次 503 与一轮无谓的凭据轮转。
//
// 处理：命中判据后把「平台 + 模型」拉黑 —— /v1/models 不再对外提供（客户端
// 选不到），面板「收费模型」区可见、可人工测试/解禁。解禁只是移出名单，
// 程序下次遇到同样原因仍会再次拉黑（不是永久豁免）。
//
// 落盘 DATA_DIR/.model-blacklist.json：拉黑是低频事件，即时落盘（不设周期）。
// ============================================================================

// 封禁原因统一文案（面板「封禁原因」列直接展示，不再放上游原话——
// 上游原始报文保留在 Detail 字段，悬停提示可见）。
const (
	BanReasonAuto   = "账号权限不足，自动封禁"
	BanReasonManual = "管理员手动封禁"
)

// 平台标识（与对外模型前缀对应）。
const (
	platWorkBuddy   = "workbuddy"
	platZen         = "zen"
	platCline       = "cline"
	platAMD         = "amd"
	platOpenRouter  = "openrouter"
	platTokenHarbor = "tokenharbor"
	platTrae        = "trae"
	platOther       = "other"
)

// modelBlacklistEntry 一条拉黑记录。Model 存**对外模型 ID**（含前缀，
// 如 wbgb:gpt-6-luna），与 /v1/models 的 id 同口径，过滤时直接比对。
type modelBlacklistEntry struct {
	Platform  string    `json:"platform"`
	Model     string    `json:"model"`
	Code      string    `json:"code,omitempty"`   // 上游业务码，如 11158
	Reason    string    `json:"reason"`           // 统一文案：自动/手动封禁
	Detail    string    `json:"detail,omitempty"` // 上游原始报文（悬停提示用，不进表格列）
	CreatedAt time.Time `json:"createdAt"`        // 加入黑名单时间
	LastAt    time.Time `json:"lastAt"`           // 最近一次命中
	Hits      int       `json:"hits"`             // 命中次数
}

type modelBlacklistFile struct {
	SavedAt time.Time                        `json:"savedAt"`
	Items   map[string]*modelBlacklistEntry `json:"items"`
}

var (
	modelBlacklistMu     sync.Mutex
	modelBlacklistItems  = map[string]*modelBlacklistEntry{}
	modelBlacklistLoaded bool
)

func modelBlacklistPath() string {
	return kit.ResolveDataPath(".model-blacklist.json")
}

func blacklistKey(platform, model string) string {
	return platform + "|" + model
}

func ensureModelBlacklistLoaded() {
	modelBlacklistMu.Lock()
	defer modelBlacklistMu.Unlock()
	if modelBlacklistLoaded {
		return
	}
	modelBlacklistLoaded = true
	raw, err := os.ReadFile(modelBlacklistPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("model blacklist: read %s: %v", modelBlacklistPath(), err)
		}
		return
	}
	var f modelBlacklistFile
	if json.Unmarshal(raw, &f) != nil {
		log.Printf("model blacklist: corrupt JSON at %s (starting clean)", modelBlacklistPath())
		return
	}
	if f.Items != nil {
		modelBlacklistItems = f.Items
	}
	// 旧版本落盘的条目 Reason 存的是上游原话；统一改写为标准文案
	//（判据：code=11158 或原因文案命中权限 marker）。Detail 老条目没有，留空。
	for _, e := range modelBlacklistItems {
		if e == nil || e.Reason == "" || e.Reason == BanReasonAuto || e.Reason == BanReasonManual {
			continue
		}
		if e.Code == "11158" || IsModelPermissionDenied(0, e.Reason) {
			e.Reason = BanReasonAuto
		}
	}
	if n := len(modelBlacklistItems); n > 0 {
		log.Printf("model blacklist: restored %d entry(ies) from %s", n, modelBlacklistPath())
	}
}

func flushModelBlacklist() {
	modelBlacklistMu.Lock()
	out := modelBlacklistFile{
		SavedAt: time.Now(),
		Items:   make(map[string]*modelBlacklistEntry, len(modelBlacklistItems)),
	}
	for k, e := range modelBlacklistItems {
		cp := *e
		out.Items[k] = &cp
	}
	modelBlacklistMu.Unlock()

	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Printf("model blacklist: marshal: %v", err)
		return
	}
	path := modelBlacklistPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		log.Printf("model blacklist: write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("model blacklist: replace %s: %v", path, err)
		_ = os.Remove(tmp)
	}
}

// BlacklistModel 把「平台 + 对外模型 ID」加入黑名单（自动拉黑路径，reason 传
// 统一文案 BanReasonAuto，detail 传上游原始报文）。已存在时只累加命中次数与
// 刷新最近时间（不重置 createdAt——面板展示的是首次被拉黑的时间）。
func BlacklistModel(platform, model, code, reason, detail string) {
	blacklistHit(platform, model, code, reason, detail)
}

// BanModel 管理员手动封禁：原因统一为 BanReasonManual。已存在时覆盖原因
// （手动封禁表达的是管理员最新意志）并累加命中次数。返回是否为新加入。
func BanModel(platform, model string) bool {
	platform = strings.TrimSpace(platform)
	model = strings.TrimSpace(model)
	if platform == "" || model == "" {
		return false
	}
	ensureModelBlacklistLoaded()
	modelBlacklistMu.Lock()
	k := blacklistKey(platform, model)
	isNew := modelBlacklistItems[k] == nil
	blacklistHitLocked(k, platform, model, "", BanReasonManual, "")
	modelBlacklistMu.Unlock()
	flushModelBlacklist()
	if isNew {
		log.Printf("model blacklist: +%s %s (manual ban)", platform, model)
	}
	return isNew
}

// blacklistHit 加锁包装：写 entry（新增或累加），flush 落盘。
func blacklistHit(platform, model, code, reason, detail string) {
	platform = strings.TrimSpace(platform)
	model = strings.TrimSpace(model)
	if platform == "" || model == "" {
		return
	}
	ensureModelBlacklistLoaded()
	modelBlacklistMu.Lock()
	blacklistHitLocked(blacklistKey(platform, model), platform, model, code, reason, detail)
	modelBlacklistMu.Unlock()
	flushModelBlacklist()
}

// blacklistHitLocked 实际写入（调用方须持有 modelBlacklistMu）。
func blacklistHitLocked(k, platform, model, code, reason, detail string) {
	now := time.Now()
	if e := modelBlacklistItems[k]; e != nil {
		e.Hits++
		e.LastAt = now
		if code != "" {
			e.Code = code
		}
		if reason != "" {
			e.Reason = kit.Truncate(reason, 300)
		}
		if detail != "" {
			e.Detail = kit.Truncate(detail, 2000)
		}
		return
	}
	modelBlacklistItems[k] = &modelBlacklistEntry{
		Platform:  platform,
		Model:     model,
		Code:      code,
		Reason:    kit.Truncate(reason, 300),
		Detail:    kit.Truncate(detail, 2000),
		CreatedAt: now,
		LastAt:    now,
		Hits:      1,
	}
	log.Printf("model blacklist: +%s %s (%s) %s", platform, model, code, kit.Truncate(reason, 120))
}

// IsModelBlacklisted 该对外模型 ID 是否已拉黑（按对外 ID 判，跨平台无歧义：
// 同一 ID 只可能属于一个平台）。
func IsModelBlacklisted(model string) bool {
	if model == "" {
		return false
	}
	ensureModelBlacklistLoaded()
	modelBlacklistMu.Lock()
	defer modelBlacklistMu.Unlock()
	for _, e := range modelBlacklistItems {
		if e.Model == model {
			return true
		}
	}
	return false
}

// UnblockModel 解禁：移出名单。仅此而已——后续若再次命中同样原因，会重新
// 被拉黑（不是永久豁免）。
func UnblockModel(platform, model string) bool {
	ensureModelBlacklistLoaded()
	modelBlacklistMu.Lock()
	k := blacklistKey(platform, model)
	_, existed := modelBlacklistItems[k]
	delete(modelBlacklistItems, k)
	modelBlacklistMu.Unlock()
	if existed {
		log.Printf("model blacklist: -%s %s (manual unblock)", platform, model)
		flushModelBlacklist()
	}
	return existed
}

// modelBlacklistSnapshot 面板表格数据（按加入时间倒序）。
func modelBlacklistSnapshot() []map[string]any {
	ensureModelBlacklistLoaded()
	modelBlacklistMu.Lock()
	rows := make([]map[string]any, 0, len(modelBlacklistItems))
	for _, e := range modelBlacklistItems {
		rows = append(rows, map[string]any{
			"platform":  e.Platform,
			"model":     e.Model,
			"code":      e.Code,
			"reason":    e.Reason,
			"detail":    e.Detail,
			"createdAt": e.CreatedAt.UTC().Format(time.RFC3339),
			"lastAt":    e.LastAt.UTC().Format(time.RFC3339),
			"hits":      e.Hits,
		})
	}
	modelBlacklistMu.Unlock()
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i]["createdAt"].(string) > rows[j]["createdAt"].(string)
	})
	return rows
}

// ============================================================================
// 权限级错误判据
// ============================================================================

// modelPermissionDeniedMarkers 上游「账号类型/等级不支持该模型」的确定性文案。
// 命中即拉黑：这类答复换号、重试都不会变（同等级凭据都没有权限）。
var modelPermissionDeniedMarkers = []string{
	"is not allowed for user type",
	"does not support this model",
	"account type does not support",
	"not available for your account type",
}

// IsModelPermissionDenied 判断上游响应是否属于「账号类型不支持该模型」。
// 判据：业务码 11158（WorkBuddy 实测）或文案命中 marker。
func IsModelPermissionDenied(status int, body string) bool {
	if body == "" {
		return false
	}
	if strings.Contains(body, "11158") {
		return true
	}
	lower := strings.ToLower(body)
	for _, m := range modelPermissionDeniedMarkers {
		if strings.Contains(lower, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// ============================================================================
// 自动拉黑：响应侧扫描
//
// 上游的「账号类型不支持该模型」不是某条链路独有的形态（各平台错误信封不同），
// 所以判据放在**网关出口**统一做：包装 ResponseWriter，只在 status>=400 时
// 把响应体 tee 进一个 64KB 小缓冲（成功/流式响应不缓冲，零额外开销），请求
// 结束后扫描权限判据 → 命中就把「平台 + 模型」拉黑。
// ============================================================================

// platformOfModel 按对外模型 ID 前缀判平台。裸模型名（cline 默认目录、combo
// 别名）返回空串 —— 这些 ID 不带平台信息，宁可不动也不误拉黑。
func platformOfModel(model string) string {
	switch {
	case strings.HasPrefix(model, wbRealmPrefixCN), strings.HasPrefix(model, wbRealmPrefixGlobal),
		strings.HasPrefix(model, vendorRealmPrefixCN), strings.HasPrefix(model, vendorRealmPrefixGlobal):
		return platWorkBuddy
	case strings.HasPrefix(model, "zen:"):
		return platZen
	case strings.HasPrefix(model, amd.ModelPrefix):
		return platAMD
	case strings.HasPrefix(model, openrouter.ModelPrefix):
		return platOpenRouter
	case strings.HasPrefix(model, tokenharbor.ModelPrefix):
		return platTokenHarbor
	}
	if i := strings.IndexByte(model, ':'); i > 0 {
		if p := other.PlatformByPrefix(model[:i]); p != nil {
			return platOther
		}
	}
	return ""
}

// modelErrorCapture 只捕获错误响应体的 ResponseWriter 包装。
type modelErrorCapture struct {
	http.ResponseWriter
	model  string
	status int
	wrote  bool
	buf    bytes.Buffer
}

func (c *modelErrorCapture) WriteHeader(code int) {
	if !c.wrote {
		c.status, c.wrote = code, true
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *modelErrorCapture) Write(b []byte) (int, error) {
	if !c.wrote {
		c.status, c.wrote = http.StatusOK, true
	}
	// 只留错误体（成功响应可能是 SSE 长流，绝不能缓冲）。
	if c.status >= 400 && c.buf.Len() < 64<<10 {
		c.buf.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

// Flush 透传，保证流式响应行为不变（包装层不能把 Flusher 吃掉）。
func (c *modelErrorCapture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// finish 请求结束后判定并拉黑（命中一次即够，重复命中只累加次数）。
func (c *modelErrorCapture) finish() {
	if c.buf.Len() == 0 || c.model == "" {
		return
	}
	body := c.buf.String()
	if !IsModelPermissionDenied(c.status, body) {
		return
	}
	platform := platformOfModel(c.model)
	if platform == "" {
		return
	}
	BlacklistModel(platform, c.model, businessCodeOf(body), BanReasonAuto, body)
}

// businessCodeOf 从错误体里取业务码（形如 "code":11158），仅用于展示。
// 网关错误信封常把上游 JSON 当字符串内嵌，此时业务码带转义（\"code\":11158），
// 故先解一层转义再找，找不到再用转义形态匹配一遍。
func businessCodeOf(body string) string {
	candidates := []string{body, strings.ReplaceAll(body, `\"`, `"`)}
	for _, src := range candidates {
		if code := businessCodeIn(src); code != "" {
			return code
		}
	}
	return ""
}

func businessCodeIn(body string) string {
	for _, key := range []string{`"code":`, `"code" :`} {
		off := 0
		for {
			i := strings.Index(body[off:], key)
			if i < 0 {
				break
			}
			i += off
			rest := strings.TrimLeft(body[i+len(key):], " ")
			j := 0
			for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			if j > 0 {
				return rest[:j]
			}
			// 网关信封里先出现的是字符串型业务码（如 "code":"no_healthy_account"），
			// 继续往后找第一个数字型业务码（内嵌上游 JSON 里的 11158）。
			off = i + len(key)
		}
	}
	return ""
}

// withModelBlacklistScan 在网关入口统一挂自动拉黑：先读出请求体取 model
//（用于判平台与记录），再把可捕获错误体的 writer 交给下游 handler。
// 同一入口顺带挂**封禁平台闸**（platform_blacklist.go）：平台被管理员封禁后
// 不参与任何会话路由，命中直接 503——放在读出 model 之后、分发之前，三个
// /chat/completions、/messages、/responses 入口一处收口。判定口径：combo 别名
// 按声明平台、带前缀按前缀平台、裸名按 Cline（routePlatformKeyOf；裸名 zen 兼容
// 解析是旧形态遗留，封 Cline 时一并被闸属 fail-closed 的可接受代价）。
func withModelBlacklistScan(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Body == nil {
			h(w, r)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			h(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var probe struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &probe)
		// 封禁平台闸：模型封禁逐个摘、平台封禁整平台拒（错误信封与网关其余
		// 错误同形；type=api_error 便于客户端与 402/429 等业务错误区分）。
		if plat := bannedPlatformOf(probe.Model); plat != "" {
			log.Printf("  platform banned: %q routed to %s (out of routing)", probe.Model, plat)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": map[string]string{
					"message": "model " + probe.Model + " belongs to platform " + plat +
						" which is banned by the admin; unban it in the admin panel (Gateway settings → Banned platforms)",
					"type": "api_error",
				},
			})
			return
		}
		// 平台可路由性闸：没有启用凭据（账号总闸/反代开关/key 启用列全关）
		// 的平台与封禁同等对待 —— 宁拒不路由（2026-10-26 用户定）。
		if plat := routePlatformKeyOf(probe.Model); !platformHasRoutableCredential(plat) {
			log.Printf("  platform unroutable: %q routed to %s (no enabled credential)", probe.Model, plat)
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": map[string]string{
					"message": "model " + probe.Model + " belongs to platform " + plat +
						" which has no enabled credential — enable an account/key (or its routing switch) on the platform page",
					"type": "api_error",
				},
			})
			return
		}
		cw := &modelErrorCapture{ResponseWriter: w, model: probe.Model}
		h(cw, r)
		cw.finish()
	}
}

// ============================================================================
// 管理 API
// ============================================================================

// GET /admin/api/models/blacklist
func handleModelBlacklist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"items": modelBlacklistSnapshot(),
	}})
}

// POST /admin/api/models/blacklist/unblock  body: { platform, model }
func handleModelBlacklistUnblock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if !UnblockModel(req.Platform, req.Model) {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "not in blacklist"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "unblocked"})
}

// POST /admin/api/models/blacklist/ban  body: { platform, model }
//
// 管理员手动封禁：把不想对外提供的模型加入黑名单（原因统一「管理员手动封禁」）。
// platform 缺省时按模型 ID 前缀推断；裸名推不出平台则拒绝（宁可不动也不误封）。
func handleModelBlacklistBan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = platformOfModel(req.Model)
	}
	if platform == "" || strings.TrimSpace(req.Model) == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "platform unknown and model has no platform prefix"})
		return
	}
	BanModel(platform, req.Model)
	log.Printf("Ban model %s %s (manual)", platform, req.Model)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "banned"})
}

// POST /admin/api/models/blacklist/test  body: { platform, model }
//
// 用该平台**第一个启用的凭据**对指定模型发一次真实请求：通了说明上游已放开
// （此时应点「解禁」），不通则展示上游原话。测试本身不解禁、不拉黑。
func handleModelBlacklistTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	result, status := testBlacklistModel(req.Platform, req.Model)
	result["status"] = status
	log.Printf("Test blacklisted model %s %s: status=%s reason=%v", req.Platform, req.Model, status, result["error"])
	writeAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// testBlacklistModel 按平台分派到各平台既有的探测实现（都取第一个启用凭据）。
func testBlacklistModel(platform, model string) (map[string]any, string) {
	bare := model
	if i := strings.IndexByte(model, ':'); i >= 0 {
		bare = model[i+1:]
	}
	switch platform {
	case platWorkBuddy:
		if workbuddySub == nil || workbuddySub.panel == nil {
			return map[string]any{"ok": false, "model": model, "error": "workbuddy subsystem not loaded"}, "error"
		}
		return workbuddySub.panel.ProbeModel(model)
	case platZen:
		cfg := getZenConfig()
		for i, k := range cfg.Keys {
			if k == "" || k == "public" || !zenKeyRoutingEnabled(k) {
				continue
			}
			return testZenKey(k, i, bare)
		}
		return map[string]any{"ok": false, "model": model, "error": "no enabled zen key"}, "error"
	case platAMD:
		for i, k := range amd.Keys() {
			if !amd.RoutingEnabled(k) {
				continue
			}
			return amdTestKey(i, bare)
		}
		return map[string]any{"ok": false, "model": model, "error": "no enabled amd key"}, "error"
	case platOpenRouter:
		for i, k := range openrouter.Keys() {
			if !openrouter.RoutingEnabled(k) {
				continue
			}
			return orTestKey(i, bare)
		}
		return map[string]any{"ok": false, "model": model, "error": "no enabled openrouter key"}, "error"
	case platTokenHarbor:
		for i, k := range tokenharbor.Keys() {
			if !tokenharbor.RoutingEnabled(k) {
				continue
			}
			return thTestKey(i, bare)
		}
		return map[string]any{"ok": false, "model": model, "error": "no enabled tokenharbor key"}, "error"
	case platTrae:
		return traeProbeBlacklistedModel(bare)
	case platOther:
		// 子平台按前缀定位：模型 ID 形如 <prefix>:<model>。
		pfx := model
		if i := strings.IndexByte(model, ':'); i >= 0 {
			pfx = model[:i]
		}
		p := other.PlatformByPrefix(pfx)
		if p == nil {
			return map[string]any{"ok": false, "model": model, "error": "unknown other sub-platform: " + pfx}, "error"
		}
		for i, k := range p.Keys {
			if !p.RoutingEnabled(k) {
				continue
			}
			return otherTestKey(p.Slug, i, bare)
		}
		return map[string]any{"ok": false, "model": model, "error": "no enabled key on " + p.Slug}, "error"
	case platCline:
		for _, a := range pool.Accounts {
			if !accountPoolEnabled(a) {
				continue
			}
			return testAccount(a)
		}
		return map[string]any{"ok": false, "model": model, "error": "no enabled cline account"}, "error"
	}
	return map[string]any{"ok": false, "model": model, "error": "unknown platform: " + platform}, "error"
}

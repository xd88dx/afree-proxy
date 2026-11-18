package app

import (
	"afree-proxy/internal/blacklist"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/other"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// blacklist_gateway.go —— 黑名单的网关闸面（P4 从 blacklist 域拆回）：
// withModelBlacklistScan 包裹/错误捕获/业务码解析属网关职责，未来 P4-gateway
// 批并入 gateway 包；存储与判定在 internal/blacklist。

// ============================================================================
// 自动拉黑：响应侧扫描
//
// 上游的「账号类型不支持该模型」不是某条链路独有的形态（各平台错误信封不同），
// 所以判据放在**网关出口**统一做：包装 ResponseWriter，只在 status>=400 时
// 把响应体 tee 进一个 64KB 小缓冲（成功/流式响应不缓冲，零额外开销），请求
// 结束后扫描权限判据 → 命中就把「平台 + 模型」拉黑。
// ============================================================================

// PlatformOfModel 按对外模型 ID 前缀判平台。裸模型名（cline 默认目录、combo
// 别名）返回空串 —— 这些 ID 不带平台信息，宁可不动也不误拉黑。
type ModelErrorCapture struct {
	http.ResponseWriter
	model  string
	status int
	wrote  bool
	buf    bytes.Buffer
}

// BusinessCodeOf 从错误体里取业务码（形如 "code":11158），仅用于展示。
// 网关错误信封常把上游 JSON 当字符串内嵌，此时业务码带转义（\"code\":11158），
// 故先解一层转义再找，找不到再用转义形态匹配一遍。
func WithModelBlacklistScan(h http.HandlerFunc) http.HandlerFunc {
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
		// ALLOW_BANNED=true 时本闸退化为仅目录摘除：请求照常路由（调试/灰度）。
		if !gwconfig.AllowBanned() {
			if plat := BannedPlatformOf(probe.Model); plat != "" {
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
		}
		// 平台可路由性闸：没有启用凭据（账号总闸/反代开关/key 启用列全关）
		// 的平台与封禁同等对待 —— 宁拒不路由（2026-10-26 用户定）。
		if plat := ModelRoutePlatformKey(probe.Model); !platformHasRoutableCredential(plat) {
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
		// 模型服务闸（2026-10-31 用户定）：已拉黑模型与不在对外目录的模型
		// 在请求路径直接拒，不再透传上游 —— 目录里能看到的才能请求到，
		// 与 /v1/models 严格同口径（见 servedModelGate）。
		if status, msg := servedModelGate(probe.Model); status != 0 {
			log.Printf("  model gate reject: %q -> %d", probe.Model, status)
			writeJSON(w, status, map[string]any{
				"error": map[string]string{"message": msg, "type": "invalid_request_error"},
			})
			return
		}
		cw := &ModelErrorCapture{ResponseWriter: w, model: probe.Model}
		h(cw, r)
		cw.finish()
	}
}

// RoutePlatformKeyOf 按对外模型 ID 推导封禁键：PlatformOfModel 的细粒度版。
// 区别有两处——Other 细化到子平台（other:<slug>），裸名按 Cline 处理（/v1/models
// 与 chat 路由里裸名 = Cline 免费目录；combo 别名由调用方先行解析）。
// 返回 "" 表示无法判定（放行，交由各平台的未知模型报错路径处理）。

func handlePlatformBlacklistBan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if !blacklist.PlatformBanKeyValid(strings.TrimSpace(req.Platform)) {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown platform: " + req.Platform})
		return
	}
	blacklist.BanPlatform(req.Platform)
	log.Printf("Ban platform %s (manual)", req.Platform)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "banned"})
}

// POST /admin/api/platforms/blacklist/unban  body: { platform }
func handlePlatformBlacklistUnban(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if !blacklist.UnbanPlatform(req.Platform) {
		WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "not banned"})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "unbanned"})
}

// ============================================================================
// 管理 API
// ============================================================================

// GET /admin/api/models/blacklist
func handleModelBlacklist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"items": modelBlacklistSnapshot(),
	}})
}

// POST /admin/api/models/blacklist/unblock  body: { platform, model }

// POST /admin/api/models/blacklist/unblock  body: { platform, model }
func handleModelBlacklistUnblock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	// scope 参数兼容读取但忽略（2026-11-16 wb 化后封禁无档位维度）；解禁按
	// (platform, model) 精确键删除，与「这个模型别封了」的面板语义一致。
	if !blacklist.UnblockModel(req.Platform, req.Model) {
		// 无条目可删时如实告知（success=false + message）：此前一律回 success，
		// 前端无条件弹「已解禁」，用户以为解禁成功、刷新后条目还在（2026-10-28）。
		WriteAPI(w, http.StatusOK, apiResponse{Success: false, Message: "not in blacklist"})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "unblocked"})
}

// platformModelIDs 枚举平台全部对外模型 ID（与 /v1/models 各平台段同源，
// 不做黑名单过滤——批量封禁要能重新命中已封禁条目做幂等跳过）。
// combo 别名不在内：封禁表达的是对真实模型的意志，别名只是虚拟入口。

// POST /admin/api/models/blacklist/batch-ban  body: { platform, keyword }
//
// 批量封禁（2026-10-31 用户定）：面板「封禁模型」表格下方的平台下拉 +
// 关键字输入（大小写不敏感）+ 批量封禁按钮。例如 platform=trae、
// keyword=doubao → trae 目录里所有 ID 含 "doubao" 的模型全部封禁。
// 空平台/空关键字拒绝（空关键字会匹配全部模型，属于危险操作）。
func handleModelBlacklistBatchBan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
		Keyword  string `json:"keyword"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid json"})
		return
	}
	platform := strings.TrimSpace(req.Platform)
	keyword := strings.TrimSpace(req.Keyword)
	if platform == "" || keyword == "" {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "platform and keyword are required"})
		return
	}
	matched, banned := BatchBanPlatformModels(platform, keyword)
	log.Printf("  batch ban: platform=%s keyword=%q matched=%d banned=%d", platform, keyword, matched, len(banned))
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"matched": matched,
		"banned":  len(banned),
		"models":  banned,
	}})
}

// POST /admin/api/models/blacklist/ban  body: { platform, model }
//
// 管理员手动封禁：把不想对外提供的模型加入黑名单（原因统一「管理员手动封禁」）。
// platform 缺省时按模型 ID 前缀推断；裸名推不出平台则拒绝（宁可不动也不误封）。

// POST /admin/api/models/blacklist/ban  body: { platform, model }
//
// 管理员手动封禁：把不想对外提供的模型加入黑名单（原因统一「管理员手动封禁」）。
// platform 缺省时按模型 ID 前缀推断；裸名推不出平台则拒绝（宁可不动也不误封）。
func handleModelBlacklistBan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = platformOfModel(req.Model)
	}
	if platform == "" || strings.TrimSpace(req.Model) == "" {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "platform unknown and model has no platform prefix"})
		return
	}
	// scope 已废除（2026-11-16 wb 化）：trae 对外 ID（tacn:/tagb: 前缀）本身
	// 编码合并域，按对外 ID 封禁即可，档位维度不再存在。旧面板传来的 scope
	// 字段直接忽略。
	blacklist.BanModel(platform, req.Model)
	log.Printf("Ban model %s %s (manual)", platform, req.Model)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "banned"})
}

// POST /admin/api/models/blacklist/test  body: { platform, model }
//
// 用该平台**第一个启用的凭据**对指定模型发一次真实请求：通了说明上游已放开
// （此时应点「解禁」），不通则展示上游原话。测试本身不解禁、不拉黑。

// POST /admin/api/models/blacklist/test  body: { platform, model }
//
// 用该平台**第一个启用的凭据**对指定模型发一次真实请求：通了说明上游已放开
// （此时应点「解禁」），不通则展示上游原话。测试本身不解禁、不拉黑。
func handleModelBlacklistTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	result, status := testBlacklistModel(r.Context(), req.Platform, req.Model)
	result["status"] = status
	log.Printf("Test blacklisted model %s %s: status=%s reason=%v", req.Platform, req.Model, status, result["error"])
	WriteAPI(w, http.StatusOK, apiResponse{
		Success: status == "active",
		Message: status,
		Data:    result,
	})
}

// testBlacklistModel 按平台分派到各平台既有的探测实现（都取第一个启用凭据）。
// ctx 透传给 cline 的 testAccount（其他平台探测实现尚不接收 ctx）。

// testBlacklistModel 按平台分派到各平台既有的探测实现（都取第一个启用凭据）。
// ctx 透传给 cline 的 testAccount（其他平台探测实现尚不接收 ctx）。
func testBlacklistModel(ctx context.Context, platform, model string) (map[string]any, string) {
	bare := model
	if i := strings.IndexByte(model, ':'); i >= 0 {
		bare = model[i+1:]
	}
	switch platform {
	case platWorkBuddy:
		if workbuddySub == nil || (*workbuddySub).Panel == nil {
			return map[string]any{"ok": false, "model": model, "error": "workbuddy subsystem not loaded"}, "error"
		}
		return (*workbuddySub).Panel.ProbeModel(model)
	case platZen:
		cfg := getZenConfig()
		for i, k := range cfg.Keys {
			if k == "" || !zenKeyRoutingEnabled(k) {
				continue
			}
			return testZenKey(k, i, bare)
		}
		return map[string]any{"ok": false, "model": model, "error": "no enabled zen key"}, "error"
	case platTrae:
		return trae2apiProbeBlacklistedModel(model)
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
		// 与可路由性闸同口径（Status=active 且 PoolEnabled），且按池锁读取快照：
		// 裸读 pool.Accounts 会与面板的写时复制替换竞态。
		p := loadPool()
		poolMu.Lock()
		var acc *Account
		for _, a := range p.Accounts {
			if a != nil && accountStatusLocked(a) == "active" && accountPoolEnabled(a) {
				acc = a
				break
			}
		}
		poolMu.Unlock()
		if acc != nil {
			return testAccount(ctx, acc)
		}
		return map[string]any{"ok": false, "model": model, "error": "no enabled cline account"}, "error"
	}
	return map[string]any{"ok": false, "model": model, "error": "unknown platform: " + platform}, "error"
}

// ============================================================================
// 管理 API
// ============================================================================

// GET /admin/api/platforms/blacklist
func handlePlatformBlacklist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"items": platformBanSnapshot(),
	}})
}

// POST /admin/api/platforms/blacklist/ban  body: { platform }

func (c *ModelErrorCapture) WriteHeader(code int) {
	if !c.wrote {
		c.status, c.wrote = code, true
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *ModelErrorCapture) Write(b []byte) (int, error) {
	if !c.wrote {
		c.status, c.wrote = http.StatusOK, true
	}
	// 只留错误体（成功响应可能是 SSE 长流，绝不能缓冲）。
	if c.status >= 400 && c.buf.Len() < 64<<10 {
		c.buf.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

func (c *ModelErrorCapture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *ModelErrorCapture) finish() {
	if c.buf.Len() == 0 || c.model == "" {
		return
	}
	body := c.buf.String()
	platform := platformOfModel(c.model)
	if platform == "" {
		return
	}
	if platform == platCline {
		// cline 上游对免费目录已移除的模型（官方 feed 缩水后目录残留）固定回
		// 404 {"error":"model not found"}；网关错误信封是 500 内嵌 "API 404: ..."，
		// 状态码不可用，只认文案。不放进 IsModelPermissionDenied 全局判据：
		// trae 的封禁按合并域（tacn/tagb）查询（TraeModelBannedIn），全局文案若
		// 对 trae 也按全档拉黑会破坏合并域语义（合并域条目或全档条目才算命中）。
		// 原因用 blacklist.BanReasonFeedRemoved：这不是账号档位问题，是官方免费 feed
		// 已把模型移走（2026-10-31 实测移入付费 clinePass 档）。
		if !strings.Contains(strings.ToLower(body), "model not found") {
			return
		}
		BlacklistModel(platform, c.model, businessCodeOf(body), blacklist.BanReasonFeedRemoved, body)
		return
	}
	if !IsModelPermissionDenied(c.status, body) {
		return
	}
	BlacklistModel(platform, c.model, businessCodeOf(body), blacklist.BanReasonAuto, body)
}

// BatchBanPlatformModels 批量封禁：平台目录里 ID 包含关键字（大小写不敏感）
// 的模型全部加入黑名单。已在名单里的条目保持原样（不覆盖自动封禁原因）。
// 返回命中数与新封禁的 ID 列表。
func BatchBanPlatformModels(platform, keyword string) (matched int, banned []string) {
	lk := strings.ToLower(keyword)
	for _, id := range platformModelIDs(platform) {
		if !strings.Contains(strings.ToLower(id), lk) {
			continue
		}
		matched++
		if BanModel(platform, id) {
			banned = append(banned, id)
		}
	}
	return matched, banned
}

func platformModelIDs(platform string) []string {
	var rows []map[string]any
	switch platform {
	case platCline:
		rows = apiModelList()
	case platZen:
		for _, zm := range zenModelList() {
			if id, _ := zm["id"].(string); id != "" {
				rows = append(rows, map[string]any{"id": id})
			}
		}
	case platWorkBuddy:
		rows = workbuddyModelEntries()
	case platTrae:
		rows = trae2apiModelEntries()
	case platOther:
		rows = otherModelList()
	}
	ids := make([]string, 0, len(rows))
	for _, m := range rows {
		if id, _ := m["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

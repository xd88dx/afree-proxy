package app

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/tokenharbor"
	"afree-proxy/internal/trae"
)

// ============================================================================
// 平台封禁（afree 本地功能）
//
// 与模型黑名单（model_blacklist.go）同一族：模型黑名单逐模型摘除，本文件
// 整平台摘除。场景：上游官方改版导致某平台反代整体失灵、所有账号/key 都在
// 冷却（如 OpenCode 改版实测），此时封禁整个平台——
//
//   - 路由：/v1/models 不再列出该平台模型（客户端选不到）；/v1/chat/completions、
//     /v1/messages、/v1/responses 命中该平台模型直接 503（withModelBlacklistScan
//     入口闸，与自动拉黑扫描同一包裹层）。
//   - 维护：签到/保活/余额刷新等任务不受影响（那是 WorkBuddy 账号开关的辖域；
//     平台封禁只断会话路由）。平台页面/模型目录照常显示，便于解封后恢复。
//
// 落盘 DATA_DIR/.platform-blacklist.json：低频管理操作，即时落盘。
// 封禁键 = 模型黑名单的 platform 维度：cline / zen / workbuddy / amd /
// openrouter / tokenharbor / other:<slug>（Other 细化到子平台）。
// ============================================================================

type platformBanEntry struct {
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"createdAt"`
}

type platformBanFile struct {
	SavedAt   time.Time                    `json:"savedAt"`
	Platforms map[string]*platformBanEntry `json:"platforms"`
}

var (
	platformBanMu     sync.Mutex
	platformBanItems  = map[string]*platformBanEntry{}
	platformBanLoaded bool
)

func ensurePlatformBanLoaded() {
	platformBanMu.Lock()
	defer platformBanMu.Unlock()
	if platformBanLoaded {
		return
	}
	platformBanLoaded = true
	globalCfgMu.Lock()
	g := loadGlobalConfigLocked()
	if len(g.PlatformBlacklist) > 0 {
		platformBanItems = make(map[string]*platformBanEntry, len(g.PlatformBlacklist))
		for k, e := range g.PlatformBlacklist {
			platformBanItems[k] = e
		}
	}
	globalCfgMu.Unlock()
	if n := len(platformBanItems); n > 0 {
		log.Printf("platform blacklist: restored %d platform(s) from %s", n, globalConfigPath())
	}
}

func flushPlatformBan() {
	platformBanMu.Lock()
	out := make(map[string]*platformBanEntry, len(platformBanItems))
	for k, e := range platformBanItems {
		cp := *e
		out[k] = &cp
	}
	platformBanMu.Unlock()

	mutateGlobalConfig(func(g *globalConfigData) { g.PlatformBlacklist = out })
}

// platformBanKeyValid 校验封禁键：六个基础平台或存在的 Other 子平台
// （other:<slug>）。未知键拒绝——拼写错误静默入库等于封了个寂寞。
func platformBanKeyValid(key string) bool {
	switch key {
	case platCline, platZen, platWorkBuddy, platAMD, platOpenRouter, platTokenHarbor, platTrae:
		return true
	}
	if slug, ok := strings.CutPrefix(key, "other:"); ok && slug != "" {
		return other.PlatformBySlug(slug) != nil
	}
	return false
}

// IsPlatformBanned 报告平台是否已被管理员封禁（封禁键见文件头注释）。
func IsPlatformBanned(platform string) bool {
	if platform == "" {
		return false
	}
	ensurePlatformBanLoaded()
	platformBanMu.Lock()
	defer platformBanMu.Unlock()
	return platformBanItems[platform] != nil
}

// BanPlatform 封禁平台（已封禁时幂等返回 false）。子平台封禁只影响该子平台；
// 基础平台封禁覆盖其全部对外模型。
func BanPlatform(platform string) bool {
	platform = strings.TrimSpace(platform)
	if !platformBanKeyValid(platform) {
		return false
	}
	ensurePlatformBanLoaded()
	platformBanMu.Lock()
	_, existed := platformBanItems[platform]
	if !existed {
		platformBanItems[platform] = &platformBanEntry{Platform: platform, CreatedAt: time.Now()}
	}
	platformBanMu.Unlock()
	if existed {
		return false
	}
	flushPlatformBan()
	log.Printf("platform blacklist: +%s (platform banned, out of routing)", platform)
	return true
}

// UnbanPlatform 解封平台（未封禁时幂等返回 false）。
func UnbanPlatform(platform string) bool {
	platform = strings.TrimSpace(platform)
	ensurePlatformBanLoaded()
	platformBanMu.Lock()
	_, existed := platformBanItems[platform]
	delete(platformBanItems, platform)
	platformBanMu.Unlock()
	if !existed {
		return false
	}
	flushPlatformBan()
	log.Printf("platform blacklist: -%s (platform unbanned)", platform)
	return true
}

// platformBanSnapshot 面板数据（按封禁时间倒序）。
func platformBanSnapshot() []map[string]any {
	ensurePlatformBanLoaded()
	platformBanMu.Lock()
	rows := make([]map[string]any, 0, len(platformBanItems))
	for _, e := range platformBanItems {
		rows = append(rows, map[string]any{
			"platform":  e.Platform,
			"createdAt": e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	platformBanMu.Unlock()
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i]["createdAt"].(string) > rows[j]["createdAt"].(string)
	})
	return rows
}

// exportPlatformBlacklist 平台封禁表快照（配置导出用；锁内拷贝）。
func exportPlatformBlacklist() map[string]*platformBanEntry {
	ensurePlatformBanLoaded()
	platformBanMu.Lock()
	defer platformBanMu.Unlock()
	if len(platformBanItems) == 0 {
		return nil
	}
	out := make(map[string]*platformBanEntry, len(platformBanItems))
	for k, v := range platformBanItems {
		if v == nil {
			continue
		}
		cp := *v
		out[k] = &cp
	}
	return out
}

// importPlatformBlacklist 整体替换平台封禁表并即时落盘（与模型封禁导入同语义：
// 备份里没有的段不动；有则连清空也生效）。无效键跳过，不阻断其余段导入。
func importPlatformBlacklist(in map[string]*platformBanEntry) {
	ensurePlatformBanLoaded()
	next := make(map[string]*platformBanEntry, len(in))
	for k, v := range in {
		if k == "" || v == nil || !platformBanKeyValid(k) {
			continue
		}
		e := *v
		e.Platform = k // key 为准，条目内 platform 字段仅展示
		next[k] = &e
	}
	platformBanMu.Lock()
	platformBanItems = next
	platformBanMu.Unlock()
	flushPlatformBan()
	log.Printf("  config import: platform blacklist replaced (%d platforms)", len(next))
}

// ============================================================================
// 模型 ID → 封禁键
// ============================================================================

// routePlatformKeyOf 按对外模型 ID 推导封禁键：platformOfModel 的细粒度版。
// 区别有两处——Other 细化到子平台（other:<slug>），裸名按 Cline 处理（/v1/models
// 与 chat 路由里裸名 = Cline 免费目录；combo 别名由调用方先行解析）。
// 返回 "" 表示无法判定（放行，交由各平台的未知模型报错路径处理）。
func routePlatformKeyOf(model string) string {
	model = strings.TrimSpace(model)
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
	case strings.HasPrefix(model, trae.ModelPrefix):
		return platTrae
	}
	if i := strings.IndexByte(model, ':'); i > 0 {
		if p := other.PlatformByPrefix(model[:i]); p != nil {
			return "other:" + p.Slug
		}
		return ""
	}
	// 裸名：Cline 免费目录（zen 裸名兼容解析是旧形态遗留，封 Cline 时一并
	// 被闸住是 fail-closed 的可接受代价，见 withModelBlacklistScan 闸注释）。
	return platCline
}

// bannedPlatformOf 返回模型将被路由到的平台封禁键（combo 别名按其声明平台、
// 其余按模型 ID 前缀判，裸名 = Cline）；该平台未封禁或无法判定时返回 ""。
// /v1/models 过滤与网关入口闸共用本判定。
func bannedPlatformOf(model string) string {
	if model == "" {
		return ""
	}
	plat := ""
	if c := resolveCombo(model); c != nil {
		plat = c.Platform
	} else {
		plat = routePlatformKeyOf(model)
	}
	if plat == "" || !IsPlatformBanned(plat) {
		return ""
	}
	return plat
}

// ============================================================================
// 管理 API
// ============================================================================

// GET /admin/api/platforms/blacklist
func handlePlatformBlacklist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"items": platformBanSnapshot(),
	}})
}

// POST /admin/api/platforms/blacklist/ban  body: { platform }
func handlePlatformBlacklistBan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if !platformBanKeyValid(strings.TrimSpace(req.Platform)) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown platform: " + req.Platform})
		return
	}
	BanPlatform(req.Platform)
	log.Printf("Ban platform %s (manual)", req.Platform)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "banned"})
}

// POST /admin/api/platforms/blacklist/unban  body: { platform }
func handlePlatformBlacklistUnban(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if !UnbanPlatform(req.Platform) {
		writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "not banned"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "unbanned"})
}

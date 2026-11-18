package blacklist

import (
	"afree-proxy/internal/gwconfig"

	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/other"
)

// ============================================================================
// 平台封禁（afree 本地功能）
//
// 与模型黑名单（model_blacklist.go）同一族：模型黑名单逐模型摘除，本文件
// 整平台摘除。场景：上游官方改版导致某平台反代整体失灵、所有账号/key 都在
// 冷却（如 OpenCode 改版实测），此时封禁整个平台——
//
//   - 路由：/v1/models 不再列出该平台模型（客户端选不到）；/v1/chat/completions、
//     /v1/messages、/v1/responses 命中该平台模型直接 503（WithModelBlacklistScan
//     入口闸，与自动拉黑扫描同一包裹层）。
//   - 维护：签到/保活/余额刷新等任务不受影响（那是 WorkBuddy 账号开关的辖域；
//     平台封禁只断会话路由）。平台页面/模型目录照常显示，便于解封后恢复。
//
// 落盘 DATA_DIR/.platform-blacklist.json：低频管理操作，即时落盘。
// 封禁键 = 模型黑名单的 platform 维度：cline / zen / workbuddy /
// other:<slug>（Other 细化到子平台）。
// ============================================================================

var (
	PlatformBanMu     sync.Mutex
	PlatformBanItems  = map[string]*gwconfig.PlatformBanEntry{}
	PlatformBanLoaded bool
)

func EnsurePlatformBanLoaded() {
	PlatformBanMu.Lock()
	defer PlatformBanMu.Unlock()
	if PlatformBanLoaded {
		return
	}
	PlatformBanLoaded = true
	gwconfig.GlobalCfgMu.Lock()
	g := gwconfig.LoadGlobalConfigLocked()
	if len(g.PlatformBlacklist) > 0 {
		PlatformBanItems = make(map[string]*gwconfig.PlatformBanEntry, len(g.PlatformBlacklist))
		for k, e := range g.PlatformBlacklist {
			PlatformBanItems[k] = e
		}
	}
	gwconfig.GlobalCfgMu.Unlock()
	if n := len(PlatformBanItems); n > 0 {
		log.Printf("platform blacklist: restored %d platform(s) from %s", n, gwconfig.GlobalConfigPath())
	}
}

func FlushPlatformBan() {
	PlatformBanMu.Lock()
	out := make(map[string]*gwconfig.PlatformBanEntry, len(PlatformBanItems))
	for k, e := range PlatformBanItems {
		cp := *e
		out[k] = &cp
	}
	PlatformBanMu.Unlock()

	gwconfig.MutateGlobalConfig(func(g *gwconfig.GlobalConfigData) { g.PlatformBlacklist = out })
}

// PlatformBanKeyValid 校验封禁键：基础平台或存在的 Other 子平台
// （other:<slug>）。未知键拒绝——拼写错误静默入库等于封了个寂寞。
func PlatformBanKeyValid(key string) bool {
	switch key {
	case PlatCline, PlatZen, PlatWorkBuddy, PlatTrae:
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
	EnsurePlatformBanLoaded()
	PlatformBanMu.Lock()
	defer PlatformBanMu.Unlock()
	return PlatformBanItems[platform] != nil
}

// BanPlatform 封禁平台（已封禁时幂等返回 false）。子平台封禁只影响该子平台；
// 基础平台封禁覆盖其全部对外模型。
func BanPlatform(platform string) bool {
	platform = strings.TrimSpace(platform)
	if !PlatformBanKeyValid(platform) {
		return false
	}
	EnsurePlatformBanLoaded()
	PlatformBanMu.Lock()
	_, existed := PlatformBanItems[platform]
	if !existed {
		PlatformBanItems[platform] = &gwconfig.PlatformBanEntry{Platform: platform, CreatedAt: time.Now()}
	}
	PlatformBanMu.Unlock()
	if existed {
		return false
	}
	FlushPlatformBan()
	log.Printf("platform blacklist: +%s (platform banned, out of routing)", platform)
	return true
}

// UnbanPlatform 解封平台（未封禁时幂等返回 false）。
func UnbanPlatform(platform string) bool {
	platform = strings.TrimSpace(platform)
	EnsurePlatformBanLoaded()
	PlatformBanMu.Lock()
	_, existed := PlatformBanItems[platform]
	delete(PlatformBanItems, platform)
	PlatformBanMu.Unlock()
	if !existed {
		return false
	}
	FlushPlatformBan()
	log.Printf("platform blacklist: -%s (platform unbanned)", platform)
	return true
}

// PlatformBanSnapshot 面板数据（按封禁时间倒序）。
func PlatformBanSnapshot() []map[string]any {
	EnsurePlatformBanLoaded()
	PlatformBanMu.Lock()
	rows := make([]map[string]any, 0, len(PlatformBanItems))
	for _, e := range PlatformBanItems {
		rows = append(rows, map[string]any{
			"platform":  e.Platform,
			"createdAt": e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	PlatformBanMu.Unlock()
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i]["createdAt"].(string) > rows[j]["createdAt"].(string)
	})
	return rows
}

// ExportPlatformBlacklist 平台封禁表快照（配置导出用；锁内拷贝）。
func ExportPlatformBlacklist() map[string]*gwconfig.PlatformBanEntry {
	EnsurePlatformBanLoaded()
	PlatformBanMu.Lock()
	defer PlatformBanMu.Unlock()
	if len(PlatformBanItems) == 0 {
		return nil
	}
	out := make(map[string]*gwconfig.PlatformBanEntry, len(PlatformBanItems))
	for k, v := range PlatformBanItems {
		if v == nil {
			continue
		}
		cp := *v
		out[k] = &cp
	}
	return out
}

// ImportPlatformBlacklist 整体替换平台封禁表并即时落盘（与模型封禁导入同语义：
// 备份里没有的段不动；有则连清空也生效）。无效键跳过，不阻断其余段导入。
func ImportPlatformBlacklist(in map[string]*gwconfig.PlatformBanEntry) {
	EnsurePlatformBanLoaded()
	next := make(map[string]*gwconfig.PlatformBanEntry, len(in))
	for k, v := range in {
		if k == "" || v == nil || !PlatformBanKeyValid(k) {
			continue
		}
		e := *v
		e.Platform = k // key 为准，条目内 platform 字段仅展示
		next[k] = &e
	}
	PlatformBanMu.Lock()
	PlatformBanItems = next
	PlatformBanMu.Unlock()
	FlushPlatformBan()
	log.Printf("  config import: platform blacklist replaced (%d platforms)", len(next))
}

// ============================================================================
// 模型 ID → 封禁键
// ============================================================================

package app

// blacklist_bridge.go —— blacklist 包（P4）的 app 侧小写消费面别名。
// BanReason*/plat* 常量与 ModelBlacklistMu/Items 锁与内存真身指针别名。

import (
	"afree-proxy/internal/gateway"
	"net/http"

	"afree-proxy/internal/blacklist"
	"afree-proxy/internal/gwconfig"
)

const (
	BanReasonAuto        = blacklist.BanReasonAuto
	BanReasonManual      = blacklist.BanReasonManual
	BanReasonModelGone   = blacklist.BanReasonModelGone
	BanReasonFeedRemoved = blacklist.BanReasonFeedRemoved

	platWorkBuddy = blacklist.PlatWorkBuddy
	platZen       = blacklist.PlatZen
	platCline     = blacklist.PlatCline
	platTrae      = blacklist.PlatTrae
	platOther     = blacklist.PlatOther
)

var modelBlacklistMu = &blacklist.ModelBlacklistMu

var modelBlacklistItems = &blacklist.ModelBlacklistItems

var modelBlacklistLoaded = &blacklist.ModelBlacklistLoaded

var platformBanMu = &blacklist.PlatformBanMu

var platformBanItems = &blacklist.PlatformBanItems

var platformBanLoaded = &blacklist.PlatformBanLoaded

func withModelBlacklistScan(h http.HandlerFunc) http.HandlerFunc { return WithModelBlacklistScan(h) }

func modelBlacklistSnapshot() []map[string]any { return blacklist.ModelBlacklistSnapshot() }

func ensureModelBlacklistLoaded() { blacklist.EnsureModelBlacklistLoaded() }

func flushModelBlacklist() { blacklist.FlushModelBlacklist() }

func platformOfModel(model string) string { return blacklist.PlatformOfModel(model) }

func batchBanPlatformModels(platform, keyword string) (matched int, banned []string) {
	return BatchBanPlatformModels(platform, keyword)
}

func blacklistKey(platform, model string) string { return blacklist.BlacklistKey(platform, model) }

func IsModelBlacklisted(model string) bool { return blacklist.IsModelBlacklisted(model) }

func IsModelPermissionDenied(status int, body string) bool {
	return blacklist.IsModelPermissionDenied(status, body)
}

func BlacklistModel(platform, model, code, reason, detail string) {
	blacklist.BlacklistModel(platform, model, code, reason, detail)
}

func BanModel(platform, model string) bool { return blacklist.BanModel(platform, model) }

func UnblockModel(platform, model string) bool { return blacklist.UnblockModel(platform, model) }

func TraeModelBannedIn(merged, bare string) bool { return blacklist.TraeModelBannedIn(merged, bare) }

func businessCodeOf(body string) string { return blacklist.BusinessCodeOf(body) }

func businessCodeIn(body string) string { return blacklist.BusinessCodeIn(body) }

func modelRoutePlatformKey(model string) string { return ModelRoutePlatformKey(model) }

func bannedPlatformOf(model string) string { return gateway.BannedPlatformOfExport(model) }

func IsPlatformBanned(platform string) bool { return blacklist.IsPlatformBanned(platform) }

func BanPlatform(platform string) bool { return blacklist.BanPlatform(platform) }

func UnbanPlatform(platform string) bool { return blacklist.UnbanPlatform(platform) }

func platformBanSnapshot() []map[string]any { return blacklist.PlatformBanSnapshot() }

func exportPlatformBlacklist() map[string]*gwconfig.PlatformBanEntry {
	return blacklist.ExportPlatformBlacklist()
}

func importPlatformBlacklist(in map[string]*gwconfig.PlatformBanEntry) {
	blacklist.ImportPlatformBlacklist(in)
}

func flushPlatformBan() { blacklist.FlushPlatformBan() }

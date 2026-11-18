package app

// trae_bridge.go —— app 侧对 internal/trae 域的小写消费面别名（2026-11-18
// P3e：trae 平台域出包 internal/trae，本文件让既有消费文件零改动）。
// P4 收尾时消费方改直接 trae.X 后本文件只留编排钩子注入。

import (
	"afree-proxy/internal/reqlog"

	"encoding/json"
	"io"
	"net/http"

	"trae2api"

	"afree-proxy/internal/trae"
)

// ---- 类型 ----

type trae2apiProxyBinding = trae.Trae2apiProxyBinding

// ---- 锁 ----

var traeBindingsMu = &trae.TraeBindingsMu

// ---- 绑定/路由内存表（指针别名，handler 直接读写）----

var traeBindings = &trae.TraeBindings
var traeRouting = &trae.TraeRouting
var traeRoutingMu = &trae.TraeRoutingMu

func saveTraeBindingsLocked() { trae.SaveTraeBindingsLocked() }

func saveTraeRoutingLocked() { trae.SaveTraeRoutingLocked() }

func trae2apiRestart() error { return trae.Trae2apiRestart() }

func trae2apiIntlEnabled() bool { return trae.Trae2apiIntlEnabled() }

func trae2apiPlatformModel(model string) bool { return trae.Trae2apiPlatformModel(model) }

func trae2apiModelExists(id string) bool { return trae.Trae2apiModelExists(id) }

func trae2apiModelEntries() []map[string]any { return trae.Trae2apiModelEntries() }

func initTrae() { trae.InitTrae() }

func trae2apiCurrent() *trae.Trae2apiSubsystem { return trae.Trae2apiCurrent() }

func trae2apiRoutableCount() int { return trae.Trae2apiRoutableCount() }

func isTrae2apiModel(model string) bool { return trae.IsTrae2apiModel(model) }

func shouldServeTrae2api(model string) bool { return trae.ShouldServeTrae2api(model) }

func trae2apiProbeBlacklistedModel(model string) (map[string]any, string) {
	return trae.Trae2apiProbeBlacklistedModel(model)
}

func mutateTraeConfig(fn func(*trae.TraeConfigData)) { trae.MutateTraeConfig(fn) }

func traeConfigUseProxies() *bool { return trae.TraeConfigUseProxies() }

type traeConfigData = trae.TraeConfigData

const trae2apiVersion = trae.Trae2apiVersion

func traeProxiesEnabled() bool { return trae.TraeProxiesEnabled() }

func traeConfigPath() string { return trae.TraeConfigPath() }

func traeApplyAfreeConstraints(cfg *trae2api.Config) { trae.TraeApplyAfreeConstraints(cfg) }

func trae2apiAccountStates(sub *trae.Trae2apiSubsystem) []map[string]any {
	return trae.Trae2apiAccountStates(sub)
}

func trae2apiRateFilterThresholdCN() float64 { return trae.Trae2apiRateFilterThresholdCN() }

func trae2apiRateFilterThresholdIntl() float64 { return trae.Trae2apiRateFilterThresholdIntl() }

func traeRoutingGet(uid string) bool { return trae.TraeRoutingGet(uid) }

func traeRoutingSet(uid string, enabled bool) { trae.TraeRoutingSet(uid, enabled) }

func traeApplyRateFilterPatch(patch traeRateFilterPatch) { trae.TraeApplyRateFilterPatch(patch) }

type traeRateFilterPatch = trae.TraeRateFilterPatch

func trae2apiAdminCall(method, path string, body []byte) (int, []byte, error) {
	return trae.Trae2apiAdminCall(method, path, body)
}

func webAuthSourceAllowed(r *http.Request) bool { return trae.WebAuthSourceAllowed(r) }

func helperScript(relayURL string) string { return trae.HelperScript(relayURL) }

func traeNormalizeStreamDefault(body []byte) []byte { return trae.TraeNormalizeStreamDefault(body) }

func trae2apiTranslateMergedBody(body []byte) []byte { return trae.Trae2apiTranslateMergedBody(body) }

func vendoredErrCode(code int) string { return trae.VendoredErrCode(code) }

func vendoredErr(vendored map[string]any, code int) string { return trae.VendoredErr(vendored, code) }

var traeLastIngestMu = &trae.TraeLastIngestMu

func setTraeLastIngest(rec *trae.TraeHelperIngest) { trae.TraeLastIngest = rec }

func traeLastIngest() *trae.TraeHelperIngest { return trae.TraeLastIngest }

type traeHelperIngest = trae.TraeHelperIngest

func trae2apiIngestCreds(creds map[string]any) (uid, label string, added bool, err error) {
	return trae.Trae2apiIngestCreds(creds)
}

func trae2apiSetDisplayOrder(uids []string) int { return trae.Trae2apiSetDisplayOrder(uids) }

var traeInprocessHTTP = trae.TraeInprocessHTTP

const traeInprocessBase = trae.TraeInprocessBase

func bytesReader(b []byte) io.Reader { return trae.BytesReader(b) }

func trae2apiTranslateMerged(model string) string { return trae.Trae2apiTranslateMerged(model) }

type traeFallbackGuardTransport = trae.TraeFallbackGuardTransport

func traePooledClient(proxyURL, uid string) *http.Client { return trae.TraePooledClient(proxyURL, uid) }

const traeAgentTaskPath = trae.TraeAgentTaskPath

// ---- 合并域常量 ----

const (
	traeMergedCN   = trae.TraeMergedCN
	traeMergedIntl = trae.TraeMergedIntl
)

func loadTraeRoutingLocked() { trae.LoadTraeRoutingLocked() }

// ---- 数据路径 ----

func traeDataDir() string { return trae.TraeDataDir() }

// ---- 绑定段 ----

func loadTraeBindingsLocked() { trae.LoadTraeBindingsLocked() }

// ---- 展示顺序 ----

func traeDisplayOrderForBackup() []string { return trae.TraeDisplayOrderForBackup() }

func importTrae2apiDisplayOrder(uids []string) int { return trae.ImportTrae2apiDisplayOrder(uids) }

// ---- 代理绑定导入导出 ----

func exportTrae2apiProxyBindings() map[string]trae2apiProxyBinding {
	return trae.ExportTrae2apiProxyBindings()
}

func importTrae2apiProxyBindings(b map[string]trae2apiProxyBinding) int {
	return trae.ImportTrae2apiProxyBindings(b)
}

// ---- 备份段 ----

func trae2apiBackupSnapshot() json.RawMessage { return trae.Trae2apiBackupSnapshot() }

func trae2apiRestoreBackup(raw json.RawMessage, keepEnabled bool) (int, error) {
	return trae.Trae2apiRestoreBackup(raw, keepEnabled)
}

// ---- 测试白盒别名（lockorder/backup 测试直接操纵域内状态）----

var traeAfreeConfigPathOverride = &trae.TraeAfreeConfigPathOverride
var traeCfgMu = &trae.TraeCfgMu
var traeCfgLoaded = &trae.TraeCfgLoaded
var traeCfg = &trae.TraeCfg
var trae2apiSub = &trae.Trae2apiSub
var traeTokensMu = &trae.TraeTokensMu
var traeTokens = &trae.TraeTokens
var traeOrderMu = &trae.TraeOrderMu
var traeDisplayOrder = &trae.TraeDisplayOrder

type trae2apiTokenStats = trae.Trae2apiTokenStats

func trae2apiTokensSnapshot(uid string) (today, total int64, lastUsed, lastModel string) {
	return trae.Trae2apiTokensSnapshot(uid)
}

func traeDisplayOrderSave(uids []string) { trae.TraeDisplayOrderSave(uids) }

func traeDisplayOrderLoad() { trae.TraeDisplayOrderLoad() }

func trae2apiLoadConfig() (*trae2api.Config, error) { return trae.Trae2apiLoadConfig() }

func trae2apiValidateGateway(gw string) error { return trae.Trae2apiValidateGateway(gw) }

var traeBindingsLoaded = &trae.TraeBindingsLoaded

func traeConfigRouting() map[string]bool { return trae.TraeConfigRouting() }

func traeAfreeConfigPath() string { return trae.TraeAfreeConfigPath() }

type trae2apiSubsystem = trae.Trae2apiSubsystem

// ---- Host 注入：请求日志槽位与模型黑名单 ----

func init() {
	trae.Host.LogUpstreamInfo = reqlog.SetUpstreamInfo
	trae.Host.LogUpstreamExit = reqlog.SetUpstreamExit
	trae.Host.ModelBannedIn = TraeModelBannedIn
}

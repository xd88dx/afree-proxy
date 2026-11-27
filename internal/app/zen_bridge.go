package app

// zen 桥接层（2026-11-17 结构重构 P3d，go/parser 生成）：zen 平台已出包
// internal/zen，本文件把 app 侧既有小写消费面桥接过去（类型别名 + 同签名
// 包装 + Host 注入）。

import (
	"afree-proxy/internal/gateway"
	"afree-proxy/internal/reqlog"

	"context"
	"net/http"
	"time"

	"afree-proxy/internal/cline"
	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/zen"
)

// ---- 类型别名 ----

type ZenModel = zen.ZenModel
type ZenCallOpts = zen.ZenCallOpts

type zenCallOpts = zen.ZenCallOpts
type zenConfigData = zen.ZenConfigData
type zenConfigFileData = zen.ZenConfigFileData
type zenDepEntry = zen.ZenDepEntry
type zenHTTPError = zen.ZenHTTPError
type zenSessionEntry = zen.ZenSessionEntry
type zenUsageFile = zen.ZenUsageFile

type ZenConfigData = zen.ZenConfigData
type ZenConfigFileData = zen.ZenConfigFileData
type ZenDepEntry = zen.ZenDepEntry
type ZenHTTPError = zen.ZenHTTPError
type ZenParkState = zen.ZenParkState
type ZenSessionEntry = zen.ZenSessionEntry
type ZenUsageFile = zen.ZenUsageFile

type CompactOutcome = zen.CompactOutcome

type zenParkState = zen.ZenParkState

type zenCompactConfig = zen.ZenCompactConfig

var zenNativeUA = zen.ZenNativeUAPtr()

var zenConfigPathOverride = zen.ZenConfigOverridePtr()

var zenUsagePathOverride = zen.ZenUsageOverridePtr()

var zenKeyMu = zen.ZenKeyMuPtr()

var zenKeyCool = zen.ZenKeyCoolPtr()

var zenKeyTokens = zen.ZenKeyTokensPtr()

var zenKeyPark = zen.ZenKeyParkPtr()

var zenModelsMu = zen.ZenModelsMuPtr()

var zenModels = zen.ZenModelsPtr()

var zenSessMu = zen.ZenSessMuPtr()

var zenSessions = zen.ZenSessionsPtr()

var egressMu = &zen.EgressMu

var egressLoaded = &zen.EgressLoaded

var egressBindings = &zen.EgressBindings

type egressBinding = zen.EgressBinding

var zenKeyIdx = zen.ZenKeyIdxPtr()

func boolPtr(b bool) *bool { return &b }

func setZenGateHeaders(req *http.Request, sess, user, key string) {
	zen.SetZenGateHeaders(req, sess, user, key)
}

var zenSessLoaded = zen.ZenSessLoadedPtr()

var zenSessSaveBlocked = zen.ZenSessSaveBlockedPtr()

var zenStateMu = zen.ZenStateMuPtr()

func cooldownZenKey(key string, d time.Duration) time.Duration {
	return zen.CooldownZenKey(key, d)
}

const zenRateLimitCooldown = zen.ZenRateLimitCooldown

func zenKeyCooling(key string) bool { return zen.ZenKeyCooling(key) }

var zenFailCount = zen.ZenFailCountPtr()

var zenFailUntil = zen.ZenFailUntilPtr()

var max429KeySwitches = zen.Max429KeySwitches

var defaultCompactBuffer = zen.DefaultCompactBuffer

var zenSessFailedMu = zen.ZenSessFailedMuPtr()

var zenSessFailed = zen.ZenSessFailedPtr()

func zenAnyKeyRotatable(key string) bool { return zen.ZenAnyKeyRotatable(key) }

func zenSessionRefreshedRecently(key string, now time.Time) bool {
	return zen.ZenSessionRefreshedRecently(key, now)
}

func refreshZenSession(key string) { zen.RefreshZenSession(key) }

var zenDepMu = zen.ZenDepMuPtr()

var zenDepAliases = zen.ZenDepAliasesPtr()

// ---- 函数别名 ----

func applyZenEndpoints(learned map[string]string) int { return zen.ApplyZenEndpoints(learned) }
func buildZenBody(params map[string]any, stream bool) map[string]any {
	return zen.BuildZenBody(params, stream)
}
func callZenAPI(ctx context.Context, params map[string]any, stream bool, opts ...ZenCallOpts) (*http.Response, int, error) {
	return zen.CallZenAPI(ctx, params, stream, opts...)
}
func callZenResponsesAPI(ctx context.Context, params map[string]any, stream bool, opts ...ZenCallOpts) (*http.Response, int, error) {
	return zen.CallZenResponsesAPI(ctx, params, stream, opts...)
}
func captureZenRequest(label string, params map[string]any, h http.Header) {
	zen.CaptureZenRequest(label, params, h)
}
func cleanupCompactStates()                            { zen.CleanupCompactStates() }
func defaultZenConfig() *ZenConfigData                 { return zen.DefaultZenConfig() }
func egressBindingActive() bool                        { return zen.EgressBindingActive() }
func egressProxyForZen(key string) (string, bool)      { return zen.EgressProxyForZen(key) }
func estimateJSON(v any) int                           { return zen.EstimateJSON(v) }
func getZenConfig() *ZenConfigData                     { return zen.GetZenConfig() }
func importZenStateBackup(f *ZenUsageFile)             { zen.ImportZenStateBackup(f) }
func importZenStateFacts(f *ZenUsageFile)              { zen.ImportZenStateFacts(f) }
func isRateLimited(status int, body string) bool       { return zen.IsRateLimited(status, body) }
func isWrongEndpoint(err error) bool                   { return zen.IsWrongEndpoint(err) }
func isWrongEndpointResponses(err error) bool          { return zen.IsWrongEndpointResponses(err) }
func isZenFreeModel(m *ZenModel) bool                  { return zen.IsZenFreeModel(m) }
func keyIndex(key string) int                          { return zen.KeyIndex(key) }
func learnZenEndpoint(modelID string, upstream string) { zen.LearnZenEndpoint(modelID, upstream) }
func loadEgressBindings()                              { zen.LoadEgressBindings() }
func loadZenDepsLocked()                               { zen.LoadZenDepsLocked() }
func loadZenEndpoints()                                { zen.LoadZenEndpoints() }
func loadZenEndpointsFile() map[string]string          { return zen.LoadZenEndpointsFile() }
func loadZenSessions()                                 { zen.LoadZenSessions() }
func maskZenKey(k string) string                       { return zen.MaskZenKey(k) }
func maybeCompact(params map[string]any, m *ZenModel, sessionID string) CompactOutcome {
	return zen.MaybeCompact(params, m, sessionID)
}
func mutateZenConfigFile(fn func(*ZenConfigFileData)) { zen.MutateZenConfigFile(fn) }
func normalizeZenKeys(cfg *ZenConfigData)             { zen.NormalizeZenKeys(cfg) }
func pickZenKey() string                              { return zen.PickZenKey() }
func pinProxyPerKeyEnabled() bool                     { return zen.PinProxyPerKeyEnabled() }
func proxyIndex(p string) int                         { return zen.ProxyIndex(p) }

// 代理规则 kind 白名单/排除表清理原语住 exitpool（2026-11-22 自 zen 迁入，
// 语义归属代理规则域；此处保留小写别名供既有 handler/测试零改动消费）。
func proxyRuleKindValid(kind string) bool { return exitpool.ProxyRuleKindValid(kind) }
func pruneProxyExclusions(ex map[string][]string, pool []string) map[string][]string {
	return exitpool.PruneProxyExclusions(ex, pool)
}
func pruneProxyEntries(list []string, pool []string) []string {
	return exitpool.PruneProxyEntries(list, pool)
}
func reconcileEgressBindings() { zen.ReconcileEgressBindings() }
func reconcileZenKeyRouting(next *ZenConfigData, cur *ZenConfigData) {
	zen.ReconcileZenKeyRouting(next, cur)
}
func recordZenKeyTokens(key string, tokens int64)               { zen.RecordZenKeyTokens(key, tokens) }
func reorderZenKeys(order []int) error                          { return zen.ReorderZenKeys(order) }
func requestSessionID(r map[string]any, hdr http.Header) string { return zen.RequestSessionID(r, hdr) }
func resolveZenFreeModel(id string) (*ZenModel, bool)           { return zen.ResolveZenFreeModel(id) }
func resolveZenModel(id string) (*ZenModel, bool)               { return zen.ResolveZenModel(id) }
func responsesSSEToChat(resp *http.Response) (map[string]any, error) {
	return zen.ResponsesSSEToChat(resp)
}
func routeModel(id string) string                     { return zen.RouteModel(id) }
func saveZenDepsLocked()                              { zen.SaveZenDepsLocked() }
func saveZenEndpoints()                               { zen.SaveZenEndpoints() }
func saveZenSessions()                                { zen.SaveZenSessions() }
func saveZenSessionsLocked()                          { zen.SaveZenSessionsLocked() }
func setAllZenKeysRoutingEnabled(enabled bool) int    { return zen.SetAllZenKeysRoutingEnabled(enabled) }
func setZenConfig(c *ZenConfigData)                   { zen.SetZenConfig(c) }
func setZenKeyBindings(m map[string]exitpool.Binding) { zen.SetZenKeyBindings(m) }
func setZenKeyRoutingEnabled(index int, enabled bool) error {
	return zen.SetZenKeyRoutingEnabled(index, enabled)
}
func startZenModelsRefresher()                           { zen.StartZenModelsRefresher() }
func syncZenModels() (int, error)                        { return zen.SyncZenModels() }
func uncoolZenKey(key string)                            { zen.UncoolZenKey(key) }
func unparkZenKey(key string)                            { zen.UnparkZenKey(key) }
func validateProxyList(proxies []string) error           { return zen.ValidateProxyList(proxies) }
func validateZenUpstreamBase(raw string) error           { return zen.ValidateZenUpstreamBase(raw) }
func zenClientErrorStatus(err error) int                 { return zen.ZenClientErrorStatus(err) }
func zenConfigUseProxies() *bool                         { return zen.ZenConfigUseProxies() }
func zenEgressSnapshot() map[string]any                  { return zen.ZenEgressSnapshot() }
func zenHasRoutableKey() bool                            { return zen.ZenHasRoutableKey() }
func zenKeyCooldownUntil(key string) (time.Time, bool)   { return zen.ZenKeyCooldownUntil(key) }
func zenKeyParkSnapshot(key string) (ZenParkState, bool) { return zen.ZenKeyParkSnapshot(key) }
func zenKeyRoutingEnabled(key string) bool               { return zen.ZenKeyRoutingEnabled(key) }
func zenKeyStatus() []map[string]any                     { return zen.ZenKeyStatus() }
func zenModelList() []map[string]any                     { return zen.ZenModelList() }
func zenPrefixedID(id string) string                     { return zen.ZenPrefixedID(id) }
func zenProbeModel() *ZenModel                           { return zen.ZenProbeModel() }
func zenSessionMarkSuccess(key string)                   { zen.ZenSessionMarkSuccess(key) }
func zenStateBackupSnapshot() *ZenUsageFile              { return zen.ZenStateBackupSnapshot() }

// ---- Host 注入（装配根 init）----

func init() {
	zen.Host.CollectStreamResponse = func(upstream *http.Response, hintModel ...string) (map[string]any, error) {
		return gateway.CollectStreamResponse(upstream, hintModel...)
	}
	zen.Host.IsDirect = func(v string) bool { return isEgressDirect(v) }
	zen.Host.PassThroughKeys = cline.PassThroughKeys
	zen.Host.RepairToolArguments = gateway.RepairToolArguments
	zen.Host.AsInt = kit.AsInt
	zen.Host.BoundExitFor = boundExitFor
	zen.Host.MaskProxyURLRaw = func(raw string) string { return kit.MaskProxyURL(raw) }
	zen.Host.LogUpstreamInfo = reqlog.SetUpstreamInfo
	zen.Host.LogUpstreamExitType = reqlog.SetUpstreamExit
	zen.Host.ClineHeaderWatchdogFor = cline.ClineHeaderWatchdogFor
	zen.Host.ProxyExitType = proxyExitType
	zen.Host.ShouldCooldownExit = shouldCooldownExit
	zen.Host.PlatformCooldown = proxyPlatformCooldown
	zen.Host.IsolationEnabled = proxyIsolationEnabled
	zen.Host.AccountIDs = func() []string {
		p := loadPool()
		poolMu.Lock()
		defer poolMu.Unlock()
		ids := make([]string, 0, len(p.Accounts))
		for _, a := range p.Accounts {
			ids = append(ids, a.AccountID)
		}
		return ids
	}
	zen.Host.HasClineCatalogConflict = cline.CatalogConflict
	zen.Host.Strategy = globalStrategy
	zen.Host.LogUpstream = func(ctx context.Context, label string) { reqlog.SetUpstreamInfo(ctx, label) }
	zen.Host.LogUpstreamExit = func(ctx context.Context, exit string) { reqlog.SetUpstreamExit(ctx, exit) }
	zen.Host.ComboPlatform = func(id string) (string, bool) {
		c := resolveCombo(id)
		if c == nil {
			return "", false
		}
		return c.Platform, true
	}
	zen.Host.HasModelEntry = cline.CatalogConflict
	zen.Host.MaskProxyURL = func(raw string) string { return kit.MaskProxyURL(raw) }
	zen.Host.PoolStatusSnapshot = poolStatusSnapshot
	zen.Host.UpstreamErrorStatus = gateway.UpstreamErrorStatus
	zen.Host.KeyBindingOf = keyBindingOf
	zen.Host.BoundProxiesRoutableFor = boundProxiesRoutableFor
	zen.Host.BlockedExitMessage = blockedExitMessage
	zen.Host.GetMsgCount = cline.GetMsgCount
	zen.Host.ZenRoutable = func() bool { return platformHasRoutableCredential(platZen) }
}

func zenRetryDelay(d time.Duration) time.Duration { return zen.ZenRetryDelay(d) }

func FallbackTruncate(params map[string]any, m *ZenModel) CompactOutcome {
	return zen.FallbackTruncate(params, m)
}

func compactThreshold(context, output, buffer int) int {
	return zen.CompactThreshold(context, output, buffer)
}

func strField(m map[string]any, key string) string { return zen.StrField(m, key) }

func fallbackTruncate(params map[string]any, m *ZenModel) CompactOutcome {
	return zen.FallbackTruncate(params, m)
}

func loadZenConfig() *zenConfigData { return zen.LoadZenConfig() }

func zenExtractReplacement(body string) string { return zen.ZenExtractReplacement(body) }

func zenSyncClient() (*http.Client, error) { return zen.ZenSyncClient() }

func recordProxyTestOutcome(proxyURL string, ok bool, err error, elapsedMs int64) {
	exitpool.RecordProxyTestOutcome(proxyURL, ok, err, elapsedMs)
}

const proxyRuleDirectKey = exitpool.ProxyRuleDirectKey

func ZenMergeTools(clientTools []any, gateTools []any) []any {
	return zen.ZenMergeTools(clientTools, gateTools)
}

func ZenGateTools() []any { return zen.ZenGateTools() }

func ZenClientFlatTools(params map[string]any) []map[string]any {
	return zen.ZenClientFlatTools(params)
}

func BuildZenBody(params map[string]any, stream bool) map[string]any {
	return zen.BuildZenBody(params, stream)
}

var ZenGateToolSpecs = zen.ZenGateToolSpecs

var proxyRuleKinds = exitpool.ProxyRuleKinds

func responsesContentToInput(m map[string]any) { zen.ResponsesContentToInput(m) }

func StrField(m map[string]any, key string) string { return zen.StrField(m, key) }

func ToolCallIDs(m map[string]any) []string { return zen.ToolCallIDs(m) }

var pool = cline.RawPool()

func markZenFail() { zen.MarkZenFail() }

func markZenSuccess() { zen.MarkZenSuccess() }

func zenFailedNow() bool { return zen.ZenFailedNow() }

func mintZenSessionEntryLocked(e *zenSessionEntry, now time.Time) {
	zen.MintZenSessionEntryLocked(e, now)
}

func zenNoKeyError() error { return zen.ZenNoKeyError() }

func parkZenKey(key, reason string) time.Duration { return zen.ParkZenKey(key, reason) }

func zenDailyExhausted(status int, body string, retryAfter time.Duration) bool {
	return zen.ZenDailyExhausted(status, body, retryAfter)
}

var zenParkMaxBackoff = zen.ZenParkMaxBackoff

var zenParkInitial = zen.ZenParkInitial

func zenKeyParked(key string) (time.Time, bool) { return zen.ZenKeyParked(key) }

func zenAllParkedUntil() (time.Time, bool) { return zen.ZenAllParkedUntil() }

func zenDeprecatedReplacement(id string) string { return zen.ZenDeprecatedReplacement(id) }

func zenRecordDeprecation(id, replacement string) { zen.ZenRecordDeprecation(id, replacement) }

func zenModelDead(id string) bool { return zen.ZenModelDead(id) }

var zenDeadModels = &zen.ZenDeadModels

const zenDeadTTL = zen.ZenDeadTTL

func zenSessionLive(key string) bool { return zen.ZenSessionLive(key) }

func zenSessionDesc(key string) string { return zen.ZenSessionDesc(key) }

func zenSessionFailed(key string) { zen.ZenSessionFailed(key) }

func pruneZenKeyState(valid map[string]bool) { zen.PruneZenKeyState(valid) }

func saveZenUsage() { zen.SaveZenUsage() }

func loadZenUsage() { zen.LoadZenUsage() }

type zenKeyTokenStats = zen.ZenKeyTokenStats

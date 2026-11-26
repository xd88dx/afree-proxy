package gateway

// 装配钩子与跨域包装（gateway 批）：平台调用器/注册表查询经本文件注入或
// 直调各平台包；moved 文本保持原名以零改动迁移。

import (
	"context"
	"io"
	"net/http"

	"afree-proxy/internal/blacklist"
	"afree-proxy/internal/cline"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/other"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/workbuddy"
	"afree-proxy/internal/zen"
)

// PlatformRoutable 平台可路由性查询（registry 单例在装配根；init 注入）。
var PlatformRoutable func(string) bool

func routeModel(id string) string                { return zen.RouteModel(id) }
func globalStrategy() string                     { return gwconfig.GlobalStrategy() }
func globalClientKeys() []string                 { return gwconfig.GlobalClientKeys() }
func APIKeyEnv() string                          { return gwconfig.APIKeyEnv() }
func AllowBanned() bool                          { return gwconfig.AllowBanned() }
func StreamMaxAttempts() int                     { return gwconfig.StreamMaxAttempts() }
func normalizeModelEntryFields(m map[string]any) { cline.NormalizeModelEntryFields(m) }
func shouldServeWorkBuddy(m string) bool         { return workbuddy.ShouldServeWorkBuddy(m) }
func shouldServeTrae2api(m string) bool          { return trae.ShouldServeTrae2api(m) }
func syncZenModels() (int, error)                { return zen.SyncZenModels() }
func zenModelList() []map[string]any             { return zen.ZenModelList() }
func estimateJSON(v any) int                     { return zen.EstimateJSON(v) }
func clineProxiesEnabled() bool {
	if v := platformUseProxies("cline"); v != nil {
		return *v
	}
	return true
}

// ---- gateway 导出面（装配根/测试别名消费）----

func ListCombos() []*gwconfig.Combo                                  { return listCombos() }
func ResolveCombo(id string) *gwconfig.Combo                         { return resolveCombo(id) }
func WriteJSON(w http.ResponseWriter, status int, data any)          { writeJSON(w, status, data) }
func UsageFromChatPayload(body string) map[string]any                { return usageFromChatPayload(body) }
func TakeUsedKey(resp *http.Response) string                         { return takeUsedKey(resp) }
func ClassifyReqRoute(r *http.Request, model string) (string, string) {
	return classifyReqRoute(r, model)
}
func ServeTrae2apiChat(rw http.ResponseWriter, r *http.Request, body []byte) {
	serveTrae2apiChat(rw, r, body)
}

// ---- zen/cline/gwconfig 侧别名（moved 文本原名直呼；桥接自各域导出面）----

type (
	ZenModel       = zen.ZenModel
	ZenConfigData  = zen.ZenConfigData
	CompactOutcome = zen.CompactOutcome
	ZenCallOpts    = zen.ZenCallOpts
)

var globalCfgMu = &gwconfig.GlobalCfgMu

func loadGlobalConfigLocked() *gwconfig.GlobalConfigData { return gwconfig.LoadGlobalConfigLocked() }

const (
	traeMergedCN   = trae.TraeMergedCN
	traeMergedIntl = trae.TraeMergedIntl
)

func getZenConfig() *ZenConfigData                { return zen.GetZenConfig() }
func resolveZenModel(id string) (*ZenModel, bool) { return zen.ResolveZenModel(id) }
func zenKeyStatus() []map[string]any              { return zen.ZenKeyStatus() }
func zenKeyCooling(key string) bool               { return zen.ZenKeyCooling(key) }
func zenKeyRoutingEnabled(key string) bool        { return zen.ZenKeyRoutingEnabled(key) }

type globalConfigData = gwconfig.GlobalConfigData

func modelInClineList(id string) bool                 { return cline.ModelInClineList(id) }
func mutateGlobalConfig(fn func(*globalConfigData))   { gwconfig.MutateGlobalConfig(fn) }
func resolveZenFreeModel(id string) (*ZenModel, bool) { return zen.ResolveZenFreeModel(id) }
func trae2apiIntlEnabled() bool                       { return trae.Trae2apiIntlEnabled() }
func trae2apiModelExists(id string) bool              { return trae.Trae2apiModelExists(id) }

func maybeCompact(params map[string]any, m *ZenModel, sessionID string) CompactOutcome {
	return zen.MaybeCompact(params, m, sessionID)
}

func requestSessionID(r map[string]any, hdr http.Header) string { return zen.RequestSessionID(r, hdr) }
func zenPrefixedID(id string) string                            { return zen.ZenPrefixedID(id) }
func SystemPromptOverrideEnabled() bool                         { return gwconfig.SystemPromptOverrideEnabled() }
func usageTokens(u map[string]any, params map[string]any) int64 { return kit.UsageTokens(u, params) }
func BlacklistModel(platform, model, code, reason, detail string) {
	blacklist.BlacklistModel(platform, model, code, reason, detail)
}
func IsModelBlacklisted(model string) bool { return blacklist.IsModelBlacklisted(model) }

const (
	platZen              = blacklist.PlatZen
	BanReasonFeedRemoved = blacklist.BanReasonFeedRemoved
)

type zenHTTPError = zen.ZenHTTPError

func isWrongEndpoint(err error) bool                   { return zen.IsWrongEndpoint(err) }
func isWrongEndpointResponses(err error) bool          { return zen.IsWrongEndpointResponses(err) }
func learnZenEndpoint(modelID string, upstream string) { zen.LearnZenEndpoint(modelID, upstream) }
func recordZenKeyTokens(key string, tokens int64)      { zen.RecordZenKeyTokens(key, tokens) }
func zenClientErrorStatus(err error) int               { return zen.ZenClientErrorStatus(err) }
func captureZenRequest(label string, params map[string]any, h http.Header) {
	zen.CaptureZenRequest(label, params, h)
}
func callZenAPI(ctx context.Context, params map[string]any, stream bool, opts ...ZenCallOpts) (*http.Response, int, error) {
	return zen.CallZenAPI(ctx, params, stream, opts...)
}

func responsesSSEToChat(resp *http.Response) (map[string]any, error) {
	return zen.ResponsesSSEToChat(resp)
}
func callZenResponsesAPI(ctx context.Context, params map[string]any, stream bool, opts ...ZenCallOpts) (*http.Response, int, error) {
	return zen.CallZenResponsesAPI(ctx, params, stream, opts...)
}

func StreamLogEnabled() bool         { return gwconfig.StreamLogEnabled() }
func apiModelList() []map[string]any { return cline.APIModelList() }
func ensureModelsFresh()             { cline.EnsureModelsFresh() }
func platformHasRoutableCredential(p string) bool {
	if PlatformRoutable == nil {
		panic("gateway: PlatformRoutable not injected (assembly root contract)")
	}
	return PlatformRoutable(p)
}

const platWorkBuddy = blacklist.PlatWorkBuddy

const PlatWorkBuddy = blacklist.PlatWorkBuddy

func clineModelCooling(id string) bool        { return cline.ClineModelCooling(id) }
func poolKeysSnapshot() []string              { return gwconfig.GlobalClientKeys() }
func trae2apiModelEntries() []map[string]any  { return trae.Trae2apiModelEntries() }
func trae2apiPlatformModel(model string) bool { return trae.Trae2apiPlatformModel(model) }
func isTrae2apiModel(model string) bool       { return trae.IsTrae2apiModel(model) }

func poolAccountCount() int { return cline.PoolAccountCount() }

func trae2apiCurrent() *trae.Trae2apiSubsystem       { return trae.Trae2apiCurrent() }
func trae2apiRoutableCount() int                     { return trae.Trae2apiRoutableCount() }
func traeNormalizeStreamDefault(body []byte) []byte  { return trae.TraeNormalizeStreamDefault(body) }
func trae2apiTranslateMergedBody(body []byte) []byte { return trae.Trae2apiTranslateMergedBody(body) }
func bytesReader(b []byte) io.Reader                 { return trae.BytesReader(b) }

var traeInprocessHTTP = trae.TraeInprocessHTTP

const traeInprocessBase = trae.TraeInprocessBase

func trae2apiTranslateMerged(model string) string { return trae.Trae2apiTranslateMerged(model) }

func ServedModelGate(model string) (int, string) { return servedModelGate(model) }
func WorkbuddyModelEntries() []map[string]any    { return workbuddyModelEntries() }
func OtherModelList() []map[string]any           { return otherModelList() }

func OtherExitFn(p *other.Platform) func(string) (string, bool) { return otherExitFn(p) }
func OtherBoundRoutable(p *other.Platform) func(string) bool    { return otherBoundRoutable(p) }
func OtherPlatformHasRoutableKey(p *other.Platform) bool        { return otherPlatformHasRoutableKey(p) }

func HandleResponses(w http.ResponseWriter, r *http.Request) { handleResponses(w, r) }
func CollectStreamResponse(upstream *http.Response, hintModel ...string) (map[string]any, error) {
	return collectStreamResponse(upstream, hintModel...)
}
func RepairToolArguments(args string) string          { return repairToolArguments(args) }
func UpstreamErrorStatus(err error, fallback int) int { return upstreamErrorStatus(err, fallback) }

func ComboPlatformValid(p string) bool { return comboPlatformValid(p) }

func ServedModelEntries() []map[string]any { return servedModelEntries() }
func ServedModelOf(resp map[string]any, requested string) string {
	return servedModelOf(resp, requested)
}
func ModelIsServed(model string) bool { return modelIsServed(model) }
func HandleStreamBufferedGeneric(w http.ResponseWriter, r *http.Request, plat string, params map[string]any, attempt streamAttemptFn) {
	handleStreamBufferedGeneric(w, r, plat, params, attempt)
}

type StreamAttemptFn = streamAttemptFn

// ---- moved 符号导出面（装配根测试别名消费）----

type PlatformChatCall = platformChatCall

type TextCallGW = textCallGW

func HandleZenChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	handleZenChat(w, r, params)
}

func HandleZenResponsesNative(w http.ResponseWriter, r *http.Request, params map[string]any, zm *ZenModel) bool {
	return handleZenResponsesNative(w, r, params, zm)
}

func EmitChatAsSSE(w http.ResponseWriter, chat map[string]any, usageFn func(map[string]any)) {
	emitChatAsSSE(w, chat, usageFn)
}

func BufferUpstreamSSE(body io.Reader) (lines []string, usage map[string]any, complete bool, brk error) {
	return bufferUpstreamSSE(body)
}

func HandleClineStreamBuffered(w http.ResponseWriter, r *http.Request, params map[string]any, useProxies bool) {
	handleClineStreamBuffered(w, r, params, useProxies)
}

func ClientWantsStreamUsage(params map[string]any) bool { return clientWantsStreamUsage(params) }

func IsUsageOnlyChunk(obj map[string]any) bool { return isUsageOnlyChunk(obj) }

func ReplaySSE(w http.ResponseWriter, lines []string, keepUsageOnly bool) {
	replaySSE(w, lines, keepUsageOnly)
}

func HandleNonStreamResponseWithUsage(w http.ResponseWriter, upstream *http.Response, onUsage func(map[string]any), stops []string) {
	handleNonStreamResponseWithUsage(w, upstream, onUsage, stops)
}

func WithStreamUsage(params map[string]any) { withStreamUsage(params) }

func ParseToolArgs(raw string) (any, error) { return parseToolArgs(raw) }

func HandleAnthropicMessages(w http.ResponseWriter, r *http.Request) { handleAnthropicMessages(w, r) }

func GetNested(obj map[string]any, keys ...any) any { return getNested(obj, keys...) }

func SortModelEntriesByID(list []map[string]any) { sortModelEntriesByID(list) }

func RejectCaller(msg string, authError bool) PlatformChatCall { return rejectCaller(msg, authError) }

func IsContextOverflow(msg string) bool { return isContextOverflow(msg) }

func WriteUpstreamAPIError(w http.ResponseWriter, model string, err error, fallbackStatus int) {
	writeUpstreamAPIError(w, model, err, fallbackStatus)
}

func NewTextCallGateway(w http.ResponseWriter, names []string, model string) *TextCallGW {
	return newTextCallGateway(w, names, model)
}

func MigrateTraeComboTarget(target string) (string, bool) { return migrateTraeComboTarget(target) }

func ZenAutoBanRegion(err error, zm *ZenModel) { zenAutoBanRegion(err, zm) }

func ValidateCombo(id, platform, target string) error { return validateCombo(id, platform, target) }

type UpstreamHTTPError = upstreamHTTPError

func ChatToolArguments(args string) string { return chatToolArguments(args) }

func ApiModelList() []map[string]any { return apiModelList() }

func CallZenAPI(ctx context.Context, params map[string]any, stream bool, opts ...ZenCallOpts) (*http.Response, int, error) {
	return callZenAPI(ctx, params, stream, opts...)
}

func CallZenResponsesAPI(ctx context.Context, params map[string]any, stream bool, opts ...ZenCallOpts) (*http.Response, int, error) {
	return callZenResponsesAPI(ctx, params, stream, opts...)
}

func EnsureModelsFresh() { ensureModelsFresh() }

func FilterToolInput(name string, input map[string]any, schemas map[string]map[string]bool) map[string]any {
	return filterToolInput(name, input, schemas)
}

func GetZenConfig() *ZenConfigData { return getZenConfig() }

func IsWrongEndpoint(err error) bool { return isWrongEndpoint(err) }

func LearnZenEndpoint(modelID string, upstream string) { learnZenEndpoint(modelID, upstream) }

func RecordZenKeyTokens(key string, tokens int64) { recordZenKeyTokens(key, tokens) }

func ResolveZenFreeModel(id string) (*ZenModel, bool) { return resolveZenFreeModel(id) }

func ResolveZenModel(id string) (*ZenModel, bool) { return resolveZenModel(id) }

func ResponsesSSEToChat(resp *http.Response) (map[string]any, error) { return responsesSSEToChat(resp) }

func RouteModel(id string) string { return routeModel(id) }

func ShouldServeWorkBuddy(m string) bool { return shouldServeWorkBuddy(m) }

func SyncZenModels() (int, error) { return syncZenModels() }

func Trae2apiRoutableCount() int { return trae2apiRoutableCount() }

func TraeNormalizeStreamDefault(body []byte) []byte { return traeNormalizeStreamDefault(body) }

func UsageTokens(u map[string]any, params map[string]any) int64 { return usageTokens(u, params) }

func ZenClientErrorStatus(err error) int { return zenClientErrorStatus(err) }

func ZenKeyCooling(key string) bool { return zenKeyCooling(key) }

func ZenKeyRoutingEnabled(key string) bool { return zenKeyRoutingEnabled(key) }

func ZenKeyStatus() []map[string]any { return zenKeyStatus() }

func ZenModelList() []map[string]any { return zenModelList() }

func ZenPrefixedID(id string) string { return zenPrefixedID(id) }

type ZenHTTPError = zenHTTPError

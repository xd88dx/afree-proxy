package app

// gateway 桥接层（2026-11-18 结构重构 gateway 批）：/v1 网关已出包
// internal/gateway，本文件把 app 侧既有小写消费面桥接过去——面板 handler、
// 清空数据、测试与 StartProxy 保留段零改动。

import (
	"io"
	"net/http"

	"afree-proxy/internal/gateway"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/other"
)

func servedModelGate(model string) (int, string) { return gateway.ServedModelGate(model) }

func workbuddyModelEntries() []map[string]any { return gateway.WorkbuddyModelEntries() }

func otherModelList() []map[string]any { return gateway.OtherModelList() }

func RoutePlatformKeyOf(model string) string { return gateway.RoutePlatformKeyOf(model) }

func ModelRoutePlatformKey(model string) string { return gateway.ModelRoutePlatformKey(model) }

func BannedPlatformOf(model string) string { return gateway.BannedPlatformOf(model) }

func writeJSON(w http.ResponseWriter, status int, data any) { gateway.WriteJSON(w, status, data) }

func corsHandler(h http.HandlerFunc) http.HandlerFunc { return gateway.CORSHandler(h) }

func usageFromChatPayload(body string) map[string]any { return gateway.UsageFromChatPayload(body) }

func probeRespTokens(resp *http.Response, record func(tokens int64)) {
	gateway.ProbeRespTokens(resp, record)
}

func takeUsedKey(resp *http.Response) string { return gateway.TakeUsedKey(resp) }

func platformUseProxies(kind string) *bool { return gateway.PlatformUseProxies(kind) }

func setPlatformUseProxies(kind string, v *bool) { gateway.SetPlatformUseProxies(kind, v) }

func listCombos() []*gwconfig.Combo { return gateway.ListCombos() }

func resolveCombo(id string) *gwconfig.Combo { return gateway.ResolveCombo(id) }

func addCombo(id, platform, target string, useProxies bool) (*gwconfig.Combo, error) {
	return gateway.AddCombo(id, platform, target, useProxies)
}

func deleteCombo(id string) bool { return gateway.DeleteCombo(id) }

func otherExitFn(p *other.Platform) func(string) (string, bool) { return gateway.OtherExitFn(p) }

func otherBoundRoutable(p *other.Platform) func(string) bool { return gateway.OtherBoundRoutable(p) }

func otherPlatformHasRoutableKey(p *other.Platform) bool {
	return gateway.OtherPlatformHasRoutableKey(p)
}

func comboPlatformValid(p string) bool { return gateway.ComboPlatformValid(p) }

func classifyReqRoute(r *http.Request, model string) (string, string) {
	return gateway.ClassifyReqRoute(r, model)
}

func collectStreamResponse(upstream *http.Response, hintModel ...string) (map[string]any, error) {
	return gateway.CollectStreamResponse(upstream, hintModel...)
}

func servedModelEntries() []map[string]any { return gateway.ServedModelEntries() }

func servedModelOf(resp map[string]any, requested string) string {
	return gateway.ServedModelOf(resp, requested)
}

func modelIsServed(model string) bool { return gateway.ModelIsServed(model) }

func handleStreamBufferedGeneric(w http.ResponseWriter, r *http.Request, plat string, params map[string]any, attempt gateway.StreamAttemptFn) {
	gateway.HandleStreamBufferedGeneric(w, r, plat, params, attempt)
}

// ---- moved 符号别名（面板/测试零改动）----

type platformChatCall = gateway.PlatformChatCall

func handleZenChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	gateway.HandleZenChat(w, r, params)
}

func handleZenResponsesNative(w http.ResponseWriter, r *http.Request, params map[string]any, zm *ZenModel) bool {
	return gateway.HandleZenResponsesNative(w, r, params, zm)
}

func emitChatAsSSE(w http.ResponseWriter, chat map[string]any, usageFn func(map[string]any)) {
	gateway.EmitChatAsSSE(w, chat, usageFn)
}

func bufferUpstreamSSE(body io.Reader) (lines []string, usage map[string]any, complete bool, brk error) {
	return gateway.BufferUpstreamSSE(body)
}

func handleClineStreamBuffered(w http.ResponseWriter, r *http.Request, params map[string]any, useProxies bool) {
	gateway.HandleClineStreamBuffered(w, r, params, useProxies)
}

func clientWantsStreamUsage(params map[string]any) bool {
	return gateway.ClientWantsStreamUsage(params)
}

func isUsageOnlyChunk(obj map[string]any) bool { return gateway.IsUsageOnlyChunk(obj) }

func replaySSE(w http.ResponseWriter, lines []string, keepUsageOnly bool) {
	gateway.ReplaySSE(w, lines, keepUsageOnly)
}

func handleNonStreamResponseWithUsage(w http.ResponseWriter, upstream *http.Response, onUsage func(map[string]any), stops []string) {
	gateway.HandleNonStreamResponseWithUsage(w, upstream, onUsage, stops)
}

func withStreamUsage(params map[string]any) { gateway.WithStreamUsage(params) }

func parseToolArgs(raw string) (any, error) { return gateway.ParseToolArgs(raw) }

func handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	gateway.HandleAnthropicMessages(w, r)
}

func getNested(obj map[string]any, keys ...any) any { return gateway.GetNested(obj, keys...) }

func sortModelEntriesByID(list []map[string]any) { gateway.SortModelEntriesByID(list) }

func handleResponses(w http.ResponseWriter, r *http.Request) { gateway.HandleResponses(w, r) }

func rejectCaller(msg string, authError bool) platformChatCall {
	return gateway.RejectCaller(msg, authError)
}

func isContextOverflow(msg string) bool { return gateway.IsContextOverflow(msg) }

func writeUpstreamAPIError(w http.ResponseWriter, model string, err error, fallbackStatus int) {
	gateway.WriteUpstreamAPIError(w, model, err, fallbackStatus)
}

func newTextCallGateway(w http.ResponseWriter, names []string, model string) *gateway.TextCallGW {
	return gateway.NewTextCallGateway(w, names, model)
}

func migrateTraeComboTarget(target string) (string, bool) {
	return gateway.MigrateTraeComboTarget(target)
}

func serveTrae2apiChat(rw http.ResponseWriter, r *http.Request, body []byte) {
	gateway.ServeTrae2apiChat(rw, r, body)
}

func zenAutoBanRegion(err error, zm *ZenModel) { gateway.ZenAutoBanRegion(err, zm) }

func repairToolArguments(args string) string { return gateway.RepairToolArguments(args) }

func chatToolArguments(args string) string { return gateway.ChatToolArguments(args) }

func upstreamErrorStatus(err error, fallback int) int {
	return gateway.UpstreamErrorStatus(err, fallback)
}

func validateCombo(id, platform, target string) error {
	return gateway.ValidateCombo(id, platform, target)
}

type upstreamHTTPError = gateway.UpstreamHTTPError

func filterToolInput(name string, input map[string]any, schemas map[string]map[string]bool) map[string]any {
	return gateway.FilterToolInput(name, input, schemas)
}

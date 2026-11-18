package app

// wb_bridge.go —— app 侧对 internal/workbuddy 域的小写消费面别名（2026-11-18
// P3e：wb 平台域出包 internal/workbuddy，本文件让既有消费文件零改动）。
// P4 收尾时消费方改直接 workbuddy.X 后本文件只留编排钩子注入。

import (
	"afree-proxy/internal/reqlog"

	"context"
	"time"

	wbbridge "github.com/misakano7545/workbuddy2api-panel"

	"afree-proxy/internal/workbuddy"
)

// ---- 类型 ----

type workbuddyProxyBinding = workbuddy.WorkbuddyProxyBinding

type WorkbuddySubsystem = workbuddy.WorkbuddySubsystem

type workbuddySubsystem = workbuddy.WorkbuddySubsystem

type wbShareSuggestion = workbuddy.WbShareSuggestion

// ---- 绑定段内存表（指针别名，handler 直接读写）----

var wbProxyMu = &workbuddy.WbProxyMu
var WorkbuddySub = &workbuddy.WorkbuddySub

func workbuddyProxyBindingsSnapshot() ([]string, map[string]workbuddyProxyBinding) {
	return workbuddy.WorkbuddyProxyBindingsSnapshot()
}

func setWorkbuddyProxyBinding(uid, main, backup string) error {
	return workbuddy.SetWorkbuddyProxyBinding(uid, main, backup)
}

func workbuddyProxyFor(a *wbbridge.Auth) (string, bool) { return workbuddy.WorkbuddyProxyFor(a) }

var wbProxyBindings = &workbuddy.WbProxyBindings
var wbProxyLoaded = &workbuddy.WbProxyLoaded

func loadWorkbuddyProxyBindingsLocked() { workbuddy.LoadWorkbuddyProxyBindingsLocked() }

func saveWorkbuddyProxyBindingsLocked() { workbuddy.SaveWorkbuddyProxyBindingsLocked() }

func saveWorkbuddyConfig(path string, raw []byte, live *wbbridge.LivecfgHolder, p *wbbridge.Pool,
	up *wbbridge.UpClient, sch *wbbridge.Scheduler, gw *wbbridge.Handler, pn *wbbridge.Panel,
	authDir, stateFile string) ([]string, error) {
	return workbuddy.SaveWorkbuddyConfig(path, raw, live, p, up, sch, gw, pn, authDir, stateFile)
}

var workbuddySub = &workbuddy.WorkbuddySub

func startWorkBuddy() (*workbuddy.WorkbuddySubsystem, error) { return workbuddy.StartWorkBuddy() }

func isWorkBuddyModel(model string) bool { return workbuddy.IsWorkBuddyModel(model) }

func wbVendorToRealm(model string) string { return workbuddy.WbVendorToRealm(model) }

func wbRealmToVendor(model string) string { return workbuddy.WbRealmToVendor(model) }

func computeShareSuggestions(list []wbbridge.PoolStatus, now time.Time) ([]workbuddy.WbShareSuggestion, []string) {
	return workbuddy.ComputeShareSuggestions(list, now)
}

func shareMapsEqual(a, b map[string]float64) bool { return workbuddy.ShareMapsEqual(a, b) }

var wkbdConfigPathOverride = &workbuddy.WkbdConfigPathOverride

func wkbdConfigPath() string { return workbuddy.WkbdConfigPath() }

var wkbdCfgMu = &workbuddy.WkbdCfgMu
var wkbdCfgLoaded = &workbuddy.WkbdCfgLoaded
var wkbdCfg = &workbuddy.WkbdCfg

func workbuddyObserveAccount(ctx context.Context, a *wbbridge.Auth, exitRaw string) {
	workbuddy.WorkbuddyObserveAccount(ctx, a, exitRaw)
}

func shouldServeWorkBuddy(model string) bool { return workbuddy.ShouldServeWorkBuddy(model) }

func workbuddyProxiesEnabled() bool { return workbuddy.WbProxiesEnabled() }

func mutateWkbdConfig(fn func(*workbuddy.WkbdConfigData)) { workbuddy.MutateWkbdConfig(fn) }

type wkbdConfigData = workbuddy.WkbdConfigData

func clearAllWorkbuddyProxyBindings() int { return workbuddy.ClearAllWorkbuddyProxyBindings() }

const (
	wbRealmPrefixCN         = workbuddy.WbRealmPrefixCN
	wbRealmPrefixGlobal     = workbuddy.WbRealmPrefixGlobal
	vendorRealmPrefixCN     = workbuddy.VendorRealmPrefixCN
	vendorRealmPrefixGlobal = workbuddy.VendorRealmPrefixGlobal
)

func wkbdConfigUseProxies() *bool { return workbuddy.WkbdConfigUseProxies() }

// ---- Host 注入：请求日志槽位 / 面板日志 sink / 权重优化循环 ----

func init() {
	workbuddy.Host.WriteJSON = writeJSON
	workbuddy.Host.LogUpstreamInfo = reqlog.SetUpstreamInfo
	workbuddy.Host.LogUpstreamExit = reqlog.SetUpstreamExit
	// AttachWorkbuddyLogSink / StartWorkbuddyShareOptimizeLoop 已随 handler
	// 拆回本包（wb_handlers.go），直接注入。
	workbuddy.Host.AttachLogSink = AttachWorkbuddyLogSink
	workbuddy.Host.StartShareOptimizeLoop = StartWorkbuddyShareOptimizeLoop
}

package app

// cline 桥接层（2026-11-17 结构重构 P3c）：cline 平台已出包 internal/cline
//（池/痕迹/流式学习/类型），本文件把 app 侧既有小写消费面桥接过去——类型
// 别名 + 同签名包装 + poolMu 指针别名（同一实例），消费文件零改动。
// Host 钩子在 init 注入（全局配置读/走池开关/模型规范化/egress 重算/
// 指纹头升级/上游调用闭包）。

import (
	"context"
	"net/http"
	"time"

	"afree-proxy/internal/cline"
)

// ---- 类型别名 ----

type (
	Account       = cline.Account
	AccountPool   = cline.AccountPool
	AccountView   = cline.AccountView
	LoginMethod   = cline.LoginMethod
	accountUsage  = cline.AccountUsage
	poolUsageData = cline.PoolUsageData
)

// poolMu 指针别名：与 cline.PoolMu 同一实例（测试直用 poolMu.Lock 零改动）。
var poolMu = &cline.PoolMu

// ---- 池读写 ----

func loadPool() *AccountPool { return cline.LoadPool() }

func savePool() { cline.SavePool() }

func savePoolLocked() { cline.SavePoolLocked() }

func mutatePoolConfig(fn func(p *AccountPool)) { cline.MutateConfig(fn) }

func poolDataPath() string { return cline.DataPath() }

func poolAccountCount() int { return cline.PoolAccountCount() }

func poolStatusSnapshot() (total, active int) { return cline.PoolStatusSnapshot() }

func poolStatusCounts() (total, active, cooldown, expired int) {
	return cline.PoolStatusCounts()
}

func accountPoolEnabled(a *Account) bool { return cline.AccountPoolEnabled(a) }

func accountStatusLocked(a *Account) string { return cline.AccountStatusLocked(a) }

func setAccountPoolEnabled(accountID string, enabled bool) error {
	return cline.SetAccountPoolEnabled(accountID, enabled)
}

func setAllAccountsPoolEnabled(enabled bool) int { return cline.SetAllAccountsPoolEnabled(enabled) }

func poolCurrentIdx() int { return cline.PoolCurrentIdx() }

func reorderAccounts(order []string) (int, error) { return cline.ReorderAccounts(order) }

func addAccount(acc *Account) { cline.AddAccount(acc) }

func removeAccount(accountID string) bool { return cline.RemoveAccount(accountID) }

func getAccountByID(accountID string) *Account { return cline.GetAccountByID(accountID) }

func refreshAccountToken(acc *Account) error { return cline.RefreshAccountToken(acc) }

func ensureAccountToken(acc *Account) (string, error) { return cline.EnsureToken(acc) }

func ListAccounts() []*AccountView { return cline.ListAccounts() }

func markAccountCooldown(acc *Account, reason string, d time.Duration) time.Time {
	return cline.MarkAccountCooldown(acc, reason, d)
}

func bumpUsage(acc *Account) { cline.BumpUsage(acc) }

func resetTodayUsage(acc *Account) { cline.ResetTodayUsage(acc) }

func recordAccountTokens(acc *Account, tokens int64) { cline.RecordAccountTokens(acc, tokens) }

func describePoolStatus() string { return cline.DescribePoolStatus() }

func AddAccountFromDeviceAuth() (*Account, error) { return cline.AddAccountFromDeviceAuth() }

func pickAccount() *Account { return cline.PickAccount() }

func pickAccountExcluding(exclude map[string]bool) *Account {
	return cline.PickAccountExcluding(exclude)
}

func pickAccountAny() *Account { return cline.PickAccountAny() }

// persistProxyConfig 面板保存请求头（策略/代理段在各自域）：cline 侧只收头。
func persistProxyConfig(cfg *proxyConfigData) { cline.SetHeaders(cfg.Headers) }

// clineAPIError 上游错误类型（proxy.go 分发链判 429/5xx 换号）。
type clineAPIError = cline.APIError

func accountNumber(acc *Account) int { return cline.AccountNumber(acc) }

const maxCooldown = cline.MaxCooldown

// ---- 全局配置读的 cline 视角（真身在 gwconfig/全局段）----

func poolAdminLogEnabled() bool { return globalAdminLogEnabled() }

func poolKeysSnapshot() []string { return globalClientKeys() }

func clineUseProxiesPtr() *bool {
	// 直读池字段（原语义）：不得调 platformUseProxies("cline")——分发器
	// 的 cline 分支就是本函数，桥接层回环即栈溢出（P3c 全量实测）。
	p := cline.LoadPool()
	if p.UseProxies == nil {
		return nil
	}
	v := *p.UseProxies
	return &v
}

func poolClineUseProxies() bool {
	if v := clineUseProxiesPtr(); v != nil {
		return *v
	}
	return true
}

func globalClineCatalogFeedOnly() bool { return cline.CatalogFeedOnly() }

func setGlobalClineCatalogFeedOnly(v bool) { cline.SetCatalogFeedOnly(v) }

// ---- 痕迹（pool_usage）----

func poolUsagePath() string { return cline.UsagePath() }

func savePoolUsage() { cline.SaveUsage() }

func poolStateBackupSnapshotLocked() *poolUsageData { return cline.StateBackupSnapshotLocked() }

func importPoolStateFactsLocked(f *poolUsageData) { cline.ImportStateFactsLocked(f) }

func prunePoolUsageLocked(valid map[string]bool) { cline.PruneUsageLocked(valid) }

func AccountStatus(acc *Account) string { return cline.AccountStatus(acc) }

// ---- 内部真身访问（admin 数据管理路径）----

func markPoolDirtyLocked() { cline.MarkDirtyLocked() }

func clineExpiryMs(expiresAt any) int64 { return cline.ExpiryMs(expiresAt) }

func usageForLocked(acc *Account) *accountUsage { return cline.UsageForLocked(acc) }

func usagePeekLocked(acc *Account) *accountUsage { return cline.UsagePeekLocked(acc) }

func savePoolUsageLocked() { cline.SaveUsageLocked() }

func clearPoolUsageLocked() { cline.ClearUsageLocked() }

func importPoolStateBackupLocked(f *poolUsageData) { cline.ImportStateBackupLocked(f) }

func flushPoolNow() { cline.FlushNow() }

func poolDirtyFlag() bool { return cline.DirtyFlag() }

func startPoolFlusher() { cline.StartFlusher() }

func ensureRefreshFlightCaller() {}

// ---- 流式学习 ----

func clineStreamRequired(modelID string) bool { return cline.StreamRequired(modelID) }

func learnClineStreamRequired(modelID string) { cline.LearnStreamRequired(modelID) }

func loadClineStreamLearned() { cline.LoadStreamLearned() }

func saveClineStreamLatest() { cline.SaveStreamLatest() }

func saveClineStream(learned map[string]string) { cline.SaveStream(learned) }

func clineEmptyStreamErr(status int, body []byte) bool {
	return cline.EmptyStreamErr(status, body)
}

func callClineAutoStream(ctx context.Context, params map[string]any, stream, useProxies bool) (*http.Response, *Account, bool, error) {
	return cline.CallAutoStream(ctx, params, stream, useProxies)
}

// ---- Host 注入 ----

func init() {
	cline.Host.ClientKeys = globalClientKeys
	cline.Host.Strategy = globalStrategy
	cline.Host.AdminLogEnabled = globalAdminLogEnabled
	cline.Host.UseProxies = func() *bool { return platformUseProxies("cline") }
	cline.Host.NormalizeModel = normalizeRequestModel
	cline.Host.ReconcileEgress = reconcileEgressBindings
	cline.Host.NormalizeIdentityHeader = normalizeIdentityHeaders
	cline.Host.SyncPersistedHeaders = func(h map[string]string) {
		proxyConfigMu.Lock()
		proxyConfig.Headers = h
		proxyConfigMu.Unlock()
	}
	cline.Host.CallAPI = func(ctx context.Context, params map[string]any, stream, useProxies bool) (*http.Response, *Account, error) {
		return callClineAPI(ctx, params, stream, useProxies)
	}
}

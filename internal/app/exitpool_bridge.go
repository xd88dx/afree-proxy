package app

// exitpool 桥接层（2026-11-17 结构重构 P3b）：代理域数据与运行时已出包
// internal/exitpool，本文件把 app 侧既有小写消费面桥接过去——类型别名 +
// 同签名包装，消费文件零改动。P4 收尾时消费方改直接 import 后本文件只留
// setProxyPool 编排（冷却失效/健康清零/客户端驱逐/egress 重算依赖各消费
// 域，必须留在装配根）。

import (
	"errors"
	"net/http"
	"time"

	"afree-proxy/internal/cline"
	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/freepi"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/other"
)

// ---- kind 常量（出口冷却/记账的平台维度，历史小写名）----

const (
	pkCline  = exitpool.KindCline
	pkZen    = exitpool.KindZen
	pkOther  = exitpool.KindOther
	pkFreepi = exitpool.KindFreepi
	pkTrae   = exitpool.KindTrae
	pkWb     = exitpool.KindWb
)

// ---- 类型别名（备份段/面板 payload/测试照旧引用小写名）----

type proxyPoolConfigData = exitpool.PoolConfig

type proxyBindingConfigData = exitpool.BindingConfig

// ---- 池段 ----

func getProxyPool() *proxyPoolConfigData { return exitpool.GetPool() }

// defaultProxyPoolConfig 缺省池配置（数据清空等场景重建用）。
func defaultProxyPoolConfig() *proxyPoolConfigData {
	return &proxyPoolConfigData{Strategy: "round_robin"}
}

func proxiesChanged(a, b []string) bool { return exitpool.ProxiesChanged(a, b) }

func setProxyTestTarget(target string) { exitpool.SetTestTarget(target) }

func proxyClientFor(proxyURL string) *http.Client { return exitpool.ClientFor(proxyURL) }

// otherClientFor other 平台出站客户端工厂：Chrome 指纹（2026-11-19 用户定）。
// Bun 指纹只对 zen/opencode 上游有风控意义；普通 OpenAI 兼容上游用 Chrome，
// 也规避 agnes 2026-11-18 换证书后对 Bun ClientHello 的 TLS 兼容缺陷。
func otherClientFor(proxyURL string) *http.Client {
	return exitpool.ClientForProfile(proxyURL, exitpool.ProfileChrome)
}

func upstreamProxyForKind(kind string) (string, int, bool) {
	return exitpool.UpstreamForKind(kind)
}

func pickUpstreamProxyFor(kind string) (string, int) {
	// 包内直通（③ 步 pickUpstreamProxyFor 已随 ②-a 迁出；此处别名保历史调用点）。
	return exitpool.PickUpstreamProxyFor(kind)
}

func parseProxyLine(raw string) (canonical string, alias string, err error) {
	return exitpool.ParseProxyLine(raw)
}

func maskProxyURL(raw string) string { return kit.MaskProxyURL(raw) }

func proxyIdxInPool(raw string) int { return exitpool.IdxInPool(raw) }

func proxyAliasOf(line string) string { return exitpool.AliasOf(line) }

// ---- 健康统计/延迟测试（②-b 出包别名）----

type proxyHealthFile = exitpool.HealthFile

func recordProxyOutcome(kind, proxyURL string, ok bool, err error, hsMs int64) {
	exitpool.RecordProxyOutcome(kind, proxyURL, ok, err, hsMs)
}

func resetProxyHealthStats() { exitpool.ResetHealthStats() }

func flushProxyHealthManual() { exitpool.FlushHealthManual() }

func flushProxyHealth() { exitpool.FlushHealth() }

func ensureProxyHealthLoaded() { exitpool.EnsureHealthLoaded() }

func startProxyHealthFlushLoop() { exitpool.StartHealthFlushLoop() }

func proxyHealthSnapshot() []map[string]any { return exitpool.HealthSnapshot() }

func proxyHealthBackupSnapshot() *proxyHealthFile { return exitpool.HealthBackupSnapshot() }

func importProxyHealthBackup(f *proxyHealthFile) { exitpool.ImportHealthBackup(f) }

func proxyTestTarget() string { return exitpool.EffectiveTestTarget() }

// ---- 冷却 ----

func cooldownUpstreamProxy(idx int, d time.Duration) {
	exitpool.CooldownUpstreamProxy(idx, d)
}

func cooldownUpstreamProxyFor(kind string, idx int, d time.Duration) {
	exitpool.CooldownUpstreamProxyFor(kind, idx, d)
}

func markProxyManualCooled(idx int) { exitpool.MarkManualCooled(idx) }

func proxyCooldownStatus() map[string]string { return exitpool.CooldownStatus() }

func proxyCooldownUntil(raw string) time.Time { return exitpool.CooldownUntil(raw) }

func proxyCooldownsSnapshot() (map[int]time.Time, map[string]map[int]time.Time) {
	return exitpool.CooldownsSnapshot()
}

func resetProxyCooldownsForTest() { exitpool.ResetCooldownsForTest() }

// setExpiredCooldownForTest 测试写入原语：塞一条已过期的 timed 冷却
// （验证手动标记优先于过期记录的判定）。
func setExpiredCooldownForTest(idx int) {
	exitpool.SetCooldownForTest(idx, time.Now().Add(-time.Minute))
}

func shouldCooldownExit(kind, proxyURL string, err error) (string, bool) {
	return exitpool.ShouldCooldownExit(kind, proxyURL, err)
}

// proxyPlatformCooldown 平台级冷却时长（2026-10-30 用户定 10 分钟）。
const proxyPlatformCooldown = 10 * time.Minute

func proxyAvailable(idx int) bool { return exitpool.ProxyAvailableFor("", idx) }

func proxyAvailableFor(kind string, idx int) bool {
	return exitpool.ProxyAvailableFor(kind, idx)
}

func snapshotProxyManualCooled() map[string]bool { return exitpool.SnapshotManualCooled() }

func restoreProxyManualCooled(m map[string]bool) { exitpool.RestoreManualCooled(m) }

// platformCooldownRows 面板健康表随行的平台级冷却读数（exitpool 读接口包装）。
func platformCooldownRows(idx int) []exitpool.PlatformCooldownEntry {
	return exitpool.PlatformCooldowns(idx)
}

// ---- 隔离/绑定核心（P3b-③ 出包别名；消费面零改动）----

func proxyIsolationEnv() (bool, bool) { return exitpool.IsolationEnv() }

func proxyIsolationEnvLocked() bool { return exitpool.IsolationEnvLocked() }

func proxyIsolationEnabled() bool { return exitpool.IsolationEnabled() }

const egressDirect = exitpool.DirectSentinel

func isEgressDirect(v string) bool { return exitpool.IsDirect(v) }

func backupProxyEnabled() bool { return exitpool.BackupProxyEnabled() }

func resolveBackupSlot(submitted, storedBackup string) string {
	return exitpool.ResolveBackupSlot(submitted, storedBackup)
}

func pickBoundProxyFor(kind, main, backup string) (string, int, bool) {
	return exitpool.PickBoundProxyFor(kind, main, backup)
}

func boundProxiesRoutableFor(kind, main, backup string) bool {
	return exitpool.BoundProxiesRoutableFor(kind, main, backup)
}

func exitStateLabel(raw string) string { return exitpool.ExitStateLabel(raw) }

func boundExitDetail(main, backup string) string { return exitpool.BoundExitDetail(main, backup) }

func validateProxyBinding(main, backup string) error {
	return exitpool.ValidateBinding(main, backup)
}

func proxyExitType(proxyURL, main, backup string, bound bool) string {
	return exitpool.ProxyExitType(proxyURL, main, backup, bound)
}

func sameProxyAlias(a, b string) bool { return exitpool.SameAlias(a, b) }

// ---- 隔离/绑定段 ----

func getProxyBindingConfig() *proxyBindingConfigData { return exitpool.GetBinding() }

func setProxyBindingConfig(c *proxyBindingConfigData) { exitpool.SetBinding(c) }

// reorderKeepSec 生效阈值（秒）：未配置/非正值回落默认。
func reorderKeepSec() float64 {
	if v := getProxyBindingConfig().ReorderKeepSec; v > 0 {
		return v
	}
	return exitpool.DefaultReorderKeepSec
}

// proxyConfigPath 保留旧名（锁序注释与测试钩子迁移期可读性），指向
// exitpool 的路径解析。
func proxyConfigPath() string { return exitpool.ConfigPath() }

// setProxyPool 替换并落盘代理池配置，并承担代理清理链：列表变化时冷却全部
// 失效、在线率统计清零、被移除出口的钉定客户端驱逐。数据层委托 exitpool
// （ActivatePool = 换单例 + 落盘；ResetCooldownsForPoolEdit/EvictPoolClients
// 为清理原语），编排链依赖各消费域故留在本包。
func setProxyPool(c *proxyPoolConfigData) {
	exitpool.NormalizePool(c)
	old := exitpool.GetPool()
	// 池替换前快照手动冷却（索引→URL 必须按旧池解释：ActivatePool 之后
	// Snapshot 的 cfg 已是新池，索引会错位）。
	manualKept := snapshotProxyManualCooled()
	exitpool.ActivatePool(c)
	if proxiesChanged(old.Proxies, c.Proxies) {
		exitpool.ResetCooldownsForPoolEdit()
		// 健康统计按备注名剪除（2026-11-26 用户定，替代旧"编辑池即清零"）：
		// 别名仍在池 → 统计保留（订阅换端口连续）；真被删除 → 条目剪除。
		exitpool.PruneHealthForPool(c.Proxies)
		// 手动冷却按 URL/别名保留仍在新池的条目（2026-11-18 修复）：池编辑
		// 一律清零会让订阅同步（顺序变化即触发整池替换）丢掉手动封禁——
		// 同一代理仍在池中时封禁意图应延续。重置后按新池重锚定恢复（消失
		// 的 URL 由 RestoreManualCooled 丢弃），再把保留后的表同步落盘。
		restoreProxyManualCooled(manualKept)
		flushProxyHealthManual()
		exitpool.EvictPoolClients(validProxySet(c.Proxies))
	}
	// 池列表变化重算 egress 自动绑定：新代理分给空闲 key、被移除代理的绑定
	// 作废（key 变回"无出口"从而被 pickZenKey 过滤）。key 列表变化侧的挂钩
	// 在 setZenConfig。幂等：分配没变时不写盘。
	reconcileEgressBindings()
}

// validProxySet 池列表 → 直连哨兵+全部代理 的集合（客户端驱逐判据）。
func validProxySet(proxies []string) map[string]bool {
	valid := map[string]bool{"": true}
	for _, p := range proxies {
		valid[p] = true
	}
	return valid
}

// FlushManualCooledToState exitpool.FlushManualHook 的注入目标：手动冷却
// 段随 proxy-state.json 落盘（health 域在 ②-b 出包前暂留本包）。
func FlushManualCooledToState() { flushProxyHealthManual() }

// init 注册手动冷却落盘钩子与平台错误类型传输层判定
// （cline.StatusError / other.HTTPError 是平台业务语义，不算传输层失败）。
func init() {
	exitpool.FlushManualHook = FlushManualCooledToState
	exitpool.IsTransportFailureExtras = func(err error) bool {
		var ot *other.HTTPError
		if errors.As(err, &ot) {
			return false
		}
		var fe *freepi.HTTPError
		if errors.As(err, &fe) {
			return false
		}
		var ce *cline.StatusError
		if errors.As(err, &ce) {
			return false
		}
		return true
	}
}

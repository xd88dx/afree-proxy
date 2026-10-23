package app

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/tokenharbor"
)

// ============ 账号/key 代理隔离（绑定出口） ============
//
// 目标：固定"身份 ↔ 出口 IP"的对应关系，让上游风控看到的是"一个账号一个
// 固定 IP"，而不是旧规则下"每个账号的 IP 在整个代理池里每请求漂移 + 同一
// 出口 IP 上轮流出现多个账号"——两者都是典型风控红旗。
//
// 规则（默认启用）：
//   - 每个账号 / zen key 可绑定一个主代理和一个备用代理；请求永远从绑定出口
//     发出，主可用走主，主不可用走备（备用代理开关打开时）。
//   - 两者都不可用（冷却中，或已从代理池删除）时，该身份在选号阶段被整体
//     跳过 —— 隔离优先于可用性，绝不退回其他出口或直连。
//   - 未绑定任何代理的身份沿用全局规则（cline/zen/workbuddy 各看面板的全局
//     走池开关，nil 默认开启），因此空配置部署的行为与隔离启用前完全一致。
//   - 收割机的 CLI 铸造同样走该 key 的绑定代理（同一个会话 ID 必须始终来自
//     同一个 IP，否则隔离形同虚设）；绑定出口不可用时本轮跳过该 key 的铸造。
//
// 开关：
//   - ProxyIsolation（nil = 默认启用），PROXY_ISOLATION env 只接受 true/false：
//     true 强制开启且面板只读；false 仅把默认值改为关闭，面板仍可修改。关闭
//     即完全回到旧版"请求级全局轮转"，绑定字段被忽略。
//   - BackupProxyEnabled（nil = 默认关闭）：关闭时备用槽整体失效，主代理不可用
//     即跳过该身份，不再尝试备用代理 —— 隔离语义不变，依然绝不回退直连。

// proxyIsolationEnv 解析 PROXY_ISOLATION：只接受 true/false（大小写不敏感），
// 其他值告警并按未设置处理。返回 (值, 是否有效设置)。
func proxyIsolationEnv() (bool, bool) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PROXY_ISOLATION")))
	switch v {
	case "true":
		return true, true
	case "false":
		return false, true
	case "":
		return false, false
	}
	fmt.Printf("  WARNING: PROXY_ISOLATION only accepts true/false, got %q; treated as unset\n", v)
	return false, false
}

// proxyIsolationEnvLocked PROXY_ISOLATION=true 时为真：隔离强制开启，面板
// 下拉只读。false 不锁定 —— 它只是把默认值改为关闭，面板仍可修改。
func proxyIsolationEnvLocked() bool {
	v, ok := proxyIsolationEnv()
	return ok && v
}

// proxyIsolationEnabled 账号/key 代理隔离当前是否启用：
//   - PROXY_ISOLATION=true：强制开启（无视面板设置）。
//   - PROXY_ISOLATION=false：默认关闭；面板显式设置过（ProxyIsolation 非 nil）
//     则按面板值。
//   - 未设置：默认开启；面板显式设置过则按面板值（nil 视为开启）。
func proxyIsolationEnabled() bool {
	if v, ok := proxyIsolationEnv(); ok {
		if v {
			return true
		}
		cfg := getZenConfig()
		return cfg.ProxyIsolation != nil && *cfg.ProxyIsolation
	}
	cfg := getZenConfig()
	return cfg.ProxyIsolation == nil || *cfg.ProxyIsolation
}

// proxyIdxInPool 代理在当前池列表中的索引；不在池中（已被删除）返回 -1。
// proxyAliasOf 取代理行的备注名（#fragment）；无备注返回空串。
// 备注名是代理的"身份标识"：订阅/服务端更新导致端口、凭据变化时，
// 备注名相同即视为同一代理（2026-10-18 用户定）。
func proxyAliasOf(line string) string {
	if i := strings.LastIndexByte(line, '#'); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

func proxyIdxInPool(raw string) int {
	if raw == "" {
		return -1
	}
	cfg := getZenConfig()
	for i, p := range cfg.Proxies {
		if p == raw {
			return i
		}
	}
	// 备注名 fallback：绑定行与池行备注名相同即命中（先精确后模糊）。
	// 无备注名的行不参与模糊匹配，仍按精确 URI —— 避免任意无备注 URI 互通。
	if a := proxyAliasOf(raw); a != "" {
		for i, p := range cfg.Proxies {
			if proxyAliasOf(p) == a {
				return i
			}
		}
	}
	return -1
}

// proxyAvailableURL 绑定代理当前是否可用：必须仍在池列表中且未冷却。
// 已从池中删除的绑定一律视为不可用 —— 隔离语义下宁可跳过身份，也不能让它
// 静默改走直连（那就把服务器本机 IP 暴露给了本应隔离的身份）。
func proxyAvailableURL(raw string) bool {
	idx := proxyIdxInPool(raw)
	if idx < 0 {
		return false
	}
	return zenProxyAvailable(idx)
}

// egressDirect 直连哨兵：绑定主/备用槽位存 "direct" 表示该槽位选择直连出口。
// 主 = direct → 该身份强制直连（备无意义）；主 = 代理、备 = direct → 主不可用
// 时直连兜底。哨兵不会与真实代理 URL 冲突（代理必须带 scheme://host:port）。
const egressDirect = "direct"

// isEgressDirect 判断槽位值是否为直连哨兵。
func isEgressDirect(v string) bool { return v == egressDirect }

// backupProxyEnabled 备用代理开关（zen 配置持久化 BackupProxyEnabled）：
// nil = 未配置 = 关闭（默认）。关闭时"主不通走备"的兜底不存在，主出口不可用
// 就等于该身份本轮不可用（调用方跳过）—— 隔离优先不变，仍然绝不回退直连。
func backupProxyEnabled() bool {
	v := getZenConfig().BackupProxyEnabled
	return v != nil && *v
}

// resolveBackupSlot 按备用代理开关决定落盘的备用槽值。
//
// 开关开启：备用槽参与路由，采纳调用方提交的值。
// 开关关闭：备用槽整体退出路由（pickBoundProxy 在此分支直接返回 ok=false），
//   面板把备用槽锁成只读并跟随主槽，提交上来的值只是显示值而非用户意图，
//   此时保留该身份已持久化的旧备用槽原值 —— 重开开关即可复用，无需重选。
//   保留而非清空是刻意的：清空会让"关一下开关"变成不可逆的数据丢失。
//
// 旧值由调用方从当前绑定里取出（storedBackup），空串表示此前没有备用槽。
func resolveBackupSlot(submitted, storedBackup string) string {
	if backupProxyEnabled() {
		return submitted
	}
	return storedBackup
}

// pickBoundProxy 为一次上游尝试从绑定对中选可用出口：主优先，备兜底
// （备用代理开关关闭时备用槽整体失效，退化为"只看主槽"）。直连哨兵视为"总是
// 可用"的出口（返回 ("", -1, true)，调用方按直连发出）。两者都不可用返回
// ok=false —— 调用方（隔离模式）必须跳过该身份。主备相同视为一个出口
// （面板已阻止这样配置，防御性兜底）。
// pickBoundProxy 为一次上游尝试从绑定对中选可用出口。返回的 proxyURL 是
// **池内当前地址**：绑定行可能因订阅更新只保留旧端口/凭据（备注名相同即
// 同一代理），实际拨号必须用池内最新 URI，否则旧地址已死。
func pickBoundProxy(main, backup string) (proxyURL string, idx int, ok bool) {
	if isEgressDirect(main) {
		return "", -1, true
	}
	if main != "" {
		if idx := proxyIdxInPool(main); idx >= 0 && zenProxyAvailable(idx) {
			return getZenConfig().Proxies[idx], idx, true
		}
	}
	if !backupProxyEnabled() {
		return "", -1, false
	}
	if isEgressDirect(backup) {
		return "", -1, true
	}
	if backup != "" && backup != main {
		if idx := proxyIdxInPool(backup); idx >= 0 && zenProxyAvailable(idx) {
			return getZenConfig().Proxies[idx], idx, true
		}
	}
	return "", -1, false
}

// boundProxiesRoutable 绑定对里是否还有可用出口。双空 = 未绑定 = 可路由
// （未绑定身份走全局规则，不算被隔离挡住）。备用代理开关关闭时只剩备用槽的绑定
// （主槽为空）同样不可路由 —— 那种状态面板产不出来，只在手改配置文件时出现，
// 一律按"该身份没有出口"处理并跳过，不退回全局规则（那可能是直连）。
func boundProxiesRoutable(main, backup string) bool {
	if main == "" && backup == "" {
		return true
	}
	_, _, ok := pickBoundProxy(main, backup)
	return ok
}

// ============ 出口不可用的可读详情 ============
//
// 绑定出口全部冷却/被移除时，各平台的探测（面板 Test）会直接失败。旧文案
// "bound proxies unavailable (cooling or removed)" 只说了结论：既看不出是
// 哪个出口，也看不出是短暂冷却还是节点已从池里删除 —— 用户据此会误判成
// "这个 key 废了"并把好凭据删掉。下面把出口（打码）与冷却解除时刻写进文案。

// exitStateLabel 单个出口槽位的可读状态。raw 为空表示未配置该槽。
func exitStateLabel(raw string) string {
	if raw == "" {
		return "none"
	}
	label := maskProxyURL(raw)
	if proxyIdxInPool(raw) < 0 {
		return label + " (removed from proxy pool)"
	}
	if until := zenProxyCooldownUntil(raw); !until.IsZero() {
		return label + " (cooling until " + until.UTC().Format(time.RFC3339) + ")"
	}
	return label + " (unreachable)"
}

// boundExitDetail 绑定对的可读详情："main=… (…), backup=…"。
func boundExitDetail(main, backup string) string {
	return "main=" + exitStateLabel(main) + ", backup=" + exitStateLabel(backup)
}

// blockedExitMessage 供各平台探测失败时复用：前缀 + 出口详情（有详情才拼）。
// scope 用于 other 子平台定位（其余平台传空串）。
func blockedExitMessage(prefix string, main, backup string) string {
	if main == "" && backup == "" {
		return prefix
	}
	return prefix + ": " + boundExitDetail(main, backup) + " " +
		"— the credential itself was not verified (its egress is down)"
}

// proxyBlockedDetailFor 是注入给 kit 的实现：按 scope + key 查出绑定再格式化。
//
// scope 取值："amd" / "openrouter" / "tokenharbor"，或 other 子平台的 slug。
// zen 与 cline 在主包内直接调用 blockedExitMessage，不走这个注入点。
func proxyBlockedDetailFor(scope, key string) string {
	if key == "" {
		return ""
	}
	var main, backup string
	var bound bool
	switch scope {
	case "amd":
		main, backup, bound = amd.BindingOf(key)
	case "openrouter":
		main, backup, bound = openrouter.BindingOf(key)
	case "tokenharbor":
		main, backup, bound = tokenharbor.BindingOf(key)
	default:
		if scope != "" {
			if p := other.PlatformBySlug(scope); p != nil {
				main, backup, bound = other.BindingOf(p, key)
			}
		}
	}
	if !bound {
		return ""
	}
	return boundExitDetail(main, backup)
}

func init() {
	kit.ProxyBlockedDetail = proxyBlockedDetailFor
}

// zenProxiesEnabled OpenCode 上游是否走共享代理池：配置缺省（nil）默认开启
// （走池）；面板下拉可显式选择走池/直连。
func zenProxiesEnabled() bool {
	zup := getZenConfig().ZenUseProxies
	if zup == nil {
		return true
	}
	return *zup
}

// workbuddyProxiesEnabled WorkBuddy 未绑定账号是否走共享代理池，语义与
// zenProxiesEnabled 一致：缺省（nil）默认开启（走池）。
func workbuddyProxiesEnabled() bool {
	wup := getZenConfig().WorkbuddyUseProxies
	if wup == nil {
		return true
	}
	return *wup
}

// zenAttemptExit 为一次 zen 上游尝试解析出口。key 有绑定时（隔离模式）主→备，
// 双不可用返回 ok=false —— 调用方对 pinned 探测直接报错，正常请求则短冷却
// 该 key 换下一个（绝不退回其他出口，否则粘性会话的 IP 就漂移了）；未绑定
// key 沿用全局轮转（未配置代理则直连）。
func zenAttemptExit(key string) (proxyURL string, idx int, ok bool) {
	main, backup, bound := zenKeyBindingOf(key)
	if !bound {
		if !zenProxiesEnabled() {
			return "", -1, true // 面板选择直连：即使代理列表非空也不走池
		}
		proxyURL, idx = pickUpstreamProxy()
		return proxyURL, idx, true
	}
	return pickBoundProxy(main, backup)
}

// proxyExitType 请求日志"代理类型"列的取值：绑定主代理返回 "main"，绑定备
// 代理返回 "backup"，直连出口（绑定的直连哨兵或未绑定直连）返回 "direct"；
// 全局代理池轮转或未命中上游返回 ""（面板显示 -）。
func proxyExitType(proxyURL, main, backup string, bound bool) string {
	if bound {
		if proxyURL == "" {
			return "direct"
		}
		// pickBoundProxy 返回的是池内当前地址，与绑定行可能只差端口/凭据
		//（备注名相同）—— 备注名一致即视为命中该槽位。
		if proxyURL == main || sameProxyAlias(proxyURL, main) {
			return "main"
		}
		if proxyURL == backup || sameProxyAlias(proxyURL, backup) {
			return "backup"
		}
		return ""
	}
	if proxyURL == "" {
		return "direct"
	}
	return ""
}

// sameProxyAlias 两个代理行备注名相同（且非空）即视为同一代理。
func sameProxyAlias(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	alias := proxyAliasOf(a)
	return alias != "" && alias == proxyAliasOf(b)
}

// accountProxyBinding 账号的绑定出口；未启用隔离或未绑定时 bound=false，
// 调用方沿用全局代理规则。绑定字段的写入方（面板）持有 poolMu，读取也
// 短暂持锁，避免与面板写并发竞态。
func accountProxyBinding(acc *Account) (main, backup string, bound bool) {
	if acc == nil || !proxyIsolationEnabled() {
		return "", "", false
	}
	poolMu.Lock()
	main, backup = acc.ProxyMain, acc.ProxyBackup
	poolMu.Unlock()
	if main == "" && backup == "" {
		return "", "", false
	}
	return main, backup, true
}

// zenKeyBindingOf zen key 的绑定出口；未启用隔离或未绑定时 bound=false。
func zenKeyBindingOf(key string) (main, backup string, bound bool) {
	if key == "" || !proxyIsolationEnabled() {
		return "", "", false
	}
	cfg := getZenConfig()
	b, ok := cfg.KeyBindings[key]
	if !ok || (b.Main == "" && b.Backup == "") {
		return "", "", false
	}
	return b.Main, b.Backup, true
}

// validateProxyBinding 校验一对绑定：代理槽位要么为空（全局）、要么是当前
// 代理池里真实存在的代理、要么是直连哨兵；主备不能是同一个代理（否则"备"
// 毫无意义，还掩盖配置错误）。主 = 直连时备只能是空或直连（UI 会自动跟随
// 锁定）。允许 socks5 —— 网关上游请求支持全部协议；收割机 CLI 的 socks5 由
// 本地桥兑现（socks_bridge.go），不依赖 CLI 自身。
//
// main == backup 这条在备用代理开关关闭时放行：关闭态下备用槽只是被保留、并不
// 参与路由（pickBoundProxy 会在该分支直接返回 ok=false），面板又把备用槽锁成
// 跟随主槽，用户完全无法主动制造这个组合 —— 它只可能来自"关闭前 main=a /
// backup=b，之后把 main 改成 b"这类正常操作。拦住它只会让用户改不动主槽。
func validateProxyBinding(main, backup string) error {
	for _, p := range []string{main, backup} {
		if p == "" || isEgressDirect(p) {
			continue
		}
		if proxyIdxInPool(p) < 0 {
			return fmt.Errorf("proxy %q is not in the current proxy pool; add it on the Proxy pool page first", maskProxyURL(p))
		}
		if _, err := url.Parse(p); err != nil {
			return fmt.Errorf("invalid proxy url %q", maskProxyURL(p))
		}
	}
	if isEgressDirect(main) && backup != "" && !isEgressDirect(backup) {
		return fmt.Errorf("main = direct means no proxy at all; the backup slot must also be direct")
	}
	if main != "" && !isEgressDirect(main) && main == backup && backupProxyEnabled() {
		return fmt.Errorf("main and backup proxy must differ (same URL makes the backup meaningless)")
	}
	return nil
}

// setAccountProxyBinding 设置/清除单个账号的绑定（面板入口）。
// main/backup 传空串即清除；全空 = 解除绑定。
func setAccountProxyBinding(accountID, main, backup string) error {
	main = strings.TrimSpace(main)
	backup = strings.TrimSpace(backup)
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	var storedBackup string
	for _, a := range p.Accounts {
		if a.AccountID == accountID {
			storedBackup = a.ProxyBackup
			backup = resolveBackupSlot(backup, storedBackup)
			if err := validateProxyBinding(main, backup); err != nil {
				return err
			}
			a.ProxyMain = main
			a.ProxyBackup = backup
			markPoolDirtyLocked()
			return nil
		}
	}
	return fmt.Errorf("account not found: %s", accountID)
}

// setZenKeyProxyBinding 设置/清除单个 zen key 的绑定（面板入口，按索引定位）。
// 写时复制：zenConfig 是被请求路径并发读取的活配置，先整体替换再 setZenConfig。
func setZenKeyProxyBinding(index int, main, backup string) error {
	main = strings.TrimSpace(main)
	backup = strings.TrimSpace(backup)
	cfg := getZenConfig()
	if index < 0 || index >= len(cfg.Keys) {
		return fmt.Errorf("key index out of range")
	}
	key := cfg.Keys[index]
	if key == "" || key == "public" {
		return fmt.Errorf("the anonymous public key has no credential to bind")
	}
	backup = resolveBackupSlot(backup, cfg.KeyBindings[key].Backup)
	if err := validateProxyBinding(main, backup); err != nil {
		return err
	}
	next := *cfg
	bindings := make(map[string]zenProxyBinding, len(cfg.KeyBindings)+1)
	for k, v := range cfg.KeyBindings {
		bindings[k] = v
	}
	if main == "" && backup == "" {
		delete(bindings, key)
	} else {
		bindings[key] = zenProxyBinding{Main: main, Backup: backup}
	}
	next.KeyBindings = bindings
	setZenConfig(&next)
	return nil
}

// clearAllAccountProxies 一键清空全部账号的代理绑定（面板入口）：
// 所有账号回到"全局"默认值。
func clearAllAccountProxies() int {
	p := loadPool()
	poolMu.Lock()
	n := 0
	for _, a := range p.Accounts {
		if a.ProxyMain == "" && a.ProxyBackup == "" {
			continue
		}
		a.ProxyMain = ""
		a.ProxyBackup = ""
		n++
	}
	markPoolDirtyLocked()
	poolMu.Unlock()
	return n
}

// clearAllZenKeyProxies 一键清空全部 zen key 的代理绑定（面板入口）：
// 所有 key 回到"全局"默认值。路由参与状态（KeyRoutingEnabled）不受影响。
func clearAllZenKeyProxies() int {
	cfg := getZenConfig()
	if len(cfg.KeyBindings) == 0 {
		return 0
	}
	n := len(cfg.KeyBindings)
	next := *cfg
	next.KeyBindings = nil
	setZenConfig(&next)
	return n
}

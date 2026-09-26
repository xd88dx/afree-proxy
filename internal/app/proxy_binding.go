package app

import (
	"fmt"
	"log"
	"net/url"
	"strings"
)

// ============ 账号/key 代理隔离（绑定出口） ============
//
// 目标：固定"身份 ↔ 出口 IP"的对应关系，让上游风控看到的是"一个账号一个
// 固定 IP"，而不是旧规则下"每个账号的 IP 在整个代理池里每请求漂移 + 同一
// 出口 IP 上轮流出现多个账号"——两者都是典型风控红旗。
//
// 规则（默认启用）：
//   - 每个账号 / zen key 可绑定一个主代理和一个辅代理；请求永远从绑定出口
//     发出，主可用走主，主不可用走辅。
//   - 两者都不可用（冷却中，或已从代理池删除）时，该身份在选号阶段被整体
//     跳过 —— 隔离优先于可用性，绝不退回其他出口或直连。
//   - 未绑定任何代理的身份沿用全局规则（cline 看 CLINE_USE_PROXIES/面板开关，
//     zen 看代理列表是否非空），因此空配置部署的行为与隔离启用前完全一致。
//   - 收割机的 CLI 铸造同样走该 key 的绑定代理（同一个会话 ID 必须始终来自
//     同一个 IP，否则隔离形同虚设）；绑定出口不可用时本轮跳过该 key 的铸造。
//
// 开关：zen 配置持久化 ProxyIsolation（nil = 默认启用），PROXY_ISOLATION env
// 显式设置时优先。关闭即完全回到旧版"请求级全局轮转"，绑定字段被忽略。

// proxyIsolationEnvLocked PROXY_ISOLATION env 是否显式设置（面板据此提示
// "开关被 env 覆盖"）。
func proxyIsolationEnvLocked() bool {
	_, ok := envBool("PROXY_ISOLATION")
	return ok
}

// proxyIsolationEnabled 账号/key 代理隔离当前是否启用（默认 true）。
// PROXY_ISOLATION env 显式设置时优先（env 覆盖面板，与 POOL_STRATEGY 同规则）；
// 否则读 zen 配置的持久化开关，缺省（nil）视为启用。
func proxyIsolationEnabled() bool {
	if v, ok := envBool("PROXY_ISOLATION"); ok {
		return v
	}
	cfg := getZenConfig()
	return cfg.ProxyIsolation == nil || *cfg.ProxyIsolation
}

// proxyIdxInPool 代理在当前池列表中的索引；不在池中（已被删除）返回 -1。
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

// egressDirect 直连哨兵：绑定主/辅槽位存 "direct" 表示该槽位选择直连出口。
// 主 = direct → 该身份强制直连（辅无意义）；主 = 代理、辅 = direct → 主不可用
// 时直连兜底。哨兵不会与真实代理 URL 冲突（代理必须带 scheme://host:port）。
const egressDirect = "direct"

// isEgressDirect 判断槽位值是否为直连哨兵。
func isEgressDirect(v string) bool { return v == egressDirect }

// pickBoundProxy 为一次上游尝试从绑定对中选可用出口：主优先，辅兜底。
// 直连哨兵视为"总是可用"的出口（返回 ("", -1, true)，调用方按直连发出）。
// 两者都不可用返回 ok=false —— 调用方（隔离模式）必须跳过该身份。
// 主辅相同视为一个出口（面板已阻止这样配置，防御性兜底）。
func pickBoundProxy(main, backup string) (proxyURL string, idx int, ok bool) {
	if isEgressDirect(main) {
		return "", -1, true
	}
	if main != "" && proxyAvailableURL(main) {
		return main, proxyIdxInPool(main), true
	}
	if isEgressDirect(backup) {
		return "", -1, true
	}
	if backup != "" && backup != main && proxyAvailableURL(backup) {
		return backup, proxyIdxInPool(backup), true
	}
	return "", -1, false
}

// boundProxiesRoutable 绑定对里是否还有可用出口。双空 = 未绑定 = 可路由
//（未绑定身份走全局规则，不算被隔离挡住）。
func boundProxiesRoutable(main, backup string) bool {
	if main == "" && backup == "" {
		return true
	}
	_, _, ok := pickBoundProxy(main, backup)
	return ok
}

// zenProxiesEnabled OpenCode 上游是否走共享代理池：配置缺省（nil）时按旧
// 规则（代理列表非空即走池）；面板下拉可显式选择走池/直连。
func zenProxiesEnabled() bool {
	zup := getZenConfig().ZenUseProxies
	if zup == nil {
		return len(getZenConfig().Proxies) > 0
	}
	return *zup
}

// zenAttemptExit 为一次 zen 上游尝试解析出口。key 有绑定时（隔离模式）主→辅，
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

// proxyExitType 请求日志"代理类型"列的取值：绑定主代理返回 "main"，绑定辅
// 代理返回 "backup"，直连出口（绑定的直连哨兵或未绑定直连）返回 "direct"；
// 全局代理池轮转或未命中上游返回 ""（面板显示 -）。
func proxyExitType(proxyURL, main, backup string, bound bool) string {
	if bound {
		if proxyURL == "" {
			return "direct"
		}
		if proxyURL == main {
			return "main"
		}
		if proxyURL == backup {
			return "backup"
		}
		return ""
	}
	if proxyURL == "" {
		return "direct"
	}
	return ""
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
// 代理池里真实存在的代理、要么是直连哨兵；主辅不能是同一个代理（否则"辅"
// 毫无意义，还掩盖配置错误）。主 = 直连时辅只能是空或直连（UI 会自动跟随
// 锁定）。允许 socks5 —— 网关上游请求支持全部协议；收割机 CLI 的 socks5 由
// 本地桥兑现（socks_bridge.go），不依赖 CLI 自身。
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
	if main != "" && !isEgressDirect(main) && main == backup {
		return fmt.Errorf("main and backup proxy must differ (same URL makes the backup meaningless)")
	}
	return nil
}

// assignProxiesEvenly 把代理池按顺序均匀分配给全部账号：账号 i 的主代理 =
// 池[i % m]，辅代理 = 池[(i+1) % m]（仅当池 ≥ 2 个）。账号数超过代理数时
// 多个账号共享出口（隔离范围收缩，但对应关系固定且显式）。
// 只改有账号的绑定，不动状态字段；返回分配到的账号数。
func assignProxiesEvenly() (int, error) {
	cfg := getZenConfig()
	m := len(cfg.Proxies)
	if m == 0 {
		return 0, fmt.Errorf("proxy pool is empty; add proxies first")
	}
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for i, a := range p.Accounts {
		a.ProxyMain = cfg.Proxies[i%m]
		if m >= 2 {
			a.ProxyBackup = cfg.Proxies[(i+1)%m]
		} else {
			a.ProxyBackup = ""
		}
	}
	markPoolDirtyLocked()
	log.Printf("proxy isolation: assigned %d proxies across %d accounts (main = pool[i%%m], backup = pool[(i+1)%%m])",
		m, len(p.Accounts))
	return len(p.Accounts), nil
}

// setAccountProxyBinding 设置/清除单个账号的绑定（面板入口）。
// main/backup 传空串即清除；全空 = 解除绑定。
func setAccountProxyBinding(accountID, main, backup string) error {
	main = strings.TrimSpace(main)
	backup = strings.TrimSpace(backup)
	if err := validateProxyBinding(main, backup); err != nil {
		return err
	}
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, a := range p.Accounts {
		if a.AccountID == accountID {
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
	if err := validateProxyBinding(main, backup); err != nil {
		return err
	}
	cfg := getZenConfig()
	if index < 0 || index >= len(cfg.Keys) {
		return fmt.Errorf("key index out of range")
	}
	key := cfg.Keys[index]
	if key == "" || key == "public" {
		return fmt.Errorf("the anonymous public key has no credential to bind")
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
// 所有 key 回到"全局"默认值。启用状态（KeyEnabled）不受影响。
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

package app

import (
	"afree-proxy/internal/cline"
	"fmt"
	"strings"

	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/other"
)

// proxyIdxInPool 代理在当前池列表中的索引；不在池中（已被删除）返回 -1。
// proxyAliasOf 取代理行的备注名（#fragment）；无备注返回空串。
// 备注名是代理的"身份标识"：订阅/服务端更新导致端口、凭据变化时，
// 备注名相同即视为同一代理（2026-10-18 用户定）。

// egressDirect 直连哨兵：绑定主/备用槽位存 "direct" 表示该槽位选择直连出口。
// 主 = direct → 该身份强制直连（备无意义）；主 = 代理、备 = direct → 主不可用
// 时直连兜底。哨兵不会与真实代理 URL 冲突（代理必须带 scheme://host:port）。

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
// scope 取值：other 子平台的 slug。
// zen 与 cline 在主包内直接调用 blockedExitMessage，不走这个注入点。
func proxyBlockedDetailFor(scope, key string) string {
	if key == "" {
		return ""
	}
	var main, backup string
	var bound bool
	switch scope {
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
// （走池）；面板下拉可显式选择走池/直连。配置住 zen-config.json（2026-11-17
// 迁回平台文件）。
func zenProxiesEnabled() bool {
	if zup := zenConfigUseProxies(); zup != nil {
		return *zup
	}
	return true
}

// boundExitFor 为一次 zen 上游尝试解析出口。key 有绑定时（隔离模式）主→备，
// 双不可用返回 ok=false —— 调用方对 pinned 探测直接报错，正常请求则短冷却
// 该 key 换下一个（绝不退回其他出口，否则粘性会话的 IP 就漂移了）；未绑定
// key 沿用全局轮转（未配置代理则直连）。
func boundExitFor(key string) (proxyURL string, idx int, ok bool) {
	main, backup, bound := keyBindingOf(key)
	if bound {
		return exitpool.PickBoundProxyFor(pkZen, main, backup)
	}
	// egress 自动钉定层（上游 3758eb7 整合）：钉定生效（开关开 + 池非空）时，
	// 未绑定 key 使用自动分配的固定出口（重启不变，分配幂等）——zen 免费层按
	// key 记录出口 IP，轮转让整池 key 都见过 N 个 IP，即使代理干净也会被判
	// 可疑。没分到出口（代理比 key 少）返回 ok=false：直连或借用别人的出口
	// 都会重新打上"一 key 多 IP"指纹。pickZenKey 已把这类 key 过滤出轮转，
	// 这里挡住的是 pinKey（面板 Test）与 ZEN_PIN_KEY 这两条绕过轮转的路径。
	// 显式 KeyBindings 恒优先于本层；钉定关闭或池为空时维持旧规则
	// （useProxies 开关决定走池轮转或直连）。
	if egressBindingActive() {
		if p, found := egressProxyForZen(key); found {
			// 钉定出口同样要过可用性判定（2026-11-09）：被自动/手动冷却的出口不能
			// 继续承载该 key——否则钉定路径对冷却完全免疫，与显式绑定路径结论相反。
			if i := proxyIndex(p); i >= 0 && exitpool.ProxyAvailableFor(pkZen, i) {
				return p, i, true
			}
			return "", -1, false
		}
		return "", -1, false
	}
	if !zenProxiesEnabled() {
		if kit.DisableDirect() {
			return "", -1, false // 直连被禁：key 失效（2026-10-28）
		}
		return "", -1, true // 面板选择直连：即使代理列表非空也不走池
	}
	var exitOK bool
	proxyURL, idx, exitOK = upstreamProxyForKind(pkZen)
	if !exitOK {
		return "", -1, false // 出口全被手动冷却：摘出轮转，不走直连兜底
	}
	if proxyURL == "" && kit.DisableDirect() {
		return "", -1, false // 代理池空兜底的直连同样不放行
	}
	return proxyURL, idx, true
}

// accountProxyBinding 账号的绑定出口；未启用隔离或未绑定时 bound=false，
// 调用方沿用全局代理规则。绑定字段的写入方（面板）持有 poolMu，读取也
// 短暂持锁，避免与面板写并发竞态。
func accountProxyBinding(acc *Account) (main, backup string, bound bool) {
	return cline.AccountProxyBinding(acc)
}

// keyBindingOf zen key 的绑定出口；未启用隔离或未绑定时 bound=false。
func keyBindingOf(key string) (main, backup string, bound bool) {
	if key == "" || !exitpool.IsolationEnabled() {
		return "", "", false
	}
	cfg := getZenConfig()
	b, ok := cfg.KeyBindings[key]
	if !ok || (b.Main == "" && b.Backup == "") {
		return "", "", false
	}
	return b.Main, b.Backup, true
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
			// 绑定是配置态，必须显式落盘：2026-10-31 配置/痕迹拆分后后台
			// flusher 只写 cline-state.json（痕迹），只 markPoolDirty 的
			// 配置改动在重启后丢失（2026-11-05 生产实证：一键代理+保存后
			// 重启回滚）。与 setAccountPoolEnabled 的 savePool 同口径。
			savePoolLocked()
			return nil
		}
	}
	return fmt.Errorf("account not found: %s", accountID)
}

// setKeyProxyBinding 设置/清除单个 zen key 的绑定（面板入口，按索引定位）。
// 绑定表住在 proxy-config.json 的隔离段，写时复制后整体替换（setProxyBindingConfig）。
func setKeyProxyBinding(index int, main, backup string) error {
	main = strings.TrimSpace(main)
	backup = strings.TrimSpace(backup)
	keys := getZenConfig().Keys
	if index < 0 || index >= len(keys) {
		return fmt.Errorf("key index out of range")
	}
	key := keys[index]
	if key == "" {
		return fmt.Errorf("empty key cannot be bound to a proxy")
	}
	cfg := getZenConfig()
	backup = resolveBackupSlot(backup, cfg.KeyBindings[key].Backup)
	if err := validateProxyBinding(main, backup); err != nil {
		return err
	}
	bindings := make(map[string]proxyBinding, len(cfg.KeyBindings)+1)
	for k, v := range cfg.KeyBindings {
		bindings[k] = v
	}
	if main == "" && backup == "" {
		delete(bindings, key)
	} else {
		bindings[key] = proxyBinding{Main: main, Backup: backup}
	}
	setZenKeyBindings(bindings)
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
	if n > 0 {
		// 配置态显式落盘（同 setAccountProxyBinding，见彼处注释）。
		savePoolLocked()
	}
	poolMu.Unlock()
	return n
}

// clearAllKeyProxies 一键清空全部 zen key 的代理绑定（面板入口）：
// 所有 key 回到"全局"默认值。路由参与状态（KeyRoutingEnabled）不受影响。
func clearAllKeyProxies() int {
	cfg := getZenConfig()
	if len(cfg.KeyBindings) == 0 {
		return 0
	}
	setZenKeyBindings(nil)
	return len(cfg.KeyBindings)
}

// proxyBinding 一个身份的出口绑定（值语义对全部平台一致）。
// 2026-11-17 P3b：类型迁 exitpool.Binding，此处别名保消费面。
type proxyBinding = exitpool.Binding

// inheritProxyExclusionsForNewPlatform 新建 other 子平台时继承「全平台禁用」
// 的代理（2026-11-19 用户定）：凡当前全部平台叶子（acc/trae/key/wb + 既有
// other:<slug>，不含新建平台自身）都已排除的代理，对新平台同样排除——否则
// 批量追加框里"已在所有平台排除"的代理会在新平台立刻复活。只排除部分平台
// 的代理不继承；「排除直连」（__direct__ 哨兵键）不继承。
func inheritProxyExclusionsForNewPlatform(newSlug string) {
	binding := getProxyBindingConfig()
	if binding == nil || len(binding.ProxyExclusions) == 0 || newSlug == "" {
		return
	}
	leaves := map[string]bool{"acc": true, "trae": true, "key": true, "wb": true, "freepi": true}
	for _, p := range other.Get().Platforms {
		if p.Slug != newSlug {
			leaves["other:"+p.Slug] = true
		}
	}
	covered := func(kinds []string) bool {
		for leaf := range leaves {
			found := false
			for _, k := range kinds {
				if k == leaf {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	next := *binding
	next.ProxyExclusions = make(map[string][]string, len(binding.ProxyExclusions))
	newKind := "other:" + newSlug
	for u, kinds := range binding.ProxyExclusions {
		if u == proxyRuleDirectKey {
			next.ProxyExclusions[u] = kinds
			continue
		}
		if covered(kinds) {
			dup := false
			for _, k := range kinds {
				if k == newKind {
					dup = true
					break
				}
			}
			if !dup {
				kinds = append(append([]string(nil), kinds...), newKind)
			}
		}
		next.ProxyExclusions[u] = kinds
	}
	setProxyBindingConfig(&next)
}

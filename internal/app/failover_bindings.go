package app

// 出口差分故障转移的平台绑定适配器（2026-11-24 用户定）。
//
// exitpool.OnPlatformScopedFailure 差分判定成立后需要：读该平台全部绑定对
// （判"未被绑定"严格候选）+ 把绑定故障出口的槽位改绑到替补。各平台的绑定
// 表住在各自配置文件、锁也归各域，适配器在此装配层统一实现并注册——依赖
// 方向 app → exitpool / app → 各平台包，均无环。
//
// Rebind 的 from 用别名等价匹配（SameAlias）：绑定行可能因订阅更新只保留
// 旧端口/凭据，备注名相同即同一代理；to 一律写池内当前地址。

import (
	"strings"

	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/freepi"
	"afree-proxy/internal/other"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/workbuddy"
)

// ---- cline（绑定住账号字段 ProxyMain/ProxyBackup，cline-config.json）----

type clineFailoverBindings struct{}

func (clineFailoverBindings) Pairs() []exitpool.Binding {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	var out []exitpool.Binding
	for _, a := range p.Accounts {
		if a.ProxyMain != "" || a.ProxyBackup != "" {
			out = append(out, exitpool.Binding{Main: a.ProxyMain, Backup: a.ProxyBackup})
		}
	}
	return out
}

// Entries 带凭据标识与域（cline 无域语义，Realm 恒空）。
func (clineFailoverBindings) Entries() []exitpool.BindingEntry {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	var out []exitpool.BindingEntry
	for _, a := range p.Accounts {
		if a.ProxyMain != "" || a.ProxyBackup != "" {
			out = append(out, exitpool.BindingEntry{Key: a.AccountID, Main: a.ProxyMain, Backup: a.ProxyBackup, Realm: exitpool.DeclaredRealm("acc", ""), Kind: "acc"})
		}
	}
	return out
}

// Rebind 差分改绑：平台声明国内域时全部凭据不参与（2026-11-26 用户定自动
// 排除不参与国内凭据），返回 0。
func (clineFailoverBindings) Rebind(from, to string) int {
	if exitpool.DeclaredRealm("acc", "") == "cn" {
		return 0
	}
	return (clineFailoverBindings{}).RebindAll(from, to)
}

// RebindAll 无域过滤的改绑（订阅换端口绑定行跟随迁移用）。
func (clineFailoverBindings) RebindAll(from, to string) int {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	n := 0
	for _, a := range p.Accounts {
		if a.ProxyMain != "" && exitpool.SameAlias(a.ProxyMain, from) {
			a.ProxyMain = to
			n++
		}
		if a.ProxyBackup != "" && exitpool.SameAlias(a.ProxyBackup, from) {
			a.ProxyBackup = to
			n++
		}
	}
	if n > 0 {
		// 配置态显式落盘（同 setAccountProxyBinding 口径）。
		savePoolLocked()
	}
	return n
}

// ---- zen key（绑定住 zen-config.json KeyBindings）----

type zenFailoverBindings struct{}

func (zenFailoverBindings) Pairs() []exitpool.Binding {
	cfg := getZenConfig()
	var out []exitpool.Binding
	for _, b := range cfg.KeyBindings {
		if b.Main != "" || b.Backup != "" {
			out = append(out, exitpool.Binding{Main: b.Main, Backup: b.Backup})
		}
	}
	return out
}

// Entries 带凭据标识与域（zen key 无域语义，Realm 恒空）。
func (zenFailoverBindings) Entries() []exitpool.BindingEntry {
	cfg := getZenConfig()
	var out []exitpool.BindingEntry
	for k, b := range cfg.KeyBindings {
		if b.Main != "" || b.Backup != "" {
			out = append(out, exitpool.BindingEntry{Key: k, Main: b.Main, Backup: b.Backup})
		}
	}
	return out
}

// Rebind 差分改绑：平台声明国内域时全部 key 不参与（2026-11-26 用户定）。
func (zenFailoverBindings) Rebind(from, to string) int {
	if exitpool.DeclaredRealm("key", "") == "cn" {
		return 0
	}
	return (zenFailoverBindings{}).RebindAll(from, to)
}

// RebindAll 无域过滤的改绑（订阅迁移用）。
func (zenFailoverBindings) RebindAll(from, to string) int {
	cfg := getZenConfig()
	if len(cfg.KeyBindings) == 0 {
		return 0
	}
	bindings := make(map[string]exitpool.Binding, len(cfg.KeyBindings))
	for k, v := range cfg.KeyBindings {
		bindings[k] = v
	}
	n := 0
	for k, v := range bindings {
		if v.Main != "" && exitpool.SameAlias(v.Main, from) {
			v.Main = to
			n++
		}
		if v.Backup != "" && exitpool.SameAlias(v.Backup, from) {
			v.Backup = to
			n++
		}
		bindings[k] = v
	}
	if n > 0 {
		setZenKeyBindings(bindings)
	}
	return n
}

// ---- workbuddy（绑定住 wkbd-config.json bindings 段）----

type wbFailoverBindings struct{}

func (wbFailoverBindings) Pairs() []exitpool.Binding {
	workbuddy.WbProxyMu.Lock()
	defer workbuddy.WbProxyMu.Unlock()
	workbuddy.LoadWorkbuddyProxyBindingsLocked()
	var out []exitpool.Binding
	for _, b := range workbuddy.WbProxyBindings {
		if b.Main != "" || b.Backup != "" {
			out = append(out, exitpool.Binding{Main: b.Main, Backup: b.Backup})
		}
	}
	return out
}

// Entries 带凭据标识与域：realm 从 vendor 账号池按 uid 反查（auth.Realm()，
// cn/global；查询失败按空域处理——宁可不跳过不误跳过）。
func (wbFailoverBindings) Entries() []exitpool.BindingEntry {
	workbuddy.WbProxyMu.Lock()
	defer workbuddy.WbProxyMu.Unlock()
	workbuddy.LoadWorkbuddyProxyBindingsLocked()
	var out []exitpool.BindingEntry
	for uid, b := range workbuddy.WbProxyBindings {
		if b.Main == "" && b.Backup == "" {
			continue
		}
		realm := ""
		if workbuddy.WorkbuddySub != nil && workbuddy.WorkbuddySub.Pool != nil {
			if a := workbuddy.WorkbuddySub.Pool.AuthByUID(uid); a != nil {
				realm = a.Realm()
			}
		}
		out = append(out, exitpool.BindingEntry{Key: uid, Main: b.Main, Backup: b.Backup, Realm: exitpool.DeclaredRealm("wb", realm), Kind: "wb"})
	}
	return out
}

// wbRealmOf wb 凭据的原始区域（auth.Realm，cn/global；查询失败按空域）。
func wbRealmOf(uid string) string {
	if workbuddy.WorkbuddySub != nil && workbuddy.WorkbuddySub.Pool != nil {
		if a := workbuddy.WorkbuddySub.Pool.AuthByUID(uid); a != nil {
			return a.Realm()
		}
	}
	return ""
}

// Rebind 差分改绑：声明国内域的凭据槽位跳过（2026-11-26 用户定）。
func (wbFailoverBindings) Rebind(from, to string) int { return wbRebind(from, to, true) }

// RebindAll 无域过滤的改绑（订阅迁移用）。
func (wbFailoverBindings) RebindAll(from, to string) int { return wbRebind(from, to, false) }

func wbRebind(from, to string, skipCN bool) int {
	workbuddy.WbProxyMu.Lock()
	defer workbuddy.WbProxyMu.Unlock()
	workbuddy.LoadWorkbuddyProxyBindingsLocked()
	n := 0
	for uid, b := range workbuddy.WbProxyBindings {
		if skipCN && exitpool.DeclaredRealm("wb", wbRealmOf(uid)) == "cn" {
			continue
		}
		if b.Main != "" && exitpool.SameAlias(b.Main, from) {
			b.Main = to
			n++
		}
		if b.Backup != "" && exitpool.SameAlias(b.Backup, from) {
			b.Backup = to
			n++
		}
		workbuddy.WbProxyBindings[uid] = b
	}
	if n > 0 {
		workbuddy.SaveWorkbuddyProxyBindingsLocked()
	}
	return n
}

// ---- other 子平台（绑定住 other-config.json 各平台 KeyBindings）----

type otherFailoverBindings struct{}

func (otherFailoverBindings) Pairs() []exitpool.Binding {
	var out []exitpool.Binding
	for i := range other.Get().Platforms {
		for _, b := range other.Get().Platforms[i].KeyBindings {
			if b.Main != "" || b.Backup != "" {
				out = append(out, exitpool.Binding{Main: b.Main, Backup: b.Backup})
			}
		}
	}
	return out
}

// Entries 带凭据标识与域：realm = 子平台 Realm 字段（2026-11-26 用户定，
// SenseNova 等国内服务标 cn）。
func (otherFailoverBindings) Entries() []exitpool.BindingEntry {
	var out []exitpool.BindingEntry
	for i := range other.Get().Platforms {
		p := other.Get().Platforms[i]
		for k, b := range p.KeyBindings {
			if b.Main != "" || b.Backup != "" {
				out = append(out, exitpool.BindingEntry{Key: p.Slug + "/" + k, Main: b.Main, Backup: b.Backup, Realm: exitpool.DeclaredRealm("other:"+p.Slug, ""), Kind: "other:" + p.Slug})
			}
		}
	}
	return out
}

// Rebind 差分改绑：声明国内域的子平台跳过（2026-11-26 用户定）。
func (otherFailoverBindings) Rebind(from, to string) int { return otherRebind(from, to, true) }

// RebindAll 无域过滤的改绑（订阅迁移用）。
func (otherFailoverBindings) RebindAll(from, to string) int { return otherRebind(from, to, false) }

func otherRebind(from, to string, skipCN bool) int {
	n := 0
	for i := range other.Get().Platforms {
		p := other.Get().Platforms[i]
		if len(p.KeyBindings) == 0 {
			continue
		}
		if skipCN && exitpool.DeclaredRealm("other:"+p.Slug, "") == "cn" {
			continue
		}
		next := other.ClonePlatform(&p)
		changed := false
		for k, v := range next.KeyBindings {
			if v.Main != "" && exitpool.SameAlias(v.Main, from) {
				v.Main = to
				n++
				changed = true
			}
			if v.Backup != "" && exitpool.SameAlias(v.Backup, from) {
				v.Backup = to
				n++
				changed = true
			}
			next.KeyBindings[k] = v
		}
		if changed {
			otherReplacePlatform(next)
		}
	}
	return n
}

// ---- freepi（绑定住 freepi-config.json KeyBindings）----

type freepiFailoverBindings struct{}

func (freepiFailoverBindings) Pairs() []exitpool.Binding {
	var out []exitpool.Binding
	for _, b := range freepi.Get().KeyBindings {
		if b.Main != "" || b.Backup != "" {
			out = append(out, exitpool.Binding{Main: b.Main, Backup: b.Backup})
		}
	}
	return out
}

// Entries 带凭据标识与域（freepi 无域语义，Realm 恒空）。
func (freepiFailoverBindings) Entries() []exitpool.BindingEntry {
	var out []exitpool.BindingEntry
	for k, b := range freepi.Get().KeyBindings {
		if b.Main != "" || b.Backup != "" {
			out = append(out, exitpool.BindingEntry{Key: k, Main: b.Main, Backup: b.Backup})
		}
	}
	return out
}

// Rebind 差分改绑：平台声明国内域时全部 key 不参与（2026-11-26 用户定）。
func (freepiFailoverBindings) Rebind(from, to string) int {
	if exitpool.DeclaredRealm("freepi", "") == "cn" {
		return 0
	}
	return (freepiFailoverBindings{}).RebindAll(from, to)
}

// RebindAll 无域过滤的改绑（订阅迁移用）。
func (freepiFailoverBindings) RebindAll(from, to string) int {
	cur := freepi.Get()
	if len(cur.KeyBindings) == 0 {
		return 0
	}
	next := freepi.CloneConfig(cur)
	n := 0
	for k, v := range next.KeyBindings {
		if v.Main != "" && exitpool.SameAlias(v.Main, from) {
			v.Main = to
			n++
		}
		if v.Backup != "" && exitpool.SameAlias(v.Backup, from) {
			v.Backup = to
			n++
		}
		next.KeyBindings[k] = v
	}
	if n > 0 {
		freepi.Set(next)
	}
	return n
}

// ---- trae（绑定住 trae-config.json bindings 段）----

type traeFailoverBindings struct{}

func (traeFailoverBindings) Pairs() []exitpool.Binding {
	trae.TraeBindingsMu.Lock()
	defer trae.TraeBindingsMu.Unlock()
	trae.LoadTraeBindingsLocked()
	var out []exitpool.Binding
	for _, b := range trae.TraeBindings {
		if b.Main != "" || b.Backup != "" {
			out = append(out, exitpool.Binding{Main: b.Main, Backup: b.Backup})
		}
	}
	return out
}

// Entries 带凭据标识与域：realm 按 uid 前缀推导（trae-intl- = 国际版，
// 其余 = cn，与面板行渲染 data-realm 同口径，html.go:7138）。
func (traeFailoverBindings) Entries() []exitpool.BindingEntry {
	trae.TraeBindingsMu.Lock()
	defer trae.TraeBindingsMu.Unlock()
	trae.LoadTraeBindingsLocked()
	var out []exitpool.BindingEntry
	for uid, b := range trae.TraeBindings {
		if b.Main != "" || b.Backup != "" {
			realm := "cn"
			if strings.HasPrefix(uid, "trae-intl-") {
				realm = "intl"
			}
			out = append(out, exitpool.BindingEntry{Key: uid, Main: b.Main, Backup: b.Backup, Realm: exitpool.DeclaredRealm("trae", realm), Kind: "trae"})
		}
	}
	return out
}

// traeRealmOf trae 凭据原始区域：uid 前缀 trae-intl- = 国际版，其余 cn。
func traeRealmOf(uid string) string {
	if strings.HasPrefix(uid, "trae-intl-") {
		return "intl"
	}
	return "cn"
}

// Rebind 差分改绑：声明国内域的凭据槽位跳过（2026-11-26 用户定）。
func (traeFailoverBindings) Rebind(from, to string) int { return traeRebind(from, to, true) }

// RebindAll 无域过滤的改绑（订阅迁移用）。
func (traeFailoverBindings) RebindAll(from, to string) int { return traeRebind(from, to, false) }

func traeRebind(from, to string, skipCN bool) int {
	trae.TraeBindingsMu.Lock()
	defer trae.TraeBindingsMu.Unlock()
	trae.LoadTraeBindingsLocked()
	n := 0
	for uid, b := range trae.TraeBindings {
		if skipCN && exitpool.DeclaredRealm("trae", traeRealmOf(uid)) == "cn" {
			continue
		}
		if b.Main != "" && exitpool.SameAlias(b.Main, from) {
			b.Main = to
			n++
		}
		if b.Backup != "" && exitpool.SameAlias(b.Backup, from) {
			b.Backup = to
			n++
		}
		trae.TraeBindings[uid] = b
	}
	if n > 0 {
		trae.SaveTraeBindingsLocked()
	}
	return n
}

// RegisterFailoverAdapters 装配根注册：各平台适配器 + other 子平台枚举。
// 由 app 启动序列调用（Setup 内，代理域初始化后）。
func RegisterFailoverAdapters() {
	exitpool.RegisterFailoverBindings("acc", clineFailoverBindings{})
	exitpool.RegisterFailoverBindings("key", zenFailoverBindings{})
	exitpool.RegisterFailoverBindings("wb", wbFailoverBindings{})
	exitpool.RegisterFailoverBindings("trae", traeFailoverBindings{})
	exitpool.RegisterFailoverBindings("freepi", freepiFailoverBindings{})
	exitpool.RegisterFailoverBindings("other", otherFailoverBindings{})
	exitpool.RegisterOtherRuleKinds(func() []string {
		var out []string
		for _, p := range other.Get().Platforms {
			out = append(out, "other:"+p.Slug)
		}
		return out
	})
}

// ---- RebindMain（只改 Main 槽，backup 原样——定时重排专用，2026-11-26）----

func (clineFailoverBindings) RebindMain(ruleKind, from, to string) int {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	n := 0
	for _, a := range p.Accounts {
		if a.ProxyMain != "" && exitpool.SameAlias(a.ProxyMain, from) {
			a.ProxyMain = to
			n++
		}
	}
	if n > 0 {
		savePoolLocked()
	}
	return n
}

func (zenFailoverBindings) RebindMain(ruleKind, from, to string) int {
	cfg := getZenConfig()
	if len(cfg.KeyBindings) == 0 {
		return 0
	}
	bindings := make(map[string]exitpool.Binding, len(cfg.KeyBindings))
	for k, v := range cfg.KeyBindings {
		bindings[k] = v
	}
	n := 0
	for k, v := range bindings {
		if v.Main != "" && exitpool.SameAlias(v.Main, from) {
			v.Main = to
			n++
			bindings[k] = v
		}
	}
	if n > 0 {
		setZenKeyBindings(bindings)
	}
	return n
}

func (wbFailoverBindings) RebindMain(ruleKind, from, to string) int {
	workbuddy.WbProxyMu.Lock()
	defer workbuddy.WbProxyMu.Unlock()
	workbuddy.LoadWorkbuddyProxyBindingsLocked()
	n := 0
	for uid, b := range workbuddy.WbProxyBindings {
		if b.Main != "" && exitpool.SameAlias(b.Main, from) {
			b.Main = to
			n++
			workbuddy.WbProxyBindings[uid] = b
		}
	}
	if n > 0 {
		workbuddy.SaveWorkbuddyProxyBindingsLocked()
	}
	return n
}

func (otherFailoverBindings) RebindMain(ruleKind, from, to string) int {
	// 按 ruleKind（other:<slug>）过滤子平台（2026-11-26 修）：聚合适配器的
	// 改绑只动目标子平台，否则定时重排会跨子平台洗牌。ruleKind 不是
	// other:<slug> 形态时退回全子平台（差分语义）。
	slug := strings.TrimPrefix(ruleKind, "other:")
	perSlug := strings.HasPrefix(ruleKind, "other:") && slug != ""
	n := 0
	for i := range other.Get().Platforms {
		p := other.Get().Platforms[i]
		if perSlug && p.Slug != slug {
			continue
		}
		if len(p.KeyBindings) == 0 {
			continue
		}
		next := other.ClonePlatform(&p)
		changed := false
		for k, v := range next.KeyBindings {
			if v.Main != "" && exitpool.SameAlias(v.Main, from) {
				v.Main = to
				n++
				changed = true
			}
			next.KeyBindings[k] = v
		}
		if changed {
			otherReplacePlatform(next)
		}
	}
	return n
}

func (freepiFailoverBindings) RebindMain(ruleKind, from, to string) int {
	cur := freepi.Get()
	if len(cur.KeyBindings) == 0 {
		return 0
	}
	next := freepi.CloneConfig(cur)
	n := 0
	for k, v := range next.KeyBindings {
		if v.Main != "" && exitpool.SameAlias(v.Main, from) {
			v.Main = to
			n++
		}
		next.KeyBindings[k] = v
	}
	if n > 0 {
		freepi.Set(next)
	}
	return n
}

func (traeFailoverBindings) RebindMain(ruleKind, from, to string) int {
	trae.TraeBindingsMu.Lock()
	defer trae.TraeBindingsMu.Unlock()
	trae.LoadTraeBindingsLocked()
	n := 0
	for uid, b := range trae.TraeBindings {
		if b.Main != "" && exitpool.SameAlias(b.Main, from) {
			b.Main = to
			n++
			trae.TraeBindings[uid] = b
		}
	}
	if n > 0 {
		trae.SaveTraeBindingsLocked()
	}
	return n
}

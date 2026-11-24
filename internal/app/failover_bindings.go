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

func (clineFailoverBindings) Rebind(from, to string) int {
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

func (zenFailoverBindings) Rebind(from, to string) int {
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

func (wbFailoverBindings) Rebind(from, to string) int {
	workbuddy.WbProxyMu.Lock()
	defer workbuddy.WbProxyMu.Unlock()
	workbuddy.LoadWorkbuddyProxyBindingsLocked()
	n := 0
	for uid, b := range workbuddy.WbProxyBindings {
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

func (otherFailoverBindings) Rebind(from, to string) int {
	n := 0
	for i := range other.Get().Platforms {
		p := other.Get().Platforms[i]
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

func (freepiFailoverBindings) Rebind(from, to string) int {
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

func (traeFailoverBindings) Rebind(from, to string) int {
	trae.TraeBindingsMu.Lock()
	defer trae.TraeBindingsMu.Unlock()
	trae.LoadTraeBindingsLocked()
	n := 0
	for uid, b := range trae.TraeBindings {
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

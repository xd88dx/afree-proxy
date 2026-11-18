package workbuddy

// ============================================================================
// WorkBuddy 平台独立配置(wkbd-config.json) —— afree 侧增量配置
//
// 2026-11-17 配置重组：afree 层实现的 wb 平台业务增量从三处迁入本文件——
//   - Bindings      账号出口绑定（原 proxy-config.json workbuddyBindings 段）
//   - UseProxies    走池开关（原 proxy-config.json workbuddyUseProxies）
//   - AccountPrefs  账号意志（反代开关/账号总开关/昵称锁；原随 vendor auths
//                   凭据文件落盘的 pool_enabled/account_enabled/nickname_locked
//                   三键——vendor 凭据文件回归纯凭据，意志经
//                   auth.AccountPrefProvider/AccountPrefObserver 钩子回填/通知）
//   - ShareOptimize 凭据权重优化定时任务（原 vendor config.json schedule.share_
//                   optimize_* 补丁键——afree 增量配置不进 vendor 配置文件）
//
// 命名用 wkbd- 前缀：与 vendor 子目录 data/workbuddy/（vendor 自管的
// config.json/state.json/auths）在文件名层面即不可混淆。
//
// wkbd-state.json（状态态）：wb 用量台账（原 vendor 子目录 workbuddy/usage.json
// 迁出，路径由桥接层注入 wbbridge.UsageNew）——afree 自有状态不落 vendor 目录。
//
// 锁序：WkbdCfgMu 是本文件唯一锁，绝不与 WbProxyMu 交叉持有（绑定访问器
// 在锁外返回副本）。
// ============================================================================

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

type WkbdConfigData struct {
	// UseProxies 走池开关：nil = 未配置 = 走池（与其他平台同约定）。
	UseProxies *bool `json:"useProxies,omitempty"`
	// Bindings 账号出口绑定（键 = 池 UID）。主优先、备兜底，双不可用 = 本次
	// 请求直接失败（隔离优先），绝不退回直连。
	Bindings map[string]WorkbuddyProxyBinding `json:"bindings,omitempty"`
	// AccountPrefs 账号意志（键 = UID）。vendor auth 结构体的内存三键（反代
	// 开关/账号总开关/昵称锁）由此回填与持久化；nil 值 = 未记录 = 启用。
	AccountPrefs map[string]WkbdAccountPref `json:"accountPrefs,omitempty"`
	// ShareOptimize 凭据权重优化定时任务（2026-11-15 立项，workbuddy_share.go）。
	ShareOptimize *WkbdShareOptimize `json:"shareOptimize,omitempty"`
}

// WkbdAccountPref 单账号意志。指针字段 nil = 未记录 = 启用（与 vendor 内存
// 语义一致）；NicknameLocked 是布尔（vendor 内存即布尔，缺省 false = 跟随上游）。
type WkbdAccountPref struct {
	PoolEnabled    *bool `json:"poolEnabled,omitempty"`
	AccountEnabled *bool `json:"accountEnabled,omitempty"`
	NicknameLocked bool  `json:"nicknameLocked,omitempty"`
}

// WkbdShareOptimize 权重优化定时任务开关与节奏（热值经 shareOptimizeSched
// atomic 分发，见 workbuddy.go）。
type WkbdShareOptimize struct {
	Enabled bool   `json:"enabled,omitempty"`
	Cron    string `json:"cron,omitempty"`
}

var (
	WkbdCfgMu     sync.Mutex
	WkbdCfg       *WkbdConfigData
	WkbdCfgLoaded bool
)

// WkbdConfigPathOverride 测试钩子：非空时覆盖配置文件路径。
var WkbdConfigPathOverride string

func WkbdConfigPath() string {
	if WkbdConfigPathOverride != "" {
		return WkbdConfigPathOverride
	}
	return kit.ResolveDataPath("wkbd-config.json")
}

func LoadWkbdConfigLocked() *WkbdConfigData {
	if WkbdCfgLoaded {
		return WkbdCfg
	}
	WkbdCfgLoaded = true
	WkbdCfg = &WkbdConfigData{}
	raw, err := os.ReadFile(WkbdConfigPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("wkbd config: read %s: %v", WkbdConfigPath(), err)
		}
		return WkbdCfg
	}
	if json.Unmarshal(raw, WkbdCfg) != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(WkbdConfigPath(), WkbdConfigPath()+".corrupt-"+stamp); renErr == nil {
			log.Printf("SEVERE: wkbd config is corrupt JSON; moved to %s.corrupt-%s for manual recovery; starting clean", WkbdConfigPath(), stamp)
		} else {
			log.Printf("SEVERE: wkbd config is corrupt JSON (quarantine rename failed: %v); starting clean", renErr)
		}
	}
	return WkbdCfg
}

// MutateWkbdConfig 在 WkbdCfgMu 锁内读改写并落盘。
func MutateWkbdConfig(fn func(*WkbdConfigData)) {
	WkbdCfgMu.Lock()
	defer WkbdCfgMu.Unlock()
	fn(LoadWkbdConfigLocked())
	SaveWkbdConfigLocked()
}

func SaveWkbdConfigLocked() {
	raw, err := json.MarshalIndent(WkbdCfg, "", "  ")
	if err != nil {
		log.Printf("wkbd config: marshal: %v", err)
		return
	}
	kit.AtomicWriteFile(WkbdConfigPath(), raw)
}

// ---- 访问器（锁外返回副本） ----

func WkbdConfigUseProxies() *bool {
	WkbdCfgMu.Lock()
	defer WkbdCfgMu.Unlock()
	g := LoadWkbdConfigLocked()
	if g.UseProxies == nil {
		return nil
	}
	v := *g.UseProxies
	return &v
}

func WkbdConfigBindings() map[string]WorkbuddyProxyBinding {
	WkbdCfgMu.Lock()
	defer WkbdCfgMu.Unlock()
	g := LoadWkbdConfigLocked()
	if g.Bindings == nil {
		return nil
	}
	cp := make(map[string]WorkbuddyProxyBinding, len(g.Bindings))
	for k, v := range g.Bindings {
		cp[k] = v
	}
	return cp
}

// WkbdConfigAccountPref 单账号意志读视图。
func WkbdConfigAccountPref(uid string) (poolEnabled, accountEnabled *bool, nicknameLocked bool) {
	WkbdCfgMu.Lock()
	defer WkbdCfgMu.Unlock()
	g := LoadWkbdConfigLocked()
	p, ok := g.AccountPrefs[uid]
	if !ok {
		return nil, nil, false
	}
	return p.PoolEnabled, p.AccountEnabled, p.NicknameLocked
}

// WkbdSetAccountPref 落单账号意志（整体覆盖该 uid 的 pref；nil 字段保持语义）。
func WkbdSetAccountPref(uid string, poolEnabled, accountEnabled *bool, nicknameLocked bool) {
	WkbdCfgMu.Lock()
	defer WkbdCfgMu.Unlock()
	g := LoadWkbdConfigLocked()
	if g.AccountPrefs == nil {
		g.AccountPrefs = map[string]WkbdAccountPref{}
	}
	g.AccountPrefs[uid] = WkbdAccountPref{
		PoolEnabled:    poolEnabled,
		AccountEnabled: accountEnabled,
		NicknameLocked: nicknameLocked,
	}
	SaveWkbdConfigLocked()
}

// WkbdConfigShareOptimize 权重优化定时任务热值。nil = 未配置（调用方按默认
// 处置：沿用与引入前一致的行为）。
func WkbdConfigShareOptimize() *WkbdShareOptimize {
	WkbdCfgMu.Lock()
	defer WkbdCfgMu.Unlock()
	g := LoadWkbdConfigLocked()
	if g.ShareOptimize == nil {
		return nil
	}
	cp := *g.ShareOptimize
	return &cp
}

// WbProxiesEnabled WorkBuddy 未绑定账号是否走共享代理池，语义与
// zenProxiesEnabled 一致：缺省（nil）默认开启（走池）。配置住 wkbd-config.json
// （2026-11-17 迁回平台文件）。
func WbProxiesEnabled() bool {
	if wup := WkbdConfigUseProxies(); wup != nil {
		return *wup
	}
	return true
}

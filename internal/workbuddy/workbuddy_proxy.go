package workbuddy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"afree-proxy/internal/exitpool"

	"afree-proxy/internal/kit"
	wbbridge "github.com/misakano7545/workbuddy2api-panel"
)

// WorkbuddyProxyBinding 是 WorkBuddy 账号的出口绑定。值语义与现有 Cline/zen
// 绑定一致：主优先、备兜底；两者都不可用 = 该账号本次请求直接失败（隔离优先），
// 绝不退回直连或其他出口。空绑定 = 尚未分配；代理池为空时等价于直连。
type WorkbuddyProxyBinding struct {
	Main   string `json:"main,omitempty"`
	Backup string `json:"backup,omitempty"`
}

var (
	WbProxyMu       sync.Mutex
	WbProxyLoaded   bool
	WbProxyBindings = map[string]WorkbuddyProxyBinding{}
)

// LoadWorkbuddyProxyBindingsLocked 从 wkbd-config.json 的 bindings 段载入绑定
// （2026-11-17 配置重组迁入，原 proxy-config.json workbuddyBindings 段）。调用
// 方必须持有 WbProxyMu。
func LoadWorkbuddyProxyBindingsLocked() {
	if WbProxyLoaded {
		return
	}
	WbProxyLoaded = true
	if bindings := WkbdConfigBindings(); bindings != nil {
		WbProxyBindings = bindings
	}
}

// SaveWorkbuddyProxyBindingsLocked 只覆盖 wkbd-config.json 的 bindings 段。
// 调用方必须持有 WbProxyMu。
func SaveWorkbuddyProxyBindingsLocked() {
	cp := make(map[string]WorkbuddyProxyBinding, len(WbProxyBindings))
	for uid, b := range WbProxyBindings {
		cp[uid] = b
	}
	MutateWkbdConfig(func(k *WkbdConfigData) { k.Bindings = cp })
}

// WorkbuddyProxyFor 是 upstream.ProxyFor 的实现。语义与 Cline/OpenCode 一致：
//   - 隔离关闭：忽略绑定，未绑定出口按全局走池开关轮转（开关关 = 直连）。
//   - 隔离开启且账号有绑定：主代理优先、备用代理兜底，双不可用 fail-closed。
//   - 隔离开启且账号未绑定：沿用全局走池开关；显式绑定 "direct" 才强制直连。
func WorkbuddyProxyFor(a *wbbridge.Auth) (string, bool) {
	if a == nil {
		return "", true
	}
	if !exitpool.IsolationEnabled() {
		return WorkbuddyGlobalExit()
	}
	uid := strings.TrimSpace(a.UID)
	if uid == "" {
		return "", true
	}

	WbProxyMu.Lock()
	LoadWorkbuddyProxyBindingsLocked()
	b, bound := WbProxyBindings[uid]
	WbProxyMu.Unlock()

	// 未绑定 = 全局策略，不是永久直连；代理池为空时 pickUpstreamProxy 返回直连。
	if !bound || (b.Main == "" && b.Backup == "") {
		return WorkbuddyGlobalExit()
	}
	url, _, usable := exitpool.PickBoundProxyFor(exitpool.KindWb, b.Main, b.Backup)
	return url, usable
}

// WorkbuddyObserveAccount 是 upstream.ObserveAccount 的实现：vendor 选号
// 之后把命中的账号与本次实际出口回报，网关侧据此填请求日志的「账号/Key」
// 与「代理」两列（此前只有 Cline 账号与 zen key 有这两列，WorkBuddy 恒空）。
// ctx 取自发起请求的 *http.Request：网关 /v1 请求带着日志槽位，面板请求与
// 定时任务的 ctx 没有槽位，Host.LogUpstreamInfo/Exit 注入的实现内部即 no-op。
func WorkbuddyObserveAccount(ctx context.Context, a *wbbridge.Auth, exitRaw string) {
	if ctx == nil || a == nil {
		return
	}
	uid := strings.TrimSpace(a.UID)
	if uid == "" {
		return
	}
	Host.LogUpstreamInfo(ctx, WorkbuddyUpstreamLabel(uid))
	Host.LogUpstreamExit(ctx, WorkbuddyExitType(uid, exitRaw))
}

// WorkbuddyUpstreamLabel 日志「账号/Key」列的账号标识：与面板 # 列同口径的
// 展示行号 —— account #N（N = 当前 List 展示顺序中的行号，拖拽重排后按新顺序
// 重新编号；英文源文，面板渲染时经 T('account #') 词条中文化）。昵称不参与
// （面板账号列已展示，日志只留序号方便排查）。
func WorkbuddyUpstreamLabel(uid string) string {
	uid = strings.TrimSpace(uid)
	n := 0
	if WorkbuddySub != nil && WorkbuddySub.Pool != nil {
		n = WorkbuddySub.Pool.IndexOf(uid)
	}
	return fmt.Sprintf("account #%d", n)
}

// WorkbuddyExitType 与 WorkbuddyProxyFor 同口径判定出口类型：隔离开启且
// 账号有绑定时按绑定归类 main/backup/direct，否则走全局池轮转（返回空串，
// 面板显示 -）。fail-closed（绑定出口全不可用、未出网）按直连记 direct。
func WorkbuddyExitType(uid, exitRaw string) string {
	if !exitpool.IsolationEnabled() {
		return exitpool.ProxyExitType(exitRaw, "", "", false)
	}
	WbProxyMu.Lock()
	LoadWorkbuddyProxyBindingsLocked()
	b, bound := WbProxyBindings[uid]
	WbProxyMu.Unlock()
	if !bound || (b.Main == "" && b.Backup == "") {
		return exitpool.ProxyExitType(exitRaw, "", "", false)
	}
	return exitpool.ProxyExitType(exitRaw, b.Main, b.Backup, true)
}

// WorkbuddyGlobalExit 未绑定身份的全局出口：面板开关选直连时即使代理列表
// 非空也不走池（与 OpenCode 的 boundExitFor 同语义）。
func WorkbuddyGlobalExit() (string, bool) {
	if !WbProxiesEnabled() {
		if kit.DisableDirect() {
			return "", false // 直连被禁：凭据失效（2026-10-28）
		}
		return "", true
	}
	url, _, ok := exitpool.UpstreamForKind(exitpool.KindWb)
	if !ok {
		return "", false // 出口全被手动冷却：摘出轮转，不走直连兜底
	}
	if url == "" && kit.DisableDirect() {
		return "", false // 代理池空兜底的直连同样不放行
	}
	return url, true
}

// WorkbuddyProxyBindingsSnapshot 返回代理池与当前绑定，供管理 API 排障。
func WorkbuddyProxyBindingsSnapshot() ([]string, map[string]WorkbuddyProxyBinding) {
	cfg := exitpool.GetPool()
	proxies := append([]string(nil), cfg.Proxies...)
	WbProxyMu.Lock()
	defer WbProxyMu.Unlock()
	LoadWorkbuddyProxyBindingsLocked()
	out := make(map[string]WorkbuddyProxyBinding, len(WbProxyBindings))
	for uid, b := range WbProxyBindings {
		out[uid] = b
	}
	return proxies, out
}

func SetWorkbuddyProxyBinding(uid, main, backup string) error {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return fmt.Errorf("uid is required")
	}
	main = strings.TrimSpace(main)
	backup = strings.TrimSpace(backup)
	WbProxyMu.Lock()
	defer WbProxyMu.Unlock()
	LoadWorkbuddyProxyBindingsLocked()
	backup = exitpool.ResolveBackupSlot(backup, WbProxyBindings[uid].Backup)
	if err := exitpool.ValidateBinding(main, backup); err != nil {
		return err
	}
	if main == "" && backup == "" {
		delete(WbProxyBindings, uid)
	} else {
		WbProxyBindings[uid] = WorkbuddyProxyBinding{Main: main, Backup: backup}
	}
	SaveWorkbuddyProxyBindingsLocked()
	return nil
}

func ClearAllWorkbuddyProxyBindings() int {
	WbProxyMu.Lock()
	defer WbProxyMu.Unlock()
	LoadWorkbuddyProxyBindingsLocked()
	n := len(WbProxyBindings)
	WbProxyBindings = map[string]WorkbuddyProxyBinding{}
	SaveWorkbuddyProxyBindingsLocked()
	return n
}

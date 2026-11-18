package exitpool

// 隔离/绑定核心语义（2026-11-17 结构重构 P3b-③ 从 app/proxy_binding.go 出包）：
// 隔离开关、直连哨兵、主备槽选择（fail-closed）、绑定校验、退出类型标注与
// 可读详情。平台接线（boundExitFor/keyBindingOf/面板 setter）留装配根。

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"afree-proxy/internal/kit"
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

// IsolationEnv 解析 PROXY_ISOLATION：只接受 true/false（大小写不敏感），
// 其他值告警并按未设置处理。返回 (值, 是否有效设置)。
func IsolationEnv() (bool, bool) {
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

// IsolationEnvLocked PROXY_ISOLATION=true 时为真：隔离强制开启，面板
// 下拉只读。false 不锁定 —— 它只是把默认值改为关闭，面板仍可修改。
func IsolationEnvLocked() bool {
	v, ok := IsolationEnv()
	return ok && v
}

// IsolationEnabled 账号/key 代理隔离当前是否启用：
//   - PROXY_ISOLATION=true：强制开启（无视面板设置）。
//   - PROXY_ISOLATION=false：默认关闭；面板显式设置过（ProxyIsolation 非 nil）
//     则按面板值。
//   - 未设置：默认开启；面板显式设置过则按面板值（nil 视为开启）。
func IsolationEnabled() bool {
	if v, ok := IsolationEnv(); ok {
		if v {
			return true
		}
		cfg := GetBinding()
		return cfg.ProxyIsolation != nil && *cfg.ProxyIsolation
	}
	cfg := GetBinding()
	return cfg.ProxyIsolation == nil || *cfg.ProxyIsolation
}

// DirectSentinel 直连哨兵：绑定主/备用槽位存 "direct" 表示该槽位选择直连出口。
// 主 = direct → 该身份强制直连（备无意义）；主 = 代理、备 = direct → 主不可用
// 时直连兜底。哨兵不会与真实代理 URL 冲突（代理必须带 scheme://host:port）。
const DirectSentinel = "direct"

// IsDirect 判断槽位值是否为直连哨兵。
func IsDirect(v string) bool { return v == DirectSentinel }

// BackupProxyEnabled 备用代理开关（proxy-config.json 的 BackupProxyEnabled）：
// nil = 未配置 = 关闭（默认）。关闭时"主不通走备"的兜底不存在，主出口不可用
// 就等于该身份本轮不可用（调用方跳过）—— 隔离优先不变，仍然绝不回退直连。
func BackupProxyEnabled() bool {
	v := GetBinding().BackupProxyEnabled
	return v != nil && *v
}

// ResolveBackupSlot 按备用代理开关决定落盘的备用槽值。
//
// 开关开启：备用槽参与路由，采纳调用方提交的值。
// 开关关闭：备用槽整体退出路由（pickBoundProxy 在此分支直接返回 ok=false），
//
//	面板把备用槽锁成只读并跟随主槽，提交上来的值只是显示值而非用户意图，
//	此时保留该身份已持久化的旧备用槽原值 —— 重开开关即可复用，无需重选。
//	保留而非清空是刻意的：清空会让"关一下开关"变成不可逆的数据丢失。
//
// 旧值由调用方从当前绑定里取出（storedBackup），空串表示此前没有备用槽。
func ResolveBackupSlot(submitted, storedBackup string) string {
	if BackupProxyEnabled() {
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
func PickBoundProxyFor(kind, main, backup string) (proxyURL string, idx int, ok bool) {
	if IsDirect(main) {
		// DISABLE_DIRECT=true：绑定直连哨兵 = 明确选择了直连，凭据整体失效
		//（隔离候选过滤经 boundProxiesRoutable 自动剔除，2026-10-28 用户定）。
		if kit.DisableDirect() {
			return "", -1, false
		}
		return "", -1, true
	}
	if main != "" {
		if idx := IdxInPool(main); idx >= 0 && ProxyAvailableFor(kind, idx) {
			return GetPool().Proxies[idx], idx, true
		}
	}
	if !BackupProxyEnabled() {
		return "", -1, false
	}
	if IsDirect(backup) {
		if kit.DisableDirect() {
			return "", -1, false
		}
		return "", -1, true
	}
	if backup != "" && backup != main {
		if idx := IdxInPool(backup); idx >= 0 && ProxyAvailableFor(kind, idx) {
			return GetPool().Proxies[idx], idx, true
		}
	}
	return "", -1, false
}

// boundProxiesRoutable 绑定对里是否还有可用出口。双空 = 未绑定 = 可路由
// （未绑定身份走全局规则，不算被隔离挡住）。备用代理开关关闭时只剩备用槽的绑定
// （主槽为空）同样不可路由 —— 那种状态面板产不出来，只在手改配置文件时出现，
// 一律按"该身份没有出口"处理并跳过，不退回全局规则（那可能是直连）。
func BoundProxiesRoutableFor(kind, main, backup string) bool {
	if main == "" && backup == "" {
		return true
	}
	_, _, ok := PickBoundProxyFor(kind, main, backup)
	return ok
}

// ============ 出口不可用的可读详情 ============
//
// 绑定出口全部冷却/被移除时，各平台的探测（面板 Test）会直接失败。旧文案
// "bound proxies unavailable (cooling or removed)" 只说了结论：既看不出是
// 哪个出口，也看不出是短暂冷却还是节点已从池里删除 —— 用户据此会误判成
// "这个 key 废了"并把好凭据删掉。下面把出口（打码）与冷却解除时刻写进文案。

// ExitStateLabel 单个出口槽位的可读状态。raw 为空表示未配置该槽。
func ExitStateLabel(raw string) string {
	if raw == "" {
		return "none"
	}
	label := kit.MaskProxyURL(raw)
	if IdxInPool(raw) < 0 {
		return label + " (removed from proxy pool)"
	}
	if until := CooldownUntil(raw); !until.IsZero() {
		return label + " (cooling until " + until.UTC().Format(time.RFC3339) + ")"
	}
	return label + " (unreachable)"
}

// BoundExitDetail 绑定对的可读详情："main=… (…), backup=…"。
func BoundExitDetail(main, backup string) string {
	return "main=" + ExitStateLabel(main) + ", backup=" + ExitStateLabel(backup)
}

// ProxyExitType 请求日志"代理类型"列的取值：绑定主代理返回 "main"，绑定备
// 代理返回 "backup"，直连出口（绑定的直连哨兵或未绑定直连）返回 "direct"；
// 全局代理池轮转或未命中上游返回 ""（面板显示 -）。
func ProxyExitType(proxyURL, main, backup string, bound bool) string {
	if bound {
		if proxyURL == "" {
			return "direct"
		}
		// pickBoundProxy 返回的是池内当前地址，与绑定行可能只差端口/凭据
		//（备注名相同）—— 备注名一致即视为命中该槽位。
		if proxyURL == main || SameAlias(proxyURL, main) {
			return "main"
		}
		if proxyURL == backup || SameAlias(proxyURL, backup) {
			return "backup"
		}
		return ""
	}
	if proxyURL == "" {
		return "direct"
	}
	return ""
}

// SameAlias 两个代理行备注名相同（且非空）即视为同一代理。
func SameAlias(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	alias := AliasOf(a)
	return alias != "" && alias == AliasOf(b)
}

// ValidateBinding 校验一对绑定：代理槽位要么为空（全局）、要么是当前
// 代理池里真实存在的代理、要么是直连哨兵；主备不能是同一个代理（否则"备"
// 毫无意义，还掩盖配置错误）。主 = 直连时备只能是空或直连（UI 会自动跟随
// 锁定）。允许 socks5 —— 网关上游请求支持全部协议；收割机 CLI 的 socks5 由
// 本地桥兑现（socks_bridge.go），不依赖 CLI 自身。
//
// main == backup 这条在备用代理开关关闭时放行：关闭态下备用槽只是被保留、并不
// 参与路由（pickBoundProxy 会在该分支直接返回 ok=false），面板又把备用槽锁成
// 跟随主槽，用户完全无法主动制造这个组合 —— 它只可能来自"关闭前 main=a /
// backup=b，之后把 main 改成 b"这类正常操作。拦住它只会让用户改不动主槽。
func ValidateBinding(main, backup string) error {
	for _, p := range []string{main, backup} {
		if p == "" || IsDirect(p) {
			continue
		}
		if IdxInPool(p) < 0 {
			return fmt.Errorf("proxy %q is not in the current proxy pool; add it on the Proxy pool page first", kit.MaskProxyURL(p))
		}
		if _, err := url.Parse(p); err != nil {
			return fmt.Errorf("invalid proxy url %q", kit.MaskProxyURL(p))
		}
	}
	if IsDirect(main) && backup != "" && !IsDirect(backup) {
		return fmt.Errorf("main = direct means no proxy at all; the backup slot must also be direct")
	}
	if main != "" && !IsDirect(main) && main == backup && BackupProxyEnabled() {
		return fmt.Errorf("main and backup proxy must differ (same URL makes the backup meaningless)")
	}
	return nil
}

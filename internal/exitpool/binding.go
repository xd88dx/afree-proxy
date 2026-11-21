package exitpool

// 隔离/绑定段（proxy-config.json 的一部分）。
//
// 历史包袱:这些字段原先住在 .zen-config.json 里 —— 代理是全局能力,却被
// 一个平台的配置文件托管,面板上"zen 代理隔离"的措辞也随之误导。现在它们
// 落到 proxy-config.json 的独立分段,由本文件内存态 + 锁独占读写。
//
// 凭据↔出口的绑定表已迁各平台配置文件（2026-11-17 配置重组：zen keyBindings
// 住 zen-config.json、cline 住账号字段、wb 住 wkbd-config.json）；本段只剩
// 隔离开关/备用开关/排除表/绑定上限——都是代理域自己的语义。

import (
	"regexp"
	"sync"
)

// BindingConfig 隔离/绑定段的内存形态,与 FileData 中对应字段一一对应。
// 指针三态(ProxyIsolation/BackupProxyEnabled)保留原语义:
// nil = 未配置,走各自默认值。
type BindingConfig struct {
	ProxyIsolation     *bool               `json:"proxyIsolation,omitempty"`
	BackupProxyEnabled *bool               `json:"backupProxyEnabled,omitempty"`
	ProxyExclusions    map[string][]string `json:"proxyExclusions,omitempty"`
	ProxyBindCaps      map[string]int      `json:"proxyBindCaps,omitempty"`
	// 延迟重排的"保持不变"阈值（秒）：平均握手延迟 ≤ 该值的代理视为健康、
	// 保持现绑定不参与重排。0/缺项 = 取 DefaultReorderKeepSec。
	ReorderKeepSec float64 `json:"reorderKeepSec,omitempty"`
}

// Binding 一个身份的出口绑定：主代理优先，备用代理兜底；两者都不可用时
// 跳过该身份（隔离优先于可用性）。值语义对全部平台一致——zen key、cline
// 账号、other 子平台 key、trae 账号都用它。
type Binding struct {
	Main   string `json:"main,omitempty"`
	Backup string `json:"backup,omitempty"`
}

// DefaultReorderKeepSec 阈值缺省值（秒）：与旧版硬编码 6s 的口径不同——
// 默认 8s（2026-10-29 用户定），面板可改并落盘。
const DefaultReorderKeepSec = 8.0

var (
	bindingMu  sync.Mutex
	bindingCfg *BindingConfig
	// bindingLoadedPath 缓存对应的配置文件路径:测试改写 PathOverride 时
	// 自动重载,避免读到上一个用例的内存态。
	bindingLoadedPath string
)

// loadBindingLocked 取(必要时加载)绑定段。调用方必须持有 bindingMu。
func loadBindingLocked() *BindingConfig {
	path := ConfigPath()
	if bindingCfg != nil && bindingLoadedPath == path {
		return bindingCfg
	}
	fileMu.Lock()
	f := loadFileLocked()
	fileMu.Unlock()
	bindingCfg = &BindingConfig{
		ProxyIsolation:     f.ProxyIsolation,
		BackupProxyEnabled: f.BackupProxyEnabled,
		ProxyExclusions:    f.ProxyExclusions,
		ProxyBindCaps:      f.ProxyBindCaps,
		ReorderKeepSec:     f.ReorderKeepSec,
	}
	bindingLoadedPath = path
	return bindingCfg
}

// GetBinding 返回隔离/绑定段的活配置(只读语义:调用方要改就整体复制后
// SetBinding,与 zen 配置的写时复制一致)。
func GetBinding() *BindingConfig {
	bindingMu.Lock()
	defer bindingMu.Unlock()
	return loadBindingLocked()
}

// SetBinding 整体替换隔离/绑定段并落盘(只覆盖本段,不动代理池/健康统计)。
func SetBinding(c *BindingConfig) {
	bindingMu.Lock()
	cp := *c
	bindingCfg = &cp
	bindingLoadedPath = ConfigPath()
	bindingMu.Unlock()

	MutateFile(func(f *FileData) {
		f.ProxyIsolation = cp.ProxyIsolation
		f.BackupProxyEnabled = cp.BackupProxyEnabled
		f.ProxyExclusions = cp.ProxyExclusions
		f.ProxyBindCaps = cp.ProxyBindCaps
		f.ReorderKeepSec = cp.ReorderKeepSec
	})
}

// ProxyRuleKinds 自动分配规则（ProxyExclusions/ProxyBindCaps）的基础平台
// kind，与前端凭据页 kind / REORDER_KINDS 同一口径。
var ProxyRuleKinds = map[string]bool{
	"acc": true, "trae": true, "key": true, "wb": true,
	"other": true, "freepi": true,
}

// OtherRuleKindRe 匹配 Other 子平台粒度的规则 kind（other:<slug>）。面板把
// Other 渲染成归类行，其规则按子平台独立配置；slug 由后端生成（other_ + 随机
// hex，2026-11-20 起前缀从 op_ 迁为 other_），这里只校验形态，避免子平台被
// 删/临时加载失败时整表保存失败。
var OtherRuleKindRe = regexp.MustCompile(`^other:[A-Za-z0-9_-]{1,64}$`)

// ProxyRuleKindValid 规则 kind 是否合法：基础平台，或 other:<slug>。
func ProxyRuleKindValid(kind string) bool {
	return ProxyRuleKinds[kind] || OtherRuleKindRe.MatchString(kind)
}

// ProxyRuleDirectKey 直连在排除表里的哨兵键。池条目一律带 scheme
// （http(s)://、socks5:// 等），不会与之冲突。含义 = 不允许该平台的自动分配
// （一键代理 / 延迟重排）产出"直连"出口 —— 例如某些子平台不对大陆服务，
// 直连必被上游拒绝，批量分配就不再把直连同给任何子平台凭据；手动绑定
// 与运行时路由不受影响。池变动清理时该键永不删除。
const ProxyRuleDirectKey = "__direct__"

// PruneProxyExclusions 按代理池清理排除表：已不在池内的代理条目丢弃（它们
// 永远不会被自动分配命中，留着只是垃圾配置），直连哨兵键保留 —— 它不是池
// 条目，且不随池变动失效。面板保存池与配置导入（跨机迁移）共用此口径。
func PruneProxyExclusions(ex map[string][]string, pool []string) map[string][]string {
	if len(ex) == 0 {
		return ex
	}
	inPool := make(map[string]bool, len(pool))
	for _, p := range pool {
		inPool[p] = true
	}
	out := make(map[string][]string, len(ex))
	for p, kinds := range ex {
		if inPool[p] || p == ProxyRuleDirectKey {
			out[p] = kinds
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

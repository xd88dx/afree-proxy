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

import "sync"

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

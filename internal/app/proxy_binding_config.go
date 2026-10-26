package app

// ============================================================================
// 代理隔离与凭据绑定段(proxy-config.json 的一部分)
//
// 历史包袱:这些字段原先住在 .zen-config.json 里 —— 代理是全局能力,却被
// 一个平台的配置文件托管,面板上"zen 代理隔离"的措辞也随之误导。现在它们
// 落到 proxy-config.json 的独立分段,由本文件的内存态 + 锁独占读写。
//
// 锁序:proxyBindingMu 在外,proxyFileMu 在内(见 proxy_config.go)。
// ============================================================================

import (
	"sync"
)

// proxyBindingConfigData 隔离/绑定段的内存形态,与 proxyConfigFileData 中
// 对应字段一一对应。指针三态(ProxyIsolation/BackupProxyEnabled)保留原语义:
// nil = 未配置,走各自默认值。
type proxyBindingConfigData struct {
	ProxyIsolation     *bool               `json:"proxyIsolation,omitempty"`
	BackupProxyEnabled *bool               `json:"backupProxyEnabled,omitempty"`
	ProxyExclusions    map[string][]string `json:"proxyExclusions,omitempty"`
	ProxyBindCaps      map[string]int      `json:"proxyBindCaps,omitempty"`
}

var (
	proxyBindingMu  sync.Mutex
	proxyBindingCfg *proxyBindingConfigData
	// proxyBindingLoadedPath 缓存对应的配置文件路径:测试改写
	// proxyConfigPathOverride 时自动重载,避免读到上一个用例的内存态。
	proxyBindingLoadedPath string
)

// loadProxyBindingConfigLocked 取(必要时加载)绑定段。调用方必须持有 proxyBindingMu。
func loadProxyBindingConfigLocked() *proxyBindingConfigData {
	path := proxyConfigPath()
	if proxyBindingCfg != nil && proxyBindingLoadedPath == path {
		return proxyBindingCfg
	}
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	proxyFileMu.Unlock()
	proxyBindingCfg = &proxyBindingConfigData{
		ProxyIsolation:     f.ProxyIsolation,
		BackupProxyEnabled: f.BackupProxyEnabled,
		ProxyExclusions:    f.ProxyExclusions,
		ProxyBindCaps:      f.ProxyBindCaps,
	}
	proxyBindingLoadedPath = path
	return proxyBindingCfg
}

// getProxyBindingConfig 返回隔离/绑定段的活配置(只读语义:调用方要改就整体
// 复制后 setProxyBindingConfig,与 zen 配置的写时复制一致)。
func getProxyBindingConfig() *proxyBindingConfigData {
	proxyBindingMu.Lock()
	defer proxyBindingMu.Unlock()
	return loadProxyBindingConfigLocked()
}

// setProxyBindingConfig 整体替换隔离/绑定段并落盘(只覆盖本段,不动代理池/
// 健康统计/WorkBuddy 绑定)。
func setProxyBindingConfig(c *proxyBindingConfigData) {
	proxyBindingMu.Lock()
	cp := *c
	proxyBindingCfg = &cp
	proxyBindingLoadedPath = proxyConfigPath()
	proxyBindingMu.Unlock()

	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.ProxyIsolation = cp.ProxyIsolation
		f.BackupProxyEnabled = cp.BackupProxyEnabled
		f.ProxyExclusions = cp.ProxyExclusions
		f.ProxyBindCaps = cp.ProxyBindCaps
	})
}

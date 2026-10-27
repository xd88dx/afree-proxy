package app

// 代理池配置独立于 OpenCode zen。
//
// 历史上代理池寄居在 .zen-config.json 的 zenConfigData 里，只因为最早只有
// zen 平台走代理池；后来 amd / openrouter / tokenharbor / other / trae /
// workbuddy 全部复用同一个出口池，"zen" 前缀就变成了纯误导——代理池是全局
// 能力，不属于任何一个平台。
//
// 落盘位置是 proxy-config.json 的出口池段（见 proxy_config.go），与出口健康
// 统计、WorkBuddy 账号绑定同文件；本文件只管出口池这一段的内存态与语义。

import (
	"log"
	"strings"
	"sync"
	"time"
)

// proxyPoolConfigData 是全局出口代理池配置：列表、调度策略、订阅，以及两个
// 平台级「是否走共享池」开关。
//
// JSON 键名沿用面板既有契约（proxyStrategy/proxySubURL/...），前端不用改。
type proxyPoolConfigData struct {
	Proxies  []string `json:"proxies"`       // http(s)/socks5 代理,轮询出口
	Strategy string   `json:"proxyStrategy"` // round_robin / random / fill
	// 代理订阅：SubURL 非空时按 SubCron（5 字段分钟级 cron，空 = 不调度）
	// 定时拉取订阅并整池替换 Proxies。
	SubURL  string `json:"proxySubURL,omitempty"`
	SubCron string `json:"proxySubCron,omitempty"`
	// 未绑定代理的身份是否走共享池：nil = 缺省（池非空即走）。
	// 每个平台一份，语义相同，都属于代理域（见 platform_proxies.go）。
	ClineUseProxies     *bool `json:"clineUseProxies,omitempty"`
	AmdUseProxies       *bool `json:"amdUseProxies,omitempty"`
	OprtUseProxies      *bool `json:"oprtUseProxies,omitempty"`
	TkhbUseProxies      *bool `json:"tkhbUseProxies,omitempty"`
	TraeUseProxies      *bool `json:"traeUseProxies,omitempty"`
	OtherUseProxies     *bool `json:"otherUseProxies,omitempty"`
	ZenUseProxies       *bool `json:"zenUseProxies,omitempty"`
	WorkbuddyUseProxies *bool `json:"workbuddyUseProxies,omitempty"`
}

func defaultProxyPoolConfig() *proxyPoolConfigData {
	return &proxyPoolConfigData{Strategy: "round_robin"}
}

var (
	proxyPool   = loadProxyPoolConfig()
	proxyPoolMu sync.Mutex
)

// loadProxyPoolConfig 从 proxy-config.json 取出口池段。文件不存在时由
// loadProxyConfigFileLocked 完成一次性迁移。读失败退回默认值，绝不让网关
// 因配置损坏起不来。
func loadProxyPoolConfig() *proxyPoolConfigData {
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	cfg := &proxyPoolConfigData{
		Proxies:             append([]string(nil), f.Proxies...),
		Strategy:            f.Strategy,
		SubURL:              f.SubURL,
		SubCron:             f.SubCron,
		ClineUseProxies:     f.ClineUseProxies,
		AmdUseProxies:       f.AmdUseProxies,
		OprtUseProxies:      f.OprtUseProxies,
		TkhbUseProxies:      f.TkhbUseProxies,
		TraeUseProxies:      f.TraeUseProxies,
		OtherUseProxies:     f.OtherUseProxies,
		ZenUseProxies:       f.ZenUseProxies,
		WorkbuddyUseProxies: f.WorkbuddyUseProxies,
	}
	proxyFileMu.Unlock()
	normalizeProxyPoolConfig(cfg)
	return cfg
}

// proxiesChanged 按顺序比较两个代理列表是否不同（数量或任一位置变化都算）。
func proxiesChanged(a, b []string) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

// normalizeProxyPoolConfig 收敛策略取值：未知/空值一律 round_robin。
func normalizeProxyPoolConfig(c *proxyPoolConfigData) {
	if c == nil {
		return
	}
	switch strings.TrimSpace(c.Strategy) {
	case "fill", "random", "round_robin":
		c.Strategy = strings.TrimSpace(c.Strategy)
	default:
		c.Strategy = "round_robin"
	}
}

// saveProxyPoolConfig 只覆盖 proxy-config.json 的出口池段，不动健康统计与
// WorkBuddy 绑定。
func saveProxyPoolConfig(c *proxyPoolConfigData) error {
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.Proxies = append([]string(nil), c.Proxies...)
		f.Strategy = c.Strategy
		f.SubURL = c.SubURL
		f.SubCron = c.SubCron
		f.ClineUseProxies = c.ClineUseProxies
		f.AmdUseProxies = c.AmdUseProxies
		f.OprtUseProxies = c.OprtUseProxies
		f.TkhbUseProxies = c.TkhbUseProxies
		f.TraeUseProxies = c.TraeUseProxies
		f.OtherUseProxies = c.OtherUseProxies
		f.ZenUseProxies = c.ZenUseProxies
		f.WorkbuddyUseProxies = c.WorkbuddyUseProxies
	})
	return nil
}

func getProxyPool() *proxyPoolConfigData {
	proxyPoolMu.Lock()
	defer proxyPoolMu.Unlock()
	return proxyPool
}

// setProxyPool 替换并落盘代理池配置，并承担原 setZenConfig 里的代理清理链：
// 列表变化时按索引记录的冷却全部失效、在线率统计清零、被移除出口的钉定
// 客户端驱逐（索引会位移，留着就是错位的冷却和泄漏的连接）。
func setProxyPool(c *proxyPoolConfigData) {
	normalizeProxyPoolConfig(c)
	proxyPoolMu.Lock()
	old := proxyPool
	proxyPool = c
	proxyPoolMu.Unlock()
	if err := saveProxyPoolConfig(c); err != nil {
		log.Printf("proxy pool config save failed: %v", err)
	}
	if proxiesChanged(old.Proxies, c.Proxies) {
		proxyCooldownsMu.Lock()
		proxyCooldowns = map[int]time.Time{}
		proxyCooldownsMu.Unlock()
		resetProxyHealthStats()
	}
	validProxy := map[string]bool{"": true}
	for _, p := range c.Proxies {
		validProxy[p] = true
	}
	proxyClientCacheMu.Lock()
	for u, cl := range proxyClientCache {
		if !validProxy[u] {
			delete(proxyClientCache, u)
			if cl != nil {
				cl.CloseIdleConnections()
			}
		}
	}
	proxyClientCacheMu.Unlock()
	// 池列表变化重算 egress 自动绑定：新代理分给空闲 key、被移除代理的绑定
	// 作废（key 变回"无出口"从而被 pickZenKey 过滤）。key 列表变化侧的挂钩
	// 在 setZenConfig。幂等：分配没变时不写盘。
	reconcileEgressBindings()
}

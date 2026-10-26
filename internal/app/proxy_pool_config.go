package app

// 代理池配置独立于 OpenCode zen。
//
// 历史上代理池寄居在 .zen-config.json 的 zenConfigData 里，只因为最早只有
// zen 平台走代理池；后来 amd / openrouter / tokenharbor / other / trae /
// workbuddy 全部复用同一个出口池，"zen" 前缀就变成了纯误导——代理池是全局
// 能力，不属于任何一个平台。这里把它拆到 .proxy-pool.json。
//
// 迁移：首次启动（新文件不存在）时从 .zen-config.json 读取旧的
// proxies / proxyStrategy / proxySubURL / proxySubCron / zenUseProxies /
// workbuddyUseProxies 写入新文件。旧键不主动删除（zenConfigData 已无这些
// 字段，下次保存 zen 配置时自然消失），这样回滚到旧版本仍能读到代理列表。

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/kit"
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

func proxyPoolPath() string { return kit.ResolveDataPath(".proxy-pool.json") }

// loadProxyPoolConfig 读 .proxy-pool.json；文件不存在时执行一次从 zen 配置的
// 迁移。读失败（非 NotExist）退回默认值，绝不让网关因配置损坏起不来。
func loadProxyPoolConfig() *proxyPoolConfigData {
	path := proxyPoolPath()
	data, err := os.ReadFile(path)
	if err == nil {
		cfg := defaultProxyPoolConfig()
		if err := json.Unmarshal(data, cfg); err != nil {
			log.Printf("proxy pool config parse failed (%s): %v; using defaults", path, err)
			return defaultProxyPoolConfig()
		}
		normalizeProxyPoolConfig(cfg)
		// 旧版把代理别名单独存在 zen 配置的 proxyAliases 表里；折叠幂等，
		// 代理行已带 #fragment 时直接跳过。
		if foldLegacyProxyAliases(cfg) {
			if err := saveProxyPoolConfig(cfg); err != nil {
				log.Printf("proxy pool config save failed after alias fold: %v", err)
			}
		}
		return cfg
	}
	if !os.IsNotExist(err) {
		log.Printf("proxy pool config read failed (%s): %v; using defaults", path, err)
		return defaultProxyPoolConfig()
	}
	cfg := migrateProxyPoolFromZen()
	if err := saveProxyPoolConfig(cfg); err != nil {
		log.Printf("proxy pool config migrate-write failed: %v", err)
	}
	return cfg
}

// migrateProxyPoolFromZen 从 .zen-config.json 提取历史遗留的代理池字段。
// 直接读原始 JSON 而不是 getZenConfig()：包级变量初始化顺序不保证 zen 配置
// 已就绪，读文件才是稳定的。
func migrateProxyPoolFromZen() *proxyPoolConfigData {
	cfg := defaultProxyPoolConfig()
	data, err := os.ReadFile(kit.ResolveDataPath(".zen-config.json"))
	if err != nil {
		return cfg
	}
	var legacy struct {
		Proxies             []string          `json:"proxies"`
		ProxyStrategy       string            `json:"proxyStrategy"`
		ProxySubURL         string            `json:"proxySubURL"`
		ProxySubCron        string            `json:"proxySubCron"`
		ZenUseProxies       *bool             `json:"zenUseProxies"`
		WorkbuddyUseProxies *bool             `json:"workbuddyUseProxies"`
		ProxyAliases        map[string]string `json:"proxyAliases"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		log.Printf("proxy pool migration skipped: .zen-config.json unreadable: %v", err)
		return cfg
	}
	cfg.Proxies = legacy.Proxies
	if s := strings.TrimSpace(legacy.ProxyStrategy); s != "" {
		cfg.Strategy = s
	}
	cfg.SubURL = strings.TrimSpace(legacy.ProxySubURL)
	cfg.SubCron = strings.TrimSpace(legacy.ProxySubCron)
	cfg.ZenUseProxies = legacy.ZenUseProxies
	cfg.WorkbuddyUseProxies = legacy.WorkbuddyUseProxies
	foldProxyAliases(cfg, legacy.ProxyAliases)
	normalizeProxyPoolConfig(cfg)
	if len(cfg.Proxies) > 0 || cfg.SubURL != "" || cfg.ZenUseProxies != nil || cfg.WorkbuddyUseProxies != nil {
		log.Printf("proxy pool config migrated from .zen-config.json: %d proxies, strategy=%s, subURL=%t",
			len(cfg.Proxies), cfg.Strategy, cfg.SubURL != "")
	}
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

// foldLegacyProxyAliases 从 .zen-config.json 读取旧版 proxyAliases 表并折叠。
// 返回是否发生改动。
func foldLegacyProxyAliases(cfg *proxyPoolConfigData) bool {
	data, err := os.ReadFile(kit.ResolveDataPath(".zen-config.json"))
	if err != nil {
		return false
	}
	var legacy struct {
		ProxyAliases map[string]string `json:"proxyAliases"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil || len(legacy.ProxyAliases) == 0 {
		return false
	}
	return foldProxyAliases(cfg, legacy.ProxyAliases)
}

// foldProxyAliases 把"代理地址 -> 别名"折叠成代理行的 #fragment（面板与拨号
// 都按整行处理，别名随行保存）。已带 # 的行不重复折叠，因此可反复调用。
func foldProxyAliases(cfg *proxyPoolConfigData, aliases map[string]string) bool {
	if cfg == nil || len(aliases) == 0 {
		return false
	}
	changed := false
	for u, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" || strings.Contains(u, "#") {
			continue
		}
		for i, row := range cfg.Proxies {
			if row == u {
				cfg.Proxies[i] = row + "#" + alias
				changed = true
			}
		}
	}
	return changed
}

func saveProxyPoolConfig(c *proxyPoolConfigData) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	path := proxyPoolPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
		zenProxyCooldownsMu.Lock()
		zenProxyCooldowns = map[int]time.Time{}
		zenProxyCooldownsMu.Unlock()
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
}

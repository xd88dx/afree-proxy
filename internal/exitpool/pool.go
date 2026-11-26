package exitpool

// 出口池段（proxy-config.json 的 proxies/proxyStrategy/proxySubURL/...）。
//
// 历史上代理池寄居在 .zen-config.json 的 zenConfigData 里，只因为最早只有
// zen 平台走代理池；后来 other / trae / workbuddy 全部复用同一个出口池，
// "zen" 前缀就变成了纯误导——代理池是全局能力，不属于任何一个平台。
//
// 池单例（pool）在本包；"池变化 → 冷却失效/健康清零/客户端驱逐"的编排链
// 依赖各消费域，留在装配根——本包只提供 Get/Activate 与变化判定原语。

import (
	"strings"
	"sync"
)

// PoolConfig 全局出口代理池配置：列表、调度策略、订阅、测速目标。
// JSON 键名沿用面板既有契约（proxyStrategy/proxySubURL/...），前端不用改。
type PoolConfig struct {
	Proxies  []string `json:"proxies"`       // http(s)/socks5 代理,轮询出口
	Strategy string   `json:"proxyStrategy"` // round_robin / random / fill
	// 代理订阅：SubURL 非空时按 SubCron（5 字段分钟级 cron，空 = 不调度）
	// 定时拉取订阅并整池替换 Proxies。
	SubURL  string `json:"proxySubURL,omitempty"`
	SubCron string `json:"proxySubCron,omitempty"`
	// 代理测试的测速目标（探测）：空 = 默认 gstatic generate_204。
	TestTarget string `json:"proxyTestTarget,omitempty"`
	// ReorderCron 延迟重排定时（2026-11-26 用户定）：5 字段分钟级 cron，空 =
	// 不调度（面板可配，命中时全平台跑一轮重排）。
	ReorderCron string `json:"proxyReorderCron,omitempty"`
	// Zones 出口分区（2026-11-26 用户定）：出口 URL → "cn"/"global"（空/缺 =
	// 不限）。自动分配与差分改绑按 ZoneCompatible 过滤；手动绑定不受限。
	Zones map[string]string `json:"proxyZones,omitempty"`
	// 各平台走池开关已迁出（2026-11-17 配置重组）：平台键回归各平台配置文件
	// （platform_proxies.go 分发），代理文件只剩代理域本体。
}

var (
	pool     = loadPoolConfig()
	poolLock sync.Mutex
)

// loadPoolConfig 从 proxy-config.json 取出口池段。读失败退回默认值，绝不让
// 网关因配置损坏起不来。
func loadPoolConfig() *PoolConfig {
	fileMu.Lock()
	f := loadFileLocked()
	fileMu.Unlock()
	cfg := &PoolConfig{
		Proxies:     append([]string(nil), f.Proxies...),
		Strategy:    f.Strategy,
		SubURL:      f.SubURL,
		SubCron:     f.SubCron,
		TestTarget:  f.TestTarget,
		ReorderCron: f.ReorderCron,
		Zones:       CloneZones(f.Zones),
	}
	NormalizePool(cfg)
	return cfg
}

// NormalizePool 收敛策略取值：未知/空值一律 round_robin。分区表按池清理：
// 非池条目丢弃、非法 zone 值归空删除。
func NormalizePool(c *PoolConfig) {
	if c == nil {
		return
	}
	switch strings.TrimSpace(c.Strategy) {
	case "fill", "random", "round_robin":
		c.Strategy = strings.TrimSpace(c.Strategy)
	default:
		c.Strategy = "round_robin"
	}
	c.Zones = PruneZones(c.Zones, c.Proxies)
}

// PruneZones 区域表按池清理：键对不上池的条目丢弃（备注名口径）、键 rekey
// 为备注名、值非法（global 已废除/空串）删除。
func PruneZones(zones map[string]string, pool []string) map[string]string {
	if len(zones) == 0 {
		return nil
	}
	out := make(map[string]string, len(zones))
	for p, z := range zones {
		if NormalizeZone(z) == "" || !ruleKeyInPool(p, pool) {
			continue
		}
		out[RuleKeyOf(p)] = ZoneCN
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CloneZones 深拷贝分区表。
func CloneZones(zones map[string]string) map[string]string {
	if len(zones) == 0 {
		return nil
	}
	out := make(map[string]string, len(zones))
	for k, v := range zones {
		out[k] = v
	}
	return out
}

// ProxiesChanged 按顺序比较两个代理列表是否不同（数量或任一位置变化都算）。
func ProxiesChanged(a, b []string) bool {
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

// GetPool 返回池活配置（只读语义：调用方要改就整体复制后走装配根的
// setProxyPool 编排，与 zen 配置的写时复制一致）。
func GetPool() *PoolConfig {
	poolLock.Lock()
	defer poolLock.Unlock()
	return pool
}

// SavePool 只覆盖 proxy-config.json 的出口池段，不动隔离/绑定段。
func SavePool(c *PoolConfig) {
	MutateFile(func(f *FileData) {
		f.Proxies = append([]string(nil), c.Proxies...)
		f.Strategy = c.Strategy
		f.SubURL = c.SubURL
		f.SubCron = c.SubCron
		f.TestTarget = c.TestTarget
		f.ReorderCron = c.ReorderCron
		f.Zones = CloneZones(c.Zones)
	})
}

// ActivatePool 替换池活配置单例并落盘（编排链由装配根的 setProxyPool 承担：
// 冷却失效/健康清零/客户端驱逐/egress 重算都在那边做——本包不感知消费域）。
func ActivatePool(c *PoolConfig) {
	// NormalizePool 统一入口收敛（策略值/区域表 rekey+剪除）：测试与生产
	// 同口径，直塞原始键的调用方也得到一致的内存态。
	NormalizePool(c)
	poolLock.Lock()
	pool = c
	poolLock.Unlock()
	SavePool(c)
}

// SetTestTarget 更新测速目标并落盘（面板健康表输入框，回车/失焦即存）。
// 只动这一个字段，不走池编排——池子没变，不该触发冷却失效链。
func SetTestTarget(target string) {
	poolLock.Lock()
	pool.TestTarget = target
	poolLock.Unlock()
	MutateFile(func(f *FileData) {
		f.TestTarget = target
	})
}

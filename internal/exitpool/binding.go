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
	"strings"
	"sync"
)

// BindingConfig 隔离/绑定段的内存形态,与 FileData 中对应字段一一对应。
// 指针三态(ProxyIsolation/BackupProxyEnabled)保留原语义:
// nil = 未配置,走各自默认值。
type BindingConfig struct {
	ProxyIsolation     *bool               `json:"proxyIsolation,omitempty"`
	BackupProxyEnabled *bool               `json:"backupProxyEnabled,omitempty"`
	ProxyExclusions    map[string][]string `json:"proxyExclusions,omitempty"`
	// ProxyExclusionsAll 全局排除（2026-11-24 用户定）：显式字段取代「kinds
	// 覆盖全部叶子 = 全局」的推断语义——推断式在叶集增长（freepi 立项、新增
	// 平台）时会把旧规则静默降级成部分排除，新平台一键代理就漏掉排除。列进
	// 全局集的池条目对所有现有与未来平台的自动分配（一键代理/延迟重排）一律
	// 排除；手动绑定不受限。不含直连哨兵（__direct__ 仍是每条目单独设置）。
	ProxyExclusionsAll []string `json:"proxyExclusionsAll,omitempty"`
	// ProxyExclusionsAllMigrated 存量规则一次性迁移标记（2026-11-24）：启动时
	// 把 kinds 覆盖当时全部叶子的条目转入全局集并从推断表移除，防止下次启动
	// 对用户新建的「逐平台列满」规则再次自动升级（语义应由用户显式选择）。
	ProxyExclusionsAllMigrated bool           `json:"proxyExclusionsAllMigrated,omitempty"`
	ProxyBindCaps              map[string]int `json:"proxyBindCaps,omitempty"`
	// 延迟重排的"保持不变"阈值（秒）：平均握手延迟 ≤ 该值的代理视为健康、
	// 保持现绑定不参与重排。0/缺项 = 取 DefaultReorderKeepSec。
	ReorderKeepSec float64 `json:"reorderKeepSec,omitempty"`
	// ProxyAutoFailover 差分自动排除开关（2026-11-24 用户定）：开启后平台级
	// 冷却触发时跑差分判定（故障出口 gstatic 通 + 其他出口对目标 host 通），
	// 成立则把出口加入该平台排除规则并自动改绑。nil = 默认关。
	ProxyAutoFailover *bool `json:"proxyAutoFailover,omitempty"`
	// ProxyExclusionStamps 差分排除条目的 TTL 复核台账（2026-11-26 用户定）：
	// 键 = proxyURL+"\x00"+kind，值 = "since|target|okCount"（RFC3339 时间、
	// 排除时的目标 host、连续探活通过计数）。只有差分自动写入的条目有台账
	// ——手动条目不复核（永不自动移除）。每小时探活一次，连续 2 次恢复可达
	// 自动移除该 kind（exit-failover-ttl）。
	ProxyExclusionStamps map[string]string `json:"proxyExclusionStamps,omitempty"`
	// ZoneRealms 平台域声明集（2026-11-26 用户定）：面板下拉勾选"哪些平台
	// 可作为 cn 平台"（参考封禁平台交互）。键形态与语义见 zone.go
	// DeclaredRealm——zone 兼容判定中凭据有效域的唯一来源。
	ZoneRealms []string `json:"zoneRealms,omitempty"`
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
	// 存量迁移（2026-11-26 用户定备注名主键）：排除表/全局集/台账键从完整
	// URL rekey 为备注名——加载即迁移，不等首次池编辑。同备注名多键合并。
	f.ProxyExclusions = PruneProxyExclusions(f.ProxyExclusions, f.Proxies)
	f.ProxyExclusionsAll = PruneProxyEntries(f.ProxyExclusionsAll, f.Proxies)
	if len(f.ProxyExclusionStamps) > 0 {
		next := make(map[string]string, len(f.ProxyExclusionStamps))
		for k, v := range f.ProxyExclusionStamps {
			url, kind, ok := strings.Cut(k, "\x00")
			if !ok {
				continue
			}
			nk := stampKey(RuleKeyOf(url), kind)
			if _, dup := next[nk]; !dup {
				next[nk] = v
			}
		}
		f.ProxyExclusionStamps = next
	}
	bindingCfg = &BindingConfig{
		ProxyIsolation:             f.ProxyIsolation,
		BackupProxyEnabled:         f.BackupProxyEnabled,
		ProxyExclusions:            f.ProxyExclusions,
		ProxyExclusionsAll:         f.ProxyExclusionsAll,
		ProxyExclusionsAllMigrated: f.ProxyExclusionsAllMigrated,
		ProxyBindCaps:              f.ProxyBindCaps,
		ReorderKeepSec:             f.ReorderKeepSec,
		ProxyAutoFailover:          f.ProxyAutoFailover,
		ProxyExclusionStamps:       f.ProxyExclusionStamps,
		ZoneRealms:                 f.ZoneRealms,
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
		f.ProxyExclusionsAll = cp.ProxyExclusionsAll
		f.ProxyExclusionsAllMigrated = cp.ProxyExclusionsAllMigrated
		f.ProxyBindCaps = cp.ProxyBindCaps
		f.ReorderKeepSec = cp.ReorderKeepSec
		f.ProxyAutoFailover = cp.ProxyAutoFailover
		f.ProxyExclusionStamps = cp.ProxyExclusionStamps
		f.ZoneRealms = cp.ZoneRealms
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

// ruleKeyInPool 规则键是否仍对得上池：键相等，或键的备注名与池内某行备注名
// 相同（订阅换端口后键不失配，2026-11-26 用户定备注名主键）。键为裸备注名
// （规则键形态，不带 scheme）时整串视作备注名匹配。
func ruleKeyInPool(key string, pool []string) bool {
	a := AliasOf(key)
	if a == "" {
		a = key
	}
	for _, p := range pool {
		if p == key || AliasOf(p) == a {
			return true
		}
	}
	return false
}

// PruneProxyEntries 全局排除表按池清理：键对不上池的条目丢弃（备注名口径，
// 见 ruleKeyInPool），键统一 rekey 为备注名（RuleKeyOf），去重保序；直连
// 哨兵永不进全局集（直连排除仍是每条目单独设置，2026-11-24 用户定）。
func PruneProxyEntries(list []string, pool []string) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, p := range list {
		if p == ProxyRuleDirectKey || !ruleKeyInPool(p, pool) {
			continue
		}
		k := RuleKeyOf(p)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// PruneProxyExclusions 按代理池清理排除表：键对不上池的条目丢弃（备注名
// 口径——订阅换端口后规则不失配，只有代理真被删除才清），键统一 rekey 为
// 备注名，直连哨兵键保留 —— 它不是池条目，且不随池变动失效。面板保存池与
// 配置导入（跨机迁移）共用此口径。
func PruneProxyExclusions(ex map[string][]string, pool []string) map[string][]string {
	if len(ex) == 0 {
		return ex
	}
	out := make(map[string][]string, len(ex))
	for p, kinds := range ex {
		if p != ProxyRuleDirectKey && !ruleKeyInPool(p, pool) {
			continue
		}
		k := RuleKeyOf(p)
		if exist, ok := out[k]; ok {
			// rekey 合并（存量 URL 键与新别名键同指一行）：kinds 并集。
			merged := append([]string(nil), exist...)
			for _, kind := range kinds {
				dup := false
				for _, e := range merged {
					if e == kind {
						dup = true
						break
					}
				}
				if !dup {
					merged = append(merged, kind)
				}
			}
			out[k] = merged
			continue
		}
		out[k] = kinds
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

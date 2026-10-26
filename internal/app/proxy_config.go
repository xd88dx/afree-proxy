package app

// ============================================================================
// 代理域单文件配置(proxy-config.json)
//
// 代理是全局能力,不属于任何一个平台。历史上它被切成三份文件:
//   .proxy-pool.json      出口池列表 / 调度策略 / 订阅 / 平台走池开关
//   .proxy-health.json    每个出口的被动健康统计(60s 脏标记落盘)
//   .workbuddy-proxies.json  WorkBuddy 账号的主/备出口绑定
//
// 三份文件互相引用同一批出口地址,编辑一处要同步三处,且每次都要读三个文件。
// 现合并为单一 proxy-config.json:同一把 proxyFileMu 串行化"读-改-写",
// 各子系统只覆盖自己那一段,互不吞掉对方的改动。
//
// 锁序:子系统锁(proxyPoolMu / proxyHealthMu / wbProxyMu)在外,proxyFileMu
// 在内。持有 proxyFileMu 期间绝不获取任何子系统锁。
// ============================================================================

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// proxyConfigFileData 是 proxy-config.json 的落盘形态。字段按用途分段,
// JSON 键名沿用面板既有契约(proxies/proxyStrategy/...),前端零改动。
type proxyConfigFileData struct {
	// ---- 出口池 ----
	Proxies  []string `json:"proxies"`
	Strategy string   `json:"proxyStrategy"` // round_robin / random / fill
	SubURL   string   `json:"proxySubURL,omitempty"`
	SubCron  string   `json:"proxySubCron,omitempty"`
	// 各平台"未绑定凭据是否走共享池"开关。nil = 未配置 = 走池。
	// 这些开关属于代理域（代理池页的下拉），所以跟池本体放同一个文件，
	// 平台配置文件不再持有它们。
	ClineUseProxies     *bool `json:"clineUseProxies,omitempty"`
	AmdUseProxies       *bool `json:"amdUseProxies,omitempty"`
	OprtUseProxies      *bool `json:"oprtUseProxies,omitempty"`
	TkhbUseProxies      *bool `json:"tkhbUseProxies,omitempty"`
	TraeUseProxies      *bool `json:"traeUseProxies,omitempty"`
	OtherUseProxies     *bool `json:"otherUseProxies,omitempty"`
	ZenUseProxies       *bool `json:"zenUseProxies,omitempty"`
	WorkbuddyUseProxies *bool `json:"workbuddyUseProxies,omitempty"`

	// ---- 账号/凭据隔离与绑定(见 proxy_binding.go) ----
	// 隔离总开关。nil = 未配置 = 启用(默认);显式 false = 用户主动关闭。
	ProxyIsolation *bool `json:"proxyIsolation,omitempty"`
	// 备用代理开关。nil = 未配置 = 关闭;显式 true 才允许"主不通走备"。
	BackupProxyEnabled *bool `json:"backupProxyEnabled,omitempty"`
	// 自动分配排除表:代理条目 -> 排除的平台 kind,只约束自动分配路径。
	ProxyExclusions map[string][]string `json:"proxyExclusions,omitempty"`
	// 每代理绑定上限:平台 kind -> 单代理最多绑几条凭据。
	ProxyBindCaps map[string]int `json:"proxyBindCaps,omitempty"`
	// 凭据代理绑定表不放这里:它的键是 key 明文,跟 key 一起留在各平台的
	// <平台>-config.json 里(zen 的见 zenConfigData.KeyBindings)。

	// ---- 出口健康统计(被动口径,见 proxy_health.go) ----
	HealthSavedAt time.Time                    `json:"healthSavedAt,omitempty"`
	Health        map[string]*proxyHealthEntry `json:"health,omitempty"`

	// ---- WorkBuddy 账号出口绑定 ----
	WorkbuddyBindings map[string]workbuddyProxyBinding `json:"workbuddyBindings,omitempty"`
}

var (
	proxyFileMu sync.Mutex
	// proxyConfigPathOverride 测试钩子:非空时覆盖配置文件路径。
	proxyConfigPathOverride string
)

func proxyConfigPath() string {
	if proxyConfigPathOverride != "" {
		return proxyConfigPathOverride
	}
	return kit.ResolveDataPath("proxy-config.json")
}

func defaultProxyConfigFile() *proxyConfigFileData {
	return &proxyConfigFileData{Strategy: "round_robin"}
}

// loadProxyConfigFileLocked 读取(必要时迁移)代理配置。调用方必须持有 proxyFileMu。
func loadProxyConfigFileLocked() *proxyConfigFileData {
	path := proxyConfigPath()
	raw, err := os.ReadFile(path)
	if err == nil {
		var f proxyConfigFileData
		if err := json.Unmarshal(raw, &f); err != nil {
			log.Printf("proxy config: corrupt JSON at %s (using defaults; file kept as-is until next write)", path)
			return defaultProxyConfigFile()
		}
		if f.Strategy == "" {
			f.Strategy = "round_robin"
		}
		return &f
	}
	if !os.IsNotExist(err) {
		log.Printf("proxy config: read %s: %v (using defaults)", path, err)
		return defaultProxyConfigFile()
	}
	f := migrateProxyConfigFileLocked()
	saveProxyConfigFileLocked(f)
	return f
}

func saveProxyConfigFileLocked(f *proxyConfigFileData) {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		log.Printf("proxy config: marshal: %v", err)
		return
	}
	atomicWriteFile(proxyConfigPath(), raw)
}

// mutateProxyConfigFile 串行化"读-改-写"。fn 只能改自己那一段。
func mutateProxyConfigFile(fn func(*proxyConfigFileData)) {
	proxyFileMu.Lock()
	defer proxyFileMu.Unlock()
	f := loadProxyConfigFileLocked()
	fn(f)
	saveProxyConfigFileLocked(f)
}

// migrateProxyConfigFileLocked 一次性迁移:把三份旧文件的内容合并进
// proxy-config.json。旧文件保留不删,便于回滚。调用方必须持有 proxyFileMu。
func migrateProxyConfigFileLocked() *proxyConfigFileData {
	f := defaultProxyConfigFile()
	sections := 0

	// 出口池:优先 .proxy-pool.json(上一版已从 zen 拆出);
	// 没有则退回更早的 .zen-config.json 内嵌字段。
	if raw, err := os.ReadFile(kit.ResolveDataPath(".proxy-pool.json")); err == nil {
		var old proxyPoolConfigData
		if json.Unmarshal(raw, &old) == nil {
			f.Proxies = old.Proxies
			if old.Strategy != "" {
				f.Strategy = old.Strategy
			}
			f.SubURL = old.SubURL
			f.SubCron = old.SubCron
			f.ZenUseProxies = old.ZenUseProxies
			f.WorkbuddyUseProxies = old.WorkbuddyUseProxies
			sections++
		}
	} else if raw, err := os.ReadFile(kit.ResolveDataPath(".zen-config.json")); err == nil {
		var legacy struct {
			Proxies             []string          `json:"proxies"`
			ProxyStrategy       string            `json:"proxyStrategy"`
			ProxySubURL         string            `json:"proxySubURL"`
			ProxySubCron        string            `json:"proxySubCron"`
			ZenUseProxies       *bool             `json:"zenUseProxies"`
			WorkbuddyUseProxies *bool             `json:"workbuddyUseProxies"`
			ProxyAliases        map[string]string `json:"proxyAliases"`
		}
		if json.Unmarshal(raw, &legacy) == nil {
			f.Proxies = legacy.Proxies
			if legacy.ProxyStrategy != "" {
				f.Strategy = legacy.ProxyStrategy
			}
			f.SubURL = legacy.ProxySubURL
			f.SubCron = legacy.ProxySubCron
			f.ZenUseProxies = legacy.ZenUseProxies
			f.WorkbuddyUseProxies = legacy.WorkbuddyUseProxies
			foldProxyAliases(&proxyPoolConfigData{Proxies: f.Proxies}, legacy.ProxyAliases)
			sections++
		}
	}

	// 出口健康统计
	if raw, err := os.ReadFile(kit.ResolveDataPath(".proxy-health.json")); err == nil {
		var old proxyHealthFile
		if json.Unmarshal(raw, &old) == nil && len(old.Proxies) > 0 {
			f.HealthSavedAt = old.SavedAt
			f.Health = old.Proxies
			sections++
		}
	}

	// 隔离/绑定段:旧版寄居在 .zen-config.json(代理是全局能力,却由 zen 配置托管)。
	// 与出口池分支独立读取 —— 两段可能同时存在于同一个旧文件里。
	if raw, err := os.ReadFile(kit.ResolveDataPath(".zen-config.json")); err == nil {
		var old struct {
			ProxyIsolation     *bool               `json:"proxyIsolation"`
			BackupProxyEnabled *bool               `json:"backupProxyEnabled"`
			ProxyExclusions    map[string][]string `json:"proxyExclusions"`
			ProxyBindCaps      map[string]int      `json:"proxyBindCaps"`
		}
		if json.Unmarshal(raw, &old) == nil {
			moved := false
			if old.ProxyIsolation != nil {
				f.ProxyIsolation = old.ProxyIsolation
				moved = true
			}
			if old.BackupProxyEnabled != nil {
				f.BackupProxyEnabled = old.BackupProxyEnabled
				moved = true
			}
			if len(old.ProxyExclusions) > 0 {
				f.ProxyExclusions = old.ProxyExclusions
				moved = true
			}
			if len(old.ProxyBindCaps) > 0 {
				f.ProxyBindCaps = old.ProxyBindCaps
				moved = true
			}
			if moved {
				sections++
			}
		}
	}

	// WorkBuddy 账号出口绑定
	if raw, err := os.ReadFile(kit.ResolveDataPath(".workbuddy-proxies.json")); err == nil {
		var old map[string]workbuddyProxyBinding
		if json.Unmarshal(raw, &old) == nil && len(old) > 0 {
			f.WorkbuddyBindings = old
			sections++
		}
	}

	// 各平台走池开关:旧版分散在各平台配置文件里(cline 的叫 clineUseProxies,
	// 其余平台都叫 useProxies)。zen/workbuddy 的开关本来就在池文件里,无需搬。
	legacyUseProxies := []struct {
		file string
		dst  **bool
	}{
		{".cline-accounts.json", &f.ClineUseProxies},
		{".amd-config.json", &f.AmdUseProxies},
		{".or-config.json", &f.OprtUseProxies},
		{".th-config.json", &f.TkhbUseProxies},
		{".trae-config.json", &f.TraeUseProxies},
		{".other-config.json", &f.OtherUseProxies},
	}
	movedUseProxies := false
	for _, item := range legacyUseProxies {
		raw, err := os.ReadFile(kit.ResolveDataPath(item.file))
		if err != nil {
			continue
		}
		var old struct {
			UseProxies      *bool `json:"useProxies"`
			ClineUseProxies *bool `json:"clineUseProxies"`
		}
		if json.Unmarshal(raw, &old) != nil {
			continue
		}
		v := old.UseProxies
		if v == nil {
			v = old.ClineUseProxies
		}
		if v != nil {
			*item.dst = v
			movedUseProxies = true
		}
	}
	if movedUseProxies {
		sections++
	}

	if sections > 0 {
		log.Printf("proxy config: migrated %d section(s) from legacy files into %s (%d proxies)",
			sections, proxyConfigPath(), len(f.Proxies))
	}
	return f
}

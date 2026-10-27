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

	// ---- zen key 出口自动钉定（egress.go，上游 400736b）----
	// 键是 namespace+身份哈希（zen key 只存 sha256 前 8 字节 hex，不含明文）。
	// 原上游独立落 .egress-bindings.json；本仓并入 proxy-config.json 的
	// egress 段——它就是代理域的数据，与隔离/绑定段同文件同锁管理。
	EgressBindings map[string]egressBinding `json:"egressBindings,omitempty"`
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

// loadProxyConfigFileLocked 读取代理配置。调用方必须持有 proxyFileMu。
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
	f := defaultProxyConfigFile()
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

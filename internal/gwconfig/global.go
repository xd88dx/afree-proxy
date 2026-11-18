package gwconfig

import (
	"afree-proxy/internal/kit"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// ============================================================================
// 全局配置(afree-config.json) —— 跨平台运行态的唯一家
//
// 收纳真正的全局段：会话签名密钥、网关客户端 key、账号选号策略、自定义模型
// 别名(combo)、模型黑名单、平台黑名单、/admin 请求日志开关。
//
// 2026-11-17 配置重组：平台私有键全部迁出到各自平台配置文件——cline 的
// streamRequired/catalogFeedOnly 归 cline-config.json（pool.go），trae 的
// 绑定/路由/倍率/顺序/走池开关归 trae-config.json（trae_config.go），wb 的
// 绑定/账号意志/权重优化/走池开关归 wkbd-config.json（wkbd_config.go）。
// afree-config.json 只剩无平台归属的全局键。
//
// 锁序约定:子系统锁(combosMu/modelBlacklistMu/platformBanMu)在外,GlobalCfgMu
// 在内。GlobalCfgMu 持有期间绝不获取任何子系统锁,以保证单向锁序不死锁。
// ============================================================================

type GlobalConfigData struct {
	SessionSecret     string                          `json:"sessionSecret,omitempty"`
	ClientKeys        []string                        `json:"clientKeys,omitempty"` // 网关客户端 API key
	AdminLogEnabled   *bool                           `json:"adminLogEnabled,omitempty"`
	Strategy          string                          `json:"strategy,omitempty"` // 账号选号策略(各平台共用)
	Combos            []*Combo                        `json:"combos,omitempty"`
	ModelBlacklist    map[string]*ModelBlacklistEntry `json:"modelBlacklist,omitempty"`
	PlatformBlacklist map[string]*PlatformBanEntry    `json:"platformBlacklist,omitempty"`
}

var (
	GlobalCfgMu     sync.Mutex
	GlobalCfg       *GlobalConfigData
	GlobalCfgLoaded bool
)

// GlobalConfigPathOverride 测试钩子:非空时覆盖配置文件路径。
var GlobalConfigPathOverride string

func GlobalConfigPath() string {
	if GlobalConfigPathOverride != "" {
		return GlobalConfigPathOverride
	}
	return kit.ResolveDataPath("afree-config.json")
}

func DefaultGlobalConfig() *GlobalConfigData {
	return &GlobalConfigData{Strategy: "round_robin"}
}

// LoadGlobalConfigLocked 读取(并缓存)全局配置;文件不存在时直接用默认配置
// （旧文件一次性迁移已随 2026-10-27 死代码清理移除）。调用方必须持有 GlobalCfgMu。
func LoadGlobalConfigLocked() *GlobalConfigData {
	if GlobalCfgLoaded {
		return GlobalCfg
	}
	GlobalCfgLoaded = true
	GlobalCfg = DefaultGlobalConfig()
	raw, err := os.ReadFile(GlobalConfigPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("global config: read %s: %v", GlobalConfigPath(), err)
		}
		return GlobalCfg
	}
	var g GlobalConfigData
	if json.Unmarshal(raw, &g) != nil {
		// 损坏文件绝不能被默认值静默覆盖：本文件承载 clientKeys（/v1 鉴权）、
		// combos、模型/平台黑名单，而首个面板会话（loadSessionSecret）就会
		// 触发整文件写回——不隔离等于一次损坏就永久丢失这些配置。
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(GlobalConfigPath(), GlobalConfigPath()+".corrupt-"+stamp); renErr == nil {
			log.Printf("SEVERE: global config is corrupt JSON; moved to %s.corrupt-%s for manual recovery; starting clean", GlobalConfigPath(), stamp)
		} else {
			log.Printf("SEVERE: global config is corrupt JSON (quarantine rename failed: %v); starting clean", renErr)
		}
		return GlobalCfg
	}
	if g.Strategy == "" {
		g.Strategy = "round_robin"
	}
	GlobalCfg = &g
	return GlobalCfg
}

// MutateGlobalConfig 在全局锁内改配置并落盘。
func MutateGlobalConfig(fn func(*GlobalConfigData)) {
	GlobalCfgMu.Lock()
	defer GlobalCfgMu.Unlock()
	g := LoadGlobalConfigLocked()
	fn(g)
	SaveGlobalConfigLocked()
}

func SaveGlobalConfigLocked() {
	raw, err := json.MarshalIndent(GlobalCfg, "", "  ")
	if err != nil {
		log.Printf("global config: marshal: %v", err)
		return
	}
	kit.AtomicWriteFile(GlobalConfigPath(), raw)
}

// ---- 访问器(各自加锁,供外部子系统调用) ----

func GlobalSessionSecretHex() (string, bool) {
	GlobalCfgMu.Lock()
	defer GlobalCfgMu.Unlock()
	g := LoadGlobalConfigLocked()
	if g.SessionSecret == "" {
		return "", false
	}
	return g.SessionSecret, true
}

func SetGlobalSessionSecret(hexSecret string) {
	MutateGlobalConfig(func(g *GlobalConfigData) { g.SessionSecret = hexSecret })
}

func GlobalStrategy() string {
	if PoolStrategyFromEnv && EnvStrategy != "" {
		return EnvStrategy
	}
	GlobalCfgMu.Lock()
	defer GlobalCfgMu.Unlock()
	g := LoadGlobalConfigLocked()
	if g.Strategy == "" {
		return "round_robin"
	}
	return g.Strategy
}

func SetGlobalStrategy(s string) {
	MutateGlobalConfig(func(g *GlobalConfigData) { g.Strategy = s })
}

func GlobalAdminLogEnabled() bool {
	GlobalCfgMu.Lock()
	defer GlobalCfgMu.Unlock()
	g := LoadGlobalConfigLocked()
	return g.AdminLogEnabled != nil && *g.AdminLogEnabled
}

func SetGlobalAdminLogEnabled(v bool) {
	MutateGlobalConfig(func(g *GlobalConfigData) { g.AdminLogEnabled = &v })
}

func GlobalClientKeys() []string {
	GlobalCfgMu.Lock()
	defer GlobalCfgMu.Unlock()
	g := LoadGlobalConfigLocked()
	return append([]string(nil), g.ClientKeys...)
}

func SetGlobalClientKeys(keys []string) {
	cp := append([]string(nil), keys...)
	MutateGlobalConfig(func(g *GlobalConfigData) { g.ClientKeys = cp })
}

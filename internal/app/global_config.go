package app

import (
	"afree-proxy/internal/kit"
	"encoding/json"
	"log"
	"os"
	"sync"
)

// ============================================================================
// 全局配置(afree-config.json)
//
// 收纳所有"跨平台"的运行态:会话签名密钥、网关客户端 key、账号选号策略、
// 自定义模型别名(combo)、模型黑名单、平台黑名单、cline 流式学习结果。
//
// 这些数据原先散落在 .session-secret / .cline-accounts.json(adminLogEnabled、
// strategy、keys 三个字段)/ combos.json / .model-blacklist.json /
// .platform-blacklist.json / .cline-stream-required.json 里,现合并为单一
// afree-config.json。各子系统只读写自己的字段,mutateGlobalConfig 串行化
// "读-改-写"避免互相覆盖。
//
// 锁序约定:子系统锁(combosMu/modelBlacklistMu/platformBanMu)在外,globalCfgMu
// 在内。globalCfgMu 持有期间绝不获取任何子系统锁,以保证单向锁序不死锁。
// ============================================================================

type globalConfigData struct {
	SessionSecret       string                          `json:"sessionSecret,omitempty"`
	ClientKeys          []string                        `json:"clientKeys,omitempty"` // 网关客户端 API key
	AdminLogEnabled     *bool                           `json:"adminLogEnabled,omitempty"`
	Strategy            string                          `json:"strategy,omitempty"` // 账号选号策略(七平台共用)
	Combos              []*Combo                        `json:"combos,omitempty"`
	ModelBlacklist      map[string]*modelBlacklistEntry `json:"modelBlacklist,omitempty"`
	PlatformBlacklist   map[string]*platformBanEntry    `json:"platformBlacklist,omitempty"`
	ClineStreamRequired map[string]string               `json:"clineStreamRequired,omitempty"`
}

var (
	globalCfgMu     sync.Mutex
	globalCfg       *globalConfigData
	globalCfgLoaded bool
)

// globalConfigPathOverride 测试钩子:非空时覆盖配置文件路径。
var globalConfigPathOverride string

func globalConfigPath() string {
	if globalConfigPathOverride != "" {
		return globalConfigPathOverride
	}
	return kit.ResolveDataPath("afree-config.json")
}

func defaultGlobalConfig() *globalConfigData {
	return &globalConfigData{Strategy: "round_robin"}
}

// loadGlobalConfigLocked 读取(并缓存)全局配置;文件不存在时直接用默认配置
// （旧文件一次性迁移已随 2026-10-27 死代码清理移除）。调用方必须持有 globalCfgMu。
func loadGlobalConfigLocked() *globalConfigData {
	if globalCfgLoaded {
		return globalCfg
	}
	globalCfgLoaded = true
	globalCfg = defaultGlobalConfig()
	raw, err := os.ReadFile(globalConfigPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("global config: read %s: %v", globalConfigPath(), err)
		}
		return globalCfg
	}
	var g globalConfigData
	if json.Unmarshal(raw, &g) != nil {
		log.Printf("global config: corrupt JSON at %s (starting clean)", globalConfigPath())
		return globalCfg
	}
	if g.Strategy == "" {
		g.Strategy = "round_robin"
	}
	globalCfg = &g
	return globalCfg
}

// mutateGlobalConfig 在全局锁内改配置并落盘。
func mutateGlobalConfig(fn func(*globalConfigData)) {
	globalCfgMu.Lock()
	defer globalCfgMu.Unlock()
	g := loadGlobalConfigLocked()
	fn(g)
	saveGlobalConfigLocked()
}

func saveGlobalConfigLocked() {
	raw, err := json.MarshalIndent(globalCfg, "", "  ")
	if err != nil {
		log.Printf("global config: marshal: %v", err)
		return
	}
	atomicWriteFile(globalConfigPath(), raw)
}

// atomicWriteFile 原子写:先写临时文件再 rename,崩溃中途写入不会截断原文件。
func atomicWriteFile(path string, data []byte) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("replace %s: %v", path, err)
		_ = os.Remove(tmp)
	}
}

// ---- 访问器(各自加锁,供外部子系统调用) ----

func globalSessionSecretHex() (string, bool) {
	globalCfgMu.Lock()
	defer globalCfgMu.Unlock()
	g := loadGlobalConfigLocked()
	if g.SessionSecret == "" {
		return "", false
	}
	return g.SessionSecret, true
}

func setGlobalSessionSecret(hexSecret string) {
	mutateGlobalConfig(func(g *globalConfigData) { g.SessionSecret = hexSecret })
}

func globalStrategy() string {
	if poolStrategyFromEnv && envStrategy != "" {
		return envStrategy
	}
	globalCfgMu.Lock()
	defer globalCfgMu.Unlock()
	g := loadGlobalConfigLocked()
	if g.Strategy == "" {
		return "round_robin"
	}
	return g.Strategy
}

func setGlobalStrategy(s string) {
	mutateGlobalConfig(func(g *globalConfigData) { g.Strategy = s })
}

func globalAdminLogEnabled() bool {
	globalCfgMu.Lock()
	defer globalCfgMu.Unlock()
	g := loadGlobalConfigLocked()
	return g.AdminLogEnabled != nil && *g.AdminLogEnabled
}

func setGlobalAdminLogEnabled(v bool) {
	mutateGlobalConfig(func(g *globalConfigData) { g.AdminLogEnabled = &v })
}

func globalClientKeys() []string {
	globalCfgMu.Lock()
	defer globalCfgMu.Unlock()
	g := loadGlobalConfigLocked()
	return append([]string(nil), g.ClientKeys...)
}

func setGlobalClientKeys(keys []string) {
	cp := append([]string(nil), keys...)
	mutateGlobalConfig(func(g *globalConfigData) { g.ClientKeys = cp })
}

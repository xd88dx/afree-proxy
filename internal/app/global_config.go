package app

import (
	"afree-proxy/internal/kit"
	"encoding/json"
	"log"
	"os"
	"strings"
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

// loadGlobalConfigLocked 读取(并缓存)全局配置;文件不存在时先尝试从六个旧
// 文件一次性迁移。调用方必须持有 globalCfgMu。
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
		migrateGlobalConfigLocked()
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

// migrateGlobalConfigLocked 一次性迁移:把散落的旧文件内容合并进
// afree-config.json。仅在 afree-config.json 不存在时执行;迁移成功后旧文件
// 保留(部署方确认后自行删除),以便回滚。调用方必须持有 globalCfgMu。
func migrateGlobalConfigLocked() {
	moved := 0
	if data, err := os.ReadFile(kit.ResolveDataPath(".session-secret")); err == nil {
		if s := strings.TrimSpace(string(data)); len(s) >= 64 {
			globalCfg.SessionSecret = s
			moved++
		}
	}
	if data, err := os.ReadFile(kit.ResolveDataPath("combos.json")); err == nil {
		var list []*Combo
		if json.Unmarshal(data, &list) == nil && len(list) > 0 {
			globalCfg.Combos = list
			moved++
		}
	}
	if data, err := os.ReadFile(kit.ResolveDataPath(".model-blacklist.json")); err == nil {
		var f modelBlacklistFile
		if json.Unmarshal(data, &f) == nil && len(f.Items) > 0 {
			globalCfg.ModelBlacklist = f.Items
			moved++
		}
	}
	if data, err := os.ReadFile(kit.ResolveDataPath(".platform-blacklist.json")); err == nil {
		var f platformBanFile
		if json.Unmarshal(data, &f) == nil && len(f.Platforms) > 0 {
			globalCfg.PlatformBlacklist = f.Platforms
			moved++
		}
	}
	if data, err := os.ReadFile(kit.ResolveDataPath(".cline-stream-required.json")); err == nil {
		var m map[string]string
		if json.Unmarshal(data, &m) == nil && len(m) > 0 {
			globalCfg.ClineStreamRequired = m
			moved++
		}
	}
	if data, err := os.ReadFile(kit.ResolveDataPath(".cline-accounts.json")); err == nil {
		var probe struct {
			AdminLogEnabled *bool    `json:"adminLogEnabled"`
			Strategy        string   `json:"strategy"`
			Keys            []string `json:"keys"`
		}
		if json.Unmarshal(data, &probe) == nil {
			if probe.AdminLogEnabled != nil {
				globalCfg.AdminLogEnabled = probe.AdminLogEnabled
				moved++
			}
			if probe.Strategy != "" {
				globalCfg.Strategy = probe.Strategy
				moved++
			}
			if len(probe.Keys) > 0 {
				globalCfg.ClientKeys = probe.Keys
				moved++
			}
		}
	}
	if moved > 0 {
		saveGlobalConfigLocked()
		log.Printf("global config: migrated %d section(s) from legacy files into %s", moved, globalConfigPath())
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

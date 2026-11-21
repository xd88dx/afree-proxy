package gwconfig

import (
	"afree-proxy/internal/kit"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
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
	// ToolRename 出站工具改名（2026-11-21，免费层身份指纹闸对策的名称面）：
	// 请求侧把客户端工具名改写为 Prefix+SHA256 前 12 位，响应侧还原真名。
	// 无独立开关：platforms 有勾选即开启（2026-11-21 用户定）。
	ToolRename *ToolRenameConfig `json:"toolRename,omitempty"`
}

// ToolRenameConfig 工具改名全局配置。Platforms 键=平台 kind（cline/zen/trae/
// workbuddy/freepi/other），缺项=该平台不改写；全空=整体关闭。
type ToolRenameConfig struct {
	Prefix    string          `json:"prefix,omitempty"`
	Platforms map[string]bool `json:"platforms,omitempty"`
}

// Active 是否有生效平台（勾选即开启，2026-11-21 用户定）。
func (c *ToolRenameConfig) Active() bool {
	if c == nil {
		return false
	}
	for _, on := range c.Platforms {
		if on {
			return true
		}
	}
	return false
}

// Normalize 校验并规范化：有勾选平台时前缀必填且必须是安全工具名头（字母
// 开头，字母数字下划线，1~16 字符），禁用已知反代前缀与官方前缀（防撞指
// 纹/闸）；无勾选=整体关闭，前缀不校验。
func (c *ToolRenameConfig) Normalize() error {
	if c == nil || !c.Active() {
		return nil
	}
	p := strings.TrimSpace(c.Prefix)
	if p == "" {
		return fmt.Errorf("toolRename.prefix is required when platforms are selected")
	}
	if len(p) > 16 {
		return fmt.Errorf("toolRename.prefix too long (max 16)")
	}
	if first := p[0]; !(first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z') {
		return fmt.Errorf("toolRename.prefix must start with a letter")
	}
	for i := 0; i < len(p); i++ {
		ch := p[i]
		ok := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_'
		if !ok {
			return fmt.Errorf("toolRename.prefix accepts letters/digits/underscore only")
		}
	}
	low := strings.ToLower(p) + "_"
	for _, banned := range []string{"clint_", "mcp_", "oc_", "zen_", "freepi_", "trae_", "wbcn_", "wbgb_", "other_"} {
		if strings.HasPrefix(low, banned) {
			return fmt.Errorf("toolRename.prefix %q collides with reserved prefix %q", p, banned)
		}
	}
	c.Prefix = p
	return nil
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

// GetToolRename 返回工具改名配置（nil → 全关缺省）。启用时先 Normalize：
// 配置文件被手工改出非法前缀时按关闭处理并告警，绝不让半非法配置半生效。
func GetToolRename() *ToolRenameConfig {
	GlobalCfgMu.Lock()
	defer GlobalCfgMu.Unlock()
	g := LoadGlobalConfigLocked()
	c := g.ToolRename
	if c == nil {
		return &ToolRenameConfig{}
	}
	cp := *c
	if cp.Platforms == nil {
		cp.Platforms = map[string]bool{}
	}
	if cp.Active() {
		if err := cp.Normalize(); err != nil {
			log.Printf("WARN: toolRename config invalid (%v), treating as disabled", err)
			return &ToolRenameConfig{}
		}
	}
	return &cp
}

// SetToolRename 保存工具改名配置（面板保存口）。写入前 Normalize 校验，
// 非法前缀返回错误不落盘。
func SetToolRename(c ToolRenameConfig) error {
	if err := c.Normalize(); err != nil {
		return err
	}
	MutateGlobalConfig(func(g *GlobalConfigData) { g.ToolRename = &c })
	return nil
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

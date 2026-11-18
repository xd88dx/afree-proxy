package trae

// ============================================================================
// Trae 平台独立配置(trae-config.json) —— afree 侧增量配置
//
// 2026-11-17 配置重组：原住 afree-config.json 的 trae 子域（2026-11-16 前住
// proxy-config.json 的 trae2api* 段）迁入本文件，与「一平台一配置文件」铁律
// 对齐。收纳 afree 层实现的 trae 平台业务增量：
//   - Bindings  账号出口绑定（键 = 池 UID）
//   - Routing   反代开关（键 = 池 UID）
//   - ModelRateFilterCN/Intl 分域倍率筛选
//   - DisplayOrder 凭据展示顺序
//   - UseProxies 走池开关（原 proxy-config.json 的 traeUseProxies，平台键
//     统一迁回各平台配置文件）
//
// vendored trae2api 的持久层（trae2api/config.json、账号快照）是 vendor 自管
// 语义，afree 零写入——两套文件互不混放。trae-config.json 是配置态（进
// config-manifest 监测）；data/trae2api/tokens.json 是状态态（token 记账，见
// trae2api_stats.go）。
//
// 锁序：TraeCfgMu 是本文件唯一锁，绝不与 Trae2apiMu/TraeBindingsMu 交叉持有
// ——调用方先取本锁再取子系统锁的路径不存在（访问器都在锁外返回副本）。
// ============================================================================

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

type TraeConfigData struct {
	// Bindings 账号出口绑定（键 = 池 UID，trae-cn-<userId>）。主优先、备兜底，
	// 双不可用 = 本次请求直接失败（隔离优先），绝不退回直连。
	Bindings map[string]Trae2apiProxyBinding `json:"bindings,omitempty"`
	// Routing 反代开关（键 = 池 UID）。查表缺失 = 启用，新增身份由
	// AccountAddedHook 显式落 false。
	Routing map[string]bool `json:"routing,omitempty"`
	// ModelRateFilterCN/Intl 分域倍率筛选（2026-11-07 拆分）：nil = 未配置 =
	// 不筛（0 值语义）。CN 过滤 tacn:，Intl 过滤 tagb:。显式 0 = 关
	// （指针区分「未配置」与「关」）。
	ModelRateFilterCN   *float64 `json:"modelRateFilterCN,omitempty"`
	ModelRateFilterIntl *float64 `json:"modelRateFilterIntl,omitempty"`
	// DisplayOrder 凭据展示顺序（面板拖拽；UID 列表，未提及者排尾按 UID）。
	DisplayOrder []string `json:"displayOrder,omitempty"`
	// UseProxies 走池开关：nil = 未配置 = 走池（与其他平台同约定）。
	UseProxies *bool `json:"useProxies,omitempty"`
}

var (
	TraeCfgMu     sync.Mutex
	TraeCfg       *TraeConfigData
	TraeCfgLoaded bool
)

// TraeAfreeConfigPathOverride 测试钩子：非空时覆盖配置文件路径。
var TraeAfreeConfigPathOverride string

func TraeAfreeConfigPath() string {
	if TraeAfreeConfigPathOverride != "" {
		return TraeAfreeConfigPathOverride
	}
	return kit.ResolveDataPath("trae-config.json")
}

// LoadTraeConfigLocked 读取（并缓存）trae 平台配置；文件不存在用零值起步
// （无兼容迁移代码——配置重组后旧键靠 JSON 未知字段忽略，不读旧位置）。
func LoadTraeConfigLocked() *TraeConfigData {
	if TraeCfgLoaded {
		return TraeCfg
	}
	TraeCfgLoaded = true
	TraeCfg = &TraeConfigData{}
	raw, err := os.ReadFile(TraeAfreeConfigPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("trae config: read %s: %v", TraeAfreeConfigPath(), err)
		}
		return TraeCfg
	}
	if json.Unmarshal(raw, TraeCfg) != nil {
		stamp := TimeNowStamp()
		if renErr := os.Rename(TraeAfreeConfigPath(), TraeAfreeConfigPath()+".corrupt-"+stamp); renErr == nil {
			log.Printf("SEVERE: trae config is corrupt JSON; moved to %s.corrupt-%s for manual recovery; starting clean", TraeAfreeConfigPath(), stamp)
		} else {
			log.Printf("SEVERE: trae config is corrupt JSON (quarantine rename failed: %v); starting clean", renErr)
		}
	}
	return TraeCfg
}

// MutateTraeConfig 在 TraeCfgMu 锁内读改写并落盘。
func MutateTraeConfig(fn func(*TraeConfigData)) {
	TraeCfgMu.Lock()
	defer TraeCfgMu.Unlock()
	fn(LoadTraeConfigLocked())
	SaveTraeConfigLocked()
}

func SaveTraeConfigLocked() {
	raw, err := json.MarshalIndent(TraeCfg, "", "  ")
	if err != nil {
		log.Printf("trae config: marshal: %v", err)
		return
	}
	kit.AtomicWriteFile(TraeAfreeConfigPath(), raw)
}

// ---- 访问器（锁外返回副本） ----

func TraeConfigBindings() map[string]Trae2apiProxyBinding {
	TraeCfgMu.Lock()
	defer TraeCfgMu.Unlock()
	g := LoadTraeConfigLocked()
	if g.Bindings == nil {
		return nil
	}
	cp := make(map[string]Trae2apiProxyBinding, len(g.Bindings))
	for k, v := range g.Bindings {
		cp[k] = v
	}
	return cp
}

func TraeConfigRouting() map[string]bool {
	TraeCfgMu.Lock()
	defer TraeCfgMu.Unlock()
	g := LoadTraeConfigLocked()
	if g.Routing == nil {
		return nil
	}
	cp := make(map[string]bool, len(g.Routing))
	for k, v := range g.Routing {
		cp[k] = v
	}
	return cp
}

func TraeConfigDisplayOrder() []string {
	TraeCfgMu.Lock()
	defer TraeCfgMu.Unlock()
	g := LoadTraeConfigLocked()
	if len(g.DisplayOrder) == 0 {
		return nil
	}
	return append([]string(nil), g.DisplayOrder...)
}

func TraeConfigUseProxies() *bool {
	TraeCfgMu.Lock()
	defer TraeCfgMu.Unlock()
	g := LoadTraeConfigLocked()
	if g.UseProxies == nil {
		return nil
	}
	v := *g.UseProxies
	return &v
}

func TraeConfigRateFilters() (cn, intl *float64) {
	TraeCfgMu.Lock()
	defer TraeCfgMu.Unlock()
	g := LoadTraeConfigLocked()
	if g.ModelRateFilterCN != nil {
		v := *g.ModelRateFilterCN
		cn = &v
	}
	if g.ModelRateFilterIntl != nil {
		v := *g.ModelRateFilterIntl
		intl = &v
	}
	return cn, intl
}

func TimeNowStamp() string {
	return time.Now().Format("20060102-150405")
}

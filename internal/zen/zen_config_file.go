package zen

// ============================================================================
// zen 单文件配置(zen-config.json)
//
// zen 的配置态与三个附加段（sessions / modelAliases / endpoints）同住
// zen-config.json：同一把 zenFileMu 串行化"读-改-写"，各子系统只覆盖自己
// 那一段。历史上散在 .zen-config.json / .zen-sessions.json /
// .zen-model-aliases.json / .zen-endpoints.json 四份旧文件里的数据早已合并
// 完毕；旧点文件的读取迁移随 2026-11-01 死代码清理移除（生产/本机部署态
// 均已无旧点文件，实测判据）。
//
// 锁序:子系统锁(zenConfigMu / zenSessMu / zenDepMu / zenEndpointSaveMu)在外,
// zenFileMu 在内。持有 zenFileMu 期间绝不获取任何子系统锁。
// ============================================================================

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// ZenConfigFileData 是 zen-config.json 的落盘形态。ZenConfigData 内嵌展平,
// 保持既有 JSON 键名(key/keys/baseURL/...)不变,前端零改动。
type ZenConfigFileData struct {
	ZenConfigData
	// ZenUseProxies 走池开关（2026-11-17 配置重组迁入：原 proxy-config.json
	// 的 zenUseProxies）。nil = 未配置 = 走池（与其他平台同约定）。
	ZenUseProxies *bool `json:"zenUseProxies,omitempty"`
	// EgressBindings zen key 出口自动钉定表（egress.go；2026-11-17 配置重组
	// 从 proxy-config.json 迁入——凭据级出口绑定跟凭据所在平台文件走，铁律 2）。
	// 键是 namespace+身份哈希（zen key 只存 sha256 前 8 字节 hex，不含明文）。
	// 真身在 egress.go 包级（egressMu），本段只是持久层。
	EgressBindings map[string]EgressBinding `json:"egressBindings,omitempty"`
	// Sessions 粘性会话表（已弃用，2026-11-15 迁入 zen-state.json）：真身与
	// 持久化都在 zen_session.go/zen_usage.go，本字段仅供一次性迁移读取
	//（LoadZenSessions 把 config 残留条目合入 state 后清空写回）。旧备份
	// 文件里的 sessions 键由备份导入链直接回填真身，不经本字段。
	Sessions map[string]*ZenSessionEntry `json:"sessions,omitempty"`
	// ModelAliases 弃用模型映射:规范化模型 ID -> 继任者。
	ModelAliases map[string]*ZenDepEntry `json:"modelAliases,omitempty"`
	// Endpoints 端点学习结果:模型 ID -> "responses" / "chat"。
	Endpoints map[string]string `json:"endpoints,omitempty"`
}

var (
	zenFileMu sync.Mutex
	// zenConfigPathOverride 测试钩子:非空时覆盖配置文件路径。
	zenConfigPathOverride string
)

func ZenConfigPath() string {
	if zenConfigPathOverride != "" {
		return zenConfigPathOverride
	}
	return kit.ResolveDataPath("zen-config.json")
}

func DefaultZenConfigFile() *ZenConfigFileData {
	return &ZenConfigFileData{ZenConfigData: *DefaultZenConfig()}
}

// LoadZenConfigFileLocked 读取(必要时迁移)zen 配置。调用方必须持有 zenFileMu。
// 与历史实现一致:解析失败先把坏文件改名留证,再以默认配置启动,避免下一次
// 保存把可手工恢复的原文覆盖掉。
func LoadZenConfigFileLocked() *ZenConfigFileData {
	path := ZenConfigPath()
	raw, err := os.ReadFile(path)
	if err == nil {
		var f ZenConfigFileData
		if err := json.Unmarshal(raw, &f); err != nil {
			stamp := time.Now().Format("20060102-150405")
			if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
				log.Printf("zen config is corrupt JSON; moved to %s.corrupt-%s", path, stamp)
			}
			log.Printf("zen config parse failed: %v", err)
			return DefaultZenConfigFile()
		}
		return &f
	}
	if !os.IsNotExist(err) {
		log.Printf("zen config: read %s: %v (using defaults)", path, err)
		return DefaultZenConfigFile()
	}
	// 文件不存在：默认配置落盘。旧点文件（.zen-config.json 等）的一次性迁移
	// 已随 2026-11-01 死代码清理移除——生产/本机部署态均已无旧点文件（实测）。
	f := DefaultZenConfigFile()
	SaveZenConfigFileLocked(f)
	return f
}

func SaveZenConfigFileLocked(f *ZenConfigFileData) {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		log.Printf("zen config marshal failed: %v", err)
		return
	}
	kit.AtomicWriteFile(ZenConfigPath(), raw)
}

// MutateZenConfigFile 串行化"读-改-写"。fn 只能改自己那一段。
func MutateZenConfigFile(fn func(*ZenConfigFileData)) {
	zenFileMu.Lock()
	defer zenFileMu.Unlock()
	f := LoadZenConfigFileLocked()
	fn(f)
	SaveZenConfigFileLocked(f)
}

// ZenConfigUseProxies 走池开关读取（nil = 走池默认）。
func ZenConfigUseProxies() *bool {
	zenFileMu.Lock()
	defer zenFileMu.Unlock()
	return LoadZenConfigFileLocked().ZenUseProxies
}

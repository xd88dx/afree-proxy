package app

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

// zenConfigFileData 是 zen-config.json 的落盘形态。zenConfigData 内嵌展平,
// 保持既有 JSON 键名(key/keys/baseURL/...)不变,前端零改动。
type zenConfigFileData struct {
	zenConfigData
	// Sessions 粘性会话表:key 明文 -> 会话条目。
	Sessions map[string]*zenSessionEntry `json:"sessions,omitempty"`
	// ModelAliases 弃用模型映射:规范化模型 ID -> 继任者。
	ModelAliases map[string]*zenDepEntry `json:"modelAliases,omitempty"`
	// Endpoints 端点学习结果:模型 ID -> "responses" / "chat"。
	Endpoints map[string]string `json:"endpoints,omitempty"`
}

var (
	zenFileMu sync.Mutex
	// zenConfigPathOverride 测试钩子:非空时覆盖配置文件路径。
	zenConfigPathOverride string
)

func zenConfigPath() string {
	if zenConfigPathOverride != "" {
		return zenConfigPathOverride
	}
	return kit.ResolveDataPath("zen-config.json")
}

func defaultZenConfigFile() *zenConfigFileData {
	return &zenConfigFileData{zenConfigData: *defaultZenConfig()}
}

// loadZenConfigFileLocked 读取(必要时迁移)zen 配置。调用方必须持有 zenFileMu。
// 与历史实现一致:解析失败先把坏文件改名留证,再以默认配置启动,避免下一次
// 保存把可手工恢复的原文覆盖掉。
func loadZenConfigFileLocked() *zenConfigFileData {
	path := zenConfigPath()
	raw, err := os.ReadFile(path)
	if err == nil {
		var f zenConfigFileData
		if err := json.Unmarshal(raw, &f); err != nil {
			stamp := time.Now().Format("20060102-150405")
			if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
				log.Printf("zen config is corrupt JSON; moved to %s.corrupt-%s", path, stamp)
			}
			log.Printf("zen config parse failed: %v", err)
			return defaultZenConfigFile()
		}
		return &f
	}
	if !os.IsNotExist(err) {
		log.Printf("zen config: read %s: %v (using defaults)", path, err)
		return defaultZenConfigFile()
	}
	// 文件不存在：默认配置落盘。旧点文件（.zen-config.json 等）的一次性迁移
	// 已随 2026-11-01 死代码清理移除——生产/本机部署态均已无旧点文件（实测）。
	f := defaultZenConfigFile()
	saveZenConfigFileLocked(f)
	return f
}

func saveZenConfigFileLocked(f *zenConfigFileData) {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		log.Printf("zen config marshal failed: %v", err)
		return
	}
	atomicWriteFile(zenConfigPath(), raw)
}

// mutateZenConfigFile 串行化"读-改-写"。fn 只能改自己那一段。
func mutateZenConfigFile(fn func(*zenConfigFileData)) {
	zenFileMu.Lock()
	defer zenFileMu.Unlock()
	f := loadZenConfigFileLocked()
	fn(f)
	saveZenConfigFileLocked(f)
}


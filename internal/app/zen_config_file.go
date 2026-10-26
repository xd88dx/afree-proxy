package app

// ============================================================================
// zen 单文件配置(zen-config.json)
//
// 历史上 zen 的数据散在四个文件里:
//   .zen-config.json        key 池 / baseURL / 并发 / 重试 / compaction
//   .zen-sessions.json      粘性会话表(每 key 一个会话 ID)
//   .zen-model-aliases.json 弃用模型 -> 继任模型的学习结果
//   .zen-endpoints.json     模型走 chat / responses 的学习结果
//
// 后三者都是"zen 这一个平台的运行态",却被拆成三个文件:同一把锁要维护四份
// 落盘,损坏/备份/迁移都要逐文件处理。现合并为单一 zen-config.json:
// 内嵌 zen 配置 + sessions / modelAliases / endpoints 三个附加段,
// 同一把 zenFileMu 串行化"读-改-写",各子系统只覆盖自己那一段。
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
	f := migrateZenConfigFileLocked()
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

// migrateZenConfigFileLocked 一次性迁移:把四份旧文件合并进 zen-config.json。
// 旧文件保留不删,便于回滚。调用方必须持有 zenFileMu。
func migrateZenConfigFileLocked() *zenConfigFileData {
	f := defaultZenConfigFile()
	sections := 0

	if raw, err := os.ReadFile(kit.ResolveDataPath(".zen-config.json")); err == nil {
		var old zenConfigData
		if json.Unmarshal(raw, &old) == nil {
			f.zenConfigData = old
			sections++
		}
	}
	if raw, err := os.ReadFile(kit.ResolveDataPath(".zen-sessions.json")); err == nil {
		var old map[string]*zenSessionEntry
		if json.Unmarshal(raw, &old) == nil && len(old) > 0 {
			f.Sessions = old
			sections++
		}
	}
	if raw, err := os.ReadFile(kit.ResolveDataPath(".zen-model-aliases.json")); err == nil {
		var old map[string]*zenDepEntry
		if json.Unmarshal(raw, &old) == nil && len(old) > 0 {
			f.ModelAliases = old
			sections++
		}
	}
	if raw, err := os.ReadFile(kit.ResolveDataPath(".zen-endpoints.json")); err == nil {
		var old map[string]string
		if json.Unmarshal(raw, &old) == nil && len(old) > 0 {
			f.Endpoints = old
			sections++
		}
	}

	if sections > 0 {
		log.Printf("zen config: migrated %d section(s) from legacy files into %s", sections, zenConfigPath())
	}
	return f
}

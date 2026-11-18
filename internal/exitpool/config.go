// Package exitpool 出口代理域（2026-11-17 结构重构 P3b 从 internal/app 出包）：
// 代理池/隔离绑定/健康统计/探测/订阅的配置存取与核心语义。平台无关——
// cline/zen/trae/wb/other 共用同一出口池；各平台自己的绑定接线（凭据↔出口）
// 与面板入口仍留装配根（internal/app），经本包导出面存取代理域数据。
package exitpool

// proxy-config.json 是代理域唯一配置文件：出口池段 / 隔离绑定段 / （健康
// 统计已拆 proxy-state.json）。本文件承载文件读写与顶层段结构；池语义见
// pool.go，隔离/绑定段语义见 binding.go。

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// FileData proxy-config.json 的落盘形态（全部段合一）。
type FileData struct {
	Proxies    []string          `json:"proxies"`
	Strategy   string            `json:"proxyStrategy"`
	SubURL     string            `json:"proxySubURL,omitempty"`
	SubCron    string            `json:"proxySubCron,omitempty"`
	TestTarget string            `json:"proxyTestTarget,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	// ---- 出口健康统计已拆出到独立 proxy-state.json（proxy_health.go，
	// 2026-10-31）：随请求高频变化的使用痕迹不进配置文件，外部监测配置变更
	// 不被统计噪声淹没。（旧文件 health 段的一次性迁移已随清理移除。）

	// ---- 平台键已全部迁出（2026-11-17 配置重组）：五平台走池开关归各自
	// 平台配置文件（platform_proxies.go 分发）、workbuddyBindings 归
	// wkbd-config.json、trae2api* 段归 trae-config.json（2026-11-16）、
	// zen egress 钉定表归 zen-config.json（凭据级绑定跟平台文件走，铁律 2）。
	// 旧键经 JSON 未知字段忽略，一次面板保存后从文件消失。

	// ---- 隔离/绑定段（binding.go）----
	ProxyIsolation     *bool               `json:"proxyIsolation,omitempty"`
	BackupProxyEnabled *bool               `json:"backupProxyEnabled,omitempty"`
	ProxyExclusions    map[string][]string `json:"proxyExclusions,omitempty"`
	ProxyBindCaps      map[string]int      `json:"proxyBindCaps,omitempty"`
	ReorderKeepSec     float64             `json:"reorderKeepSec,omitempty"`
}

var (
	fileMu sync.Mutex
	// PathOverride 测试钩子：非空时覆盖配置文件路径。
	PathOverride string
)

// ConfigPath proxy-config.json 路径（测试可经 PathOverride 覆盖）。
func ConfigPath() string {
	if PathOverride != "" {
		return PathOverride
	}
	return kit.ResolveDataPath("proxy-config.json")
}

// loadFileLocked 读取代理域配置。调用方必须持有 fileMu。
// 解析失败先把坏文件改名留证，再以默认配置启动，避免下一次保存把可手工
// 恢复的原文覆盖掉。
func loadFileLocked() *FileData {
	path := ConfigPath()
	raw, err := os.ReadFile(path)
	if err == nil {
		var f FileData
		if err := json.Unmarshal(raw, &f); err != nil {
			stamp := time.Now().Format("20060102-150405")
			if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
				log.Printf("proxy config is corrupt JSON; moved to %s.corrupt-%s", path, stamp)
			}
			log.Printf("proxy config parse failed: %v", err)
			return &FileData{}
		}
		return &f
	}
	if !os.IsNotExist(err) {
		log.Printf("proxy config: read %s: %v (using defaults)", path, err)
		return &FileData{}
	}
	return &FileData{}
}

// saveFileLocked 整文件落盘（tmp+rename 原子写）。调用方必须持有 fileMu。
func saveFileLocked(f *FileData) {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		log.Printf("proxy config marshal failed: %v", err)
		return
	}
	kit.AtomicWriteFile(ConfigPath(), raw)
}

// MutateFile 串行化"读-改-写"代理域配置文件。fn 只改自己关心的段——整文件
// 原文重写，未触及字段原样保留。导出给装配根的分段写方使用；包内写路径
// （SavePool/SetBinding）也经它落盘。
func MutateFile(fn func(f *FileData)) {
	fileMu.Lock()
	defer fileMu.Unlock()
	f := loadFileLocked()
	fn(f)
	saveFileLocked(f)
}

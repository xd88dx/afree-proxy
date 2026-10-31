package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// ============================================================================
// 配置指纹清单（config-manifest.json）
//
// 外部监测口径的收敛点（2026-11-01 用户定）：配置态文件集合随平台增减而变
// （每平台一个 <x>-config.json + workbuddy 子系统），为免外部监测脚本跟着
// 改，程序自己维护一份全部配置文件的 sha256 清单——外部只盯 config-manifest
// .json 这一个文件：它变了 = 某个配置文件真的变了。
//
// 清单来源用 glob 收集而非硬编码列表，新平台落盘 <x>-config.json 自动纳入：
//   - 数据目录顶层 *-config.json（各平台配置态）
//   - workbuddy/config.json（vendor 子系统配置）
//   - workbuddy/auths/*.json（wb 凭据文件，token 刷新会回写）
// 状态态文件（*-state.json 等）不进清单——它们随请求高频变化，不属监测对象。
//
// 零噪声保证：与上次 entries 对比无变化时不写盘（连 updatedAt 都保持不变），
// 所以对 manifest 本身做 watch/hash 不会收到任何非配置变化的假信号。
//
// 重建：后台 10s 轮询（配置文件总量通常 < 百 KB，sha256 开销可忽略），无需
// 在各域保存路径上挂钩子——轮询天然覆盖所有写点，漏接不可能发生。
// ============================================================================

// configManifestEntry 单个配置态文件的指纹。File 为相对 DATA_DIR 的路径
//（正斜杠语义由 filepath.Join 产出，Windows 上显示为反斜杠——监测脚本按
// 平台分隔符处理即可）。
type configManifestEntry struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// configManifestFile config-manifest.json 的落盘形态。UpdatedAt 是配置态
// 最后一次变化的时刻（而非清单重建时刻）——entries 无变化时本文件不动。
type configManifestFile struct {
	UpdatedAt string                `json:"updatedAt"`
	Entries   []configManifestEntry `json:"entries"`
}

func configManifestPath() string { return kit.ResolveDataPath("config-manifest.json") }

var (
	manifestMu       sync.Mutex
	manifestLastBody string // 上次写盘的 entries 序列化体（判变化；空 = 尚未建过）
)

// collectConfigManifestEntries 收集全部配置态文件的指纹，按路径排序（稳定
// 输出）。读不了的文件跳过（可能正在写 tmp/rename 窗口内，下轮轮询重试）。
func collectConfigManifestEntries() []configManifestEntry {
	dir := kit.DataDir()
	var out []configManifestEntry
	add := func(rel, abs string) {
		data, err := os.ReadFile(abs)
		if err != nil {
			return
		}
		sum := sha256.Sum256(data)
		out = append(out, configManifestEntry{
			File:   rel,
			SHA256: hex.EncodeToString(sum[:]),
			Size:   int64(len(data)),
		})
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if name := e.Name(); strings.HasSuffix(name, "-config.json") {
				add(name, filepath.Join(dir, name))
			}
		}
	}
	add(filepath.Join("workbuddy", "config.json"), filepath.Join(dir, "workbuddy", "config.json"))
	if authEntries, err := os.ReadDir(filepath.Join(dir, "workbuddy", "auths")); err == nil {
		for _, e := range authEntries {
			if e.IsDir() {
				continue
			}
			if name := e.Name(); strings.HasSuffix(name, ".json") {
				add(filepath.Join("workbuddy", "auths", name), filepath.Join(dir, "workbuddy", "auths", name))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

// rebuildConfigManifest 重建清单并在配置态有变化时写盘。并发安全。
func rebuildConfigManifest() {
	entries := collectConfigManifestEntries()
	body, err := json.Marshal(entries)
	if err != nil {
		return
	}
	manifestMu.Lock()
	if string(body) == manifestLastBody {
		manifestMu.Unlock()
		return // 配置态无变化：连 updatedAt 都不更新，对 manifest 的 watch 零噪声
	}
	manifestLastBody = string(body)
	manifestMu.Unlock()

	out := configManifestFile{
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Entries:   entries,
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	atomicWriteFile(configManifestPath(), raw)
}

// startConfigManifestWatcher 启动清单重建循环（StartProxy 启动序列调用）。
// 首轮立即建一次（外部脚本从进程启动起就有完整清单可读）并留一行观测日志。
func startConfigManifestWatcher() {
	go func() {
		rebuildConfigManifest()
		if _, err := os.Stat(configManifestPath()); err == nil {
			log.Printf("config manifest: ready at %s", configManifestPath())
		}
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			rebuildConfigManifest()
		}
	}()
}

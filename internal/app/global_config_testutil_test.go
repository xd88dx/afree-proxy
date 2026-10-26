package app

import (
	"path/filepath"
	"testing"
)

// resetGlobalConfigForTest 把全局配置指向独立临时文件并清空内存缓存，
// 避免测试写进真实 data 目录;t.Cleanup 时还原。
func resetGlobalConfigForTest(t *testing.T) {
	t.Helper()
	globalCfgMu.Lock()
	oldPath, oldCfg, oldLoaded := globalConfigPathOverride, globalCfg, globalCfgLoaded
	globalConfigPathOverride = filepath.Join(t.TempDir(), "afree-config.json")
	globalCfg, globalCfgLoaded = nil, false
	globalCfgMu.Unlock()
	t.Cleanup(func() {
		globalCfgMu.Lock()
		globalConfigPathOverride, globalCfg, globalCfgLoaded = oldPath, oldCfg, oldLoaded
		globalCfgMu.Unlock()
	})
}

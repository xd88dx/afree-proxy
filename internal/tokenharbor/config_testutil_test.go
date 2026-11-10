package tokenharbor

import (
	"path/filepath"
	"testing"
	"time"
)

// resetTokenHarborConfigForTest 重置包级单例到"未加载"态并自带恢复。
//
// 必须重置的原因：Load 的 loaded 单飞对后续调用直接 return——上一个测试留在
// usage.Cool 的冷却（pool.go PickKey 会滤掉冷却 key）会渗进下一个测试，
// go test -shuffle=1 稳定复现。与 internal/amd/config_testutil_test.go 同纪律。
func resetTokenHarborConfigForTest(t *testing.T) {
	t.Helper()
	mu.Lock()
	oldCfg, oldLoaded := cfg, loaded
	cfg = DefaultConfig()
	loaded = false
	mu.Unlock()
	oldUsage := usage
	usage = &usageData{Cool: map[string]time.Time{}, KeyTokens: map[string]KeyTokenStats{}}
	keyMu.Lock()
	oldKeyIdx := keyIdx
	keyIdx = 0
	keyMu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		cfg, loaded = oldCfg, oldLoaded
		mu.Unlock()
		usage = oldUsage
		keyMu.Lock()
		keyIdx = oldKeyIdx
		keyMu.Unlock()
	})
	Load(filepath.Join(t.TempDir(), ".tkhb-config.json"))
}

// restoreCatalogForTest 保存当前模型目录并在测试结束后恢复。
//
// 各同步测试直接改写 catalogModels/catalogLoaded（真身无 setter），不恢复会把
// live 假目录带进后续测试；TestColdStartEmptyUntilSync 的 InitCatalog 还会起
// 后台 SyncCatalog goroutine 消费上一测试残留的配置——恢复纪律掐断这条渗漏链。
func restoreCatalogForTest(t *testing.T) {
	t.Helper()
	catalogMu.Lock()
	oldModels, oldLoaded := catalogModels, catalogLoaded
	catalogMu.Unlock()
	t.Cleanup(func() {
		catalogMu.Lock()
		catalogModels, catalogLoaded = oldModels, oldLoaded
		catalogMu.Unlock()
	})
}

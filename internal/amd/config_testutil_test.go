package amd

import (
	"path/filepath"
	"testing"
	"time"
)

// resetAmdConfigForTest 重置包级单例到"未加载"态并自带恢复。
//
// 必须重置的原因：Load 的 loaded 单飞对后续调用直接 return——上一个测试留在
// usage.Cool 的冷却（pool.go PickKey 会滤掉冷却 key）与 UNABLE_FORCE env 会
// 渗进下一个测试，go test -shuffle=1 稳定复现（TestPickKeySkips... 与
// TestPickKeyAllCoolingFollowsUnableForce 同跑时后者毒化前者，picked=""）。
//
// 只应在"测试要一个干净配置态"时调用；依赖前序测试状态的多测试组合不属于
// 本包的既有纪律（参照 zen_key_test.go 的 withZenModelForTest 恢复模式）。
func resetAmdConfigForTest(t *testing.T) {
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
	Load(filepath.Join(t.TempDir(), ".amd-config.json"))
}

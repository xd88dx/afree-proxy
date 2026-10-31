package trae

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateFromLegacyConfigWithRealShape 回归（2026-11-01 本机容器实测）：
// 真实旧 trae-config.json（含 per-account 痕迹键）首次升级时走
// migrateUsageFromConfig——彼时包级 usage.Accounts 若为 nil map 会
// "assignment to entry in nil map" panic 崩启动。e2e 造数没放 trae-config
// 才漏掉；本用例按真实数据形态锁死该路径。
func TestMigrateFromLegacyConfigWithRealShape(t *testing.T) {
	dir := t.TempDir()
	oldPath, oldUsagePath := ConfigPath, UsagePath
	t.Cleanup(func() { ConfigPath, UsagePath = oldPath, oldUsagePath })

	ConfigPath = filepath.Join(dir, "trae-config.json")
	UsagePath = filepath.Join(dir, "trae-state.json")

	// 真实形态：痕迹键嵌在 accounts 数组内（第一段迁移前的老格式）
	legacy := map[string]any{
		"gateway":        DefaultGateway,
		"maxConcurrency": 8,
		"retries":        3,
		"accounts": []any{map[string]any{
			"id":               "u1",
			"token":            "tok-legacy",
			"refreshToken":     "rt-legacy",
			"tokens":           map[string]any{"today": 7, "total": 70, "date": "2026-10-31", "lastUsedAt": "2026-10-31T08:00:00Z"},
			"credits":          map[string]any{"remaining": 12.5, "limit": 100, "identity": "ProPlus"},
			"checkin":          map[string]any{"checkedIn": true, "date": "2026-10-31", "rotations": 2},
			"checkinDeviceGen": 3,
		}},
		"cool": map[string]any{"u1": "2030-01-01T00:00:00Z"},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, raw, 0600); err != nil {
		t.Fatal(err)
	}

	// 模拟冷启动：痕迹真身归零（包级变量在其它测试可能已被写脏）
	mu.Lock()
	usage = &usageData{} // 刻意回到 nil-map 形态，锁死历史 panic 条件
	mu.Unlock()

	Load(ConfigPath) // 修复前：panic assignment to entry in nil map

	u := UsageFor("u1")
	if u.Tokens.Total != 70 || u.Credits == nil || u.Credits.Identity != "ProPlus" || u.Checkin == nil || u.CheckinDeviceGen != 3 {
		t.Fatalf("legacy traces not migrated: %+v", u)
	}
	if _, ok := CooldownUntil("u1"); !ok {
		t.Fatal("legacy cooldown not migrated")
	}
	// state 文件已落盘（迁移即持久化）
	if _, err := os.Stat(UsagePath); err != nil {
		t.Fatalf("usage file not written after migration: %v", err)
	}
}

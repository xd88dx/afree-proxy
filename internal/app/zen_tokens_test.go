package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestZenKeyTokensPersist zen key 记账必须随配置落盘（2026-10-30 修复）：
// 之前记账只活在包级 zenKeyTokens 里，重启后面板"今日/累计 token"整列清零。
// 三段覆盖：save 快照写盘 → set 回填运行时表（重启加载/导入还原同源）→
// 已移除 key 的记账条目被清理（避免文件里堆永不命中的累计值）。
func TestZenKeyTokensPersist(t *testing.T) {
	dir := t.TempDir()
	oldPath := zenConfigPathOverride
	oldTokens := zenKeyTokens
	zenConfigPathOverride = filepath.Join(dir, "zen-config.json")
	defer func() {
		zenConfigPathOverride = oldPath
		zenKeyMu.Lock()
		zenKeyTokens = oldTokens
		zenKeyMu.Unlock()
	}()

	// ① 记账 → 落盘：配置文件里必须出现 keyTokens 且带本次消耗
	zenKeyMu.Lock()
	zenKeyTokens = map[string]zenKeyTokenStats{}
	zenKeyMu.Unlock()
	recordZenKeyTokens("zk-test", 42)
	saveZenConfig()

	raw, err := os.ReadFile(zenConfigPathOverride)
	if err != nil {
		t.Fatalf("read zen config: %v", err)
	}
	var f struct {
		KeyTokens map[string]zenKeyTokenStats `json:"keyTokens"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse zen config: %v", err)
	}
	st, ok := f.KeyTokens["zk-test"]
	if !ok {
		t.Fatalf("keyTokens not persisted on disk: %s", raw)
	}
	if st.Total != 42 {
		t.Fatalf("persisted total=%d, want 42", st.Total)
	}

	// ② 配置 → 运行时回填：停表重启（或导入备份）后今日/累计要回到界面
	cur := getZenConfig()
	next := *cur
	next.Keys = []string{"zk-test"}
	next.KeyTokens = map[string]zenKeyTokenStats{"zk-test": {Today: 7, Total: 7}}
	setZenConfig(&next)
	zenKeyMu.Lock()
	got := zenKeyTokens["zk-test"]
	zenKeyMu.Unlock()
	if got.Total != 7 {
		t.Fatalf("runtime tokens after reload=%d, want 7", got.Total)
	}

	// ③ key 被移除后记账条目一并清理（Key 是单 key 兼容字段，normalize 会用它
	// 回填 Keys，两个都要清才是真的"没有这个 key 了"）
	next2 := *getZenConfig()
	next2.Keys = nil
	next2.Key = ""
	setZenConfig(&next2)
	zenKeyMu.Lock()
	left := len(zenKeyTokens)
	zenKeyMu.Unlock()
	if left != 0 {
		t.Fatalf("stale token entries left after key removal: %d", left)
	}
}

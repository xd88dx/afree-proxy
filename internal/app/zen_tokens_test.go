package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestZenKeyTokensPersist zen key 记账必须落盘（2026-10-30 修复的延续；
// 2026-10-31 拆分后持久化目标是独立的 zen-state.json，配置文件不再携带记账）。
// 三段覆盖：save 快照写盘 → load 回填运行时表（重启加载同源）→ 已移除 key
// 的记账条目被清理（避免文件里堆永不命中的累计值）。
func TestZenKeyTokensPersist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir) // setZenConfig 里的 reconcileEgressBindings 会写 zen-config.json，别污染真实数据目录
	oldOverride := *zenUsagePathOverride
	oldCfgPath := *zenConfigPathOverride
	oldTokens := zenKeyTokens
	*zenUsagePathOverride = filepath.Join(dir, "zen-state.json")
	*zenConfigPathOverride = filepath.Join(dir, "zen-config.json")
	defer func() {
		*zenUsagePathOverride = oldOverride
		// 解引用恢复真身的值（gateway 批 shuffle=1 实证）：整体重绑别名只会把
		// app 侧别名指回真身地址，真身的字符串值仍是本测试的已删 TempDir——
		// zen 包内后续所有 zen-config.json 写（学习/别名/egress）持续指向
		// 不存在的目录（CI 337 次 write 失败），污染后续 zen 域测试。
		*zenConfigPathOverride = oldCfgPath
		zenKeyMu.Lock()
		zenKeyTokens = oldTokens
		zenKeyMu.Unlock()
	}()

	// ① 记账 → 落盘：痕迹文件里必须出现 keyTokens 且带本次消耗
	zenKeyMu.Lock()
	*zenKeyTokens = map[string]zenKeyTokenStats{}
	zenKeyMu.Unlock()
	setZenConfig(&zenConfigData{Keys: []string{"zk-test"}}) // 先落一份配置文件
	recordZenKeyTokens("zk-test", 42)
	saveZenUsage()

	raw, err := os.ReadFile(*zenUsagePathOverride)
	if err != nil {
		t.Fatalf("read zen usage: %v", err)
	}
	var f struct {
		KeyTokens map[string]zenKeyTokenStats `json:"keyTokens"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse zen usage: %v", err)
	}
	st, ok := f.KeyTokens["zk-test"]
	if !ok {
		t.Fatalf("keyTokens not persisted on disk: %s", raw)
	}
	if st.Total != 42 {
		t.Fatalf("persisted total=%d, want 42", st.Total)
	}

	// 配置文件不再携带记账（外部监测口径：zen-config.json 无 keyTokens 键）
	cfgRaw, err := os.ReadFile(*zenConfigPathOverride)
	if err != nil {
		t.Fatalf("read zen config: %v", err)
	}
	if strings.Contains(string(cfgRaw), "keyTokens") {
		t.Fatalf("zen config must not carry usage traces: %s", cfgRaw)
	}

	// ② 重启加载：loadZenUsage 回填运行时表，今日/累计要回到界面
	zenKeyMu.Lock()
	*zenKeyTokens = map[string]zenKeyTokenStats{}
	zenKeyMu.Unlock()
	loadZenUsage()
	zenKeyMu.Lock()
	got := (*zenKeyTokens)["zk-test"]
	zenKeyMu.Unlock()
	if got.Total != 42 {
		t.Fatalf("runtime tokens after reload=%d, want 42", got.Total)
	}

	// ③ key 被移除后记账条目一并清理
	cur := getZenConfig()
	next := *cur
	next.Keys = nil
	setZenConfig(&next)
	zenKeyMu.Lock()
	left := len(*zenKeyTokens)
	zenKeyMu.Unlock()
	if left != 0 {
		t.Fatalf("stale token entries left after key removal: %d", left)
	}
}

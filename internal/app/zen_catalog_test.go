package app

import (
	"testing"
)

// 目录可达时价格门是唯一权威：免费集合原样采纳，不掺种子、不看名字后缀。
func TestZenDesiredFromLiveRegistryWins(t *testing.T) {
	free := map[string]bool{
		"big-pickle":                      true, // 无 free 后缀但 cost 0/0
		"mimo-v2.5-free":                  true,
		"muse-spark-1.3-contributor-free": true,
	}
	got, err := zenDesiredFromLive(true, free)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(free) {
		t.Fatalf("desired = %v, want exactly the registry free set %v", got, free)
	}
	for id := range free {
		if !got[id] {
			t.Errorf("registry-free model %q missing from desired", id)
		}
	}
	if got["gpt-5.5"] {
		t.Error("paid model leaked into the free catalog")
	}
}

// 目录不可达：报错（收割机删除后不再有 CLI 成员表兜底，调用方保留旧表）。
func TestZenDesiredFromLiveRegistryUnreachable(t *testing.T) {
	if _, err := zenDesiredFromLive(false, nil); err == nil {
		t.Fatal("expected error when the registry is unreachable (no CLI fallback after harvester removal)")
	}
}

// 两个来源都不可用：报错（调用方保留旧表），绝不能把空集当成"没有免费模型"。
func TestZenDesiredFromLiveNoSources(t *testing.T) {
	if _, err := zenDesiredFromLive(false, nil); err == nil {
		t.Fatal("expected error when registry is down")
	}
	if _, err := zenDesiredFromLive(true, map[string]bool{}); err == nil {
		t.Fatal("expected error when registry returned no free models")
	}
}

// live 列表是权威：不在其中的条目一律移除，而不是只升级来源。
func TestZenPruneRemovesStaleEntries(t *testing.T) {
	zenModelsMu.Lock()
	savedModels := zenModels
	zenModels = map[string]*ZenModel{
		"mimo-v2.5-free":         {ID: "mimo-v2.5-free", Source: "live"},
		"deepseek-v4-flash-free": {ID: "deepseek-v4-flash-free", Source: "live"}, // 已 deprecated
		"big-pickle":             {ID: "big-pickle", Source: "live"},
	}
	zenModelsMu.Unlock()
	defer func() {
		zenModelsMu.Lock()
		zenModels = savedModels
		zenModelsMu.Unlock()
	}()

	desired := map[string]bool{"mimo-v2.5-free": true, "big-pickle": true}
	pruneZenModelsTo(desired)

	zenModelsMu.RLock()
	defer zenModelsMu.RUnlock()
	if _, ok := zenModels["deepseek-v4-flash-free"]; ok {
		t.Error("deprecated entry survived a live sync (should be pruned)")
	}
	if _, ok := zenModels["mimo-v2.5-free"]; !ok {
		t.Error("live model was pruned")
	}
}

// 残表防御：live 结果明显偏小（接口抖动）时不得清空。
func TestZenPruneSkipsOnSuspiciouslySmallLiveList(t *testing.T) {
	zenModelsMu.Lock()
	savedModels := zenModels
	zenModels = map[string]*ZenModel{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		zenModels[id] = &ZenModel{ID: id, Source: "live"}
	}
	zenModelsMu.Unlock()
	defer func() {
		zenModelsMu.Lock()
		zenModels = savedModels
		zenModelsMu.Unlock()
	}()

	pruneZenModelsTo(map[string]bool{"a": true}) // 1 live vs 6 known

	zenModelsMu.RLock()
	defer zenModelsMu.RUnlock()
	if len(zenModels) != 6 {
		t.Fatalf("table size = %d, want 6 (small live list must not prune)", len(zenModels))
	}
}

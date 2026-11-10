package app

import (
	"os"
	"strings"
	"testing"
)

// FeedFree 标记与「仅展示插件免费」过滤（2026-11-11 用户定）：插件 UI 的 FREE
// 组是官方推荐位（recommended-models free 组）；catalog 零价发现不属于该组。

// 标记必须对全量缓存重算：模型掉出 feed 后仍可能经 catalog 零价路径留在
// 缓存，上一轮残留的 true 会让开关误展示。
func TestApplyFeedFreeMarkRecomputesForWholeCache(t *testing.T) {
	cache := map[string]*ModelInfo{
		"in-feed":      {ID: "in-feed", FeedFree: true},
		"dropped":      {ID: "dropped", FeedFree: true},  // 上一轮在 feed，本轮掉出
		"catalog-only": {ID: "catalog-only"},             // catalog 零价发现
		"residual":     {ID: "residual", FeedFree: true}, // 历史残留标记
	}
	applyFeedFreeMark(cache, map[string]bool{"in-feed": true})
	for id, want := range map[string]bool{
		"in-feed": true, "dropped": false, "catalog-only": false, "residual": false,
	} {
		if cache[id].FeedFree != want {
			t.Errorf("%s FeedFree=%v, want %v", id, cache[id].FeedFree, want)
		}
	}
}

func TestFeedFreeModelsFilter(t *testing.T) {
	in := []*ModelInfo{
		{ID: "a", FeedFree: true},
		{ID: "b"},
		nil,
		{ID: "c", FeedFree: true},
	}
	out := feedFreeModels(in)
	if len(out) != 2 || out[0].ID != "a" || out[1].ID != "c" {
		t.Fatalf("应只留 FeedFree 条目，得到 %v", out)
	}
}

// 开关状态落盘往返：默认关闭 → 开 → getter true 且 afree-config.json 含键 → 关。
func TestClineCatalogFeedOnlyPersisted(t *testing.T) {
	resetGlobalConfigForTest(t)
	if globalClineCatalogFeedOnly() {
		t.Fatal("默认应为关闭")
	}
	setGlobalClineCatalogFeedOnly(true)
	if !globalClineCatalogFeedOnly() {
		t.Fatal("设置后应为开启")
	}
	raw, err := os.ReadFile(globalConfigPath())
	if err != nil {
		t.Fatalf("read afree-config.json: %v", err)
	}
	if !strings.Contains(string(raw), `"clineCatalogFeedOnly": true`) {
		t.Fatalf("开关应落盘 afree-config.json: %s", raw)
	}
	setGlobalClineCatalogFeedOnly(false)
	if globalClineCatalogFeedOnly() {
		t.Fatal("关闭后 getter 应为 false")
	}
}

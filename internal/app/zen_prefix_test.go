package app

import "testing"

// zenPrefixFixture 预置一个 big-pickle 目录条目并保存/恢复包级表。
// 种子快照已取消（2026-11-11），冷启动表为空——前缀/裸 ID 解析语义改用
// 字面量造态验证。
func zenPrefixFixture(t *testing.T) {
	t.Helper()
	zenModelsMu.Lock()
	savedModels := *zenModels
	*zenModels = map[string]*ZenModel{"big-pickle": {ID: "big-pickle", Source: "live", Context: 200000, Output: 32000}}
	zenModelsMu.Unlock()
	t.Cleanup(func() {
		zenModelsMu.Lock()
		*zenModels = savedModels
		zenModelsMu.Unlock()
	})
}

// opcd: 前缀是附加能力：带前缀与裸 ID 都应解析到同一模型；旧 zen: 前缀
// （2026-11-28 改名前）仅在内部解析仍被接受（对外请求面由目录闸硬切）。
func TestZenPrefixResolvesSameModel(t *testing.T) {
	zenPrefixFixture(t)
	bare, ok := resolveZenModel("big-pickle")
	if !ok {
		t.Fatal("bare id must resolve (backwards compat)")
	}
	prefixed, ok := resolveZenModel("opcd:big-pickle")
	if !ok {
		t.Fatal("opcd: prefix must resolve")
	}
	if bare.ID != prefixed.ID {
		t.Fatalf("prefix and bare id must resolve to the same model: %q vs %q", bare.ID, prefixed.ID)
	}
	// 改名前旧前缀与遗留 opencode/ 前缀在内部解析同样兼容
	for _, legacy := range []string{"zen:big-pickle", "opencode/big-pickle"} {
		lm, ok := resolveZenModel(legacy)
		if !ok {
			t.Fatalf("legacy prefix %q must still resolve internally", legacy)
		}
		if lm.ID != bare.ID {
			t.Fatalf("legacy prefix %q resolved to a different model: %q", legacy, lm.ID)
		}
	}
}

// 免费判定同样覆盖前缀形态（面板 Test 传 opcd: 前缀也不能被拒）。
func TestZenFreeModelResolutionWithPrefix(t *testing.T) {
	// 夹具自足：本测试不依赖前序测试——单跑（或 -shuffle 后任意顺序）时
	// 包级表由 zenPrefixFixture 提供，resolve 不受启动顺序影响。
	zenPrefixFixture(t)
	m, ok := resolveZenFreeModel("opcd:big-pickle")
	if !ok || m == nil || m.ID != "big-pickle" {
		t.Fatalf("prefixed free model must resolve to the bare ID, got %v %v", m, ok)
	}
}

// 路由判定：前缀形态必须同样路由到 zen，而不是掉到 cline。
func TestRouteModelWithZenPrefix(t *testing.T) {
	zenPrefixFixture(t)
	if got := routeModel("opcd:big-pickle"); got != "zen" {
		t.Fatalf("prefixed zen model routed to %q", got)
	}
}

// /v1/models 与面板列表输出的 ID 必须带 opcd: 前缀（2026-11-28 由 zen:
// 改名）；裸 ID 写请求照常可用（内部解析宽松）。
func TestZenModelListIDsArePrefixed(t *testing.T) {
	// 展示门控要求至少一个参与路由的 key（空池不展示）。
	t.Setenv("DATA_DIR", testDataDir(t))
	prev := getZenConfig()
	t.Cleanup(func() {
		c := *prev
		setZenConfig(&c)
	})
	cfg := *getZenConfig()
	cfg.Keys = []string{"sk-test"}
	cfg.KeyRoutingEnabled = nil
	setZenConfig(&cfg)

	zenPrefixFixture(t)
	list := zenModelList()
	if len(list) == 0 {
		t.Fatal("model list empty")
	}
	for _, m := range list {
		id, _ := m["id"].(string)
		if len(id) == 0 || id[:5] != "opcd:" {
			t.Fatalf("model list id must carry the opcd: prefix, got %q", id)
		}
	}
}

// combo ID 与真实 zen 模型冲突的判定必须覆盖前缀形态。
func TestComboIDConflictWithPrefixedZenModel(t *testing.T) {
	zenPrefixFixture(t)
	if err := validateCombo("opcd:big-pickle", "cline", "big-pickle"); err == nil {
		t.Error("combo id must not collide with a real prefixed zen model")
	}
	if err := validateCombo("big-pickle-alias", "cline", "big-pickle"); err == nil {
		t.Error("combo id must not collide with a real bare zen model")
	}
}

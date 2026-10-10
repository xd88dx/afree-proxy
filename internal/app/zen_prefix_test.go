package app

import "testing"

// zen: 前缀是附加能力：带前缀、裸 ID、别名都应解析到同一模型。
func TestZenPrefixResolvesSameModel(t *testing.T) {
	initZenModels()
	bare, ok := resolveZenModel("big-pickle")
	if !ok {
		t.Fatal("bare id must resolve (backwards compat)")
	}
	prefixed, ok := resolveZenModel("zen:big-pickle")
	if !ok {
		t.Fatal("zen: prefix must resolve")
	}
	if bare.ID != prefixed.ID {
		t.Fatalf("prefix and bare id must resolve to the same model: %q vs %q", bare.ID, prefixed.ID)
	}
	// 遗留 opencode/ 前缀同样兼容
	legacy, ok := resolveZenModel("opencode/big-pickle")
	if !ok {
		t.Fatal("legacy opencode/ prefix must still resolve")
	}
	if legacy.ID != bare.ID {
		t.Fatalf("legacy prefix resolved to a different model: %q", legacy.ID)
	}
}

// 免费判定同样覆盖前缀形态（面板 Test 传 zen: 前缀也不能被拒）。
func TestZenFreeModelResolutionWithPrefix(t *testing.T) {
	m, ok := resolveZenFreeModel("zen:big-pickle")
	if !ok || m == nil || m.ID != "big-pickle" {
		t.Fatalf("prefixed free model must resolve to the bare ID, got %v %v", m, ok)
	}
}

// 路由判定：前缀形态必须同样路由到 zen，而不是掉到 cline。
func TestRouteModelWithZenPrefix(t *testing.T) {
	initZenModels()
	if got := routeModel("zen:big-pickle"); got != "zen" {
		t.Fatalf("prefixed zen model routed to %q", got)
	}
}

// /v1/models 与面板列表输出的 ID 必须带前缀；裸 ID 写请求照常可用。
func TestZenModelListIDsArePrefixed(t *testing.T) {
	initZenModels()
	list := zenModelList()
	if len(list) == 0 {
		t.Fatal("model list empty")
	}
	for _, m := range list {
		id, _ := m["id"].(string)
		if len(id) == 0 || id[:4] != "zen:" {
			t.Fatalf("model list id must carry the zen: prefix, got %q", id)
		}
	}
}

// combo ID 与真实 zen 模型冲突的判定必须覆盖前缀形态。
func TestComboIDConflictWithPrefixedZenModel(t *testing.T) {
	initZenModels()
	if err := validateCombo("zen:big-pickle", "cline", "big-pickle"); err == nil {
		t.Error("combo id must not collide with a real prefixed zen model")
	}
	if err := validateCombo("big-pickle-alias", "cline", "big-pickle"); err == nil {
		t.Error("combo id must not collide with a real bare zen model")
	}
}

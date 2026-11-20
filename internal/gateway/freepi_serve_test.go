package gateway

import (
	"testing"

	"afree-proxy/internal/freepi"
)

// freepiModelList 段内恒序 + 展示门控：无参与路由的 key 时整段摘除
// （「列出来的必须调得通」），有凭据时按 ID 升序恒序输出 freepi: 前缀 ID。
func TestFreepiModelListOrderedAndGated(t *testing.T) {
	savedCfg := freepi.Get()
	t.Cleanup(func() { freepi.Set(savedCfg) })

	// 无启用 key：整段摘除
	freepi.Set(&freepi.Config{Keys: []string{"a"}, KeyRoutingEnabled: map[string]bool{"a": false}})
	if got := freepiModelList(); len(got) != 0 {
		t.Fatalf("no enabled key must yield empty segment, got %d entries", len(got))
	}

	// 有启用 key：恒序升序 + 前缀拼接（目录含静态兜底，Fetched 覆盖同名条目）
	freepi.Set(&freepi.Config{Keys: []string{"a"}, KeyRoutingEnabled: map[string]bool{"a": true},
		Models: freepi.ModelState{Fetched: []freepi.Model{
			{ID: "zzz", Name: "Z"},
			{ID: "aaa", Name: "A"},
			{ID: "glm-5.3-flash", Name: "Live GLM"},
		}}})
	got := freepiModelList()
	if len(got) == 0 {
		t.Fatal("routable key must expose the catalog")
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, m["id"].(string))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("freepi segment not ordered: %q >= %q", ids[i-1], ids[i])
		}
	}
	if ids[0] != "freepi:aaa" {
		t.Fatalf("first id = %q, want freepi:aaa (prefix + sorted)", ids[0])
	}
	// Fetched 与静态表同名条目：Fetched 覆盖（目录是上游真值）
	found := false
	for _, m := range got {
		if m["id"] == "freepi:glm-5.3-flash" {
			found = true
		}
	}
	if !found {
		t.Fatal("merged catalog must contain glm-5.3-flash")
	}
}

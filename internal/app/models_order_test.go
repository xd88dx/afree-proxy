package app

import (
	"net/http"
	"testing"
)

// /v1/models 的「段内恒序」契约:各平台段内部一律确定有序。
//
// 背景:cline(getFreeModels 按**裸 feed ID** 升序,输出再统一加 cline-free/
// 前缀,故输出 id 观感上非升序但顺序确定)、zen(zenModelList 末尾 sort.Slice)、
// oprt/amd/tkhb/trae(各 ModelList 内 sort.Slice)、other(KnownModels 内
// sort.Slice)均已排序;唯独 WorkBuddy 段沿用上游返回序,是整条链路上唯一
// 不确定来源。本组测试锁死该口径,防止将来回退。

// 排序本身:乱序 → 升序;同 id 相对次序保持(稳定排序)。
func TestSortModelEntriesByID(t *testing.T) {
	list := []map[string]any{
		{"id": "wbcn:zeta", "tag": "first"},
		{"id": "wbgb:mid"},
		{"id": "wbcn:alpha"},
		{"id": "wbcn:zeta", "tag": "second"},
	}
	sortModelEntriesByID(list)

	var got []string
	for _, m := range list {
		got = append(got, m["id"].(string))
	}
	want := []string{"wbcn:alpha", "wbcn:zeta", "wbcn:zeta", "wbgb:mid"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted ids = %v, want %v", got, want)
		}
	}
	// 稳定:两条同 id 条目不得互换(tag 顺序是稳定性的观测点)。
	if list[1]["tag"] != "first" || list[2]["tag"] != "second" {
		t.Fatalf("unstable sort swapped equal ids: %v", list)
	}
}

// 缺 id / id 非字符串的条目不得 panic(按空串参与排序,排在队首)。
func TestSortModelEntriesByIDToleratesMissingID(t *testing.T) {
	list := []map[string]any{{"id": "b"}, {"no-id": true}, {"id": 42}}
	sortModelEntriesByID(list) // 不 panic 即可
	if len(list) != 3 {
		t.Fatalf("length changed: %d", len(list))
	}
}

// fakeWBServer 冒充 vendor 的 server.Handler:只回固定(乱序)模型列表。
type fakeWBServer struct{ models []map[string]any }

func (f *fakeWBServer) ServeHTTP(http.ResponseWriter, *http.Request) {}

func (f *fakeWBServer) ModelList() []map[string]any { return f.models }

// WorkBuddy 段端到端:vendor 按上游序吐出乱序列表 → 网关出口必须恒序,
// 且保留「对话模型在前、出图模型在后」的分组结构。
func TestWorkbuddyModelEntriesAreOrdered(t *testing.T) {
	saved := workbuddySub
	t.Cleanup(func() { workbuddySub = saved })
	workbuddySub = &workbuddySubsystem{server: &fakeWBServer{models: []map[string]any{
		{"id": "cn:zeta"},
		{"id": "global:mid"},
		{"id": "cn:alpha"},
		{"id": "cn:z-image", "image_generation": true},
		{"id": "cn:a-image", "image_generation": true},
	}}}

	got := workbuddyModelEntries()
	want := []string{"wbcn:alpha", "wbcn:zeta", "wbgb:mid", "wbcn:a-image", "wbcn:z-image"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if id, _ := got[i]["id"].(string); id != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
}

// 子系统未装配时 WorkBuddy 段为空(不 panic)。
func TestWorkbuddyModelEntriesNilSubsystem(t *testing.T) {
	saved := workbuddySub
	t.Cleanup(func() { workbuddySub = saved })
	workbuddySub = nil
	if got := workbuddyModelEntries(); len(got) != 0 {
		t.Fatalf("nil subsystem must yield empty segment, got %v", got)
	}
}

// 回归护栏:各「自带排序」平台的段内顺序确实恒升序。这里直接打各段函数,
// 不经过网络——只需目录已初始化(种子/空目录都算恒序)。
func TestPlatformSegmentsAreIDSorted(t *testing.T) {
	segs := map[string][]map[string]any{
		"openrouter":  orModelList(),
		"amd":         amdModelList(),
		"tokenharbor": thModelList(),
		"trae":        traeModelList(),
	}
	for name, seg := range segs {
		prev := ""
		for _, m := range seg {
			id, _ := m["id"].(string)
			if id < prev {
				t.Fatalf("%s segment out of order: %q after %q", name, id, prev)
			}
			prev = id
		}
	}
}

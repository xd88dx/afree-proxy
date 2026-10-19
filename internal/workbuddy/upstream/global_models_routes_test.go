package upstream

import (
	"errors"
	"testing"

	"afree-proxy/internal/workbuddy/auth"
)

// TestMergeV3RoutesPriority 主路字段权威：同 id 在三路都出现时，字段取自主路
// （桌面端 UA），后续路不得覆盖其窗口/effort/credits。
func TestMergeV3RoutesPriority(t *testing.T) {
	mk := func(id string, ctx, out int64, credits string, efforts []string) ModelInfo {
		return ModelInfo{ID: id, ContextWindow: ctx, MaxTokens: out, Credits: credits, Efforts: efforts}
	}
	desktop := probeResult{
		names: []string{"shared", "only-desktop"},
		infos: []ModelInfo{
			mk("shared", 1000000, 128000, "x1.33", []string{"low", "high"}),
			mk("only-desktop", 500000, 64000, "x0.10", []string{"medium"}),
		},
	}
	ide := probeResult{
		names: []string{"shared", "only-ide"},
		infos: []ModelInfo{
			// 同 id 但字段不同：不得覆盖主路。
			mk("shared", 176000, 24000, "x9.99", []string{"max"}),
			mk("only-ide", 200000, 32000, "x0.20", nil),
		},
	}
	cli := probeResult{
		names: []string{"shared", "only-cli"},
		infos: []ModelInfo{
			mk("shared", 272000, 72000, "x8.88", []string{"xhigh"}),
			mk("only-cli", 256000, 32000, "x0.30", nil),
		},
	}
	got := mergeV3Routes([]struct {
		label string
		res   probeResult
	}{
		{"desktop-UA", desktop},
		{"IDE-UA", ide},
		{"CLI-UA", cli},
	})
	if got.err != nil {
		t.Fatalf("unexpected err: %v", got.err)
	}
	wantIDs := []string{"shared", "only-desktop", "only-ide", "only-cli"}
	if len(got.names) != len(wantIDs) {
		t.Fatalf("names = %v, want %v", got.names, wantIDs)
	}
	for i, id := range wantIDs {
		if got.names[i] != id {
			t.Errorf("names[%d] = %q, want %q (full %v)", i, got.names[i], id, got.names)
		}
	}
	// 主路字段权威：shared 的窗口必须是主路值。
	var shared *ModelInfo
	for i := range got.infos {
		if got.infos[i].ID == "shared" {
			shared = &got.infos[i]
		}
	}
	if shared == nil {
		t.Fatal("shared model missing from infos")
	}
	if shared.ContextWindow != 1000000 || shared.MaxTokens != 128000 {
		t.Errorf("shared window = %d/%d, want 1000000/128000 (primary route must win)", shared.ContextWindow, shared.MaxTokens)
	}
	if shared.Credits != "x1.33" {
		t.Errorf("shared credits = %q, want x1.33", shared.Credits)
	}
	if len(shared.Efforts) != 2 || shared.Efforts[0] != "low" {
		t.Errorf("shared efforts = %v, want [low high]", shared.Efforts)
	}
}

// TestMergeV3RoutesPartialFailure 单路失败不拖垮整次探测：失败的路由被跳过，
// 成功路并集照常产出；全路失败才返回 err。
func TestMergeV3RoutesPartialFailure(t *testing.T) {
	mk := func(id string) ModelInfo { return ModelInfo{ID: id} }
	ok := probeResult{names: []string{"a"}, infos: []ModelInfo{mk("a")}}
	bad := probeResult{err: errors.New("v3/config status 500")}

	got := mergeV3Routes([]struct {
		label string
		res   probeResult
	}{
		{"desktop-UA", bad},
		{"IDE-UA", ok},
		{"CLI-UA", bad},
	})
	if got.err != nil {
		t.Fatalf("partial failure should not error, got %v", got.err)
	}
	if len(got.names) != 1 || got.names[0] != "a" {
		t.Errorf("names = %v, want [a]", got.names)
	}

	// 全路失败 → err（取首路错误，供调用方降级到企业端点）。
	all := mergeV3Routes([]struct {
		label string
		res   probeResult
	}{
		{"desktop-UA", bad},
		{"IDE-UA", bad},
		{"CLI-UA", bad},
	})
	if all.err == nil {
		t.Fatal("all routes failed: want err, got nil")
	}
}

// TestNonChatModelGenerationTags 生成类（图片/视频）模型不得进对话目录。
// 回归用例：nonChatModel 早期只拦 text-to-image，桌面端目录下的
// text-to-video / image-to-video（seedance 系列）会漏过并混进 /v1/models。
func TestNonChatModelGenerationTags(t *testing.T) {
	cases := []struct {
		name string
		id   string
		out  int64
		tags []string
		want bool
	}{
		{"图片生成（既有规则）", "hunyuan-image-alpha", 0, []string{"text-to-image"}, true},
		{"图生图", "gpt-image-2.5-sunburst", 0, []string{"text-to-image", "image-to-image"}, true},
		{"文生视频（本次修复）", "seedance-2.5", 0, []string{"text-to-video", "image-to-video"}, true},
		{"图生视频（本次修复）", "seedance-2.5-pro", 1024, []string{"image-to-video"}, true},
		{"普通对话模型", "gpt-6-sol", 128000, nil, false},
		{"新模型无 tags", "grok-4.7", 128000, []string{}, false},
		{"对话模型带无关 tag", "balanced-model", 32000, []string{"craft"}, false},
		{"tiny 输出仍剔除", "completion-1.0", 256, nil, true},
		{"nes 前缀仍剔除", "nes-1.2", 8192, nil, true},
	}
	for _, c := range cases {
		if got := nonChatModel(c.id, c.out, c.tags); got != c.want {
			t.Errorf("%s: nonChatModel(%q, %d, %v) = %v, want %v", c.name, c.id, c.out, c.tags, got, c.want)
		}
	}
}

// TestDesktopUAForGlobalAccount 桌面端 UA 按 realm 切平台段：global 账号必须是
// `WorkBuddy AI`（送错平台段会触发上游 403 code=11140 风控）。探测主路用这个 UA，
// 与 chat 路径同源——global 目录探测因此拿到客户端同款模型集。
func TestDesktopUAForGlobalAccount(t *testing.T) {
	c := New()
	global := &auth.Auth{UID: "u1", Domain: "www.workbuddy.ai"}
	global.BackfillRealm()
	ua := c.defaultWorkBuddyUAFor(global)
	if want := "WorkBuddy AI"; !contains(ua, want) {
		t.Errorf("global desktop UA = %q, want it to contain %q", ua, want)
	}
	if !contains(ua, "CLI/") {
		t.Errorf("global desktop UA = %q, want CLI segment", ua)
	}
	// CN 账号保持 WorkBuddy（零回归）。
	cn := &auth.Auth{UID: "u2", Domain: "copilot.tencent.com"}
	cn.BackfillRealm()
	if ua := c.defaultWorkBuddyUAFor(cn); contains(ua, "WorkBuddy AI") {
		t.Errorf("cn desktop UA = %q, must not contain 'WorkBuddy AI'", ua)
	}
}

// TestSortGlobalCatalogDeterministic 探测结果恒定化：无论输入顺序如何（各探测路
// 内部存在 Go map 迭代随机序），names/infos 排序后输出恒定——同集合同顺序，
// 下游 /v1/models（newapi 消费）不再观察到顺序翻转。infos 与 names 键一致、
// 各自按 id 有序；nil infos（窄表）保持 nil；字段值不被排序破坏。
func TestSortGlobalCatalogDeterministic(t *testing.T) {
	mk := func(id string, ctx int64) ModelInfo { return ModelInfo{ID: id, ContextWindow: ctx} }
	// 两种"随机"洗牌（模拟两次 map 迭代序），排序后必须得到同一输出。
	in1 := []ModelInfo{mk("grok-4.7", 3), mk("default-model", 1), mk("kimi-k3", 2), mk("gpt-6-sol", 4)}
	in2 := []ModelInfo{mk("kimi-k3", 2), mk("gpt-6-sol", 4), mk("grok-4.7", 3), mk("default-model", 1)}
	want := []string{"default-model", "gpt-6-sol", "grok-4.7", "kimi-k3"}

	n1, i1 := sortGlobalCatalog([]string{"grok-4.7", "default-model", "kimi-k3", "gpt-6-sol"}, in1)
	n2, i2 := sortGlobalCatalog([]string{"kimi-k3", "gpt-6-sol", "grok-4.7", "default-model"}, in2)
	for i, id := range want {
		if n1[i] != id || n2[i] != id {
			t.Errorf("names[%d] = (%q, %q), want %q both", i, n1[i], n2[i], id)
		}
		if i1[i].ID != id || i2[i].ID != id {
			t.Errorf("infos[%d] = (%q, %q), want %q both", i, i1[i].ID, i2[i].ID, id)
		}
	}
	// 字段值不被排序破坏：default-model 的窗口必须还是 1。
	if i1[0].ContextWindow != 1 || i2[0].ContextWindow != 1 {
		t.Errorf("default-model ctx = (%d, %d), want 1 both", i1[0].ContextWindow, i2[0].ContextWindow)
	}
	// 窄表形态：infos nil 保持 nil。
	n3, i3 := sortGlobalCatalog([]string{"b", "a"}, nil)
	if n3[0] != "a" || n3[1] != "b" {
		t.Errorf("narrow names = %v, want [a b]", n3)
	}
	if i3 != nil {
		t.Errorf("narrow infos = %v, want nil", i3)
	}
	// 空 infos / 单元素：不 panic、names 仍有序。
	n4, i4 := sortGlobalCatalog([]string{"z", "y"}, []ModelInfo{})
	if n4[0] != "y" || len(i4) != 0 {
		t.Errorf("empty-infos case: names=%v infos=%v", n4, i4)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

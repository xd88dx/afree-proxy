package app

import (
	"afree-proxy/internal/gateway"
	"afree-proxy/internal/gwconfig"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"afree-proxy/internal/cline"

	"afree-proxy/internal/other"
)

// 展示门控回归：模型目录 = 可服务目录。没有参与路由的凭据时，平台段不得

// 出现在 /v1/models（与请求入口的可路由性闸同判定），否则客户端选中后

// 必然 401/403/503。口径：账号池 = 总闸开 + 反代开关开；key 平台 = 启用

// 列开。

// otherModelList 逐子平台判定：没有启用 key 的子平台整段不输出。

func TestOtherModelListRequiresEnabledKey(t *testing.T) {

	t.Setenv("DATA_DIR", testDataDir(t))

	prev := other.Get()

	prevPath := other.ConfigPath

	other.ConfigPath = filepath.Join(t.TempDir(), ".other-config.json")

	t.Cleanup(func() {

		other.Set(prev)

		other.ConfigPath = prevPath

	})

	build := func(enabled bool) *other.Config {

		return &other.Config{Platforms: []other.Platform{{

			Slug: "test",

			Name: "Test",

			Prefix: "ot",

			BaseURL: "http://127.0.0.1:1/v1",

			Keys: []string{"k1"},

			KeyRoutingEnabled: map[string]bool{"k1": enabled},

			Models: other.ModelState{

				Manual: []string{"m1"},

				Selected: map[string]bool{"m1": true},
			},
		}}}

	}

	other.Set(build(false))

	if got := otherModelList(); len(got) != 0 {

		t.Fatalf("disabled key must hide subplatform: %v", got)

	}

	other.Set(build(true))

	got := otherModelList()

	if len(got) != 1 || got[0]["id"] != "ot:m1" {

		t.Fatalf("enabled key must list selected model: %v", got)

	}

}

// zen 需要至少一个参与路由的 key 才展示：空池在请求入口按不可路由处理，

// 目录继续展示只会让用户选中必 503 的模型。

func TestZenModelListRequiresEnabledKey(t *testing.T) {

	t.Setenv("DATA_DIR", testDataDir(t))

	prev := getZenConfig()

	t.Cleanup(func() {

		c := *prev

		setZenConfig(&c)

	})

	empty := *getZenConfig()

	empty.Keys = nil

	empty.KeyRoutingEnabled = nil

	setZenConfig(&empty)

	if got := zenModelList(); len(got) != 0 {

		t.Fatalf("empty zen pool must not list models: %d", len(got))

	}

	// 目录夹具（种子快照已取消，直接字面量造态）：门控语义 = 无启用 key

	// 不出目录，有启用 key 出目录。

	zenModelsMu.Lock()

	savedModels := *zenModels

	*zenModels = map[string]*ZenModel{"big-pickle": {ID: "big-pickle", Source: "live"}}

	zenModelsMu.Unlock()

	t.Cleanup(func() {

		zenModelsMu.Lock()

		*zenModels = savedModels

		zenModelsMu.Unlock()

	})

	named := *getZenConfig()

	named.Keys = []string{"sk-test"}

	named.KeyRoutingEnabled = nil // nil = 参与路由

	setZenConfig(&named)

	if got := zenModelList(); len(got) == 0 {

		t.Fatal("enabled named key must list zen models")

	}

}

// cline 裸模型名按账号池路由：无 active + 反代开的账号时不出目录。

func TestAPIModelListRequiresRoutableAccount(t *testing.T) {

	savedPool := cline.RawPool()

	t.Cleanup(func() { cline.SetPool(savedPool) })

	off, on := false, true

	cline.SetPool(&AccountPool{Accounts: []*Account{}})

	if got := apiModelList(); len(got) != 0 {

		t.Fatalf("no account must hide cline catalog: %d", len(got))

	}

	cline.SetPool(&AccountPool{Accounts: []*Account{{
		AccountID: "a1", PoolEnabled: &off,
	}}})

	if got := apiModelList(); len(got) != 0 {

		t.Fatalf("pool switch off must hide cline catalog: %d", len(got))

	}

	cline.SetPool(&AccountPool{Accounts: []*Account{{
		AccountID: "c3", PoolEnabled: &on,
	}}})
	// 目录内容只来自官方 feed（无 seed 兜底），注入条目验证"有可路由账号

	// 时目录可见"——账号门控与目录内容是两个独立维度。

	injectClineModels(t, "z-ai/glm-5.3-flash")

	if got := apiModelList(); len(got) == 0 {

		t.Fatal("routable account must list cline catalog")

	}

}

// combo 别名不带平台前缀：请求入口的可路由性闸必须按声明平台判，不能把

// 别名当裸名（Cline）——否则声明 zen 的别名会去看 Cline 的凭据。

func TestModelRoutePlatformKeyResolvesCombo(t *testing.T) {

	t.Setenv("DATA_DIR", testDataDir(t))

	gateway.SetCombosForTest([]*gwconfig.Combo{{ID: "my-alias", Platform: "zen", Target: "big-pickle"}}, true)

	prevList, prevLoaded := gateway.CombosSnapshotForTest()

	t.Cleanup(func() {

		gateway.SetCombosForTest(prevList, prevLoaded)

	})

	if got := modelRoutePlatformKey("my-alias"); got != platZen {

		t.Fatalf("combo alias must route to its declared platform: got %q", got)

	}

	if got := modelRoutePlatformKey("zen:big-pickle"); got != platZen {

		t.Fatalf("prefixed id must route to zen: got %q", got)

	}

	// 2026-11-28 前缀改名：opcd:/cline: 新形态必须显式命中（否则落冒号兜底
	// 分支判成 other/未知）；旧形态保留存量分类。
	if got := modelRoutePlatformKey("opcd:big-pickle"); got != platZen {

		t.Fatalf("opcd:-prefixed id must route to zen: got %q", got)

	}

	if got := modelRoutePlatformKey("cline:z-ai/glm-5.3-flash"); got != platCline {

		t.Fatalf("cline:-prefixed id must route to cline: got %q", got)

	}

	if got := modelRoutePlatformKey("cline-free/z-ai/glm-5.3-flash"); got != platCline {

		t.Fatalf("legacy cline-free/ id must still classify as cline: got %q", got)

	}

	if got := modelRoutePlatformKey("plain-model"); got != platCline {

		t.Fatalf("bare name must route to cline: got %q", got)

	}

}


// servedModelEntries 条目归一化（2026-11-28 用户定）：全平台统一基础 7 字段
// （id/object/created/status/cost/context/output），缺失置 nil 不给假信息；
// cline 的 context_length/max_output_tokens 映射到 context/output，feed 原生
// cline-free/ 前缀剥掉后统一补 cline:（无双前缀）；平台特有富字段不外透。
func TestServedModelEntriesNormalizedShape(t *testing.T) {

	t.Setenv("DATA_DIR", testDataDir(t))

	// zen 段夹具：一条带窗口、一条零值（未知）。
	zenModelsMu.Lock()

	savedModels := *zenModels

	*zenModels = map[string]*ZenModel{

		"big-pickle":    {ID: "big-pickle", Source: "live", Context: 200000, Output: 32000},

		"mystery-model": {ID: "mystery-model", Source: "live"},
	}

	zenModelsMu.Unlock()

	t.Cleanup(func() {

		zenModelsMu.Lock()

		*zenModels = savedModels

		zenModelsMu.Unlock()

	})

	cfg := *getZenConfig()

	cfg.Keys = []string{"sk-test"}

	cfg.KeyRoutingEnabled = nil

	setZenConfig(&cfg)

	t.Cleanup(func() { c := *getZenConfig(); setZenConfig(&c) })

	// cline 段夹具：feed 原生 cline-free/ 前缀条目 + 带元数据的裸条目。
	savedPool := cline.RawPool()

	t.Cleanup(func() { cline.SetPool(savedPool) })

	on := true

	cline.SetPool(&AccountPool{Accounts: []*Account{{AccountID: "shape-c1", PoolEnabled: &on}}})

	injectClineModels(t, "cline-free/deepseek-v4.1-flash", "z-ai/glm-5.3-flash")

	list := servedModelEntries()

	byID := map[string]map[string]any{}

	for _, m := range list {

		id, _ := m["id"].(string)

		byID[id] = m

	}

	baseKeys := []string{"id", "object", "created", "status", "cost", "context", "output"}

	zp := byID["opcd:big-pickle"]

	if zp == nil {

		t.Fatal("zen entry must be listed under the opcd: prefix")

	}

	if len(zp) != len(baseKeys) {

		t.Fatalf("zen entry keys = %v, want exactly the base 7", keysOf(zp))

	}

	if zp["context"] != 200000 || zp["output"] != 32000 {

		t.Fatalf("zen context/output = %v/%v, want 200000/32000", zp["context"], zp["output"])

	}

	if zp["cost"] != "free" || zp["status"] != "active" {

		t.Fatalf("zen cost/status = %v/%v, want free/active", zp["cost"], zp["status"])

	}

	zm := byID["opcd:mystery-model"]

	if zm == nil {

		t.Fatal("zero-value zen entry must still be listed")

	}

	if zm["context"] != nil || zm["output"] != nil {

		t.Fatalf("unknown window must be nil, got %v/%v", zm["context"], zm["output"])

	}

	cd := byID["cline:deepseek-v4.1-flash"]

	if cd == nil {

		t.Fatal("feed-native cline-free/* entry must be listed under cline: (stripped, no double prefix)")

	}

	if len(cd) != len(baseKeys) {

		t.Fatalf("cline entry keys = %v, want exactly the base 7", keysOf(cd))

	}

	if byID["cline:z-ai/glm-5.3-flash"] == nil {

		t.Fatal("bare feed id entry must be listed under cline:")

	}

	for id, m := range byID {

		for _, legacy := range []string{"cline-free/", "zen:"} {

			if strings.HasPrefix(id, legacy) {

				t.Errorf("served id %q must not carry the legacy %q prefix", id, legacy)

			}

		}

		if _, ok := m["owned_by"]; ok {

			t.Errorf("served id %q must not leak owned_by", id)

		}

	}

}

func keysOf(m map[string]any) []string {

	out := make([]string, 0, len(m))

	for k := range m {

		out = append(out, k)

	}

	sort.Strings(out)

	return out

}

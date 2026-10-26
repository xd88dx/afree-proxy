package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/tokenharbor"
	"afree-proxy/internal/trae"
	wbauth "afree-proxy/internal/workbuddy/auth"
	wbpool "afree-proxy/internal/workbuddy/pool"
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

// routableWBSub 构造一个能过可路由性闸的假子系统：server 吐模型列表，
// pool 里放一个健康账号（AvailableUIDs 非空）。
func routableWBSub(models []map[string]any) *workbuddySubsystem {
	p := wbpool.New("")
	p.Add(&wbauth.Auth{UID: "cn1", Domain: "www.codebuddy.cn"})
	return &workbuddySubsystem{server: &fakeWBServer{models: models}, pool: p}
}

// WorkBuddy 段端到端:vendor 按上游序吐出乱序列表 → 网关出口必须恒序,
// 且保留「对话模型在前、出图模型在后」的分组结构。
func TestWorkbuddyModelEntriesAreOrdered(t *testing.T) {
	saved := workbuddySub
	t.Cleanup(func() { workbuddySub = saved })
	workbuddySub = routableWBSub([]map[string]any{
		{"id": "cn:zeta"},
		{"id": "global:mid"},
		{"id": "cn:alpha"},
		{"id": "cn:z-image", "image_generation": true},
		{"id": "cn:a-image", "image_generation": true},
	})

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

// 展示门控:子系统在但池里没有可用账号(AvailableUIDs 空,含出图缓存)时,
// 整段不出目录——与请求入口闸同口径,别让客户端选中必失败条目。
func TestWorkbuddyModelEntriesGatedByRoutablePool(t *testing.T) {
	saved := workbuddySub
	t.Cleanup(func() { workbuddySub = saved })
	workbuddySub = &workbuddySubsystem{server: &fakeWBServer{models: []map[string]any{
		{"id": "cn:alpha"},
		{"id": "cn:z-image", "image_generation": true},
	}}}
	if got := workbuddyModelEntries(); len(got) != 0 {
		t.Fatalf("empty pool must gate the whole segment, got %v", got)
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
// 不经过网络——目录由各包种子快照填充。展示门控（4e7d263）之后各段
// ModelList 无启用凭据即返回空，断言会空转，所以先给四个平台各装一个
// 可路由凭据。
func TestPlatformSegmentsAreIDSorted(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	// or/amd/th：装一个启用 key；trae：装一个可路由账号。各自保存/恢复原配置。
	orPrev, orPath := openrouter.Get(), openrouter.ConfigPath
	openrouter.ConfigPath = filepath.Join(t.TempDir(), "or-config.json")
	orc := openrouter.DefaultConfig()
	orc.Keys = []string{"sk-or-v1-test"}
	orc.KeyRoutingEnabled = map[string]bool{"sk-or-v1-test": true}
	openrouter.Set(orc)
	amdPrev, amdPath := amd.Get(), amd.ConfigPath
	amd.ConfigPath = filepath.Join(t.TempDir(), "amd-config.json")
	amdc := amd.DefaultConfig()
	amdc.Keys = []string{"rc-test"}
	amdc.KeyRoutingEnabled = map[string]bool{"rc-test": true}
	amd.Set(amdc)
	thPrev, thPath := tokenharbor.Get(), tokenharbor.ConfigPath
	tokenharbor.ConfigPath = filepath.Join(t.TempDir(), "th-config.json")
	thc := tokenharbor.DefaultConfig()
	thc.Keys = []string{"thk_live_test"}
	thc.KeyRoutingEnabled = map[string]bool{"thk_live_test": true}
	tokenharbor.Set(thc)

	// trae：种子条目不带 Native（按设计只服务解析兜底、不进展示列表），
	// 所以用注入缝跑一遍目录同步造两条 Native 条目（t-b 故意放在 t-a 前面）。
	traeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/remote/v1/models":
			io.WriteString(w, `{"data":{"list":[{"function":"solo_work_remote","models":[{"name":"t-w"}]}]}}`)
		case "/api/ide/v1/get_detail_param":
			io.WriteString(w, `{"config_info_list":[
				{"config_name":"t-b","model_detail_list":[{"model_name":"t-b__dev"}]},
				{"config_name":"t-a","model_detail_list":[{"model_name":"t-a__dev"}]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer traeSrv.Close()
	traePrev, traeCatalogClient, traeCatalogURL := trae.Get(), trae.CatalogClient, trae.CatalogURLOverride
	trae.CatalogClient = traeSrv.Client()
	trae.CatalogURLOverride = traeSrv.URL + "/api/remote/v1/models"
	trae.Set(&trae.Config{Gateway: traeSrv.URL, Accounts: []*trae.Account{{ID: "u1", Token: "tok"}}})
	if _, err := trae.SyncCatalog(); err != nil {
		t.Fatalf("trae SyncCatalog: %v", err)
	}
	t.Cleanup(func() {
		openrouter.Set(orPrev)
		openrouter.ConfigPath = orPath
		amd.Set(amdPrev)
		amd.ConfigPath = amdPath
		tokenharbor.Set(thPrev)
		tokenharbor.ConfigPath = thPath
		trae.Set(traePrev)
		trae.CatalogClient = traeCatalogClient
		trae.CatalogURLOverride = traeCatalogURL
	})

	segs := map[string][]map[string]any{
		"openrouter":  orModelList(),
		"amd":         amdModelList(),
		"tokenharbor": thModelList(),
		"trae":        traeModelList(),
	}
	for name, seg := range segs {
		if len(seg) == 0 {
			t.Fatalf("%s segment is empty despite a routable credential — catalog seed missing or gating drifted", name)
		}
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

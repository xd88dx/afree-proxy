package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/tokenharbor"
	wbauth "afree-proxy/internal/workbuddy/auth"
	wbconfig "afree-proxy/internal/workbuddy/config"
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
// 出图模型(image_generation 条目)走费率同源筛选——wkbd 页列表按倍率阈值
// 过滤且缺失不透出,对外目录与请求闸同源(2026-11-03 用户定:只对外路由各
// 平台页列表可见的模型;同日追定剔除按费率同源,不按模态一刀切)。
// 无 credits 牌价的出图条目在缺省阈值 0.2 下全部挡住(见下组测试)。
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
	want := []string{"wbcn:alpha", "wbcn:zeta", "wbgb:mid"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v (image entries must be excluded)", got, want)
	}
	for i := range want {
		if id, _ := got[i]["id"].(string); id != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
	for _, m := range got {
		if isImage, _ := m["image_generation"].(bool); isImage {
			t.Fatalf("image_generation entry leaked into served directory: %v", m)
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

// 服务闸同源回归（2026-11-03）：无牌价出图模型从目录剔除后，请求路径点名必须
// 同样拒——modelIsServed/servedModelGate 与 /v1/models 共用 servedModelEntries，
// 不存在「列表看不见却点得通」的绕过口。

// 费率同源筛选（2026-11-03 用户定）：出图条目不再按模态一刀切，改用与面板
// 前端 wbRate/wbModelMatchesFilter 同构的倍率判据——promo 生效需
// promo_factor + promo_credits 同时空场且后者可解析，否则取 credits；
// 缺失/不可解析一律隐藏（缺失 ≠ 免费），≤ 阈值（缺省 0.2，1e-9 容差）放行。
// 将来上游给某个出图模型发牌价且 ≤ 阈值时自动进目录，无需改代码。
// 2026-11-07 分域：CN 阈值管 wbcn、Global 阈值管 wbgb，两域互不越界。
func TestWorkbuddyImageModelRateFilter(t *testing.T) {
	throttleGateRefreshForTest() // servedModelGate 未知名不做 live 目录同步
	saved := workbuddySub
	t.Cleanup(func() { workbuddySub = saved })
	sub := routableWBSub([]map[string]any{
		{"id": "cn:alpha"},
		{"id": "cn:img-free", "image_generation": true, "credits": "x0.1"},
		{"id": "cn:img-pricey", "image_generation": true, "credits": "x5"},
		{"id": "cn:img-norate", "image_generation": true},
		{"id": "cn:img-promo", "image_generation": true, "credits": "x5", "promo_factor": "0.5", "promo_credits": "x0.15"},
		{"id": "cn:img-promo-bad", "image_generation": true, "promo_factor": "0.5", "promo_credits": ""},
		{"id": "cn:img-garbage", "image_generation": true, "credits": "abc"},
		// global 域：gbimg-mid 的 0.15 在 CN 阈值 0.2 下可过、在国际阈值 0.1 下
		// 必须被滤——证明国际段没有蹭国内阈值。
		{"id": "global:gbimg-free", "image_generation": true, "credits": "x0.1"},
		{"id": "global:gbimg-mid", "image_generation": true, "credits": "x0.15"},
	})
	sub.cfg = &wbconfig.Config{}
	sub.cfg.Pool.ModelRateFilterCN = 0.2
	sub.cfg.Pool.ModelRateFilterGlobal = 0.1
	workbuddySub = sub

	got := workbuddyModelEntries()
	want := []string{"wbcn:alpha", "wbcn:img-free", "wbcn:img-promo", "wbgb:gbimg-free"}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if id, _ := got[i]["id"].(string); id != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
	// 请求闸同源：放行条目必须可点名，剔除条目必须拒。
	if !modelIsServed("wbcn:img-free") || !modelIsServed("wbcn:img-promo") {
		t.Fatal("rate-passing image models must be servable")
	}
	if !modelIsServed("wbgb:gbimg-free") {
		t.Fatal("global rate-passing image model must be servable")
	}
	for _, id := range []string{"wbcn:img-pricey", "wbcn:img-norate", "wbcn:img-promo-bad", "wbcn:img-garbage", "wbgb:gbimg-mid"} {
		if modelIsServed(id) {
			t.Fatalf("%s must not be servable", id)
		}
	}
}
func TestServedGateRejectsWorkbuddyImageModel(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	throttleGateRefreshForTest() // servedModelGate 未知名不做 live 目录同步
	saved := workbuddySub
	t.Cleanup(func() { workbuddySub = saved })
	workbuddySub = routableWBSub([]map[string]any{
		{"id": "cn:alpha"},
		{"id": "cn:hunyuan-image-alpha", "image_generation": true},
	})
	if !modelIsServed("wbcn:alpha") {
		t.Fatal("workbuddy chat model must stay servable")
	}
	if modelIsServed("wbcn:hunyuan-image-alpha") {
		t.Fatal("workbuddy image model must not be servable (not in wkbd page list)")
	}
}

// 回归护栏:各「自带排序」平台的段内顺序确实恒升序。这里直接打各段函数,
// 不经过网络——种子快照已取消（2026-11-11，or/amd/th 同日口径），目录改用
// httptest 假上游注入：各包目录真身无导出 setter，但 SyncCatalog 是导出入口，
// 指到本地假服务器即可造态。展示门控（4e7d263）之后各段 ModelList 无启用
// 凭据即返回空，断言会空转，所以先给三个平台各装一个可路由凭据。
func TestPlatformSegmentsAreIDSorted(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	// or/amd/th：装一个启用 key + 一个假目录上游。各自保存/恢复原配置。
	orPrev, orPath := openrouter.Get(), openrouter.ConfigPath
	openrouter.ConfigPath = filepath.Join(t.TempDir(), "or-config.json")
	orc := openrouter.DefaultConfig()
	orc.Keys = []string{"sk-or-v1-test"}
	orc.KeyRoutingEnabled = map[string]bool{"sk-or-v1-test": true}
	openrouter.Set(orc)
	orSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 乱序喂入，验证 ModelList 的 sort.Slice 输出恒升序。
		_, _ = w.Write([]byte(`{"data":[
			{"id":"or-b:free","name":"B","context_length":1000,"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools"],"architecture":{"output_modalities":["text"]}},
			{"id":"or-a:free","name":"A","context_length":2000,"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools"],"architecture":{"output_modalities":["text"]}}
		]}`))
	}))
	defer orSrv.Close()
	orPrevOverride := openrouter.GetCatalogURLOverride()
	openrouter.SetCatalogURLOverride(orSrv.URL)
	amdPrev, amdPath := amd.Get(), amd.ConfigPath
	amd.ConfigPath = filepath.Join(t.TempDir(), "amd-config.json")
	amdc := amd.DefaultConfig()
	amdc.Keys = []string{"rc-test"}
	amdc.KeyRoutingEnabled = map[string]bool{"rc-test": true}
	amdc.BaseURL = amdSrv(t).URL
	amd.Set(amdc)
	thPrev, thPath := tokenharbor.Get(), tokenharbor.ConfigPath
	tokenharbor.ConfigPath = filepath.Join(t.TempDir(), "th-config.json")
	thc := tokenharbor.DefaultConfig()
	thc.Keys = []string{"thk_live_test"}
	thc.KeyRoutingEnabled = map[string]bool{"thk_live_test": true}
	thc.BaseURL = thSrv(t).URL
	tokenharbor.Set(thc)
	t.Cleanup(func() {
		openrouter.Set(orPrev)
		openrouter.ConfigPath = orPath
		openrouter.SetCatalogURLOverride(orPrevOverride)
		amd.Set(amdPrev)
		amd.ConfigPath = amdPath
		tokenharbor.Set(thPrev)
		tokenharbor.ConfigPath = thPath
	})

	// 显式首拉（种子时代靠快照即时有目录；现在必须同步一次才有目录）。
	// 先 InitCatalog 把 loaded 置位（否则 ModelList 内部的 InitCatalog 会把
	// 已同步的目录清空再异步首拉，竞态窗口里读到空段）。
	openrouter.InitCatalog()
	amd.InitCatalog()
	tokenharbor.InitCatalog()
	if _, err := openrouter.SyncCatalog(); err != nil {
		t.Fatalf("openrouter fake catalog sync: %v", err)
	}
	if _, err := amd.SyncCatalog(); err != nil {
		t.Fatalf("amd fake catalog sync: %v", err)
	}
	if _, err := tokenharbor.SyncCatalog(); err != nil {
		t.Fatalf("tokenharbor fake catalog sync: %v", err)
	}

	segs := map[string][]map[string]any{
		"openrouter":  orModelList(),
		"amd":         amdModelList(),
		"tokenharbor": thModelList(),
	}
	for name, seg := range segs {
		if len(seg) == 0 {
			t.Fatalf("%s segment is empty despite a routable credential and a synced fake catalog — gating drifted", name)
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

// amdSrv 假 AMDCloud 目录：乱序两条（output 留空=chat 可对话）。
func amdSrv(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"DeepSeek-V4-Flash","name":"DeepSeek-V4-Flash","context_length":1048576,"providers":[{"tools":true}]},
			{"id":"A/first","name":"A/first","context_length":4096}
		]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// thSrv 假 TokenHarbor 目录：乱序两条 :free（category=free 参数直接忽略）。
func thSrv(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"mimo-v2.6-flash:free","context_length":262144},
			{"id":"a-first:free","context_length":4096}
		]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

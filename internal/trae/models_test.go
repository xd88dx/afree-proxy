package trae

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// resetCatalog 把全局目录换成空表并在测试后还原（含目录客户端注入点）。
func resetCatalog(t *testing.T) {
	t.Helper()
	catalogMu.Lock()
	prevModels, prevLoaded := catalogModels, catalogLoaded
	catalogModels = map[string]Model{}
	catalogLoaded = true
	catalogMu.Unlock()

	prevClient, prevURL := CatalogClient, CatalogURLOverride
	t.Cleanup(func() {
		catalogMu.Lock()
		catalogModels, catalogLoaded = prevModels, prevLoaded
		catalogMu.Unlock()
		CatalogClient, CatalogURLOverride = prevClient, prevURL
	})
}

func catalogSnapshot() map[string]Model {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	out := make(map[string]Model, len(catalogModels))
	for id, m := range catalogModels {
		out[id] = m
	}
	return out
}

// applyCatalog：live 没给 raw 名时沿用已验证映射，函数缺省 chat_v3。
func TestApplyCatalogKeepsVerifiedRawName(t *testing.T) {
	resetCatalog(t)
	catalogModels["glm-5.1"] = Model{ID: "glm-5.1", RawName: "glm-5__v2", Function: "chat_v3"}

	changed := applyCatalog(map[string]Model{
		"glm-5.1":   {ID: "glm-5.1"},   // 无 raw → 保留 __v2
		"new-model": {ID: "new-model"}, // 无历史 → raw = ID
		"solo":      {ID: "solo", RawName: "solo__dev", Function: "solo_agent"},
	})
	if changed != 2 {
		t.Fatalf("changed = %d, want 2 (glm-5.1 unchanged)", changed)
	}
	got := catalogSnapshot()
	if got["glm-5.1"].RawName != "glm-5__v2" || got["glm-5.1"].Function != "chat_v3" {
		t.Fatalf("verified mapping lost: %+v", got["glm-5.1"])
	}
	if got["new-model"].RawName != "new-model" || got["new-model"].Function != "chat_v3" {
		t.Fatalf("new entry = %+v", got["new-model"])
	}
	if got["solo"].Function != "solo_agent" || got["solo"].RawName != "solo__dev" {
		t.Fatalf("solo entry = %+v", got["solo"])
	}
	// 幂等：同一批再合并不应再记变更
	if again := applyCatalog(map[string]Model{"glm-5.1": {ID: "glm-5.1"}}); again != 0 {
		t.Fatalf("second apply changed = %d", again)
	}
}

// get_detail_param 目录：函数档写入每个条目（上游不回显 raw_chat_function），
// __dev 优先于 __max，非 chat_completion/custom_models/隐藏项过滤。
func TestFetchSoloModelConfigsFunctionScope(t *testing.T) {
	var gotFunction, gotUID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ide/v1/get_detail_param" {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		gotUID = r.Header.Get("X-Uid")
		var body struct {
			Function string `json:"function"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotFunction = body.Function
		io.WriteString(w, `{"config_info_list":[
			{"config_name":"glm-5.2","display_name":"GLM-5.2",
			 "model_detail_list":[{"model_name":"glm-5.2__max"},{"model_name":"glm-5.2__dev"}],
			 "display_config":{"context_window_tokens":200000}},
			{"config_name":"x","model_name":"x__dev","display_name":"X"},
			{"config_name":"custom_model_1M_text","usage":"custom_model_1M_text"},
			{"config_name":"computer_use_subagent"},
			{"config_name":"with-custom-models","custom_models":[{"name":"m"}]}
		]}`)
	}))
	defer srv.Close()

	CatalogClient = srv.Client()
	got, err := fetchSoloModelConfigs(srv.URL, "tok", "uid-1", "solo_agent")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotFunction != "solo_agent" {
		t.Fatalf("request function = %q", gotFunction)
	}
	if gotUID != "uid-1" {
		t.Fatalf("X-Uid = %q", gotUID)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d (%v)", len(got), got)
	}
	glm := got["glm-5.2"]
	if glm.RawName != "glm-5.2__dev" {
		t.Fatalf("__dev not preferred: %+v", glm)
	}
	if glm.Function != "solo_agent" {
		t.Fatalf("function not recorded: %+v", glm)
	}
	if glm.Context != 200000 {
		t.Fatalf("context = %d", glm.Context)
	}
	if got["x"].Function != "solo_agent" || got["x"].RawName != "x__dev" {
		t.Fatalf("model_name entry = %+v", got["x"])
	}
}

// data.config_info_list 包裹形态兼容（上游不同版本两种形态都出现过）。
func TestFetchSoloModelConfigsDataEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"config_info_list":[{"config_name":"m","model_detail_list":[{"model_name":"m__dev"}]}]}}`)
	}))
	defer srv.Close()
	CatalogClient = srv.Client()
	got, err := fetchSoloModelConfigs(srv.URL, "tok", "uid", "chat_v3")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got["m"].RawName != "m__dev" || got["m"].Function != "chat_v3" {
		t.Fatalf("envelope entry = %+v", got["m"])
	}
}

// 目录两档合并：chat_v3 与 solo_agent 都拉，同 ID 以 solo_agent 为准
// （solo_agent 准入更宽），只在一档出现的条目保留自己的函数。
func TestSyncCatalogMergesNativeScopes(t *testing.T) {
	resetCatalog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/remote/v1/models":
			io.WriteString(w, `{"data":{"list":[{"function":"solo_work_remote","models":[
				{"name":"glm-5.2","display_name":"GLM-5.2"},
				{"name":"web-only","display_name":"Web Only"}
			]}]}}`)
		case "/api/ide/v1/get_detail_param":
			var body struct {
				Function string `json:"function"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body.Function {
			case "chat_v3":
				io.WriteString(w, `{"config_info_list":[
					{"config_name":"glm-5.2","model_detail_list":[{"model_name":"glm-5.2__ide"}]},
					{"config_name":"ide-only","model_detail_list":[{"model_name":"ide-only__dev"}]}
				]}`)
			case "solo_agent":
				io.WriteString(w, `{"config_info_list":[
					{"config_name":"glm-5.2","model_detail_list":[{"model_name":"glm-5.2__solo"}]},
					{"config_name":"solo-only","model_detail_list":[{"model_name":"solo-only__dev"}]}
				]}`)
			default:
				t.Errorf("unexpected catalog function %q", body.Function)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// 挂到测试服务器上：web 目录走 URL 覆盖，原生目录走账号网关。
	prev := Get()
	t.Cleanup(func() { Set(prev) })
	Set(&Config{Gateway: srv.URL, Accounts: []*Account{{ID: "u1", Token: "tok"}}})
	CatalogClient = srv.Client()
	CatalogURLOverride = srv.URL + "/api/remote/v1/models"

	changed, err := SyncCatalog()
	if err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}
	if changed != 4 {
		t.Fatalf("changed = %d, want 4", changed)
	}
	got := catalogSnapshot()
	if m := got["glm-5.2"]; m.Function != "solo_agent" || m.RawName != "glm-5.2__solo" {
		t.Fatalf("solo_agent scope must win: %+v", m)
	}
	if m := got["ide-only"]; m.Function != "chat_v3" || m.RawName != "ide-only__dev" {
		t.Fatalf("ide-only entry = %+v", m)
	}
	if m := got["solo-only"]; m.Function != "solo_agent" {
		t.Fatalf("solo-only entry = %+v", m)
	}
	if m := got["web-only"]; m.RawName != "web-only" || m.Function != "chat_v3" || m.Native {
		t.Fatalf("web-only entry = %+v", m)
	}
	if m := got["glm-5.2"]; !m.Native {
		t.Fatalf("native flag not set: %+v", m)
	}
	// 展示层只列原生档确认过的条目；web-only 仍可被显式解析（返回上游真实错误）
	list := ModelList()
	if len(list) != 3 {
		t.Fatalf("ModelList = %d entries (%v), want 3", len(list), list)
	}
	for _, m := range list {
		if m.ID == "web-only" {
			t.Fatal("web-only entry must not be listed")
		}
	}
	if _, ok := Resolve("web-only"); !ok {
		t.Fatal("hidden entry must stay resolvable")
	}
}

// web 清单同名条目不得把原生档已确认的映射降级（Function 会被刷成 chat_v3，
// 多数模型在 chat_v3 下 4001），只允许补显示名与上下文。
func TestApplyCatalogNativeEntryNotDowngradedByWeb(t *testing.T) {
	resetCatalog(t)
	applyCatalog(map[string]Model{
		"m": {ID: "m", Name: "M", RawName: "m__dev", Function: "solo_agent", Context: 200000, Native: true},
	})
	changed := applyCatalog(map[string]Model{
		"m": {ID: "m", Function: "chat_v3", Context: 100000},
	})
	if changed != 0 {
		t.Fatalf("web merge changed = %d, want 0", changed)
	}
	got := catalogSnapshot()["m"]
	if got.RawName != "m__dev" || got.Function != "solo_agent" || !got.Native || got.Context != 200000 {
		t.Fatalf("native entry downgraded: %+v", got)
	}
}

// 展示门控：种子（未确认）条目不列，原生条目列；ProbeModel 在列表为空时
// 退回完整目录，保证刚启动、原生档尚未同步时账号健康检查仍有模型可用。
func TestModelListHidesUnconfirmedEntries(t *testing.T) {
	resetCatalog(t)
	prev := Get()
	t.Cleanup(func() { Set(prev) })
	Set(&Config{Accounts: []*Account{{ID: "u1", Token: "tok"}}})

	applyCatalog(map[string]Model{
		"native":    {ID: "native", RawName: "native__dev", Function: "solo_agent", Native: true},
		"seed-only": {ID: "seed-only", RawName: "seed__v2", Function: "chat_v3"},
	})
	list := ModelList()
	if len(list) != 1 || list[0].ID != "native" {
		t.Fatalf("ModelList = %v, want [native]", list)
	}
	if _, ok := Resolve("seed-only"); !ok {
		t.Fatal("hidden entry must stay resolvable")
	}
	if m, ok := ProbeModel(); !ok || m.ID != "native" {
		t.Fatalf("ProbeModel = %+v, %v", m, ok)
	}

	// 原生目录一条都没有时（刚启动）：展示为空，探测回退到完整目录。
	resetCatalog(t)
	applyCatalog(map[string]Model{"seed-only": {ID: "seed-only", RawName: "seed__v2", Function: "chat_v3"}})
	if l := ModelList(); len(l) != 0 {
		t.Fatalf("ModelList without native entries = %v", l)
	}
	if m, ok := ProbeModel(); !ok || m.ID != "seed-only" {
		t.Fatalf("ProbeModel fallback = %+v, %v", m, ok)
	}
}

// 目录自动跟随上游：本轮原生档全部成功、某个旧原生条目已不在清单里 →
// 降级为未确认（展示层摘除），但仍可解析（显式请求给上游真实错误）。
func TestSyncCatalogPrunesNativeWhenCatalogComplete(t *testing.T) {
	resetCatalog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/remote/v1/models":
			io.WriteString(w, `{"data":{"list":[{"function":"solo_work_remote","models":[
				{"name":"keep"},{"name":"gone"}]}]}}`)
		case "/api/ide/v1/get_detail_param":
			io.WriteString(w, `{"config_info_list":[{"config_name":"keep","model_detail_list":[{"model_name":"keep__dev"}]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prev := Get()
	t.Cleanup(func() { Set(prev) })
	Set(&Config{Gateway: srv.URL, Accounts: []*Account{{ID: "u1", Token: "tok"}}})
	CatalogClient = srv.Client()
	CatalogURLOverride = srv.URL + "/api/remote/v1/models"

	// 上一轮同步把 gone 确认成原生条目
	applyCatalog(map[string]Model{
		"gone": {ID: "gone", RawName: "gone__dev", Function: "solo_agent", Native: true},
	})

	if _, err := SyncCatalog(); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}
	got := catalogSnapshot()
	if !got["keep"].Native {
		t.Fatalf("keep not native: %+v", got["keep"])
	}
	if got["gone"].Native {
		t.Fatalf("offline model still native: %+v", got["gone"])
	}
	for _, m := range ModelList() {
		if m.ID == "gone" {
			t.Fatal("offline model must not be listed")
		}
	}
	if _, ok := Resolve("gone"); !ok {
		t.Fatal("offline model must stay resolvable")
	}
}

// 单档故障不做下线回收：原生档没全拉成功时无法区分"模型下线"与"拉取失败"。
func TestSyncCatalogKeepsNativeWhenScopeFails(t *testing.T) {
	resetCatalog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/remote/v1/models":
			io.WriteString(w, `{"data":{"list":[{"function":"solo_work_remote","models":[{"name":"keep"}]}]}}`)
		case "/api/ide/v1/get_detail_param":
			var body struct {
				Function string `json:"function"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Function == "solo_agent" {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			io.WriteString(w, `{"config_info_list":[{"config_name":"keep","model_detail_list":[{"model_name":"keep__dev"}]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prev := Get()
	t.Cleanup(func() { Set(prev) })
	Set(&Config{Gateway: srv.URL, Accounts: []*Account{{ID: "u1", Token: "tok"}}})
	CatalogClient = srv.Client()
	CatalogURLOverride = srv.URL + "/api/remote/v1/models"

	applyCatalog(map[string]Model{
		"gone": {ID: "gone", RawName: "gone__dev", Function: "solo_agent", Native: true},
	})

	if _, err := SyncCatalog(); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}
	if !catalogSnapshot()["gone"].Native {
		t.Fatal("single-scope failure must not prune native entries")
	}
}

// 没有可路由凭据（总闸/反代开关至少一个关）时不得发起目录请求：同步是给
// 可服务清单取数，关停平台不该产生上游流量。
func TestSyncCatalogRequiresRoutableAccount(t *testing.T) {
	resetCatalog(t)
	prev := Get()
	t.Cleanup(func() { Set(prev) })
	off := false
	Set(&Config{Accounts: []*Account{{ID: "u1", Token: "tok", PoolEnabled: &off}}})

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	CatalogClient = srv.Client()
	CatalogURLOverride = srv.URL

	if _, err := SyncCatalog(); err == nil {
		t.Fatal("SyncCatalog must fail without a routable account")
	}
	if hits != 0 {
		t.Fatalf("catalog fetched %d time(s) without routable credential", hits)
	}
}

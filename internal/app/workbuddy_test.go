package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	wbauth "afree-proxy/internal/workbuddy/auth"
)

type fakeWorkbuddyServer struct {
	requests     chan []byte
	status       int
	contentType  string
	responseBody string
}

func (f *fakeWorkbuddyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.requests <- body
	if f.contentType != "" {
		w.Header().Set("Content-Type", f.contentType)
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, f.responseBody)
}

func (f *fakeWorkbuddyServer) ModelList() []map[string]any { return nil }

func useFakeWorkbuddyServer(t *testing.T, fake *fakeWorkbuddyServer) {
	t.Helper()
	saved := workbuddySub
	workbuddySub = &workbuddySubsystem{server: fake}
	t.Cleanup(func() { workbuddySub = saved })
}

func TestWorkbuddyAnthropicRouteUsesChatBridge(t *testing.T) {
	fake := &fakeWorkbuddyServer{
		requests:     make(chan []byte, 1),
		contentType:  "application/json",
		responseBody: `{"id":"chatcmpl-1","model":"cn:test","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
	}
	useFakeWorkbuddyServer(t, fake)

	body := []byte(`{"model":"cn:test","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleAnthropicMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"text":"hello"`) {
		t.Fatalf("Anthropic response did not contain converted text: %s", rec.Body.String())
	}
	select {
	case got := <-fake.requests:
		var chat map[string]any
		if err := json.Unmarshal(got, &chat); err != nil {
			t.Fatalf("decode bridged chat request: %v", err)
		}
		if chat["model"] != "cn:test" {
			t.Fatalf("bridged model=%v want cn:test", chat["model"])
		}
	default:
		t.Fatal("WorkBuddy handler was not called")
	}
}

func TestWorkbuddyResponsesRouteStreamsChatBridge(t *testing.T) {
	fake := &fakeWorkbuddyServer{
		requests:    make(chan []byte, 1),
		contentType: "text/event-stream",
		responseBody: "data: {\"id\":\"chatcmpl-1\",\"model\":\"cn:test\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
			"data: [DONE]\n\n",
	}
	useFakeWorkbuddyServer(t, fake)

	body := []byte(`{"model":"cn:test","input":"hi","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleResponses(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	response := rec.Body.String()
	if !strings.Contains(response, "event: response.output_text.delta") || !strings.Contains(response, `"delta":"hello"`) {
		t.Fatalf("Responses stream did not contain converted text delta: %s", response)
	}
	select {
	case got := <-fake.requests:
		var chat map[string]any
		if err := json.Unmarshal(got, &chat); err != nil {
			t.Fatalf("decode bridged chat request: %v", err)
		}
		if chat["stream"] != true {
			t.Fatalf("bridged stream=%v want true", chat["stream"])
		}
	default:
		t.Fatal("WorkBuddy handler was not called")
	}
}

// resetWorkbuddyProxyState 隔离 WorkBuddy 绑定全局，避免测试间串数据。
func resetWorkbuddyProxyState(t *testing.T) {
	t.Helper()
	wbProxyMu.Lock()
	savedLoaded := wbProxyLoaded
	savedBindings := wbProxyBindings
	wbProxyLoaded = false
	wbProxyBindings = map[string]workbuddyProxyBinding{}
	wbProxyMu.Unlock()
	t.Cleanup(func() {
		wbProxyMu.Lock()
		wbProxyLoaded = savedLoaded
		wbProxyBindings = savedBindings
		wbProxyMu.Unlock()
	})
}

func TestShouldServeWorkBuddyModelPrefixes(t *testing.T) {
	saved := workbuddySub
	workbuddySub = &workbuddySubsystem{}
	t.Cleanup(func() { workbuddySub = saved })

	for _, model := range []string{"cn:glm-5.2", " global:claude-sonnet-4.5 "} {
		if !shouldServeWorkBuddy(model) {
			t.Fatalf("WorkBuddy model %q was not routed to the subsystem", model)
		}
	}
	for _, model := range []string{"", "glm-5.2", "cline-free/deepseek-v4.1-flash", "mimo-v2.5-free"} {
		if shouldServeWorkBuddy(model) {
			t.Fatalf("bare model %q must keep the existing Cline/OpenCode route", model)
		}
	}
}

func TestAdminHTMLUsesNativeWorkBuddyConsole(t *testing.T) {
	if strings.Contains(adminHTML, `id="wbFrame"`) || strings.Contains(adminHTML, `<iframe`) {
		t.Fatal("WorkBuddy admin page must not embed the upstream console in an iframe")
	}
	for _, marker := range []string{
		`id="wbAccountsBody"`,
		`id="wbConfigForm"`,
		`id="wbDetailsDialog"`,
		`id="wbTodoDialog"`,
		`popover="manual"`,
		`function loadWorkbuddyAccounts`,
		`function openWorkbuddyTodoScan`,
		`function renderWorkbuddyDetails`,
		`function renderWorkbuddyQueue`,
		`WorkBuddy 模型`,
	} {
		if !strings.Contains(adminHTML, marker) {
			t.Fatalf("native WorkBuddy page marker missing: %s", marker)
		}
	}
}

func TestWorkbuddyProxyBindingStableAndFailClosed(t *testing.T) {
	// 辅代理开关打开：本用例验证"主冷却 → 辅兜底"这条路径
	setupBindingTest(t, &zenConfigData{
		Keys:               []string{"public"},
		Proxies:            []string{"http://a:1", "http://b:2"},
		ProxyIsolation:     boolPtr(true),
		BackupProxyEnabled: boolPtr(true),
	})
	resetWorkbuddyProxyState(t)

	first := &wbauth.Auth{UID: "uid-a"}
	globalA, ok := workbuddyProxyFor(first)
	if !ok || globalA == "" {
		t.Fatalf("unbound account should use the global proxy pool: proxy=%q ok=%v", globalA, ok)
	}
	if _, bindings := workbuddyProxyBindingsSnapshot(); len(bindings) != 0 {
		t.Fatalf("global fallback must not persist an implicit binding: %+v", bindings)
	}

	if err := setWorkbuddyProxyBinding("uid-a", "http://a:1", "http://b:2"); err != nil {
		t.Fatalf("set binding: %v", err)
	}
	mainA, ok := workbuddyProxyFor(first)
	if !ok || mainA != "http://a:1" {
		t.Fatalf("bound account should use main proxy: proxy=%q ok=%v", mainA, ok)
	}

	// 主出口冷却时应改用绑定中的备份出口。
	setProxyCooldownIdx(t, proxyIdxInPool(mainA), time.Minute)
	fallback, ok := workbuddyProxyFor(first)
	if !ok || fallback == "" || fallback == mainA {
		t.Fatalf("cooled main should use backup: main=%q fallback=%q ok=%v", mainA, fallback, ok)
	}

	// 绑定出口都不在当前池中时必须失败，不能静默退回直连或新出口。
	setZenConfig(&zenConfigData{
		Keys:           []string{"public"},
		Proxies:        []string{"http://new:3"},
		ProxyIsolation: boolPtr(true),
	})
	if proxy, ok := workbuddyProxyFor(first); ok {
		t.Fatalf("stale bound exits must fail closed, got proxy=%q", proxy)
	}

	if err := setWorkbuddyProxyBinding("uid-direct", "direct", ""); err != nil {
		t.Fatalf("set direct binding: %v", err)
	}
	if proxy, ok := workbuddyProxyFor(&wbauth.Auth{UID: "uid-direct"}); !ok || proxy != "" {
		t.Fatalf("direct binding should force direct egress: proxy=%q ok=%v", proxy, ok)
	}
}

func TestWorkbuddyProxyBindingPersistence(t *testing.T) {
	// 辅槽要落盘就得先打开辅代理开关（关闭时写入即被收敛为空）
	setupBindingTest(t, &zenConfigData{
		Keys:               []string{"public"},
		Proxies:            []string{"http://a:1", "http://b:2"},
		ProxyIsolation:     boolPtr(true),
		BackupProxyEnabled: boolPtr(true),
	})
	resetWorkbuddyProxyState(t)

	if err := setWorkbuddyProxyBinding("uid-persist", "http://a:1", "http://b:2"); err != nil {
		t.Fatalf("set binding: %v", err)
	}
	raw, err := os.ReadFile(workbuddyProxyPath())
	if err != nil {
		t.Fatalf("read binding file: %v", err)
	}
	var onDisk map[string]workbuddyProxyBinding
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("decode binding file: %v", err)
	}
	if got := onDisk["uid-persist"]; got.Main != "http://a:1" || got.Backup != "http://b:2" {
		t.Fatalf("binding was not persisted correctly: %+v", got)
	}

	// 模拟重启：丢弃内存态后必须从文件恢复。
	wbProxyMu.Lock()
	wbProxyLoaded = false
	wbProxyBindings = map[string]workbuddyProxyBinding{}
	wbProxyMu.Unlock()
	_, bindings := workbuddyProxyBindingsSnapshot()
	if got := bindings["uid-persist"]; got.Main != "http://a:1" || got.Backup != "http://b:2" {
		t.Fatalf("binding was not restored after reload: %+v", got)
	}

	if n := clearAllWorkbuddyProxyBindings(); n != 1 {
		t.Fatalf("clear bindings: got %d, want 1", n)
	}
	raw, err = os.ReadFile(workbuddyProxyPath())
	if err != nil {
		t.Fatalf("read cleared binding file: %v", err)
	}
	if strings.Contains(string(raw), "uid-persist") {
		t.Fatal("cleared binding remains in persistent file")
	}
}

func TestWorkbuddyUnboundAccountUsesGlobalPoolWithoutPersisting(t *testing.T) {
	setupBindingTest(t, &zenConfigData{Keys: []string{"public"}, ProxyIsolation: boolPtr(true)})
	resetWorkbuddyProxyState(t)

	a := &wbauth.Auth{UID: "uid-late-pool"}
	if proxy, ok := workbuddyProxyFor(a); !ok || proxy != "" {
		t.Fatalf("empty proxy pool should run direct: proxy=%q ok=%v", proxy, ok)
	}
	if _, bindings := workbuddyProxyBindingsSnapshot(); len(bindings) != 0 {
		t.Fatalf("empty pool must not persist an empty binding: %+v", bindings)
	}

	setZenConfig(&zenConfigData{
		Keys:           []string{"public"},
		Proxies:        []string{"http://a:1", "http://b:2"},
		ProxyIsolation: boolPtr(true),
	})
	proxy, ok := workbuddyProxyFor(a)
	if !ok || proxy == "" {
		t.Fatalf("unbound account should use the global pool once available: proxy=%q ok=%v", proxy, ok)
	}
	if _, bindings := workbuddyProxyBindingsSnapshot(); len(bindings) != 0 {
		t.Fatalf("global pool use must not create a binding: %+v", bindings)
	}
}

func TestWorkbuddyIsolationDisabledIgnoresDirectBinding(t *testing.T) {
	setupBindingTest(t, &zenConfigData{
		Keys:           []string{"public"},
		Proxies:        []string{"http://a:1", "http://b:2"},
		ProxyIsolation: boolPtr(false),
	})
	resetWorkbuddyProxyState(t)

	if err := setWorkbuddyProxyBinding("uid-a", "direct", ""); err != nil {
		t.Fatalf("set binding: %v", err)
	}
	proxy, ok := workbuddyProxyFor(&wbauth.Auth{UID: "uid-a"})
	if !ok || proxy == "" {
		t.Fatalf("isolation off should ignore direct binding and use global rotation: proxy=%q ok=%v", proxy, ok)
	}
}

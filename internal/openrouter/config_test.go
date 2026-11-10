package openrouter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigNormalize(t *testing.T) {
	c := DefaultConfig()
	c.Keys = []string{"a", "a", "", " b ", "b"}
	c.BaseURL = "https://example.com/api/v1/"
	c.normalize()
	if len(c.Keys) != 2 || c.Keys[0] != "a" || c.Keys[1] != "b" {
		t.Fatalf("dedup failed: %v", c.Keys)
	}
	if c.BaseURL != "https://example.com/api/v1" {
		t.Fatalf("baseURL trim failed: %q", c.BaseURL)
	}
	if c.MaxConcurrency != 8 || c.Retries != 3 {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestReconcileRoutingNewKeysDefaultDisabled(t *testing.T) {
	cur := &Config{Keys: []string{"old"}}
	cur.KeyRoutingEnabled = map[string]bool{"old": true}
	next := &Config{Keys: []string{"old", "new1", "new2"}}
	next.KeyRoutingEnabled = map[string]bool{}
	next.ReconcileRouting(next, cur)
	if !next.RoutingEnabled("old") {
		t.Error("existing key should keep enabled state")
	}
	if next.RoutingEnabled("new1") || next.RoutingEnabled("new2") {
		t.Error("new keys must default to routing-disabled")
	}
}

func TestRoutingEnabledNilIsParticipating(t *testing.T) {
	c := &Config{Keys: []string{"k"}}
	if !c.RoutingEnabled("k") {
		t.Error("missing entry must mean participating (nil = enabled)")
	}
}

func TestMaskKey(t *testing.T) {
	if got := maskKey("sk-or-v1-0123456789abcdef"); got != "sk-or-v1-012…" {
		t.Fatalf("mask: %q", got)
	}
	if got := maskKey(""); got != "-" {
		t.Fatalf("empty: %q", got)
	}
	if got := maskKey("short"); got != "shor…" {
		t.Fatalf("short: %q", got)
	}
}

func TestSetPersistsAndLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".or-config.json")
	Load(path)
	cfg := &Config{BaseURL: "https://openrouter.ai/api/v1", Keys: []string{"k1"}, MaxConcurrency: 4, Retries: 2}
	cfg.KeyRoutingEnabled = map[string]bool{"k1": false}
	Set(cfg)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	var disk Config
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("corrupt on-disk config: %v", err)
	}
	if len(disk.Keys) != 1 || disk.Keys[0] != "k1" {
		t.Fatalf("keys not persisted: %v", disk.Keys)
	}
	// 重新加载：从磁盘恢复
	loaded = false
	Load(path)
	if len(Get().Keys) != 1 || Get().Keys[0] != "k1" {
		t.Fatalf("reload lost keys: %v", Get().Keys)
	}
}

func TestLoadCorruptFileKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".or-config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded = false
	cfg = DefaultConfig()
	Load(path)
	if Get().BaseURL != DefaultAPIBase {
		t.Fatalf("defaults lost: %q", Get().BaseURL)
	}
	// 损坏文件应被改名留档（不覆盖原文）
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".or-config.json.corrupt-") {
			found = true
		}
	}
	if !found {
		t.Errorf("corrupt file was not archived: %v", entries)
	}
}

func TestPickKeySkipsRoutingDisabledAndCooling(t *testing.T) {
	dir := t.TempDir()
	Load(filepath.Join(dir, ".or-config.json"))
	cfg := &Config{Keys: []string{"a", "b", "c"}}
	cfg.KeyRoutingEnabled = map[string]bool{"b": false}
	Set(cfg)
	Cooldown("c", 5*60e9)
	for i := 0; i < 3; i++ {
		if got := PickKey("round_robin", nil); got != "a" {
			t.Fatalf("picked disabled/cooling key: %q", got)
		}
	}
}

func TestPickKeyBlockedExitExcluded(t *testing.T) {
	dir := t.TempDir()
	Load(filepath.Join(dir, ".or-config.json"))
	Set(&Config{Keys: []string{"a", "b"}})
	got := PickKey("round_robin", func(key string) bool { return key != "a" })
	if got != "b" {
		t.Fatalf("blocked key a was still picked: %q", got)
	}
	Set(&Config{Keys: []string{"a"}})
	if k := PickKey("round_robin", func(key string) bool { return false }); k != "" {
		t.Fatalf("all blocked must yield empty, got %q", k)
	}
}

func TestPickKeyRoundRobinAdvances(t *testing.T) {
	dir := t.TempDir()
	Load(filepath.Join(dir, ".or-config.json"))
	Set(&Config{Keys: []string{"a", "b"}})
	first := PickKey("round_robin", nil)
	second := PickKey("round_robin", nil)
	if first == second {
		t.Fatalf("round-robin did not advance: %q %q", first, second)
	}
}

func TestPrefixStrip(t *testing.T) {
	id, ok := StripPrefix("oprt:openrouter/free")
	if !ok || id != "openrouter/free" {
		t.Fatalf("strip failed: %q %v", id, ok)
	}
	if _, ok := StripPrefix("openrouter/free"); ok {
		t.Error("bare id must not count as prefixed")
	}
	if !HasPrefix(" oprt:foo ") {
		t.Error("trimmed prefix must be detected")
	}
}

func TestPricingAllZero(t *testing.T) {
	cases := []struct {
		pricing map[string]any
		want    bool
	}{
		{map[string]any{"prompt": "0", "completion": "0", "request": "0"}, true},
		{map[string]any{"prompt": "0", "completion": "0"}, true},
		{map[string]any{"prompt": "0.0000001", "completion": "0"}, false},
		{map[string]any{"prompt": "0", "completion": "0.5"}, false},
		{map[string]any{"prompt": "1e-9", "completion": "0"}, false},
		{map[string]any{}, true},
	}
	for i, c := range cases {
		if got := pricingAllZero(c.pricing); got != c.want {
			t.Errorf("case %d: pricingAllZero(%v) = %v, want %v", i, c.pricing, got, c.want)
		}
	}
}

// live 为唯一权威（2026-11-05 快照白名单退役）：零价 + chatCapable 全量收
// （上游上新自动进目录，stealth/crouton-alpha 类页面新增不再被 2026-10-10
// 静态快照挡住）；价格转正剔除；非 chat 杂类（分类器/音频）不收。
func TestSyncCatalogLiveAuthoritative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			// 快照里：仍免费 → 保留（限额更新）
			{"id": "openrouter/free", "name": "Free Models Router", "context_length": 123456, "pricing": map[string]any{"prompt": "0", "completion": "0"}, "supported_parameters": []string{"tools"}},
			// 快照里：价格已非零 → 剔除
			{"id": "liquid/lfm-2.5-2.6b:free", "context_length": 65536, "pricing": map[string]any{"prompt": "0.001", "completion": "0"}},
			// 快照外新上的免费对话模型 → 自动收录（本次修复的核心）
			{"id": "stealth/crouton-alpha", "name": "Crouton Alpha", "context_length": 1048576, "pricing": map[string]any{"prompt": "0", "completion": "0"}, "supported_parameters": []string{"tools", "tool_choice"}, "architecture": map[string]any{"output_modalities": []string{"text"}}},
			// 零价但非 chat（分类器，无 tools 参数）→ 不收
			{"id": "nvidia/nemotron-3.5-content-safety:free", "context_length": 4096, "pricing": map[string]any{"prompt": "0", "completion": "0"}, "supported_parameters": []string{"max_tokens", "temperature"}, "architecture": map[string]any{"output_modalities": []string{"text"}}},
			// 零价但非 chat（音频输出）→ 不收
			{"id": "google/lyria-3-pro-preview", "context_length": 8192, "pricing": map[string]any{"prompt": "0", "completion": "0"}, "supported_parameters": []string{"max_tokens"}, "architecture": map[string]any{"output_modalities": []string{"text", "audio"}}},
			// 付费
			{"id": "openai/gpt-5", "context_length": 400000, "pricing": map[string]any{"prompt": "1.25", "completion": "10"}, "supported_parameters": []string{"tools"}},
		}})
	}))
	defer srv.Close()
	SetCatalogURLOverride(srv.URL)
	defer SetCatalogURLOverride("")
	catalogMu.Lock()
	// 现目录夹具（快照已取消，直接字面量造态）：openrouter/free 仍免费保留、
	// lfm 已转付费剔除（changed=1 的来源）。
	catalogModels = map[string]Model{
		"openrouter/free":          {ID: "openrouter/free", Context: 200000, ToolCall: true},
		"liquid/lfm-2.5-2.6b:free": {ID: "liquid/lfm-2.5-2.6b:free", Context: 65536},
	}
	catalogLoaded = true
	catalogMu.Unlock()
	changed, err := SyncCatalog()
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if changed != 1 {
		t.Errorf("expected 1 price-state change (lfm became paid), got %d", changed)
	}
	if _, ok := Resolve("liquid/lfm-2.5-2.6b:free"); ok {
		t.Error("model that is no longer free must be dropped")
	}
	if _, ok := Resolve("nvidia/nemotron-3.5-content-safety:free"); ok {
		t.Error("zero-priced non-chat classifier must not enter the catalog")
	}
	if _, ok := Resolve("google/lyria-3-pro-preview"); ok {
		t.Error("zero-priced audio model must not enter the catalog")
	}
	if _, ok := Resolve("openai/gpt-5"); ok {
		t.Error("paid model leaked into the catalog")
	}
	m, ok := Resolve("openrouter/free")
	if !ok {
		t.Fatal("openrouter/free must stay")
	}
	if m.Context != 123456 {
		t.Errorf("context not refreshed from live: %d", m.Context)
	}
	if !m.ToolCall {
		t.Error("tool_call flag not captured")
	}
	// 核心断言：快照外的新免费模型自动进目录
	nc, ok := Resolve("stealth/crouton-alpha")
	if !ok {
		t.Fatal("new off-snapshot free chat model must be auto-included (crouton-alpha regression)")
	}
	if nc.Context != 1048576 || !nc.ToolCall {
		t.Errorf("new model fields not captured: %+v", nc)
	}
}

func enableTestKey(t *testing.T) {
	t.Helper()
	prevCfg := Get()
	prevPath := ConfigPath
	ConfigPath = filepath.Join(t.TempDir(), ".or-config.json")
	t.Cleanup(func() {
		ConfigPath = prevPath
		mu.Lock()
		cfg = prevCfg
		mu.Unlock()
	})
	c := DefaultConfig()
	c.Keys = []string{"test-key"}
	c.KeyRoutingEnabled = map[string]bool{"test-key": true}
	Set(c)
}

// 展示门控：没有参与路由的启用 key 时目录为空（与请求入口的可路由性闸
// 同口径），但条目仍可被显式解析。
func TestModelListRequiresEnabledKey(t *testing.T) {
	catalogMu.Lock()
	catalogModels = map[string]Model{
		"openrouter/free": {ID: "openrouter/free", ToolCall: true},
		"a/first:free":    {ID: "a/first:free"},
	}
	catalogLoaded = true
	catalogMu.Unlock()

	prevCfg := Get()
	prevPath := ConfigPath
	ConfigPath = filepath.Join(t.TempDir(), ".or-config.json")
	t.Cleanup(func() {
		ConfigPath = prevPath
		mu.Lock()
		cfg = prevCfg
		mu.Unlock()
	})

	Set(DefaultConfig()) // 无 key
	if l := ModelList(); len(l) != 0 {
		t.Fatalf("no key must hide catalog: got %d", len(l))
	}

	c := DefaultConfig()
	c.Keys = []string{"k1"}
	c.KeyRoutingEnabled = map[string]bool{"k1": false}
	Set(c) // key 路由关闭
	if l := ModelList(); len(l) != 0 {
		t.Fatalf("disabled key must hide catalog: got %d", len(l))
	}

	c2 := DefaultConfig()
	c2.Keys = []string{"k1"}
	c2.KeyRoutingEnabled = map[string]bool{"k1": true}
	Set(c2) // 启用
	if l := ModelList(); len(l) != 2 {
		t.Fatalf("enabled key must list the catalog: got %d, want 2", len(l))
	}
	if _, ok := Resolve("openrouter/free"); !ok {
		t.Fatal("hidden catalog must stay resolvable")
	}
}

func TestSyncCatalogUnreachableKeepsCatalog(t *testing.T) {
	enableTestKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	SetCatalogURLOverride(srv.URL)
	defer SetCatalogURLOverride("")
	catalogMu.Lock()
	catalogModels = map[string]Model{
		"openrouter/free": {ID: "openrouter/free", ToolCall: true},
		"a/first:free":    {ID: "a/first:free"},
	}
	catalogLoaded = true
	catalogMu.Unlock()
	if _, err := SyncCatalog(); err == nil {
		t.Error("expected error on unreachable catalog")
	}
	if len(ModelList()) != 2 {
		t.Errorf("current catalog must survive an unreachable catalog: got %d, want 2", len(ModelList()))
	}
}

// 冷启动不再注入快照（2026-11-11 用户定，与 cline 同口径）：目录如实反映
// live 同步结果，宁空勿过期快照——官方会把模型移出免费层，快照会把已计费
// 模型当免费对外。空目录期间请求按 strict-gate fail-closed 拒。同步端点指到
// 必失败的服务器，保证 goroutine 首拉不会异步填目录造成竞态。
func TestColdStartEmptyUntilSync(t *testing.T) {
	enableTestKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	SetCatalogURLOverride(srv.URL)
	defer SetCatalogURLOverride("")
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	catalogLoaded = false
	catalogMu.Unlock()
	InitCatalog()
	if l := ModelList(); len(l) != 0 {
		t.Fatalf("cold start must be empty (no snapshot seed): got %d", len(l))
	}
	if _, ok := Resolve("openrouter/free"); ok {
		t.Error("no snapshot seed: openrouter/free must not appear before a successful sync")
	}
}

func TestProbeModelPrefersOfficialRouter(t *testing.T) {
	enableTestKey(t)
	catalogMu.Lock()
	catalogModels = map[string]Model{"a/first:free": {ID: "a/first:free"}, "openrouter/free": {ID: "openrouter/free"}}
	catalogLoaded = true
	catalogMu.Unlock()
	m, ok := ProbeModel()
	if !ok || m.ID != "openrouter/free" {
		t.Fatalf("probe model should prefer openrouter/free, got %q", m.ID)
	}
}

// 全冷却默认拒绝（UNABLE_FORCE 未设，2026-10-28 用户定）；true 时恢复兜底。
func TestPickKeyAllCoolingFollowsUnableForce(t *testing.T) {
	dir := t.TempDir()
	Load(filepath.Join(dir, ".or-config.json"))
	Set(&Config{Keys: []string{"a", "b"}})
	Cooldown("a", 5*60e9)
	Cooldown("b", 5*60e9)
	if got := PickKey("round_robin", nil); got != "" {
		t.Fatalf("default must reject when all keys are cooling, got %q", got)
	}
	t.Setenv("UNABLE_FORCE", "true")
	if got := PickKey("round_robin", nil); got != "a" && got != "b" {
		t.Fatalf("UNABLE_FORCE=true must fall back to a cooling key, got %q", got)
	}
}

// 冷却落盘回读（2026-10-28 用户定）：Cooldown 后重新 Load 同一配置文件，
// 冷却必须保留——重启后上游刚限流过的凭据不得立即回池被重试。
func TestCooldownPersistedAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".or-config.json")
	Load(path)
	Set(&Config{Keys: []string{"a", "b"}})
	Cooldown("a", 5*60e9)
	// 模拟重启：重新加载同一文件
	Load(path)
	until, ok := CooldownUntil("a")
	if !ok || until.Before(time.Now()) {
		t.Fatalf("cooldown for key a must survive reload, ok=%v until=%v", ok, until)
	}
	Uncool("a")
	Load(path)
	if _, ok := CooldownUntil("a"); ok {
		t.Fatal("uncooled key must stay uncooled after reload")
	}
}

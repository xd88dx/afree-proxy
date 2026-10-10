package openrouter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if disk.Usage != nil {
		t.Error("usage must not be persisted to disk")
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

func TestSyncCatalogKeepsOnlyPageModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			// 页面上：仍免费 → 保留（限额更新）
			{"id": "openrouter/free", "name": "Free Models Router", "context_length": 123456, "pricing": map[string]any{"prompt": "0", "completion": "0"}, "supported_parameters": []string{"tools"}},
			// 页面上：价格已非零 → 剔除
			{"id": "liquid/lfm-2.5-2.6b:free", "context_length": 65536, "pricing": map[string]any{"prompt": "0.001", "completion": "0"}},
			// 页面外：即使免费也不收
			{"id": "nvidia/nemotron-3-embed-1b:free", "context_length": 4096, "pricing": map[string]any{"prompt": "0", "completion": "0"}},
			// 页面外且付费
			{"id": "openai/gpt-5", "context_length": 400000, "pricing": map[string]any{"prompt": "1.25", "completion": "10"}},
		}})
	}))
	defer srv.Close()
	CatalogURLOverride = srv.URL
	defer func() { CatalogURLOverride = "" }()
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	for _, m := range PageModels {
		catalogModels[m.ID] = m
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
		t.Error("page model that is no longer free must be dropped")
	}
	if _, ok := Resolve("nvidia/nemotron-3-embed-1b:free"); ok {
		t.Error("off-page free model leaked into the catalog")
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
}

func TestSyncCatalogUnreachableKeepsPageSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	CatalogURLOverride = srv.URL
	defer func() { CatalogURLOverride = "" }()
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	for _, m := range PageModels {
		catalogModels[m.ID] = m
	}
	catalogLoaded = true
	catalogMu.Unlock()
	if _, err := SyncCatalog(); err == nil {
		t.Error("expected error on unreachable catalog")
	}
	if len(ModelList()) != len(PageModels) {
		t.Errorf("page snapshot must survive an unreachable catalog: got %d, want %d", len(ModelList()), len(PageModels))
	}
}

func TestSeedCatalogIsPageSnapshot(t *testing.T) {
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	catalogLoaded = false
	catalogMu.Unlock()
	InitCatalog()
	list := ModelList()
	if len(list) != len(PageModels) {
		t.Fatalf("cold start must use the page snapshot: got %d, want %d", len(list), len(PageModels))
	}
	for _, pm := range PageModels {
		if _, ok := Resolve(pm.ID); !ok {
			t.Errorf("page model %q missing from seed catalog", pm.ID)
		}
	}
	if _, ok := Resolve("openrouter/free"); !ok {
		t.Error("official fallback openrouter/free must be in the seed catalog")
	}
}

func TestProbeModelPrefersOfficialRouter(t *testing.T) {
	catalogMu.Lock()
	catalogModels = map[string]Model{"a/first:free": {ID: "a/first:free"}, "openrouter/free": {ID: "openrouter/free"}}
	catalogLoaded = true
	catalogMu.Unlock()
	m, ok := ProbeModel()
	if !ok || m.ID != "openrouter/free" {
		t.Fatalf("probe model should prefer openrouter/free, got %q", m.ID)
	}
}

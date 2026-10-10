package amd

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

func TestMaskKey(t *testing.T) {
	if got := maskKey("rc-4f8a19c7e02b6d3a5c81f70e9b2d4a6c"); got != "rc-4f8a19c7e…" {
		t.Fatalf("mask: %q", got)
	}
	if got := maskKey(""); got != "-" {
		t.Fatalf("empty: %q", got)
	}
}

func TestSetPersistsAndLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".amd-config.json")
	Load(path)
	cfg := &Config{BaseURL: DefaultAPIBase, Keys: []string{"rc-k1"}, MaxConcurrency: 4, Retries: 2}
	cfg.KeyRoutingEnabled = map[string]bool{"rc-k1": false}
	Set(cfg)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	var disk Config
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("corrupt on-disk config: %v", err)
	}
	if len(disk.Keys) != 1 || disk.Keys[0] != "rc-k1" {
		t.Fatalf("keys not persisted: %v", disk.Keys)
	}
	if disk.Usage != nil {
		t.Error("usage must not be persisted to disk")
	}
	loaded = false
	cfg = DefaultConfig()
	Load(path)
	if len(Get().Keys) != 1 || Get().Keys[0] != "rc-k1" {
		t.Fatalf("reload lost keys: %v", Get().Keys)
	}
}

func TestPickKeySkipsRoutingDisabledAndCooling(t *testing.T) {
	dir := t.TempDir()
	Load(filepath.Join(dir, ".amd-config.json"))
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

func TestPickKeyRoundRobinAdvances(t *testing.T) {
	dir := t.TempDir()
	Load(filepath.Join(dir, ".amd-config.json"))
	Set(&Config{Keys: []string{"a", "b"}})
	first := PickKey("round_robin", nil)
	second := PickKey("round_robin", nil)
	if first == second {
		t.Fatalf("round-robin did not advance: %q %q", first, second)
	}
}

func TestPrefixStrip(t *testing.T) {
	id, ok := StripPrefix("amd:DeepSeek-V4-Flash")
	if !ok || id != "DeepSeek-V4-Flash" {
		t.Fatalf("strip failed: %q %v", id, ok)
	}
	if _, ok := StripPrefix("DeepSeek-V4-Flash"); ok {
		t.Error("bare id must not count as prefixed")
	}
	if !HasPrefix(" amd:foo ") {
		t.Error("trimmed prefix must be detected")
	}
}

func TestChatCapableFilter(t *testing.T) {
	if !chatCapable(catalogEntry{Output: []string{"text"}}) {
		t.Error("text output must be chat-capable")
	}
	if chatCapable(catalogEntry{Output: []string{"ocr"}}) {
		t.Error("ocr-only output must be excluded")
	}
	if !chatCapable(catalogEntry{}) {
		t.Error("missing output field defaults to chat-capable")
	}
}

func TestSyncCatalogFiltersAndUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "DeepSeek-V4-Flash", "name": "DeepSeek-V4-Flash", "context_length": 999999, "output": []string{"text"}, "providers": []map[string]any{{"tools": true}}},
			{"id": "MinerU2.5-Pro", "name": "MinerU2.5-Pro", "context_length": 0, "output": []string{"ocr"}},
		}})
	}))
	defer srv.Close()
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	catalogLoaded = true
	catalogMu.Unlock()
	dir := t.TempDir()
	Load(filepath.Join(dir, ".amd-config.json"))
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	Set(cfg)
	if _, err := SyncCatalog(); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	list := ModelList()
	if len(list) != 1 {
		t.Fatalf("expected 1 chat model, got %d: %v", len(list), list)
	}
	m, ok := Resolve("DeepSeek-V4-Flash")
	if !ok {
		t.Fatal("DeepSeek-V4-Flash missing")
	}
	if m.Context != 999999 || !m.ToolCall {
		t.Errorf("metadata not captured: %+v", m)
	}
}

func TestSyncCatalogUnreachableKeepsSeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	for _, m := range SeedModels {
		catalogModels[m.ID] = m
	}
	catalogLoaded = true
	catalogMu.Unlock()
	dir := t.TempDir()
	Load(filepath.Join(dir, ".amd-config.json"))
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	Set(cfg)
	if _, err := SyncCatalog(); err == nil {
		t.Error("expected error on unreachable catalog")
	}
	if len(ModelList()) != len(SeedModels) {
		t.Errorf("seed must survive an unreachable catalog: got %d, want %d", len(ModelList()), len(SeedModels))
	}
}

func TestSeedCatalogShape(t *testing.T) {
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	catalogLoaded = false
	catalogMu.Unlock()
	InitCatalog()
	list := ModelList()
	if len(list) != len(SeedModels) {
		t.Fatalf("cold start must use the seed snapshot: got %d, want %d", len(list), len(SeedModels))
	}
	for _, sm := range SeedModels {
		if _, ok := Resolve(sm.ID); !ok {
			t.Errorf("seed model %q missing", sm.ID)
		}
	}
	if _, ok := Resolve("MinerU2.5-Pro"); ok {
		t.Error("OCR-only model must not be in the seed catalog")
	}
}

func TestProbeModelPrefersDocumentedModel(t *testing.T) {
	catalogMu.Lock()
	catalogModels = map[string]Model{"A/first": {ID: "A/first"}, "DeepSeek-V4-Flash": {ID: "DeepSeek-V4-Flash"}}
	catalogLoaded = true
	catalogMu.Unlock()
	m, ok := ProbeModel()
	if !ok || m.ID != "DeepSeek-V4-Flash" {
		t.Fatalf("probe model should prefer DeepSeek-V4-Flash, got %q", m.ID)
	}
}

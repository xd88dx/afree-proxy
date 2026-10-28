package tokenharbor

import (
	"time"
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
	c.BaseURL = "https://example.com/v1/"
	c.normalize()
	if len(c.Keys) != 2 || c.Keys[0] != "a" || c.Keys[1] != "b" {
		t.Fatalf("dedup failed: %v", c.Keys)
	}
	if c.BaseURL != "https://example.com/v1" {
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
	if got := maskKey("thk_live_0123456789abcdef"); got != "thk_live_012…" {
		t.Fatalf("mask: %q", got)
	}
	if got := maskKey(""); got != "-" {
		t.Fatalf("empty: %q", got)
	}
}

func TestSetPersistsAndLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".th-config.json")
	Load(path)
	cfg := &Config{BaseURL: DefaultAPIBase, Keys: []string{"thk_live_k1"}, MaxConcurrency: 4, Retries: 2}
	cfg.KeyRoutingEnabled = map[string]bool{"thk_live_k1": false}
	Set(cfg)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	var disk Config
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("corrupt on-disk config: %v", err)
	}
	if len(disk.Keys) != 1 || disk.Keys[0] != "thk_live_k1" {
		t.Fatalf("keys not persisted: %v", disk.Keys)
	}
	loaded = false
	cfg = DefaultConfig()
	Load(path)
	if len(Get().Keys) != 1 || Get().Keys[0] != "thk_live_k1" {
		t.Fatalf("reload lost keys: %v", Get().Keys)
	}
}

func TestPickKeySkipsRoutingDisabledAndCooling(t *testing.T) {
	dir := t.TempDir()
	Load(filepath.Join(dir, ".th-config.json"))
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
	Load(filepath.Join(dir, ".th-config.json"))
	Set(&Config{Keys: []string{"a", "b"}})
	first := PickKey("round_robin", nil)
	second := PickKey("round_robin", nil)
	if first == second {
		t.Fatalf("round-robin did not advance: %q %q", first, second)
	}
}

func TestPrefixStrip(t *testing.T) {
	id, ok := StripPrefix("tkhb:deepseek-v4.1-flash:free")
	if !ok || id != "deepseek-v4.1-flash:free" {
		t.Fatalf("strip failed: %q %v", id, ok)
	}
	if _, ok := StripPrefix("deepseek-v4.1-flash:free"); ok {
		t.Error("bare id must not count as prefixed")
	}
	if !HasPrefix(" tkhb:foo ") {
		t.Error("trimmed prefix must be detected")
	}
}

func TestSyncCatalogKeepsOnlyFreeSuffix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer thk_live_k1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/models") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{
			{"id": "claude-haiku-5.5:free", "context_length": 200000},
			{"id": "deepseek-v4.1-flash:free", "context_window": 262144},
			{"id": "glm-5.3", "context_length": 262144},     // 付费模型 → 排除
			{"id": "qwen3.8-max", "context_length": 262144}, // 付费模型 → 排除
		}})
	}))
	defer srv.Close()
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	catalogLoaded = true
	catalogMu.Unlock()
	dir := t.TempDir()
	Load(filepath.Join(dir, ".th-config.json"))
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Keys = []string{"thk_live_k1"}
	Set(cfg)
	if _, err := SyncCatalog(); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	list := ModelList()
	if len(list) != 2 {
		t.Fatalf("expected 2 free models, got %d: %v", len(list), list)
	}
	if _, ok := Resolve("glm-5.3"); ok {
		t.Error("paid model leaked into the catalog")
	}
	m, ok := Resolve("claude-haiku-5.5:free")
	if !ok || m.Context != 200000 {
		t.Errorf("context not captured: %+v", m)
	}
	m2, ok := Resolve("deepseek-v4.1-flash:free")
	if !ok || m2.Context != 262144 {
		t.Errorf("context_window fallback not captured: %+v", m2)
	}
}

func TestSyncCatalogWithoutKeyKeepsSeed(t *testing.T) {
	catalogMu.Lock()
	catalogModels = map[string]Model{}
	for _, m := range SeedModels {
		catalogModels[m.ID] = m
	}
	catalogLoaded = true
	catalogMu.Unlock()
	dir := t.TempDir()
	Load(filepath.Join(dir, ".th-config.json"))
	Set(DefaultConfig()) // 无 key
	changed, err := SyncCatalog()
	if err != nil || changed != 0 {
		t.Fatalf("no-key sync must be a silent no-op: %v %d", err, changed)
	}
	// 展示门控：无启用 key 时 ModelList 对外为空（种子仍留在目录里供
	// Resolve 请求侧兜底）——用带启用 key 的配置验证种子还在。
	cfg := DefaultConfig()
	cfg.Keys = []string{"tk-test"}
	cfg.KeyRoutingEnabled = map[string]bool{"tk-test": true}
	Set(cfg)
	if len(ModelList()) != len(SeedModels) {
		t.Errorf("seed must stand with an enabled key: got %d, want %d", len(ModelList()), len(SeedModels))
	}
	if l := ModelList(); len(l) != 0 {
		// 反向确认门控本身：切回无 key 配置应为空
		Set(DefaultConfig())
		if len(ModelList()) != 0 {
			t.Errorf("no enabled key must hide catalog: got %d", len(ModelList()))
		}
	}
}

func TestSyncCatalogUnreachableKeepsCurrent(t *testing.T) {
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
	Load(filepath.Join(dir, ".th-config.json"))
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Keys = []string{"thk_live_k1"}
	Set(cfg)
	if _, err := SyncCatalog(); err == nil {
		t.Error("expected error on unreachable catalog")
	}
	if len(ModelList()) != len(SeedModels) {
		t.Errorf("current catalog must survive an unreachable upstream: got %d, want %d", len(ModelList()), len(SeedModels))
	}
}

func TestSyncCatalogEmptyKeepsCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer srv.Close()
	catalogMu.Lock()
	catalogModels = map[string]Model{"deepseek-v4.1-flash:free": {ID: "deepseek-v4.1-flash:free"}}
	catalogLoaded = true
	catalogMu.Unlock()
	dir := t.TempDir()
	Load(filepath.Join(dir, ".th-config.json"))
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Keys = []string{"thk_live_k1"}
	Set(cfg)
	SyncCatalog()
	if _, ok := Resolve("deepseek-v4.1-flash:free"); !ok {
		t.Error("empty live result must not wipe the existing catalog")
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
		if !strings.HasSuffix(sm.ID, FreeSuffix) {
			t.Errorf("seed model %q must carry the :free suffix", sm.ID)
		}
	}
}

func TestProbeModelPrefersDeepSeek(t *testing.T) {
	catalogMu.Lock()
	catalogModels = map[string]Model{
		"claude-haiku-5.5:free":    {ID: "claude-haiku-5.5:free"},
		"deepseek-v4.1-flash:free": {ID: "deepseek-v4.1-flash:free"},
		"mimo-v2.6-flash:free":     {ID: "mimo-v2.6-flash:free"},
	}
	catalogLoaded = true
	catalogMu.Unlock()
	m, ok := ProbeModel()
	if !ok || m.ID != "deepseek-v4.1-flash:free" {
		t.Fatalf("probe model should prefer deepseek-v4.1-flash:free, got %q", m.ID)
	}
}

// 全冷却默认拒绝（UNABLE_FORCE 未设，2026-10-28 用户定）；true 时恢复兜底。
func TestPickKeyAllCoolingFollowsUnableForce(t *testing.T) {
	dir := t.TempDir()
	Load(filepath.Join(dir, ".tkhb-config.json"))
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
	path := filepath.Join(dir, ".tkhb-config.json")
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

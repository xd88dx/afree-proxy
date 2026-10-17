package tokenharbor

import (
	"path/filepath"
	"testing"
	"time"
)

// TestRecordKeyTokensAndStatus 验证 key 级 token 记账：记录、跨日重置、
// KeyStatus 字段出口。amd/openrouter/other 三处实现与本处同构。
func TestRecordKeyTokensAndStatus(t *testing.T) {
	Load(filepath.Join(t.TempDir(), ".th-config.json"))
	cfg := &Config{BaseURL: DefaultAPIBase, Keys: []string{"thk_live_k1"}, MaxConcurrency: 4, Retries: 2}
	Set(cfg)

	// 空 key / 非正数忽略：不得创建条目
	RecordKeyTokens("", 100)
	RecordKeyTokens("thk_live_k1", 0)
	RecordKeyTokens("thk_live_k1", -5)
	if c := Get(); len(c.KeyTokens) != 0 {
		t.Fatalf("invalid records must be ignored, got %v", c.KeyTokens)
	}

	RecordKeyTokens("thk_live_k1", 1500)
	ks := KeyStatus()
	if len(ks) != 1 {
		t.Fatalf("keyStates len = %d", len(ks))
	}
	st := ks[0]
	if st["tokensToday"].(int64) != 1500 || st["tokensTotal"].(int64) != 1500 {
		t.Fatalf("tokens mismatch: %+v", st)
	}
	if _, ok := st["lastUsed"]; !ok {
		t.Fatal("lastUsed missing")
	}
	if _, ok := st["lastUsed"].(string); !ok {
		t.Fatalf("lastUsed must be RFC3339 string, got %T", st["lastUsed"])
	}

	// 同日累计
	RecordKeyTokens("thk_live_k1", 500)
	if c := Get(); c.KeyTokens["thk_live_k1"].Today != 2000 || c.KeyTokens["thk_live_k1"].Total != 2000 {
		t.Fatalf("same-day accumulate: %+v", c.KeyTokens["thk_live_k1"])
	}

	// 跨日：Today 重置，Total 保留（直接改包级 cfg，勿在写锁内调 Get()）
	mu.Lock()
	ent := cfg.KeyTokens["thk_live_k1"]
	ent.Date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	cfg.KeyTokens["thk_live_k1"] = ent
	mu.Unlock()
	RecordKeyTokens("thk_live_k1", 300)
	if c := Get(); c.KeyTokens["thk_live_k1"].Today != 300 || c.KeyTokens["thk_live_k1"].Total != 2300 {
		t.Fatalf("cross-day reset: %+v", c.KeyTokens["thk_live_k1"])
	}

	// TokenStats 不落盘字段外的 keyTokens 持久化由 TestSetPersistsAndLoads 覆盖
	// （json tag keyTokens）；这里只确认 LastUsedAt 时间戳合理
	if ts := Get().KeyTokens["thk_live_k1"].LastUsedAt; time.Since(ts) > time.Minute {
		t.Fatalf("lastUsedAt stale: %v", ts)
	}
}

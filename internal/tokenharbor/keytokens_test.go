package tokenharbor

import (
	"path/filepath"
	"testing"
	"time"
)

// TestRecordKeyTokensAndStatus 验证 key 级 token 记账：记录、跨日重置、
// KeyStatus 字段出口。amd/openrouter/other 三处实现与本处同构。
// 2026-10-31 拆分：记账真身在包级 usage（keyMu/mu 同锁），不再挂在 Config 上。
func TestRecordKeyTokensAndStatus(t *testing.T) {
	Load(filepath.Join(t.TempDir(), ".th-config.json"))
	cfg := &Config{BaseURL: DefaultAPIBase, Keys: []string{"thk_live_k1"}, MaxConcurrency: 4, Retries: 2}
	Set(cfg)

	// 空 key / 非正数忽略：不得创建条目
	RecordKeyTokens("", 100)
	RecordKeyTokens("thk_live_k1", 0)
	RecordKeyTokens("thk_live_k1", -5)
	mu.RLock()
	n := len(usage.KeyTokens)
	mu.RUnlock()
	if n != 0 {
		t.Fatalf("invalid records must be ignored, got %d entries", n)
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
	mu.RLock()
	today, total := usage.KeyTokens["thk_live_k1"].Today, usage.KeyTokens["thk_live_k1"].Total
	mu.RUnlock()
	if today != 2000 || total != 2000 {
		t.Fatalf("same-day accumulate: today=%d total=%d", today, total)
	}

	// 跨日：Today 重置，Total 保留（直接改包级 usage，持锁操作）
	mu.Lock()
	ent := usage.KeyTokens["thk_live_k1"]
	ent.Date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	usage.KeyTokens["thk_live_k1"] = ent
	mu.Unlock()
	RecordKeyTokens("thk_live_k1", 300)
	mu.RLock()
	today, total = usage.KeyTokens["thk_live_k1"].Today, usage.KeyTokens["thk_live_k1"].Total
	lastUsed := usage.KeyTokens["thk_live_k1"].LastUsedAt
	mu.RUnlock()
	if today != 300 || total != 2300 {
		t.Fatalf("cross-day reset: today=%d total=%d", today, total)
	}

	// 这里只确认 LastUsedAt 时间戳合理（keyTokens 的落盘由 usage 文件承接）
	if time.Since(lastUsed) > time.Minute {
		t.Fatalf("lastUsedAt stale: %v", lastUsed)
	}
}

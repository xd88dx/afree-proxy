package app

import (
	"testing"
	"time"
)

// TestRecordZenKeyTokens 验证 zen key 级 token 记账：累计、跨日重置、
// zenKeyStatus 字段出口（面板"今日/累计 token"与"最近使用"列）。
func TestRecordZenKeyTokens(t *testing.T) {
	t.Cleanup(func() { zenKeyMu.Lock(); zenKeyTokens = map[string]zenKeyTokenStats{}; zenKeyMu.Unlock() })

	// 空 key / 非正数忽略
	recordZenKeyTokens("", 100)
	recordZenKeyTokens("zk1", 0)
	recordZenKeyTokens("zk1", -1)
	zenKeyMu.Lock()
	n := len(zenKeyTokens)
	zenKeyMu.Unlock()
	if n != 0 {
		t.Fatalf("invalid records must be ignored, got %d entries", n)
	}

	recordZenKeyTokens("zk1", 1200)
	recordZenKeyTokens("zk1", 800)
	zenKeyMu.Lock()
	st := zenKeyTokens["zk1"]
	zenKeyMu.Unlock()
	if st.Today != 2000 || st.Total != 2000 {
		t.Fatalf("same-day accumulate: %+v", st)
	}

	// 跨日：Today 重置，Total 保留
	zenKeyMu.Lock()
	st.Date = time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	zenKeyTokens["zk1"] = st
	zenKeyMu.Unlock()
	recordZenKeyTokens("zk1", 400)
	zenKeyMu.Lock()
	st = zenKeyTokens["zk1"]
	zenKeyMu.Unlock()
	if st.Today != 400 || st.Total != 2400 {
		t.Fatalf("cross-day reset: %+v", st)
	}
}

// TestZenKeyStatusExposesTokens 验证 zenKeyStatus 在有记账数据时输出
// tokensToday/tokensTotal/lastUsed，无数据时不输出（前端显示 -）。
func TestZenKeyStatusExposesTokens(t *testing.T) {
	t.Cleanup(func() { zenKeyMu.Lock(); zenKeyTokens = map[string]zenKeyTokenStats{}; zenKeyMu.Unlock() })

	savedCfg := getZenConfig()
	cfgCopy := *savedCfg
	cfgCopy.Keys = []string{"zk-status"}
	setZenConfig(&cfgCopy)
	t.Cleanup(func() { setZenConfig(savedCfg) })

	recordZenKeyTokens("zk-status", 77)
	ks := zenKeyStatus()
	var found map[string]any
	for _, k := range ks {
		if k["index"] == 0 { // 单 key 池：keyMask 已打码，按序号取
			found = k
			break
		}
	}
	if found == nil {
		t.Fatalf("key zk-status not in zenKeyStatus: %v", ks)
	}
	if found["tokensToday"].(int64) != 77 {
		t.Fatalf("tokensToday = %v", found["tokensToday"])
	}
	if found["tokensTotal"].(int64) != 77 {
		t.Fatalf("tokensTotal = %v", found["tokensTotal"])
	}
	lu, ok := found["lastUsed"].(string)
	if !ok {
		t.Fatalf("lastUsed missing or not string: %v", found["lastUsed"])
	}
	if _, err := time.Parse(time.RFC3339, lu); err != nil {
		t.Fatalf("lastUsed not RFC3339: %v", err)
	}
}

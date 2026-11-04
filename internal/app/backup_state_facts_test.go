package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// mergeStateFactsJSON 五平台 state 快照的账号事实合并（2026-11-03 三层口径）：
//   - cool（冷却表）= 账号事实，备份携带即整体替换；
//   - keyTokens / trae 的 accounts.*.tokens = 本机统计，永远保留本机值；
//   - trae 的 credits/checkin/checkinDeviceGen = 账号事实，按账号覆盖；
//     备份有而本机没有的账号建条目（无 tokens）。
func TestMergeStateFactsJSON(t *testing.T) {
	t.Run("standard shape (amd/oprt/tkhb/other)", func(t *testing.T) {
		local := json.RawMessage(`{"cool":{"k1":"2026-01-01T00:00:00Z"},"keyTokens":{"k1":{"Today":9}}}`)
		backup := json.RawMessage(`{"cool":{"k2":"2026-02-02T00:00:00Z"},"keyTokens":{"k1":{"Today":100}}}`)
		merged, err := mergeStateFactsJSON(local, backup, false)
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Cool      map[string]string `json:"cool"`
			KeyTokens map[string]struct {
				Today int64 `json:"Today"`
			} `json:"keyTokens"`
		}
		if err := json.Unmarshal(merged, &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m.Cool["k2"]; !ok {
			t.Fatalf("backup cool must replace local: %v", m.Cool)
		}
		if m.KeyTokens["k1"].Today != 9 {
			t.Fatalf("local keyTokens must be kept: %+v", m.KeyTokens)
		}
	})

	t.Run("backup without cool keeps local", func(t *testing.T) {
		local := json.RawMessage(`{"cool":{"k1":"2026-01-01T00:00:00Z"}}`)
		backup := json.RawMessage(`{}`)
		merged, err := mergeStateFactsJSON(local, backup, false)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(merged), "k1") {
			t.Fatalf("absent cool in backup must keep local: %s", merged)
		}
	})

	t.Run("trae shape", func(t *testing.T) {
		local := json.RawMessage(`{"accounts":{
			"a":{"tokens":{"Total":50},"credits":{"credits":1000},"checkin":{"checkedIn":true}},
			"lonely":{"tokens":{"Total":7}}
		},"cool":{"a":"2026-03-03T00:00:00Z"}}`)
		backup := json.RawMessage(`{"accounts":{
			"a":{"tokens":{"Total":999},"credits":{"credits":2222},"checkin":{"checkedIn":false}},
			"new":{"credits":{"credits":5}}
		},"cool":{"a":"2026-04-04T00:00:00Z"}}`)
		merged, err := mergeStateFactsJSON(local, backup, true)
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Accounts map[string]struct {
				Tokens  *json.RawMessage `json:"tokens"`
				Credits *struct {
					Credits float64 `json:"credits"`
				} `json:"credits"`
				Checkin *struct {
					CheckedIn bool `json:"checkedIn"`
				} `json:"checkin"`
			} `json:"accounts"`
			Cool map[string]string `json:"cool"`
		}
		if err := json.Unmarshal(merged, &m); err != nil {
			t.Fatal(err)
		}
		// a：事实覆盖、tokens 保留本机。
		a := m.Accounts["a"]
		if a.Credits == nil || a.Credits.Credits != 2222 {
			t.Fatalf("trae credits (fact) must be overwritten from backup: %+v", a)
		}
		if a.Checkin == nil || a.Checkin.CheckedIn {
			t.Fatalf("trae checkin (fact) must be overwritten from backup: %+v", a)
		}
		if a.Tokens == nil || !strings.Contains(string(*a.Tokens), "50") {
			t.Fatalf("local trae tokens must be kept: %+v", a.Tokens)
		}
		// new：备份新账号建条目，但不带 tokens（统计不迁移）。
		if m.Accounts["new"].Tokens != nil {
			t.Fatalf("backup account must not carry tokens into the new deployment: %+v", m.Accounts["new"])
		}
		if m.Accounts["new"].Credits == nil || m.Accounts["new"].Credits.Credits != 5 {
			t.Fatalf("backup new account credits missing: %+v", m.Accounts["new"])
		}
		// lonely：本机账号不在备份 → 原样保留。
		if m.Accounts["lonely"].Tokens == nil {
			t.Fatalf("local-only account must be kept untouched: %+v", m.Accounts["lonely"])
		}
		// cool：整体替换。
		if _, ok := m.Cool["a"]; !ok || m.Cool["a"] != "2026-04-04T00:00:00Z" {
			t.Fatalf("backup cool must replace local: %v", m.Cool)
		}
	})
}

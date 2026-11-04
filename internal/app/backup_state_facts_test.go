package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// mergeStateFactsJSON 五平台 state 快照的账号事实合并（2026-11-03 三层口径）：
//   - cool（冷却表）= 账号事实，备份携带即整体替换；
//   - keyTokens = 本机统计，永远保留本机值。
//
// trae 段不走本函数（2026-11-04 起由 trae2apiRestoreBackup 全段处理）。
func TestMergeStateFactsJSON(t *testing.T) {
	t.Run("standard shape (amd/oprt/tkhb/other)", func(t *testing.T) {
		local := json.RawMessage(`{"cool":{"k1":"2026-01-01T00:00:00Z"},"keyTokens":{"k1":{"Today":9}}}`)
		backup := json.RawMessage(`{"cool":{"k2":"2026-02-02T00:00:00Z"},"keyTokens":{"k1":{"Today":100}}}`)
		merged, err := mergeStateFactsJSON(local, backup)
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
		merged, err := mergeStateFactsJSON(local, backup)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(merged), "k1") {
			t.Fatalf("absent cool in backup must keep local: %s", merged)
		}
	})
}

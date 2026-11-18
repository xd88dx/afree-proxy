package zen

import (
	"strings"
	"testing"
)

// Gate tools must be the v2 12-tool set, not the v1 11-tool set.
// （原随 tls_bun_test.go；2026-11-17 P3b TLS 指纹测试随 exitpool 出包，本断言
// 属 zen 域留 app。）
func TestZenGateToolDefsV2Set(t *testing.T) {
	defs := ZenGateToolDefs()
	if len(defs) != 12 {
		t.Fatalf("gate tool defs = %d, want 12", len(defs))
	}
	wantNames := []string{"edit", "glob", "grep", "question", "read", "shell", "skill", "subagent", "webfetch", "websearch", "write", "execute"}
	for i, n := range wantNames {
		if defs[i].Name != n {
			t.Fatalf("gate tool[%d] = %q, want %q", i, defs[i].Name, n)
		}
		if defs[i].ParametersChat == nil {
			t.Fatalf("gate tool %q has no chat parameters", n)
		}
	}
	if !strings.Contains(defs[0].Name, "edit") {
		t.Fatalf("order sanity: first = %q", defs[0].Name)
	}
}

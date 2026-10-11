package kit

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func resetZenIDState(ts int64) {
	zenIDMu.Lock()
	zenIDLastTS = ts
	zenIDCtr = 0
	zenIDMu.Unlock()
}

func TestMintZenSessionIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
	for i := 0; i < 100; i++ {
		id := MintZenSessionID()
		if len(id) != 30 {
			t.Fatalf("len = %d, want 30: %s", len(id), id)
		}
		if !re.MatchString(id) {
			t.Fatalf("format mismatch: %s", id)
		}
	}
}

func TestMintZenSessionIDUniqueWithinSameMillisecond(t *testing.T) {
	resetZenIDState(time.Now().UnixMilli())
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := MintZenSessionID()
		if seen[id] {
			t.Fatalf("duplicate ID within same ms: %s", id)
		}
		seen[id] = true
	}
}

func TestValidZenSessionID(t *testing.T) {
	id := MintZenSessionID()
	if !ValidZenSessionID(id) {
		t.Fatalf("minted ID must be valid: %s", id)
	}
	for _, bad := range []string{
		"",
		"sess_" + strings.Repeat("a", 26),
		"ses_" + strings.Repeat("g", 26),
		"ses_" + strings.Repeat("a", 12) + strings.Repeat("-", 14),
	} {
		if ValidZenSessionID(bad) {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

// 匿名路径的身份也必须过免费门：session 组件必须是合法 ses_ 格式，
// 否则路由到 zen 时每请求必 403（旧实现 mint "sess_"+26 随机串即此问题）。
func TestFreshZenIdentityMintsValidSession(t *testing.T) {
	re := regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
	for i := 0; i < 50; i++ {
		sess, req, ua := FreshZenIdentity()
		if !re.MatchString(sess) {
			t.Fatalf("anonymous session not gate-valid: %q", sess)
		}
		if !ValidZenSessionID(sess) {
			t.Fatalf("ValidZenSessionID rejected a freshly minted session: %q", sess)
		}
		if !strings.HasPrefix(req, "msg_") || len(req) != 30 {
			t.Fatalf("request id malformed: %q", req)
		}
		if ua == "" {
			t.Fatal("user-agent must not be empty")
		}
	}
}

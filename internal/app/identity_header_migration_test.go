package app

import "testing"

// 持久化 headers 会覆盖 clineHeaders 的默认值，所以旧版本指纹必须在加载时升级，
// 否则改了常量线上仍发旧客户端版本。此测试钉住"升级哪些、不动哪些"的边界。
func TestNormalizeIdentityHeaders(t *testing.T) {
	t.Run("legacy defaults are upgraded", func(t *testing.T) {
		h := map[string]string{
			"User-Agent":         "Cline/3.0.70",
			"X-CLIENT-VERSION":   "3.0.70",
			"X-PLATFORM-VERSION": "3.0.70",
			"X-CORE-VERSION":     "0.0.92",
			"X-Title":            "Cline",
			"HTTP-Referer":       "https://cline.bot",
		}
		if n := normalizeIdentityHeaders(h); n != 4 {
			t.Fatalf("upgraded %d headers, want 4 (%v)", n, h)
		}
		if got := h["User-Agent"]; got != clineUserAgent() {
			t.Errorf("User-Agent = %q, want %q", got, clineUserAgent())
		}
		if got := h["X-CLIENT-VERSION"]; got != clineClientVersion {
			t.Errorf("X-CLIENT-VERSION = %q, want %q", got, clineClientVersion)
		}
		if got := h["X-PLATFORM-VERSION"]; got != clineClientVersion {
			t.Errorf("X-PLATFORM-VERSION = %q, want %q", got, clineClientVersion)
		}
		if got := h["X-CORE-VERSION"]; got != clineCoreVersion {
			t.Errorf("X-CORE-VERSION = %q, want %q", got, clineCoreVersion)
		}
		// 非指纹头一律不动
		if h["X-Title"] != "Cline" || h["HTTP-Referer"] != "https://cline.bot" {
			t.Errorf("non-identity headers must not be touched: %v", h)
		}
	})

	t.Run("stale suffix UA is upgraded", func(t *testing.T) {
		h := map[string]string{"User-Agent": "Cline/3.0.70 ai-sdk/openai-compatible/1.0.0"}
		if n := normalizeIdentityHeaders(h); n != 1 || h["User-Agent"] != clineUserAgent() {
			t.Fatalf("stale suffix UA not upgraded: n=%d ua=%q", n, h["User-Agent"])
		}
	})

	t.Run("current values are left alone", func(t *testing.T) {
		h := map[string]string{
			"User-Agent":       clineUserAgent(),
			"X-CLIENT-VERSION": clineClientVersion,
			"X-CORE-VERSION":   clineCoreVersion,
		}
		if n := normalizeIdentityHeaders(h); n != 0 {
			t.Errorf("upgraded %d headers although all current: %v", n, h)
		}
	})

	t.Run("custom values are untouched", func(t *testing.T) {
		h := map[string]string{
			"User-Agent":       "MyAgent/1.2.3",
			"X-CLIENT-VERSION": "beta",
			"X-CORE-VERSION":   "0.0", // 非三段 semver，不猜
		}
		if n := normalizeIdentityHeaders(h); n != 0 {
			t.Errorf("upgraded %d custom headers, want 0 (%v)", n, h)
		}
		if h["User-Agent"] != "MyAgent/1.2.3" || h["X-CLIENT-VERSION"] != "beta" || h["X-CORE-VERSION"] != "0.0" {
			t.Errorf("custom values were modified: %v", h)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		if n := normalizeIdentityHeaders(nil); n != 0 {
			t.Errorf("nil map changed %d", n)
		}
	})
}

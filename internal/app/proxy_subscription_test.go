package app

import (
	"testing"
	"time"
)

func TestValidateCronExpr(t *testing.T) {
	valid := []string{
		"*/30 * * * *", "* * * * *", "0 4 * * *", "15 2,8 * * 1-5",
		"0 */6 * * *", "30 4 1,15 * 0", "5 4 * * 7", "10-40/5 * * * *",
	}
	for _, e := range valid {
		if err := validateCronExpr(e); err != nil {
			t.Errorf("validateCronExpr(%q) = %v, want nil", e, err)
		}
	}
	invalid := []string{
		"", "* * * *", "* * * * * *", // 字段数
		"60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", // 越界
		"a * * * *", "*/ * * * *", "1-5-9 * * * *", "", // 语法
	}
	for _, e := range invalid {
		if err := validateCronExpr(e); err == nil {
			t.Errorf("validateCronExpr(%q) = nil, want error", e)
		}
	}
}

func TestCronMatches(t *testing.T) {
	// time.Date(y, m, d, h, min, ...) 2026-10-19 是周一
	cases := []struct {
		expr string
		t    time.Time
		want bool
	}{
		{"*/30 * * * *", time.Date(2026, 10, 19, 8, 30, 0, 0, time.Local), true},
		{"*/30 * * * *", time.Date(2026, 10, 19, 8, 31, 0, 0, time.Local), false},
		{"0 4 * * *", time.Date(2026, 10, 19, 4, 0, 0, 0, time.Local), true},
		{"0 4 * * *", time.Date(2026, 10, 19, 5, 0, 0, 0, time.Local), false},
		{"15 2,8 * * 1-5", time.Date(2026, 10, 19, 8, 15, 0, 0, time.Local), true},  // 周一
		{"15 2,8 * * 1-5", time.Date(2026, 10, 18, 8, 15, 0, 0, time.Local), false}, // 周日
		{"* * * * 7", time.Date(2026, 10, 18, 0, 0, 0, 0, time.Local), true},        // 周日=0/7 等价
		{"10-40/5 * * * *", time.Date(2026, 10, 19, 1, 20, 0, 0, time.Local), true},
		{"10-40/5 * * * *", time.Date(2026, 10, 19, 1, 21, 0, 0, time.Local), false},
	}
	for _, c := range cases {
		if got := cronMatches(c.expr, c.t); got != c.want {
			t.Errorf("cronMatches(%q, %s) = %v, want %v", c.expr, c.t, got, c.want)
		}
	}
}

func TestProxyAliasOf(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"socks5://10.10.10.5:10101#EUserv", "EUserv"},
		{"socks5://Og@10.10.10.5:10101#EUserv", "EUserv"},
		{"http://u:p@h:1#a b", "a b"},
		{"http://h:1", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := proxyAliasOf(c.in); got != c.want {
			t.Errorf("proxyAliasOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProxyIdxInPoolAliasFallback(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Proxies: []string{"socks5://10.10.10.5:20101#EUserv", "socks5://10.10.10.6:30001"},
	})
	// 端口/凭据变了但备注名相同 → 命中池内新地址
	if got := proxyIdxInPool("socks5://10.10.10.5:10101#EUserv"); got != 0 {
		t.Errorf("alias fallback idx = %d, want 0", got)
	}
	// 无备注行：仍按精确 URI，不模糊互通
	if got := proxyIdxInPool("socks5://10.10.10.6:9999"); got != -1 {
		t.Errorf("no-alias exact idx = %d, want -1", got)
	}
	if got := proxyIdxInPool("socks5://10.10.10.6:30001"); got != 1 {
		t.Errorf("no-alias exact idx = %d, want 1", got)
	}
}

package app

// mainSlotMatch 单元测试（2026-11-26 修）：RebindMain 槽位匹配语义——
// 别名匹配 / 直连哨兵匹配 / 主备同代理守卫。锁定 mainSlotMatch 的三组
// 判定，防止适配器改绑路径退化（URL 级替换误伤、落盘非法绑定）。

import (
	"testing"

	"afree-proxy/internal/exitpool"
)

func TestMainSlotMatch(t *testing.T) {
	const from = "socks5://old:1#a"
	const to = "socks5://new:2#b"
	cases := []struct {
		name  string
		main  string
		backup string
		from  string
		to    string
		want  bool
	}{
		{"alias match", "socks5://old:9#a", "", from, to, true},
		{"url differs", "socks5://other:1#c", "", from, to, false},
		{"empty main never matches url", "", "", from, to, false},
		{"direct from matches direct main", exitpool.DirectSentinel, "", exitpool.DirectSentinel, to, true},
		{"direct from skips proxy main", "socks5://old:9#a", "", exitpool.DirectSentinel, to, false},
		{"url from skips direct main", exitpool.DirectSentinel, "", from, to, false},
		{"backup equals target guarded", "socks5://old:9#a", to, from, to, false},
		{"direct backup is fine as backup", "socks5://old:9#a", exitpool.DirectSentinel, from, to, true},
	}
	for _, c := range cases {
		if got := mainSlotMatch(c.main, c.backup, c.from, c.to); got != c.want {
			t.Errorf("%s: mainSlotMatch(%q,%q,%q,%q) = %v, want %v", c.name, c.main, c.backup, c.from, c.to, got, c.want)
		}
	}
}

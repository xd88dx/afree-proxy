package app

import (
	"reflect"
	"testing"
)

// TestPruneProxyExclusions 锁定自动分配规则排除表的清理口径：
// 池内代理保留、池外代理丢弃、直连哨兵键永不删除、全空归 nil。
func TestPruneProxyExclusions(t *testing.T) {
	pool := []string{"socks5://h1:1#A", "socks5://h2:2#B"}
	cases := []struct {
		name string
		in   map[string][]string
		want map[string][]string
	}{
		{
			name: "keeps in-pool and direct sentinel, drops off-pool",
			in: map[string][]string{
				"socks5://h1:1#A": {"key"},
				"socks5://gone:9#Z": {"key", "th"},
				proxyRuleDirectKey:  {"th"},
			},
			want: map[string][]string{
				"socks5://h1:1#A":  {"key"},
				proxyRuleDirectKey: {"th"},
			},
		},
		{
			name: "all off-pool entries collapse to nil",
			in:   map[string][]string{"socks5://gone:9#Z": {"acc"}},
			want: nil,
		},
		{
			name: "empty input stays empty",
			in:   nil,
			want: nil,
		},
		{
			name: "empty pool keeps only sentinel",
			in: map[string][]string{
				"socks5://h1:1#A":  {"key"},
				proxyRuleDirectKey: {"th"},
			},
			want: map[string][]string{proxyRuleDirectKey: {"th"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p []string
			if tc.name != "empty pool keeps only sentinel" {
				p = pool
			}
			got := pruneProxyExclusions(tc.in, p)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("prune = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestProxyRuleKinds 保证前端 kind 与后端白名单同口径，漏一个平台会让该平台
// 的规则整表保存失败（400 invalid platform kind）。
func TestProxyRuleKinds(t *testing.T) {
	for _, k := range []string{"acc", "key", "wb", "amd", "or", "th", "other"} {
		if !proxyRuleKinds[k] {
			t.Fatalf("kind %q missing from proxyRuleKinds", k)
		}
	}
	if proxyRuleKinds["bogus"] {
		t.Fatal("bogus kind accepted")
	}
}

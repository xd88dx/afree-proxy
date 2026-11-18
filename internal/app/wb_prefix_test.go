package app

import "testing"

func TestWorkbuddyPrefixTranslation(t *testing.T) {

	cases := []struct{ in, want string }{

		{"wbcn:claude-sonnet-4.5", "cn:claude-sonnet-4.5"},

		{"wbgb:claude-sonnet-4.5", "global:claude-sonnet-4.5"},

		{"cn:legacy-model", "cn:legacy-model"}, // 旧前缀原样（vendor 自解析）

		{"global:legacy-model", "global:legacy-model"},

		{"bare-model", "bare-model"},
	}

	for _, c := range cases {

		if got := wbRealmToVendor(c.in); got != c.want {

			t.Errorf("wbRealmToVendor(%q) = %q, want %q", c.in, got, c.want)

		}

	}

	back := []struct{ in, want string }{

		{"cn:claude-sonnet-4.5", "wbcn:claude-sonnet-4.5"},

		{"global:claude-sonnet-4.5", "wbgb:claude-sonnet-4.5"},

		{"wbcn:already", "wbcn:already"},
	}

	for _, c := range back {

		if got := wbVendorToRealm(c.in); got != c.want {

			t.Errorf("wbVendorToRealm(%q) = %q, want %q", c.in, got, c.want)

		}

	}

}

// 路由判定必须认新前缀与旧前缀两种形态。

func TestIsWorkBuddyModelAcceptsBothPrefixes(t *testing.T) {

	for _, m := range []string{"wbcn:x", "wbgb:x", "cn:x", "global:x"} {

		if !isWorkBuddyModel(m) {

			t.Errorf("isWorkBuddyModel(%q) must be true", m)

		}

	}

	for _, m := range []string{"bare", "zen:big-pickle", "cline-free/x"} {

		if isWorkBuddyModel(m) {

			t.Errorf("isWorkBuddyModel(%q) must be false", m)

		}

	}

}

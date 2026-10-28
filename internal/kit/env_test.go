package kit

import "testing"

// UNABLE_FORCE 解析（2026-10-28 用户定）：真值 1/true/yes/y/on（大小写不敏感），
// 其余（含未设置/空白/0/false）为假。默认 false = 全冷却一律拒绝。
func TestUnableForce(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"  ", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"Y", true},
		{"on", true},
		{" true ", true},
	}
	for _, c := range cases {
		t.Setenv("UNABLE_FORCE", c.val)
		if got := UnableForce(); got != c.want {
			t.Errorf("UnableForce with UNABLE_FORCE=%q = %v, want %v", c.val, got, c.want)
		}
	}
	// 未设置 = 默认 false（拒绝语义）。
	t.Setenv("UNABLE_FORCE", "")
	if got := UnableForce(); got {
		t.Error("UnableForce default must be false")
	}
}

// DISABLE_DIRECT / ONLY_ROUTER（2026-10-28 用户定）：与 UnableForce 同一套
// 布尔解析（1/true/yes/y/on），默认均为 false。
func TestDisableDirectAndOnlyRouter(t *testing.T) {
	cases := []struct {
		env  string
		call func() bool
	}{
		{"DISABLE_DIRECT", DisableDirect},
		{"ONLY_ROUTER", OnlyRouter},
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			t.Setenv(c.env, "")
			if c.call() {
				t.Errorf("%s default must be false", c.env)
			}
			t.Setenv(c.env, "true")
			if !c.call() {
				t.Errorf("%s must be true when env is true", c.env)
			}
			t.Setenv(c.env, "0")
			if c.call() {
				t.Errorf("%s must be false when env is 0", c.env)
			}
		})
	}
}

// 账号总开关（auth.account_enabled，afree 本地补丁）的选号过滤契约：
// 关闭的账号不参与 Pick（含请求级轮换与全冷却兜底）、粘性直取（PickByUID
// 及其模型变体）与可用集合；但保留在池内，Status.AccountOn 透出开关值。
// 与 pool_enabled 正交：仅关闭反代开关（PoolEnabled）不影响 AccountOn，
// 仅关闭账号开关时 Status.PoolEnabled 保持不变。
// 缺省语义：零值 auth（未写 account_enabled 键）恒为启用，存量账号零迁移。
package pool

import (
	"testing"

	"afree-proxy/internal/workbuddy/auth"
)

func TestAccountEnabledFiltering(t *testing.T) {
	withNoPickGap(t)
	p := New("")
	a1 := &auth.Auth{UID: "u1"}
	a2 := &auth.Auth{UID: "u2"}
	p.Add(a1)
	p.Add(a2)
	p.SetCredits("u1", 100, 0)
	p.SetCredits("u2", 50000, 0) // 权重更高：若过滤失效会被选中
	a2.SetAccountEnabled(false)

	for i := 0; i < 100; i++ {
		if got := p.Pick(); got == nil || got.UID != "u1" {
			t.Fatalf("pick=%+v, want u1 only (u2 account-disabled)", got)
		}
	}
	if got := p.PickExcluding(map[string]bool{"u1": true}); got != nil {
		t.Fatalf("PickExcluding=%+v, want nil (u2 account-disabled)", got)
	}
	if got := p.PickByUID("u2"); got != nil {
		t.Fatal("PickByUID(u2) should be nil")
	}
	if got := p.PickByUIDForModel("u2", "some-model"); got != nil {
		t.Fatal("PickByUIDForModel(u2) should be nil")
	}
	if got := p.PickByUID("u1"); got == nil {
		t.Fatal("PickByUID(u1) should succeed")
	}
	for _, uid := range p.AvailableUIDs() {
		if uid == "u2" {
			t.Fatal("available UIDs contains account-disabled u2")
		}
	}
	for _, uid := range p.AvailableUIDsForModel("some-model") {
		if uid == "u2" {
			t.Fatal("available UIDs for model contains account-disabled u2")
		}
	}
	// Status 透出：AccountOn=false 而 PoolEnabled 保持不变（两开关正交）
	if st, ok := p.Status("u2"); !ok || st.AccountOn() {
		t.Fatalf("status u2 AccountOn=%v ok=%v, want false", st.AccountOn(), ok)
	}
	if st, ok := p.Status("u2"); !ok || !st.PoolEnabled {
		t.Fatalf("status u2 pool_enabled=%v, want true (orthogonal switches)", st.PoolEnabled)
	}
	if st, ok := p.Status("u1"); !ok || !st.AccountOn() {
		t.Fatalf("status u1 AccountOn=%v ok=%v, want true", st.AccountOn(), ok)
	}
	// 重新开启后立即回池
	a2.SetAccountEnabled(true)
	if got := p.PickByUID("u2"); got == nil {
		t.Fatal("PickByUID(u2) should succeed after re-enable")
	}
}

func TestAccountEnabledDefaultTrue(t *testing.T) {
	// 零值 auth（存量账号，无 account_enabled 键）恒为启用
	a := &auth.Auth{UID: "u1"}
	if !a.AccountEnabled() {
		t.Fatal("zero-value auth should default to account enabled")
	}
	// 与 pool_enabled 互不影响
	a.SetPoolEnabled(false)
	if !a.AccountEnabled() {
		t.Fatal("SetPoolEnabled(false) must not touch account switch")
	}
	a.SetAccountEnabled(false)
	if a.PoolEnabled() {
		t.Fatal("SetAccountEnabled(false) must not touch pool switch")
	}
}

// 管理员"启用"开关（auth.pool_enabled）的选号过滤契约：
// 禁用的账号不参与 Pick（含请求级轮换与全冷却兜底）、粘性直取（PickByUID
// 及其模型变体）与可用集合；但保留在池内，Status 透出 pool_enabled，
// 重新勾选后立即回池。
package pool

import (
	"testing"

	"afree-proxy/internal/workbuddy/auth"
)

func TestPoolEnabledFiltering(t *testing.T) {
	withNoPickGap(t)
	p := New("")
	a1 := &auth.Auth{UID: "u1"}
	a2 := &auth.Auth{UID: "u2"}
	p.Add(a1)
	p.Add(a2)
	p.SetCredits("u1", 100, 0)
	p.SetCredits("u2", 50000, 0) // 权重更高：若过滤失效会被选中
	a2.SetPoolEnabled(false)

	for i := 0; i < 100; i++ {
		if got := p.Pick(); got == nil || got.UID != "u1" {
			t.Fatalf("pick=%+v, want u1 only (u2 pool-disabled)", got)
		}
	}
	// 请求级轮换把 u1 排除后无候选 → 兜底也必须跳过 u2 返回 nil
	if got := p.PickExcluding(map[string]bool{"u1": true}); got != nil {
		t.Fatalf("PickExcluding=%+v, want nil (u2 pool-disabled)", got)
	}
	// 粘性直取
	if got := p.PickByUID("u2"); got != nil {
		t.Fatal("PickByUID(u2) should be nil")
	}
	if got := p.PickByUIDForModel("u2", "some-model"); got != nil {
		t.Fatal("PickByUIDForModel(u2) should be nil")
	}
	if got := p.PickByUID("u1"); got == nil {
		t.Fatal("PickByUID(u1) should succeed")
	}
	// 可用集合
	for _, uid := range p.AvailableUIDs() {
		if uid == "u2" {
			t.Fatal("available UIDs contains pool-disabled u2")
		}
	}
	for _, uid := range p.AvailableUIDsForModel("some-model") {
		if uid == "u2" {
			t.Fatal("available UIDs for model contains pool-disabled u2")
		}
	}
	// Status 透出开关；启用账号照常
	if st, ok := p.Status("u2"); !ok || st.PoolEnabled {
		t.Fatalf("status u2 pool_enabled=%v ok=%v, want false", st.PoolEnabled, ok)
	}
	if st, ok := p.Status("u1"); !ok || !st.PoolEnabled {
		t.Fatalf("status u1 pool_enabled=%v ok=%v, want true", st.PoolEnabled, ok)
	}
	// 重新启用后立即回池
	a2.SetPoolEnabled(true)
	if got := p.PickByUID("u2"); got == nil {
		t.Fatal("PickByUID(u2) should succeed after re-enable")
	}
}

package other

import "testing"

// TestReconcileOtherRoutingNewKeysDefaultDisabled 新增 key 必须默认不参与
// 路由（与 amd/openrouter/tokenharbor 同口径），已有 key 保持勾选状态，
// 被移除的 key 清理表项。
func TestReconcileOtherRoutingNewKeysDefaultDisabled(t *testing.T) {
	cur := &Platform{Keys: []string{}}
	next := &Platform{Keys: []string{"k1", "k2"}}
	next.ReconcileRouting(cur)
	if next.RoutingEnabled("k1") {
		t.Fatal("new key k1 should default routing-disabled")
	}
	if next.RoutingEnabled("k2") {
		t.Fatal("new key k2 should default routing-disabled")
	}

	// 勾选 k1 后再追加 k3：k1 保持启用，k3 默认禁用。
	next2 := &Platform{Keys: []string{"k1", "k3"}, KeyRoutingEnabled: map[string]bool{"k1": true}}
	next2.ReconcileRouting(next)
	if !next2.RoutingEnabled("k1") {
		t.Fatal("existing key k1 should stay enabled")
	}
	if next2.RoutingEnabled("k3") {
		t.Fatal("newly added key k3 should default routing-disabled")
	}

	// 移除 k2：表项清理，nil/缺项 = 参与（legacy 语义）。
	next3 := &Platform{Keys: []string{"k1"}, KeyRoutingEnabled: map[string]bool{"k1": true, "k2": false}}
	next3.ReconcileRouting(next)
	if _, ok := next3.KeyRoutingEnabled["k2"]; ok {
		t.Fatal("removed key k2 should be dropped from the routing table")
	}
}

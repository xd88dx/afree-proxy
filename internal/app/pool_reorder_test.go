package app

import (
	"encoding/json"
	"os"
	"testing"
)

// TestReorderAccounts 管理面板拖拽排序契约（POST /admin/api/accounts/reorder）：
// 顺序即池数组顺序（round-robin 轮转起点语义）；CurrentIdx 重映射到原游标账号的
// 新位置；未知 accountId 整体拒绝且池不被半改；未提及的账号按原相对顺序追加尾部；
// 立即落盘。
func TestReorderAccounts(t *testing.T) {
	a1 := &Account{AccountID: "a1", Email: "1@example.com", Status: "active"}
	a2 := &Account{AccountID: "a2", Email: "2@example.com", Status: "active"}
	a3 := &Account{AccountID: "a3", Email: "3@example.com", Status: "active"}
	resetPoolForEnableTest(t, []*Account{a1, a2, a3})
	pool.CurrentIdx = 1 // 游标指向 a2

	// 全量置换：a3 提到最前；游标账号 a2 从 index 1 挪到 index 2，游标跟随
	n, err := reorderAccounts([]string{"a3", "a1", "a2"})
	if err != nil {
		t.Fatalf("reorderAccounts: %v", err)
	}
	if n != 3 {
		t.Fatalf("reorderAccounts n = %d, want 3", n)
	}
	if pool.Accounts[0] != a3 || pool.Accounts[1] != a1 || pool.Accounts[2] != a2 {
		t.Fatalf("order = %s,%s,%s, want a3,a1,a2", pool.Accounts[0].AccountID, pool.Accounts[1].AccountID, pool.Accounts[2].AccountID)
	}
	if pool.CurrentIdx != 2 {
		t.Fatalf("CurrentIdx = %d, want 2 (cursor follows a2)", pool.CurrentIdx)
	}
	// 立即落盘：读回 JSON 首个 accountId 必须是 a3
	raw, err := os.ReadFile(poolPath)
	if err != nil {
		t.Fatalf("read pool file: %v", err)
	}
	var saved AccountPool
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("unmarshal pool file: %v", err)
	}
	if len(saved.Accounts) == 0 || saved.Accounts[0].AccountID != "a3" {
		t.Fatalf("persisted order[0] = %v, want a3", saved.Accounts)
	}

	// 未知 accountId 整体拒绝，池保持原状（不能半改）
	if _, err := reorderAccounts([]string{"a3", "ghost"}); err == nil {
		t.Fatal("unknown accountId must be rejected")
	}
	if pool.Accounts[0] != a3 || pool.Accounts[1] != a1 || pool.Accounts[2] != a2 {
		t.Fatal("pool must stay untouched after a rejected reorder")
	}

	// 空顺序拒绝
	if _, err := reorderAccounts(nil); err == nil {
		t.Fatal("empty order must be rejected")
	}

	// 部分顺序：未提及的账号按原相对顺序追加尾部；游标仍跟随 a2
	if _, err := reorderAccounts([]string{"a1"}); err != nil {
		t.Fatalf("reorderAccounts partial: %v", err)
	}
	if pool.Accounts[0] != a1 || pool.Accounts[1] != a3 || pool.Accounts[2] != a2 {
		t.Fatalf("partial order = %s,%s,%s, want a1,a3,a2 (untouched keep relative order)",
			pool.Accounts[0].AccountID, pool.Accounts[1].AccountID, pool.Accounts[2].AccountID)
	}
	if pool.CurrentIdx != 2 {
		t.Fatalf("CurrentIdx = %d, want 2 (cursor still on a2)", pool.CurrentIdx)
	}
}

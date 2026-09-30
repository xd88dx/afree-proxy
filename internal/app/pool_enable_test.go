package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func poolBool(v bool) *bool { return &v }

func resetPoolForEnableTest(t *testing.T, accounts []*Account) {
	t.Helper()
	oldPool, oldPath, oldDirty := pool, poolPath, poolDirty
	pool = &AccountPool{Accounts: accounts, Keys: []string{}}
	poolPath = t.TempDir() + "/pool.json"
	poolDirty = false
	t.Cleanup(func() {
		pool, poolPath, poolDirty = oldPool, oldPath, oldDirty
	})
}

func TestPoolEnabledDefaultsEnabledAndFiltersRotation(t *testing.T) {
	disabled := &Account{AccountID: "off", Email: "off@example.com", Status: "active", PoolEnabled: poolBool(false)}
	legacy := &Account{AccountID: "legacy", Email: "legacy@example.com", Status: "active"}
	cooldown := &Account{AccountID: "cooldown", Email: "cool@example.com", Status: "cooldown"}
	resetPoolForEnableTest(t, []*Account{disabled, legacy, cooldown})

	if !accountPoolEnabled(legacy) {
		t.Fatal("legacy account without poolEnabled must default to enabled")
	}
	if accountPoolEnabled(disabled) {
		t.Fatal("poolEnabled=false must disable rotation")
	}

	proxyConfigMu.Lock()
	oldStrategy := proxyConfig.Strategy
	proxyConfig.Strategy = "fill"
	proxyConfigMu.Unlock()
	t.Cleanup(func() {
		proxyConfigMu.Lock()
		proxyConfig.Strategy = oldStrategy
		proxyConfigMu.Unlock()
	})

	if got := pickAccount(); got != legacy {
		t.Fatalf("pickAccount() = %v, want legacy account", got)
	}
	if got := pickAccount(); got != legacy {
		t.Fatalf("pickAccount() = %v, want legacy account again", got)
	}

	total, active, cooldownCount, expired := poolStatusCounts()
	if total != 3 || active != 1 || cooldownCount != 1 || expired != 0 {
		t.Fatalf("poolStatusCounts() = %d/%d/%d/%d, want 3/1/1/0", total, active, cooldownCount, expired)
	}
}

func TestPoolEnabledCanBePersistedExplicitly(t *testing.T) {
	path := t.TempDir() + "/pool.json"
	oldPath := poolPath
	poolPath = path
	t.Cleanup(func() { poolPath = oldPath })

	a := &Account{AccountID: "off", Email: "off@example.com", Status: "active", PoolEnabled: poolBool(false)}
	pool = &AccountPool{Accounts: []*Account{a}, Keys: []string{}}
	savePool()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var saved AccountPool
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(saved.Accounts) != 1 || saved.Accounts[0].PoolEnabled == nil || *saved.Accounts[0].PoolEnabled {
		t.Fatalf("persisted poolEnabled = %+v, want explicit false", saved.Accounts)
	}
}

func TestLegacyPoolDefaultsPoolEnabledToTrue(t *testing.T) {
	path := t.TempDir() + "/pool.json"
	raw := `{"accounts":[{"accountId":"legacy","email":"legacy@example.com","status":"active"}],"keys":[]}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldPool, oldPath, oldDirty := pool, poolPath, poolDirty
	pool, poolPath, poolDirty = nil, path, false
	t.Cleanup(func() {
		pool, poolPath, poolDirty = oldPool, oldPath, oldDirty
	})

	p := loadPool()
	if len(p.Accounts) != 1 || p.Accounts[0].PoolEnabled == nil || !*p.Accounts[0].PoolEnabled {
		t.Fatalf("legacy account poolEnabled = %+v, want explicit true", p.Accounts[0].PoolEnabled)
	}
	accounts := ListAccounts()
	if len(accounts) != 1 || accounts[0].PoolEnabled == nil || !*accounts[0].PoolEnabled {
		t.Fatalf("ListAccounts() poolEnabled = %+v, want explicit true", accounts[0].PoolEnabled)
	}
}

func TestAdminAccountSetPoolEnabledDoesNotChangeStatus(t *testing.T) {
	acc := &Account{
		AccountID:   "acct_1",
		Email:       "acct@example.com",
		Status:      "active",
		PoolEnabled: poolBool(true),
	}
	resetPoolForEnableTest(t, []*Account{acc})

	body := `{"accountId":"acct_1","poolEnabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/admin/api/accounts/pool/enabled", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleAdminAccountSetPoolEnabled(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handler status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if acc.Status != "active" {
		t.Fatalf("Status changed to %q; pool toggle must not change account status", acc.Status)
	}
	if acc.PoolEnabled == nil || *acc.PoolEnabled {
		t.Fatalf("PoolEnabled = %+v, want false", acc.PoolEnabled)
	}

	data, err := os.ReadFile(poolPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var saved AccountPool
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(saved.Accounts) != 1 || saved.Accounts[0].PoolEnabled == nil || *saved.Accounts[0].PoolEnabled {
		t.Fatalf("persisted poolEnabled = %+v, want explicit false", saved.Accounts)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/api/accounts", nil)
	listRec := httptest.NewRecorder()
	handleAdminAccounts(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("accounts handler status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Accounts []*Account `json:"accounts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !resp.Success || len(resp.Data.Accounts) != 1 {
		t.Fatalf("accounts response = %s", listRec.Body.String())
	}
	got := resp.Data.Accounts[0]
	if got.PoolEnabled == nil || *got.PoolEnabled {
		t.Fatalf("accounts API poolEnabled = %+v, want false", got.PoolEnabled)
	}
	if got.Status != "active" {
		t.Fatalf("accounts API status = %q, want active", got.Status)
	}
}

// 新增账号默认不参与轮换（三平台统一口径）：addAccount 对未显式携带开关的
// 账号物化为 false，显式传入的状态保留。存量兼容语义（缺字段=启用）只属于
// loadPool 的物化，不影响新添加。
func TestAddAccountDefaultsDisabled(t *testing.T) {
	resetPoolForEnableTest(t, nil)
	addAccount(&Account{AccountID: "fresh", Email: "fresh@example.com", Status: "active"})
	if got := pool.Accounts[0]; got.PoolEnabled == nil || *got.PoolEnabled {
		t.Fatalf("fresh account must default to routing-disabled, got %v", got.PoolEnabled)
	}
	enabled := true
	addAccount(&Account{AccountID: "explicit", Email: "explicit@example.com", Status: "active", PoolEnabled: &enabled})
	if got := pool.Accounts[1]; got.PoolEnabled == nil || !*got.PoolEnabled {
		t.Fatal("explicit PoolEnabled=true must survive addAccount")
	}
}

package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSetLocalNicknameLockRoundTrip（afree 本地补丁，2026-11-17 三键外置后）：
// 手改昵称（locked=true）落盘的是纯凭据文件（nickname_locked 不再落盘——锁的
// 唯一持久层是宿主平台配置），内存锁语义不变（syncNicknames 读内存跳过覆盖）；
// 变更经 AccountPrefObserver 通知宿主落盘；重新 Parse 时锁经 AccountPrefProvider
// 回填恢复（无 Provider 时锁丢失 = 上游部署语义：昵称恒跟随上游）。
func TestSetLocalNicknameLockRoundTrip(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "workbuddy-u1.json")
	a := &Auth{UID: "u1", AccessToken: "at-1", Nickname: "upstream", FilePath: fp}

	observed := make(chan string, 8)
	prevObs := AccountPrefObserver
	AccountPrefObserver = func(uid string) { observed <- uid }
	t.Cleanup(func() { AccountPrefObserver = prevObs })

	// 手改加锁
	if err := a.SetLocalNickname("本地名", true); err != nil {
		t.Fatalf("set local nickname: %v", err)
	}
	if a.Nickname != "本地名" || !a.NicknameLocked() {
		t.Fatalf("in-memory = %q locked=%v, want 本地名/true", a.Nickname, a.NicknameLocked())
	}
	select {
	case uid := <-observed:
		if uid != "u1" {
			t.Fatalf("observer uid=%q, want u1", uid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AccountPrefObserver not notified on SetLocalNickname")
	}

	raw, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Account struct {
			Nickname       string `json:"nickname"`
			NicknameLocked bool   `json:"nickname_locked"`
		} `json:"account"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	// 昵称本身是凭据文件的一部分（上游同步写回的展示形态）照常落盘；
	// nickname_locked 是账号意志，绝不落 vendor 文件。
	if doc.Account.Nickname != "本地名" {
		t.Fatalf("disk nickname = %q, want 本地名", doc.Account.Nickname)
	}
	if doc.Account.NicknameLocked {
		t.Fatalf("auth file is credentials-only; nickname_locked must not be written: %s", raw)
	}

	// 重 Parse 回读：昵称恢复；锁经 Provider 回填（模拟宿主持久层）。
	prevProv := AccountPrefProvider
	AccountPrefProvider = func(uid string) (poolEnabled, accountEnabled *bool, nicknameLocked bool) {
		return nil, nil, true
	}
	t.Cleanup(func() { AccountPrefProvider = prevProv })
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Nickname != "本地名" || !parsed.NicknameLocked() {
		t.Fatalf("parsed = %q locked=%v, want 本地名/true (via provider)", parsed.Nickname, parsed.NicknameLocked())
	}

	// 解锁恢复上游昵称：文件形状不受影响（本就从不写锁键）
	if err := a.SetLocalNickname("upstream", false); err != nil {
		t.Fatalf("restore upstream nickname: %v", err)
	}
	raw2, _ := os.ReadFile(fp)
	var doc2 map[string]any
	if err := json.Unmarshal(raw2, &doc2); err != nil {
		t.Fatal(err)
	}
	acc := doc2["account"].(map[string]any)
	if _, exists := acc["nickname_locked"]; exists {
		t.Fatalf("unlocked account must not carry nickname_locked key: %s", raw2)
	}
	if acc["nickname"] != "upstream" {
		t.Fatalf("restored nickname = %v, want upstream", acc["nickname"])
	}

	// 空昵称拒绝（展示会打回 uid，不允许）
	if err := a.SetLocalNickname("  ", true); err == nil {
		t.Fatal("empty nickname must be rejected")
	}
}

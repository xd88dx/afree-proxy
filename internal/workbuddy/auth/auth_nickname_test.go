package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSetLocalNicknameLockRoundTrip（afree 本地补丁）：手改昵称（locked=true）
// 落盘后重新 Parse 必须原样恢复昵称与锁；恢复跟随上游（locked=false）时
// nickname_locked 键不写入，旧文件形状不受影响。
func TestSetLocalNicknameLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "workbuddy-u1.json")
	a := &Auth{UID: "u1", AccessToken: "at-1", Nickname: "upstream", FilePath: fp}

	// 手改加锁
	if err := a.SetLocalNickname("本地名", true); err != nil {
		t.Fatalf("set local nickname: %v", err)
	}
	if a.Nickname != "本地名" || !a.NicknameLocked() {
		t.Fatalf("in-memory = %q locked=%v, want 本地名/true", a.Nickname, a.NicknameLocked())
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
	if doc.Account.Nickname != "本地名" || !doc.Account.NicknameLocked {
		t.Fatalf("disk = %+v, want nickname=本地名 locked=true", doc.Account)
	}

	// 重 Parse 回读（嵌套形）
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Nickname != "本地名" || !parsed.NicknameLocked() {
		t.Fatalf("parsed = %q locked=%v, want 本地名/true", parsed.Nickname, parsed.NicknameLocked())
	}

	// 解锁恢复上游昵称：锁键不再写入
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

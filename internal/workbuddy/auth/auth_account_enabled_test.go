// 账号总开关（account_enabled，afree 本地补丁）的持久化契约：
// 嵌套形/扁平形 auth 文件的 auth.account_enabled 键均可解析；SaveAtomic 写回后
// 往返保持；未显式设置过的账号不引入新键（零迁移），缺项 = 启用。
package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAccountEnabledBothShapes(t *testing.T) {
	nested := []byte(`{"auth":{"accessToken":"at","refreshToken":"rt","expiresAt":1,"domain":"","account_enabled":false},"account":{"uid":"u1"}}`)
	a, err := Parse(nested)
	if err != nil {
		t.Fatalf("nested parse: %v", err)
	}
	if a.AccountEnabled() {
		t.Fatal("nested account_enabled=false should parse as disabled")
	}

	flat := []byte(`{"accessToken":"at","refreshToken":"rt","expiresAt":1,"domain":"","uid":"u1","account_enabled":false}`)
	a, err = Parse(flat)
	if err != nil {
		t.Fatalf("flat parse: %v", err)
	}
	if a.AccountEnabled() {
		t.Fatal("flat account_enabled=false should parse as disabled")
	}

	missing := []byte(`{"accessToken":"at","refreshToken":"rt","expiresAt":1,"domain":"","uid":"u1"}`)
	a, err = Parse(missing)
	if err != nil {
		t.Fatalf("missing-key parse: %v", err)
	}
	if !a.AccountEnabled() {
		t.Fatal("missing account_enabled should default to enabled")
	}
}

func TestSaveAtomicAccountEnabledRoundtrip(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "workbuddy-u1.json")
	a := &Auth{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 1753600000, UID: "u1", FilePath: fp}
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// 未显式设置过：不引入新键（与插件 OAuth 输出形状一致）
	if strings.Contains(string(raw), "account_enabled") {
		t.Fatalf("untouched account should not gain account_enabled key: %s", raw)
	}

	a.SetAccountEnabled(false)
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("save disabled: %v", err)
	}
	raw, err = os.ReadFile(fp)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if v, ok := doc["auth"]["account_enabled"].(bool); !ok || v {
		t.Fatalf("disabled account should persist auth.account_enabled=false, got: %s", raw)
	}
	b, err := Parse(raw)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if b.AccountEnabled() {
		t.Fatal("roundtrip lost account_enabled=false")
	}
}

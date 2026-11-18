// 账号总开关（account_enabled，afree 本地补丁）的持久化契约（2026-11-17 三键
// 外置后）：auth 文件回归纯凭据——文件里的 account_enabled 键不再解析（宿主
// 平台配置是唯一持久层，解析后经 AccountPrefProvider 按 UID 回填）；SaveAtomic
// 不写三键（旧文件残留键经 JSON 未知字段忽略自然消失）；SetAccountEnabled 变更
// 经 AccountPrefObserver 通知宿主落盘。本文件锁三点：文件键被忽略（不误导）、
// Provider 回填生效、变更通知触发。
package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAccountEnabledBothShapes(t *testing.T) {
	// 旧文件里残留的 account_enabled=false 键不再被解析（文件不是权威）：
	// 未挂 Provider 时内存意志保持零值启用，宿主配置经 Provider 回填后生效。
	nested := []byte(`{"auth":{"accessToken":"at","refreshToken":"rt","expiresAt":1,"domain":"","account_enabled":false},"account":{"uid":"u1"}}`)
	a, err := Parse(nested)
	if err != nil {
		t.Fatalf("nested parse: %v", err)
	}
	if !a.AccountEnabled() {
		t.Fatal("stale file key must be ignored (host config is authoritative)")
	}

	flat := []byte(`{"accessToken":"at","refreshToken":"rt","expiresAt":1,"domain":"","uid":"u1","account_enabled":false}`)
	a, err = Parse(flat)
	if err != nil {
		t.Fatalf("flat parse: %v", err)
	}
	if !a.AccountEnabled() {
		t.Fatal("stale flat key must be ignored")
	}

	// Provider 回填：宿主配置才是账号意志的真身。
	prev := AccountPrefProvider
	disabled := false
	AccountPrefProvider = func(uid string) (poolEnabled, accountEnabled *bool, nicknameLocked bool) {
		if uid != "u1" {
			t.Fatalf("provider called with uid=%q, want u1", uid)
		}
		return nil, &disabled, false
	}
	t.Cleanup(func() { AccountPrefProvider = prev })
	a, err = Parse(flat)
	if err != nil {
		t.Fatalf("provider parse: %v", err)
	}
	if a.AccountEnabled() {
		t.Fatal("provider-provided account_enabled=false must apply after parse")
	}
}

func TestSaveAtomicAccountEnabledRoundtrip(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "workbuddy-u1.json")
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

	// 变更通知（宿主持久层订阅）：SetAccountEnabled(false) 必须触发 Observer。
	observed := make(chan string, 8)
	prevObs := AccountPrefObserver
	AccountPrefObserver = func(uid string) { observed <- uid }
	t.Cleanup(func() { AccountPrefObserver = prevObs })

	a.SetAccountEnabled(false)
	select {
	case uid := <-observed:
		if uid != "u1" {
			t.Fatalf("observer uid=%q, want u1", uid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AccountPrefObserver not notified on SetAccountEnabled(false)")
	}

	// 纯凭据铁律：显式禁用后落盘文件也**不**携带 account_enabled（宿主落自己的配置）。
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("save disabled: %v", err)
	}
	raw, err = os.ReadFile(fp)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	authDoc, _ := doc["auth"].(map[string]any)
	if authDoc == nil {
		t.Fatalf("missing auth section: %s", raw)
	}
	if _, ok := authDoc["account_enabled"]; ok {
		t.Fatalf("auth file is credentials-only; account_enabled must not be written: %s", raw)
	}
	// 内存意志保持 false（选号/任务过滤读内存值，不依赖文件）。
	if a.AccountEnabled() {
		t.Fatal("in-memory account_enabled must stay false after SetAccountEnabled(false)")
	}
}

package app

import (
	"path/filepath"
	"testing"

	"afree-proxy/internal/other"
)

// 展示门控回归：模型目录 = 可服务目录。没有参与路由的凭据时，平台段不得
// 出现在 /v1/models（与请求入口的可路由性闸同判定），否则客户端选中后
// 必然 401/403/503。口径：账号池 = 总闸开 + 反代开关开；key 平台 = 启用
// 列开。

// otherModelList 逐子平台判定：没有启用 key 的子平台整段不输出。
func TestOtherModelListRequiresEnabledKey(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	prev := other.Get()
	prevPath := other.ConfigPath
	other.ConfigPath = filepath.Join(t.TempDir(), ".other-config.json")
	t.Cleanup(func() {
		other.Set(prev)
		other.ConfigPath = prevPath
	})

	build := func(enabled bool) *other.Config {
		return &other.Config{Platforms: []other.Platform{{
			Slug:              "test",
			Name:              "Test",
			Prefix:            "ot",
			BaseURL:           "http://127.0.0.1:1/v1",
			Keys:              []string{"k1"},
			KeyRoutingEnabled: map[string]bool{"k1": enabled},
			Models: other.ModelState{
				Manual:   []string{"m1"},
				Selected: map[string]bool{"m1": true},
			},
		}}}
	}

	other.Set(build(false))
	if got := otherModelList(); len(got) != 0 {
		t.Fatalf("disabled key must hide subplatform: %v", got)
	}

	other.Set(build(true))
	got := otherModelList()
	if len(got) != 1 || got[0]["id"] != "ot:m1" {
		t.Fatalf("enabled key must list selected model: %v", got)
	}
}

// zen 使用具名启用 key 才展示：匿名 public 哨兵在请求入口按不可路由处理，
// 目录继续展示只会让用户选中必 503 的模型。
func TestZenModelListRequiresEnabledKey(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	prev := getZenConfig()
	t.Cleanup(func() {
		c := *prev
		setZenConfig(&c)
	})

	publicOnly := *getZenConfig()
	publicOnly.Keys = []string{"public"}
	publicOnly.Key = "public"
	publicOnly.KeyRoutingEnabled = nil
	setZenConfig(&publicOnly)
	initZenModels()
	if got := zenModelList(); len(got) != 0 {
		t.Fatalf("public-only zen must not list models: %d", len(got))
	}

	named := *getZenConfig()
	named.Keys = []string{"sk-test"}
	named.Key = "sk-test"
	named.KeyRoutingEnabled = nil // nil = 参与路由
	setZenConfig(&named)
	if got := zenModelList(); len(got) == 0 {
		t.Fatal("enabled named key must list zen models")
	}
}

// cline 裸模型名按账号池路由：无 active + 反代开的账号时不出目录。
func TestAPIModelListRequiresRoutableAccount(t *testing.T) {
	savedPool := pool
	t.Cleanup(func() { pool = savedPool })
	off, on := false, true

	pool = &AccountPool{Accounts: []*Account{}}
	if got := apiModelList(); len(got) != 0 {
		t.Fatalf("no account must hide cline catalog: %d", len(got))
	}

	pool = &AccountPool{Accounts: []*Account{{
		AccountID: "a1", Email: "a@test", Status: "active", PoolEnabled: &off,
	}}}
	if got := apiModelList(); len(got) != 0 {
		t.Fatalf("pool switch off must hide cline catalog: %d", len(got))
	}

	pool = &AccountPool{Accounts: []*Account{{
		AccountID: "a2", Email: "b@test", Status: "active", PoolEnabled: &on,
	}}}
	if got := apiModelList(); len(got) == 0 {
		t.Fatal("routable account must list cline catalog")
	}
}

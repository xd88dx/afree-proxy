package other

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// catalogServer 返回一个记录 Authorization 头的假上游，返回两个模型。
func catalogServer(gotAuth *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1","name":"One"},{"id":"m2"},{"id":"m1"}]}`))
	}))
}

// TestFetchModelsUsesFirstEnabledCredential 锁住凭据选择口径：跳过禁用项，
// 用第一个已启用（参与路由）的 key，而不是无条件取 Keys[0]。
func TestFetchModelsUsesFirstEnabledCredential(t *testing.T) {
	var gotAuth string
	srv := catalogServer(&gotAuth)
	defer srv.Close()

	p := &Platform{
		BaseURL: srv.URL,
		Keys:    []string{"sk-disabled", "sk-live", "sk-also-disabled"},
		KeyRoutingEnabled: map[string]bool{
			"sk-disabled":      false,
			"sk-live":          true,
			"sk-also-disabled": false,
		},
	}
	models, err := FetchModels(p)
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if got := strings.TrimPrefix(gotAuth, "Bearer "); got != "sk-live" {
		t.Fatalf("同步使用了凭据 %q，期望 %q（应跳过禁用项，取第一个启用的）", got, "sk-live")
	}
	if len(models) != 2 || models[0].ID != "m1" || models[1].ID != "m2" {
		t.Fatalf("目录解析 = %+v，期望去重后的 [m1 m2]", models)
	}
}

// TestFetchModelsRejectsWhenAllDisabled 回归：全部凭据禁用时必须拒绝，
// 不能悄悄用禁用凭据去拉上游（面板用户报告过这个行为）。
func TestFetchModelsRejectsWhenAllDisabled(t *testing.T) {
	var gotAuth string
	srv := catalogServer(&gotAuth)
	defer srv.Close()

	p := &Platform{
		BaseURL:           srv.URL,
		Keys:              []string{"sk-a", "sk-b"},
		KeyRoutingEnabled: map[string]bool{"sk-a": false, "sk-b": false},
	}
	_, err := FetchModels(p)
	if err == nil {
		t.Fatal("全部凭据禁用时应当报错，实际却成功了")
	}
	if !strings.Contains(err.Error(), "no enabled credential") {
		t.Fatalf("错误 = %q，期望包含 %q", err.Error(), "no enabled credential")
	}
	if gotAuth != "" {
		t.Fatalf("凭据全部禁用时不应发出请求，实际带了 Authorization=%q", gotAuth)
	}
}

// TestFetchModelsMissingRoutingTable 缺项默认参与路由（历史口径）：
// KeyRoutingEnabled 里没登记的 key 视为启用。
func TestFetchModelsMissingRoutingTable(t *testing.T) {
	var gotAuth string
	srv := catalogServer(&gotAuth)
	defer srv.Close()

	p := &Platform{BaseURL: srv.URL, Keys: []string{"sk-legacy"}}
	if _, err := FetchModels(p); err != nil {
		t.Fatalf("未登记路由状态的 key 应视为启用，实际报错: %v", err)
	}
	if got := strings.TrimPrefix(gotAuth, "Bearer "); got != "sk-legacy" {
		t.Fatalf("使用凭据 = %q，期望 %q", got, "sk-legacy")
	}
}

// TestFetchModelsRejectsWhenNoCredential 一个凭据都没有时给出的是
// "先添加凭据"，与"有但全禁用"区分开，面板据此给不同提示。
func TestFetchModelsRejectsWhenNoCredential(t *testing.T) {
	_, err := FetchModels(&Platform{BaseURL: "https://api.example.com/v1"})
	if err == nil {
		t.Fatal("无凭据时应当报错")
	}
	if !strings.Contains(err.Error(), "no key configured") {
		t.Fatalf("错误 = %q，期望包含 %q", err.Error(), "no key configured")
	}
}

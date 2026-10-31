package app

import (
	"testing"
	"time"
)

// injectClineModels 测试注入：modelsCache 是全局的且不再有 seed 兜底
// （2026-10-31 用户定：目录唯一来源是官方 feed），需要条目的测试锁内注入、
// 结束恢复空缓存。注入 ID 用 feed 原样形态（cline-free/* 自带前缀、其余不带）。
func injectClineModels(t *testing.T, ids ...string) {
	t.Helper()
	initModelsCache()
	modelsMu.Lock()
	saved := make(map[string]*ModelInfo, len(modelsCache))
	for k, v := range modelsCache {
		saved[k] = v
	}
	now := time.Now()
	for _, id := range ids {
		modelsCache[id] = &ModelInfo{ID: id, Source: "free", Cost: "free", Status: ModelActive, SyncedAt: now}
	}
	modelsMu.Unlock()
	t.Cleanup(func() {
		modelsMu.Lock()
		modelsCache = saved
		modelsMu.Unlock()
	})
}

// Cline 模型 ID 规范化：feed 自带 cline-free/ 前缀的原样，其余补前缀。
func TestDisplayClineModelID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cline-free/deepseek-v4.1-flash", "cline-free/deepseek-v4.1-flash"},
		{"poolside/laguna-s-2.1:free", "cline-free/poolside/laguna-s-2.1:free"},
		{"z-ai/glm-5.3-flash", "cline-free/z-ai/glm-5.3-flash"},
		{"", ""},
	}
	for _, c := range cases {
		if got := displayClineModelID(c.in); got != c.want {
			t.Errorf("displayClineModelID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 两种展示形态都应还原到 feed 真实 ID。
func TestCanonicalClineModelID(t *testing.T) {
	for _, in := range []string{"poolside/laguna-s-2.1:free", "cline-free/poolside/laguna-s-2.1:free"} {
		if got := canonicalClineModelID(in); got != "poolside/laguna-s-2.1:free" {
			t.Errorf("canonicalClineModelID(%q) = %q", in, got)
		}
	}
}

// modelInClineList 接受两种形态。
func TestModelInClineListAcceptsBothForms(t *testing.T) {
	// 注入条目（测试进程无 live 同步）：z-ai/*、poolside/* 不带前缀，
	// cline-free/* 自带。两种都必须命中。
	injectClineModels(t, "z-ai/glm-5.3-flash", "poolside/laguna-s-2.1:free", "cline-free/deepseek-v4.1-flash")
	if !modelInClineList("z-ai/glm-5.3-flash") {
		t.Error("injected id without prefix must be in list")
	}
	if !modelInClineList("cline-free/z-ai/glm-5.3-flash") {
		t.Error("prefixed id must resolve back to the feed id")
	}
	if !modelInClineList("poolside/laguna-s-2.1:free") {
		t.Error("injected id without prefix must be in list")
	}
	// cline-free/* 原生条目的缓存键自带前缀：展示形态与剥前缀裸名都必须
	// 命中（只查剥前缀形态会漏掉整类原生条目，STRICT 门控误拒）。
	if !modelInClineList("cline-free/deepseek-v4.1-flash") {
		t.Error("native cline-free/* id must be in list")
	}
	if !modelInClineList("deepseek-v4.1-flash") {
		t.Error("bare form of a native cline-free/* entry must be in list")
	}
	if modelInClineList("no/such-model") {
		t.Error("unknown model must not be in list")
	}
}

// normalizeRequestModel 带前缀请求要解析到 feed 真实 ID（出站/记账用真实 ID）：
// cline-free/* 原生条目的真实 ID 自带前缀，z-ai/* poolside/* 等不带。
func TestNormalizeRequestModelResolvesFeedID(t *testing.T) {
	injectClineModels(t, "z-ai/glm-5.3-flash", "cline-free/deepseek-v4.1-flash")
	if got := normalizeRequestModel("cline-free/z-ai/glm-5.3-flash"); got != "z-ai/glm-5.3-flash" {
		t.Errorf("normalizeRequestModel = %q, want the feed id", got)
	}
	if got := normalizeRequestModel("z-ai/glm-5.3-flash"); got != "z-ai/glm-5.3-flash" {
		t.Errorf("normalizeRequestModel(bare) = %q", got)
	}
	if got := normalizeRequestModel("cline-free/deepseek-v4.1-flash"); got != "cline-free/deepseek-v4.1-flash" {
		t.Errorf("normalizeRequestModel(native) = %q, want the prefixed feed id", got)
	}
	if got := normalizeRequestModel("deepseek-v4.1-flash"); got != "cline-free/deepseek-v4.1-flash" {
		t.Errorf("normalizeRequestModel(native bare) = %q, want the prefixed feed id", got)
	}
}

// /v1/models 输出的 Cline 条目必须都带 cline-free/ 前缀。
func TestAPIModelListClineIDsArePrefixed(t *testing.T) {
	savedPool := pool
	on := true
	poolMu.Lock()
	pool = &AccountPool{Accounts: []*Account{{AccountID: "a", PoolEnabled: &on}}}
	poolMu.Unlock()
	defer func() {
		poolMu.Lock()
		pool = savedPool
		poolMu.Unlock()
	}()
	initModelsCache()
	injectClineModels(t, "z-ai/glm-5.3-flash", "cline-free/deepseek-v4.1-flash")
	list := apiModelList()
	if len(list) == 0 {
		t.Fatal("routable account must yield a non-empty cline catalog")
	}
	for _, m := range list {
		id, _ := m["id"].(string)
		if len(id) < 11 || id[:11] != "cline-free/" {
			t.Errorf("cline model id %q must carry the cline-free/ prefix", id)
		}
	}
}

// WorkBuddy 前缀翻译：网关 wbcn:/wbgb: ↔ vendor cn:/global:，双向正确。
func TestWorkbuddyPrefixTranslation(t *testing.T) {
	cases := []struct{ in, want string }{
		{"wbcn:claude-sonnet-4.5", "cn:claude-sonnet-4.5"},
		{"wbgb:claude-sonnet-4.5", "global:claude-sonnet-4.5"},
		{"cn:legacy-model", "cn:legacy-model"}, // 旧前缀原样（vendor 自解析）
		{"global:legacy-model", "global:legacy-model"},
		{"bare-model", "bare-model"},
	}
	for _, c := range cases {
		if got := wbRealmToVendor(c.in); got != c.want {
			t.Errorf("wbRealmToVendor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	back := []struct{ in, want string }{
		{"cn:claude-sonnet-4.5", "wbcn:claude-sonnet-4.5"},
		{"global:claude-sonnet-4.5", "wbgb:claude-sonnet-4.5"},
		{"wbcn:already", "wbcn:already"},
	}
	for _, c := range back {
		if got := wbVendorToRealm(c.in); got != c.want {
			t.Errorf("wbVendorToRealm(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 路由判定必须认新前缀与旧前缀两种形态。
func TestIsWorkBuddyModelAcceptsBothPrefixes(t *testing.T) {
	for _, m := range []string{"wbcn:x", "wbgb:x", "cn:x", "global:x"} {
		if !isWorkBuddyModel(m) {
			t.Errorf("isWorkBuddyModel(%q) must be true", m)
		}
	}
	for _, m := range []string{"bare", "zen:big-pickle", "oprt:openrouter/free", "cline-free/x"} {
		if isWorkBuddyModel(m) {
			t.Errorf("isWorkBuddyModel(%q) must be false", m)
		}
	}
}

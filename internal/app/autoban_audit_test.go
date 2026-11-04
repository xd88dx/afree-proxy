package app

// 2026-11-04 全平台可用性审计回归：openrouter 与 zen 此前没有模型级自动拉黑
// 接线（cline 有 404/计费双哨兵、trae 有模型冷却）——下线/区域死模型目录照
// 列，客户端每次撞墙还把 key 打进冷却链。本测试锁死判定与接线。

import (
	"errors"
	"fmt"
	"testing"

	"afree-proxy/internal/kit"
	"afree-proxy/internal/openrouter"
)

func resetAutobanForTest(t *testing.T) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	resetModelBlacklistForTest(t)
}

func TestOpenRouterAutoBanPermanent(t *testing.T) {
	resetAutobanForTest(t)

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"404 no longer available", &openrouter.HTTPError{Status: 404, Body: `{"message":"poolside/laguna-s-2.1:free is no longer available on OpenRouter: no provider serves this model anymore"}`}, true},
		{"404 unavailable for free", &openrouter.HTTPError{Status: 404, Body: `{"message":"This model is unavailable for free. The paid version is available now"}`}, true},
		{"403 agentic harnesses", &openrouter.HTTPError{Status: 403, Body: `{"message":"thinkingmachines/inkling-small:free is only available on agentic harnesses"}`}, true},
		{"400 chat endpoint unsupported", &openrouter.HTTPError{Status: 400, Body: `{"message":"x is a decisions model and cannot be used with the chat/completions endpoint"}`}, true},
		{"403 rate-limit flavored (key-level)", &openrouter.HTTPError{Status: 403, Body: `{"message":"rate limit exceeded"}`, RateLimited: true}, false},
		{"429", &openrouter.HTTPError{Status: 429, Body: "rate limited", RateLimited: true}, false},
		{"500 transient", &openrouter.HTTPError{Status: 500, Body: "internal"}, false},
		{"non-typed error", errors.New("dial tcp: timeout"), false},
	}
	for _, tc := range cases {
		resetAutobanForTest(t)
		orAutoBanPermanent("poolside/laguna-s-2.1:free", tc.err)
		id := openrouter.ModelPrefix + "poolside/laguna-s-2.1:free"
		got := IsModelBlacklisted(id)
		if got != tc.want {
			t.Errorf("%s: blacklisted=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestZenAutoBanRegion(t *testing.T) {
	resetAutobanForTest(t)

	zm := &ZenModel{ID: "crouton-alpha-free"}
	// RegionError 403 → 拉黑
	zenAutoBanRegion(&zenHTTPError{Status: 403, Body: `{"type":"error","error":{"type":"RegionError","message":"This model is not available in your region"}}`}, zm)
	if !IsModelBlacklisted(zenPrefixedID("crouton-alpha-free")) {
		t.Fatal("RegionError must ban the model")
	}
	// 非 RegionError → 不动
	resetAutobanForTest(t)
	zenAutoBanRegion(&zenHTTPError{Status: 403, Body: `{"error":"session dead"}`}, zm)
	if IsModelBlacklisted(zenPrefixedID("crouton-alpha-free")) {
		t.Fatal("non-region 403 must not ban the model")
	}
	// 非 zenHTTPError → 不动
	resetAutobanForTest(t)
	zenAutoBanRegion(fmt.Errorf("dial tcp: timeout"), zm)
	if IsModelBlacklisted(zenPrefixedID("crouton-alpha-free")) {
		t.Fatal("non-typed error must not ban")
	}
	_ = kit.Truncate // 保持 import 使用
}

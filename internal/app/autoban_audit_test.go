package app

// 2026-11-04 全平台可用性审计回归：zen 此前没有模型级自动拉黑接线（cline 有
// 404/计费双哨兵、trae 有模型冷却）——下线/区域死模型目录照列，客户端每次撞
// 墙还把 key 打进冷却链。本测试锁死判定与接线。（openrouter 的同族测试随
// 2026-11-16 平台删除移除。）

import (
	"fmt"
	"testing"

	"afree-proxy/internal/kit"
)

func resetAutobanForTest(t *testing.T) {
	t.Helper()
	t.Setenv("DATA_DIR", testDataDir(t))
	resetModelBlacklistForTest(t)
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

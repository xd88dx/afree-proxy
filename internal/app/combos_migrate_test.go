package app

import (
	"testing"
)

// 存量 combo trae 旧前缀 target 迁移（2026-11-07 合并域化后）：
// trae:/solo: → tacn:（CN）；已是合并域/非 trae 平台/解析不到：原样保留。
func TestMigrateTraeComboTarget(t *testing.T) {
	newTrae2apiLockOrderFixture(t)

	// 已是合并域 → 不改；非 trae 前缀 → 不改；解析不到 → 原样保留。
	if got, ok := migrateTraeComboTarget("tacn:xcfoo-unresolvable"); ok || got != "tacn:xcfoo-unresolvable" {
		t.Fatalf("tacn: must stay, got %q ok=%v", got, ok)
	}
	if got, ok := migrateTraeComboTarget("cline-free/x"); ok || got != "cline-free/x" {
		t.Fatalf("non-trae must stay, got %q ok=%v", got, ok)
	}
	if got, ok := migrateTraeComboTarget("trae:xcfoo-unresolvable"); ok || got != "trae:xcfoo-unresolvable" {
		t.Fatalf("unresolvable must stay, got %q ok=%v", got, ok)
	}
	// intl 关闭时 tagb 不迁移（fixture 未开 intl）。
	if got, ok := migrateTraeComboTarget("trae-intl:xcfoo-unresolvable"); ok || got != "trae-intl:xcfoo-unresolvable" {
		t.Fatalf("intl-off must stay, got %q ok=%v", got, ok)
	}
}

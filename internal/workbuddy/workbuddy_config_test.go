package workbuddy

import (
	"reflect"
	"testing"
)

// account_share 整体替换特例（2026-11-18 修复）：占比表是「完整集合」语义，
// 深合并对 map 键只增不删——残留旧键独占抽签、空表清空失效。
func TestApplyAccountShareReplace(t *testing.T) {
	t.Run("整体替换：掉出集合的旧键不再残留", func(t *testing.T) {
		merged := map[string]any{"pool": map[string]any{
			"account_share": map[string]any{"A": 60.0, "C": 30.0},
		}}
		incoming := map[string]any{"pool": map[string]any{
			"account_share": map[string]any{"B": 40.0},
		}}
		ApplyAccountShareReplace(merged, incoming)
		got := merged["pool"].(map[string]any)["account_share"].(map[string]any)
		want := map[string]any{"B": 40.0}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("account_share = %v, want %v（旧键 C 必须被清除）", got, want)
		}
	})
	t.Run("空表=清空：键被删除而非保留旧值", func(t *testing.T) {
		merged := map[string]any{"pool": map[string]any{
			"account_share": map[string]any{"A": 60.0},
			"strategy":      "weighted",
		}}
		incoming := map[string]any{"pool": map[string]any{"account_share": map[string]any{}}}
		ApplyAccountShareReplace(merged, incoming)
		pool := merged["pool"].(map[string]any)
		if _, ok := pool["account_share"]; ok {
			t.Fatalf("account_share 键应被删除，仍存在: %v", pool["account_share"])
		}
		if pool["strategy"] != "weighted" {
			t.Fatalf("兄弟键被误伤: %v", pool["strategy"])
		}
	})
	t.Run("未带键=普通配置表单保存：保留磁盘现值", func(t *testing.T) {
		merged := map[string]any{"pool": map[string]any{
			"account_share": map[string]any{"A": 60.0},
		}}
		incoming := map[string]any{"pool": map[string]any{"strategy": "round_robin"}}
		ApplyAccountShareReplace(merged, incoming)
		got := merged["pool"].(map[string]any)["account_share"].(map[string]any)
		if len(got) != 1 || got["A"] != 60.0 {
			t.Fatalf("未带键时 account_share 应保留, got %v", got)
		}
	})
	t.Run("incoming 无 pool 节：整体不动", func(t *testing.T) {
		merged := map[string]any{"pool": map[string]any{"account_share": map[string]any{"A": 60.0}}}
		ApplyAccountShareReplace(merged, map[string]any{"thinking": map[string]any{"enabled": true}})
		got := merged["pool"].(map[string]any)["account_share"].(map[string]any)
		if len(got) != 1 {
			t.Fatalf("无 pool 节时不应触碰 account_share, got %v", got)
		}
	})
}

// SaveWorkbuddyConfig 合并链端到端：深合并保留兄弟键 + account_share 以提交为准。
func TestMergeWorkbuddyConfigMapsShareSemantics(t *testing.T) {
	cur := map[string]any{"pool": map[string]any{
		"account_share":     map[string]any{"A": 60.0, "C": 30.0},
		"strategy":          "weighted",
		"model_rate_filter": 0.2,
	}}
	incoming := map[string]any{"pool": map[string]any{
		"account_share": map[string]any{"B": 40.0},
		"strategy":      "round_robin",
	}}
	merged := MergeWorkbuddyConfigMaps(cur, incoming)
	pool := merged["pool"].(map[string]any)
	if pool["strategy"] != "round_robin" {
		t.Fatalf("strategy 应被覆盖, got %v", pool["strategy"])
	}
	if _, ok := pool["model_rate_filter"]; !ok {
		t.Fatalf("深合并应保留磁盘旧键 model_rate_filter（由调用方 delete）")
	}
	ApplyAccountShareReplace(merged, incoming)
	got := pool["account_share"].(map[string]any)
	if !reflect.DeepEqual(got, map[string]any{"B": 40.0}) {
		t.Fatalf("account_share 应整体替换为提交集合, got %v", got)
	}
}

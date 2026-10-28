package other

import (
	"testing"
	"time"
)

// 全冷却默认拒绝（UNABLE_FORCE 未设，2026-10-28 用户定）；true 时恢复兜底。
func TestPickKeyAllCoolingFollowsUnableForce(t *testing.T) {
	p := &Platform{Slug: "t", Name: "T", Keys: []string{"a", "b"}}
	Cooldown(p, "a", 5*time.Minute)
	Cooldown(p, "b", 5*time.Minute)
	if got := PickKey(p, "round_robin", nil); got != "" {
		t.Fatalf("default must reject when all keys are cooling, got %q", got)
	}
	t.Setenv("UNABLE_FORCE", "true")
	if got := PickKey(p, "round_robin", nil); got != "a" && got != "b" {
		t.Fatalf("UNABLE_FORCE=true must fall back to a cooling key, got %q", got)
	}
}

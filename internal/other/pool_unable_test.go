package other

import (
	"path/filepath"
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

// 冷却落盘回读（2026-10-28 用户定）：Cooldown 后重新加载配置，冷却保留。
func TestCooldownPersistedAcrossReload(t *testing.T) {
	dir := t.TempDir()
	ConfigPath = filepath.Join(dir, "other-config.json")
	t.Cleanup(func() { ConfigPath = "" })
	p := &Platform{Slug: "t", Name: "T", Keys: []string{"a", "b"}}
	Load(ConfigPath)
	Set(&Config{Platforms: []Platform{*p}})
	p = &Get().Platforms[0] // Set 内部是值拷贝，Cooldown 必须作用于池内指针
	Cooldown(p, "a", 5*time.Minute)
	// 模拟重启：重新加载同一文件
	Load(ConfigPath)
	idx := -1
	for i := range Get().Platforms {
		if Get().Platforms[i].Slug == "t" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("platform missing after reload")
	}
	until, ok := Get().Platforms[idx].Cool["a"]
	if !ok || until.Before(time.Now()) {
		t.Fatalf("cooldown must survive reload, ok=%v until=%v", ok, until)
	}
}

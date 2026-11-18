package exitpool

import (
	"testing"
	"time"
)

// TestPlatformCooldownIsolation 白盒：平台级冷却只影响该 kind 的可用性判定，
// 其它平台与无 kind 调用方不受影响；全局层冷却对所有平台生效。
func TestPlatformCooldownIsolation(t *testing.T) {
	mu := &cooldownsMu
	mu.Lock()
	platformCooldowns = map[string]map[int]time.Time{}
	cooldowns = map[int]time.Time{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		platformCooldowns = map[string]map[int]time.Time{}
		cooldowns = map[int]time.Time{}
		mu.Unlock()
	})

	ActivatePool(&PoolConfig{Proxies: []string{"http://a:1", "http://b:2"}, Strategy: "round_robin"})
	t.Cleanup(func() { ActivatePool(&PoolConfig{Strategy: "round_robin"}) })

	CooldownUpstreamProxyFor("wb", 0, 10*time.Minute)
	if ProxyAvailableFor("wb", 0) {
		t.Fatal("wb-kind cooldown must hide exit 0 from wb")
	}
	if !ProxyAvailableFor("zen", 0) {
		t.Fatal("other platforms must be unaffected by wb-kind cooldown")
	}
	if !ProxyAvailableFor("", 0) {
		t.Fatal("no-kind callers only see the global layer")
	}
	CooldownUpstreamProxy(0, 10*time.Minute)
	if ProxyAvailableFor("zen", 0) || ProxyAvailableFor("", 0) {
		t.Fatal("global cooldown must hide the exit from everyone")
	}
}

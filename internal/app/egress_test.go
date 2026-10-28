package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 出口绑定（一 key 一代理）的行为契约：
//   - 每个身份固定一个代理，且同一 namespace 内一个代理只服务一个身份；
//   - 代理不足时多出的身份没有绑定（被 pickZenKey/pickAccountExcluding 过滤）；
//   - 绑定跨重启保留（重新 reconcile 不改变既有分配）——这是"key 不换 IP"的全部意义；
//   - 代理被移出池 → 相关绑定作废，而不是静默换一个新出口；
//   - zen 与 cline 是两个独立 namespace，同一个代理可以各服务一个身份。

func setupEgressTest(t *testing.T, proxies []string) {
	t.Helper()
	t.Setenv("DATA_DIR", t.TempDir())
	// 代理池在 proxy-config.json（proxy_pool_config.go），测试重定向路径并
	// 保存/恢复现池——与 setupBindingTest 同一隔离套路。
	savedPoolPath := proxyConfigPathOverride
	proxyConfigPathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { proxyConfigPathOverride = savedPoolPath })
	savedPool := *getProxyPool()
	t.Cleanup(func() { setProxyPool(&savedPool) })
	setProxyPool(&proxyPoolConfigData{Proxies: proxies, Strategy: "round_robin"})
	egressMu.Lock()
	egressBindings = map[string]egressBinding{}
	egressLoaded = true
	egressMu.Unlock()
	zenKeyMu.Lock()
	zenKeyCool = map[string]time.Time{}
	zenKeyIdx = 0
	zenKeyMu.Unlock()
	zenSessMu.Lock()
	zenSessions = map[string]*zenSessionEntry{}
	zenSessLoaded = true
	zenSessPath = ""
	zenSessSaveBlocked = false
	zenSessMu.Unlock()

	saved := getZenConfig()
	cp := *saved
	cp.Keys = nil
	setZenConfig(&cp)
	t.Cleanup(func() { setZenConfig(saved) })
}

func setEgressKeys(t *testing.T, keys []string) {
	t.Helper()
	cur := getZenConfig()
	cp := *cur
	cp.Keys = keys
	setZenConfig(&cp)
}

// 一个代理只服务一个 key；多出来的 key 没有绑定。
func TestEgressAssignsOneProxyPerKey(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080", "socks5://p2:1080"})
	setEgressKeys(t, []string{"sk-a", "sk-b", "sk-c"})

	p1, ok1 := egressProxyForZen("sk-a")
	p2, ok2 := egressProxyForZen("sk-b")
	_, ok3 := egressProxyForZen("sk-c")
	if !ok1 || !ok2 {
		t.Fatalf("first two keys must be bound, got (%q,%v) (%q,%v)", p1, ok1, p2, ok2)
	}
	if p1 == p2 {
		t.Fatalf("two keys share one proxy: %q", p1)
	}
	if ok3 {
		t.Fatal("third key must stay unbound when only two proxies exist")
	}
}

// 绑定必须稳定：再次 reconcile（重启/配置保存）不改变既有分配。
func TestEgressBindingSurvivesReconcile(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080", "socks5://p2:1080"})
	setEgressKeys(t, []string{"sk-a", "sk-b"})
	before, _ := egressProxyForZen("sk-a")
	for i := 0; i < 5; i++ {
		reconcileEgressBindings()
	}
	after, _ := egressProxyForZen("sk-a")
	if before != after {
		t.Fatalf("binding changed across reconcile: %q -> %q (a key must never switch exit IP)", before, after)
	}
}

// 新增 key 只会用空闲代理，绝不抢占已绑定 key 的出口。
func TestEgressNewKeyTakesFreeProxy(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080", "socks5://p2:1080"})
	setEgressKeys(t, []string{"sk-a"})
	pa, _ := egressProxyForZen("sk-a")

	setEgressKeys(t, []string{"sk-a", "sk-b"})
	pb, ok := egressProxyForZen("sk-b")
	if !ok || pb == pa {
		t.Fatalf("new key must take the remaining proxy: a=%q b=%q ok=%v", pa, pb, ok)
	}
	if again, _ := egressProxyForZen("sk-a"); again != pa {
		t.Fatalf("existing key moved to %q (was %q)", again, pa)
	}
}

// 代理被移出池：绑定作废（key 变回"无出口"），不得悄悄换到别的代理。
func TestEgressDropsBindingWhenProxyRemoved(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080", "socks5://p2:1080"})
	setEgressKeys(t, []string{"sk-a", "sk-b"})
	pa, _ := egressProxyForZen("sk-a")

	// 移除 sk-a 所在的那个代理
	keep := "socks5://p1:1080"
	if pa == keep {
		keep = "socks5://p2:1080"
	}
	setProxyPool(&proxyPoolConfigData{Proxies: []string{keep}, Strategy: "round_robin"}) // 会触发 reconcile

	if p, ok := egressProxyForZen("sk-a"); ok {
		t.Fatalf("key whose proxy was removed must be unbound, got %q", p)
	}
}

// pickZenKey 必须跳过没有出口的 key（代理不足时），绝不退回直连。
func TestPickZenKeySkipsKeysWithoutEgress(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080"})
	setEgressKeys(t, []string{"sk-a", "sk-b", "sk-c"})
	bound, ok := egressProxyForZen("sk-a")
	if !ok {
		t.Fatal("sk-a should be bound to the single proxy")
	}

	seen := map[string]bool{}
	for i := 0; i < 12; i++ {
		k := pickZenKey()
		if k == "" {
			t.Fatal("pickZenKey returned empty while one key has an egress")
		}
		seen[k] = true
	}
	if len(seen) != 1 || !seen["sk-a"] {
		t.Fatalf("only the bound key may be picked, saw %v (proxy %q)", seen, bound)
	}
}

// 代理全部用尽时 pickZenKey 返回 ""，调用方据此报容量错误。
func TestPickZenKeyEmptyWhenNoEgressAvailable(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080"})
	setEgressKeys(t, []string{"sk-a", "sk-b"})
	// 人为让唯一已绑定的 key 失效：把代理从池里移走
	setProxyPool(&proxyPoolConfigData{Proxies: []string{"socks5://other:1080"}, Strategy: "round_robin"})
	// 新代理会分配给 sk-a（启动/保存时 reconcile），所以这里直接清空绑定
	egressMu.Lock()
	egressBindings = map[string]egressBinding{}
	egressMu.Unlock()

	if k := pickZenKey(); k != "" {
		t.Fatalf("pickZenKey must return empty when no key has an egress, got %q", k)
	}
}

// 绑定关闭（或没有代理）时恢复旧的轮转行为，不能把整个池判死。
func TestEgressDisabledFallsBackToRotation(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080", "socks5://p2:1080"})
	setEgressKeys(t, []string{"sk-a", "sk-b", "sk-c"})
	cur := getZenConfig()
	cp := *cur
	cp.PinProxyPerKey = boolPtr(false)
	setZenConfig(&cp)

	seen := map[string]bool{}
	for i := 0; i < 12; i++ {
		seen[pickZenKey()] = true
	}
	if !seen["sk-a"] || !seen["sk-b"] || !seen["sk-c"] {
		t.Fatalf("with pinning off all keys must rotate, saw %v", seen)
	}
}

// 空代理池 = 直连：绑定策略自动失效，否则默认配置会让 zen 链路全断。
func TestEgressInactiveWithoutProxies(t *testing.T) {
	setupEgressTest(t, nil)
	setEgressKeys(t, []string{"sk-a", "sk-b"})
	if egressBindingActive() {
		t.Fatal("binding must be inactive when no proxies are configured (direct mode)")
	}
	if k := pickZenKey(); k == "" {
		t.Fatal("pickZenKey must still hand out a key in direct mode")
	}
}

// 回归：面板 Test（pinKey）与 ZEN_PIN_KEY 绕过 pickZenKey 的过滤。
// 未绑定的 key 必须在这里报错，绝不能以直连发出——直连会给该 key 记下
// 一个额外 IP，正是整套机制要消除的指纹。
func TestZenEgressRefusesUnboundKeyNeverDirect(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080"})
	setEgressKeys(t, []string{"sk-bound", "sk-unbound"})
	if _, ok := egressProxyForZen("sk-bound"); !ok {
		t.Fatal("sk-bound should own the single proxy")
	}
	if _, ok := egressProxyForZen("sk-unbound"); ok {
		t.Fatal("sk-unbound should be unbound with only one proxy")
	}
	if proxyURL, pidx, ok := boundExitFor("sk-unbound"); ok {
		t.Fatalf("unbound key must be refused under pinning, got proxy %q idx %d (a direct send leaks a new IP)", proxyURL, pidx)
	}
	// 已绑定的 key 仍正常走自己的代理
	if p, _, ok := boundExitFor("sk-bound"); !ok || p != "socks5://p1:1080" {
		t.Fatalf("bound key must use its proxy: p=%q ok=%v", p, ok)
	}
}

// 绑定表持久化：不写明文 key，重启后同一 key 仍是同一代理。
func TestEgressBindingsPersistWithoutPlaintextKeys(t *testing.T) {
	setupEgressTest(t, []string{"socks5://p1:1080", "socks5://p2:1080"})
	const secret = "not-a-real-key-assertion-only"
	setEgressKeys(t, []string{secret, "sk-b"})
	want, _ := egressProxyForZen(secret)

	// 绑定表现落在 proxy-config.json 的 egressBindings 段
	proxyFileMu.Lock()
	sec := loadProxyConfigFileLocked().EgressBindings
	proxyFileMu.Unlock()
	if len(sec) == 0 {
		t.Fatal("egress bindings section not written to proxy-config.json")
	}
	data, err := json.Marshal(sec)
	if err != nil {
		t.Fatal(err)
	}
	if containsBytes(data, []byte(secret)) {
		t.Fatalf("binding table leaks the key in plaintext: %s", data)
	}

	// 模拟重启：清空内存，从磁盘重新加载
	egressMu.Lock()
	egressBindings = map[string]egressBinding{}
	egressLoaded = false
	egressMu.Unlock()
	got, ok := egressProxyForZen(secret)
	if !ok || got != want {
		t.Fatalf("binding did not survive reload: got %q want %q (ok=%v)", got, want, ok)
	}
}

// （TestEgressNamespacesAreIndependent 已随 cline egress namespace 一并删除：
// reconcileEgressBindings 只分配 zen，cline 出口绑定走账号级 ProxyMain/ProxyBackup
// 体系，cline namespace 无任何写入方，属死通路。2026-10-29 审计清理。）

func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// 数值自检：默认配置文件里没有该键时按"开启"解释（旧配置升级路径）。
func TestPinProxyPerKeyDefaultsOnForLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	path := filepath.Join(dir, "zen-config.json")
	if err := os.WriteFile(path, []byte(`{"keys":["sk-legacy"],"baseURL":"https://opencode.ai/inference/v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := loadZenConfig()
	if cfg.PinProxyPerKey == nil || !*cfg.PinProxyPerKey {
		t.Fatal("legacy config without pinProxyPerKey must default to enabled")
	}
}

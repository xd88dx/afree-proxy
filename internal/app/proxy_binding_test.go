package app

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// setupZenSessionTest 清空会话粘性表（本地铸造时代替代 setupHarvestTest 的
// 会话表清理职责；DATA_DIR 由 setupBindingTest 负责）。
func setupZenSessionTest(t *testing.T) {
	t.Helper()
	zenSessMu.Lock()
	zenSessions = map[string]*zenSessionEntry{}
	zenSessLoaded = true
	zenSessPath = ""
	zenSessMu.Unlock()
}

// zenTestConfig 是测试专用的配置字面量：zen 段字段 + 2026-10-26 拆出去的
// 代理池字段，让测试可以在一处描述整个环境。setupBindingTest 会分别落到
// .zen-config.json 与 .proxy-pool.json。生产代码请用 zenConfigData /
// proxyPoolConfigData。（Go 1.27 之前不允许在复合字面量里写内嵌结构的提升
// 字段，所以这里显式复制 zenConfigData 的字段，而不是内嵌。）
type zenTestConfig struct {
	Key                  string
	Keys                 []string
	BaseURL              string
	MaxConcurrency       int
	Retries              int
	Compaction           zenCompactConfig
	ProxyAliases         map[string]string
	ProxyIsolation       *bool
	BackupProxyEnabled   *bool
	ProxyExclusions      map[string][]string
	ProxyBindCaps        map[string]int
	KeyBindings          map[string]proxyBinding
	KeyRoutingEnabled    map[string]bool
	SessionRotateMinutes int

	Proxies             []string
	ProxyStrategy       string
	ProxySubURL         string
	ProxySubCron        string
	ZenUseProxies       *bool
	WorkbuddyUseProxies *bool
}

func (c *zenTestConfig) zen() *zenConfigData {
	return &zenConfigData{
		Key:                  c.Key,
		Keys:                 c.Keys,
		BaseURL:              c.BaseURL,
		MaxConcurrency:       c.MaxConcurrency,
		Retries:              c.Retries,
		Compaction:           c.Compaction,
		ProxyAliases:         c.ProxyAliases,
		KeyBindings:          c.KeyBindings,
		KeyRoutingEnabled:    c.KeyRoutingEnabled,
		SessionRotateMinutes: c.SessionRotateMinutes,
	}
}

// binding 隔离/绑定段（proxy-config.json）的测试构造。
func (c *zenTestConfig) binding() *proxyBindingConfigData {
	return &proxyBindingConfigData{
		ProxyIsolation:     c.ProxyIsolation,
		BackupProxyEnabled: c.BackupProxyEnabled,
		ProxyExclusions:    c.ProxyExclusions,
		ProxyBindCaps:      c.ProxyBindCaps,
	}
}

func (c *zenTestConfig) pool(base proxyPoolConfigData) *proxyPoolConfigData {
	base.Proxies = c.Proxies
	base.Strategy = c.ProxyStrategy
	base.SubURL = c.ProxySubURL
	base.SubCron = c.ProxySubCron
	base.ZenUseProxies = c.ZenUseProxies
	base.WorkbuddyUseProxies = c.WorkbuddyUseProxies
	if base.Strategy == "" {
		base.Strategy = "round_robin"
	}
	return &base
}

// setupBindingTest 把 DATA_DIR 重定向到临时目录（setZenConfig 的落盘写进临时
// 目录而不是仓库根），保存/恢复 zen 配置与出口代理池全局。
func setupBindingTest(t *testing.T, cfg *zenTestConfig) {
	t.Helper()
	dir, err := os.MkdirTemp("", "binding-test")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("DATA_DIR", dir)
	savedProxyPath := proxyConfigPathOverride
	proxyConfigPathOverride = filepath.Join(dir, "proxy-config.json")
	t.Cleanup(func() { proxyConfigPathOverride = savedProxyPath })
	savedZen := getZenConfig()
	savedPool := *getProxyPool()
	savedBinding := *getProxyBindingConfig()
	if cfg != nil {
		setZenConfig(cfg.zen())
		setProxyPool(cfg.pool(*getProxyPool()))
		setProxyBindingConfig(cfg.binding())
	}
	t.Cleanup(func() {
		setZenConfig(savedZen)
		setProxyPool(&savedPool)
		setProxyBindingConfig(&savedBinding)
	})
}

// clearTestProxyPool 清空出口代理池（让测试请求走直连），测试结束恢复。
func clearTestProxyPool(t *testing.T) {
	t.Helper()
	saved := *getProxyPool()
	empty := saved
	empty.Proxies = nil
	setProxyPool(&empty)
	t.Cleanup(func() { setProxyPool(&saved) })
}

// swapTestPool 临时替换账号池全局，测试结束恢复。
func swapTestPool(t *testing.T, accounts []*Account) {
	t.Helper()
	poolMu.Lock()
	saved := pool
	pool = &AccountPool{Accounts: accounts}
	poolMu.Unlock()
	t.Cleanup(func() {
		poolMu.Lock()
		pool = saved
		poolMu.Unlock()
	})
}

func setProxyCooldownIdx(t *testing.T, idx int, d time.Duration) {
	t.Helper()
	cooldownUpstreamProxy(idx, d)
	t.Cleanup(func() {
		proxyCooldownsMu.Lock()
		delete(proxyCooldowns, idx)
		proxyCooldownsMu.Unlock()
	})
}

func boolPtr(b bool) *bool { return &b }

func TestProxyIsolationEnabledDefaults(t *testing.T) {
	// 缺省（nil 字段）= 启用
	setupBindingTest(t, testZenCfg(nil))
	if !proxyIsolationEnabled() {
		t.Fatal("isolation must default to enabled when ProxyIsolation is nil")
	}
	// 显式关闭
	setupBindingTest(t, testZenCfg(boolPtr(false)))
	if proxyIsolationEnabled() {
		t.Fatal("isolation must be disabled when ProxyIsolation=false")
	}
	// env=true：强制开启，覆盖面板的显式关闭
	t.Setenv("PROXY_ISOLATION", "true")
	setupBindingTest(t, testZenCfg(boolPtr(false)))
	if !proxyIsolationEnabled() {
		t.Fatal("PROXY_ISOLATION=true must override persisted false")
	}
	// env=false：默认关闭，但面板显式设置过（非 nil）时面板优先
	t.Setenv("PROXY_ISOLATION", "false")
	setupBindingTest(t, testZenCfg(nil))
	if proxyIsolationEnabled() {
		t.Fatal("PROXY_ISOLATION=false must default to disabled when panel has no explicit value")
	}
	setupBindingTest(t, testZenCfg(boolPtr(true)))
	if !proxyIsolationEnabled() {
		t.Fatal("PROXY_ISOLATION=false must not override an explicit panel true")
	}
	// 非法值（只接受 true/false）：告警并按未设置处理 → 默认启用
	t.Setenv("PROXY_ISOLATION", "yes")
	setupBindingTest(t, testZenCfg(nil))
	if !proxyIsolationEnabled() {
		t.Fatal("invalid PROXY_ISOLATION value must be treated as unset (default enabled)")
	}
	if proxyIsolationEnvLocked() {
		t.Fatal("invalid PROXY_ISOLATION value must not lock the panel toggle")
	}
}

func testZenCfg(isolation *bool) *zenTestConfig {
	return &zenTestConfig{
		Keys:           []string{"sk-test"},
		BaseURL:        zenAPIBase,
		ProxyStrategy:  "round_robin",
		MaxConcurrency: 8,
		Retries:        3,
		ProxyIsolation: isolation,
	}
}

func TestPickBoundProxyMainThenBackup(t *testing.T) {
	// 备用代理开关显式打开才存在"主不通走备"的兜底
	setupBindingTest(t, &zenTestConfig{
		Keys:               []string{"sk-test"},
		Proxies:            []string{"http://a:1", "http://b:2"},
		BackupProxyEnabled: boolPtr(true),
	})
	// 主可用 → 主
	if u, idx, ok := pickBoundProxy("http://a:1", "http://b:2"); !ok || u != "http://a:1" || idx != 0 {
		t.Fatalf("main expected, got %q idx=%d ok=%v", u, idx, ok)
	}
	// 主冷却 → 备
	setProxyCooldownIdx(t, 0, time.Minute)
	if u, idx, ok := pickBoundProxy("http://a:1", "http://b:2"); !ok || u != "http://b:2" || idx != 1 {
		t.Fatalf("backup expected, got %q idx=%d ok=%v", u, idx, ok)
	}
	// 双冷却 → 不可用（调用方必须跳过该身份）
	setProxyCooldownIdx(t, 1, time.Minute)
	if _, _, ok := pickBoundProxy("http://a:1", "http://b:2"); ok {
		t.Fatal("both cooling must report unavailable")
	}
}

func TestPickBoundProxyStaleBinding(t *testing.T) {
	// 主代理已从池中删除：绑定视为不可用（绝不静默回退直连/其他出口）
	setupBindingTest(t, &zenTestConfig{
		Keys:               []string{"sk-test"},
		Proxies:            []string{"http://b:2"},
		BackupProxyEnabled: boolPtr(true),
	})
	if u, idx, ok := pickBoundProxy("http://removed:9", "http://b:2"); !ok || u != "http://b:2" || idx != 0 {
		t.Fatalf("stale main should fall to backup, got %q idx=%d ok=%v", u, idx, ok)
	}
	if _, _, ok := pickBoundProxy("http://removed:9", "http://also-removed:9"); ok {
		t.Fatal("both stale must report unavailable")
	}
	// 双空 = 未绑定：也报不可用（boundProxiesRoutable 才区分未绑定语义）
	if _, _, ok := pickBoundProxy("", ""); ok {
		t.Fatal("empty pair must report unavailable")
	}
}

func TestBoundProxiesRoutableUnboundSemantics(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:    []string{"sk-test"},
		Proxies: []string{"http://a:1"},
	})
	if !boundProxiesRoutable("", "") {
		t.Fatal("unbound identity must count as routable")
	}
	setProxyCooldownIdx(t, 0, time.Minute)
	if boundProxiesRoutable("http://a:1", "") {
		t.Fatal("bound exit cooling must count as not routable")
	}
}

func TestPickAccountSkipsBoundBlockedAccounts(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:    []string{"sk-test"},
		Proxies: []string{"http://a:1", "http://b:2"},
	})
	accA := &Account{AccountID: "a", Email: "a@x", Status: "active", APIToken: "sk_test", ProxyMain: "http://a:1", ProxyBackup: "http://b:2"}
	accB := &Account{AccountID: "b", Email: "b@x", Status: "active"}
	swapTestPool(t, []*Account{accA, accB})

	// 双出口都冷却：绑定账号被跳过，未绑定账号仍可选
	setProxyCooldownIdx(t, 0, time.Minute)
	setProxyCooldownIdx(t, 1, time.Minute)
	acc := pickAccount()
	if acc == nil || acc.AccountID != "b" {
		t.Fatalf("expected unbound account b, got %+v", acc)
	}

	// B 也进入冷却、绑定账号仍被隔离挡住 → 无号可选
	poolMu.Lock()
	accB.Status = "cooldown"
	accB.CooldownUntil = time.Now().Add(time.Hour)
	poolMu.Unlock()
	if acc := pickAccount(); acc != nil {
		t.Fatalf("expected nil when every account is blocked/cooling, got %+v", acc)
	}

	// 关闭隔离开关：回到旧规则，绑定字段被完全忽略（A 重新可选）
	setupBindingTest(t, &zenTestConfig{
		Keys:           []string{"sk-test"},
		Proxies:        []string{"http://a:1", "http://b:2"},
		ProxyIsolation: boolPtr(false),
	})
	if acc := pickAccount(); acc == nil {
		t.Fatal("legacy mode must ignore bindings and pick any active account")
	}
}

func TestPickZenKeySkipsBoundBlockedKeys(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:           []string{"sk-1", "sk-2"},
		Proxies:        []string{"http://a:1", "http://b:2"},
		KeyBindings:    map[string]proxyBinding{"sk-1": {Main: "http://a:1", Backup: "http://b:2"}},
		ProxyIsolation: boolPtr(true),
	})
	zenKeyMu.Lock()
	zenKeyIdx = 0
	zenKeyMu.Unlock()

	setProxyCooldownIdx(t, 0, time.Minute)
	setProxyCooldownIdx(t, 1, time.Minute)
	if k := pickZenKey(); k != "sk-2" {
		t.Fatalf("expected unbound key sk-2, got %q", k)
	}

	// 全部 key 都被挡住 → 空串（调用方报"无可用出口"，绝不静默直连）
	setupBindingTest(t, &zenTestConfig{
		Keys:    []string{"sk-1", "sk-2"},
		Proxies: []string{"http://a:1"},
		KeyBindings: map[string]proxyBinding{
			"sk-1": {Main: "http://a:1"},
			"sk-2": {Main: "http://a:1"},
		},
		ProxyIsolation: boolPtr(true),
	})
	setProxyCooldownIdx(t, 0, time.Minute)
	if k := pickZenKey(); k != "" {
		t.Fatalf("expected empty key when all keys are blocked, got %q", k)
	}
}

func TestValidateProxyBinding(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:    []string{"sk-test"},
		Proxies: []string{"http://a:1", "socks5://b:2"},
	})
	if err := validateProxyBinding("http://a:1", "socks5://b:2"); err != nil {
		t.Fatalf("valid pair rejected: %v", err)
	}
	if err := validateProxyBinding("", ""); err != nil {
		t.Fatalf("clearing pair rejected: %v", err)
	}
	if err := validateProxyBinding("http://not-in-pool:1", ""); err == nil {
		t.Fatal("proxy outside pool must be rejected")
	}
	// main == backup：仅在备用代理开关开启（备用槽真正参与路由）时拒绝。关闭态
	// 备用槽被保留但不参与路由，且面板锁成跟随主槽，用户无法主动造出该组合，
	// 放行以免"关闭前 main=a/backup=b，之后把 main 改成 b"这类操作被拒。
	setupBindingTest(t, &zenTestConfig{
		Keys:               []string{"sk-1"},
		Proxies:            []string{"http://a:1"},
		BackupProxyEnabled: boolPtr(true),
	})
	if err := validateProxyBinding("http://a:1", "http://a:1"); err == nil {
		t.Fatal("main == backup must be rejected when backup proxies are enabled")
	}
	setupBindingTest(t, &zenTestConfig{
		Keys:               []string{"sk-1"},
		Proxies:            []string{"http://a:1"},
		BackupProxyEnabled: boolPtr(false),
	})
	if err := validateProxyBinding("http://a:1", "http://a:1"); err != nil {
		t.Fatalf("main == backup must be allowed when backup proxies are disabled: %v", err)
	}
	// 池外代理在任何开关状态下都要拒绝（与备开关无关）
	if err := validateProxyBinding("http://a:1", "http://not-in-pool:1"); err == nil {
		t.Fatal("backup outside pool must be rejected even when backup proxies are disabled")
	}
	// 直连哨兵：主直连合法；主直连 + 直连备合法（UI 联动锁定）；
	// 主直连 + 代理备非法；代理 + 直连兜底合法
	if err := validateProxyBinding("direct", ""); err != nil {
		t.Fatalf("main=direct rejected: %v", err)
	}
	if err := validateProxyBinding("direct", "direct"); err != nil {
		t.Fatalf("main=direct with direct backup rejected: %v", err)
	}
	if err := validateProxyBinding("direct", "http://a:1"); err == nil {
		t.Fatal("main=direct with proxy backup must be rejected")
	}
	if err := validateProxyBinding("http://a:1", "direct"); err != nil {
		t.Fatalf("direct backup rejected: %v", err)
	}
}

func TestPickBoundProxyDirectSentinel(t *testing.T) {
	// 备用槽的直连哨兵属于"兜底"，只有备用代理开关打开时才生效
	setupBindingTest(t, &zenTestConfig{
		Keys:               []string{"sk-test"},
		Proxies:            []string{"http://a:1"},
		BackupProxyEnabled: boolPtr(true),
	})
	// 主 = direct：无论池状态如何都直连
	if u, idx, ok := pickBoundProxy("direct", ""); !ok || u != "" || idx != -1 {
		t.Fatalf("main=direct must yield direct egress, got %q idx=%d ok=%v", u, idx, ok)
	}
	// 主代理冷却 + 备 = direct：直连兜底
	setProxyCooldownIdx(t, 0, time.Minute)
	if u, idx, ok := pickBoundProxy("http://a:1", "direct"); !ok || u != "" || idx != -1 {
		t.Fatalf("direct backup must yield direct egress, got %q idx=%d ok=%v", u, idx, ok)
	}
	// 主代理恢复可用时不走直连兜底（直接清除冷却，避免 1ms 计时竞态）
	proxyCooldownsMu.Lock()
	delete(proxyCooldowns, 0)
	proxyCooldownsMu.Unlock()
	if u, _, ok := pickBoundProxy("http://a:1", "direct"); !ok || u != "http://a:1" {
		t.Fatalf("available main must win over direct backup, got %q ok=%v", u, ok)
	}
}

// TestBackupProxyDisabledIgnoresBackupSlot：备用代理开关默认关闭 —— 主代理不可用
// 时不再尝试备用代理（该身份本轮跳过），也绝不因此退回直连。
func TestBackupProxyDisabledIgnoresBackupSlot(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:    []string{"sk-test"},
		Proxies: []string{"http://a:1", "http://b:2"},
	})
	if backupProxyEnabled() {
		t.Fatal("backup proxy switch must default to off")
	}
	// 主可用 → 主（备用槽被完全忽略）
	if u, idx, ok := pickBoundProxy("http://a:1", "http://b:2"); !ok || u != "http://a:1" || idx != 0 {
		t.Fatalf("main expected, got %q idx=%d ok=%v", u, idx, ok)
	}
	// 主冷却 → 不可用（不尝试备）
	setProxyCooldownIdx(t, 0, time.Minute)
	if u, _, ok := pickBoundProxy("http://a:1", "http://b:2"); ok {
		t.Fatalf("backup must not be tried while the switch is off, got %q", u)
	}
	// 备用槽的直连哨兵同样不生效：关闭备用代理不等于打开直连兜底
	if _, _, ok := pickBoundProxy("http://a:1", "direct"); ok {
		t.Fatal("direct backup must not be used while the switch is off")
	}
	// 主 = direct 的强制直连语义不受开关影响
	if u, _, ok := pickBoundProxy("direct", ""); !ok || u != "" {
		t.Fatalf("main=direct must still egress directly, got %q ok=%v", u, ok)
	}
	// 主代理已从池中删除 → 不可用（不回退备、不回退直连）
	if _, _, ok := pickBoundProxy("http://removed:9", "http://b:2"); ok {
		t.Fatal("stale main must report unavailable instead of falling to backup")
	}
	// routable 判定同步收敛：主不可用即不可路由
	if boundProxiesRoutable("http://a:1", "http://b:2") {
		t.Fatal("pair must not be routable when the main is cooling and the backup switch is off")
	}
	if !boundProxiesRoutable("", "") {
		t.Fatal("unbound identity must still count as routable")
	}
}

func TestSetAccountProxyBinding(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:    []string{"sk-test"},
		Proxies: []string{"http://a:1", "http://b:2"},
	})
	swapTestPool(t, []*Account{{AccountID: "a", Email: "a@x", Status: "active"}})

	if err := setAccountProxyBinding("a", "http://a:1", "http://b:2"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if p := loadPool(); p.Accounts[0].ProxyMain != "http://a:1" {
		t.Fatalf("binding not persisted: %+v", p.Accounts[0])
	}
	// 传空串 = 解除绑定
	if err := setAccountProxyBinding("a", "", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if p := loadPool(); p.Accounts[0].ProxyMain != "" || p.Accounts[0].ProxyBackup != "" {
		t.Fatalf("binding not cleared: %+v", p.Accounts[0])
	}
	// 未知账号
	if err := setAccountProxyBinding("missing", "http://a:1", ""); err == nil {
		t.Fatal("unknown account must error")
	}
}

func TestSetZenKeyProxyBinding(t *testing.T) {
	// 备用代理开关打开才能写入备用槽（关闭时 normalizeBackupSlot 会把备用槽收敛为空）
	setupBindingTest(t, &zenTestConfig{
		Keys:               []string{"sk-1", "sk-2"},
		Proxies:            []string{"http://a:1", "http://b:2"},
		BackupProxyEnabled: boolPtr(true),
	})
	if err := setKeyProxyBinding(0, "http://a:1", "http://b:2"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if b := getZenConfig().KeyBindings["sk-1"]; b.Main != "http://a:1" || b.Backup != "http://b:2" {
		t.Fatalf("binding wrong: %+v", b)
	}
	// 解除
	if err := setKeyProxyBinding(0, "", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := getZenConfig().KeyBindings["sk-1"]; ok {
		t.Fatal("binding not removed")
	}
	// 越界 / 空 key / 校验失败
	if err := setKeyProxyBinding(9, "http://a:1", ""); err == nil {
		t.Fatal("out-of-range index must error")
	}
	setupBindingTest(t, &zenTestConfig{
		Keys:    []string{"sk-1"},
		Proxies: []string{"http://a:1"},
	})
	// normalize 会剔除空 key，这里直接改内存态配置以覆盖函数内的防御分支。
	zenConfigMu.Lock()
	zenConfig.Keys = []string{""}
	zenConfigMu.Unlock()
	if err := setKeyProxyBinding(0, "http://a:1", ""); err == nil {
		t.Fatal("empty key must not be bindable")
	}
}

// fakeSocks5Server 最小 SOCKS5 服务端（无认证，仅 CONNECT），供桥接测试：
// 握手 05 00 → 请求解析（ATYP 1/3/4）→ dial → 隧道透传。
func fakeSocks5Server(t *testing.T, dial func(addr string) (net.Conn, error)) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("socks listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				head := make([]byte, 2)
				if _, err := io.ReadFull(c, head); err != nil {
					return
				}
				methods := make([]byte, int(head[1]))
				if _, err := io.ReadFull(c, methods); err != nil {
					return
				}
				if _, err := c.Write([]byte{0x05, 0x00}); err != nil {
					return
				}
				req := make([]byte, 4)
				if _, err := io.ReadFull(c, req); err != nil {
					return
				}
				var host string
				switch req[3] {
				case 0x01:
					a := make([]byte, 4)
					if _, err := io.ReadFull(c, a); err != nil {
						return
					}
					host = net.IP(a).String()
				case 0x03:
					l := make([]byte, 1)
					if _, err := io.ReadFull(c, l); err != nil {
						return
					}
					d := make([]byte, l[0])
					if _, err := io.ReadFull(c, d); err != nil {
						return
					}
					host = string(d)
				case 0x04:
					a := make([]byte, 16)
					if _, err := io.ReadFull(c, a); err != nil {
						return
					}
					host = net.IP(a).String()
				default:
					return
				}
				p := make([]byte, 2)
				if _, err := io.ReadFull(c, p); err != nil {
					return
				}
				addr := net.JoinHostPort(host, strconv.Itoa(int(p[0])<<8|int(p[1])))
				if req[1] != 0x01 { // 仅支持 CONNECT
					c.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
					return
				}
				up, err := dial(addr)
				if err != nil {
					c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				if _, err := c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
					return
				}
				done := make(chan struct{}, 2)
				go func() { io.Copy(up, c); done <- struct{}{} }()
				go func() { io.Copy(c, up); done <- struct{}{} }()
				<-done
				<-done
			}(c)
		}
	}()
	return ln
}

func TestSocksBridgeTunnelsConnect(t *testing.T) {
	// 目标：一个 TLS httptest 服务；桥 → 假 SOCKS5 → 目标，完整走一遍
	// CONNECT 隧道。CLI 对桥发的就是 CONNECT（opencode 端点全是 https）。
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("through-socks-bridge"))
	}))
	defer target.Close()

	socksLn := fakeSocks5Server(t, func(addr string) (net.Conn, error) {
		return net.Dial("tcp", addr)
	})

	br, err := startSocksBridge(context.Background(), "socks5://"+socksLn.Addr().String())
	if err != nil {
		t.Fatalf("start bridge: %v", err)
	}
	defer br.close()

	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(mustParseURL(t, br.httpProxyURL())),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // httptest 自签证书
		},
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(target.URL)
	if err != nil {
		t.Fatalf("GET via bridge: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "through-socks-bridge" {
		t.Fatalf("unexpected response: %d %q", resp.StatusCode, string(body))
	}
}

func TestSocksBridgeRejectsNonConnect(t *testing.T) {
	// 明文 http 请求不该被隧道化：桥必须拒绝（opencode 端点全是 https，
	// 出现明文请求说明行为异常）。
	socksLn := fakeSocks5Server(t, func(addr string) (net.Conn, error) {
		return net.Dial("tcp", addr)
	})
	br, err := startSocksBridge(context.Background(), "socks5://"+socksLn.Addr().String())
	if err != nil {
		t.Fatalf("start bridge: %v", err)
	}
	defer br.close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(br.httpProxyURL(), "http://"))
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n")
	brd := bufio.NewReader(conn)
	resp, err := http.ReadResponse(brd, nil)
	if err != nil {
		// 桥直接断开（没有回响应）也算拒绝 —— 两种行为都可接受
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("non-CONNECT must be rejected, got %d", resp.StatusCode)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestParseProxyLine(t *testing.T) {
	// 订阅分享格式：socks:// + base64(user:pass) + #别名。
	// 测试向量用合成凭据 —— 真实代理链接（含真实凭据）绝不能进仓库。
	line := "socks://dGVzdHVzZXI6dGVzdHBhc3M=@1.2.3.4:1093#socks5-demo-FR"
	got, alias, err := parseProxyLine(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != "socks5://testuser:testpass@1.2.3.4:1093#socks5-demo-FR" {
		t.Fatalf("canonical wrong: %q", got)
	}
	if alias != "socks5-demo-FR" {
		t.Fatalf("alias wrong: %q", alias)
	}
	// 明文凭据 + socks5 标准格式 + 别名
	got, alias, err = parseProxyLine("socks5://user:pass@1.2.3.4:1080#my-node")
	if err != nil || got != "socks5://user:pass@1.2.3.4:1080#my-node" || alias != "my-node" {
		t.Fatalf("plain creds: %q %q %v", got, alias, err)
	}
	// 无凭据、无别名
	got, alias, err = parseProxyLine("socks5://1.2.3.4:1080")
	if err != nil || got != "socks5://1.2.3.4:1080" || alias != "" {
		t.Fatalf("no creds: %q %q %v", got, alias, err)
	}
	// http 透传，fragment 随行保留
	got, alias, err = parseProxyLine("http://1.2.3.4:8080#FR-1")
	if err != nil || got != "http://1.2.3.4:8080#FR-1" || alias != "FR-1" {
		t.Fatalf("http: %q %q %v", got, alias, err)
	}
	// 非法协议
	if _, _, err := parseProxyLine("ss://xxx@1.2.3.4:443"); err == nil {
		t.Fatal("unsupported scheme must error")
	}
	// 缺端口
	if _, _, err := parseProxyLine("socks5://1.2.3.4"); err == nil {
		t.Fatal("missing port must error")
	}
	// 空行
	if _, _, err := parseProxyLine("   "); err != nil {
		t.Fatalf("blank line must not error: %v", err)
	}
	// base64 解码结果不含冒号时按明文用户名处理
	got, _, err = parseProxyLine("socks://cGxhaW51c2Vy@1.2.3.4:1080")
	if err != nil || got != "socks5://cGxhaW51c2Vy@1.2.3.4:1080" {
		t.Fatalf("non-splitting b64: %q %v", got, err)
	}
}

func TestProxyAliasLivesInPoolLine(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{
		Keys:    []string{"sk-test"},
		Proxies: []string{"socks5://u:p@1.2.3.4:1080#old-alias"},
	})
	// 别名随行持久化：面板保存时行回带 #fragment，别名自然保留；
	// 追加新行（带别名）互不影响
	body := `{"proxies":["socks5://u:p@1.2.3.4:1080#old-alias","socks://dGVzdHVzZXI6dGVzdHBhc3M=@5.6.7.8:1093#new-alias"]}`
	req := httptest.NewRequest("POST", "/admin/api/opencode/config/update", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update failed: %d %s", rec.Code, rec.Body.String())
	}
	cfg := getProxyPool()
	if len(cfg.Proxies) != 2 ||
		cfg.Proxies[0] != "socks5://u:p@1.2.3.4:1080#old-alias" ||
		cfg.Proxies[1] != "socks5://testuser:testpass@5.6.7.8:1093#new-alias" {
		t.Fatalf("aliases must persist inside pool lines: %v", cfg.Proxies)
	}
	// 行去掉 #fragment = 明确清除该代理的别名
	body = `{"proxies":["socks5://u:p@1.2.3.4:1080","socks5://testuser:testpass@5.6.7.8:1093#new-alias"]}`
	req = httptest.NewRequest("POST", "/admin/api/opencode/config/update", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update failed: %d", rec.Code)
	}
	if got := getProxyPool().Proxies[0]; got != "socks5://u:p@1.2.3.4:1080" {
		t.Fatalf("fragment-less line must clear the alias: %q", got)
	}
}

func TestPickZenKeyStrategyUnified(t *testing.T) {
	setupZenSessionTest(t)
	setupBindingTest(t, &zenTestConfig{
		Keys: []string{"sk-1", "sk-2", "sk-3"},
	})
	resetGlobalConfigForTest(t)
	setCfg := func(strategy string) { setGlobalStrategy(strategy) }
	resetCursor := func() {
		zenKeyMu.Lock()
		zenKeyIdx = 0
		zenKeyMu.Unlock()
	}

	// fill：永远选池序第一个可用 key（与 Cline 账号池 fill 语义一致）
	setCfg("fill")
	for i := 0; i < 5; i++ {
		if k := pickZenKey(); k != "sk-1" {
			t.Fatalf("fill must always pick the first pool key, got %q", k)
		}
	}

	// random：30 次抽取应覆盖全部 key
	setCfg("random")
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		seen[pickZenKey()] = true
	}
	for _, k := range []string{"sk-1", "sk-2", "sk-3"} {
		if !seen[k] {
			t.Fatalf("random must be able to pick %q, seen=%v", k, seen)
		}
	}

	// round_robin：游标依次轮转
	setCfg("round_robin")
	resetCursor()
	for i, want := range []string{"sk-1", "sk-2", "sk-3", "sk-1"} {
		if k := pickZenKey(); k != want {
			t.Fatalf("round_robin pick %d = %q, want %q", i, k, want)
		}
	}
}

func TestBoundProxyNetworkErrorDoesNotPoisonAccount(t *testing.T) {
	// 回归：隔离开 + 账号绑定代理 + Cline 上游开关为直连（useProxies=false）时，
	// 绑定代理的网络错误必须只冷却代理、绝不毒化账号（曾错误地走直连分支
	// 给账号上 5 分钟冷却，导致备用代理重试也不会发生）。
	setupBindingTest(t, &zenTestConfig{
		Proxies:        []string{"http://127.0.0.1:1"}, // 不可达端口 = 必然网络错误
		ProxyIsolation: boolPtr(true),
	})
	swapTestPool(t, []*Account{{
		AccountID: "a", Email: "a@x", Status: "active", APIToken: "sk_test",
		ProxyMain: "http://127.0.0.1:1",
	}})
	// 假上游：网络错误发生在代理拨号层，上游不该被碰到
	clineAPIBaseBak := clineAPIBase
	clineAPIBase = "http://127.0.0.1:1"
	t.Cleanup(func() { clineAPIBase = clineAPIBaseBak })

	_, _, err := callClineAPI(context.Background(), map[string]any{
		"model":    "m",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, false, false) // useProxies=false：全局开关为直连
	if err == nil {
		t.Fatal("expected error from unreachable bound proxy")
	}
	poolMu.Lock()
	st := pool.Accounts[0].Status
	until := pool.Accounts[0].CooldownUntil
	poolMu.Unlock()
	if st != "active" || !until.IsZero() {
		t.Fatalf("bound proxy network error must not cool the account, got status=%s until=%v", st, until)
	}
}

// TestResolveBackupSlotPreservesStored 备用代理开关关闭时保留已持久化的备用槽
// （而不是清空）：面板把备用槽锁成只读并跟随主槽，提交上来的值只是显示值，
// 清空会让"关一下开关"变成不可逆的数据丢失，重开开关也无法复用。
func TestResolveBackupSlotPreservesStored(t *testing.T) {
	// 开关开启：采纳提交值
	setupBindingTest(t, &zenTestConfig{
		Keys: []string{"sk-1"}, Proxies: []string{"http://a:1", "http://b:2"},
		BackupProxyEnabled: boolPtr(true),
	})
	if got := resolveBackupSlot("http://b:2", "http://c:3"); got != "http://b:2" {
		t.Fatalf("enabled: got %q, want submitted value", got)
	}
	// 开关关闭：忽略提交值，保留旧备用槽
	setupBindingTest(t, &zenTestConfig{
		Keys: []string{"sk-1"}, Proxies: []string{"http://a:1", "http://b:2"},
		BackupProxyEnabled: boolPtr(false),
	})
	if got := resolveBackupSlot("http://a:1", "http://b:2"); got != "http://b:2" {
		t.Fatalf("disabled: got %q, want stored backup preserved", got)
	}
	// 关闭态清空提交也不能抹掉旧备用槽
	if got := resolveBackupSlot("", "http://b:2"); got != "http://b:2" {
		t.Fatalf("disabled + empty submitted: got %q, want stored backup preserved", got)
	}
	// 此前没有备用槽：保持空（不会凭空造出绑定）
	if got := resolveBackupSlot("http://a:1", ""); got != "" {
		t.Fatalf("no stored backup: got %q, want empty", got)
	}
}

// TestSetZenKeyProxyBindingBackupToggleRoundTrip 端到端覆盖用户描述的场景：
// 开关开 → 设 main=a/backup=b；关开关 → 只改 main，backup 仍是 b；
// 重开开关 → b 立刻重新生效。全程同一个配置实例，只翻 BackupProxyEnabled，
// 模拟真实的"面板保存开关"而不是重建配置。
func TestSetZenKeyProxyBindingBackupToggleRoundTrip(t *testing.T) {
	setBackupSwitch := func(on bool) {
		next := *getProxyBindingConfig()
		next.BackupProxyEnabled = boolPtr(on)
		setProxyBindingConfig(&next)
	}
	setupBindingTest(t, &zenTestConfig{
		Keys: []string{"sk-1"}, Proxies: []string{"http://a:1", "http://b:2", "http://c:3"},
		BackupProxyEnabled: boolPtr(true),
	})

	// 1. 开关开启：主备正常持久化
	if err := setKeyProxyBinding(0, "http://a:1", "http://b:2"); err != nil {
		t.Fatalf("set with backup on: %v", err)
	}
	if b := getZenConfig().KeyBindings["sk-1"]; b.Main != "http://a:1" || b.Backup != "http://b:2" {
		t.Fatalf("binding wrong: %+v", b)
	}

	// 2. 关开关后只改主槽：面板提交 backup = 跟随显示的主值，备用槽应维持 b
	setBackupSwitch(false)
	if err := setKeyProxyBinding(0, "http://c:3", "http://c:3"); err != nil {
		t.Fatalf("set main while backup disabled: %v", err)
	}
	b := getZenConfig().KeyBindings["sk-1"]
	if b.Main != "http://c:3" {
		t.Fatalf("main not updated: %+v", b)
	}
	if b.Backup != "http://b:2" {
		t.Fatalf("backup must be preserved while disabled, got %+v", b)
	}

	// 3. 关闭态下把 main 改成 == 旧备值，必须放行（方案 A）
	if err := setKeyProxyBinding(0, "http://b:2", "http://b:2"); err != nil {
		t.Fatalf("main == stored backup must be allowed while disabled: %v", err)
	}
	if got := getZenConfig().KeyBindings["sk-1"].Main; got != "http://b:2" {
		t.Fatalf("main not updated to http://b:2, got %q", got)
	}

	// 4. 关闭态：备用槽不参与路由 —— 主不可用时无兜底（ok=false）
	if _, _, ok := pickBoundProxy("http://gone:1", getZenConfig().KeyBindings["sk-1"].Backup); ok {
		t.Fatal("backup must not act as fallback while backup proxies are disabled")
	}
	// 主可用则正常走主
	if url, _, ok := pickBoundProxy("http://a:1", getZenConfig().KeyBindings["sk-1"].Backup); !ok || url != "http://a:1" {
		t.Fatalf("main must be used while enabled: url=%q ok=%v", url, ok)
	}

	// 5. 重开开关：保留的备值立刻重新生效
	setBackupSwitch(true)
	if _, _, ok := pickBoundProxy("http://gone:1", getZenConfig().KeyBindings["sk-1"].Backup); !ok {
		t.Fatal("re-enabled backup must be usable again")
	}

	// 6. 关闭态清空主槽：旧备值被保留，绑定不被误删（用户要求备值维持）
	setBackupSwitch(false)
	if err := setKeyProxyBinding(0, "", ""); err != nil {
		t.Fatalf("clear main while disabled: %v", err)
	}
	if got := getZenConfig().KeyBindings["sk-1"]; got.Backup != "http://b:2" {
		t.Fatalf("backup must survive a main-only clear while disabled, got %+v", got)
	}
}

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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// setupBindingTest 把 DATA_DIR 重定向到临时目录（setZenConfig 的落盘写进临时
// 目录而不是仓库根），保存/恢复 zen 配置全局。
func setupBindingTest(t *testing.T, cfg *zenConfigData) {
	t.Helper()
	dir, err := os.MkdirTemp("", "binding-test")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("DATA_DIR", dir)
	saved := getZenConfig()
	if cfg != nil {
		setZenConfig(cfg)
	}
	t.Cleanup(func() { setZenConfig(saved) })
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
		zenProxyCooldownsMu.Lock()
		delete(zenProxyCooldowns, idx)
		zenProxyCooldownsMu.Unlock()
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
	// env 优先于面板配置
	t.Setenv("PROXY_ISOLATION", "true")
	setupBindingTest(t, testZenCfg(boolPtr(false)))
	if !proxyIsolationEnabled() {
		t.Fatal("PROXY_ISOLATION=true must override persisted false")
	}
	t.Setenv("PROXY_ISOLATION", "false")
	setupBindingTest(t, testZenCfg(boolPtr(true)))
	if proxyIsolationEnabled() {
		t.Fatal("PROXY_ISOLATION=false must override persisted true")
	}
}

func testZenCfg(isolation *bool) *zenConfigData {
	return &zenConfigData{
		Enabled:        true,
		Keys:           []string{"public"},
		BaseURL:        zenAPIBase,
		ProxyStrategy:  "round_robin",
		MaxConcurrency: 8,
		Retries:        3,
		ProxyIsolation: isolation,
	}
}

func TestPickBoundProxyMainThenBackup(t *testing.T) {
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
		Proxies: []string{"http://a:1", "http://b:2"},
	})
	// 主可用 → 主
	if u, idx, ok := pickBoundProxy("http://a:1", "http://b:2"); !ok || u != "http://a:1" || idx != 0 {
		t.Fatalf("main expected, got %q idx=%d ok=%v", u, idx, ok)
	}
	// 主冷却 → 辅
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
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
		Proxies: []string{"http://b:2"},
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
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
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
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
		Proxies: []string{"http://a:1", "http://b:2"},
	})
	accA := &Account{AccountID: "a", Email: "a@x", Status: "active", ProxyMain: "http://a:1", ProxyBackup: "http://b:2"}
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
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
		Proxies:        []string{"http://a:1", "http://b:2"},
		ProxyIsolation: boolPtr(false),
	})
	if acc := pickAccount(); acc == nil {
		t.Fatal("legacy mode must ignore bindings and pick any active account")
	}
}

func TestPickZenKeySkipsBoundBlockedKeys(t *testing.T) {
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"sk-1", "sk-2"},
		Proxies:        []string{"http://a:1", "http://b:2"},
		KeyBindings:    map[string]zenProxyBinding{"sk-1": {Main: "http://a:1", Backup: "http://b:2"}},
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
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"sk-1", "sk-2"},
		Proxies: []string{"http://a:1"},
		KeyBindings: map[string]zenProxyBinding{
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
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
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
	if err := validateProxyBinding("http://a:1", "http://a:1"); err == nil {
		t.Fatal("main == backup must be rejected")
	}
	// 直连哨兵：主直连合法；主直连 + 直连辅合法（UI 联动锁定）；
	// 主直连 + 代理辅非法；代理 + 直连兜底合法
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
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
		Proxies: []string{"http://a:1"},
	})
	// 主 = direct：无论池状态如何都直连
	if u, idx, ok := pickBoundProxy("direct", ""); !ok || u != "" || idx != -1 {
		t.Fatalf("main=direct must yield direct egress, got %q idx=%d ok=%v", u, idx, ok)
	}
	// 主代理冷却 + 辅 = direct：直连兜底
	setProxyCooldownIdx(t, 0, time.Minute)
	if u, idx, ok := pickBoundProxy("http://a:1", "direct"); !ok || u != "" || idx != -1 {
		t.Fatalf("direct backup must yield direct egress, got %q idx=%d ok=%v", u, idx, ok)
	}
	// 主代理恢复可用时不走直连兜底（直接清除冷却，避免 1ms 计时竞态）
	zenProxyCooldownsMu.Lock()
	delete(zenProxyCooldowns, 0)
	zenProxyCooldownsMu.Unlock()
	if u, _, ok := pickBoundProxy("http://a:1", "direct"); !ok || u != "http://a:1" {
		t.Fatalf("available main must win over direct backup, got %q ok=%v", u, ok)
	}
}

func TestAssignProxiesEvenly(t *testing.T) {
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
		Proxies: []string{"http://a:1", "http://b:2", "http://c:3"},
	})
	swapTestPool(t, []*Account{
		{AccountID: "a", Email: "a@x", Status: "active"},
		{AccountID: "b", Email: "b@x", Status: "active"},
	})
	if _, err := assignProxiesEvenly(); err != nil {
		t.Fatalf("assign: %v", err)
	}
	p := loadPool()
	if p.Accounts[0].ProxyMain != "http://a:1" || p.Accounts[0].ProxyBackup != "http://b:2" {
		t.Fatalf("account0 binding wrong: %+v", p.Accounts[0])
	}
	if p.Accounts[1].ProxyMain != "http://b:2" || p.Accounts[1].ProxyBackup != "http://c:3" {
		t.Fatalf("account1 binding wrong: %+v", p.Accounts[1])
	}

	// 单代理：主有值、辅为空（无"下一个"可指）
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
		Proxies: []string{"http://a:1"},
	})
	if _, err := assignProxiesEvenly(); err != nil {
		t.Fatalf("assign single: %v", err)
	}
	if p := loadPool(); p.Accounts[0].ProxyMain != "http://a:1" || p.Accounts[0].ProxyBackup != "" {
		t.Fatalf("single-proxy binding wrong: %+v", p.Accounts[0])
	}

	// 空池：明确报错而不是静默清空绑定
	setupBindingTest(t, testZenCfg(nil))
	if _, err := assignProxiesEvenly(); err == nil {
		t.Fatal("empty proxy pool must error")
	}
}

func TestSetAccountProxyBinding(t *testing.T) {
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
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
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"sk-1", "sk-2"},
		Proxies: []string{"http://a:1", "http://b:2"},
	})
	if err := setZenKeyProxyBinding(0, "http://a:1", "http://b:2"); err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg := getZenConfig()
	if b := cfg.KeyBindings["sk-1"]; b.Main != "http://a:1" || b.Backup != "http://b:2" {
		t.Fatalf("binding wrong: %+v", b)
	}
	// 解除
	if err := setZenKeyProxyBinding(0, "", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := getZenConfig().KeyBindings["sk-1"]; ok {
		t.Fatal("binding not removed")
	}
	// 越界 / public key / 校验失败
	if err := setZenKeyProxyBinding(9, "http://a:1", ""); err == nil {
		t.Fatal("out-of-range index must error")
	}
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"public"},
		Proxies: []string{"http://a:1"},
	})
	if err := setZenKeyProxyBinding(0, "http://a:1", ""); err == nil {
		t.Fatal("public key must not be bindable")
	}
}

func TestHarvestSkipsMintWhenBoundProxiesDown(t *testing.T) {
	setupHarvestTest(t)
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"sk-diag"},
		Proxies: []string{"http://a:1"},
		KeyBindings:    map[string]zenProxyBinding{"sk-diag": {Main: "http://a:1"}},
		ProxyIsolation: boolPtr(true),
	})
	setProxyCooldownIdx(t, 0, time.Minute)

	var calls int32
	prev := harvestRunFn
	harvestRunFn = func(ctx context.Context, bin, home, model, proxyURL string) harvestRunResult {
		atomic.AddInt32(&calls, 1)
		return harvestRunResult{ExitCode: 0, Elapsed: time.Millisecond}
	}
	t.Cleanup(func() { harvestRunFn = prev })

	_, err := harvestSession(context.Background(), "sk-diag")
	if err == nil {
		t.Fatal("expected error when bound proxies are unavailable")
	}
	if !strings.Contains(err.Error(), "skipping mint") {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("CLI must not run when bound proxies are unavailable")
	}
}

func TestHarvestPassesBoundProxyToCLI(t *testing.T) {
	setupHarvestTest(t)
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"sk-diag"},
		Proxies: []string{"http://a:1", "http://b:2"},
		KeyBindings:    map[string]zenProxyBinding{"sk-diag": {Main: "http://a:1", Backup: "http://b:2"}},
		ProxyIsolation: boolPtr(true),
	})
	var gotProxy string
	var n int32
	prev := harvestRunFn
	harvestRunFn = func(ctx context.Context, bin, home, model, proxyURL string) harvestRunResult {
		gotProxy = proxyURL
		writeFakeSessionLog(t, home, fmt.Sprintf("ses_fake%d%010d", atomic.AddInt32(&n, 1), time.Now().UnixNano()%1e10))
		return harvestRunResult{ExitCode: 0, Elapsed: time.Millisecond}
	}
	t.Cleanup(func() { harvestRunFn = prev })

	if _, err := harvestSession(context.Background(), "sk-diag"); err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if gotProxy != "http://a:1" {
		t.Fatalf("CLI must mint via bound main proxy, got %q", gotProxy)
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

func TestHarvestSocksBindingUsesBridge(t *testing.T) {
	setupHarvestTest(t)
	setupBindingTest(t, &zenConfigData{
		Enabled: true, Keys: []string{"sk-diag"},
		Proxies: []string{"socks5://s1:1080"},
		KeyBindings:    map[string]zenProxyBinding{"sk-diag": {Main: "socks5://s1:1080"}},
		ProxyIsolation: boolPtr(true),
	})
	var gotProxy string
	var n int32
	prev := harvestRunFn
	harvestRunFn = func(ctx context.Context, bin, home, model, proxyURL string) harvestRunResult {
		gotProxy = proxyURL
		writeFakeSessionLog(t, home, fmt.Sprintf("ses_fake%d%010d", atomic.AddInt32(&n, 1), time.Now().UnixNano()%1e10))
		return harvestRunResult{ExitCode: 0, Elapsed: time.Millisecond}
	}
	t.Cleanup(func() { harvestRunFn = prev })

	if _, err := harvestSession(context.Background(), "sk-diag"); err != nil {
		t.Fatalf("harvest: %v", err)
	}
	// CLI 拿到的必须是本地 http 桥地址，而不是 socks5 原始 URL ——
	// socks5 语义由网关兑现，不赌 CLI 的 socks 支持。
	if !strings.HasPrefix(gotProxy, "http://127.0.0.1:") {
		t.Fatalf("socks binding must surface a local http bridge to the CLI, got %q", gotProxy)
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

func TestZenKeyAutoHarvestEnabled(t *testing.T) {
	setupHarvestTest(t)
	setupBindingTest(t, &zenConfigData{
		Enabled:    true,
		Keys:       []string{"sk-new", "sk-legacy", "sk-off", "sk-on", "public"},
		KeyEnabled: map[string]bool{"sk-off": false, "sk-on": true},
	})
	// 存量迁移兼容：有 minted 会话、无显式记录的 key 视为启用
	zenSessMu.Lock()
	zenSessions["sk-legacy"] = &zenSessionEntry{Session: "sess_x", Minted: true, UA: zenNativeUA}
	zenSessMu.Unlock()
	if !zenKeyAutoHarvestEnabled("sk-legacy") {
		t.Fatal("legacy minted key must stay enabled (migration fallback)")
	}
	// 新 key 无会话、无记录：默认未启用（不再自动铸造）
	if zenKeyAutoHarvestEnabled("sk-new") {
		t.Fatal("new key without session must default to disabled")
	}
	// 显式记录优先于回退
	if zenKeyAutoHarvestEnabled("sk-off") {
		t.Fatal("explicit false must win over migration fallback")
	}
	if !zenKeyAutoHarvestEnabled("sk-on") {
		t.Fatal("explicit true must enable even without a session")
	}
	if zenKeyAutoHarvestEnabled("public") {
		t.Fatal("public key always disabled")
	}
}

func TestProxyAliasLivesInPoolLine(t *testing.T) {
	setupBindingTest(t, &zenConfigData{
		Enabled: true,
		Keys:    []string{"public"},
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
	cfg := getZenConfig()
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
	if got := getZenConfig().Proxies[0]; got != "socks5://u:p@1.2.3.4:1080" {
		t.Fatalf("fragment-less line must clear the alias: %q", got)
	}
}

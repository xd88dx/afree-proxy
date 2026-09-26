package app

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"
)

var (
	zenProxyCount atomic.Uint64

	zenProxyCooldowns   = map[int]time.Time{} // 代理索引 -> 冷却截止
	zenProxyCooldownsMu sync.Mutex
)

// proxyClientCache 按代理 URL 缓存钉定代理的 HTTP 客户端（uTLS Chrome 指纹 + h2）。
// key 为代理 URL；"" 表示直连。zen 与 cline 上游共用，请求级轮转时
// 每次上游尝试显式挑选代理并用对应客户端发出。
var (
	proxyClientCacheMu sync.Mutex
	proxyClientCache   = map[string]*http.Client{}
)

// proxyClientFor 返回钉定到指定代理的 HTTP 客户端（缓存复用）。
// proxyURL 为空时返回直连客户端。
func proxyClientFor(proxyURL string) *http.Client {
	proxyClientCacheMu.Lock()
	defer proxyClientCacheMu.Unlock()
	if c, ok := proxyClientCache[proxyURL]; ok {
		return c
	}
	var transport *http.Transport
	if proxyURL == "" {
		transport = buildTransport(func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			return d.DialContext(ctx, network, addr)
		})
	} else {
		transport = buildTransport(func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialViaProxy(ctx, proxyURL, network, addr)
		})
	}
	c := &http.Client{Transport: transport}
	proxyClientCache[proxyURL] = c
	return c
}

// cooldownUpstreamProxy 标记某出口代理冷却,冷却期内轮询跳过
func cooldownUpstreamProxy(idx int, d time.Duration) {
	if idx < 0 {
		return
	}
	if d <= 0 {
		d = 10 * time.Minute
	}
	if d > maxCooldown {
		d = maxCooldown
	}
	zenProxyCooldownsMu.Lock()
	zenProxyCooldowns[idx] = time.Now().Add(d)
	zenProxyCooldownsMu.Unlock()
}

func zenProxyAvailable(idx int) bool {
	zenProxyCooldownsMu.Lock()
	defer zenProxyCooldownsMu.Unlock()
	until, ok := zenProxyCooldowns[idx]
	if !ok {
		return true
	}
	if time.Now().After(until) {
		delete(zenProxyCooldowns, idx)
		return true
	}
	return false
}

// zenProxyCooldownStatus 返回仍处于冷却的出口及其解除时刻。
// 时刻用 RFC3339（带时区）而不是服务器格式化的 "15:04:05"：面板按浏览器本地
// 时区渲染，服务器格式化只会给出容器时区（UTC）的读数，用户看到的是错的钟点。
func zenProxyCooldownStatus() map[string]string {
	cfg := getZenConfig()
	zenProxyCooldownsMu.Lock()
	defer zenProxyCooldownsMu.Unlock()
	out := map[string]string{}
	for idx, until := range zenProxyCooldowns {
		if idx >= 0 && idx < len(cfg.Proxies) {
			if time.Now().Before(until) {
				out[cfg.Proxies[idx]] = until.UTC().Format(time.RFC3339)
			}
		}
	}
	return out
}

// pickUpstreamProxy 按策略为一次上游尝试选择代理,返回 (代理URL, 索引);
// 无代理配置返回 ("", -1)。跳过冷却中的代理;全部冷却时返回轮转位。
// 由调用方在每次上游尝试时显式调用 —— 请求级轮转（round_robin 一比一）,
// 不依赖连接复用时机。
func pickUpstreamProxy() (string, int) {
	cfg := getZenConfig()
	n := len(cfg.Proxies)
	if n == 0 {
		return "", -1
	}
	idx := int(zenProxyCount.Add(1)-1) % n
	switch cfg.ProxyStrategy {
	case "random":
		idx = int(time.Now().UnixNano() % int64(n))
	case "fill":
		idx = 0
	}
	// 冷却跳过:线性探测下一个可用代理
	for i := 0; i < n; i++ {
		if zenProxyAvailable(idx) {
			break
		}
		idx = (idx + 1) % n
	}
	return cfg.Proxies[idx], idx
}

func maskProxyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("***")
	return u.String()
}

// parseProxyLine 把用户粘贴的代理行规范化为网关标准格式，并提取别名。
//
// 支持的输入：
//   - socks://b64(user:pass)@host:port#别名   （常见订阅分享格式：socks scheme、
//     base64 编码的账号密码、#号后的别名）→ socks5://user:pass@host:port + 别名
//   - socks5(h)://[user:pass@]host:port[#别名]
//   - http(s)://[user:pass@]host:port[#别名]
//
// userinfo 的 base64 解码是启发式的：仅当不含冒号、可成功解码、且解码结果
// 含冒号且全部可打印时才采用，否则按明文用户名处理。
func parseProxyLine(raw string) (canonical string, alias string, err error) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return "", "", nil
	}
	// #fragment 是别名（用户可读标注），不属于代理地址本身
	u, err := url.Parse(line)
	if err != nil {
		return "", "", fmt.Errorf("代理格式无效 %q: %v", line, err)
	}
	alias = strings.TrimSpace(u.Fragment)

	switch u.Scheme {
	case "socks":
		u.Scheme = "socks5"
	case "socks5", "socks5h", "http", "https":
	default:
		return "", "", fmt.Errorf("代理 %q 协议不受支持（支持 http/https/socks5/socks5h，及订阅分享的 socks://）", line)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("代理 %q 缺少 host:port", line)
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return "", "", fmt.Errorf("代理 %q 缺少端口: %v", line, err)
	}

	// userinfo：常见订阅分享把 user:pass 整体 base64 后放在 @ 前
	if u.User != nil {
		info := u.User.String()
		if u.User.Username() != "" && !strings.Contains(info, ":") {
			if decoded, derr := base64.StdEncoding.DecodeString(info); derr == nil && len(decoded) > 0 {
				s := string(decoded)
				if i := strings.IndexByte(s, ':'); i > 0 && !strings.ContainsAny(s, " \t\r\n@") && isPrintableASCII(s) {
					u.User = url.UserPassword(s[:i], s[i+1:])
				}
			}
		}
	}

	// 重建：别名（fragment）随行保留，查询串与路径剥离，统一 scheme
	u.RawQuery = ""
	u.Path = ""
	return u.String(), alias, nil
}

// isPrintableASCII 判断字符串是否全部为可打印 ASCII（base64 误判兜底）。
func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// buildTransport 构造 Bun/BoringSSL 指纹(h1,官方 CLI 实测只用 http/1.1)+
// 可注入拨号的 Transport。注意: Bun 指纹 ALPN 只报 http/1.1,握手协商出 h1,
// 因此不能再 RegisterProtocol("https"→h2),否则 h2 帧解析器会对 h1 明文
// 连接报错(frame too large)。标准 Transport 按 ALPN 自动走 h1。
func buildTransport(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Transport {
	t := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
		// 上游"接受连接后不再回任何字节"时必须能自愈。这条 transport 同时服务
		// zen 与 cline：zen 恒 stream=true（首字节很快），但 cline 的非流式请求
		// 上游要等生成完成才发响应头，长生成合法地可能好几分钟 —— 所以取 5 分钟
		// 这个"远超正常首字节、又远小于永久"的值，而不是 90s 那种会砍掉正常请求
		// 的激进值。此前完全不设，一个挂死的上游能让 zen 的 MaxConcurrency(默认 8)
		// 槽位被永久占满、整条 zen 链路停摆。
		// 只限握手与响应头，不影响 SSE 响应体的长读取。
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
	}
	t.DialContext = dial
	t.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		raw, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			raw.Close()
			return nil, err
		}
		uconn := utls.UClient(raw, &utls.Config{
			ServerName: host,
			NextProtos: []string{"http/1.1"},
		}, utls.HelloCustom)
		if err := uconn.ApplyPreset(bunSpecForConn()); err != nil {
			raw.Close()
			return nil, err
		}
		if err := uconn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		return uconn, nil
	}
	return t
}

// dialViaProxy 统一拨号:http/https 走 CONNECT,socks5 走 SOCKS5 握手
func dialViaProxy(ctx context.Context, raw, network, addr string) (net.Conn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("bad proxy url: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		return dialHTTPProxy(ctx, u, network, addr)
	case "socks5", "socks5h":
		auth := &proxy.Auth{}
		if u.User != nil {
			auth.User = u.User.Username()
			auth.Password, _ = u.User.Password()
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		type ctxDialer interface {
			DialContext(context.Context, string, string) (net.Conn, error)
		}
		if cd, ok := d.(ctxDialer); ok {
			return cd.DialContext(ctx, network, addr)
		}
		// 旧接口无 ctx:包装
		type result struct {
			c   net.Conn
			err error
		}
		ch := make(chan result, 1)
		go func() {
			c, err := d.Dial(network, addr)
			ch <- result{c, err}
		}()
		select {
		case <-ctx.Done():
			// 后台拨号可能已成功: 异步取出并关闭,避免连接泄漏
			go func() {
				if r := <-ch; r.c != nil {
					r.c.Close()
				}
			}()
			return nil, ctx.Err()
		case r := <-ch:
			return r.c, r.err
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
}

// dialHTTPProxy 通过 http(s) 代理建立 CONNECT 隧道
func dialHTTPProxy(ctx context.Context, u *url.URL, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	rawConn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		tlsConn := tls.Client(rawConn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			rawConn.Close()
			return nil, err
		}
		rawConn = tlsConn
	}

	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
	}
	if u.User != nil {
		cred := base64.StdEncoding.EncodeToString([]byte(u.User.String()))
		req.Header.Set("Proxy-Authorization", "Basic "+cred)
	}
	// CONNECT 握手阶段加截止时间: 代理接受 TCP 却不响应 CONNECT 时不能永久
	// 挂起; 客户端取消(ctx.Done)时同步中断。隧道建立后清除截止,不影响后续使用
	rawConn.SetDeadline(time.Now().Add(30 * time.Second))
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			rawConn.Close()
		case <-stop:
		}
	}()
	if err := req.Write(rawConn); err != nil {
		rawConn.Close()
		return nil, err
	}

	br := bufio.NewReader(rawConn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		rawConn.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rawConn.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("proxy CONNECT %s: %s %s", u.Host, resp.Status, strings.TrimSpace(string(b)))
	}
	rawConn.SetDeadline(time.Time{})
	return rawConn, nil
}

package exitpool

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"

	"afree-proxy/internal/kit"
)

// 平台 kind 常量：出口冷却/记账的平台维度取值（与面板凭据页 kind 同口径）。
const (
	KindCline = "cline"
	KindZen   = "zen"
	KindOther = "other"
	KindTrae  = "trae"
	KindWb    = "wb"

	// KindFreepi freepi.ai key 池平台（2026-11-20 接入）。
	KindFreepi = "freepi"
)

// PlatformCooldownConst 平台级冷却时长（2026-10-30 用户定 10 分钟）：隧道后
// 流错误只冷却失败发生的平台。
const PlatformCooldownConst = 10 * time.Minute

// MaxCooldown 冷却时长上限：上游给的间隔（Retry-After / 错误文本解析）
// 可能异常大，兜底上限 24h。
const MaxCooldown = 24 * time.Hour

var (
	rrCounter atomic.Uint64

	cooldowns   = map[int]time.Time{} // 代理索引 -> 冷却截止（全局层：建连失败）
	cooldownsMu sync.Mutex

	// manualCooled 手动冷却（面板「冷却」按钮，2026-11-02 用户定）：
	// 不设到期时间——摘出轮转后只有手动解禁（clearProxyCooldowns）才恢复。
	// 与 timed 冷却并存：手动标记优先（到期与否都不参与轮转）。
	// 2026-11-04 用户定持久化：随 proxy-state.json 的 manualCooled 段落盘/恢复
	// （索引键 ↔ URL 键互转见 SnapshotManualCooled/RestoreManualCooled），
	// 标记与解禁时立即触发同步落盘。
	manualCooled = map[int]bool{}
)

// SnapshotManualCooled 内存索引键 → 落盘 URL 键。越界索引（池编辑后残留）
// 忽略——它们本来就该在下次池编辑清零，不该带进文件。
func SnapshotManualCooled() map[string]bool {
	cfg := GetPool()
	cooldownsMu.Lock()
	defer cooldownsMu.Unlock()
	out := make(map[string]bool, len(manualCooled))
	for idx, on := range manualCooled {
		if on && idx >= 0 && idx < len(cfg.Proxies) {
			out[cfg.Proxies[idx]] = true
		}
	}
	return out
}

// RestoreManualCooled 落盘 URL 键 → 内存索引键。池内不存在的 URL 先按精确
// URL、再按备注名（别名）回退重锚定——与 IdxInPool 同语义：备注名相同即同
// 一代理（2026-10-18 用户定），订阅更新换端口后手动冷却仍能跟到新行。两者
// 都不命中（代理真被移除）丢弃。
func RestoreManualCooled(m map[string]bool) {
	if len(m) == 0 {
		return
	}
	cfg := GetPool()
	idxOf := make(map[string]int, len(cfg.Proxies))
	for i, p := range cfg.Proxies {
		idxOf[p] = i
	}
	cooldownsMu.Lock()
	for url, on := range m {
		if !on {
			continue
		}
		if idx, ok := idxOf[url]; ok {
			manualCooled[idx] = true
			continue
		}
		if a := AliasOf(url); a != "" {
			for i, p := range cfg.Proxies {
				if AliasOf(p) == a {
					manualCooled[i] = true
					break
				}
			}
		}
	}
	n := len(manualCooled)
	cooldownsMu.Unlock()
	if n > 0 {
		log.Printf("proxy health: restored %d manual-cooled exit(s)", n)
	}
}

// platformCooldowns 平台粒度冷却：kind -> idx -> 冷却截止（2026-10-30）。
// 隧道已建通后的流错误（目标平台封出口 IP / 上游问题）只冷却失败发生的平台，
// 绑同一出口的其它平台完全无感——否则个别平台封 IP 会毒化整个出口池。
var platformCooldowns = map[string]map[int]time.Time{}

// clientCache 按代理 URL 缓存钉定代理的 HTTP 客户端（uTLS Chrome 指纹 + h2）。
// key 为代理 URL；"" 表示直连。zen 与 cline 上游共用，请求级轮转时
// 每次上游尝试显式挑选代理并用对应客户端发出。
var (
	clientCacheMu sync.Mutex
	clientCache   = map[string]*http.Client{}
)

// ClientFor 返回钉定到指定代理的 HTTP 客户端（缓存复用，Bun 指纹）。
// proxyURL 为空时返回直连客户端。
func ClientFor(proxyURL string) *http.Client {
	return ClientForProfile(proxyURL, ProfileBun)
}

// ClientForProfile 同 ClientFor，指纹画像可选；缓存键带画像维度。
func ClientForProfile(proxyURL string, hello utls.ClientHelloID) *http.Client {
	key := hello.Client + "/" + hello.Version + "|" + proxyURL
	clientCacheMu.Lock()
	defer clientCacheMu.Unlock()
	if c, ok := clientCache[key]; ok {
		return c
	}
	var transport *http.Transport
	if proxyURL == "" {
		transport = BuildTransportProfile(func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
			return d.DialContext(ctx, network, addr)
		}, hello)
	} else {
		transport = BuildTransportProfile(func(ctx context.Context, network, addr string) (net.Conn, error) {
			return DialViaProxy(ctx, proxyURL, network, addr)
		}, hello)
	}
	c := &http.Client{Transport: transport}
	clientCache[key] = c
	return c
}

// CooldownUpstreamProxy 标记某出口代理**全局**冷却，冷却期内所有平台跳过。
// 只用于建连阶段失败（拨号/CONNECT/SOCKS/TLS）——出口本身不可用，对谁都死。
func CooldownUpstreamProxy(idx int, d time.Duration) {
	if idx < 0 {
		return
	}
	if d <= 0 {
		d = 10 * time.Minute
	}
	if d > MaxCooldown {
		d = MaxCooldown
	}
	cooldownsMu.Lock()
	cooldowns[idx] = time.Now().Add(d)
	cooldownsMu.Unlock()
}

// markProxyManualCooled 手动冷却某出口：无到期时间，只有 clearProxyCooldowns
// 解禁才恢复（代理池重建时一并清空，索引失效后标记无意义）。
// 标记即同步落盘（proxy-state.json 的 manualCooled 段，2026-11-04 用户定）。
func MarkManualCooled(idx int) {
	if idx < 0 {
		return
	}
	cooldownsMu.Lock()
	manualCooled[idx] = true
	cooldownsMu.Unlock()
	if FlushManualHook != nil {
		FlushManualHook()
	}
}

// CooldownUpstreamProxyFor 标记某出口对**单个平台**冷却。
func CooldownUpstreamProxyFor(kind string, idx int, d time.Duration) {
	if kind == "" || idx < 0 {
		return
	}
	if d <= 0 {
		d = 10 * time.Minute
	}
	if d > MaxCooldown {
		d = MaxCooldown
	}
	cooldownsMu.Lock()
	m := platformCooldowns[kind]
	if m == nil {
		m = map[int]time.Time{}
		platformCooldowns[kind] = m
	}
	m[idx] = time.Now().Add(d)
	cooldownsMu.Unlock()
}

// ProxyAvailableFor 出口对 kind 平台当前是否可选：全局冷却 + 平台级冷却两层
// 任一命中即不可选。kind 为空 = 只看全局层（无平台上下文的调用方）。
func ProxyAvailableFor(kind string, idx int) bool {
	if !proxyAvailable(idx) {
		return false
	}
	if kind == "" {
		return true
	}
	cooldownsMu.Lock()
	defer cooldownsMu.Unlock()
	m := platformCooldowns[kind]
	until, ok := m[idx]
	if !ok {
		return true
	}
	if time.Now().After(until) {
		delete(m, idx)
		return true
	}
	return false
}

func proxyAvailable(idx int) bool {
	cooldownsMu.Lock()
	defer cooldownsMu.Unlock()
	// 手动冷却无到期概念：timed 冷却过期与否都不影响手动标记的判定。
	if manualCooled[idx] {
		return false
	}
	until, ok := cooldowns[idx]
	if !ok {
		return true
	}
	if time.Now().After(until) {
		delete(cooldowns, idx)
		return true
	}
	return false
}

// CooldownStatus 返回仍处于冷却的出口及其解除时刻。
// 时刻用 RFC3339（带时区）而不是服务器格式化的 "15:04:05"：面板按浏览器本地
// 时区渲染，服务器格式化只会给出容器时区（UTC）的读数，用户看到的是错的钟点。
func CooldownStatus() map[string]string {
	cfg := GetPool()
	cooldownsMu.Lock()
	defer cooldownsMu.Unlock()
	out := map[string]string{}
	for idx, until := range cooldowns {
		if idx >= 0 && idx < len(cfg.Proxies) {
			if time.Now().Before(until) {
				out[cfg.Proxies[idx]] = until.UTC().Format(time.RFC3339)
			}
		}
	}
	// 手动冷却以空串标记（覆盖同出口的 timed 记录）：无到期时间，面板显示
	// 「手动」而不是一个虚假的恢复时刻。
	for idx, on := range manualCooled {
		if on && idx >= 0 && idx < len(cfg.Proxies) {
			out[cfg.Proxies[idx]] = ""
		}
	}
	return out
}

// CooldownUntil 单个出口的冷却解除时刻。未冷却返回零值。
// 供"出口不可用"类错误文案把恢复时刻讲清楚（否则用户只看到一句 cooling or
// removed，无法判断是节点真挂了还是短暂冷却）。
func CooldownUntil(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	idx := IdxInPool(raw)
	if idx < 0 {
		return time.Time{}
	}
	cooldownsMu.Lock()
	defer cooldownsMu.Unlock()
	// 手动冷却没有恢复时刻：返回零值，调用方的错误文案只说不可用、不编时间。
	if manualCooled[idx] {
		return time.Time{}
	}
	until, ok := cooldowns[idx]
	if !ok || !time.Now().Before(until) {
		return time.Time{}
	}
	return until
}

// pickUpstreamProxy 按策略为一次上游尝试选择代理,返回 (代理URL, 索引);
// 无代理配置返回 ("", -1)。跳过冷却中的代理;全部冷却时返回轮转位。
// 由调用方在每次上游尝试时显式调用 —— 请求级轮转（round_robin 一比一）,
// 不依赖连接复用时机。
// PickUpstreamProxyFor 全局代理池轮转选口（kind 平台视角）：冷却跳过含平台级
// 冷却——某出口被 wb 平台冷却时，其他平台的兜底轮转仍可选中它。
func PickUpstreamProxyFor(kind string) (string, int) {
	cfg := GetPool()
	n := len(cfg.Proxies)
	if n == 0 {
		return "", -1
	}
	idx := int(rrCounter.Add(1)-1) % n
	switch cfg.Strategy {
	case "random":
		idx = int(time.Now().UnixNano() % int64(n))
	case "fill":
		idx = 0
	}
	// 冷却跳过：线性探测下一个可用出口。探测无果时按冷却种类分岔——限时冷却
	// fail-open 返回轮转位（历史口径：探测无果也不能断流）；命中手动冷却
	//（用户显式摘除、无到期时间、只有解禁恢复）则返回空，交调用方按"无可用
	// 出口"fail-closed。否则单出口池点「冷却」对路由零效果，等于控件撒谎。
	for i := 0; i < n; i++ {
		if ProxyAvailableFor(kind, idx) {
			return cfg.Proxies[idx], idx
		}
		idx = (idx + 1) % n
	}
	if proxyManualCooledAt(idx) {
		return "", -1
	}
	return cfg.Proxies[idx], idx
}

// proxyManualCooledAt 该池下标是否处于手动冷却（无到期时间，仅解禁恢复）。
func proxyManualCooledAt(idx int) bool {
	cooldownsMu.Lock()
	defer cooldownsMu.Unlock()
	return manualCooled[idx]
}

// UpstreamForKind 出口选择的三态封装，供各平台未绑定路径统一调用：
// 池空 → ("", -1, true)，调用方按既有口径决定直连兜底（受 DISABLE_DIRECT 约束）；
// 池非空且有可用出口 → (url, idx, true)；池非空但出口全被手动冷却 →
// ("", -1, false)，调用方必须 fail-closed，绝不静默改用被摘除的出口、也不静默转直连。
func UpstreamForKind(kind string) (string, int, bool) {
	if len(GetPool().Proxies) == 0 {
		return "", -1, true
	}
	url, idx := PickUpstreamProxyFor(kind)
	if url == "" {
		return "", -1, false
	}
	return url, idx, true
}

func maskProxyURL(raw string) string { return kit.MaskProxyURL(raw) }

// ParseProxyLine 把用户粘贴的代理行规范化为网关标准格式，并提取别名。
//
// 支持的输入：
//   - socks://b64(user:pass)@host:port#别名   （常见订阅分享格式：socks scheme、
//     base64 编码的账号密码、#号后的别名）→ socks5://user:pass@host:port + 别名
//   - socks5(h)://[user:pass@]host:port[#别名]
//   - http(s)://[user:pass@]host:port[#别名]
//
// userinfo 的 base64 解码是启发式的：仅当不含冒号、可成功解码、且解码结果
// 含冒号且全部可打印时才采用，否则按明文用户名处理。
func ParseProxyLine(raw string) (canonical string, alias string, err error) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return "", "", nil
	}
	// #fragment 是别名（用户可读标注），不属于代理地址本身
	u, err := url.Parse(line)
	if err != nil {
		return "", "", fmt.Errorf("invalid proxy line %q: %v", line, err)
	}
	alias = strings.TrimSpace(u.Fragment)

	switch u.Scheme {
	case "socks":
		u.Scheme = "socks5"
	case "socks5", "socks5h", "http", "https":
	default:
		return "", "", fmt.Errorf("proxy %q: unsupported scheme (supported: http/https/socks5/socks5h, and socks:// from subscriptions)", line)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("proxy %q: missing host:port", line)
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil {
		return "", "", fmt.Errorf("proxy %q: missing port: %v", line, err)
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

// BuildTransport 构造 Bun/BoringSSL 指纹(h1,官方 CLI 实测只用 http/1.1)+
// 可注入拨号的 Transport。注意: Bun 指纹 ALPN 只报 http/1.1,握手协商出 h1,
// 因此不能再 RegisterProtocol("https"→h2),否则 h2 帧解析器会对 h1 明文
// 连接报错(frame too large)。标准 Transport 按 ALPN 自动走 h1。
// 出站 TLS 指纹画像（2026-11-19 用户定）：Bun 指纹是对齐 opencode CLI 的
// 风控伪装，只对 zen/opencode 上游有意义；普通 OpenAI 兼容上游（other 子
// 平台）用 Chrome 指纹即可——agnes 2026-11-18 换证书后其 TLS 栈对 Bun 形状
// ClientHello 的 CertificateVerify 签名验证不过（Chrome/Go 栈均正常，本仓
// utls 消融实验实锤），遂按平台分栈。零值 ClientHelloID = Bun。
var (
	ProfileBun    = utls.ClientHelloID{}
	ProfileChrome = utls.HelloChrome_Auto
)

func BuildTransport(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *http.Transport {
	return BuildTransportProfile(dial, ProfileBun)
}

// BuildTransportProfile 同 BuildTransport，指纹画像可选。Bun 分支维持原有
// HelloCustom+bunSpecForConn 应用路径；其余画像交给 utls 内置模板。
func BuildTransportProfile(dial func(ctx context.Context, network, addr string) (net.Conn, error), hello utls.ClientHelloID) *http.Transport {
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
		isBun := hello == utls.ClientHelloID{}
		uconn := utls.UClient(raw, &utls.Config{
			ServerName: host,
			NextProtos: []string{"http/1.1"},
		}, utls.HelloCustom)
		if isBun {
			if err := uconn.ApplyPreset(bunSpecForConn()); err != nil {
				raw.Close()
				return nil, err
			}
		} else {
			// 内置模板（Chrome 等）自带 ALPN ["h2","http/1.1"]，spec 应用时
			// ALPNExtension.writeToUConn 会覆盖上面的 Config.NextProtos——
			// h1-only transport 读到服务器回的 h2 SETTINGS 帧即报 malformed
			// HTTP response（2026-11-20 tokenharbor.ai 经池出口实证）。取模板
			// spec 钉 ALPN 为 http/1.1 再应用，其余指纹不变；spec 每连接新建，
			// 不触碰 ApplyPreset 文档告诫的共享状态。
			spec, err := utls.UTLSIdToSpec(hello)
			if err != nil {
				raw.Close()
				return nil, err
			}
			for _, ext := range spec.Extensions {
				if alpn, ok := ext.(*utls.ALPNExtension); ok {
					alpn.AlpnProtocols = []string{"http/1.1"}
				}
			}
			if err := uconn.ApplyPreset(&spec); err != nil {
				raw.Close()
				return nil, err
			}
		}
		if err := uconn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		return uconn, nil
	}
	return t
}

// DialViaProxy 统一拨号:http/https 走 CONNECT,socks5 走 SOCKS5 握手
func DialViaProxy(ctx context.Context, raw, network, addr string) (net.Conn, error) {
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

// ClearCooldownsAt 解禁某出口：清全局冷却、手动冷却、全部平台级冷却
// （面板「解禁」按钮的冷却侧原语；strike 计数清理由装配根的 health 域做）。
func ClearCooldownsAt(idx int) {
	if idx < 0 {
		return
	}
	cooldownsMu.Lock()
	delete(cooldowns, idx)
	delete(manualCooled, idx)
	for _, m := range platformCooldowns {
		delete(m, idx)
	}
	cooldownsMu.Unlock()
}

// CooldownsSnapshot 冷却表快照（测试/面板读用；判定路径走 ProxyAvailableFor）。
// 返回副本，调用方改副本不影响真身。
func CooldownsSnapshot() (global map[int]time.Time, platform map[string]map[int]time.Time) {
	cooldownsMu.Lock()
	defer cooldownsMu.Unlock()
	g := make(map[int]time.Time, len(cooldowns))
	for k, v := range cooldowns {
		g[k] = v
	}
	pl := make(map[string]map[int]time.Time, len(platformCooldowns))
	for kind, m := range platformCooldowns {
		cm := make(map[int]time.Time, len(m))
		for k, v := range m {
			cm[k] = v
		}
		pl[kind] = cm
	}
	return g, pl
}

// SetCooldownForTest 测试写入原语：直写一条全局冷却（任意时刻，含已过期）。
func SetCooldownForTest(idx int, until time.Time) {
	cooldownsMu.Lock()
	cooldowns[idx] = until
	cooldownsMu.Unlock()
}

// ResetCooldownsForTest 清空全部冷却表（测试隔离用；生产路径走
// ResetCooldownsForPoolEdit，带落盘语义的池编辑编排）。
func ResetCooldownsForTest() {
	cooldownsMu.Lock()
	cooldowns = map[int]time.Time{}
	manualCooled = map[int]bool{}
	platformCooldowns = map[string]map[int]time.Time{}
	cooldownsMu.Unlock()
}

// PlatformCooldownEntry 单出口的平台级冷却读数（面板健康表随行输出）。
type PlatformCooldownEntry struct {
	Kind  string
	Until time.Time
}

// PlatformCooldowns 返回某出口当前全部生效的平台级冷却（面板展示用读接口；
// 判定路径走 ProxyAvailableFor，不经本表）。
func PlatformCooldowns(idx int) []PlatformCooldownEntry {
	if idx < 0 {
		return nil
	}
	cooldownsMu.Lock()
	defer cooldownsMu.Unlock()
	now := time.Now()
	var out []PlatformCooldownEntry
	for kind, m := range platformCooldowns {
		if until, ok := m[idx]; ok && now.Before(until) {
			out = append(out, PlatformCooldownEntry{Kind: kind, Until: until})
		}
	}
	return out
}

// ResetCooldownsForPoolEdit 池编辑清理链第一步：清空全局/平台级/手动冷却
// （索引会位移，残留就是错位的标记）。
func ResetCooldownsForPoolEdit() {
	cooldownsMu.Lock()
	cooldowns = map[int]time.Time{}
	manualCooled = map[int]bool{}
	cooldownsMu.Unlock()
	cooldownsMu.Lock()
	platformCooldowns = map[string]map[int]time.Time{}
	cooldownsMu.Unlock()
}

// EvictPoolClients 驱逐不在新池列表里的钉定客户端（索引位移后旧客户端
// 连接泄漏）。
func EvictPoolClients(valid map[string]bool) {
	clientCacheMu.Lock()
	for u, cl := range clientCache {
		// 缓存键是 "profile|proxyURL"（ClientForProfile，2026-11-19）——驱逐
		// 判据只看 URL 段，否则池每次编辑会把所有画像的客户端整体驱逐。
		url := u
		if i := strings.Index(u, "|"); i >= 0 {
			url = u[i+1:]
		}
		if !valid[url] {
			delete(clientCache, u)
			if cl != nil {
				cl.CloseIdleConnections()
			}
		}
	}
	clientCacheMu.Unlock()
}

// AliasOf 取代理行的备注名（#fragment）；无备注返回空串。
// 备注名是代理的"身份标识"：订阅/服务端更新导致端口、凭据变化时，
// 备注名相同即视为同一代理（2026-10-18 用户定）。
func AliasOf(line string) string {
	if i := strings.LastIndexByte(line, '#'); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

// IdxInPool 代理在当前池列表中的索引；不在池中（已被删除）返回 -1。
// 行/备注名相同即视为同一代理（订阅更新换端口后绑定行仍能命中）。
func IdxInPool(raw string) int {
	if raw == "" {
		return -1
	}
	cfg := GetPool()
	for i, p := range cfg.Proxies {
		if p == raw {
			return i
		}
	}
	if a := AliasOf(raw); a != "" {
		for i, p := range cfg.Proxies {
			if AliasOf(p) == a {
				return i
			}
		}
	}
	return -1
}

// FlushManualHook 手动冷却落盘钩子（装配根注入 proxy-state.json 的
// manualCooled 段落盘；nil = 不落盘）。health 域 ②-b 出包后转包内直调。
var FlushManualHook func()

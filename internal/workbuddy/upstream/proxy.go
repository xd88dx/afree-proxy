package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"afree-proxy/internal/workbuddy/auth"
)

// ProxyFunc 为账号解析出站代理 URL（空串 = 直连）。ok=false 表示该账号当前
// 没有可用出口，请求必须失败而不是回退到其他出口或直连。
type ProxyFunc func(a *auth.Auth) (proxyURL string, ok bool)

var errNoUsableProxy = errors.New("workbuddy upstream: no usable egress proxy for account")

// baseTransport 返回可克隆的共享 Transport。测试注入自定义 RoundTripper 时
// 返回 nil，此时只有直连可用，显式代理会失败。
func (c *Client) baseTransport() *http.Transport {
	if c.HTTP != nil {
		if tr, ok := c.HTTP.Transport.(*http.Transport); ok {
			return tr
		}
	}
	if c.ChatHTTP != nil {
		if tr, ok := c.ChatHTTP.Transport.(*http.Transport); ok {
			return tr
		}
	}
	return nil
}

// proxyTransportFor 返回账号本次请求使用的 Transport 及其代理 URL。tr 为 nil
// 表示直连/未启用账号代理（此时 URL 恒为空串）。错误分两类：errNoUsableProxy
// = 绑定出口全不可用（无单一可归属的 URL，调用方不观测）；其余 = 配置问题。
// ctx 仅用于 ObserveAccount 钩子透传，不参与出口决策。
func (c *Client) proxyTransportFor(ctx context.Context, a *auth.Auth) (*http.Transport, string, error) {
	if c.ProxyFor == nil {
		return nil, "", nil
	}
	raw, ok := c.ProxyFor(a)
	if !ok {
		c.observeAccount(ctx, a, "")
		return nil, "", errNoUsableProxy
	}
	raw = strings.TrimSpace(raw)
	c.observeAccount(ctx, a, raw)
	if raw == "" {
		return nil, "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, "", fmt.Errorf("invalid egress proxy %q: %w", raw, err)
	}

	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	if c.proxyTransports == nil {
		c.proxyTransports = make(map[string]*http.Transport)
	}
	if tr := c.proxyTransports[raw]; tr != nil {
		return tr, raw, nil
	}
	base := c.baseTransport()
	if base == nil {
		return nil, "", fmt.Errorf("egress proxy %q: base Transport is not cloneable", raw)
	}
	tr := base.Clone()
	tr.Proxy = http.ProxyURL(u)
	c.proxyTransports[raw] = tr
	return tr, raw, nil
}

// httpClientFor 返回短 RPC 使用的 HTTP client（按账号出口钉定）及出口 URL
// （直连为空串），供出口成败观测归属。
func (c *Client) httpClientFor(ctx context.Context, a *auth.Auth) (*http.Client, string, error) {
	tr, raw, err := c.proxyTransportFor(ctx, a)
	if err != nil {
		return nil, "", err
	}
	if tr == nil {
		if c.HTTP == nil {
			return nil, "", errors.New("workbuddy upstream: HTTP client is nil")
		}
		return c.HTTP, "", nil
	}
	cp := http.Client{Transport: tr}
	if c.HTTP != nil {
		cp = *c.HTTP
		cp.Transport = tr
	}
	return &cp, raw, nil
}

// chatClientFor 返回聊天 SSE 使用的 HTTP client（Timeout=0，首字节/空闲由
// upstream 自身的 HeaderTimeout/IdleTimeout 监控）及出口 URL（直连为空串）。
func (c *Client) chatClientFor(ctx context.Context, a *auth.Auth) (*http.Client, string, error) {
	tr, raw, err := c.proxyTransportFor(ctx, a)
	if err != nil {
		return nil, "", err
	}
	if tr == nil {
		if c.chatHTTP() == nil {
			return nil, "", errors.New("workbuddy upstream: chat HTTP client is nil")
		}
		return c.chatHTTP(), "", nil
	}
	base := c.chatHTTP()
	cp := http.Client{Transport: tr, Timeout: 0}
	if base != nil {
		cp = *base
		cp.Transport = tr
	}
	return &cp, raw, nil
}

// observeAccount 账号归属观测（afree 本地扩展）：把 ctx + 账号 + 实际出口
// 交给宿主（网关）写进请求日志。仅观测，不参与出口选择。
func (c *Client) observeAccount(ctx context.Context, a *auth.Auth, exitRaw string) {
	if c.ObserveAccount == nil {
		return
	}
	c.ObserveAccount(ctx, a, exitRaw)
}

// observeProxy 出口成败回报（afree 本地扩展）：只在真实走了代理（raw 非空）
// 且挂了钩子时回调。ctx 取消的过滤交给钩子实现方，这里原样上抛错误。
// hsMs 握手耗时（毫秒）：调用方 Do 前后打点传入；拿不到时传 0（不记握手平均）。
func (c *Client) observeProxy(raw string, err error, hsMs int64) {
	if raw == "" || c.ObserveProxy == nil {
		return
	}
	c.ObserveProxy(raw, err == nil, err, hsMs)
}

func (c *Client) doHTTP(req *http.Request, a *auth.Auth) (*http.Response, error) {
	cl, raw, err := c.httpClientFor(req.Context(), a)
	if err != nil {
		return nil, err
	}
	hsStart := time.Now()
	resp, err := cl.Do(req)
	c.observeProxy(raw, err, time.Since(hsStart).Milliseconds())
	return resp, err
}

// ---- 上游 40d98f9 起的统一出站口（原上游 proxy.go 内容，随 PRESERVE 落本仓）----

// officialConnectionValue 给出该 path 上官方客户端实际发送的 Connection 值。
//
// 上游 2026-10-30 双版本真机抓包（CN 5.9.3 / GL 5.9.4）：官方在每条 RPC 上都
// 显式写了 Connection —— 除 /v3/config 是 close 外，其余全是 keep-alive。
// HTTP/1.1 默认即复用，Go 因此从不发送这一行，但头集合少了它就是少一个字段；
// 风控按白名单查头时，"缺一个官方必发的头"和"多一个官方不发的头"一样显眼。
//
// 没抓到过的 path 返回空串，保持 Go 默认行为，不臆造。
func officialConnectionValue(path string) string {
	switch path {
	case "/v3/config":
		return "close"
	case "/v2/chat/completions", "/v2/chat/busy", "/v2/report":
		return "keep-alive"
	}
	return ""
}

// applyConnectionHeader 把官方那个显式的 Connection 头补回去。
//
// close 不能写进 Header：net/http 的 transferWriter 会按 req.Close 自己写一行
// Connection: close，手写会重复成两行。keep-alive 相反 —— Go 认为它是默认值所以
// 从不发出，必须手写才会出现在线上字节里。
func applyConnectionHeader(req *http.Request) {
	switch officialConnectionValue(req.URL.Path) {
	case "close":
		req.Close = true
	case "keep-alive":
		req.Header.Set("Connection", "keep-alive")
	}
}

// do 是账号感知的唯一出站口（上游 40d98f9 起全部 13 个出站点收敛于此），
// 在上游形态上叠加 afree 的 ProxyFor 按账号钉定出口：ProxyFor 已挂时走
// fail-closed 的账号出口（短 RPC 记账走 doHTTP、chat 走 doChat 带握手观测）；
// 未挂时保持上游语义（ProxyPool 或直连）。
func (c *Client) do(req *http.Request, a *auth.Auth, chat bool) (*http.Response, error) {
	req = normalizeHeaderSpelling(req)
	applyConnectionHeader(req)
	if c.ProxyFor != nil && a != nil {
		if chat {
			return c.doChat(req, a)
		}
		return c.doHTTP(req, a)
	}
	client := c.HTTP
	if chat {
		client = c.chatHTTP()
	}
	if c.ProxyPool == nil || a == nil {
		return client.Do(req)
	}
	return c.ProxyPool.Do(client, req, a.UID, a.Realm())
}

// doChat chat SSE 出站：Timeout=0 的钉定 client + 握手耗时观测（hsStart 拨号
// 前打点，与 cline/zen 路径同口径；此前恒传 0 导致 wb 出口「平均握手延迟」
// 徽标永远无样本）。
func (c *Client) doChat(req *http.Request, a *auth.Auth) (*http.Response, error) {
	cl, raw, err := c.chatClientFor(req.Context(), a)
	if err != nil {
		return nil, err
	}
	hsStart := time.Now()
	resp, err := cl.Do(req)
	c.observeProxy(raw, err, time.Since(hsStart).Milliseconds())
	return resp, err
}

// closeIdleEgress 传输层失败后清空闲连接（上游 chat 失败分支调用）。账号钉定
// 出口时清对应的代理 Transport——上游原形态只清基础 chat Transport，钉定场景
// 清错了对象；直连（ProxyFor 未挂/出口为空）回落基础 Transport，与上游一致。
func (c *Client) closeIdleEgress(a *auth.Auth) {
	target := http.RoundTripper(c.chatHTTP().Transport)
	if c.ProxyFor != nil && a != nil {
		if raw, ok := c.ProxyFor(a); ok {
			raw = strings.TrimSpace(raw)
			if raw != "" {
				c.proxyMu.Lock()
				tr := c.proxyTransports[raw]
				c.proxyMu.Unlock()
				if tr != nil {
					target = tr
				}
			}
		}
	}
	roundTripCloseIdle(target)
}

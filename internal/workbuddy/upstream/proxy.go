package upstream

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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
func (c *Client) proxyTransportFor(a *auth.Auth) (*http.Transport, string, error) {
	if c.ProxyFor == nil {
		return nil, "", nil
	}
	raw, ok := c.ProxyFor(a)
	if !ok {
		return nil, "", errNoUsableProxy
	}
	raw = strings.TrimSpace(raw)
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
//（直连为空串），供出口成败观测归属。
func (c *Client) httpClientFor(a *auth.Auth) (*http.Client, string, error) {
	tr, raw, err := c.proxyTransportFor(a)
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
func (c *Client) chatClientFor(a *auth.Auth) (*http.Client, string, error) {
	tr, raw, err := c.proxyTransportFor(a)
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

// observeProxy 出口成败回报（afree 本地扩展）：只在真实走了代理（raw 非空）
// 且挂了钩子时回调。ctx 取消的过滤交给钩子实现方，这里原样上抛错误。
func (c *Client) observeProxy(raw string, err error) {
	if raw == "" || c.ObserveProxy == nil {
		return
	}
	c.ObserveProxy(raw, err == nil, err)
}

func (c *Client) doHTTP(req *http.Request, a *auth.Auth) (*http.Response, error) {
	cl, raw, err := c.httpClientFor(a)
	if err != nil {
		return nil, err
	}
	resp, err := cl.Do(req)
	c.observeProxy(raw, err)
	return resp, err
}

func (c *Client) doChatHTTP(req *http.Request, a *auth.Auth) (*http.Response, error) {
	cl, raw, err := c.chatClientFor(a)
	if err != nil {
		return nil, err
	}
	resp, err := cl.Do(req)
	c.observeProxy(raw, err)
	return resp, err
}

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

// proxyTransportFor 返回账号本次请求使用的 Transport。nil 表示直连/未启用
// 账号代理，调用方沿用 Client 自带 HTTP/ChatHTTP。
func (c *Client) proxyTransportFor(a *auth.Auth) (*http.Transport, error) {
	if c.ProxyFor == nil {
		return nil, nil
	}
	raw, ok := c.ProxyFor(a)
	if !ok {
		return nil, errNoUsableProxy
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid egress proxy %q: %w", raw, err)
	}

	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()
	if c.proxyTransports == nil {
		c.proxyTransports = make(map[string]*http.Transport)
	}
	if tr := c.proxyTransports[raw]; tr != nil {
		return tr, nil
	}
	base := c.baseTransport()
	if base == nil {
		return nil, fmt.Errorf("egress proxy %q: base Transport is not cloneable", raw)
	}
	tr := base.Clone()
	tr.Proxy = http.ProxyURL(u)
	c.proxyTransports[raw] = tr
	return tr, nil
}

// httpClientFor 返回短 RPC 使用的 HTTP client（按账号出口钉定）。
func (c *Client) httpClientFor(a *auth.Auth) (*http.Client, error) {
	tr, err := c.proxyTransportFor(a)
	if err != nil {
		return nil, err
	}
	if tr == nil {
		if c.HTTP == nil {
			return nil, errors.New("workbuddy upstream: HTTP client is nil")
		}
		return c.HTTP, nil
	}
	cp := http.Client{Transport: tr}
	if c.HTTP != nil {
		cp = *c.HTTP
		cp.Transport = tr
	}
	return &cp, nil
}

// chatClientFor 返回聊天 SSE 使用的 HTTP client（Timeout=0，首字节/空闲由
// upstream 自身的 HeaderTimeout/IdleTimeout 监控）。
func (c *Client) chatClientFor(a *auth.Auth) (*http.Client, error) {
	tr, err := c.proxyTransportFor(a)
	if err != nil {
		return nil, err
	}
	if tr == nil {
		if c.chatHTTP() == nil {
			return nil, errors.New("workbuddy upstream: chat HTTP client is nil")
		}
		return c.chatHTTP(), nil
	}
	base := c.chatHTTP()
	cp := http.Client{Transport: tr, Timeout: 0}
	if base != nil {
		cp = *base
		cp.Transport = tr
	}
	return &cp, nil
}

func (c *Client) doHTTP(req *http.Request, a *auth.Auth) (*http.Response, error) {
	cl, err := c.httpClientFor(a)
	if err != nil {
		return nil, err
	}
	return cl.Do(req)
}

func (c *Client) doChatHTTP(req *http.Request, a *auth.Auth) (*http.Response, error) {
	cl, err := c.chatClientFor(a)
	if err != nil {
		return nil, err
	}
	return cl.Do(req)
}

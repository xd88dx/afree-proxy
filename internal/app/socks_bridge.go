package app

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
)

// ============ 本地 HTTP CONNECT → SOCKS5 桥 ============
//
// 只为收割机的 CLI mint 服务。背景：CLI 的代理只能通过 HTTPS_PROXY 环境变量
// 注入，而它跑在 Bun 上 —— Bun 官方只承诺 http 代理环境变量，socks5:// 是否
// 生效没有文档背书。赌错了的最坏形态不是报错，而是 CLI **静默直连**：会话从
// 服务器本机 IP 铸出，该 key 的代理隔离悄悄失效。
//
// 因此 socks5(h) 绑定不再把 socks URL 直接塞给 CLI，而是由网关在 127.0.0.1
// 上起一个一次性 HTTP CONNECT 代理，把 CONNECT 隧道经网关已有的 SOCKS5 拨号
// 能力（dialViaProxy，与日常上游流量同一条代码路径）转发出去。CLI 只见到它
// 确定支持的 http 代理，socks5 语义由网关自己兑现。桥随本次 mint 生灭。

// isSocksProxy 判断代理 URL 是否为 socks5/socks5h。
func isSocksProxy(raw string) bool {
	return strings.HasPrefix(raw, "socks5://") || strings.HasPrefix(raw, "socks5h://")
}

type socksBridge struct {
	ln     net.Listener
	target string // socks5(h)://... 绑定代理
	ctx    context.Context
}

// startSocksBridge 在 127.0.0.1 的随机端口上起 HTTP CONNECT → SOCKS5 桥。
// ctx 取消（收割预算到期）后仍在握手中的拨号会被中断；已建立的隧道随
// close（监听器关闭 + CLI 进程退出）自然终结。
func startSocksBridge(ctx context.Context, target string) (*socksBridge, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := &socksBridge{ln: ln, target: target, ctx: ctx}
	go b.serve()
	return b, nil
}

// httpProxyURL 交给 CLI 的 HTTP_PROXY/HTTPS_PROXY 值。
func (b *socksBridge) httpProxyURL() string {
	return "http://" + b.ln.Addr().String()
}

func (b *socksBridge) close() { b.ln.Close() }

func (b *socksBridge) serve() {
	for {
		conn, err := b.ln.Accept()
		if err != nil {
			return // 监听器已关闭
		}
		go b.handle(conn)
	}
}

func (b *socksBridge) handle(down net.Conn) {
	defer down.Close()
	br := bufio.NewReader(down)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		// opencode 的端点全是 https（CLI 只会发 CONNECT）；出现明文请求说明
		// 行为不符合预期，明确记录而不是静默拒绝。
		log.Printf("socks bridge: unexpected non-CONNECT %s %s — dropping", req.Method, req.Host)
		return
	}
	up, err := dialViaProxy(b.ctx, b.target, "tcp", req.Host)
	if err != nil {
		log.Printf("socks bridge: dial %s via %s failed: %v", req.Host, maskProxyURL(b.target), err)
		down.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer up.Close()
	if _, err := down.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	// 双向透传。TCP 层任何一个方向结束后关闭连接（defer），另一方向的
	// Copy 随之报错退出 —— 隧道寿命与 CLI 进程一致。
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, br); done <- struct{}{} }()
	go func() { io.Copy(down, up); done <- struct{}{} }()
	<-done
	<-done
}

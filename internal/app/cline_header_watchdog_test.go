package app

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// 默认 60s；CLINE_HEADER_TIMEOUT=0 表示关闭。
func TestClineHeaderTimeout_Defaults(t *testing.T) {
	t.Setenv("CLINE_HEADER_TIMEOUT", "")
	if d := ClineHeaderTimeout(); d != 60*time.Second {
		t.Fatalf("默认应为 60s，实际 %v", d)
	}
	t.Setenv("CLINE_HEADER_TIMEOUT", "0")
	if d := ClineHeaderTimeout(); d != 0 {
		t.Fatalf("0 应关闭看门狗，实际 %v", d)
	}
	t.Setenv("CLINE_HEADER_TIMEOUT", "25")
	if d := ClineHeaderTimeout(); d != 25*time.Second {
		t.Fatalf("应为 25s，实际 %v", d)
	}
}

// 关闭时不派生 ctx（零开销、行为等价于改动前）。
func TestClineHeaderWatchdog_Disabled(t *testing.T) {
	parent := context.Background()
	ctx, stop := clineHeaderWatchdogFor(parent, 0)
	if ctx != parent {
		t.Fatal("关闭时应直接复用父 ctx")
	}
	if stop() {
		t.Fatal("关闭时不应报触发")
	}
}

// 头到达后 stop 必须在取消前叫停，且之后 ctx 不能被看门狗取消。
func TestClineHeaderWatchdog_StopBeforeFire(t *testing.T) {
	ctx, stop := clineHeaderWatchdogFor(context.Background(), 300*time.Millisecond)
	if stop() {
		t.Fatal("300ms 内不应触发")
	}
	time.Sleep(400 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatalf("stop 之后仍被取消: %v", ctx.Err())
	}
}

// 真实黑洞场景：对端接受 TCP 但永不发响应头，Do 必须在看门狗时长附近返回，
// 而不是挂到 transport 的 5 分钟 ResponseHeaderTimeout。
func TestClineHeaderWatchdog_BlackHole(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listen: %v", err)
	}
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			conns = append(conns, c) // 收下但一个字都不回
		}
	}()
	defer func() { ln.Close() }()

	ctx, stop := clineHeaderWatchdogFor(context.Background(), 150*time.Millisecond)
	defer stop()
	req, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/", nil)
	req = req.WithContext(ctx)

	start := time.Now()
	resp, derr := (&http.Client{}).Do(req)
	elapsed := time.Since(start)
	if derr == nil {
		resp.Body.Close()
		t.Fatal("黑洞出口不应成功返回响应头")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("看门狗未生效，Do 挂了 %v", elapsed)
	}
	if !stop() {
		t.Fatal("应报告看门狗已触发")
	}
	ln.Close()
	<-done // accept 循环退出后再读 conns，避免与追加并发
	for _, c := range conns {
		c.Close()
	}
}

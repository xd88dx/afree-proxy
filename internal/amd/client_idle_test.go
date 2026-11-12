package amd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestChatBodyIdleMonitorBreaksStalledStream 复刻 2026-11-12 生产挂起形态：
// 上游发 200 + 响应头 + 一帧后 body 永久停滞。修复前阻塞读会一直挂到客户端
// 断开（生产实测 22 分钟、received=0 且零输出全额计费）；修复后
// streamIdleTimeout 静默超阈值 cancel attemptCtx，阻塞读在阈值窗口内返回
// 错误，交给网关缓冲聚合器按 upstream_break 换号重试。
func TestChatBodyIdleMonitorBreaksStalledStream(t *testing.T) {
	resetAmdConfigForTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: first\n\n")
		f.Flush()
		// 停滞：不再发任何字节。监控 cancel 打断客户端读 → 传输层断连 →
		// r.Context().Done() 触发 handler 返回，srv.Close 不被在途连接卡住。
		<-r.Context().Done()
	}))
	defer srv.Close()

	Set(&Config{BaseURL: srv.URL, Keys: []string{"rc-k1"}, MaxConcurrency: 2, Retries: 1})
	SetExitResolver(nil)
	SetClientProvider(nil) // 直连默认 client，本测试只考察 body 阶段

	old := streamIdleTimeout
	streamIdleTimeout = 300 * time.Millisecond
	t.Cleanup(func() { streamIdleTimeout = old })

	resp, _, err := Chat(context.Background(), map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, "MiMo-V2.6-Flash", true, "round_robin", func(string) bool { return true })
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	defer resp.Body.Close()

	// 首帧必须原样穿过监控层到达读者（监控不吞数据、不破坏正常流）。
	first := make([]byte, 64)
	n, firstErr := resp.Body.Read(first)
	if n <= 0 || firstErr != nil {
		t.Fatalf("首帧应原样穿过监控层: n=%d err=%v", n, firstErr)
	}

	// 之后上游永久停滞：阻塞读必须在阈值窗口内被打断返回错误。
	start := time.Now()
	buf := make([]byte, 256)
	var stallErr error
	for {
		if _, stallErr = resp.Body.Read(buf); stallErr != nil {
			break
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("停滞流 5s 内未被空闲监控打断（读仍无错误返回）")
		}
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("打断耗时过长: %v（阈值 300ms，应 ~400ms 内）", elapsed)
	}
	if stallErr == nil {
		t.Fatal("停滞流必须以错误收场，不能静默 EOF")
	}
}

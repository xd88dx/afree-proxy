package traectl

// afree 自有测试文件（PRESERVE_RELATIVE）：回退链中间端点降级留痕。
// 原 TestChatChainFallbackWarn 曾内联在上游 chat_test.go（未登记，re-vendor
// 即丢）——2026-11-17 起独立成文件，chat_test.go 回归上游原样。

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestChatChainFallbackWarn 回退链中间端点降级必须留痕（2026-11-11）：
// 上游常用「200 + error 事件」拒绝请求（无正文 → consume 返回普通
// UpstreamError 允许降级），此前 ①② 为何挂完全静默，排查只能靠猜。
// 只记中间端点；末级错误由上层（server/chat.go 收尾日志）携带账号记录，不重复。
func TestChatChainFallbackWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == agentTaskPath {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":4001,"message":"bad request"}`))
			return
		}
		// 前两级：200 + error 事件（无正文 → 允许降级下一级）
		_, _ = w.Write([]byte("event: error\ndata: {\"code\":4001,\"message\":\"unsupported\"}\n\n"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	c := &Client{Store: testStore(t, Products[0]), Base: srv.URL}
	err := c.Chat(context.Background(), ChatRequest{Model: "m"}, func(Delta) error { return nil })
	if err == nil {
		t.Fatal("三级全失败应返回错误")
	}
	logStr := buf.String()
	if got := strings.Count(logStr, "[trae-chain]"); got != 2 {
		t.Errorf("应恰好 2 条中间降级 WARN（末级不记），得到 %d:\n%s", got, logStr)
	}
	for _, p := range []string{chatIDEPath, ideV1ChatPath} {
		if !strings.Contains(logStr, p+" 降级") {
			t.Errorf("中间端点 %s 的降级 WARN 未落日志:\n%s", p, logStr)
		}
	}
}

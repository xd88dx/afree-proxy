package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// errAfterReader 读完 r 的内容后返回 err，模拟上游中途断流。
type errAfterReader struct {
	r   io.Reader
	err error
}

func (e *errAfterReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err == io.EOF {
		return n, e.err
	}
	return n, err
}

func TestBufferUpstreamSSEComplete(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	lines, usage, complete, brk := bufferUpstreamSSE(strings.NewReader(body))
	if !complete {
		t.Fatal("stream with finish_reason + [DONE] must be complete")
	}
	if brk != nil {
		t.Fatalf("brk = %v, want nil", brk)
	}
	if usage == nil || usage["prompt_tokens"].(float64) != 10 {
		t.Fatalf("usage not captured: %+v", usage)
	}
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "\"content\":\"hi\"") || !strings.Contains(joined, "[DONE]") {
		t.Fatalf("lines not preserved verbatim:\n%s", joined)
	}
}

func TestBufferUpstreamSSEBroken(t *testing.T) {
	partial := "data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n"
	brkErr := errors.New("connection reset by peer")
	r := &errAfterReader{r: strings.NewReader(partial), err: brkErr}
	_, usage, complete, brk := bufferUpstreamSSE(r)
	if complete {
		t.Fatal("broken stream must not be complete")
	}
	if brk == nil || !errors.Is(brk, brkErr) {
		t.Fatalf("brk = %v, want %v", brk, brkErr)
	}
	if usage != nil {
		t.Fatalf("broken stream should not report usage, got %+v", usage)
	}
}

func TestBufferUpstreamSSEEOFWithoutDone(t *testing.T) {
	// 干净 EOF 但没看到 finish/[DONE]：必须判为中断，交给上层重试
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"half\"}}]}\n\n"
	_, _, complete, brk := bufferUpstreamSSE(strings.NewReader(body))
	if complete {
		t.Fatal("EOF without finish must not be complete")
	}
	if !errors.Is(brk, io.ErrUnexpectedEOF) {
		t.Fatalf("brk = %v, want io.ErrUnexpectedEOF", brk)
	}
}

// 第一次尝试中途断流：换账号重试；第二次完整。客户端收到第二次的完整回放，
// 无 error 事件；上游被调用了两次。
func TestHandleClineStreamBufferedRetriesAndReplays(t *testing.T) {
	t.Setenv("STREAM_ATTEMPT", "3")
	orig := clineCallFn
	defer func() { clineCallFn = orig }()

	calls := 0
	clineCallFn = func(ctx context.Context, params map[string]any, stream, useProxies bool) (*http.Response, *Account, error) {
		calls++
		if calls == 1 {
			r := &errAfterReader{
				r:   strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n"),
				err: errors.New("connection reset by peer"),
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(r)}, &Account{Email: "first@x"}, nil
		}
		ok := "data: {\"choices\":[{\"delta\":{\"content\":\"full\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n"
		return fakeUpstream(200, ok), &Account{Email: "second@x"}, nil
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"m","stream":true}`))
	w := httptest.NewRecorder()
	handleClineStreamBuffered(w, req, map[string]any{"model": "m", "stream": true}, false)

	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2", calls)
	}
	out := w.Body.String()
	if strings.Contains(out, "upstream_error") {
		t.Fatalf("client must not see error event, got:\n%s", out)
	}
	if !strings.Contains(out, "\"content\":\"full\"") || !strings.Contains(out, "finish_reason\":\"tool_calls\"") || !strings.Contains(out, "[DONE]") {
		t.Fatalf("client did not receive the complete replay:\n%s", out)
	}
	if strings.Contains(out, "\"content\":\"par\"") {
		t.Fatal("first broken attempt must not leak to the client")
	}
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// 客户端在读流期间断开（ctx 取消）：不重试、不写响应，直接放弃。
func TestHandleClineStreamBufferedClientGoneNoRetry(t *testing.T) {
	orig := clineCallFn
	defer func() { clineCallFn = orig }()

	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clineCallFn = func(c context.Context, params map[string]any, stream, useProxies bool) (*http.Response, *Account, error) {
		calls++
		cancel() // 模拟上游流式期间客户端断开，连带取消请求 ctx
		r := &errAfterReader{
			r:   strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n"),
			err: context.Canceled,
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(r)}, &Account{Email: "a@x"}, nil
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	handleClineStreamBuffered(w, req, map[string]any{"model": "m", "stream": true}, false)

	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1 (client gone must not retry)", calls)
	}
	if w.Body.String() != "" {
		t.Fatalf("nothing should be written to a gone client, got:\n%s", w.Body.String())
	}
}

// 所有尝试都断流：缓冲模式下尚未写出任何字节，直接返回 502 JSON 错误
// （newapi 记为真实错误、面板 Error 列可见、openai SDK 走客户端重试）。
func TestHandleClineStreamBufferedAllBrokenReturns502(t *testing.T) {
	t.Setenv("STREAM_ATTEMPT", "3")
	orig := clineCallFn
	defer func() { clineCallFn = orig }()

	calls := 0
	clineCallFn = func(ctx context.Context, params map[string]any, stream, useProxies bool) (*http.Response, *Account, error) {
		calls++
		r := &errAfterReader{
			r:   strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n"),
			err: errors.New("connection reset by peer"),
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(r)}, &Account{Email: "a@x"}, nil
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	handleClineStreamBuffered(w, req, map[string]any{"model": "m", "stream": true}, false)

	if calls != 3 {
		t.Fatalf("upstream calls = %d, want %d", calls, 3)
	}
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
	out := w.Body.String()
	if !strings.Contains(out, "upstream_error") || !strings.Contains(out, "upstream stream interrupted") {
		t.Fatalf("502 error body expected:\n%s", out)
	}
	if strings.Contains(out, "\"content\":\"par\"") || strings.Contains(out, "[DONE]") {
		t.Fatal("partial content or SSE framing must not leak to the client")
	}
}

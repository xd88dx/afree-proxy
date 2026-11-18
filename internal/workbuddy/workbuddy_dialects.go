package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// WorkbuddyPipeWriter adapts the in-process WorkBuddy handler to an
// http.Response body. It keeps SSE writes streaming instead of buffering the
// full completion in memory.
type WorkbuddyPipeWriter struct {
	header     http.Header
	writer     *io.PipeWriter
	statusCh   chan int
	statusOnce sync.Once
	status     int
}

func NewWorkbuddyPipeWriter(w *io.PipeWriter) *WorkbuddyPipeWriter {
	return &WorkbuddyPipeWriter{
		header:   make(http.Header),
		writer:   w,
		statusCh: make(chan int, 1),
	}
}

func (w *WorkbuddyPipeWriter) Header() http.Header {
	return w.header
}

func (w *WorkbuddyPipeWriter) WriteHeader(status int) {
	w.MarkStatus(status)
}

func (w *WorkbuddyPipeWriter) Write(p []byte) (int, error) {
	w.MarkStatus(http.StatusOK)
	return w.writer.Write(p)
}

func (w *WorkbuddyPipeWriter) Flush() {}

func (w *WorkbuddyPipeWriter) MarkStatus(status int) {
	w.statusOnce.Do(func() {
		w.status = status
		w.statusCh <- status
	})
}

// ServeWorkBuddyChatHTTP invokes the WorkBuddy OpenAI handler in-process and
// exposes its response as an *http.Response for the existing dialect
// converters. The returned cleanup must always be called.
func (w *WorkbuddySubsystem) ServeWorkBuddyChatHTTP(ctx context.Context, src *http.Request, body []byte) (*http.Response, func(), error) {
	if w == nil || w.Server == nil {
		return nil, func() {}, fmt.Errorf("WorkBuddy subsystem unavailable")
	}
	// 网关前缀 → vendor realm 前缀（wbcn:/wbgb: → cn:/global:），
	// vendor 只认自己的模型协议。
	body = WorkbuddyRewriteModel(body)
	reader, writer := io.Pipe()
	pipeWriter := NewWorkbuddyPipeWriter(writer)
	req := src.Clone(ctx)
	req.Method = http.MethodPost
	req.URL.Path = "/v1/chat/completions"
	req.URL.RawPath = ""
	req.RequestURI = ""
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header = src.Header.Clone()

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer writer.Close()
		w.Server.ServeHTTP(pipeWriter, req)
		pipeWriter.MarkStatus(http.StatusOK)
	}()

	cleanup := func() {
		_ = writer.CloseWithError(context.Canceled)
		_ = reader.Close()
		<-done
	}

	select {
	case status := <-pipeWriter.statusCh:
		return &http.Response{
			StatusCode: status,
			Header:     pipeWriter.header.Clone(),
			Body:       reader,
		}, cleanup, nil
	case <-ctx.Done():
		cleanup()
		return nil, func() {}, ctx.Err()
	}
}

func WorkbuddyErrorText(r io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(r, 1<<20))
	var payload struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		switch e := payload.Error.(type) {
		case string:
			if strings.TrimSpace(e) != "" {
				return strings.TrimSpace(e)
			}
		case map[string]any:
			if msg, _ := e["message"].(string); strings.TrimSpace(msg) != "" {
				return strings.TrimSpace(msg)
			}
		}
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "WorkBuddy upstream request failed"
	}
	return text
}

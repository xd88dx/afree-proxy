package app

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

// workbuddyPipeWriter adapts the in-process WorkBuddy handler to an
// http.Response body. It keeps SSE writes streaming instead of buffering the
// full completion in memory.
type workbuddyPipeWriter struct {
	header     http.Header
	writer     *io.PipeWriter
	statusCh   chan int
	statusOnce sync.Once
	status     int
}

func newWorkbuddyPipeWriter(w *io.PipeWriter) *workbuddyPipeWriter {
	return &workbuddyPipeWriter{
		header:   make(http.Header),
		writer:   w,
		statusCh: make(chan int, 1),
	}
}

func (w *workbuddyPipeWriter) Header() http.Header {
	return w.header
}

func (w *workbuddyPipeWriter) WriteHeader(status int) {
	w.markStatus(status)
}

func (w *workbuddyPipeWriter) Write(p []byte) (int, error) {
	w.markStatus(http.StatusOK)
	return w.writer.Write(p)
}

func (w *workbuddyPipeWriter) Flush() {}

func (w *workbuddyPipeWriter) markStatus(status int) {
	w.statusOnce.Do(func() {
		w.status = status
		w.statusCh <- status
	})
}

// serveWorkBuddyChatHTTP invokes the WorkBuddy OpenAI handler in-process and
// exposes its response as an *http.Response for the existing dialect
// converters. The returned cleanup must always be called.
func (w *workbuddySubsystem) serveWorkBuddyChatHTTP(ctx context.Context, src *http.Request, body []byte) (*http.Response, func(), error) {
	if w == nil || w.server == nil {
		return nil, func() {}, fmt.Errorf("WorkBuddy subsystem unavailable")
	}
	// 网关前缀 → vendor realm 前缀（wbcn:/wbgb: → cn:/global:），
	// vendor 只认自己的模型协议。
	body = workbuddyRewriteModel(body)
	reader, writer := io.Pipe()
	pipeWriter := newWorkbuddyPipeWriter(writer)
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
		w.server.ServeHTTP(pipeWriter, req)
		pipeWriter.markStatus(http.StatusOK)
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

func workbuddyErrorText(r io.Reader) string {
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

func writeWorkbuddyAnthropicError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "api_error",
			"message": message,
		},
	})
}

func (w *workbuddySubsystem) serveAnthropicMessages(rw http.ResponseWriter, r *http.Request, req anthropicReq, openAIReq map[string]any, toolSchemas map[string]map[string]bool) {
	body, err := json.Marshal(openAIReq)
	if err != nil {
		writeWorkbuddyAnthropicError(rw, http.StatusBadRequest, "encode request: "+err.Error())
		return
	}
	resp, cleanup, err := w.serveWorkBuddyChatHTTP(r.Context(), r, body)
	if err != nil {
		writeWorkbuddyAnthropicError(rw, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer cleanup()
	if resp.StatusCode >= http.StatusBadRequest {
		writeWorkbuddyAnthropicError(rw, resp.StatusCode, workbuddyErrorText(resp.Body))
		return
	}

	if req.Stream {
		handleAnthropicStreamWithUsage(rw, resp, req.Model, toolSchemas, stopSequencesFrom(openAIReq), nil)
		return
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		writeWorkbuddyAnthropicError(rw, http.StatusBadGateway, "decode WorkBuddy response: "+err.Error())
		return
	}
	out = normalizeOpenAIResponse(out)
	truncateAtStopSequences(out, stopSequencesFrom(openAIReq))
	anthropicResp := openAIToAnthropic(out)
	if tc, ok := getNested(out, "choices", 0, "message", "tool_calls").([]any); ok && len(tc) > 0 {
		anthropicResp["stop_reason"] = "tool_use"
	}
	writeJSON(rw, http.StatusOK, anthropicResp)
}

func (w *workbuddySubsystem) serveResponses(rw http.ResponseWriter, r *http.Request, chat map[string]any, isStream bool) {
	body, err := json.Marshal(chat)
	if err != nil {
		writeJSON(rw, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	resp, cleanup, err := w.serveWorkBuddyChatHTTP(r.Context(), r, body)
	if err != nil {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	defer cleanup()
	if resp.StatusCode >= http.StatusBadRequest {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(rw, resp.Body)
		return
	}

	if isStream {
		rw.Header().Set("Content-Type", "text/event-stream")
		rw.Header().Set("Cache-Control", "no-cache")
		rw.Header().Set("Connection", "keep-alive")
		rw.Header().Set("Access-Control-Allow-Origin", "*")
		rw.WriteHeader(http.StatusOK)
		chatStreamToResponses(rw, resp, nil)
		return
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		writeJSON(rw, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if data, ok := out["data"].(map[string]any); ok {
		out = data
	}
	writeJSON(rw, http.StatusOK, chatToResponses(normalizeOpenAIResponse(out)))
}

package trae

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// newSSEStream 从原始 SSE 文本构造解析游标（上游线格式：event: + data: + 空行）。
func newSSEStream(body string) *Stream {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	return newStream(resp)
}

// nextOfType 持续取事件直到指定类型（跳过无事件的帧），EOF 返回 nil。
func nextOfType(t *testing.T, s *Stream, typ string) *StreamEvent {
	t.Helper()
	for i := 0; i < 50; i++ {
		ev, err := s.Next()
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		if ev == nil {
			return nil
		}
		if ev.Type == typ {
			return ev
		}
	}
	t.Fatalf("event %q not found", typ)
	return nil
}

// 回归：event: 行的事件名必须跨行保留。曾经 pending 在循环体内重置，导致
// token_usage/done 退化成按 payload 猜名字，用量丢失、done 识别失败。
func TestStreamEventLinesAreHonored(t *testing.T) {
	s := newSSEStream(strings.Join([]string{
		"event: metadata",
		`data: {"conversation_id":"c1"}`,
		"",
		"event: output",
		`data: {"response":"你好"}`,
		"",
		"event: token_usage",
		`data: {"input_tokens":5,"output_tokens":2,"total_tokens":7}`,
		"",
		"event: done",
		`data: {}`,
		"",
	}, "\n"))
	defer s.Close()

	ev := nextOfType(t, s, "text")
	if ev.Text != "你好" {
		t.Fatalf("text = %q", ev.Text)
	}
	uev := nextOfType(t, s, "usage")
	if uev.Usage == nil || uev.Usage.Prompt != 5 || uev.Usage.Completion != 2 || uev.Usage.Total != 7 {
		t.Fatalf("usage = %+v", uev.Usage)
	}
	dev := nextOfType(t, s, "done")
	if dev == nil || !s.SawDone {
		t.Fatal("done event not recognized (SawDone=false)")
	}
	if ev, err := s.Next(); ev != nil || err != nil {
		t.Fatalf("after done = %+v, %v", ev, err)
	}
}

// 回归：event: error 不能被吞掉（旧实现最终表现为 "empty stream"）。
func TestStreamErrorEventSurfaces(t *testing.T) {
	s := newSSEStream(strings.Join([]string{
		"event: metadata",
		`data: {"conversation_id":"c1"}`,
		"",
		"event: error",
		`data: {"code":4001,"error":"","message":"We're sorry, the param is invalid."}`,
		"",
	}, "\n"))
	defer s.Close()

	ev := nextOfType(t, s, "error")
	if ev.Code != "4001" || !strings.Contains(ev.Message, "param is invalid") {
		t.Fatalf("error event = %+v", ev)
	}
	if s.Err == nil {
		t.Fatal("Stream.Err not set for error event")
	}
	if !s.Err.ParamError {
		t.Fatalf("4001 not classified as param error: %+v", s.Err)
	}
	if ev, err := s.Next(); ev != nil || err != nil {
		t.Fatalf("after error = %+v, %v", ev, err)
	}
}

// 限流码走换号重试语义，必须被分类出来。
func TestStreamErrorRateLimitedClassified(t *testing.T) {
	s := newSSEStream("event: error\ndata: {\"code\":4011,\"message\":\"rate limit\"}\n\n")
	defer s.Close()
	ev := nextOfType(t, s, "error")
	if ev.Code != "4011" || s.Err == nil || !s.Err.RateLimited {
		t.Fatalf("rate limit classification: ev=%+v err=%+v", ev, s.Err)
	}
}

// 错误事件的 data 不是 JSON 时也要上抛原文，而不是当成可忽略的注入行。
func TestStreamErrorNonJSONPayloadSurfaces(t *testing.T) {
	s := newSSEStream("event: error\ndata: upstream exploded\n\n")
	defer s.Close()
	ev := nextOfType(t, s, "error")
	if ev.Message != "upstream exploded" {
		t.Fatalf("error payload = %+v", ev)
	}
	if s.Err == nil || s.Err.Body != "upstream exploded" {
		t.Fatalf("Stream.Err = %+v", s.Err)
	}
}

// 事件名不得跨帧泄漏：error 之后的事件必须按自己的名字解析。
func TestStreamEventNameDoesNotLeakAcrossFrames(t *testing.T) {
	s := newSSEStream(strings.Join([]string{
		"event: error",
		`data: {"code":4001,"message":"bad"}`,
		"",
		"event: output",
		`data: {"response":"ok"}`,
		"",
	}, "\n"))
	defer s.Close()

	nextOfType(t, s, "error")
	ev := nextOfType(t, s, "text")
	if ev.Text != "ok" {
		t.Fatalf("frame after error parsed as %+v", ev)
	}
}

// code 缺少 message 时退回 extra（上游 {"code":1005,"extra":"{\"plan\":3}"}）。
func TestStreamErrorFallsBackToExtra(t *testing.T) {
	s := newSSEStream("event: error\ndata: {\"code\":1005,\"error\":\"\",\"extra\":\"{\\\"plan\\\":3}\"}\n\n")
	defer s.Close()
	ev := nextOfType(t, s, "error")
	if ev.Code != "1005" || !strings.Contains(ev.Message, "plan") {
		t.Fatalf("error with extra = %+v", ev)
	}
}

// 回归（2026-10-26 实测）：extra_info.content 是整段 reasoning 的 JSON 序列化，
// 不能当正文渲染——否则回答末尾会多出一段 {"reasoning_content":"..."}。
func TestStreamExtraInfoContentNotRendered(t *testing.T) {
	s := newSSEStream(strings.Join([]string{
		"event: output",
		`data: {"response":"","reasoning_content":"thinking..."}`,
		"",
		"event: output",
		`data: {"response":"Hi there","reasoning_content":null}`,
		"",
		"event: extra_info",
		`data: {"model":"glm-5.2","content":"{\"reasoning_content\":\"thinking...\"}"}`,
		"",
		"event: done",
		`data: {"finish_reason":"stop"}`,
		"",
	}, "\n"))
	defer s.Close()

	var visible strings.Builder
	for {
		ev, err := s.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if ev == nil {
			t.Fatal("stream ended without done")
		}
		if ev.Type == "text" {
			visible.WriteString(ev.Text)
		}
		if ev.Type == "done" {
			break
		}
	}
	if got := visible.String(); got != "Hi there" {
		t.Fatalf("visible text = %q", got)
	}
}

// TraeCode 增量事件族（text_delta/translated_text）仍要产出正文。
func TestStreamDeltaEventsRender(t *testing.T) {
	s := newSSEStream(strings.Join([]string{
		"event: text_delta",
		`data: {"delta":"abc"}`,
		"",
		"event: translated_text",
		`data: {"text":"def"}`,
		"",
	}, "\n"))
	defer s.Close()
	if ev := nextOfType(t, s, "text"); ev.Text != "abc" {
		t.Fatalf("text_delta = %+v", ev)
	}
	if ev := nextOfType(t, s, "text"); ev.Text != "def" {
		t.Fatalf("translated_text = %+v", ev)
	}
}

package app

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type countingTransport struct {
	calls int
	resp  *http.Response
	err   error
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return c.resp, nil
}

// 死腿短路：不出网、返回与上游同构的 4001。
func TestTraeGuardShortCircuitsAgentTask(t *testing.T) {
	inner := &countingTransport{}
	guard := traeFallbackGuardTransport{inner: inner}

	req, _ := http.NewRequest("POST", "https://trae-api-cn.mchost.guru"+traeAgentTaskPath, strings.NewReader("{}"))
	resp, err := guard.RoundTrip(req)
	if err != nil {
		t.Fatalf("短路不应报错: %v", err)
	}
	if inner.calls != 0 {
		t.Fatalf("死腿不应出网，inner 被调用了 %d 次", inner.calls)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("应返回 400，得到 %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "conversation_id") || !strings.Contains(string(b), "afree gateway") {
		t.Fatalf("响应体应与上游 4001 同构并标注短路来源: %s", b)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type 应为 application/json，得到 %q", got)
	}
}

// 其他端点正常透传（2xx 不产生额外行为）。
func TestTraeGuardPassthrough2xx(t *testing.T) {
	inner := &countingTransport{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}}
	guard := traeFallbackGuardTransport{inner: inner}

	req, _ := http.NewRequest("POST", "https://trae-api-cn.mchost.guru/api/agent/v3/llm_utils_chat", strings.NewReader("{}"))
	resp, err := guard.RoundTrip(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("2xx 应透传: err=%v status=%d", err, resp.StatusCode)
	}
	if inner.calls != 1 {
		t.Fatalf("inner 应被调用 1 次，得到 %d", inner.calls)
	}
}

// 传输失败与 non-2xx 透传（行为不变，仅多观测日志）。
func TestTraeGuardPassthroughNon2xxAndError(t *testing.T) {
	inner := &countingTransport{resp: &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom"))}}
	guard := traeFallbackGuardTransport{inner: inner}
	req, _ := http.NewRequest("POST", "https://trae-api-cn.mchost.guru/api/ide/v1/chat", nil)
	resp, err := guard.RoundTrip(req)
	if err != nil || resp.StatusCode != 500 {
		t.Fatalf("500 应透传: err=%v status=%d", err, resp.StatusCode)
	}

	inner2 := &countingTransport{err: errors.New("socks EOF")}
	guard2 := traeFallbackGuardTransport{inner: inner2}
	_, err = guard2.RoundTrip(req)
	if err == nil || !strings.Contains(err.Error(), "socks EOF") {
		t.Fatalf("传输错误应透传: %v", err)
	}
}

// 直连出口（traePooledClient("")）也必须被守卫覆盖：此前返回 nil 交还原生
// client，死腿 create_agent_task 真实出网白耗一个往返（2026-11-10 生产
// tacn 案例：未绑定出口账号的第 1 轮尝试 4s）。短路发生在守卫层，不会触网。
func TestTraeDirectEgressShortCircuitsDeadLeg(t *testing.T) {
	cl := traePooledClient("", "uid-test")
	if cl == nil {
		t.Fatal("直连出口应返回守卫客户端，而不是 nil（交还原生直连）")
	}
	req, _ := http.NewRequest("POST", "https://trae-api-cn.mchost.guru"+traeAgentTaskPath, strings.NewReader("{}"))
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("短路不应报错: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("应返回 400，得到 %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "afree gateway") {
		t.Fatalf("应由守卫短路返回（含 afree 标注）: %s", b)
	}
}

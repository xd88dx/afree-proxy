package app

// 上游 429 透传回归(2026-11-09):限流是"该凭据用完了,等会儿再来",客户端会按
// Retry-After 退避重试;包成 502 会让 IDE 判定网关故障并标记渠道不可用。
// 判据:错误类型自带 429 → 对外 429;其余状态码与普通错误 → fallback 不变。

import (
	"errors"
	"net/http"
	"testing"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/tokenharbor"
)

func TestUpstreamErrorStatusPasses429(t *testing.T) {
	fallback := http.StatusBadGateway
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"amd 429", &amd.HTTPError{Status: 429, Body: "rate limited"}, http.StatusTooManyRequests},
		{"openrouter 429", &openrouter.HTTPError{Status: 429}, http.StatusTooManyRequests},
		{"tokenharbor 429", &tokenharbor.HTTPError{Status: 429}, http.StatusTooManyRequests},
		{"other 429", &other.HTTPError{Status: 429, RateLimited: true}, http.StatusTooManyRequests},
		{"zen 429", &zenHTTPError{Status: 429}, http.StatusTooManyRequests},
		{"cline 429", &clineAPIError{Status: 429}, http.StatusTooManyRequests},
		{"跨端点 429", &upstreamHTTPError{Status: 429, Body: "slow down"}, http.StatusTooManyRequests},
		{"包装后的 429", wrapped429{}, http.StatusTooManyRequests},
		// 非 429 一律走 fallback(既有语义不变)
		{"amd 401", &amd.HTTPError{Status: 401}, fallback},
		{"other 503", &other.HTTPError{Status: 503}, fallback},
		{"cline 403", &clineAPIError{Status: 403}, fallback},
		{"普通错误", errors.New("dial tcp: connection refused"), fallback},
		{"nil 错误", nil, fallback},
	}
	for _, c := range cases {
		if got := upstreamErrorStatus(c.err, fallback); got != c.want {
			t.Errorf("%s: upstreamErrorStatus = %d, want %d", c.name, got, c.want)
		}
	}
	// zen 的既有别名必须与统一判定一致(它被 9 个调用点共用)。
	if got := zenClientErrorStatus(&zenHTTPError{Status: 429}); got != http.StatusTooManyRequests {
		t.Errorf("zenClientErrorStatus(429) = %d, want 429", got)
	}
	if got := zenClientErrorStatus(errors.New("boom")); got != http.StatusBadGateway {
		t.Errorf("zenClientErrorStatus(plain) = %d, want 502", got)
	}
}

// wrapped429 验证 errors.As 能穿透 fmt.Errorf 的 %w 包装。
type wrapped429 struct{}

func (wrapped429) Error() string { return "wrapped" }
func (wrapped429) Unwrap() error { return &other.HTTPError{Status: 429} }

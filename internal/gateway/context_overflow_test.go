package gateway

import "testing"

// 锁溢出签名的识别面：每类上游至少一条实测原文；新形态发现时在此追加。
// freepi 413 原文（2026-11-28 生产 requests.jsonl 实录）此前裸透传 502。
func TestIsContextOverflow(t *testing.T) {
	hit := []string{
		// OpenAI 兼容系
		`{"error":{"message":"This model's maximum context length is 4097 tokens"}}`,
		// Anthropic 系
		`prompt is too long: 250000 tokens > 200000 maximum`,
		// freepi 413 信封（code 形态）
		`freepi api 413: {"code":"context_too_long","message":"This conversation is too long. Run /compact or start a new session with /new."}`,
		// freepi 文案单独出现（上游改信封结构时仍可命中）
		`This conversation is too long`,
		// HTTP 413 文案
		`413 request entity too large`,
		// 中文上游
		`输入长度超过模型的上下文上限`,
	}
	for _, msg := range hit {
		if !isContextOverflow(msg) {
			t.Errorf("expected overflow signature hit: %q", msg)
		}
	}
	miss := []string{
		// 普通参数错误与账号错误不得误判
		`model gpt-x is not served by this gateway`,
		`account expired, please re-login`,
		`invalid api key`,
		`freepi api 426: {"code":"upgrade_required","message":"your free-pi-cli is below the required minimum"}`,
	}
	for _, msg := range miss {
		if isContextOverflow(msg) {
			t.Errorf("unexpected overflow signature hit: %q", msg)
		}
	}
}

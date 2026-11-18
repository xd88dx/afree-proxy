package kit

// 用量估算原语（2026-11-18 gateway 批归位）：JSON token 估算（自 zen 迁入）
// 与 chat usage 包提取（自 app/proxy.go 迁入）——cline/gateway/app 三方共用的
// 纯函数，无域依赖。

import (
	"encoding/json"
)

// EstimateJSON 官方 Token.estimate 近似：JSON 长度 / 4。
func EstimateJSON(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b) / 4
}

// AsInt 把 JSON 解码出的数值统一成 int（float64/int/int64 三形态）。
func AsInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

// UsageTokens 从上游 usage 包提取 prompt+completion 消耗；
// 上游未返回 usage 时用入站请求估算兜底（与 zen 统计一致）。
func UsageTokens(u map[string]any, params map[string]any) int64 {
	var pt, ct float64
	if v, ok := u["prompt_tokens"].(float64); ok {
		pt = v
	}
	if v, ok := u["completion_tokens"].(float64); ok {
		ct = v
	}
	tokens := int64(pt + ct)
	if tokens <= 0 && params != nil {
		tokens = int64(EstimateJSON(params))
	}
	return tokens
}

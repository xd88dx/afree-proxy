package app

// Trae 上游链路的中间层观测/死腿拦截（app 层 RoundTripper，不碰 vendor 文件）。
//
// ## 背景（2026-11-07 22:39 实测）
//
// vendored 引擎的三级端点回退链（llm_utils_chat → ide/v1/chat →
// agent/v3/create_agent_task）当晚全部失败：
//   - ①② 传输层失败（疑出口代理抖动，同窗口 wb 出口在批量 EOF 冷却）——
//     但回退循环的中间失败**完全静默**，排查时无法定位前两级为何挂；
//   - ③ agent/v3/create_agent_task 要求请求体携带 conversation_id，而引擎的
//     chatBody 从不携带该字段 → 只要前两级失败落到它就**必然** 4001，
//     白耗一个上游往返（约 8s）后照样把错误抛给客户端。
//
// 本层在出口观察层之外补两件事：
//   - 每个 non-2xx / 传输失败的上游请求记 WARN（回退链中间失败不再静默）；
//   - create_agent_task 死腿直接短路返回同构 4001，不再出网。
//
// 短路返回与真实上游同构的 4001 响应体：引擎既有错误分类（code=4001 →
// 「请求本身不合法，不冷却」）与客户端可见行为完全不变，只是更快、日志更诚实。
//
// 2026-11-11 补口子：直连出口此前经 traePooledClient("") 返回 nil 交还原生
// client、绕过本守卫，死腿仍真实出网（生产 tacn 案例 21:03 第 1 轮尝试实测
// 4s）；traectl 导出 SharedChatTransport 后直连也包守卫（traeDirectClient）。
// 回退链「200 + error 事件」形态的中间降级（传输层只见 2xx，本层记不到）
// 由 traectl Chat 循环补 [trae-chain] WARN。

import (
	"io"
	"log"
	"net/http"
	"strings"
)

// traeAgentTaskPath vendored 引擎回退链第三级（死腿）。
const traeAgentTaskPath = "/api/agent/v3/create_agent_task"

// traeDeadLegBody 与真实上游 4001 同构；后缀标注短路来源，避免误判为上游行为。
const traeDeadLegBody = `{"code":4001,"message":"bad request: binding: expr_path=conversation_id, cause=missing required parameter (afree gateway: dead fallback leg short-circuited, request not sent upstream)"}`

type traeFallbackGuardTransport struct {
	inner http.RoundTripper
}

func (t traeFallbackGuardTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// 死腿短路：引擎回退链的最后一级永远缺 conversation_id，出网必然 4001。
	// 不计入出口健康统计（没出网），也不产生真实上游负载。
	if r.URL.Path == traeAgentTaskPath {
		log.Printf("[trae-guard] WARN: 拦截死腿回退端点 %s（引擎不携带 conversation_id，出网必 4001）——直接短路", r.URL.Path)
		return &http.Response{
			StatusCode:    http.StatusBadRequest,
			Status:        "400 Bad Request",
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header{"Content-Type": []string{"application/json"}},
			Body:          io.NopCloser(strings.NewReader(traeDeadLegBody)),
			ContentLength: int64(len(traeDeadLegBody)),
			Request:       r,
		}, nil
	}
	resp, err := t.inner.RoundTrip(r)
	// 回退链中间失败观测：此前两级失败完全静默，定位只能靠猜。
	if err != nil {
		log.Printf("[trae-guard] WARN: 上游 %s 传输失败: %v（非 2xx 时引擎将降级下一级端点）", r.URL.Path, err)
	} else if resp.StatusCode >= 400 {
		log.Printf("[trae-guard] WARN: 上游 %s -> HTTP %d（非 2xx，引擎将降级下一级端点）", r.URL.Path, resp.StatusCode)
	}
	return resp, err
}

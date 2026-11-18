package gateway

// TraeWork 网关分发壳（gateway 批自 app/trae_handlers.go 迁入）：域前缀模型
// 进程内进 vendored /v1/chat/completions；面板 handler 留装配根。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func serveTrae2apiChat(rw http.ResponseWriter, r *http.Request, body []byte) {
	sub := trae2apiCurrent()
	if sub == nil {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{"message": "TraeWork subsystem unavailable", "type": "api_error"},
		})
		return
	}
	if trae2apiRoutableCount() == 0 {
		writeJSON(rw, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"message": "No routable Trae accounts. Enable an account (and its gateway switch) on the Trae page.",
				"type":    "api_error",
			},
		})
		return
	}
	// OpenAI 契约：stream 缺省 = false。vendored 引擎把缺省当 true（上游实现
	// `req.Stream == nil || *req.Stream`），按规范省略 stream 的客户端会收到
	// SSE 而不是 JSON（2026-11-05 审计实测）。只兜 /v1/chat/completions 入口：
	// 缺省时显式置 false；/v1/messages、/v1/responses 走 trae2apiCaller，由
	// 调用方参数决定流式语义，不经这里。
	body = traeNormalizeStreamDefault(body)
	// 合并域翻译（2026-11-07）：对外 tacn:/tagb: → 内部 realm:bare 透传给
	// vendored（交集以 trae 为主，solo 独有归 solo）。
	body = trae2apiTranslateMergedBody(body)

	// 流式请求走"缓冲+中断重试"（与 cline/zen 同语义）：上游 SSE 先完整缓冲、
	// 拿到 finish/[DONE] 才向客户端发 200 并回放；只有**缓冲不完整**（真流中断：
	// EOF/连接重置/idle 断流）才换账号重试整个请求（每次 attempt 由引擎自选新
	// 账号，重试上限 StreamMaxAttempts、ctx 取消不重试——不存在无限重试）。
	// 引擎级 error 帧（上下文超长/4001/权限类确定性错误）后跟 [DONE]，缓冲完整、
	// 不重试，error 帧原样回放给客户端——换账号救不回的错误不白打上游。
	// 2026-11-10 用户定：trae 上游（IDE 逆向接口+恒流式+WAF）中断率高，直通流
	// 时代"响应头已发就只能 200+error 帧"，agent 把半截回复当完成（伪成功）。
	// 非流式保持直通：引擎聚合语义已在 vendored 内，无需外层包装。
	var params map[string]any
	_ = json.Unmarshal(body, &params)
	isStream, _ := params["stream"].(bool)
	if isStream {
		handleStreamBufferedGeneric(rw, r, "trae", params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
			reqBody, err := json.Marshal(params)
			if err != nil {
				return nil, nil, err
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost,
				traeInprocessBase+"/v1/chat/completions", bytesReader(reqBody))
			if err != nil {
				return nil, nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := traeInprocessHTTP.Do(req)
			if err != nil {
				return nil, nil, err
			}
			if resp.StatusCode != http.StatusOK {
				eb, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
				resp.Body.Close()
				return nil, nil, &upstreamHTTPError{Status: resp.StatusCode, Body: string(eb)}
			}
			// 记账回调恒 nil：token/积分记账由 vendored metrics 按账号自行落账。
			return resp, nil, nil
		})
		return
	}

	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	sub.Srv.Handler().ServeHTTP(rw, r)
}

// traeInprocessCaller 为平台 chat 调用器抽象（cross_endpoint）构造的进程内
// client：请求直接进 vendored Handler，响应以 *http.Response 形态交还外层
// 缓冲转发层（与 cline/zen 同一套流式/重试语义）。

// trae2apiCaller 平台 chat 调用器（anthropic 跨端点路径）：model 透传，
// 记账回调恒 nil（token/积分记账由 vendored metrics 按账号自行落账）。
func trae2apiCaller(model string) (platformChatCall, bool) {
	if !isTrae2apiModel(model) {
		return nil, false
	}
	if trae2apiCurrent() == nil {
		return rejectCaller("TraeWork subsystem unavailable", false), true
	}
	if trae2apiRoutableCount() == 0 {
		return rejectCaller("No routable Trae accounts. Add accounts on the Trae page.", true), true
	}
	return func(ctx context.Context, params map[string]any, stream bool) (*http.Response, func(map[string]any), error) {
		// 流式语义以调用方参数为准（/v1/responses 恒 true 与其余平台对齐，
		// /v1/messages 跟随客户端意图），不依赖 body 里的 stream 键——vendored
		// 引擎把「缺省」当 true，与 OpenAI 缺省 false 相反，缺省请求会被错误
		// 流式化（2026-11-05 审计实测）。
		params["stream"] = stream
		// 合并域翻译（同 serveTrae2apiChat）：caller 服务跨端点路径。
		params["model"] = trae2apiTranslateMerged(model)
		body, err := json.Marshal(params)
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			traeInprocessBase+"/v1/chat/completions", bytesReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := traeInprocessHTTP.Do(req)
		if err != nil {
			return nil, nil, err
		}
		// 非 200 必须是 error：vendored 的错误体是普通 JSON，让调用方拿 200 假设
		// 去解析（流式路径按 SSE 收、非流式路径按 chat JSON 解）会把它变成
		// "upstream stream interrupted"/解析失败，真实状态码（400/429/5xx）与上游
		// 原话全丢（2026-11-09 修复；429 经 upstreamErrorStatus 原样透传）。
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			return nil, nil, &upstreamHTTPError{Status: resp.StatusCode, Body: string(body)}
		}
		return resp, nil, nil
	}, true
}

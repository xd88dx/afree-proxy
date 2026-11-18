package gateway

// Other 平台网关侧段（gateway 批自 app/other.go 迁入）：模型列表、出口解析、
// 隔离判定与 chat 分发。管理 API（子平台增删/key 池）留装配根。

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/other"
)

// ============ 网关侧 ============

// otherModelList /v1/models 合并用的模型条目（全部子平台勾选的模型，
// "<prefix>:<model-id>" 形式）。
func otherModelList() []map[string]any {
	out := make([]map[string]any, 0)
	for _, p := range other.Get().Platforms {
		// 展示门控：没有参与路由的启用 key 的子平台不出目录（与请求入口
		// 的可路由性闸同判定），否则客户端选中后必然 401/403。
		if !otherPlatformHasRoutableKey(&p) {
			continue
		}
		for _, m := range other.SelectedModels(&p) {
			out = append(out, map[string]any{
				"id":      p.Prefix + ":" + m.ID,
				"object":  "model",
				"created": time.Now().UnixMilli(),
				"status":  "active",
				"cost":    "free",
			})
		}
	}
	return out
}

// otherExitFn 平台未绑定 key 的出口解析（与 zen 同语义）：
// 未绑定 = 全局规则（Other 全局开关关则直连，否则共享代理池轮转）；
// 绑定 = 主/备用出口，双不可用 = 本轮跳过（隔离优先于可用性）。
func otherExitFn(p *other.Platform) func(key string) (string, bool) {
	return func(key string) (string, bool) {
		if key == "" {
			return "", true
		}
		main, backup, bound := other.BindingOf(p, key)
		if !bound {
			if !other.ProxiesEnabled() {
				if kit.DisableDirect() {
					return "", false // 直连被禁：凭据失效（2026-10-28）
				}
				return "", true
			}
			url, _, ok := exitpool.UpstreamForKind(exitpool.KindOther)
			if !ok {
				return "", false // 出口全被手动冷却：摘出轮转，不走直连兜底
			}
			if url == "" && kit.DisableDirect() {
				return "", false // 代理池空兜底的直连同样不放行
			}
			return url, true
		}
		url, _, ok := exitpool.PickBoundProxyFor(exitpool.KindOther, main, backup)
		return url, ok
	}
}

// otherBoundRoutable 该 key 绑定出口是否可路由（隔离判定）。
func otherBoundRoutable(p *other.Platform) func(key string) bool {
	return func(key string) bool {
		if !exitpool.IsolationEnabled() {
			return true
		}
		main, backup, bound := other.BindingOf(p, key)
		if !bound {
			return true
		}
		return exitpool.BoundProxiesRoutableFor(exitpool.KindOther, main, backup)
	}
}

// handleOtherChat Other chat/completions 转发（网关命中 <prefix>: 时调用）。
func handleOtherChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	model, _ := params["model"].(string)
	p, bare, ok := other.ResolvePrefix(model)
	if !ok || p == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not an Other platform model", model), "type": "invalid_request_error"},
		})
		return
	}
	m, ok := other.ResolveSelected(p, bare)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("model %q is not enabled on platform %q — tick it in the model selector on the Other page", bare, p.Name), "type": "invalid_request_error"},
		})
		return
	}
	if len(p.Keys) == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("No keys configured for platform %q. Add keys on the Other page.", p.Name), "type": "auth_error"},
		})
		return
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": map[string]string{"message": fmt.Sprintf("Base URL is not configured for platform %q. Set it in the settings area on the Other page.", p.Name), "type": "api_error"},
		})
		return
	}
	isStream, _ := params["stream"].(bool)
	if isStream {
		handleStreamBufferedGeneric(w, r, "other:"+p.Slug, params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
			resp, _, err := other.Chat(ctx, p, params, m.ID, true, gwconfig.GlobalStrategy(), otherBoundRoutable(p), otherExitFn(p))
			if err != nil {
				return nil, nil, err
			}
			usedKey := takeUsedKey(resp)
			return resp, func(u map[string]any) { other.RecordKeyTokens(p, usedKey, kit.UsageTokens(u, params)) }, nil
		})
		return
	}
	resp, _, err := other.Chat(r.Context(), p, params, m.ID, true, gwconfig.GlobalStrategy(), otherBoundRoutable(p), otherExitFn(p))
	if err != nil {
		log.Printf("  other api error (%s): %v", p.Slug, err)
		writeUpstreamAPIError(w, m.ID, err, upstreamErrorStatus(err, http.StatusBadGateway))
		return
	}
	defer resp.Body.Close()
	usedKey := takeUsedKey(resp)
	usageFn := func(u map[string]any) {
		other.RecordKeyTokens(p, usedKey, kit.UsageTokens(u, params))
	}
	finishZenChatNonStream(w, resp, &ZenModel{ID: m.ID}, params, usageFn)
}

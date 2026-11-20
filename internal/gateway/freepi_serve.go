package gateway

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/freepi"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/kit"
)

// ============ FreePI 网关侧段（freepi.ai 免费日额度 key 池）============
//
// 与 other_serve.go 同构的单平台形态：模型目录条目、出口解析闭包（fail-closed）、
// 隔离判定与 chat 分发。闸门（ads/session/concurrent）的处理在 freepi 包内。

// freepiModelList /v1/models 的 freepi 段：前缀拼接 + 段内恒序（按 ID 升序，
// models_order_test 锁死契约）。展示门控与请求入口闸同判定——无参与路由的
// key 时整段摘除（「列出来的必须调得通」）。
func freepiModelList() []map[string]any {
	out := make([]map[string]any, 0)
	c := freepi.Get()
	routable := false
	for _, k := range c.Keys {
		if c.RoutingEnabled(k) {
			routable = true
			break
		}
	}
	if !routable {
		return out
	}
	for _, m := range freepi.KnownModels() {
		out = append(out, map[string]any{
			"id":      freepi.ModelPrefix + ":" + m.ID,
			"object":  "model",
			"created": time.Now().UnixMilli(),
			"status":  "active",
			"cost":    "free",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	return out
}

// freepiExitFn key 的出口解析（fail-closed，与 otherExitFn 同语义）：
// 未绑定 key 走全局规则（平台走池开关 → 池轮转；DISABLE_DIRECT 时直连不放行），
// 绑定 key 走主备 fail-closed——双不可用整体跳过，绝不回退其他出口或直连。
func freepiExitFn(key string) (string, bool) {
	if key == "" {
		return "", true
	}
	main, backup, bound := freepi.BindingOf(key)
	if !bound {
		if !freepi.ProxiesEnabled() {
			if kit.DisableDirect() {
				return "", false // 直连被禁：凭据失效
			}
			return "", true
		}
		url, _, ok := exitpool.UpstreamForKind(exitpool.KindFreepi)
		if !ok {
			return "", false // 出口全被手动冷却
		}
		if url == "" && kit.DisableDirect() {
			return "", false
		}
		return url, true
	}
	url, _, ok := exitpool.PickBoundProxyFor(exitpool.KindFreepi, main, backup)
	return url, ok
}

// freepiBoundRoutable 隔离模式下 key 绑定出口的可用性判定（未绑定恒可路由）。
func freepiBoundRoutable() func(key string) bool {
	return func(key string) bool {
		if !exitpool.IsolationEnabled() {
			return true
		}
		main, backup, bound := freepi.BindingOf(key)
		if !bound {
			return true
		}
		return exitpool.BoundProxiesRoutableFor(exitpool.KindFreepi, main, backup)
	}
}

// freepiChatCall 平台 chat 调用器（cross_endpoint 的 platformChatCall 契约）：
// 返回 (resp, usageFn, err)——usageFn 漏接 = /v1/messages 与 /v1/responses 上
// 该平台用量恒 0。非 200 错误由 freepi.Chat 内部已分类冷却。
func freepiChatCall(ctx context.Context, params map[string]any, bare string, stream bool) (*http.Response, func(map[string]any), error) {
	m, ok := freepi.ResolveModel(bare)
	if !ok {
		return nil, nil, fmt.Errorf("model %q is not in the FreePI catalog — refresh the catalog on the FreePI page", bare)
	}
	resp, _, err := freepi.Chat(ctx, params, m.ID, stream, gwconfig.GlobalStrategy(), freepiBoundRoutable(), freepiExitFn)
	if err != nil {
		return nil, nil, err
	}
	usedKey := takeUsedKey(resp)
	return resp, func(u map[string]any) { freepi.RecordKeyTokens(usedKey, kit.UsageTokens(u, params)) }, nil
}

// handleFreepiChat /v1/chat/completions 的 freepi 分发（handleOtherChat 同构，
// 单平台无 slug 维度）。
func handleFreepiChat(w http.ResponseWriter, r *http.Request, params map[string]any) {
	model, _ := params["model"].(string)
	bare, ok := freepi.StripPrefix(model)
	if !ok || strings.TrimSpace(bare) == "" {
		writeUpstreamAPIError(w, model, fmt.Errorf("invalid model: expected %s:<model-id>", freepi.ModelPrefix), http.StatusBadRequest)
		return
	}
	bare = strings.TrimSpace(bare)
	m, ok := freepi.ResolveModel(bare)
	if !ok {
		writeUpstreamAPIError(w, freepi.ModelPrefix+":"+bare, fmt.Errorf("model %q is not in the FreePI catalog", bare), http.StatusBadRequest)
		return
	}
	if len(freepi.Get().Keys) == 0 {
		writeUpstreamAPIError(w, freepi.ModelPrefix+":"+bare, fmt.Errorf("no freepi credentials configured. Add keys on the FreePI page"), http.StatusUnauthorized)
		return
	}
	isStream, _ := params["stream"].(bool)
	if isStream {
		handleStreamBufferedGeneric(w, r, "freepi", params, func(ctx context.Context) (*http.Response, func(map[string]any), error) {
			return freepiChatCall(ctx, params, bare, true)
		})
		return
	}
	resp, usageFn, err := freepiChatCall(r.Context(), params, bare, true)
	if err != nil {
		log.Printf("  freepi api error: %v", err)
		writeUpstreamAPIError(w, freepi.ModelPrefix+":"+bare, err, upstreamErrorStatus(err, http.StatusBadGateway))
		return
	}
	defer resp.Body.Close()
	finishZenChatNonStream(w, resp, &ZenModel{ID: m.ID}, params, usageFn)
}

// FreepiModelList/FreepiExitFn/FreepiBoundRoutable 导出面（app 装配根消费，
// 对齐 other 的 OtherXxx 家族；新代码直接 import gateway 即可）。
func FreepiModelList() []map[string]any         { return freepiModelList() }
func FreepiExitFn() func(string) (string, bool) { return freepiExitFn }
func FreepiBoundRoutable() func(string) bool    { return freepiBoundRoutable() }

// 网关侧工具改名接线（2026-11-21，方案二一期）。
//
// 请求侧：各端点把请求收敛到 chat 形态后，按路由平台查全局勾选——勾选平台
// 用 toolrename.RewriteChatForm 改写 params（复制纪律）；zen 例外，改写发生
// 在域包出站 body 处（CallZenAPI/CallZenResponsesAPI，toolfilter 真名匹配不
// 受影响）。
// 响应侧：出口包 restoreWriter（字节级假名→真名还原，协议无关）。
// trae/workbuddy 属二期（管道内转换链保真待验证）。
package gateway

import (
	"net/http"
	"strings"

	"afree-proxy/internal/toolrename"
)

// toolNamesOfChat 提取 chat 形态工具名清单（chat 嵌套形态；HandleChat 与
// responses/anthropic 转换后的 chat params 共用）。
func toolNamesOfChat(params map[string]any) []string {
	var names []string
	if tools, ok := params["tools"].([]any); ok {
		for _, ti := range tools {
			tm, _ := ti.(map[string]any)
			if tm == nil {
				continue
			}
			fn, _ := tm["function"].(map[string]any)
			n, _ := fn["name"].(string)
			if n == "" {
				n, _ = tm["name"].(string)
			}
			if n != "" {
				names = append(names, n)
			}
		}
	}
	return names
}

// toolRenameKind 平台路由键 → 改名勾选键（other:<slug> 折到 other）。
func toolRenameKind(routeKey string) string {
	if strings.HasPrefix(routeKey, "other:") {
		return toolrename.KindOther
	}
	return routeKey
}

// toolRenameApplyChat 命中勾选平台的 chat 形态接线三件套：构建映射、改写
// params、包还原出口。返回还原后的 w（未启用时原样返回，params 不动）。
func toolRenameApplyChat(kind string, params map[string]any, w http.ResponseWriter) http.ResponseWriter {
	m := toolrename.Build(kind, toolNamesOfChat(params))
	if m == nil {
		return w
	}
	toolrename.RewriteChatForm(params, m)
	return toolrename.NewRestoreWriter(w, m)
}

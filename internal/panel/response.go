package panel

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

type ApiResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

func WriteAPI(w http.ResponseWriter, status int, resp ApiResponse) {
	w.Header().Set("Content-Type", "application/json")
	// 管理 API 的响应里可能带凭据（opencode/config 直接回明文 key、账号列表
	// 带 email/refresh token 片段）。不标 no-store 就会被浏览器缓存、被中间
	// 代理落盘。静态页与 accounts/export 原本就标了，这里补齐到所有管理 JSON。
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func AdminOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func AdminCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if ct := r.Header.Get("Content-Type"); ct != "" {
				if mt, _, err := mime.ParseMediaType(ct); err != nil || mt != "application/json" {
					WriteAPI(w, http.StatusUnsupportedMediaType, ApiResponse{
						Error: "state-changing admin requests must use Content-Type: application/json",
					})
					return
				}
			}
			if !AdminOriginAllowed(r) {
				WriteAPI(w, http.StatusForbidden, ApiResponse{Error: "cross-origin request rejected"})
				return
			}
		}
		h(w, r)
	}
}

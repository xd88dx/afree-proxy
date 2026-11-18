package app

// panel_bridge.go —— panel 包（P4）的 app 侧消费面桥接：apiResponse/writeAPI/
// adminCORS/adminAuthMiddleware/HandleAdminHTML 等的别名转发。

import (
	"net/http"

	"afree-proxy/internal/panel"
)

type apiResponse = panel.ApiResponse

func writeAPI(w http.ResponseWriter, status int, resp apiResponse) {
	panel.WriteAPI(w, status, resp)
}

func WriteAPI(w http.ResponseWriter, status int, resp apiResponse) {
	panel.WriteAPI(w, status, resp)
}

func adminCORS(h http.HandlerFunc) http.HandlerFunc { return panel.AdminCORS(h) }

func adminOriginAllowed(r *http.Request) bool { return panel.AdminOriginAllowed(r) }

func AdminAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return panel.AdminAuthMiddleware(next)
}

// ---- 小写别名（供 admin_zen/other/proxy 等引用小写名的文件使用）----

func adminAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return panel.AdminAuthMiddleware(next)
}

func writeCatalogResult(w http.ResponseWriter, platform string, added, total int) {
	panel.WriteCatalogResult(w, platform, added, total)
}

func writeCatalogResultExtra(w http.ResponseWriter, platform string, added, total int, extra map[string]any) {
	panel.WriteCatalogResultExtra(w, platform, added, total, extra)
}

func writeCatalogNoCredential(w http.ResponseWriter, platform string) {
	panel.WriteCatalogNoCredential(w, platform)
}

func writeCatalogSyncFailed(w http.ResponseWriter, platform string, err error) {
	panel.WriteCatalogSyncFailed(w, platform, err)
}

func writeCatalogSyncStarted(w http.ResponseWriter, platform string) {
	panel.WriteCatalogSyncStarted(w, platform)
}

func writeCatalogAlreadyRunning(w http.ResponseWriter, platform string) {
	panel.WriteCatalogAlreadyRunning(w, platform)
}

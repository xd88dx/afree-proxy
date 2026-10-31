package app

import (
	"net/http"
)

// 各平台「刷新模型目录」的统一响应契约（2026-10-31）。
//
// 此前每个平台自成一格：
//   - 返回值形态各异：amd `synced, %d models`、openrouter/tokenharbor `synced, %d free models`、
//     zen `synced, %d new models`、other `Catalog refreshed`、cline `model sync started`，
//     WorkBuddy 更是直接甩回一大坨 `{"cached":true,"models":[...]}`；
//   - 同步失败被 syncXxxCatalog 内部吞掉（只写日志），handler 照样按新目录长度报成功，
//     于是出现「刷新成功，0 个模型」这种自相矛盾的提示；
//   - Cline 页的刷新按钮顺带刷了 OpenCode 目录，两边结果拼成一条提示串台。
//
// 统一后：所有平台返回同一组 message key + {platform,added,total}，前端一套
// refreshCatalog() 渲染；同步后目录为空一律判为失败，绝不再报假成功。
const (
	msgCatalogSynced         = "synced"
	msgCatalogSyncStarted    = "sync_started"
	msgCatalogAlreadyRunning = "sync_already_running"
	msgNoEnabledCredential   = "no_enabled_credential"
	msgNoModelsReturned      = "no_models_returned"
)

// writeCatalogResult 按统一契约写入「刷新模型目录」的结果。
// added 为本次新增数（<0 视为无从计算，按 0 处理），total 为同步后目录总数；
// 两者由调用方在同步前后取目录长度算出，避免改动各平台 sync 函数签名。
func writeCatalogResult(w http.ResponseWriter, platform string, added, total int) {
	writeCatalogResultExtra(w, platform, added, total, nil)
}

// writeCatalogResultExtra 同 writeCatalogResult，额外把 extra 并入 Data——
// 供「响应需要附带平台快照」的平台使用（目前只有 Other：前端靠这份快照
// 即时重绘模型勾选区，省一次往返）。
func writeCatalogResultExtra(w http.ResponseWriter, platform string, added, total int, extra map[string]any) {
	if added < 0 {
		added = 0
	}
	data := map[string]any{"platform": platform}
	for k, v := range extra {
		data[k] = v
	}
	if total <= 0 {
		// 同步跑完但目录空 = 上游/凭据/出口至少一处不通，报成功是自欺欺人
		data["added"] = 0
		data["total"] = 0
		writeAPI(w, http.StatusOK, apiResponse{
			Success: false,
			Message: msgNoModelsReturned,
			Data:    data,
		})
		return
	}
	data["added"] = added
	data["total"] = total
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: msgCatalogSynced,
		Data:    data,
	})
}

// writeCatalogSyncError 同步前置门控失败（无启用凭据）——可预期拒绝，不抛
// 异常语义，前端按 message 渲染中文引导文案。
func writeCatalogNoCredential(w http.ResponseWriter, platform string) {
	writeAPI(w, http.StatusOK, apiResponse{
		Success: false,
		Message: msgNoEnabledCredential,
		Data:    map[string]any{"platform": platform},
	})
}

// writeCatalogSyncFailed 同步本身失败（上游不可达 / 认证被拒 / 出口不可用）。
// 走 Error 字段让统一的前端 api() 抛异常，把真实原因带到提示里。
func writeCatalogSyncFailed(w http.ResponseWriter, platform string, err error) {
	detail := "unknown error"
	if err != nil {
		detail = err.Error()
	}
	writeAPI(w, http.StatusOK, apiResponse{
		Success: false,
		Message: "sync_failed",
		Error:   detail,
		Data:    map[string]any{"platform": platform},
	})
}

// writeCatalogSyncStarted / writeCatalogAlreadyRunning：异步同步类平台（cline）
// 无法当场给出数量，如实告知已在后台跑，前端不会再拿陈旧数字冒充结果。
func writeCatalogSyncStarted(w http.ResponseWriter, platform string) {
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: msgCatalogSyncStarted,
		Data:    map[string]any{"platform": platform},
	})
}

func writeCatalogAlreadyRunning(w http.ResponseWriter, platform string) {
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: msgCatalogAlreadyRunning,
		Data:    map[string]any{"platform": platform},
	})
}

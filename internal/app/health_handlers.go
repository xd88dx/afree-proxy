package app

// 出口冷却面板 handler（2026-11-17 P3b 从 exitpool/health.go 拆回——panel 域
// 响应封装 writeAPI/apiResponse 留装配根；冷却原语经 exitpool 导出面）。

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"afree-proxy/internal/exitpool"
)

// handleProxyCooldownSet 手动把某出口置入冷却。两种形态：
//   - manual=true（面板「冷却」按钮，2026-11-02 用户定）：无到期时间，摘出轮转后只有手动解禁才恢复；
//   - minutes=N：定时冷却，到期自动恢复。
func handleProxyCooldownSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Proxy   string `json:"proxy"`
		Minutes int    `json:"minutes"`
		Manual  bool   `json:"manual"`
		Kind    string `json:"kind"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	idx := exitpool.IdxByPanelID(strings.TrimSpace(req.Proxy))
	if idx < 0 {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "proxy not in pool"})
		return
	}
	if req.Manual {
		exitpool.MarkManualCooled(idx)
	} else {
		exitpool.CooldownUpstreamProxy(idx, time.Duration(req.Minutes)*time.Minute)
	}
	if req.Kind != "" {
		exitpool.CooldownUpstreamProxyFor(req.Kind, idx, time.Duration(req.Minutes)*time.Minute)
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// handleProxyCooldownClear 解禁（原 handler 主体迁移）。
func handleProxyCooldownClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Proxy string `json:"proxy"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	raw := clearProxyCooldowns(strings.TrimSpace(req.Proxy))
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]string{"proxy": raw}})
}

// clearProxyCooldowns 面板解禁（strike 计数清理由 exitpool.ClearByPanelID；
// 此薄壳保历史调用点）。
func clearProxyCooldowns(id string) string { return exitpool.ClearByPanelID(id) }

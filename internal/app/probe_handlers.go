package app

// 延迟测试面板 handler（2026-11-17 P3b 从 exitpool/probe.go 拆回——panel 域
// 响应封装留装配根；测试/统计原语经 exitpool 导出面）。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"afree-proxy/internal/exitpool"
)

func handleProxyPoolLatencyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Proxies []string `json:"proxies"`
		Target  string   `json:"target,omitempty"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = json.Unmarshal(body, &req)
	rows := runProxyLatencyBatch(context.Background())
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: rows})
}

func handleProxyTestTargetSet(w http.ResponseWriter, r *http.Request) {
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
		Target string `json:"target"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	norm, err := exitpool.NormalizeTestTarget(req.Target)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	exitpool.SetTestTarget(norm)
	writeAPI(w, http.StatusOK, apiResponse{Success: true})
}

// runProxyLatencyBatch 延迟测试批次（原 probe 域函数薄壳；统计真身在 exitpool）。
func runProxyLatencyBatch(ctx context.Context) []map[string]any {
	return exitpool.RunProxyLatencyBatch(ctx)
}

// init 注册批次完成后的健康统计刷盘钩子。
func init() { exitpool.FlushHealthHook = func() { flushProxyHealth() } }

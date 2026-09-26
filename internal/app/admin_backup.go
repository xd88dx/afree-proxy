package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 配置导入导出（全量持久化状态） ============
//
// 导出：把所有需要持久化的状态聚合成一个 JSON（Cline 账号、客户端 key、
// OpenCode key 及启用/绑定、代理池（别名随行）、请求头、调度策略、默认模型、
// 自定义别名、zen 粘性会话、端点学习结果）。文件包含全部凭据，仅经管理面板
// 鉴权后下载。
//
// 导入：同一格式的 JSON，**按段替换** —— 文件中存在的段整体替换当前值，
// 文件中没有的段保持不动。导入后各缓存/持久化文件同步重建。

const configBackupVersion = 1

type configBackup struct {
	Version         int                         `json:"version"`
	ExportedAt      time.Time                   `json:"exportedAt"`
	Accounts        []*Account                  `json:"accounts,omitempty"`
	ClientKeys      []string                    `json:"clientKeys,omitempty"`
	DefaultModel    string                      `json:"defaultModel,omitempty"`
	ClineUseProxies bool                        `json:"clineUseProxies,omitempty"`
	Strategy        string                      `json:"strategy,omitempty"`
	Headers         map[string]string           `json:"headers,omitempty"`
	Zen             *zenConfigData              `json:"zen,omitempty"`
	Sessions        map[string]*zenSessionEntry `json:"sessions,omitempty"`
	Endpoints       map[string]string           `json:"endpoints,omitempty"`
	Combos          []*Combo                    `json:"combos,omitempty"`
}

// handleAdminConfigExport GET /admin/api/config/export
func handleAdminConfigExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	p := loadPool()
	poolMu.Lock()
	accounts := make([]*Account, len(p.Accounts))
	for i, a := range p.Accounts {
		cp := *a
		accounts[i] = &cp
	}
	clientKeys := append([]string(nil), p.Keys...)
	poolMu.Unlock()

	pc := getProxyConfig()
	headers := make(map[string]string, len(pc.Headers))
	for k, v := range pc.Headers {
		headers[k] = v
	}

	loadZenSessions()
	zenSessMu.Lock()
	sessions := make(map[string]*zenSessionEntry, len(zenSessions))
	for k, e := range zenSessions {
		if e != nil {
			cp := *e
			sessions[k] = &cp
		}
	}
	zenSessMu.Unlock()

	backup := configBackup{
		Version:         configBackupVersion,
		ExportedAt:      time.Now().UTC(),
		Accounts:        accounts,
		ClientKeys:      clientKeys,
		DefaultModel:    getDefaultModel(),
		ClineUseProxies: poolClineUseProxies(),
		Strategy:        getProxyConfig().Strategy,
		Headers:         headers,
		Zen:             getZenConfig(),
		Sessions:        sessions,
		Endpoints:       loadZenEndpointsFile(),
		Combos:          listCombos(),
	}
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: "marshal: " + err.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=afree-proxy-config-"+time.Now().Format("20060102")+".json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// handleAdminConfigImport POST /admin/api/config/import
// body = 导出格式的 JSON；存在的段整体替换，缺失的段保持不动。
func handleAdminConfigImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var backup configBackup
	if err := json.NewDecoder(r.Body).Decode(&backup); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if backup.Version != configBackupVersion {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: fmt.Sprintf("unsupported backup version %d (want %d)", backup.Version, configBackupVersion)})
		return
	}

	imported := map[string]int{}

	// ---- Cline 账号 ----
	if backup.Accounts != nil {
		seen := map[string]bool{}
		accounts := make([]*Account, 0, len(backup.Accounts))
		for _, a := range backup.Accounts {
			if a == nil || (a.RefreshToken == "" && a.APIToken == "") {
				continue
			}
			if a.AccountID == "" || seen[a.AccountID] {
				a.AccountID = "acc_" + kit.RandHex(8)
			}
			seen[a.AccountID] = true
			if a.Status != "active" && a.Status != "cooldown" && a.Status != "expired" {
				a.Status = "active"
			}
			if a.CreatedAt.IsZero() {
				a.CreatedAt = time.Now()
			}
			accounts = append(accounts, a)
		}
		p := loadPool()
		poolMu.Lock()
		p.Accounts = accounts
		p.CurrentIdx = 0
		markPoolDirtyLocked()
		poolMu.Unlock()
		// 旧账号的刷新单飞句柄一并作废
		refreshFlightMu.Lock()
		refreshFlight = map[*Account]*refreshFlightCall{}
		refreshFlightMu.Unlock()
		savePool()
		imported["accounts"] = len(accounts)
	}

	// ---- 客户端 key / 默认模型 / cline 走代理池 ----
	if backup.ClientKeys != nil {
		p := loadPool()
		poolMu.Lock()
		p.Keys = append([]string(nil), backup.ClientKeys...)
		markPoolDirtyLocked()
		poolMu.Unlock()
		imported["clientKeys"] = len(backup.ClientKeys)
	}
	if backup.ClineUseProxies {
		p := loadPool()
		poolMu.Lock()
		p.ClineUseProxies = true
		markPoolDirtyLocked()
		poolMu.Unlock()
		imported["clineUseProxies"] = 1
	}
	if backup.DefaultModel != "" {
		setDefaultModel(backup.DefaultModel)
		imported["defaultModel"] = 1
	}

	// ---- 调度策略 + 请求头 ----
	if backup.Strategy != "" || backup.Headers != nil {
		cur := getProxyConfig()
		merged := &proxyConfigData{Strategy: cur.Strategy, Headers: map[string]string{}}
		for k, v := range cur.Headers {
			merged.Headers[k] = v
		}
		switch backup.Strategy {
		case "round_robin", "fill", "random":
			merged.Strategy = backup.Strategy
		}
		for k, v := range backup.Headers {
			merged.Headers[k] = v
		}
		setProxyConfig(merged)
		persistProxyConfig(merged)
		imported["strategy+headers"] = 1
	}

	// ---- OpenCode 全套配置（keys/启用/绑定/代理池/策略/压缩/隔离...）----
	if backup.Zen != nil {
		setZenConfig(backup.Zen)
		imported["zenKeys"] = len(backup.Zen.Keys)
		if len(backup.Zen.Proxies) > 0 {
			imported["proxies"] = len(backup.Zen.Proxies)
		}
	}
	// ---- zen 粘性会话（在 setZenConfig 之后写入，避免被按 key 清理）----
	if backup.Sessions != nil {
		zenSessMu.Lock()
		zenSessions = map[string]*zenSessionEntry{}
		for k, e := range backup.Sessions {
			if e == nil || e.Session == "" {
				continue
			}
			cp := *e
			zenSessions[k] = &cp
		}
		zenSessLoaded = true
		zenSessMu.Unlock()
		saveZenSessions()
		imported["sessions"] = len(backup.Sessions)
	}
	// ---- 端点学习结果 ----
	if backup.Endpoints != nil {
		if n := applyZenEndpoints(backup.Endpoints); n > 0 {
			saveZenEndpoints()
		}
		imported["endpoints"] = len(backup.Endpoints)
	}
	// ---- 自定义别名 ----
	if backup.Combos != nil {
		combosMu.Lock()
		valid := make([]*Combo, 0, len(backup.Combos))
		for _, c := range backup.Combos {
			if c == nil || !comboIDRe.MatchString(c.ID) {
				continue
			}
			if c.Platform != "cline" && c.Platform != "zen" {
				continue
			}
			if c.Target == "" {
				continue
			}
			valid = append(valid, c)
		}
		combosList = valid
		combosLoaded = true
		saveCombosLocked()
		combosMu.Unlock()
		imported["combos"] = len(valid)
	}

	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"imported": imported}})
}

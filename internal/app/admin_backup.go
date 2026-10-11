package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/tokenharbor"
	wbauth "afree-proxy/internal/workbuddy/auth"
)

// ============ 配置导入导出（全量持久化状态） ============
//
// 导出：把所有需要持久化的状态聚合成一个 JSON——六个平台的凭据与配置
// （Cline 账号、OpenCode key 及启用/绑定、OpenRouter/AMD/TokenHarbor key 池、
// WorkBuddy config + 凭据文件）、代理池（别名随行）、请求头、调度策略、默认
// 模型、自定义别名、zen 粘性会话、端点学习结果。文件包含全部凭据，仅经管理
// 面板鉴权后下载。新平台接入时必须同步补这里的导出/导入段，否则备份丢数据。
//
// 导入：同一格式的 JSON，**按段替换** —— 文件中存在的段整体替换当前值，
// 文件中没有的段保持不动；导入后各缓存/持久化文件同步重建。
//
// **隔离护栏（v2 起）**：导入的一切上游身份（Cline 账号、zen/or/amd/th key、
// WorkBuddy 账号）一律置为禁用——备份里的启用状态属于导出方部署，其绑定
// 出口在本地未必存在，原样启用等于把流量引向未经验证的身份。启用哪个由
// 用户在面板逐个勾选。客户端 key（本地访问凭据）与代理池不在此列。

const configBackupVersion = 2

// configBackupVersionOK 导入接受的版本：v2（六平台全量）与 v1（旧备份，
// 缺的段保持不动）。更新的版本直接拒绝——按段替换语义下，未知段被静默
// 丢弃比报错危险。
func configBackupVersionOK(v int) bool { return v == 1 || v == 2 }

type workbuddyBackup struct {
	// Config vendor config.json 原文。导入经 saveWorkbuddyConfig 落盘，
	// listen/api_key/auth_dir/state_file 四项强制回写为本地部署值。
	Config json.RawMessage `json:"config,omitempty"`
	// AuthFiles WorkBuddy 凭据文件：文件名 -> base64(原文)。导入时整体
	// 替换 auths 目录（workbuddy*.json），随后全部账号禁用。
	AuthFiles map[string]string `json:"authFiles,omitempty"`
}

type configBackup struct {
	Version         int                         `json:"version"`
	ExportedAt      time.Time                   `json:"exportedAt"`
	Accounts        []*Account                  `json:"accounts,omitempty"`
	ClientKeys      []string                    `json:"clientKeys,omitempty"`
	DefaultModel    string                      `json:"defaultModel,omitempty"`
	ClineUseProxies *bool                       `json:"clineUseProxies,omitempty"`
	AdminLogEnabled *bool                       `json:"adminLogEnabled,omitempty"`
	Strategy        string                      `json:"strategy,omitempty"`
	Headers         map[string]string           `json:"headers,omitempty"`
	Zen             *zenConfigData              `json:"zen,omitempty"`
	Sessions        map[string]*zenSessionEntry `json:"sessions,omitempty"`
	Endpoints       map[string]string           `json:"endpoints,omitempty"`
	Combos          []*Combo                    `json:"combos,omitempty"`
	// v2 补全的 key 池平台与 WorkBuddy（v1 备份无这些段，导入时保持不动）。
	OpenRouter  *openrouter.Config  `json:"openrouter,omitempty"`
	AMD         *amd.Config         `json:"amd,omitempty"`
	TokenHarbor *tokenharbor.Config `json:"tokenharbor,omitempty"`
	WorkBuddy   *workbuddyBackup    `json:"workbuddy,omitempty"`
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

	// cline 走代理池开关：显式落盘（nil = 未设置 = 默认开启），导入按指针还原。
	clineUse := poolClineUseProxies()
	adminLog := poolAdminLogEnabled()
	backup := configBackup{
		Version:         configBackupVersion,
		ExportedAt:      time.Now().UTC(),
		Accounts:        accounts,
		ClientKeys:      clientKeys,
		DefaultModel:    getDefaultModel(),
		ClineUseProxies: &clineUse,
		AdminLogEnabled: &adminLog,
		Strategy:        getProxyConfig().Strategy,
		Headers:         headers,
		Zen:             getZenConfig(),
		Sessions:        sessions,
		Endpoints:       loadZenEndpointsFile(),
		Combos:          listCombos(),
		OpenRouter:      openrouter.Get(),
		AMD:             amd.Get(),
		TokenHarbor:     tokenharbor.Get(),
		WorkBuddy:       exportWorkbuddyBackup(),
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

// exportWorkbuddyBackup 读 vendor config.json 原文与 auths 目录全部凭据文件。
// 子系统未启动（启动失败）时返回 nil——此时没有可导出的活状态。
func exportWorkbuddyBackup() *workbuddyBackup {
	sub := workbuddySub
	if sub == nil || sub.cfg == nil {
		return nil
	}
	wb := &workbuddyBackup{}
	if raw, err := os.ReadFile(sub.cfgPath); err == nil {
		wb.Config = json.RawMessage(raw)
	}
	files, _ := filepath.Glob(filepath.Join(sub.cfg.AuthDir, "workbuddy*.json"))
	if len(files) > 0 {
		wb.AuthFiles = make(map[string]string, len(files))
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			wb.AuthFiles[filepath.Base(f)] = base64.StdEncoding.EncodeToString(raw)
		}
	}
	return wb
}

// handleAdminConfigImport POST /admin/api/config/import
// body = 导出格式的 JSON；存在的段整体替换，缺失的段保持不动。
// 隔离护栏：导入的上游身份全部禁用（见文件头注释）。
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
	if !configBackupVersionOK(backup.Version) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: fmt.Sprintf("unsupported backup version %d (want 1 or %d)", backup.Version, configBackupVersion)})
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
			// 隔离护栏：导入账号一律禁用参与轮换（poolEnabled 缺项 = 启用，
			// 必须显式写 false，不能靠清字段）。
			off := false
			a.PoolEnabled = &off
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
	if backup.ClineUseProxies != nil {
		p := loadPool()
		poolMu.Lock()
		v := *backup.ClineUseProxies
		p.ClineUseProxies = &v
		markPoolDirtyLocked()
		poolMu.Unlock()
		imported["clineUseProxies"] = 1
	}
	if backup.AdminLogEnabled != nil {
		p := loadPool()
		poolMu.Lock()
		v := *backup.AdminLogEnabled
		p.AdminLogEnabled = &v
		markPoolDirtyLocked()
		poolMu.Unlock()
		imported["adminLogEnabled"] = 1
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
		// 隔离护栏：导入的 zen key 一律显式禁用。KeyRoutingEnabled 缺项 =
		// 参与路由，必须逐 key 写 false 而不是清空表。先 normalize 让 Keys
		// 定型（setZenConfig 内部还会再 normalize 一次，幂等）。
		normalizeZenKeys(backup.Zen)
		off := make(map[string]bool, len(backup.Zen.Keys))
		for _, k := range backup.Zen.Keys {
			off[k] = false
		}
		backup.Zen.KeyRoutingEnabled = off
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
			// 平台白名单与 validateCombo 同口径（六平台）；旧备份只有
			// cline/zen 两平台的别名，白名单放宽不影响它们。
			if !comboPlatformValid(c.Platform) {
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

	// ---- key 池平台（v2 段）：整配置替换 + 全部 key 禁用 ----
	if backup.OpenRouter != nil {
		or := backup.OpenRouter
		if or.KeyRoutingEnabled == nil {
			or.KeyRoutingEnabled = map[string]bool{}
		}
		for _, k := range or.Keys {
			or.KeyRoutingEnabled[k] = false
		}
		openrouter.Set(or)
		imported["openrouterKeys"] = len(or.Keys)
	}
	if backup.AMD != nil {
		amdc := backup.AMD
		if amdc.KeyRoutingEnabled == nil {
			amdc.KeyRoutingEnabled = map[string]bool{}
		}
		for _, k := range amdc.Keys {
			amdc.KeyRoutingEnabled[k] = false
		}
		amd.Set(amdc)
		imported["amdKeys"] = len(amdc.Keys)
	}
	if backup.TokenHarbor != nil {
		th := backup.TokenHarbor
		if th.KeyRoutingEnabled == nil {
			th.KeyRoutingEnabled = map[string]bool{}
		}
		for _, k := range th.Keys {
			th.KeyRoutingEnabled[k] = false
		}
		tokenharbor.Set(th)
		imported["tokenharborKeys"] = len(th.Keys)
	}

	// ---- WorkBuddy（v2 段）：config 经 saveWorkbuddyConfig 回写部署四项，
	// 凭据文件整体替换 auths 目录，全部账号禁用 ----
	if backup.WorkBuddy != nil {
		wbAuths, wbCfg := importWorkbuddyBackup(backup.WorkBuddy)
		if wbAuths >= 0 {
			imported["workbuddyAuths"] = wbAuths
		}
		if wbCfg {
			imported["workbuddyConfig"] = 1
		}
	}

	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"imported": imported}})
}

// importWorkbuddyBackup 落盘 vendor config 与凭据文件并禁用全部账号。
// 返回 (导入账号数, config 是否已应用)；子系统未运行时返回 (-1, false)——
// 此时凭据文件仍写盘（重启后生效），config 因需要热应用句柄而跳过。
func importWorkbuddyBackup(wb *workbuddyBackup) (int, bool) {
	sub := workbuddySub
	if sub == nil || sub.cfg == nil {
		log.Printf("  config import: workbuddy subsystem not running, config segment skipped")
		return -1, false
	}
	importedAuths := -1
	if wb.AuthFiles != nil {
		importedAuths = 0
		authDir := sub.cfg.AuthDir
		// 整体替换：清掉本目录全部凭据文件再写导入文件（按段替换语义，
		// 与确认弹窗文案一致）。文件名取 Base 防路径穿越，且必须落在
		// LoadDir 的扫描模式内，否则白写。
		old, _ := filepath.Glob(filepath.Join(authDir, "workbuddy*.json"))
		for _, f := range old {
			_ = os.Remove(f)
		}
		for name, b64 := range wb.AuthFiles {
			base := filepath.Base(name)
			if base != name || !strings.HasPrefix(base, "workbuddy") || !strings.HasSuffix(base, ".json") {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				log.Printf("  config import: workbuddy auth %s decode failed: %v", base, err)
				continue
			}
			if err := os.WriteFile(filepath.Join(authDir, base), raw, 0o600); err != nil {
				log.Printf("  config import: workbuddy auth %s write failed: %v", base, err)
				continue
			}
			importedAuths++
		}
		auths, err := wbauth.LoadDir(authDir)
		if err != nil {
			log.Printf("  config import: workbuddy auth rescan failed: %v", err)
		} else {
			// 隔离护栏：导入账号一律禁用（写回凭据文件，重启后仍是禁用态），
			// 随后让池对齐扫描结果（新号加入、消失的号剔除并落盘）。
			for _, a := range auths {
				if a.SetPoolEnabled(false) {
					if err := a.SaveAtomic(); err != nil {
						log.Printf("  config import: workbuddy auth %s disable save failed: %v", a.UID, err)
					}
				}
			}
			sub.pool.SyncToDir(auths)
		}
	}
	cfgApplied := false
	if len(wb.Config) > 0 {
		if _, err := saveWorkbuddyConfig(sub.cfgPath, wb.Config, sub.live, sub.pool, sub.upstream, sub.scheduler, sub.cfg.AuthDir, sub.cfg.StateFile); err != nil {
			log.Printf("  config import: workbuddy config apply failed: %v", err)
		} else {
			cfgApplied = true
		}
	}
	return importedAuths, cfgApplied
}

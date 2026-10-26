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
	"afree-proxy/internal/other"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/tokenharbor"
	wbauth "afree-proxy/internal/workbuddy/auth"
	wbserver "afree-proxy/internal/workbuddy/server"
)

// ============ 配置导入导出（全量持久化状态） ============
//
// 导出：把所有需要持久化的状态聚合成一个 JSON——各平台凭据与配置
// （Cline 账号、OpenCode key 及启用/绑定、OpenRouter/AMDCloud/TokenHarbor key 池、
// Other 子平台聚合、WorkBuddy config + 凭据文件 + 出口绑定）、代理池（别名随行）、
// 请求头、调度策略、默认
// 模型、自定义别名、zen 粘性会话、端点学习结果。文件包含全部凭据，仅经管理
// 面板鉴权后下载。新平台接入时必须同步补这里的导出/导入段，否则备份丢数据。
//
// 导入：同一格式的 JSON，**按段替换** —— 文件中存在的段整体替换当前值，
// 文件中没有的段保持不动；导入后各缓存/持久化文件同步重建。
//
// **隔离护栏（v2 起）**：导入的一切上游身份（Cline 账号、zen/or/amd/th key、
// WorkBuddy 账号）默认一律置为禁用——备份里的启用状态属于导出方部署，其绑定
// 出口在本地未必存在，原样启用等于把流量引向未经验证的身份。启用哪个由
// 用户在面板逐个勾选。导入弹窗提供逐平台"沿用启用状态"switch（KeepEnabled，
// 默认全不沿用 = 原护栏行为），显式勾选的平台保留备份内原状态。客户端 key
// （本地访问凭据）与代理池不在此列。

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
	// Order 面板展示顺序（账号 uid 列表）。顺序存在 state.json 的 order 字段，
	// 既不在 vendor config.json 也不在凭据文件里——不带这一段，跨机迁移后
	// 表格退回 uid 排序，用户拖好的顺序等于白做（2026-10-21 实测发现）。
	// 仅影响 List()/面板顺序，不参与选号（加权路由与顺序无关）。
	Order []string `json:"order,omitempty"`
}

// legacyZenBackup 是备份文件里的 zen 段。代理池字段（proxies/proxyStrategy/
// proxySubURL/proxySubCron/zenUseProxies/workbuddyUseProxies）在 2026-10-26
// 拆到独立 ProxyPool 段之前一直存在这里；保留这些键让旧备份仍可导入。
type legacyZenBackup struct {
	zenConfigData
	Proxies             []string `json:"proxies,omitempty"`
	ProxyStrategy       string   `json:"proxyStrategy,omitempty"`
	ProxySubURL         string   `json:"proxySubURL,omitempty"`
	ProxySubCron        string   `json:"proxySubCron,omitempty"`
	ZenUseProxies       *bool    `json:"zenUseProxies,omitempty"`
	WorkbuddyUseProxies *bool    `json:"workbuddyUseProxies,omitempty"`
}

// zenBackupSnapshot 取一份 zen 配置的拷贝用于备份。代理池字段不再随 zen 段
// 导出（它们由 ProxyPool 段承载），legacyZenBackup 里的同名字段只用于读旧备份。
func zenBackupSnapshot() *legacyZenBackup {
	return &legacyZenBackup{zenConfigData: *getZenConfig()}
}

type configBackup struct {
	Version         int                         `json:"version"`
	ExportedAt      time.Time                   `json:"exportedAt"`
	Accounts        []*Account                  `json:"accounts,omitempty"`
	ClientKeys      []string                    `json:"clientKeys,omitempty"`
	ClineUseProxies *bool                       `json:"clineUseProxies,omitempty"`
	AdminLogEnabled *bool                       `json:"adminLogEnabled,omitempty"`
	Strategy        string                      `json:"strategy,omitempty"`
	Headers         map[string]string           `json:"headers,omitempty"`
	Zen             *legacyZenBackup            `json:"zen,omitempty"`
	// ProxyPool 全局出口代理池(.proxy-pool.json)。2026-10-26 从 zen 段拆出：
	// 代理池是全局能力，历史上寄居在 zen 配置里只是命名包袱。
	ProxyPool *proxyPoolConfigData `json:"proxyPool,omitempty"`
	Sessions        map[string]*zenSessionEntry `json:"sessions,omitempty"`
	Endpoints       map[string]string           `json:"endpoints,omitempty"`
	Combos          []*Combo                    `json:"combos,omitempty"`
	// v2 补全的 key 池平台与 WorkBuddy（v1 备份无这些段，导入时保持不动）。
	OpenRouter  *openrouter.Config  `json:"openrouter,omitempty"`
	TokenHarbor *tokenharbor.Config `json:"tokenharbor,omitempty"`
	WorkBuddy   *workbuddyBackup    `json:"workbuddy,omitempty"`
	// AMDCloud v2 段原名 amd；2026-10-12 随平台改名同步为 amdcloud。本项目
	// 无存量备份文件，不保留旧键兼容——若日后需要，参照"改段名铁律"。
	AMDCloud *amd.Config `json:"amdcloud,omitempty"`
	// Other 子平台聚合配置（.other-config.json）。2026-10-14 补：Other 页
	// 上线时只做了网关/面板，未接备份，导致子平台与 key 池无法随配置迁移。
	Other *other.Config `json:"other,omitempty"`
	// Trae 账号池平台（.trae-config.json，trae: 前缀）。
	Trae *trae.Config `json:"trae,omitempty"`
	// WorkBuddyProxies WorkBuddy 账号代理绑定（.workbuddy-proxies.json）。
	// 2026-10-14 补：workbuddyBackup 原只覆盖 vendor config.json 与 auths
	// 凭据，漏了出口绑定，导入后该文件仍留旧值（而非本机值），跨机迁移出错。
	WorkBuddyProxies map[string]workbuddyProxyBinding `json:"workBuddyProxies,omitempty"`
	// ModelBlacklist 模型封禁表（.model-blacklist.json）。2026-10-19 补：
	// 导出导入一直漏了封禁数据，跨机迁移后 /v1/models 把已确认不可调用的
	// 模型重新暴露给客户端，每次选中都吃一轮无谓失败。
	ModelBlacklist map[string]*modelBlacklistEntry `json:"modelBlacklist,omitempty"`
	// PlatformBlacklist 平台封禁表（.platform-blacklist.json，afree 本地功能）：
	// 整平台退出会话路由的管理操作，与模型封禁同性质，随配置迁移。
	PlatformBlacklist map[string]*platformBanEntry `json:"platformBlacklist,omitempty"`
	// ZenDepAliases zen 死模型别名表（.zen-model-aliases.json）：弃用 ID ->
	// 继任模型映射，与 sessions/endpoints 同性质的学习结果持久化。
	ZenDepAliases map[string]*zenDepEntry `json:"zenDepAliases,omitempty"`
	// ClineStreamRequired cline "必须流式"自学习结果（.cline-stream-required.json）。
	ClineStreamRequired map[string]string `json:"clineStreamRequired,omitempty"`
	// KeepEnabled 导入方对"沿用启用状态"的逐平台选择（导入弹窗 switch，默认
	// 全不沿用）。缺项/false = 隔离护栏生效（导入身份一律禁用，原行为）；
	// true = 保留备份内该平台的启用状态。key：cline / zen / oprt / amd /
	// tkhb / wb 与 "other:<slug>"（Other 子平台逐个）。
	KeepEnabled map[string]bool `json:"keepEnabled,omitempty"`
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
		Version:             configBackupVersion,
		ExportedAt:          time.Now().UTC(),
		Accounts:            accounts,
		ClientKeys:          clientKeys,
		ClineUseProxies:     &clineUse,
		AdminLogEnabled:     &adminLog,
		Strategy:            getProxyConfig().Strategy,
		Headers:             headers,
		Zen:                 zenBackupSnapshot(),
		ProxyPool:           getProxyPool(),
		Sessions:            sessions,
		Endpoints:           loadZenEndpointsFile(),
		Combos:              listCombos(),
		OpenRouter:          openrouter.Get(),
		AMDCloud:            amd.Get(),
		TokenHarbor:         tokenharbor.Get(),
		WorkBuddy:           exportWorkbuddyBackup(),
		Other:               other.Get(),
		Trae:                trae.Get(),
		WorkBuddyProxies:    exportWorkbuddyProxyBindings(),
		ModelBlacklist:      exportModelBlacklist(),
		PlatformBlacklist:   exportPlatformBlacklist(),
		ZenDepAliases:       exportZenDepAliases(),
		ClineStreamRequired: exportClineStreamRequired(),
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

// exportWorkbuddyProxyBindings 快照 WorkBuddy 账号代理绑定，供导入还原。
// 返回副本而非内部 map：调用方 json.Marshal 时不得与后台写入竞争。
func exportWorkbuddyProxyBindings() map[string]workbuddyProxyBinding {
	wbProxyMu.Lock()
	defer wbProxyMu.Unlock()
	loadWorkbuddyProxyBindingsLocked()
	if len(wbProxyBindings) == 0 {
		return nil
	}
	out := make(map[string]workbuddyProxyBinding, len(wbProxyBindings))
	for k, v := range wbProxyBindings {
		out[k] = v
	}
	return out
}

// importWorkbuddyProxyBindings 还原 WorkBuddy 出口绑定，返回写入条数。
// 与其它段一致采用"整体替换"：备份里出现的 uid 覆盖本机值；本机多出来的
// uid 一并清掉，避免跨机迁移后残留旧机的绑定。
func importWorkbuddyProxyBindings(in map[string]workbuddyProxyBinding) int {
	wbProxyMu.Lock()
	defer wbProxyMu.Unlock()
	loadWorkbuddyProxyBindingsLocked()
	next := make(map[string]workbuddyProxyBinding, len(in))
	for k, v := range in {
		next[k] = v
	}
	wbProxyBindings = next
	saveWorkbuddyProxyBindingsLocked()
	return len(next)
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
	// 展示顺序单独带上：state.json 不随备份走（里面还有 credits/cooling
	// 等运行时状态，属于本机事实）。List() 已按自定义顺序输出，直接取 uid。
	if sub.pool != nil {
		for _, st := range sub.pool.List() {
			if st.UID != "" {
				wb.Order = append(wb.Order, st.UID)
			}
		}
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
			// 隔离护栏：导入账号一律禁用参与轮换（poolEnabled 缺项 = 启用，
			// 必须显式写 false，不能靠清字段）。导入弹窗对该平台勾选"沿用
			// 启用状态"时保留备份内原状态（nil = 启用同口径）。
			if !backup.KeepEnabled["cline"] {
				off := false
				a.PoolEnabled = &off
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
		// 定型（setZenConfig 内部还会再 normalize 一次，幂等）。弹窗勾选
		// "沿用启用状态"时保留备份内原表（缺项 = 启用同口径）。
		normalizeZenKeys(&backup.Zen.zenConfigData)
		// 旧备份把代理池存在 zen 段里：迁移到 ProxyPool 段，供下方统一恢复。
		if backup.ProxyPool == nil {
			if len(backup.Zen.Proxies) > 0 || backup.Zen.ProxySubURL != "" ||
				backup.Zen.ZenUseProxies != nil || backup.Zen.WorkbuddyUseProxies != nil {
				backup.ProxyPool = &proxyPoolConfigData{
					Proxies:             backup.Zen.Proxies,
					Strategy:            backup.Zen.ProxyStrategy,
					SubURL:              backup.Zen.ProxySubURL,
					SubCron:             backup.Zen.ProxySubCron,
					ZenUseProxies:       backup.Zen.ZenUseProxies,
					WorkbuddyUseProxies: backup.Zen.WorkbuddyUseProxies,
				}
			}
		}
		if !backup.KeepEnabled["zen"] {
			off := make(map[string]bool, len(backup.Zen.Keys))
			for _, k := range backup.Zen.Keys {
				off[k] = false
			}
			backup.Zen.KeyRoutingEnabled = off
		}
		// 自动分配规则的排除表按导入池清理一次：备份来自另一台机器时，池条目
		// 未必在本机存在，留着就是永不命中的垃圾配置（与面板保存池同口径）。
		// 备份未带池时不动排除表，避免把"池为空"的意图误升格为"清空规则"。
		if len(backup.Zen.ProxyExclusions) > 0 {
			// 先按 kind 白名单过滤：旧备份/手改文件里可能有非法 kind（例如把
			// 平台名写成 zen 而非 key），它们同样永不命中，与池外条目一并丢弃。
			clean := make(map[string][]string, len(backup.Zen.ProxyExclusions))
			for p, kinds := range backup.Zen.ProxyExclusions {
				var okKinds []string
				for _, k := range kinds {
					if proxyRuleKindValid(k) {
						okKinds = append(okKinds, k)
					}
				}
				if len(okKinds) > 0 {
					clean[p] = okKinds
				}
			}
			if len(clean) == 0 {
				clean = nil
			}
			backup.Zen.ProxyExclusions = clean
		}
		if len(backup.Zen.ProxyExclusions) > 0 && backup.ProxyPool != nil && len(backup.ProxyPool.Proxies) > 0 {
			backup.Zen.ProxyExclusions = pruneProxyExclusions(backup.Zen.ProxyExclusions, backup.ProxyPool.Proxies)
		}
		// 上限表同样只保留正值（缺项/0/负数 = 不限）。
		if len(backup.Zen.ProxyBindCaps) > 0 {
			caps := make(map[string]int, len(backup.Zen.ProxyBindCaps))
			for k, v := range backup.Zen.ProxyBindCaps {
				if proxyRuleKindValid(k) && v > 0 {
					caps[k] = v
				}
			}
			if len(caps) == 0 {
				caps = nil
			}
			backup.Zen.ProxyBindCaps = caps
		}
		setZenConfig(&backup.Zen.zenConfigData)
		imported["zenKeys"] = len(backup.Zen.Keys)
	}
	// ---- 全局代理池(独立于 zen)----
	if backup.ProxyPool != nil {
		setProxyPool(backup.ProxyPool)
		if len(backup.ProxyPool.Proxies) > 0 {
			imported["proxies"] = len(backup.ProxyPool.Proxies)
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

	// ---- key 池平台（v2 段）：整配置替换；默认全部 key 禁用，弹窗勾选
	// "沿用启用状态"的平台保留备份内原表（KeyRoutingEnabled 缺项 = 启用）----
	if backup.OpenRouter != nil {
		or := backup.OpenRouter
		if or.KeyRoutingEnabled == nil {
			or.KeyRoutingEnabled = map[string]bool{}
		}
		if !backup.KeepEnabled["oprt"] {
			for _, k := range or.Keys {
				or.KeyRoutingEnabled[k] = false
			}
		}
		openrouter.Set(or)
		imported["openrouterKeys"] = len(or.Keys)
	}
	if backup.AMDCloud != nil {
		amdc := backup.AMDCloud
		if amdc.KeyRoutingEnabled == nil {
			amdc.KeyRoutingEnabled = map[string]bool{}
		}
		if !backup.KeepEnabled["amd"] {
			for _, k := range amdc.Keys {
				amdc.KeyRoutingEnabled[k] = false
			}
		}
		amd.Set(amdc)
		imported["amdKeys"] = len(amdc.Keys)
	}
	if backup.TokenHarbor != nil {
		th := backup.TokenHarbor
		if th.KeyRoutingEnabled == nil {
			th.KeyRoutingEnabled = map[string]bool{}
		}
		if !backup.KeepEnabled["tkhb"] {
			for _, k := range th.Keys {
				th.KeyRoutingEnabled[k] = false
			}
		}
		tokenharbor.Set(th)
		imported["tokenharborKeys"] = len(th.Keys)
	}
	if backup.Other != nil {
		// 与其它 key 池平台同口径：默认导入的 key 一律禁用（启用状态属导出方
		// 部署）；子平台在弹窗里逐个勾选沿用（key = "other:<slug>"）。
		ocfg := backup.Other
		nKeys := 0
		for i := range ocfg.Platforms {
			p := &ocfg.Platforms[i]
			if len(p.Keys) == 0 {
				continue
			}
			if p.KeyRoutingEnabled == nil {
				p.KeyRoutingEnabled = map[string]bool{}
			}
			if !backup.KeepEnabled["other:"+p.Slug] {
				for _, k := range p.Keys {
					p.KeyRoutingEnabled[k] = false
				}
			}
			nKeys += len(p.Keys)
		}
		other.Set(ocfg)
		imported["otherKeys"] = nKeys
	}
	if backup.Trae != nil {
		// Trae 账号池同口径：导入账号默认全部禁用（PoolEnabled 显式 false；
		// nil = 参与路由，清表不算禁用），弹窗勾选"沿用启用状态"才保留原表。
		tc := backup.Trae
		for _, a := range tc.Accounts {
			if a == nil {
				continue
			}
			if !backup.KeepEnabled["trae"] {
				f := false
				a.PoolEnabled = &f
			}
		}
		trae.Set(tc)
		trae.SetMaxConcurrency()
		imported["traeAccounts"] = len(tc.Accounts)
	}
	if len(backup.WorkBuddyProxies) > 0 {
		imported["workBuddyProxies"] = importWorkbuddyProxyBindings(backup.WorkBuddyProxies)
	}

	// ---- 模型封禁表 / 平台封禁表 / zen 死模型别名 / cline 必须流式学习结果
	//（2026-10-19 补：此前导出导入一直漏了这三个落盘数据，跨机迁移后分别丢失
	// 封禁状态、死模型迁移映射与流式自学习结果；平台封禁随 2026-10-24 功能补齐）----
	if backup.ModelBlacklist != nil {
		importModelBlacklist(backup.ModelBlacklist)
		imported["modelBlacklist"] = len(backup.ModelBlacklist)
	}
	if backup.PlatformBlacklist != nil {
		importPlatformBlacklist(backup.PlatformBlacklist)
		imported["platformBlacklist"] = len(backup.PlatformBlacklist)
	}
	if backup.ZenDepAliases != nil {
		importZenDepAliases(backup.ZenDepAliases)
		imported["zenDepAliases"] = len(backup.ZenDepAliases)
	}
	if backup.ClineStreamRequired != nil {
		importClineStreamRequired(backup.ClineStreamRequired)
		imported["clineStreamRequired"] = len(backup.ClineStreamRequired)
	}

	// ---- WorkBuddy（v2 段）：config 经 saveWorkbuddyConfig 回写部署四项，
	// 凭据文件整体替换 auths 目录；默认全部账号禁用，弹窗勾选"沿用启用
	// 状态"时保留凭据文件内的原始 pool_enabled ----
	if backup.WorkBuddy != nil {
		wbAuths, wbCfg := importWorkbuddyBackup(backup.WorkBuddy, backup.KeepEnabled["wb"])
		if wbAuths >= 0 {
			imported["workbuddyAuths"] = wbAuths
		}
		if wbCfg {
			imported["workbuddyConfig"] = 1
		}
	}

	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"imported": imported}})
}

// exportModelBlacklist 模型封禁表快照（锁内拷贝，Marshal 不得与后台写入竞争）。
func exportModelBlacklist() map[string]*modelBlacklistEntry {
	ensureModelBlacklistLoaded()
	modelBlacklistMu.Lock()
	defer modelBlacklistMu.Unlock()
	if len(modelBlacklistItems) == 0 {
		return nil
	}
	out := make(map[string]*modelBlacklistEntry, len(modelBlacklistItems))
	for k, v := range modelBlacklistItems {
		if v == nil {
			continue
		}
		cp := *v
		out[k] = &cp
	}
	return out
}

// importModelBlacklist 整体替换封禁表并即时落盘（与其他段同语义：
// 备份里没有的段不动；有则连清空也生效）。空 key / nil 条目跳过。
func importModelBlacklist(in map[string]*modelBlacklistEntry) {
	ensureModelBlacklistLoaded()
	next := make(map[string]*modelBlacklistEntry, len(in))
	for k, v := range in {
		if k == "" || v == nil || v.Platform == "" || v.Model == "" {
			continue
		}
		next[k] = v
	}
	modelBlacklistMu.Lock()
	modelBlacklistItems = next
	modelBlacklistMu.Unlock()
	flushModelBlacklist()
	log.Printf("  config import: model blacklist replaced (%d entries)", len(next))
}

// exportZenDepAliases zen 死模型别名表快照（锁内加载 + 拷贝）。
func exportZenDepAliases() map[string]*zenDepEntry {
	zenDepMu.Lock()
	defer zenDepMu.Unlock()
	loadZenDepsLocked()
	if len(zenDepAliases) == 0 {
		return nil
	}
	out := make(map[string]*zenDepEntry, len(zenDepAliases))
	for k, v := range zenDepAliases {
		if v == nil {
			continue
		}
		cp := *v
		out[k] = &cp
	}
	return out
}

// importZenDepAliases 整体替换死模型别名表并落盘。Replacement 为空的无效项跳过。
func importZenDepAliases(in map[string]*zenDepEntry) {
	zenDepMu.Lock()
	defer zenDepMu.Unlock()
	loadZenDepsLocked()
	next := make(map[string]*zenDepEntry, len(in))
	for k, v := range in {
		if k == "" || v == nil || v.Replacement == "" {
			continue
		}
		next[k] = v
	}
	zenDepAliases = next
	zenDepLoaded = true
	saveZenDepsLocked()
	log.Printf("  config import: zen model aliases replaced (%d entries)", len(next))
}

// exportClineStreamRequired cline "必须流式"自学习结果快照。
func exportClineStreamRequired() map[string]string {
	clineStreamMu.RLock()
	defer clineStreamMu.RUnlock()
	if len(clineStreamLearned) == 0 {
		return nil
	}
	out := make(map[string]string, len(clineStreamLearned))
	for k, v := range clineStreamLearned {
		out[k] = v
	}
	return out
}

// importClineStreamRequired 整体替换学习表并落盘。
func importClineStreamRequired(in map[string]string) {
	next := make(map[string]string, len(in))
	for k, v := range in {
		if k == "" {
			continue
		}
		next[k] = v
	}
	clineStreamMu.Lock()
	clineStreamLearned = next
	clineStreamMu.Unlock()
	saveClineStream(next)
	log.Printf("  config import: cline stream-required replaced (%d entries)", len(next))
}

// importWorkbuddyBackup 落盘 vendor config 与凭据文件；keepWbEnabled=false
// 时禁用全部账号（隔离护栏），true 时保留凭据文件内的原始启用状态。
// 返回 (导入账号数, config 是否已应用)；子系统未运行时返回 (-1, false)——
// 此时凭据文件仍写盘（重启后生效），config 因需要热应用句柄而跳过。
func importWorkbuddyBackup(wb *workbuddyBackup, keepWbEnabled bool) (int, bool) {
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
			// 隔离护栏：导入账号默认一律禁用（写回凭据文件，重启后仍是禁用
			// 态），随后让池对齐扫描结果（新号加入、消失的号剔除并落盘）。
			// 弹窗勾选 WorkBuddy"沿用启用状态"时保留原始 pool_enabled。
			if !keepWbEnabled {
				for _, a := range auths {
					if a.SetPoolEnabled(false) {
						if err := a.SaveAtomic(); err != nil {
							log.Printf("  config import: workbuddy auth %s disable save failed: %v", a.UID, err)
						}
					}
				}
			}
			sub.pool.SyncToDir(auths)
		}
	}
	cfgApplied := false
	if len(wb.Config) > 0 {
		// sub.server 是解耦接口；预算闸热应用需要具体 Handler，断言失败（测试
		// fake）时传 nil，saveWorkbuddyConfig 内部跳过 gw 段。
		gw, _ := sub.server.(*wbserver.Handler)
		if _, err := saveWorkbuddyConfig(sub.cfgPath, wb.Config, sub.live, sub.pool, sub.upstream, sub.scheduler, gw, sub.panel, sub.cfg.AuthDir, sub.cfg.StateFile); err != nil {
			log.Printf("  config import: workbuddy config apply failed: %v", err)
		} else {
			cfgApplied = true
		}
	}
	// 展示顺序最后套用：必须排在 SyncToDir（账号集合对齐）之后，否则对齐时
	// 新号按 uid 落到尾部、顺序被打乱。备份未带 order 时不动本机顺序（按段
	// 替换语义里"缺字段 = 保持不动"）。未知 uid 由 SetOrder 自行忽略。
	if len(wb.Order) > 0 && sub.pool != nil {
		if n := sub.pool.SetOrder(wb.Order); n > 0 {
			log.Printf("  config import: workbuddy display order applied (%d accounts)", n)
		}
	}
	return importedAuths, cfgApplied
}

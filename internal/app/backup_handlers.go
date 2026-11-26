package app

// backup_handlers.go —— 备份面板端点（P4 从 cb 域拆回：panel 响应封装
// 属装配根职责；域逻辑在 internal/cb）。

import (
	"afree-proxy/internal/freepi"
	"afree-proxy/internal/gateway"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"afree-proxy/internal/backup"
	"afree-proxy/internal/blacklist"
	"afree-proxy/internal/cline"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/other"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/workbuddy"
	"afree-proxy/internal/zen"
)

// handleAdminConfigExport GET /admin/api/config/export
func handleAdminConfigExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	p := cline.LoadPool()
	cline.PoolMu.Lock()
	accounts := make([]*Account, len(p.Accounts))
	for i, a := range p.Accounts {
		cp := *a
		accounts[i] = &cp
	}
	// cline 统计快照同锁取（poolUsage 由 cline.PoolMu 保护）
	clineState := cline.StateBackupSnapshotLocked()
	cline.PoolMu.Unlock()
	clientKeys := gwconfig.GlobalClientKeys()

	pc := getProxyConfig()
	headers := make(map[string]string, len(pc.Headers))
	for k, v := range pc.Headers {
		headers[k] = v
	}

	loadZenSessions()
	zenSessMu.Lock()
	sessions := make(map[string]*zenSessionEntry, len(*zenSessions))
	for k, e := range *zenSessions {
		if e != nil {
			cp := *e
			sessions[k] = &cp
		}
	}
	zenSessMu.Unlock()

	// cline 走代理池开关：显式落盘（nil = 未设置 = 默认开启），导入按指针还原。
	clineUse := poolClineUseProxies()
	adminLog := poolAdminLogEnabled()
	catalogFeedOnly := globalClineCatalogFeedOnly()
	cb := backup.ConfigBackup{
		Version:                backup.ConfigBackupVersion,
		ExportedAt:             time.Now().UTC(),
		Accounts:               accounts,
		ClientKeys:             clientKeys,
		ClineUseProxies:        &clineUse,
		AdminLogEnabled:        &adminLog,
		ClineCatalogFeedOnly:   &catalogFeedOnly,
		Strategy:               gwconfig.GlobalStrategy(),
		Headers:                headers,
		Zen:                    backup.ZenBackupSnapshot(),
		ProxyPool:              getProxyPool(),
		Proxy:                  getProxyBindingConfig(),
		Sessions:               sessions,
		Endpoints:              loadZenEndpointsFile(),
		Combos:                 listCombos(),
		WorkBuddy:              backup.ExportWorkbuddyBackup(),
		Other:                  other.Get(),
		Freepi:                 freepi.Get(),
		Trae:                   trae.Trae2apiBackupSnapshot(),
		WorkBuddyProxies:       backup.ExportWorkbuddyProxyBindings(),
		TraeProxies:            exportTrae2apiProxyBindings(),
		TraeDisplayOrder:       traeDisplayOrderForBackup(),
		ModelBlacklist:         backup.ExportModelBlacklist(),
		PlatformBlacklist:      blacklist.ExportPlatformBlacklist(),
		ZenDepAliases:          backup.ExportZenDepAliases(),
		ClineStreamRequired:    backup.ExportClineStreamRequired(),
		ToolRename:             gwconfig.GetToolRename(),
		ZenUseProxies:          gateway.PlatformUseProxies("zen"),
		TraeUseProxies:         gateway.PlatformUseProxies("trae"),
		WorkBuddyUseProxies:    gateway.PlatformUseProxies("workbuddy"),
		FreepiUseProxies:       gateway.PlatformUseProxies("freepi"),
		WorkBuddyAccountPrefs:  backup.ExportWorkbuddyAccountPrefs(),
		WorkBuddyDisplayOrder:  backup.ExportWorkbuddyDisplayOrder(),
		WorkBuddyShareOptimize: workbuddy.WkbdConfigShareOptimize(),
		State: &backup.StateBackup{
			Cline:       clineState,
			Zen:         zenStateBackupSnapshot(),
			Other:       other.StateSnapshotJSON(),
			Freepi:      freepi.StateSnapshotJSON(),
			ProxyHealth: proxyHealthBackupSnapshot(),
		},
	}
	// trae 分域倍率筛选（trae-config.json，2026-11-07 立项）：nil = 未配置。
	if cn, intl := trae.TraeConfigRateFilters(); cn != nil || intl != nil {
		cb.TraeRateFilterCN = cn
		cb.TraeRateFilterIntl = intl
	}
	data, err := json.MarshalIndent(cb, "", "  ")
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
// 隔离护栏：导入的上游身份全部禁用（见文件头注释）。
func handleAdminConfigImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var cb backup.ConfigBackup
	if err := json.NewDecoder(r.Body).Decode(&cb); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if !backup.ConfigBackupVersionOK(cb.Version) {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: fmt.Sprintf("unsupported cb version %d (want 1 or %d)", cb.Version, backup.ConfigBackupVersion)})
		return
	}

	imported := map[string]int{}
	// 统计层开关（2026-11-03 三层口径）：勾选（缺省）= 本机统计随备份还原；
	// 取消勾选 = 统计保留本机现值。配置态与账号事实不受此开关影响，总是导入。
	includeStats := cb.ImportState == nil || *cb.ImportState

	// ---- Cline 账号 ----
	if cb.Accounts != nil {
		seen := map[string]bool{}
		accounts := make([]*Account, 0, len(cb.Accounts))
		for _, a := range cb.Accounts {
			if a == nil || (a.RefreshToken == "" && a.APIToken == "") {
				continue
			}
			if a.AccountID == "" || seen[a.AccountID] {
				a.AccountID = "acc_" + kit.RandHex(8)
			}
			seen[a.AccountID] = true
			// 隔离护栏：导入账号一律禁用参与轮换（poolEnabled 缺项 = 启用，
			// 必须显式写 false，不能靠清字段）。导入弹窗对该平台勾选"沿用
			// 启用状态"时保留备份内原状态（nil = 启用同口径）。
			if !cb.KeepEnabled["cline"] {
				off := false
				a.PoolEnabled = &off
			}
			accounts = append(accounts, a)
		}
		p := cline.LoadPool()
		cline.PoolMu.Lock()
		p.Accounts = accounts
		// 账号集合整体替换：清掉不在新集合里的孤儿痕迹（同 ID 账号的本机统计
		// 保留现值——2026-11-03 三层口径，取消勾选统计也不再清零）。勾选统计
		// 时随后由 state 快照整体回填；账号事实由 state 段合并（勾选与否都还原）。
		valid := make(map[string]bool, len(accounts))
		for _, a := range accounts {
			valid[a.AccountID] = true
		}
		cline.PruneUsageLocked(valid)
		cline.PoolMu.Unlock()
		// 旧账号的刷新单飞句柄一并作废
		cline.ClearRefreshFlight()
		cline.SavePool()
		imported["accounts"] = len(accounts)
	}

	// ---- 客户端 key / 默认模型 / cline 走代理池 ----
	if cb.ClientKeys != nil {
		gwconfig.SetGlobalClientKeys(cb.ClientKeys)
		imported["clientKeys"] = len(cb.ClientKeys)
	}
	if cb.ClineUseProxies != nil {
		setPlatformUseProxies("cline", cb.ClineUseProxies)
		imported["clineUseProxies"] = 1
	}
	if cb.AdminLogEnabled != nil {
		setGlobalAdminLogEnabled(*cb.AdminLogEnabled)
		imported["adminLogEnabled"] = 1
	}
	if cb.ClineCatalogFeedOnly != nil {
		setGlobalClineCatalogFeedOnly(*cb.ClineCatalogFeedOnly)
		imported["clineCatalogFeedOnly"] = 1
	}

	// ---- 调度策略 + 请求头 ----
	if cb.Strategy != "" || cb.Headers != nil {
		cur := getProxyConfig()
		merged := &proxyConfigData{Headers: map[string]string{}}
		for k, v := range cur.Headers {
			merged.Headers[k] = v
		}
		switch cb.Strategy {
		case "round_robin", "fill", "random":
			gwconfig.SetGlobalStrategy(cb.Strategy)
		}
		for k, v := range cb.Headers {
			merged.Headers[k] = v
		}
		setProxyConfig(merged)
		persistProxyConfig(merged)
		imported["strategy+headers"] = 1
	}

	// ---- OpenCode 全套配置（keys/启用/绑定/代理池/策略/压缩/隔离...）----
	if cb.Zen != nil {
		// 隔离护栏：导入的 zen key 一律显式禁用。KeyRoutingEnabled 缺项 =
		// 参与路由，必须逐 key 写 false 而不是清空表。先 normalize 让 Keys
		// 定型（zen.SetZenConfig 内部还会再 normalize 一次，幂等）。弹窗勾选
		// "沿用启用状态"时保留备份内原表（缺项 = 启用同口径）。
		normalizeZenKeys(cb.Zen)
		if !cb.KeepEnabled["zen"] {
			off := make(map[string]bool, len(cb.Zen.Keys))
			for _, k := range cb.Zen.Keys {
				off[k] = false
			}
			cb.Zen.KeyRoutingEnabled = off
		}
		zen.SetZenConfig(cb.Zen)
		imported["zenKeys"] = len(cb.Zen.Keys)
	}
	// ---- 全局代理池(独立于 zen)----
	if cb.ProxyPool != nil {
		setProxyPool(cb.ProxyPool)
		if len(cb.ProxyPool.Proxies) > 0 {
			imported["proxies"] = len(cb.ProxyPool.Proxies)
		}
	}
	// ---- 代理隔离/绑定段（独立于出口池与 zen）----
	binding := cb.Proxy
	if binding != nil {
		next := *binding
		// 自动分配规则的排除表按导入池清理一次：备份来自另一台机器时，池条目
		// 未必在本机存在，留着就是永不命中的垃圾配置（与面板保存池同口径）。
		// 备份未带池时不动排除表，避免把"池为空"的意图误升格为"清空规则"。
		if len(next.ProxyExclusions) > 0 {
			// 先按 kind 白名单过滤：旧备份/手改文件里可能有非法 kind（例如把
			// 平台名写成 zen 而非 key），它们同样永不命中，与池外条目一并丢弃。
			clean := make(map[string][]string, len(next.ProxyExclusions))
			for p, kinds := range next.ProxyExclusions {
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
			next.ProxyExclusions = clean
		}
		if len(next.ProxyExclusions) > 0 && cb.ProxyPool != nil && len(cb.ProxyPool.Proxies) > 0 {
			next.ProxyExclusions = pruneProxyExclusions(next.ProxyExclusions, cb.ProxyPool.Proxies)
		}
		// 全局排除表同样按导入池清理（非池条目丢弃、去重）；直连哨兵不属于全局
		// 集（与面板保存同口径，2026-11-24）。备份未带池时不动，避免把"池为空"
		// 的意图误升格为"清空规则"。
		if len(next.ProxyExclusionsAll) > 0 && cb.ProxyPool != nil && len(cb.ProxyPool.Proxies) > 0 {
			next.ProxyExclusionsAll = pruneProxyEntries(next.ProxyExclusionsAll, cb.ProxyPool.Proxies)
		}
		// 上限表同样只保留正值（缺项/0/负数 = 不限）。
		if len(next.ProxyBindCaps) > 0 {
			caps := make(map[string]int, len(next.ProxyBindCaps))
			for k, v := range next.ProxyBindCaps {
				if proxyRuleKindValid(k) && v > 0 {
					caps[k] = v
				}
			}
			if len(caps) == 0 {
				caps = nil
			}
			next.ProxyBindCaps = caps
		}
		setProxyBindingConfig(&next)
		imported["proxyBinding"] = 1
	}

	// ---- zen 粘性会话（在 zen.SetZenConfig 之后写入，避免被按 key 清理）----
	if cb.Sessions != nil {
		zenSessMu.Lock()
		*zenSessions = map[string]*zenSessionEntry{}
		for k, e := range cb.Sessions {
			if e == nil || e.Session == "" {
				continue
			}
			cp := *e
			(*zenSessions)[k] = &cp
		}
		*zenSessLoaded = true
		zenSessMu.Unlock()
		saveZenSessions()
		imported["sessions"] = len(cb.Sessions)
	}
	// ---- 端点学习结果 ----
	if cb.Endpoints != nil {
		if n := applyZenEndpoints(cb.Endpoints); n > 0 {
			saveZenEndpoints()
		}
		imported["endpoints"] = len(cb.Endpoints)
	}
	// ---- 自定义别名 ----
	if cb.Combos != nil {
		imported["combos"] = gateway.ImportCombosBackup(cb.Combos)
	}

	// ---- key 池平台（v2 段）：整配置替换；默认全部 key 禁用，弹窗勾选
	// "沿用启用状态"的平台保留备份内原表（KeyRoutingEnabled 缺项 = 启用）----
	if cb.Other != nil {
		// 与其它 key 池平台同口径：默认导入的 key 一律禁用（启用状态属导出方
		// 部署）；子平台在弹窗里逐个勾选沿用（key = "other:<slug>"）。
		ocfg := cb.Other
		nKeys := 0
		for i := range ocfg.Platforms {
			p := &ocfg.Platforms[i]
			if len(p.Keys) == 0 {
				continue
			}
			if p.KeyRoutingEnabled == nil {
				p.KeyRoutingEnabled = map[string]bool{}
			}
			if !cb.KeepEnabled["other:"+p.Slug] {
				for _, k := range p.Keys {
					p.KeyRoutingEnabled[k] = false
				}
			}
			nKeys += len(p.Keys)
		}
		other.Set(ocfg)
		imported["otherKeys"] = nKeys
	}
	if cb.Freepi != nil {
		// freepi 同口径：默认导入的 key 一律禁用（启用状态属导出方部署）；
		// 弹窗勾选"沿用启用状态"（key = "freepi"）才保留备份内原表。
		fcfg := cb.Freepi
		nKeys := 0
		if len(fcfg.Keys) > 0 {
			if fcfg.KeyRoutingEnabled == nil {
				fcfg.KeyRoutingEnabled = map[string]bool{}
			}
			if !cb.KeepEnabled["freepi"] {
				for _, k := range fcfg.Keys {
					fcfg.KeyRoutingEnabled[k] = false
				}
			}
			nKeys = len(fcfg.Keys)
		}
		freepi.Set(fcfg)
		imported["freepiKeys"] = nKeys
	}
	if len(cb.Trae) > 0 {
		// TraeWork（vendored trae2api）同口径：导入账号默认全部禁用（反代开关
		// 显式 false），弹窗勾选"沿用启用状态"才保留备份内开关表。旧引擎形态
		// 的 trae 段（无 config 键）视为 legacy，忽略并提示。
		n, err := trae.Trae2apiRestoreBackup(cb.Trae, cb.KeepEnabled["trae"])
		if err != nil {
			log.Printf("  config import: trae2api restore skipped: %v", err)
		} else if n > 0 {
			imported["traeAccounts"] = n
		}
	}
	if len(cb.WorkBuddyProxies) > 0 {
		imported["workBuddyProxies"] = backup.ImportWorkbuddyProxyBindings(cb.WorkBuddyProxies)
	}
	// ---- wkbd/trae/zen 平台增量段（2026-11-19 补：走池开关只有 cline 进备份、
	// wb 账号意志与 shareOptimize、trae 分域倍率此前全部不随备份迁移）----
	// 注意：WorkBuddyAccountPrefs 必须在 wb auths 段（下方 ImportWorkbuddyBackup
	// 的 AuthLoadDir 重扫）之前落盘，重扫经 AccountPrefProvider 把意志带回内存。
	if cb.WorkBuddyAccountPrefs != nil {
		imported["workbuddyAccountPrefs"] = backup.ImportWorkbuddyAccountPrefs(cb.WorkBuddyAccountPrefs)
	}
	if cb.WorkBuddyDisplayOrder != nil {
		imported["workbuddyDisplayOrder"] = backup.ImportWorkbuddyDisplayOrder(cb.WorkBuddyDisplayOrder)
	}
	if cb.WorkBuddyShareOptimize != nil {
		workbuddy.MutateWkbdConfig(func(k *workbuddy.WkbdConfigData) { k.ShareOptimize = cb.WorkBuddyShareOptimize })
		if workbuddy.WorkbuddySub != nil {
			(*workbuddy.WorkbuddySub).ShareOptimizeSched.Store(&workbuddy.WbShareSched{
				Enabled: cb.WorkBuddyShareOptimize.Enabled,
				Cron:    cb.WorkBuddyShareOptimize.Cron,
			})
		}
		imported["workbuddyShareOptimize"] = 1
	}
	if cb.FreepiUseProxies != nil {
		gateway.SetPlatformUseProxies("freepi", cb.FreepiUseProxies)
		imported["freepiUseProxies"] = 1
	}
	if cb.ZenUseProxies != nil {
		gateway.SetPlatformUseProxies("zen", cb.ZenUseProxies)
		imported["zenUseProxies"] = 1
	}
	if cb.TraeUseProxies != nil {
		gateway.SetPlatformUseProxies("trae", cb.TraeUseProxies)
		imported["traeUseProxies"] = 1
	}
	if cb.WorkBuddyUseProxies != nil {
		gateway.SetPlatformUseProxies("workbuddy", cb.WorkBuddyUseProxies)
		imported["workbuddyUseProxies"] = 1
	}
	if cb.TraeRateFilterCN != nil || cb.TraeRateFilterIntl != nil {
		trae.MutateTraeConfig(func(tc *trae.TraeConfigData) {
			if cb.TraeRateFilterCN != nil {
				tc.ModelRateFilterCN = cb.TraeRateFilterCN
			}
			if cb.TraeRateFilterIntl != nil {
				tc.ModelRateFilterIntl = cb.TraeRateFilterIntl
			}
		})
		imported["traeRateFilters"] = 1
	}
	if len(cb.TraeProxies) > 0 {
		imported["trae.TraeProxies"] = importTrae2apiProxyBindings(cb.TraeProxies)
	}
	if len(cb.TraeDisplayOrder) > 0 {
		imported["trae.TraeDisplayOrder"] = importTrae2apiDisplayOrder(cb.TraeDisplayOrder)
	}

	// ---- state 段（2026-11-03 三层口径）：全量快照按层分流。账号事实（冷却表/
	// cline 状态 / trae 的 credits·签到）**无论勾选与否都应用**；本机统计（token
	// 记账/请求计数/轮转游标/出口健康）仅勾选时随备份还原，不勾选 = 保留本机
	// 现值（不再清零）。旧版备份带的统计字段在不勾选时同样被忽略。
	if st := cb.State; st != nil {
		if st.Cline != nil {
			cline.PoolMu.Lock()
			if includeStats {
				cline.ImportStateBackupLocked(st.Cline)
			} else {
				importPoolStateFactsLocked(st.Cline)
			}
			savePoolUsageLocked()
			cline.PoolMu.Unlock()
		}
		if st.Zen != nil {
			if includeStats {
				importZenStateBackup(st.Zen)
			} else {
				importZenStateFacts(st.Zen)
			}
		}
		// 四平台 JSON 包：勾选 = 整体替换（全量随备份）；不勾选 = 账号事实合并
		// （冷却表覆盖，本机统计键保留本机值）。trae 段 2026-11-04 起由
		// trae.Trae2apiRestoreBackup 单独处理（含 config/routing/accounts），不再走
		// state 事实合并（vendored state.json 冷却不随备份迁移）。
		mergeOrReplace := func(local, backupSeg json.RawMessage, importFn func(json.RawMessage) error, factKeys ...string) {
			if len(backupSeg) == 0 {
				return
			}
			if includeStats {
				_ = importFn(backupSeg)
				return
			}
			merged, err := backup.MergeStateFactsJSON(local, backupSeg, factKeys...)
			if err != nil {
				log.Printf("  config import: state facts merge failed — keeping local values: %v", err)
				return
			}
			_ = importFn(merged)
		}
		mergeOrReplace(other.StateSnapshotJSON(), st.Other, other.ImportStateJSON)
		// freepi 的 credits 余额快照属账号事实（key 级），不勾选统计也随备份
		// 迁移（2026-11-27 审计 S15 归层）。
		mergeOrReplace(freepi.StateSnapshotJSON(), st.Freepi, freepi.ImportStateJSON, "credits")
		if includeStats && st.ProxyHealth != nil {
			importProxyHealthBackup(st.ProxyHealth)
		}
		imported["state"] = 1
	}

	// ---- 模型封禁表 / 平台封禁表 / zen 死模型别名 / cline 必须流式学习结果
	//（2026-10-19 补：此前导出导入一直漏了这三个落盘数据，跨机迁移后分别丢失
	// 封禁状态、死模型迁移映射与流式自学习结果；平台封禁随 2026-10-24 功能补齐）----
	if cb.ModelBlacklist != nil {
		backup.ImportModelBlacklist(cb.ModelBlacklist)
		imported["modelBlacklist"] = len(cb.ModelBlacklist)
	}
	if cb.PlatformBlacklist != nil {
		blacklist.ImportPlatformBlacklist(cb.PlatformBlacklist)
		imported["platformBlacklist"] = len(cb.PlatformBlacklist)
	}
	if cb.ZenDepAliases != nil {
		backup.ImportZenDepAliases(cb.ZenDepAliases)
		imported["zen.DepAliasesExport"] = len(cb.ZenDepAliases)
	}
	if cb.ClineStreamRequired != nil {
		backup.ImportClineStreamRequired(cb.ClineStreamRequired)
		imported["clineStreamRequired"] = len(cb.ClineStreamRequired)
	}
	// ---- 工具改名（afree-config.json 的 toolRename 段，2026-11-25 补）：段存在
	// 才替换（旧备份无此键不动本机值）；段内容非法时保留本机现值并记日志，
	// 与"按段替换、坏段不回退"口径一致。
	if cb.ToolRename != nil {
		if err := gwconfig.SetToolRename(*cb.ToolRename); err != nil {
			log.Printf("  config import: toolRename segment invalid (%v), keeping local values", err)
		} else {
			imported["toolRename"] = 1
		}
	}

	// ---- WorkBuddy（v2 段）：config 经 saveWorkbuddyConfig 回写部署四项，
	// 凭据文件整体替换 auths 目录；默认全部账号禁用，弹窗勾选"沿用启用
	// 状态"时保留凭据文件内的原始 pool_enabled ----
	if cb.WorkBuddy != nil {
		wbAuths, wbCfg := backup.ImportWorkbuddyBackup(cb.WorkBuddy, cb.KeepEnabled["wb"], includeStats)
		if wbAuths >= 0 {
			imported["workbuddyAuths"] = wbAuths
		}
		if wbCfg {
			imported["workbuddyConfig"] = 1
		}
	}
	// wb 显示顺序回放必须在 auths 段之后：pool.SetOrder 会过滤池外 uid，
	// 提前回放等于把未导入账号的位次丢给「未入序排尾」（重扫已把账号带回池）。
	if cb.WorkBuddy != nil && len(cb.WorkBuddyDisplayOrder) > 0 {
		if sub := workbuddy.WorkbuddySub; sub != nil && (*sub).Pool != nil {
			(*sub).Pool.SetOrder(cb.WorkBuddyDisplayOrder)
		}
	}

	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"imported": imported}})
}

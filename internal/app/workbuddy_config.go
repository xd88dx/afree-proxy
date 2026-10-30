package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"time"

	wbauth "afree-proxy/internal/workbuddy/auth"
	wbconfig "afree-proxy/internal/workbuddy/config"
	"afree-proxy/internal/workbuddy/livecfg"
	wbpanel "afree-proxy/internal/workbuddy/panel"
	wbpool "afree-proxy/internal/workbuddy/pool"
	"afree-proxy/internal/workbuddy/scheduler"
	wbserver "afree-proxy/internal/workbuddy/server"
	"afree-proxy/internal/workbuddy/upstream"
)

// saveWorkbuddyConfig 与源项目 cmd/server.saveConfig 同口径：保留磁盘上的
// 未提交键，按同一套 Default+normalize 校验，原子落盘，然后热应用能立即生效
// 的字段。afree-proxy 接管 listen/api_key/auth_dir/state_file 的装配，因此这
// 四项固定回写为当前部署值，避免面板误改后重启把数据搬出统一目录。
func saveWorkbuddyConfig(
	path string,
	raw []byte,
	live *livecfg.Holder,
	p *wbpool.Pool,
	up *upstream.Client,
	sch *scheduler.Scheduler,
	gw *wbserver.Handler,
	pn *wbpanel.Panel,
	authDir string,
	stateFile string,
) ([]string, error) {
	cur := map[string]any{}
	var curCfg *wbconfig.Config
	if oldRaw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(oldRaw, &cur)
		// 磁盘旧值即上次生效配置（afree 固定回写部署四项后再落盘），作为
		// restartRequired 的比较基线（上游 PR #104 口径：只报真实变化）。
		// 解析失败退化 curCfg=nil，按保守清单全报。
		curCfg, _ = wbconfig.ParseConfig(oldRaw)
	}
	var incoming map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return nil, fmt.Errorf("parse submitted config: %w", err)
	}
	merged := mergeWorkbuddyConfigMaps(cur, incoming)
	merged["api_key"] = ""
	merged["listen"] = ""
	merged["auth_dir"] = authDir
	merged["state_file"] = stateFile

	newCfg, err := wbconfig.ParseConfig(mergedWorkbuddyJSON(merged))
	if err != nil {
		return nil, err
	}
	newCfg.APIKey = ""
	newCfg.Listen = ""

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Windows/单文件 bind mount 上 rename 可能失败；配置位于 data 子目录，
		// 正常路径不会走到这里，fallback 仅作为可操作性兜底。
		if werr := os.WriteFile(path, out, 0o600); werr != nil {
			_ = os.Remove(tmp)
			return nil, fmt.Errorf("replace config: %w (fallback: %v)", err, werr)
		}
		_ = os.Remove(tmp)
	}

	newAllowedModels, newAllowlistEnabled := livecfg.NormalizeAllowedModels(newCfg.Models.Allowed)
	live.Store(livecfg.Snapshot{
		APIKey:               "",
		SoftCooldown:         newCfg.SoftRateDur,
		SanitizeFingerprints: newCfg.Features.SanitizeBlacklistFingerprints,
		RecordClientInfo:     newCfg.Logging.RequestClientInfo,
		ModelRateFilter:      newCfg.Pool.ModelRateFilter,
		HiddenModels:         livecfg.NormalizeHiddenModels(newCfg.Models.Hidden),
		// models.allowed 白名单（上游 issue #68，37b3f4c）：与 Hidden 对称的热字段。
		AllowedModels:    newAllowedModels,
		AllowlistEnabled: newAllowlistEnabled,
		// misakano 线新增热字段：模型名映射与 6004 降级路由表。
		ModelAliases:  livecfg.NormalizeModelAliases(newCfg.Models.Aliases),
		ModelFallback: newCfg.Pool.ModelFallback,
		// 思考档位策略镜像：与 up.SetEffortPolicy 同批热更新，面板立即回显新生效值。
		ThinkingEnabled:        newCfg.Thinking.Enabled,
		ThinkingDefaultEffort:  newCfg.Thinking.DefaultEffort,
		ThinkingClientPriority: newCfg.Thinking.ClientPriority,
		ThinkingOverrideScope:  newCfg.Thinking.OverrideScope,
		ThinkingModelDefaults:  newCfg.Thinking.ModelDefaults,
	})
	up.SanitizeFingerprints.Store(newCfg.Features.SanitizeBlacklistFingerprints)
	up.SanitizeBrandScope.Store(newCfg.Features.SanitizeBrandScope)
	// 思考档位策略热生效：改默认档 / 客户端优先 / 作用域后，下一次 chat 即按新策略。
	up.SetEffortPolicy(effortPolicyOf(newCfg))
	log.Printf("workbuddy: 思考档位策略已热更新（开=%v，默认档=%s，客户端优先=%v，作用域=%s）",
		newCfg.Thinking.Enabled, newCfg.Thinking.DefaultEffort,
		newCfg.Thinking.ClientPriority, newCfg.Thinking.OverrideScope)
	up.HTTP.Timeout = time.Duration(newCfg.Upstream.TimeoutSeconds) * time.Second
	up.HeaderTimeout = time.Duration(newCfg.Upstream.HeaderTimeoutSeconds) * time.Second
	if tr, ok := up.ChatHTTP.Transport.(*http.Transport); ok {
		tr.ResponseHeaderTimeout = up.HeaderTimeout
	}
	up.IdleTimeout = time.Duration(newCfg.Upstream.IdleTimeoutSeconds) * time.Second
	up.UserAgent = newCfg.Upstream.UserAgent
	up.ClientVersion = newCfg.Upstream.ClientVersion
	up.CliVersion = newCfg.Upstream.CliVersion
	up.ClientName = newCfg.Upstream.ClientName
	up.DeviceToken = newCfg.Upstream.DeviceToken
	up.DeviceTokenFile = newCfg.Upstream.DeviceTokenFile
	up.PassthroughIP = newCfg.Upstream.PassthroughIP
	up.GlobalEnabled = newCfg.Global.Enabled
	up.ChatBaseGlobal = newCfg.Global.ChatBase
	up.BillingBaseGlobal = newCfg.Global.BillingBase
	// 同 workbuddy.go：空值不覆盖（chatBase() 无空值回退，空串会打断 CN 上游）。
	if newCfg.CN.ChatBase != "" {
		up.ChatBaseCN = newCfg.CN.ChatBase
	}
	// cn.billing_base 同口径：billingBase() 对 CN 无空值回退，仅在显式非空时覆盖。
	if newCfg.CN.BillingBase != "" {
		up.BillingBaseCN = newCfg.CN.BillingBase
	}
	wbauth.SetGlobalEnabled(newCfg.Global.Enabled)

	p.SetBreaker(newCfg.Pool.BreakerThreshold, newCfg.BreakerCooldownDur, newCfg.BreakerCooldownMaxD)
	p.SetMaxInFlight(newCfg.Pool.MaxInFlight)
	p.SetMaxInFlightGlobal(newCfg.Pool.MaxInFlightGlobal)
	p.SetDegrade(newCfg.Pool.DegradeThreshold, newCfg.DegradeCooldownDur, newCfg.DegradeCooldownMaxD)
	p.SetSoftRateMax(newCfg.SoftRateMaxDur)
	p.SetCostExploreInterval(newCfg.CostExploreIntervalDur)
	p.SetCreditFloor(newCfg.Pool.CreditFloor)
	p.SetWeights(newCfg.Pool.IdleWeightPerHour, newCfg.Pool.IdleWeightMax)
	p.SetPreferExpiring(newCfg.Pool.PreferExpiring, newCfg.ExpiringSoonDur)
	_ = p.SetScheduling(newCfg.Pool.Strategy, newCfg.Pool.AccountOrder)
	// misakano 线新增热键：保留积分（normalize 保证非 nil 且 >=0）、账号优先级/
	// 占比（issue #62）、保底倍率上限、补领清扫间隔。
	p.SetReserveCredits(*newCfg.Pool.ReserveCredits)
	p.SetAccountPriority(newCfg.Pool.AccountPriority)
	p.SetAccountShare(newCfg.Pool.AccountShare)
	p.SetCreditFloorMaxRate(newCfg.Pool.CreditFloorMaxRate)
	sch.SetTaskSweepInterval(newCfg.TaskSweepInterval)
	if pn != nil {
		pn.SetGrowthConcurrency(newCfg.Schedule.GrowthConcurrency) // 排程轮次并发热生效
	}
	if gw != nil {
		// 预算三闸（issue #229）：日上限/突发上限/缓存低命中率。<=0 均为关闭。
		gw.SetBudgetLimit(newCfg.Budget.DailyCreditLimit)
		gw.SetBudgetGuards(newCfg.Budget.BurstCreditLimit, newCfg.Budget.CacheLowRate)
	}

	sch.Reconfigure(scheduler.ScheduleParams{
		CheckinHours:   newCfg.Schedule.CheckinHours,
		TravelHours:    newCfg.Schedule.TravelHours,
		ActivityHours:  newCfg.Schedule.ActivityHours,
		KeepaliveHours: newCfg.Schedule.KeepaliveHours,
		GrowthHours:    newCfg.Schedule.GrowthHours,

		CheckinDisabled:        !newCfg.Schedule.CheckinEnabled,
		TravelDisabled:         !newCfg.Schedule.TravelEnabled,
		ActivityDisabled:       !newCfg.Schedule.ActivityEnabled,
		KeepaliveDisabled:      !newCfg.Schedule.KeepaliveEnabled,
		GrowthDisabled:         !newCfg.Schedule.GrowthEnabled,
		IncludeDisabledInTasks: newCfg.Schedule.IncludeDisabledInTasks,

		JitterMinutes:              newCfg.Schedule.JitterMinutes,
		AccountSpreadMinutes:       newCfg.Schedule.AccountSpreadMinutes,
		AccountSpreadMinGapSeconds: newCfg.Schedule.AccountSpreadMinGapSeconds,
	})
	sch.SetBalanceInterval(newCfg.BalanceRefreshInterval)
	sch.SetExpiringSoonWindow(newCfg.ExpiringSoonDur)
	// wbconfig.ServerReadTimeoutDur（server.read_timeout）在本项目无消费方：
	// WorkBuddy 无独立 http.Server，主网关刻意不设 ReadTimeout（见 proxy.go，
	// SSE 长连接），源项目 issue #100 的 60s 掐断问题在此天然不存在。
	return restartRequiredWorkbuddyFields(curCfg, newCfg), nil
}

// restartRequiredWorkbuddyFields 上游 PR #104 口径：比较「上次生效配置 vs 新配置」，
// 只报告确实变化且无法热生效的字段——未变化不重复提示，改回原值自动清除。
// afree 与上游字段集差异（均为有意）：
//   - listen/auth_dir/state_file/api_key 被 afree 固定回写部署值，永不变化，不列；
//   - upstream.*（UA/版本/超时等）、global.*、cn.chat_base、cn.billing_base、
//     models.hidden 在 saveWorkbuddyConfig 中热应用，不列；
//   - admin/alerting 在 afree 部署中无消费方（未接线，重启也不生效），不列，
//     避免诱导用户无效重启。
func restartRequiredWorkbuddyFields(cur, next *wbconfig.Config) []string {
	if cur == nil {
		// 读盘/解析失败时保守全报（等同旧行为的可重启面）。
		return []string{
			"upstash.url", "upstash.token",
			"session_sticky.enabled", "session_sticky.ttl", "session_sticky.gc_interval",
			"prompt.mode", "prompt.text",
			"metrics",
			"logging.request_archive_enabled", "logging.request_retention_days", "logging.request_archive_max_mb",
		}
	}
	var out []string
	changed := func(name string, before, after any) {
		if !reflect.DeepEqual(before, after) {
			out = append(out, name)
		}
	}
	// upstash：store 启动时创建（workbuddy.go redisstore.New）。
	changed("upstash.url", cur.Upstash.URL, next.Upstash.URL)
	changed("upstash.token", cur.Upstash.Token, next.Upstash.Token)
	// 会话粘性：Router 启动时创建（enabled=false 时 sess 为 nil）。
	changed("session_sticky.enabled", cur.SessionSticky.Enabled, next.SessionSticky.Enabled)
	changed("session_sticky.ttl", cur.SessionTTL, next.SessionTTL)
	changed("session_sticky.gc_interval", cur.SessionGCInterval, next.SessionGCInterval)
	// 提示词：server.Handler 启动时装配，无热应用路径。
	changed("prompt.mode", cur.Prompt.Mode, next.Prompt.Mode)
	changed("prompt.text", cur.PromptText, next.PromptText)
	// metrics：/metrics 端点在 NewHandler 时按开关注册，无热摘除。
	changed("metrics", cur.Metrics, next.Metrics)
	// 请求归档：reqlog 目录/句柄启动时装配。
	changed("logging.request_archive_enabled", cur.Logging.RequestArchiveEnabled, next.Logging.RequestArchiveEnabled)
	changed("logging.request_retention_days", cur.Logging.RequestRetentionDays, next.Logging.RequestRetentionDays)
	changed("logging.request_archive_max_mb", cur.Logging.RequestArchiveMaxMB, next.Logging.RequestArchiveMaxMB)
	return out
}

func mergeWorkbuddyConfigMaps(cur, incoming map[string]any) map[string]any {
	for k, v := range incoming {
		if inMap, ok := v.(map[string]any); ok {
			if curMap, ok := cur[k].(map[string]any); ok {
				cur[k] = mergeWorkbuddyConfigMaps(curMap, inMap)
				continue
			}
		}
		cur[k] = v
	}
	return cur
}

func mergedWorkbuddyJSON(m map[string]any) []byte {
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// effortPolicyOf 把 config thinking 节转成 upstream 运行时策略（同上游
// cmd/server.effortPolicyOf）。nil 配置返回 nil，SetEffortPolicy 对 nil 记
// 「无策略」，即完全不碰出站档位；Enabled=false 的策略同样不生效。
func effortPolicyOf(cfg *wbconfig.Config) *upstream.EffortPolicy {
	if cfg == nil {
		return nil
	}
	return &upstream.EffortPolicy{
		Enabled:        cfg.Thinking.Enabled,
		DefaultEffort:  cfg.Thinking.DefaultEffort,
		ClientPriority: cfg.Thinking.ClientPriority,
		OverrideScope:  cfg.Thinking.OverrideScope,
		ModelDefaults:  cfg.Thinking.ModelDefaults,
	}
}

package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	wbauth "afree-proxy/internal/workbuddy/auth"
	wbconfig "afree-proxy/internal/workbuddy/config"
	"afree-proxy/internal/workbuddy/livecfg"
	wbpool "afree-proxy/internal/workbuddy/pool"
	"afree-proxy/internal/workbuddy/scheduler"
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
	authDir string,
	stateFile string,
) ([]string, error) {
	cur := map[string]any{}
	if oldRaw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(oldRaw, &cur)
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

	live.Store(livecfg.Snapshot{
		APIKey:               "",
		SoftCooldown:         newCfg.SoftRateDur,
		SanitizeFingerprints: newCfg.Features.SanitizeBlacklistFingerprints,
		RecordClientInfo:     newCfg.Logging.RequestClientInfo,
	})
	up.SanitizeFingerprints.Store(newCfg.Features.SanitizeBlacklistFingerprints)
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
	wbauth.SetGlobalEnabled(newCfg.Global.Enabled)

	p.SetBreaker(newCfg.Pool.BreakerThreshold, newCfg.BreakerCooldownDur, newCfg.BreakerCooldownMaxD)
	p.SetMaxInFlight(newCfg.Pool.MaxInFlight)
	p.SetMaxInFlightGlobal(newCfg.Pool.MaxInFlightGlobal)
	p.SetDegrade(newCfg.Pool.DegradeThreshold, newCfg.DegradeCooldownDur, newCfg.DegradeCooldownMaxD)
	p.SetSoftRateMax(newCfg.SoftRateMaxDur)
	p.SetCostExploreInterval(newCfg.CostExploreIntervalDur)
	p.SetCreditFloor(newCfg.Pool.CreditFloor)
	p.SetWeights(newCfg.Pool.IdleWeightPerHour, newCfg.Pool.IdleWeightMax)
	p.SetPreferExpiring(newCfg.Pool.PreferExpiring)

	sch.Reconfigure(
		newCfg.Schedule.CheckinHours, newCfg.Schedule.TravelHours,
		newCfg.Schedule.ActivityHours, newCfg.Schedule.KeepaliveHours,
		newCfg.Schedule.BlackcatHours, newCfg.Schedule.GrowthHours,
		!newCfg.Schedule.CheckinEnabled, !newCfg.Schedule.TravelEnabled,
		!newCfg.Schedule.ActivityEnabled, !newCfg.Schedule.KeepaliveEnabled,
		!newCfg.Schedule.BlackcatEnabled, !newCfg.Schedule.GrowthEnabled,
	)
	sch.SetBalanceInterval(newCfg.BalanceRefreshInterval)
	sch.SetIncludeDisabledInTasks(newCfg.Schedule.IncludeDisabledInTasks)
	sch.SetExpiringSoonWindow(newCfg.ExpiringSoonDur)
	// wbconfig.ServerReadTimeoutDur（server.read_timeout）在本项目无消费方：
	// WorkBuddy 无独立 http.Server，主网关刻意不设 ReadTimeout（见 proxy.go，
	// SSE 长连接），源项目 issue #100 的 60s 掐断问题在此天然不存在。
	return restartRequiredWorkbuddyFields(newCfg), nil
}

func restartRequiredWorkbuddyFields(c *wbconfig.Config) []string {
	out := []string{"listen", "auth_dir", "state_file"}
	out = append(out, "upstream.timeout_seconds", "upstream.header_timeout_seconds", "upstream.idle_timeout_seconds")
	if c.Upstash.URL != "" || c.Upstash.Token != "" {
		out = append(out, "upstash")
	}
	out = append(out, "session_sticky.ttl", "session_sticky.gc_interval")
	// 请求归档的目录/句柄在启动时装配，改动需重启（与源项目 restartRequiredFields 同口径）。
	out = append(out, "logging.request_archive_enabled", "logging.request_retention_days", "logging.request_archive_max_mb")
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

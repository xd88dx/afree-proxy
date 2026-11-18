package app

// gwconfig_bridge.go —— gwconfig 包（P4：env/全局配置/config-manifest 迁出）
// 的 app 侧消费面桥接：类型别名 + 同签名包装 + 锁指针别名。锁序约束不变：
// 子系统锁在外、gwconfig.GlobalCfgMu 在内（别名指向同一 mutex）。

import (
	"time"

	"afree-proxy/internal/gwconfig"
)

// ---- 类型别名（全局段数据形态，P4 类型归配置主人）----

type Combo = gwconfig.Combo

type modelBlacklistEntry = gwconfig.ModelBlacklistEntry

type globalConfigData = gwconfig.GlobalConfigData

type platformBanEntry = gwconfig.PlatformBanEntry

// ---- 锁与单例（指针别名）----

var globalCfgMu = &gwconfig.GlobalCfgMu

var globalCfg = &gwconfig.GlobalCfg

var globalCfgLoaded = &gwconfig.GlobalCfgLoaded

var globalConfigPathOverride = &gwconfig.GlobalConfigPathOverride

// ---- 读写包装 ----

func loadGlobalConfigLocked() *gwconfig.GlobalConfigData { return gwconfig.LoadGlobalConfigLocked() }

func mutateGlobalConfig(fn func(*gwconfig.GlobalConfigData)) { gwconfig.MutateGlobalConfig(fn) }

func globalConfigPath() string { return gwconfig.GlobalConfigPath() }

func globalSessionSecretHex() (string, bool) { return gwconfig.GlobalSessionSecretHex() }

func setGlobalSessionSecret(hexSecret string) { gwconfig.SetGlobalSessionSecret(hexSecret) }

func globalStrategy() string { return gwconfig.GlobalStrategy() }

func setGlobalStrategy(s string) { gwconfig.SetGlobalStrategy(s) }

func globalAdminLogEnabled() bool { return gwconfig.GlobalAdminLogEnabled() }

func setGlobalAdminLogEnabled(v bool) { gwconfig.SetGlobalAdminLogEnabled(v) }

func globalClientKeys() []string { return gwconfig.GlobalClientKeys() }

func setGlobalClientKeys(keys []string) { gwconfig.SetGlobalClientKeys(keys) }

// ---- config-manifest ----

func rebuildConfigManifest() { gwconfig.RebuildConfigManifest() }

func startConfigManifestWatcher() { gwconfig.StartConfigManifestWatcher() }

// ---- env 层 ----

func AdminAuthRequired() bool { return gwconfig.AdminAuthRequired() }

func LogRequestsEnabled() bool { return gwconfig.LogRequestsEnabled() }

func AllowBanned() bool { return gwconfig.AllowBanned() }

func AdminPasswordEnv() string { return gwconfig.AdminPasswordEnv() }

func APIKeyEnv() string { return gwconfig.APIKeyEnv() }

func LogFileMaxBytes() int64 { return gwconfig.LogFileMaxBytes() }

func MaxRequestBodyBytes() int64 { return gwconfig.MaxRequestBodyBytes() }

func CheckPublicExposure(host string) error { return gwconfig.CheckPublicExposure(host) }

func ApplyEnvConfig() { gwconfig.ApplyEnvConfig() }

func StrictModelMatchEnv() bool { return gwconfig.StrictModelMatchEnv() }

func StreamMaxAttempts() int { return gwconfig.StreamMaxAttempts() }

func SystemPromptOverrideEnabled() bool { return gwconfig.SystemPromptOverrideEnabled() }

func ClineHeaderTimeout() time.Duration { return gwconfig.ClineHeaderTimeout() }

func StreamLogEnabled() bool { return gwconfig.StreamLogEnabled() }

func ForceStream() bool { return gwconfig.ForceStream() }

func poolStrategyEnvLocked() bool { return gwconfig.PoolStrategyEnvLocked() }

func clineProxiesEnabled() bool { return poolClineUseProxies() }

func defaultGlobalConfig() *gwconfig.GlobalConfigData { return gwconfig.DefaultGlobalConfig() }

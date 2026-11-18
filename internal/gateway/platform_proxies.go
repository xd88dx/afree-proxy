package gateway

// 各平台「未绑定凭据是否走共享代理池」开关的统一读取入口（gateway 批自
// app/platform_proxies.go 迁入，分发体直调各平台包）。2026-11-17 配置重组：
// 平台键回归各平台配置文件——cline-config.json 的 useProxies（AccountPool）、
// zen-config.json 的 zenUseProxies、other-config.json 的 Platform.UseProxies
// （逐子平台）、trae-config.json 的 useProxies、wkbd-config.json 的 useProxies。

import (
	"afree-proxy/internal/cline"
	"afree-proxy/internal/other"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/workbuddy"
	"afree-proxy/internal/zen"
)

// platformUseProxies 读取某平台的走池开关。nil = 该平台未配置（缺省走池）。
func platformUseProxies(kind string) *bool {
	switch kind {
	case "cline":
		p := cline.LoadPool()
		cline.PoolMu.Lock()
		v := p.UseProxies
		cline.PoolMu.Unlock()
		if v == nil {
			return nil
		}
		copied := *v
		return &copied
	case "trae":
		return trae.TraeConfigUseProxies()
	case "other":
		return other.CurrentUseProxies()
	case "zen":
		return zen.ZenConfigUseProxies()
	case "workbuddy":
		return workbuddy.WkbdConfigUseProxies()
	}
	return nil
}

// setPlatformUseProxies 改写某平台的走池开关并落盘到该平台配置文件。
func setPlatformUseProxies(kind string, v *bool) {
	switch kind {
	case "cline":
		cline.MutateConfig(func(p *cline.AccountPool) { p.UseProxies = v })
	case "trae":
		trae.MutateTraeConfig(func(t *trae.TraeConfigData) { t.UseProxies = v })
	case "zen":
		zen.MutateZenConfigFile(func(z *zen.ZenConfigFileData) { z.ZenUseProxies = v })
	case "other":
		other.SetConfigUseProxies(v)
	case "workbuddy":
		workbuddy.MutateWkbdConfig(func(k *workbuddy.WkbdConfigData) { k.UseProxies = v })
	}
}

// PlatformUseProxies / SetPlatformUseProxies 导出面（装配根别名消费）。
func PlatformUseProxies(kind string) *bool    { return platformUseProxies(kind) }
func SetPlatformUseProxies(k string, v *bool) { setPlatformUseProxies(k, v) }

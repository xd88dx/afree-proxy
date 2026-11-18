package app

// 各平台「未绑定凭据是否走共享代理池」开关的统一读取入口。
//
// 2026-11-17 配置重组：平台键回归各平台配置文件——cline-config.json 的
// useProxies（AccountPool）、zen-config.json 的 zenUseProxies、other-config.json
// 的 Platform.UseProxies（逐子平台）、trae-config.json 的 useProxies、
// wkbd-config.json 的 useProxies。本文件只做 kind → 平台读取器的分发；
// 代理池页（"Global policy" 下拉）保存时仍按平台写各自文件。
//
// 其他 key 池平台（amd/tkhb、openrouter）已随平台删除移除；克隆新 key 池平台
// 时在 addPlatformUseProxiesWriter 登记（见 add-platform skill backend §6）。

import (
	"afree-proxy/internal/other"
)

// platformUseProxies 读取某平台的走池开关。nil = 该平台未配置（缺省走池）。
// kind 取 cline/trae/other/zen/workbuddy。
func platformUseProxies(kind string) *bool {
	switch kind {
	case "cline":
		return clineUseProxiesPtr()
	case "trae":
		return traeConfigUseProxies()
	case "other":
		return other.CurrentUseProxies()
	case "zen":
		return zenConfigUseProxies()
	case "workbuddy":
		return wkbdConfigUseProxies()
	}
	return nil
}

// setPlatformUseProxies 改写某平台的走池开关并落盘到该平台配置文件。只动这
// 一个字段，出口列表不变，因此不触发冷却/健康统计的清理链。
func setPlatformUseProxies(kind string, v *bool) {
	switch kind {
	case "cline":
		mutatePoolConfig(func(p *AccountPool) { p.UseProxies = v })
	case "trae":
		mutateTraeConfig(func(t *traeConfigData) { t.UseProxies = v })
	case "zen":
		mutateZenConfigFile(func(z *zenConfigFileData) { z.ZenUseProxies = v })
	case "other":
		other.SetConfigUseProxies(v)
	case "workbuddy":
		mutateWkbdConfig(func(k *wkbdConfigData) { k.UseProxies = v })
	}
}

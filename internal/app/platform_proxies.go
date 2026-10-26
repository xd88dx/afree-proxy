package app

// 各平台「未绑定凭据是否走共享代理池」开关的统一归属。
//
// 这个开关是代理池页上的下拉（"Global policy"），语义属于代理域，跟平台自身
// 的账号/key 数据无关；历史上却分散在各平台的配置文件里（.cline-accounts.json、
// .amd-config.json、.or-config.json、.th-config.json、.trae-config.json、
// .other-config.json 各存一份），改一个开关要记住去哪找。
//
// 现统一存进 proxy-config.json 的出口池段（见 proxy_pool_config.go），内存态
// 由 getProxyPool() 缓存，读取不落磁盘。平台包通过注入的 resolver 取值，
// 平台配置文件里不再保存该字段（update 时显式置 nil，旧值自然消失）。

import (
	"afree-proxy/internal/amd"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/tokenharbor"
	"afree-proxy/internal/trae"
)

func init() {
	amd.SetUseProxiesResolver(func() *bool { return platformUseProxies("amd") })
	openrouter.SetUseProxiesResolver(func() *bool { return platformUseProxies("oprt") })
	tokenharbor.SetUseProxiesResolver(func() *bool { return platformUseProxies("tkhb") })
	trae.SetUseProxiesResolver(func() *bool { return platformUseProxies("trae") })
	other.SetUseProxiesResolver(func() *bool { return platformUseProxies("other") })
}

// platformUseProxies 读取某平台的走池开关。nil = 该平台未配置（缺省走池）。
// kind 取 cline/amd/oprt/tkhb/trae/other/zen/workbuddy。
func platformUseProxies(kind string) *bool {
	c := getProxyPool()
	switch kind {
	case "cline":
		return c.ClineUseProxies
	case "amd":
		return c.AmdUseProxies
	case "oprt":
		return c.OprtUseProxies
	case "tkhb":
		return c.TkhbUseProxies
	case "trae":
		return c.TraeUseProxies
	case "other":
		return c.OtherUseProxies
	case "zen":
		return c.ZenUseProxies
	case "workbuddy":
		return c.WorkbuddyUseProxies
	}
	return nil
}

// setPlatformUseProxies 改写某平台的走池开关并落盘。只动这一个字段，
// 出口列表不变，因此不触发冷却/健康统计的清理链。
func setPlatformUseProxies(kind string, v *bool) {
	proxyPoolMu.Lock()
	next := *proxyPool
	proxyPoolMu.Unlock()
	switch kind {
	case "cline":
		next.ClineUseProxies = v
	case "amd":
		next.AmdUseProxies = v
	case "oprt":
		next.OprtUseProxies = v
	case "tkhb":
		next.TkhbUseProxies = v
	case "trae":
		next.TraeUseProxies = v
	case "other":
		next.OtherUseProxies = v
	case "zen":
		next.ZenUseProxies = v
	case "workbuddy":
		next.WorkbuddyUseProxies = v
	default:
		return
	}
	setProxyPool(&next)
}

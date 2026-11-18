package app

import (
	"afree-proxy/internal/other"
	"afree-proxy/internal/platform"
)

// ============================================================================
// 平台注册表装配（2026-11-17 结构重构 P3 第一步）
//
// 五平台以 Platform 适配器注册进 registry，共享层（platform_routability/
// platform_blacklist 等）逐步改查 registry——共享层不再 switch 各平台符号，
// 新平台注册即接入。适配器是既有判定函数的薄包装，行为逐字节等价：
// 各平台"凭据启用"语义不变（见 platform_routability.go 顶部口径表）。
//
// other 子平台是动态多实例：每个 slug 一个 "other:<slug>" 复合键。子平台
// 增删时 ReconcileOtherPlatforms 按当前配置重建注册（静态注册只做一次的
// 模型对动态子平台不成立）。
// ============================================================================

// platformRegistry 全局注册表（装配根唯一实例）。
var platformRegistry = platform.NewRegistry()

// ---- 静态四平台适配器（other 走动态注册）----

type clinePlatform struct{}

func (clinePlatform) Kind() string { return platform.KindCline }
func (clinePlatform) Routable() bool {
	p := loadPool()
	poolMu.Lock()
	defer poolMu.Unlock()
	for _, a := range p.Accounts {
		if a != nil && accountStatusLocked(a) == "active" && accountPoolEnabled(a) {
			return true
		}
	}
	return false
}

type traePlatform struct{}

func (traePlatform) Kind() string { return platform.KindTrae }
func (traePlatform) Routable() bool {
	// 账号总闸开（disabled=false）且反代开关开的账号数 > 0。
	return trae2apiRoutableCount() > 0
}

type workbuddyPlatform struct{}

func (workbuddyPlatform) Kind() string { return platform.KindWorkBuddy }
func (workbuddyPlatform) Routable() bool {
	if *workbuddySub == nil || (*workbuddySub).Pool == nil {
		return false
	}
	return len((*workbuddySub).Pool.AvailableUIDs()) > 0
}

type zenPlatform struct{}

func (zenPlatform) Kind() string { return platform.KindZen }
func (zenPlatform) Routable() bool {
	cfg := getZenConfig()
	for _, k := range cfg.Keys {
		if k != "" && zenKeyRoutingEnabled(k) {
			return true
		}
	}
	return false
}

// otherPlatform 单个子平台的适配器（kind = "other:<slug>"）。
type otherPlatform struct{ slug string }

func (p otherPlatform) Kind() string { return platOtherPrefix + p.slug }
func (p otherPlatform) Routable() bool {
	return otherPlatformHasRoutableKey(other.PlatformBySlug(p.slug))
}

// platOtherPrefix other 子平台复合键前缀（registry 键与封禁/路由判定同口径）。
const platOtherPrefix = "other:"

// registerStaticPlatforms 静态四平台注册（进程一次）。
func registerStaticPlatforms() {
	registerStaticPlatformsInto(platformRegistry)
}

// registerStaticPlatformsInto 把静态四平台注册进指定表（重建路径复用）。
func registerStaticPlatformsInto(r *platform.Registry) {
	r.Register(clinePlatform{})
	r.Register(traePlatform{})
	r.Register(workbuddyPlatform{})
	r.Register(zenPlatform{})
}

// ReconcileOtherPlatforms 按当前 other 配置重建子平台注册：整表重建（静态
// 四平台 + 现存子平台逐slug）。other 配置变更（增删子平台）后调用；频率极低
// （面板子平台增删才触发），适配器全是无状态结构体，整表重建最简单。
func ReconcileOtherPlatforms() {
	fresh := platform.NewRegistry()
	registerStaticPlatformsInto(fresh)
	for i := range other.Get().Platforms {
		fresh.Register(otherPlatform{slug: other.Get().Platforms[i].Slug})
	}
	platformRegistry.ResetFrom(fresh)
}

// init 装配：静态四平台注册。other 子平台动态：首次 Load 后由
// ReconcileOtherPlatforms 补齐（other.Load 的调用方负责触发）。
func init() {
	registerStaticPlatforms()
}

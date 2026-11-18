// Package platform 承载平台契约：kind 常量、Platform 能力点接口、注册表。
//
// 定位（2026-11-17 结构重构 P3）：平台代码逐步出仓为独立包（internal/cline、
// internal/zen、internal/trae、internal/workbuddy、internal/other），它们只
// import 本契约；装配根（internal/app）在启动时把各平台实现注册进 registry，
// 分发链/可路由性闸/封禁平台判定等共享层改由 registry 驱动——共享层不再
// switch 各平台符号，新增平台注册即接入（分发顺序语义由注册顺序承载，保持
// 与既有 routeModel 链一致）。
//
// 契约刻意最小：只收敛「共享层确实需要向平台问的问题」。能力点随平台出仓
// 逐步补入（如目录快照、凭据测试端点），不在出仓前预造接口。
package platform

import "sync"

// 平台 kind（与对外模型前缀对应；与历史 platXxx 字符串值完全一致——封禁表/
// 备份段/面板键都存这些字符串，改值即数据迁移）。
const (
	KindWorkBuddy = "workbuddy"
	KindZen       = "zen"
	KindCline     = "cline"
	KindTrae      = "trae"
	KindOther     = "other"
)

// Platform 平台能力点接口。各平台包提供实现，装配根注册。
//
// Routable 报告平台当前是否至少有一个可参与请求路由的凭据（各平台的
// "凭据启用"语义不同：cline=active+反代开关、trae=总闸+反代、wb=vendor
// 池可用账号、zen=参与路由的 key、other=RoutingEnabled key）。无法识别
// 的平台注册方应自行兜底，本契约不做默认值。
type Platform interface {
	// Kind 平台标识（KindXxx 常量之一；other 子平台以 "other:<slug>" 复合
	// ID 注册多个实例）。
	Kind() string
	// Routable 平台是否至少有一个可路由凭据（可路由性闸与 /v1/models
	// 展示门控同口径）。
	Routable() bool
}

// Registry 平台注册表。并发安全；Slice 返回注册顺序（分发链语义由注册
// 顺序承载）。
type Registry struct {
	mu    sync.RWMutex
	byKey map[string]Platform
	order []string // 注册顺序（byKey 键），Slice 按此输出
}

// NewRegistry 空注册表。
func NewRegistry() *Registry {
	return &Registry{byKey: map[string]Platform{}}
}

// Register 注册平台实现；同 Kind 覆盖（other 子平台按 "other:<slug>" 各自
// 独立键）。注册顺序保留首次出现位置（覆盖不改序）。
func (r *Registry) Register(p Platform) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := p.Kind()
	if _, exists := r.byKey[key]; !exists {
		r.order = append(r.order, key)
	}
	r.byKey[key] = p
}

// Lookup 按 Kind 取平台实现；未注册返回 nil。
func (r *Registry) Lookup(kind string) Platform {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byKey[kind]
}

// Slice 按注册顺序返回全部平台实现（分发链消费此序）。
func (r *Registry) Slice() []Platform {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Platform, 0, len(r.order))
	for _, k := range r.order {
		if p, ok := r.byKey[k]; ok {
			out = append(out, p)
		}
	}
	return out
}

// Kinds 按注册顺序返回全部 kind 键。
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// ResetFrom 用 source 的内容整体替换本表（动态平台集重建路径：other 子平台
// 增删后按当前配置重建注册）。持锁原子换表，观察方看到旧表或新表，不见中间态。
func (r *Registry) ResetFrom(source *Registry) {
	if source == nil {
		return
	}
	source.mu.RLock()
	keys := append([]string(nil), source.order...)
	table := make(map[string]Platform, len(source.byKey))
	for k, v := range source.byKey {
		table[k] = v
	}
	source.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey = table
	r.order = keys
}

package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ============ 配置与数据模型 ============

// Config 是 OpenRouter 平台的全部可配置状态，落盘于 DATA_DIR/.or-config.json。
// 与 zen 的 zenConfigData 同构（多 key 池 + 主/辅代理绑定 + 路由参与表），
// 复用 afree-proxy 主包已有的代理池与隔离语义，但**自成独立池**：
// 模型目录、凭据、用量、绑定都不与 zen/cline/workbuddy 混用。
type Config struct {
	// UseProxies 未绑定 key 是否走共享代理池（代理池页下拉）。nil=缺省=走池。
	UseProxies *bool `json:"useProxies,omitempty"`
	// BaseURL 上游 OpenAI 兼容端点，默认 https://openrouter.ai/api/v1。
	BaseURL string `json:"baseURL"`
	// Keys 多 key 池，请求按池调度策略轮转。OpenRouter key 形如 sk-or-v1-…。
	Keys []string `json:"keys,omitempty"`
	// KeyRoutingEnabled 路由参与表：key 明文 -> 是否进入请求轮转（面板"启用"列）。
	// nil/缺项 = 参与。新增 key 默认 false（三平台统一口径），需面板勾选启用。
	KeyRoutingEnabled map[string]bool `json:"keyRoutingEnabled,omitempty"`
	// KeyBindings 出口绑定表：key 明文 -> 主/辅代理。隔离模式下绑定决定出口。
	KeyBindings map[string]Binding `json:"keyBindings,omitempty"`
	// MaxConcurrency 上游最大并发（防 worker 瞬时超限），默认 8。
	MaxConcurrency int `json:"maxConcurrency"`
	// Retries 限流/网络错误重试次数，默认 3。
	Retries int `json:"retries"`
	// 用量统计（内存态 + 落盘聚合，面板展示用）：key 明文 -> 成功调用计数。
	Usage map[string]int64 `json:"usage,omitempty"`
	// Cooldown 冷却表：key 明文 -> 冷却截止时刻（内存态，不落盘）。
	Cool map[string]time.Time `json:"-"`
}

// Binding 一个 key 的出口绑定：主代理优先、辅代理兜底；双不可用 = 该 key
// 本轮跳过（隔离优先于可用性，绝不回退直连）。空值 = 未绑定 = 走全局规则。
type Binding struct {
	Main   string `json:"main,omitempty"`
	Backup string `json:"backup,omitempty"`
}

// DefaultAPIBase OpenRouter 官方 OpenAI 兼容端点。
const DefaultAPIBase = "https://openrouter.ai/api/v1"

// SeedModels 冷启动兜底模型目录（openrouter.ai/collections/free-models 实测
// 快照，2026-10-10）：仅在首次从上游 /api/v1/models 同步成功之前作为可服务
// 集合，同步成功后以 live 为准（含下线裁剪）。全部为 pricing 全零的免费条目。
var SeedModels = []Model{
	{ID: "openrouter/free", Name: "Free Models Router", Context: 200000, ToolCall: true},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b:free", Name: "NVIDIA: Nemotron 3 Ultra (free)", Context: 1000000},
	{ID: "thinkingmachines/inkling:free", Name: "Thinking Machines: Inkling (free)", Context: 1048576},
	{ID: "thinkingmachines/inkling-small:free", Name: "Thinking Machines: Inkling Small (free)", Context: 1048576},
	{ID: "nvidia/nemotron-3.5-lightning:free", Name: "NVIDIA: Nemotron 3.5 Lightning (free)", Context: 1000000},
	{ID: "dots-studio/dots-3-note-preview:free", Name: "Dots Studio: Dots3-Note Preview (free)", Context: 512000},
	{ID: "inclusionai/ling-3.1-flash", Name: "inclusionAI: Ling 3.1 Flash", Context: 262144},
	{ID: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free", Name: "NVIDIA: Nemotron 3 Nano Omni (free)", Context: 256000},
	{ID: "nvidia/nemotron-3-super-120b-a12b:free", Name: "NVIDIA: Nemotron 3 Super (free)", Context: 262144},
	{ID: "google/gemma-4-26b-a4b-it:free", Name: "Google: Gemma 4 26B A4B (free)", Context: 262144},
	{ID: "google/gemma-4-31b-it:free", Name: "Google: Gemma 4 31B (free)", Context: 262144},
	{ID: "apodex/apodex-1.1-mini:free", Name: "Apodex: Apodex 1.1 Mini (free)", Context: 262144},
	{ID: "poolside/laguna-s-2.1:free", Name: "Poolside: Laguna S 2.1 (free)", Context: 262144},
	{ID: "poolside/laguna-xs-2.1:free", Name: "Poolside: Laguna XS 2.1 (free)", Context: 262144},
	{ID: "cohere/north-mini-code:free", Name: "Cohere: North Mini Code (free)", Context: 256000},
	{ID: "nvidia/nemotron-3.5-content-safety:free", Name: "NVIDIA: Nemotron 3.5 Content Safety (free)", Context: 128000},
	{ID: "liquid/lfm-2.5-2.6b:free", Name: "LiquidAI: LFM2.5-2.6B (free)", Context: 65536},
}

// Model OpenRouter 免费模型条目（/v1/models 合并与面板展示用）。
type Model struct {
	ID       string
	Name     string
	Context  int
	ToolCall bool
}

// ModelPrefix 网关分发前缀：客户端以 "oprt:<model-id>" 形式调用本平台模型。
// oprt = openrouter 的缩写（or: 过于简短，模型列表里辨识度低）。
const ModelPrefix = "oprt:"

// DefaultConfig 出厂配置：key 池空、并发 8、重试 3、baseURL 官方端点。
func DefaultConfig() *Config {
	return &Config{
		BaseURL:        DefaultAPIBase,
		MaxConcurrency: 8,
		Retries:        3,
	}
}

// normalizeKeys 规范 key 池：去空去重，保持顺序。
func (c *Config) normalizeKeys() {
	cleaned := make([]string, 0, len(c.Keys))
	seen := map[string]bool{}
	for _, k := range c.Keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		cleaned = append(cleaned, k)
	}
	c.Keys = cleaned
}

// normalize 常规化：key 池去重、baseURL 去尾斜杠、并发/重试下限修正。
func (c *Config) normalize() {
	c.normalizeKeys()
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.BaseURL == "" {
		c.BaseURL = DefaultAPIBase
	}
	if c.MaxConcurrency <= 0 {
		c.MaxConcurrency = 8
	}
	if c.Retries < 0 {
		c.Retries = 0
	}
	if c.Cool == nil {
		c.Cool = map[string]time.Time{}
	}
}

// ReconcileRouting 整体替换 key 池后对齐路由参与表（保存 key 列表时调用）：
// 新引入的 key 默认不参与路由（面板勾选"启用"后才生效，三平台统一口径），
// 已有 key 保持原状态，被移除的 key 清理表项。
func (c *Config) ReconcileRouting(next, cur *Config) {
	old := map[string]bool{}
	for _, k := range cur.Keys {
		old[k] = true
	}
	nextSet := map[string]bool{}
	for _, k := range next.Keys {
		nextSet[k] = true
	}
	routing := map[string]bool{}
	for k, v := range next.KeyRoutingEnabled {
		if nextSet[k] {
			routing[k] = v
		}
	}
	for _, k := range next.Keys {
		if !old[k] {
			routing[k] = false
		}
	}
	if len(routing) == 0 {
		next.KeyRoutingEnabled = nil
		return
	}
	next.KeyRoutingEnabled = routing
}

// RoutingEnabled key 是否参与请求路由（面板"启用"列）。nil/缺项 = 参与。
func (c *Config) RoutingEnabled(key string) bool {
	if v, ok := c.KeyRoutingEnabled[key]; ok {
		return v
	}
	return true
}

// maskKey 面板展示的 key 掩码（前 8 位 + 省略号）。
func maskKey(k string) string {
	switch {
	case k == "":
		return "-"
	case len(k) <= 12:
		return k[:4] + "…"
	default:
		return k[:12] + "…"
	}
}

// MaskKey 对外暴露的掩码函数（管理 API 用；key 明文永不出口）。
func MaskKey(k string) string { return maskKey(k) }

// ============ 存储 ============

var (
	mu     sync.RWMutex
	cfg    = DefaultConfig()
	saveMu sync.Mutex
	loaded bool
)

// ConfigPath 数据文件路径（由主包注入，指向 DATA_DIR/.or-config.json）。
var ConfigPath string

// Load 读取持久化配置（主包装配期调用一次）。文件不存在或损坏时保留默认值
// （损坏文件改名留档，不静默覆盖）。
func Load(path string) {
	ConfigPath = path
	mu.Lock()
	defer mu.Unlock()
	if loaded {
		return
	}
	loaded = true
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
			fmt.Printf("  openrouter config corrupt JSON; moved to %s.corrupt-%s\n", path, stamp)
		}
		return
	}
	cfg.normalize()
}

// ============ 并发信号量 ============
//
// MaxConcurrency 上游最大并发（防单平台瞬时打满上游；与 zen 的 zenSem 同构）。
// Set/Load 时重建，正在飞行的请求持旧信号量完成，新请求换新信号量。

var (
	semMu sync.Mutex
	sem   chan struct{}
)

func rebuildSem() {
	n := Get().MaxConcurrency
	if n <= 0 {
		n = 8
	}
	semMu.Lock()
	sem = make(chan struct{}, n)
	semMu.Unlock()
}

// acquireSem 占一个并发槽位；ctx 取消时不再占用。返回 false 表示客户端已离开。
func acquireSem(ctx context.Context) bool {
	semMu.Lock()
	s := sem
	semMu.Unlock()
	if s == nil {
		return true
	}
	select {
	case s <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func releaseSem() {
	semMu.Lock()
	s := sem
	semMu.Unlock()
	if s != nil {
		<-s
	}
}

// Get 返回活配置指针（写时复制语义由调用方保证：先拷贝再 Set）。
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return cfg
}

// Set 替换配置：normalize → 落盘 → 裁剪已移除 key 的绑定/路由/用量/冷却。
func Set(next *Config) {
	next.normalize()
	mu.Lock()
	cfg = next
	mu.Unlock()
	save()
	rebuildSem()
	pruneRemovedKeys(next)
}

// save 原子落盘（临时文件 + rename）。Usage/Cool 为运行时字段不落盘。
func save() {
	saveMu.Lock()
	defer saveMu.Unlock()
	if ConfigPath == "" {
		return
	}
	mu.RLock()
	disk := *cfg
	mu.RUnlock()
	disk.Usage = nil
	disk.Cool = nil
	data, err := json.MarshalIndent(&disk, "", "  ")
	if err != nil {
		fmt.Printf("  openrouter config marshal failed: %v\n", err)
		return
	}
	tmp := ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  openrouter config save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, ConfigPath); err != nil {
		if werr := os.WriteFile(ConfigPath, data, 0600); werr != nil {
			fmt.Printf("  openrouter config replace failed: %v\n", err)
			return
		}
		_ = os.Remove(tmp)
	}
}

// pruneRemovedKeys 清理不再存在于 key 池的绑定/路由/用量/冷却条目。
func pruneRemovedKeys(c *Config) {
	if len(c.KeyBindings) == 0 && len(c.KeyRoutingEnabled) == 0 && len(c.Usage) == 0 && len(c.Cool) == 0 {
		return
	}
	valid := map[string]bool{}
	for _, k := range c.Keys {
		valid[k] = true
	}
	mu.Lock()
	for k := range c.KeyBindings {
		if !valid[k] {
			delete(c.KeyBindings, k)
		}
	}
	for k := range c.KeyRoutingEnabled {
		if !valid[k] {
			delete(c.KeyRoutingEnabled, k)
		}
	}
	for k := range c.Usage {
		if !valid[k] {
			delete(c.Usage, k)
		}
	}
	for k := range c.Cool {
		if !valid[k] {
			delete(c.Cool, k)
		}
	}
	if len(c.KeyBindings) == 0 {
		c.KeyBindings = nil
	}
	if len(c.KeyRoutingEnabled) == 0 {
		c.KeyRoutingEnabled = nil
	}
	mu.Unlock()
}

// KeyStatus 面板展示用的每 key 运行时状态（key 值打码，明文不出口）。
func KeyStatus() []map[string]any {
	c := Get()
	mu.RLock()
	defer mu.RUnlock()
	now := time.Now()
	out := make([]map[string]any, 0, len(c.Keys))
	for i, k := range c.Keys {
		st := map[string]any{
			"index":          i,
			"keyMask":        maskKey(k),
			"usage":          c.Usage[k],
			"routingEnabled": c.RoutingEnabled(k),
		}
		if b, ok := c.KeyBindings[k]; ok && (b.Main != "" || b.Backup != "") {
			st["proxyMain"] = b.Main
			st["proxyBackup"] = b.Backup
		}
		if until, ok := c.Cool[k]; ok && now.Before(until) {
			st["cooling"] = true
			st["cooldownUntil"] = until.Format(time.RFC3339)
		} else {
			st["cooling"] = false
		}
		out = append(out, st)
	}
	return out
}

// BindingOf key 的出口绑定；未绑定时 bound=false。
func BindingOf(key string) (main, backup string, bound bool) {
	c := Get()
	b, ok := c.KeyBindings[key]
	if !ok || (b.Main == "" && b.Backup == "") {
		return "", "", false
	}
	return b.Main, b.Backup, true
}

// ProxiesEnabled 未绑定 key 是否走共享代理池：缺省（nil）默认走池。
func ProxiesEnabled() bool {
	up := Get().UseProxies
	if up == nil {
		return true
	}
	return *up
}

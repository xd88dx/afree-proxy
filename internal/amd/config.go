package amd

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
//
// AMD Radeon Cloud（developer.amd.com.cn/radeon/tokenfactory 签发 key 的
// Public Free Model APIs）是纯 key 池平台：rc- 前缀的 API key 按池调度策略
// 轮转，未绑定 key 走共享代理池（代理池页下拉），隔离模式下每 key 可绑
// 主/辅出口。结构与 internal/openrouter 同构，自成独立池。

// Config 是 AMD 平台的全部可配置状态，落盘于 DATA_DIR/.amd-config.json。
type Config struct {
	// UseProxies 未绑定 key 是否走共享代理池（代理池页下拉）。nil=缺省=走池。
	UseProxies *bool `json:"useProxies,omitempty"`
	// BaseURL 上游 OpenAI 兼容端点，默认官方 Public Free Model APIs。
	BaseURL string `json:"baseURL"`
	// Keys 多 key 池，请求按池调度策略轮转。AMD key 形如 rc-<48 hex>。
	Keys []string `json:"keys,omitempty"`
	// KeyRoutingEnabled 路由参与表：key 明文 -> 是否进入请求轮转（面板"启用"列）。
	// nil/缺项 = 参与。新增 key 默认 false（各平台统一口径），需面板勾选启用。
	KeyRoutingEnabled map[string]bool `json:"keyRoutingEnabled,omitempty"`
	// KeyBindings 出口绑定表：key 明文 -> 主/辅代理。隔离模式下绑定决定出口。
	KeyBindings map[string]Binding `json:"keyBindings,omitempty"`
	// MaxConcurrency 上游最大并发（上游每 key 并发上限 8，面板默认 8）。
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

// DefaultAPIBase AMD Radeon Cloud Public Free Model APIs 端点
// （developer.amd.com.cn/radeon/tokenfactory 是控制台页面，API 根在 /radeon/api/v1）。
const DefaultAPIBase = "https://developer.amd.com.cn/radeon/api/v1"

// ModelPrefix 网关分发前缀：客户端以 "amd:<model-id>" 形式调用本平台模型。
const ModelPrefix = "amd:"

// SeedModels 冷启动兜底模型目录（/v1/models 免认证实测快照，2026-10-10；
// 仅收录 output_modalities 含 text 的对话模型，OCR 专用模型 MinerU2.5-Pro
// 不适合 chat 网关，排除）。同步可达后以 live 为准。
var SeedModels = []Model{
	{ID: "DeepSeek-V4-Flash", Name: "DeepSeek-V4-Flash", Context: 1048576, ToolCall: true},
	{ID: "DeepSeek-V4.1-Flash", Name: "DeepSeek-V4.1-Flash", Context: 1048576, ToolCall: true},
	{ID: "DeepSeek-V4-Flash-Vision-Exp", Name: "DeepSeek-V4-Flash-Vision-Exp", Context: 1048576, ToolCall: true},
	{ID: "GLM-5.3-Flash", Name: "GLM-5.3-Flash", Context: 262144, ToolCall: true},
	{ID: "MiMo-V2.6-Flash", Name: "MiMo-V2.6-Flash", Context: 1048576, ToolCall: true},
	{ID: "Qwen3.8-Flash-Next", Name: "Qwen3.8-Flash-Next", Context: 262144, ToolCall: true},
	{ID: "Qwen3.8-27B", Name: "Qwen3.8-27B", Context: 262144, ToolCall: true},
	{ID: "Gemma-4-31B-IT", Name: "Gemma-4-31B-IT", Context: 262144, ToolCall: true},
	{ID: "MiniCPM5-2B", Name: "MiniCPM5-2B", Context: 131072, ToolCall: true},
}

// Model AMD 免费模型条目（/v1/models 合并与面板展示用）。
type Model struct {
	ID       string
	Name     string
	Context  int
	ToolCall bool
}

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
// 新引入的 key 默认不参与路由（面板勾选"启用"后才生效），已有 key 保持原状态，
// 被移除的 key 清理表项。
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

// maskKey 面板展示的 key 掩码（前 12 位 + 省略号，与 openrouter 同口径）。
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

// ConfigPath 数据文件路径（由主包注入，指向 DATA_DIR/.amd-config.json）。
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
			fmt.Printf("  amd config corrupt JSON; moved to %s.corrupt-%s\n", path, stamp)
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
		fmt.Printf("  amd config marshal failed: %v\n", err)
		return
	}
	tmp := ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Printf("  amd config save failed: %v\n", err)
		return
	}
	if err := os.Rename(tmp, ConfigPath); err != nil {
		if werr := os.WriteFile(ConfigPath, data, 0600); werr != nil {
			fmt.Printf("  amd config replace failed: %v\n", err)
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

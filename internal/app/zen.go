package app

import (
	"afree-proxy/internal/kit"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ZenModel opencode zen 免费模型定义
// ZenModel zen 模型条目。
//
// 并发约定（重要）：写者（initZenModels / applyZenCatalog / learnZenEndpoint /
// loadZenEndpoints）一律在 zenModelsMu 下"改副本再挂回"，绝不就地改写已发布的
// 结构体。因此 resolveZenModel 返回的指针在锁外可安全读取任意字段——请求路径
// 正是在锁外读 Upstream/Source/Context。新增写者必须遵守 copy-on-write，否则
// 就是与请求路径的数据竞争。
type ZenModel struct {
	ID        string   `json:"id"`
	Aliases   []string `json:"aliases,omitempty"`
	Context   int      `json:"context"`
	Output    int      `json:"output"`
	Source    string   `json:"source"`               // seed=内置 / registry=公共目录同步 / synced=zen 上游同步
	Upstream  string   `json:"upstream,omitempty"`   // 强制原生上游端点: "responses"；空=默认 chat/completions
	ToolCall  bool     `json:"toolCall,omitempty"`   // 目录声明的 tool_call 能力
	Reasoning bool     `json:"reasoning,omitempty"`  // 目录声明的 reasoning 能力
	Attach    bool     `json:"attachment,omitempty"` // 目录声明的图片/附件输入能力
}

// zenSeedModels 冷启动兜底列表：仅在"首次成功同步之前 + 目录/CLI 都不可达"
// 时作为可服务的模型集（离线也能用）。它不再是 ID 的来源——运行时模型表由
// syncZenModels 从 live 目录（官方 CLI `opencode models` 读的同一份
// models.opencode.ai 数据）按价格门推导，种子条目若通不过价格门会被移除。
// 种子里保留的别名（mimo/ling/nemotron/pickle…）与 muse-spark 的
// Upstream=responses 提示会随同名 live 条目沿用。
// 免费资格 = 公共目录 cost.input==0 && cost.output==0（big-pickle 这类无
// "free" 后缀的免费模型也因此入选；deepseek-v4-flash 这类 status=deprecated
// 的条目即使 cost 为 0 也不入选）。
var zenSeedModels = []ZenModel{
	{ID: "mimo-v2.5-free", Aliases: []string{"mimo-v2.5", "mimo"}, Context: 200000, Output: 32000, Source: "seed"},
	{ID: "nemotron-3-ultra-free", Aliases: []string{"nemotron-3-ultra", "nemotron"}, Context: 1000000, Output: 128000, Source: "seed"},
	{ID: "nemotron-3.5-lightning-free", Aliases: []string{"nemotron-3.5-lightning", "nemotron-lightning"}, Context: 262144, Output: 262144, Source: "seed"},
	{ID: "ling-3.0-flash-fin-free", Aliases: []string{"ling-3.0-flash-fin", "ling-fin", "ling"}, Context: 262144, Output: 32768, Source: "seed"},
	// big-pickle：zen 创始免费模型（无 free 后缀，目录 cost 0/0，1.18.31 实测可用；
	// opencode zen 免费层的默认别名，永久保留在种子中）
	{ID: "big-pickle", Aliases: []string{"pickle"}, Context: 200000, Output: 32000, Source: "seed"},
	// muse-spark 只在原生 /v1/responses 端点上可用：官方 opencode CLI 实测
	// 对该模型只发 POST /zen/v1/responses（带 tools + reason、返回 SSE），
	// chat/completions 上该模型 500（需经 responses 原生调用再转回 chat 形态）
	{ID: "muse-spark-1.3-contributor-free", Aliases: []string{"muse-spark-contributor"}, Context: 1048576, Output: 131072, Source: "seed", Upstream: "responses"},
	{ID: "muse-spark-1.2-contributor-free", Aliases: []string{"muse-spark"}, Context: 1048576, Output: 131072, Source: "seed", Upstream: "responses"},
}

var (
	zenModelsMu sync.RWMutex
	zenModels   = make(map[string]*ZenModel) // 主表:ID
	zenAliases  = make(map[string]*ZenModel) // 别名表
)

// zenPruneGrace 目录裁剪宽限期。上游 live 目录存在分钟级闪断：实测同一模型
// 1 分钟内先被 prune（02:01:14）再被加回（02:02:05），立即删除会让客户端
// 请求恰好落在窗口内被严格门控误拒 400。被裁条目转入宽限表：仍可解析
// （routeModel/门控继续放行，请求照发上游——真下线由上游返回真实错误，
// 该类错误不冷却 key），但不进面板模型列表；重新同步到则
// 原样回活，超过宽限期历经多轮同步仍未回来，视为真下线彻底清除。
const zenPruneGrace = 30 * time.Minute

type zenGraceEntry struct {
	m        *ZenModel
	aliased  []string // 随模型一起被裁掉的别名（解析时一并兜底）
	prunedAt time.Time
}

var zenGraceModels = map[string]zenGraceEntry{} // 读写均走 zenModelsMu

// zenAPIBase 上游基地址。2026-10-15 实测：官方目录（models.opencode.ai）
// 已把 opencode provider 的 api 指向 /inference/v1，CLI 直连新后端；旧
// /zen/v1 经 console 中转后匿名免费请求恒 403 FreeTierError（中转剥
// Authorization 并改名 x-opencode-request，校验无法通过），故默认与存量
// 配置一并迁移到 /inference/v1（见 loadZenConfig 的迁移逻辑）。
const zenAPIBase = "https://opencode.ai/inference/v1"

// zenLegacyBase 已废弃的旧基地址，仅用于存量配置迁移识别。
const zenLegacyBase = "https://opencode.ai/zen/v1"

// ZenModelPrefix zen 免费模型的网关前缀（与 workbuddy 的 cn:/global:、
// openrouter 的 or: 同思路）：平台越来越多，加前缀让客户端在模型列表里
// 一眼分清归属，也避免与 Cline 免费模型表里的同名条目（如
// cline-free/laguna 之类）静默冲突。裸模型名（无前缀）仍照旧解析——
// 存量 IDE 配置与 combo target 不受影响。
const ZenModelPrefix = "zen:"

// zenCanonicalID 归一化模型 ID：剥掉 zen: 前缀与遗留的 opencode/ 前缀并
// 小写化，返回目录里的真实 ID。所有解析入口（resolveZenModel / 冲突判定 /
// 出站改写）先过它，保证带不带前缀、大小写如何都走同一条路。
func zenCanonicalID(id string) string {
	return normalizeZenModelID(strings.TrimPrefix(strings.TrimSpace(id), ZenModelPrefix))
}

// zenPrefixedID 把目录 ID 加上网关前缀（/v1/models 输出用）。
func zenPrefixedID(id string) string { return ZenModelPrefix + id }

func initZenModels() {
	zenModelsMu.Lock()
	defer zenModelsMu.Unlock()
	if len(zenModels) > 0 {
		return
	}
	for _, m := range zenSeedModels {
		cp := m
		zenModels[cp.ID] = &cp
		for _, a := range cp.Aliases {
			zenAliases[a] = &cp
		}
	}
}

// normalizeZenModelID 规范化 zen 模型 ID 用于表查找：trim + 小写 + 去掉
// "opencode/" 前缀。zen 模型 ID（种子与官方目录 id）均为小写；客户端可能发
// "Big-Pickle" 或 "opencode/big-pickle"，不规范化的查找会落空，进而把请求
// 错误路由到 cline 池（或让 dead/alias 标记不可达）。
func normalizeZenModelID(id string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "opencode/")
}

// resolveZenModel 解析模型名到 zen 模型。支持 "zen:" 前缀、遗留的
// "opencode/" 前缀与裸 ID，以及别名（大小写不敏感）。前缀与别名都可解析：
// 面板与 /v1/models 输出 zen:<id>，存量配置用裸 id 继续可用。
// 别名优先: 同步来的付费同名模型(如 deepseek-v4-flash)不会覆盖 free 别名解析。
// 410 迁移别名是最后兜底：原始 ID 不存在时返回继任模型（真实模型永远优先，
// 继任者上线不会被弃用别名遮蔽）。
func resolveZenModel(id string) (*ZenModel, bool) {
	id = zenCanonicalID(id)
	if id == "" {
		return nil, false
	}
	zenModelsMu.RLock()
	defer zenModelsMu.RUnlock()
	if m, ok := zenAliases[id]; ok {
		return m, true
	}
	if m, ok := zenModels[id]; ok {
		return m, true
	}
	if m, ok := resolveZenGrace(id); ok {
		return m, true
	}
	// 弃用别名兜底：原始 ID 从模型表移除后（410 迁移过）落到继任模型。
	// 真实模型永远优先于别名——前面所有分支都没命中才会走到这里。
	if repl := zenDeprecatedReplacement(id); repl != "" {
		if m, ok := zenModels[repl]; ok {
			return m, true
		}
	}
	return nil, false
}

// resolveZenGrace 宽限表兜底解析：ID、别名都能认。需在 zenModelsMu 读锁内
// 调用；宽限过期的条目视为不存在（彻底清除由下一轮同步的 prune 顺带做）。
func resolveZenGrace(id string) (*ZenModel, bool) {
	for gid, e := range zenGraceModels {
		if time.Since(e.prunedAt) > zenPruneGrace {
			continue
		}
		if gid == id || e.m.ID == id {
			return e.m, true
		}
		for _, a := range e.aliased {
			if a == id {
				return e.m, true
			}
		}
	}
	return nil, false
}

// isZenFreeModel 免费判定: seed 白名单、live 同步（已过价格门）、
// learned（学习器改过端点但仍是同一批已入选免费模型）、或通用的 -free 后缀。
// big-pickle/union-alpha 无 free 后缀，靠 Source=="live"/"learned" 入选。
func isZenFreeModel(m *ZenModel) bool {
	if m == nil {
		return false
	}
	if m.Source == "seed" || m.Source == "live" || m.Source == "learned" {
		return true
	}
	return strings.HasSuffix(m.ID, "-free")
}

// zenProbeModel 管理面板 per-key "Test" 探测用的模型，按优先级：
//  1. big-pickle —— zen 免费层的默认别名（mint 收割的首选同款），最稳；
//  2. Source=="live" 的最小 id 模型 —— live 条目来自官方目录同步，
//     是"上游当前确实在供"的证明，种子条目可能早已下架；
//  3. 任意 free 模型的最小 id —— 冷启动且同步不可达时的纯种子兜底。
//
// native-responses 模型也可能被选中——testZenKey 按其 Upstream 字段走对应
// 上游调用，无需特判。目录为空时返回 nil，探测直接报错。
func zenProbeModel() *ZenModel {
	// 目录表是惰性填充的（面板打开模型页 / 定时同步 / 首个请求才会触发）：
	// 探测必须自给自足，进程刚启动、谁都没碰过模型页时也要能测。
	initZenModels()
	zenModelsMu.RLock()
	defer zenModelsMu.RUnlock()
	if m, ok := zenModels["big-pickle"]; ok && isZenFreeModel(m) {
		return m
	}
	var best *ZenModel
	for _, m := range zenModels {
		if !isZenFreeModel(m) || (m.Source != "live" && m.Source != "learned") {
			continue
		}
		if best == nil || m.ID < best.ID {
			best = m
		}
	}
	if best != nil {
		return best
	}
	for _, m := range zenModels {
		if !isZenFreeModel(m) {
			continue
		}
		if best == nil || m.ID < best.ID {
			best = m
		}
	}
	return best
}

// resolveZenFreeModel 只解析免费 zen 模型
func resolveZenFreeModel(id string) (*ZenModel, bool) {
	m, ok := resolveZenModel(id)
	if !ok || !isZenFreeModel(m) {
		return nil, false
	}
	return m, true
}

// routeModel 决定请求走哪个上游: "zen" / "cline" / "reject"
// zen 免费模型 -> zen; zen 付费模型 -> reject(400); 其他 -> cline
func routeModel(id string) string {
	id = strings.TrimSpace(id)
	// combo 别名兜底：调用方通常已把 model 改写为 target，这里防止 combo ID
	// 直接进入路由（按 combo 声明的平台走，不解析为普通模型）
	if c := resolveCombo(id); c != nil {
		return c.Platform
	}
	// zen 表查找统一用小写去前缀的规范 ID：dead 标记与 410 别名都以规范 ID
	// 为键，客户端发 "Big-Pickle" / "opencode/big-pickle" 时不做归一就查不到，
	// 请求会误落到 cline 池（或让标记形同虚设）。
	zenID := normalizeZenModelID(id)
	initZenModels()
	// 上游 400 "Model is unavailable" 的死亡标记：直接拒绝，不再每次请求
	// 都白烧一个上游往返。下次目录同步成功（或 TTL 过期）后自动解除。
	// 例外：该 id 若已有 410 继任别名，别名优先——迁移后请求仍有真实去处，
	// 不该被一条陈旧的 400 标记挡住。
	if zenModelDead(zenID) && zenDeprecatedReplacement(zenID) == "" {
		log.Printf("  route: %q marked unavailable by upstream, rejecting until next catalog sync", zenID)
		return "reject"
	}
	if zm, ok := resolveZenModel(zenID); ok {
		if isZenFreeModel(zm) {
			// 与 cline 模型表冲突时(几乎不可能)走 cline
			initModelsCache()
			modelsMu.Lock()
			_, inCline := modelsCache[zenID]
			modelsMu.Unlock()
			if !inCline {
				return "zen"
			}
		} else {
			return "reject"
		}
	}
	return "cline"
}

// ============ zen 配置 ============

type zenCompactConfig struct {
	Auto         bool   `json:"auto"`         // 官方风格摘要压缩开关
	Buffer       int    `json:"buffer"`       // 预留输出缓冲 token,默认 20000
	KeepTokens   int    `json:"keepTokens"`   // 尾部保留 token 预算,默认 8000
	SummaryModel string `json:"summaryModel"` // 摘要模型,空=用请求模型
	MaxSummary   int    `json:"maxSummary"`   // 摘要最大输出 token,默认 4096
}

type zenConfigData struct {
	Key            string           `json:"key"`            // 兼容字段：始终等于 Keys[0]（旧版单 key 读取用）
	Keys           []string         `json:"keys,omitempty"` // zen 多 key 池，请求按 round-robin 轮转
	// OrgID：OAuth 账户的组织 ID（x-opencode-org-id）。CLI v2 在 OAuth
	// 账户流量中发该头，原始 API key 流量中没有。网关无法从 key 字符串
	// 推导它，空值即省略（正确）；使用 OAuth token 作 key 时，应从
	// 账户元数据填入，否则上游看到 OAuth token 却无 org 头，与官方
	// 客户端形态不一致。绝不编造。
	OrgID          string           `json:"orgID,omitempty"`
	BaseURL        string           `json:"baseURL"`
	MaxConcurrency int              `json:"maxConcurrency"` // zen 上游最大并发,防 worker 瞬时超限,默认 8
	Retries        int              `json:"retries"`        // 限流/网络错误重试次数,默认 3
	Compaction     zenCompactConfig `json:"compaction"`
	// Deprecated 迁移字段：旧版把代理别名存在独立表里，normalizeZenKeys
	// 会把别名折叠回代理行（#fragment）并清空本字段。
	ProxyAliases map[string]string `json:"proxyAliases,omitempty"`
	// 代理隔离/备用/排除表/绑定上限已迁出到 proxy-config.json 的隔离与绑定段
	// (见 proxy_binding_config.go)—— 代理池与隔离规则是全局能力,不该由某个
	// 平台的配置文件托管。
	//
	// KeyBindings 反过来留在这里:它的键是 key 明文,属于 key 自己的数据。放进
	// proxy-config.json 会让同一批明文 key 在 zen-config.json 的 keys 数组和
	// 代理文件里各存一份,冗余且容易写不一致。其他平台(amd/or/th/other)的绑定
	// 表同理留在各自配置文件里。
	KeyBindings map[string]proxyBinding `json:"keyBindings,omitempty"`
	//
	// zen key 的路由参与表：key 明文 -> 是否进入请求轮转（面板"启用"列勾选）。
	// nil/缺项 = 参与。路由启用是铸造的前提（父集语义）：禁用路由的 key 不参与
	// 选号，收割机也不再为它铸造会话（zenKeyAutoHarvestEnabled）。
	KeyRoutingEnabled map[string]bool `json:"keyRoutingEnabled,omitempty"`
	// SessionRotateMinutes 已废弃（2026-10-27，上游 a99d94b：会话改为每次
	// API 调用新铸，轮换无意义）。字段保留只为读取旧配置文件不报错，
	// 装载后零作用；新落盘恒写 0。
	SessionRotateMinutes int `json:"sessionRotateMinutes"`
	// PinProxyPerKey 每个 key/账号固定一个出口代理（默认 true）。指针类型：
	// nil = 旧配置文件没有该键 → 按默认开启处理（见 pinProxyPerKeyEnabled）。
	// 关闭后恢复按请求轮转（zen 按 key 的 IP 指纹会重新出现，仅排障用）。
	PinProxyPerKey *bool `json:"pinProxyPerKey,omitempty"`
}

// proxyRuleKinds 自动分配规则（ProxyExclusions/ProxyBindCaps）的基础平台
// kind，与前端凭据页 kind / REORDER_KINDS 同一口径。
var proxyRuleKinds = map[string]bool{
	"acc": true, "trae": true, "key": true, "wb": true, "amd": true,
	"or": true, "th": true, "other": true,
}

// otherRuleKindRe 匹配 Other 子平台粒度的规则 kind（other:<slug>）。面板把
// Other 渲染成归类行，其规则按子平台独立配置；slug 由后端生成（op_xxxxxxxx），
// 这里只校验形态，避免子平台被删/临时加载失败时整表保存失败。
var otherRuleKindRe = regexp.MustCompile(`^other:[A-Za-z0-9_-]{1,64}$`)

// proxyRuleKindValid 规则 kind 是否合法：基础平台，或 other:<slug>。
func proxyRuleKindValid(kind string) bool {
	return proxyRuleKinds[kind] || otherRuleKindRe.MatchString(kind)
}

// proxyRuleDirectKey 直连在排除表里的哨兵键。池条目一律带 scheme
// （http(s)://、socks5:// 等），不会与之冲突。含义 = 不允许该平台的自动分配
// （一键代理 / 延迟重排）产出"直连"出口 —— 例如 tokenharbor 不对大陆服务，
// 直连必被上游拒绝，批量分配就不再把直连同给任何 tokenharbor 凭据；手动绑定
// 与运行时路由不受影响。池变动清理时该键永不删除。
const proxyRuleDirectKey = "__direct__"

// pruneProxyExclusions 按代理池清理排除表：已不在池内的代理条目丢弃（它们
// 永远不会被自动分配命中，留着只是垃圾配置），直连哨兵键保留 —— 它不是池
// 条目，且不随池变动失效。面板保存池与配置导入（跨机迁移）共用此口径。
func pruneProxyExclusions(ex map[string][]string, pool []string) map[string][]string {
	if len(ex) == 0 {
		return ex
	}
	inPool := make(map[string]bool, len(pool))
	for _, p := range pool {
		inPool[p] = true
	}
	out := make(map[string][]string, len(ex))
	for p, kinds := range ex {
		if inPool[p] || p == proxyRuleDirectKey {
			out[p] = kinds
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// zenKeyAutoHarvestEnabled 报告 key 是否参与自动铸造（启动补缺 / 403 触发 /
// 周期巡检 / 面板 Mint）。铸造跟随路由启用：路由关 = 该 key 不出请求，无需
// 会话，也不铸造；空 key 不铸造。
func zenKeyAutoHarvestEnabled(key string) bool {
	if key == "" {
		return false
	}
	return zenKeyRoutingEnabled(key)
}

// zenKeyRoutingEnabled key 是否参与请求路由（"启用"列）。nil/缺项 = 参与；
// 空 key 不参与（配置池里不会有空项，这里只做防御）。
func zenKeyRoutingEnabled(key string) bool {
	if key == "" {
		return false
	}
	cfg := getZenConfig()
	if v, ok := cfg.KeyRoutingEnabled[key]; ok {
		return v
	}
	return true
}

// zenHasRoutableKey 是否存在参与路由的启用 key（面板模型目录/刷新的展示
// 门控，与 platformHasRoutableCredential 的 zen 分支同判定）。
func zenHasRoutableKey() bool {
	for _, k := range getZenConfig().Keys {
		if zenKeyRoutingEnabled(k) {
			return true
		}
	}
	return false
}

// reconcileZenKeyRouting 整体替换 key 池后对齐"参与路由"表（面板保存 key 列表
// 时调用）。统一口径：新引入的 key 默认不参与路由（面板勾选"启用"后才生效，
// 与 Cline/WorkBuddy 的新增身份默认禁用一致）；已有 key 保持原状态；被移除的
// key 清理表项，避免陈旧数据。
func reconcileZenKeyRouting(next, cur *zenConfigData) {
	oldKeys := map[string]bool{}
	for _, k := range cur.Keys {
		oldKeys[k] = true
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
		if !oldKeys[k] && k != "" {
			routing[k] = false // 新 key 强制默认禁用（覆盖可能的陈旧表项）
		}
	}
	if len(routing) == 0 {
		next.KeyRoutingEnabled = nil
		return
	}
	next.KeyRoutingEnabled = routing
}

// setZenKeyRoutingEnabled 写入 key 的路由参与状态（面板"启用"勾选）。
// 写时复制：zenConfig 是被请求路径并发读取的活配置。
func setZenKeyRoutingEnabled(index int, enabled bool) error {
	cfg := getZenConfig()
	if index < 0 || index >= len(cfg.Keys) {
		return fmt.Errorf("key index out of range")
	}
	key := cfg.Keys[index]
	if key == "" {
		return fmt.Errorf("empty key cannot participate in routing")
	}
	next := *cfg
	en := make(map[string]bool, len(cfg.KeyRoutingEnabled)+1)
	for k, v := range cfg.KeyRoutingEnabled {
		en[k] = v
	}
	en[key] = enabled
	next.KeyRoutingEnabled = en
	setZenConfig(&next)
	return nil
}

// setAllZenKeysRoutingEnabled 一键启用/禁用全部 key 的路由参与（面板入口）。
// 空 key 跳过。返回实际改写的 key 数。
func setAllZenKeysRoutingEnabled(enabled bool) int {
	cfg := getZenConfig()
	en := make(map[string]bool, len(cfg.KeyRoutingEnabled)+len(cfg.Keys))
	for k, v := range cfg.KeyRoutingEnabled {
		en[k] = v
	}
	n := 0
	for _, k := range cfg.Keys {
		if k == "" {
			continue
		}
		en[k] = enabled
		n++
	}
	next := *cfg
	next.KeyRoutingEnabled = en
	setZenConfig(&next)
	return n
}

// reorderZenKeys 按旧 index 置换重排 zen key 池（管理面板拖拽排序）。
// order[i] = 重排后第 i 个位置上的原 index；面板只见掩码不见 key 明文，
// 因此协议用 index 置换而非 key 值。order 必须覆盖全部 key（缺失的按原相对
// 顺序追加在尾部，重复 index 幂等跳过；越界 index 整体拒绝）。
// 持久化、Key=Keys[0] 兼容字段同步、轮转游标归零都由 setZenConfig 统一处理
// ——重排后 round-robin 从第一个 key 重新开始，与新顺序的语义一致。
// 冷却/token/会话粘性都按 key 明文存储，与顺序无关，重排零影响。
func reorderZenKeys(order []int) error {
	cfg := getZenConfig()
	n := len(cfg.Keys)
	if len(order) == 0 {
		return fmt.Errorf("order is required")
	}
	next := make([]string, 0, n)
	seen := make(map[int]bool, n)
	for _, idx := range order {
		if idx < 0 || idx >= n {
			return fmt.Errorf("key index out of range: %d", idx)
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		next = append(next, cfg.Keys[idx])
	}
	if len(next) != n {
		for i, k := range cfg.Keys {
			if !seen[i] {
				next = append(next, k)
			}
		}
	}
	nc := *cfg
	nc.Keys = next
	setZenConfig(&nc)
	return nil
}

func defaultZenConfig() *zenConfigData {
	return &zenConfigData{
		// 不再有匿名 "public" 兜底：没有 key 就是空池，平台不可路由。
		Keys:           nil,
		Key:            "",
		BaseURL:        zenAPIBase,
		MaxConcurrency: 8,
		Retries:        3,
		// SessionRotateMinutes 不再赋默认值：会话轮换已随 2026-10-27
		// fresh-per-call 语义下线（上游见 a99d94b）。字段保留只为兼容读旧
		// 配置文件（装载后零作用），新落盘写 0。
		// 每 key 固定出口：2026-10-21 起 zen 按 key 记录出口 IP，轮转会
		// 让整池 key 被判可疑，故默认开启（无代理时自动退回直连）。
		PinProxyPerKey: boolPtr(true),
		Compaction: zenCompactConfig{
			Auto:       true,
			Buffer:     20000,
			KeepTokens: 8000,
			MaxSummary: 4096,
		},
	}
}

var (
	zenConfig   = loadZenConfig()
	zenConfigMu sync.Mutex
)

// 并发信号量：zen 上游最大并发（maxConcurrency 配置，rebuildZenSem 重建）。
var (
	zenSem     chan struct{}
	zenStateMu sync.Mutex
)

func init() {
	rebuildZenSem()
}

func rebuildZenSem() {
	cfg := getZenConfig()
	n := cfg.MaxConcurrency
	if n <= 0 {
		n = 8
	}
	zenStateMu.Lock()
	zenSem = make(chan struct{}, n)
	zenStateMu.Unlock()
}

// isRateLimited 限流信号识别: 429/503 直接命中; 502/403 按错误体关键词。
// 关键词含配额耗尽类措辞——它们与限流同样应触发冷却/换 key，而不是当作
// 普通错误立刻回给客户端。
func isRateLimited(status int, body string) bool {
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return true
	}
	if status == http.StatusBadGateway || status == http.StatusForbidden {
		low := strings.ToLower(body)
		for _, kw := range []string{
			"resourceexhausted", "limit reached", "rate limit", "too many", "overloaded", "busy",
			"quota", "exceeded", "insufficient",
		} {
			if strings.Contains(low, kw) {
				return true
			}
		}
	}
	return false
}

// normalizeZenKeys 规范 key 池：迁移旧单 key 字段、去空去重、剔除遗留的匿名
// 哨兵 "public"（2026-10-26 起不再支持匿名访问）、同步兼容字段 Key = Keys[0]。
// 允许空池：没有 key 时 zen 平台不可路由（目录隐藏、请求明确报错），不再回退
// 到无凭据的 "public"。
func normalizeZenKeys(cfg *zenConfigData) {
	if len(cfg.Keys) == 0 && cfg.Key != "" {
		cfg.Keys = []string{cfg.Key}
	}
	cleaned := make([]string, 0, len(cfg.Keys))
	seen := map[string]bool{}
	for _, k := range cfg.Keys {
		k = strings.TrimSpace(k)
		if k == "" || k == "public" || seen[k] {
			continue
		}
		seen[k] = true
		cleaned = append(cleaned, k)
	}
	cfg.Keys = cleaned
	if len(cleaned) == 0 {
		cfg.Key = ""
	} else {
		cfg.Key = cleaned[0]
	}
	// 旧版别名表（ProxyAliases）的折叠已随代理池拆出，见
	// proxy_pool_config.go 的 foldLegacyProxyAliases：代理行不再属于 zen 配置，
	// 这里不能再去改它。本字段保留只为读取旧配置文件。
}

func loadZenConfig() *zenConfigData {
	// 配置本体与 sessions/modelAliases/endpoints 同住 zen-config.json，
	// 这里只取 zen 段（损坏留档与迁移逻辑都在 loadZenConfigFileLocked 里）。
	zenFileMu.Lock()
	file := loadZenConfigFileLocked()
	zenFileMu.Unlock()
	cfg := &file.zenConfigData
	normalizeZenKeys(cfg)
	if cfg.BaseURL == "" {
		cfg.BaseURL = zenAPIBase
	}
	// 存量配置迁移：旧默认 /zen/v1 对匿名免费层已失效（恒 403），
	// 只迁移恰好等于旧默认值的配置；用户显式改过的其它地址不动。
	if strings.TrimRight(cfg.BaseURL, "/") == zenLegacyBase {
		cfg.BaseURL = zenAPIBase
		log.Printf("zen baseURL migrated: %s -> %s (legacy zen endpoint rejects anonymous free-tier traffic)", zenLegacyBase, zenAPIBase)
	}
	// PinProxyPerKey 缺省物化（上游 3758eb7）：旧配置文件没有该键时按"开启"
	// 落定为显式值。使用侧 pinProxyPerKeyEnabled 本就把 nil 解释为开启，这里
	// 物化让缺省语义在读取后即可见、下次保存即写回配置文件。
	if cfg.PinProxyPerKey == nil {
		cfg.PinProxyPerKey = boolPtr(true)
	}
	return cfg
}

func saveZenConfig() {
	zenConfigMu.Lock()
	next := *zenConfig
	zenConfigMu.Unlock()
	// 只覆盖 zen 段，sessions/modelAliases/endpoints 原样保留。
	mutateZenConfigFile(func(f *zenConfigFileData) {
		f.zenConfigData = next
	})
}

func getZenConfig() *zenConfigData {
	zenConfigMu.Lock()
	defer zenConfigMu.Unlock()
	return zenConfig
}

// setZenKeyBindings 只替换 zen key 的代理绑定段并落盘。不走 setZenConfig ——
// 那条路会重建信号量、重置轮转游标、按 key 列表清理各种状态,对"只改绑定"
// 是过度副作用。
func setZenKeyBindings(m map[string]proxyBinding) {
	zenConfigMu.Lock()
	next := *zenConfig
	next.KeyBindings = m
	zenConfig = &next
	zenConfigMu.Unlock()
	saveZenConfig()
}

func setZenConfig(c *zenConfigData) {
	normalizeZenKeys(c)
	zenConfigMu.Lock()
	zenConfig = c
	zenConfigMu.Unlock()
	saveZenConfig()
	rebuildZenSem()
	// 清理已移除 key 的轮转状态
	valid := map[string]bool{}
	for _, k := range c.Keys {
		valid[k] = true
	}
	zenKeyMu.Lock()
	for k := range zenKeyCool {
		if !valid[k] {
			delete(zenKeyCool, k)
		}
	}
	zenKeyIdx = 0
	zenKeyMu.Unlock()
	// 会话粘性与收割计数同样按 key 存储，移除的 key 必须一并清掉：否则长跑容器
	// 上轮换 key 会让 .zen-sessions.json 与两个收割表单调增长，被删掉的旧 key
	// 的粘性身份还留在盘上。
	pruneZenKeyState(valid)
	// 路由参与表随 key 列表清理：移除 key 的记录只剩泄漏价值，不清理会让
	// 长跑部署上配置文件单调增长。
	if len(c.KeyRoutingEnabled) > 0 {
		for k := range c.KeyRoutingEnabled {
			if !valid[k] {
				delete(c.KeyRoutingEnabled, k)
			}
		}
		if len(c.KeyRoutingEnabled) == 0 {
			c.KeyRoutingEnabled = nil
		}
	}
	// key 列表变化都要重算 egress 绑定：新 key 分配空闲代理、被移除的 key
	// 释放代理（代理池变化侧的挂钩在 setProxyPool，本地代理池不在 zen 配置里）。
	reconcileEgressBindings()
}

// validateProxyList 校验代理列表格式: 支持 http/https/socks5/socks5h, 必须包含 host:port。
func validateProxyList(proxies []string) error {
	for _, p := range proxies {
		line := strings.TrimSpace(p)
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			return fmt.Errorf("代理格式无效 %q: %v", line, err)
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("代理 %q 协议不受支持（支持 http/https/socks5/socks5h）", line)
		}
		if u.Host == "" {
			return fmt.Errorf("代理 %q 缺少 host:port", line)
		}
		if _, _, err := net.SplitHostPort(u.Host); err != nil {
			return fmt.Errorf("代理 %q 缺少端口: %v", line, err)
		}
	}
	return nil
}

// ============ zen 多 key 轮转（round_robin 默认策略） ============

// maskZenKey 管理面板与 mint 结果里展示 key 的掩码形式。
// 不复用 kit.Truncate：它对短于上限的串原样返回，短 key 会被整只打印出来。
func maskZenKey(k string) string {
	switch {
	case k == "":
		return "-"
	case len(k) <= 6:
		return "…" // 太短，连前缀都不给
	default:
		return k[:6] + "…"
	}
}

var (
	zenKeyMu  sync.Mutex
	zenKeyIdx int
	// zenKeyTokens 每 key token 记账（内存态）：今日/累计消耗与最近使用。
	zenKeyTokens = map[string]zenKeyTokenStats{}
	zenKeyCool   = map[string]time.Time{}
)

// pickZenKey round-robin 选取一个未冷却的 key；全部冷却时按轮转顺序返回下一个。
// 隔离模式（proxy_binding.go）下，绑定主备用出口都不可用的 key 被排除出所有
// 候选轮 —— 隔离优先于可用性；全部 key 都被挡住时返回 ""，调用方报
// "无可用出口"而不是静默直连。返回 "" 也表示未配置任何 key。
//
// egress 钉定层（3758eb7，egress.go）：显式 KeyBindings 之下，未绑定 key 在
// pinProxyPerKey 开启且代理池非空时自动分配一个稳定出口（重启不变），代理比
// key 少时多出来的 key 被过滤——它们的流量若继续发出（直连或借用别人的出口），
// 上游就会把"一个 key 多个 IP"的指纹重新打上，这正是要消除的。
// zenNoKeyError 轮转取不到 key 时的错误：区分"没有配置任何 key"与"全部
// 被代理隔离挡住"，避免运维按空池排查代理池。
func zenNoKeyError() error {
	if len(getZenConfig().Keys) == 0 {
		return fmt.Errorf("no zen key configured: add an enabled key in the admin panel")
	}
	// egress 钉定生效时，"全被挡"最常见的原因是代理比 key 少（一代理一 key），
	// 用容量口径报错并给出可操作建议，别让人按"绑定出口冷却"去排查。
	if egressBindingActive() {
		return egressShortfallError("zen", egressNSZen, getZenConfig().Keys)
	}
	return fmt.Errorf("no zen key routable: all configured keys' bound proxies are cooling or removed (proxy isolation)")
}

// zenBlockedExitErr 固定 key（pinKey 面板 Test / ZEN_PIN_KEY）出口不可用时
// 的错误：显式绑定 = 主备都冷却/被删（隔离口径文案）；无显式绑定 = egress
// 自动钉定没分到出口（池容量不足，容量口径文案）。
func zenBlockedExitErr(key string) error {
	if main, backup, bound := keyBindingOf(key); bound {
		return errors.New(blockedExitMessage(
			fmt.Sprintf("key#%d bound proxies unavailable (cooling or removed); probe aborted by proxy isolation", keyIndex(key)),
			main, backup))
	}
	return egressShortfallError("zen", egressNSZen, getZenConfig().Keys)
}

func pickZenKey() string {
	keys := getZenConfig().Keys
	if len(keys) == 0 {
		return ""
	}
	isolation := proxyIsolationEnabled()
	// 绑定可用性只依赖叶子锁，先在循环外批量算好，三轮选择零额外加锁。
	blocked := map[string]bool{}
	if isolation {
		for _, k := range keys {
			main, backup, bound := keyBindingOf(k)
			if bound && !boundProxiesRoutable(main, backup) {
				blocked[k] = true
			}
		}
	}
	// 面板"启用"列关掉的 key 不参与任何一轮候选，也不会被自动铸造
	// （zenKeyAutoHarvestEnabled 跟随路由启用，见 KeyRoutingEnabled 字段注释：
	// 路由启用是铸造的前提）。
	routingOff := map[string]bool{}
	for _, k := range keys {
		if !zenKeyRoutingEnabled(k) {
			routingOff[k] = true
		}
	}
	// egress 钉定（3758eb7）：显式 KeyBindings 之下的自动层——钉定生效
	// （开关开 + 池非空）却没分到自动出口的 key 整体过滤，直连或借用别人的
	// 出口都会重新打上"一 key 多 IP"指纹。快照在 zenSessMu 之外取（egressMu
	// 自带锁、不嵌套）。
	noEgress := map[string]bool{}
	if egressBindingActive() {
		for _, k := range keys {
			if _, ok := egressProxyForZen(k); !ok {
				noEgress[k] = true
			}
		}
	}
	alive := 0
	for _, k := range keys {
		if !blocked[k] && !routingOff[k] && !noEgress[k] {
			alive++
		}
	}
	if alive == 0 {
		return ""
	}
	// 先取 live 会话集合（zenSessMu）再进 zenKeyMu：两把锁不嵌套，避免与
	// 收割路径（持 zenSessMu 时不取 zenKeyMu，反之亦然）形成锁序反转。
	live := zenLiveKeys(keys)
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	now := time.Now()
	// 先清理过期冷却
	for k, until := range zenKeyCool {
		if now.After(until) {
			delete(zenKeyCool, k)
		}
	}
	// 三轮候选（保持既有优先级）：live 优先（未 mint 的 key 必 403，见
	// zen_session.go 顶部注释）→ 未冷却 → 全冷却时兜底（保持请求流动）。
	// 每轮候选按轮转顺序生成，最终由账号调度策略（round_robin/fill/random，
	// 与 Cline 账号池共用同一配置）挑选。三轮都带着 blocked/routingOff/
	// noEgress 过滤——没出口的 key 连"冷却重试"都不该发出去。
	buildCandidates := func(requireLive, ignoreCooling bool) []string {
		var out []string
		for i := 0; i < len(keys); i++ {
			k := keys[(zenKeyIdx+i)%len(keys)]
			if blocked[k] || routingOff[k] || noEgress[k] {
				continue
			}
			if requireLive && !live[k] {
				continue
			}
			if !ignoreCooling {
				if _, cooling := zenKeyCool[k]; cooling {
					continue
				}
			}
			out = append(out, k)
		}
		return out
	}
	candidates := buildCandidates(true, false)
	if len(candidates) == 0 {
		candidates = buildCandidates(false, false)
	}
	if len(candidates) == 0 {
		candidates = buildCandidates(false, true)
	}
	if len(candidates) == 0 {
		return ""
	}

	var picked string
	switch globalStrategy() {
	case "fill":
		// 池序第一个候选（与 Cline 账号池 fill 语义一致：用满一个再换）
		set := map[string]bool{}
		for _, k := range candidates {
			set[k] = true
		}
		for _, k := range keys {
			if set[k] {
				picked = k
				break
			}
		}
		if picked == "" {
			picked = candidates[0]
		}
	case "random":
		// rand.IntN：并发安全的真随机源。不能用 time.Now().UnixNano()%n ——
		// Windows 墙钟是粗粒度中断时钟（0.5~15.6ms 一跳），同一跳内的连续
		// 请求拿到相同时间戳，random 会退化成固定选一个。
		picked = candidates[rand.IntN(len(candidates))]
	default:
		// round_robin：轮转头的候选，游标推进到它之后
		picked = candidates[0]
		for i, k := range keys {
			if k == picked {
				zenKeyIdx = (i + 1) % len(keys)
				break
			}
		}
	}
	return picked
}

// zenKeyTokenStats 单个 key 的 token 记账：今日/累计消耗（prompt+completion）
// 与跨日日期、最近成功时间。仅内存态。
type zenKeyTokenStats struct {
	Today      int64
	Total      int64
	Date       string
	LastUsedAt time.Time
}

// recordZenKeyTokens 记录 key 本次请求消耗的 token，自动处理跨日重置，
// 累计值不重置；同时刷新最近使用时间。tokens<=0 时忽略。
func recordZenKeyTokens(key string, tokens int64) {
	if key == "" || tokens <= 0 {
		return
	}
	zenKeyMu.Lock()
	today := time.Now().Format("2006-01-02")
	st := zenKeyTokens[key]
	if st.Date != today {
		st.Date = today
		st.Today = 0
	}
	st.Today += tokens
	st.Total += tokens
	st.LastUsedAt = time.Now()
	zenKeyTokens[key] = st
	zenKeyMu.Unlock()
}

// cooldownZenKey 将 key 置为冷却，冷却期内 round-robin 跳过它。
// 返回实际生效的冷却时长（0 = 空 key 未冷却）——
// 调用点的日志必须打印这个值而不是原始解析值，否则"缺头默认 1 分钟 / 24h 上限"
// 的换算不会体现在日志里。
func cooldownZenKey(key string, d time.Duration) time.Duration {
	if key == "" {
		return 0
	}
	if d <= 0 {
		d = time.Minute
	}
	if d > maxCooldown {
		d = maxCooldown
	}
	zenKeyMu.Lock()
	zenKeyCool[key] = time.Now().Add(d)
	zenKeyMu.Unlock()
	return d
}

// zenKeyCooling 查询 key 是否处于冷却期。
func zenKeyCooling(key string) bool {
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	until, ok := zenKeyCool[key]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(zenKeyCool, key)
		return false
	}
	return true
}

// zenKeyCooldownUntil 只读查询冷却截止时刻（不清理过期项）。
// 管理面板"Test"命中 429 后用它回报预计恢复时间——上游的 Retry-After 决定
// 冷却时长（上限 24h），不写进日志的话面板是唯一能看到的观测点。
func zenKeyCooldownUntil(key string) (time.Time, bool) {
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	until, ok := zenKeyCool[key]
	return until, ok
}

// uncoolZenKey 清除 key 的冷却。管理面板"Test"探测成功后调用，语义与 cline
// 账号的"测试成功即复位"一致：一次真实的 2xx 是"该 key 现在能用"的最强证据，
// 比干等 Retry-After 更可信。对未冷却/无关 key 是无害 no-op。
func uncoolZenKey(key string) {
	if key == "" {
		return
	}
	zenKeyMu.Lock()
	delete(zenKeyCool, key)
	zenKeyMu.Unlock()
}

// zenCallOpts 单次上游调用的可选参数（变参：既有 callZenAPI/callZenResponsesAPI
// 调用点零改动）。目前只有 pinKey：把本次调用的**所有**尝试固定在一个 key 上。
// 与 ZEN_PIN_KEY 环境变量的区别：那是进程级排障开关，会影响所有并发请求；
// pinKey 只作用于这一次调用（管理面板的 per-key "Test" 按钮），绝不影响正常轮转。
type zenCallOpts struct {
	pinKey string
}

// zenKeyStatus 每个 key 的运行时状态（管理面板展示用，key 值打码）。
func zenKeyStatus() []map[string]any {
	keys := getZenConfig().Keys
	// 会话状态先快照（zenSessMu），再进 zenKeyMu：两把锁不嵌套。
	sess := make([]zenSessionSnapshot, len(keys))
	for i, k := range keys {
		sess[i] = zenSessionSnapshotOf(k)
	}
	// 出口绑定同样先快照：egress 路径取 zenConfigMu/egressMu，不能与 zenKeyMu 嵌套。
	egress := zenEgressSnapshotOf(keys)
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	now := time.Now()
	out := make([]map[string]any, 0, len(keys))
	for i, k := range keys {
		st := map[string]any{
			"index":   i,
			"keyMask": maskZenKey(k),
			"current": i == zenKeyIdx%maxInt(len(keys), 1),
			// 路由参与状态（面板"启用"列勾选，勾掉 = 不进入请求轮转）。
			"routingEnabled": zenKeyRoutingEnabled(k),
			// live 会话状态：面板据此看出哪些 key 已有粘性会话。
			"sessionLive": sess[i].Live,
			"session":     sess[i].Session,
		}
		if ts, ok := zenKeyTokens[k]; ok {
			st["tokensToday"] = ts.Today
			st["tokensTotal"] = ts.Total
			if !ts.LastUsedAt.IsZero() {
				st["lastUsed"] = ts.LastUsedAt.Format(time.RFC3339)
			}
		}
		// 代理绑定（隔离模式，面板编辑用）。stale = 绑定的代理已不在当前
		// 代理池里（被删除/改写），该 key 会被隔离一直跳过，必须提示。
		// 直连哨兵不参与 stale 判定。
		if main, backup, bound := keyBindingOf(k); bound {
			st["proxyMain"] = main
			st["proxyBackup"] = backup
			st["proxyStale"] = (main != "" && !isEgressDirect(main) && proxyIdxInPool(main) < 0) ||
				(backup != "" && !isEgressDirect(backup) && proxyIdxInPool(backup) < 0)
		}
		if sess[i].CreatedAt > 0 {
			st["createdAt"] = time.Unix(sess[i].CreatedAt, 0).Format(time.RFC3339)
		}
		// 出口绑定：面板必须能看出"这个 key 为什么不在轮转里"。
		// 没有绑定的 key 在绑定策略下等于停用（见 pickZenKey）。
		// 快照已在进 zenKeyMu 之前算好（egress.go 的锁不与 zenKeyMu 嵌套）。
		if e := egress[i]; e.proxy != "" {
			st["proxy"] = maskProxyURL(e.proxy)
			st["proxyIndex"] = e.index
		} else if k == "public" {
			st["proxy"] = "rotating (no key)"
		} else if e.bound {
			st["proxy"] = ""
			st["noEgress"] = true
		}
		if until, cooling := zenKeyCool[k]; cooling && now.Before(until) {
			st["cooling"] = true
			st["cooldownUntil"] = until.Format(time.RFC3339)
		} else {
			st["cooling"] = false
		}
		out = append(out, st)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// pinnedZenKey 取 ZEN_PIN_KEY 指定的固定 key（1-based 序号，如 "2"）。
// 排障/单 key 直测用：所有 attempt 都用该 key，避免轮换污染归因。
// 未设置或越界时返回 ""（正常轮转）。
func pinnedZenKey() string {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ZEN_PIN_KEY")))
	if err != nil || n < 1 {
		return ""
	}
	keys := getZenConfig().Keys
	if n > len(keys) {
		return ""
	}
	return keys[n-1]
}

// ============ zen 上游调用 ============

// zenGateToolSpecs 免费层两个端点必须携带的 opencode 工具集（工具名与官方
// CLI 一致）。校验规则 2026-10-15 在新 inference 后端重新实测：缺工具 →
// 403；工具在但 parameters 为空 schema → 403；描述文本、tool_choice、UA
// 均不参与校验。注入的定义（描述+schema）统一取 zen_gate_tools.go 的官方
// CLI 2.1.4 原文（2026-10-26 认证抓包），本表只保留名字与精简描述作兜底/名录用途。
// v2 与 v1 的区别：bash/task/todowrite 被移除，新增 question/subagent/
// execute，顺序为 CLI 发送顺序。单一来源：改名/增删必须同时满足两个
// 端点的校验，只改这里。
var zenGateToolSpecs = []struct{ name, desc string }{
	{"edit", "Edit a file"},
	{"glob", "Find files by glob pattern"},
	{"grep", "Search file contents"},
	{"question", "Ask the user a question"},
	{"read", "Read a file"},
	{"shell", "Execute a shell command"},
	{"skill", "Load a skill"},
	{"subagent", "Run a subagent"},
	{"webfetch", "Fetch a web page"},
	{"websearch", "Search the web"},
	{"write", "Write a file"},
	{"execute", "Run JavaScript in a confined Code Mode runtime"},
}

// zenGateTools chat 端点形态：tools[].{type,function:{name,description,parameters}}。
// 返回 []any 与客户端解码出来的 tools 同型（下游要拼接两者）。
// parameters 必须是官方真实 schema（空 schema 会被新后端 403，见
// zen_gate_tools.go）；嵌入定义缺失时才退回精简描述+空 schema 兜底。
func zenGateTools() []any {
	defs := zenGateToolDefs()
	out := make([]any, 0, len(zenGateToolSpecs))
	if len(defs) > 0 {
		for _, d := range defs {
			out = append(out, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        d.Name,
					"description": d.Description,
					"parameters":  d.ParametersChat,
				},
			})
		}
		return out
	}
	for _, s := range zenGateToolSpecs {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        s.name,
				"description": s.desc,
				"parameters":  zenEmptySchema(),
			},
		})
	}
	return out
}

// buildZenBody 构造 zen 请求体:只带 OpenAI 兼容字段,改写模型为 zen ID
func buildZenBody(params map[string]any, stream bool) map[string]any {
	body := map[string]any{}
	for _, key := range passThroughKeys {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}
	for _, key := range []string{"model", "messages", "max_tokens", "max_completion_tokens", "stream"} {
		if val, ok := params[key]; ok {
			body[key] = val
		}
	}
	if stream {
		body["stream"] = true
	}
	if model, ok := params["model"].(string); ok {
		if m, ok := resolveZenModel(model); ok {
			body["model"] = m.ID
		}
	}
	delete(body, "reasoning_effort")
	delete(body, "reasoningEffort")
	// FreeTier gate（2026-09-18 实测解码）：zen 免费层 chat 端点校验请求体
	// 是否携带 opencode 工具集，缺失则 403（"can only be used from within
	// OpenCode"）。恒注入 12 个 v2 规范工具名保证过闸。
	// tool_choice：CLI v2 在 chat 路径完全不发该字段（2026-10-26 认证
	// 抓包，body keys 无 tool_choice），上游缺省即 auto，简单问答仍
	// 回文本（CLI hi 实测）。旧网关的 none/auto 强制值是 v1 遗留指纹，
	// 2026-10-26 实验：TLS/UA/头/工具全部对齐后仍 429，tool_choice 是
	// 剩余差异之一，故改为：客户端显式指定时原样透传，否则省略。
	delete(body, "functions")
	delete(body, "function_call")
	clientTools := zenClientTools(params)
	if len(clientTools) == 0 {
		body["tools"] = zenGateTools()
	} else {
		body["tools"] = zenMergeTools(clientTools, zenGateTools())
	}
	if tc, ok := params["tool_choice"]; ok {
		body["tool_choice"] = tc
	} else {
		delete(body, "tool_choice")
	}
	ensureZenEnvMarkerChat(body)
	return body
}

// zenEnvBlock returns the CLI's <env> marker block. 2026-10-26 ablation
// (raw uTLS call, same IP/account/TLS/headers/tools, body varied one
// element at a time): removing the system prompt -> 429; truncating it
// to 8000 chars -> 429; last 1000 chars alone -> 200; env block alone
// -> 200; env block without "Working directory:" -> 429; "Working
// directory:" outside <env> -> 429; fake directory value -> 200.
// Upstream therefore checks, in a system message, for a line
// "Working directory:" inside <env>...</env> — the CLI always injects
// this block (working directory, platform, git status, date) at the
// end of its system prompt, and a generic OpenAI client does not.
// The value itself is not validated, so the gateway uses its own
// working directory (truthful for the gateway process) rather than
// fabricating a client's directory.
func zenEnvBlock() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		wd = "/tmp/opencode"
	}
	return "<env>\n  Working directory: " + wd + "\n</env>"
}

func zenTextHasEnvMarker(s string) bool {
	return strings.Contains(s, "<env>") && strings.Contains(s, "Working directory:")
}

// ensureZenEnvMarkerChat appends the env block to the first system
// message (or prepends a system message with just the block) when no
// system/developer message already carries the marker. It never
// removes or rewrites client content; it only appends the marker the
// free tier checks for, at the end, mirroring CLI's layout.
func ensureZenEnvMarkerChat(body map[string]any) {
	msgs, ok := body["messages"].([]any)
	if !ok {
		return
	}
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		if s, ok := mm["content"].(string); ok && zenTextHasEnvMarker(s) {
			return
		}
	}
	// No marker yet: append to the first string system message if one
	// exists, otherwise prepend a system message with just the block.
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		if s, ok := mm["content"].(string); ok {
			mm["content"] = s + "\n\n" + zenEnvBlock()
			return
		}
	}
	body["messages"] = append([]any{map[string]any{"role": "system", "content": zenEnvBlock()}}, msgs...)
}

func ensureZenEnvMarkerResponses(body map[string]any) {
	if s, ok := body["instructions"].(string); ok && zenTextHasEnvMarker(s) {
		return
	}
	if s, ok := body["instructions"].(string); ok && s != "" {
		body["instructions"] = s + "\n\n" + zenEnvBlock()
		return
	}
	body["instructions"] = zenEnvBlock()
}

// zenClientTools 提取客户端工具（chat 嵌套形态 tools[]，兼容已废弃的
// functions[] 形态），返回可直接发给 chat 端点的嵌套定义。
func zenClientTools(params map[string]any) []any {
	var out []any
	if ct, ok := params["tools"].([]any); ok {
		for _, t := range ct {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			fn, ok := tm["function"].(map[string]any)
			if !ok {
				continue
			}
			n, _ := fn["name"].(string)
			if n == "" {
				continue
			}
			if fn["parameters"] == nil {
				fn["parameters"] = zenEmptySchema() // null schema 上游判非法
			}
			out = append(out, tm)
		}
	}
	if len(out) > 0 {
		return out
	}
	if fns, ok := params["functions"].([]any); ok {
		for _, f := range fns {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			n, _ := fm["name"].(string)
			if n == "" {
				continue
			}
			fn := map[string]any{"name": n}
			if d, ok := fm["description"].(string); ok {
				fn["description"] = d
			}
			if p, ok := fm["parameters"]; ok && p != nil {
				fn["parameters"] = p
			} else {
				fn["parameters"] = zenEmptySchema()
			}
			out = append(out, map[string]any{"type": "function", "function": fn})
		}
	}
	return out
}

// zenMergeTools 合并客户端工具与 gate 占位工具：客户端定义优先，同名 gate
// stub 跳过。gate 只校验工具名是否存在，带真实 schema 的客户端定义同样过闸，
// 而 stub 抢先会让模型拿到空参数结构（如客户端 read 的 file_path 丢失）。
func zenMergeTools(clientTools []any, gateTools []any) []any {
	merged := make([]any, 0, len(clientTools)+len(gateTools))
	provided := map[string]bool{}
	for _, t := range clientTools {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tm["function"].(map[string]any)
		if fn == nil {
			continue
		}
		n, _ := fn["name"].(string)
		if n == "" || provided[n] {
			continue
		}
		provided[n] = true
		merged = append(merged, tm)
	}
	for _, gt := range gateTools {
		tm, ok := gt.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tm["function"].(map[string]any)
		n, _ := fn["name"].(string)
		if n == "" || provided[n] {
			continue
		}
		provided[n] = true
		merged = append(merged, tm)
	}
	return merged
}

func zenEmptySchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// ============ zen 上游调用：原生 /v1/responses 端点 ============
//
// 背景: 部分 zen 免费模型（如 muse-spark）在 chat/completions 端点上 500，
// 官方 opencode CLI 对它们只发原生 POST /zen/v1/responses（Responses 形态
// 请求体 + SSE 事件流响应）。网关对这类模型走同样路径：请求体按 Responses
// 形态构造，响应再转回 chat completions 形态，上下游都不感知差异。

// buildZenResponsesBody 把 chat 请求参数映射为 Responses 形态请求体：
// messages -> input（system/developer 指令并入 instructions），
// max_tokens/max_completion_tokens -> max_output_tokens。
// 字段经官方 CLI 实际请求逐字段核对：reasoning.summary=auto、
// include=[reasoning.encrypted_content]、tool_choice 缺省 auto、
// prompt_cache_key=会话 ID 均为 CLI 常发字段，缺省会与原生形态不一致。
// promptKey 为本次上游会话 ID（调用方传入本次请求的 sess_ 值）。
// 无 stream 形参：上游恒为 stream=true（见函数内注释）。
func buildZenResponsesBody(params map[string]any, modelID string, promptKey string) map[string]any {
	body := map[string]any{
		"model":  modelID,
		"stream": true, // FreeTier gate 硬要求：responses 端点只接受 stream=true，
		// stream=false（即使显式传）上游按"非 CLI"请求 403。网关非流式客户端
		// 由调用方聚合（responsesSSEToChat），body 恒发 true。
		"store": false,
	}
	if promptKey != "" {
		body["prompt_cache_key"] = promptKey
	}
	var input []any
	if msgs, ok := params["messages"].([]any); ok {
		for _, m := range msgs {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			role, _ := mm["role"].(string)
			content := responsesContentToInput(mm["content"])
			if role == "system" || role == "developer" {
				// system/developer 指令提升为顶层 instructions（Responses 标准字段；
				// 官方 CLI 发 role=developer 条目且无顶层 instructions——网关把
				// 历史 system/developer 内容并入 instructions 保持等价）
				if s, ok := content.(string); ok && s != "" {
					if prev, _ := body["instructions"].(string); prev != "" {
						body["instructions"] = prev + "\n\n" + s
					} else {
						body["instructions"] = s
					}
				}
				continue
			}
			if role == "tool" {
				// tool 结果 -> function_call_output（Responses 标准形态）
				callID, _ := mm["tool_call_id"].(string)
				out := ""
				switch o := content.(type) {
				case string:
					out = o
				default:
					if b, err := json.Marshal(content); err == nil {
						out = string(b)
					}
				}
				input = append(input, map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  out,
				})
				continue
			}
			entry := map[string]any{"role": role, "content": content}
			// assistant 历史 tool_calls -> function_call 条目（保留调用链）。
			// 内容与工具调用分开：assistant 的文本内容仍在 entry 里，每个
			// tool_call 各 append 一条 function_call 输入（旧实现 break 只保
			// 第一条且把 entry 换成 function_call，丢掉正文文本）。
			if role == "assistant" {
				if tcs, ok := mm["tool_calls"].([]any); ok {
					for ci, tc := range tcs {
						tcm, ok := tc.(map[string]any)
						if !ok {
							continue
						}
						fn, _ := tcm["function"].(map[string]any)
						id, _ := tcm["id"].(string)
						name := ""
						args := ""
						if fn != nil {
							name, _ = fn["name"].(string)
							args, _ = fn["arguments"].(string)
						}
						if id == "" {
							// function_call 的 call_id 是必填：缺 id 的历史
							// 条目会让整个请求 400；补一个稳定的合成 id
							id = fmt.Sprintf("call_hist_%d_%d", len(input), ci)
						}
						if args == "" {
							args = "{}" // 空串不是合法 JSON 参数
						} else if !json.Valid([]byte(args)) {
							args = repairToolArguments(args)
						}
						input = append(input, map[string]any{
							"type":      "function_call",
							"id":        id,
							"call_id":   id,
							"name":      name,
							"arguments": args,
						})
					}
				}
			}
			input = append(input, entry)
		}
	}
	if len(input) == 0 {
		input = []any{map[string]any{"role": "user", "content": ""}}
	}
	body["input"] = input
	// 输出上限：Responses 字段名是 max_output_tokens（官方 CLI 实测发送该字段）
	if mt := asInt(params["max_tokens"]); mt > 0 {
		body["max_output_tokens"] = mt
	} else if mt := asInt(params["max_completion_tokens"]); mt > 0 {
		body["max_output_tokens"] = mt
	}
	// 透传采样参数（Responses 与 chat 同名）
	for _, k := range []string{"temperature", "top_p"} {
		if v, ok := params[k]; ok {
			body[k] = v
		}
	}
	// reasoning_effort 在 Responses 形态下保留为 reasoning.effort（官方 CLI 实测发送）
	if eff, ok := params["reasoning_effort"].(string); ok && eff != "" {
		body["reasoning"] = map[string]any{"effort": eff, "summary": "auto"}
	} else if eff, ok := params["reasoningEffort"].(string); ok && eff != "" {
		body["reasoning"] = map[string]any{"effort": eff, "summary": "auto"}
	} else {
		// 官方 CLI 即使无 effort 也发 reasoning.summary=auto；缺该字段的
		// 请求与原生形态不一致，补默认值保持一致
		body["reasoning"] = map[string]any{"summary": "auto"}
	}
	// include 原生字段：官方 CLI 实测发送 reasoning.encrypted_content；
	// 缺省时补齐以匹配原生请求形态
	body["include"] = []any{"reasoning.encrypted_content"}
	// FreeTier gate（2026-09-18 实测解码）：/v1/responses 端点同样校验请求体
	// 是否携带 opencode 工具集，缺失则 403；且必须 flat 形态 + tool_choice=auto
	//（chat 嵌套形态 400 missing name、tool_choice=none 400 only auto allowed）。
	// 恒注入 11 个规范工具名 + auto——auto 是 responses 端点唯一接受的值。
	// 客户端工具（chat 嵌套或已是 flat）优先于同名 gate stub，schema 才不丢。
	body["tools"] = zenMergeResponsesTools(zenClientFlatTools(params))
	body["tool_choice"] = "auto"
	ensureZenEnvMarkerResponses(body)
	return body
}

// setZenGateHeaders 写入 zen 免费门要求的客户端身份头，两条上游路径（chat
// /responses）共用——此前两处逐行重复，改一处漏一处就会让某条路径掉头。
// 直接写 map（而非 Header.Set）以保留小写头名：官方 CLI 发的就是小写，
// Set 会规范化为 X-Opencode-...；HTTP 头大小写不敏感，但字节级对齐更安全。
// Authorization/Content-Type/User-Agent 因需承载变量，由调用方先行设置。
// 2026-10-26 实测（CLI v2.1.4 认证抓包）：
//   - CLI v2 在两条路径都发 x-opencode-session-id, x-opencode-session,
//     x-session-affinity, x-session-id，四个头同值（新鲜会话时四者
//     相等；父会话/分支会话时 q 规则见静态 RE 报告，网关粘性会话
//     属于新鲜会话情形，故四者同值正确）。
//   - CLI v2 完全不发 x-opencode-request（静态计数 0，抓包亦无）。
//     旧网关仍发该头是 v1 遗留指纹，必须去掉；requestID 参数保留
//     仅为调用点兼容，不再写入任何头。
//   - x-opencode-org-id 只在 OAuth 账户元数据有 orgID 时出现，网关
//     的原始 key 字符串中没有该信息，绝不编造，缺省即正确。
//   - x-opencode-project：网关没有客户端项目目录，global 是唯一诚实值。
func setZenGateHeaders(req *http.Request, sess, requestID string) {
	_ = requestID // v2 CLI does not send x-opencode-request; do not emit it
	req.Header["x-opencode-session-id"] = []string{sess}
	req.Header["x-opencode-session"] = []string{sess}
	req.Header["x-session-affinity"] = []string{sess}
	req.Header["x-session-id"] = []string{sess}
	req.Header["x-opencode-client"] = []string{"cli"}
	req.Header["x-opencode-project"] = []string{"global"}
	if org := getZenConfig().OrgID; org != "" {
		req.Header["x-opencode-org-id"] = []string{org}
	}
	// CLI v2 wire headers (2026-10-26 capture): Accept */*, and the
	// full Accept-Encoding list. Go's default is only "gzip" and no
	// Accept header, which is another non-CLI fingerprint marker.
	// Setting Accept-Encoding explicitly disables Go's automatic gzip
	// decompression; upstream SSE for stream=true is uncompressed in
	// practice, and the gateway parses SSE as raw bytes, so this is safe.
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	// CLI v2 tracing headers, same trace/span across b3 and traceparent.
	traceID := kit.RandHex(16)
	spanID := kit.RandHex(8)
	req.Header.Set("b3", traceID+"-"+spanID+"-1")
	req.Header.Set("traceparent", "00-"+traceID+"-"+spanID+"-01")
}

// zenClientFlatTools 提取客户端工具并转成 responses 端点要求的 flat 形态。
// 同时接受 chat 嵌套形态 {type,function:{...}} 与 responses 原生 flat 形态
// {type,name,description,parameters,strict}。
func zenClientFlatTools(params map[string]any) []map[string]any {
	var out []map[string]any
	add := func(name, desc string, p any) {
		if name == "" {
			return
		}
		if p == nil {
			p = zenEmptySchema()
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        name,
			"description": desc,
			"parameters":  p,
			"strict":      false,
		})
	}
	if ct, ok := params["tools"].([]any); ok {
		for _, t := range ct {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			if fn, ok := tm["function"].(map[string]any); ok {
				n, _ := fn["name"].(string)
				desc, _ := fn["description"].(string)
				add(n, desc, fn["parameters"])
				continue
			}
			n, _ := tm["name"].(string)
			if n == "" {
				continue
			}
			desc, _ := tm["description"].(string)
			add(n, desc, tm["parameters"])
		}
	}
	if len(out) > 0 {
		return out
	}
	if fns, ok := params["functions"].([]any); ok {
		for _, f := range fns {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			n, _ := fm["name"].(string)
			desc, _ := fm["description"].(string)
			add(n, desc, fm["parameters"])
		}
	}
	return out
}

// zenMergeResponsesTools 客户端 flat 工具优先，缺名补 gate stub 过闸。
func zenMergeResponsesTools(clientTools []map[string]any) []any {
	merged := make([]any, 0, len(clientTools)+len(zenGateToolSpecs))
	provided := map[string]bool{}
	for _, t := range clientTools {
		n, _ := t["name"].(string)
		if n == "" || provided[n] {
			continue
		}
		provided[n] = true
		merged = append(merged, t)
	}
	for _, s := range zenGateToolSpecs {
		if provided[s.name] {
			continue
		}
		provided[s.name] = true
		// parameters 必须用 responses 形态的官方真实 schema：空 schema
		// 会被新后端 403（2026-10-15 实测，见 zen_gate_tools.go）。
		desc, params := s.desc, zenEmptySchema()
		if d := lookupZenGateToolDef(s.name); d != nil {
			desc, params = d.Description, d.ParametersResponses
		}
		merged = append(merged, map[string]any{
			"type":        "function",
			"name":        s.name,
			"description": desc,
			"parameters":  params,
			"strict":      false,
		})
	}
	return merged
}

// responsesContentToInput chat content -> Responses input content。
// 字符串原样；parts 数组只保留文本/图片两种 Responses 原生类型。
func responsesContentToInput(content any) any {
	if s, ok := content.(string); ok {
		return s
	}
	parts, ok := content.([]any)
	if !ok {
		return ""
	}
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		t, _ := pm["type"].(string)
		switch t {
		case "text", "input_text":
			txt, _ := pm["text"].(string)
			out = append(out, map[string]any{"type": "input_text", "text": txt})
		case "image_url":
			url := ""
			if u, ok := pm["image_url"].(map[string]any); ok {
				url, _ = u["url"].(string)
			}
			if url != "" {
				// Responses 原生图片 part 是 input_image；chat 形态的
				// image_url 会被上游拒绝/忽略
				out = append(out, map[string]any{"type": "input_image", "image_url": url})
			}
		}
	}
	if len(out) == 1 {
		if one, ok := out[0].(map[string]any); ok && one["type"] == "input_text" {
			txt, _ := one["text"].(string)
			return txt
		}
	}
	return out
}

// responsesSSEToChat 把原生 responses SSE 事件流聚合成 chat completions 形态。
// 处理两种事件风格（上游按次返回其一）：
//   - delta 流：response.output_text.delta / function_call_arguments.delta /
//     reasoning_summary_text.delta（逐块增量文本）
//   - output_item.done：response.output_item.done 内 item.content[].text 全量
//     文本（muse-spark 对纯文本问答只发该事件，无 delta 流）
//
// usage 从 response.completed 或 response.incomplete 提取；
// incomplete_details.reason 映射为 finish 原因（length→length）。
// zenSSECall 单个 function_call 输出项的流式累积器。上游可能并行/连续输出
// 多个工具调用，必须按 item 分开累积参数，否则两个调用的 arguments delta
// 会串成一段坏 JSON（上游实测：webfetch 双调用 jam 成 {"query":...}{"url":...}）。
type zenSSECall struct {
	id, name, callID string
	idx              int // response.output_index：无 id/call_id 时的绑定键（缺失为 -1）
	args             strings.Builder
	final            string // output_item.done 一次性给全参（无 delta 事件时）
}

// findZenSSECall 定位事件所属的累积器：id/call_id 精确匹配 → output_index
// 匹配。两者都对不上时返回 nil，由调用方按场景决定要不要认领无主累积器 ——
// 绝不"回退到最后一个调用"：并行调用时那会把 A 的参数 delta 追加到 B 上，
// 拼出 {"query":...}{"url":...} 这种坏 JSON（上游实测的 jam 正是如此）。
func findZenSSECall(calls []*zenSSECall, id, callID string, outIdx int) *zenSSECall {
	for _, c := range calls {
		if id != "" && (c.id == id || c.callID == id) {
			return c
		}
		if callID != "" && (c.callID == callID || c.id == callID) {
			return c
		}
	}
	if outIdx >= 0 {
		for _, c := range calls {
			if c.idx == outIdx {
				return c
			}
		}
	}
	return nil
}

// zenOrphanCall 返回第一个"无标识但已有内容"的累积器：delta/done 先于
// added 到达时内容落在无主累积器里，随后的 added/done 按它归位。
func zenOrphanCall(calls []*zenSSECall) *zenSSECall {
	for _, c := range calls {
		if c.id == "" && c.callID == "" && (c.args.Len() > 0 || c.final != "") {
			return c
		}
	}
	return nil
}

// zenSSEOutIdx 取事件里的 output_index，缺失返回 -1。
func zenSSEOutIdx(ev map[string]any) int {
	if v, ok := ev["output_index"].(float64); ok {
		return int(v)
	}
	return -1
}

func responsesSSEToChat(resp *http.Response) (map[string]any, error) {
	defer resp.Body.Close()
	var text, reasoning strings.Builder
	var usage map[string]any
	var incompleteReason string
	var failedMsg string
	failed := false
	textDeltas := false
	sawData := false
	var rawHead strings.Builder // 非 SSE 响应体（错误 JSON）的采样，用于报错
	calls := []*zenSSECall{}
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "data:") {
				sawData = true
				payload := strings.TrimSpace(line[5:])
				if payload != "" && payload != "[DONE]" {
					var ev map[string]any
					if json.Unmarshal([]byte(payload), &ev) == nil {
						typ, _ := ev["type"].(string)
						switch typ {
						case "response.output_text.delta":
							if d, _ := ev["delta"].(string); d != "" {
								textDeltas = true
								text.WriteString(d)
							}
						case "response.reasoning_summary_text.delta":
							if d, _ := ev["delta"].(string); d != "" {
								reasoning.WriteString(d)
							}
						case "response.function_call_arguments.delta":
							if d, _ := ev["delta"].(string); d != "" {
								iid, _ := ev["item_id"].(string)
								c := findZenSSECall(calls, iid, "", zenSSEOutIdx(ev))
								if c == nil {
									switch {
									case len(calls) == 0:
										// 首个事件就是 delta：先建无主累积器，
										// 用 item_id 当身份，后续 added/done 才找得回来
										c = &zenSSECall{idx: zenSSEOutIdx(ev), id: iid}
										calls = append(calls, c)
									case len(calls) == 1 && calls[0].id == "" && calls[0].callID == "":
										// 唯一且无主的累积器不可能是别人的
										c = calls[0]
									default:
										// 归属不明的 delta：宁可丢弃（done 事件会带全量
										// 参数），也不能把 A 的碎片拼进 B 的参数里
										c = zenOrphanCall(calls)
									}
								}
								if c != nil {
									c.args.WriteString(d)
								}
							}
						case "response.function_call_arguments.done":
							// 收尾事件：部分上游只发 delta + 本事件（无 output_item.done）
							if a, _ := ev["arguments"].(string); a != "" {
								iid, _ := ev["item_id"].(string)
								c := findZenSSECall(calls, iid, "", zenSSEOutIdx(ev))
								if c == nil {
									c = zenOrphanCall(calls)
								}
								if c != nil && c.args.Len() == 0 && c.final == "" {
									c.final = a
								}
							}
						case "response.output_item.added":
							if item, ok := ev["item"].(map[string]any); ok {
								if it, _ := item["type"].(string); it == "function_call" {
									id, _ := item["id"].(string)
									cid, _ := item["call_id"].(string)
									name, _ := item["name"].(string)
									// 就地更新：重复 added、以及先到的孤儿 delta 都归位到
									// 同一累积器，不再无条件新建（新建会把 delta 变成无名壳）
									c := findZenSSECall(calls, id, cid, zenSSEOutIdx(ev))
									if c == nil {
										c = zenOrphanCall(calls)
									}
									if c == nil {
										c = &zenSSECall{idx: zenSSEOutIdx(ev)}
										calls = append(calls, c)
									}
									if id != "" {
										c.id = id
									}
									if cid != "" {
										c.callID = cid
									}
									if name != "" {
										c.name = name
									}
								}
							}
						case "response.output_item.done":
							// 全量输出项：message 条目 content[].output_text/text 即最终文本；
							// function_call 条目（无 delta 事件的模型）一次性给全参
							if item, ok := ev["item"].(map[string]any); ok {
								switch it, _ := item["type"].(string); it {
								case "message":
									// delta 流已经给过正文时不再采纳全量文本，否则文本翻倍
									if textDeltas {
										break
									}
									if content, ok := item["content"].([]any); ok {
										for _, part := range content {
											pm, ok := part.(map[string]any)
											if !ok {
												continue
											}
											pt, _ := pm["type"].(string)
											if pt != "output_text" && pt != "text" {
												continue
											}
											if t, _ := pm["text"].(string); t != "" {
												text.WriteString(t)
											}
										}
									}
								case "function_call":
									// 全量参数（无 delta 事件的模型一次给全）
									a := ""
									if av, ok := item["arguments"].(string); ok {
										a = av
									}
									id, _ := item["id"].(string)
									cid, _ := item["call_id"].(string)
									name, _ := item["name"].(string)
									target := findZenSSECall(calls, id, cid, zenSSEOutIdx(ev))
									if target == nil {
										target = zenOrphanCall(calls)
									}
									if target == nil {
										// 无标识可归属时只能收回"最后一个完全空的壳"；
										// 否则新建（不能覆盖已有调用的 id/name/参数）
										if n := len(calls); n > 0 && calls[n-1].id == "" &&
											calls[n-1].callID == "" && calls[n-1].name == "" &&
											calls[n-1].args.Len() == 0 && calls[n-1].final == "" {
											target = calls[n-1]
										} else {
											target = &zenSSECall{idx: zenSSEOutIdx(ev)}
											calls = append(calls, target)
										}
									}
									if id != "" {
										target.id = id
									}
									if cid != "" {
										target.callID = cid
									}
									if name != "" {
										target.name = name
									}
									if a != "" && target.args.Len() == 0 && target.final == "" {
										target.final = a
									}
								}
							}
						case "response.completed", "response.incomplete", "response.failed":
							if r, ok := ev["response"].(map[string]any); ok {
								if u, ok := r["usage"].(map[string]any); ok {
									usage = u
								}
								if det, ok := r["incomplete_details"].(map[string]any); ok {
									if reason, _ := det["reason"].(string); reason != "" {
										incompleteReason = reason
									}
								}
								if typ == "response.failed" {
									failed = true
									if incompleteReason == "" {
										incompleteReason = "error"
									}
									// failed 事件里 response.error.message 才是真因
									// （限流/余额/会话失效），不带出去就只剩 "error"
									if em, ok := r["error"].(map[string]any); ok {
										if m, _ := em["message"].(string); m != "" {
											failedMsg = m
										}
									}
								}
							}
						}
					}
				}
			}
		}
		if !sawData && !strings.HasPrefix(line, "data:") && rawHead.Len() < 8192 {
			rawHead.WriteString(line)
			rawHead.WriteString("\n")
		}
		if err != nil {
			break
		}
	}
	// 一个 data: 行都没有 = 上游返回的不是 SSE（402/403/500 全是 JSON 错误体）。
	// 直接报错，别把它当成"空成功响应"下发（客户端会收到空 output 且无报错）。
	if !sawData {
		head := strings.TrimSpace(rawHead.String())
		if head == "" {
			return nil, fmt.Errorf("zen responses upstream returned empty body")
		}
		var obj map[string]any
		if json.Unmarshal([]byte(head), &obj) == nil {
			if e, ok := obj["error"].(map[string]any); ok {
				if m, _ := e["message"].(string); m != "" {
					return nil, fmt.Errorf("zen responses upstream error: %s", m)
				}
			}
		}
		if len(head) > 300 {
			head = head[:300]
		}
		return nil, fmt.Errorf("zen responses upstream returned non-SSE body: %s", head)
	}
	if failed {
		if failedMsg != "" {
			return nil, fmt.Errorf("zen responses upstream failed: %s", failedMsg)
		}
		return nil, fmt.Errorf("zen responses upstream failed: %s", incompleteReason)
	}
	textStr := text.String()
	msg := map[string]any{"role": "assistant", "content": textStr}
	finish := "stop"
	if incompleteReason == "max_output_tokens" || incompleteReason == "length" {
		finish = "length"
	}
	if len(calls) > 0 {
		outCalls := make([]any, 0, len(calls))
		for i, call := range calls {
			if call.name == "" {
				// 无名调用客户端无法执行（多半是 added 后未闭合的空壳）。有内容时
				// 说明上游真的发了调用却缺名字，值得留日志排查；空壳则静默丢弃。
				if call.args.Len() > 0 || call.final != "" {
					log.Printf("  zen responses: dropping nameless tool call (%d bytes args)",
						call.args.Len()+len(call.final))
				}
				continue
			}
			raw := call.final
			if call.args.Len() > 0 {
				raw = call.args.String()
			}
			if raw == "" {
				raw = "{}" // 无参调用：空串不是合法 JSON，IDE 解析会直接失败
			}
			id := call.callID
			if id == "" {
				id = call.id
			}
			if id == "" {
				// 带序号，避免同一响应内多个无 id 调用撞成同一个 call_id
				id = fmt.Sprintf("call_%x_%d", time.Now().UnixNano(), i)
			}
			outCalls = append(outCalls, map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      call.name,
					"arguments": repairToolArguments(raw),
				},
			})
		}
		if len(outCalls) > 0 {
			msg["tool_calls"] = outCalls
			finish = "tool_calls"
		}
	}
	if reasoning.Len() > 0 {
		msg["reasoning_content"] = reasoning.String()
	}
	out := map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%x", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "",
		"choices": []any{map[string]any{
			"index":         0,
			"message":       msg,
			"finish_reason": finish,
		}},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
	if usage != nil {
		u := map[string]any{
			"prompt_tokens":     float64(0),
			"completion_tokens": float64(0),
			"total_tokens":      float64(0),
		}
		if v, ok := usage["input_tokens"].(float64); ok {
			u["prompt_tokens"] = v
		}
		if v, ok := usage["output_tokens"].(float64); ok {
			u["completion_tokens"] = v
		}
		if v, ok := usage["total_tokens"].(float64); ok {
			u["total_tokens"] = v
		} else {
			u["total_tokens"] = u["prompt_tokens"].(float64) + u["completion_tokens"].(float64)
		}
		out["usage"] = u
	}
	return out, nil
}

// callZenResponsesAPI 原生 /v1/responses 端点调用：与 callZenAPI 相同的
// 轮转/重试/冷却/统计语义，只差请求体形态与端点路径。
//   - stream=true 时返回原生 SSE 流（调用方自行转换呈现）
//   - stream=false 时在内部把 SSE 聚合成 chat completions 形态返回，
//     响应体可直接按 chat 结构解码
//
// 返回 (响应, 命中限流次数, 错误)。
//
// prompt_cache_key 绑定本次上游会话：官方 CLI 发 prompt_cache_key=<会话 ID>
// 且与 x-opencode-session 同值。网关此前每 attempt 换新 sess_ 导致同一请求
// 的 header 与 body 会话不一致；现 body 在请求体构造时绑定当次 sess_。
func callZenResponsesAPI(ctx context.Context, params map[string]any, stream bool, opts ...zenCallOpts) (*http.Response, int, error) {
	var o zenCallOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	cfg := getZenConfig()
	model, _ := params["model"].(string)
	zm, ok := resolveZenModel(model)
	if !ok {
		return nil, 0, fmt.Errorf("model %q is not a known zen model", model)
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/responses"

	zenStateMu.Lock()
	sem := zenSem
	zenStateMu.Unlock()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, 0, fmt.Errorf("client aborted: %w", ctx.Err())
	}
	defer func() { <-sem }()

	retries := cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	delay := time.Second
	rateLimited := 0
	retryKey := ""          // 非空时重试沿用该 key（保持 key sess_ 一致）
	remapped := false       // 410 弃用迁移至多一次，防别名链循环
	sessionRetried := false // FreeTier 403 换新会话后同 key 重试至多一次
	// 2026-10-27: fresh session per API call (zenCallIdentity). Same
	// session across retries of this call, fresh if the key switches
	// or a 403 forces a refresh. See zen_session.go.
	callIdent := newZenCallIdentity()

	for attempt := 0; ; attempt++ {
		// 先选 key：attempt==0 或尚无粘性 key 时轮转；重试链内沿用
		// retryKey（会话绑定要求 key 与 sess_ 一致；换 key 由下方
		// 限流/403 分支显式改写 retryKey 后 continue 实现）。
		// ZEN_PIN_KEY=n 时固定用第 n 个 key（排障直测），默认轮转。
		// pinKey（面板 Test）：整条调用链固定该 key，优先级最高——探测的
		// 结论只对被探测的 key 成立，中途换 key 会把别的 key 的成败算到它头上。
		var key string
		if o.pinKey != "" {
			key = o.pinKey
			retryKey = o.pinKey
		} else if pk := pinnedZenKey(); pk != "" {
			key = pk
			retryKey = pk
		} else if retryKey == "" {
			retryKey = pickZenKey()
		}
		key = retryKey
		if key == "" {
			return nil, rateLimited, zenNoKeyError()
		}
		// 出口由 key 决定：隔离模式下同一 key 永远从绑定 IP 出现（主→备）；
		// 绑定出口全不可用时短冷却该 key 并清空 retryKey 换下一个 —— 绝不
		// 退回其他出口（那会让粘性会话的 IP 漂移）。
		proxyURL, pidx, exitOK := boundExitFor(key)
		if !exitOK {
			if o.pinKey != "" || pinnedZenKey() != "" {
				return nil, rateLimited, zenBlockedExitErr(key)
			}
			cooldownZenKey(key, 2*time.Minute)
			retryKey = ""
			log.Printf("  zen responses key#%d bound proxies unavailable, skipping key for 2m", keyIndex(key))
			continue
		}
		viaProxy := "direct"
		if proxyURL != "" {
			viaProxy = maskProxyURL(proxyURL)
		}
		// 每次调用新铸会话（2026-10-27，a99d94b）：同一调用的重试沿用同一
		// 会话，新调用/换 key 重新铸；msg_ 请求 ID 仍每次随机。
		// （key 为空已在上方报错返回。）
		sess, user, ua := callIdent.forKey(key)
		body := buildZenResponsesBody(params, zm.ID, sess)
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			return nil, rateLimited, fmt.Errorf("marshal zen responses body: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, rateLimited, fmt.Errorf("create zen responses request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", ua)
		// x-opencode-session-id：真实 CLI 两个头都发（request.ts）；对本仓库的
		// 格式门结论无害，补上只为字节级对齐官方流量。
		setZenGateHeaders(req, sess, user)
		log.Printf("  zen upstream: model=%s responses stream=%v via=%s key=#%d attempt=%d session=%s",
			zm.ID, stream, viaProxy, keyIndex(key), attempt+1, kit.Truncate(sess, 24))
		setUpstreamInfo(ctx, fmt.Sprintf("key#%d", keyIndex(key)))

		// hsStart 拨号前打点（与 cline 路径同口径）：Do 返回即隧道建立（首字节未回），
		// 差值=TCP+TLS+CONNECT 握手耗时。此前恒传 0，导致 zen/opencode 出口的
		// 「平均握手延迟」徽标永远无样本（面板显示 -），只有在线率在动。
		hsStart := time.Now()
		resp, err := proxyClientFor(proxyURL).Do(req)
		if err == nil {
			mainP, backupP, boundP := keyBindingOf(key)
			setUpstreamExit(ctx, proxyExitType(proxyURL, mainP, backupP, boundP))
			recordProxyOutcome(proxyURL, true, nil, time.Since(hsStart).Milliseconds())
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, rateLimited, fmt.Errorf("client aborted: %w", err)
			}
			if pidx >= 0 {
				cooldownUpstreamProxy(pidx, 2*time.Minute)
				recordProxyOutcome(proxyURL, false, err, 0)
				log.Printf("  zen proxy failed (%v), cooldown exit %s for 2m", err, viaProxy)
			}
			if attempt < retries {
				log.Printf("  zen responses network error (%v), retry %d/%d after %v", err, attempt+1, retries, delay)
				if !sleepCtx(ctx, kit.WithRetryJitter(delay)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = zenRetryDelay(delay)
				continue
			}
			return nil, rateLimited, fmt.Errorf("zen responses request: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			zenSessionMarkSuccess(key)
			if stream {
				resp.Header.Set("X-Afree-Used-Key", key)
				return resp, rateLimited, nil
			}
			// 非流式：内部聚合为 chat completions 形态后返回
			chat, aerr := responsesSSEToChat(resp)
			if aerr != nil {
				return nil, rateLimited, aerr
			}
			chat["model"] = zm.ID
			data, merr := json.Marshal(chat)
			if merr != nil {
				return nil, rateLimited, merr
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":     []string{"application/json"},
					"X-Afree-Used-Key": []string{key},
				},
				Body: io.NopCloser(bytes.NewReader(data)),
			}, rateLimited, nil
		}

		bodyBytes := kit.ReadBody(resp)
		resp.Body.Close()
		// 类型化错误携带状态码：端点学习只认"上游按 HTTP 拒绝了"，
		// 不认错误文本（网络错误串里也有 "no such host" 之类的词）
		apiErr := &zenHTTPError{Status: resp.StatusCode, Body: kit.Truncate(bodyBytes, 500)}

		if isRateLimited(resp.StatusCode, bodyBytes) {
			rateLimited++
			// 上游按限流处理（429，或关键词命中的 403/502/503）：标记随错误
			// 返回，消费者据此区分"限流型 403"（key 已冷却、收割机未触发）与
			// "会话死亡型 403"（FreeTier，触发收割机）。
			apiErr.RateLimited = true
			// 原样记录上游的 Retry-After（HTTP 日期还是秒数、值是多少）：
			// 冷却时长完全由它决定，而面板只能看到换算后的截止时刻。没有这行，
			// "这个 key 为什么冷却这么久"只能靠猜（实测 FreeUsageLimitError
			// 的 Retry-After 落在每日窗口复位点，见 README.md Troubleshooting 一节）。
			rawRetry := resp.Header.Get("Retry-After")
			applied := cooldownZenKey(key, parseRetryAfter(rawRetry))
			log.Printf("  zen rate limited (%d) key#%d: Retry-After=%q -> cooldown %v",
				resp.StatusCode, keyIndex(key), rawRetry, applied)
			// pinKey（面板 Test）：探测结论必须是被探测 key 自己的——立即原样
			// 上报 429（冷却已在上一行生效），不换 key 重试、不吃重试睡眠。
			if o.pinKey != "" {
				return nil, rateLimited, apiErr
			}
			if next := pickZenKey(); next != "" && next != key && !zenKeyCooling(next) {
				// 必须同步改写 retryKey：循环头 key=retryKey，只改 key 不改
				// retryKey 会让下一次迭代继续用刚冷却的旧 key，无限 429 空转。
				key = next
				retryKey = next
				log.Printf("  zen responses rate limited (%d), switching to next zen key (%d configured)", resp.StatusCode, len(cfg.Keys))
				continue
			}
			if attempt < retries {
				wait := delay
				if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After")); retryAfter > wait {
					wait = retryAfter
				}
				if wait > 30*time.Second {
					wait = 30 * time.Second
				}
				log.Printf("  zen responses rate limited (%d), retry %d/%d after %v", resp.StatusCode, attempt+1, retries, wait)
				if !sleepCtx(ctx, kit.WithRetryJitter(wait)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = zenRetryDelay(delay)
				continue
			}
			return nil, rateLimited, apiErr
		}

		// 5xx 不再推进任何全局状态：故障转移已删除，上游 500 直接按重试/
		// 报错路径处理（同 chat 路径注释：pinned 探测结论只属于这次点击）。
		// 会话失效（FreeTier 403 且非限流）：403 针对 key/会话，换到别的 key 只会
		// 把同一批坏状态扩散到全池，绕过不了它。正确做法是同一 key 上同步换一次
		// 会话再重试一次（sessionRetried 保证本请求至多一次）；仍 403 则该 key
		// 确实不可用——短暂冷却并快速失败，不再跨 key 扇出。
		if resp.StatusCode == http.StatusForbidden {
			// pinKey（面板 Test）：探测结论只属于这次点击，绝不触碰会话状态、
			// 不冷却、不重试——同 key 立刻原样上报。
			if o.pinKey != "" {
				return nil, rateLimited, apiErr
			}
			// 持续性故障闸门（与 chat 路径同款，见 zenSessionRefreshedRecently）。
			if zenSessionRefreshedRecently(key, time.Now()) {
				zenSessionFailed(key)
				applied := cooldownZenKey(key, zenForbiddenCooldown)
				log.Printf("  zen responses session rejected (403) again within %v of a local refresh [%s] key#%d via=%s: key/exit suspect, cooled %v (no re-mint)",
					zenForbiddenEscalateWindow, zenSessionDesc(key), keyIndex(key), viaProxy, applied)
				return nil, rateLimited, apiErr
			}
			if !sessionRetried {
				sessionRetried = true
				log.Printf("  zen responses session rejected (403) [%s] key#%d: minting fresh session for this call, retry same key",
					kit.Truncate(sess, 24), keyIndex(key))
				callIdent.refresh(key)
				continue
			}
			// 换新后仍 403：该 key 不可用，冷却 5 分钟后快速失败。
			zenSessionFailed(key)
			applied := cooldownZenKey(key, 5*time.Minute)
			log.Printf("  zen responses session rejected twice (403) key#%d: key cooled %v, failing fast (no cross-key fan-out)",
				keyIndex(key), applied)
			return nil, rateLimited, apiErr
		}
		// 弃用迁移（410 + replacement / 400 unavailable）：非 pin 探测才迁移——
		// 探测的结论只属于这次点击，不得改写共享状态。
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusBadRequest {
			if migrated := zenHandleDeprecatedModel(resp.StatusCode, bodyBytes, zm.ID, params, o.pinKey != "", remapped); migrated {
				remapped = true
				// params["model"] 已改写为继任模型；重建 body 后就地重试。
				body = buildZenResponsesBody(params, params["model"].(string), sess)
				bodyJSON, err = json.Marshal(body)
				if err != nil {
					return nil, rateLimited, fmt.Errorf("marshal zen responses body: %w", err)
				}
				zm, _ = resolveZenModel(params["model"].(string))
				if zm == nil {
					// 极窄竞态（继任模型刚被目录修剪）：退回原错误，不裸解引用。
					return nil, rateLimited, apiErr
				}
				continue
			}
		}
		return nil, rateLimited, apiErr
	}
}

// callZenAPI 调用 zen 上游,带限流防御: 并发信号量 + 指数退避重试 + 代理冷却 + 故障计数
// 返回 (响应, 命中限流次数, 错误)。
// ctx 来自客户端请求: 客户端取消（IDE abort）时立即终止上游调用,不重试、
// 不计数、不冷却任何 key/代理 —— 客户端行为不会污染限流状态。
func callZenAPI(ctx context.Context, params map[string]any, stream bool, opts ...zenCallOpts) (*http.Response, int, error) {
	var o zenCallOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	cfg := getZenConfig()
	body := buildZenBody(params, stream)

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal zen body: %w", err)
	}

	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"

	// 先取信号量再等待：不能在持有 zenStateMu 时阻塞在 channel 上，
	// 否则并发打满时（in-flight 调用要拿 zenStateMu 记成功/失败才能释放槽位）
	// 会形成确定性死锁。ctx 取消（客户端 abort）时不再占用槽位。
	zenStateMu.Lock()
	sem := zenSem
	zenStateMu.Unlock()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, 0, fmt.Errorf("client aborted: %w", ctx.Err())
	}
	defer func() { <-sem }()

	retries := cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	delay := time.Second
	rateLimited := 0
	remapped := false       // 410 弃用迁移至多一次，防别名链循环
	sessionRetried := false // FreeTier 403 换新会话后同 key 重试至多一次
	// 2026-10-27: fresh session per API call (zenCallIdentity). Same
	// session across retries of this call, fresh if the key switches
	// or a 403 forces a refresh. See zen_session.go.
	callIdent := newZenCallIdentity()
	// retryKey pins the key for the duration of this call. Previously
	// this was an emergent property of the sticky session: pickZenKey
	// prefers keys with a live session, so a 403 retry naturally stayed
	// on the same key. With fresh sessions there is no live session to
	// prefer, so without this pin a 403 retry would round-robin to the
	// next key and fan a single session failure out across the pool.
	// Rate-limit switching (below) is the only branch that rewrites it,
	// mirroring callZenResponsesAPI.
	retryKey := ""

	for attempt := 0; ; attempt++ {
		// 先选 key 再解析出口：隔离模式要求出口由 key 的绑定决定（同一 key
		// 永远从同一 IP 出现）；未绑定 key 在 egress 钉定生效时用自动分配的
		// 固定出口，否则走池轮转（未配置代理时直连）。
		// 客户端身份：每次 API 调用全新会话（2026-10-27，见 zen_session.go），
		// 同一调用的重试沿用同一会话，msg_ 请求 ID 每次随机。
		// ZEN_PIN_KEY=n 时固定用第 n 个 key（单 key 直测/排障），默认轮转。
		// pinKey（面板 Test）优先且不碰轮转指针：一次探测不该挪动 round-robin
		// 的游标，否则每点一次 Test 都会让下一次正常请求换 key。
		var key string
		if o.pinKey != "" {
			key = o.pinKey
			retryKey = o.pinKey
		} else if pk := pinnedZenKey(); pk != "" {
			key = pk
			retryKey = pk
		} else if retryKey == "" {
			retryKey = pickZenKey()
		}
		key = retryKey
		if key == "" {
			// 空池 / 全隔离挡住 / egress 钉定容量不足（代理比 key 少）——
			// zenNoKeyError 按当前生效模式选对应文案（含 egress 容量口径）。
			return nil, rateLimited, zenNoKeyError()
		}
		proxyURL, pidx, exitOK := boundExitFor(key)
		if !exitOK {
			// ZEN_PIN_KEY/pinKey 固定死的 key 没有可换的余地，直接报错；
			// 正常轮转把该 key 短冷却（pickZenKey 下一轮跳过它）后换 key。
			if o.pinKey != "" || pinnedZenKey() != "" {
				return nil, rateLimited, zenBlockedExitErr(key)
			}
			cooldownZenKey(key, 2*time.Minute)
			log.Printf("  zen key#%d bound proxies unavailable, skipping key for 2m", keyIndex(key))
			continue
		}
		viaProxy := "direct"
		if proxyURL != "" {
			viaProxy = maskProxyURL(proxyURL)
		}
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, rateLimited, fmt.Errorf("create zen request: %w", err)
		}
		// Fresh session per API call (2026-10-27, see zenCallIdentity).
		sess, user, ua := callIdent.forKey(key)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", ua)
		// x-opencode-session-id：真实 CLI 两个头都发（request.ts），见 responses 路径。
		setZenGateHeaders(req, sess, user)

		log.Printf("  zen upstream: model=%s stream=%v msgs=%d via=%s key=#%d attempt=%d session=%s",
			body["model"], stream, getMsgCount(params), viaProxy, keyIndex(key), attempt+1, kit.Truncate(sess, 24))
		setUpstreamInfo(ctx, fmt.Sprintf("key#%d", keyIndex(key)))

		// hsStart 拨号前打点（与 cline 路径同口径）：Do 返回即隧道建立（首字节未回），
		// 差值=TCP+TLS+CONNECT 握手耗时。此前恒传 0，导致 zen/opencode 出口的
		// 「平均握手延迟」徽标永远无样本（面板显示 -），只有在线率在动。
		hsStart := time.Now()
		resp, err := proxyClientFor(proxyURL).Do(req)
		if err == nil {
			mainP, backupP, boundP := keyBindingOf(key)
			setUpstreamExit(ctx, proxyExitType(proxyURL, mainP, backupP, boundP))
			recordProxyOutcome(proxyURL, true, nil, time.Since(hsStart).Milliseconds())
		}
		if err != nil {
			// 客户端取消: 立即返回,不重试不冷却不计故障
			if ctx.Err() != nil {
				return nil, rateLimited, fmt.Errorf("client aborted: %w", err)
			}
			// 隧道层失败才冷却出口: 拨号/握手/连接被重置。冷却从 5m 缩到 2m ——
			// 上游过载时 CF 重置连接的表现与死代理相同,5~10 分钟的冷却会让
			// 几次慢模型测试就毒化整个池;2 分钟仍能跳过真死代理,又快速自愈。
			if pidx >= 0 {
				cooldownUpstreamProxy(pidx, 2*time.Minute)
				recordProxyOutcome(proxyURL, false, err, 0)
				log.Printf("  zen proxy failed (%v), cooldown exit %s for 2m", err, viaProxy)
			}
			// 网络错误:退避重试(瞬时可恢复);ctx 取消时中断等待
			if attempt < retries {
				log.Printf("  zen network error (%v), retry %d/%d after %v", err, attempt+1, retries, delay)
				if !sleepCtx(ctx, kit.WithRetryJitter(delay)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = zenRetryDelay(delay)
				continue
			}
			return nil, rateLimited, fmt.Errorf("zen request: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			zenSessionMarkSuccess(key)
			resp.Header.Set("X-Afree-Used-Key", key)
			return resp, rateLimited, nil
		}

		bodyBytes := kit.ReadBody(resp)
		resp.Body.Close()
		// 类型化错误携带状态码（同 callZenResponsesAPI）：端点学习只看状态码
		apiErr := &zenHTTPError{Status: resp.StatusCode, Body: kit.Truncate(bodyBytes, 500)}

		if isRateLimited(resp.StatusCode, bodyBytes) {
			rateLimited++
			// 上游按限流处理（标记随错误返回，同 responses 路径）。
			apiErr.RateLimited = true
			// 不冷却出口代理: 代理成功送达了 HTTP 响应,它没有故障。限流是 zen
			// 对 key/身份/IP 组合的判定,把出口毒化 10 分钟只会让上游繁忙期
			// (慢模型 503/429)把整个池打瘫;下一次尝试的轮转自然换到下一出口,
			// key 冷却 + 轮转已足够分摊负载。
			// 冷却当前 key；若还有其他未冷却 key 则立即切换重试（不睡眠）。
			// Retry-After 原样入日志（同 responses 路径）。
			rawRetry := resp.Header.Get("Retry-After")
			applied := cooldownZenKey(key, parseRetryAfter(rawRetry))
			log.Printf("  zen rate limited (%d) key#%d: Retry-After=%q -> cooldown %v",
				resp.StatusCode, keyIndex(key), rawRetry, applied)
			// pinKey（面板 Test）：立即原样上报 429（冷却已生效），不换 key。
			if o.pinKey != "" {
				return nil, rateLimited, apiErr
			}
			if next := pickZenKey(); next != "" && next != key && !zenKeyCooling(next) {
				log.Printf("  zen rate limited (%d), switching to next zen key (%d configured)", resp.StatusCode, len(cfg.Keys))
				key = next
				retryKey = next
				continue
			}
			if attempt < retries {
				// Retry-After 封顶 30s：更长的等待没有意义（槽位被占、客户端早已离开）
				wait := delay
				if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After")); retryAfter > wait {
					wait = retryAfter
				}
				if wait > 30*time.Second {
					wait = 30 * time.Second
				}
				log.Printf("  zen rate limited (%d), retry %d/%d after %v", resp.StatusCode, attempt+1, retries, wait)
				if !sleepCtx(ctx, kit.WithRetryJitter(wait)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = zenRetryDelay(delay)
				continue
			}
			return nil, rateLimited, apiErr
		}

		// 非 2xx 直接按重试/报错路径处理（故障转移已删除，无全局状态可污染；
		// pinned 探测结论只属于这次点击）。
		// 会话失效（FreeTier 403 且非限流）：403 针对 key/会话，换到别的 key 只会
		// 把坏状态扩散到全池。同一 key 上同步换一次会话再重试一次（sessionRetried
		// 保证至多一次）；仍 403 则冷却该 key 并快速失败，不再跨 key 扇出。
		if resp.StatusCode == http.StatusForbidden {
			// 记录会话年龄：合法格式会话 403 是"会话寿命/额度窗口到期或
			// 门禁收紧"的证据；换新频率升高即是 tripwire。
			// pinKey（面板 Test）：探测结论只属于这次点击，不触碰会话状态。
			if o.pinKey != "" {
				log.Printf("  zen chat session rejected (403) [%s], key#%d (probe: no state change)", kit.Truncate(sess, 24), keyIndex(key))
				return nil, rateLimited, apiErr
			}
			// 持续性故障闸门：窗口内刚因 403 被动换过会话、又收到干净 403 ——
			// 实测这类 403 跟着出口走（同 key 同会话只换出口即 200），换新救
			// 不回来。冷却该 key（退出轮转）而不是再铸一个会话白跑一次上游。
			if zenSessionRefreshedRecently(key, time.Now()) {
				zenSessionFailed(key)
				applied := cooldownZenKey(key, zenForbiddenCooldown)
				log.Printf("  zen chat session rejected (403) again within %v of a local refresh [%s] key#%d via=%s: key/exit suspect, cooled %v (no re-mint)",
					zenForbiddenEscalateWindow, zenSessionDesc(key), keyIndex(key), viaProxy, applied)
				return nil, rateLimited, apiErr
			}
			if !sessionRetried {
				sessionRetried = true
				log.Printf("  zen chat session rejected (403) [%s] key#%d: minting fresh session for this call, retry same key",
					kit.Truncate(sess, 24), keyIndex(key))
				callIdent.refresh(key)
				continue
			}
			zenSessionFailed(key)
			applied := cooldownZenKey(key, 5*time.Minute)
			log.Printf("  zen chat session rejected twice (403) key#%d: key cooled %v, failing fast (no cross-key fan-out)",
				keyIndex(key), applied)
			return nil, rateLimited, apiErr
		}
		// 弃用迁移（410 + replacement / 400 unavailable）：非 pin 探测才迁移；
		// 迁移成功则重建 body 就地重试（至多一次）。
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusBadRequest {
			if migrated := zenHandleDeprecatedModel(resp.StatusCode, bodyBytes, body["model"].(string), params, o.pinKey != "", remapped); migrated {
				remapped = true
				body = buildZenBody(params, stream)
				bodyJSON, err = json.Marshal(body)
				if err != nil {
					return nil, rateLimited, fmt.Errorf("marshal zen body: %w", err)
				}
				continue
			}
		}
		return nil, rateLimited, apiErr
	}
}

// sleepCtx ctx 感知的睡眠；返回 false 表示等待期间被取消。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// keyIndex key 在池中的序号（日志用）
func keyIndex(key string) int {
	for i, k := range getZenConfig().Keys {
		if k == key {
			return i + 1
		}
	}
	return 0
}

// zenRetryDelay 指数退避的下一步延迟，封顶 30s。
//
// 必须封顶：Retries 来自面板配置（.zen-config.json 亦可手改），裸翻倍到约 63 次
// 就会让 int64 纳秒溢出为负数，而 time.NewTimer(负值) 立即触发 —— 重试退化成
// 无退避热循环，把并发槽位全部耗在空转上。封顶同时也限住了网络错误重试的等待
// 时长（该路径不做 Retry-After 修正，直接用 delay）。
func zenRetryDelay(d time.Duration) time.Duration {
	d *= 2
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

func zenModelList() []map[string]any {
	initZenModels()
	// 展示门控：与请求入口的可路由性闸同判定——没有启用的具名 key 时
	// 平台不可路由，目录继续展示只会让用户选中必失败的模型。
	if !platformHasRoutableCredential(platZen) {
		return nil
	}
	// 别名表先快照（zenDepMu 不与 zenModelsMu 嵌套）。
	depAliases := map[string]string{}
	zenDepMu.Lock()
	for dead, e := range zenDepAliases {
		depAliases[dead] = e.Replacement
	}
	zenDepMu.Unlock()
	zenModelsMu.RLock()
	out := make([]map[string]any, 0, len(zenModels))
	for _, m := range zenModels {
		if !isZenFreeModel(m) {
			continue
		}
		cp := *m
		entry := map[string]any{
			// 网关前缀（zen:<id>）：客户端按列表 ID 直接调用；裸 ID 仍兼容。
			"id":        zenPrefixedID(cp.ID),
			"context":   cp.Context,
			"output":    cp.Output,
			"source":    cp.Source,
			"upstream":  cp.Upstream,
			"toolCall":  cp.ToolCall,
			"reasoning": cp.Reasoning,
			"attach":    cp.Attach,
		}
		if repl, dep := depAliases[cp.ID]; dep {
			entry["replacedBy"] = repl
		}
		out = append(out, entry)
	}
	zenModelsMu.RUnlock()
	// map 迭代序随机：输出前按 id 排序（与 wb global 的 sortGlobalCatalog 同因
	// 同修）。zen 段无缓存——不排序的话 /v1/models 每次请求都是一套新随机序，
	// 下游 newapi 等按序消费方每次看到的顺序都不同。
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i]["id"].(string)
		b, _ := out[j]["id"].(string)
		return a < b
	})
	return out
}

// opencodeModelsRegistry 公共模型目录（官方 CLI 同源，无需认证；
// 替代需要有效 key 才能访问的 zen /v1/models）。
const opencodeModelsRegistry = "https://models.opencode.ai/api.json"

// maxRegistryBytes 目录响应体积上限（8 MiB）。正常目录约几百 KB，设置上限
// 只为防止异常响应把内存吃穿；超限按"不可达"处理。
const maxRegistryBytes = 8 << 20

// zenRegistryURL 可测试覆写：默认指向公共目录，测试指向 httptest 服务。
var zenRegistryURL = opencodeModelsRegistry

// zenModelOverlay 公共目录里比 zen 真源多的限额/旗标字段。
type zenModelOverlay struct {
	Context, Output     int
	ToolCall, Reasoning bool
	Attachment          bool
	// NPM 该模型的 SDK 包名（per-model provider.npm；解析规则与官方 CLI
	// fromModelsDevModel 一致：model.provider?.npm ?? provider.npm ?? 默认）。
	// 端点路由的种子信号，见 zenNPMToUpstream。
	NPM string
}

// fetchZenRegistry 拉取公共目录 api.json，返回（限额 overlay、价格门集合、
// 是否可达）。价格门 = cost.input==0 && cost.output==0 且 status 非
// deprecated。模型 ID 以条目内 id 优先、map key 兜底。目录不可达返回
// (空, 空, false)，调用方按 fail-open 处理。
// 唯一实现：syncZenModels 的层 2/3 共用，避免两处价格门逻辑漂移。
func fetchZenRegistry() (map[string]zenModelOverlay, map[string]bool, bool) {
	overlay := map[string]zenModelOverlay{}
	freeGate := map[string]bool{}
	oreq, err := http.NewRequest("GET", zenRegistryURL, nil)
	if err != nil {
		return overlay, freeGate, false
	}
	oreq.Header.Set("User-Agent", "opencode/latest/cli")
	client := &http.Client{Timeout: 25 * time.Second}
	oresp, err := client.Do(oreq)
	if err != nil {
		return overlay, freeGate, false
	}
	defer oresp.Body.Close()
	if oresp.StatusCode != 200 {
		return overlay, freeGate, false
	}
	var payload map[string]struct {
		NPM    string `json:"npm"`
		Models map[string]struct {
			ID         string `json:"id"`
			Status     string `json:"status"`
			ToolCall   bool   `json:"tool_call"`
			Reasoning  bool   `json:"reasoning"`
			Attachment bool   `json:"attachment"`
			NPM        string `json:"npm"`
			Provider   struct {
				NPM string `json:"npm"`
			} `json:"provider"`
			Limit struct {
				Context int `json:"context"`
				Output  int `json:"output"`
			} `json:"limit"`
			// Cost 用指针以区分"cost 键缺失"与"cost 全为 0"：缺失时不能当作
			// 免费（fail-closed），否则目录漏填价格字段的模型会被误判入免费池。
			Cost *struct {
				Input  float64 `json:"input"`
				Output float64 `json:"output"`
			} `json:"cost"`
		} `json:"models"`
	}
	// 体积上限：目录正常约几百 KB，恶意/异常响应不应无界读入内存。
	// 超限即视为不可达（fail-open 的调用方保留上次目录）。
	if json.NewDecoder(io.LimitReader(oresp.Body, maxRegistryBytes+1)).Decode(&payload) != nil {
		return overlay, freeGate, false
	}
	prov, ok := payload["opencode"]
	if !ok {
		return overlay, freeGate, false
	}
	for id, m := range prov.Models {
		if m.ID != "" {
			id = m.ID
		}
		// npm 解析优先级 = 官方 CLI fromModelsDevModel：
		// 模型 provider.npm > 模型顶层 npm > 提供方 npm > 默认（openai-compatible）。
		npm := m.Provider.NPM
		if npm == "" {
			npm = m.NPM
		}
		if npm == "" {
			npm = prov.NPM
		}
		if npm == "" {
			npm = "@ai-sdk/openai-compatible"
		}
		overlay[id] = zenModelOverlay{
			Context: m.Limit.Context, Output: m.Limit.Output,
			ToolCall: m.ToolCall, Reasoning: m.Reasoning, Attachment: m.Attachment,
			NPM: npm,
		}
		if m.Cost != nil && m.Cost.Input == 0 && m.Cost.Output == 0 &&
			!strings.Contains(strings.ToLower(m.Status), "deprecat") {
			freeGate[id] = true
		}
	}
	return overlay, freeGate, true
}

// zenDesiredFromLive 推导可服务的免费模型集（纯函数，便于测试）：
// 目录可达时以价格门为唯一权威（= 官方 CLI `opencode models` 读的同一份
// 数据的 cost 0/0 子集）；目录不可达时退回 CLI 成员表 + "-free" 启发式
// （fail-open，离线也能列出免费模型）。两者都拿不到时返回错误，调用方保留旧表。
func zenDesiredFromLive(registryOK bool, freeGate map[string]bool) (map[string]bool, error) {
	desired := map[string]bool{}
	if registryOK && len(freeGate) > 0 {
		for id := range freeGate {
			desired[id] = true
		}
		return desired, nil
	}
	// 收割机时代的 CLI 成员表兜底已随内嵌 CLI 一起移除——离线启动不再有
	// 第三方成员表来源，目录不可达就返回错误、调用方保留旧表（种子条目
	// 在首次成功同步前仍可服务）。
	return nil, fmt.Errorf("model catalog unavailable: registry unreachable")
}

// syncZenModels 同步免费模型（live 为准，种子只做冷启动兜底）。判定链：
//  1. 免费资格（唯一权威）：公共目录 models.opencode.ai 的 opencode 条目
//     cost.input==0 && cost.output==0 且 status 无 deprecated 字样。无 free
//     后缀但价格为 0 的模型（big-pickle）因此入选；status=deprecated 的
//     *-free 条目即使价格为 0 也排除（上游已报 Model unavailable）。
//     官方 CLI `opencode models` 读的是同一份目录（实测 2026-09-18：CLI 7 个
//     = 目录中 opencode 提供方 cost 0/0 非 deprecated 的 7 个），所以价格门
//     就是"真实模型列表"本身，无需再与种子交叉核对。
//  2. 目录不可达时：用 `opencode models`（CLI，无认证）当成员表，配
//     "-free" 后缀启发式（fail-open，保证离线/抖动时免费模型仍可用）。
//     这是 CLI 列表唯一的用武之地——目录可用时它不给任何额外信息，而每次
//     同步 spawn 一次 CLI 进程并不便宜。
//  3. 限额 overlay：目录 limit.context/output + tool_call / reasoning /
//     attachment 旗标 + per-model provider.npm（端点种子，见 applyZenCatalog）。
//
// 增删一律以本次 live 结果为准：表中的种子条目若不再通过价格门会被移除
// （种子只在首次成功同步之前的冷启动/离线状态提供兜底，不参与日常增删）。
// 返回新增模型数。无任何 live 来源可达时返回错误并保留旧表。
func syncZenModels() (int, error) {
	initZenModels()
	overlay, freeGate, registryOK := fetchZenRegistry()

	// 目录不可达时返回错误并保留旧表（收割机时代的 CLI 成员表兜底已随
	// 内嵌 CLI 一起移除，见 zenDesiredFromLive）。
	desired, derr := zenDesiredFromLive(registryOK, freeGate)
	if derr != nil {
		return 0, derr
	}
	added := applyZenCatalog(desired, overlay)
	pruned := pruneZenModelsTo(desired)
	// 重新敷用已学习的端点覆盖：新出现的模型（以及被 prune 后重建的条目）不在
	// 启动时 loadZenEndpoints 的视野里，不敷用就会每次重启重新探测一遍。
	reapplyLearnedEndpoints()
	// 目录刷新成功：解除上游 400 "Model is unavailable" 的死亡标记（此前可能
	// 只是目录滞后），并清理指向已不存在模型的别名/标记。
	zenClearDeadModels()
	pruneZenDeadAndAliases(desired)
	if added > 0 {
		log.Printf("zen model sync: %d new free model(s) from live catalog", added)
	}
	if pruned > 0 {
		log.Printf("zen model sync: pruned %d model(s) no longer free/online", pruned)
	}
	return added, nil
}

// zenNPMToUpstream 把 SDK 包名映射到上游路由种子（与官方 CLI 的 resolveSDK
// 语义一致）。返回 (upstream, known)：
//   - "@ai-sdk/openai"          → "responses"（原生 POST {base}/responses）
//   - "@ai-sdk/openai-compatible" → ""（默认 POST {base}/chat/completions）
//   - "@ai-sdk/anthropic" / "@ai-sdk/google" → ("", false)：免费层目前没有
//     存活的这类模型；映射记下来但不做原生分发，交端点学习器自行纠正，
//     日志留 warning（将来出现原生模型再补真分发）。
//   - 其他未知值 → ("", false)：绝不猜，让学习器用上游拒绝信号决定。
func zenNPMToUpstream(npm string) (string, bool) {
	switch npm {
	case "@ai-sdk/openai":
		return "responses", true
	case "@ai-sdk/openai-compatible":
		return "", true
	case "@ai-sdk/anthropic", "@ai-sdk/google":
		return "", false
	default:
		return "", false
	}
}

// applyZenCatalog 把 live 结果写进模型表：存量条目就地更新（保留别名与
// Upstream 提示），新条目按 overlay 限额建立。返回新增数量。
func applyZenCatalog(desired map[string]bool, overlay map[string]zenModelOverlay) int {
	zenModelsMu.Lock()
	defer zenModelsMu.Unlock()
	added := 0
	for id := range desired {
		ov := overlay[id]
		if cur, ok := zenModels[id]; ok {
			// 存量条目：overlay 限额覆盖种子/上次估算；别名与 Upstream 提示
			// （如 muse-spark 的原生 responses）沿用，Source 升级为 live。
			// 改副本再挂回（copy-on-write）：resolveZenModel 返回的指针在释放
			// RLock 后仍被请求路径读取，就地改写会与其裸读竞争。
			next := *cur
			if ov.Context > 0 {
				next.Context = ov.Context
			}
			if ov.Output > 0 {
				next.Output = ov.Output
			}
			if ov != (zenModelOverlay{}) {
				next.ToolCall, next.Reasoning, next.Attach = ov.ToolCall, ov.Reasoning, ov.Attachment
			}
			// npm 路由重播种：仅当该模型尚无明确端点决策时按目录 npm 刷新。
			// "决策"= Upstream 非空，或学习器已定论（Source=learned，含 chat
			// 定论：learning 后 Upstream 归零但不得再被 npm 播回 responses）。
			// load/reapplyLearnedEndpoints 恢复学习条目，这里不得回翻；
			// muse-spark 等种子提示同样保留。
			if next.Upstream != "" || next.Source == "learned" {
				// 已有决策，保持
			} else if ov.NPM != "" {
				if up, known := zenNPMToUpstream(ov.NPM); known && up != "" {
					next.Upstream = up
				}
			}
			// Source 保留"learned"标记：它既表示"学习器已定论，npm 不得重播种"，
			// 也是 saveZenEndpoints 写盘时的筛选条件（只存 learned 条目）。这里若
			// 无条件抹成 "live"，sync 结束前并发的 saveZenEndpoints 会把该条目从
			// 学习文件里丢掉，reapplyLearnedEndpoints 也因此失去依据——学习决策
			// 只在 reapply 恰好晚于本函数的时序下才存活。保留标记使契约显式成立。
			if next.Source != "learned" {
				next.Source = "live"
			}
			zenModels[id] = &next
			continue
		}
		// 跳过与免费别名冲突的 ID（如付费 id 撞别名），保证别名解析不被覆盖
		if _, conflict := zenAliases[id]; conflict {
			continue
		}
		ctxN, outN := ov.Context, ov.Output
		if ctxN <= 0 {
			ctxN = 200000
		}
		if outN <= 0 {
			outN = 32768
		}
		// 端点种子：按目录 npm 确定性路由（muse-spark 类 responses 模型不再
		// 靠名字启发式）。未知/暂不支持的原生 npm 交给端点学习器，绝不猜。
		upstream := ""
		if npm := ov.NPM; npm != "" {
			if up, known := zenNPMToUpstream(npm); known {
				upstream = up
			} else {
				log.Printf("zen catalog: model=%s has unsupported provider npm %q; endpoint learner will decide", id, npm)
			}
		}
		zenModels[id] = &ZenModel{
			ID:        id,
			Context:   ctxN,
			Output:    outN,
			Source:    "live",
			Upstream:  upstream,
			ToolCall:  ov.ToolCall,
			Reasoning: ov.Reasoning,
			Attach:    ov.Attachment,
		}
		// 模型回到 live 列表：若它此前在裁剪宽限表中，原样回活
		delete(zenGraceModels, id)
		added++
	}
	return added
}

// pruneZenModelsTo 修剪掉不在 live 结果中的条目（含种子）：种子只是首次成功
// 同步前的冷启动兜底，同步成功后以 live 为准，避免已下线/已转付费的模型继续
// 挂出（别名一并移除）。防御：live 残表（接口抖动/改版）不清空本地列表 ——
// 要求 live 结果不少于现有条数的一半。返回移除数量。
func pruneZenModelsTo(desired map[string]bool) int {
	zenModelsMu.Lock()
	defer zenModelsMu.Unlock()
	current := len(zenModels)
	if len(desired) == 0 || len(desired)*2 < current {
		if current > 0 {
			log.Printf("zen model sync: live list too small (%d live vs %d known), skipping prune", len(desired), current)
		}
		return 0
	}
	pruned := 0
	now := time.Now()
	for id, m := range zenModels {
		if desired[id] {
			continue
		}
		// 转入宽限表而非直接删除：上游目录闪断时客户端请求仍可解析、照发
		// 上游（见 zenPruneGrace 注释）。宽限期内不回 live 列表（面板不挂出）。
		zenGraceModels[id] = zenGraceEntry{
			m:        m,
			aliased:  append([]string(nil), m.Aliases...),
			prunedAt: now,
		}
		delete(zenModels, id)
		for _, a := range m.Aliases {
			delete(zenAliases, a)
		}
		pruned++
	}
	// 宽限到期（历经多轮同步仍未回到 live 列表）：真下线，彻底清除
	for id, e := range zenGraceModels {
		if now.Sub(e.prunedAt) > zenPruneGrace {
			delete(zenGraceModels, id)
		}
	}
	return pruned
}

// startZenModelsRefresher 定时同步 zen 模型列表（5 分钟，与各平台模型
// 目录的自动同步节奏一致）。同步走公共注册表 api.json（免认证），不消耗任何
// key 的配额。
func startZenModelsRefresher() {
	go func() {
		if _, err := syncZenModels(); err != nil {
			log.Printf("zen model sync: failed (%v), keeping the current list", err)
		}
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			if added, err := syncZenModels(); err != nil {
				log.Printf("zen model sync: failed (%v)", err)
			} else if added > 0 {
				log.Printf("zen model sync: %d new models from official feed", added)
			}
		}
	}()
}

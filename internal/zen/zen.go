package zen

import (
	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/toolrename"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ZenModel opencode zen 免费模型定义
// ZenModel zen 模型条目。
//
// 并发约定（重要）：写者（ApplyZenCatalog / LearnZenEndpoint /
// LoadZenEndpoints）一律在 zenModelsMu 下"改副本再挂回"，绝不就地改写已发布的
// 结构体。因此 ResolveZenModel 返回的指针在锁外可安全读取任意字段——请求路径
// 正是在锁外读 Upstream/Source/Context。新增写者必须遵守 copy-on-write，否则
// 就是与请求路径的数据竞争。
type ZenModel struct {
	ID        string `json:"id"`
	Context   int    `json:"context"`
	Output    int    `json:"output"`
	Source    string `json:"source"`               // live=目录同步 / learned=端点学习器定论
	Upstream  string `json:"upstream,omitempty"`   // 强制原生上游端点: "responses"；空=默认 chat/completions
	ToolCall  bool   `json:"toolCall,omitempty"`   // 目录声明的 tool_call 能力
	Reasoning bool   `json:"reasoning,omitempty"`  // 目录声明的 reasoning 能力
	Attach    bool   `json:"attachment,omitempty"` // 目录声明的图片/附件输入能力
}

// zenSeedModels 冷启动兜底快照（含短别名表）已于 2026-11-11 用户拍板整机制
// 删除（目录如实反映 live 同步结果，
// 宁空勿过期快照——免费层下架/转付费的模型不该靠内置清单续命）。历史形态：
// 7 条免费模型快照，在首次成功同步之前作为可服务模型集，并绑定短别名
// （mimo/pickle/muse-spark…）与 muse-spark 的 responses 端点提示。删除后：
// 冷启动模型表为空直到首次 SyncZenModels 成功（注册表免认证，启动即拉 +
// 30s×10 快速重试链）；短别名不再解析（zen:<id>/裸 <id> 照常；410 迁移别名
// 不受影响，持久化在 zen-config.json）；端点判定完全由目录 provider.npm
// （ZenNPMToUpstream）+ 端点学习器承担。

var (
	zenModelsMu sync.RWMutex
	zenModels   = make(map[string]*ZenModel) // 主表:ID
)

// ZenPruneGrace 目录裁剪宽限期。上游 live 目录存在分钟级闪断：实测同一模型
// 1 分钟内先被 prune（02:01:14）再被加回（02:02:05），立即删除会让客户端
// 请求恰好落在窗口内被严格门控误拒 400。被裁条目转入宽限表：仍可解析
// （RouteModel/门控继续放行，请求照发上游——真下线由上游返回真实错误，
// 该类错误不冷却 key），但不进面板模型列表；重新同步到则
// 原样回活，超过宽限期历经多轮同步仍未回来，视为真下线彻底清除。
const ZenPruneGrace = 30 * time.Minute

type ZenGraceEntry struct {
	m        *ZenModel
	prunedAt time.Time
}

var zenGraceModels = map[string]ZenGraceEntry{} // 读写均走 zenModelsMu

// ZenAPIBase 上游基地址（编译期默认值）。
//
// 2026-10-30 抓包更正：官方目录（models.opencode.ai）里 opencode provider 的
// api 是 /inference/openai/v1，全部 8 个免费模型的 provider.api 也一致；真实
// CLI 2.1.4 发往 /inference/openai/v1/chat/completions。此前这里写死的
// /inference/v1 来自 2026-10-15 的一次观测，上游改了路径而仓库没跟——差一个
// 路径段。默认值已同步，并且 SyncZenModels 会按目录里的 provider.api 自愈
// （见 ApplyZenRegistryBase），不再依赖某次人工观测。
const ZenAPIBase = "https://opencode.ai/inference/openai/v1"

// Max429KeySwitches 一次客户端请求因 429 最多切换几个 key 的硬上限。
// 换 key 是本次调用里唯一不可逆的操作：CooldownZenKey 会把该 key 冻结到
// ZenRateLimitCooldown。若不设上限，一个大请求 429 后沿池顺序把每个 key 逐个
// 冷却，整池在几秒内报废——2026-10-30 实测：单个 291 条消息的请求 4 秒内冷却
// 了 5 个 key。上限取 4（实际生效值再与 retries 取小），保证一次坏请求最多
// 牺牲少数几个 key，而不是整个池。
const Max429KeySwitches = 4

// MaxZenAttempts 单次上游调用的循环硬上限（纯安全网）。
//
// 睡眠重试由 sleeps 管、换 key 由 keySwitches 管，两者各自有界，理论上循环
// 已收敛。但所有出口都是 continue + 分散的计数器判断，任何一个分支漏判都会
// 变成无限循环。加一个与业务无关的绝对上限，最坏情况只是提前放弃一次调用，
// 而不是挂死一个请求 goroutine。取值远高于正常路径可能达到的迭代数。
const MaxZenAttempts = 64

// ZenRateLimitCooldown zen key 因 429 冷却的时长上限。
//
// 上游的 Retry-After 是"距下一个 00:00 UTC"的倒计时，不是真实恢复时间：实测
// 最后一个 429 报 RA=20h46m，而 18 分钟后同一 key 就恢复 200 了。按 RA 冷藏会
// 让多 key 池里的 key 白停一整天。取 20 分钟：足够把流量让给别的 key，又能在
// 一次瞬时节流过去后很快自愈。（整池都冷却时 PickZenKey 仍会轮转试探，不会
// 出现无 key 可用。）
// ZenRateLimitCooldown 普通限流（瞬时节流）的 key 冷却上限。
//
// 区分两类 429（2026-10-31 新增，见 ZenDailyExhausted）：
//   - 瞬时节流：等一等就恢复。冷却要有上限，否则一次抖动停整池。
//   - 日额度耗尽（FreeUsageLimitError）：当天不会再好。这种 key 停放到
//     下一次探针时刻，不再参与每次请求的轮转。
const ZenRateLimitCooldown = 20 * time.Minute

// ZenParkInitial 日额度耗尽后的首次停放时长。
//
// 为什么不直接停到 00:00 UTC：Retry-After 对"恢复时刻"撒过谎——2026-10-30
// 实测 RA=20h46m（正好指向 00:00 UTC）的那个 key，18 分钟后就正常 200 了。
// 停到午夜会让一次误判白停一整天。改为"停放 + 单个探针"：停放期内完全
// 不参与轮转（IDE 重试风暴不再逐请求打上游），到期只放一个探针出去。
const ZenParkInitial = 30 * time.Minute

// ZenParkMaxBackoff 停放间隔的指数退避上限。若额度真的整天不恢复，间隔从
// 30m 翻倍到 6h，一天只花 4~5 个探针，而不是每分钟一个。
const ZenParkMaxBackoff = 6 * time.Hour

// ZenModelPrefix zen 免费模型的网关前缀（与 workbuddy 的 cn:/global:、
// other 子平台同思路）：平台越来越多，加前缀让客户端在模型列表里
// 一眼分清归属，也避免与 Cline 免费模型表里的同名条目（如
// cline-free/laguna 之类）静默冲突。裸模型名（无前缀）仍照旧解析——
// 存量 IDE 配置与 combo target 不受影响。
var ZenModelPrefix = "zen:"

// ZenCanonicalID 归一化模型 ID：剥掉 zen: 前缀与遗留的 opencode/ 前缀并
// 小写化，返回目录里的真实 ID。所有解析入口（ResolveZenModel / 冲突判定 /
// 出站改写）先过它，保证带不带前缀、大小写如何都走同一条路。
func ZenCanonicalID(id string) string {
	return NormalizeZenModelID(strings.TrimPrefix(strings.TrimSpace(id), ZenModelPrefix))
}

// ZenPrefixedID 把目录 ID 加上网关前缀（/v1/models 输出用）。
func ZenPrefixedID(id string) string { return ZenModelPrefix + id }

// DefaultCompactBuffer 压缩预留缓冲（token）。20000 的旧默认值对"客户端自身
// 上限低于模型窗口"的场景无效：阈值 = ctx - max(output, buffer)，200k 窗口、
// 32k 输出时阈值 168k，而客户端（OpenClaw 等）可能在 128k 就封顶——压缩永远
// 不会触发，网关把满载请求原样送上游。
//
// 80000 让 200k/32k 这类模型的阈值降到 120k，落进 128k 客户端上限之内；
// 大窗口模型（beam 1M、nemotron-lightning 262k/262k）仍由 max(output, buffer)
// 主导，行为不变。压缩本身保留 8000 token 尾部（keepTokens），所以抬高 buffer
// 只是"更早开始摘要"，不会让上下文变浅。
const DefaultCompactBuffer = 80000

// NormalizeZenModelID 规范化 zen 模型 ID 用于表查找：trim + 小写 + 去掉
// "opencode/" 前缀。zen 模型 ID（种子与官方目录 id）均为小写；客户端可能发
// "Big-Pickle" 或 "opencode/big-pickle"，不规范化的查找会落空，进而把请求
// 错误路由到 cline 池（或让 dead/alias 标记不可达）。
func NormalizeZenModelID(id string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "opencode/")
}

// ResolveZenModel 解析模型名到 zen 模型。支持 "zen:" 前缀、遗留的
// "opencode/" 前缀与裸 ID（统一走 ZenCanonicalID 规范化）。面板与 /v1/models
// 输出 zen:<id>，存量配置用裸 id 继续可用。短别名（mimo/pickle 等）已随
// 种子快照一并删除（2026-11-11 用户定），不再解析。
// 410 迁移别名是最后兜底：原始 ID 不存在时返回继任模型（真实模型永远优先，
// 继任者上线不会被弃用别名遮蔽）。
func ResolveZenModel(id string) (*ZenModel, bool) {
	return ResolveZenModelDepth(id, 0)
}

// ZenAliasMaxDepth 410 迁移解析链的最大深度。持久化的迁移别名表是外部文件，
// 可能被手改成自环（A→A）或互相成环（A→B→A）。没有上限时递归会耗尽
// goroutine 栈，那是 fatal error: stack overflow —— recover 接不住，整个进程死。
const ZenAliasMaxDepth = 8

func ResolveZenModelDepth(id string, depth int) (*ZenModel, bool) {
	id = ZenCanonicalID(id)
	if id == "" {
		return nil, false
	}
	if depth > ZenAliasMaxDepth {
		log.Printf("zen model alias chain too deep at %q, giving up", id)
		return nil, false
	}
	zenModelsMu.RLock()
	if m, ok := zenModels[id]; ok {
		zenModelsMu.RUnlock()
		return m, true
	}
	// 宽限表兜底解析：ID、别名都能认。需在 zenModelsMu 读锁内调用；
	// 宽限过期的条目视为不存在（彻底清除由下一轮同步的 prune 顺带做）。
	if m, ok := ResolveZenGrace(id); ok {
		zenModelsMu.RUnlock()
		return m, true
	}
	zenModelsMu.RUnlock()
	// 弃用别名兜底：原始 ID 从模型表移除后（410 迁移过）落到继任模型。
	// 真实模型永远优先于别名——前面所有分支都没命中才会走到这里。
	// 递归解析继任者（深度受 ZenAliasMaxDepth 约束，防外部文件手改成环）。
	if repl := ZenDeprecatedReplacement(id); repl != "" && repl != id {
		return ResolveZenModelDepth(repl, depth+1)
	}
	return nil, false
}

// ResolveZenGrace 宽限表兜底解析：ID、别名都能认。需在 zenModelsMu 读锁内
// 调用；宽限过期的条目视为不存在（彻底清除由下一轮同步的 prune 顺带做）。
func ResolveZenGrace(id string) (*ZenModel, bool) {
	for gid, e := range zenGraceModels {
		if time.Since(e.prunedAt) > ZenPruneGrace {
			continue
		}
		if gid == id || e.m.ID == id {
			return e.m, true
		}
	}
	return nil, false
}

// IsZenFreeModel 免费判定: live 同步（已过价格门）、learned（学习器改过端点
// 但仍是同一批已入选免费模型）、或通用的 -free 后缀。
// big-pickle/union-alpha 无 free 后缀，靠 Source=="live"/"learned" 入选。
func IsZenFreeModel(m *ZenModel) bool {
	if m == nil {
		return false
	}
	if m.Source == "live" || m.Source == "learned" {
		return true
	}
	return strings.HasSuffix(m.ID, "-free")
}

// ZenProbeModel 管理面板 per-key "Test" 探测用的模型，按优先级：
//  1. big-pickle —— zen 免费层的默认别名（mint 收割的首选同款），最稳；
//  2. Source=="live" 的最小 id 模型 —— live 条目来自官方目录同步，
//     是"上游当前确实在供"的证明；
//  3. 任意 free 模型的最小 id —— 兜底。
//
// native-responses 模型也可能被选中——testZenKey 按其 Upstream 字段走对应
// 上游调用，无需特判。目录为空时返回 nil，探测直接报错（种子快照已取消，
// 冷启动空表靠启动同步 + 30s×10 快速重试链补齐，与 cline 空目录同口径）。
func ZenProbeModel() *ZenModel {
	zenModelsMu.RLock()
	defer zenModelsMu.RUnlock()
	if m, ok := zenModels["big-pickle"]; ok && IsZenFreeModel(m) {
		return m
	}
	var best *ZenModel
	for _, m := range zenModels {
		if !IsZenFreeModel(m) || (m.Source != "live" && m.Source != "learned") {
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
		if !IsZenFreeModel(m) {
			continue
		}
		if best == nil || m.ID < best.ID {
			best = m
		}
	}
	return best
}

// ResolveZenFreeModel 只解析免费 zen 模型
func ResolveZenFreeModel(id string) (*ZenModel, bool) {
	m, ok := ResolveZenModel(id)
	if !ok || !IsZenFreeModel(m) {
		return nil, false
	}
	return m, true
}

// RouteModel 决定请求走哪个上游: "zen" / "cline" / "reject"
// zen 免费模型 -> zen; zen 付费模型 -> reject(400); 其他 -> cline
func RouteModel(id string) string {
	id = strings.TrimSpace(id)
	// combo 别名兜底：调用方通常已把 model 改写为 target，这里防止 combo ID
	// 直接进入路由（按 combo 声明的平台走，不解析为普通模型）
	if plat, ok := Host.ComboPlatform(id); ok {
		return plat
	}
	// zen 表查找统一用小写去前缀的规范 ID：dead 标记与 410 别名都以规范 ID
	// 为键，客户端发 "Big-Pickle" / "opencode/big-pickle" 时不做归一就查不到，
	// 请求会误落到 cline 池（或让标记形同虚设）。
	zenID := NormalizeZenModelID(id)
	// 上游 400 "Model is unavailable" 的死亡标记：直接拒绝，不再每次请求
	// 都白烧一个上游往返。下次目录同步成功（或 TTL 过期）后自动解除。
	// 例外：该 id 若已有 410 继任别名，别名优先——迁移后请求仍有真实去处，
	// 不该被一条陈旧的 400 标记挡住。
	if ZenModelDead(zenID) && ZenDeprecatedReplacement(zenID) == "" {
		log.Printf("  route: %q marked unavailable by upstream, rejecting until next catalog sync", zenID)
		return "reject"
	}
	if zm, ok := ResolveZenModel(zenID); ok {
		if IsZenFreeModel(zm) {
			// 与 cline 模型表冲突时(几乎不可能)走 cline
			if !Host.HasModelEntry(zenID) {
				return "zen"
			}
		} else {
			return "reject"
		}
	}
	return "cline"
}

// ============ zen 配置 ============

type ZenCompactConfig struct {
	Auto         bool   `json:"auto"`         // 官方风格摘要压缩开关
	Buffer       int    `json:"buffer"`       // 预留输出缓冲 token,默认 20000
	KeepTokens   int    `json:"keepTokens"`   // 尾部保留 token 预算,默认 8000
	SummaryModel string `json:"summaryModel"` // 摘要模型,空=用请求模型
	MaxSummary   int    `json:"maxSummary"`   // 摘要最大输出 token,默认 4096
}

type ZenConfigData struct {
	Keys []string `json:"keys,omitempty"` // zen 多 key 池，请求按 round-robin 轮转
	// （旧单 key 兼容字段 Key 已随 2026-11-05 清理移除：现网配置均为 keys 数组
	// 形态，旧文件残留的 "key" 键按 JSON 未知字段忽略。）
	// OrgID：OAuth 账户的组织 ID（x-opencode-org-id）。CLI v2 在 OAuth
	// 账户流量中发该头，原始 API key 流量中没有。网关无法从 key 字符串
	// 推导它，空值即省略（正确）；使用 OAuth token 作 key 时，应从
	// 账户元数据填入，否则上游看到 OAuth token 却无 org 头，与官方
	// 客户端形态不一致。绝不编造。
	OrgID          string `json:"orgID,omitempty"`
	BaseURL        string `json:"baseURL"`
	MaxConcurrency int    `json:"maxConcurrency"` // zen 上游最大并发,防 worker 瞬时超限,默认 8
	Retries        int    `json:"retries"`        // 限流/网络错误重试次数,默认 3
	// 故障转移阈值/窗口（上游 b8a1be0 随 MarkZenFail 组引入）：连续 FailoverCount
	// 次失败才降级 zen，窗口 FailoverMinutes 分钟。429 恒不计数（限流不是降级）；
	// cline 池为空时 MarkZenFail 直接忽略（无转移目标，路由过去只会 401）。
	FailoverCount   int              `json:"failoverCount"`
	FailoverMinutes int              `json:"failoverMinutes"`
	Compaction      ZenCompactConfig `json:"compaction"`
	// KeyBindings 反过来留在这里:它的键是 key 明文,属于 key 自己的数据。放进
	// proxy-config.json 会让同一批明文 key 在 zen-config.json 的 keys 数组和
	// 代理文件里各存一份,冗余且容易写不一致。其他平台(or/other)的绑定
	// 表同理留在各自配置文件里。
	KeyBindings map[string]exitpool.Binding `json:"keyBindings,omitempty"`
	//
	// zen key 的路由参与表：key 明文 -> 是否进入请求轮转（面板"启用"列勾选）。
	// nil/缺项 = 参与。路由启用是铸造的前提（父集语义）：禁用路由的 key 不参与
	// 选号，收割机也不再为它铸造会话（ZenKeyAutoHarvestEnabled）。
	KeyRoutingEnabled map[string]bool `json:"keyRoutingEnabled,omitempty"`
	// KeyNicknames zen key 昵称表（账号列堆叠展示，2026-11-19）：key 明文 ->
	// 显示名。仅影响面板展示，不参与路由；空/缺 = 显示掩码 key。
	KeyNicknames map[string]string `json:"keyNicknames,omitempty"`
	// ToolFilter 请求体积裁剪（默认关闭）。按前缀/名单丢弃客户端工具、可选
	// 截断过长描述；gate 占位工具永不参与过滤。背景与实测数据见
	// zen_toolfilter.go 顶部注释（tools 占 ZCode 单请求 82% 体积）。
	ToolFilter ZenToolFilterConfig `json:"toolFilter"`
	// SessionRotateMinutes 已删除（2026-11-15 死配置清理）：后台粘性会话
	// rotator 已废弃（2026-11-08 随 a8fe356 吸收上游移除），旧 config 键经
	// JSON 未知字段自然忽略。
	// PinProxyPerKey 每个 key/账号固定一个出口代理（默认 true）。指针类型：
	// nil = 旧配置文件没有该键 → 按默认开启处理（见 PinProxyPerKeyEnabled）。
	// 关闭后恢复按请求轮转（zen 按 key 的 IP 指纹会重新出现，仅排障用）。
	PinProxyPerKey *bool `json:"pinProxyPerKey,omitempty"`
	// Cool（key 冷却表）与 KeyTokens（key token 记账）已拆出到独立
	// zen-state.json（zen_usage.go，2026-10-31）：它们是随请求/429 变化的
	// 使用痕迹，留在配置文件里会干扰外部对配置变更的监测。运行时真身仍是
	// 包级 zenKeyCool/zenKeyTokens（zenKeyMu 保护），冷却落盘持久化语义不变。
}

// ProxyRuleKinds 等代理规则 kind 白名单与排除表清理原语已迁
// internal/exitpool/binding.go（2026-11-22：消费方全在 app 层，与 zen 平台
// 调度无关，语义归属代理规则域）；app 侧经 zen_bridge 改指 exitpool。

// ZenKeyAutoHarvestEnabled 报告 key 是否参与自动铸造（启动补缺 / 403 触发 /
// 周期巡检 / 面板 Mint）。铸造跟随路由启用：路由关 = 该 key 不出请求，无需
// 会话，也不铸造；空 key 不铸造。
func ZenKeyAutoHarvestEnabled(key string) bool {
	if key == "" {
		return false
	}
	return ZenKeyRoutingEnabled(key)
}

// ZenKeyRoutingEnabled key 是否参与请求路由（"启用"列）。nil/缺项 = 参与；
// 空 key 不参与（配置池里不会有空项，这里只做防御）。
func ZenKeyRoutingEnabled(key string) bool {
	if key == "" {
		return false
	}
	cfg := GetZenConfig()
	if v, ok := cfg.KeyRoutingEnabled[key]; ok {
		return v
	}
	return true
}

// ZenHasRoutableKey 是否存在参与路由的启用 key（面板模型目录/刷新的展示
// 门控，与 platformHasRoutableCredential 的 zen 分支同判定）。
func ZenHasRoutableKey() bool {
	for _, k := range GetZenConfig().Keys {
		if ZenKeyRoutingEnabled(k) {
			return true
		}
	}
	return false
}

// ReconcileZenKeyRouting 整体替换 key 池后对齐"参与路由"表（面板保存 key 列表
// 时调用）。统一口径：新引入的 key 默认不参与路由（面板勾选"启用"后才生效，
// 与 Cline/WorkBuddy 的新增身份默认禁用一致）；已有 key 保持原状态；被移除的
// key 清理表项，避免陈旧数据。
func ReconcileZenKeyRouting(next, cur *ZenConfigData) {
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

// SetZenKeyRoutingEnabled 写入 key 的路由参与状态（面板"启用"勾选）。
// 写时复制：ZenConfig  是被请求路径并发读取的活配置。
func SetZenKeyRoutingEnabled(index int, enabled bool) error {
	cfg := GetZenConfig()
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
	SetZenConfig(&next)
	return nil
}

// SetAllZenKeysRoutingEnabled 一键启用/禁用全部 key 的路由参与（面板入口）。
// 空 key 跳过。返回实际改写的 key 数。
func SetAllZenKeysRoutingEnabled(enabled bool) int {
	cfg := GetZenConfig()
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
	SetZenConfig(&next)
	return n
}

// ReorderZenKeys 按旧 index 置换重排 zen key 池（管理面板拖拽排序）。
// order[i] = 重排后第 i 个位置上的原 index；面板只见掩码不见 key 明文，
// 因此协议用 index 置换而非 key 值。order 必须覆盖全部 key（缺失的按原相对
// 顺序追加在尾部，重复 index 幂等跳过；越界 index 整体拒绝）。
// 持久化、Key=Keys[0] 兼容字段同步、轮转游标归零都由 SetZenConfig 统一处理
// ——重排后 round-robin 从第一个 key 重新开始，与新顺序的语义一致。
// 冷却/token/会话粘性都按 key 明文存储，与顺序无关，重排零影响。
func ReorderZenKeys(order []int) error {
	cfg := GetZenConfig()
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
	SetZenConfig(&nc)
	return nil
}

func DefaultZenConfig() *ZenConfigData {
	return &ZenConfigData{
		// 不再有匿名 "public" 兜底：没有 key 就是空池，平台不可路由。
		Keys:           nil,
		BaseURL:        ZenAPIBase,
		MaxConcurrency: 8,
		Retries:        3,
		// 每 key 固定出口：2026-10-21 起 zen 按 key 记录出口 IP，轮转会
		// 让整池 key 被判可疑，故默认开启（无代理时自动退回直连）。
		PinProxyPerKey: BoolPtr(true),
		Compaction: ZenCompactConfig{
			Auto:       true,
			Buffer:     DefaultCompactBuffer,
			KeepTokens: 8000,
			MaxSummary: 4096,
		},
		// 体积裁剪默认关闭：丢工具会削减模型可用能力，必须由使用者显式开启。
		// 开启后按默认名单（丢弃 mcp__* 前缀工具）工作。
		ToolFilter: DefaultZenToolFilter(),
	}
}

var (
	ZenConfig   = LoadZenConfig()
	zenConfigMu sync.Mutex
)

// 并发信号量：zen 上游最大并发（maxConcurrency 配置，RebuildZenSem 重建）。
var (
	zenSem       chan struct{}
	zenFailCount int       // 连续失败计数（MarkZenFail/MarkZenSuccess 维护）
	zenFailUntil time.Time // 故障转移截止时间
	zenStateMu   sync.Mutex
)

func init() {
	RebuildZenSem()
	// 冷却/记账从独立痕迹文件回填（zen_usage.go，2026-10-31 拆分；原从
	// ZenConfig .Cool/KeyTokens 回填的路径随字段一并移除）。
	LoadZenUsage()
}

func RebuildZenSem() {
	cfg := GetZenConfig()
	n := cfg.MaxConcurrency
	if n <= 0 {
		n = 8
	}
	zenStateMu.Lock()
	zenSem = make(chan struct{}, n)
	zenStateMu.Unlock()
}

func MarkZenSuccess() {
	zenStateMu.Lock()
	zenFailCount = 0
	zenFailUntil = time.Time{}
	zenStateMu.Unlock()
}

func MarkZenFail() {
	// 故障转移只在"有地方可去"时才有意义。cline 池为空时把 zen 判为 degraded
	// 会把请求路由到一个没有账号的池，客户端拿到 401 "No accounts in pool" ——
	// 比原来的 429 更糟且更难懂（2026-10-30 实测：8 并发打满 3 次失败阈值后，
	// 后续所有请求都变成 401，看起来像"zen 彻底坏了"）。
	// 池为空时直接不记失败：zen 自己的 429/冷却逻辑仍在工作，没有可转移的目标。
	if total, active := Host.PoolStatusSnapshot(); total == 0 || active == 0 {
		return
	}
	cfg := GetZenConfig()
	thr := cfg.FailoverCount
	if thr <= 0 {
		thr = 3
	}
	window := cfg.FailoverMinutes
	if window <= 0 {
		window = 5
	}
	zenStateMu.Lock()
	zenFailCount++
	if zenFailCount >= thr {
		zenFailUntil = time.Now().Add(time.Duration(window) * time.Minute)
	}
	zenStateMu.Unlock()
}

// ZenFailedNow zen 是否处于故障转移状态
func ZenFailedNow() bool {
	zenStateMu.Lock()
	defer zenStateMu.Unlock()
	if zenFailUntil.IsZero() {
		return false
	}
	if time.Now().After(zenFailUntil) {
		zenFailCount = 0
		zenFailUntil = time.Time{}
		return false
	}
	return true
}

// ZenClientErrorStatus 把 zen 上游的错误映射成回给客户端的 HTTP 状态码。
//
// 默认 502：网关确实没能从上游拿到 2xx，语义是"坏网关"。
// 限流例外：上游 429 是"我这个 key 用完了，你等会再来"，原因在客户端可以
// 应对的一侧（退避重试、换模型）。回 502 会让 IDE 认为网关坏了——实测
// OpenCode/Claude 系客户端对 502 的处置是标记服务不可用，而对 429 会按
// Retry-After 退避重试。故 429 原样透传状态码。
func ZenClientErrorStatus(err error) int {
	// 2026-11-09 起 429 透传判定由 upstreamErrorStatus 全平台共用,本函数保留为
	// zen 调用点的可读别名(行为不变:429 → 429,其余 → 502)。
	return Host.UpstreamErrorStatus(err, http.StatusBadGateway)
}

// IsRateLimited 限流信号识别: 429/503 直接命中; 502/403 按错误体关键词。
// 关键词含配额耗尽类措辞——它们与限流同样应触发冷却/换 key，而不是当作
// 普通错误立刻回给客户端。
func IsRateLimited(status int, body string) bool {
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

// NormalizeZenKeys 规范 key 池：去空去重、剔除遗留的匿名哨兵 "public"
// （2026-10-26 起不再支持匿名访问）。允许空池：没有 key 时 zen 平台不可路由
// （目录隐藏、请求明确报错），不再回退到无凭据的 "public"。
func NormalizeZenKeys(cfg *ZenConfigData) {
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
}

func LoadZenConfig() *ZenConfigData {
	// 配置本体与 sessions/modelAliases/endpoints 同住 zen-config.json，
	// 这里只取 zen 段（损坏留档与迁移逻辑都在 LoadZenConfigFileLocked 里）。
	zenFileMu.Lock()
	file := LoadZenConfigFileLocked()
	zenFileMu.Unlock()
	cfg := &file.ZenConfigData
	// ZEN_ORG_ID：OAuth 账户的组织 ID，写入 x-opencode-org-id 头。原始 API key
	// 里没有这个信息，无法推导；只有用 OAuth token 作 key 时才需要显式配置。
	// 未设置即沿用配置文件里的值（空 = 省略该头，上游对原始 key 流量也不发）。
	if org := kit.EnvStr("ZEN_ORG_ID"); org != "" {
		cfg.OrgID = org
		log.Printf("zen org id set from env: %s", org)
	}
	NormalizeZenKeys(cfg)
	if cfg.BaseURL == "" {
		cfg.BaseURL = ZenAPIBase
	}
	// PinProxyPerKey 缺省物化（上游 3758eb7）：旧配置文件没有该键时按"开启"
	// 落定为显式值。使用侧 PinProxyPerKeyEnabled 本就把 nil 解释为开启，这里
	// 物化让缺省语义在读取后即可见、下次保存即写回配置文件。
	if cfg.PinProxyPerKey == nil {
		cfg.PinProxyPerKey = BoolPtr(true)
	}
	// 配置文件是外部可编辑的，也要过同一把校验锁：否则绕过管理面板的校验
	//（改文件即可）就能把 zen key 发去内网。非法值就地回退到默认，不阻断启动
	// —— zen 只是可选上游，不该因为它启动失败。
	if err := ValidateZenUpstreamBase(cfg.BaseURL); err != nil {
		log.Printf("zen baseURL rejected from config file (%v), falling back to %s", err, ZenAPIBase)
		cfg.BaseURL = ZenAPIBase
	}
	return cfg
}

func SaveZenConfig() {
	zenConfigMu.Lock()
	next := *ZenConfig
	zenConfigMu.Unlock()
	// 只覆盖 zen 段，sessions/modelAliases/endpoints 原样保留。冷却/记账
	// 已拆到 zen-state.json（zen_usage.go），不再随配置落盘。
	MutateZenConfigFile(func(f *ZenConfigFileData) {
		f.ZenConfigData = next
	})
}

// RawZenConfig 池真身指针访问（测试白盒替换）。
func RawZenConfig() *ZenConfigData { return ZenConfig }

// GetZenConfig 活配置读。
func GetZenConfig() *ZenConfigData {
	zenConfigMu.Lock()
	defer zenConfigMu.Unlock()
	return ZenConfig
}

// SetZenKeyBindings 只替换 zen key 的代理绑定段并落盘。不走 SetZenConfig ——
// 那条路会重建信号量、重置轮转游标、按 key 列表清理各种状态,对"只改绑定"
// 是过度副作用。
func SetZenKeyBindings(m map[string]exitpool.Binding) {
	zenConfigMu.Lock()
	next := *ZenConfig
	next.KeyBindings = m
	ZenConfig = &next
	zenConfigMu.Unlock()
	SaveZenConfig()
}

func SetZenConfig(c *ZenConfigData) {
	NormalizeZenKeys(c)
	zenConfigMu.Lock()
	ZenConfig = c
	zenConfigMu.Unlock()
	RebuildZenSem()
	// 清理已移除 key 的轮转状态
	valid := map[string]bool{}
	for _, k := range c.Keys {
		valid[k] = true
	}
	zenKeyMu.Lock()
	// 冷却/记账真身保留运行期现值（2026-10-31 拆分：痕迹不再随配置回填，
	// 备份导入不还原冷却/记账，与 or/other 口径一致）；已移除 key 的
	// 条目清理掉。
	for k := range zenKeyCool {
		if !valid[k] {
			delete(zenKeyCool, k)
		}
	}
	for k := range zenKeyTokens {
		if !valid[k] {
			delete(zenKeyTokens, k)
		}
	}
	ZenKeyIdx = 0
	zenKeyMu.Unlock()
	// 会话粘性与收割计数同样按 key 存储，移除的 key 必须一并清掉：否则长跑容器
	// 上轮换 key 会让 .zen-sessions.json 与两个收割表单调增长，被删掉的旧 key
	// 的粘性身份还留在盘上。
	PruneZenKeyState(valid)
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
	ReconcileEgressBindings()
	// 落盘放最后：配置写 zen-config.json；冷却/记账的现值落痕迹文件
	// （zen-state.json，key 移除后的清理结果随本次落盘持久化）。
	SaveZenConfig()
	SaveZenUsage()
}

// ValidateZenUpstreamBase 校验管理面板设置的 zen 上游基地址。
//
// BaseURL 是"把 zen key 发到哪里"的开关，所以它既是 SSRF 入口也是凭据外泄
// 入口。目录同步那条路径只接受 https://opencode.ai/inference/...；管理面板
// 则必须放行自建中转/抓包代理（这是文档化的用法），不能套用同一把锁。折中：
// 要求绝对 URL + http/https + 有 host，并显式拒绝回环/链路本地/私网地址 ——
// 一个"上游中转"没有任何理由指向 127.0.0.1 或 169.254.169.254。
//
// ZEN_ALLOW_PRIVATE_BASE=true 可放行私网（本地跑 mock 上游测试用）。
func ValidateZenUpstreamBase(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("baseURL 无法解析: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("baseURL 必须是 http:// 或 https:// 开头，得到 %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("baseURL 缺少主机名")
	}
	if ZenAllowPrivateBase() {
		return nil
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("baseURL 指向内网/回环地址 %q 被拒绝（如需本地测试请设 ZEN_ALLOW_PRIVATE_BASE=true）", host)
		}
		return nil
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return fmt.Errorf("baseURL 指向内网主机 %q 被拒绝（如需本地测试请设 ZEN_ALLOW_PRIVATE_BASE=true）", host)
	}
	return nil
}

func ZenAllowPrivateBase() bool {
	v, ok := kit.EnvBool("ZEN_ALLOW_PRIVATE_BASE")
	return ok && v
}

// ValidateProxyList 校验代理列表格式: 支持 http/https/socks5/socks5h, 必须包含 host:port。
func ValidateProxyList(proxies []string) error {
	for _, p := range proxies {
		line := strings.TrimSpace(p)
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil {
			return fmt.Errorf("invalid proxy line %q: %v", line, err)
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("proxy %q: unsupported scheme (supported: http/https/socks5/socks5h)", line)
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

// MaskZenKey 管理面板与 mint 结果里展示 key 的掩码形式。
// 不复用 kit.Truncate：它对短于上限的串原样返回，短 key 会被整只打印出来。
func MaskZenKey(k string) string {
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
	ZenKeyIdx int
	// zenKeyTokens 每 key token 记账（内存态）：今日/累计消耗与最近使用。
	// （上游的 zenKeyUsage 成功计数在本地由本表替代，声明一并省去。）
	zenKeyTokens = map[string]ZenKeyTokenStats{}
	zenKeyCool   = map[string]time.Time{}
	// zenKeyPark 日额度耗尽（FreeUsageLimitError）的 key 停放状态。
	// 与 zenKeyCool 分开：cool 是"等一会再说"（瞬时节流），park 是
	// "今天没了，别再逐请求打上游"——两者在面板/日志里语义不同，
	// 混用一个 map 会让排查时分不清"抖动"和"额度用完"。
	zenKeyPark = map[string]ZenParkState{}
)

// ZenParkState 一个被停放的 key：到 NextProbe 之前完全不参与轮转；
// 到期后放行**一次**探针，若仍 429 则间隔翻倍（MaxBackoff 封顶）。
type ZenParkState struct {
	Until     time.Time // 停放截止；此前 PickZenKey 直接跳过
	NextProbe time.Time // 下一个探针时刻（== Until，保留可读性）
	Backoff   time.Duration
	Probes    int    // 已发出的探针次数（面板展示"试过几次了"）
	Day       string // 触发停放的 UTC 日期（跨日自动失效）
	Reason    string // 上游错误摘要（面板展示）
}

// ZenDailyExhausted 判断一次限流回包是否是"当天免费额度已耗尽"，
// 而不是可等待恢复的瞬时节流。
//
// 判据来自实测（2026-10-30/31）：
//   - 错误体 type 是 FreeUsageLimitError，消息形如
//     "Free usage limit reached. Free models work in OpenCode ... subscribe to
//     OpenCode Go to keep going"，这是日额度耗尽的固定文案；
//   - FreeTierError（403）是门禁/策略拒绝，完全不同的类别，不能被当成额度耗尽；
//   - Retry-After 恰好指向 00:00 UTC（误差容忍几分钟）。
//
// 三者都满足才判定"日额度耗尽"。只靠 Retry-After 指向午夜是不够的：
// 一个瞬时节流恰好发生在 23:50 时，其 RA 也会指向午夜。
func ZenDailyExhausted(status int, body string, retryAfter time.Duration) bool {
	if status != http.StatusTooManyRequests {
		return false
	}
	low := strings.ToLower(body)
	// 必须是"用量额度"类错误；显式排除门禁类
	if strings.Contains(low, "freetiererror") {
		return false
	}
	if !strings.Contains(low, "freeusagelimiterror") &&
		!strings.Contains(low, "free usage limit") {
		return false
	}
	// RA 须指向下一个 00:00 UTC（容差 30 分钟：上游按秒倒计时，
	// 且我们从收到响应到计算的延迟会让值略小）。
	if retryAfter <= 0 {
		return false
	}
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
	delta := midnight.Sub(now.Add(retryAfter))
	if delta < 0 {
		delta = -delta
	}
	return delta <= 30*time.Minute
}

// zenFingerprintSuspectTokens 判定"疑似指纹闸"的当日用量上限：一把当天几乎
// 没用过（≤100 tokens，覆盖一次最小冒烟的消耗）的 key 吃到"日额度耗尽"形态
// 的 429，更可能是免费层身份指纹闸按请求内容命中（见 zen_toolfilter.go 机制
// 注释），而非真额度耗尽。写死不做成配置：这是排障判据不是调优参数。
const zenFingerprintSuspectTokens = 100

// zenKeyTokensToday key 当日已记账 tokens（无记录或跨日=0）。
// 指纹闸自愈判据用；zenKeyMu 短临界区，调用方不得已持锁。
func zenKeyTokensToday(key string) int64 {
	if key == "" {
		return 0
	}
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	st, ok := zenKeyTokens[key]
	if !ok || st.Date != time.Now().Format("2006-01-02") {
		return 0
	}
	return st.Today
}

// zenClientToolNameList 客户端工具名清单（保持原顺序）。工具改名映射构建与
// 指纹闸自愈日志共用。
func zenClientToolNameList(params map[string]any) []string {
	names := make([]string, 0, 8)
	for _, t := range ZenClientTools(params) {
		if tm, ok := t.(map[string]any); ok {
			if fn, ok := tm["function"].(map[string]any); ok {
				if n, ok := fn["name"].(string); ok {
					names = append(names, n)
				}
			}
		}
	}
	return names
}

// zenRenameCandidateNames 改名候选 = 客户端工具名 − gate 占位名。客户端与
// gate 同名时（ZenMergeTools 客户端优先），同名工具改名会让出站 tools 缺失
// gate 名、FreeTier 直接 403——同名工具保持真名出站，闸与还原两侧同时自洽
// （2026-11-27 审计 S3）。
func zenRenameCandidateNames(params map[string]any) []string {
	gate := make(map[string]bool, len(ZenGateToolSpecs))
	for _, g := range ZenGateToolSpecs {
		gate[g.name] = true
	}
	out := make([]string, 0, len(gate))
	for _, n := range zenClientToolNameList(params) {
		if gate[n] {
			continue
		}
		out = append(out, n)
	}
	return out
}

// zenClientToolNames 客户端工具名清单（逗号连接），指纹闸自愈日志用——命中
// 后靠它做二分定位（哪个工具的名字/描述是指纹）。
func zenClientToolNames(params map[string]any) string {
	return strings.Join(zenClientToolNameList(params), ",")
}

// ParkZenKey 把 key 停放起来（日额度耗尽）。返回本次的停放时长，
// 0 表示没有停放（单 key 的 public 哨兵，语义同 CooldownZenKey）。
//
// 指数退避：首次 ZenParkInitial（30m），之后每次探针仍耗尽则翻倍，
// 上限 ZenParkMaxBackoff（6h）。这样即便额度整天不恢复，一天也只消耗
// 不到 6 个探针；而万一上游提前恢复（实测确实会提前），最多 30 分钟
// 后就能自己发现——不需要人手动解封。
func ParkZenKey(key string, reason string) time.Duration {
	if key == "" {
		return 0
	}
	if key == "public" && len(GetZenConfig().Keys) <= 1 {
		return 0
	}
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	st := zenKeyPark[key]
	day := time.Now().UTC().Format("2006-01-02")
	if st.Day != day {
		// 新的一天：退避从头开始（跨日必须重置，否则昨天的 6h 会
		// 让今天的第一个探针白等半天）
		st = ZenParkState{}
	}
	if st.Backoff <= 0 {
		st.Backoff = ZenParkInitial
	} else {
		st.Backoff *= 2
		if st.Backoff > ZenParkMaxBackoff {
			st.Backoff = ZenParkMaxBackoff
		}
	}
	st.Probes++
	st.Day = day
	st.Reason = kit.Truncate(reason, 200)
	until := time.Now().Add(st.Backoff)
	st.Until = until
	st.NextProbe = until
	zenKeyPark[key] = st
	// 冷却表里的同类项清掉：park 已经表达"更久的不可用"，
	// 留着 20m 的 cool 会让面板同时显示两个截止时刻。
	delete(zenKeyCool, key)
	return st.Backoff
}

// ZenKeyParked 查询 key 是否处于停放期（未到探针时刻）。
func ZenKeyParked(key string) (time.Time, bool) {
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	st, ok := zenKeyPark[key]
	if !ok {
		return time.Time{}, false
	}
	if st.Day != time.Now().UTC().Format("2006-01-02") {
		delete(zenKeyPark, key) // 跨日自动失效
		return time.Time{}, false
	}
	if time.Now().Before(st.Until) {
		return st.Until, true
	}
	return time.Time{}, false
}

// zenKeyParkState 快照（面板用），不改变状态。
func ZenKeyParkSnapshot(key string) (ZenParkState, bool) {
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	st, ok := zenKeyPark[key]
	if !ok || st.Day != time.Now().UTC().Format("2006-01-02") {
		return ZenParkState{}, false
	}
	return st, true
}

// ZenAllParkedUntil 当池内每个 key 都在停放期时，返回最早的探针时刻。
// ok=false 表示至少有一个 key 可用（含"有 key 但没停放"的常态）。
//
// 与 AnyZenKeyParked 的差别：这个函数返回"还要等多久"，供拒绝错误和
// 日志使用；AnyZenKeyParked 只回答布尔问题。
func ZenAllParkedUntil() (time.Time, bool) {
	keys := GetZenConfig().Keys
	if len(keys) == 0 {
		return time.Time{}, false
	}
	var earliest time.Time
	for _, k := range keys {
		if k == "" {
			continue
		}
		// public 哨兵在单 key 池里从不被停放（ParkZenKey 直接返回 0），
		// 因此这里自然跳过它、判定为"有可用 key"。
		until, parked := ZenKeyParked(k)
		if !parked {
			return time.Time{}, false
		}
		if earliest.IsZero() || until.Before(earliest) {
			earliest = until
		}
	}
	return earliest, !earliest.IsZero()
}

// ZenParkProbeCount 已发出的探针次数（日志用；未停放时为 0）。
func ZenParkProbeCount(key string) int {
	st, ok := ZenKeyParkSnapshot(key)
	if !ok {
		return 0
	}
	return st.Probes
}

// UnparkZenKey 解除停放（面板"Test"成功、或配置变更时调用）。
func UnparkZenKey(key string) {
	if key == "" {
		return
	}
	zenKeyMu.Lock()
	delete(zenKeyPark, key)
	zenKeyMu.Unlock()
}

// AnyZenKeyParked 池内是否所有 key 都在停放期（用于给出明确的
// "今日免费额度已用完，预计 <时间> 恢复"错误，而不是反复 429）。
func AnyZenKeyParked() bool {
	keys := GetZenConfig().Keys
	if len(keys) == 0 {
		return false
	}
	for _, k := range keys {
		if _, parked := ZenKeyParked(k); !parked {
			return false
		}
	}
	return true
}

// PickZenKey round-robin 选取一个未冷却的 key；全部冷却时按轮转顺序返回下一个。
// 隔离模式（proxy_binding.go）下，绑定主备用出口都不可用的 key 被排除出所有
// 候选轮 —— 隔离优先于可用性；全部 key 都被挡住时返回 ""，调用方报
// "无可用出口"而不是静默直连。返回 "" 也表示未配置任何 key。
//
// egress 钉定层（3758eb7，egress.go）：显式 KeyBindings 之下，未绑定 key 在
// pinProxyPerKey 开启且代理池非空时自动分配一个稳定出口（重启不变），代理比
// key 少时多出来的 key 被过滤——它们的流量若继续发出（直连或借用别人的出口），
// 上游就会把"一个 key 多个 IP"的指纹重新打上，这正是要消除的。
// ZenKeyGate key 参与轮转前的三层门（PickZenKey 的过滤与 ZenNoKeyError 的
// 报错共用同一实现，保证错误口径与真实过滤一致）：blocked = 隔离模式下显式
// 绑定主备出口都不可用；routingOff = 面板"启用"列关闭（这类 key 也不参与
// 自动铸造，见 KeyRoutingEnabled 字段注释：路由启用是铸造的前提）；
// noEgress = egress 钉定生效（开关开 + 池非空）却没分到自动出口——直连或
// 借用别人的出口都会重新打上"一 key 多 IP"指纹，整体过滤。不含 parked 层
// （zenKeyMu 锁内状态，PickZenKey 的 buildCandidates 自行过滤）。
func ZenKeyGate(keys []string) (blocked, routingOff, noEgress map[string]bool) {
	blocked = map[string]bool{}
	routingOff = map[string]bool{}
	noEgress = map[string]bool{}
	if exitpool.IsolationEnabled() {
		for _, k := range keys {
			if main, backup, bound := Host.KeyBindingOf(k); bound && !exitpool.BoundProxiesRoutableFor(exitpool.KindZen, main, backup) {
				blocked[k] = true
			}
		}
	}
	for _, k := range keys {
		if !ZenKeyRoutingEnabled(k) {
			routingOff[k] = true
		}
	}
	if EgressBindingActive() {
		for _, k := range keys {
			if _, ok := EgressProxyForZen(k); !ok {
				noEgress[k] = true
			}
		}
	}
	return blocked, routingOff, noEgress
}

// ZenNoKeyError 轮转取不到 key 时的错误：按真实拒绝层给出可操作原因——
// parked（免费日配额耗尽）、显式绑定出口隔离阻断、路由关闭、钉定层没分到
// 出口四层分开报（2026-11-05 生产实证：20 个 key 白天全部因免费配额 parked，
// 旧文案却恒报 "add proxies, or turn off pinProxyPerKey"，把官方配额限制
// 误导成代理池容量问题）。
func ZenNoKeyError() error {
	keys := GetZenConfig().Keys
	if len(keys) == 0 {
		return fmt.Errorf("no zen key configured: add an enabled key in the admin panel")
	}
	total := len(keys)
	// 全部停放（日配额耗尽）：直接给配额语义与恢复时刻——这是官方免费层
	// 限制，与代理池无关，别让人去查代理。
	if until, ok := ZenAllParkedUntil(); ok {
		return fmt.Errorf("zen free daily quota exhausted: all %d key(s) parked; earliest auto-probe at %s UTC",
			total, until.UTC().Format("15:04 MST"))
	}
	blocked, routingOff, noEgress := ZenKeyGate(keys)
	parkedN := 0
	for _, k := range keys {
		if _, p := ZenKeyParked(k); p {
			parkedN++
		}
	}
	parts := make([]string, 0, 4)
	if parkedN > 0 {
		parts = append(parts, fmt.Sprintf("%d parked (daily quota exhausted)", parkedN))
	}
	if n := len(blocked); n > 0 {
		parts = append(parts, fmt.Sprintf("%d bound-exit-blocked (bound exits cooling or removed)", n))
	}
	if n := len(routingOff); n > 0 {
		parts = append(parts, fmt.Sprintf("%d routing-disabled", n))
	}
	if n := len(noEgress); n > 0 {
		parts = append(parts, fmt.Sprintf("%d without an auto-assigned egress (%d proxies configured)", n, len(exitpool.GetPool().Proxies)))
	}
	if len(parts) == 0 {
		return fmt.Errorf("no zen key routable: all %d key(s) are cooling — wait for cooldown expiry", total)
	}
	return fmt.Errorf("no zen key routable (%d keys): %s", total, strings.Join(parts, "; "))
}

// ZenBlockedExitErr 固定 key（pinKey 面板 Test / ZEN_PIN_KEY）出口不可用时
// 的错误：显式绑定 = 主备都冷却/被删（隔离口径文案）；无显式绑定 = egress
// 自动钉定没分到出口（池容量不足，容量口径文案）。
func ZenBlockedExitErr(key string) error {
	if main, backup, bound := Host.KeyBindingOf(key); bound {
		return errors.New(Host.BlockedExitMessage(
			fmt.Sprintf("key#%d bound proxies unavailable (cooling or removed); probe aborted by proxy isolation", KeyIndex(key)),
			main, backup))
	}
	return EgressShortfallError("zen", egressNSZen, GetZenConfig().Keys)
}

func PickZenKey() string {
	keys := GetZenConfig().Keys
	if len(keys) == 0 {
		return ""
	}
	// 三层门一次算好（ZenKeyGate 与 ZenNoKeyError 共用，保证报错口径与真实
	// 过滤一致）；绑定可用性只依赖叶子锁，先在循环外批量算好，三轮选择零
	// 额外加锁。
	blocked, routingOff, noEgress := ZenKeyGate(keys)
	alive := 0
	for _, k := range keys {
		if !blocked[k] && !routingOff[k] && !noEgress[k] {
			alive++
		}
	}
	if alive == 0 {
		return ""
	}
	// live 会话优先轮已删除（2026-11-05）：粘性会话时代"未 mint 的 key 必
	// 403"是成立的（首用即铸造才有条目）；现铸模式（2026-10-27 起）下每次
	// 调用现铸会话、zenSessions 不再随首用增长，live 集合退化为"老 key 化石
	// 集合"——live 优先轮把此后添加的 key 永久挡在轮转外（生产实证：5 条
	// 新 key 一天零流量恒"待启用"）。现铸下所有可路由 key 平等参与轮转。
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	now := time.Now()
	// 先清理过期冷却
	for k, until := range zenKeyCool {
		if now.After(until) {
			delete(zenKeyCool, k)
		}
	}
	// 清理跨日的停放记录：昨日的额度状态对今天没有意义，留着会让
	// 今天第一个请求就吃到昨天退避到的 6h。
	today := now.UTC().Format("2006-01-02")
	for k, st := range zenKeyPark {
		if st.Day != today {
			delete(zenKeyPark, k)
		}
	}
	// parked 停放期内的 key（日额度耗尽）完全不参与轮转。
	// 这是与"冷却"的关键区别：冷却期满会主动再试，停放期满同样放行，
	// 但停放期内的请求**一次都不发**——IDE 的重试风暴不该逐个撞上游。
	parked := func(k string) bool {
		st, ok := zenKeyPark[k]
		if !ok || st.Day != today {
			return false
		}
		// 到期（Until 已过）即视为可放行一次探针
		return now.Before(st.Until)
	}

	// 两轮候选：未冷却 → 全冷却时兜底（UNABLE_FORCE=true，保持请求流动）。
	// 每轮候选按轮转顺序生成，最终由账号调度策略（round_robin/fill/random，
	// 与 Cline 账号池共用同一配置）挑选。两轮都带着 blocked/routingOff/
	// noEgress 过滤——没出口的 key 连"冷却重试"都不该发出去。
	buildCandidates := func(ignoreCooling bool) []string {
		var out []string
		for i := 0; i < len(keys); i++ {
			k := keys[(ZenKeyIdx+i)%len(keys)]
			if blocked[k] || routingOff[k] || noEgress[k] || parked(k) {
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
	candidates := buildCandidates(false)
	if len(candidates) == 0 && kit.UnableForce() {
		candidates = buildCandidates(true) // 全冷却兜底（UNABLE_FORCE=true：保持请求流动）
	}
	if len(candidates) == 0 {
		return ""
	}

	var picked string
	switch Host.Strategy() {
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
				ZenKeyIdx = (i + 1) % len(keys)
				break
			}
		}
	}
	return picked
}

// ZenKeyTokenStats 单个 key 的 token 记账：今日/累计消耗（prompt+completion）
// 与跨日日期、最近成功时间。仅内存态。
type ZenKeyTokenStats struct {
	Today      int64
	Total      int64
	Date       string
	LastUsedAt time.Time
}

// RecordZenKeyTokens 记录 key 本次请求消耗的 token，自动处理跨日重置，
// 累计值不重置；同时刷新最近使用时间。tokens<=0 时忽略。
func RecordZenKeyTokens(key string, tokens int64) {
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
	// 200 是"该 key 现在确实可用"的最强证据：解除停放并清掉退避。
	// 停放到期后的第一次探针若成功，走的正是这里——不停用重试路径，
	// 上游提前恢复时最多 30 分钟就能自动回到轮转。
	delete(zenKeyPark, key)
	zenKeyMu.Unlock()
	MarkZenTokensDirty() // 合批落盘：不在这里同步写文件（热路径）
}

// zen token 记账落盘：记账在每个请求的热路径上，逐请求重写 zen-config.json
// 太重，故只打脏标记，由后台 flusher 合批写盘（kit.DirtyFlusher，与 cline
// 账号池的 markPoolDirtyLocked + startPoolFlusher 同思路）。
// SaveZenConfig 自身会取 zenKeyMu，故 Mark 必须在 zenKeyMu 之外调用。
var zenTokensFlush = kit.NewDirtyFlusher(10*time.Second, SaveZenUsage)

func MarkZenTokensDirty() { zenTokensFlush.Mark() }

// CooldownZenKey 将 key 置为冷却，冷却期内 round-robin 跳过它。
// 返回实际生效的冷却时长（0 = 空 key 未冷却）——
// 调用点的日志必须打印这个值而不是原始解析值，否则"缺头默认 1 分钟 / 24h 上限"
// 的换算不会体现在日志里。
func CooldownZenKey(key string, d time.Duration) time.Duration {
	if key == "" {
		return 0
	}
	if d <= 0 {
		d = time.Minute
	}
	// Retry-After 不可信，必须封顶（2026-10-30 实测）。
	//
	// zen 的 Retry-After 恒等于"距下一个 00:00 UTC 的秒数"（实测 75020/74810/
	// 73790…，全部落在同日 23:59:5x），它是每日窗口的复位倒计时，不是这次限流
	// 的真实恢复时间。实际观测：最后一个 429（RA=20h46m）之后 **18 分钟**就恢复
	// 正常 200 了——按 RA 冷藏 key 会让 6 key 池里的 5 个 key 白停一整天，而池子
	// 其实已经可用。
	//
	// 因此 zen key 的冷却以 ZenRateLimitCooldown 封顶：够久到把流量让给别的 key，
	// 又不至于让一次瞬时节流把整池停到午夜。若整个池都在冷却，PickZenKey 仍会
	// 轮转返回一个 key 继续试探，所以封顶不会造成"无 key 可用"。
	if d > ZenRateLimitCooldown {
		d = ZenRateLimitCooldown
	}
	zenKeyMu.Lock()
	zenKeyCool[key] = time.Now().Add(d)
	zenKeyMu.Unlock()
	SaveZenUsage() // 冷却落盘持久化（2026-10-28）：重启后冷却保留
	return d
}

// ZenKeyCooling 查询 key 是否处于冷却期。
func ZenKeyCooling(key string) bool {
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

// ZenKeyCooldownUntil 只读查询冷却截止时刻（不清理过期项）。
// 管理面板"Test"命中 429 后用它回报预计恢复时间——上游的 Retry-After 决定
// 冷却时长（上限 24h），不写进日志的话面板是唯一能看到的观测点。
func ZenKeyCooldownUntil(key string) (time.Time, bool) {
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	until, ok := zenKeyCool[key]
	return until, ok
}

// UncoolZenKey 清除 key 的冷却。管理面板"Test"探测成功后调用，语义与 cline
// 账号的"测试成功即复位"一致：一次真实的 2xx 是"该 key 现在能用"的最强证据，
// 比干等 Retry-After 更可信。对未冷却/无关 key 是无害 no-op。
func UncoolZenKey(key string) {
	if key == "" {
		return
	}
	zenKeyMu.Lock()
	delete(zenKeyCool, key)
	zenKeyMu.Unlock()
	SaveZenUsage() // 解除冷却同步落盘：否则重启后 LoadZenUsage 会把盘上的冷却复活
}

// ZenAnyKeyRotatable 是否还有"当前 key 之外、且未在冷却、且未被停放"的 key
// 可以马上换过去。决定 429 之后该不该睡眠重试：没有可换的 key 时，睡 30s 再打
// 同一个已冷却的 key 只是在挂住客户端的连接（实测单 key 池一次请求挂 101s 才
// 返回 429）。与 PickZenKey 不同：这里不推进 round-robin 游标，只是只读探测。
//
// 停放（park）必须一起排除：ParkZenKey 会把 key 从 zenKeyCool 里删掉，所以
// "不在冷却"包含"已停放"。漏掉它，判定就会说"还有 key 可换"，于是睡眠重试
// 打回同一个额度耗尽的 key —— 正是 101s 挂起那个 bug 换了条路回来。
func ZenAnyKeyRotatable(cur string) bool {
	for _, k := range GetZenConfig().Keys {
		if k == "" || k == cur || ZenKeyCooling(k) {
			continue
		}
		if _, parked := ZenKeyParked(k); parked {
			continue
		}
		return true
	}
	return false
}

// ZenCallOpts 单次上游调用的可选参数（变参：既有 CallZenAPI/CallZenResponsesAPI
// 调用点零改动）。目前只有 pinKey：把本次调用的**所有**尝试固定在一个 key 上。
// 与 ZEN_PIN_KEY 环境变量的区别：那是进程级排障开关，会影响所有并发请求；
// pinKey 只作用于这一次调用（管理面板的 per-key "Test" 按钮），绝不影响正常轮转。
type ZenCallOpts struct {
	PinKey string
}

// ZenKeyStatus 每个 key 的运行时状态（管理面板展示用，key 值打码）。
func ZenKeyStatus() []map[string]any {
	cfg := GetZenConfig()
	keys := cfg.Keys
	nickOf := cfg.KeyNicknames
	// 出口绑定先快照：egress 路径取 zenConfigMu/egressMu，不能与 zenKeyMu 嵌套。
	// （会话快照已随面板会话区块拆除移除：现铸模式下无粘性会话可展示，
	// zenSessions 表仅剩 403 换新闸门在用，见 zen_session.go。）
	egress := ZenEgressSnapshotOf(keys)
	// 每 key 组织 ID 也先快照（zenOrgMu），同样不与 zenKeyMu 嵌套。
	orgs := ZenKeyOrgMap(keys)
	zenKeyMu.Lock()
	defer zenKeyMu.Unlock()
	now := time.Now()
	out := make([]map[string]any, 0, len(keys))
	for i, k := range keys {
		st := map[string]any{
			"index":   i,
			"keyMask": MaskZenKey(k),
			"current": i == ZenKeyIdx%MaxInt(len(keys), 1),
			// 路由参与状态（面板"启用"列勾选，勾掉 = 不进入请求轮转）。
			"routingEnabled": ZenKeyRoutingEnabled(k),
			// 昵称（账号列堆叠展示，2026-11-19）：空 = 显示掩码 key。
			"nickname": nickOf[k],
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
		if main, backup, bound := Host.KeyBindingOf(k); bound {
			st["proxyMain"] = main
			st["proxyBackup"] = backup
			st["proxyStale"] = (main != "" && !Host.IsDirect(main) && exitpool.IdxInPool(main) < 0) ||
				(backup != "" && !Host.IsDirect(backup) && exitpool.IdxInPool(backup) < 0)
		}
		// 出口绑定：面板必须能看出"这个 key 为什么不在轮转里"。
		// 没有绑定的 key 在绑定策略下等于停用（见 PickZenKey）。
		// 快照已在进 zenKeyMu 之前算好（egress.go 的锁不与 zenKeyMu 嵌套）。
		if e := egress[i]; e.proxy != "" {
			st["proxy"] = kit.MaskProxyURL(e.proxy)
			st["ProxyIndex"] = e.index
		} else if k == "public" {
			st["proxy"] = "rotating (no key)"
		} else if e.bound {
			st["proxy"] = ""
			st["noEgress"] = true
		}
		// 每 key 组织 ID（x-opencode-org-id 的来源）。空 = 未解析（该头省略）。
		// 面板必须能看出来，否则"为什么这个 key 的请求不带 org 头"无从判断。
		if o := orgs[i]; o.OrgID != "" {
			st["orgID"] = o.OrgID
			st["orgSource"] = o.Source
		}
		if until, cooling := zenKeyCool[k]; cooling && now.Before(until) {
			st["cooling"] = true
			st["cooldownUntil"] = until.Format(time.RFC3339)
		} else {
			st["cooling"] = false
		}
		// 停放状态（日额度耗尽）与冷却分开表达：两者都让 key 退出轮转，
		// 但原因和持续时间差一个量级，面板混在一起会让人误判"抖动"。
		if ps, ok := zenKeyPark[k]; ok && ps.Day == now.UTC().Format("2006-01-02") {
			st["parked"] = true
			st["parkedUntil"] = ps.Until.Format(time.RFC3339)
			st["parkProbes"] = ps.Probes
			st["parkReason"] = ps.Reason
		} else {
			st["parked"] = false
		}
		out = append(out, st)
	}
	return out
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// PinnedZenKey 取 ZEN_PIN_KEY 指定的固定 key（1-based 序号，如 "2"）。
// 排障/单 key 直测用：所有 attempt 都用该 key，避免轮换污染归因。
// 未设置或越界时返回 ""（正常轮转）。
func PinnedZenKey() string {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ZEN_PIN_KEY")))
	if err != nil || n < 1 {
		return ""
	}
	keys := GetZenConfig().Keys
	if n > len(keys) {
		return ""
	}
	return keys[n-1]
}

// ============ zen 上游调用 ============

// ZenGateToolSpecs 免费层两个端点必须携带的 opencode 工具集（工具名与官方
// CLI 一致）。校验规则 2026-10-15 在新 inference 后端重新实测：缺工具 →
// 403；工具在但 parameters 为空 schema → 403；描述文本、tool_choice、UA
// 均不参与校验。注入的定义（描述+schema）统一取 zen_gate_tools.go 的官方
// CLI 2.1.4 原文（2026-10-26 认证抓包），本表只保留名字与精简描述作兜底/名录用途。
// v2 与 v1 的区别：bash/task/todowrite 被移除，新增 question/subagent/
// execute，顺序为 CLI 发送顺序。单一来源：改名/增删必须同时满足两个
// 端点的校验，只改这里。
var ZenGateToolSpecs = []struct{ name, desc string }{
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

// ZenToolChoiceAutoOnly 个别免费模型只接受 tool_choice:"auto"（2026-11-05
// 实测 crouton-alpha-free 400 拒 none/required）。恒 none 的 gate 过闸策略
// 对它们降级为 auto。命中判据用模型 ID 精确匹配（含别名解析前后的形态）。
func ZenToolChoiceAutoOnly(reqModel string) bool {
	m, ok := ResolveZenModel(reqModel)
	if !ok {
		return false
	}
	switch m.ID {
	case "crouton-alpha-free":
		return true
	}
	return false
}

// ZenGateTools chat 端点形态：tools[].{type,function:{name,description,parameters}}。
// 返回 []any 与客户端解码出来的 tools 同型（下游要拼接两者）。
// parameters 必须是官方真实 schema（空 schema 会被新后端 403，见
// zen_gate_tools.go）；嵌入定义缺失时才退回精简描述+空 schema 兜底。
func ZenGateTools() []any {
	defs := ZenGateToolDefs()
	out := make([]any, 0, len(ZenGateToolSpecs))
	if len(defs) > 0 {
		for _, d := range defs {
			out = append(out, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        d.Name,
					"description": d.Description,
					"parameters":  d.ParametersChat,
					// CLI 2.1.4 抓包：12 个 gate 工具的 function 里都带
					// "strict": false。responses 路径一直有该字段
					// （ZenMergeResponsesTools），chat 路径漏了——同一份工具集
					// 在两条路径上形态不一致，补齐。语义上 strict 缺省即 false，
					// 这里是形态对齐而非行为改变。
					"strict": false,
				},
			})
		}
		return out
	}
	for _, s := range ZenGateToolSpecs {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        s.name,
				"description": s.desc,
				"parameters":  ZenEmptySchema(),
				"strict":      false,
			},
		})
	}
	return out
}

// BuildZenBody 构造 zen 请求体:只带 OpenAI 兼容字段,改写模型为 zen ID
func BuildZenBody(params map[string]any, stream bool) map[string]any {
	body := map[string]any{}
	for _, key := range Host.PassThroughKeys {
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
		// CLI 2.1.4 在 chat 路径恒发 {"include_usage":true}（2026-10-30 抓包）。
		// 上游只有收到该字段才会在 SSE 末尾补一帧纯 usage 块——不发的话网关的
		// 流式 token 统计恒为 0（onUsage 从来收不到数据）。这一帧不带 choices，
		// 客户端的 OpenAI 解析器会忽略它，形态安全。
		//
		// 在客户端给的对象上合并而不是整体替换：客户端若传了别的
		// stream_options 子键，不该因为网关要统计就丢掉。客户端显式写
		// include_usage:false 时仍改为 true —— 网关需要该帧做统计，而下发时
		// 会按客户端的原始意愿吞掉它（见 clientWantsStreamUsage）。
		so := map[string]any{"include_usage": true}
		if cs, ok := params["stream_options"].(map[string]any); ok {
			for k, v := range cs {
				if k != "include_usage" {
					so[k] = v
				}
			}
		}
		body["stream_options"] = so
	} else {
		// 非流式路径不该带上流式专有字段（Host.PassThroughKeys 会原样搬过来）。
		delete(body, "stream_options")
	}
	if model, ok := params["model"].(string); ok {
		if m, ok := ResolveZenModel(model); ok {
			body["model"] = m.ID
			// 输出上限默认值：CLI 2.1.4 对 big-pickle 发 max_completion_tokens
			// =32000，正是目录里该模型的 limit.output（2026-10-30 抓包 + 目录
			// 核对一致）。网关此前完全不发该字段——上游用自己更大的缺省值，
			// 与真实客户端形态不同。客户端显式给了就尊重客户端的值。
			if _, has := body["max_completion_tokens"]; !has {
				if _, hasTok := body["max_tokens"]; !hasTok && m.Output > 0 {
					body["max_completion_tokens"] = m.Output
				}
			}
		}
	}
	// reasoning_effort：2026-11-11 桌面端 2.2.7 实测抓包推翻了此前"客户端
	// 不发该字段"的结论。真实行为是"按模型能力决定"：
	//   - deepseek-v4.1-flash-free（目录 capabilities.reasoning.effort =
	//     ["low","high","max"]）→ 客户端发 reasoning_effort:"high"。
	//   - big-pickle（capabilities.reasoning 只有 supported，无 effort）→
	//     客户端完全不发。
	// 也就是说 effort 由客户端的模型变体选择决定，不是网关该猜的东西。
	// 故这里改为纯透传：客户端给就原样转发（驼峰统一成下划线），不给就
	// 绝不补默认值——凭空造一个 "high" 反而会让非 reasoning 模型收到非法字段。
	if eff, ok := params["reasoning_effort"]; ok && eff != nil {
		body["reasoning_effort"] = eff
	} else if eff, ok := params["reasoningEffort"]; ok && eff != nil {
		body["reasoning_effort"] = eff
	}
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
	clientTools := ZenClientTools(params)
	// 描述 NBSP 整形（恒定开启）：免费层闸按顶层描述字面子串匹配（2026-11-21
	// 实测，见 zen_toolfilter.go 对策注释），整形必须在裁剪/合并之前。
	clientTools = zenShapeClientToolDescriptionsChat(clientTools)
	// 体积裁剪（默认关闭）：先裁客户端工具，再合并 gate 占位工具——
	// gate 工具是过闸必需，绝不参与过滤。裁剪结果只影响本次上游 body。
	if cfg, on := ZenToolFilterActive(); on && len(clientTools) > 0 {
		var st ZenFilterToolStats
		clientTools, st = ZenFilterClientToolsChat(clientTools, cfg)
		LogZenToolFilter("chat", st)
	}
	if len(clientTools) == 0 {
		body["tools"] = ZenGateTools()
	} else {
		body["tools"] = ZenMergeTools(clientTools, ZenGateTools())
	}
	// tool_choice 分流（客户端必须拿到它声明的语义）：
	//  - 客户端无工具（纯文本问答）：恒定 none，即使客户端传了 auto/required。
	//    此时 tools[] 里只有 gate 占位 stub，客户端没有执行层；让上游按
	//    auto/required 处理会让模型返回 read/shell/execute 这类客户端从未
	//    声明的调用，IDE 解析失败（required 更会强制成一次必失败的工具调用）。
	//    这是**故意**偏离官方客户端的唯一一处：官方客户端永远带着自己真实的
	//    工具集，不存在"有 gate stub 但无执行层"的状态，所以它可以直接省略。
	//  - 客户端有工具但没指定：省略该字段（上游 82abe7f）。2026-11-11 桌面端 2.2.7
	//    抓包证实官方客户端在 chat 路径不发 tool_choice（上游缺省即 auto）。
	//    此前网关补 "auto" 是多余字段——语义相同，但形态与真实客户端不同。
	//  - 客户端显式指定且有工具：原样透传。旧版 function_call 语义等价于
	//    tool_choice，转换 tools[] 后同样透传（否则老客户端的 "required"
	//    会在转换中丢失）。
	//  例外：个别免费模型只接受 "auto"（2026-11-05 实测 crouton-alpha-free：
	//  400 "only \"auto\" is supported for tool_choice"，none/required 一律
	//  拒）。这类模型降级为 auto——tools[] 里只有 gate 占位 stub，即便被
	//  调用也只是格式门无副作用，好过整个请求 400。
	if len(clientTools) == 0 {
		if reqModel, _ := params["model"].(string); ZenToolChoiceAutoOnly(reqModel) {
			body["tool_choice"] = "auto"
		} else {
			body["tool_choice"] = "none"
		}
	} else if tc, ok := params["tool_choice"]; ok {
		body["tool_choice"] = tc
	} else if tc, ok := params["function_call"]; ok {
		body["tool_choice"] = tc
	} else {
		delete(body, "tool_choice")
	}
	EnsureZenEnvMarkerChat(body)
	return body
}

// ZenEnvBlock returns the CLI's <env> marker block. 2026-10-26 ablation
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
func ZenEnvBlock() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		wd = "/tmp/opencode"
	}
	return "<env>\n  Working directory: " + wd + "\n</env>"
}

func ZenTextHasEnvMarker(s string) bool {
	return strings.Contains(s, "<env>") && strings.Contains(s, "Working directory:")
}

// ZenContentText 取出 message content 的纯文本，兼容三种客户端形态：
// 裸字符串、parts 数组 [{"type":"text","text":...}]（IDE 常见）、
// 单 part 对象。仅用于 marker 检测，不做内容改写。
func ZenContentText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, p := range c {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := pm["text"].(string); ok {
				b.WriteString(s)
				b.WriteString("\n")
			}
		}
		return b.String()
	case map[string]any:
		if s, ok := c["text"].(string); ok {
			return s
		}
	}
	return ""
}

// EnsureZenEnvMarkerChat appends the env block to the first system
// message (or prepends a system message with just the block) when no
// system/developer message already carries the marker. It never
// removes or rewrites client content; it only appends the marker the
// free tier checks for, at the end, mirroring CLI's layout.
//
// 两点必须成立，否则会伤到客户端：
//  1. marker 检测必须看得懂 parts 数组形态。只认 string 时，已带
//     marker 的数组 system 会被判为"没有"，于是又 prepend 一个
//     system 消息，产生两条相邻 system。
//  2. 不得就地改写调用方的 messages。body["messages"] 与
//     params["messages"] 同底层切片（BuildZenBody 只做浅拷贝），
//     就地改 map 会把 marker 写进客户端参数，端点自适应重试、
//     日志、token 估算随后读到的都是被污染的版本。
func EnsureZenEnvMarkerChat(body map[string]any) {
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
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
		if ZenTextHasEnvMarker(ZenContentText(mm["content"])) {
			return
		}
	}
	// 先复制消息 map（以及 parts 切片），再在副本上加 marker。
	out := make([]any, len(msgs))
	for i, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			out[i] = m
			continue
		}
		cp := make(map[string]any, len(mm)+1)
		for k, v := range mm {
			cp[k] = v
		}
		if parts, ok := cp["content"].([]any); ok {
			nc := make([]any, len(parts), len(parts)+1)
			copy(nc, parts)
			cp["content"] = nc
		}
		out[i] = cp
	}
	for _, m := range out {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		switch c := mm["content"].(type) {
		case string:
			mm["content"] = c + "\n\n" + ZenEnvBlock()
		case []any:
			// 数组形态：追加一个 text part，保持客户端原有结构。
			mm["content"] = append(c, map[string]any{"type": "text", "text": ZenEnvBlock()})
		default:
			mm["content"] = ZenEnvBlock()
		}
		body["messages"] = out
		return
	}
	body["messages"] = append([]any{map[string]any{"role": "system", "content": ZenEnvBlock()}}, out...)
}

func EnsureZenEnvMarkerResponses(body map[string]any) {
	if s, ok := body["instructions"].(string); ok && ZenTextHasEnvMarker(s) {
		return
	}
	if s, ok := body["instructions"].(string); ok && s != "" {
		body["instructions"] = s + "\n\n" + ZenEnvBlock()
		return
	}
	body["instructions"] = ZenEnvBlock()
}

// ZenClientTools 提取客户端工具（chat 嵌套形态 tools[]，兼容已废弃的
// functions[] 形态），返回可直接发给 chat 端点的嵌套定义。
func ZenClientTools(params map[string]any) []any {
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
				fn["parameters"] = ZenEmptySchema() // null schema 上游判非法
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
				fn["parameters"] = ZenEmptySchema()
			}
			out = append(out, map[string]any{"type": "function", "function": fn})
		}
	}
	return out
}

// ZenMergeTools 合并客户端工具与 gate 占位工具：客户端定义优先，同名 gate
// stub 跳过。gate 只校验工具名是否存在，带真实 schema 的客户端定义同样过闸，
// 而 stub 抢先会让模型拿到空参数结构（如客户端 read 的 file_path 丢失）。
func ZenMergeTools(clientTools []any, gateTools []any) []any {
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

func ZenEmptySchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// ============ zen 上游调用：原生 /v1/responses 端点 ============
//
// 背景: 部分 zen 免费模型（如 muse-spark）在 chat/completions 端点上 500，
// 官方 opencode CLI 对它们只发原生 POST /zen/v1/responses（Responses 形态
// 请求体 + SSE 事件流响应）。网关对这类模型走同样路径：请求体按 Responses
// 形态构造，响应再转回 chat completions 形态，上下游都不感知差异。

// BuildZenResponsesBody 把 chat 请求参数映射为 Responses 形态请求体：
// messages -> input（system/developer 指令并入 instructions），
// max_tokens/max_completion_tokens -> max_output_tokens。
// 字段经官方 CLI 实际请求逐字段核对：reasoning.summary=auto、
// include=[reasoning.encrypted_content]、tool_choice 缺省 auto、
// prompt_cache_key=会话 ID 均为 CLI 常发字段，缺省会与原生形态不一致。
// promptKey 为本次上游会话 ID（调用方传入本次请求的 sess_ 值）。
// 无 stream 形参：上游恒为 stream=true（见函数内注释）。
func BuildZenResponsesBody(params map[string]any, modelID string, promptKey string) map[string]any {
	body := map[string]any{
		"model":  modelID,
		"stream": true, // FreeTier gate 硬要求：responses 端点只接受 stream=true，
		// stream=false（即使显式传）上游按"非 CLI"请求 403。网关非流式客户端
		// 由调用方聚合（ResponsesSSEToChat），body 恒发 true。
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
			content := ResponsesContentToInput(mm["content"])
			if role == "system" || role == "developer" {
				// system/developer 指令提升为顶层 instructions（Responses 标准字段；
				// 官方 CLI 发 role=developer 条目且无顶层 instructions——网关把
				// 历史 system/developer 内容并入 instructions 保持等价）
				// 用 ZenContentText 取文本：IDE 常发 parts 数组形态，只认
				// string 会把客户端的系统提示词整段静默丢掉。
				if s := ZenContentText(content); s != "" {
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
							args = Host.RepairToolArguments(args)
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
	if mt := Host.AsInt(params["max_tokens"]); mt > 0 {
		body["max_output_tokens"] = mt
	} else if mt := Host.AsInt(params["max_completion_tokens"]); mt > 0 {
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
	flatTools := ZenClientFlatTools(params)
	// 描述 NBSP 整形（恒定开启）：同 chat 路径，免费层闸字面措辞对策。
	flatTools = zenShapeClientToolDescriptionsFlat(flatTools)
	// 体积裁剪（默认关闭）：同 chat 路径，只裁客户端工具，gate stub 不动。
	if cfg, on := ZenToolFilterActive(); on && len(flatTools) > 0 {
		var st ZenFilterToolStats
		flatTools, st = ZenFilterClientToolsFlat(flatTools, cfg)
		LogZenToolFilter("responses", st)
	}
	body["tools"] = ZenMergeResponsesTools(flatTools)
	body["tool_choice"] = "auto"
	EnsureZenEnvMarkerResponses(body)
	return body
}

// SetZenGateHeaders 写入 zen 免费门要求的客户端身份头，两条上游路径（chat
// /responses）共用——此前两处逐行重复，改一处漏一处就会让某条路径掉头。
// 直接写 map（而非 Header.Set）以保留小写头名：官方 CLI 发的就是小写，
// Set 会规范化为 X-Opencode-...；HTTP 头大小写不敏感，但字节级对齐更安全。
// Authorization/Content-Type/User-Agent 因需承载变量，由调用方先行设置。
// 2026-10-26 实测（CLI v2.1.4 认证抓包）：
//   - CLI v2 在两条路径都发 x-opencode-session-id, x-opencode-session,
//     x-session-affinity, x-session-id，四个头同值（新鲜会话时四者
//     相等；父会话/分支会话时 q 规则见静态 RE 报告，网关粘性会话
//     属于新鲜会话情形，故四者同值正确）。
//   - x-opencode-request：v2 CLI 完全不发（静态计数 0，抓包亦无）。旧网关仍发该头
//     是 v1 遗留指纹，必须去掉；requestID 参数保留仅为调用点兼容，不再写入任何头。
//   - x-opencode-org-id：**按 key 解析**，绝不能是全局常量。2026-11-11 实测确认
//     每个 zen key 绑定各自的 org（wrk_/org_ 前缀），跨 key 套用同一值会让上游
//     看到"key 属于 A org、请求声称 B org"的矛盾形态。解析逻辑见 zen_key_org.go
//     （按 key 向 console/api/orgs 发现 + 负缓存）；发现不到则省略该头，并在
//     管理员显式配置 cfg.OrgID 时回退到该值。绝不编造。
//   - x-opencode-project：网关没有客户端项目目录，global 是唯一诚实值。
//
// key 是本次请求实际使用的那把 key（轮转/故障转移后可能不是 Keys[0]）——
// 按它解析该 key 自己的 org，才能保证"头里的 org 就是这把 key 的 org"。
// key 为空（匿名路径）时不发该头，除非管理员显式配置了 cfg.OrgID 兜底。
func SetZenGateHeaders(req *http.Request, sess, requestID, key string) {
	_ = requestID // v2 CLI does not send x-opencode-request; do not emit it
	req.Header["x-opencode-session-id"] = []string{sess}
	req.Header["x-opencode-session"] = []string{sess}
	req.Header["x-session-affinity"] = []string{sess}
	req.Header["x-session-id"] = []string{sess}
	req.Header["x-opencode-client"] = []string{"cli"}
	req.Header["x-opencode-project"] = []string{"global"}
	if key != "" {
		if org, _ := ZenKeyOrgForRequest(key); org != "" {
			req.Header["x-opencode-org-id"] = []string{org}
		}
	} else if org := strings.TrimSpace(GetZenConfig().OrgID); org != "" {
		// 无 key（匿名路径）：只有显式配置的值可用。
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
	// Connection: keep-alive。2026-11-11 Android 客户端 2.2.7 抓包（匿名真实
	// inference 请求）确认官方客户端显式发这个头；Go 的 Transport 在正常
	// keep-alive 时**省略**它（HTTP/1.1 默认即持久连接）。语义完全等价，纯粹
	// 是头集合形态对齐。
	req.Header.Set("Connection", "keep-alive")
	// CLI v2 tracing headers, same trace/span across b3 and traceparent.
	//
	// b3 是 4 段（traceid-spanid-sampled-parentSpanId）。2026-11-11 桌面端
	// 2.2.7 抓包逐字确认：真实客户端发的是
	//   b3: <32hex>-<16hex>-1-<16hex>
	// 而网关此前只发 3 段（省略 parentSpanId）。B3 规范里 parentSpanId 是
	// 可选的，但官方客户端总是带上——网关是整条链路的最上游，没有真实父
	// span，故随机生成一个：字段形态正确，且不虚构任何真实调用关系。
	//
	// 头名用小写：2026-11-11 Android 客户端 2.2.7 抓包（未登录、真实
	// inference 请求）确认官方客户端发的是小写 `b3` / `traceparent`。Header.Set
	// 会把名字规范化成 `B3` / `Traceparent`，字节层面与真实客户端不同。HTTP
	// 头本就不区分大小写，但本函数其余身份头（x-opencode-*）都刻意用 map 直写
	// 保持小写，这里若用 Set 就自相矛盾了。
	traceID := kit.RandHex(16)
	spanID := kit.RandHex(8)
	parentSpanID := kit.RandHex(8)
	req.Header["b3"] = []string{traceID + "-" + spanID + "-1-" + parentSpanID}
	req.Header["traceparent"] = []string{"00-" + traceID + "-" + spanID + "-01"}
}

// ZenClientFlatTools 提取客户端工具并转成 responses 端点要求的 flat 形态。
// 同时接受 chat 嵌套形态 {type,function:{...}} 与 responses 原生 flat 形态
// {type,name,description,parameters,strict}。
func ZenClientFlatTools(params map[string]any) []map[string]any {
	var out []map[string]any
	add := func(name, desc string, p any) {
		if name == "" {
			return
		}
		if p == nil {
			p = ZenEmptySchema()
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

// ZenMergeResponsesTools 客户端 flat 工具优先，缺名补 gate stub 过闸。
func ZenMergeResponsesTools(clientTools []map[string]any) []any {
	merged := make([]any, 0, len(clientTools)+len(ZenGateToolSpecs))
	provided := map[string]bool{}
	for _, t := range clientTools {
		n, _ := t["name"].(string)
		if n == "" || provided[n] {
			continue
		}
		provided[n] = true
		merged = append(merged, t)
	}
	for _, s := range ZenGateToolSpecs {
		if provided[s.name] {
			continue
		}
		provided[s.name] = true
		// parameters 必须用 responses 形态的官方真实 schema：空 schema
		// 会被新后端 403（2026-10-15 实测，见 zen_gate_tools.go）。
		desc, params := s.desc, ZenEmptySchema()
		if d := LookupZenGateToolDef(s.name); d != nil {
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

// ResponsesContentToInput chat content -> Responses input content。
// 字符串原样；parts 数组只保留文本/图片两种 Responses 原生类型。
func ResponsesContentToInput(content any) any {
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

// ResponsesSSEToChat 把原生 responses SSE 事件流聚合成 chat completions 形态。
// 处理两种事件风格（上游按次返回其一）：
//   - delta 流：response.output_text.delta / function_call_arguments.delta /
//     reasoning_summary_text.delta（逐块增量文本）
//   - output_item.done：response.output_item.done 内 item.content[].text 全量
//     文本（muse-spark 对纯文本问答只发该事件，无 delta 流）
//
// usage 从 response.completed 或 response.incomplete 提取；
// incomplete_details.reason 映射为 finish 原因（length→length）。
// ZenSSECall 单个 function_call 输出项的流式累积器。上游可能并行/连续输出
// 多个工具调用，必须按 item 分开累积参数，否则两个调用的 arguments delta
// 会串成一段坏 JSON（上游实测：webfetch 双调用 jam 成 {"query":...}{"url":...}）。
type ZenSSECall struct {
	id, name, callID string
	idx              int // response.output_index：无 id/call_id 时的绑定键（缺失为 -1）
	args             strings.Builder
	final            string // output_item.done 一次性给全参（无 delta 事件时）
}

// FindZenSSECall 定位事件所属的累积器：id/call_id 精确匹配 → output_index
// 匹配。两者都对不上时返回 nil，由调用方按场景决定要不要认领无主累积器 ——
// 绝不"回退到最后一个调用"：并行调用时那会把 A 的参数 delta 追加到 B 上，
// 拼出 {"query":...}{"url":...} 这种坏 JSON（上游实测的 jam 正是如此）。
func FindZenSSECall(calls []*ZenSSECall, id, callID string, outIdx int) *ZenSSECall {
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

// ZenOrphanCall 返回第一个"无标识但已有内容"的累积器：delta/done 先于
// added 到达时内容落在无主累积器里，随后的 added/done 按它归位。
func ZenOrphanCall(calls []*ZenSSECall) *ZenSSECall {
	for _, c := range calls {
		if c.id == "" && c.callID == "" && (c.args.Len() > 0 || c.final != "") {
			return c
		}
	}
	return nil
}

// ZenSSEOutIdx 取事件里的 output_index，缺失返回 -1。
func ZenSSEOutIdx(ev map[string]any) int {
	if v, ok := ev["output_index"].(float64); ok {
		return int(v)
	}
	return -1
}

func ResponsesSSEToChat(resp *http.Response) (map[string]any, error) {
	defer resp.Body.Close()
	var text, reasoning strings.Builder
	var usage map[string]any
	var incompleteReason string
	var failedMsg string
	failed := false
	textDeltas := false
	sawData := false
	var rawHead strings.Builder // 非 SSE 响应体（错误 JSON）的采样，用于报错
	calls := []*ZenSSECall{}
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
								c := FindZenSSECall(calls, iid, "", ZenSSEOutIdx(ev))
								if c == nil {
									switch {
									case len(calls) == 0:
										// 首个事件就是 delta：先建无主累积器，
										// 用 item_id 当身份，后续 added/done 才找得回来
										c = &ZenSSECall{idx: ZenSSEOutIdx(ev), id: iid}
										calls = append(calls, c)
									case len(calls) == 1 && calls[0].id == "" && calls[0].callID == "":
										// 唯一且无主的累积器不可能是别人的
										c = calls[0]
									default:
										// 归属不明的 delta：宁可丢弃（done 事件会带全量
										// 参数），也不能把 A 的碎片拼进 B 的参数里
										c = ZenOrphanCall(calls)
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
								c := FindZenSSECall(calls, iid, "", ZenSSEOutIdx(ev))
								if c == nil {
									c = ZenOrphanCall(calls)
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
									c := FindZenSSECall(calls, id, cid, ZenSSEOutIdx(ev))
									if c == nil {
										c = ZenOrphanCall(calls)
									}
									if c == nil {
										c = &ZenSSECall{idx: ZenSSEOutIdx(ev)}
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
									target := FindZenSSECall(calls, id, cid, ZenSSEOutIdx(ev))
									if target == nil {
										target = ZenOrphanCall(calls)
									}
									if target == nil {
										// 无标识可归属时只能收回"最后一个完全空的壳"；
										// 否则新建（不能覆盖已有调用的 id/name/参数）
										if n := len(calls); n > 0 && calls[n-1].id == "" &&
											calls[n-1].callID == "" && calls[n-1].name == "" &&
											calls[n-1].args.Len() == 0 && calls[n-1].final == "" {
											target = calls[n-1]
										} else {
											target = &ZenSSECall{idx: ZenSSEOutIdx(ev)}
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
					"arguments": Host.RepairToolArguments(raw),
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

// CallZenResponsesAPI 原生 /v1/responses 端点调用：与 CallZenAPI 相同的
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
func CallZenResponsesAPI(ctx context.Context, params map[string]any, stream bool, opts ...ZenCallOpts) (*http.Response, int, error) {
	var o ZenCallOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	cfg := GetZenConfig()
	model, _ := params["model"].(string)
	zm, ok := ResolveZenModel(model)
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
	// 指纹闸自愈（同 CallZenAPI，见该处注释）：barely-used key 的"日额度
	// 耗尽"形态 429 → 同 key 去客户端工具重试一次。
	stripTools := false
	toolRetryDone := false
	// keySwitches 限制"一次客户端请求最多冷却几个 key"（同 chat 路径，
	// 见 CallZenAPI 里的说明：换 key 会不可逆地冷却 key，必须单独设上限）。
	keySwitches := 0
	maxKeySwitches := retries
	if maxKeySwitches > Max429KeySwitches {
		maxKeySwitches = Max429KeySwitches
	}
	// sleeps 是"等一会儿再用同一个 key"的预算，与 keySwitches 分开计。
	// 早先两者共用 attempt：换 key 的 continue 也会 ++attempt，于是 key 换得
	// 多的请求会把睡眠重试预算吃光——网络抖动 + 限流同时出现时反而一次都不重试，
	// 而"更该重试"的正是这种场景。
	sleeps := 0
	// 2026-10-27: fresh session per API call (ZenCallIdentity). Same
	// session across retries of this call, fresh if the key switches
	// or a 403 forces a refresh. See zen_session.go.
	callIdent := NewZenCallIdentity()

	for attempt := 0; attempt < MaxZenAttempts; attempt++ {
		// 全池停放（日额度耗尽）：与 chat 路径一致，不发上游往返，
		// 直接给出带恢复时刻的错误。
		if attempt == 0 && o.PinKey == "" && PinnedZenKey() == "" {
			if until, ok := ZenAllParkedUntil(); ok {
				log.Printf("  zen responses daily quota exhausted on every key: next probe %v, rejecting without upstream call",
					until.Format(time.RFC3339))
				return nil, 1, &ZenHTTPError{
					Status:      http.StatusTooManyRequests,
					RateLimited: true,
					Body: fmt.Sprintf("free daily quota exhausted on all %d key(s); next automatic probe at %s UTC",
						len(cfg.Keys), until.UTC().Format("2006-01-02 15:04 MST")),
				}
			}
		}
		// 先选 key：attempt==0 或尚无粘性 key 时轮转；重试链内沿用
		// retryKey（会话绑定要求 key 与 sess_ 一致；换 key 由下方
		// 限流/403 分支显式改写 retryKey 后 continue 实现）。
		// ZEN_PIN_KEY=n 时固定用第 n 个 key（排障直测），默认轮转。
		// pinKey（面板 Test）：整条调用链固定该 key，优先级最高——探测的
		// 结论只对被探测的 key 成立，中途换 key 会把别的 key 的成败算到它头上。
		var key string
		if o.PinKey != "" {
			key = o.PinKey
			retryKey = o.PinKey
		} else if pk := PinnedZenKey(); pk != "" {
			key = pk
			retryKey = pk
		} else if retryKey == "" {
			retryKey = PickZenKey()
		}
		key = retryKey
		if key == "" {
			return nil, rateLimited, ZenNoKeyError()
		}
		// 出口由 key 决定：隔离模式下同一 key 永远从绑定 IP 出现（主→备）；
		// 绑定出口全不可用时短冷却该 key 并清空 retryKey 换下一个 —— 绝不
		// 退回其他出口（那会让粘性会话的 IP 漂移）。
		proxyURL, pidx, exitOK := Host.BoundExitFor(key)
		if !exitOK {
			if o.PinKey != "" || PinnedZenKey() != "" {
				return nil, rateLimited, ZenBlockedExitErr(key)
			}
			CooldownZenKey(key, 2*time.Minute)
			retryKey = ""
			log.Printf("  zen responses key#%d bound proxies unavailable, skipping key for 2m", KeyIndex(key))
			continue
		}
		viaProxy := "direct"
		if proxyURL != "" {
			viaProxy = kit.MaskProxyURL(proxyURL)
		}
		// 每次调用新铸会话（2026-10-27，a99d94b）：同一调用的重试沿用同一
		// 会话，新调用/换 key 重新铸；msg_ 请求 ID 仍每次随机。
		// （key 为空已在上方报错返回。）
		sess, user, ua := callIdent.forKey(key)
		body := BuildZenResponsesBody(params, zm.ID, sess)
		// 工具改名（同 CallZenAPI；responses 形态：tools[].name + input 的
		// function_call 项）。
		if m := toolrename.Build(toolrename.KindZen, zenRenameCandidateNames(params)); m != nil {
			toolrename.RewriteResponsesForm(body, m)
		}
		if stripTools {
			// 指纹闸自愈：只留 gate stub（tool_choice 已恒 auto）。
			body["tools"] = ZenMergeResponsesTools(nil)
		}
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
		SetZenGateHeaders(req, sess, user, key)
		log.Printf("  zen upstream: model=%s responses stream=%v via=%s key=#%d attempt=%d session=%s",
			zm.ID, stream, viaProxy, KeyIndex(key), attempt+1, kit.Truncate(sess, 24))
		Host.LogUpstreamInfo(ctx, fmt.Sprintf("key#%d", KeyIndex(key)))

		// hsStart 拨号前打点（与 cline 路径同口径）：Do 返回即隧道建立（首字节未回），
		// 差值=TCP+TLS+CONNECT 握手耗时。此前恒传 0，导致 zen/opencode 出口的
		// 「平均握手延迟」徽标永远无样本（面板显示 -），只有在线率在动。
		hsStart := time.Now()
		// 流式请求的响应头看门狗（与 cline 同款，复用 clineHeaderWatchdogFor）：
		// 黑洞出口（TCP 通、响应头被静默丢弃）会把请求悬到共用 transport 的
		// ResponseHeaderTimeout（5 分钟）甚至更久（2026-11-05 生产实证 632 秒），
		// 期间占着并发信号量且换 key 重试链不触发。只包流式——非流式的响应头
		// 要等整段生成完成，看门狗语义不成立。
		doCtx, stopWatch := ctx, func() bool { return false }
		if stream {
			doCtx, stopWatch = Host.ClineHeaderWatchdogFor(ctx, ZenHeaderTimeout())
		}
		resp, err := exitpool.ClientFor(proxyURL).Do(req.WithContext(doCtx))
		if stopWatch() {
			log.Printf("  zen upstream: response header watchdog fired after %v (exit unresponsive, retrying)", ZenHeaderTimeout())
			err = fmt.Errorf("zen response header watchdog fired after %v", ZenHeaderTimeout())
		}
		if err == nil {
			mainP, backupP, boundP := Host.KeyBindingOf(key)
			Host.LogUpstreamExitType(ctx, exitpool.ProxyExitType(proxyURL, mainP, backupP, boundP))
			exitpool.RecordProxyOutcome(exitpool.KindZen, proxyURL, true, nil, time.Since(hsStart).Milliseconds())
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, rateLimited, fmt.Errorf("client aborted: %w", err)
			}
			if pidx >= 0 {
				// shouldCooldownExit 纯查询（不写计数），真正的 strike 记账与冷却
				// 在下面的 recordProxyOutcome 单点完成。建连失败与隧道后流错误
				// 都是 60s 窗口 2-strike（2026-10-31）。
				if scope, cool := exitpool.ShouldCooldownExit(exitpool.KindZen, proxyURL, err); cool {
					if scope == "global" {
						exitpool.CooldownUpstreamProxy(pidx, 2*time.Minute)
					} else {
						exitpool.CooldownUpstreamProxyFor(exitpool.KindZen, pidx, exitpool.PlatformCooldownConst)
					}
					log.Printf("  zen proxy failed (%v), cooldown exit %s %s for 2m", err, scope, viaProxy)
				} else {
					log.Printf("  zen upstream-side stream error (%v) on exit %s — not cooling the exit", err, viaProxy)
				}
				exitpool.RecordProxyOutcome(exitpool.KindZen, proxyURL, false, err, 0)
			}
			if sleeps < retries {
				log.Printf("  zen responses network error (%v), retry %d/%d after %v", err, sleeps+1, retries, delay)
				if !SleepCtx(ctx, kit.WithRetryJitter(delay)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = ZenRetryDelay(delay)
				sleeps++
				continue
			}
			return nil, rateLimited, fmt.Errorf("zen responses request: %w", err)
		}
		// 显式 Accept-Encoding 关闭了 net/http 的自动解压，必须自己解。
		if derr := kit.DecodeResponse(resp); derr != nil {
			resp.Body.Close()
			return nil, rateLimited, fmt.Errorf("zen responses decode: %w", derr)
		}
		if resp.StatusCode == http.StatusOK {
			ZenSessionMarkSuccess(key)
			if stripTools {
				log.Printf("  zen fingerprint gate bypassed: request served without client tools (key#%d) — client tool capability lost for this round", KeyIndex(key))
			}
			if stream {
				resp.Header.Set("X-Afree-Used-Key", key)
				return resp, rateLimited, nil
			}
			// 非流式：内部聚合为 chat completions 形态后返回
			chat, aerr := ResponsesSSEToChat(resp)
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
		apiErr := &ZenHTTPError{Status: resp.StatusCode, Body: kit.Truncate(bodyBytes, 500)}

		if IsRateLimited(resp.StatusCode, bodyBytes) {
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
			raDur := kit.ParseRetryAfter(rawRetry)
			// 日额度耗尽 != 瞬时节流：前者今天不会再好，逐请求重试只是把
			// IDE 的重试风暴放大成对上游的持续打点（还会让每个失败都记一次
			// rateLimited，统计失真）。判定后改为"停放该 key"，停放期内
			// PickZenKey 直接跳过，到期只放一个探针。
			if ZenDailyExhausted(resp.StatusCode, bodyBytes, raDur) {
				// 指纹闸自愈（同 CallZenAPI 分支）：barely-used key 的伪额度
				// 429 → 同 key 去客户端工具重试一次，仍耗尽才停放。
				if used := zenKeyTokensToday(key); !toolRetryDone && o.PinKey == "" && PinnedZenKey() == "" && used <= zenFingerprintSuspectTokens {
					toolRetryDone = true
					stripTools = true
					log.Printf("  zen responses daily-quota-shaped 429 on barely-used key#%d (%d tok today): suspect fingerprint gate, retrying same key without client tools (dropped: %s)",
						KeyIndex(key), used, kit.Truncate(zenClientToolNames(params), 260))
					continue
				}
				parked := ParkZenKey(key, bodyBytes)
				log.Printf("  zen responses daily quota exhausted (429) key#%d: parked %v (probe #%d), Retry-After=%q",
					KeyIndex(key), parked, ZenParkProbeCount(key), rawRetry)
				// 停放后立即尝试换到别的 key：多 key 池里这一条就把
				// "一个 key 用完"变成"池子继续可用"。
				if o.PinKey == "" && keySwitches < maxKeySwitches {
					if next := PickZenKey(); next != "" && next != key && !ZenKeyCooling(next) {
						log.Printf("  zen responses daily quota exhausted: switching to next zen key (switch %d/%d)",
							keySwitches+1, maxKeySwitches)
						key = next
						retryKey = next
						keySwitches++
						continue
					}
				}
				return nil, rateLimited, apiErr
			}
			applied := CooldownZenKey(key, raDur)
			log.Printf("  zen rate limited (%d) key#%d: Retry-After=%q -> cooldown %v",
				resp.StatusCode, KeyIndex(key), rawRetry, applied)
			// pinKey（面板 Test）：探测结论必须是被探测 key 自己的——立即原样
			// 上报 429（冷却已在上一行生效），不换 key 重试、不吃重试睡眠。
			if o.PinKey != "" {
				return nil, rateLimited, apiErr
			}
			if keySwitches < maxKeySwitches {
				if next := PickZenKey(); next != "" && next != key && !ZenKeyCooling(next) {
					// 必须同步改写 retryKey：循环头 key=retryKey，只改 key 不改
					// retryKey 会让下一次迭代继续用刚冷却的旧 key，无限 429 空转。
					log.Printf("  zen responses rate limited (%d), switching to next zen key (%d configured, switch %d/%d)",
						resp.StatusCode, len(cfg.Keys), keySwitches+1, maxKeySwitches)
					key = next
					retryKey = next
					keySwitches++
					continue
				}
			} else if len(cfg.Keys) > keySwitches+1 {
				log.Printf("  zen responses rate limited (%d): key-switch budget exhausted (%d/%d) with %d key(s) configured, not cooling the rest",
					resp.StatusCode, keySwitches, maxKeySwitches, len(cfg.Keys))
			}
			// 无 key 可换时快速失败，语义与 chat 路径一致（见那里的长注释）：
			// 睡 30s 后必然重撞同一个已冷却的 key，只是把客户端连接挂住。
			// 同 chat 路径：429 不睡眠重试（睡完只会重撞同一个刚冷却的 key）。
			log.Printf("  zen responses rate limited (%d): failing fast (key switching already attempted)",
				resp.StatusCode)
			// 限流不是"zen 挂了"：上游明确回了 HTTP 429，服务正常，只是这个
			// key/身份当前超了窗口。把它计入故障转移会让一次瞬时节流把整个 zen
			// 路由切走（cline 池为空时客户端直接吃 401），而限流本来已由 key
			// 冷却 + 换 key + 重试处理。故此处不推进 failover。
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
			if o.PinKey != "" {
				return nil, rateLimited, apiErr
			}
			// 持续性故障闸门（与 chat 路径同款，见 ZenSessionRefreshedRecently）。
			if ZenSessionRefreshedRecently(key, time.Now()) {
				ZenSessionFailed(key)
				applied := CooldownZenKey(key, zenForbiddenCooldown)
				log.Printf("  zen responses session rejected (403) again within %v of a local refresh [%s] key#%d via=%s: key/exit suspect, cooled %v (no re-mint)",
					zenForbiddenEscalateWindow, ZenSessionDesc(key), KeyIndex(key), viaProxy, applied)
				return nil, rateLimited, apiErr
			}
			if !sessionRetried {
				sessionRetried = true
				log.Printf("  zen responses session rejected (403) [%s] key#%d: minting fresh session for this call, retry same key",
					kit.Truncate(sess, 24), KeyIndex(key))
				callIdent.refresh(key)
				continue
			}
			// 换新后仍 403：该 key 不可用，冷却 5 分钟后快速失败。
			ZenSessionFailed(key)
			applied := CooldownZenKey(key, 5*time.Minute)
			log.Printf("  zen responses session rejected twice (403) key#%d: key cooled %v, failing fast (no cross-key fan-out)",
				KeyIndex(key), applied)
			return nil, rateLimited, apiErr
		}
		// 弃用迁移（410 + replacement / 400 unavailable）：非 pin 探测才迁移——
		// 探测的结论只属于这次点击，不得改写共享状态。
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusBadRequest {
			if migrated := ZenHandleDeprecatedModel(resp.StatusCode, bodyBytes, zm.ID, params, o.PinKey != "", remapped); migrated {
				remapped = true
				// params["model"] 已改写为继任模型；重建 body 后就地重试。
				body = BuildZenResponsesBody(params, params["model"].(string), sess)
				// 与 chat 路径 410 重建同款（CallZenAPI 尾部）：重放出站改名、
				// 保全指纹 strip——否则重建会把客户端工具真名带回出站，改名在
				// 迁移请求上静默失效、strip 后工具回潮（2026-11-27 审计 S4）。
				if m := toolrename.Build(toolrename.KindZen, zenRenameCandidateNames(params)); m != nil {
					toolrename.RewriteResponsesForm(body, m)
				}
				if stripTools {
					// 指纹自愈的 strip 必须跨 410 重建存活。
					body["tools"] = ZenMergeResponsesTools(nil)
				}
				bodyJSON, err = json.Marshal(body)
				if err != nil {
					return nil, rateLimited, fmt.Errorf("marshal zen responses body: %w", err)
				}
				zm, _ = ResolveZenModel(params["model"].(string))
				if zm == nil {
					// 极窄竞态（继任模型刚被目录修剪）：退回原错误，不裸解引用。
					return nil, rateLimited, apiErr
				}
				continue
			}
		}
		return nil, rateLimited, apiErr
	}
	// 循环上限兜底（MaxZenAttempts）：正常路径全部 return/continue 有界，
	// 走到这里说明计数器出了偏差，宁可失败也不要空转。
	return nil, rateLimited, fmt.Errorf("zen responses: retry budget exhausted")
}

// CallZenAPI 调用 zen 上游,带限流防御: 并发信号量 + 指数退避重试 + 代理冷却 + 故障计数
// 返回 (响应, 命中限流次数, 错误)。
// ctx 来自客户端请求: 客户端取消（IDE abort）时立即终止上游调用,不重试、
// 不计数、不冷却任何 key/代理 —— 客户端行为不会污染限流状态。
func CallZenAPI(ctx context.Context, params map[string]any, stream bool, opts ...ZenCallOpts) (*http.Response, int, error) {
	var o ZenCallOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	cfg := GetZenConfig()
	body := BuildZenBody(params, stream)
	// 工具改名（2026-11-21，免费层身份指纹闸名称面对策）：在 BuildZenBody
	// 之后应用——toolfilter 的真名匹配不受影响。gate 工具名不在映射内原样
	// 保留（过闸必需）；历史 tool_calls 一并改写（指纹面在历史里同样成立）。
	// 平台勾选/开关在 toolrename.Build 内判定（gwconfig 全局配置）。
	if m := toolrename.Build(toolrename.KindZen, zenRenameCandidateNames(params)); m != nil {
		toolrename.RewriteChatForm(body, m)
	}

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
	// keySwitches 限制"一次客户端请求最多冷却几个 key"。
	//
	// 2026-10-30 实测事故：一个 291 条消息的大请求 429 后，下面的换 key 分支
	// 用 `continue` 绕过了重试判断，循环头又没有上界——结果 4 秒内把池里 5 个
	// key 全部冷却到 24h（maxCooldown）。一次请求的规模问题因此被放大成
	// "整池被封"的假象。
	//
	// 语义区分：睡眠重试（sleeps < retries）是"等一等再用同一个 key"，成本低；
	// 换 key 会永久冷却一个 key，成本高且不可逆。故单独计数、单独设上限，
	// 不占用重试预算：keySwitches 上限 = retries（默认 3），即一次请求最多
	// 触碰 1 + 3 = 4 个 key，最多冷却 4 个。两者必须是两个计数器——
	// 早先共用一个 attempt 时，换 key 的 continue 也会 ++attempt，
	// 换得多就把睡眠预算吃光（网络抖动 + 限流同时出现时一次都不重试）。
	keySwitches := 0
	maxKeySwitches := retries
	if maxKeySwitches > Max429KeySwitches {
		maxKeySwitches = Max429KeySwitches
	}
	// sleeps 与 keySwitches 分开计（见 CallZenResponsesAPI 同注）。
	sleeps := 0
	// 2026-10-27: fresh session per API call (ZenCallIdentity). Same
	// session across retries of this call, fresh if the key switches
	// or a 403 forces a refresh. See zen_session.go.
	callIdent := NewZenCallIdentity()
	// retryKey pins the key for the duration of this call. Previously
	// this was an emergent property of the sticky session: PickZenKey
	// prefers keys with a live session, so a 403 retry naturally stayed
	// on the same key. With fresh sessions there is no live session to
	// prefer, so without this pin a 403 retry would round-robin to the
	// next key and fan a single session failure out across the pool.
	// Rate-limit switching (below) is the only branch that rewrites it,
	// mirroring CallZenResponsesAPI.
	retryKey := ""
	// 指纹闸自愈（2026-11-21，见 daily-exhausted 分支与 zen_toolfilter.go
	// 机制注释）：当天几乎没用的 key 吃到"日额度耗尽"形态 429，大概率是
	// 免费层身份指纹闸命中（按请求内容判定，换 key 无用——实测一次请求
	// 沿 failover 链烧 4 把 key）。stripTools=true 时出站 body 去掉客户端
	// 工具重试一次（至多一次，toolRetryDone 防重入）；成功即自愈（本轮
	// 无工具能力），仍耗尽则按真额度停放走原链路。
	stripTools := false
	toolRetryDone := false

	for attempt := 0; attempt < MaxZenAttempts; attempt++ {
		// 全池停放（日额度耗尽）：连一个上游往返都不发，直接给出带恢复
		// 时刻的明确错误。这是停放的真正收益——IDE 的重试风暴不会再
		// 逐个变成上游请求，用户看到的是"X 点恢复"而不是一串 429。
		// 面板 Test（pinKey）不受此限：管理员要能主动探一次。
		if attempt == 0 && o.PinKey == "" && PinnedZenKey() == "" {
			if until, ok := ZenAllParkedUntil(); ok {
				log.Printf("  zen daily quota exhausted on every key: next probe %v, rejecting without upstream call",
					until.Format(time.RFC3339))
				return nil, 1, &ZenHTTPError{
					Status:      http.StatusTooManyRequests,
					RateLimited: true,
					Body: fmt.Sprintf("free daily quota exhausted on all %d key(s); next automatic probe at %s UTC",
						len(cfg.Keys), until.UTC().Format("2006-01-02 15:04 MST")),
				}
			}
		}
		// 先选 key 再解析出口：隔离模式要求出口由 key 的绑定决定（同一 key
		// 永远从同一 IP 出现）；未绑定 key 在 egress 钉定生效时用自动分配的
		// 固定出口，否则走池轮转（未配置代理时直连）。
		// 客户端身份：每次 API 调用全新会话（2026-10-27，见 zen_session.go），
		// 同一调用的重试沿用同一会话，msg_ 请求 ID 每次随机。
		// ZEN_PIN_KEY=n 时固定用第 n 个 key（单 key 直测/排障），默认轮转。
		// pinKey（面板 Test）优先且不碰轮转指针：一次探测不该挪动 round-robin
		// 的游标，否则每点一次 Test 都会让下一次正常请求换 key。
		var key string
		if o.PinKey != "" {
			key = o.PinKey
			retryKey = o.PinKey
		} else if pk := PinnedZenKey(); pk != "" {
			key = pk
			retryKey = pk
		} else if retryKey == "" {
			retryKey = PickZenKey()
		}
		key = retryKey
		if key == "" {
			// 空池 / 全隔离挡住 / egress 钉定容量不足（代理比 key 少）——
			// ZenNoKeyError 按当前生效模式选对应文案（含 egress 容量口径）。
			return nil, rateLimited, ZenNoKeyError()
		}
		proxyURL, pidx, exitOK := Host.BoundExitFor(key)
		if !exitOK {
			// ZEN_PIN_KEY/pinKey 固定死的 key 没有可换的余地，直接报错；
			// 正常轮转把该 key 短冷却（PickZenKey 下一轮跳过它）后换 key。
			if o.PinKey != "" || PinnedZenKey() != "" {
				return nil, rateLimited, ZenBlockedExitErr(key)
			}
			CooldownZenKey(key, 2*time.Minute)
			// 清空 retryKey 换下一个 key（与 CallZenResponsesAPI 同款）。留着的话
			// 下一轮 retryKey 非空会跳过 PickZenKey 原样复用本 key，而其绑定出口
			// 在冷却期内恒不可用——2026-11-06 生产实证：出口 10 分钟冷却期内单
			// key 无睡眠无限自旋，刷出 30 万+ 行日志并打满一核。
			retryKey = ""
			log.Printf("  zen key#%d bound proxies unavailable, skipping key for 2m", KeyIndex(key))
			continue
		}
		viaProxy := "direct"
		if proxyURL != "" {
			viaProxy = kit.MaskProxyURL(proxyURL)
		}
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, rateLimited, fmt.Errorf("create zen request: %w", err)
		}
		// Fresh session per API call (2026-10-27, see ZenCallIdentity).
		sess, user, ua := callIdent.forKey(key)
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", ua)
		// x-opencode-session-id：真实 CLI 两个头都发（request.ts），见 responses 路径。
		SetZenGateHeaders(req, sess, user, key)

		log.Printf("  zen upstream: model=%s stream=%v msgs=%d via=%s key=#%d attempt=%d session=%s",
			body["model"], stream, Host.GetMsgCount(params), viaProxy, KeyIndex(key), attempt+1, kit.Truncate(sess, 24))
		Host.LogUpstreamInfo(ctx, fmt.Sprintf("key#%d", KeyIndex(key)))

		// hsStart 拨号前打点（与 cline 路径同口径）：Do 返回即隧道建立（首字节未回），
		// 差值=TCP+TLS+CONNECT 握手耗时。此前恒传 0，导致 zen/opencode 出口的
		// 「平均握手延迟」徽标永远无样本（面板显示 -），只有在线率在动。
		hsStart := time.Now()
		// 流式请求的响应头看门狗（与 cline 同款，复用 clineHeaderWatchdogFor）：
		// 黑洞出口（TCP 通、响应头被静默丢弃）会把请求悬到共用 transport 的
		// ResponseHeaderTimeout（5 分钟）甚至更久（2026-11-05 生产实证 632 秒），
		// 期间占着并发信号量且换 key 重试链不触发。只包流式——非流式的响应头
		// 要等整段生成完成，看门狗语义不成立。
		doCtx, stopWatch := ctx, func() bool { return false }
		if stream {
			doCtx, stopWatch = Host.ClineHeaderWatchdogFor(ctx, ZenHeaderTimeout())
		}
		resp, err := exitpool.ClientFor(proxyURL).Do(req.WithContext(doCtx))
		if stopWatch() {
			log.Printf("  zen upstream: response header watchdog fired after %v (exit unresponsive, retrying)", ZenHeaderTimeout())
			err = fmt.Errorf("zen response header watchdog fired after %v", ZenHeaderTimeout())
		}
		if err == nil {
			mainP, backupP, boundP := Host.KeyBindingOf(key)
			Host.LogUpstreamExitType(ctx, exitpool.ProxyExitType(proxyURL, mainP, backupP, boundP))
			exitpool.RecordProxyOutcome(exitpool.KindZen, proxyURL, true, nil, time.Since(hsStart).Milliseconds())
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
				// shouldCooldownExit 纯查询（不写计数），真正的 strike 记账与冷却
				// 在下面的 recordProxyOutcome 单点完成。建连失败与隧道后流错误
				// 都是 60s 窗口 2-strike（2026-10-31）。
				if scope, cool := exitpool.ShouldCooldownExit(exitpool.KindZen, proxyURL, err); cool {
					if scope == "global" {
						exitpool.CooldownUpstreamProxy(pidx, 2*time.Minute)
					} else {
						exitpool.CooldownUpstreamProxyFor(exitpool.KindZen, pidx, exitpool.PlatformCooldownConst)
					}
					log.Printf("  zen proxy failed (%v), cooldown exit %s %s for 2m", err, scope, viaProxy)
				} else {
					log.Printf("  zen upstream-side stream error (%v) on exit %s — not cooling the exit", err, viaProxy)
				}
				exitpool.RecordProxyOutcome(exitpool.KindZen, proxyURL, false, err, 0)
			}
			// 网络错误:退避重试(不计入故障转移,瞬时可恢复);ctx 取消时中断等待
			if sleeps < retries {
				log.Printf("  zen network error (%v), retry %d/%d after %v", err, sleeps+1, retries, delay)
				if !SleepCtx(ctx, kit.WithRetryJitter(delay)) {
					return nil, rateLimited, fmt.Errorf("client aborted during retry wait")
				}
				delay = ZenRetryDelay(delay)
				sleeps++
				continue
			}
			return nil, rateLimited, fmt.Errorf("zen request: %w", err)
		}
		// 显式 Accept-Encoding 关闭了 net/http 的自动解压，必须自己解。
		if derr := kit.DecodeResponse(resp); derr != nil {
			resp.Body.Close()
			return nil, rateLimited, fmt.Errorf("zen decode: %w", derr)
		}
		if resp.StatusCode == http.StatusOK {
			ZenSessionMarkSuccess(key)
			resp.Header.Set("X-Afree-Used-Key", key)
			if stripTools {
				log.Printf("  zen fingerprint gate bypassed: request served without client tools (key#%d) — client tool capability lost for this round", KeyIndex(key))
			}
			return resp, rateLimited, nil
		}

		bodyBytes := kit.ReadBody(resp)
		resp.Body.Close()
		// 类型化错误携带状态码（同 CallZenResponsesAPI）：端点学习只看状态码
		apiErr := &ZenHTTPError{Status: resp.StatusCode, Body: kit.Truncate(bodyBytes, 500)}

		if IsRateLimited(resp.StatusCode, bodyBytes) {
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
			raDur := kit.ParseRetryAfter(rawRetry)
			// 日额度耗尽 != 瞬时节流：前者今天不会再好，逐请求重试只是把
			// IDE 的重试风暴放大成对上游的持续打点（还会让每个失败都记一次
			// rateLimited，统计失真）。判定后改为"停放该 key"，停放期内
			// PickZenKey 直接跳过，到期只放一个探针。
			if ZenDailyExhausted(resp.StatusCode, bodyBytes, raDur) {
				// 指纹闸自愈：当天几乎没用过的 key 吃到"日额度耗尽"形态的
				// 429，大概率是免费层身份指纹闸按请求内容命中（换 key 无用）。
				// 同 key 去客户端工具重试一次（至多一次）；仍耗尽才按真额度
				// 停放。pinKey（面板 Test）保持原始语义不参与自愈。
				if used := zenKeyTokensToday(key); !toolRetryDone && o.PinKey == "" && PinnedZenKey() == "" && used <= zenFingerprintSuspectTokens {
					toolRetryDone = true
					stripTools = true
					body["tools"] = ZenGateTools()
					// auto-only 模型（crouton 等）拒收 none——与"客户端无工具"
					// 分支同款降级（见 BuildZenBody），否则自愈重试必 400、400
					// 又会进弃用迁移分支把更难诊断的错误返回客户端（2026-11-27
					// 审计 S2）。
					bm, _ := body["model"].(string)
					if ZenToolChoiceAutoOnly(bm) {
						body["tool_choice"] = "auto"
					} else {
						body["tool_choice"] = "none" // 与"客户端无工具"既有语义一致
					}
					bodyJSON, err = json.Marshal(body)
					if err != nil {
						return nil, rateLimited, fmt.Errorf("marshal zen body: %w", err)
					}
					log.Printf("  zen daily-quota-shaped 429 on barely-used key#%d (%d tok today): suspect fingerprint gate, retrying same key without client tools (dropped: %s)",
						KeyIndex(key), used, kit.Truncate(zenClientToolNames(params), 260))
					continue
				}
				parked := ParkZenKey(key, bodyBytes)
				log.Printf("  zen daily quota exhausted (429) key#%d: parked %v (probe #%d), Retry-After=%q",
					KeyIndex(key), parked, ZenParkProbeCount(key), rawRetry)
				// 停放后立即尝试换到别的 key：多 key 池里这一条就把
				// "一个 key 用完"变成"池子继续可用"。
				if o.PinKey == "" && keySwitches < maxKeySwitches {
					if next := PickZenKey(); next != "" && next != key && !ZenKeyCooling(next) {
						log.Printf("  zen daily quota exhausted: switching to next zen key (switch %d/%d)",
							keySwitches+1, maxKeySwitches)
						key = next
						retryKey = next
						keySwitches++
						continue
					}
				}
				return nil, rateLimited, apiErr
			}
			applied := CooldownZenKey(key, raDur)
			log.Printf("  zen rate limited (%d) key#%d: Retry-After=%q -> cooldown %v",
				resp.StatusCode, KeyIndex(key), rawRetry, applied)
			// pinKey（面板 Test）：立即原样上报 429（冷却已生效），不换 key。
			if o.PinKey != "" {
				return nil, rateLimited, apiErr
			}
			if keySwitches < maxKeySwitches {
				if next := PickZenKey(); next != "" && next != key && !ZenKeyCooling(next) {
					log.Printf("  zen rate limited (%d), switching to next zen key (%d configured, switch %d/%d)",
						resp.StatusCode, len(cfg.Keys), keySwitches+1, maxKeySwitches)
					key = next
					retryKey = next
					keySwitches++
					continue
				}
			} else if len(cfg.Keys) > keySwitches+1 {
				// 还有没用过的 key，但本次请求的换 key 预算已用完。必须明确
				// 记一行：否则表现为"明明还有 key 却直接 429 返回"，排查时
				// 会误以为是 key 池配置问题。
				log.Printf("  zen rate limited (%d): key-switch budget exhausted (%d/%d) with %d key(s) configured, not cooling the rest",
					resp.StatusCode, keySwitches, maxKeySwitches, len(cfg.Keys))
			}
			// 睡眠重试只在"等一等就可能变好"时有价值。这个 key 刚被冷却
			// ZenRateLimitCooldown（20 分钟），而这里的等待上限只有 30s ——
			// 睡醒后重试必然还是撞同一个已冷却的 key。实测单 key 池下，
			// 一次请求要挂 101 秒（4 次 upstream × 30s 睡眠）才把 429 交还，
			// IDE 侧看起来像"网关卡死"，而真实结论在第一秒就已确定。
			// 故：还有别的 key 可轮转 → 上面的换 key 分支已 continue；
			// 没有 → 立即返回 429，把退避权交还给客户端。
			// 429 一律不睡眠重试，交由客户端退避。
			//
			// 睡眠重试在 429 上没有正收益：能推进的只有"换 key"（上面已做），
			// 而睡完打回的是同一个刚被 CooldownZenKey 冻结的 key。历史实测
			// 一次请求因此挂 101 秒（4 次往返 × 30s 睡眠），IDE 侧表现为网关
			// 卡死，而结论在第一秒就已确定。这里直接返回 429。
			log.Printf("  zen rate limited (%d): failing fast (key switching already attempted)",
				resp.StatusCode)
			// 限流不是"zen 挂了"：上游明确回了 HTTP 429，服务正常，只是这个
			// key/身份当前超了窗口。把它计入故障转移会让一次瞬时节流把整个 zen
			// 路由切走（cline 池为空时客户端直接吃 401），而限流本来已由 key
			// 冷却 + 换 key + 重试处理。故此处不推进 failover。
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
			if o.PinKey != "" {
				log.Printf("  zen chat session rejected (403) [%s], key#%d (probe: no state change)", kit.Truncate(sess, 24), KeyIndex(key))
				return nil, rateLimited, apiErr
			}
			// 持续性故障闸门：窗口内刚因 403 被动换过会话、又收到干净 403 ——
			// 实测这类 403 跟着出口走（同 key 同会话只换出口即 200），换新救
			// 不回来。冷却该 key（退出轮转）而不是再铸一个会话白跑一次上游。
			if ZenSessionRefreshedRecently(key, time.Now()) {
				ZenSessionFailed(key)
				applied := CooldownZenKey(key, zenForbiddenCooldown)
				log.Printf("  zen chat session rejected (403) again within %v of a local refresh [%s] key#%d via=%s: key/exit suspect, cooled %v (no re-mint)",
					zenForbiddenEscalateWindow, ZenSessionDesc(key), KeyIndex(key), viaProxy, applied)
				return nil, rateLimited, apiErr
			}
			if !sessionRetried {
				sessionRetried = true
				log.Printf("  zen chat session rejected (403) [%s] key#%d: minting fresh session for this call, retry same key",
					kit.Truncate(sess, 24), KeyIndex(key))
				callIdent.refresh(key)
				continue
			}
			ZenSessionFailed(key)
			applied := CooldownZenKey(key, 5*time.Minute)
			log.Printf("  zen chat session rejected twice (403) key#%d: key cooled %v, failing fast (no cross-key fan-out)",
				KeyIndex(key), applied)
			return nil, rateLimited, apiErr
		}
		// 弃用迁移（410 + replacement / 400 unavailable）：非 pin 探测才迁移；
		// 迁移成功则重建 body 就地重试（至多一次）。
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusBadRequest {
			if migrated := ZenHandleDeprecatedModel(resp.StatusCode, bodyBytes, body["model"].(string), params, o.PinKey != "", remapped); migrated {
				remapped = true
				body = BuildZenBody(params, stream)
				if m := toolrename.Build(toolrename.KindZen, zenRenameCandidateNames(params)); m != nil {
					toolrename.RewriteChatForm(body, m)
				}
				if stripTools {
					// 指纹自愈的 strip 必须跨 410 重建存活，否则重建会把
					// 客户端工具带回来（重建走的是完整 BuildZenBody）。
					body["tools"] = ZenGateTools()
					body["tool_choice"] = "none"
				}
				bodyJSON, err = json.Marshal(body)
				if err != nil {
					return nil, rateLimited, fmt.Errorf("marshal zen body: %w", err)
				}
				continue
			}
		}
		return nil, rateLimited, apiErr
	}
	// 循环上限兜底（MaxZenAttempts），同 CallZenResponsesAPI。
	return nil, rateLimited, fmt.Errorf("zen: retry budget exhausted")
}

// SleepCtx ctx 感知的睡眠；返回 false 表示等待期间被取消。
func SleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// KeyIndex key 在池中的序号（日志用）
func KeyIndex(key string) int {
	for i, k := range GetZenConfig().Keys {
		if k == key {
			return i + 1
		}
	}
	return 0
}

// ZenRetryDelay 指数退避的下一步延迟，封顶 30s。
//
// 必须封顶：Retries 来自面板配置（.zen-config.json 亦可手改），裸翻倍到约 63 次
// 就会让 int64 纳秒溢出为负数，而 time.NewTimer(负值) 立即触发 —— 重试退化成
// 无退避热循环，把并发槽位全部耗在空转上。封顶同时也限住了网络错误重试的等待
// 时长（该路径不做 Retry-After 修正，直接用 delay）。
func ZenRetryDelay(d time.Duration) time.Duration {
	d *= 2
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

func ZenModelList() []map[string]any {
	// 展示门控：与请求入口的可路由性闸同判定——没有启用的具名 key 时
	// 平台不可路由，目录继续展示只会让用户选中必失败的模型。
	if !Host.ZenRoutable() {
		return nil
	}
	// 别名表先快照（DepMu 不与 zenModelsMu 嵌套）。
	depAliases := map[string]string{}
	DepMu.Lock()
	for dead, e := range DepAliasesVar {
		depAliases[dead] = e.Replacement
	}
	DepMu.Unlock()
	zenModelsMu.RLock()
	out := make([]map[string]any, 0, len(zenModels))
	for _, m := range zenModels {
		if !IsZenFreeModel(m) {
			continue
		}
		cp := *m
		entry := map[string]any{
			// 网关前缀（zen:<id>）：客户端必须按列表 ID 调用——请求闸 servedModelGate
			// 精确匹配目录，裸 ID 直连按 strict-gate 404；旧形态只在 combo target
			// 与内部解析里仍被接受。
			"id":        ZenPrefixedID(cp.ID),
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

// OpencodeModelsRegistry 公共模型目录（官方 CLI 同源，无需认证；
// 替代需要有效 key 才能访问的 zen /v1/models）。
const OpencodeModelsRegistry = "https://models.opencode.ai/api.json"

// MaxRegistryBytes 目录响应体积上限（8 MiB）。正常目录约几百 KB，设置上限
// 只为防止异常响应把内存吃穿；超限按"不可达"处理。
const MaxRegistryBytes = 8 << 20

// ZenRegistryURL 可测试覆写：默认指向公共目录，测试指向 httptest 服务。
var ZenRegistryURL = OpencodeModelsRegistry

// ZenModelOverlay 公共目录里比 zen 真源多的限额/旗标字段。
type ZenModelOverlay struct {
	Context, Output     int
	ToolCall, Reasoning bool
	Attachment          bool
	// NPM 该模型的 SDK 包名（per-model provider.npm；解析规则与官方 CLI
	// fromModelsDevModel 一致：model.provider?.npm ?? provider.npm ?? 默认）。
	// 端点路由的种子信号，见 ZenNPMToUpstream。
	NPM string
}

// FetchZenRegistry 拉取公共目录 api.json，返回（限额 overlay、价格门集合、
// provider 级 api 基地址、是否可达）。价格门 = cost.input==0 && cost.output==0
// 且 status 非 deprecated。模型 ID 以条目内 id 优先、map key 兜底。目录不可达
// 返回 (空, 空, "", false)，调用方按 fail-open 处理。
// providerAPI 是 opencode provider 的 api 字段——免费层真正的上游基地址。
// 它此前只存在于一次人工观测里（见 ZenAPIBase 的注释），现在每次目录同步都
// 重新取一遍，供 ApplyZenRegistryBase 校正默认值。
// 唯一实现：SyncZenModels 使用（会话铸造改本地后不再有第二消费者），
// 避免两处价格门逻辑漂移。

// ZenSyncClient zen 目录同步（models.opencode.ai，无凭据的公开元数据请求）
// 的出口解析，与会话请求同语义（boundExitFor：显式绑定 > egress 钉定 > 走池
// 开关）——遍历参与路由的 key 取第一个可用出口；无 key/全不可用时按走池
// 开关走池或直连，DISABLE_DIRECT 恒拦。此前是裸直连（2026-11-05 审计实测）：
// 直连不可达的部署上 zen 免费目录自重启起每轮 "registry unreachable"，
// 目录冻结在种子/旧表。
func ZenSyncClient() (*http.Client, error) {
	cfg := GetZenConfig()
	for _, key := range cfg.Keys {
		if !ZenKeyRoutingEnabled(key) {
			continue
		}
		if proxyURL, _, ok := Host.BoundExitFor(key); ok {
			return WithSyncTimeout(exitpool.ClientFor(proxyURL)), nil
		}
	}
	if ZenProxiesEnabled() {
		proxyURL, _, exitOK := exitpool.UpstreamForKind(exitpool.KindZen)
		if !exitOK {
			return nil, fmt.Errorf("every pool exit is manually cooled")
		}
		if proxyURL == "" && kit.DisableDirect() {
			return nil, fmt.Errorf("pool empty and direct egress disabled by DISABLE_DIRECT")
		}
		return WithSyncTimeout(exitpool.ClientFor(proxyURL)), nil
	}
	if kit.DisableDirect() {
		return nil, fmt.Errorf("direct egress disabled by DISABLE_DIRECT")
	}
	return WithSyncTimeout(kit.HTTPClient), nil
}

func FetchZenRegistry() (map[string]ZenModelOverlay, map[string]bool, string, bool) {
	overlay := map[string]ZenModelOverlay{}
	freeGate := map[string]bool{}
	oreq, err := http.NewRequest("GET", ZenRegistryURL, nil)
	if err != nil {
		return overlay, freeGate, "", false
	}
	oreq.Header.Set("User-Agent", "opencode/latest/cli")
	client, err := ZenSyncClient()
	if err != nil {
		return overlay, freeGate, "", false
	}
	oresp, err := client.Do(oreq)
	if err != nil {
		return overlay, freeGate, "", false
	}
	defer oresp.Body.Close()
	if oresp.StatusCode != 200 {
		return overlay, freeGate, "", false
	}
	var payload map[string]struct {
		NPM    string `json:"npm"`
		API    string `json:"api"`
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
	if json.NewDecoder(io.LimitReader(oresp.Body, MaxRegistryBytes+1)).Decode(&payload) != nil {
		return overlay, freeGate, "", false
	}
	prov, ok := payload["opencode"]
	if !ok {
		return overlay, freeGate, "", false
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
		overlay[id] = ZenModelOverlay{
			Context: m.Limit.Context, Output: m.Limit.Output,
			ToolCall: m.ToolCall, Reasoning: m.Reasoning, Attachment: m.Attachment,
			NPM: npm,
		}
		if m.Cost != nil && m.Cost.Input == 0 && m.Cost.Output == 0 &&
			!strings.Contains(strings.ToLower(m.Status), "deprecat") {
			freeGate[id] = true
		}
	}
	return overlay, freeGate, strings.TrimRight(strings.TrimSpace(prov.API), "/"), true
}

// ApplyZenRegistryBase 用目录里的 provider.api 校正 zen 上游基地址。
//
// 背景：ZenAPIBase 是编译期常量，而免费层的路径已经改过一次（/zen/v1 →
// /inference/v1 → /inference/openai/v1），每次都靠人工观测再改代码，期间网关
// 一直打在已经不存在的旧路径上。目录本身就是这个地址的唯一权威（真实 CLI
// 也读同一份数据），所以每次目录同步都拿它校正一次。
//
// 三条纪律：
//   - 只在基地址仍等于"本项目的默认值"时校正。用户在面板里显式填过别的
//     地址（自建中转、抓包代理）就绝不动它——那是明确的意图，不是过时的
//     观测。
//   - 只接受 https://opencode.ai/inference/... 的值。目录是远端数据，不能让
//     一个被污染或中间人改写的响应把带 Authorization 的流量导去任意主机，也
//     不能把它指回已失效的 /zen/v1（匿名流量在那里恒 403）。
//   - 写盘时走 SetZenConfig 的既有路径（含 SaveZenConfig 的原子写），并留日志。
//
// 返回是否发生校正。
func ApplyZenRegistryBase(registryAPI string, current string) bool {
	want := strings.TrimRight(strings.TrimSpace(registryAPI), "/")
	if want == "" {
		return false
	}
	// 目录给的是顶级 provider api；免费模型都在 /inference/openai/v1 下。
	// 只接受 https://opencode.ai/inference/...：既避免把凭据送去别处，也避免
	// 被篡改的目录把基地址指回已失效的 /zen/v1（匿名流量在那里恒 403）。
	u, err := url.Parse(want)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "opencode.ai") ||
		!strings.HasPrefix(u.Path, "/inference/") {
		log.Printf("zen registry api ignored (not https://opencode.ai/inference/...): %q", kit.Truncate(want, 120))
		return false
	}
	cur := strings.TrimRight(strings.TrimSpace(current), "/")
	// 只覆盖"仍是当前默认值"的情形；用户显式改过的地址不碰。
	if cur != ZenAPIBase {
		return false
	}
	if cur == want {
		return false
	}
	cfg := *GetZenConfig()
	cfg.BaseURL = want
	SetZenConfig(&cfg)
	log.Printf("zen baseURL corrected from registry: %s -> %s", cur, want)
	return true
}

// ZenDesiredFromLive 推导可服务的免费模型集（纯函数，便于测试）：
// 目录可达时以价格门为唯一权威（= 官方 CLI `opencode models` 读的同一份
// 数据的 cost 0/0 子集）；目录不可达时退回 CLI 成员表 + "-free" 启发式
// （fail-open，离线也能列出免费模型）。两者都拿不到时返回错误，调用方保留旧表。
func ZenDesiredFromLive(registryOK bool, freeGate map[string]bool) (map[string]bool, error) {
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

// SyncZenModels 同步免费模型（live 为准）。判定链：
//  1. 免费资格（唯一权威）：公共目录 models.opencode.ai 的 opencode 条目
//     cost.input==0 && cost.output==0 且 status 无 deprecated 字样。无 free
//     后缀但价格为 0 的模型（big-pickle）因此入选；status=deprecated 的
//     *-free 条目即使价格为 0 也排除（上游已报 Model unavailable）。
//     官方 CLI `opencode models` 读的是同一份目录（实测 2026-09-18：CLI 7 个
//     = 目录中 opencode 提供方 cost 0/0 非 deprecated 的 7 个），所以价格门
//     就是"真实模型列表"本身。
//  2. 限额 overlay：目录 limit.context/output + tool_call / reasoning /
//     attachment 旗标 + per-model provider.npm（端点种子，见 ApplyZenCatalog）。
//  3. 上游基地址：目录 provider.api 与"仍是默认值"的配置不符时自动校正
//     （见 ApplyZenRegistryBase）——免费层路径改过一次，不该再靠人工观测。
//
// 目录不可达时返回错误并保留旧表（收割机时代的 CLI 成员表兜底已随内嵌 CLI
// 移除，离线启动不再有第三方成员表来源，见 ZenDesiredFromLive）。冷启动不再
// 注入种子快照（2026-11-11 用户定）：
// 表为空直到首次成功同步，宁空勿过期。
// 返回新增模型数。
func SyncZenModels() (int, error) {
	overlay, freeGate, registryAPI, registryOK := FetchZenRegistry()
	desired, derr := ZenDesiredFromLive(registryOK, freeGate)
	if derr != nil {
		return 0, derr
	}
	// 基地址先校正：后面 ApplyZenCatalog 之后马上可能有请求进来。
	ApplyZenRegistryBase(registryAPI, GetZenConfig().BaseURL)
	added := ApplyZenCatalog(desired, overlay)
	pruned := PruneZenModelsTo(desired)
	// 重新敷用已学习的端点覆盖：新出现的模型（以及被 prune 后重建的条目）不在
	// 启动时 LoadZenEndpoints 的视野里，不敷用就会每次重启重新探测一遍。
	ReapplyLearnedEndpoints()
	// 目录刷新成功：解除上游 400 "Model is unavailable" 的死亡标记（此前可能
	// 只是目录滞后），并清理指向已不存在模型的别名/标记。
	ZenClearDeadModels()
	PruneZenDeadAndAliases(desired)
	if added > 0 {
		log.Printf("zen model sync: %d new free model(s) from live catalog", added)
	}
	if pruned > 0 {
		log.Printf("zen model sync: pruned %d model(s) no longer free/online", pruned)
	}
	return added, nil
}

// ZenNPMToUpstream 把 SDK 包名映射到上游路由种子（与官方 CLI 的 resolveSDK
// 语义一致）。返回 (upstream, known)：
//   - "@ai-sdk/openai"          → "responses"（原生 POST {base}/responses）
//   - "@ai-sdk/openai-compatible" → ""（默认 POST {base}/chat/completions）
//   - "@ai-sdk/anthropic" / "@ai-sdk/google" → ("", false)：免费层目前没有
//     存活的这类模型；映射记下来但不做原生分发，交端点学习器自行纠正，
//     日志留 warning（将来出现原生模型再补真分发）。
//   - 其他未知值 → ("", false)：绝不猜，让学习器用上游拒绝信号决定。
func ZenNPMToUpstream(npm string) (string, bool) {
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

// ApplyZenCatalog 把 live 结果写进模型表：存量条目就地更新（保留已定端的
// Upstream 提示），新条目按 overlay 限额建立。返回新增数量。
func ApplyZenCatalog(desired map[string]bool, overlay map[string]ZenModelOverlay) int {
	zenModelsMu.Lock()
	defer zenModelsMu.Unlock()
	added := 0
	for id := range desired {
		ov := overlay[id]
		if cur, ok := zenModels[id]; ok {
			// 存量条目：overlay 限额覆盖上次估算；已定端的 Upstream 提示
			// 沿用，Source 升级为 live。
			// 改副本再挂回（copy-on-write）：ResolveZenModel 返回的指针在释放
			// RLock 后仍被请求路径读取，就地改写会与其裸读竞争。
			next := *cur
			if ov.Context > 0 {
				next.Context = ov.Context
			}
			if ov.Output > 0 {
				next.Output = ov.Output
			}
			if ov != (ZenModelOverlay{}) {
				next.ToolCall, next.Reasoning, next.Attach = ov.ToolCall, ov.Reasoning, ov.Attachment
			}
			// npm 路由重播种：仅当该模型尚无明确端点决策时按目录 npm 刷新。
			// "决策"= Upstream 非空，或学习器已定论（Source=learned，含 chat
			// 定论：learning 后 Upstream 归零但不得再被 npm 播回 responses）。
			// load/ReapplyLearnedEndpoints 恢复学习条目，这里不得回翻。
			if next.Upstream != "" || next.Source == "learned" {
				// 已有决策，保持
			} else if ov.NPM != "" {
				if up, known := ZenNPMToUpstream(ov.NPM); known && up != "" {
					next.Upstream = up
				}
			}
			// Source 保留"learned"标记：它既表示"学习器已定论，npm 不得重播种"，
			// 也是 SaveZenEndpoints 写盘时的筛选条件（只存 learned 条目）。这里若
			// 无条件抹成 "live"，sync 结束前并发的 SaveZenEndpoints 会把该条目从
			// 学习文件里丢掉，ReapplyLearnedEndpoints 也因此失去依据——学习决策
			// 只在 reapply 恰好晚于本函数的时序下才存活。保留标记使契约显式成立。
			if next.Source != "learned" {
				next.Source = "live"
			}
			zenModels[id] = &next
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
			if up, known := ZenNPMToUpstream(npm); known {
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

// PruneZenModelsTo 修剪掉不在 live 结果中的条目：同步成功后以 live 为准，
// 避免已下线/已转付费的模型继续挂出。防御：live 残表（接口抖动/改版）不清空
// 本地列表 —— 要求 live 结果不少于现有条数的一半。返回移除数量。
func PruneZenModelsTo(desired map[string]bool) int {
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
		// 上游（见 ZenPruneGrace 注释）。宽限期内不回 live 列表（面板不挂出）。
		zenGraceModels[id] = ZenGraceEntry{
			m:        m,
			prunedAt: now,
		}
		delete(zenModels, id)
		pruned++
	}
	// 宽限到期（历经多轮同步仍未回到 live 列表）：真下线，彻底清除
	for id, e := range zenGraceModels {
		if now.Sub(e.prunedAt) > ZenPruneGrace {
			delete(zenGraceModels, id)
		}
	}
	return pruned
}

// StartZenModelsRefresher 定时同步 zen 模型列表（5 分钟，与各平台模型
// 目录的自动同步节奏一致）。同步走公共注册表 api.json（免认证），不消耗任何
// key 的配额。冷启动阶段以 30s 间隔快速重试直至首次同步成功（至多 10 次，
// 与 other 同款）：种子快照取消后空表意味着目录暂不可见，首拉要尽快。
func StartZenModelsRefresher() {
	go func() {
		for i := 0; i < 10; i++ {
			if _, err := SyncZenModels(); err == nil {
				break
			} else if i == 9 {
				log.Printf("zen model sync: cold-start retries exhausted (%v), keeping the current list", err)
			}
			time.Sleep(30 * time.Second)
		}
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			if added, err := SyncZenModels(); err != nil {
				log.Printf("zen model sync: failed (%v)", err)
			} else if added > 0 {
				log.Printf("zen model sync: %d new models from official feed", added)
			}
		}
	}()
}

// ZenHeaderTimeout zen 上游响应头看门狗超时（ZEN_HEADER_TIMEOUT 覆盖，缺省 60s）。
func ZenHeaderTimeout() time.Duration {
	if n, ok := kit.EnvInt("ZEN_HEADER_TIMEOUT"); ok {
		if n <= 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	return 60 * time.Second
}

// ZenProxiesEnabled OpenCode 上游是否走共享代理池：配置缺省（nil）默认开启
// （走池）；面板下拉可显式选择走池/直连。配置住 zen-config.json（2026-11-17
// 迁回平台文件）。
func ZenProxiesEnabled() bool {
	if zup := ZenConfigUseProxies(); zup != nil {
		return *zup
	}
	return true
}

// WithSyncTimeout 目录同步 HTTP client：同步是低频后台操作，响应头超时取
// ZenHeaderTimeout 的 2 倍（目录接口偶发慢），不做长连接复用。
func WithSyncTimeout(c *http.Client) *http.Client {
	t := c.Transport.(*http.Transport)
	clone := t.Clone()
	clone.ResponseHeaderTimeout = ZenHeaderTimeout() * 2
	return &http.Client{Transport: clone, Timeout: c.Timeout}
}

// ModelEntriesDetailed 面板模型表（zen 域出包后整段迁入）：仅免费模型，
// 输出与 /v1/models 同口径（zen:<id> 网关前缀）。
func ModelEntriesDetailed() ([]map[string]any, bool) {
	credentialsEnabled := ZenHasRoutableKey()
	zenModelsMu.RLock()
	models := make([]map[string]any, 0, len(zenModels))
	if credentialsEnabled {
		for _, m := range zenModels {
			if !IsZenFreeModel(m) {
				continue
			}
			models = append(models, map[string]any{
				"id":        ZenPrefixedID(m.ID),
				"context":   m.Context,
				"output":    m.Output,
				"source":    m.Source,
				"upstream":  m.Upstream,
				"toolCall":  m.ToolCall,
				"reasoning": m.Reasoning,
				"attach":    m.Attach,
			})
		}
	}
	zenModelsMu.RUnlock()
	return models, credentialsEnabled
}

// NewPinKeyOpts 面板 Test 指定 key 的调用选项（pinKey 字段未导出，经构造器）。
func NewPinKeyOpts(key string) []ZenCallOpts {
	return []ZenCallOpts{{PinKey: key}}
}

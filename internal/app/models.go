package app

import (
	"afree-proxy/internal/cline"
	"afree-proxy/internal/kit"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ModelStatus string

const (
	ModelActive  ModelStatus = "active"
	ModelRemoved ModelStatus = "removed"
	ModelUnknown ModelStatus = "unknown"
)

// ModelInfo 一个可服务的免费模型。
//
// 元数据字段（ContextLength 起）来自 /ai/cline/models，是"尽力补全"而非权威值：
// 免费层的 id 是别名（cline-free/x、y-free），该 feed 里根本不存在这些别名，
// 只能按末段名回连到真实条目（MetaSource 记录连到了谁，便于审计）。连不上就
// 留零值，绝不猜。
type ModelInfo struct {
	ID             string      `json:"id"`
	Name           string      `json:"name,omitempty"`
	Source         string      `json:"source"`
	Provider       string      `json:"provider"`
	Cost           string      `json:"cost"`
	Status         ModelStatus `json:"status"`
	RequiresStream bool        `json:"requiresStream,omitempty"`
	SyncedAt       time.Time   `json:"syncedAt,omitempty"`

	// 元数据（可选，来自 /ai/cline/models 的尽力回连）
	ContextLength            int      `json:"contextLength,omitempty"`
	MaxOutputTokens          int      `json:"maxOutputTokens,omitempty"`
	InputModalities          []string `json:"inputModalities,omitempty"`
	OutputModalities         []string `json:"outputModalities,omitempty"`
	SupportsReasoning        bool     `json:"supportsReasoning,omitempty"`
	SupportsTools            bool     `json:"supportsTools,omitempty"`
	SupportsStructuredOutput bool     `json:"supportsStructuredOutput,omitempty"`
	ReasoningEfforts         []string `json:"reasoningEfforts,omitempty"`
	MetaSource               string   `json:"metaSource,omitempty"`

	// FeedFree 官方推荐位成员（recommended-models free 组，即插件 UI 的 FREE
	// 组）：cline 目录「仅展示插件免费」开关的过滤依据。catalog 零价发现
	// 不属于该组；每次同步对全量缓存重算——模型掉出 feed 后仍可能经 catalog
	// 零价路径留在缓存，标记必须回落（见 applyFeedFreeMark）。
	FeedFree bool      `json:"feedFree,omitempty"`
	MetaAt   time.Time `json:"metaAt,omitempty"`

	// 自愈：连续失败达到阈值后暂时从列表移除，冷却后自动复活。
	// 上游的免费模型会整批下线（404）或 provider 挂掉（500 stream init），
	// 这些状态在 feed 里看不出来 —— 只能靠实际调用结果判定。
	FailStreak  int       `json:"failStreak,omitempty"`
	CooldownTil time.Time `json:"cooldownTil,omitempty"`
	LastError   string    `json:"lastError,omitempty"`
}

var (
	modelsMu       sync.Mutex
	modelsCache    map[string]*ModelInfo
	modelsSyncing  bool
	modelsLastSync time.Time
)

const (
	modelsRefreshInterval = 5 * time.Minute
	modelsSyncTimeout     = 25 * time.Second
)

const recommendedModelsURL = cline.ClineAPIBase + "/ai/cline/recommended-models"

// initModelsCache 建立空缓存。没有冷启动兜底（2026-10-31 用户定）：免费
// 模型的唯一来源是官方两个上游（recommended-models free 组 + /ai/cline/models
// 零价扫描）的并集，首次同步成功之前 / feed 不可达时目录就是空的 —— 宁愿
// 空着也不用过期快照兜底。背景：官方会把 free 模型静默移入付费档（2026-10-31
// 实测 deepseek/muse/solar-pro4 移入 clinePass，z-ai/glm 转为计费），过期
// 快照会把计费模型当免费模型对外提供。extraFreeModels 本地兜底已于
// 2026-11-11 用户拍板取消（beam-free 实测 200 但 catalog 缺条目，随机制一起
// 丢弃；官方 catalog 收录它时自然回来）。
func initModelsCache() {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	if modelsCache != nil {
		return
	}
	modelsCache = make(map[string]*ModelInfo)
}

func getFreeModels() []*ModelInfo {
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()
	out := make([]*ModelInfo, 0, len(modelsCache))
	for _, m := range modelsCache {
		// 冷却中的模型不出现在列表里：feed 显示它免费，但实测调不通，
		// 列出来只会让客户端选中后报错。冷却到期自动复活（见 recordModelSuccess）。
		if modelInCooldown(m) {
			continue
		}
		cp := *m
		out = append(out, &cp)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			if out[j-1].ID < out[j].ID {
				break
			}
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

type recommendedPayload struct {
	Free []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	} `json:"free"`
}

// clineSyncClient 目录同步的出口解析，与账号请求路径同语义（proxy.go chat）：
// 选中账号有绑定走绑定出口（全不可用 fail-closed，不回落池/直连——feed 请求
// 带该账号的 Bearer，出口漂移就是身份 IP 漂移）；未绑定按走池开关走池或直连，
// DISABLE_DIRECT 恒拦直连。此前是裸直连（机制存在但没接全，2026-11-05 审计
// 实测）：直连不可达的部署上 cline 目录自重启起每轮 EOF——目录清空、整段从
// /v1/models 摘除、探测模型退化为空串。与五平台 catalogHTTPClient 同一纪律：
// 无可用出口时跳过本轮，绝不隐式回落直连。
func clineSyncClient() (*http.Client, string, error) {
	acc := pickAccount()
	if acc == nil {
		acc = pickAccountAny()
	}
	if acc != nil {
		if main, backup, bound := accountProxyBinding(acc); bound {
			proxyURL, _, ok := pickBoundProxyFor(pkCline, main, backup)
			if !ok {
				return nil, "", fmt.Errorf("no routable exit for model sync: account %s bound proxies unavailable (cooling or removed)", truncateEmail(acc.Email))
			}
			return withSyncTimeout(proxyClientFor(proxyURL)), proxyURL, nil
		}
	}
	if clineProxiesEnabled() {
		proxyURL, _, exitOK := upstreamProxyForKind(pkCline)
		if !exitOK {
			return nil, "", fmt.Errorf("no routable exit for model sync: every pool exit is manually cooled")
		}
		if proxyURL == "" && kit.DisableDirect() {
			return nil, "", fmt.Errorf("no routable exit for model sync: pool empty and direct egress disabled by DISABLE_DIRECT")
		}
		return withSyncTimeout(proxyClientFor(proxyURL)), proxyURL, nil
	}
	if kit.DisableDirect() {
		return nil, "", fmt.Errorf("no routable exit for model sync: direct egress disabled by DISABLE_DIRECT")
	}
	return withSyncTimeout(kit.HTTPClient), "", nil
}

// withSyncTimeout 共享客户端不能改 Timeout（proxyClientFor 缓存复用），浅拷贝
// 挂同步超时。Transport 指针共享是安全的（http.Client 并发只读自身字段）。
func withSyncTimeout(c *http.Client) *http.Client {
	cp := *c
	cp.Timeout = modelsSyncTimeout
	return &cp
}

// ============ 免费模型自愈 ============
//
// feed 只能说明"这个模型存在且标价免费"，说明不了"现在真的能调通"。实测
// 2026-11-08：19 个标价 0 的模型里有 4 个是坏的 —— gemma ×2 上游 500
// inference failed、laguna 的 provider 整个挂掉、crouton-alpha 直接 404。
// 这些在 feed 里全都正常列着。
//
// 所以用真实调用结果兜底：连续失败 modelFailThreshold 次后进入冷却，冷却
// 期间不出现在列表里；冷却到期自动复活再试。成功一次就清零。
// 只对"模型级"错误计数：429/限流属于配额问题，不是模型坏了，不能因此下架。
const (
	modelFailThreshold = 3
	modelFailCooldown  = 30 * time.Minute
)

// isModelLevelFailure 该错误是否说明"模型本身不可用"（而非配额/网络/鉴权）。
//
// 这里必须"宁可漏判不可错判"：把一个好的模型误下架 30 分钟的代价，远大于
// 让一个坏模型多失败几次。实测踩过的坑：免费模型的上游是 OpenRouter，
// 它会把"上游暂时限流"也包成 500 "inference request failed" —— 但那句
// 错误体的真实内容是 "is temporarily rate-limited upstream, retry shortly"，
// 属于临时限流，跟模型好坏无关。若按 "inference request failed" 一刀切，
// 一个健康的模型会因一阵高峰期流量被永久冷却。
//
// 所以只认两条**确定性**信号：
//   - "model not found"：模型确实已下线（feed 还列着但上游没了）；
//   - "failed to create stream" / stream_initialization_failed：provider 侧
//     彻底不可用（laguna 当时整个上游挂掉）。
//
// 限流、鉴权、超时、5xx 过载一律不算 —— 那些是"等一会儿就好"。
func isModelLevelFailure(status int, body string) bool {
	lb := strings.ToLower(body)
	if strings.Contains(lb, "rate-limited") || strings.Contains(lb, "rate limited") ||
		strings.Contains(lb, "try again") || strings.Contains(lb, "retry shortly") ||
		strings.Contains(lb, "overloaded") || strings.Contains(lb, "timeout") ||
		strings.Contains(lb, "unauthorized") || strings.Contains(lb, "quota") {
		return false
	}
	switch {
	// 形态不止一种，实测两种都出现过：
	//   {"error":"model not found"}
	//   Model 'x-free' not found. If you were using its free tier, that has ended.
	case strings.Contains(lb, "model not found"), strings.Contains(lb, "not found"):
		return true
	case strings.Contains(lb, "free tier, that has ended"),
		strings.Contains(lb, "free tier has ended"):
		return true
	case strings.Contains(lb, "stream_initialization_failed"),
		strings.Contains(lb, "failed to create stream"):
		return true
	// "failed to invoke model 'x' from O..."：provider 侧彻底挂掉（2026-11-10
	// 实测 inkling-2-small / xing4.0 形态，API 层 200 无关，错误体即此话术）。
	// 与 "failed to create stream" 同族——模型级确定性故障，换账号救不回来。
	case strings.Contains(lb, "failed to invoke model"):
		return true
	case status == http.StatusNotFound:
		return true
	}
	return false
}

// recordModelFailure 记一次模型级失败；达到阈值就进入冷却。
func recordModelFailure(modelID string, status int, body string) {
	if !isModelLevelFailure(status, body) {
		return
	}
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()
	m, ok := modelsCache[modelID]
	if !ok {
		return
	}
	m.FailStreak++
	m.LastError = truncateErr(body)
	if m.FailStreak >= modelFailThreshold {
		m.CooldownTil = time.Now().Add(modelFailCooldown)
		log.Printf("  model %q marked unavailable after %d failures (%s), cooldown %s",
			modelID, m.FailStreak, m.LastError, modelFailCooldown)
	}
}

// recordModelSuccess 成功一次就清零失败计数并解除冷却。
func recordModelSuccess(modelID string) {
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()
	if m, ok := modelsCache[modelID]; ok && m.FailStreak > 0 {
		m.FailStreak = 0
		m.CooldownTil = time.Time{}
		m.LastError = ""
	}
}

// modelInCooldown 该模型当前是否处于冷却期（nil 安全）。
func modelInCooldown(m *ModelInfo) bool {
	return !m.CooldownTil.IsZero() && time.Now().Before(m.CooldownTil)
}

func truncateErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// ============ 免费模型元数据补全 ============
//
// /ai/cline/recommended-models 的 free 条目只有 id/name/description/tags，
// 拿不到上下文窗口、输出上限、模态、推理支持度这些客户端关心的信息。
// 完整元数据在 /ai/cline/models（447 条，2026-11 实测约 520KB / 0.5s）。
//
// 关键约束：free 列表里的 id 是别名，/ai/cline/models 里并不存在这些别名。
// 实测（2026-11-08）：
//
//	cline-free/nemotron-3.5-super-vl  → nvidia/nemotron-3.5-super-vl{,:free}
//	cline-free/solar-mini4            → upstage/solar-mini4
//	nvidia/nemotron-3.5-lightning-free → nvidia/nemotron-3.5-lightning{,:free}
//
// 因此按"末段名"回连：先精确匹配整条 id，再匹配去掉 -free 后缀的末段，
// 再按末段唯一命中。歧义（多个候选）时优先非 :free 的付费条目——免费别名
// 的上游就是该模型本身，:free 变体只是同一模型的限流版本，参数上限往往更小，
// 用它反而低估能力。
//
// 全部失败就留零值：宁可缺字段，也不给客户端一个猜出来的错窗口。
const clineModelsMetaURL = cline.ClineAPIBase + "/ai/cline/models"

// clineMetaEntry /ai/cline/models 单条（只取用得上的字段）。
//
// Pricing 是"该模型是否免费"的权威信号：免费条目的 prompt/completion 都是
// 字符串 "0"。注意 Recommended 的 free 组只是人工挑选的一小撮，而 catalog 里
// 标价 0 的聊天模型有十几个（beam-free、flint、inkling、nemotron-*-free…），
// 用 CLI 都能选中并回话 —— 只读 free 组会漏掉一大半免费模型。
type clineMetaEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContextLen   int    `json:"context_length"`
	Architecture struct {
		Modality        string   `json:"modality"`
		InputModalities []string `json:"input_modalities"`
		OutputModality  []string `json:"output_modalities"`
	} `json:"architecture"`
	SupportedParameters []string `json:"supported_parameters"`
	Pricing             struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
	TopProvider struct {
		ContextLength       int `json:"context_length"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
}

// extraFreeModels 本地兜底已于 2026-11-11 用户拍板取消（机制与 beam-free
// 条目一并丢弃）：catalog 完全缺失的模型不再靠硬编码清单维持——实测 200
// 也一样下架，官方 catalog 收录时自然回来。历史背景：该兜底曾覆盖
// reflection/beam-free 与 poolside/laguna-s-2.1-free（后者 2026-11-10 实测
// 上游 404 官方下线、先行移除）；两者在 /ai/cline/models 里都是整个条目
// 缺失（数据缺口而非价格不对），来源是 @cline/llms dist/models.js 的
// cline block（2026-11-08 提取）。

// isZeroPrice 上游把价格写成字符串（"0" / "0.0" / "0.00000008"）。空串表示
// 该字段缺失，不能当成免费——否则任何没填价的条目都会被误判为免费模型。
func isZeroPrice(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f == 0
}

// isFreeChatModel catalog 里这条模型是否属于"免费层可直接调用"的集合。
//
// 判据（全部来自实测 + catalog 事实）：
//   - 输入与输出价格都是 0（prompt 缺失就不算——宁可少收不可误收）；
//   - ID 带 :free 后缀（官方免费层命名约定）或输出模态含 text。
//     modality 检查本意是排除 lyria 这类纯音频/专用生成模型；2026-11-10 起
//     官方把 :free 条目的 architecture.output_modalities 整体置空（三次重拉
//     稳定复现），modality 对这批条目不再可用——:free 后缀本身就是免费的
//     权威标记，零价+:free+tools 即收录，否则维持 text 模态要求；
//   - 声明支持 tools（免费层的对话模型都支持；不支持的基本是专用生成模型）。
func (e *clineMetaEntry) isFreeChatModel() bool {
	if !isZeroPrice(e.Pricing.Prompt) || !isZeroPrice(e.Pricing.Completion) {
		return false
	}
	hasTextOut := false
	for _, m := range e.Architecture.OutputModality {
		if m == "text" {
			hasTextOut = true
			break
		}
	}
	if !hasTextOut && !strings.HasSuffix(e.ID, ":free") {
		// 无 :free 标记的零价条目（lyria 纯音频等）维持模态门槛防误收。
		return false
	}
	for _, p := range e.SupportedParameters {
		if p == "tools" {
			return true
		}
	}
	return false
}

// metaCandidateIndex 末段名 -> 该名下的所有完整 id，供别名回连用。
func metaCandidateIndex(entries []clineMetaEntry) map[string][]string {
	idx := make(map[string][]string, len(entries))
	for _, e := range entries {
		seg := e.ID
		if i := strings.LastIndex(seg, "/"); i >= 0 {
			seg = seg[i+1:]
		}
		if i := strings.Index(seg, ":"); i >= 0 {
			seg = seg[:i]
		}
		idx[seg] = append(idx[seg], e.ID)
	}
	return idx
}

// resolveMetaID 把模型 id 回连到 /ai/cline/models 里的条目。
//
// 索引的键是"末段名再去掉 :variant"（见 metaCandidateIndex），所以查询也必须
// 先做同样的归一化：`liquid/lfm-3-1.7b:free` 要先变成 `lfm-3-1.7b` 才能命中。
// 早期实现只剥了 -free 后缀、没剥 :variant，导致所有 :free 条目永远回连不上
// （面板上一整片 ctx/out 空白）。
func resolveMetaID(freeID string, idx map[string][]string) (string, bool) {
	seg := freeID
	if i := strings.LastIndex(seg, "/"); i >= 0 {
		seg = seg[i+1:]
	}
	if i := strings.Index(seg, ":"); i >= 0 {
		seg = seg[:i]
	}
	for _, cand := range []string{seg, strings.TrimSuffix(seg, "-free")} {
		ids := idx[cand]
		if len(ids) == 0 {
			continue
		}
		if len(ids) == 1 {
			return ids[0], true
		}
		// 同名多条时优先精确匹配原始 id（它自己就在 catalog 里），
		// 其次非 :free 变体（:free 是限流版，参数上限更小会低估能力）。
		for _, id := range ids {
			if id == freeID {
				return id, true
			}
		}
		for _, id := range ids {
			if !strings.Contains(id, ":free") {
				return id, true
			}
		}
		return ids[0], true
	}
	return "", false
}

// fetchClineMeta 拉取完整模型元数据。失败返回 nil（元数据是可选增强，
// 绝不能让它的失败影响免费列表本身）。
func fetchClineMeta(client *http.Client, token string) []clineMetaEntry {
	req, err := http.NewRequest("GET", clineModelsMetaURL, nil)
	if err != nil {
		return nil
	}
	req.Header = clineHeaders(token, "")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("  model meta: fetch failed (%v), keeping previous metadata", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("  model meta: HTTP %d, keeping previous metadata", resp.StatusCode)
		return nil
	}
	var payload struct {
		Data []clineMetaEntry `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		log.Printf("  model meta: decode failed (%v), keeping previous metadata", err)
		return nil
	}
	return payload.Data
}

// reasoningEffortsFor 推理档位。feed 只给出"是否支持 reasoning"
// （supported_parameters 里有 reasoning/include_reasoning 之类），不给可接受
// 的档位取值，所以不臆造：实测三个免费模型都接受 minimal/low/medium/high/xhigh
// （2026-11-08，bogus 会被上游拒绝），统一给出这五档并标注来源是实测约定。
func reasoningEffortsFor(entry *clineMetaEntry) []string {
	if entry == nil {
		return nil
	}
	has := false
	for _, p := range entry.SupportedParameters {
		if p == "reasoning" || p == "include_reasoning" || p == "reasoning_effort" {
			has = true
			break
		}
	}
	if !has {
		return nil
	}
	return []string{"minimal", "low", "medium", "high", "xhigh"}
}

// applyClineMetaLocked 把元数据写进缓存条目。调用方必须已持有 modelsMu
// （网络拉取在外面做完，避免持锁发请求）。
// 元数据缺失的条目保留旧值——feed 抖一下不该让面板字段清空。
func applyClineMetaLocked(entries []clineMetaEntry) int {
	if len(entries) == 0 {
		return 0
	}
	byID := make(map[string]*clineMetaEntry, len(entries))
	for i := range entries {
		byID[entries[i].ID] = &entries[i]
	}
	idx := metaCandidateIndex(entries)

	enriched := 0
	for id, m := range modelsCache {
		metaID, ok := resolveMetaID(id, idx)
		if !ok {
			continue
		}
		e := byID[metaID]
		if e == nil {
			continue
		}
		ctx := e.ContextLen
		if e.TopProvider.ContextLength > 0 {
			ctx = e.TopProvider.ContextLength
		}
		m.ContextLength = ctx
		m.MaxOutputTokens = e.TopProvider.MaxCompletionTokens
		m.InputModalities = e.Architecture.InputModalities
		m.OutputModalities = e.Architecture.OutputModality
		has := func(p string) bool {
			for _, x := range e.SupportedParameters {
				if x == p {
					return true
				}
			}
			return false
		}
		m.SupportsTools = has("tools")
		m.SupportsStructuredOutput = has("structured_outputs")
		m.ReasoningEfforts = reasoningEffortsFor(e)
		m.SupportsReasoning = len(m.ReasoningEfforts) > 0
		m.MetaSource = metaID
		m.MetaAt = time.Now()
		if m.Name == "" && e.Name != "" {
			m.Name = e.Name
		}
		enriched++
	}
	return enriched
}

func syncRecommendedModels() (int, error) {
	initModelsCache()

	// 优先活跃账号；全部冷却时退回任意持凭证账号——模型列表与推理配额无关
	// （实测 429 期间该接口仍 200），否则模型列表会长期停留在种子兜底状态
	acc := pickAccount()
	if acc == nil {
		acc = pickAccountAny()
	}
	if acc == nil {
		return 0, fmt.Errorf("no accounts")
	}
	token, err := ensureAccountToken(acc)
	if err != nil {
		return 0, err
	}

	client, _, err := clineSyncClient()
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("GET", recommendedModelsURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header = clineHeaders(token, "")
	// 官方 CLI 拉 recommended-models 时是纯 GET，不带 X-Task-ID（真机抓包
	// 2026-10-19 核对）；此前塞的 sess_sync_<ms> 既非官方形态也不是任务，
	// 反而不像真实客户端。

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var payload recommendedPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}

	// 元数据拉取放在加锁之前：/ai/cline/models 约 520KB，不能持锁等网络。
	// 失败返回 nil，本轮只是不补全，不影响免费列表本身。
	meta := fetchClineMeta(client, token)

	modelsMu.Lock()
	defer modelsMu.Unlock()

	// 免费模型来自两个来源，必须取并集：
	//   1. recommended-models 的 free 组 —— 官方"推荐"的一小撮（2026-11 只有 3 个）；
	//   2. /ai/cline/models 里标价 0 的对话模型 —— 真正的免费全集（beam-free、
	//      flint、inkling、nemotron-*-free 等，CLI 里都能选中并正常回话）。
	// 只读来源 1 会漏掉大半免费模型（用户实测在 CLI 里选 Beam 1.0 (Free) 能聊）。
	live := make(map[string]bool, len(payload.Free)+16)
	// 官方推荐位成员表（recommended-models free 组，即插件 UI 的 FREE 组）：
	// 目录「仅展示插件免费」开关的过滤依据（feedFree 标记）。catalog 零价
	// 发现都不算——它们不在插件 FREE 组里。
	feedFree := make(map[string]bool, len(payload.Free))
	for _, m := range payload.Free {
		feedFree[m.ID] = true
	}
	added := 0

	upsert := func(id, name string) {
		if id == "" {
			return
		}
		live[id] = true
		provider := id
		if i := indexByte(id, '/'); i >= 0 {
			provider = id[:i]
		}
		if cached, ok := modelsCache[id]; ok {
			cached.Source = "free"
			cached.Cost = "free"
			cached.Provider = provider
			cached.Status = ModelActive
			cached.SyncedAt = time.Now()
			if cached.Name == "" {
				cached.Name = name
			}
			return
		}
		modelsCache[id] = &ModelInfo{
			ID:             id,
			Name:           name,
			Source:         "free",
			Provider:       provider,
			Cost:           "free",
			Status:         ModelActive,
			RequiresStream: indexByte(id, ':') < 0,
			SyncedAt:       time.Now(),
		}
		added++
	}

	for _, m := range payload.Free {
		upsert(m.ID, m.Name)
	}
	freeFromCatalog := 0
	for i := range meta {
		if meta[i].isFreeChatModel() {
			upsert(meta[i].ID, meta[i].Name)
			freeFromCatalog++
		}
	}
	if freeFromCatalog > 0 {
		log.Printf("model sync: %d zero-priced model(s) discovered in catalog", freeFromCatalog)
	}

	// feedFree 标记对全量缓存统一重算：模型掉出 feed 后仍可能经 catalog/extra
	// 路径留在缓存，按上一轮残留的 true 会误展示。
	applyFeedFreeMark(modelsCache, feedFree)

	pruned := pruneModelsLocked(live, meta != nil)
	if pruned > 0 {
		log.Printf("model sync: pruned %d model(s) no longer on official feed", pruned)
	}

	if enriched := applyClineMetaLocked(meta); enriched > 0 {
		log.Printf("model sync: metadata resolved for %d/%d model(s)", enriched, len(live))
	}

	modelsLastSync = time.Now()
	return added, nil
}

// applyFeedFreeMark 对全量缓存重算 FeedFree 标记（每次同步调用一次）：只在
// 官方推荐位（recommended-models free 组，即插件 UI 的 FREE 组）里的条目为
// true。全量重算而非增量置位，是因为模型掉出 feed 后仍可能经 catalog 零价
// 发现留在缓存，残留 true 会让「仅展示插件免费」误展示。
func applyFeedFreeMark(cache map[string]*ModelInfo, feedFree map[string]bool) {
	for _, m := range cache {
		m.FeedFree = feedFree[m.ID]
	}
}

// feedFreeModels 「仅展示插件免费」过滤：只留 FeedFree 条目。服务端过滤=
// 数据事实生效（/admin/api/models 返回的就是过滤后的目录）；开关状态落盘
// afree-config.json（clineCatalogFeedOnly，见 global_config.go）。
func feedFreeModels(models []*ModelInfo) []*ModelInfo {
	out := make([]*ModelInfo, 0, len(models))
	for _, m := range models {
		if m != nil && m.FeedFree {
			out = append(out, m)
		}
	}
	return out
}

// clineModelPrefix Cline 免费模型的命名空间前缀（官方 feed 里 cline-free/*
// 自带、poolside/*、z-ai/* 等不带）。网关对外统一用这个前缀标识 Cline 平台，
// 与 zen: / oprt: / wbcn: / wbgb: 保持同一套"前缀即平台"的约定。
const clineModelPrefix = "cline-free/"

// displayClineModelID 对外展示/调用的 Cline 模型 ID：已带 cline-free/ 前缀的
// 原样返回（官方 feed 的 cline-free/* 条目），其余（poolside/*、z-ai/* …）
// 补上前缀，避免与 zen:/oprt: 前缀平台的裸名混淆。
func displayClineModelID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.HasPrefix(id, clineModelPrefix) {
		return id
	}
	return clineModelPrefix + id
}

// canonicalClineModelID 请求侧归一化：剥掉 cline-free/ 前缀还原 feed 真实 ID。
// 两种形态（带/不带前缀）都接受，解析到同一条目。
func canonicalClineModelID(id string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), clineModelPrefix))
}

// clineListLookupLocked 把请求侧 ID 解析到 feed 真实缓存键。缓存键就是 feed
// 原样 ID：cline-free/* 条目自带前缀、poolside/* z-ai/* 等不带，因此剥前缀
// 之后两种键形态都要查——只查剥前缀形态会漏掉 cline-free/* 原生条目，请求
// 会被 STRICT 门控误拒（2026-10-12 修复）。调用方须持 modelsMu。
func clineListLookupLocked(id string) (string, bool) {
	canonical := canonicalClineModelID(id)
	if canonical == "" {
		return "", false
	}
	if _, ok := modelsCache[canonical]; ok {
		return canonical, true
	}
	prefixed := clineModelPrefix + canonical
	if _, ok := modelsCache[prefixed]; ok {
		return prefixed, true
	}
	return "", false
}

// modelInClineList 判断（带/不带 cline-free/ 前缀两种请求形态都认）模型 ID
// 是否在 Cline 免费模型表内。
func modelInClineList(id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()
	_, ok := clineListLookupLocked(id)
	return ok
}

// pruneModelsLocked 修剪已从官方 feed 下线的模型。调用方必须已持有 modelsMu。
//
// 防御：feed 短暂为空时不清空本地列表。旧实现用"live 数量不足现有模型一半
// 就跳过"的比例判断，这在 free 列表大幅收缩时会把修剪永久禁用——2026-11 官方
// free 列表从 5 降到 3，缓存里却还挂着 8 个（含 5 个早已 404 的死模型），
// 3*2 < 8 每轮都跳过，死模型就一直留在面板上。
//
// catalogOK 说明本轮的"免费全集"来源是否成功。live 是
// recommended 的 free 组 **并上** catalog 里标价 0 的模型；catalog 拉取失败时
// fetchClineMeta 返回 nil，live 里就只剩推荐的那 3 个 —— 此时 live 非空，
// 但它是**残缺**的。若照旧按它修剪，一次 520KB 接口抖动就会删掉十几个
// beam-free/flint/inkling 这类只从 catalog 发现的模型，下一轮再以
// FailStreak=0 重建，自愈计数被静默清零。所以残缺 feed 只增不删。
func pruneModelsLocked(live map[string]bool, catalogOK bool) int {
	if len(live) == 0 {
		if len(modelsCache) > 0 {
			log.Printf("model sync: empty live feed (%d cached), skipping prune", len(modelsCache))
		}
		return 0
	}
	if !catalogOK && len(modelsCache) > 0 {
		log.Printf("model sync: catalog fetch failed, keeping %d cached model(s) (partial feed: %d)",
			len(modelsCache), len(live))
		return 0
	}
	pruned := 0
	for id := range modelsCache {
		if !live[id] {
			delete(modelsCache, id)
			pruned++
		}
	}
	return pruned
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func syncModelsOnce() {
	initModelsCache()
	modelsMu.Lock()
	if modelsSyncing {
		modelsMu.Unlock()
		return
	}
	modelsSyncing = true
	modelsMu.Unlock()
	defer func() {
		modelsMu.Lock()
		modelsSyncing = false
		modelsMu.Unlock()
	}()

	added, err := syncRecommendedModels()
	if err != nil {
		log.Printf("  model sync: failed (%v), using cached list", err)
		return
	}
	if added > 0 {
		log.Printf("  model sync: %d new free models from official feed", added)
	} else {
		log.Printf("  model sync: %d free models up to date", len(getFreeModels()))
	}
}

// firstActiveModel 确定性取 live 免费列表里排序最小的可用模型（探测用：
// 面板 Test 未指定模型时需要打一个真实存在的模型）。
func firstActiveModel() string {
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()

	best := ""
	for id, m := range modelsCache {
		if m.Status != ModelActive || modelInCooldown(m) {
			continue
		}
		if best == "" || id < best {
			best = id
		}
	}
	return best
}

// lookupFreeModel 在缓存里查模型，并且**把冷却中的模型视为不可用**。
// 调用方必须已持有 modelsMu。
//
// 为什么冷却必须在解析路径生效、而不只是在列表路径：IDE 客户端会把模型名
// 写进配置并发起长期流量，它从来不看 /v1/models。只从列表里隐藏，等于对
// 这类客户端毫无作用——请求照旧打到死模型，每次失败又把 CooldownTil 续期
// 30 分钟，冷却在持续流量下永远不会到期。冷却必须在"选模型"这一步生效，
// 死模型才会真正被绕开、计时才有机会走完。
func lookupFreeModel(id string) (*ModelInfo, bool) {
	m, ok := modelsCache[id]
	if !ok || modelInCooldown(m) {
		return nil, false
	}
	return m, true
}

// normalizeRequestModel 把请求侧两种形态（带/不带 cline-free/ 前缀）解析到
// feed 真实 ID（缓存键原样），出站请求体与账号池记账一律用真实 ID。
// 不在免费表的名字原样返回——未知模型由入口 STRICT 门控显式拒绝，这里
// 绝不静默改写成别的模型。
func normalizeRequestModel(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	initModelsCache()
	modelsMu.Lock()
	resolved, ok := clineListLookupLocked(id)
	cooling := ok && modelInCooldown(modelsCache[resolved])
	modelsMu.Unlock()
	if ok && !cooling {
		return resolved
	}
	if cooling {
		// 冷却中的模型按"不可解析"处理返回空串（上游 2026-11-08 修复的本地
		// 落地：IDE 客户端把模型名钉在配置里从不看 /v1/models，冷却只在列表
		// 生效等于没用——持续流量会把冷却无限续期）。调用方须对空串显式拒绝。
		log.Printf("  model %q is cooling down after repeated failures, rejecting", id)
		return ""
	}
	log.Printf("  model %q not in free list, passing through as-is", id)
	return id
}

// clineModelCooling 请求侧判定：该模型（两种前缀形态都认）在 cline 目录内
// 且正处于失败冷却中。chat 入口据此显式 503，宁弃请求不撞已知坏模型。
func clineModelCooling(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()
	resolved, ok := clineListLookupLocked(id)
	return ok && modelInCooldown(modelsCache[resolved])
}

// modelInFreeList 门控放行判定：空串放行（入口校验负责拒绝无 model 请求），
// 否则 Cline 表（带/不带 cline-free/ 前缀都认）或 zen 模型命中即放行。
func modelInFreeList(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return true
	}
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()
	key, ok := clineListLookupLocked(id)
	if !ok || modelInCooldown(modelsCache[key]) {
		return false
	}
	return true
}

// strictModelGate STRICT_MODEL_MATCH 开启时，对既不在 cline 免费模型表、
// 也不是 zen 模型的名字返回错误提示；空串表示放行。zen 模型必须放行：
// zen 目录按分钟同步，闪断空窗里的误拒靠这里兜住。combo 别名由调用方先改写。
// gateRefreshMinInterval 门控触发的强制刷新节流：30 秒内多个未命中只真实
// 刷新一次，客户端循环发未知模型名也不会打爆上游 feed。
const gateRefreshMinInterval = 30 * time.Second

var gateRefreshLast atomic.Int64 // 上次门控刷新时间（unix 秒）

func strictModelGate(model string) string {
	if !StrictModelMatchEnv() || modelInFreeList(model) {
		return ""
	}
	initZenModels()
	if _, ok := resolveZenModel(model); ok {
		return ""
	}
	// 未命中不立即拒绝：两份模型缓存按分钟刷新，且上游 zen 目录存在闪断
	// （实测 2026-09-27 02:01 prune 一个模型、02:02 又加回），客户端请求
	// 可能恰好落进空窗被误拒。节流地强制同步两份缓存再重查；仍找不到才
	// 拒绝。真下线的模型该请求随后会在上游得到真实错误（该类错误不冷却
	// key），不会把死模型硬塞给上游。
	if last := gateRefreshLast.Load(); time.Since(time.Unix(last, 0)) >= gateRefreshMinInterval &&
		gateRefreshLast.CompareAndSwap(last, time.Now().Unix()) {
		syncModelsOnce()
		if _, err := syncZenModels(); err != nil {
			log.Printf("  strict gate: zen catalog refresh failed (%v)", err)
		}
		if modelInFreeList(model) {
			return ""
		}
		if _, ok := resolveZenModel(model); ok {
			return ""
		}
	}
	return fmt.Sprintf("model %q is not available on this gateway (see /v1/models for the model list)", model)
}

func apiModelList() []map[string]any {
	// 展示门控：没有可路由的 Cline 账号时不出目录——裸模型名按 Cline 路由，
	// 账号总闸/反代开关全关时请求入口直接 503，继续展示只会让客户端选中
	// 必失败的模型（与 amd/tokenharbor/trae 同口径）。
	if !platformHasRoutableCredential(platCline) {
		return nil
	}
	// 先取快照再装箱：getFreeModels 内部持锁，但容量提示不能直接读
	// len(modelsCache)——那是无锁读 map 头，与 60s 刷新器并发时是 data race
	//（/v1/models 是客户端热路径，每个请求都会走到这里）。
	free := getFreeModels()
	// 「仅展示插件免费」开关（2026-11-11 用户定）：对外目录与面板一致——
	// 面板有什么，对外提供什么。开启后 cline 段只列官方推荐位（recommended
	// free 组）。servedModelEntries/modelIsServed 与本函数同源，被隐藏的
	// 模型请求侧也会被「不在目录」兜底拒，列表与路由语义保持一致。
	if globalClineCatalogFeedOnly() {
		free = feedFreeModels(free)
	}
	out := make([]map[string]any, 0, len(free))
	for _, m := range free {
		item := map[string]any{
			// cline-free/ 前缀：feed 自带前缀的条目（cline-free/*）原样，
			// 其余（poolside/*、z-ai/* …）补上，对外统一前缀标识平台。
			"id":             displayClineModelID(m.ID),
			"object":         "model",
			"created":        time.Now().UnixMilli(),
			"status":         m.Status,
			"cost":           m.Cost,
			"requiresStream": m.RequiresStream,
			"syncedAt":       m.SyncedAt,
		}
		// 元数据是"尽力补全"的：拿不到就不出现这些键，别给客户端假信息。
		if m.Name != "" {
			item["name"] = m.Name
		}
		if m.ContextLength > 0 {
			item["context_length"] = m.ContextLength
		}
		if m.MaxOutputTokens > 0 {
			item["max_completion_tokens"] = m.MaxOutputTokens
			item["max_output_tokens"] = m.MaxOutputTokens
		}
		if len(m.InputModalities) > 0 || len(m.OutputModalities) > 0 {
			arch := map[string]any{}
			if len(m.InputModalities) > 0 {
				arch["input_modalities"] = m.InputModalities
			}
			if len(m.OutputModalities) > 0 {
				arch["output_modalities"] = m.OutputModalities
			}
			item["architecture"] = arch
		}
		if m.SupportsReasoning || len(m.ReasoningEfforts) > 0 {
			item["supports_reasoning"] = m.SupportsReasoning
			if len(m.ReasoningEfforts) > 0 {
				item["reasoning_efforts"] = m.ReasoningEfforts
			}
		}
		if m.SupportsTools {
			item["supports_tools"] = true
		}
		if m.SupportsStructuredOutput {
			item["supports_structured_output"] = true
		}
		if m.MetaSource != "" {
			item["metadata_source"] = m.MetaSource
		}
		out = append(out, item)
	}
	return out
}

// normalizeModelEntryFields 把单条 /v1/models 条目的上下文类字段统一为业界
// 标准形态（映射规则见 proxy.go 调用处的注释）。已存在的标准字段不覆盖
// （上游权威优先）。
func normalizeModelEntryFields(m map[string]any) {
	if _, ok := m["context_length"]; !ok {
		if n := modelEntryInt(m["context"]); n > 0 {
			m["context_length"] = n
		}
	}
	if _, ok := m["max_output_tokens"]; !ok {
		if n := modelEntryInt(m["output"]); n > 0 {
			m["max_output_tokens"] = n
		}
	}
}

// modelEntryInt 宽松取整：来源字段可能是 int/int64/float64（JSON 解码产物）。
func modelEntryInt(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// modelsSyncSnapshot 无锁安全地取同步状态。modelsLastSync 是 time.Time（三个
// 字），syncRecommendedModels 持锁写它；admin 面板直接读字段会拿到撕裂值，
// 也会被 -race 判为 data race。
func modelsSyncSnapshot() (lastSync time.Time, syncing bool) {
	initModelsCache()
	modelsMu.Lock()
	defer modelsMu.Unlock()
	return modelsLastSync, modelsSyncing
}

func ensureModelsFresh() {
	initModelsCache()
	modelsMu.Lock()
	needSync := modelsLastSync.IsZero() || time.Since(modelsLastSync) > modelsRefreshInterval
	syncing := modelsSyncing
	modelsMu.Unlock()
	if needSync && !syncing {
		go syncModelsOnce()
	}
}

func startModelsRefresher() {
	go func() {
		syncModelsOnce()
		ticker := time.NewTicker(modelsRefreshInterval)
		for range ticker.C {
			syncModelsOnce()
		}
	}()
}

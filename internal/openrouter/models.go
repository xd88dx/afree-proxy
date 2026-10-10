package openrouter

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============ 免费模型目录 ============
//
// 事实源：openrouter.ai/collections/free-models 页面列出的模型（2026-10-10
// 实测 15 个免费生成模型 + 官方路由 openrouter/free）。使用率低/性能不足的
// 其他零价模型（embedding / rerank / 图像 / 安全分类器等）不在页面上，一律
// 不收 —— 页面集合是唯一白名单。同步时对 API 的 pricing 全零结果与该集合求
// 交集：上游新增的免费模型在页面收录前不进入目录，页面列了但 API 还没上的
// 也不进入（请求会 404）。冷启动以页面快照兜底（离线可用）。

// CatalogURL 公共模型目录端点（免认证，含 pricing，用于交叉校验）。
const CatalogURL = "https://openrouter.ai/api/v1/models"

// PageModels openrouter.ai/collections/free-models 页面列出的免费模型快照
// （2026-10-10 实测）。它是可服务模型的唯一白名单；live 同步只做
// "价格仍为零"的交叉校验与限额字段更新，不引入页面之外的新模型。
var PageModels = []Model{
	{ID: "openrouter/free", Name: "Free Models Router (official fallback)", Context: 200000, ToolCall: true},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b:free", Name: "NVIDIA: Nemotron 3 Ultra (free)", Context: 1000000},
	{ID: "thinkingmachines/inkling:free", Name: "Thinking Machines: Inkling (free)", Context: 1048576},
	{ID: "thinkingmachines/inkling-small:free", Name: "Thinking Machines: Inkling Small (free)", Context: 1048576},
	{ID: "nvidia/nemotron-3.5-lightning:free", Name: "NVIDIA: Nemotron 3.5 Lightning (free)", Context: 1000000},
	{ID: "dots-studio/dots-3-note-preview:free", Name: "Dots Studio: Dots3-Note Preview (free)", Context: 512000},
	{ID: "inclusionai/ling-3.1-flash", Name: "inclusionAI: Ling 3.1 Flash", Context: 262144},
	{ID: "nvidia/nemotron-3-super-120b-a12b:free", Name: "NVIDIA: Nemotron 3 Super (free)", Context: 262144},
	{ID: "nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free", Name: "NVIDIA: Nemotron 3 Nano Omni (free)", Context: 256000},
	{ID: "poolside/laguna-s-2.1:free", Name: "Poolside: Laguna S 2.1 (free)", Context: 262144},
	{ID: "poolside/laguna-xs-2.1:free", Name: "Poolside: Laguna XS 2.1 (free)", Context: 262144},
	{ID: "apodex/apodex-1.1-mini:free", Name: "Apodex: Apodex 1.1 Mini (free)", Context: 262144},
	{ID: "cohere/north-mini-code:free", Name: "Cohere: North Mini Code (free)", Context: 256000},
	{ID: "inception/mercury-decide:free", Name: "Inception: Mercury Decide (free)", Context: 262144},
	{ID: "liquid/lfm-2.5-2.6b:free", Name: "LiquidAI: LFM2.5-2.6B (free)", Context: 65536},
}

// pageSet 页面集合（同步时的白名单；键为模型 ID）。
var pageSet = func() map[string]bool {
	m := make(map[string]bool, len(PageModels))
	for _, e := range PageModels {
		m[e.ID] = true
	}
	return m
}()

// Overrides 便于测试注入。
var (
	catalogMu     sync.RWMutex
	catalogModels = map[string]Model{}
	catalogLoaded bool
	catalogSyncMu sync.Mutex

	CatalogURLOverride string
	CatalogClient      *http.Client
)

func catalogEndpoint() string {
	if CatalogURLOverride != "" {
		return CatalogURLOverride
	}
	return CatalogURL
}

func catalogHTTPClient() *http.Client {
	if CatalogClient != nil {
		return CatalogClient
	}
	return &http.Client{Timeout: 25 * time.Second}
}

// pricingAllZero 判断一条 pricing 记录是否全零（免费）。字符串解析失败视为不免费。
func pricingAllZero(m map[string]any) bool {
	for _, k := range []string{"prompt", "completion", "request"} {
		v, ok := m[k]
		if !ok {
			continue
		}
		s, isStr := v.(string)
		if !isStr {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" || s == "0" {
			continue
		}
		// 非零字符串（含科学计数法）一律不免费
		return false
	}
	return true
}

type catalogEntry struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Context   int            `json:"context_length"`
	Pricing   map[string]any `json:"pricing"`
	Supported []string       `json:"supported_parameters"`
}

// InitCatalog 初始化目录：已加载过则立即返回；否则用页面快照兜底，再异步
// 交叉校验（live 价格为零 + 限额更新）。失败保留快照（离线可用）。
func InitCatalog() {
	catalogMu.RLock()
	ready := catalogLoaded
	catalogMu.RUnlock()
	if ready {
		return
	}
	seed := map[string]Model{}
	for _, m := range PageModels {
		seed[m.ID] = m
	}
	catalogMu.Lock()
	if catalogLoaded {
		catalogMu.Unlock()
		return
	}
	catalogModels = seed
	catalogLoaded = true
	catalogMu.Unlock()
	go func() { _, _ = SyncCatalog() }()
}

// SyncCatalog 拉取 live 目录并与页面集合求交集：
//   - 页面集合里的模型，live 显示价格非零（已取消免费）→ 剔除；
//   - 页面集合里的模型，live 有更新限额/名称 → 就地更新；
//   - 页面之外的价格为零模型 → 不收（页面白名单是唯一权威）；
//   - 网络/解析失败 → 保留当前目录（不清空快照）。
//
// 返回"价格状态发生变化"的模型数（日志用）。
func SyncCatalog() (int, error) {
	catalogSyncMu.Lock()
	defer catalogSyncMu.Unlock()
	req, err := http.NewRequest(http.MethodGet, catalogEndpoint(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := catalogHTTPClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, &http.ProtocolError{ErrorString: "catalog status " + resp.Status}
	}
	var payload struct {
		Data []catalogEntry `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	live := map[string]catalogEntry{}
	for _, e := range payload.Data {
		if id := strings.TrimSpace(e.ID); id != "" {
			live[id] = e
		}
	}
	catalogMu.Lock()
	defer catalogMu.Unlock()
	changed := 0
	next := make(map[string]Model, len(catalogModels))
	for id, cur := range catalogModels {
		e, ok := live[id]
		if !ok {
			// 上游还没上架（或临时不可达）：保留快照，价格状态未知不剔除
			next[id] = cur
			continue
		}
		if !pricingAllZero(e.Pricing) {
			// 页面仍列着但价格已非零：该模型不再是免费模型，剔除
			changed++
			continue
		}
		if e.Context > 0 {
			cur.Context = e.Context
		}
		if e.Name != "" {
			cur.Name = e.Name
		}
		for _, p := range e.Supported {
			if p == "tools" || p == "tool_choice" {
				cur.ToolCall = true
				break
			}
		}
		next[id] = cur
	}
	if len(next) == 0 {
		return 0, nil
	}
	catalogModels = next
	return changed, nil
}

// ModelList 排序后的免费模型列表（面板与 /v1/models 共用）。
func ModelList() []Model {
	InitCatalog()
	catalogMu.RLock()
	out := make([]Model, 0, len(catalogModels))
	for _, m := range catalogModels {
		out = append(out, m)
	}
	catalogMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Resolve 解析裸模型 ID（去前缀由调用方处理）。不在目录中即拒绝 —— 未知模型
// 显式 400，不静默改写。
func Resolve(id string) (Model, bool) {
	InitCatalog()
	catalogMu.RLock()
	m, ok := catalogModels[strings.TrimSpace(id)]
	catalogMu.RUnlock()
	return m, ok
}

// ProbeModel 探测用模型：优先官方路由 openrouter/free（官方兜底，最稳），
// 其次排序最小的可用模型。
func ProbeModel() (Model, bool) {
	list := ModelList()
	if len(list) == 0 {
		return Model{}, false
	}
	for _, m := range list {
		if m.ID == "openrouter/free" {
			return m, true
		}
	}
	return list[0], true
}

// Count 当前目录规模（日志/测试用）。
func Count() int {
	InitCatalog()
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	return len(catalogModels)
}

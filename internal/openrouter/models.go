package openrouter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============ 免费模型目录 ============
//
// 事实源：openrouter.ai/api/v1/models live 目录（零价 + chatCapable 判据，
// 2026-11-05 起为唯一权威）。收录面与官方 free 页的对话模型口径对齐：
// 零价集合里的 embedding / rerank / 图像 / 安全分类器等非对话杂类不收。
// 冷启动快照（PageModels/SeedModels）已于 2026-11-11 用户拍板整机制取消
//（与 cline extraFreeModels 同款处理）：目录如实反映 live 同步结果，宁空
// 勿过期快照——官方会把模型移出免费层，快照会把已计费模型当免费对外。
// 空目录期间请求按 strict-gate fail-closed 拒；startOrModelsRefresher 的
// 30s×10 快速重试链负责尽快完成首拉。

// CatalogURL 公共模型目录端点（免认证，含 pricing，用于交叉校验）。
const CatalogURL = "https://openrouter.ai/api/v1/models"

// Overrides 便于测试注入。
var (
	catalogMu     sync.RWMutex
	catalogModels = map[string]Model{}
	catalogLoaded bool
	catalogSyncMu sync.Mutex

	CatalogClient *http.Client

	// catalogOverrideMu 保护目录端点覆盖值：InitCatalog 的异步首拉 goroutine
	// 会与测试的注入/恢复并发读写，裸包级变量在 -race 下必红（2026-11-11
	// CI 实测）。存取一律走 SetCatalogURLOverride / catalogEndpoint。
	catalogOverrideMu     sync.RWMutex
	catalogURLOverrideVal string
)

// SetCatalogURLOverride 覆盖公共目录端点（测试注入/特殊部署用；空串 = 用 CatalogURL）。
func SetCatalogURLOverride(v string) {
	catalogOverrideMu.Lock()
	catalogURLOverrideVal = v
	catalogOverrideMu.Unlock()
}

// GetCatalogURLOverride 读取当前覆盖值（测试保存/恢复用）。
func GetCatalogURLOverride() string {
	catalogOverrideMu.RLock()
	defer catalogOverrideMu.RUnlock()
	return catalogURLOverrideVal
}

func catalogEndpoint() string {
	if v := GetCatalogURLOverride(); v != "" {
		return v
	}
	return CatalogURL
}

func catalogHTTPClient() (*http.Client, error) {
	if CatalogClient != nil {
		return CatalogClient, nil
	}
	// 目录同步与 chat 同链路：按 key 绑定/全局规则解析代理出口取 client。
	// 此前此处是裸直连——CF 对直连/uTLS 指纹连接的间歇重置会让目录同步偶发
	// 失败（与 tokenharbor/amd 同现象，tokenharbor 曾因此连续 16h 失败）。
	// 逐 key 试出口：停用的 key 不参与路由（等同不在池里），解析失败（含
	// DISABLE_DIRECT 判否）换下一个；面板显式选直连（resolveExit 返回空但
	// ok）按显式直连取 client；全部不可用时 fail-closed 返回错误，本轮同步
	// 跳过、下轮再试——绝不隐式回落直连（2026-10-29 审计修复）。
	for _, key := range Keys() {
		if !RoutingEnabled(key) {
			continue
		}
		proxyURL, ok := resolveExit(key)
		if !ok {
			continue
		}
		return egressClient(proxyURL), nil
	}
	// key 全禁用/无 key：退到平台全局出口规则（app 注入 resolver 的空 key 语义
	// = 未绑定 key 同款：走池/显式直连/DISABLE_DIRECT fail-closed）。目录端点
	// 免认证，凭据全禁不应掐死公开数据的同步——2026-11-05 生产空窗实证：备份
	// 导入后 key 全禁用，live 目录同步持续失败，快照外模型 404 达 5 分钟。
	proxyURL, ok := resolveExit("")
	if !ok {
		return nil, fmt.Errorf("no routable exit for catalog sync (keys cooling/disabled, global egress blocked by DISABLE_DIRECT)")
	}
	return egressClient(proxyURL), nil
}

// egressClient 目录同步的出站 client：代理出口走池 client 原样；显式直连
// （空 URL）用 25s 超时的直连拷贝。
func egressClient(proxyURL string) *http.Client {
	if proxyURL == "" {
		dc := *httpClientFor(proxyURL)
		dc.Timeout = 25 * time.Second
		return &dc
	}
	return httpClientFor(proxyURL)
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
	Arch      struct {
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
}

// chatCapable live 收录判据（2026-11-05 快照白名单退役）：输出模态含 text
// 且声明 tools/tool_choice——本网关是 chat/agent 网关，收录面与官方 free 页
// 的对话模型口径对齐。零价集合里的非对话杂类被它排除：nemotron content-safety
// 分类器（无 tools 参数）、lyria 音频模型（output 含 audio 且无 tools）。
// 输出模态缺失（旧字段形态）按含 text 处理，不误杀。
func chatCapable(e catalogEntry) bool {
	hasText := true // 缺失 = 不判
	if len(e.Arch.OutputModalities) > 0 {
		hasText = false
		for _, m := range e.Arch.OutputModalities {
			if m == "text" {
				hasText = true
				break
			}
		}
	}
	if !hasText {
		return false
	}
	for _, p := range e.Supported {
		if p == "tools" || p == "tool_choice" {
			return true
		}
	}
	return false
}

// InitCatalog 初始化目录：已加载过则立即返回。冷启动不注入任何快照
// （2026-11-11 用户定，与 cline 同口径：目录如实反映 live 同步结果，宁空
// 勿过期快照）。空目录期间请求按 strict-gate fail-closed 拒；异步 SyncCatalog
// 完成首拉后目录即为 live 权威态，startOrModelsRefresher 的快速重试链保证
// 首拉失败也尽快补齐。
func InitCatalog() {
	catalogMu.RLock()
	ready := catalogLoaded
	catalogMu.RUnlock()
	if ready {
		return
	}
	catalogMu.Lock()
	if catalogLoaded {
		catalogMu.Unlock()
		return
	}
	catalogModels = map[string]Model{}
	catalogLoaded = true
	catalogMu.Unlock()
	go func() { _, _ = SyncCatalog() }()
}

// SyncCatalog 拉取 live 目录，live 为唯一权威（2026-11-05 快照白名单退役，
// 与 amd/tkhb/zen/cline 同款口径）：
//   - live pricing 全零 + chatCapable（输出含 text 且支持 tools）→ 收录，
//     上游新增免费模型自动进目录（此前卡在 2026-10-10 页面快照里，
//     stealth/crouton-alpha 等页面上新被永久挡住——静态快照当永久白名单
//     是反模式，页面会变、快照不会）；
//   - live 价格非零（已取消免费）→ 剔除（含快照时代的收录项）；
//   - 网络/解析失败 → 保留当前目录（不清空，离线可用）。
//
// PageModels 快照已于 2026-11-11 整机制取消（冷启动目录为空，见 InitCatalog）。
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
	client, err := catalogHTTPClient()
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
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
	catalogMu.Lock()
	defer catalogMu.Unlock()
	changed := 0
	next := make(map[string]Model, len(payload.Data))
	for _, e := range payload.Data {
		id := strings.TrimSpace(e.ID)
		if id == "" || !chatCapable(e) {
			continue
		}
		if !pricingAllZero(e.Pricing) {
			// 仍留在 live 里但价格已非零：不再免费，剔除（上轮收录项也走这）
			if _, was := catalogModels[id]; was {
				changed++
			}
			continue
		}
		toolCall := false
		for _, p := range e.Supported {
			if p == "tools" || p == "tool_choice" {
				toolCall = true
				break
			}
		}
		name := e.Name
		if name == "" {
			name = id
		}
		m := Model{ID: id, Name: name, Context: e.Context, ToolCall: toolCall}
		if cur, ok := catalogModels[id]; ok && m != cur {
			changed++
		}
		next[id] = m
	}
	if len(next) == 0 {
		// live 全空（异常响应）：保留当前目录，不清空
		return 0, nil
	}
	catalogModels = next
	return changed, nil
}

// ModelList 排序后的免费模型列表（面板与 /v1/models 共用）。展示门控：
// 没有参与路由的启用 key 时返回空列表——模型目录只列可服务条目，否则
// 客户端选中后必然 401/403（与 amd/tokenharbor/trae 同口径）。
func ModelList() []Model {
	InitCatalog()
	if !HasEnabledKey() {
		return []Model{}
	}
	catalogMu.RLock()
	out := make([]Model, 0, len(catalogModels))
	for _, m := range catalogModels {
		out = append(out, m)
	}
	catalogMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// HasEnabledKey 是否存在参与路由的启用 key。模型目录的展示门控：无启用
// key 既发不出请求也不该展示（面板与 /v1/models 同一判定）。
func HasEnabledKey() bool {
	for _, k := range Keys() {
		if RoutingEnabled(k) {
			return true
		}
	}
	return false
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

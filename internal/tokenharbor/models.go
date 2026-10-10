package tokenharbor

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============ 模型目录 ============
//
// 事实源：上游 GET /v1/models（需要 key，与其他 /v1 调用一致）。免费模型以
// ":free" 后缀标识（models?category=free 页面列出的条目全部如此），同步时
// 只保留 :free 条目 —— 付费模型一律不收。未配置 key 或同步失败时以种子快照
// 兜底（离线也能列出可用模型）。

// Overrides 便于测试注入。
var (
	catalogMu     sync.RWMutex
	catalogModels = map[string]Model{}
	catalogLoaded bool
	catalogSyncMu sync.Mutex

	CatalogClient *http.Client
)

func catalogHTTPClient() *http.Client {
	if CatalogClient != nil {
		return CatalogClient
	}
	return &http.Client{Timeout: 25 * time.Second}
}

type catalogEntry struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length"`
	ContextWindow int    `json:"context_window"`
}

// InitCatalog 初始化目录：已加载过则立即返回；否则用种子快照兜底，再异步
// 同步 live 目录（需 key）。失败保留快照（离线可用）。
func InitCatalog() {
	catalogMu.RLock()
	ready := catalogLoaded
	catalogMu.RUnlock()
	if ready {
		return
	}
	seed := map[string]Model{}
	for _, m := range SeedModels {
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

// SyncCatalog 用池内第一个可用 key 拉取 live 目录，只保留 :free 条目。
// 返回变更条数；无 key 配置时静默跳过（种子即目录）；网络/解析失败返回
// 错误且保留当前目录。上游返回空集合同样不清空。
func SyncCatalog() (int, error) {
	catalogSyncMu.Lock()
	defer catalogSyncMu.Unlock()
	keys := Get().Keys
	if len(keys) == 0 {
		return 0, nil // 无 key：目录保持种子（同步需要认证）
	}
	base := Get().BaseURL
	req, err := http.NewRequest(http.MethodGet, base+"/models", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+keys[0])
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
	next := map[string]Model{}
	for _, e := range payload.Data {
		id := strings.TrimSpace(e.ID)
		if id == "" || !strings.HasSuffix(id, FreeSuffix) {
			continue // 只收免费模型（页面白名单口径）
		}
		ctx := e.ContextLength
		if ctx == 0 {
			ctx = e.ContextWindow
		}
		next[id] = Model{ID: id, Name: id, Context: ctx}
	}
	if len(next) == 0 {
		// 上游没有 :free 条目（或返回形态变化）：保留当前目录
		return 0, nil
	}
	catalogMu.Lock()
	changed := 0
	for id, m := range next {
		if cur, ok := catalogModels[id]; !ok || cur != m {
			changed++
		}
	}
	catalogModels = next
	catalogMu.Unlock()
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

// ProbeModel 探测用模型：优先 deepseek-v4.1-flash:free（与 zen/amd 平台的
// DeepSeek 探针同思路，快且稳），其次排序最小的可用模型。
func ProbeModel() (Model, bool) {
	list := ModelList()
	if len(list) == 0 {
		return Model{}, false
	}
	for _, m := range list {
		if m.ID == "deepseek-v4.1-flash:free" {
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

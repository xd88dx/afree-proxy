package amd

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
// 事实源：上游 GET /v1/models（免认证实测可用，返回全部 Public Free Model
// APIs 条目及其限额元数据）。这些端点按免费额度计费，无价格门可言；同步只
// 做"输出模态含 text"的过滤（OCR 专用模型不适合 chat 网关），冷启动以种子
// 快照兜底（离线也能列出可用模型）。

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
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Context   int      `json:"context_length"`
	Output    []string `json:"output"`
	Providers []struct {
		Tools bool `json:"tools"`
	} `json:"providers"`
}

// chatCapable 输出模态含 text 才可对话（MinerU2.5-Pro 输出 ocr，排除）。
func chatCapable(e catalogEntry) bool {
	if len(e.Output) == 0 {
		return true
	}
	for _, o := range e.Output {
		if strings.EqualFold(o, "text") {
			return true
		}
	}
	return false
}

// InitCatalog 初始化目录：已加载过则立即返回；否则用种子快照兜底，再异步
// 同步 live 目录。失败保留快照（离线可用）。
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

// SyncCatalog 拉取 live 模型目录（免认证）。返回变更条数；网络或解析失败
// 时返回错误且保留当前目录（不清空种子）。上游空列表同样不清空。
func SyncCatalog() (int, error) {
	catalogSyncMu.Lock()
	defer catalogSyncMu.Unlock()
	base := Get().BaseURL
	req, err := http.NewRequest(http.MethodGet, base+"/models", nil)
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
	next := map[string]Model{}
	for _, e := range payload.Data {
		id := strings.TrimSpace(e.ID)
		if id == "" || !chatCapable(e) {
			continue
		}
		tools := false
		for _, p := range e.Providers {
			if p.Tools {
				tools = true
				break
			}
		}
		name := e.Name
		if name == "" {
			name = id
		}
		next[id] = Model{ID: id, Name: name, Context: e.Context, ToolCall: tools}
	}
	if len(next) == 0 {
		// 上游返回空集合：保留当前目录，避免把可用列表清空
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

// ProbeModel 探测用模型：优先 DeepSeek-V4-Flash（官方文档示例模型，最稳），
// 其次排序最小的可用模型。
func ProbeModel() (Model, bool) {
	list := ModelList()
	if len(list) == 0 {
		return Model{}, false
	}
	for _, m := range list {
		if m.ID == "DeepSeek-V4-Flash" {
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

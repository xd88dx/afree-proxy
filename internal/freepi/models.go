package freepi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============ 模型目录 ============
//
// 事实源：上游 GET {APIOrigin}/client-version（注意在根域，不在 /v1 下；
// 需要 key 认证）。响应 {models:[{id,name,unavailable?}]}，name 自带倍率
// 标注（如 "Solar Pro 4 · ~0.6x (cheapest)"）。拉取结果整体替换 Fetched；
// 拉不到回落 staticModels（2026-11-10 目录快照，参考 plugin MODELS）。
// 勾选暴露机制刻意没有（other 有）：目录即上游真值，全量暴露。

// staticModels 静态兜底目录（plugin 2026-11-10 抓取的 17 条）。
var staticModels = []Model{
	{ID: "particle", Name: "Particle · ~1x", Context: 128000},
	{ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash · 1x", Context: 128000},
	{ID: "glm-5.3-flash", Name: "GLM 5.3 Flash · ~1.5x", Context: 128000},
	{ID: "gpt-6-luna", Name: "GPT-6 Luna · ~2x", Context: 128000},
	{ID: "gpt-6-luna-flex", Name: "GPT-6 Luna Flex · ~2x · slower", Context: 128000},
	{ID: "mimo-v2.6-flash", Name: "MiMo V2.6 Flash · ~2.8x", Context: 128000},
	{ID: "qwen3.5-35b-darkbloom", Name: "Qwen3.5 35B Darkbloom · ~2.9x", Context: 128000},
	{ID: "deepseek-v4.1-flash", Name: "DeepSeek V4.1 Flash · ~3x", Context: 128000},
	{ID: "gpt-oss-120b-speed", Name: "GPT-OSS 120B · ~3x", Context: 128000},
	{ID: "muse-spark-1.3-contributor", Name: "Muse Spark 1.3 Contributor (trains on your data) · ~2x", Context: 128000},
	{ID: "solar-pro-4", Name: "Solar Pro 4 · ~0.6x (cheapest)", Context: 32000},
	{ID: "laguna-s-2.1", Name: "Laguna S 2.1 · free preview then ~1.8x", Context: 128000},
	{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro · ~18x", Context: 128000},
	{ID: "gpt-6.1-terra", Name: "GPT-6.1 Terra · ~20x", Context: 128000},
	{ID: "gpt-6.1-terra-flex", Name: "GPT-6.1 Terra Flex · ~20x · slower", Context: 128000},
	{ID: "gpt-6.1-sol-flex", Name: "GPT-6.1 Sol Flex · ~41x", Context: 128000},
	{ID: "kimi-k3.1-pro", Name: "Kimi K3.1 Pro · daily limit · ~41x", Context: 128000},
}

// CatalogClient 覆盖 HTTP client（测试注入）；nil = 按 catalogExit 解析出口。
var CatalogClient *http.Client

// catalogExit 目录同步的出站解析（主包装配期注入），语义同 Chat 的 exitFn：
// 未绑定 key 走全局规则，绑定 key 走主备 fail-closed，DISABLE_DIRECT 判否。
// 未注入（单测/独立使用）时退回直连默认 client。
var catalogExit func(key string) (string, bool)

// SetCatalogExitResolver 注入目录同步的出站解析（主包装配期调用）。
func SetCatalogExitResolver(f func(key string) (string, bool)) { catalogExit = f }

func defaultCatalogClient() *http.Client { return &http.Client{Timeout: 25 * time.Second} }

// catalogClientFor 一次定妥认证 key 与 HTTP client：key 与出口必须配对选取，
// 逐个已启用 key 试出口（停用 key 等同不在池里）；全部不可用即 fail-closed，
// 本轮同步跳过、下轮再试——绝不隐式回落直连（2026-11-09 补齐的 other 同款）。
func catalogClientFor(keys []string) (string, *http.Client, error) {
	if CatalogClient != nil {
		return keys[0], CatalogClient, nil
	}
	if catalogExit == nil {
		return keys[0], defaultCatalogClient(), nil
	}
	for _, k := range keys {
		proxyURL, ok := catalogExit(k)
		if !ok {
			continue
		}
		if proxyURL == "" {
			return k, defaultCatalogClient(), nil
		}
		return k, httpClientFor(proxyURL), nil
	}
	return "", nil, fmt.Errorf("no routable exit for catalog sync (keys disabled or DISABLE_DIRECT)")
}

// FetchModels 用池内已启用 key 拉取上游模型目录（GET /client-version）。
// 无 key/无启用 key 时返回错误（目录保持现有内容，不清空）。
func FetchModels() ([]Model, error) {
	mu.RLock()
	keys := make([]string, 0, len(cfg.Keys))
	for _, k := range cfg.Keys {
		if cfg.RoutingEnabled(k) {
			keys = append(keys, k)
		}
	}
	anyKey := len(cfg.Keys) > 0
	mu.RUnlock()
	if !anyKey {
		return nil, fmt.Errorf("no key configured: add a credential first")
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no enabled credential: enable a key in the pool, then sync")
	}
	authKey, client, err := catalogClientFor(keys)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, APIOrigin+"/client-version", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+authKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &http.ProtocolError{ErrorString: "catalog status " + resp.Status}
	}
	var payload struct {
		Models []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Unavailable bool   `json:"unavailable"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(payload.Models))
	seen := map[string]bool{}
	for _, e := range payload.Models {
		id := strings.TrimSpace(e.ID)
		if id == "" || e.Unavailable || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = id
		}
		out = append(out, Model{ID: id, Name: name})
	}
	if len(out) == 0 {
		// 上游返回空集合：视为无变化，保留现有目录
		return nil, nil
	}
	return out, nil
}

// KnownModels 全部已知模型（Fetched ∪ 静态兜底，按 ID 排序）。静态表兜底
// 刻意参与：/client-version 拉不到时 /v1/models 与请求判定仍有完整目录。
func KnownModels() []Model {
	mu.RLock()
	fetched := append([]Model(nil), cfg.Models.Fetched...)
	mu.RUnlock()
	seen := map[string]bool{}
	out := make([]Model, 0, len(fetched)+len(staticModels))
	for _, m := range fetched {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	for _, m := range staticModels {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ResolveModel 解析裸模型 ID：必须已知（目录成员判定，与「列出来的必须
// 调得通」对称）——未知模型显式拒绝，不静默改写。
func ResolveModel(id string) (Model, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Model{}, false
	}
	for _, m := range KnownModels() {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// ProbeModel 探测用模型：glm-5.3-flash（plugin 的 provider test 同款）优先，
// 否则目录排序第一个。目录恒非空（静态兜底），false 仅在目录异常时出现。
func ProbeModel() (Model, bool) {
	list := KnownModels()
	if m, ok := ResolveModel("glm-5.3-flash"); ok {
		return m, true
	}
	if len(list) == 0 {
		return Model{}, false
	}
	return list[0], true
}

// SetFetched 整体替换拉取目录（同步成功时调用）。返回新增/变更条数。
func SetFetched(fetched []Model) int {
	mu.Lock()
	defer mu.Unlock()
	cur := map[string]Model{}
	for _, m := range cfg.Models.Fetched {
		cur[m.ID] = m
	}
	changed := 0
	for _, m := range fetched {
		if c, ok := cur[m.ID]; !ok || c != m {
			changed++
		}
	}
	cfg.Models.Fetched = fetched
	return changed
}

// syncMu 串行化目录同步（面板 Refresh 与周期刷新共用）。
var (
	syncMu  sync.Mutex
	syncing bool
)

// TrySync 做一次目录同步（无 key/已在同步中时静默跳过）。返回 (changed, err)；
// changed>0 表示目录有更新。写回与其他 key 池平台同款：SetFetched 在 mu 内
// 改活 cfg 即可——目录是易失缓存（启动即重同步），落盘跟随下一次任意面板
// Set，无独立保存路径。
func TrySync() (int, error) {
	syncMu.Lock()
	if syncing {
		syncMu.Unlock()
		return 0, nil
	}
	syncing = true
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		syncing = false
		syncMu.Unlock()
	}()
	fetched, err := FetchModels()
	if err != nil {
		return 0, err
	}
	if fetched == nil {
		return 0, nil
	}
	changed := SetFetched(fetched)
	return changed, nil
}

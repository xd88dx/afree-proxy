package other

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
// 事实源：上游 GET {baseURL}/models（OpenAI 兼容，需要 key —— 用池内第一个
// key 认证）。拉取结果整体替换 Fetched；Manual 为手动添加的模型（常驻，
// 供上游不规范或无 models 端点的小平台使用）。Known = Fetched ∪ Manual。
// Selected 勾选表按模型 ID 记录，上游模型上下线不清勾选 —— 只勾选的
// 模型对外暴露（/v1/models 列出、网关放行），未勾选显式 400。

// CatalogClient 覆盖 HTTP client（测试注入）；nil = 按 catalogExit 解析出口。
var CatalogClient *http.Client

// catalogExit 目录同步的出站解析（主包装配期注入），语义同 Chat 的 exitFn：
// 未绑定 key 走全局规则，绑定 key 走主备 fail-closed，DISABLE_DIRECT 判否。
// 未注入（单测/独立使用）时退回直连默认 client。
var catalogExit func(p *Platform, key string) (string, bool)

// SetCatalogExitResolver 注入目录同步的出站解析（主包装配期调用）。
func SetCatalogExitResolver(f func(p *Platform, key string) (string, bool)) {
	catalogExit = f
}

func defaultCatalogClient() *http.Client { return &http.Client{Timeout: 25 * time.Second} }

// catalogClientFor 一次定妥认证 key 与 HTTP client：key 与出口必须配对选取，
// 逐个已启用 key 试出口（停用 key 等同不在池里）；全部不可用即 fail-closed，
// 本轮同步跳过、下轮再试——绝不隐式回落直连（与 amd/tokenharbor 同口径）。
func catalogClientFor(p *Platform, keys []string) (string, *http.Client, error) {
	if CatalogClient != nil {
		return keys[0], CatalogClient, nil
	}
	if catalogExit == nil {
		return keys[0], defaultCatalogClient(), nil
	}
	for _, k := range keys {
		proxyURL, ok := catalogExit(p, k)
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

type catalogEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	ContextWindow int    `json:"context_window"`
	Tools         bool   `json:"-"`
}

// FetchModels 用平台池内第一个 key 拉取上游模型目录。无 key 时返回错误
// （目录保持现有内容，不清空）。
func FetchModels(p *Platform) ([]Model, error) {
	mu.RLock()
	base := strings.TrimRight(p.BaseURL, "/")
	anyKey := len(p.Keys) > 0
	// 凭据口径与真实会话一致：只取已启用（参与路由）的 key，在锁内连同
	// 出口解析一起取出，避免解锁后再读 p.Keys 产生竞态。
	keys := make([]string, 0, len(p.Keys))
	for _, k := range p.Keys {
		if p.RoutingEnabled(k) {
			keys = append(keys, k)
		}
	}
	mu.RUnlock()
	if !anyKey {
		return nil, fmt.Errorf("no key configured: add a credential first")
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no enabled credential: enable a key in the pool, then sync")
	}
	if base == "" {
		return nil, fmt.Errorf("base URL is empty")
	}
	authKey, client, err := catalogClientFor(p, keys)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, base+"/models", nil)
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
		Data []catalogEntry `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(payload.Data))
	seen := map[string]bool{}
	for _, e := range payload.Data {
		id := strings.TrimSpace(e.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(e.Name)
		if name == "" {
			name = id
		}
		ctx := e.ContextLength
		if ctx == 0 {
			ctx = e.ContextWindow
		}
		out = append(out, Model{ID: id, Name: name, Context: ctx})
	}
	if len(out) == 0 {
		// 上游返回空集合：视为无变化，保留现有目录
		return nil, nil
	}
	return out, nil
}

// KnownModels 平台全部已知模型（Fetched ∪ Manual，按 ID 排序）。
func KnownModels(p *Platform) []Model {
	mu.RLock()
	fetched := append([]Model(nil), p.Models.Fetched...)
	manual := append([]string(nil), p.Models.Manual...)
	mu.RUnlock()
	seen := map[string]bool{}
	out := make([]Model, 0, len(fetched)+len(manual))
	for _, m := range fetched {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		out = append(out, m)
	}
	for _, id := range manual {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Model{ID: id, Name: id})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ModelSelected 模型是否勾选（对外暴露）。
func ModelSelected(p *Platform, id string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return p.Models.Selected[strings.TrimSpace(id)]
}

// SelectedModels 勾选的已知模型（按 ID 排序）。
func SelectedModels(p *Platform) []Model {
	known := KnownModels(p)
	mu.RLock()
	selected := p.Models.Selected
	mu.RUnlock()
	out := make([]Model, 0, len(known))
	for _, m := range known {
		if selected[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// SelectedModelIDs 勾选模型的 ID 列表（按 ID 排序；面板多选框回显用）。
func SelectedModelIDs(p *Platform) []string {
	ids := make([]string, 0)
	for _, m := range SelectedModels(p) {
		ids = append(ids, m.ID)
	}
	return ids
}

// ResolveSelected 解析裸模型 ID：必须已勾选才放行 —— 未勾选/未知模型显式
// 拒绝，不静默改写。
func ResolveSelected(p *Platform, id string) (Model, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Model{}, false
	}
	mu.RLock()
	ok := p.Models.Selected[id]
	mu.RUnlock()
	if !ok {
		return Model{}, false
	}
	for _, m := range KnownModels(p) {
		if m.ID == id {
			return m, true
		}
	}
	// 勾选表与目录短暂不一致（理论不应发生）：以勾选为准合成条目
	return Model{ID: id, Name: id}, true
}

// ProbeModel 探测用模型：面板 Probe 优先（须已勾选），否则勾选列表中
// 排序第一个。无勾选模型时返回 false（Test 按钮应提示先勾选）。
func ProbeModel(p *Platform) (Model, bool) {
	mu.RLock()
	probe := strings.TrimSpace(p.Models.Probe)
	mu.RUnlock()
	if probe != "" {
		if m, ok := ResolveSelected(p, probe); ok {
			return m, true
		}
	}
	list := SelectedModels(p)
	if len(list) == 0 {
		return Model{}, false
	}
	return list[0], true
}

// SetFetched 整体替换拉取目录（同步成功时调用）。返回新增/变更条数。
func SetFetched(p *Platform, fetched []Model) int {
	changed := 0
	cur := map[string]Model{}
	for _, m := range p.Models.Fetched {
		cur[m.ID] = m
	}
	for _, m := range fetched {
		if c, ok := cur[m.ID]; !ok || c != m {
			changed++
		}
	}
	p.Models.Fetched = fetched
	return changed
}

// AddManual 手动添加模型（返回是否新增；重复返回 false）。新增后默认勾选。
func AddManual(p *Platform, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, m := range p.Models.Manual {
		if m == id {
			return false
		}
	}
	p.Models.Manual = append(p.Models.Manual, id)
	if p.Models.Selected == nil {
		p.Models.Selected = map[string]bool{}
	}
	p.Models.Selected[id] = true
	return true
}

// RemoveManual 移除手动添加的模型（拉取目录里的不受影响）。
func RemoveManual(p *Platform, id string) bool {
	id = strings.TrimSpace(id)
	for i, m := range p.Models.Manual {
		if m == id {
			p.Models.Manual = append(p.Models.Manual[:i], p.Models.Manual[i+1:]...)
			return true
		}
	}
	return false
}

// SetSelected 整体替换勾选表（面板多选框保存时调用；只保留已知模型）。
func SetSelected(p *Platform, ids []string) {
	known := map[string]bool{}
	for _, m := range KnownModels(p) {
		known[m.ID] = true
	}
	sel := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && known[id] {
			sel[id] = true
		}
	}
	p.Models.Selected = sel
}

// syncMu 串行化每平台的目录同步（面板 Refresh 与周期刷新共用）。
var (
	syncMu  sync.Mutex
	syncing = map[string]bool{}
)

// TrySync 对平台做一次目录同步（无 key / 已在同步中时静默跳过）。
// 返回 (changed, err)；changed>0 表示目录有更新。入口重取活指针，
// 避免对已被 Set 替换掉的旧平台对象写入。
func TrySync(p *Platform) (int, error) {
	live := PlatformBySlug(p.Slug)
	if live == nil {
		return 0, nil // 平台已被删除
	}
	syncMu.Lock()
	if syncing[p.Slug] {
		syncMu.Unlock()
		return 0, nil
	}
	syncing[p.Slug] = true
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		delete(syncing, p.Slug)
		syncMu.Unlock()
	}()
	fetched, err := FetchModels(live)
	if err != nil {
		return 0, err
	}
	if fetched == nil {
		return 0, nil
	}
	mu.Lock()
	changed := SetFetched(live, fetched)
	mu.Unlock()
	return changed, nil
}

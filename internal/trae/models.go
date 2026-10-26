package trae

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============ 模型目录 ============
//
// 事实源：Trae 网关的 live 模型目录（web 端点，按启用账号的 JWT 拉取）；
// 离线兜底 = TraeCode 桌面客户端已知模型表（raw id 与 config_name 的映射
// 逐条来自上游 TraeWork 客户端实测，含 __v2 raw 名特例——这批名字是唯一
// 经过验证的 wire 事实，live 目录解析不到 raw 名时仍以本表为准）。

// Model 一个可服务模型：ID 对外暴露（trae:<ID> 调用），RawName 是发往上游
// 的 model_name（config_name 与 raw 名可能不同，如 glm-5.1 → glm-5__v2），
// Function 是 raw_chat_function（chat_v3 = IDE 主链路）。
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	RawName  string `json:"rawName,omitempty"`
	Function string `json:"function,omitempty"`
	Context  int    `json:"context,omitempty"`
	// Native 该 ID 在本账号的原生目录（chat_v3 / solo_agent）中出现过。
	// 只有原生档确认过的条目才对外展示：web Remote 目录的独有条目与种子
	// 里的 __v2 映射没有可用的 raw_chat_function，发裸 config_name 必报
	// 4023 model unknown（2026-10-26 实测 kimi-k2.6 / deepseek-v4-pro）。
	// 不落盘、不外发（-），仅用于展示过滤。
	Native bool `json:"-"`
}

// seedModels TraeCode 客户端已知模型（raw_client.py _KNOWN_MODELS 实测表）。
// config_name / raw_model_name / 显示名三列原样移植。
var seedModels = []Model{
	{ID: "glm-5.2", Name: "GLM-5.2", RawName: "glm-5.2", Function: "chat_v3"},
	{ID: "glm-5.3", Name: "GLM-5.3", RawName: "glm-5.3", Function: "chat_v3"},
	{ID: "glm-5.1", Name: "GLM-5.1", RawName: "glm-5__v2", Function: "chat_v3"},
	{ID: "kimi-k2.6", Name: "Kimi-K2.6", RawName: "kimi-k2.6__v2", Function: "chat_v3"},
	{ID: "kimi-k2.7-code", Name: "Kimi-K2.7-Code", RawName: "kimi-k2.7-code", Function: "chat_v3"},
	{ID: "deepseek-v4-pro", Name: "DeepSeek-V4-Pro", RawName: "DeepSeek-V4-Pro__v2", Function: "chat_v3"},
	{ID: "deepseek-v4-flash", Name: "DeepSeek-V4-Flash", RawName: "DeepSeek-V4-Flash__v2", Function: "chat_v3"},
	{ID: "deepseek-v4-pro-official", Name: "DeepSeek-V4-Pro 正式版", RawName: "DeepSeek-V4-Pro-Official", Function: "chat_v3"},
	{ID: "deepseek-v4-flash-official", Name: "DeepSeek-V4-Flash 正式版", RawName: "DeepSeek-V4-Flash-Official", Function: "chat_v3"},
	{ID: "minimax-m3", Name: "MiniMax M3", RawName: "minimax-m3", Function: "chat_v3"},
	{ID: "qwen-3.7-plus", Name: "Qwen 3.7 Plus", RawName: "qwen-3.7-plus", Function: "chat_v3"},
}

var (
	catalogMu     sync.RWMutex
	catalogModels = map[string]Model{}
	catalogLoaded bool
	catalogSyncMu sync.Mutex

	// CatalogURLOverride / CatalogClient 测试注入点。
	CatalogURLOverride string
	CatalogClient      *http.Client
)

// InitCatalog 初始化目录：种子兜底 + 异步 live 同步（失败保留种子，离线可用）。
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
	for _, m := range seedModels {
		catalogModels[m.ID] = m
	}
	catalogLoaded = true
	catalogMu.Unlock()
}

// SyncCatalog 用池内首个启用账号的 JWT 拉 live 目录，并入现有表：
//   - web Remote 目录给可服务清单与上下文限制（raw 名不全）；
//   - 原生 get_detail_param 目录按函数档拉两遍（chat_v3 = IDE 默认、
//     solo_agent = Solo 代理档）：同一账号两档准入不同，solo_agent 覆盖更广
//     （实测 DeepSeek-V4-Flash-Official 在 chat_v3 下 1005 plan 限制、
//     solo_agent 正常；多数 __max 型号同理），故 solo_agent 后合并覆盖；
//   - live 条目补全 raw 名（上一轮目录/种子里已验证的映射优先）与上下文限制；
//   - 种子条目 live 没列的保留（上游临时裁剪不该立刻掐断可用模型）；
//   - 网络/解析失败返回错误，目录原地不动（单档失败不阻断其余档）。
func SyncCatalog() (int, error) {
	catalogSyncMu.Lock()
	defer catalogSyncMu.Unlock()
	token, edition := catalogProbeCredential()
	if token == "" {
		return 0, fmt.Errorf("no enabled trae account to fetch the live catalog")
	}
	live, err := fetchWebModelConfigs(token, edition)
	if err != nil {
		return 0, err
	}
	// TraeCode 原生档目录（get_detail_param）：与 Remote 目录不是同一集合——
	// Solo 档会先于 Remote 上新模型，model_name 是上游真实 raw id（权威，
	// 覆盖种子映射）；失败不阻断 Remote 合并。
	base := GatewayFor(&Account{Token: token, Edition: edition})
	userID := TokenAccountID(token)
	for _, function := range nativeCatalogFunctions {
		entries, ferr := fetchSoloModelConfigs(base, token, userID, function)
		if ferr != nil {
			continue
		}
		for id, m := range entries {
			live[id] = m
		}
	}
	return applyCatalog(live), nil
}

// nativeCatalogFunctions 原生目录拉取的函数档（顺序即优先级，后者覆盖前者）。
// chat_v3 是 IDE 默认；solo_agent 是 Solo 主档，实测准入最宽。
var nativeCatalogFunctions = []string{"chat_v3", "solo_agent"}

// applyCatalog 把一批 live 条目并入现行目录，返回变更条目数。
//
// 两条保真规则（都来自实测）：
//   - 原生档确认过的条目不被 web 清单覆盖：web 条目的 Function 恒为
//     chat_v3、raw 名恒为空，直接合并会把 solo_agent 档降级回 chat_v3
//     （多数模型在 chat_v3 下 4001），只在 web 目录某次拉取成功、原生档
//     失败时触发，故以已有原生条目为准，仅补显示名与上下文。
//   - raw 名缺失时沿用上一轮目录（含种子）的映射：这些名字是历史实测的
//     wire 事实，直接退化成裸 ID 会丢信息（裸 ID 对 TraeCode 常报 4023）。
func applyCatalog(in map[string]Model) int {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	changed := 0
	for id, m := range in {
		cur, exists := catalogModels[id]
		if exists && cur.Native && !m.Native {
			if cur.Name == "" {
				cur.Name = m.Name
			}
			cur.Context = maxInt(cur.Context, m.Context)
			m = cur
		}
		if m.RawName == "" {
			if exists && cur.RawName != "" {
				m.RawName = cur.RawName
			} else {
				m.RawName = id
			}
		}
		if m.Function == "" {
			m.Function = "chat_v3"
		}
		if exists && cur == m {
			continue
		}
		changed++
		catalogModels[id] = m
	}
	return changed
}

// fetchSoloModelConfigs 拉取 TraeCode 原生目录（按函数档）：
//
//	POST {base}/api/ide/v1/get_detail_param
//	body {"function":"<function>","config_names":null,"need_prompt":false,
//	      "current_config_info":null,"poly_prompt":true,"mode_type":null,"agent_type":null}
//
// 请求头带 Solo 客户端指纹（Trae/0.1.52 / 83DG / stable_cn 等，上游实测值）。
// 响应 config_info_list（或 data.config_info_list）；usage 非 chat_completion 的
// 路由配置（如 custom_model_1M_text）与 custom_models 条目跳过；model_name 取
// model_detail_list 里 __dev 后缀项优先（上游真实 raw id；__max 在 chat_v3 下
// 实测大量 4001/4023）。条目的 Function 记为查询用的函数档——上游目录不回显
// raw_chat_function，请求必须与目录档一致才不会 4001。
func fetchSoloModelConfigs(base, token, userID, function string) (map[string]Model, error) {
	body, _ := json.Marshal(map[string]any{
		"function":            function,
		"config_names":        nil,
		"need_prompt":         false,
		"current_config_info": nil,
		"poly_prompt":         true,
		"mode_type":           nil,
		"agent_type":          nil,
	})
	req, err := http.NewRequest(http.MethodPost, base+"/api/ide/v1/get_detail_param", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range soloHeaders(token, userID) {
		req.Header.Set(k, v)
	}
	client := CatalogClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("solo catalog status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload struct {
		ConfigInfoList []struct {
			Usage       string `json:"usage"`
			ConfigName  string `json:"config_name"`
			Name        string `json:"name"`
			ModelName   string `json:"model_name"`
			DisplayName string `json:"display_name"`
			RawFunction string `json:"raw_chat_function"`
			DisplayCfg  struct {
				ContextWindow int `json:"context_window_size"`
				ContextTokens int `json:"context_window_tokens"`
				MaxTokens     int `json:"prompt_max_tokens"`
			} `json:"display_config"`
			CustomModels   any `json:"custom_models"`
			ModelDetailLst []struct {
				ModelName string `json:"model_name"`
			} `json:"model_detail_list"`
		} `json:"config_info_list"`
		Data struct {
			ConfigInfoList []struct {
				Usage       string `json:"usage"`
				ConfigName  string `json:"config_name"`
				Name        string `json:"name"`
				ModelName   string `json:"model_name"`
				DisplayName string `json:"display_name"`
				RawFunction string `json:"raw_chat_function"`
				DisplayCfg  struct {
					ContextWindow int `json:"context_window_size"`
					ContextTokens int `json:"context_window_tokens"`
					MaxTokens     int `json:"prompt_max_tokens"`
				} `json:"display_config"`
				CustomModels   any `json:"custom_models"`
				ModelDetailLst []struct {
					ModelName string `json:"model_name"`
				} `json:"model_detail_list"`
			} `json:"config_info_list"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	list := payload.ConfigInfoList
	if len(list) == 0 {
		list = payload.Data.ConfigInfoList
	}
	out := map[string]Model{}
	for _, e := range list {
		u := strings.ToLower(strings.TrimSpace(e.Usage))
		if u != "" && u != "chat_completion" {
			continue
		}
		if e.CustomModels != nil {
			continue
		}
		id := firstNonEmpty(e.ConfigName, e.Name)
		if id == "" || soloHiddenIDs[id] {
			continue
		}
		raw := e.ModelName
		if raw == "" {
			// model_detail_list 里 __dev 后缀项优先（上游真实 raw id）
			for _, d := range e.ModelDetailLst {
				if strings.HasSuffix(strings.ToLower(d.ModelName), "__dev") {
					raw = d.ModelName
					break
				}
			}
			if raw == "" {
				for _, d := range e.ModelDetailLst {
					if d.ModelName != "" {
						raw = d.ModelName
						break
					}
				}
			}
		}
		m := Model{
			ID:       id,
			Name:     firstNonEmpty(e.DisplayName, e.Name),
			RawName:  raw,
			Function: firstNonEmpty(e.RawFunction, function),
			Context:  maxInt(e.DisplayCfg.ContextWindow, e.DisplayCfg.ContextTokens, e.DisplayCfg.MaxTokens),
			Native:   true,
		}
		if out[id].ID == "" || m.RawName != "" {
			out[id] = m
		}
	}
	return out, nil
}

// soloHeaders get_detail_param 的 Solo 客户端指纹（上游 Trae2api-cn 实测常量）。
func soloHeaders(token, userID string) map[string]string {
	fp := fingerprintFor(userID)
	return map[string]string{
		"Content-Type":         "application/json",
		"Accept":               "application/json",
		"Authorization":        "Cloud-IDE-JWT " + token,
		"X-Cloudide-Token":     token,
		"X-Ide-Token":          token,
		"X-App-Id":             traeAppID,
		"X-App-Version":        "default",
		"X-App-Version-Code":   "20260811",
		"X-Ide-Version":        "0.1.52",
		"X-Ide-Version-Code":   "20260811",
		"X-Ide-Version-Type":   "stable",
		"X-Plugin-Channel":     "icube-ai",
		"X-Uid":                userID,
		"x-device-id":          fp.DeviceID,
		"x-machine-id":         fp.MachineID,
		"x-device-cpu":         "AMD",
		"x-device-brand":       "83DG",
		"x-device-type":        "windows",
		"x-device-platform":    "windows",
		"x-os-version":         "Windows 11 Pro",
		"x-version-code":       "20260811",
		"x-ide-token":          token,
		"package-type":         "stable_cn",
		"x-lscbd-aid":          "787976",
		"x-lscbd-platform":     "windows",
		"x-ss-dp":              "787976",
		"app-version":          "0.1.52",
		"request-traffic-type": "prod",
		"User-Agent":           "Trae/0.1.52",
		"Origin":               "https://solo.trae.cn",
		"Referer":              "https://solo.trae.cn/",
	}
}

// catalogProbeCredential 取首个启用账号的凭据与版本（live 目录请求用）。
// 判定用账号总闸（EnabledOrDefault）而非反代开关——目录同步是维护动作，
// 反代关着的账号也应提供模型清单。
func catalogProbeCredential() (token, edition string) {
	for _, a := range Get().Accounts {
		if a.EnabledOrDefault() && a.Token != "" {
			return a.Token, a.Edition
		}
	}
	return "", ""
}

// soloHiddenIDs 原版隐藏的内部路由/子代理配置（上游也下发它们，但不是
// 可选聊天模型）：computer_use_subagent 等（trae_client.py 隐藏清单）。
var soloHiddenIDs = map[string]bool{
	"computer_use_subagent": true, "browser_use_subagent": true, "aquila": true,
	"explore_sub_agent_v2": true, "file_search_agent": true, "work": true,
}

// fetchWebModelConfigs 拉取 web 客户端模型目录：
// GET {remote_base}/models?functions=solo_agent_remote,solo_work_remote,solo_design_remote&show_custom_model=true
// 响应 data.list[] 为分组（function/agent_type 字段），组内 models[] 每条
// config_name/configName（或旧载荷的 name/model_name）是请求 id。
func fetchWebModelConfigs(token, edition string) (map[string]Model, error) {
	base := RemoteBaseFor(edition)
	url := base + "/models?functions=solo_agent_remote%2Csolo_work_remote%2Csolo_design_remote&show_custom_model=true"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range webHeaders(token, edition) {
		req.Header.Set(k, v)
	}
	client := CatalogClient
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	endpoint := CatalogURLOverride
	if endpoint != "" {
		req.URL, err = req.URL.Parse(endpoint)
		if err != nil {
			return nil, err
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("trae model catalog status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload struct {
		Data struct {
			List []struct {
				Function string `json:"function"`
				Models   []struct {
					ConfigName      string `json:"config_name"`
					ConfigNameCamel string `json:"configName"`
					Name            string `json:"name"`
					ModelName       string `json:"model_name"`
					ModelNameCamel  string `json:"modelName"`
					DisplayName     string `json:"display_name"`
					DisplayNameCam  string `json:"displayName"`
					RawFunction     string `json:"raw_chat_function"`
					RawFunctionCam  string `json:"rawChatFunction"`
					// 上下文字段上游形态不稳定（int / 字符串 / 对象都出现过），
					// 宽松存 any 再转数
					ContextWindow  any `json:"context_window_size"`
					ContextWindowC any `json:"contextWindow"`
					ContextTokens  any `json:"context_window_tokens"`
				} `json:"models"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	out := map[string]Model{}
	for _, g := range payload.Data.List {
		for _, rm := range g.Models {
			id := firstNonEmpty(rm.ConfigName, rm.ConfigNameCamel, rm.ModelName, rm.ModelNameCamel, rm.Name)
			if id == "" {
				continue
			}
			m := Model{ID: id, Function: firstNonEmpty(rm.RawFunction, rm.RawFunctionCam, "chat_v3")}
			m.Name = firstNonEmpty(rm.DisplayName, rm.DisplayNameCam, rm.Name)
			if m.Context == 0 {
				m.Context = maxInt(anyToInt(rm.ContextWindow), anyToInt(rm.ContextWindowC), anyToInt(rm.ContextTokens))
			}
			if _, exists := out[id]; !exists {
				out[id] = m
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("trae model catalog returned no models")
	}
	return out, nil
}

// RemoteBaseFor web 目录端点前缀（区别于 raw_chat 网关：必须带 /api/remote/v1）。
func RemoteBaseFor(edition string) string {
	if edition == "sg" || edition == "solo-sg" {
		return "https://core-normal.trae.ai/api/remote/v1"
	}
	return DefaultGateway + "/api/remote/v1"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// anyToInt 宽松转 int（float64 / json.Number / 数字串；其余 0）。
func anyToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	}
	return 0
}

func maxInt(vals ...int) int {
	m := 0
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return m
}

// HasEnabledAccount 是否有可用账号（账号总闸开、反代开关开且有 token）。
// 模型目录的展示门控：没有**可路由**账号就不对外提供模型——与 WorkBuddy
// 同口径（AvailableUIDs 判 AccountEnabled && PoolEnabled）：反代开关全关 =
// 没有凭据可参与请求路由，目录照展示只会让用户选了模型吃 502。种子表只留
// 作请求侧 raw 名解析，不再当作"可服务目录"展示。
// （2026-10-26 用户澄清：此前只判总闸不看反代开关，导致"总闸开/反代关"
// 时目录照出而请求全拒，两平台口径不一。）
func HasEnabledAccount() bool {
	for _, a := range Get().Accounts {
		if a.Routable() && a.Token != "" {
			return true
		}
	}
	return false
}

// ModelList 排序后的模型列表（面板与 /v1/models 共用）。两个门控：
//   - 没有可路由账号时返回空列表（种子目录不对外展示，只服务请求侧解析）；
//   - 只列原生档（chat_v3 / solo_agent）确认过的条目：web Remote 独有条目
//     与种子里的 __v2 映射没有可用 raw_chat_function，必然 4023/4001，
//     列出来只会让用户选了吃 502。这些 ID 仍留在目录里供 Resolve 解析，
//     显式指定时返回上游真实错误而不是 400 unknown model。
func ModelList() []Model { return modelList(true) }

func modelList(nativeOnly bool) []Model {
	InitCatalog()
	if !HasEnabledAccount() {
		return []Model{}
	}
	catalogMu.RLock()
	out := make([]Model, 0, len(catalogModels))
	for _, m := range catalogModels {
		if nativeOnly && !m.Native {
			continue
		}
		out = append(out, m)
	}
	catalogMu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Resolve 解析裸模型 ID。未知模型显式拒绝（400），不静默改写。
// 不按 Native 过滤：展示层隐藏的条目仍可被显式请求（失败时给上游真实原因）。
func Resolve(id string) (Model, bool) {
	InitCatalog()
	catalogMu.RLock()
	m, ok := catalogModels[strings.TrimSpace(id)]
	catalogMu.RUnlock()
	return m, ok
}

// ProbeModel 探测用模型：优先 GLM-5.2（TraeCode 的 auto 默认，最稳），
// 其次排序最小条目。展示列表为空时退回完整目录（刚启动、原生档尚未同步
// 完成时账号健康检查仍需一个模型可用）。
func ProbeModel() (Model, bool) {
	list := ModelList()
	if len(list) == 0 {
		list = modelList(false)
	}
	if len(list) == 0 {
		return Model{}, false
	}
	for _, m := range list {
		if m.ID == "glm-5.2" {
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

// StripPrefix 去掉 "trae:" 前缀，返回裸模型 ID；无前缀时返回原串与 false。
func StripPrefix(model string) (id string, ok bool) {
	m := strings.TrimSpace(model)
	if strings.HasPrefix(m, ModelPrefix) {
		return strings.TrimSpace(m[len(ModelPrefix):]), true
	}
	return m, false
}

// HasPrefix 判断模型 ID 是否带本平台前缀。
func HasPrefix(model string) bool {
	_, ok := StripPrefix(model)
	return ok
}

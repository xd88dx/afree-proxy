package trae

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"afree-proxy/internal/kit"
)

// ============ Remote / TraeWork chat_sessions 传输协议 ============
//
// 移植自上游 Trae2api-cn 的 trae_remote_client.py（9router 风格 web remote
// 会话传输）。与 raw_chat（TraeCode 桌面协议）并存的第二条链路：
//   - remote   → POST /api/remote/v1/chat_sessions, agent_type=solo_agent_remote, mode=code
//   - traework → 同一传输，agent_type=solo_work_remote, mode=work（Work 档
//     common_params 与会话标记按原项目对齐）
//
// 协议形状（原项目实测）：
//  1. POST {base}/chat_sessions 创建会话 → data.chat_session_id / message_id
//  2. GET  {base}/chat_sessions/{sid}/events?reply_to_message_id={mid}（SSE）
//     事件族：heartbeat / plan_item / message|assistant_message|response|text|output
//     / token_usage / error / done
//  3. POST {base}/chat_sessions/{sid}/stop 提前终止（尽力而为）

const (
	remoteAgentType      = "solo_agent_remote"
	workAgentType        = "solo_work_remote"
	remoteOriginCN       = "https://solo.trae.cn"
	remoteUA             = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
	remoteAppVersion     = "1.0.0.1229"
	workBuildVersion     = "2.3.79946"
	workVSCodeVersion    = "1.107.1"
	workAppVersion       = "3.3.96"
	workProductCode      = "SOLO_Lite"
	remoteConnectTimeout = 60 * time.Second
)

// remoteBaseOf 会话传输基址（与 live 目录同一个 /api/remote/v1 前缀）。
func remoteBaseOf(acc *Account) string {
	gw := strings.TrimRight(Get().Gateway, "/")
	if gw != "" && gw != DefaultGateway {
		// 自定义网关：调用方自行保证带 /api/remote/v1 路径（与原项目
		// _normalize_remote_base 的自定义主机语义一致，原样使用）。
		return gw + "/api/remote/v1"
	}
	return RemoteBaseFor(acc.Edition)
}

// buildRemoteHeaders chat_sessions 请求头（web 客户端指纹，原项目 build_headers）。
func buildRemoteHeaders(acc *Account, stream bool) map[string]string {
	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	return map[string]string{
		"Authorization":          "Cloud-IDE-JWT " + acc.Token,
		"Content-Type":           "application/json",
		"X-Trae-Client-Type":     "web",
		"X-Preferenced-Language": "zh-CN",
		"x-user-region":          "CN",
		"Origin":                 remoteOriginCN,
		"Referer":                remoteOriginCN + "/",
		"User-Agent":             remoteUA,
		"Accept":                 accept,
	}
}

// remoteStableSessionID 稳定会话键（原项目 model_session_id）：
// sha256(account ⊦ model ⊦ variant) 前 32 位 hex 转 UUID 排版。
func remoteStableSessionID(accountID, model string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{accountID, model, ""}, "\x1f")))
	h := hex.EncodeToString(sum[:])[:32]
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// remoteIdentityText 套餐身份文案（afree 用 Credits.Identity 快照；
// 空 = Free。原项目 raw identity 数值本地没有，按 Free=0 / 其他=文案回退）。
func remoteIdentityText(acc *Account) (raw, text string) {
	text = "Free"
	if acc.Credits != nil && strings.TrimSpace(acc.Credits.Identity) != "" {
		text = strings.TrimSpace(acc.Credits.Identity)
	}
	if strings.EqualFold(text, "free") {
		return "0", text
	}
	return text, text
}

// remoteCommonParams 会话 common_params（原项目 common_params；JSON 字符串落请求体）。
func remoteCommonParams(acc *Account, mode, sessionID string) string {
	intl := acc.Edition == "sg" || acc.Edition == "solo-sg"
	scope, region := "marscode-cn", "cn"
	language, appLanguage := "zh-cn", "zh-CN"
	if intl {
		scope, region = "marscode-us", "US-East"
		language, appLanguage = "en-us", "en"
	}
	fp := fingerprintFor(acc.ID)
	identityRaw, identityText := remoteIdentityText(acc)
	params := map[string]any{
		"language":        language,
		"app_language":    appLanguage,
		"quality":         "stable",
		"app_version":     remoteAppVersion,
		"web_id":          "",
		"user_identity":   identityText,
		"is_freshman":     "0",
		"biz_user_id":     acc.ID,
		"user_unique_id":  acc.ID,
		"scope":           scope,
		"tenant":          "marscode",
		"region":          region,
		"aiRegion":        region,
		"is_privacy_mode": 0,
		"privacy_mode":    "off",
		"solo_chat_mode":  mode,
	}
	if mode == "work" {
		// Work 档：设备/身份字段按原项目 work 分支常量（TraeCode Linux 指纹）。
		machineSeed := strings.Join([]string{acc.ID, fp.DeviceID, "traework-linux-probe"}, "\x1f")
		machineSum := sha256.Sum256([]byte(machineSeed))
		localSum := sha256.Sum256([]byte("local-" + acc.ID + "-" + fp.DeviceID))
		params["icube_uid"] = acc.ID
		params["user_id"] = acc.ID
		params["device_id"] = fp.DeviceID
		params["local_device_id"] = "aha-" + hex.EncodeToString(localSum[:])[:32]
		params["machine_id"] = hex.EncodeToString(machineSum[:])
		params["arch"] = "x64"
		params["system"] = "linux"
		params["scope"] = "marscode"
		params["tenant"] = "marscode"
		params["region"] = "CN"
		params["aiRegion"] = "CN"
		params["build_version"] = workBuildVersion
		params["vscode_version"] = workVSCodeVersion
		params["app_version"] = workAppVersion
		params["os_name"] = "linux"
		params["os_version"] = "Linux x64"
		params["os_release"] = "6.1"
		params["platform"] = "electron"
		params["device_model"] = "TraeCode Linux"
		params["device_manufacturer"] = "TraeCode"
		params["cpu"] = "x86_64"
		params["cpu_brand"] = "x86_64"
		params["cpu_speed"] = 0
		params["memory"] = 0
		params["chat_mode"] = 1
		params["identity"] = identityRaw
		params["identity_str"] = identityText
		params["product_code"] = workProductCode
		params["message_source"] = "manual"
	}
	if sessionID != "" {
		params["biz_session_id"] = sessionID
	}
	b, _ := json.Marshal(params)
	return string(b)
}

// normalizeWebConfig web 目录条目 → custom_model（原项目 _build_web_model_config）。
func normalizeWebConfig(raw map[string]any, fn string) map[string]any {
	cfg := make(map[string]any, len(raw)+6)
	for k, v := range raw {
		cfg[k] = v
	}
	if s, ok := cfg["features"].(string); ok {
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			cfg["features"] = parsed
		}
	}
	if s, ok := cfg["reasoning_effort_config"].(string); ok {
		var parsed map[string]any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			cfg["reasoning_effort_config"] = parsed
		}
	}
	name, _ := cfg["name"].(string)
	configName := firstNonEmpty(
		strField(cfg, "config_name"), strField(cfg, "configName"), name,
		strField(cfg, "model_name"), strField(cfg, "modelName"))
	modelName := firstNonEmpty(strField(cfg, "model_name"), strField(cfg, "modelName"), configName)
	cfg["config_name"] = configName
	cfg["model_name"] = modelName
	if strField(cfg, "config_source") == "" {
		if _, isNum := cfg["config_source"]; !isNum {
			cfg["config_source"] = 1
		}
	}
	if _, ok := cfg["provider"]; !ok {
		cfg["provider"] = ""
	}
	if strField(cfg, "raw_chat_function") == "" && fn != "" {
		cfg["raw_chat_function"] = fn
	}
	if _, ok := cfg["ak"]; !ok {
		cfg["ak"] = ""
	}
	if _, ok := cfg["sk"]; !ok {
		cfg["sk"] = ""
	}
	if _, ok := cfg["base_url"]; !ok {
		cfg["base_url"] = ""
	}
	if _, ok := cfg["auth_type"]; !ok {
		cfg["auth_type"] = 0
	}
	if _, ok := cfg["use_remote_service"]; !ok {
		_, clientConnect := cfg["client_connect"]
		cc, _ := cfg["client_connect"].(bool)
		cfg["use_remote_service"] = !(clientConnect && cc)
	}
	return cfg
}

func strField(obj map[string]any, key string) string {
	s, _ := obj[key].(string)
	return strings.TrimSpace(s)
}

// flattenRemoteQuery OpenAI messages → remote 端点 query（原项目 flatten_query）：
//   - system → "[System]\n…"；assistant → "[Assistant]\n…"（含工具调用历史）
//   - tool   → "[Client Tool Result: id name status=…]\n…"
//   - 结果包成 [{"type":"text","data":{"content":…}}] 的 parts JSON
func flattenRemoteQuery(msgs []any) string {
	parts := make([]string, 0, len(msgs))
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		switch role {
		case "developer":
			role = "system"
		case "function":
			role = "tool"
		}
		content := remoteContentText(m["content"])
		switch role {
		case "system":
			parts = append(parts, "[System]\n"+content)
		case "assistant":
			blocks := []string{}
			if content != "" {
				blocks = append(blocks, content)
			}
			if hist := serializeRemoteToolHistory(m["tool_calls"]); hist != "" {
				blocks = append(blocks, hist)
			}
			if len(blocks) > 0 {
				parts = append(parts, "[Assistant]\n"+strings.Join(blocks, "\n\n"))
			}
		case "tool":
			id, _ := m["tool_call_id"].(string)
			if id == "" {
				id = "unknown"
			}
			name, _ := m["name"].(string)
			if name == "" {
				name = "tool"
			}
			status := "succeeded"
			if remoteToolFailed(m) {
				status = "failed"
			}
			parts = append(parts, fmt.Sprintf("[Client Tool Result: %s %s status=%s]\n%s", id, name, status, content))
		default:
			parts = append(parts, content)
		}
	}
	text := strings.Join(parts, "\n\n")
	wrapped, _ := json.Marshal([]map[string]any{{"type": "text", "data": map[string]any{"content": text}}})
	return string(wrapped)
}

// remoteContentText content 字段 → 纯文本（字符串直取；块数组拼接 text 块）。
func remoteContentText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, block := range c {
			switch s := block.(type) {
			case string:
				b.WriteString(s)
			case map[string]any:
				if t, ok := s["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	}
	return ""
}

// serializeRemoteToolHistory assistant 历史工具调用（原项目 _serialize_tool_calls）。
func serializeRemoteToolHistory(v any) string {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	calls := make([]map[string]any, 0, len(list))
	for _, raw := range list {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		if fn == nil {
			fn = call
		}
		args, _ := fn["arguments"].(string)
		if args == "" {
			if inp, ok := fn["input"]; ok {
				args, _ = inp.(string)
			}
		}
		id, _ := call["id"].(string)
		if id == "" {
			id, _ = call["tool_call_id"].(string)
		}
		name, _ := fn["name"].(string)
		calls = append(calls, map[string]any{"id": id, "name": name, "input": args})
	}
	if len(calls) == 0 {
		return ""
	}
	b, _ := json.Marshal(calls)
	return "Client tool history (already handled; do not repeat):\n" + string(b)
}

// remoteToolFailed tool 消息是否失败（原项目 tool_result_is_failed 的常用形态）。
func remoteToolFailed(m map[string]any) bool {
	if b, ok := m["is_error"].(bool); ok {
		return b
	}
	if b, ok := m["isError"].(bool); ok {
		return b
	}
	return false
}

// remoteDebugEvents 事件转储开关（TRAE_REMOTE_DEBUG=1 时打印每个事件帧，
// 供现场排障；默认关闭）。
var remoteDebugEvents = strings.EqualFold(strings.TrimSpace(os.Getenv("TRAE_REMOTE_DEBUG")), "1")

// remoteWorkChatOnce TraeWork 档传输（端点钉 traework，或 auto 模式下模型
// 仅有 traework 档时由 transportSelector 选用）。
func remoteWorkChatOnce(ctx context.Context, acc *Account, opts ChatOpts, proxyURL string) (*Stream, error) {
	opts.PreferWorkTier = true
	return remoteChatOnce(ctx, acc, opts, proxyURL)
}

// remoteChatOnce remote/traework 端点单次请求：创建会话 → 打开事件流。
// 返回的 Stream 与 raw_chat 同构（Next/Close/Usage/Err/ToolCallsSnapshot），
// 事件翻译在 Stream.consumeRemote。
func remoteChatOnce(ctx context.Context, acc *Account, opts ChatOpts, proxyURL string) (*Stream, error) {
	endpoint := strings.ToLower(strings.TrimSpace(Get().Endpoint))
	agentType := remoteAgentType
	mode := "code"
	if endpoint == "traework" || opts.PreferWorkTier {
		agentType = workAgentType
		mode = "work"
	}
	if !modelHasTier(opts.Model, agentTypeTier(agentType)) {
		return nil, fmt.Errorf("Trae remote model is not available for the bound account: %s", opts.Model.ID)
	}
	// TierCfg 以档位名（remote/traework）为键；同一档的原始条目就是
	// custom_model 构造源（agent_type 只决定传输执行器与查询分组）。
	rawCfg := opts.Model.TierCfg[agentTypeTier(agentType)]
	if rawCfg == nil {
		return nil, fmt.Errorf("Trae remote model is not available for the bound account: %s", opts.Model.ID)
	}
	customModel := normalizeWebConfig(rawCfg, agentType)
	configName := strField(customModel, "config_name")
	if configName == "" {
		configName = opts.Model.ID
	}
	// config_source / is_preset 以目录条目为准（原项目：custom_model 取值
	// or 缺省；写死 1/true 与上游目录不符会 4001 param invalid）
	configSource := 1
	if v, ok := customModel["config_source"].(float64); ok && v != 0 {
		configSource = int(v)
	}
	isPreset := true
	if v, ok := customModel["is_preset"].(bool); ok {
		isPreset = v
	}
	provider := strField(customModel, "provider")
	base := remoteBaseOf(acc)
	sid := remoteStableSessionID(acc.ID, configName)
	msgs, _ := opts.Params["messages"].([]any)
	initialMessage := map[string]any{
		"chat_session_id":          "",
		"content":                  []any{},
		"query":                    flattenRemoteQuery(msgs),
		"model_name":               configName,
		"agent_type":               agentType,
		"agent_id":                 agentType,
		"model_selection_strategy": "manual",
		"common_params":            remoteCommonParams(acc, mode, sid),
		"model_config_source":      configSource,
		"model_is_preset":          isPreset,
		"model_provider":           provider,
		"custom_model":             customModel,
	}
	if mode == "work" {
		initialMessage["__hit_shared_artifact_and_workroom"] = true
		initialMessage["__hit_my_space"] = true
	}
	body := map[string]any{
		"mode":                mode,
		"environment_id":      "default",
		"initial_message":     initialMessage,
		"env":                 "remote",
		"auto_create_project": false,
		"origin":              "web",
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal remote body: %w", err)
	}
	client := httpClientFor(proxyURL)
	cctx, cancel := context.WithTimeout(ctx, remoteConnectTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/chat_sessions", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, err
	}
	for k, v := range buildRemoteHeaders(acc, false) {
		req.Header.Set(k, v)
	}
	hsStart := time.Now()
	resp, err := client.Do(req)
	observeProxy(proxyURL, err == nil, err, time.Since(hsStart).Milliseconds())
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("client aborted: %w", ctx.Err())
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		he := parseUpstreamError(resp.StatusCode, b)
		he.Transport = agentType
		log.Printf("trae remote create_session failed status=%d code=%s body=%s model=%s agent=%s",
			resp.StatusCode, he.Code, kit.Truncate(string(b), 600), opts.Model.ID, agentType)
		return nil, he
	}
	var payload struct {
		Code any `json:"code"`
		Data struct {
			ChatSessionID string `json:"chat_session_id"`
			MessageID     string `json:"message_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("trae remote invalid create_session response: %w", err)
	}
	resp.Body.Close()
	if payload.Data.ChatSessionID == "" || payload.Data.MessageID == "" {
		return nil, fmt.Errorf("trae remote create_session missing ids")
	}
	// 事件流（无整体超时：SSE 长流，读取超时交给上层 ctx/缓冲层）
	eventsURL := fmt.Sprintf("%s/chat_sessions/%s/events?reply_to_message_id=%s", base, payload.Data.ChatSessionID, payload.Data.MessageID)
	ereq, err := http.NewRequestWithContext(ctx, http.MethodGet, eventsURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range buildRemoteHeaders(acc, true) {
		ereq.Header.Set(k, v)
	}
	eresp, err := client.Do(ereq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("client aborted: %w", ctx.Err())
		}
		return nil, err
	}
	if eresp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(eresp.Body, 1<<16))
		eresp.Body.Close()
		he := parseUpstreamError(eresp.StatusCode, b)
		he.Transport = agentType
		return nil, he
	}
	stream := newStream(eresp)
	stream.remote = true
	stream.Transport = agentType // remote / work（档位名与封禁 scope 对齐）
	stream.remoteStop = &remoteStopInfo{
		base: base, sessionID: payload.Data.ChatSessionID,
		messageID: payload.Data.MessageID, token: acc.Token,
		headers: buildRemoteHeaders(acc, false), proxyURL: proxyURL,
	}
	return stream, nil
}

// remoteStopInfo 会话提前终止所需信息（Close 时 !SawDone 尽力 stop）。
type remoteStopInfo struct {
	base      string
	sessionID string
	messageID string
	token     string
	headers   map[string]string
	proxyURL  string
}

func agentTypeTier(agentType string) string {
	if agentType == workAgentType {
		return "traework"
	}
	return "remote"
}

func modelHasTier(m Model, tier string) bool {
	for _, t := range m.Tiers {
		if t == tier {
			return true
		}
	}
	return false
}

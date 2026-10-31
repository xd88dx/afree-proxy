package trae

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// ============ 上游调用 ============
//
// TraeCode raw 主链路（Trae 桌面客户端同款协议，明文 JSON + SSE，无传输加密）：
//   POST {gateway}/api/ide/v2/llm_raw_chat   （404 时回退 /api/ide/v1）
// 网关候选：配置的 Gateway → api5-normal.mchost.guru（仅默认网关参与回退，
// 自定义网关不回退）。消息是块数组私有格式（content: [{type:text|image_url}]），
// 工具调用走原生协议（tools/tool_choice 直接进 body，assistant.tool_calls 与
// tool 消息原样保留）。key 选择与出口解析留在主包桥接（注入钩子），本包只做
// "给定账号 + 出口"发一次请求 + 账号轮转重试。

// wire 常量（TraeCode 桌面客户端实测指纹，raw_client.py）。
const (
	rawChatV2Endpoint = "/api/ide/v2/llm_raw_chat"
	rawChatV1Endpoint = "/api/ide/v1/llm_raw_chat"

	traeAppID        = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
	traeIDEVersion   = "3.4.4"
	traeVersionCode  = "20260212"
	defaultClientID  = "ono9krqynydwx5"
	apiHostCN        = "https://api.trae.cn"
	fallbackGateway  = "https://api5-normal.mchost.guru"
	uaEmpty          = ""
	uaWeb            = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
	refreshBeforeSec = 30 * 60 // 距过期 <30min 提前刷新
	validBufferSec   = 5 * 60  // 距过期 <5min 视为不可用（先刷新再发）
)

// deviceBrands 设备品牌池（trae_client.py _DEVICE_BRANDS）。
var deviceBrands = []string{"92L3", "91C9", "814S", "8P15V", "35G4", "65G4", "55G4"}

// HTTPError 类型化上游错误（携带状态码、业务码与分类标记）。
// 分类事实源（sse.py/raw_client.py）：3004/4008/4011/429 = 容量/限流（可轮换
// 账号）；4001 = 参数错误（换账号也无法修复）；401/403 = 凭据失效。
type HTTPError struct {
	Status      int
	Body        string
	Code        string
	RateLimited bool
	ParamError  bool
	AuthFailure bool
	// Transport 失败发生的传输（raw/remote/work；空 = 未知，如网络错误）。
	// 按端点封禁（2026-10-27）据此把封禁落到对应档位。
	Transport string
}

func (e *HTTPError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("trae api %d (code %s): %s", e.Status, e.Code, e.Body)
	}
	return fmt.Sprintf("trae api %d: %s", e.Status, e.Body)
}

// ============ 注入钩子（主包桥接） ============

var (
	exitResolverMu sync.RWMutex
	exitResolver   func(acc *Account) (proxyURL string, ok bool)
	clientProvider func(proxyURL string) *http.Client

	attemptObserverMu sync.RWMutex
	attemptObserver   func(ctx context.Context, acc *Account, proxyURL string)

	proxyObserverMu sync.RWMutex
	proxyObserver   func(proxyURL string, ok bool, err error, hsMs int64)
)

// SetExitResolver 注入出站解析（主包装配期调用）：返回 ok=false 表示绑定出口
// 全不可用，调用方必须跳过该账号（隔离优先，绝不直连回退）。
func SetExitResolver(f func(acc *Account) (string, bool)) {
	exitResolverMu.Lock()
	exitResolver = f
	exitResolverMu.Unlock()
}

// SetClientProvider 注入按代理 URL 取 HTTP client 的工厂。
func SetClientProvider(f func(proxyURL string) *http.Client) {
	clientProvider = f
}

// SetAttemptObserver 注入「命中账号 + 出口」观测（请求日志两列）。
func SetAttemptObserver(f func(ctx context.Context, acc *Account, proxyURL string)) {
	attemptObserverMu.Lock()
	attemptObserver = f
	attemptObserverMu.Unlock()
}

// SetProxyObserver 注入出口成败观测（在线率统计）。
func SetProxyObserver(f func(proxyURL string, ok bool, err error, hsMs int64)) {
	proxyObserverMu.Lock()
	proxyObserver = f
	proxyObserverMu.Unlock()
}

func resolveExitFor(acc *Account) (string, bool) {
	exitResolverMu.RLock()
	r := exitResolver
	exitResolverMu.RUnlock()
	if r == nil {
		return "", true
	}
	return r(acc)
}

func httpClientFor(proxyURL string) *http.Client {
	if clientProvider != nil {
		return clientProvider(proxyURL)
	}
	return http.DefaultClient
}

func observeAttempt(ctx context.Context, acc *Account, proxyURL string) {
	attemptObserverMu.RLock()
	obs := attemptObserver
	attemptObserverMu.RUnlock()
	if obs != nil && acc != nil {
		obs(ctx, acc, proxyURL)
	}
}

func observeProxy(proxyURL string, ok bool, err error, hsMs int64) {
	proxyObserverMu.RLock()
	obs := proxyObserver
	proxyObserverMu.RUnlock()
	if obs != nil {
		obs(proxyURL, ok, err, hsMs)
	}
}

// ============ 设备指纹（按账号派生，稳定不漂移） ============
//
// 同一账号恒定同一指纹（比上游的"3~5 请求后轮换"更利于风控画像稳定）：
// sha256(accountID + "#fp") 派生 18 位设备号、32 位 machine id 与品牌。

type deviceFP struct {
	DeviceID  string // x-device-id：18 位数字
	MachineID string // x-machine-id：32 位 hex
	Brand     string // x-device-brand
}

func fingerprintFor(accountID string) deviceFP {
	sum := sha256.Sum256([]byte(accountID + "#fp"))
	n := uint64(0)
	for _, b := range sum[:8] {
		n = n<<8 | uint64(b)
	}
	dev := 100_000_000_000_000_000 + n%900_000_000_000_000_000
	machine := sha256.Sum256([]byte(accountID + "#machine"))
	return deviceFP{
		DeviceID:  strconv.FormatUint(dev, 10),
		MachineID: fmt.Sprintf("%x", machine[:16]),
		Brand:     deviceBrands[int(sum[8])%len(deviceBrands)],
	}
}

// sessionID 确定性会话 ID：sha256(account\x1fconfig\x1fraw\x1ffunction)[:32]
// 组装成 UUID 形态。同一账号 + 同一模型的会话 ID 恒定（上游按会话做亲和）。
func sessionID(accountID string, m Model) string {
	material := strings.Join([]string{accountID, m.ID, rawNameOf(m), functionOf(m)}, "\x1f")
	sum := sha256.Sum256([]byte(material))
	h := fmt.Sprintf("%x", sum[:16])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func rawNameOf(m Model) string {
	if m.RawName != "" {
		return m.RawName
	}
	return m.ID
}

func functionOf(m Model) string {
	if m.Function != "" {
		return m.Function
	}
	return "chat_v3"
}

// ============ 请求头 ============

// chatHeaders raw_chat 请求头（TraeCode 桌面客户端指纹）。
func chatHeaders(acc *Account, m Model, requestID string) map[string]string {
	fp := fingerprintFor(acc.ID)
	h := map[string]string{
		"Content-Type":         "application/json",
		"Accept":               "text/event-stream",
		"Connection":           "keep-alive",
		"Authorization":        "Cloud-IDE-JWT " + acc.Token,
		"X-Cloudide-Token":     acc.Token,
		"X-Ide-Token":          acc.Token,
		"X-App-Id":             traeAppID,
		"X-App-Version":        "default",
		"X-App-Version-Code":   traeVersionCode,
		"X-Ide-Version":        traeIDEVersion,
		"X-Ide-Version-Code":   traeVersionCode,
		"X-Ide-Version-Type":   "stable",
		"X-Plugin-Channel":     "desktop",
		"X-App-Function":       functionOf(m),
		"X-Ide-Function":       functionOf(m),
		"X-Request-Id":         requestID,
		"X-Custom-Trace-Id":    randHex(16),
		"X-Flow-Traceparent":   "04-" + randHex(16) + "-" + randHex(8) + "-01",
		"request-traffic-type": "prod",
		"X-Uid":                acc.ID,
		"x-device-id":          fp.DeviceID,
		"x-machine-id":         fp.MachineID,
		"x-device-cpu":         "AMD",
		"x-device-brand":       fp.Brand,
		"x-device-type":        "windows",
		"x-os-version":         "Windows 10",
		"x-system-type":        "Windows",
		"x-ide-version":        traeIDEVersion,
		"x-ide-version-code":   traeVersionCode,
		"x-request-id":         requestID,
		"User-Agent":           uaEmpty,
	}
	return h
}

// webHeaders web 模型目录请求头（remote_base /models）。
func webHeaders(token, edition string) map[string]string {
	h := map[string]string{
		"Authorization":          "Cloud-IDE-JWT " + token,
		"Content-Type":           "application/json",
		"Accept":                 "application/json",
		"X-Trae-Client-Type":     "web",
		"X-Preferenced-Language": "zh-CN",
		"x-user-region":          "CN",
		"Origin":                 "https://solo.trae.cn",
		"Referer":                "https://solo.trae.cn/",
		"User-Agent":             uaWeb,
	}
	if edition == "sg" || edition == "solo-sg" {
		h["X-Preferenced-Language"] = "en"
		h["x-user-region"] = "US"
		h["Origin"] = "https://solo.trae.ai"
		h["Referer"] = "https://solo.trae.ai/"
	}
	return h
}

// oauthHeaders OAuth/账号域请求头（刷新 / GetUserInfo / 额度）。
// GetUserInfo 只认 X-Cloudide-Token（带 Authorization 反而 401，实测），
// 所以两个头都带上。
func oauthHeaders(token string, deviceID16 string) map[string]string {
	return map[string]string{
		"Content-Type":     "application/json",
		"Authorization":    "Cloud-IDE-JWT " + token,
		"X-Cloudide-Token": token,
		"x-device-id":      deviceID16,
		"x-device-brand":   "ASUS TUF Gaming A15 FA507RM_FA507RM",
		"x-device-type":    "windows",
		"x-os-version":     "Windows 10 Pro",
		"x-app-version":    "3.3.65",
	}
}

// deviceID16 账号维度的 16 位数字设备号（OAuth/签到/额度域用）：
// sha256(accountID) 十进制取模 10^16 补零，跨 token 刷新稳定。
func deviceID16(accountID string) string {
	return deviceID16Gen(accountID, 0)
}

// deviceID16Gen 按代际派生 16 位设备 ID：材料 = 账号ID（gen 0）或
// 账号ID#gen{N}（9074 轮换后）。同代际跨 token 刷新/重启稳定。
func deviceID16Gen(accountID string, generation int) string {
	material := accountID
	if generation > 0 {
		material = fmt.Sprintf("%s#gen%d", accountID, generation)
	}
	sum := sha256.Sum256([]byte(material))
	n := uint64(0)
	for _, b := range sum[:8] {
		n = n<<8 | uint64(b)
	}
	s := strconv.FormatUint(n%1_0000_0000_0000_0000, 10)
	for len(s) < 16 {
		s = "0" + s
	}
	return s
}

// ============ 请求体 ============

// BuildChatBody 构造 raw_chat 请求体。raw 端点只接受文本消息（上游
// build_raw_chat_body docstring：带 OpenAI tools 字段会在模型产出前被上游
// 掐断——生产实测 tools≥38 的 agent 请求成簇 EOF 502，tools=0 全 200）：
//   - 工具定义文本化进运行时 system 消息（schema 48k 字符预算降级紧凑签名），
//     body 不携带 tools/tool_choice/parallel_tool_calls；
//   - 历史 tool_calls / tool 消息文本化（调用摘要 + "Client tool result" 文本），
//     历史压缩（80 条 / 120k 字符，env 可调）；
//   - 图片保留 renderer image_url 块。
func BuildChatBody(params map[string]any, m Model, accountID string) map[string]any {
	body := map[string]any{
		"config_name":       m.ID,
		"model_name":        rawNameOf(m),
		"conversation_id":   sessionID(accountID, m),
		"session_id":        sessionID(accountID, m),
		"stream":            true,
		"raw_chat_function": functionOf(m),
		"config_source":     1,
	}
	msgs, _ := params["messages"].([]any)
	toolDefs, hasToolsKey := normToolDefs(params["tools"])
	body["messages"] = buildRawMessages(msgs, toolDefs, hasToolsKey, params)
	// reasoning_effort 透传（raw_chat v2 可选字段白名单成员）
	if eff := firstString(params, "reasoning_effort", "reasoningEffort"); eff != "" {
		body["reasoning_effort"] = eff
	}
	return body
}

// normToolDefs 客户端 tools → 归一定义列表；键存在（含空列表）返回
// hasToolsKey=true（与上游 "tools" in options 语义一致）。
func normToolDefs(v any) ([]map[string]any, bool) {
	list, ok := v.([]any)
	if !ok {
		if v != nil {
			return nil, true
		}
		return nil, false
	}
	defs := make([]map[string]any, 0, len(list))
	seen := map[string]bool{}
	for _, raw := range list {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		d, ok := normToolDef(m)
		if !ok || seen[d["name"].(string)] {
			continue
		}
		seen[d["name"].(string)] = true
		defs = append(defs, d)
	}
	return defs, true
}

// convertImageBlock OpenAI image_url → Trae 渲染器块：
// data:image/png;base64,XXXX → {"type":"image_url","value":{"mimeType":"image/png","data":"XXXX"}}；
// http(s) URL → {"mimeType":"url","data":<url>}（images.py renderer_block）。
func convertImageBlock(url string, orig map[string]any) map[string]any {
	block := map[string]any{"type": "image_url"}
	if strings.HasPrefix(url, "data:") {
		mime, data := "image/png", ""
		if i := strings.Index(url, ","); i >= 0 {
			meta := url[len("data:"):i]
			data = url[i+1:]
			if j := strings.Index(meta, ";"); j >= 0 {
				mime = meta[:j]
			} else if meta != "" {
				mime = meta
			}
		}
		block["value"] = map[string]any{"mimeType": mime, "data": data}
		return block
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		block["value"] = map[string]any{"mimeType": "url", "data": url}
		return block
	}
	// 无法识别的形态：原样保留 image_url（含 detail 等字段）
	if orig != nil {
		block["image_url"] = orig
	}
	return block
}

// ============ SSE 事件解析 ============

// Usage 用量（上游多拼写映射，sse.py _map_usage）。
type Usage struct {
	Prompt     int64
	Completion int64
	Total      int64
	Credits    float64
}

// ToolCall 聚合完成的工具调用（流末整块发出，不发增量碎片）。
type ToolCall struct {
	Index    int
	ID       string
	Name     string
	ArgsJSON string
}

// StreamEvent 归一化后的流事件。
type StreamEvent struct {
	Type string // text | reasoning | tool_calls | usage | done | error | queue
	Text string
	// ToolCalls 非空表示本轮工具调用聚合完成（done 前最后一次 tool_calls 事件）。
	ToolCalls []ToolCall
	Usage     *Usage
	// FinishReason done 事件的原始 finish_reason（stop/tool_calls/length/...）。
	FinishReason string
	// Code/Message error 事件的业务码与文案（码可能为空）。
	Code    string
	Message string
}

// Stream 一条上游 SSE 的解析游标。
type Stream struct {
	resp *http.Response
	sc   *bufio.Scanner

	// 读取超时体系（对齐上游 trae_remote_client.py：首事件 120s / 流中
	// 空闲 300s，均可在 env 覆盖，0 = 关闭）。到点直接关闭底层响应体，
	// Next 以读错误返回——下游按断流处理（重试 / Work 回落），僵尸流不再
	// 无限占用会话闸门槽位（2026-10-29）。
	firstEvTimer *time.Timer
	idleTimer    *time.Timer
	firstEvSeen  bool

	// transport 形态：remote=true 走 chat_sessions 事件翻译（remote.go），
	// false = raw_chat 原生协议。Transport 是对外档位名（raw/remote/work），
	// 按端点封禁（2026-10-27）据此把封禁落到对应档位。
	remote    bool
	Transport string
	// Tier 档位名（remote/work/raw），会话型传输的回落判断用；
	// raw 流与旧数据为空。
	Tier       string
	remoteStop *remoteStopInfo
	// slotRelease 按账号会话闸门槽位释放（remoteChatOnce 成功后挂入，
	// Close 时在 stop 之后调用）。
	slotRelease func()
	// pending 消化单帧产出多个事件（如 plan_item 的思考+正文）。
	pending []*StreamEvent

	// 聚合状态：文本/思考各自独立累计（快照/增量混合处理），
	// 工具调用按 index 分槽合并（args 跨帧拼接）。
	textAcc  string
	thinkAcc string
	tools    map[int]*ToolCall
	toolOrd  []int
	lastPos  int // 排队位置去重

	// 文本协议状态（仅 raw 传输）：textAcc 累计原文里的 <opencode_tool_call>
	// 块提取出的工具调用与剥离后的可见文本快照。
	protoKeys map[string]bool
	protoVis  string

	Usage   *Usage
	Finish  string
	SawDone bool
	Err     *HTTPError
}

// Next 返回下一个归一化事件；EOF 且已终止返回 nil, nil。
//
// 上游是标准 SSE：`event:` 名与紧随其后的 `data:` 是一帧，空行结束一帧。
// 事件名必须跨行保留（曾经的 bug：pending 声明在循环体内，event 名每行都被
// 重置，导致 event: error / done / token_usage 全部退化成按 payload 猜名字，
// 上游错误被当成无内容断流、用量丢失、done 识别失败）。
func (s *Stream) Next() (*StreamEvent, error) {
	// plan_item 等单帧多事件的余量先行
	if len(s.pending) > 0 {
		ev := s.pending[0]
		s.pending = s.pending[1:]
		return ev, nil
	}
	pending := ""
	for {
		s.touchIdleWatch() // 本次阻塞读的空闲计时（首事件前由首事件计时接管）
		if !s.sc.Scan() {
			break
		}
		line := strings.TrimSpace(s.sc.Text())
		if line == "" {
			pending = "" // 空行 = 事件边界
			continue
		}
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "event:") {
			pending = normalizeEventName(strings.TrimSpace(line[6:]))
			continue
		}
		if !strings.HasPrefix(low, "data:") {
			continue // 心跳注释行（: keepalive）等
		}
		payload := strings.TrimSpace(line[5:])
		evName := pending
		pending = "" // 一帧 data 消费后事件名失效
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			s.SawDone = true
			return &StreamEvent{Type: "done", FinishReason: s.Finish}, nil
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(payload), &obj); err != nil {
			// 非 JSON data 行：代理注入的纯文本忽略；但 error 事件必须上抛，
			// 否则又是一次"错误被吞成空流"。
			if evName == "error" {
				s.Err = &HTTPError{Body: payload}
				return &StreamEvent{Type: "error", Message: payload}, nil
			}
			continue
		}
		if evName == "" {
			evName = normalizeEventName(payloadEventName(obj))
		}
		if ev, handled := s.consume(obj, evName); handled {
			switch ev.Type {
			case "text", "reasoning", "tool_calls", "usage", "done":
				s.markModelEvent()
			}
			return ev, nil
		}
	}
	if err := s.sc.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

// Close 释放底层响应体；停掉读取超时计时器；remote 会话未见 done 时尽力
// stop（原项目语义：提前失败不留挂着的事件流会话；异步执行，对齐上游
// asyncio.create_task——stop 经代理实测 7~9s，同步会拖慢断流重试路径），
// stop 之后才释放账号会话槽位（对齐上游 cleanup_remote_session：先 stop
// 再 release slot）。
func (s *Stream) Close() {
	s.stopStreamWatches()
	if s.resp != nil && s.resp.Body != nil {
		s.resp.Body.Close()
	}
	if s.remote && s.remoteStop != nil && !s.SawDone {
		go s.stopRemoteSession()
	}
	if s.slotRelease != nil {
		s.slotRelease()
	}
}

// stopRemoteSession POST /chat_sessions/{sid}/stop（尽力而为；结果落日志，
// 用于验证卡死会话清理是否真实生效——2026-10-29 排查期间 stop 静默吞错）。
func (s *Stream) stopRemoteSession() {
	info := s.remoteStop
	body, _ := json.Marshal(map[string]any{
		"chat_session_id": info.sessionID,
		"user_message_id": info.messageID,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, info.base+"/chat_sessions/"+info.sessionID+"/stop", bytes.NewReader(body))
	if err != nil {
		log.Printf("trae stop session %s: build request failed: %v", kit.Truncate(info.sessionID, 16), err)
		return
	}
	for k, v := range info.headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	client := httpClientFor(info.proxyURL)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("trae stop session %s: FAILED after %dms: %v", kit.Truncate(info.sessionID, 16), time.Since(start).Milliseconds(), err)
		return
	}
	resp.Body.Close()
	log.Printf("trae stop session %s: status=%d %dms", kit.Truncate(info.sessionID, 16), resp.StatusCode, time.Since(start).Milliseconds())
}

// consume 处理一个 data 帧；返回 (事件, 是否产出事件)。
func (s *Stream) consume(obj map[string]any, evName string) (*StreamEvent, bool) {
	// remote chat_sessions 事件族单独翻译（remote.go 注释里的协议对照）
	if s.remote {
		return s.consumeRemote(obj, evName)
	}
	// error 事件：先于一切内容检查（上游有时先吐模型输出再报错）
	if evName == "error" {
		code, msg := extractError(obj)
		s.Err = &HTTPError{Code: code, Body: msg}
		s.classifyStreamError()
		return &StreamEvent{Type: "error", Code: code, Message: msg}, true
	}
	switch evName {
	case "keepalive":
		return nil, false
	case "model_config":
		return nil, false // provider_model_name 仅日志价值，路由按请求模型走
	case "request_wait_in_queue":
		pos := int(numField(obj, "position"))
		if pos > 0 && pos != s.lastPos {
			s.lastPos = pos
			return &StreamEvent{Type: "queue", Text: fmt.Sprintf("排队中，当前位置：%d\n", pos)}, true
		}
		return nil, false
	case "token_usage":
		u := mapUsage(obj)
		if u != nil {
			s.Usage = u
			return &StreamEvent{Type: "usage", Usage: u}, true
		}
		return nil, false
	case "done":
		s.SawDone = true
		if fr := payloadFinishReason(obj); fr != "" {
			s.Finish = fr
		}
		if u := mapUsage(obj); u != nil && s.Usage == nil {
			s.Usage = u
		}
		return &StreamEvent{Type: "done", FinishReason: s.Finish}, true
	}

	// 文本/思考/工具调用混在 output、text_delta、assistant_message 等事件里
	calls := extractToolCalls(obj)
	if len(calls) > 0 {
		s.mergeToolCalls(calls)
	}
	if txt := payloadTextFor(evName, obj); txt != "" {
		delta, textChanged := s.addText(txt)
		if textChanged {
			if s.Transport == "raw" {
				// 文本协议：累计原文里提取 <opencode_tool_call> 块并产出
				// 剥离后的可见增量（见 extractTextToolCalls）。
				if ev, produced := s.protoConsume(); produced {
					return ev, true
				}
			} else if delta != "" {
				return &StreamEvent{Type: "text", Text: delta}, true
			}
		}
	}
	if th := payloadReasoning(obj); th != "" {
		delta, changed := s.addReasoning(th)
		if changed && delta != "" {
			return &StreamEvent{Type: "reasoning", Text: delta}, true
		}
	}
	if fr := payloadFinishReason(obj); fr != "" {
		s.Finish = fr
	}
	return nil, false
}

// classifyStreamError 把流内 error 事件的业务码分类到 HTTPError。
func (s *Stream) classifyStreamError() {
	if s.Err == nil {
		return
	}
	e := s.Err
	switch {
	case e.Code == "4001" || strings.Contains(strings.ToLower(e.Body), "param is invalid"):
		e.ParamError = true
	case isCapacityCode(e.Code), ContainsQuotaText(e.Body):
		e.RateLimited = true
	}
}

// ============ remote chat_sessions 事件翻译（remote.go 协议对照） ============

// consumeRemote chat_sessions 事件帧 → 归一化 StreamEvent。
// 对照原项目 sse.py translate_web_events：
//   - heartbeat/keepalive/ping → 忽略（afree 聚合层自行保活）
//   - plan_item → thought=思考快照，content=可见正文快照（两者独立累计）
//   - message|assistant_message|response|text|output → 可见正文快照
//   - token_usage / done / error 与 raw_chat 同构
func (s *Stream) consumeRemote(obj map[string]any, evName string) (*StreamEvent, bool) {
	if remoteDebugEvents {
		if raw, err := json.Marshal(obj); err == nil {
			limit := 400
			if evName == "plan_item" || evName == "message" || evName == "assistant_message" {
				limit = 1600
			}
			log.Printf("trae remote event name=%s data=%s", evName, kit.Truncate(string(raw), limit))
		}
	}
	switch evName {
	case "error":
		code, msg := extractError(obj)
		if raw, err := json.Marshal(obj); err == nil {
			log.Printf("trae remote stream error event: %s", kit.Truncate(string(raw), 800))
		}
		s.Err = &HTTPError{Code: code, Body: msg}
		s.classifyStreamError()
		return &StreamEvent{Type: "error", Code: code, Message: msg}, true
	case "heartbeat", "keepalive", "ping", "model_config", "keep_alive":
		return nil, false
	case "token_usage":
		if u := mapUsage(obj); u != nil {
			s.Usage = u
			return &StreamEvent{Type: "usage", Usage: u}, true
		}
		return nil, false
	case "done", "response_done", "stream_done":
		s.SawDone = true
		if fr := payloadFinishReason(obj); fr != "" {
			s.Finish = fr
		}
		if u := mapUsage(obj); u != nil && s.Usage == nil {
			s.Usage = u
		}
		// 原项目收尾兜底（sse.py 翻译器末段）：SOLO Remote 执行器会把
		// 整个回答放在 plan_item.thought（reasoning 通道），final summary
		// 挂在 tool_call_info(finish).params.summary。无可见正文时把思考
		// 摘要作为正文输出——否则纯文字对话 content 恒为空。thinkAcc 已在
		// plan_item 里做过工具块剥离，这里直接输出。
		if s.textAcc == "" && s.thinkAcc != "" {
			s.pending = append(s.pending, &StreamEvent{Type: "done", FinishReason: s.Finish})
			return &StreamEvent{Type: "text", Text: s.thinkAcc}, true
		}
		return &StreamEvent{Type: "done", FinishReason: s.Finish}, true
	case "plan_item":
		var out []*StreamEvent
		// thought 与 reasoning_content 是 plan_item 的两个独立字段：推理轨迹
		// 走 reasoning_content（逐帧增长快照），**工具调用块落 thought**
		//（2026-10-30 实测：get_weather 调用整块在 thought、reasoning_content
		// 此时不变）。分开提取：reasoning_content → 思考通道；thought 先剥
		// 文本工具块（调用并入 tool_calls）再并进思考通道，客户端看不到
		// 协议原文。
		if th := firstString(obj, "reasoning_content", "reasoningContent"); th != "" {
			if delta, changed := s.addReasoning(th); changed && delta != "" {
				out = append(out, &StreamEvent{Type: "reasoning", Text: delta})
			}
		}
		if thought := firstString(obj, "thought", "thinking"); thought != "" {
			vis := s.protoStrip(thought)
			if vis != "" {
				if delta, changed := s.addReasoning(vis); changed && delta != "" {
					out = append(out, &StreamEvent{Type: "reasoning", Text: delta})
				}
			}
		}
		if txt := webPlanContent(obj); txt != "" {
			if delta, changed := s.addText(s.protoStrip(txt)); changed && delta != "" {
				out = append(out, &StreamEvent{Type: "text", Text: delta})
			}
		}
		if fr := payloadFinishReason(obj); fr != "" {
			s.Finish = fr
		}
		if len(out) == 0 {
			return nil, false
		}
		s.pending = append(s.pending, out[1:]...)
		return out[0], true
	}
	if !textBearingEvents[evName] {
		return nil, false
	}
	// message 族：正文可能嵌套在 message/agent_message/assistant_message
	// 子对象里，content 可能是块数组（webMessageText 全形态解析）。
	if txt := webMessageText(obj); txt != "" {
		if calls := extractToolCalls(obj); len(calls) > 0 {
			s.mergeToolCalls(calls)
		}
		if delta, changed := s.addText(txt); changed && delta != "" {
			return &StreamEvent{Type: "text", Text: delta}, true
		}
	}
	if fr := payloadFinishReason(obj); fr != "" {
		s.Finish = fr
	}
	return nil, false
}

// webMessageText remote message 族事件可见正文（原项目 _web_message_text）。
func webMessageText(obj map[string]any) string {
	data := obj
	for _, key := range []string{"message", "agent_message", "assistant_message"} {
		if nested, ok := obj[key].(map[string]any); ok {
			data = nested
			break
		}
	}
	if c, ok := data["content"].(string); ok {
		return c
	}
	if blocks, ok := data["content"].([]any); ok {
		var b strings.Builder
		for _, block := range blocks {
			switch s := block.(type) {
			case string:
				b.WriteString(s)
			case map[string]any:
				bt, _ := s["type"].(string)
				switch strings.ToLower(bt) {
				case "reasoning", "thinking", "reasoning_text":
					continue // 思考块不计入可见正文
				}
				if t, ok := s["text"].(string); ok {
					b.WriteString(t)
				} else if t, ok := s["content"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	}
	if v := firstString(data, "text", "response", "answer", "output"); v != "" {
		return v
	}
	for _, key := range []string{"text", "response", "answer", "output"} {
		if blocks, ok := data[key].([]any); ok {
			var b strings.Builder
			for _, block := range blocks {
				switch s := block.(type) {
				case string:
					b.WriteString(s)
				case map[string]any:
					if t, ok := s["text"].(string); ok {
						b.WriteString(t)
					}
				}
			}
			if b.Len() > 0 {
				return b.String()
			}
		}
	}
	return ""
}

// webPlanContent plan_item 可见正文（content 字段；thought 走 reasoning 通道）。
func webPlanContent(obj map[string]any) string {
	return webMessageText(map[string]any{"content": obj["content"]})
}

// addText 快照/增量混合聚合（sse.py ProtocolTextAccumulator 语义）：
// 新文本以已累计文本为前缀 → 快照延长；公共前缀 ≥4 字符且更长 → 快照替换；
// 否则视为纯增量追加。返回本轮可见增量。
func (s *Stream) addText(txt string) (string, bool) {
	delta, next, changed := accumulate(s.textAcc, txt)
	if changed {
		s.textAcc = next
	}
	return delta, changed
}

// protoStrip 输入文本(常为快照)里的文本工具块提取进工具槽位后返回剥离后的
// 可见文本——remote plan_item thought/content 通道用(调用不产出 text 事件)。
func (s *Stream) protoStrip(txt string) string {
	if !strings.Contains(txt, "<") {
		return txt
	}
	visible, calls := extractTextToolCalls(txt)
	for _, c := range calls {
		key := c.Name + "\x00" + c.ArgsJSON
		if s.protoKeys == nil {
			s.protoKeys = map[string]bool{}
		}
		if s.protoKeys[key] {
			continue
		}
		s.protoKeys[key] = true
		s.mergeToolCalls([]ToolCall{{Index: 1000 + len(s.toolOrd), Name: c.Name, ArgsJSON: c.ArgsJSON}})
	}
	return visible
}

// protoConsume raw 传输的文本协议处理：从累计原文提取文本工具块（新调用并入
// 工具槽位，序号从 1000 起，避免与原生 index 撞槽），并产出剥离后可见文本的
// 增量。可见文本缩短（快照替换语义）只更新快照，无可回撤增量。
func (s *Stream) protoConsume() (*StreamEvent, bool) {
	visible, calls := extractTextToolCalls(s.textAcc)
	for _, c := range calls {
		key := c.Name + "\x00" + c.ArgsJSON
		if s.protoKeys == nil {
			s.protoKeys = map[string]bool{}
		}
		if s.protoKeys[key] {
			continue
		}
		s.protoKeys[key] = true
		s.mergeToolCalls([]ToolCall{{Index: 1000 + len(s.toolOrd), Name: c.Name, ArgsJSON: c.ArgsJSON}})
	}
	if visible == s.protoVis {
		return nil, false
	}
	delta := ""
	if strings.HasPrefix(visible, s.protoVis) {
		delta = visible[len(s.protoVis):]
	} else if !strings.HasPrefix(s.protoVis, visible) {
		// 新剥离区间打断前缀关系：从公共前缀之后继续
		if n := commonPrefixLen(s.protoVis, visible); len(visible) > n {
			delta = visible[n:]
		}
	}
	s.protoVis = visible
	if delta == "" {
		return nil, false
	}
	return &StreamEvent{Type: "text", Text: delta}, true
}

func (s *Stream) addReasoning(txt string) (string, bool) {
	delta, next, changed := accumulate(s.thinkAcc, txt)
	if changed {
		s.thinkAcc = next
	}
	return delta, changed
}

// accumulate 快照/增量判定的公共实现。
func accumulate(acc, txt string) (delta, next string, changed bool) {
	if txt == acc {
		return "", acc, false
	}
	if strings.HasPrefix(txt, acc) {
		return txt[len(acc):], txt, true
	}
	common := commonPrefixLen(acc, txt)
	shorter := len(acc)
	if len(txt) < shorter {
		shorter = len(txt)
	}
	if common >= 4 || (shorter > 0 && common*2 >= shorter) {
		if len(txt) > len(acc) {
			return txt[common:], txt, true
		}
		return "", acc, false // 快照缩短（少见）：无可回撤的增量，忽略
	}
	return txt, acc + txt, true
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// collectedToolCalls 按出现顺序返回聚合完成的工具调用副本。
func (s *Stream) collectedToolCalls() []ToolCall {
	out := make([]ToolCall, 0, len(s.toolOrd))
	for _, idx := range s.toolOrd {
		if tc := s.tools[idx]; tc != nil {
			out = append(out, *tc)
		}
	}
	return out
}

// mergeToolCalls 合并一帧工具调用分片：name/id 空值不覆盖、args 拼接
// （test_sse.py 的原生分片形态：先 name 后 arguments 分帧到达）。
func (s *Stream) mergeToolCalls(calls []ToolCall) {
	if s.tools == nil {
		s.tools = map[int]*ToolCall{}
	}
	for _, c := range calls {
		cur, ok := s.tools[c.Index]
		if !ok {
			cp := c
			s.tools[c.Index] = &cp
			s.toolOrd = append(s.toolOrd, c.Index)
			continue
		}
		if c.ID != "" && cur.ID == "" {
			cur.ID = c.ID
		}
		if c.Name != "" && cur.Name == "" {
			cur.Name = c.Name
		}
		cur.ArgsJSON += c.ArgsJSON
	}
}

// ToolCallsSnapshot 已聚合的完整工具调用（流结束时取）。
func (s *Stream) ToolCallsSnapshot() []ToolCall { return s.collectedToolCalls() }

// ============ 事件字段提取（多拼写兜底，键名来自上游实测） ============

var eventNameAliases = map[string]string{
	"keepalive": "keepalive", "modelconfig": "model_config", "planitem": "plan_item",
	"requestwaitinqueue": "request_wait_in_queue", "responsedone": "done",
	"streamdone": "done", "tokenusage": "token_usage", "textdelta": "text_delta",
	"reasoningdelta": "reasoning_delta", "assistantmessage": "assistant_message",
}

// normalizeEventName 事件名归一：驼峰/连字符 → snake_case + 别名表。
func normalizeEventName(ev string) string {
	ev = strings.TrimSpace(ev)
	if ev == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range ev {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
		case r == '-':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if alias, ok := eventNameAliases[strings.ReplaceAll(out, "_", "")]; ok {
		return alias
	}
	return out
}

// payloadEventName data JSON 内嵌事件名（event/event_type/eventType/type）。
func payloadEventName(obj map[string]any) string {
	for _, k := range []string{"event", "event_type", "eventType", "type"} {
		if v, ok := obj[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// textBearingEvents 允许产出可见文本的事件名（"" = 仅 data: 行的旧协议）。
// extra_info / timing_cost / metadata 一类事件即便带 content 也不能渲染：
// 实测 extra_info.content 是整段 reasoning 的 JSON 序列化，误当正文会把它
// 原样拼进回答（2026-10-26 solo_agent 实测）。
var textBearingEvents = map[string]bool{
	"": true, "output": true, "response": true, "message": true,
	"assistant_message": true, "text_delta": true, "translated_text": true,
	"accumulated": true, "text": true,
}

// payloadTextFor 按事件名决定是否提取文本；非文本事件返回空。
func payloadTextFor(evName string, obj map[string]any) string {
	if !textBearingEvents[evName] {
		return ""
	}
	return payloadText(obj)
}

// payloadText 文本增量/快照：事件名族 × 字段族双兜底。
func payloadText(obj map[string]any) string {
	return firstString(obj,
		"delta", "text_delta", "textDelta", "text", "content", "response", "output", "answer")
}

// payloadReasoning 思考增量/快照字段族。
func payloadReasoning(obj map[string]any) string {
	return firstString(obj, "reasoning_content", "reasoningContent", "thought", "thinking")
}

// payloadFinishReason 终止原因字段族（含 message.response_meta 嵌套）。
func payloadFinishReason(obj map[string]any) string {
	if fr := firstString(obj, "finish_reason", "stop_reason", "finishReason", "stopReason"); fr != "" {
		return fr
	}
	if msg, ok := obj["message"].(map[string]any); ok {
		if fr := firstString(msg, "finish_reason", "stop_reason", "finishReason", "stopReason"); fr != "" {
			return fr
		}
		if rm, ok := msg["response_meta"].(map[string]any); ok {
			return firstString(rm, "finish_reason", "stop_reason", "finishReason", "stopReason")
		}
	}
	return ""
}

// firstString 按键序取第一个非空字符串（顶层）。
func firstString(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// numField 宽松取数字字段（int/int64/float64/json.Number）。
func numField(obj map[string]any, key string) float64 {
	switch v := obj[key].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	}
	return 0
}

// mapUsage 用量字段族映射（usage 可嵌套在 "usage" 键下或平铺在载荷上）。
func mapUsage(obj map[string]any) *Usage {
	src := obj
	if u, ok := obj["usage"].(map[string]any); ok && len(u) > 0 {
		src = u
	}
	u := &Usage{
		Prompt:     int64(numField(src, "prompt_tokens")),
		Completion: int64(numField(src, "completion_tokens")),
	}
	if u.Prompt == 0 {
		u.Prompt = int64(firstNum(src, "input_tokens", "inputTokens", "input_token", "inputToken"))
	}
	if u.Completion == 0 {
		u.Completion = int64(firstNum(src, "output_tokens", "outputTokens", "output_token", "outputToken"))
	}
	u.Total = int64(firstNum(src, "total_tokens", "totalTokens", "total_token", "totalToken"))
	if u.Total == 0 {
		u.Total = u.Prompt + u.Completion
	}
	u.Credits = firstNum(src, "credits_consumed", "consumed_credits", "credit_cost", "credits_cost", "credits_float", "creditsFloat")
	if u.Prompt == 0 && u.Completion == 0 && u.Total == 0 && u.Credits == 0 {
		return nil
	}
	return u
}

func firstNum(obj map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if v := numField(obj, k); v != 0 {
			return v
		}
	}
	return 0
}

// extractError 错误业务码与文案：code|error_code|errorCode（顶层或 error.* 嵌套）。
// 文案为空时退回 extra（上游部分错误只带 {"code":1005,"extra":"{\"plan\":3}"}）。
func extractError(obj map[string]any) (code, msg string) {
	code = errCodeOf(obj)
	if e, ok := obj["error"].(map[string]any); ok {
		if code == "" {
			code = errCodeOf(e)
		}
		if msg == "" {
			msg = firstString(e, "message", "msg")
		}
	}
	if msg == "" {
		msg = firstString(obj, "message", "msg", "extra")
	}
	return code, msg
}

func errCodeOf(obj map[string]any) string {
	for _, k := range []string{"code", "error_code", "errorCode"} {
		switch v := obj[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

// extractToolCalls 从载荷提取工具调用分片（sse.py _calls_from_calls 键族）：
// 顶层或 message.* 的 tool_calls|tool_call|function_call|toolCallInfo，以及
// message.content 内 type:tool_use|tool_call|function_call|function 块。
func extractToolCalls(obj map[string]any) []ToolCall {
	var out []ToolCall
	collect := func(list []any) {
		for _, raw := range list {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			idx := int(numField(m, "index"))
			tc := ToolCall{Index: idx}
			tc.ID = firstString(m, "id", "tool_call_id", "toolCallId")
			// function_call / function 嵌套形态
			fn, _ := m["function"].(map[string]any)
			if fn == nil {
				fn, _ = m["function_call"].(map[string]any)
			}
			if fn != nil {
				if tc.Name == "" {
					tc.Name = firstString(fn, "name")
				}
				tc.ArgsJSON += firstString(fn, "arguments", "args", "input", "params", "parameters")
			}
			if tc.Name == "" {
				tc.Name = firstString(m, "name")
			}
			if tc.ArgsJSON == "" {
				tc.ArgsJSON += firstString(m, "arguments", "args", "input", "params", "parameters")
			}
			if tc.Name == "" && tc.ArgsJSON == "" && tc.ID == "" {
				continue
			}
			out = append(out, tc)
		}
	}
	for _, key := range []string{"tool_calls", "tool_call", "function_call", "toolCallInfo", "tool_call_info"} {
		switch v := obj[key].(type) {
		case []any:
			collect(v)
		case map[string]any:
			collect([]any{v})
		}
	}
	if msg, ok := obj["message"].(map[string]any); ok {
		for _, key := range []string{"tool_calls", "tool_call", "function_call"} {
			if v, ok := msg[key].([]any); ok {
				collect(v)
			}
		}
		if blocks, ok := msg["content"].([]any); ok {
			for _, b := range blocks {
				bm, ok := b.(map[string]any)
				if !ok {
					continue
				}
				bt, _ := bm["type"].(string)
				switch bt {
				case "tool_use", "tool_call", "function_call", "function":
					collect([]any{bm})
				}
			}
		}
	}
	return out
}

// ============ 错误分类 ============

// isCapacityCode 容量/限流业务码族（3004 并发限流、4008/4011 配额、429）。
func isCapacityCode(code string) bool {
	switch code {
	case "3004", "4008", "4011", "429":
		return true
	}
	return false
}

// ContainsQuotaText 文本形态的配额错误（sse.py _is_account_quota_error）。
func ContainsQuotaText(body string) bool {
	low := strings.ToLower(body)
	for _, kw := range []string{"rate limit", "too many requests", "quota exceeded", "requests have exceeded the quota", "parallel"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// parseUpstreamError 解析非 200 响应体为分类错误。HTTP 429 一律判限流；
// 其余看 body 业务码（code == "3004" 判限流并停止候选回退）。
func parseUpstreamError(status int, body []byte) *HTTPError {
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) > 800 {
		trimmed = trimmed[:800]
	}
	e := &HTTPError{Status: status, Body: trimmed}
	if status == http.StatusTooManyRequests {
		e.Code = "429"
		e.RateLimited = true
		return e
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err == nil {
		e.Code, _ = extractError(obj)
	}
	switch {
	case isCapacityCode(e.Code):
		e.RateLimited = true
	case e.Code == "4001" || strings.Contains(strings.ToLower(trimmed), "param is invalid"):
		e.ParamError = true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		e.AuthFailure = true
	}
	return e
}

// ============ 主调用 ============

// ChatOpts 一次 chat 调用的参数。
type ChatOpts struct {
	Model         Model
	Params        map[string]any // 客户端原始参数（messages/tools/...）
	Strategy      string
	BoundRoutable func(*Account) bool
	// PinAccount 固定账号（面板 Test 探测）。
	PinAccount *Account
	// PreferWorkTier chat_sessions 传输使用 TraeWork 执行器
	//（solo_work_remote）；false = Agent 执行器（solo_agent_remote）。
	// 由 transportSelector / remoteWorkChatOnce 设置，端点钉 traework 时等效 true。
	PreferWorkTier bool
}

// Chat 执行一次上游 chat：选号（含 token 预刷新）→ 出口解析 → 网关/端点
// 候选回退 → 账号轮转重试。返回解析就绪的 Stream（调用方负责 Close）。
// 任何输出到达客户端前失败都在本函数内消化；流中途断开由调用方缓冲重试。
func Chat(ctx context.Context, opts ChatOpts) (*Stream, *Account, error) {
	if !acquireSem(ctx) {
		return nil, nil, fmt.Errorf("client aborted: %w", ctx.Err())
	}
	defer releaseSem()

	c := Get()
	retries := c.Retries
	if retries <= 0 {
		retries = 3
	}
	primary, fallbackFn := transportSelector(opts)
	fallbackUsed := false
	delay := time.Second
	rateLimited := 0
	for attempt := 0; ; attempt++ {
		var acc *Account
		if opts.PinAccount != nil {
			acc = opts.PinAccount
		} else {
			acc = PickAccount(opts.Strategy, opts.BoundRoutable)
		}
		if acc == nil {
			return nil, nil, fmt.Errorf("no trae account routable: all configured accounts are cooling, routing-disabled or bound-exit-blocked")
		}
		// token 过期前静默续期；失败按该账号不可用处理（换号，不回退直连）
		if err := EnsureFresh(acc); err != nil {
			if opts.PinAccount != nil {
				return nil, acc, fmt.Errorf("trae token refresh failed: %w", err)
			}
			Cooldown(acc.ID, 30*time.Minute)
			continue
		}
		proxyURL, ok := resolveExitFor(acc)
		if !ok {
			if opts.PinAccount != nil {
				return nil, acc, errors.New(kit.BoundProbeBlockedMsg("trae", acc.ID,
					fmt.Sprintf("account#%d bound proxies unavailable (cooling or removed); probe aborted by proxy isolation", AccountIndex(acc.ID))))
			}
			Cooldown(acc.ID, 2*time.Minute)
			continue
		}
		observeAttempt(ctx, acc, proxyURL)
		stream, err := primary(ctx, acc, opts, proxyURL)
		if err != nil && fallbackFn != nil && !fallbackUsed && remoteFallbackEligible(err) {
			// auto 分流回落（原项目 TRAE_AUTO_ROUTE 语义，单次）：
			// 纯文字 Remote 失败回落 raw_chat；工具/图片 raw_chat 失败回落 Remote。
			fallbackUsed = true
			stream, err = fallbackFn(ctx, acc, opts, proxyURL)
		}
		if err == nil {
			return stream, acc, nil
		}
		var he *HTTPError
		if errors.As(err, &he) {
			if he.RateLimited {
				rateLimited++
				Cooldown(acc.ID, time.Minute)
				if opts.PinAccount != nil {
					return nil, acc, he
				}
				continue // 换账号立即重试（同凭据必然同样限流，不试其他候选）
			}
			if he.AuthFailure {
				Cooldown(acc.ID, time.Hour)
				if opts.PinAccount != nil {
					return nil, acc, he
				}
				continue
			}
			if he.ParamError {
				return nil, acc, he // 参数错误换账号无法修复，立即上抛
			}
			if he.Code == "1005" || he.Code == "4023" {
				// 模型级永久错误（套餐/权益 1005、模型名未知 4023）：与流内
				// error 事件同判据（app 层自动封禁决策表），换账号、换网关候选
				// 都不会变——立即上抛，别打满 (retries+1)×候选 的重试轮次。
				return nil, acc, he
			}
		}
		if ctx.Err() != nil {
			return nil, acc, fmt.Errorf("client aborted: %w", ctx.Err())
		}
		if attempt < retries {
			if !sleepCtx(ctx, jitter(delay)) {
				return nil, acc, fmt.Errorf("client aborted during retry wait")
			}
			delay = nextDelay(delay)
			continue
		}
		return nil, acc, err
	}
}

// traeDumpWire 把请求头/体写入 DATA_DIR/trae-wire/<ts>-<rand>.json
// （token 截断到 16 字符，避免凭据整段落盘）。
func traeDumpWire(req *http.Request, bodyJSON []byte) {
	dir := kit.ResolveDataPath("trae-wire")
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return
	}
	headers := map[string]string{}
	for k, v := range req.Header {
		val := strings.Join(v, ",")
		if strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "X-Cloudide-Token") || strings.EqualFold(k, "X-Ide-Token") {
			if len(val) > 32 {
				val = val[:32] + "...[trunc]"
			}
		}
		headers[k] = val
	}
	rec, _ := json.Marshal(map[string]any{
		"at": time.Now().UTC().Format(time.RFC3339), "url": req.URL.String(),
		"headers": headers, "body": json.RawMessage(bodyJSON),
	})
	name := fmt.Sprintf("%s-%s.json", time.Now().UTC().Format("150405"), randHex(4))
	_ = os.WriteFile(dir+"/"+name, rec, 0o600)
}

// chatCandidates 网关 × 端点候选：默认网关带 api5 回退与 v1 端点回退；
// 自定义网关不回退（单候选）。
func chatCandidates() [][2]string {
	gw := strings.TrimRight(Get().Gateway, "/")
	if gw == DefaultGateway {
		return [][2]string{
			{DefaultGateway, rawChatV2Endpoint},
			{DefaultGateway, rawChatV1Endpoint},
			{fallbackGateway, rawChatV2Endpoint},
			{fallbackGateway, rawChatV1Endpoint},
		}
	}
	return [][2]string{{gw, rawChatV2Endpoint}}
}

// transportSelector 端点 → (主传输, 回落传输)。原项目端点语义 + 档位路由：
//   - remote / traework：固定 chat_sessions 传输，不回落；
//   - traecode：固定 raw_chat，不回落；
//   - auto（默认，原项目 TRAE_AUTO_ROUTE + 档位路由 2026-10-27 用户定）：
//     工具/图片请求 raw_chat 优先（IDE Agent，工具协议完整）；纯文字按模型
//     档位选传输——remote > traework > raw；主传输失败（可重试类错误）回落
//     一次（Remote ↔ raw，或 Remote → TraeWork，对齐原项目 work fallback）。
//     2026-10-28 用户定（一刀切）：档位被黑名单命中即视为不可用，主/回落
//     都只在「有档位且未被封」里选——兑现「封禁摘掉不可用档位，请求自动走
//     剩下那条，只要有一条路能走通就保留路由」。免费账号权限问题全局同因，
//     不按封禁原因（4023/11158/1005）分层。全部档位被封时兜底 raw（模型已
//     从目录剔除，显式请求让上游真实错误透出并再次落档封禁，幂等）。
//     模型无任何 chat_sessions 档时不分流，维持 raw_chat。
func transportSelector(opts ChatOpts) (primary, fallback func(context.Context, *Account, ChatOpts, string) (*Stream, error)) {
	ep := strings.ToLower(strings.TrimSpace(Get().Endpoint))
	switch ep {
	case "remote":
		return remoteChatOnce, nil
	case "traework":
		return remoteWorkChatOnce, nil
	case "traecode":
		return rawChatOnce, nil
	}
	// auto：档位可用性 = 有该档（能力前提）且该档未被黑名单命中。
	// banChecker 未注入时 modelBannedIn 恒 false，退化为纯 modelHasTier。
	usable := func(tier string) bool {
		return modelHasTier(opts.Model, tier) && !modelBannedIn(tier, opts.Model.ID)
	}
	// 工具/图片：remote 优先（2026-10-30 实测反转）：raw 端点对 flash 系
	// config（glm-5.3-flash 等六个闪模型）恒 4001 "param invalid"——探针把
	// body/头/TLS 指纹逐项对齐网关后依旧复现，raw 档纯文本最小请求也 4001，
	// 与请求形状无关，是上游按档位隔离了这些 config。同请求 remote 档
	// （chat_sessions + flattenQuery 文本化）实测 200（2 工具 85k 字产出、
	// 24 工具 agent 正常）。工具协议在 remote 档经 flattenRemoteQuery 文本化，
	// 响应侧由 Stream 的文本工具块解析承接。raw 降为回落（remote 失败时
	// 仍值得试一次：非 flash 系 config 的 raw 档在生产里 200 过）。
	// 降级仅针对「remote 档存在但被封」：remote 档根本不存在（能力缺失）
	// 维持 raw 直发。
	if requestHasToolsOrImages(opts.Params) {
		hasRemoteCap := modelHasTier(opts.Model, "remote")
		if hasRemoteCap && usable("remote") {
			switch {
			case usable("traework"):
				return remoteChatOnce, remoteWorkChatOnce
			case usable("traecode"):
				return remoteChatOnce, rawChatOnce
			default:
				return remoteChatOnce, nil
			}
		}
		if modelHasTier(opts.Model, "traecode") {
			// remote 缺档或被封:有 raw 能力就直发 raw(work 仅当 remote 缺档时
			// 才值得作回落,被封 remote 换 work 同因必败)
			if !hasRemoteCap && usable("traework") {
				return rawChatOnce, remoteWorkChatOnce
			}
			return rawChatOnce, nil
		}
		switch {
		case usable("traework"):
			return remoteWorkChatOnce, nil
		default:
			return rawChatOnce, nil
		}
	}
	// 纯文字：按可用档位选主传输（remote > traework > raw 优先级不变）
	switch {
	case usable("remote"):
		switch {
		case usable("traecode"):
			return remoteChatOnce, rawChatOnce
		case usable("traework"):
			return remoteChatOnce, remoteWorkChatOnce // Remote 失败回落 Work（原项目 work fallback）
		default:
			return remoteChatOnce, nil
		}
	case usable("traework"):
		if usable("traecode") {
			return remoteWorkChatOnce, rawChatOnce
		}
		return remoteWorkChatOnce, nil
	case usable("traecode"):
		return rawChatOnce, nil
	default:
		return rawChatOnce, nil
	}
}

// remoteFallbackEligible 回落只对"可重试"类失败生效：网络错误 / 5xx 等。
// 限流、认证失效、参数错误、模型/权益级错误（4023/1005）换传输也不会变，
// 走原有的换号/上抛逻辑。
func remoteFallbackEligible(err error) bool {
	var he *HTTPError
	if !errors.As(err, &he) {
		return true // 网络级错误
	}
	if he.RateLimited || he.AuthFailure || he.ParamError {
		return false
	}
	switch he.Code {
	case "1005", "4023":
		return false
	}
	return true
}

// requestHasToolsOrImages 请求是否携带工具或图片（auto 分流判据，
// 原项目：tools/tool_choice/工具历史/图片请求 → IDE Agent）。
func requestHasToolsOrImages(params map[string]any) bool {
	if params == nil {
		return false
	}
	if tools, ok := params["tools"].([]any); ok && len(tools) > 0 {
		return true
	}
	if tc, ok := params["tool_choice"]; ok && tc != nil {
		return true
	}
	msgs, _ := params["messages"].([]any)
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role == "tool" {
			return true
		}
		if _, ok := m["tool_calls"].([]any); ok {
			return true
		}
		if contentHasImage(m["content"]) {
			return true
		}
	}
	return false
}

// contentHasImage content 中是否带图片块（image_url / image 形态）。
func contentHasImage(v any) bool {
	blocks, ok := v.([]any)
	if !ok {
		return false
	}
	for _, b := range blocks {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		bt, _ := bm["type"].(string)
		if bt == "image_url" || bt == "image" {
			return true
		}
	}
	return false
}

// rawChatOnce 单账号单次请求（raw_chat 原链路）：遍历候选，200 即返回解析流。
// traeWireDump wire 排障开关（TRAE_WIRE_DEBUG=1）：落盘每次 raw_chat 的请求
// 头/体与响应首字节到 DATA_DIR/trae-wire/，现场对照「同一 body 探针 4001、
// 网关 200」类问题（2026-10-30 排查引入）。默认关闭零开销。
var traeWireDump = strings.EqualFold(strings.TrimSpace(os.Getenv("TRAE_WIRE_DEBUG")), "1")

func rawChatOnce(ctx context.Context, acc *Account, opts ChatOpts, proxyURL string) (*Stream, error) {
	body := BuildChatBody(opts.Params, opts.Model, acc.ID)
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	var lastErr error
	for _, cand := range chatCandidates() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cand[0]+cand[1], bytes.NewReader(bodyJSON))
		if err != nil {
			return nil, err
		}
		for k, v := range chatHeaders(acc, opts.Model, randHex(8)) {
			req.Header.Set(k, v)
		}
		if traeWireDump {
			traeDumpWire(req, bodyJSON)
		}
		hsStart := time.Now()
		resp, err := httpClientFor(proxyURL).Do(req)
		observeProxy(proxyURL, err == nil, err, time.Since(hsStart).Milliseconds())
		if err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("client aborted: %w", ctx.Err())
			}
			lastErr = err
			continue
		}
		if resp.StatusCode == http.StatusOK {
			s := newStream(resp)
			s.Transport = "raw"
			return s, nil
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		he := parseUpstreamError(resp.StatusCode, b)
		he.Transport = "raw"
		// 限流（同凭据处处限流）与参数错误：不做候选回退，立即上抛
		if he.RateLimited || he.ParamError {
			return nil, he
		}
		lastErr = he
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("trae chat: no candidate attempted")
	}
	return nil, lastErr
}

// newStream 从 200 响应构造解析游标（SSE 行缓冲上限 8MB，防畸形超长行 OOM），
// 并装配读取超时看门狗。
func newStream(resp *http.Response) *Stream {
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	s := &Stream{resp: resp, sc: sc}
	s.armFirstEventWatch()
	return s
}

// ============ 流读取超时（对齐上游 120s 首事件 / 300s 空闲） ============

const (
	defaultFirstEventTimeout = 120 * time.Second
	defaultStreamIdleTimeout = 300 * time.Second
)

// streamTimeoutEnv 秒数 env（<=0 = 对应超时关闭）。
func streamTimeoutEnv(name string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n <= 0 {
				return 0
			}
			return time.Duration(n) * time.Second
		}
	}
	return def
}

// killStream 看门狗到点：关底层响应体迫使 Scan 返回读错误（幂等）。
func (s *Stream) killStream(reason string) {
	log.Printf("trae stream watchdog: %s, closing stream", reason)
	s.resp.Body.Close()
}

// armFirstEventWatch 首个模型事件前超时（上游 TRAE_REMOTE_FIRST_EVENT_TIMEOUT
// 同语义：心跳不算模型事件，排队/心跳-only 的僵尸流到点被杀）。
func (s *Stream) armFirstEventWatch() {
	d := streamTimeoutEnv("TRAE_REMOTE_FIRST_EVENT_TIMEOUT", defaultFirstEventTimeout)
	if d <= 0 || s.resp == nil || s.resp.Body == nil {
		return
	}
	s.firstEvTimer = time.AfterFunc(d, func() { s.killStream(fmt.Sprintf("no model event within %s", d)) })
}

// touchIdleWatch 流中空闲超时：每收到一行数据重置（上游 TRAE_WEB_STREAM_TIMEOUT
// 同语义；心跳行也续期——只防真死连接，不杀慢生成）。
func (s *Stream) touchIdleWatch() {
	if s.firstEvTimer != nil && !s.firstEvSeen {
		return // 首事件计时优先，避免双计时器竞争
	}
	d := streamTimeoutEnv("TRAE_STREAM_IDLE_TIMEOUT", defaultStreamIdleTimeout)
	if d <= 0 || s.resp == nil || s.resp.Body == nil {
		return
	}
	if s.idleTimer == nil {
		s.idleTimer = time.AfterFunc(d, func() { s.killStream(fmt.Sprintf("idle for %s", d)) })
		return
	}
	s.idleTimer.Reset(d)
}

// markModelEvent 产出模型事件时调用：解除首事件计时，切换到空闲计时。
func (s *Stream) markModelEvent() {
	if s.firstEvSeen {
		return
	}
	s.firstEvSeen = true
	if s.firstEvTimer != nil {
		s.firstEvTimer.Stop()
	}
}

// stopStreamWatches Close 时停掉全部计时器。
func (s *Stream) stopStreamWatches() {
	if s.firstEvTimer != nil {
		s.firstEvTimer.Stop()
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
}

// ============ 并发信号量（与 openrouter 同构） ============

var (
	semMu   sync.Mutex
	sem     chan struct{}
	semOnce sync.Once
)

func rebuildSem() {
	n := Get().MaxConcurrency
	if n <= 0 {
		n = 8
	}
	semMu.Lock()
	sem = make(chan struct{}, n)
	semMu.Unlock()
}

// ensureSem 惰性初始化（Load 未被调用时兜底）。
func ensureSem() {
	semOnce.Do(rebuildSem)
}

func acquireSem(ctx context.Context) bool {
	ensureSem()
	semMu.Lock()
	s := sem
	semMu.Unlock()
	if s == nil {
		return true
	}
	select {
	case s <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func releaseSem() {
	semMu.Lock()
	s := sem
	semMu.Unlock()
	if s != nil {
		<-s
	}
}

// SetMaxConcurrency 并发上限变化后重建信号量（保存配置时调用）。
func SetMaxConcurrency() { rebuildSem() }

// ============ 按账号会话闸门（对齐上游 solo_agent_parallel_limit） ============
//
// 上游 trae_client.py/auth.py：从权益快照读每账号 solo_agent_parallel_limit，
// 调度时按 (limit - inflight) 选号，避免把上游并发顶满触发 991502
// （solo_agent_parallel_limit 429）和事件流被掐。本地闸门只覆盖会话型传输
//（remote/traework，raw_chat 不建会话不受此限），建会话前获取、流关闭
//（stop 之后）释放。权益未知时用保守默认 defaultAccountParallelLimit。

const defaultAccountParallelLimit = 2

// accountParallelLimitOverride TRAE_ACCOUNT_PARALLEL_LIMIT 覆盖权益解析
// （排障用：<=0 不覆盖）。
var accountParallelLimitOverride = sync.OnceValue(func() int {
	n, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("TRAE_ACCOUNT_PARALLEL_LIMIT")))
	return n
})

// AccountParallelLimit 账号会话并发上限：权益快照优先，env 覆盖次之，
// 未知回保守默认。额度快照住痕迹表（usage.go）。
func AccountParallelLimit(acc *Account) int {
	if n := accountParallelLimitOverride(); n > 0 {
		return n
	}
	if c := UsageFor(acc.ID).Credits; c != nil && c.ParallelLimit > 0 {
		return c.ParallelLimit
	}
	return defaultAccountParallelLimit
}

// accountGate 计数闸门（mutex+通知通道，支持运行中改上限）。
type accountGate struct {
	mu     sync.Mutex
	limit  int
	used   int
	notify chan struct{} // release 时关闭并置 nil，唤醒所有等待者
}

func (g *accountGate) setLimit(n int) {
	if n <= 0 {
		return
	}
	g.mu.Lock()
	g.limit = n
	g.mu.Unlock()
}

func (g *accountGate) acquire(ctx context.Context) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.used >= g.limit {
		ch := g.notify
		if ch == nil {
			ch = make(chan struct{})
			g.notify = ch
		}
		g.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return false
		}
		g.mu.Lock()
	}
	g.used++
	return true
}

func (g *accountGate) release() {
	g.mu.Lock()
	if g.used > 0 {
		g.used--
	}
	if ch := g.notify; ch != nil {
		close(ch)
		g.notify = nil
	}
	g.mu.Unlock()
}

var accountGates sync.Map // accountID -> *accountGate

// acquireAccountSlot 会话型传输建会话前调用；ctx 取消返回 false。
func acquireAccountSlot(ctx context.Context, acc *Account) bool {
	v, _ := accountGates.LoadOrStore(acc.ID, &accountGate{limit: AccountParallelLimit(acc)})
	g := v.(*accountGate)
	g.setLimit(AccountParallelLimit(acc)) // 权益刷新后跟随
	return g.acquire(ctx)
}

// releaseAccountSlot 与 acquireAccountSlot 配对（流 Close 时调用）。
func releaseAccountSlot(accID string) {
	if v, ok := accountGates.Load(accID); ok {
		v.(*accountGate).release()
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// jitter 0~25% 抖动。
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d + time.Duration(float64(d)*float64(randIntn(26))/100)
}

func nextDelay(d time.Duration) time.Duration {
	if d >= 8*time.Second {
		return 8 * time.Second
	}
	return d * 2
}

// ============ token 刷新 / 探测 / 额度 ============

// tokenExpiredAt 解析账号的过期时间（RFC3339；空/非法 = 零值 = 长期有效）。
func tokenExpiredAt(a *Account) time.Time {
	if a.ExpiredAt == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, a.ExpiredAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// NeedsRefresh token 距过期 <30min（且已知过期时间、有 refresh token）。
func NeedsRefresh(a *Account) bool {
	if a == nil || a.RefreshToken == "" {
		return false
	}
	exp := tokenExpiredAt(a)
	if exp.IsZero() {
		return false
	}
	return time.Until(exp) < refreshBeforeSec*time.Second
}

// TokenUsable token 距过期 >5min（可用判据；不足则先刷新）。
func TokenUsable(a *Account) bool {
	exp := tokenExpiredAt(a)
	if exp.IsZero() {
		return true
	}
	return time.Until(exp) > validBufferSec*time.Second
}

// refreshFlight 单飞：同一账号并发的刷新合并为一次上游调用。
var (
	refreshFlightMu sync.Mutex
	refreshFlight   = map[string]*refreshCall{}
)

type refreshCall struct {
	done chan struct{}
	err  error
}

// EnsureFresh 账号 token 需要刷新时单飞刷新（成功后落盘并解除冷却）。
func EnsureFresh(a *Account) error {
	if a == nil || !NeedsRefresh(a) {
		return nil
	}
	refreshFlightMu.Lock()
	if fc, ok := refreshFlight[a.ID]; ok {
		refreshFlightMu.Unlock()
		<-fc.done
		return fc.err
	}
	fc := &refreshCall{done: make(chan struct{})}
	refreshFlight[a.ID] = fc
	refreshFlightMu.Unlock()
	go func() {
		fc.err = doRefresh(a)
		close(fc.done)
		refreshFlightMu.Lock()
		delete(refreshFlight, a.ID)
		refreshFlightMu.Unlock()
	}()
	<-fc.done
	return fc.err
}

// auxClient 辅助上游调用（token 刷新/用户信息/额度/签到）的出口化 client：
// 与会话同源解析账号出口（绑定主备 → 全局池/直连，DISABLE_DIRECT 约束在
// app 层 SetExitResolver），出口不可用时报错而非裸直连——会话走代理而辅助
// 调用走家庭 IP 的不一致是风控信号（2026-10-29 审计修复）。client 按出口
// 工厂取用后浅拷保留 30s 超时帽（会话 client 面向流式长跑不设超时，
// 辅助调用必须有界）。
func auxClient(a *Account) (*http.Client, error) {
	proxyURL, ok := resolveExitFor(a)
	if !ok {
		return nil, fmt.Errorf("account %s bound proxies unavailable (cooling or removed); skipped by proxy isolation", a.ID)
	}
	c := *httpClientFor(proxyURL)
	c.Timeout = 30 * time.Second
	return &c, nil
}

// doRefresh 调 ExchangeToken 换新 JWT 并落盘（ClientSecret 字面 "-"，
// 上游协议如此）。响应键：Result.Token/RefreshToken/TokenExpireAt/...，
// 错误在 ResponseMetadata.Error。
func doRefresh(a *Account) error {
	host := a.Host
	if host == "" {
		host = strings.TrimRight(Get().Gateway, "/")
	}
	body, _ := json.Marshal(map[string]any{
		"ClientID":     defaultClientID,
		"RefreshToken": a.RefreshToken,
		"ClientSecret": "-",
		"UserID":       a.ID,
	})
	req, err := http.NewRequest(http.MethodPost, host+"/cloudide/api/v3/trae/oauth/ExchangeToken", bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range oauthHeaders(a.Token, deviceID16(a.ID)) {
		req.Header.Set(k, v)
	}
	client, err := auxClient(a)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("trae refresh request: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("trae refresh status %d: %s", resp.StatusCode, kit.Truncate(strings.TrimSpace(string(b)), 200))
	}
	var top map[string]any
	if err := json.Unmarshal(b, &top); err != nil {
		return fmt.Errorf("trae refresh decode: %w", err)
	}
	if rm, ok := top["ResponseMetadata"].(map[string]any); ok {
		if ecode := firstString(rm, "Error", "error", "Code"); ecode != "" && ecode != "OK" && ecode != "Success" {
			return fmt.Errorf("trae refresh upstream error: %s", ecode)
		}
	}
	res, _ := top["Result"].(map[string]any)
	if res == nil {
		res, _ = top["result"].(map[string]any)
	}
	if res == nil {
		return fmt.Errorf("trae refresh: missing Result in response")
	}
	token := firstString(res, "Token", "token", "AccessToken", "accessToken")
	if token == "" {
		return fmt.Errorf("trae refresh: no token in Result")
	}
	rt := firstString(res, "RefreshToken", "refreshToken")
	if rt == "" {
		rt = a.RefreshToken // 缺省沿用旧 refresh token
	}
	mu.Lock()
	a.Token = token
	a.RefreshToken = rt
	if exp := firstString(res, "TokenExpireAt", "tokenExpireAt", "expiredAt"); exp != "" {
		a.ExpiredAt = normalizeExpire(exp)
	}
	mu.Unlock()
	Uncool(a.ID)
	save()
	return nil
}

// normalizeExpire 过期时间归一：>1e12 视为毫秒时间戳，统一 RFC3339 UTC。
func normalizeExpire(v string) string {
	v = strings.TrimSpace(v)
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n > 1_000_000_000_000 {
			return time.UnixMilli(n).UTC().Format(time.RFC3339)
		}
		if n > 1_000_000_000 {
			return time.Unix(n, 0).UTC().Format(time.RFC3339)
		}
	}
	if t, err := time.Parse(time.RFC3339, strings.ReplaceAll(v, " ", "T")); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return v
}

// GetUserInfo 用 x-cloudide-token 探测账号（面板 Test 用）：返回 (用户名, 错误)。
// 端点在 UG/OAuth 域（api.trae.cn，网关域上没有该路由）；只带
// x-cloudide-token 头——Authorization: Cloud-IDE-JWT 反而 401（实测）。
func GetUserInfo(a *Account) (string, error) {
	host := a.Host
	if host == "" {
		host = apiHostCN
	}
	body, _ := json.Marshal(map[string]any{"ReqSource": "IDE"})
	req, err := http.NewRequest(http.MethodPost, host+"/cloudide/api/v3/trae/GetUserInfo", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-cloudide-token", a.Token)
	client, err := auxClient(a)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, kit.Truncate(strings.TrimSpace(string(b)), 200))
	}
	var payload struct {
		Result struct {
			UserID     string `json:"UserID"`
			ScreenName string `json:"ScreenName"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return "", err
	}
	return payload.Result.ScreenName, nil
}

// FetchCredits 查询账号额度（ide_user_ent_usage，req_source=1 IDE 通用额度）。
// 解析首个带额度的 entitlement pack；credits_limit == -1 = unlimited。
func FetchCredits(a *Account) (*Credits, error) {
	body, _ := json.Marshal(map[string]any{"require_usage": true, "req_source": 1})
	req, err := http.NewRequest(http.MethodPost, apiHostCN+"/trae/api/v2/pay/ide_user_ent_usage", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range oauthHeaders(a.Token, deviceID16(a.ID)) {
		req.Header.Set(k, v)
	}
	client, err := auxClient(a)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, kit.Truncate(strings.TrimSpace(string(b)), 200))
	}
	c, err := parseCreditsBody(b)
	if err != nil {
		return nil, err
	}
	c.UpdatedAt = time.Now().UTC()
	mu.Lock()
	usageForLocked(a.ID).Credits = c
	mu.Unlock()
	saveUsage() // 额度快照即时落盘（痕迹文件）
	return c, nil
}

// parseCreditsBody 解析 ide_user_ent_usage 响应。
// 真实响应（2026-10-25 真账号实测）**顶层无 data 包裹**：
//
//	{"usage_summary":{"consumed_amount":0.03,"total_amount":4700},
//	 "user_entitlement_pack_list":[{entitlement_base_info.quota.credits_limit, usage...}]}
//
// 额度口径优先 usage_summary（剩余=total-consumed）；无 summary 时兜底取首个
// credits_limit != 0 的 pack（product_type=0 的功能开关 pack 无额度自然跳过）。
// 兼容带 data 包裹的形态（若上游改版）。
func parseCreditsBody(b []byte) (*Credits, error) {
	var top map[string]any
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, err
	}
	src := top
	if d, ok := top["data"].(map[string]any); ok && len(d) > 0 {
		src = d
	}
	c := &Credits{
		Identity: firstNonEmpty(
			firstString(src, "user_pay_identity_str", "userPayIdentityText"),
			firstString(top, "user_pay_identity_str", "userPayIdentityText"),
		),
	}
	// 额度口径：usage_summary 优先（剩余=total-consumed）；无 summary 时
	// 兜底取首个 credits_limit != 0 的 pack（product_type=0 的功能开关 pack
	// 无额度自然跳过）。并发权益口径独立：跨全部 pack 取
	// solo_agent_parallel_limit 最大值（上游 trae_client.py parallel_limits
	// 同口径）——两者同载于一个响应，不能因 summary 命中跳过 pack 扫描。
	creditsDone := false
	if us, ok := src["usage_summary"].(map[string]any); ok {
		total := numField(us, "total_amount")
		consumed := numField(us, "consumed_amount")
		if total > 0 {
			c.Limit = total
			c.Remaining = total - consumed
			creditsDone = true
		}
	}
	parallelMax := 0
	packs, _ := src["user_entitlement_pack_list"].([]any)
	for _, raw := range packs {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		base, _ := p["entitlement_base_info"].(map[string]any)
		if base == nil {
			continue
		}
		limit := numField(base, "credits_limit")
		quota := map[string]any(nil)
		if q, ok := base["quota"].(map[string]any); ok {
			quota = q
		}
		if limit == 0 && quota != nil {
			limit = numField(quota, "credits_limit")
		}
		if limit == 0 {
			if pe, ok := base["product_extra"].(map[string]any); ok {
				if se, ok := pe["subscription_extra"].(map[string]any); ok {
					if q, ok := se["quota"].(map[string]any); ok {
						limit = numField(q, "credits_limit")
						if quota == nil {
							quota = q
						}
					}
				}
			}
			if limit == 0 {
				if pe, ok := base["product_extra"].(map[string]any); ok {
					if pk, ok := pe["package_extra"].(map[string]any); ok {
						if q, ok := pk["quota"].(map[string]any); ok {
							limit = numField(q, "credits_limit")
							if quota == nil {
								quota = q
							}
						}
					}
				}
			}
		}
		// solo_agent_parallel_limit 与 credits_limit 同源（quota 下，含
		// product_extra 兜底链；quota 非空时以 quota 为准，对齐上游）。
		if n := numField(quota, "solo_agent_parallel_limit"); n > float64(parallelMax) {
			parallelMax = int(n)
		}
		if !creditsDone && limit != 0 {
			usage, _ := p["usage"].(map[string]any)
			if limit < 0 {
				c.Unlimited = true
				c.Limit = -1
				c.Remaining = -1
			} else {
				c.Limit = limit
				c.Remaining = limit - numField(usage, "credits_amount")
			}
			creditsDone = true
		}
	}
	c.ParallelLimit = parallelMax
	return c, nil
}

// base64RawURLDecode JWT payload 段解码（标准 base64url，补齐 padding）。
func base64RawURLDecode(s string) ([]byte, error) {
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.URLEncoding.DecodeString(s)
}

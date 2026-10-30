//go:build traeprobe

package trae

// 手工探针(默认不编译):go test -tags traeprobe ./internal/trae/ -run TestProbeRaw -v
// 对真实上游做变量隔离实验,定位带工具请求 4001 的最小触发条件。
// 需要:TRAE_PROBE_TOKEN(凭据1 JWT)、TRAE_PROBE_ACCOUNT(账号ID)、
// TRAE_PROBE_PROXY(socks5 出口,可空=直连)。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"
)

func probeClient(t *testing.T) *http.Client {
	t.Helper()
	c := &http.Client{Timeout: 90 * time.Second}
	if p := strings.TrimSpace(os.Getenv("TRAE_PROBE_PROXY")); p != "" {
		var auth *proxy.Auth
		d, err := proxy.SOCKS5("tcp", strings.TrimPrefix(p, "socks5://"), auth, proxy.Direct)
		if err != nil {
			t.Fatalf("socks5 dialer: %v", err)
		}
		c.Transport = &http.Transport{DialContext: d.(proxy.ContextDialer).DialContext}
	}
	return c
}

// probeClientBunFp 与网关 proxyClientFor 同构的 transport:uTLS Bun 指纹 + h2。
// 用于区分「TLS 指纹差异」与「请求差异」——标准 Go/curl TLS 客户端被上游判
// 4001(2026-10-30 排查实测),app 包 transport 有 import 环,这里就地复刻。
func probeClientBunFp(t *testing.T) *http.Client {
	t.Helper()
	var dial proxy.ContextDialer = proxy.Direct
	if p := strings.TrimSpace(os.Getenv("TRAE_PROBE_PROXY")); p != "" {
		d, err := proxy.SOCKS5("tcp", strings.TrimPrefix(p, "socks5://"), nil, proxy.Direct)
		if err != nil {
			t.Fatalf("socks5 dialer: %v", err)
		}
		dial = d.(proxy.ContextDialer)
	}
	tr := &http.Transport{
		TLSHandshakeTimeout: 15 * time.Second,
	}
	tr.DialContext = dial.DialContext
	tr.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		raw, err := dial.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			raw.Close()
			return nil, err
		}
		uconn := utls.UClient(raw, &utls.Config{
			ServerName: host,
			NextProtos: []string{"http/1.1"},
		}, utls.HelloCustom)
		if err := uconn.ApplyPreset(bunSpecCopy()); err != nil {
			raw.Close()
			return nil, err
		}
		if err := uconn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		return uconn, nil
	}
	tr.ForceAttemptHTTP2 = true
	return &http.Client{Transport: tr, Timeout: 120 * time.Second}
}

// bunSpecCopy 逐字段复刻 internal/app tls_bun.go bunSpecForConn 的 Bun v2
// ClientHelloSpec(测试文件不能 import app 包)。与 tls_bun.go 同步维护。
func bunSpecCopy() *utls.ClientHelloSpec {
	spec := utls.ClientHelloSpec{
		CipherSuites: []uint16{
			0x1301, 0x1302, 0x1303,
			0xC02B, 0xC02F, 0xC02C, 0xC030, 0xCCA9, 0xCCA8,
			0xC009, 0xC013, 0xC00A, 0xC014,
			0x009C, 0x009D, 0x002F, 0x0035,
		},
		CompressionMethods: []uint8{0x00},
		Extensions: []utls.TLSExtension{
			&utls.SNIExtension{},
			&utls.ExtendedMasterSecretExtension{},
			&utls.RenegotiationInfoExtension{Renegotiation: utls.RenegotiateOnceAsClient},
			&utls.SupportedCurvesExtension{Curves: []utls.CurveID{
				utls.X25519MLKEM768, utls.X25519, utls.CurveP256, utls.CurveP384,
			}},
			&utls.SupportedPointsExtension{SupportedPoints: []byte{0x00}},
			&utls.SessionTicketExtension{},
			&utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}},
			&utls.StatusRequestExtension{},
			&utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []utls.SignatureScheme{
				utls.ECDSAWithP256AndSHA256, utls.ECDSAWithP384AndSHA384, utls.ECDSAWithP521AndSHA512,
				utls.Ed25519, 2056, 2057, 2058, 2059,
				utls.PSSWithSHA256, utls.PSSWithSHA384, utls.PSSWithSHA512,
				utls.PKCS1WithSHA256, utls.PKCS1WithSHA384, utls.PKCS1WithSHA512,
				771, 769, 770, 1026, 1282, 1538,
			}},
			&utls.SCTExtension{},
			&utls.KeyShareExtension{KeyShares: []utls.KeyShare{
				{Group: utls.X25519MLKEM768},
				{Group: utls.X25519},
			}},
			&utls.PSKKeyExchangeModesExtension{Modes: []uint8{utls.PskModeDHE}},
			&utls.SupportedVersionsExtension{Versions: []uint16{utls.VersionTLS13, utls.VersionTLS12}},
		},
		TLSVersMin: utls.VersionTLS12,
		TLSVersMax: utls.VersionTLS13,
	}
	return &spec
}

func probeModel(t *testing.T) Model {
	return Model{ID: "glm-5.3-flash"}
}

func probeBody(t *testing.T, messages []map[string]any, extra map[string]any) map[string]any {
	t.Helper()
	m := probeModel(t)
	body := map[string]any{
		"config_name":       m.ID,
		"model_name":        rawNameOf(m),
		"conversation_id":   sessionID(os.Getenv("TRAE_PROBE_ACCOUNT"), m),
		"session_id":        sessionID(os.Getenv("TRAE_PROBE_ACCOUNT"), m),
		"stream":            true,
		"raw_chat_function": functionOf(m),
		"config_source":     1,
		"messages":          messages,
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func blk(role, text string) map[string]any {
	return map[string]any{"role": role, "content": []map[string]any{{"type": "text", "text": text}}}
}

func probeSend(t *testing.T, body map[string]any, tag string) (int, string) {
	t.Helper()
	bj, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "https://trae-api-cn.mchost.guru/api/ide/v2/llm_raw_chat", bytes.NewReader(bj))
	acc := &Account{ID: os.Getenv("TRAE_PROBE_ACCOUNT"), Token: os.Getenv("TRAE_PROBE_TOKEN")}
	for k, v := range chatHeaders(acc, probeModel(t), randHex(8)) {
		req.Header.Set(k, v)
	}
	resp, err := probeClientBunFp(t).Do(req)
	if err != nil {
		t.Fatalf("%s: send: %v", tag, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	out := string(raw)
	// 提取首个 data 行摘要(错误负载在此;纯 OK 时是文本增量)
	first := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			first = kit2Trunc(line, 220)
			break
		}
	}
	if first == "" {
		first = kit2Trunc(out, 220)
	}
	return resp.StatusCode, first
}

func kit2Trunc(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

const probeRuntime = "You are Trae Code connected to the user's local application.\n" +
	"The external Trae-compatible terminal client application that started this conversation provides the client tools below.\n" +
	`Caller client context (JSON): {"system_type":"Windows","terminal_context":[{"shell":"PowerShell","cwd":".","source":"request-inferred"}],"tool_discovery":{"mode":"automatic","plugin_namespaces":[],"tool_search_available":false}}` + "\n" +
	"Available client tool definitions and input schemas (JSON):\n" +
	`[{"name":"get_weather","description":"查询城市天气","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}},{"name":"read_file","description":"读取文件","input_schema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}]` + "\n" +
	`To call a tool, emit one block per call in this format and no prose or final answer in the same turn: <opencode_tool_call>{"name":"tool_name","input":{}}</opencode_tool_call>.`

// TestProbeRaw 单一实验矩阵:每条独立会话,输出 http 状态 + 首事件。
func TestProbeRaw(t *testing.T) {
	if os.Getenv("TRAE_PROBE_TOKEN") == "" {
		t.Skip("TRAE_PROBE_TOKEN not set")
	}
	toolMsg := map[string]any{
		"role": "user", "content": []map[string]any{{"type": "text", "text": "Client tool result [c1] get_weather status=succeeded:\n{\"temp\":\"25C\"}"}},
	}
	assistantCallMsg := map[string]any{
		"role": "assistant", "content": []map[string]any{{"type": "text", "text": "Client tool calls already issued in this conversation (history only; use the matching results below and do not repeat them):\nClient tool call [c1] get_weather; arguments: {\"city\":\"北京\"}"}},
	}
	exps := []struct {
		name  string
		msgs  []map[string]any
		extra map[string]any
	}{
		{"P1_pure_text", []map[string]any{blk("user", "Reply with exactly: OK")}, nil},
		{"P2_sys_plain", []map[string]any{blk("system", "You are helpful."), blk("user", "Reply with exactly: OK")}, nil},
		{"P3_sys_runtime_full", []map[string]any{blk("system", probeRuntime), blk("user", "Reply with exactly: OK")}, nil},
		{"P4_sys_runtime_min", []map[string]any{blk("system", "Available client tools (JSON): [{\"name\":\"get_weather\"}]"), blk("user", "Reply with exactly: OK")}, nil},
		{"P5_tool_result_history", []map[string]any{blk("user", "list weather"), toolMsg, blk("user", "Reply with exactly: OK")}, nil},
		{"P6_assistant_call_history", []map[string]any{blk("user", "list weather"), assistantCallMsg, toolMsg, blk("user", "Reply with exactly: OK")}, nil},
		{"P7_reasoning_effort", []map[string]any{blk("user", "Reply with exactly: OK")}, map[string]any{"reasoning_effort": "medium"}},
		{"P8_img_none_textonly_maxtok", []map[string]any{blk("user", "Reply with exactly: OK")}, map[string]any{"max_tokens": 16}},
		{"P9_message_str_field", []map[string]any{blk("user", "Reply with exactly: OK")}, map[string]any{"message": "hello"}},
		{"PA_request_type", []map[string]any{blk("user", "Reply with exactly: OK")}, map[string]any{"request_type": "chat"}},
	}
	for _, e := range exps {
		code, first := probeSend(t, probeBody(t, e.msgs, e.extra), e.name)
		verdict := "OK "
		if code != 200 || strings.Contains(first, `"code":4001`) || strings.Contains(first, `"code":4023`) {
			verdict = "ERR"
		}
		fmt.Printf("[%s] %-30s http=%d first=%s\n", verdict, e.name, code, first)
		time.Sleep(1200 * time.Millisecond)
	}
}

// TestProbeRawBodyVariants 对 P3(runtime system)逐字段增删定位 4001 触发项。
func TestProbeRawBodyVariants(t *testing.T) {
	if os.Getenv("TRAE_PROBE_TOKEN") == "" {
		t.Skip("TRAE_PROBE_TOKEN not set")
	}
	msgs := []map[string]any{blk("system", probeRuntime), blk("user", "Reply with exactly: OK")}
	variants := []struct {
		name  string
		drop  []string
		extra map[string]any
	}{
		{"V1_base", nil, nil},
		{"V2_drop_raw_chat_function", []string{"raw_chat_function"}, nil},
		{"V3_drop_config_source", []string{"config_source"}, nil},
		{"V4_drop_conversation_id", []string{"conversation_id"}, nil},
		{"V5_drop_model_name", []string{"model_name"}, nil},
		{"V6_stream_false", nil, map[string]any{"stream": false}},
		{"V7_drop_session_id", []string{"session_id"}, nil},
	}
	for _, v := range variants {
		body := probeBody(t, msgs, nil)
		for _, k := range v.drop {
			delete(body, k)
		}
		for k, val := range v.extra {
			body[k] = val
		}
		code, first := probeSend(t, body, v.name)
		verdict := "OK "
		if code != 200 || strings.Contains(first, `"code":4001`) {
			verdict = "ERR"
		}
		fmt.Printf("[%s] %-30s http=%d first=%s\n", verdict, v.name, code, first)
		time.Sleep(1200 * time.Millisecond)
	}
	_ = context.Background
}

func jsonUnmarshalForProbe(b []byte, v any) error { return json.Unmarshal(b, v) }

// TestProbeExactBody 用与网关 S1 完全一致的 body(探针发送),区分「body 差异」
// 与「传输指纹差异」。
func TestProbeExactBody(t *testing.T) {
	if os.Getenv("TRAE_PROBE_TOKEN") == "" {
		t.Skip("TRAE_PROBE_TOKEN not set")
	}
	braw := []byte(`{"config_name":"glm-5.3-flash","model_name":"glm-5.3-flash","conversation_id":"01234567-89ab-cdef-0123-456789abcdef","session_id":"01234567-89ab-cdef-0123-456789abcdef","stream":true,"raw_chat_function":"chat_v3","config_source":1,"messages":[{"role":"user","content":[{"type":"text","text":"Reply with exactly: OK"}]}]}`)
	var body map[string]any
	if err := jsonUnmarshalForProbe(braw, &body); err != nil {
		t.Fatal(err)
	}
	code, first := probeSend(t, body, "ExactS1")
	t.Logf("exact body: http=%d first=%s", code, first)
}

// TestProbeRemoteWithTools 强制 remote(chat_sessions) 传输发带工具请求:
// 验证 4001 是 raw 端点/档位级问题,remote 档是否同样拒绝。
func TestProbeRemoteWithTools(t *testing.T) {
	if os.Getenv("TRAE_PROBE_TOKEN") == "" {
		t.Skip("TRAE_PROBE_TOKEN not set")
	}
	acc := &Account{ID: os.Getenv("TRAE_PROBE_ACCOUNT"), Token: os.Getenv("TRAE_PROBE_TOKEN")}
	m := probeModel(t)
	// remote 档走 live 目录 TierCfg;探针手动构造最小条目
	m.Tiers = []string{"remote", "traework", "traecode"}
	m.TierCfg = map[string]map[string]any{
		"remote":   {"name": m.ID, "config_name": m.ID, "model_name": m.ID, "config_source": float64(1), "is_preset": true},
		"traework": {"name": m.ID, "config_name": m.ID, "model_name": m.ID, "config_source": float64(1), "is_preset": true},
		"traecode": {"name": m.ID, "config_name": m.ID, "model_name": m.ID, "config_source": float64(1), "is_preset": true},
	}
	tools := []any{
		map[string]any{"type": "function", "function": map[string]any{"name": "get_weather", "description": "查询城市天气", "parameters": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []any{"city"}}}},
		map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "description": "读取文件", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []any{"path"}}}},
	}
	params := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "北京今天天气怎么样?请用 get_weather 工具查询。"}},
		"tools":    tools,
	}
	// EnsureFresh 等钩子未注入(探针直连),手动给凭据
	stream, err := remoteChatOnce(context.Background(), acc, ChatOpts{Model: m, Params: params}, os.Getenv("TRAE_PROBE_PROXY"))
	if err != nil {
		t.Logf("remote+tools: err=%v", err)
		return
	}
	defer stream.Close()
	text := ""
	for {
		ev, rerr := stream.Next()
		if rerr != nil || ev == nil {
			break
		}
		switch ev.Type {
		case "text":
			text += ev.Text
		case "error":
			t.Logf("remote+tools: error event code=%s msg=%s", ev.Code, ev.Message)
		}
	}
	t.Logf("remote+tools: text=%d chars, transport=%s", len(text), stream.Transport)
}

// TestProbeRemoteAgentScale remote 档 24 工具 agent 场景(生产 tools=54 EOF 的
// 近似重现;24 是生产成功档上限)。
func TestProbeRemoteAgentScale(t *testing.T) {
	if os.Getenv("TRAE_PROBE_TOKEN") == "" {
		t.Skip("TRAE_PROBE_TOKEN not set")
	}
	acc := &Account{ID: os.Getenv("TRAE_PROBE_ACCOUNT"), Token: os.Getenv("TRAE_PROBE_TOKEN")}
	m := probeModel(t)
	m.Tiers = []string{"remote", "traework", "traecode"}
	m.TierCfg = map[string]map[string]any{
		"remote":   {"name": m.ID, "config_name": m.ID, "model_name": m.ID, "config_source": float64(1), "is_preset": true},
		"traework": {"name": m.ID, "config_name": m.ID, "model_name": m.ID, "config_source": float64(1), "is_preset": true},
		"traecode": {"name": m.ID, "config_name": m.ID, "model_name": m.ID, "config_source": float64(1), "is_preset": true},
	}
	tools := make([]any, 0, 24)
	for i := 0; i < 24; i++ {
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
			"name":        fmt.Sprintf("agent_tool_%02d", i),
			"description": fmt.Sprintf("Agent 工具 %d:模拟编辑器/终端/文件系统操作,参数为 input 与 target。", i),
			"parameters": map[string]any{"type": "object", "properties": map[string]any{
				"input": map[string]any{"type": "string"}, "target": map[string]any{"type": "string"}},
			}}})
	}
	params := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "You are a coding agent. Use the client tools to complete tasks."},
			map[string]any{"role": "user", "content": "请列出当前目录的文件(用工具),然后报告。"},
		},
		"tools": tools,
	}
	stream, err := remoteChatOnce(context.Background(), acc, ChatOpts{Model: m, Params: params}, os.Getenv("TRAE_PROBE_PROXY"))
	if err != nil {
		t.Logf("remote-agent24: err=%v", err)
		return
	}
	defer stream.Close()
	text := ""
	evCount := 0
	for {
		ev, rerr := stream.Next()
		if rerr != nil || ev == nil {
			break
		}
		evCount++
		if ev.Type == "text" {
			text += ev.Text
		}
		if ev.Type == "error" {
			t.Logf("remote-agent24: error code=%s msg=%s", ev.Code, ev.Message)
		}
	}
	t.Logf("remote-agent24: events=%d text=%d chars tools_snapshot=%d", evCount, len(text), len(stream.ToolCallsSnapshot()))
}

package kit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

var ExecCommand = exec.Command

var HTTPTransport = &http.Transport{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 10,
	IdleConnTimeout:     90 * time.Second,
	DisableCompression:  false,
}

var HTTPClient = &http.Client{
	Transport: HTTPTransport,
}

// MaskProxyURL 打码代理 URL 的凭据段（user:pass → ***），供日志与错误文案使用。
// 解析失败或 URL 无 userinfo 时原样返回（代理地址本身不含机密）。
func MaskProxyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("***")
	// url.User 会把 *** 百分号编码成 %2A%2A%2A，日志/面板里几乎无法辨认；
	// 还原成字面量星号（只替换这三个字符，其他转义不受影响）。
	return strings.ReplaceAll(u.String(), "%2A%2A%2A", "***")
}

// ProxyBlockedDetail 由主包注入：给定平台 scope（子平台 slug；非子平台传空串）
// 与 key 明文，返回"该凭据绑定出口为何不可用"的可读说明（含打码后的出口与
// 冷却解除时刻）。未注入时返回空串，调用方应把文案降级为不带详情的通用句。
//
// 走注入而不是让子包直接读主包状态，是因为凭据→绑定→代理冷却状态这条链全在
// 主包（app）里，子包（amd/openrouter/tokenharbor/other）不应反向依赖它。
var ProxyBlockedDetail func(scope, key string) string

// BoundProbeBlockedMsg 拼装"绑定出口不可用"的探测失败文案。主包注入了
// ProxyBlockedDetail 且能定位到绑定时，追加打码后的出口与冷却解除时刻；
// 否则原样返回 prefix（行为与此前一致，不会更差）。
//
// 句尾那句"凭据本身未验证"是重点：出口挂掉不等于凭据失效，面板 Test 报红
// 时用户必须能一眼看出该换出口而不是删 key。
func BoundProbeBlockedMsg(scope, key, prefix string) string {
	if ProxyBlockedDetail == nil {
		return prefix
	}
	detail := ProxyBlockedDetail(scope, key)
	if detail == "" {
		return prefix
	}
	return prefix + ": " + detail + " — the credential itself was not verified (its egress is down)"
}

// JSONHTTPClient 短 JSON 请求（认证/设备登录/管理探测）专用：整体 60s 超时，
// 防止上游吞连接后调用方永久挂起（启动预热的模型拉取就会被卡住）。
// 不要用于流式转发 —— 那些请求生命周期由 ctx 控制，可能远超 60s。
var JSONHTTPClient = &http.Client{
	Transport: HTTPTransport,
	Timeout:   60 * time.Second,
}

func HTTPPostForm(rawURL string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequest("POST", rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return JSONHTTPClient.Do(req)
}

func HTTPPostJSON(rawURL string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", rawURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return JSONHTTPClient.Do(req)
}

// ReadBody 读取错误响应体用于日志/报错展示。上限 1MB：这些路径只做
// 截断展示，一个恶意/异常上游返回 GB 级 body 时不能拖垮进程内存。
func ReadBody(resp *http.Response) string {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Sprintf("<read error: %v>", err)
	}
	return string(data)
}

func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func RunCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Start()
}

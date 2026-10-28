package kit

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
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
	return HTTPPostJSONWithClient(JSONHTTPClient, rawURL, body)
}

// HTTPPostJSONWithClient 同 HTTPPostJSON，但允许调用方指定 client——
// 出口化场景（token 刷新等走绑定/池代理，与凭据的会话出口保持 IP 一致）。
func HTTPPostJSONWithClient(client *http.Client, rawURL string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", rawURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

// DecodeResponse 按 Content-Encoding 给 resp.Body 套上解码器，并删除
// Content-Encoding / Content-Length 头（长度已不代表解码后的体）。
//
// 为什么必需：任何显式设置 Accept-Encoding 的请求都会让 net/http 的
// 自动解压失效（它只在客户端未设置该头时才解压）。zen 上游配了
// "gzip, deflate, br, zstd" 来对齐 CLI 指纹，因此必须自己解压，否则
// 压缩响应会被当成明文交给 JSON/SSE 解析器（表现为"乱码"或
// "non-SSE body"）。小响应通常未压缩，大响应才会压 —— 也就是说
// 越正常的请求越容易踩到。
//
// 未知/不支持的编码保持原样（fail-open）：调用方读到的仍是上游字节，
// 与未加本函数时的行为一致，不会比现在更差。
func DecodeResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	if enc == "" || enc == "identity" {
		return nil
	}
	// 多编码按应用顺序叠放，解码需逆序。
	parts := strings.Split(enc, ",")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	body := resp.Body
	for _, p := range parts {
		p = strings.TrimSpace(p)
		var err error
		switch p {
		case "", "identity":
			continue
		case "gzip", "x-gzip":
			var zr *gzip.Reader
			if zr, err = gzip.NewReader(body); err == nil {
				body = zr
			}
		case "deflate":
			// 历史上 deflate 既可能是 zlib 也可能是裸 DEFLATE，先试 zlib。
			if zr, zerr := zlib.NewReader(body); zerr == nil {
				body = zr
			} else {
				body = flate.NewReader(body)
			}
		case "br":
			body = &readCloser{Reader: brotli.NewReader(body), closer: body}
		case "zstd":
			var zr *zstd.Decoder
			if zr, err = zstd.NewReader(body); err == nil {
				body = zr.IOReadCloser()
			}
		default:
			// 未知编码：不解码，保持原样。
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode %s response: %w", p, err)
		}
	}
	resp.Body = &decodedBody{inner: body}
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	return nil
}

// decodedBody 同时关闭解码器与底层连接。
type decodedBody struct {
	inner io.ReadCloser
}

func (d *decodedBody) Read(p []byte) (int, error) { return d.inner.Read(p) }
func (d *decodedBody) Close() error               { return d.inner.Close() }

// readCloser 给只有 Read 的解码器（brotli）补上 Close（关底层连接）。
type readCloser struct {
	io.Reader
	closer io.Closer
}

func (r *readCloser) Close() error { return r.closer.Close() }

// ReadBody 读取响应体用于日志/报错展示。上限 1MB：这些路径只做
// 截断展示，一个恶意/异常上游返回 GB 级 body 时不能拖垮进程内存。
// 会先按 Content-Encoding 解码，避免压缩体被当成文本解析。
func ReadBody(resp *http.Response) string {
	if err := DecodeResponse(resp); err != nil {
		return fmt.Sprintf("<decode error: %v>", err)
	}
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

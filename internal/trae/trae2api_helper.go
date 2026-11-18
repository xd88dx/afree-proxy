package trae

// TraeWork 加号通道：本机 8765 helper 网页授权 + 手动粘贴凭据。
//
// 上游切换为 vendored trae2api 后，凭据体系变了：池账号 = data/accounts/
// 便携快照（product/userId/token/refreshToken/设备 EC P-256 密钥对，Validate
// 强校验），续期协议要求设备私钥签 DeviceProof。helper 的 web SSO 流程拿到
// 的是 token+refreshToken（无设备材料）——本文件在入库前**现场铸造设备
// 密钥对与设备身份**补齐快照。可行依据：上游 OAuth 登录每次也现场生成全新
// 密钥对（traectl oauth.go generateDeviceKeyPair），服务端只验签不过绑，
// 铸造密钥 + refreshToken 走 ExchangeToken 与全新设备登录等价（阶段 2b
// 可行性门结论；续期失败时会以 RefreshError 显式暴露，届时再回退纯导入）。
//
// 仅 token 无 refreshToken 的粘贴仍拒绝：没有刷新材料，token 过期即掉线，
// 且过不了上游 Validate（fail-fast，别给用户一个"能加但活不长"的假成功）。

import (
	"trae2api"

	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ============ helper 入库状态 ============

type TraeHelperIngest struct {
	At      time.Time `json:"at"`
	Success bool      `json:"success"`
	Label   string    `json:"label"`
	Added   bool      `json:"added"`
	Message string    `json:"message"`
}

var (
	TraeLastIngestMu sync.Mutex
	TraeLastIngest   *TraeHelperIngest
)

// WebAuthSourceAllowed /api/web-auth 的来源判定：回环、内网（RFC1918/ULA）、
// 链路本地放行；公网来源拒绝。RemoteAddr 无法解析时按拒绝处理（fail-closed）。
// CLIENT_IP_HEADER 反代头不采信——凭据写入端点信任的只能是 TCP 对端。
func WebAuthSourceAllowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func Trae2apiIngestCreds(creds map[string]any) (uid string, label string, added bool, err error) {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := creds[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	token := get("token", "Token")
	if token == "" {
		return "", "", false, fmt.Errorf("credentials carry no token")
	}
	if !Trae2apiLooksLikeCloudIDEJWT(token) {
		return "", "", false, fmt.Errorf("token is not a Cloud-IDE-JWT")
	}
	refresh := get("refreshToken", "RefreshToken")
	if refresh == "" {
		return "", "", false, fmt.Errorf(
			"refreshToken is required: the trae2api engine refreshes tokens with a minted device key + refresh token; token-only credentials cannot pass import validation")
	}
	userID := get("userId", "UserID")
	if userID == "" {
		userID = Trae2apiTokenAccountID(token)
	}
	deviceID := get("deviceId", "DeviceID")
	if deviceID == "" {
		deviceID = Trae2apiMintNumericDeviceID()
	}
	machineID := get("machineId", "MachineID")
	if machineID == "" {
		machineID = Trae2apiMintMachineID()
	}
	privPEM, pubPEM, err := Trae2apiMintDeviceKeyPair()
	if err != nil {
		return "", "", false, fmt.Errorf("mint device key pair: %w", err)
	}
	snap := &trae2api.CredentialSnapshot{
		Version:       1,
		Product:       "trae-cn",
		Label:         get("screenName", "ScreenName", "label", "Label"),
		UserID:        userID,
		Token:         token,
		RefreshToken:  refresh,
		ExpiredAt:     Trae2apiNormalizeExpire(get("expiredAt", "ExpiredAt")),
		Host:          get("host", "Host"),
		DeviceID:      deviceID,
		MachineID:     machineID,
		PrivateKeyPEM: privPEM,
		PublicKeyPEM:  pubPEM,
	}
	snapJSON, err := json.Marshal(map[string]any{"accounts": []any{snap}})
	if err != nil {
		return "", "", false, err
	}
	// 重复导入的判定：vendored 导入是覆盖语义（同名快照覆盖、uid 不变），响应里
	// 没有"已存在"标记。这里在导入前查一次池——否则面板对重复授权恒报"新增"，
	// 用户无法区分这次是新建还是覆盖（2026-11-09 修复）。
	existed := false
	if uid0 := strings.TrimSpace(userID); uid0 != "" {
		if sub := Trae2apiCurrent(); sub != nil && sub.Srv != nil {
			existed = sub.Srv.Pool().Get(uid0) != nil
		}
	}
	code, data, err := Trae2apiAdminCall(http.MethodPost, "/admin/accounts/import",
		[]byte(fmt.Sprintf(`{"text":%q,"product":"trae-cn"}`, string(snapJSON))))
	if err != nil {
		return "", "", false, err
	}
	var resp struct {
		Results []struct {
			OK    bool   `json:"ok"`
			UID   string `json:"uid"`
			Label string `json:"label"`
			Error string `json:"error"`
			File  string `json:"file"`
		} `json:"results"`
		Succeed int `json:"succeed"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", false, fmt.Errorf("import response: %w (HTTP %d, body %.200s)", err, code, string(data))
	}
	if len(resp.Results) == 0 || !resp.Results[0].OK {
		msg := "import failed"
		if len(resp.Results) > 0 && resp.Results[0].Error != "" {
			msg = resp.Results[0].Error
		}
		return "", "", false, fmt.Errorf("%s (HTTP %d)", msg, code)
	}
	r := resp.Results[0]
	label = r.Label
	if label == "" {
		label = r.UID
	}
	return r.UID, label, !existed, nil
}

func Trae2apiMintDeviceKeyPair() (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return string(privPEM), string(pubPEM), nil
}

// Trae2apiMintNumericDeviceID 16 位数字设备 ID（与上游 randomNumericDeviceID
// 同形态；DeviceID = iCubeAuthInfo://icube-dc:<此值> 的后缀约定）。
func Trae2apiMintNumericDeviceID() string {
	n, err := rand.Int(rand.Reader, big.NewInt(9_000_000_000_000_000))
	if err != nil {
		return "1000000000000000"
	}
	return fmt.Sprintf("%d", n.Int64()+1_000_000_000_000_000)
}

// Trae2apiMintMachineID 64 位 hex 机器标识（telemetry.machineId 形态）。
func Trae2apiMintMachineID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", 64)
	}
	return hex.EncodeToString(b)
}

// Trae2apiNormalizeExpire 归一时间字段：epoch 毫秒/秒 → RFC3339；其余原样
// （vendored Normalize/Validate 对空值容忍，ExpiredAt 不在必填清单）。
func Trae2apiNormalizeExpire(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if ms, err := ParseDigits(v); err == nil {
		if ms > 1_000_000_000_000 { // epoch ms
			return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z07:00")
		}
		if ms > 1_000_000_000 { // epoch s
			return time.Unix(ms, 0).UTC().Format("2006-01-02T15:04:05.000Z07:00")
		}
	}
	return v
}

func ParseDigits(v string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(strings.TrimSpace(v), &n)
	return n, err
}

// ============ JWT 形状判别（旧引擎迁入，格式约定不变） ============

// Trae2apiLooksLikeCloudIDEJWT JWT 形状判别（三段式且足够长）。非 JWT 的
// opaque 值不能用于模型网关，添加时直接拒绝。
func Trae2apiLooksLikeCloudIDEJWT(token string) bool {
	parts := strings.Split(strings.TrimSpace(token), ".")
	return len(strings.TrimSpace(token)) >= 128 && len(parts) == 3 &&
		parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// Trae2apiTokenAccountID 从 JWT payload 提取账号 ID（data.id / user_id /
// userId / sub 逐级回退；取不到用前缀截断兜底）。
func Trae2apiTokenAccountID(token string) string {
	t := strings.TrimSpace(token)
	parts := strings.Split(t, ".")
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" {
		if payload, err := Trae2apiJSONDecodeSegment(parts[1]); err == nil {
			for _, path := range []string{"data.id", "user_id", "userId", "sub"} {
				if v := Trae2apiDigString(payload, strings.Split(path, ".")); v != "" {
					return v
				}
			}
		}
	}
	if len(t) > 24 {
		return t[:24]
	}
	return t
}

// Trae2apiDigString 按路径取嵌套 map 里的字符串。
func Trae2apiDigString(m map[string]any, path []string) string {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	s, _ := cur.(string)
	return strings.TrimSpace(s)
}

// Trae2apiJSONDecodeSegment base64url 解码 JWT payload 段（RawURLEncoding，
// 自动容忍缺省 padding 由 RawURLEncoding 语义覆盖）。
func Trae2apiJSONDecodeSegment(seg string) (map[string]any, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(seg))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

package app

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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	traectl "afree-proxy/internal/trae2api/traectl"
)

// ============ helper 入库状态 ============

type traeHelperIngest struct {
	At      time.Time `json:"at"`
	Success bool      `json:"success"`
	Label   string    `json:"label"`
	Added   bool      `json:"added"`
	Message string    `json:"message"`
}

var (
	traeLastIngestMu sync.Mutex
	traeLastIngest   *traeHelperIngest
)

// webAuthSourceAllowed /api/web-auth 的来源判定：回环、内网（RFC1918/ULA）、
// 链路本地放行；公网来源拒绝。RemoteAddr 无法解析时按拒绝处理（fail-closed）。
// CLIENT_IP_HEADER 反代头不采信——凭据写入端点信任的只能是 TCP 对端。
func webAuthSourceAllowed(r *http.Request) bool {
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

// POST /api/web-auth  body: helper 解析后的凭据 dict（token/refreshToken/userId/...）
// 响应契约与原版一致：{"success":true} / {"success":false,"error":"..."}。
//
// 源限制（2026-10-29 审计修复）：本端点无认证（helper 是裸脚本，加认证会破坏
// 与上游 web_login.py 的行级基线等价），但凭据写入绝不能对公网开放。设计场景
// 是 helper 与用户浏览器同网段：本机部署=127.0.0.1，NAS/LAN 部署=内网 PC 的
// RFC1918/ULA 地址——因此只放行回环+内网源，公网/反代外来源一律 403 fail-closed。
func handleTraeWebAuth(w http.ResponseWriter, r *http.Request) {
	if !webAuthSourceAllowed(r) {
		log.Printf("trae web-auth rejected: non-local source %s", r.RemoteAddr)
		writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "error": "web-auth endpoint only accepts local/LAN sources"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	var creds map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&creds); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "invalid JSON: " + err.Error()})
		return
	}
	uid, label, added, err := trae2apiIngestCreds(creds)
	rec := &traeHelperIngest{At: time.Now().UTC(), Success: err == nil, Label: label, Added: added}
	if err != nil {
		rec.Message = err.Error()
	} else if added {
		// 英文源文 + 面板 T() 词条（I18N）双语；这两条是固定串，可整句词条。
		rec.Message = "Credentials saved to server"
	} else {
		rec.Message = "Account already exists"
	}
	traeLastIngestMu.Lock()
	traeLastIngest = rec
	traeLastIngestMu.Unlock()
	if err != nil {
		log.Printf("trae web-auth ingest failed: %v", err)
	} else {
		log.Printf("trae web-auth ingest: account %q (%s), added=%v, routing disabled by default", label, uid, added)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": err == nil, "error": rec.Message, "label": label})
}

// GET /trae/oauth/helper?as=py —— 下载按本部署预填 relay 的授权助手脚本。
// relay 取请求自身 Host（浏览器能打开面板就能运行助手连通网关）。
func handleTraeHelperDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "method not allowed"})
		return
	}
	base := "http://" + r.Host
	// 反向代理/HTTPS 部署：X-Forwarded-Proto 优先
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		base = "https://" + r.Host
	}
	script := HelperScript(base)
	w.Header().Set("Content-Type", "text/x-python; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=web_login.py")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

// GET /admin/api/trae/helper/status —— 最近一次 helper 入库结果。
func handleTraeHelperStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	traeLastIngestMu.Lock()
	rec := traeLastIngest
	traeLastIngestMu.Unlock()
	var data map[string]any
	if rec == nil {
		data = map[string]any{"seen": false}
	} else {
		data = map[string]any{
			"seen":    true,
			"at":      rec.At.Format(time.RFC3339),
			"success": rec.Success,
			"message": rec.Message,
			"label":   rec.Label,
			"added":   rec.Added,
		}
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// ============ 凭据入库 ============

// trae2apiIngestCreds 把 helper/面板粘贴的凭据转成完整便携快照并导入：
// 进程内 POST /admin/accounts/import（vendored ParseSnapshots→Validate→
// accountstore 落盘→registerAccount 热加池）。重复导入由 vendored 覆盖语义
// 处理（同名快照覆盖，uid 不变则 added=false 由调用方判）。
// 返回 (uid, label, added, err)。
func trae2apiIngestCreds(creds map[string]any) (uid string, label string, added bool, err error) {
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
	if !trae2apiLooksLikeCloudIDEJWT(token) {
		return "", "", false, fmt.Errorf("token is not a Cloud-IDE-JWT")
	}
	refresh := get("refreshToken", "RefreshToken")
	if refresh == "" {
		return "", "", false, fmt.Errorf(
			"refreshToken is required: the trae2api engine refreshes tokens with a minted device key + refresh token; token-only credentials cannot pass import validation")
	}
	userID := get("userId", "UserID")
	if userID == "" {
		userID = trae2apiTokenAccountID(token)
	}
	deviceID := get("deviceId", "DeviceID")
	if deviceID == "" {
		deviceID = trae2apiMintNumericDeviceID()
	}
	machineID := get("machineId", "MachineID")
	if machineID == "" {
		machineID = trae2apiMintMachineID()
	}
	privPEM, pubPEM, err := trae2apiMintDeviceKeyPair()
	if err != nil {
		return "", "", false, fmt.Errorf("mint device key pair: %w", err)
	}
	snap := &traectl.CredentialSnapshot{
		Version:       1,
		Product:       "trae-cn",
		Label:         get("screenName", "ScreenName", "label", "Label"),
		UserID:        userID,
		Token:         token,
		RefreshToken:  refresh,
		ExpiredAt:     trae2apiNormalizeExpire(get("expiredAt", "ExpiredAt")),
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
		if sub := trae2apiCurrent(); sub != nil && sub.srv != nil {
			existed = sub.srv.Pool().Get(uid0) != nil
		}
	}
	code, data, err := trae2apiAdminCall(http.MethodPost, "/admin/accounts/import",
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

// handleTraeAccountAdd POST /admin/api/trae/accounts/add —— 手动粘贴凭据。
// 支持行格式 `token----refreshToken`（与快照行格式一致）与 JSON
// {"token":...,"refreshToken":...}；token-only 拒绝（见文件头说明）。
func handleTraeAccountAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	var req struct {
		Tokens []string `json:"tokens"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	added, failed := 0, 0
	lastErr := ""
	for _, line := range req.Tokens {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		creds := map[string]any{}
		// 行格式 token----refreshToken（refreshToken 里不会出现 "----"）。
		if !strings.HasPrefix(line, "{") && strings.Contains(line, "----") {
			parts := strings.SplitN(line, "----", 2)
			creds["token"] = strings.TrimSpace(parts[0])
			creds["refreshToken"] = strings.TrimSpace(parts[1])
		} else if strings.HasPrefix(line, "{") {
			if err := json.Unmarshal([]byte(line), &creds); err != nil {
				failed++
				lastErr = err.Error()
				continue
			}
		} else {
			creds["token"] = line
		}
		if _, _, _, err := trae2apiIngestCreds(creds); err != nil {
			failed++
			lastErr = err.Error()
			continue
		}
		added++
	}
	msg := fmt.Sprintf("added %d account(s)", added)
	if failed > 0 {
		msg += fmt.Sprintf(", failed %d: %s", failed, lastErr)
	}
	log.Printf("trae accounts add: %s", msg)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: msg,
		Data: map[string]any{"added": added, "failed": failed, "error": lastErr}})
}

// ============ 设备身份铸造 ============

// trae2apiMintDeviceKeyPair 现场铸造设备 EC P-256 密钥对（PKCS#8 私钥 PEM +
// SPKI 公钥 PEM，与 CredentialSnapshot 的格式约定一致）。
func trae2apiMintDeviceKeyPair() (string, string, error) {
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

// trae2apiMintNumericDeviceID 16 位数字设备 ID（与上游 randomNumericDeviceID
// 同形态；DeviceID = iCubeAuthInfo://icube-dc:<此值> 的后缀约定）。
func trae2apiMintNumericDeviceID() string {
	n, err := rand.Int(rand.Reader, big.NewInt(9_000_000_000_000_000))
	if err != nil {
		return "1000000000000000"
	}
	return fmt.Sprintf("%d", n.Int64()+1_000_000_000_000_000)
}

// trae2apiMintMachineID 64 位 hex 机器标识（telemetry.machineId 形态）。
func trae2apiMintMachineID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", 64)
	}
	return hex.EncodeToString(b)
}

// trae2apiNormalizeExpire 归一时间字段：epoch 毫秒/秒 → RFC3339；其余原样
// （vendored Normalize/Validate 对空值容忍，ExpiredAt 不在必填清单）。
func trae2apiNormalizeExpire(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if ms, err := parseDigits(v); err == nil {
		if ms > 1_000_000_000_000 { // epoch ms
			return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z07:00")
		}
		if ms > 1_000_000_000 { // epoch s
			return time.Unix(ms, 0).UTC().Format("2006-01-02T15:04:05.000Z07:00")
		}
	}
	return v
}

func parseDigits(v string) (int64, error) {
	var n int64
	_, err := fmt.Sscan(strings.TrimSpace(v), &n)
	return n, err
}

// ============ JWT 形状判别（旧引擎迁入，格式约定不变） ============

// trae2apiLooksLikeCloudIDEJWT JWT 形状判别（三段式且足够长）。非 JWT 的
// opaque 值不能用于模型网关，添加时直接拒绝。
func trae2apiLooksLikeCloudIDEJWT(token string) bool {
	parts := strings.Split(strings.TrimSpace(token), ".")
	return len(strings.TrimSpace(token)) >= 128 && len(parts) == 3 &&
		parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// trae2apiTokenAccountID 从 JWT payload 提取账号 ID（data.id / user_id /
// userId / sub 逐级回退；取不到用前缀截断兜底）。
func trae2apiTokenAccountID(token string) string {
	t := strings.TrimSpace(token)
	parts := strings.Split(t, ".")
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" {
		if payload, err := trae2apiJSONDecodeSegment(parts[1]); err == nil {
			for _, path := range []string{"data.id", "user_id", "userId", "sub"} {
				if v := trae2apiDigString(payload, strings.Split(path, ".")); v != "" {
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

// trae2apiDigString 按路径取嵌套 map 里的字符串。
func trae2apiDigString(m map[string]any, path []string) string {
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

// trae2apiJSONDecodeSegment base64url 解码 JWT payload 段（RawURLEncoding，
// 自动容忍缺省 padding 由 RawURLEncoding 语义覆盖）。
func trae2apiJSONDecodeSegment(seg string) (map[string]any, error) {
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

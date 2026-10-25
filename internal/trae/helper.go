package trae

import (
	"fmt"
	"strings"
)

// ============ 本机授权助手（对齐上游原版 web_login.py 架构） ============
//
// trae.cn 授权页强依赖"本机授权助手"：页面流程要探测回调源上的本地监听器
// （原版固定 127.0.0.1:8765），探测不到直接报"登录失败/网络错误"——所以
// 凭据流必须经过本机 helper，不能让授权页直连网关回调。
//
// 完整链路（与原版逐环一致）：
//   1. 面板提供助手下载（helper_script.go 内嵌的 web_login.py，relay 地址
//      按当前部署预填），用户在本机运行，helper 监听 127.0.0.1:8765；
//   2. helper 页面打开 trae.cn/authorization（auth_callback_url 指向
//      helper 的 /authorize），授权页完成登录后 302 回 helper；
//   3. helper 解析/兑换凭据后 POST {relay}/api/web-auth 交给网关入库
//      （网关实现同名端点，见 app 层 handleTraeWebAuth）；
//   4. 面板轮询最近入库状态展示结果。账号默认不参与路由。

// HelperPort 本机授权助手监听端口（原版固定值，授权页按此探测）。
const HelperPort = 8765

// HelperOrigin 本机助手页面地址。
const HelperOrigin = "http://127.0.0.1:8765"

// AddFromHelperCreds 把 helper /authorize 解析出的凭据落库为新账号：
// 账号总闸默认开（可签到/探测），路由默认关（统一口径，面板勾选"反代"
// 后才参与）。重复主键返回已有账号。返回 (账号, 是否新增, 错误)。
func AddFromHelperCreds(creds map[string]any) (*Account, bool, error) {
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
		return nil, false, fmt.Errorf("credentials carry no token")
	}
	if !LooksLikeCloudIDEJWT(token) {
		return nil, false, fmt.Errorf("token is not a Cloud-IDE-JWT")
	}
	id := get("userId", "UserID")
	if id == "" {
		id = TokenAccountID(token)
	}
	if HasAccount(id) {
		return FindAccount(id), false, nil
	}
	acc := &Account{
		ID:           id,
		Token:        token,
		RefreshToken: get("refreshToken", "RefreshToken"),
		ExpiredAt:    normalizeExpire(get("expiredAt", "ExpiredAt")),
		Host:         get("host", "Host"),
	}
	if sn := get("screenName", "ScreenName"); sn != "" {
		acc.Label = sn
	}
	// 写时复制（与 app 层 orCloneConfig 同模式）：绝不能持 mu.Lock 再调
	// Get()/Set()——它们各自内部加锁，同 goroutine 重入 RWMutex 必死锁
	//（首个版本实测踩过：有效 token 的入库请求全部挂死）。
	cur, next := CloneConfig()
	next.Accounts = append(next.Accounts, acc)
	next.ReconcilePoolEnabled(next, cur)
	Set(next)
	return acc, true, nil
}

// truncateStr 截断并修剪字符串（错误文案限幅用）。
func truncateStr(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

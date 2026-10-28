package cline

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"afree-proxy/internal/kit"
)

const (
	workosClientID        = "client_01K3A541FN8TA3EPPHTD2325AR"
	workosDeviceAuthURL   = "https://api.workos.com/user_management/authorize/device"
	workosAuthenticateURL = "https://api.workos.com/user_management/authenticate"
	ClineAPIBase          = "https://api.cline.bot/api/v1"
)

type deviceAuthResp struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
}

type authenticateResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

type clineAuthResp struct {
	Data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    any    `json:"expiresAt"`
		UserInfo     *struct {
			Email string `json:"email"`
		} `json:"userInfo"`
	} `json:"data"`
}

type clineRefreshResp struct {
	Data struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    any    `json:"expiresAt"`
	} `json:"data"`
}

func WorkosDeviceAuth() (*deviceAuthResp, error) {
	form := url.Values{"client_id": {workosClientID}}
	resp, err := kit.HTTPPostForm(workosDeviceAuthURL, form)
	if err != nil {
		return nil, fmt.Errorf("workos device auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body := kit.ReadBody(resp)
		return nil, fmt.Errorf("workos device auth failed: %d %s", resp.StatusCode, kit.Truncate(body, 200))
	}

	var d deviceAuthResp
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("workos device auth decode: %w", err)
	}
	return &d, nil
}

func PollWorkosToken(deviceCode string, interval, expiresIn int) (*authenticateResp, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	currentInterval := interval
	if currentInterval < 5 {
		currentInterval = 5
	}

	for time.Now().Before(deadline) {
		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {workosClientID},
		}
		resp, err := kit.HTTPPostForm(workosAuthenticateURL, form)
		if err != nil {
			return nil, fmt.Errorf("workos poll: %w", err)
		}

		var a authenticateResp
		if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("workos poll decode: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode == 200 {
			return &a, nil
		}

		switch a.Error {
		case "authorization_pending":
			time.Sleep(time.Duration(currentInterval) * time.Second)
		case "slow_down":
			currentInterval += 5
			time.Sleep(time.Duration(currentInterval) * time.Second)
		default:
			errDesc := a.ErrorDesc
			if errDesc == "" {
				errDesc = a.Error
			}
			return nil, fmt.Errorf("workos polling error: %s", errDesc)
		}
	}
	return nil, fmt.Errorf("device authorization expired (timeout)")
}

func RegisterWithCline(workosAccess, workosRefresh string) (*clineAuthResp, error) {
	body := map[string]string{
		"accessToken":  workosAccess,
		"refreshToken": workosRefresh,
	}
	resp, err := kit.HTTPPostJSON(ClineAPIBase+"/auth/register", body)
	if err != nil {
		return nil, fmt.Errorf("cline register: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b := kit.ReadBody(resp)
		return nil, fmt.Errorf("cline register failed: %d %s", resp.StatusCode, kit.Truncate(b, 200))
	}

	var c clineAuthResp
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, fmt.Errorf("cline register decode: %w", err)
	}
	return &c, nil
}

// StatusError 携带 HTTP 状态码的上游错误，供调用方区分
// "上游明确拒绝认证"（400/401/403，账号确实失效）与瞬时故障（网络/5xx）。
type StatusError struct {
	Status int
	Err    error
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("status %d: %v", e.Status, e.Err)
}

func (e *StatusError) Unwrap() error { return e.Err }

// IsAuthRejection 判断 err 是否为上游明确拒绝认证（400/401/403）。
func IsAuthRejection(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && (se.Status == 400 || se.Status == 401 || se.Status == 403)
}

func RefreshClineToken(refreshToken string) (*clineRefreshResp, error) {
	return RefreshClineTokenWithClient(kit.JSONHTTPClient, refreshToken)
}

// RefreshClineTokenWithClient 允许指定 client：app 层按账号出口解析传入
// 代理 client，让刷新请求与该账号的会话流量保持同一出口 IP（风控一致性，
// 2026-10-28）。默认 RefreshClineToken 走 JSONHTTPClient（直连）。
func RefreshClineTokenWithClient(client *http.Client, refreshToken string) (*clineRefreshResp, error) {
	body := map[string]string{
		"refreshToken": refreshToken,
		"grantType":    "refresh_token",
	}
	resp, err := kit.HTTPPostJSONWithClient(client, ClineAPIBase+"/auth/refresh", body)
	if err != nil {
		return nil, fmt.Errorf("cline refresh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, &StatusError{Status: resp.StatusCode, Err: fmt.Errorf("cline refresh failed")}
	}

	var c clineRefreshResp
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil, fmt.Errorf("cline refresh decode: %w", err)
	}
	return &c, nil
}

func ParseExpiry(exp any) int64 {
	switch v := exp.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			return t.UnixMilli()
		}
		t, err = time.Parse(time.RFC3339Nano, v)
		if err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func OpenBrowser(url string) error {
	var cmd string
	var args []string

	switch {
	case IsWindows():
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		// Try common browser openers
		for _, candidate := range []string{"xdg-open", "open", "gnome-open"} {
			if _, err := os.Stat("/usr/bin/" + candidate); err == nil {
				cmd = candidate
				break
			}
			if _, err := os.Stat("/usr/local/bin/" + candidate); err == nil {
				cmd = candidate
				break
			}
		}
	}

	if cmd == "" {
		return fmt.Errorf("no browser opener found")
	}

	return kit.RunCommand(cmd, args...)
}

func IsWindows() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

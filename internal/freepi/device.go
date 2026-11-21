package freepi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ============ GitHub 设备码授权（面板加号通道）============
//
// 上游 plugin index.mjs authorize 的 Go 移植：device → consent(v4，实测为
// /auth/token 前置，缺失返 consent_required) → 轮询 token → /me 取 handle
// 由 app 层完成并入池。会话服务端无状态：sessionId 由面板 JS 持有并回调
// /admin/api/freepi/device/poll，每次回调打一次上游 /auth/token。轮询语义
// 对齐 plugin：pending、403/429、5xx 与网络抖动继续等；其余 4xx 判死
// （failed），面板停止轮询。

// deviceFlowBase 设备码流程基址（=APIOrigin；var 供 httptest 白盒覆写）。
var deviceFlowBase = APIOrigin

// deviceTimezone consent 的 timezone 字段（plugin 取浏览器 IANA 名；服务端
// 侧无浏览器上下文，按部署时区近似——上游仅用于额度重置时刻的展示）。
func deviceTimezone() string {
	if tz := time.Local.String(); strings.Contains(tz, "/") {
		return tz
	}
	return "Asia/Shanghai"
}

// DeviceSession 设备码会话（start 响应；面板 JS 持有 sessionId 并驱动轮询）。
type DeviceSession struct {
	SessionID string `json:"sessionId"`
	UserCode  string `json:"userCode"`
	URL       string `json:"url"`
	Interval  int    `json:"interval"`
	ExpiresIn int    `json:"expiresIn"`
}

// DevicePollResult 轮询单步结果：pending=继续等；done=拿到 token（面板停止
// 轮询，app 层就地入池）；failed=上游明确拒绝，Message 给面板展示。
type DevicePollResult struct {
	Status  string `json:"status"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message,omitempty"`
}

// devicePost 单次 JSON POST（10s 预算；返回状态码与 body 原文）。
func devicePost(client *http.Client, path string, payload any) (int, []byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceFlowBase+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode, body, nil
}

// fireConsent 打同意条款（plugin 固定 start 后、轮询前先打一次；best-effort，
// 失败不阻断——轮询遇 consent_required 会补打一次再试）。
func fireConsent(client *http.Client, sessionID string) {
	_, _, _ = devicePost(client, "/auth/consent", map[string]any{
		"session_id":      sessionID,
		"consent_version": "4",
		"timezone":        deviceTimezone(),
		"locale":          "en",
	})
}

// DeviceStart 申请设备码会话并打 consent。url 取 verification_uri_complete
// 优先——GitHub 页面已预填设备码，用户点 Continue → Authorize 即可。
func DeviceStart(client *http.Client) (*DeviceSession, error) {
	status, body, err := devicePost(client, "/auth/github/device", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("device session request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("device session: HTTP %d: %s", status, truncate(string(body), 200))
	}
	var d struct {
		SessionID      string `json:"session_id"`
		UserCode       string `json:"user_code"`
		VerifyComplete string `json:"verification_uri_complete"`
		Verify         string `json:"verification_uri"`
		Interval       int    `json:"interval"`
		ExpiresIn      int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("device session decode: %w", err)
	}
	if d.SessionID == "" || d.UserCode == "" {
		return nil, fmt.Errorf("device session: no session came back: %s", truncate(string(body), 200))
	}
	url := d.VerifyComplete
	if url == "" {
		url = d.Verify
	}
	interval := d.Interval
	if interval < 3 {
		interval = 3
	}
	expires := d.ExpiresIn
	if expires <= 0 {
		expires = 900
	}
	fireConsent(client, d.SessionID)
	return &DeviceSession{SessionID: d.SessionID, UserCode: d.UserCode, URL: url, Interval: interval, ExpiresIn: expires}, nil
}

// DevicePoll 轮询一次 /auth/token。pending（含 transport 抖动与 403/429/5xx，
// err 带原文供日志）；failed（非 403/429 的 4xx，判死）；done（含 token）。
// consent 网络竞态下上游点名 consent_required：补打一次 consent 后重试一轮。
func DevicePoll(client *http.Client, sessionID string) (*DevicePollResult, error) {
	for attempt := 0; attempt < 2; attempt++ {
		status, body, err := devicePost(client, "/auth/token", map[string]any{
			"session_id": sessionID,
			"timezone":   deviceTimezone(),
			"locale":     "en",
		})
		if err != nil {
			return &DevicePollResult{Status: "pending"}, err
		}
		if status == http.StatusForbidden || status == http.StatusTooManyRequests || status >= 500 {
			return &DevicePollResult{Status: "pending"}, nil
		}
		if status >= 400 {
			text := truncate(string(body), 200)
			if attempt == 0 && strings.Contains(text, "consent_required") {
				fireConsent(client, sessionID)
				continue
			}
			return &DevicePollResult{Status: "failed", Message: text}, nil
		}
		var d struct {
			Status string `json:"status"`
			Token  string `json:"token"`
		}
		if json.Unmarshal(body, &d) != nil || (d.Token == "" && d.Status != "pending") {
			return &DevicePollResult{Status: "failed", Message: "unexpected answer: " + truncate(string(body), 200)}, nil
		}
		if d.Status == "pending" {
			return &DevicePollResult{Status: "pending"}, nil
		}
		return &DevicePollResult{Status: "done", Token: d.Token}, nil
	}
	return &DevicePollResult{Status: "failed", Message: "consent_required persisted after re-consent"}, nil
}

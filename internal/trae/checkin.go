package trae

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ============ 每日签到 ============
//
// 协议照搬上游原版（trae_client.py checkin_credits）：
//   POST https://api.trae.cn/trae/api/v2/ug/checkin_credits/status   body {}
//   POST https://api.trae.cn/trae/api/v2/ug/checkin_credits/claim    body {}
// 设备 ID = 账号维度 16 位数字（deviceID16，跨 token 刷新稳定）。业务码：
//   9074 = 设备维度假性限流（可退避重试，这里只上报不自动代际推进）；
//   9095 = 该设备当天已签到（视为已签成功）。

// CheckinStatus 查询签到状态：返回 (enable, checkedIn, credits, err)。
func CheckinStatus(a *Account) (bool, bool, float64, error) {
	enable, checkedIn, credits, _, err := checkinCall(a, "status")
	return enable, checkedIn, credits, err
}

// CheckinClaim 执行签到并落状态。返回 (checkedIn, credits, err)。
func CheckinClaim(a *Account) (bool, float64, error) {
	_, checkedIn, credits, code, err := checkinCall(a, "claim")
	// 9095 = 设备维度今天已经签过：视为成功
	if err != nil && code == "9095" {
		checkedIn, credits, err = true, 0, nil
	}
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	recordCheckin(a, checkedIn, credits, errText)
	return checkedIn, credits, err
}

// checkinCall 调 status/claim 端点并解析信封（data 嵌套与平铺双兜底）。
func checkinCall(a *Account, action string) (enable bool, checkedIn bool, credits float64, code string, err error) {
	req, err := http.NewRequest(http.MethodPost, apiHostCN+"/trae/api/v2/ug/checkin_credits/"+action, bytes.NewReader([]byte("{}")))
	if err != nil {
		return false, false, 0, "", err
	}
	for k, v := range oauthHeaders(a.Token, deviceID16(a.ID)) {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, false, 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return false, false, 0, "", fmt.Errorf("status %d: %s", resp.StatusCode, truncateStr(string(b), 200))
	}
	var top map[string]any
	if err := json.Unmarshal(b, &top); err != nil {
		return false, false, 0, "", fmt.Errorf("decode: %w", err)
	}
	code, _ = extractError(top)
	data, _ := top["data"].(map[string]any)
	if data == nil {
		data = top
	}
	// 业务码可能在 data._error_code（9074 假性限流的落点）
	if c := firstString(data, "_error_code"); c != "" {
		code = c
	}
	if code == "9074" {
		return false, false, 0, code, fmt.Errorf("device rate limited (code 9074)")
	}
	if code != "" && code != "0" && code != "200" && code != "9095" {
		_, msg := extractError(top)
		return false, false, 0, code, fmt.Errorf("code %s: %s", code, msg)
	}
	enable = boolField(data, "enable")
	checkedIn = boolField(data, "checked_in") || boolField(data, "checkedIn")
	credits = numField(data, "credits")
	return enable, checkedIn, credits, code, nil
}

// recordCheckin 落签到状态（跨日由调用方按 Date 字段判断是否需要重签）。
func recordCheckin(a *Account, checkedIn bool, credits float64, errText string) {
	mu.Lock()
	a.Checkin = &CheckinState{
		CheckedIn: checkedIn,
		Date:      time.Now().Format("2006-01-02"),
		LastAt:    time.Now().UTC(),
		Credits:   credits,
		LastError: errText,
	}
	mu.Unlock()
	save()
}

// NeedsCheckin 该账号今天是否还没签（以本地记录为准；无记录视为未签）。
func NeedsCheckin(a *Account) bool {
	if a.Checkin == nil {
		return true
	}
	return a.Checkin.Date != time.Now().Format("2006-01-02") || !a.Checkin.CheckedIn
}

func boolField(obj map[string]any, key string) bool {
	switch v := obj[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	}
	return false
}

package trae

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
//   9074 = 设备维度假性限流 → 换设备 ID（代际+1）+ 固定 10 分钟后台重试
//          （2026-10-27 用户定；原项目为指数退避，此处简化为固定间隔）；
//   9095 = 该设备当天已签到（可能是设备替别的账号签的，同样视为成功）。
//
// 9074 的原理（原项目实测结论）：签到限流跟着 x-device-id 走而不跟账号——
// 同一账号在设备 A 上 claim 报 9074，换一个新派生的设备 ID 立即成功。
// 设备 ID 派生材料为 sha256(账号ID 或 账号ID#gen{N})，代际持久化在账号上，
// 跨 token 刷新与重启稳定；每次 9074 代际 +1 等效于换一台设备。
// 注意：几秒内立刻重发 claim 会延长上游限流窗口（原项目实测），所以退避
// 期间绝不重发。

const (
	// checkin9074Backoff 9074 换设备后的固定退避间隔。
	checkin9074Backoff = 10 * time.Minute
	// checkin9074MaxRotations 单账号单日最多换设备次数（防止全天无限轮换
	// 设备指纹引来更严风控）；6 次 ≈ 覆盖 1 小时的重试窗口。
	checkin9074MaxRotations = 6
)

// CheckinStatus 查询签到状态：返回 (enable, checkedIn, credits, err)。
func CheckinStatus(a *Account) (bool, bool, float64, error) {
	enable, checkedIn, credits, _, err := checkinCall(a, "status")
	return enable, checkedIn, credits, err
}

// CheckinClaim 执行签到并落状态。返回 (checkedIn, credits, err)。
// 命中 9074 时换设备 ID 并安排 10 分钟后的后台重试（链式，直到成功 /
// 9095 / 当日轮换次数用尽），本调用立即返回不阻塞面板。
func CheckinClaim(a *Account) (bool, float64, error) {
	checkedIn, credits, code, err := claimOnce(a)
	if err == nil || code != "9074" {
		return checkedIn, credits, err
	}
	// 9074：设备维度假性限流 → 换设备 ID，10 分钟后台重试
	if a.Checkin != nil && a.Checkin.Rotations >= checkin9074MaxRotations {
		errText := fmt.Sprintf("device rate limited (code 9074) — %d device rotations exhausted today", a.Checkin.Rotations)
		recordCheckin(a, false, 0, errText)
		log.Printf("trae checkin 9074: account %s gave up (%s)", a.ID, errText)
		return false, 0, fmt.Errorf("%s", errText)
	}
	rotateCheckinDevice(a)
	recordCheckin(a, false, 0, "device rate limited (code 9074) — device rotated, retry in 10m")
	log.Printf("trae checkin 9074: account %s rotated to device gen %d, retry in %s", a.ID, a.CheckinDeviceGen, checkin9074Backoff)
	scheduleCheckinRetry(a.ID)
	return false, 0, fmt.Errorf("device rate limited (code 9074) — device rotated, retry in 10m")
}

// claimOnce 单次 claim（9095 视为成功），返回业务码供 9074 判定。
func claimOnce(a *Account) (checkedIn bool, credits float64, code string, err error) {
	_, checkedIn, credits, code, err = checkinCall(a, "claim")
	// 9095 = 设备维度今天已经签过：视为成功
	if err != nil && code == "9095" {
		checkedIn, credits, err = true, 0, nil
	}
	return checkedIn, credits, code, err
}

// rotateCheckinDevice 设备代际 +1（持久化）并记当日轮换次数。
func rotateCheckinDevice(a *Account) {
	mu.Lock()
	a.CheckinDeviceGen++
	today := time.Now().Format("2006-01-02")
	if a.Checkin == nil || a.Checkin.Date != today {
		a.Checkin = &CheckinState{Date: today}
	}
	a.Checkin.Rotations++
	mu.Unlock()
	save()
}

// scheduleCheckinRetry 10 分钟后台重试：确认账号仍在且今天未签成功才继续，
// 命中 9074 会继续链式换 ID 重试（受当日轮换上限约束）。
func scheduleCheckinRetry(accountID string) {
	time.AfterFunc(checkin9074Backoff, func() {
		mu.Lock()
		var target *Account
		for _, acc := range cfg.Accounts {
			if acc != nil && acc.ID == accountID {
				target = acc
				break
			}
		}
		if target == nil || target.Token == "" {
			mu.Unlock()
			return
		}
		if !NeedsCheckin(target) {
			mu.Unlock() // 今天已签成功，无需重试
			return
		}
		mu.Unlock()
		CheckinClaim(target)
	})
}

// checkinCall 调 status/claim 端点并解析信封（data 嵌套与平铺双兜底）。
// 设备 ID 按账号当前代际派生（9074 轮换后自动使用新 ID）。
func checkinCall(a *Account, action string) (enable bool, checkedIn bool, credits float64, code string, err error) {
	req, err := http.NewRequest(http.MethodPost, apiHostCN+"/trae/api/v2/ug/checkin_credits/"+action, bytes.NewReader([]byte("{}")))
	if err != nil {
		return false, false, 0, "", err
	}
	for k, v := range oauthHeaders(a.Token, deviceID16Gen(a.ID, a.CheckinDeviceGen)) {
		req.Header.Set(k, v)
	}
	client, err := auxClient(a)
	if err != nil {
		return false, false, 0, "", err
	}
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

// RecordCheckinResult 直接落一条签到状态（供 status 探测的结论复用：
// enable=false / 账号维度已签等不经过 claim 的路径，清掉历史失败徽章）。
func RecordCheckinResult(a *Account, checkedIn bool, credits float64, errText string) {
	recordCheckin(a, checkedIn, credits, errText)
}

// recordCheckin 落签到状态（跨日由调用方按 Date 字段判断是否需要重签；
// 当日轮换次数跨记录保留，跨日自动清零）。
func recordCheckin(a *Account, checkedIn bool, credits float64, errText string) {
	mu.Lock()
	rotations := 0
	if a.Checkin != nil && a.Checkin.Date == time.Now().Format("2006-01-02") {
		rotations = a.Checkin.Rotations
	}
	a.Checkin = &CheckinState{
		CheckedIn: checkedIn,
		Date:      time.Now().Format("2006-01-02"),
		LastAt:    time.Now().UTC(),
		Credits:   credits,
		LastError: errText,
		Rotations: rotations,
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

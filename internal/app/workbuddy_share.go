package app

// WorkBuddy 凭据权重优化（2026-11-15 用户立项）：按各账号快过期积分的「日均需耗」
// 反推流量占比，写入 pool.account_share，让流量分配跟着过期压力走，减少积分作废。
//
// 数据口径（与面板「积分到期提醒」同源同义）：
//   - 日均需耗 = Σ(该日到期积分 ÷ 距到期天数)，距到期按自然日、最少按 1 天
//     （今天到期的批次 ÷ 1）；多批次自动叠加，远期批次天然低权重。
//   - 占比纯由需耗驱动（2026-11-15 用户定：日均消耗不入权重逻辑）。
//
// 独占性（issue #62 语义）：account_share 表内的号独占抽签——不在表内的号接不到单，
// 表内号全部不可用时自动回落普通加权路由。因此无快过期批次的号给保底份额
// （合计 20%，内部按余额比例分），保证总吞吐不降。

import (
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"time"

	wbpool "afree-proxy/internal/workbuddy/pool"
)

// shareReservePct 无快过期批次账号的保底份额合计（百分比口径，与 suggested 同纲）。
const shareReservePct = 20.0

// wbShareSuggestion 一行优化建议。Reserved=true 表示该号无快过期批次、按保底分得。
type wbShareSuggestion struct {
	UID       string  `json:"uid"`
	Nickname  string  `json:"nickname,omitempty"`
	Realm     string  `json:"realm,omitempty"`
	Nearest   string  `json:"nearest,omitempty"`    // 最近到期日（yyyy-mm-dd，空=无批次）
	NeedTotal float64 `json:"need_total"`           // 快过期批次合计额度
	DailyNeed float64 `json:"daily_need"`           // 日均需耗 = Σ(remain ÷ 距到期天数)
	Suggested float64 `json:"suggested"`            // 建议占比（百分比，全体和≈100）
	Reserved  bool    `json:"reserved"`             // true = 保底号（无快过期批次）
	Credits   int64   `json:"credits,omitempty"`    // 保底内部分配依据（余额比例）
}

// shareDailyNeed 汇总一个账号的到期批次：日均需耗与最近到期日。
// days 按自然日差、最少 1（今天到期的批次÷1，与积分到期提醒面板同口径）；
// 过期未扣完的批次（负天数）同样按 1 算（最紧迫）。
func shareDailyNeed(buckets []wbpool.ExpiryBucket, now time.Time) (need, daily float64, nearest string) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, b := range buckets {
		if b.Remain <= 0 {
			continue
		}
		need += float64(b.Remain)
		days := 1
		if t, err := time.ParseInLocation("2006-01-02", b.Date, now.Location()); err == nil {
			if d := int(t.Sub(today).Hours() / 24); d > days {
				days = d
			}
			if nearest == "" || b.Date < nearest {
				nearest = b.Date
			}
		} else if !b.ExpireAt.IsZero() {
			if d := int(b.ExpireAt.Sub(now).Hours() / 24); d+1 > days {
				days = d + 1
			}
		}
		daily += float64(b.Remain) / float64(days)
	}
	return need, daily, nearest
}

// shareEligible 参与优化的账号口径：两把开关都开、未被硬禁用。冷却是瞬时状态
// （会自动恢复），占比是长期配置——冷却号参与优化，恢复后即按占比接单。
func shareEligible(s wbpool.Status) bool {
	return s.AccountOn() && s.PoolEnabled && !s.Disabled
}

// computeShareSuggestions 权重优化核心：按日均需耗分配占比。纯函数便于单测。
//
// 分配规则：有快过期批次的号占 100−shareReservePct，内部按日均需耗比例分；
// 无批次的号（保底）合计 shareReservePct，内部按余额比例分（余额全 0/未知则均分）。
// 全员无批次时返回空 suggestions（无优化需求，不应写配置）。
func computeShareSuggestions(list []wbpool.Status, now time.Time) ([]wbShareSuggestion, []string) {
	type slot struct {
		st      wbpool.Status
		need    float64
		daily   float64
		nearest string
	}
	var pressured, reserved []slot
	for _, s := range list {
		if !shareEligible(s) {
			continue
		}
		need, daily, nearest := shareDailyNeed(s.CreditsExpiryBuckets, now)
		sp := slot{st: s, need: need, daily: daily, nearest: nearest}
		if daily > 0 {
			pressured = append(pressured, sp)
		} else {
			reserved = append(reserved, sp)
		}
	}
	var warnings []string
	if len(pressured) == 0 {
		if len(reserved) == 0 {
			return nil, []string{"no eligible accounts"}
		}
		return nil, []string{"no expiring credits pressure — nothing to optimize"}
	}
	// 保底权重：合计 shareReservePct，内部按余额比例（余额全 0 → 均分）。
	reserveEach := func(n int) float64 { return shareReservePct / float64(n) }
	var reserveSum int64
	for _, sp := range reserved {
		if sp.st.Credits > 0 {
			reserveSum += sp.st.Credits
		}
	}
	// 有压力号合计 100−reserve：内部按日均需耗比例。
	pressuredPct := 100.0 - shareReservePct
	var needSum float64
	for _, sp := range pressured {
		needSum += sp.daily
	}
	out := make([]wbShareSuggestion, 0, len(pressured)+len(reserved))
	for _, sp := range pressured {
		pct := pressuredPct
		if needSum > 0 {
			pct = sp.daily / needSum * pressuredPct
		}
		out = append(out, wbShareSuggestion{
			UID: sp.st.UID, Nickname: sp.st.Nickname, Realm: sp.st.Realm,
			Nearest: sp.nearest, NeedTotal: sp.need, DailyNeed: sp.daily,
			Suggested: round2(pct),
		})
	}
	for _, sp := range reserved {
		pct := reserveEach(len(reserved))
		if reserveSum > 0 && sp.st.Credits > 0 {
			pct = shareReservePct * float64(sp.st.Credits) / float64(reserveSum)
		}
		out = append(out, wbShareSuggestion{
			UID: sp.st.UID, Nickname: sp.st.Nickname, Realm: sp.st.Realm,
			Suggested: round2(pct), Reserved: true, Credits: sp.st.Credits,
		})
	}
	// 展示顺序：建议占比降序（弹窗主读数），稳定按 UID 兜底。
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Suggested != out[j].Suggested {
			return out[i].Suggested > out[j].Suggested
		}
		return out[i].UID < out[j].UID
	})
	if anyPriorityActive(list) {
		warnings = append(warnings, "account_priority configured — shares only apply within the current priority tier")
	}
	return out, warnings
}

// anyPriorityActive 检测池内是否有账号配了 account_priority（share 只在当前优先
// 层内生效，issue #62 语义；分层存在时占比的「独占」范围被层切分，需提示用户）。
func anyPriorityActive(list []wbpool.Status) bool {
	for _, s := range list {
		if s.Priority > 0 {
			return true
		}
	}
	return false
}

// round2 两位小数（占比写入/展示统一精度）。
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// handleWorkbuddyShareOptimize GET：按当前池状态计算建议占比（只算不写）。
// 响应 {suggestions, warnings, priority_active, has_pressure}。
func handleWorkbuddyShareOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddySub == nil || workbuddySub.pool == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	// 一次 List 同时供计算与 priority 检测（List 是快照，两次调用间可能变化）。
	list := workbuddySub.pool.List()
	sugs, warnings := computeShareSuggestions(list, time.Now())
	var hasPressure bool
	for _, s := range sugs {
		if !s.Reserved {
			hasPressure = true
			break
		}
	}
	priorityActive := anyPriorityActive(list)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"suggestions":     sugs,
		"warnings":        warnings,
		"priority_active": priorityActive,
		"has_pressure":    hasPressure,
	}})
}

// handleWorkbuddyShareSet POST：写入/清空 pool.account_share。请求体
// {shares: {uid: 百分比权重}} 或 {clear: true}（清空回落普通加权路由）。
// 落盘走 saveWorkbuddyConfig 同一条链（深合并保留手工段 + 原子落盘 + 热生效）。
func handleWorkbuddyShareSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if workbuddySub == nil || workbuddySub.saveConfig == nil {
		writeAPI(w, http.StatusServiceUnavailable, apiResponse{Error: "workbuddy subsystem unavailable"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Shares map[string]float64 `json:"shares"`
		Clear  bool               `json:"clear"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	shares := req.Shares
	if req.Clear {
		shares = map[string]float64{}
	}
	if shares == nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "shares or clear is required"})
		return
	}
	// 校验：uid 必须在池内（防手滑打错号）；值必须为正（占比语义）。
	known := workbuddySub.pool
	for uid, v := range shares {
		if known.AuthByUID(uid) == nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "unknown uid: " + uid})
			return
		}
		if !(v > 0) {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "share value must be positive: " + uid})
			return
		}
	}
	raw, err := json.Marshal(map[string]any{"pool": map[string]any{"account_share": shares}})
	if err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	restart, err := workbuddySub.saveConfig(raw)
	if err != nil {
		writeAPI(w, http.StatusInternalServerError, apiResponse{Error: err.Error()})
		return
	}
	log.Printf("workbuddy: account_share 已更新（%d 个账号，clear=%v）", len(shares), req.Clear)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"done": true, "count": len(shares), "restart_required": restart,
	}})
}

// handleWorkbuddyShareGet 已移除（2026-11-15）：当前占比经 overview 的
// accounts[].share 透出（Status.Share），无独立查询端点的必要——避免死端点。

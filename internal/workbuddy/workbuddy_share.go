package workbuddy

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
	wbbridge "github.com/misakano7545/workbuddy2api-panel"

	"encoding/json"
	"log"
	"math"
	"sort"
	"sync"
	"time"
)

// ShareReservePct 无快过期批次账号的保底份额合计（百分比口径，与 suggested 同纲）。
const ShareReservePct = 20.0

// WbShareSuggestion 一行优化建议。Reserved=true 表示该号无快过期批次、按保底分得。
type WbShareSuggestion struct {
	UID       string  `json:"uid"`
	Nickname  string  `json:"nickname,omitempty"`
	Realm     string  `json:"realm,omitempty"`
	Nearest   string  `json:"nearest,omitempty"` // 最近到期日（yyyy-mm-dd，空=无批次）
	NeedTotal float64 `json:"need_total"`        // 7 天内到期批次合计（与面板「7 天内合计」同口径；日均需耗仍含全部批次摊薄）
	DailyNeed float64 `json:"daily_need"`        // 日均需耗 = Σ(remain ÷ 距到期天数)，全部未来批次
	Suggested float64 `json:"suggested"`         // 建议占比（百分比，全体和≈100）
	Reserved  bool    `json:"reserved"`          // true = 保底号（无快过期批次）
	Credits   int64   `json:"credits,omitempty"` // 保底内部分配依据（余额比例）
}

// ShareDailyNeed 汇总一个账号的到期批次，返回（7 天内合计、日均需耗、最近到期日）。
//
// 日均需耗 = **前缀和约束的最大值**（2026-11-15 修正，原 Σ(remain_i÷days_i) 把
// 各批次需求重复叠加、严重高估——消耗是串行的：上游 FEFO 先扣最早到期批次，
// 一个号只有一个消耗速率，只要速率 ≥ 每个到期节点的「累计应烧 ÷ 可用天数」，
// 全部批次都赶得上。例：今天到期 102 + 明天到期 606 → 正确 708/2=354/天，
// 错误叠加 102/1+606/1=708/天）。各批次约束取 max：
//
//	daily = max_i( (Σ_{j≤i} remain_j) ÷ days_i )
//
// days 按自然日差、最少 1（今天到期的批次÷1，与积分到期提醒面板同口径）；
// 过期未扣完的批次（负天数）同样按 1 算（最紧迫）。
// need7d 只累计 7 天内到期的批次（展示口径与面板「7 天内合计」对齐）。
func ShareDailyNeed(buckets []wbbridge.PoolExpiryBucket, now time.Time) (need7d, daily float64, nearest string) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	type dayBucket struct {
		days   int
		remain float64
	}
	var bks []dayBucket
	for _, b := range buckets {
		if b.Remain <= 0 {
			continue
		}
		// 可用天数 = 自然日差 + 1（含今天）：今天到期 → 1 天；明天到期 → 2 天
		//（今天+明天）——「距 1 天」与「今天到期」不是同一约束节点，不能同桶。
		// 过期未扣完（差为负）钳 1（最紧迫）。面板「日均需耗」列是单批次
		// 自然日差口径（249/2），与本前缀和口径（含今天）天数差 1——面板是
		// 最早一批的简化读数，不与本公式逐值对齐。
		days := 1
		if t, err := time.ParseInLocation("2006-01-02", b.Date, now.Location()); err == nil {
			if d := int(t.Sub(today).Hours() / 24); d > 0 {
				days = d + 1
			}
			if nearest == "" || b.Date < nearest {
				nearest = b.Date
			}
		} else if !b.ExpireAt.IsZero() {
			if d := int(b.ExpireAt.Sub(now).Hours() / 24); d > 0 {
				days = d + 1
			}
		}
		if days <= 7 {
			need7d += float64(b.Remain)
		}
		bks = append(bks, dayBucket{days: days, remain: float64(b.Remain)})
	}
	// 前缀和约束：先按天数聚合（同日多批次合桶），再按天数升序累积，
	// 每个到期节点算「累计应烧 ÷ 可用天数」，取最大。
	byDay := map[int]float64{}
	for _, bk := range bks {
		byDay[bk.days] += bk.remain
	}
	daysList := make([]int, 0, len(byDay))
	for d := range byDay {
		daysList = append(daysList, d)
	}
	sort.Ints(daysList)
	var cum float64
	for _, d := range daysList {
		cum += byDay[d]
		if r := cum / float64(d); r > daily {
			daily = r
		}
	}
	return need7d, daily, nearest
}

// ShareEligible 参与优化的账号口径：两把开关都开、未被硬禁用。冷却是瞬时状态
// （会自动恢复），占比是长期配置——冷却号参与优化，恢复后即按占比接单。
func ShareEligible(s wbbridge.PoolStatus) bool {
	return s.AccountOn() && s.PoolEnabled && !s.Disabled
}

// ComputeShareSuggestions 权重优化核心：按日均需耗分配占比。纯函数便于单测。
//
// 分配规则：有快过期批次的号占 100−ShareReservePct，内部按日均需耗比例分；
// 无批次的号（保底）合计 ShareReservePct，内部按余额比例分（余额全 0/未知则均分）。
// 全员无批次时返回空 suggestions（无优化需求，不应写配置）。
func ComputeShareSuggestions(list []wbbridge.PoolStatus, now time.Time) ([]WbShareSuggestion, []string) {
	type slot struct {
		st      wbbridge.PoolStatus
		need    float64
		daily   float64
		nearest string
	}
	var pressured, reserved []slot
	for _, s := range list {
		if !ShareEligible(s) {
			continue
		}
		need7d, daily, nearest := ShareDailyNeed(s.CreditsExpiryBuckets, now)
		sp := slot{st: s, need: need7d, daily: daily, nearest: nearest}
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
	// 保底权重：合计 ShareReservePct，内部按余额比例（余额全 0 → 均分）。
	// 2026-11-15 修：保底份额只在真有保底号时扣留——全员有批次（无保底号）
	// 时 pressuredPct 必须回满 100%，否则 20% 无人分、占比总和恒 80
	//（生产实测：12 号全有批次，占比总和 80 而非 100）。
	reservePct := 0.0
	if len(reserved) > 0 {
		reservePct = ShareReservePct
	}
	reserveEach := func(n int) float64 { return ShareReservePct / float64(n) }
	var reserveSum int64
	for _, sp := range reserved {
		if sp.st.Credits > 0 {
			reserveSum += sp.st.Credits
		}
	}
	// 有压力号合计 100−reserve：内部按日均需耗比例。
	pressuredPct := 100.0 - reservePct
	var needSum float64
	for _, sp := range pressured {
		needSum += sp.daily
	}
	out := make([]WbShareSuggestion, 0, len(pressured)+len(reserved))
	for _, sp := range pressured {
		pct := pressuredPct
		if needSum > 0 {
			pct = sp.daily / needSum * pressuredPct
		}
		out = append(out, WbShareSuggestion{
			UID: sp.st.UID, Nickname: sp.st.Nickname, Realm: sp.st.Realm,
			Nearest: sp.nearest, NeedTotal: sp.need, DailyNeed: sp.daily,
			Suggested: Round2(pct),
		})
	}
	for _, sp := range reserved {
		pct := reserveEach(len(reserved))
		if reserveSum > 0 && sp.st.Credits > 0 {
			pct = ShareReservePct * float64(sp.st.Credits) / float64(reserveSum)
		}
		out = append(out, WbShareSuggestion{
			UID: sp.st.UID, Nickname: sp.st.Nickname, Realm: sp.st.Realm,
			Suggested: Round2(pct), Reserved: true, Credits: sp.st.Credits,
		})
	}
	// 展示顺序：建议占比降序（弹窗主读数），稳定按 UID 兜底。
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Suggested != out[j].Suggested {
			return out[i].Suggested > out[j].Suggested
		}
		return out[i].UID < out[j].UID
	})
	if AnyPriorityActive(list) {
		warnings = append(warnings, "account_priority configured — shares only apply within the current priority tier")
	}
	return out, warnings
}

// AnyPriorityActive 检测池内是否有账号配了 account_priority（share 只在当前优先
// 层内生效，issue #62 语义；分层存在时占比的「独占」范围被层切分，需提示用户）。
func AnyPriorityActive(list []wbbridge.PoolStatus) bool {
	for _, s := range list {
		if s.Priority > 0 {
			return true
		}
	}
	return false
}

// Round2 两位小数（占比写入/展示统一精度）。
func Round2(v float64) float64 { return math.Round(v*100) / 100 }

type WbShareSched struct {
	Enabled bool
	Cron    string
}

var (
	WbShareOptMu   sync.Mutex
	WbShareLastMin int // 上次触发的 Unix 分钟；同一分钟防重，跨 tick 区间扫描防漏
)

func RunWorkbuddyShareOptimize(trigger string) {
	if WorkbuddySub == nil || WorkbuddySub.SaveConfig == nil || WorkbuddySub.Pool == nil {
		return
	}
	sugs, warnings := ComputeShareSuggestions(WorkbuddySub.Pool.List(), time.Now())
	if len(sugs) == 0 {
		log.Printf("workbuddy: 权重优化（%s）：无快过期压力，跳过", trigger)
		return
	}
	shares := make(map[string]float64, len(sugs))
	for _, s := range sugs {
		shares[s.UID] = s.Suggested
	}
	// 幂等：与 pool 当前生效占比（Status.Share）全等则跳过写入。
	current := map[string]float64{}
	for _, st := range WorkbuddySub.Pool.List() {
		if st.Share > 0 {
			current[st.UID] = st.Share
		}
	}
	if ShareMapsEqual(shares, current) {
		log.Printf("workbuddy: 权重优化（%s）：占比与当前一致，跳过写入", trigger)
		return
	}
	raw, err := json.Marshal(map[string]any{"pool": map[string]any{"account_share": shares}})
	if err != nil {
		log.Printf("workbuddy: 权重优化（%s）：构造配置失败: %v", trigger, err)
		return
	}
	if _, err := WorkbuddySub.SaveConfig(raw); err != nil {
		log.Printf("workbuddy: 权重优化（%s）：写入失败: %v", trigger, err)
		return
	}
	log.Printf("workbuddy: 权重优化（%s）已应用 %d 个账号占比 %v %v", trigger, len(shares), shares, warnings)
}

// ShareMapsEqual 占比表全等（浮点两位精度比较，避开 1e-15 尾差）。
func ShareMapsEqual(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || math.Abs(va-vb) > 0.005 {
			return false
		}
	}
	return true
}

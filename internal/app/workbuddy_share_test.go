package app

// 权重优化核心算法测试（computeShareSuggestions）：占比纯按日均需耗（快过期批次
// ÷ 距到期自然日，最少 1 天），无批次号保底合计 20% 内部按余额比例分（全 0 均分）。
// 各场景的期望值都按此公式手算，作为实现的行为锚点。

import (
	"testing"
	"time"

	wbpool "afree-proxy/internal/workbuddy/pool"
)

func shareTestStatus(uid string, credits int64, buckets []wbpool.ExpiryBucket) wbpool.Status {
	on := true
	return wbpool.Status{
		UID: uid, Nickname: uid, Credits: credits,
		AccountEnabled: &on, PoolEnabled: true,
		CreditsExpiryBuckets: buckets,
	}
}

func bucketOn(date string, remain int64) wbpool.ExpiryBucket {
	return wbpool.ExpiryBucket{Date: date, Remain: remain}
}

func shareSugByUID(sugs []wbShareSuggestion, uid string) *wbShareSuggestion {
	for i := range sugs {
		if sugs[i].UID == uid {
			return &sugs[i]
		}
	}
	return nil
}

func TestShareOptimizePressuredAndReserved(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	list := []wbpool.Status{
		// a：今天到期 100 + 2 天后到期 100 → 100/1 + 100/2 = 150
		shareTestStatus("a", 1000, []wbpool.ExpiryBucket{bucketOn("2026-11-15", 100), bucketOn("2026-11-17", 100)}),
		// b：2 天后到期 60 → 30
		shareTestStatus("b", 1000, []wbpool.ExpiryBucket{bucketOn("2026-11-17", 60)}),
		// c/d：无批次保底，余额 3:1 → 15:5
		shareTestStatus("c", 300, nil),
		shareTestStatus("d", 100, nil),
		// e：disabled，不参与
		func() wbpool.Status { s := shareTestStatus("e", 500, nil); s.Disabled = true; s.DisabledReason = "x"; return s }(),
	}
	sugs, warnings := computeShareSuggestions(list, now)
	if len(sugs) != 4 {
		t.Fatalf("期望 4 条建议（e 被排除），得到 %d", len(sugs))
	}
	if got := shareSugByUID(sugs, "a"); got == nil || got.DailyNeed != 150 || got.Suggested != 66.67 {
		t.Fatalf("a 期望 daily=150 suggested=66.67（150/180×80），得到 %+v", got)
	}
	if got := shareSugByUID(sugs, "b"); got == nil || got.DailyNeed != 30 || got.Suggested != 13.33 {
		// 30/180×80 = 13.33
		t.Fatalf("b 期望 daily=30 suggested=13.33，得到 %+v", got)
	}
	if got := shareSugByUID(sugs, "c"); got == nil || !got.Reserved || got.Suggested != 15 {
		t.Fatalf("c 期望保底 15（20×300/400），得到 %+v", got)
	}
	if got := shareSugByUID(sugs, "d"); got == nil || got.Suggested != 5 {
		t.Fatalf("d 期望保底 5，得到 %+v", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("无 priority 配置不应有警告: %v", warnings)
	}
}

func TestShareOptimizeAllReserved(t *testing.T) {
	now := time.Now()
	list := []wbpool.Status{shareTestStatus("a", 100, nil), shareTestStatus("b", 100, nil)}
	sugs, warnings := computeShareSuggestions(list, now)
	if sugs != nil {
		t.Fatalf("全员无压力应返回空建议: %+v", sugs)
	}
	if len(warnings) == 0 {
		t.Fatal("应带 no expiring credits pressure 提示")
	}
}

func TestShareOptimizePastBucketCountsAsToday(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	list := []wbpool.Status{
		// 过期未扣完的批次按最紧迫（÷1）算
		shareTestStatus("a", 100, []wbpool.ExpiryBucket{bucketOn("2026-11-14", 50)}),
		shareTestStatus("b", 100, []wbpool.ExpiryBucket{bucketOn("2026-11-16", 50)}),
	}
	sugs, _ := computeShareSuggestions(list, now)
	// a: 50/1=50；b: 50/1（11-16 距 11-15 = 1 天）=50 → 无保底号占比归一，各 50
	//（2026-11-15 修：无保底号时 pressuredPct=100，原实现恒 80 导致总和 80）
	a, b := shareSugByUID(sugs, "a"), shareSugByUID(sugs, "b")
	if a == nil || b == nil || a.Suggested != 50 || b.Suggested != 50 {
		t.Fatalf("期望各 50%%，得到 a=%+v b=%+v", a, b)
	}
}

// 无保底号时占比总和必须为 100（2026-11-15 生产实测回归：12 号全有批次，
// 原实现占比总和恒 80）。
func TestShareOptimizeNoReservedSumsTo100(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	list := []wbpool.Status{
		shareTestStatus("a", 100, []wbpool.ExpiryBucket{bucketOn("2026-11-15", 102), bucketOn("2026-11-16", 606)}),
		shareTestStatus("b", 100, []wbpool.ExpiryBucket{bucketOn("2026-11-15", 1), bucketOn("2026-11-16", 709)}),
		shareTestStatus("c", 100, []wbpool.ExpiryBucket{bucketOn("2026-11-24", 3873)}),
	}
	sugs, _ := computeShareSuggestions(list, now)
	sum := 0.0
	for _, s := range sugs {
		sum += s.Suggested
	}
	if len(sugs) != 3 || sum < 99.9 || sum > 100.1 {
		t.Fatalf("3 个压力号占比和应为 100，得到 %d 条和 %v", len(sugs), sum)
	}
}

func TestShareOptimizePriorityWarning(t *testing.T) {
	now := time.Now()
	list := []wbpool.Status{
		func() wbpool.Status {
			s := shareTestStatus("a", 100, []wbpool.ExpiryBucket{bucketOn("2026-11-20", 100)})
			s.Priority = 1
			return s
		}(),
		shareTestStatus("b", 100, nil),
	}
	sugs, warnings := computeShareSuggestions(list, now)
	if len(sugs) != 2 {
		t.Fatalf("期望 2 条建议，得到 %d", len(sugs))
	}
	if len(warnings) == 0 {
		t.Fatal("priority 激活时应带警告")
	}
}

func TestShareOptimizeReservedZeroCreditsEvenSplit(t *testing.T) {
	now := time.Now()
	list := []wbpool.Status{
		shareTestStatus("a", 200, []wbpool.ExpiryBucket{bucketOn("2026-11-20", 100)}),
		// 保底号余额全 0 → 均分 20%（10/10）
		shareTestStatus("r1", 0, nil),
		shareTestStatus("r2", 0, nil),
	}
	sugs, _ := computeShareSuggestions(list, now)
	r1, r2 := shareSugByUID(sugs, "r1"), shareSugByUID(sugs, "r2")
	if r1 == nil || r2 == nil || r1.Suggested != 10 || r2.Suggested != 10 {
		t.Fatalf("余额全 0 应均分保底 10/10，得到 r1=%+v r2=%+v", r1, r2)
	}
}

// accountfacts.go wb 池状态的配置备份导出/导入（2026-11-03 用户定三层口径）：
//
//   - **导出 = 容器全部数据**：StateSnapshotJSON 返回与 state.json 落盘同形态的
//     全量快照（accounts + scheduling + order），账号事实与本机统计都在内。
//   - **导入按层分流**：账号事实（余额/冷却/熔断降权/限额/实测成本/签到/运维
//     意志——账号在上游的客观状态，换部署环境依然成立）**无论勾选与否都应用**；
//     本机统计（token 用量、成功/失败累计、最近成功/失败时刻、调度轮转游标
//     ——本机路由行为的痕迹）仅在导入方勾选「导入统计数据」时应用，不勾选时
//     保留本机现值。
//   - **配置指纹清单不涉及本层**：state 类文件不进 config-manifest（配置态专属）。
//
// 此前（2026-11-01 state 段注释）把 wb 运行态整体归为「本机事实、统计随数据卷
// 复制迁移」，2026-11-03 用户纠正：账号事实 ≠ 本机事实，账号事实必须随备份迁移。
package pool

import (
	"encoding/json"
	"math"
	"time"
)

// StateSnapshotJSON 全量状态快照（配置备份 workbuddy.state 段）：与 state.json
// 落盘同形态（accounts + scheduling + order），账号事实与本机统计都在内——
// 「导出 = 容器全部数据」由导出端保证，导入端再按勾选分流。
func (p *Pool) StateSnapshotJSON() json.RawMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, err := json.Marshal(p.stateOverviewLocked())
	if err != nil {
		return nil
	}
	return data
}

// ImportStateBackupJSON 把备份中的 wb state 快照（state.json 同形态）合并进池：
//   - 账号事实字段（见 applyAccountFactsLocked）**总是**应用，含与 load() 同款
//     惰性清洗（过期冷却/降权/成本观测不复活）；
//   - 本机统计字段（token_usage/success_count/err_total/last_success/last_err）
//     仅 includeStats 时应用；ErrCount 是旧文件迁移源，随统计一并处理；
//   - 调度轮转游标（scheduling）属本机统计，仅 includeStats 时恢复；
//   - order 不在此处理（备份的 workbuddy.order 段单独套用，避免双路径漂移）；
//   - uid 不在池中的条目丢弃——账号集合以 auths 目录为准，备份不得凭空造号。
//
// 返回应用账号数。应用后置 dirty 并同步 Flush 一次（不依赖 5s flusher，导入后
// 立刻崩溃也不丢）。
func (p *Pool) ImportStateBackupJSON(raw []byte, includeStats bool) (int, error) {
	var sf stateFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		return 0, err
	}
	now := time.Now()
	applied := 0
	p.mu.Lock()
	for uid, s := range sf.Accounts {
		e, ok := p.byUID[uid]
		if !ok || e == nil {
			continue
		}
		e.applyAccountFactsLocked(s, now)
		if includeStats {
			e.successCount = s.SuccessCount
			e.errTotal = s.ErrTotal
			if int64(s.ErrCount) > e.errTotal {
				e.errTotal = int64(s.ErrCount)
			}
			e.lastErr = s.LastErr
			e.lastSuccess = s.LastSuccess
			e.tokenUsage = s.TokenUsage
		}
		applied++
	}
	if includeStats && len(sf.Scheduling) > 0 {
		p.restoreSchedulingLocked(sf.Scheduling)
	}
	p.mu.Unlock()
	if applied > 0 {
		p.dirty.Store(true)
		p.Flush()
	}
	return applied, nil
}

// applyAccountFactsLocked 把账号事实字段从持久化形态写入 entry，含与 load()
// 同款惰性清洗（过期冷却/降权/成本观测不复活）与迁移修正。本机统计字段
// （successCount/errTotal/lastSuccess/lastErr/tokenUsage）不在本函数职责内——
// 它们是部署侧痕迹，由 applyAccountsLocked（本地/快照恢复，全量）与
// ImportStateBackupJSON（备份导入，勾选统计时）单独应用。
// 返回是否发生需要落盘的迁移修正。调用方必须已持 p.mu。
func (e *entry) applyAccountFactsLocked(s stateAccount, now time.Time) bool {
	migrated := false
	e.credits = s.Credits
	e.creditsRemainder = s.CreditsRemainder
	e.creditsTotal = s.CreditsTotal
	e.creditsExpiring = s.CreditsExpiring
	e.creditsEarliestExpiry = s.CreditsEarliestExpiry
	e.creditsEarliestRemaining = s.CreditsEarliestRemaining
	e.disabled = s.Disabled
	e.paused = s.Paused
	e.reason = s.Reason
	e.disabledAt = s.DisabledAt
	e.until = s.Until
	e.coolKind = s.CoolKind
	e.lastCheckinDay = s.LastCheckinDay
	e.lastCheckinAttemptDay = s.LastCheckinAttemptDay
	e.lastCheckinErr = s.LastCheckinError
	e.softStreak = s.SoftStreak
	e.sessionDeadFails = s.SessionDeadFails
	e.consecutiveFails = s.ConsecutiveFails
	// 亚积分余量合法性：越界/非有限值（旧数据或手改）清 0，保守回退整数口径。
	if math.IsNaN(e.creditsRemainder) || math.IsInf(e.creditsRemainder, 0) || e.creditsRemainder < -0.5 || e.creditsRemainder >= 0.5 {
		e.creditsRemainder = 0
		migrated = true
	}
	// 到期快照按当前时刻惰性清洗：已过期、零剩余或超出总余额的脏数据不恢复。
	if e.creditsExpiring < 0 {
		e.creditsExpiring = 0
	}
	if e.creditsExpiring > e.credits {
		e.creditsExpiring = e.credits
	}
	if e.creditsEarliestRemaining < 0 || e.creditsEarliestRemaining > e.credits {
		e.creditsEarliestRemaining = 0
	}
	if e.creditsEarliestRemaining == 0 || e.creditsEarliestExpiry.IsZero() || !now.Before(e.creditsEarliestExpiry) {
		e.creditsEarliestExpiry = time.Time{}
		e.creditsEarliestRemaining = 0
	}
	// 按日到期批次：惰性剔除已过期/零剩余条目；有批次时以批次为准同步最早批次，
	// 保证导入后首页快照与明细仍同口径。
	e.expiryBuckets = sanitizeExpiryBuckets(s.CreditsExpiryBuckets, now)
	if len(e.expiryBuckets) > 0 {
		e.creditsEarliestExpiry = e.expiryBuckets[0].ExpireAt
		e.creditsEarliestRemaining = e.expiryBuckets[0].Remain
	}
	// 熔断器：breakerUntil 未过期才恢复（过期不复活），retryCount 仅在熔断仍
	// 有效时保留（否则归零，不保留无用退避指数）。
	if s.BreakerUntil != nil && now.Before(*s.BreakerUntil) {
		e.breakerUntil = *s.BreakerUntil
		e.retryCount = s.RetryCount
	}
	// 连败降权：未过期才恢复（过期/零值不写不复活）。
	if s.DegradeUntil != nil && now.Before(*s.DegradeUntil) {
		e.degradeUntil = *s.DegradeUntil
	}
	// 模型级独立冷却（6004 重置墙钟 / 11102 负缓存）：惰性过滤已过期条目。
	if len(s.ModelCooldowns) > 0 {
		for m, mc := range s.ModelCooldowns {
			if mc.Until.IsZero() || !now.Before(mc.Until) {
				continue
			}
			if e.modelCooldowns == nil {
				e.modelCooldowns = map[string]modelCooldown{}
			}
			e.modelCooldowns[m] = modelCooldown{Until: mc.Until, ResetAt: mc.ResetAt, Reason: mc.Reason, AuditOnly: mc.AuditOnly}
		}
	}
	// 手动关闭 (账号, 模型)：**无过期过滤**——它是永久语义，与冷却（过期即弃）
	// 不同，不得被惰性清理顺手删掉。
	if len(s.ModelOff) > 0 {
		e.modelOff = map[string]modelOff{}
		for m, o := range s.ModelOff {
			e.modelOff[m] = modelOff{Since: o.Since, Note: o.Note}
		}
	}
	// 成本账本：惰性过滤过期（modelCostTTL 外不恢复）+ 剔除结构破损条目
	// （负/非有限 per1k / 零 LastSeen）。仅对「缺少可信标记的零价条目」降为
	// 未知（迁移一次），正价旧记录保持可用。
	if len(s.ModelCosts) > 0 {
		for m, mc := range s.ModelCosts {
			if mc.LastSeen.IsZero() || now.Sub(mc.LastSeen) > modelCostTTL || mc.CostPer1k < 0 || math.IsNaN(mc.CostPer1k) || math.IsInf(mc.CostPer1k, 0) {
				continue
			}
			if mc.CostPer1k == 0 && !mc.CreditVerified {
				migrated = true
				continue
			}
			if e.modelCost == nil {
				e.modelCost = map[string]modelCostEntry{}
			}
			e.modelCost[m] = modelCostEntry{CostPer1k: mc.CostPer1k, LastSeen: mc.LastSeen, Samples: mc.Samples, CreditVerified: mc.CreditVerified}
		}
	}
	return migrated
}

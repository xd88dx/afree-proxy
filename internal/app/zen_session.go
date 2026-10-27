package app

import (
	"fmt"
	"log"
	"sync"
	"time"

	"afree-proxy/internal/kit"
)

// ============ zen 会话（2026-10-27 起：每次 API 调用全新） ============
//
// 背景（2026-10-09 实测结论，已被 2026-10-27 观察取代）：zen 免费层的
// 会话门曾被认为是无状态的格式检查——会话 ID 匹配
// ^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$ 即放行。服务端既不检查"见过此 ID"，
// 也不检查时间戳新鲜度：本地铸造的合法格式 ID 三次直连通过（含复用），
// 而旧 sess_ 随机占位因格式不符必然 403 FreeTierError。
// （早期"只有 CLI 收割的会话能用"的结论是被占位 ID 的坏格式混淆了。)
//
// UPDATE 2026-10-27: that stateless conclusion no longer holds for
// repeated traffic. Official CLI multi-observation
// (cline-proxy-audit/cli-multi-observation-2026-10-27.md) shows a
// session reused across unrelated history-less requests gets flagged
// to midnight UTC after the first call passes, while a fresh session
// per CLI run does not. Upstream calls therefore use zenCallIdentity
// (fresh per API call) below; the sticky store below is legacy,
// kept only for admin snapshot / rotator / tests.
//
// 因此每个 zen key 绑定一个稳定的本地铸造 ses_ ID（kit.MintZenSessionID，
// 结构与 CLI 铸造字节兼容）+ 固定的 ai-sdk 形态 UA；msg_ 请求 ID 仍每次
// 随机。会话表持久化到 zen-config.json 的 sessions 段，重启后沿用同一身份。
//
// 会话失效（FreeTier 403）时本地直接换新：refreshZenSession（连续 2 次
// 403 触发，1 分钟 key 级退避防热循环）。若 zen 未来收紧门禁（合法格式
// 也被拒），日志里的换新频率会先于用户感知升高——这就是 tripwire。

type zenSessionEntry struct {
	Session string `json:"session"`
	UA      string `json:"ua"`
	Updated int64  `json:"updated"`
	// Minted 历史字段：曾区分"CLI 收割过"与"本地随机占位"。本地铸造成为
	// 唯一来源后所有条目恒为 true，保留只为兼容旧文件（读入后即不再有意义）。
	Minted bool `json:"minted,omitempty"`
	// CreatedAt 会话创建时间（unix 秒），日志/面板展示年龄用。
	// 旧文件的 harvestedAt 读入时迁移到该字段，下次保存时写出新键名。
	CreatedAt int64 `json:"createdAt,omitempty"`
	// HarvestedAt 旧字段名，仅为读入旧文件保留（unmarshal 兜底），
	// 加载时并入 CreatedAt，不再写回。
	HarvestedAt int64 `json:"harvestedAt,omitempty"`
	// RefreshedAt 最近一次"因 403 而被动换新会话"的时间（unix 秒）。
	// 后台巡检/轮换铸造不写它（那是计划内换新），只有 403 触发的换新才写。
	// 用途：识别"换新后很快又 403"的持续性故障——这种 403 换会话救不回来，
	// 继续铸会话只会每次请求白跑一次上游（见 zenForbiddenEscalateWindow）。
	RefreshedAt int64 `json:"refreshedAt,omitempty"`
}

var (
	zenSessMu     sync.Mutex
	zenSessions   = map[string]*zenSessionEntry{} // zen key -> sticky identity
	zenSessLoaded bool
	zenSessPath   string
	// zenSessSaveBlocked 会话文件读取失败（非 ENOENT）时置位：此时内存表是空的，
	// 任何 save 都会把磁盘上读不到的原文件覆盖掉。宁可本次运行不落盘，也不能
	// 销毁可能有价值的数据（重启后重试读取）。
	zenSessSaveBlocked bool
	// zenNativeUA 官方 CLI v2 的 UA（会话粘性与轮换列表共用）。
	// 2026-10-26 上游实测（3758eb7）：CLI v2.1.4 在 chat 与 responses 两条路径
	// 都发 opencode/latest/2.1.4/cli；旧 ai-sdk 形态 UA（1.18.35/1.19.3）已不是
	// 官方客户端的 UA，与 v2 TLS 指纹（tls_bun.go）组合会被识别为旧客户端。
	zenNativeUA = "opencode/latest/2.1.4/cli"
)

// loadZenSessions 启动时/首次使用时加载持久化会话（只读一次，失败则空跑）。
// 会话表住在 zen-config.json 的 sessions 段；整文件损坏时的改名留档由
// loadZenConfigFileLocked 统一处理（不会再出现"只有会话段坏了"的情况）。
func loadZenSessions() {
	zenSessMu.Lock()
	defer zenSessMu.Unlock()
	if zenSessLoaded {
		return
	}
	zenSessLoaded = true
	m := func() map[string]*zenSessionEntry {
		zenFileMu.Lock()
		defer zenFileMu.Unlock()
		return loadZenConfigFileLocked().Sessions
	}()
	replaced, migrated, stamped := 0, 0, 0
	now := time.Now().Unix()
	for k, e := range m {
		if e == nil || e.Session == "" {
			continue
		}
		// 旧 harvestedAt → CreatedAt 一次性迁移（写出时只写新键名）。
		if e.CreatedAt == 0 && e.HarvestedAt > 0 {
			e.CreatedAt = e.HarvestedAt
		}
		if kit.ValidZenSessionID(e.Session) {
			// 合法格式（旧收割条目或本会话铸造的）：原样保留，格式即凭证。
			if !e.Minted {
				e.Minted = true
				migrated++
			}
			// 无创建时间的旧条目：以加载时刻起算轮换时钟，否则老化轮换对它永久失效
			// （CreatedAt==0 被判为"年龄未知"而跳过）。只在真有会话时才补。
			if e.CreatedAt == 0 {
				e.CreatedAt = now
				stamped++
			}
		} else {
			// sess_* 占位等非法格式：今天就在 403，永远不会自愈，直接换成
			// 本地铸造的新 ID。旧文件若无 CreatedAt 以加载时刻兜底。
			e.Session = kit.MintZenSessionID()
			e.Minted = true
			if e.CreatedAt == 0 {
				e.CreatedAt = now
			}
			replaced++
		}
		zenSessions[k] = e
	}
	log.Printf("zen sessions loaded: %d key(s) with sticky identity", len(zenSessions))
	if replaced > 0 {
		log.Printf("zen sessions migrated: %d invalid-format placeholder(s) replaced with locally minted IDs", replaced)
	}
	if migrated > 0 {
		log.Printf("zen sessions migrated: %d entry(ies) marked minted from session id format", migrated)
	}
	if stamped > 0 {
		log.Printf("zen sessions migrated: %d entry(ies) stamped with a rotation clock (createdAt=load time)", stamped)
	}
	if replaced > 0 || migrated > 0 || stamped > 0 {
		saveZenSessionsLocked()
	}
}

// saveZenSessionsLocked 持久化当前会话表（调用方持有 zenSessMu）。
// 只覆盖 sessions 段，zen 配置与另外两张学习表原样保留。
func saveZenSessionsLocked() {
	if zenSessSaveBlocked {
		return
	}
	next := make(map[string]*zenSessionEntry, len(zenSessions))
	for k, v := range zenSessions {
		next[k] = v
	}
	mutateZenConfigFile(func(f *zenConfigFileData) {
		f.Sessions = next
	})
}

// saveZenSessions 对外持久化入口（内部写已在锁外调用）。
func saveZenSessions() {
	zenSessMu.Lock()
	defer zenSessMu.Unlock()
	saveZenSessionsLocked()
}

// zenSessionLive 该 key 是否有一个格式合法的粘性会话。本地铸造后所有条目
// 生而合法；保留批量/单点两个入口是为了 pickZenKey 的两段轮转语义不变。
func zenSessionLive(key string) bool {
	if key == "" {
		return false
	}
	loadZenSessions()
	zenSessMu.Lock()
	defer zenSessMu.Unlock()
	e := zenSessions[key]
	return e != nil && kit.ValidZenSessionID(e.Session)
}

// zenLiveKeys 批量查询（一次加锁），供 pickZenKey 在轮转时优先挑 live key。
func zenLiveKeys(keys []string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}
	loadZenSessions()
	zenSessMu.Lock()
	defer zenSessMu.Unlock()
	live := make(map[string]bool, len(keys))
	for _, k := range keys {
		if e := zenSessions[k]; e != nil && kit.ValidZenSessionID(e.Session) {
			live[k] = true
		}
	}
	return live
}

// zenSessionSnapshot 每个 key 的会话状态（管理面板展示；session 截断显示）。
type zenSessionSnapshot struct {
	Minted    bool
	Live      bool
	Session   string
	CreatedAt int64
}

func zenSessionSnapshotOf(key string) zenSessionSnapshot {
	loadZenSessions()
	zenSessMu.Lock()
	defer zenSessMu.Unlock()
	e := zenSessions[key]
	if e == nil {
		return zenSessionSnapshot{}
	}
	live := kit.ValidZenSessionID(e.Session)
	return zenSessionSnapshot{
		Minted:    live, // 格式合法即视为 minted（本地铸造是唯一来源）
		Live:      live,
		Session:   kit.Truncate(e.Session, 12),
		CreatedAt: e.CreatedAt,
	}
}

// zenSessionDesc 供日志使用的会话描述：展示会话年龄——合法格式会话 403
// 意味着寿命/额度窗口到期或门禁收紧，换新频率是门禁变化的 tripwire。
func zenSessionDesc(key string) string {
	s := zenSessionSnapshotOf(key)
	switch {
	case s.Session == "":
		return "no session"
	case s.CreatedAt <= 0:
		return "minted (age unknown)"
	default:
		age := time.Since(time.Unix(s.CreatedAt, 0)).Round(time.Minute)
		return fmt.Sprintf("minted %v ago (local)", age)
	}
}

// mintZenSessionEntryLocked 就地刷新条目的会话：新 ID + 新创建时间 + 合法标记，
// 并在 UA 缺失时补上客户端指纹。调用方须持有 zenSessMu。
// 请求路径与后台巡检共用，避免两处铸造逻辑漂移。
func mintZenSessionEntryLocked(e *zenSessionEntry, now time.Time) {
	e.Session = kit.MintZenSessionID()
	e.CreatedAt = now.Unix()
	e.Minted = true
	// 主动铸造＝计划内换新（轮换/首铸），不是对 403 的反应：清掉被动换新
	// 时间戳，让下一次真正的 403 仍能获得一次换新机会。
	e.RefreshedAt = 0
	if e.UA == "" {
		e.UA = zenNativeUA
	}
}

// zenCallIdentity is the per-API-call identity used by upstream calls
// since 2026-10-27. It mints one fresh session per user API call
// (Cursor, ZCode, OpenClaw, Hermes, Test, compact, Anthropic — all go
// through callZenAPI/callZenResponsesAPI, so all get this behaviour),
// reuses that session only for retries of the SAME call, and mints a
// new one if the key switches mid-call or a 403 forces a refresh.
//
// Why fresh per call, not sticky: official CLI v2.1.4 creates a fresh
// session per `run` invocation; within a session it always sends full
// history, and its first turn always includes title calls for that
// session (see cline-proxy-audit/cli-multi-observation-2026-10-27.md).
// The gateway's old sticky session (120 min, persisted, reused across
// unrelated history-less requests) therefore never looks like a real
// CLI session's 2nd call, and upstream flags that pattern to midnight
// UTC after the first call passes — a shadow ban that costs a whole
// day, for zero benefit, since IDEs/agents already send full history
// in every request themselves. Fresh per call costs no extra upstream
// calls (unlike title-call replication, which would triple traffic
// against the free allowance), and it is compatible with stateless
// OpenAI-style clients: history lives in messages, not in the session ID.
// The request ID is still fresh per attempt (CLI semantics: many msg_
// per session), while session/UA stay stable within one call.
type zenCallIdentity struct {
	byKey map[string][3]string // key -> (session, request, ua); request refreshed per attempt below
}

func newZenCallIdentity() *zenCallIdentity {
	return &zenCallIdentity{byKey: map[string][3]string{}}
}

func (c *zenCallIdentity) forKey(key string) (sess, req, ua string) {
	if v, ok := c.byKey[key]; ok {
		// Same call, same key: same session, fresh request ID per attempt.
		return v[0], kit.MintZenRequestID(), v[2]
	}
	sess, req, ua = kit.FreshZenIdentity()
	c.byKey[key] = [3]string{sess, req, ua}
	return sess, req, ua
}

// refresh drops the cached identity for key so the next attempt of
// this call mints a fresh session (403 path). It does not touch the
// legacy sticky store.
func (c *zenCallIdentity) refresh(key string) {
	delete(c.byKey, key)
}

// StickyZenIdentity is LEGACY since 2026-10-27: upstream calls no
// longer use it (see zenCallIdentity above). It is kept for the admin
// panel snapshot, the session rotator, and its tests, so those do not
// break, but no new upstream code should call it.
// It returns a key-bound stable identity: session ID and UA reused
// across requests, request ID fresh each time.
// 返回 (session, request, user-agent)。
//
// 老化轮换已随 2026-10-27 会话语义下线（fresh per call，本函数对上游调用
// 不再被走过）：条目年龄不再驱动重铸，会话长生只影响面板展示的铸造时刻。
// 保留惰性首铸：面板/巡检都需要条目存在才算"这个 key 有会话状态"。
func StickyZenIdentity(key string) (sess, req, ua string) {
	loadZenSessions()
	zenSessMu.Lock()
	defer zenSessMu.Unlock()
	now := time.Now()
	e, ok := zenSessions[key]
	if !ok || !kit.ValidZenSessionID(e.Session) {
		if e == nil {
			e = &zenSessionEntry{}
			zenSessions[key] = e
		}
		mintZenSessionEntryLocked(e, now)
		saveZenSessionsLocked()
		log.Printf("zen header session minted locally for key#%d (display only): %s", keyIndex(key), kit.Truncate(e.Session, 24))
	}
	e.Updated = now.Unix()
	// 补 UA：旧版本文件或外部改写的条目可能缺这一项，空 UA 发出会被上游
	// 按非 CLI 流量处理。UA 属于客户端指纹，须与会话铸造时一致。
	if e.UA == "" {
		e.UA = zenNativeUA
	}
	return e.Session, kit.MintZenRequestID(), e.UA
}

// pruneZenKeyState 配置变更后清理已移除 key 的运行时状态（会话粘性 + 403 印记）。
// valid 为当前有效 key 集合。
func pruneZenKeyState(valid map[string]bool) {
	zenSessFailedMu.Lock()
	for k := range zenSessFailed {
		if !valid[k] {
			delete(zenSessFailed, k)
		}
	}
	zenSessFailedMu.Unlock()

	zenSessMu.Lock()
	removed := 0
	for k := range zenSessions {
		if !valid[k] {
			delete(zenSessions, k)
			removed++
		}
	}
	if removed > 0 {
		saveZenSessionsLocked()
	}
	zenSessMu.Unlock()
	if removed > 0 {
		log.Printf("zen sessions pruned: %d removed key(s)", removed)
	}
}

// ============ 403 恢复：本地换新会话 ============
//
// 语义（重构后，替代旧的"连续 2 次 403 + 后台异步换新"）：
//   - 请求路径收到 FreeTier 403 后同步换一次会话（本请求内至多一次，由调用点的
//     sessionRetried 标志保证），随后在**同一 key** 上重试——不再跨 key 扇出。
//     跨 key 扇出是错的：403 针对的是 key/会话，换到别的 key 只会把同一批坏
//     状态扩散到全池，而不是绕过它。
//   - 同 key 换新后仍 403 ⇒ 该 key 会话/额度窗口确实不可用，短暂冷却该 key 并
//     快速失败，不发第三种请求形态。
//   - pinKey（面板 Test）探测完全不触碰会话状态、不冷却、不重试：探测结论只属于
//     那次点击，绝不能污染生产 key 状态。
//
// 若 zen 收紧门禁（合法格式也被拒），冷却+快速失败会让 403 更早浮现，而换新
// 频率仍是日志里的门禁变化信号。

// zenSessionMarkSuccess 2xx 后清除该 key 的 403 印记（成功即该 key 现在可用，
// 后续偶发 403 重新按"本请求内换新一次"处理）。调用点在两条上游调用路径的 200
// 分支；对未标记的 key 是无害 no-op。
func zenSessionMarkSuccess(key string) {
	if key == "" {
		return
	}
	zenSessFailedMu.Lock()
	delete(zenSessFailed, key)
	zenSessFailedMu.Unlock()
	// 同时清掉"被动换新"时间戳：这一跳 200 说明该 key+出口当前可用，将来
	// 偶发的一次 403 理应仍能获得一次换新机会（不能被历史失败永久封死）。
	// zenSessions 未加载时无需处理——条目本来就不存在。
	zenSessMu.Lock()
	if e, ok := zenSessions[key]; ok && e.RefreshedAt != 0 {
		e.RefreshedAt = 0
		saveZenSessionsLocked()
	}
	zenSessMu.Unlock()
}

// zenSessFailedMu/zenSessFailed 记录"换新后仍 403"的 key（tripwire/面板观测）。
// 计数本身不参与决策；跨请求的限流决策看 zenSessionEntry.RefreshedAt
// （见 zenSessionRefreshedRecently），单请求内的看调用点的 sessionRetried 标志。
var (
	zenSessFailedMu sync.Mutex
	zenSessFailed   = map[string]int{}
)

// zenSessionFailed 记录该 key 在最近一次请求里"换新后仍 403"的失败印记，
// 供面板/日志观测（tripwire）。无返回值——次数累积在 zenSessFailed 里。
func zenSessionFailed(key string) {
	if key == "" {
		return
	}
	zenSessFailedMu.Lock()
	zenSessFailed[key]++
	zenSessFailedMu.Unlock()
}

// refreshZenSession 同步把该 key 的粘性会话换成本地铸造的新 ID（新会话、保留 UA）。
// 幂等且零成本（无子进程、无额度消耗）：FreeTier 403 的调用点在本请求内至多调用
// 一次，然后同 key 重试。key 为空或无 key 配置时不动；已从配置移除的 key
// 也不重建——否则 pruneZenKeyState 刚清掉的条目会被这里复活，让
// .zen-sessions.json 单调增长。空池在取 key 阶段就会明确报错，不会走到这里。
func refreshZenSession(key string) {
	if key == "" {
		return
	}
	if !zenKeyConfigured(key) {
		return
	}
	loadZenSessions()
	zenSessMu.Lock()
	e, ok := zenSessions[key]
	if !ok {
		// key 未在会话表里（例如首次即 403）：建一个新条目即可，下次请求复用。
		e = &zenSessionEntry{}
		zenSessions[key] = e
	}
	mintZenSessionEntryLocked(e, time.Now())
	// 这是"因 403 被动换新"：记下时间戳，供持续性故障闸门判断
	// （换新后窗口内再次干净 403 ⇒ 换会话救不回来，见 zenSessionRefreshedRecently）。
	e.RefreshedAt = time.Now().Unix()
	saveZenSessionsLocked()
	sess := e.Session
	zenSessMu.Unlock()
	log.Printf("zen session refreshed locally for key#%d: %s", keyIndex(key), kit.Truncate(sess, 24))
}

// ============ 持续性 403 闸门（跨请求记忆） ============
//
// 为什么需要：单请求内的 sessionRetried 只能保证"一次请求最多换一次会话"，
// 对跨请求的持续性 403 毫无记忆。线上实测（2026-10-20）：key#4 三天 70 次上游
// 尝试里 38 次 403（54%），被拒会话年龄全部在 2~30 分钟之间——每次请求都新铸
// 一个会话、每次都 403，日志上一行行"refreshing session locally"看起来像在自愈，
// 实际每次都是白铸会话 + 白打一次上游（约 1.5s 延迟 + 一次失败重试）。
//
// 归因实测（同一会话同一个 key，只换出口）：
//
//	key#4 + northflank-NL → 403 ；key#4 + Mikrus → 200
//	key#3 + Mikrus        → 200 ；key#3 + northflank-NL → 403
//
// 即：这类"干净 403"跟着出口走，不跟着 key/会话走——换会话救不回来，冷却才是
// 正确动作（冷却后 pickZenKey 会跳过该 key，其流量转给健康 key，客户端无感）。
const (
	// zenForbiddenEscalateWindow 被动换新后仍算"同一故障"的观察窗口。
	zenForbiddenEscalateWindow = 10 * time.Minute
	// zenForbiddenCooldown 持续性干净 403 的冷却时长。比单次 403 的 5 分钟长：
	// 这类故障是持续型的，冷却太短只会让该 key 每隔几分钟回来白试一次。
	zenForbiddenCooldown = 15 * time.Minute
)

// zenSessionRefreshedRecently 该 key 是否在窗口内因 403 被动换过会话。
// 返回 true 表示"刚换新还会 403"，调用方应冷却而非再铸会话。zenSessions
// 未加载时返回 false(无从判断，保守放行一次换新)。
func zenSessionRefreshedRecently(key string, now time.Time) bool {
	if key == "" {
		return false
	}
	if zenForbiddenEscalateWindow <= 0 {
		return false
	}
	zenSessMu.Lock()
	defer zenSessMu.Unlock()
	e, ok := zenSessions[key]
	if !ok || e.RefreshedAt == 0 {
		return false
	}
	return now.Sub(time.Unix(e.RefreshedAt, 0)) <= zenForbiddenEscalateWindow
}

// zenKeyConfigured key 是否仍在当前 zen key 池中。
func zenKeyConfigured(key string) bool {
	for _, k := range getZenConfig().Keys {
		if k == key {
			return true
		}
	}
	return false
}

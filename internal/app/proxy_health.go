package app

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/amd"
	"afree-proxy/internal/cline"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/openrouter"
	"afree-proxy/internal/other"
	"afree-proxy/internal/tokenharbor"
	"afree-proxy/internal/trae"
)

// ============================================================================
// 代理出口健康度统计（afree 本地功能）
//
// 纯被动口径：只记录真实上游请求的"出口成败"，不做任何主动探活。成败判定与
// cooldownUpstreamProxy 完全同源 —— 传输层成功（拨号 + CONNECT/SOCKS + TLS）
// 算在线，传输层报错算失败；上游 4xx/5xx 是上游的问题，不扣代理的分；客户端
// 取消（context 取消/截止）不计入任何一端。
//
// 键是规范化代理 URL（不是列表索引）：索引会随列表编辑位移。按用户决策，
// 代理列表一有变动就整体清空统计（resetProxyHealthStats），所以键的稳定性
// 只需覆盖"两次编辑之间"的窗口。
//
// 落盘 proxy-config.json 的 health 段：内存记账 + 脏标记 + 60s 周期原子落盘，
// 重启续算。
// ============================================================================

const (
	// proxyHealthRecentN 最近窗口大小：最近 N 次尝试的成败环形样本。
	proxyHealthRecentN = 50
	// proxyHealthFlushInterval 落盘周期。进程被 kill 时最多丢这个窗口的数据。
	proxyHealthFlushInterval = 60 * time.Second
	// proxyTransportCooldown 传输层失败后该出口退出轮转的时长（**全局层**：
	// 建连阶段失败），与 cline/zen 请求循环里的既有口径一致：短冷却能跳过真死
	// 节点又快速自愈，长冷却会让几次慢请求毒化整个池（2026-10-28 统一到所有平台）。
	proxyTransportCooldown = 2 * time.Minute
	// proxyPlatformCooldown 平台级冷却时长（2026-10-30 用户定 10 分钟）：隧道后
	// 流错误 2-strike 触发。封禁（如 codebuddy 封出口 IP）存续期以小时/天计，
	// 10 分钟显著减少循环期的无效重试；代价是解封后最多 10 分钟才恢复，可接受。
	proxyPlatformCooldown = 10 * time.Minute
)

// isTransportLayerFailure err 是否为传输层失败（拨号 / CONNECT / SOCKS / TLS
// 握手、连接被重置或 EOF）。判定口径与 cline/zen 的冷却同源：
//
//   - 传输层失败 → 出口不可用，该代理应退出轮转；
//   - 各平台的 HTTP 状态类错误（429/5xx/4023）→ 上游返回了响应，出口是通的，
//     冷却好节点正是 09-16「stop poisoning exits on upstream-side failures」
//     要避免的事。
//
// 各平台包有各自的 HTTPError 类型（字段一致、类型不同），这里按类型排除；
// 其余（网络级）一律视为传输层失败。
// isUpstreamSideStreamFailure err 是否为"隧道已建通之后"的流错误。
//
// 判据：Go 的 *url.Error 形如 `Post "https://host/path": <cause>` —— 出现
// 「方法 + URL」前缀，说明拨号 + CONNECT/SOCKS + TLS 已经走通、请求已发出，
// 出口隧道本身是好的；失败发生在隧道之后的流上（上游主动关连接 / 对端重置 /
// 空闲连接被掐）。这类错误的责任在上游或链路中段，**不能判定为出口代理不可用**。
//
// 反例（仍要冷却，不含这些关键词）：`dial tcp …: i/o timeout`、
// `proxyconnect tcp: …`、`socks5 …`、`tls: …` —— 都发生在建连阶段。
//
// 2026-10-30 用户报：`Post "https://www.codebuddy.ai/v2/chat/completions": EOF`
// 把一直在线的 10106 出口冷却了 2 分钟，而绑同一出口的其它凭据照常通。
func isUpstreamSideStreamFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !(strings.HasPrefix(msg, "Post \"") || strings.HasPrefix(msg, "Get \"") ||
		strings.HasPrefix(msg, "Put \"") || strings.HasPrefix(msg, "Head \"") ||
		strings.HasPrefix(msg, "Patch \"") || strings.HasPrefix(msg, "Delete \"")) {
		return false
	}
	_, cause, ok := strings.Cut(msg, "\": ")
	if !ok {
		return false
	}
	// 建连阶段的关键词优先命中：隧道还没建起来就失败，责任在出口（要冷却），
	// 即使 cause 里恰好带着 EOF（例如 `proxyconnect tcp: EOF`）。
	for _, s := range []string{
		"dial tcp", "dial unix", "proxyconnect", "socks", "tls:", "no such host",
		"connect:", "handshake", "connection refused", "network is unreachable",
		"no route to host", "i/o timeout", "context deadline exceeded",
	} {
		if strings.Contains(cause, s) {
			return false
		}
	}
	for _, s := range []string{
		"EOF", "connection reset by peer", "broken pipe",
		"http: server closed idle connection", "server closed the connection", "stream closed",
	} {
		if strings.Contains(cause, s) {
			return true
		}
	}
	return false
}

// 隧道后流错误的冷却门限：单次不冷却（上游 EOF 很常见，冷却就是误判），
// 窗口内累计到阈值才认为这条隧道真断了。计数键是 kind+出口：wb 的失败不能被
// amd 的失败凑数——平台封 IP 的伤害只该落在那个平台。
const (
	proxyStreamFailWindow    = 60 * time.Second
	proxyStreamFailThreshold = 2
)

var (
	proxyStreamFailMu sync.Mutex
	proxyStreamFails  = map[string][]time.Time{}
)

// proxyStreamFailStrike 记一次"隧道后流错误"，返回是否达到冷却门限。
func proxyStreamFailStrike(kind, proxyURL string) bool {
	if proxyURL == "" {
		return false
	}
	key := kind + "\x00" + proxyURL
	now := time.Now()
	proxyStreamFailMu.Lock()
	defer proxyStreamFailMu.Unlock()
	kept := make([]time.Time, 0, proxyStreamFailThreshold+1)
	for _, t := range proxyStreamFails[key] {
		if now.Sub(t) <= proxyStreamFailWindow {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	if len(kept) >= proxyStreamFailThreshold {
		delete(proxyStreamFails, key) // 触发即清零，避免窗口内反复冷却
		return true
	}
	proxyStreamFails[key] = kept
	return false
}

// shouldCooldownExit err 是否应当让该出口退出轮转，以及**冷却的作用范围**。
// 返回 (scope, ok)：scope = "global"（建连失败，出口对谁都死）或
// "platform"（隧道后流错误达成 2-strike，大概率是目标封了出口 IP）；
// ok=false = 不冷却（上游响应类错误、未达门限的隧道后流错误）。
func shouldCooldownExit(kind, proxyURL string, err error) (string, bool) {
	if err == nil || !isTransportLayerFailure(err) {
		return "", false
	}
	if isUpstreamSideStreamFailure(err) {
		if proxyStreamFailStrike(kind, proxyURL) {
			return "platform", true
		}
		return "", false
	}
	return "global", true
}

func isTransportLayerFailure(err error) bool {
	if err == nil {
		return false
	}
	var te *trae.HTTPError
	if errors.As(err, &te) {
		return false
	}
	var ae *amd.HTTPError
	if errors.As(err, &ae) {
		return false
	}
	var oe *openrouter.HTTPError
	if errors.As(err, &oe) {
		return false
	}
	var th *tokenharbor.HTTPError
	if errors.As(err, &th) {
		return false
	}
	var ot *other.HTTPError
	if errors.As(err, &ot) {
		return false
	}
	var ce *cline.StatusError
	if errors.As(err, &ce) {
		return false
	}
	return true
}

// proxyHealthEntry 单个代理的累计 + 最近窗口记账。
type proxyHealthEntry struct {
	Attempts  int64     `json:"attempts"`
	Successes int64     `json:"successes"`
	Recent    []bool    `json:"recent"` // 最近 N 次结果，旧→新
	LastOK    time.Time `json:"lastOk,omitempty"`
	LastFail  time.Time `json:"lastFail,omitempty"`
	LastErr   string    `json:"lastErr,omitempty"`
	// HSCount/HSSumMs 握手延迟累计（仅成功尝试）：Do 返回时刻减调用时刻，
	// 口径=TCP+TLS+CONNECT 隧道建立耗时，不含上游首字节/任务时长。
	// 失败尝试不记——超时熔断的秒级耗时会污染平均值，失败已有在线率覆盖。
	HSCount int64 `json:"hsCount,omitempty"`
	HSSumMs int64 `json:"hsSumMs,omitempty"`
}

func (e *proxyHealthEntry) recentOK() int {
	n := 0
	for _, ok := range e.Recent {
		if ok {
			n++
		}
	}
	return n
}

var (
	proxyHealthMu    sync.Mutex
	proxyHealthStats = map[string]*proxyHealthEntry{}
	proxyHealthDirty bool
	proxyHealthOnce  sync.Once
)

type proxyHealthFile struct {
	SavedAt time.Time                    `json:"savedAt"`
	Proxies map[string]*proxyHealthEntry `json:"proxies"`
}

// recordProxyOutcome 记一次出口成败。kind 为平台标识（cline/zen/amd/or/th/
// other/trae/wb，见各调用点），决定平台级冷却的作用范围。proxyURL 为空 = 直连
// 出口，不是代理，不记。err 非空且源自 context 取消/截止（客户端放弃）时同样
// 不记 —— 与现有冷却判据的"ctx 取消不冷却"口径一致。
// hsMs 握手耗时（毫秒）：调用方 Do 前后 time.Since；仅 ok=true 时记入平均，
// 失败/直连/取消路径传 0 即可。
func recordProxyOutcome(kind, proxyURL string, ok bool, err error, hsMs int64) {
	if proxyURL == "" {
		return
	}
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return
	}
	// 传输层失败：按归因决定冷却范围（2026-10-30 平台粒度）。
	//   - 建连阶段失败 → 全局冷却：出口本身不可用，对谁都死；
	//   - 隧道后流错误达成 2-strike → 只冷却失败发生的平台：大概率是目标封了
	//     出口 IP（如 codebuddy 封 Oracle 段），绑同一出口的其它平台无感。
	// cline/zen 在各自请求循环里已调用，此处重复命中是幂等的。
	if !ok {
		if scope, cool := shouldCooldownExit(kind, proxyURL, err); cool {
			if idx := proxyIdxInPool(proxyURL); idx >= 0 {
				if scope == "global" {
					cooldownUpstreamProxy(idx, proxyTransportCooldown)
				} else {
					cooldownUpstreamProxyFor(kind, idx, proxyPlatformCooldown)
				}
			}
		}
	}
	ensureProxyHealthLoaded()

	proxyHealthMu.Lock()
	e := proxyHealthStats[proxyURL]
	if e == nil {
		e = &proxyHealthEntry{}
		proxyHealthStats[proxyURL] = e
	}
	e.Attempts++
	if ok {
		e.Successes++
		e.LastOK = time.Now()
		if hsMs > 0 {
			e.HSCount++
			e.HSSumMs += hsMs
		}
	} else {
		e.LastFail = time.Now()
		if err != nil {
			e.LastErr = kit.Truncate(err.Error(), 300)
		}
	}
	e.Recent = append(e.Recent, ok)
	if len(e.Recent) > proxyHealthRecentN {
		e.Recent = e.Recent[len(e.Recent)-proxyHealthRecentN:]
	}
	proxyHealthDirty = true
	proxyHealthMu.Unlock()
}

// resetProxyHealthStats 代理列表变动时整体清空统计（用户决策：编辑即清零，
// 不做键迁移）。同步落盘空文件，避免崩溃后旧统计复活。
func resetProxyHealthStats() {
	ensureProxyHealthLoaded()
	proxyHealthMu.Lock()
	proxyHealthStats = map[string]*proxyHealthEntry{}
	proxyHealthDirty = true
	proxyHealthMu.Unlock()
	flushProxyHealth()
}

func ensureProxyHealthLoaded() {
	proxyHealthOnce.Do(func() {
		loadProxyHealth()
		go proxyHealthFlushLoop()
	})
}

func loadProxyHealth() {
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	stats := f.Health
	proxyFileMu.Unlock()
	if stats == nil {
		return
	}
	for _, e := range stats {
		if e == nil {
			continue
		}
		if len(e.Recent) > proxyHealthRecentN {
			e.Recent = e.Recent[len(e.Recent)-proxyHealthRecentN:]
		}
	}
	proxyHealthStats = stats
	log.Printf("proxy health: restored %d proxy stat(s) from %s", len(proxyHealthStats), proxyConfigPath())
}

func proxyHealthFlushLoop() {
	ticker := time.NewTicker(proxyHealthFlushInterval)
	for range ticker.C {
		flushProxyHealth()
	}
}

// flushProxyHealth 脏标记落盘；无变化时是 no-op。调用方可在锁外并发调用。
func flushProxyHealth() {
	proxyHealthMu.Lock()
	if !proxyHealthDirty {
		proxyHealthMu.Unlock()
		return
	}
	out := proxyHealthFile{
		SavedAt: time.Now(),
		Proxies: make(map[string]*proxyHealthEntry, len(proxyHealthStats)),
	}
	for k, e := range proxyHealthStats {
		cp := *e
		out.Proxies[k] = &cp
	}
	proxyHealthDirty = false
	proxyHealthMu.Unlock()

	// 只覆盖 proxy-config.json 的 health 段，出口池与绑定段原样保留。
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.HealthSavedAt = out.SavedAt
		f.Health = out.Proxies
	})
}

// proxyHealthSnapshot 面板用的每代理统计表。以当前代理池为基准逐行输出
// （没被用过/没数据的代理也在列，读数为 null），已删除代理的孤儿条目不出现。
// URL 一律打码（凭据 → ***），别名保留在 fragment 里由前端 egLabel 提取。
func proxyHealthSnapshot() []map[string]any {
	ensureProxyHealthLoaded()
	cfg := getProxyPool()
	cooldowns := proxyCooldownStatus()

	proxyHealthMu.Lock()
	defer proxyHealthMu.Unlock()

	out := make([]map[string]any, 0, len(cfg.Proxies))
	for _, raw := range cfg.Proxies {
		row := map[string]any{
			"proxy":   maskProxyURL(raw),
			"cooling": false,
		}
		if until, cooling := cooldowns[raw]; cooling {
			row["cooling"] = true
			row["coolingUntil"] = until
		}
		// 平台级冷却（kind -> 截止）：随行输出，前端把在线率徽章换成 ∞。
		if idx := proxyIdxInPool(raw); idx >= 0 {
			now := time.Now()
			proxyCooldownsMu.Lock()
			var pcList []map[string]any
			for kind, m := range proxyPlatformCooldowns {
				if until, ok := m[idx]; ok && now.Before(until) {
					pcList = append(pcList, map[string]any{"kind": kind, "until": until.UTC().Format(time.RFC3339)})
				}
			}
			proxyCooldownsMu.Unlock()
			if len(pcList) > 0 {
				row["platformCooling"] = pcList
			}
		}
		if e := proxyHealthStats[raw]; e != nil {
			row["attempts"] = e.Attempts
			row["successes"] = e.Successes
			if e.Attempts > 0 {
				row["rate"] = float64(e.Successes) / float64(e.Attempts)
			}
			if e.HSCount > 0 {
				row["avgHsMs"] = float64(e.HSSumMs) / float64(e.HSCount)
				row["hsCount"] = e.HSCount
			}
			row["recentTotal"] = len(e.Recent)
			row["recentOK"] = e.recentOK()
			if len(e.Recent) > 0 {
				row["recentRate"] = float64(e.recentOK()) / float64(len(e.Recent))
			}
			if !e.LastOK.IsZero() {
				row["lastOk"] = e.LastOK.UTC().Format(time.RFC3339)
			}
			if !e.LastFail.IsZero() {
				row["lastFail"] = e.LastFail.UTC().Format(time.RFC3339)
			}
			row["lastErr"] = e.LastErr
		}
		out = append(out, row)
	}
	return out
}

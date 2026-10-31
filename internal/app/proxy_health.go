package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
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
// 落盘独立的 proxy-state.json（2026-10-31 从 proxy-config.json 的 health 段
// 拆出——它是随请求高频变化的使用痕迹，留在配置文件里会干扰外部对配置变更
// 的监测）：内存记账 + 脏标记 + 60s 周期原子落盘，重启续算；首次加载时若
// 新文件缺失则从旧 proxy-config.json 的 health 段一次性迁移。
// ============================================================================

const (
	// proxyHealthRecentN 最近窗口大小：最近 N 次尝试的成败环形样本。
	proxyHealthRecentN = 50
	// proxyHealthFlushInterval 落盘周期。进程被 kill 时最多丢这个窗口的数据。
	proxyHealthFlushInterval = 60 * time.Second
	// proxyTransportCooldown 传输层失败后该出口退出轮转的时长（**全局层**：
	// 建连阶段失败 60s 窗口 2-strike 后触发，2026-10-31 用户定——单次网络
	// 抖动（如唯一凭据的一次拨号失败）不再把出口对全部平台冷却；短冷却能跳过
	// 真死节点又快速自愈，长冷却会让几次慢请求毒化整个池。
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
// `proxyconnect tcp: …`、`socks5 …` —— 都发生在建连阶段。
//
// TLS 归平台级（2026-10-31 用户定）：端到端 TLS 握手发生在 CONNECT/SOCKS
// 隧道建立**之后**——`net/http: TLS handshake timeout`、`tls: handshake
// failure` 等说明隧道本身是通的，超时多半是链路中段或目标侧抖动，不是出口
// 死了，不该全局冷却。
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
	// 即使 cause 里恰好带着 EOF（例如 `proxyconnect tcp: EOF`）。注意 TLS
	// 不在此列——见上方注释，端到端 TLS 错误归平台级。
	for _, s := range []string{
		"dial tcp", "dial unix", "proxyconnect", "socks", "no such host",
		"connect:", "connection refused", "network is unreachable",
		"no route to host", "i/o timeout", "context deadline exceeded",
	} {
		if strings.Contains(cause, s) {
			return false
		}
	}
	for _, s := range []string{
		"EOF", "connection reset by peer", "broken pipe",
		"http: server closed idle connection", "server closed the connection", "stream closed",
		"tls", "TLS", "handshake",
	} {
		if strings.Contains(cause, s) {
			return true
		}
	}
	return false
}

// ============================================================================
// 失败 strike 计数（2026-10-31 重构）：
//
//   - 计数键 failKey(kind, proxyURL)：平台层 kind 为平台标识；全局层 kind 为
//     空串——全局失败与平台失败天然不互相凑数。
//   - **只在 recordProxyOutcome 一处记账**（recordExitFailure）。cline/zen 的
//     请求循环里也调 shouldCooldownExit，但那是**纯查询**（算上本次是否达
//     阈值），不写计数——否则同一次失败会被循环 + recordProxyOutcome 记两次，
//     2-strike 退化为 1-strike（2026-10-31 修复的存量 bug）。
//   - 全局层（建连失败）与平台层（隧道后流错误）都是窗口内 2-strike 触发
//     （2026-10-31 用户定）：单次网络抖动不再把唯一可用出口全局冷却 2 分钟。
// ============================================================================
const (
	proxyStreamFailWindow    = 60 * time.Second
	proxyStreamFailThreshold = 2
)

var (
	proxyStreamFailMu sync.Mutex
	proxyStreamFails  = map[string][]time.Time{}
)

// failKey strike 计数键：kind 为空 = 全局层。
func failKey(kind, proxyURL string) string { return kind + "\x00" + proxyURL }

// proxyFailStrike 记一次失败，返回是否达到冷却门限（达到即清零，避免窗口内
// 反复冷却）。只在 recordExitFailure 中调用——记账单点，防双计。
func proxyFailStrike(key string) bool {
	now := time.Now()
	proxyStreamFailMu.Lock()
	defer proxyStreamFailMu.Unlock()
	return proxyFailStrikeLocked(key, now)
}

// proxyFailStrikeLocked 已持锁的记录+判定。
func proxyFailStrikeLocked(key string, now time.Time) bool {
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

// proxyFailStrikePending 非变异查询：算上"本次"失败是否达到门限。供请求
// 循环里先于 recordProxyOutcome 的判定使用（只判不记，见 failKey 注释）。
func proxyFailStrikePending(key string) bool {
	now := time.Now()
	proxyStreamFailMu.Lock()
	defer proxyStreamFailMu.Unlock()
	n := 0
	for _, t := range proxyStreamFails[key] {
		if now.Sub(t) <= proxyStreamFailWindow {
			n++
		}
	}
	return n+1 >= proxyStreamFailThreshold
}

// clearFailStrikes 清掉某出口的全部失败计数（全局 + 各平台）。手动解禁用。
func clearFailStrikes(proxyURL string) {
	suffix := "\x00" + proxyURL
	proxyStreamFailMu.Lock()
	defer proxyStreamFailMu.Unlock()
	for k := range proxyStreamFails {
		if strings.HasSuffix(k, suffix) {
			delete(proxyStreamFails, k)
		}
	}
}

// recordExitFailure 记一次出口失败（唯一记账点），达到门限时返回冷却作用域
// （"global"/"platform"），否则空串。
func recordExitFailure(kind, proxyURL string, err error) string {
	if err == nil || !isTransportLayerFailure(err) {
		return ""
	}
	if isUpstreamSideStreamFailure(err) {
		if proxyFailStrike(failKey(kind, proxyURL)) {
			return "platform"
		}
		return ""
	}
	if proxyFailStrike(failKey("", proxyURL)) {
		return "global"
	}
	return ""
}

// shouldCooldownExit err 是否应当让该出口退出轮转，以及**冷却的作用范围**。
// 返回 (scope, ok)：scope = "global"（建连失败 2-strike，出口对谁都死）或
// "platform"（隧道后流错误达成 2-strike，大概率是目标封了出口 IP）；
// ok=false = 不冷却（上游响应类错误、未达门限的失败）。
//
// **本函数不写任何计数**：cline/zen 请求循环用它做即时判定（本次算第 N 次，
// 随后的 recordProxyOutcome 才真正记账并触发冷却），二者对同一次失败的结论
// 一致，冷却动作幂等。
func shouldCooldownExit(kind, proxyURL string, err error) (string, bool) {
	if err == nil || !isTransportLayerFailure(err) {
		return "", false
	}
	if isUpstreamSideStreamFailure(err) {
		if proxyFailStrikePending(failKey(kind, proxyURL)) {
			return "platform", true
		}
		return "", false
	}
	if proxyFailStrikePending(failKey("", proxyURL)) {
		return "global", true
	}
	return "", false
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
	// proxyHealthFileMu 串行化对 proxy-state.json 的写（flush 循环与
	// resetProxyHealthStats 的同步 flush 可能并发进入写文件段）。
	proxyHealthFileMu sync.Mutex
)

type proxyHealthFile struct {
	SavedAt time.Time                    `json:"savedAt"`
	Proxies map[string]*proxyHealthEntry `json:"proxies"`
}

// proxyHealthPath 独立健康统计文件（使用痕迹，不进配置监测集合）。
func proxyHealthPath() string { return kit.ResolveDataPath("proxy-state.json") }
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
	// 传输层失败：按归因决定冷却范围（2026-10-30 平台粒度；2026-10-31 全局层
	// 也 2-strike）。strike 记账只在这一处发生——cline/zen 请求循环的
	// shouldCooldownExit 是纯查询，同一次失败不会被记两次。
	//   - 建连阶段失败 2-strike → 全局冷却：出口本身不可用，对谁都死；
	//   - 隧道后流错误达成 2-strike → 只冷却失败发生的平台：大概率是目标封了
	//     出口 IP（如 codebuddy 封 Oracle 段），绑同一出口的其它平台无感。
	if !ok {
		if scope := recordExitFailure(kind, proxyURL, err); scope != "" {
			if idx := proxyIdxInPool(proxyURL); idx >= 0 {
				if scope == "global" {
					cooldownUpstreamProxy(idx, proxyTransportCooldown)
					log.Printf("[exit-cooldown] %s exit %s cooled %v globally (transport failure: %v)",
						kind, kit.MaskProxyURL(proxyURL), proxyTransportCooldown, err)
				} else {
					cooldownUpstreamProxyFor(kind, idx, proxyPlatformCooldown)
					log.Printf("[exit-cooldown] %s exit %s cooled %v platform-scoped (upstream-side stream error: %v)",
						kind, kit.MaskProxyURL(proxyURL), proxyPlatformCooldown, err)
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

// proxyHealthBackupSnapshot 统计快照（配置备份 state 段，2026-11-01）。
func proxyHealthBackupSnapshot() *proxyHealthFile {
	ensureProxyHealthLoaded()
	proxyHealthMu.Lock()
	out := proxyHealthFile{
		SavedAt: time.Now(),
		Proxies: make(map[string]*proxyHealthEntry, len(proxyHealthStats)),
	}
	for k, e := range proxyHealthStats {
		if e == nil {
			continue
		}
		cp := *e
		out.Proxies[k] = &cp
	}
	proxyHealthMu.Unlock()
	return &out
}

// importProxyHealthBackup 统计导入（配置备份还原）：整体替换真身并立即落盘。
func importProxyHealthBackup(f *proxyHealthFile) {
	ensureProxyHealthLoaded()
	proxyHealthMu.Lock()
	next := make(map[string]*proxyHealthEntry, len(f.Proxies))
	for k, e := range f.Proxies {
		if e == nil {
			continue
		}
		cp := *e
		next[k] = &cp
	}
	proxyHealthStats = next
	proxyHealthDirty = true
	proxyHealthMu.Unlock()
	flushProxyHealth()
}
func loadProxyHealth() {
	path := proxyHealthPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("proxy health: read %s: %v (starting empty)", path, err)
			return
		}
	}
	var f proxyHealthFile
	if err := json.Unmarshal(raw, &f); err != nil {
		// 坏文件防护与各 config 文件同款：改名留证，空表启动。
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(path, path+".corrupt-"+stamp); renErr == nil {
			log.Printf("proxy health: corrupt JSON at %s; moved to %s.corrupt-%s", path, path, stamp)
		}
		log.Printf("proxy health: parse failed: %v (starting empty)", err)
		return
	}
	absorbProxyHealthStats(f.Proxies, path)
}
// absorbProxyHealthStats 把读出的统计表灌入内存（Recent 截断 + 日志）。
func absorbProxyHealthStats(stats map[string]*proxyHealthEntry, from string) {
	for _, e := range stats {
		if e == nil {
			continue
		}
		if len(e.Recent) > proxyHealthRecentN {
			e.Recent = e.Recent[len(e.Recent)-proxyHealthRecentN:]
		}
	}
	proxyHealthStats = stats
	log.Printf("proxy health: restored %d proxy stat(s) from %s", len(proxyHealthStats), from)
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

	// 直接原子写独立文件，不再经过 proxy-config.json（配置文件保持"仅用户
	// 操作才变"，外部监测配置变更不被健康统计噪声淹没）。
	proxyHealthFileMu.Lock()
	defer proxyHealthFileMu.Unlock()
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Printf("proxy health: marshal: %v", err)
		return
	}
	atomicWriteFile(proxyHealthPath(), raw)
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

// proxyIdxByPanelID 面板健康表行 → 池内索引。健康表里的 proxy 是打码 URL
// （凭据不可回传给前端），所以这里按"打码后相等"匹配；也兼容原始 URL 与
// 备注名（proxyIdxInPool）。找不到返回 -1。
func proxyIdxByPanelID(id string) int {
	if id == "" {
		return -1
	}
	cfg := getProxyPool()
	for i, p := range cfg.Proxies {
		if maskProxyURL(p) == id {
			return i
		}
	}
	return proxyIdxInPool(id)
}

// clearProxyCooldowns 手动解禁某出口：清全局冷却、全部平台的平台级冷却、
// 全部失败 strike 计数。2026-10-31 用户定：单次网络抖动误冷却（尤其出口是
// 唯一可用出口时）应可即时恢复，不必干等冷却到期。
// 入参支持面板健康表的打码 URL、原始 URL 与备注名——strike 计数键是原始
// URL，所以先解析回池内原始条目再清。
func clearProxyCooldowns(id string) string {
	raw := id
	if idx := proxyIdxByPanelID(id); idx >= 0 {
		raw = getProxyPool().Proxies[idx]
		proxyCooldownsMu.Lock()
		delete(proxyCooldowns, idx)
		for _, m := range proxyPlatformCooldowns {
			delete(m, idx)
		}
		proxyCooldownsMu.Unlock()
	}
	clearFailStrikes(raw)
	return raw
}

// handleProxyCooldownSet POST /admin/api/proxypool/cooldown/set
// body: {"proxy": "<打码 URL/原始 URL/备注名>", "kind": "global"|"cline"|"zen"|...（空=global）, "minutes": 10}
// 手动把某出口置入冷却（与 clear 对称）：运维摘除出口、以及面板上制造
// 冷却态验证"解禁"按钮都用它。
func handleProxyCooldownSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Proxy   string `json:"proxy"`
		Kind    string `json:"kind"`
		Minutes int    `json:"minutes"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "bad json: " + err.Error()})
		return
	}
	req.Proxy = strings.TrimSpace(req.Proxy)
	idx := proxyIdxByPanelID(strings.TrimSpace(req.Proxy))
	if idx < 0 {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "proxy not in pool: " + req.Proxy})
		return
	}
	if req.Minutes <= 0 {
		req.Minutes = 10
	}
	d := time.Duration(req.Minutes) * time.Minute
	if d > maxCooldown {
		d = maxCooldown
	}
	raw := getProxyPool().Proxies[idx]
	kind := strings.TrimSpace(req.Kind)
	if kind == "" || kind == "global" {
		cooldownUpstreamProxy(idx, d)
	} else {
		cooldownUpstreamProxyFor(kind, idx, d)
	}
	log.Printf("[exit-cooldown] manual set: exit %s cooled %v (kind=%s)", kit.MaskProxyURL(raw), d, kind)
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Cooldown set",
		Data:    map[string]any{"proxy": maskProxyURL(raw), "kind": kind, "minutes": int(d.Minutes())},
	})
}

// handleProxyCooldownClear POST /admin/api/proxypool/cooldown/clear
// body: {"proxy": "<面板健康表里的打码 URL 或原始 URL/备注名>"}
// 一键清除该出口的全局冷却 + 平台级冷却 + 失败计数。
func handleProxyCooldownClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Proxy string `json:"proxy"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "bad json: " + err.Error()})
		return
	}
	req.Proxy = strings.TrimSpace(req.Proxy)
	if req.Proxy == "" {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "proxy is required"})
		return
	}
	if proxyIdxByPanelID(req.Proxy) < 0 {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "proxy not in pool: " + req.Proxy})
		return
	}
	raw := clearProxyCooldowns(req.Proxy)
	log.Printf("[exit-cooldown] manual unban: exit %s cooldowns and strike counters cleared",
		kit.MaskProxyURL(raw))
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Cooldown cleared",
		Data:    map[string]any{"proxy": maskProxyURL(raw)},
	})
}

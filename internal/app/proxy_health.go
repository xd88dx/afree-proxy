package app

import (
	"context"
	"errors"
	"log"
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
	// proxyTransportCooldown 传输层失败后该出口退出轮转的时长，与 cline/zen
	// 请求循环里的既有口径一致：短冷却能跳过真死节点又快速自愈，长冷却会让
	// 几次慢请求毒化整个池（2026-10-28 统一到所有平台）。
	proxyTransportCooldown = 2 * time.Minute
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

// recordProxyOutcome 记一次出口成败。proxyURL 为空 = 直连出口，不是代理，不记。
// err 非空且源自 context 取消/截止（客户端放弃）时同样不记 —— 与现有冷却
// 判据的"ctx 取消不冷却"口径一致。
// hsMs 握手耗时（毫秒）：调用方 Do 前后 time.Since；仅 ok=true 时记入平均，
// 失败/直连/取消路径传 0 即可。
func recordProxyOutcome(proxyURL string, ok bool, err error, hsMs int64) {
	if proxyURL == "" {
		return
	}
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return
	}
	// 传输层失败：让该出口退出轮转 2 分钟（2026-10-28 统一到全平台）。
	// cline/zen 在各自请求循环里已调用，此处重复命中是幂等的（同为 2 分钟）。
	// 此前其余平台只记账不自愈：坏节点会被轮转反复选中，于是同一个节点故障
	// 被轮流记到多个凭据头上（冷却 / trae 自动封禁），本行补上这个缺口。
	if !ok && isTransportLayerFailure(err) {
		if idx := proxyIdxInPool(proxyURL); idx >= 0 {
			cooldownUpstreamProxy(idx, proxyTransportCooldown)
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

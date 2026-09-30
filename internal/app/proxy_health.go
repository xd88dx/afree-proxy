package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"afree-proxy/internal/kit"
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
// 落盘 .proxy-health.json：内存记账 + 脏标记 + 60s 周期原子落盘，重启续算。
// ============================================================================

const (
	// proxyHealthRecentN 最近窗口大小：最近 N 次尝试的成败环形样本。
	proxyHealthRecentN = 50
	// proxyHealthFlushInterval 落盘周期。进程被 kill 时最多丢这个窗口的数据。
	proxyHealthFlushInterval = 60 * time.Second
)

// proxyHealthEntry 单个代理的累计 + 最近窗口记账。
type proxyHealthEntry struct {
	Attempts  int64     `json:"attempts"`
	Successes int64     `json:"successes"`
	Recent    []bool    `json:"recent"` // 最近 N 次结果，旧→新
	LastOK    time.Time `json:"lastOk,omitempty"`
	LastFail  time.Time `json:"lastFail,omitempty"`
	LastErr   string    `json:"lastErr,omitempty"`
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

func proxyHealthPath() string {
	return kit.ResolveDataPath(".proxy-health.json")
}

// recordProxyOutcome 记一次出口成败。proxyURL 为空 = 直连出口，不是代理，不记。
// err 非空且源自 context 取消/截止（客户端放弃）时同样不记 —— 与现有冷却
// 判据的"ctx 取消不冷却"口径一致。
func recordProxyOutcome(proxyURL string, ok bool, err error) {
	if proxyURL == "" {
		return
	}
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return
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
	raw, err := os.ReadFile(proxyHealthPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("proxy health: read %s: %v", proxyHealthPath(), err)
		}
		return
	}
	var f proxyHealthFile
	if json.Unmarshal(raw, &f) != nil {
		log.Printf("proxy health: corrupt JSON at %s (starting clean)", proxyHealthPath())
		return
	}
	if f.Proxies != nil {
		for _, e := range f.Proxies {
			if e == nil {
				continue
			}
			if len(e.Recent) > proxyHealthRecentN {
				e.Recent = e.Recent[len(e.Recent)-proxyHealthRecentN:]
			}
		}
		proxyHealthStats = f.Proxies
	}
	log.Printf("proxy health: restored %d proxy stat(s) from %s", len(proxyHealthStats), proxyHealthPath())
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

	raw, err := json.Marshal(out)
	if err != nil {
		log.Printf("proxy health: marshal: %v", err)
		return
	}
	path := proxyHealthPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		log.Printf("proxy health: write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("proxy health: replace %s: %v", path, err)
		_ = os.Remove(tmp)
	}
}

// proxyHealthSnapshot 面板用的每代理统计表。以当前代理池为基准逐行输出
// （没被用过/没数据的代理也在列，读数为 null），已删除代理的孤儿条目不出现。
// URL 一律打码（凭据 → ***），别名保留在 fragment 里由前端 egLabel 提取。
func proxyHealthSnapshot() []map[string]any {
	ensureProxyHealthLoaded()
	cfg := getZenConfig()
	cooldowns := zenProxyCooldownStatus()

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

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"afree-proxy/internal/kit"
)

// ============================================================================
// 代理出口延迟测试（面板「代理测试」按钮，2026-11-03 用户定方案一）
//
// 全握手测延迟：每条出口对测速目标（默认 www.gstatic.com/generate_204，面板
// 健康表输入框可改、落盘 proxy-config.json）发 N 次 HTTPS GET（CONNECT/SOCKS
// 隧道 + TLS + 请求），统计成功样本的最小值与中位数。默认目标选 anycast CDN
// 的理由：全球就近接入，读数反映「出口本地链路质量」，出口之间横向可比性最
// 好；204 空响应、零配额、零鉴权（AGENTS.md §10 实测候选代理通 gstatic 全部
// 可达）。
//
// 与真实请求同路径：拨号走 dialViaProxy、TLS 走 buildTransport 的 utls 指纹，
// 读数与真实上游请求出自同一拨号栈——这是"参考价值"的一部分。
//
// 红线（与被动统计口径一致）：**纯测量**——不进 attempts/successes 统计、
// 不记 strike、不触发任何冷却（recordProxyOutcome 一概不调）。测试失败绝不
// 毒化路由；要不要摘出轮转由人看着结果手动「冷却」。
//
// 落盘：随 proxy-state.json 的 tests 段持久化，沿用健康统计「编辑池即清零」
// 语义（resetProxyHealthStats 一并清空）；批量完成后立即落盘，60s 周期落盘
// 复用既有 flush 循环。
// ============================================================================

const (
	// proxyTestTargetDefault 默认测速目标。
	proxyTestTargetDefault = "https://www.gstatic.com/generate_204"
	// proxyTestSamples 每出口采样次数：3 次取最小值滤掉单次抖动（ping 惯例），
	// 中位数一并给出供参考。
	proxyTestSamples = 3
	// proxyTestTimeout 单次采样超时（覆盖拨号+隧道+TLS+往返全程）。
	proxyTestTimeout = 5 * time.Second
	// proxyTestSampleGap 同出口两次采样间隔，避免背靠背互相污染读数。
	proxyTestSampleGap = 200 * time.Millisecond
	// proxyTestConcurrency 并发出口数：典型 30 条池子最坏 ~3 波 × 15s 内跑完。
	proxyTestConcurrency = 8
	// proxyTestOverall 单次批量测试的总兜底超时。
	proxyTestOverall = 120 * time.Second
	// proxyTestSingleOverall 行内单出口测试的总兜底：3 样本最坏 3×5s+2×0.2s
	// ≈ 16s，留一倍余量。
	proxyTestSingleOverall = 30 * time.Second
)

// proxyTestEntry 单条出口最近一次批量测试的读数。
type proxyTestEntry struct {
	At       time.Time `json:"at"`
	Ok       bool      `json:"ok"`
	MinMs    int64     `json:"minMs,omitempty"`
	MedianMs int64     `json:"medianMs,omitempty"`
	Samples  int       `json:"samples"` // 成功样本数（0..N，ok=false 恒为 0）
	Err      string    `json:"err,omitempty"`
}

var (
	proxyTestMu    sync.Mutex
	proxyTestStats = map[string]*proxyTestEntry{} // 键 = 池内原始 URL
	// proxyTestRunning 防重入：一批没跑完时拒绝再次触发（前端按钮也会禁用，
	// 这里兜底多标签页/脚本直调）。
	proxyTestRunning atomic.Bool
)

// proxyTestTarget 生效的测速目标：池配置里有就用池里的（面板输入框写的），
// 否则回默认。字符串不改任何内容——保存时已校验过 http(s) 形态。
func proxyTestTarget() string {
	proxyPoolMu.Lock()
	t := proxyPool.TestTarget
	proxyPoolMu.Unlock()
	if t = strings.TrimSpace(t); t != "" {
		return t
	}
	return proxyTestTargetDefault
}

// normalizeProxyTestTarget 校验面板提交的测速目标：空 = 回默认；否则必须是
// 可解析的 http(s) URL（ host 非空）。返回入库形态（trim 后原样）。
func normalizeProxyTestTarget(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "", nil
	}
	u, err := url.Parse(t)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("must be an http(s) URL (e.g. %s)", proxyTestTargetDefault)
	}
	return t, nil
}

// probeOnce 单次采样：新建临时 transport（禁 keep-alive，保证每次都是完整
// 握手而非连接复用），经代理 GET 测速目标。延迟口径 = 响应头到达（Do 返回
// 时刻），与真实流量 recordProxyOutcome 的 hsMs 同口径；收到任意 HTTP 响应
// 都算样本（自定义目标可能 401/404——隧道和 TLS 已走通，读数有效），不做
// 状态码过滤；响应体直接丢弃（测延迟不需要内容，也避免大页面拖慢采样轮）。
func probeOnce(ctx context.Context, raw string) (int64, error) {
	transport := buildTransport(func(c context.Context, network, addr string) (net.Conn, error) {
		return dialViaProxy(c, raw, network, addr)
	})
	transport.DisableKeepAlives = true
	transport.ResponseHeaderTimeout = proxyTestTimeout
	transport.TLSHandshakeTimeout = proxyTestTimeout
	client := &http.Client{Transport: transport, Timeout: proxyTestTimeout}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyTestTarget(), nil)
	if err != nil {
		return 0, err
	}
	t0 := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	d := time.Since(t0).Milliseconds()
	resp.Body.Close()
	return d, nil
}

// recordProxyTestOutcome 手动测试样本计入面板统计（2026-11-03 用户定：测试
// 数据加入在线率/最近在线率/平均握手延迟）。与 recordProxyOutcome 写同一份
// proxyHealthStats（Recent 窗口、LastOK/LastFail、HS 均值口径同为 Do 返回
// 时刻），但**没有 strike 计数与冷却**——测试失败只反映在统计里，绝不摘出
// 口（摘不摘由人看着结果手动「冷却」）。批量 ctx 中止的样本在调用方就被
// 丢弃，不会到达这里。
func recordProxyTestOutcome(proxyURL string, ok bool, err error, elapsedMs int64) {
	if proxyURL == "" {
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
		if elapsedMs > 0 {
			e.HSCount++
			e.HSSumMs += elapsedMs
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

// probeProxyLatency 对单条出口采 proxyTestSamples 次。全部失败 → ok=false
// 带最后一次错误；部分失败不报错（samples 数即成功数，读数只来自成功样本——
// 失败样本的秒级超时若混入统计会污染最小值）。每个样本计入面板统计
// （recordProxyTestOutcome，2026-11-03 用户定），但绝不触发冷却。
func probeProxyLatency(ctx context.Context, raw string) *proxyTestEntry {
	var ms []int64
	var lastErr string
	for i := 0; i < proxyTestSamples; i++ {
		if ctx.Err() != nil {
			break
		}
		if i > 0 {
			select {
			case <-time.After(proxyTestSampleGap):
			case <-ctx.Done():
			}
		}
		d, err := probeOnce(ctx, raw)
		if ctx.Err() != nil {
			// 批次被中止（总兜底超时）：与"客户端放弃不计"口径一致，
			// 该样本不计入统计。
			break
		}
		recordProxyTestOutcome(raw, err == nil, err, d)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		ms = append(ms, d)
	}
	ent := &proxyTestEntry{Samples: len(ms)}
	if len(ms) == 0 {
		if lastErr == "" {
			lastErr = "aborted"
		}
		ent.Err = kit.Truncate(lastErr, 200)
		return ent
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
	ent.Ok = true
	ent.MinMs = ms[0]
	ent.MedianMs = ms[len(ms)/2]
	return ent
}

// proxyTestRow 面板响应行构造（批量与单测共用），proxy 与健康表同键（打码）。
func proxyTestRow(raw string, ent *proxyTestEntry) map[string]any {
	return map[string]any{
		"proxy":    maskProxyURL(raw),
		"ok":       ent.Ok,
		"minMs":    ent.MinMs,
		"medianMs": ent.MedianMs,
		"samples":  ent.Samples,
		"err":      ent.Err,
		"at":       ent.At.UTC().Format(time.RFC3339),
	}
}

// runProxyLatencyBatch 对当前池内全部出口做一轮延迟测试（并发
// proxyTestConcurrency、单样本 proxyTestTimeout、总兜底 proxyTestOverall），
// 结果按池内顺序写入 proxyTestStats 并立即落盘，返回面板行（URL 打码）。
// 纯测量：不触碰冷却/统计（见文件头红线）。
func runProxyLatencyBatch(ctx context.Context) []map[string]any {
	cfg := getProxyPool()
	type result struct {
		idx int
		ent *proxyTestEntry
	}
	results := make(chan result, len(cfg.Proxies))
	sem := make(chan struct{}, proxyTestConcurrency)
	var wg sync.WaitGroup
	start := time.Now()
	for idx, raw := range cfg.Proxies {
		wg.Add(1)
		go func(idx int, raw string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- result{idx, &proxyTestEntry{Err: "aborted"}}
				return
			}
			results <- result{idx, probeProxyLatency(ctx, raw)}
		}(idx, raw)
	}
	wg.Wait()
	close(results)

	byIdx := make(map[int]*proxyTestEntry, len(cfg.Proxies))
	for res := range results {
		byIdx[res.idx] = res.ent
	}
	now := time.Now()
	proxyTestMu.Lock()
	rows := make([]map[string]any, 0, len(cfg.Proxies))
	for idx, raw := range cfg.Proxies {
		ent := byIdx[idx]
		if ent == nil {
			ent = &proxyTestEntry{Err: "aborted"}
		}
		ent.At = now
		proxyTestStats[raw] = ent
		rows = append(rows, proxyTestRow(raw, ent))
	}
	proxyTestMu.Unlock()
	proxyHealthMu.Lock()
	proxyHealthDirty = true
	proxyHealthMu.Unlock()
	flushProxyHealth()
	log.Printf("[proxy-test] batch done: %d exit(s) in %v", len(rows), time.Since(start).Round(time.Millisecond))
	return rows
}

// handleProxyPoolLatencyTest POST /admin/api/proxypool/test
// 无请求体参数（面板语义固定为「测全部池内出口」）。并发批次期间重复触发
// 返回 409。响应 data.rows 的 proxy 与健康表同键（打码 URL），前端按键合并。
// 入参可选：body {"proxy": "<健康表打码 URL>"} 带了就只测这一条（行内按钮，
// 2026-11-03），不带测全池（表头按钮）。两种模式共用 proxyTestRunning 防重入。
func handleProxyPoolLatencyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	if !proxyTestRunning.CompareAndSwap(false, true) {
		writeAPI(w, http.StatusConflict, apiResponse{Error: "latency test already running"})
		return
	}
	defer proxyTestRunning.Store(false)
	ensureProxyHealthLoaded()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		Proxy string `json:"proxy"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeAPI(w, http.StatusBadRequest, apiResponse{Error: "bad json: " + err.Error()})
			return
		}
	}
	// ---- 单出口模式（行内按钮）----
	if proxy := strings.TrimSpace(req.Proxy); proxy != "" {
		idx := proxyIdxByPanelID(proxy)
		if idx < 0 {
			writeAPI(w, http.StatusNotFound, apiResponse{Error: "proxy not in pool: " + proxy})
			return
		}
		raw := getProxyPool().Proxies[idx]
		ctx, cancel := context.WithTimeout(context.Background(), proxyTestSingleOverall)
		defer cancel()
		ent := probeProxyLatency(ctx, raw)
		now := time.Now()
		ent.At = now
		proxyTestMu.Lock()
		proxyTestStats[raw] = ent
		proxyTestMu.Unlock()
		proxyHealthMu.Lock()
		proxyHealthDirty = true
		proxyHealthMu.Unlock()
		flushProxyHealth()
		log.Printf("[proxy-test] single done: %s ok=%v minMs=%d samples=%d err=%q",
			kit.MaskProxyURL(raw), ent.Ok, ent.MinMs, ent.Samples, ent.Err)
		writeAPI(w, http.StatusOK, apiResponse{
			Success: true,
			Message: "Latency test finished",
			Data:    map[string]any{"rows": []map[string]any{proxyTestRow(raw, ent)}},
		})
		return
	}
	// ---- 全池模式（表头按钮）----
	if len(getProxyPool().Proxies) == 0 {
		writeAPI(w, http.StatusOK, apiResponse{
			Success: true, Message: "Pool is empty", Data: map[string]any{"rows": []any{}},
		})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), proxyTestOverall)
	defer cancel()
	rows := runProxyLatencyBatch(ctx)
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Latency test finished",
		Data:    map[string]any{"rows": rows},
	})
}

// handleProxyTestTargetSet POST /admin/api/proxypool/test-target
// body: {"target": "<http(s) URL>"}（空串 = 恢复默认）。回车/失焦即存，落盘
// proxy-config.json 的出口池段。响应 data.target 回显生效值（空输入回显默认）。
func handleProxyTestTargetSet(w http.ResponseWriter, r *http.Request) {
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
		Target string `json:"target"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "bad json: " + err.Error()})
		return
	}
	target, err := normalizeProxyTestTarget(req.Target)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	setProxyTestTarget(target)
	effective := proxyTestTarget()
	log.Printf("[proxy-test] target set to %s (raw=%q)", effective, req.Target)
	writeAPI(w, http.StatusOK, apiResponse{
		Success: true,
		Message: "Test target saved",
		Data:    map[string]any{"target": effective},
	})
}

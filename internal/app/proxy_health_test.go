package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"trae2api"

	"afree-proxy/internal/cline"
	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/other"
)

// 代理在线率统计（纯被动口径）的行为回归：
//   - 只记真实出口成败；直连不记，ctx 取消/截止不记任何一端；
//   - 最近窗口裁剪到 exitpool.RecentN；
//   - 编辑代理池（proxiesChanged）整体清零并同步落盘；
//   - 落盘文件重启续算；
//   - 快照按当前代理池输出，URL 打码、孤儿条目不出现。

func resetProxyHealthForTest(t *testing.T) {
	t.Helper()
	// SetHealthStatsForTest/SetHealthDirtyForTest 出包后各自内部加锁，
	// 不能再在外面套 HealthMu（非重入，会自死锁）。
	exitpool.SetHealthStatsForTest(map[string]*exitpool.HealthEntry{})
	exitpool.SetHealthDirtyForTest(false)
	t.Cleanup(func() {
		exitpool.SetHealthStatsForTest(map[string]*exitpool.HealthEntry{})
		exitpool.SetHealthDirtyForTest(false)
	})
}

func TestProxyHealthRecordSkipsDirectAndAborts(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"socks5://u:p@10.0.0.1:1080#a"}})
	resetProxyHealthForTest(t)

	px := "socks5://u:p@10.0.0.1:1080#a"
	recordProxyOutcome(pkZen, px, true, nil, 150)
	recordProxyOutcome(pkZen, "", true, nil, 0)                              // 直连：不是代理，不记
	recordProxyOutcome(pkZen, px, true, context.Canceled, 0)                 // 客户端取消：不计成功
	recordProxyOutcome(pkZen, px, false, context.DeadlineExceeded, 0)        // 客户端截止：不计失败
	recordProxyOutcome(pkZen, px, false, errors.New("dial tcp: refused"), 0) // 真失败

	e := exitpool.HealthStatsForTest()[px]
	if e == nil || e.Attempts != 2 || e.Successes != 1 {
		t.Fatalf("attempts/successes = %d/%d, want 2/1", e.Attempts, e.Successes)
	}
	if e.HSCount != 1 || e.HSSumMs != 150 {
		t.Fatalf("hsCount/hsSumMs = %d/%d, want 1/150", e.HSCount, e.HSSumMs)
	}
	if !strings.Contains(e.LastErr, "refused") {
		t.Fatalf("lastErr = %q", e.LastErr)
	}
	if len(e.Recent) != 2 || e.Recent[0] != true || e.Recent[1] != false {
		t.Fatalf("recent = %v", e.Recent)
	}
}

func TestProxyHealthRecentWindowTrims(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"socks5://w:1"}})
	resetProxyHealthForTest(t)

	px := "socks5://w:1"
	// 前 exitpool.RecentN 次全失败，再补 10 次成功：窗口内应只剩这 10 次成功。
	for i := 0; i < exitpool.RecentN; i++ {
		recordProxyOutcome(pkZen, px, false, errors.New("dead"), 0)
	}
	for i := 0; i < 10; i++ {
		recordProxyOutcome(pkZen, px, true, nil, 100)
	}

	e := exitpool.HealthStatsForTest()[px]
	if len(e.Recent) != exitpool.RecentN {
		t.Fatalf("window size = %d, want %d", len(e.Recent), exitpool.RecentN)
	}
	if got := e.RecentOK(); got != 10 {
		t.Fatalf("recentOK = %d, want 10", got)
	}
	if e.Attempts != exitpool.RecentN+10 || e.Successes != 10 {
		t.Fatalf("cumulative %d/%d, want %d/10", e.Attempts, e.Successes, exitpool.RecentN+10)
	}
}

func TestProxyHealthSnapshotMasksAndFilters(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"http://user:secret@10.0.0.9:8080#home"}})
	resetProxyHealthForTest(t)

	recordProxyOutcome(pkZen, "http://user:secret@10.0.0.9:8080#home", true, nil, 0)
	recordProxyOutcome(pkZen, "http://ghost:1", true, nil, 0) // 已不在池中的孤儿条目

	rows := proxyHealthSnapshot()
	if len(rows) != 1 {
		t.Fatalf("snapshot rows = %d, want 1 (orphans filtered by current pool)", len(rows))
	}
	proxy := rows[0]["proxy"].(string)
	if strings.Contains(proxy, "secret") {
		t.Fatalf("snapshot must mask credentials, got %q", proxy)
	}
	// maskProxyURL 把 *** 转义为 %2A（既有行为）；别名 fragment 与 host:port 必须保留
	if !strings.Contains(proxy, "10.0.0.9:8080") || !strings.Contains(proxy, "#home") {
		t.Fatalf("masked proxy = %q, want host:port and alias fragment kept", proxy)
	}
	if rows[0]["attempts"].(int64) != 1 || rows[0]["successes"].(int64) != 1 {
		t.Fatalf("attempts/successes = %v/%v", rows[0]["attempts"], rows[0]["successes"])
	}
}

func TestProxyHealthSnapshotListsUnusedProxiesWithoutData(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"socks5://used:1", "socks5://idle:1"}})
	resetProxyHealthForTest(t)
	recordProxyOutcome(pkZen, "socks5://used:1", true, nil, 0)

	rows := proxyHealthSnapshot()
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r["proxy"] == "socks5://idle:1" { // 无凭据 → 打码后原样
			if _, has := r["attempts"]; has {
				t.Fatalf("unused proxy must have no data, got %v", r)
			}
		}
	}
}

func TestProxyHealthResetOnPoolEdit(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"socks5://old:1"}})
	resetProxyHealthForTest(t)

	recordProxyOutcome(pkZen, "socks5://old:1", false, errors.New("boom"), 0)

	req := httptest.NewRequest("POST", "/admin/api/opencode/config/update",
		strings.NewReader(`{"proxies":["socks5://new:1"]}`))
	rec := httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("pool edit failed: %d %s", rec.Code, rec.Body.String())
	}

	rows := proxyHealthSnapshot()
	if len(rows) != 1 || rows[0]["proxy"] != "socks5://new:1" {
		t.Fatalf("snapshot after edit = %+v", rows)
	}
	if _, has := rows[0]["attempts"]; has {
		t.Fatalf("pool edit must clear all counters, got %v", rows[0]["attempts"])
	}
}

func TestProxyHealthPersistsAcrossRestart(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"socks5://a:1"}})
	resetProxyHealthForTest(t)

	recordProxyOutcome(pkZen, "socks5://a:1", true, nil, 150)
	recordProxyOutcome(pkZen, "socks5://a:1", false, errors.New("reset by peer"), 0)
	flushProxyHealth()

	// 模拟进程重启：内存清零 + 重置懒加载，再从落盘文件恢复。
	exitpool.SetHealthStatsForTest(map[string]*exitpool.HealthEntry{})
	*exitpool.HealthOncePtr = sync.Once{}
	ensureProxyHealthLoaded()

	e := exitpool.HealthStatsForTest()["socks5://a:1"]
	if e == nil || e.Attempts != 2 || e.Successes != 1 {
		t.Fatalf("restored attempts/successes = %d/%d, want 2/1", e.Attempts, e.Successes)
	}
	if len(e.Recent) != 2 || e.Recent[0] != true || e.Recent[1] != false {
		t.Fatalf("restored recent = %v", e.Recent)
	}
}

// 传输层失败 vs 上游状态错误（2026-10-28）：只有前者该让出口退出轮转，
// 后者是上游返回了响应——冷却好节点正是 09-16 那次提交要避免的"毒化出口"。
func TestIsTransportLayerFailure(t *testing.T) {
	transport := errors.New("dial tcp 10.0.0.1:1080: i/o timeout")
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"network dial", transport, true},
		{"context canceled", context.Canceled, true}, // 传输层判定；取消过滤在 recordProxyOutcome 内
		{"trae2api RefreshError", &trae2api.RefreshError{HTTPStatus: 429, Code: 4023}, false},
		{"other HTTPError", &other.HTTPError{Status: 400}, false},
		{"cline StatusError", &cline.StatusError{Status: 401}, false},
	}
	for _, c := range cases {
		if got := exitpool.IsTransportLayerFailure(c.err); got != c.want {
			t.Errorf("%s: exitpool.IsTransportLayerFailure = %v, want %v", c.name, got, c.want)
		}
	}
}

// 传输层失败冷却出口（2026-10-28 全平台统一；2026-10-31 改 2-strike）：
// 此前只有 cline/zen 会摘掉坏节点，其余平台只记账不自愈，坏节点被轮转反复
// 选中、把故障记到多个凭据头上。单次失败只记账，窗口内第二次才冷却。
func TestRecordProxyOutcomeCoolsBadExit(t *testing.T) {
	// 锁定阈值为 2，断言的是"N-strike 触发冷却"机制本身
	t.Setenv("PROXY_COOLDOWN_STRIKES", "2")
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"http://a:1", "http://b:2"}})
	t.Cleanup(func() {
		resetProxyCooldownsForTest()
		exitpool.StreamFailMuPtr.Lock()
		*exitpool.StreamFailsPtr = map[string][]time.Time{}
		exitpool.StreamFailMuPtr.Unlock()
	})
	// 传输层失败第 1 次：只记账，不冷却（2-strike，单次抖动不摘出口）
	recordProxyOutcome(pkZen, "http://a:1", false, errors.New("EOF"), 0)
	if !proxyAvailable(0) {
		t.Fatal("single transport-layer failure must not cool the exit yet (2-strike)")
	}
	// 第 2 次（60s 窗口内）：退出轮转
	recordProxyOutcome(pkZen, "http://a:1", false, errors.New("EOF"), 0)
	if proxyAvailable(0) {
		t.Fatal("repeated transport-layer failure must cool the exit proxy")
	}
	if !proxyAvailable(1) {
		t.Fatal("unrelated proxy must stay available")
	}
	// 上游状态错误：不冷却（出口是通的）
	exitpool.ClearCooldownsAt(0)
	recordProxyOutcome(pkZen, "http://a:1", false, &other.HTTPError{Status: 502}, 0)
	if !proxyAvailable(0) {
		t.Fatal("upstream 5xx must not cool a healthy exit")
	}
	// 成功尝试不冷却
	recordProxyOutcome(pkZen, "http://b:2", true, nil, 12)
	if !proxyAvailable(1) {
		t.Fatal("successful attempt must not cool the exit")
	}
}

// 延迟测试读数（proxy_probe.go，2026-11-03）：与统计同文件落盘、同「编辑池
// 即清零」语义，且打码快照不泄漏原始 URL。
func TestProxyTestEntryPersistAndReset(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"socks5://a:1"}})
	resetProxyHealthForTest(t)

	exitpool.SetTestStatsForTest(map[string]*exitpool.TestEntry{
		"socks5://a:1": {At: time.Now(), Ok: true, MinMs: 120, MedianMs: 340, Samples: 3},
	})
	// 与生产路径一致：tests 写入必须伴随脏标记（runProxyLatencyBatch 的做法）
	exitpool.HealthMu().Lock()
	exitpool.SetHealthDirtyForTest(true)
	exitpool.HealthMu().Unlock()
	flushProxyHealth()

	// 模拟进程重启：内存清零 + 重置懒加载，再从落盘文件恢复。
	exitpool.SetTestStatsForTest(map[string]*exitpool.TestEntry{})
	*exitpool.HealthOncePtr = sync.Once{}
	ensureProxyHealthLoaded()

	e := exitpool.TestStatsForTest()["socks5://a:1"]
	if e == nil || !e.Ok || e.MinMs != 120 || e.MedianMs != 340 || e.Samples != 3 {
		t.Fatalf("restored test entry = %+v, want ok min=120 median=340 samples=3", e)
	}

	// 编辑池即清零（与统计同闸）
	req := httptest.NewRequest("POST", "/admin/api/opencode/config/update",
		strings.NewReader(`{"proxies":["socks5://b:2"]}`))
	rec := httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("pool edit failed: %d %s", rec.Code, rec.Body.String())
	}
	n := len(exitpool.TestStatsForTest())
	if n != 0 {
		t.Fatalf("pool edit must clear latency test entries, got %d", n)
	}

	// 快照行打码：tests 段不在健康快照中直接出现，行 proxy 一律打码
	snap := proxyHealthSnapshot()
	for _, row := range snap {
		if s, _ := row["proxy"].(string); strings.Contains(s, "socks5://a:1") {
			t.Fatalf("snapshot leaked raw url: %v", row)
		}
	}
}

// 测试样本计入统计（2026-11-03 用户定）：attempts/successes/recent/HS 均值
// 与真实流量同池记账，但**绝不触发冷却**——失败再多也只有统计意义。
func TestRecordProxyTestOutcomeUpdatesStatsWithoutCooldown(t *testing.T) {
	setupBindingTest(t, &zenTestConfig{Proxies: []string{"socks5://a:1"}})
	resetProxyHealthForTest(t)

	recordProxyTestOutcome("socks5://a:1", true, nil, 200)
	recordProxyTestOutcome("socks5://a:1", false, errors.New("dial tcp: refused"), 0)

	e := exitpool.HealthStatsForTest()["socks5://a:1"]
	if e == nil || e.Attempts != 2 || e.Successes != 1 {
		t.Fatalf("attempts/successes = %d/%d, want 2/1", e.Attempts, e.Successes)
	}
	if e.HSCount != 1 || e.HSSumMs != 200 {
		t.Fatalf("hsCount/hsSumMs = %d/%d, want 1/200", e.HSCount, e.HSSumMs)
	}
	if len(e.Recent) != 2 || e.Recent[0] != true || e.Recent[1] != false {
		t.Fatalf("recent = %v, want [true false]", e.Recent)
	}
	if !strings.Contains(e.LastErr, "refused") {
		t.Fatalf("lastErr = %q, want refused", e.LastErr)
	}

	// 红线：连续失败也绝不冷却（没有 strike 记账）
	for i := 0; i < 5; i++ {
		recordProxyTestOutcome("socks5://a:1", false, errors.New("EOF"), 0)
	}
	if !proxyAvailable(0) {
		t.Fatal("test failures must never trigger cooldown")
	}
}

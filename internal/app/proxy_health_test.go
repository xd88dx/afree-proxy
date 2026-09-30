package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 代理在线率统计（纯被动口径）的行为回归：
//   - 只记真实出口成败；直连不记，ctx 取消/截止不记任何一端；
//   - 最近窗口裁剪到 proxyHealthRecentN；
//   - 编辑代理池（proxiesChanged）整体清零并同步落盘；
//   - 落盘文件重启续算；
//   - 快照按当前代理池输出，URL 打码、孤儿条目不出现。

func resetProxyHealthForTest(t *testing.T) {
	t.Helper()
	proxyHealthMu.Lock()
	proxyHealthStats = map[string]*proxyHealthEntry{}
	proxyHealthDirty = false
	proxyHealthMu.Unlock()
	t.Cleanup(func() {
		proxyHealthMu.Lock()
		proxyHealthStats = map[string]*proxyHealthEntry{}
		proxyHealthDirty = false
		proxyHealthMu.Unlock()
	})
}

func TestProxyHealthRecordSkipsDirectAndAborts(t *testing.T) {
	setupBindingTest(t, &zenConfigData{Proxies: []string{"socks5://u:p@10.0.0.1:1080#a"}})
	resetProxyHealthForTest(t)

	px := "socks5://u:p@10.0.0.1:1080#a"
	recordProxyOutcome(px, true, nil)
	recordProxyOutcome("", true, nil)                              // 直连：不是代理，不记
	recordProxyOutcome(px, true, context.Canceled)                 // 客户端取消：不计成功
	recordProxyOutcome(px, false, context.DeadlineExceeded)        // 客户端截止：不计失败
	recordProxyOutcome(px, false, errors.New("dial tcp: refused")) // 真失败

	proxyHealthMu.Lock()
	e := proxyHealthStats[px]
	proxyHealthMu.Unlock()
	if e == nil || e.Attempts != 2 || e.Successes != 1 {
		t.Fatalf("attempts/successes = %d/%d, want 2/1", e.Attempts, e.Successes)
	}
	if !strings.Contains(e.LastErr, "refused") {
		t.Fatalf("lastErr = %q", e.LastErr)
	}
	if len(e.Recent) != 2 || e.Recent[0] != true || e.Recent[1] != false {
		t.Fatalf("recent = %v", e.Recent)
	}
}

func TestProxyHealthRecentWindowTrims(t *testing.T) {
	setupBindingTest(t, &zenConfigData{Proxies: []string{"socks5://w:1"}})
	resetProxyHealthForTest(t)

	px := "socks5://w:1"
	// 前 proxyHealthRecentN 次全失败，再补 10 次成功：窗口内应只剩这 10 次成功。
	for i := 0; i < proxyHealthRecentN; i++ {
		recordProxyOutcome(px, false, errors.New("dead"))
	}
	for i := 0; i < 10; i++ {
		recordProxyOutcome(px, true, nil)
	}

	proxyHealthMu.Lock()
	e := proxyHealthStats[px]
	proxyHealthMu.Unlock()
	if len(e.Recent) != proxyHealthRecentN {
		t.Fatalf("window size = %d, want %d", len(e.Recent), proxyHealthRecentN)
	}
	if got := e.recentOK(); got != 10 {
		t.Fatalf("recentOK = %d, want 10", got)
	}
	if e.Attempts != proxyHealthRecentN+10 || e.Successes != 10 {
		t.Fatalf("cumulative %d/%d, want %d/10", e.Attempts, e.Successes, proxyHealthRecentN+10)
	}
}

func TestProxyHealthSnapshotMasksAndFilters(t *testing.T) {
	setupBindingTest(t, &zenConfigData{Proxies: []string{"http://user:secret@10.0.0.9:8080#home"}})
	resetProxyHealthForTest(t)

	recordProxyOutcome("http://user:secret@10.0.0.9:8080#home", true, nil)
	recordProxyOutcome("http://ghost:1", true, nil) // 已不在池中的孤儿条目

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
	setupBindingTest(t, &zenConfigData{Proxies: []string{"socks5://used:1", "socks5://idle:1"}})
	resetProxyHealthForTest(t)
	recordProxyOutcome("socks5://used:1", true, nil)

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
	setupBindingTest(t, &zenConfigData{Proxies: []string{"socks5://old:1"}})
	resetProxyHealthForTest(t)

	recordProxyOutcome("socks5://old:1", false, errors.New("boom"))

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
	setupBindingTest(t, &zenConfigData{Proxies: []string{"socks5://a:1"}})
	resetProxyHealthForTest(t)

	recordProxyOutcome("socks5://a:1", true, nil)
	recordProxyOutcome("socks5://a:1", false, errors.New("reset by peer"))
	flushProxyHealth()

	// 模拟进程重启：内存清零 + 重置懒加载，再从落盘文件恢复。
	proxyHealthMu.Lock()
	proxyHealthStats = map[string]*proxyHealthEntry{}
	proxyHealthMu.Unlock()
	proxyHealthOnce = sync.Once{}
	ensureProxyHealthLoaded()

	proxyHealthMu.Lock()
	e := proxyHealthStats["socks5://a:1"]
	proxyHealthMu.Unlock()
	if e == nil || e.Attempts != 2 || e.Successes != 1 {
		t.Fatalf("restored attempts/successes = %d/%d, want 2/1", e.Attempts, e.Successes)
	}
	if len(e.Recent) != 2 || e.Recent[0] != true || e.Recent[1] != false {
		t.Fatalf("restored recent = %v", e.Recent)
	}
}

package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// swapReqLogSink 把请求日志的落盘目标指向临时目录，并把内存环形清空；
// reqLogsFile 是包级变量（kit.ResolveDataPath 在 init 期就定好了），测试里
// 必须整体替换，否则会把行写到仓库根下真实的 data/requests.jsonl。
func swapReqLogSink(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	reqLogsMu.Lock()
	savedLogs := reqLogs
	savedFile := reqLogsFile
	reqLogs = nil
	reqLogsFile = filepath.Join(dir, "requests.jsonl")
	reqLogsMu.Unlock()
	t.Cleanup(func() {
		reqLogsMu.Lock()
		reqLogs = savedLogs
		reqLogsFile = savedFile
		reqLogsMu.Unlock()
	})
}

// TestPoolAdminLogEnabledDefaultOff：未设置（nil）默认关闭。
func TestPoolAdminLogEnabledDefaultOff(t *testing.T) {
	poolMu.Lock()
	saved := pool
	pool = &AccountPool{Accounts: []*Account{}}
	poolMu.Unlock()
	t.Cleanup(func() {
		poolMu.Lock()
		pool = saved
		poolMu.Unlock()
	})

	if poolAdminLogEnabled() {
		t.Fatal("admin access logging must default to off when the field is nil")
	}
	on := true
	poolMu.Lock()
	pool.AdminLogEnabled = &on
	poolMu.Unlock()
	if !poolAdminLogEnabled() {
		t.Fatal("admin access logging must report on when the field is true")
	}
	off := false
	poolMu.Lock()
	pool.AdminLogEnabled = &off
	poolMu.Unlock()
	if poolAdminLogEnabled() {
		t.Fatal("admin access logging must report off when the field is false")
	}
}

// TestMiddlewareSkipsAdminLogsByDefault：开关关闭时 /admin 请求不落日志，
// 经过网关的模型会话请求照常记录。
func TestMiddlewareSkipsAdminLogsByDefault(t *testing.T) {
	swapReqLogSink(t)
	poolMu.Lock()
	saved := pool
	pool = &AccountPool{Accounts: []*Account{}}
	poolMu.Unlock()
	t.Cleanup(func() {
		poolMu.Lock()
		pool = saved
		poolMu.Unlock()
	})

	h := requestLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))

	// /admin 请求：默认不记录
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/api/stats", nil))
	if got := LoadRequestLogs(); len(got) != 0 {
		t.Fatalf("admin request must not be logged while the switch is off, got %+v", got)
	}

	// 模型会话请求：始终记录（body 探测出的 model 决定 route）
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"zen/big-pickle"}`))
	h.ServeHTTP(rec, req)
	got := LoadRequestLogs()
	if len(got) != 1 {
		t.Fatalf("model session request must always be logged, got %d entries", len(got))
	}
	if got[0].Route != "zen" || got[0].Model != "zen/big-pickle" {
		t.Fatalf("unexpected log entry: %+v", got[0])
	}
}

// TestMiddlewareLogsAdminWhenEnabled：开关打开后 /admin 请求照旧记录，
// 且带上了 admin 路由标记。
func TestMiddlewareLogsAdminWhenEnabled(t *testing.T) {
	swapReqLogSink(t)
	on := true
	poolMu.Lock()
	saved := pool
	pool = &AccountPool{Accounts: []*Account{}, AdminLogEnabled: &on}
	poolMu.Unlock()
	t.Cleanup(func() {
		poolMu.Lock()
		pool = saved
		poolMu.Unlock()
	})

	h := requestLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/admin/api/logs", nil))

	got := LoadRequestLogs()
	if len(got) != 1 || got[0].Route != "admin" || got[0].Path != "/admin/api/logs" {
		t.Fatalf("admin request must be logged while the switch is on, got %+v", got)
	}
}

// TestLoadRequestLogsFromFileSkipsAdminRows：开关关闭时不回载盘上的 admin 行，
// 开关打开时照常回载（否则"关了开关但日志页还有面板行"看起来像没生效）。
func TestLoadRequestLogsFromFileSkipsAdminRows(t *testing.T) {
	swapReqLogSink(t)
	poolMu.Lock()
	saved := pool
	pool = &AccountPool{Accounts: []*Account{}}
	poolMu.Unlock()
	t.Cleanup(func() {
		poolMu.Lock()
		pool = saved
		poolMu.Unlock()
	})

	lines := `{"client":"10.0.0.1","method":"GET","path":"/admin/api/stats","route":"admin","status":200,"duration_ms":1}
{"client":"10.0.0.1","method":"POST","path":"/v1/chat/completions","model":"zen/big-pickle","route":"zen","status":200,"duration_ms":9}
`
	if err := os.WriteFile(reqLogsFile, []byte(lines), 0600); err != nil {
		t.Fatalf("seed log file: %v", err)
	}

	reqLogsMu.Lock()
	reqLogs = nil
	reqLogsMu.Unlock()
	LoadRequestLogsFromFile()
	got := LoadRequestLogs()
	if len(got) != 1 || got[0].Route != "zen" {
		t.Fatalf("switch off must reload only non-admin rows, got %+v", got)
	}

	on := true
	poolMu.Lock()
	pool.AdminLogEnabled = &on
	poolMu.Unlock()
	reqLogsMu.Lock()
	reqLogs = nil
	reqLogsMu.Unlock()
	LoadRequestLogsFromFile()
	if got := LoadRequestLogs(); len(got) != 2 {
		t.Fatalf("switch on must reload admin rows too, got %d entries", len(got))
	}
}

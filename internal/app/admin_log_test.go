package app

// WorkBuddy/cline 面板 adminLog 开关的池侧行为（日志域测试在 internal/reqlog）。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"afree-proxy/internal/cline"
	"afree-proxy/internal/kit"
	"afree-proxy/internal/reqlog"
)

func TestPoolAdminLogEnabledDefaultOff(t *testing.T) {

	resetGlobalConfigForTest(t)

	poolMu.Lock()

	saved := cline.RawPool()

	cline.SetPool(&cline.AccountPool{Accounts: []*cline.Account{}})

	poolMu.Unlock()

	t.Cleanup(func() {

		poolMu.Lock()

		cline.SetPool(saved)

		poolMu.Unlock()

	})

	if poolAdminLogEnabled() {

		t.Fatal("admin access logging must default to off when the field is nil")

	}

	setGlobalAdminLogEnabled(true)

	if !poolAdminLogEnabled() {

		t.Fatal("admin access logging must report on when the field is true")

	}

	setGlobalAdminLogEnabled(false)

	if poolAdminLogEnabled() {

		t.Fatal("admin access logging must report off when the field is false")

	}

}

func TestMiddlewareSkipsAdminLogsByDefault(t *testing.T) {

	restoreSink := reqlog.SwapReqLogSinkForTest()
	t.Cleanup(restoreSink)

	// 显式隔离到 nil 默认态：测试名承诺的是"默认关"，不该吃前序测试残留的

	// 全局配置（-shuffle 纪律，同 TestMiddlewareLogsAdminWhenEnabled）。

	resetGlobalConfigForTest(t)

	poolMu.Lock()

	saved := cline.RawPool()

	cline.SetPool(&cline.AccountPool{Accounts: []*cline.Account{}})

	poolMu.Unlock()

	t.Cleanup(func() {

		poolMu.Lock()

		cline.SetPool(saved)

		poolMu.Unlock()

	})

	h := reqlog.RequestLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.WriteHeader(http.StatusOK)

		w.Write([]byte(`{"ok":true}`))

	}), classifyReqRoute)

	// /admin 请求：默认不记录

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/api/stats", nil))

	if got := reqlog.LoadRequestLogs(); len(got) != 0 {

		t.Fatalf("admin request must not be logged while the switch is off, got %+v", got)

	}

	// 模型会话请求：始终记录（body 探测出的 model 决定 route）

	rec = httptest.NewRecorder()

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"zen/big-pickle"}`))

	h.ServeHTTP(rec, req)

	got := reqlog.LoadRequestLogs()

	if len(got) != 1 {

		t.Fatalf("model session request must always be logged, got %d entries", len(got))

	}

	if got[0].Route != "zen" || got[0].Model != "zen/big-pickle" {

		t.Fatalf("unexpected log entry: %+v", got[0])

	}

}

func TestMiddlewareLogsAdminWhenEnabled(t *testing.T) {

	restoreSink := reqlog.SwapReqLogSinkForTest()
	t.Cleanup(restoreSink)

	resetGlobalConfigForTest(t)

	setGlobalAdminLogEnabled(true)

	poolMu.Lock()

	saved := cline.RawPool()

	cline.SetPool(&cline.AccountPool{Accounts: []*cline.Account{}})

	poolMu.Unlock()

	t.Cleanup(func() {

		poolMu.Lock()

		cline.SetPool(saved)

		poolMu.Unlock()

	})

	h := reqlog.RequestLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.WriteHeader(http.StatusOK)

	}), classifyReqRoute)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/admin/api/logs", nil))

	got := reqlog.LoadRequestLogs()

	if len(got) != 1 || got[0].Route != "admin" || got[0].Path != "/admin/api/logs" {

		t.Fatalf("admin request must be logged while the switch is on, got %+v", got)

	}

}

func TestLoadRequestLogsFromFileSkipsAdminRows(t *testing.T) {

	restoreSink := reqlog.SwapReqLogSinkForTest()
	t.Cleanup(restoreSink)

	// 全局配置隔离：本测试会把 adminLogEnabled 打开，不隔离会把 true 渗进

	// 后续依赖"默认关"的测试（-shuffle 下 TestMiddlewareSkipsAdminLogsByDefault

	// 排到本测试之后即红，2026-11-11 实测）。

	resetGlobalConfigForTest(t)

	poolMu.Lock()

	saved := cline.RawPool()

	cline.SetPool(&cline.AccountPool{Accounts: []*cline.Account{}})

	poolMu.Unlock()

	t.Cleanup(func() {

		poolMu.Lock()

		cline.SetPool(saved)

		poolMu.Unlock()

	})

	lines := `{"client":"10.0.0.1","method":"GET","path":"/admin/api/stats","route":"admin","status":200,"duration_ms":1}

{"client":"10.0.0.1","method":"POST","path":"/v1/chat/completions","model":"zen/big-pickle","route":"zen","status":200,"duration_ms":9}

`

	testFile := filepath.Join(kit.DataDir(), "reqlog-test", "requests.jsonl")
	if err := os.WriteFile(testFile, []byte(lines), 0600); err != nil {

		t.Fatalf("seed log file: %v", err)

	}

	restore := reqlog.ResetReqLogsForTest()

	t.Cleanup(restore)

	reqlog.LoadRequestLogsFromFile()

	got := reqlog.LoadRequestLogs()

	// 判据用"有没有 admin 行"而不是精确条数：reqlog.AppendReqLog 的落盘是异步

	// goroutine，前一个测试记录的 admin 行可能在本测试换好临时 sink 之后才追

	// 加写盘（CI 上实测因此多出 1 行，精确断言 ==1/==2 会随机失败）。语义只要求

	// 关闭时不回载 admin 行、且 zen 行照常回载，与盘上是否混有噪声行无关。

	if !containsRoute(got, "zen") {

		t.Fatalf("switch off must reload non-admin rows, got %+v", got)

	}

	if containsRoute(got, "admin") {

		t.Fatalf("switch off must skip admin rows, got %+v", got)

	}

	setGlobalAdminLogEnabled(true)

	reqlog.ResetReqLogsForTest()

	reqlog.LoadRequestLogsFromFile()

	got = reqlog.LoadRequestLogs()

	if !containsRoute(got, "admin") || !containsRoute(got, "zen") {

		t.Fatalf("switch on must reload admin rows too, got %+v", got)

	}

}

func containsRoute(logs []reqlog.RequestLog, route string) bool {

	for _, l := range logs {

		if l.Route == route {

			return true

		}

	}

	return false

}

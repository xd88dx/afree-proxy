package panel

import (
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misakano7545/workbuddy2api-panel/internal/auth"
	"github.com/misakano7545/workbuddy2api-panel/internal/pool"
)

// 新增身份默认不参与路由（三平台统一口径）：cockpit 导入的账号落盘即携带
// pool_enabled=false，池状态透出 PoolEnabled=false；管理员在面板勾选"启用"
// 后才进入选号。登录（login.go）走同一语义。
func TestImportCockpitDefaultsPoolDisabled(t *testing.T) {
	dir := t.TempDir()
	p := New(Config{
		Version:  "test",
		APIKey:   "test-key",
		AuthDir:  dir,
		Pool:     pool.New(""),
		Upstream: fakePanelUpstream(200, `{"code":0,"data":{}}`),
	})

	// 注册宿主通知钩子：必须在 import 请求之前（SetPoolEnabled(false) 在导入内联触发）。
	observed := make(chan string, 8)
	prevObserver := auth.AccountPrefObserver
	auth.AccountPrefObserver = func(uid string) { observed <- uid }
	t.Cleanup(func() { auth.AccountPrefObserver = prevObserver })

	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "accounts.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(`[{"id":"1","uid":"u1","access_token":"at","refresh_token":"rt","email":"a@b.c","domain":"www.workbuddy.cn"}]`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/panel/api/import/cockpit", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}

	raw, err := os.ReadFile(filepath.Join(dir, "workbuddy-u1.json"))
	if err != nil {
		t.Fatal(err)
	}
	// auth 文件回归纯凭据（2026-11-17 三键外置）：三键绝不落 vendor 文件。
	for _, key := range []string{"pool_enabled", "account_enabled", "nickname_locked"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("auth file must not carry %s (host-owned pref now): %s", key, raw)
		}
	}
	// 内存默认禁用语义不丢：导入即出池，直到管理员显式启用。
	st, ok := p.cfg.Pool.Status("u1")
	if !ok || st.PoolEnabled {
		t.Fatalf("pool status must report pool_enabled=false, got %+v ok=%v", st, ok)
	}
	// 宿主通知钩子必须触发（SetPoolEnabled(false) 变更路径）。
	select {
	case uid := <-observed:
		if uid != "u1" {
			t.Fatalf("observer notified uid=%q, want u1", uid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AccountPrefObserver was not notified on import (host would never persist the disabled pref)")
	}
}

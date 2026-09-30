package panel

import (
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"afree-proxy/internal/workbuddy/pool"
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
	if !strings.Contains(string(raw), `"pool_enabled": false`) {
		t.Fatalf("auth file must carry pool_enabled=false: %s", raw)
	}
	st, ok := p.cfg.Pool.Status("u1")
	if !ok || st.PoolEnabled {
		t.Fatalf("pool status must report pool_enabled=false, got %+v ok=%v", st, ok)
	}
}

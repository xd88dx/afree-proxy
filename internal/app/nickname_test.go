package app

import (
	"encoding/json"

	"github.com/misakano7545/workbuddy2api-panel"

	"net/http"

	"net/http/httptest"

	"os"

	"path/filepath"

	"strings"

	"testing"

	"afree-proxy/internal/cline"
)

// 账号列昵称（铅笔改名）：cline 走 /admin/api/accounts/nickname（空 = 清除，

// 恢复显示邮箱）；WorkBuddy 走 /admin/api/workbuddy/nickname（上游昵称兜底，

// 不允许清空）。两者都只改显示名，不碰路由状态。

func nickRequest(t *testing.T, path, body string) *httptest.ResponseRecorder {

	t.Helper()

	rec := httptest.NewRecorder()

	req := httptest.NewRequest("POST", path, strings.NewReader(body))

	if strings.Contains(path, "accounts/nickname") {

		handleAdminAccountNickname(rec, req)

	} else {

		handleWorkbuddyNickname(rec, req)

	}

	return rec

}

func TestAdminAccountNickname(t *testing.T) {

	tmp := t.TempDir()

	t.Setenv("DATA_DIR", tmp)

	// poolDataPath() 运行时按 DATA_DIR 求值（t.Setenv 已指向临时目录），

	// 这里直接注入池单例（测试结束恢复原值，防跨测试污染）。

	savedPool := cline.RawPool()
	cline.SetPool(&AccountPool{Accounts: []*Account{
		{AccountID: "acc_t1", Email: "a@b.c"},
		{AccountID: "acc_t2", Email: "d@e.f"},
	}})
	defer func() { cline.SetPool(savedPool) }()

	// 设置昵称

	if rec := nickRequest(t, "/admin/api/accounts/nickname", `{"accountId":"acc_t1","nickname":"  主力号  "}`); rec.Code != http.StatusOK {

		t.Fatalf("set nickname -> %d, body=%s", rec.Code, rec.Body.String())

	}

	if p := loadPool(); p.Accounts[0].Nickname != "主力号" {

		t.Fatalf("nickname = %q, want 主力号", p.Accounts[0].Nickname)

	}

	// 落盘验证（反序列化比对，不依赖 MarshalIndent 的空格格式）

	raw, err := os.ReadFile(poolDataPath())

	if err != nil {

		t.Fatal(err)

	}

	var disk AccountPool

	if err := json.Unmarshal(raw, &disk); err != nil {

		t.Fatal(err)

	}

	if len(disk.Accounts) != 2 || disk.Accounts[0].Nickname != "主力号" {

		t.Fatalf("nickname not persisted: %s", raw)

	}

	// 空串 = 清除

	if rec := nickRequest(t, "/admin/api/accounts/nickname", `{"accountId":"acc_t1","nickname":""}`); rec.Code != http.StatusOK {

		t.Fatalf("clear nickname -> %d", rec.Code)

	}

	if p := loadPool(); p.Accounts[0].Nickname != "" {

		t.Fatalf("nickname = %q, want empty after clear", p.Accounts[0].Nickname)

	}

	// 未知账号 / 缺 accountId / 超长

	if rec := nickRequest(t, "/admin/api/accounts/nickname", `{"accountId":"acc_nope","nickname":"x"}`); rec.Code != http.StatusBadRequest {

		t.Fatalf("unknown account -> %d, want 400", rec.Code)

	}

	if rec := nickRequest(t, "/admin/api/accounts/nickname", `{"nickname":"x"}`); rec.Code != http.StatusBadRequest {

		t.Fatalf("missing accountId -> %d, want 400", rec.Code)

	}

	long := strings.Repeat("长", 65)

	if rec := nickRequest(t, "/admin/api/accounts/nickname", `{"accountId":"acc_t1","nickname":"`+long+`"}`); rec.Code != http.StatusBadRequest {

		t.Fatalf("overlong nickname -> %d, want 400", rec.Code)

	}

}

func TestWorkbuddyNicknameHandler(t *testing.T) {

	tmp := t.TempDir()

	authDir := filepath.Join(tmp, "auths")

	if err := os.MkdirAll(authDir, 0o755); err != nil {

		t.Fatal(err)

	}

	p1 := wbbridge.PoolNew(filepath.Join(tmp, "state.json"))

	defer p1.Close()

	fp := filepath.Join(authDir, "workbuddy-u1.json")

	// AccessToken 必填：SaveAtomic 拒绝落空 token 的凭据（save refused）。

	p1.Add(&wbbridge.Auth{UID: "u1", Nickname: "old", AccessToken: "at-1", FilePath: fp})

	p1.Add(&wbbridge.Auth{UID: "u2", Nickname: "", AccessToken: "at-2", FilePath: filepath.Join(authDir, "workbuddy-u2.json")})

	savedSub := *workbuddySub

	defer func() { *workbuddySub = savedSub }()

	*workbuddySub = &workbuddySubsystem{CfgPath: filepath.Join(tmp, "config.json"), Cfg: &wbbridge.WBConfig{AuthDir: authDir}, Pool: p1}

	// 改昵称：池内生效 + auth 文件落盘（含本地锁 nickname_locked）

	if rec := nickRequest(t, "/admin/api/workbuddy/nickname", `{"uid":"u1","nickname":"  本地名  "}`); rec.Code != http.StatusOK {

		t.Fatalf("set nickname -> %d, body=%s", rec.Code, rec.Body.String())

	}

	if a := p1.AuthByUID("u1"); a == nil || a.Nickname != "本地名" {

		t.Fatalf("pool nickname = %+v, want 本地名", a)

	}

	if a := p1.AuthByUID("u1"); a == nil || !a.NicknameLocked() {

		t.Fatal("manual rename must lock the nickname against upstream sync")

	}

	raw, err := os.ReadFile(fp)

	if err != nil {

		t.Fatal(err)

	}

	// auth 文件是嵌套形（插件可读），昵称在 account 段。2026-11-17 三键外置：

	// nickname_locked 不再落 vendor 凭据文件（宿主经 AccountPrefObserver 落

	// wkbd-config.json），文件回归纯凭据+昵称展示形态。

	if !strings.Contains(string(raw), "本地名") {

		t.Fatalf("nickname not persisted into auth file: %s", raw)

	}

	if strings.Contains(string(raw), "nickname_locked") {

		t.Fatalf("auth file is credentials-only; nickname_locked must not be written: %s", raw)

	}

	// 幂等：同值再提交仍 200

	if rec := nickRequest(t, "/admin/api/workbuddy/nickname", `{"uid":"u1","nickname":"本地名"}`); rec.Code != http.StatusOK {

		t.Fatalf("idempotent resubmit -> %d, want 200", rec.Code)

	}

	// 清空 = 拉上游恢复：upstream 不可用时整体失败（503），不做半更新

	if rec := nickRequest(t, "/admin/api/workbuddy/nickname", `{"uid":"u1","nickname":"  "}`); rec.Code != http.StatusServiceUnavailable {

		t.Fatalf("clear with no upstream -> %d, want 503", rec.Code)

	}

	// 失败后不得破坏既有状态：昵称与锁都保持原样

	if a := p1.AuthByUID("u1"); a == nil || a.Nickname != "本地名" || !a.NicknameLocked() {

		t.Fatal("failed clear must not touch nickname or lock state")

	}

	// 未知 uid

	if rec := nickRequest(t, "/admin/api/workbuddy/nickname", `{"uid":"ghost","nickname":"x"}`); rec.Code != http.StatusBadRequest {

		t.Fatalf("unknown uid -> %d, want 400", rec.Code)

	}

	// 缺 uid

	if rec := nickRequest(t, "/admin/api/workbuddy/nickname", `{"nickname":"x"}`); rec.Code != http.StatusBadRequest {

		t.Fatalf("missing uid -> %d, want 400", rec.Code)

	}

	// 子系统不可用

	*workbuddySub = nil

	if rec := nickRequest(t, "/admin/api/workbuddy/nickname", `{"uid":"u1","nickname":"x"}`); rec.Code != http.StatusServiceUnavailable {

		t.Fatalf("subsystem down -> %d, want 503", rec.Code)

	}

}

// 昵称随 cline 配置备份走（Account 整段序列化，无需专门段）——防回归：

// 若有人把 Nickname 改成 json:"-"，导出导入会悄悄丢昵称。

func TestAccountNicknameInBackup(t *testing.T) {

	raw, err := json.Marshal(&Account{AccountID: "acc_t1", Email: "a@b.c", Nickname: "主力号"})

	if err != nil {

		t.Fatal(err)

	}

	if !strings.Contains(string(raw), `"nickname":"主力号"`) {

		t.Fatalf("Account must serialize nickname for backup round-trip: %s", raw)

	}

	var back Account

	if err := json.Unmarshal(raw, &back); err != nil {

		t.Fatal(err)

	}

	if back.Nickname != "主力号" {

		t.Fatalf("nickname round-trip = %q", back.Nickname)

	}

}

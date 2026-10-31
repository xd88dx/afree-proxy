package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

)

// 隔离测试环境：独立 DATA_DIR，并清空进程级会话/失败印记状态。
// （上游移植的测试统一调用本函数；proxy_binding_test.go 里另有同名无返回值
// 版本只清会话表，两套测试环境不能混用。）
func setupZenSessionTestUpstream(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "zen-session-test")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("DATA_DIR", dir)

	zenSessMu.Lock()
	zenSessions = map[string]*zenSessionEntry{}
	zenSessLoaded = true
	zenSessPath = ""
	zenSessSaveBlocked = false
	zenSessMu.Unlock()
	zenSessFailedMu.Lock()
	zenSessFailed = map[string]int{}
	zenSessFailedMu.Unlock()
	return dir
}

// pickZenKey 必须优先挑 live key：只要池里还有合法会话的 key，
// 未命中轮就该选它（异常窗口外的常规语义）。
func TestPickZenKeyPrefersLiveSessions(t *testing.T) {
	setupZenSessionTestUpstream(t)
	cfg := getZenConfig()
	cfg2 := *cfg
	cfg2.Keys = []string{"sk-dead-1", "sk-live-2", "sk-dead-3"}
	setZenConfig(&cfg2)
	t.Cleanup(func() { c := *cfg; setZenConfig(&c) })

	zenKeyMu.Lock()
	zenKeyCool = map[string]time.Time{}
	zenKeyIdx = 0
	zenKeyMu.Unlock()

	zenSessMu.Lock()
	zenSessions["sk-live-2"] = &zenSessionEntry{Session: "ses_" + "0123456789ab" + "0123456789ABcd"}
	zenSessMu.Unlock()

	for i := 0; i < 5; i++ {
		if got := pickZenKey(); got != "sk-live-2" {
			t.Fatalf("pickZenKey = %q; must prefer the only live key", got)
		}
	}
}

// 池里没有 live 会话时仍须返回 key（异常窗口内请求必须发得出去，
// 上游 403 是唯一可用信号，恢复路径随后本地换新会话）。
func TestPickZenKeyStillReturnsWhenNothingLive(t *testing.T) {
	setupZenSessionTestUpstream(t)
	cfg := getZenConfig()
	cfg2 := *cfg
	cfg2.Keys = []string{"sk-a", "sk-b"}
	setZenConfig(&cfg2)
	t.Cleanup(func() { c := *cfg; setZenConfig(&c) })
	zenKeyMu.Lock()
	zenKeyCool = map[string]time.Time{}
	zenKeyMu.Unlock()

	got := pickZenKey()
	if got != "sk-a" && got != "sk-b" {
		t.Fatalf("pickZenKey = %q; must still hand out a key when none is live", got)
	}
}

// 面板端点：key 快照必须报出 live 状态与已存在的会话。
func TestZenSessionsSnapshot(t *testing.T) {
	setupZenSessionTestUpstream(t)
	cfg := getZenConfig()
	cfg2 := *cfg
	cfg2.Keys = []string{"sk-one", "sk-two"}
	setZenConfig(&cfg2)
	t.Cleanup(func() { c := *cfg; setZenConfig(&c) })

	StickyZenIdentity("sk-one") // 首次请求即本地铸造
	snaps := zenKeyStatus()
	if len(snaps) != 2 {
		t.Fatalf("got %d key states, want 2", len(snaps))
	}
	if snaps[0]["sessionLive"] != true {
		t.Fatalf("key #1 must report sessionLive after local minting: %+v", snaps[0])
	}
	if snaps[1]["sessionLive"] != false {
		t.Fatalf("key #2 never requested; sessionLive must be false: %+v", snaps[1])
	}
	if _, ok := snaps[0]["createdAt"]; !ok {
		t.Fatalf("key #1 must report createdAt after local minting: %+v", snaps[0])
	}
}

// StickyZenIdentity 铸造的会话必须通过 zen 的格式门
// ^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$，且同 key 复用、跨 key 不同。
func TestStickyZenIdentityMintsValidSession(t *testing.T) {
	setupZenSessionTestUpstream(t)
	a1, _, _ := StickyZenIdentity("sk-a")
	a2, _, _ := StickyZenIdentity("sk-a")
	b, _, _ := StickyZenIdentity("sk-b")
	if a1 != a2 {
		t.Fatalf("sticky identity changed between calls: %s -> %s", a1, a2)
	}
	if a1 == b {
		t.Fatal("two keys must not share a session")
	}
	if !zenSessionLive("sk-a") || !zenSessionLive("sk-b") {
		t.Fatal("minted sessions must be live immediately (no placeholder state)")
	}
	if _, _, ua := StickyZenIdentity("sk-a"); ua != zenNativeUA {
		t.Fatalf("UA = %q, want the native ai-sdk UA", ua)
	}
}

// 403 日志描述：无会话与有会话（带年龄）必须可区分。
func TestZenSessionDescReportsAge(t *testing.T) {
	setupZenSessionTestUpstream(t)
	key := "sk-desc"
	if got := zenSessionDesc(key); got != "no session" {
		t.Fatalf("unminted key desc = %q", got)
	}

	zenSessMu.Lock()
	zenSessions[key] = &zenSessionEntry{
		Session:   "ses_" + "0123456789ab" + "0123456789ABcd",
		CreatedAt: time.Now().Add(-5 * time.Hour).Unix(),
	}
	zenSessMu.Unlock()
	got := zenSessionDesc(key)
	if !contains(got, "minted") || !contains(got, "ago") {
		t.Fatalf("minted desc = %q; must report the session age", got)
	}
}

// refreshZenSession：每次调用同步换新（幂等、零成本）；空 key / 池外 key 不动；
// 已从配置移除的 key 不重建（防止 prune 后又被复活）。
func TestRefreshZenSessionMintsFreshSession(t *testing.T) {
	setupZenSessionTestUpstream(t)
	key := "sk-refresh"
	cfg := getZenConfig()
	cfg2 := *cfg
	cfg2.Keys = []string{key}
	setZenConfig(&cfg2)
	t.Cleanup(func() { c := *cfg; setZenConfig(&c) })

	StickyZenIdentity(key)
	zenSessMu.Lock()
	before := zenSessions[key].Session
	zenSessMu.Unlock()

	refreshZenSession(key)
	zenSessMu.Lock()
	after := zenSessions[key].Session
	zenSessMu.Unlock()
	if after == before {
		t.Fatal("refreshZenSession must mint a new session id")
	}
	if !zenSessionLive(key) {
		t.Fatal("refreshed session must be format-valid/live")
	}
	// 再换一次也必须得到新身份（同步、无退避门槛）
	refreshZenSession(key)
	zenSessMu.Lock()
	third := zenSessions[key].Session
	zenSessMu.Unlock()
	if third == after {
		t.Fatal("second refresh must mint another new session id")
	}
	// 空 key 与池外 key 不动作（不铸造、不重建条目）
	refreshZenSession("")
	refreshZenSession("sk-not-in-pool")
}

// refreshZenSession 不重建已移除的 key：prune 之后该 key 的条目必须保持消失，
// 否则 .zen-sessions.json 会被删掉的旧 key 单调撑大。
func TestRefreshZenSessionSkipsUnconfiguredKey(t *testing.T) {
	setupZenSessionTestUpstream(t)
	key := "sk-removed-refresh"
	cfg := getZenConfig()
	cfg2 := *cfg
	cfg2.Keys = []string{"sk-only-active"}
	setZenConfig(&cfg2)
	t.Cleanup(func() { c := *cfg; setZenConfig(&c) })

	refreshZenSession(key)
	zenSessMu.Lock()
	_, exists := zenSessions[key]
	zenSessMu.Unlock()
	if exists {
		t.Fatal("refreshZenSession must not re-create an entry for a key absent from the config")
	}
}

// zenSessionMarkSuccess 清除失败印记：一次成功即该 key 恢复干净。
func TestZenSessionMarkSuccessClearsFailureMark(t *testing.T) {
	setupZenSessionTestUpstream(t)
	key := "sk-success"
	zenSessionFailed(key)
	zenSessFailedMu.Lock()
	n := zenSessFailed[key]
	zenSessFailedMu.Unlock()
	if n == 0 {
		t.Fatal("failure mark must be recorded")
	}
	zenSessionMarkSuccess(key)
	zenSessFailedMu.Lock()
	_, still := zenSessFailed[key]
	zenSessFailedMu.Unlock()
	if still {
		t.Fatal("success must clear the failure mark")
	}
}

// 配置移除 key 后 prune 会话与失败印记。
func TestPruneRemovesSessionsAndFailureMarks(t *testing.T) {
	setupZenSessionTestUpstream(t)
	k := "sk-removed"
	StickyZenIdentity(k)
	zenSessionFailed(k)

	pruneZenKeyState(map[string]bool{})

	if zenSessionLive(k) {
		t.Fatal("session of a removed key must be pruned")
	}
	zenSessFailedMu.Lock()
	_, marked := zenSessFailed[k]
	zenSessFailedMu.Unlock()
	if marked {
		t.Fatal("failure mark of a removed key must be pruned")
	}
	// 未移除的 key 不受影响
	k2 := "sk-kept"
	StickyZenIdentity(k2)
	sess := zenSessions[k2].Session
	pruneZenKeyState(map[string]bool{k2: true})
	zenSessMu.Lock()
	kept := zenSessions[k2].Session
	zenSessMu.Unlock()
	if kept != sess {
		t.Fatal("session of a still-configured key must survive prune")
	}
}

// 旧版本会话文件迁移：合法格式的 ses_* 保留（格式即凭证），sess_* 占位
// 直接换成新铸造 ID——占位今天就在 403，永远不会自愈。
// 坏文件先备份再空跑：否则紧随其后的 save 会直接覆盖，出问题时无从追查。
// 会话表与 zen 配置同住 zen-config.json，损坏留档针对整文件。
func TestCorruptSessionFileIsBackedUp(t *testing.T) {
	dir := setupZenSessionTestUpstream(t)
	path := filepath.Join(dir, "zen-config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	zenSessMu.Lock()
	zenSessions = map[string]*zenSessionEntry{}
	zenSessLoaded = false
	zenSessPath = ""
	zenSessMu.Unlock()
	loadZenSessions()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("corrupt zen config must be moved aside, still at %s (err=%v)", path, err)
	}
	backups, _ := filepath.Glob(path + ".corrupt-*")
	if len(backups) == 0 {
		t.Fatalf("corrupt zen config must be kept as .corrupt-<stamp>")
	}
	zenSessMu.Lock()
	n := len(zenSessions)
	zenSessMu.Unlock()
	if n != 0 {
		t.Fatalf("corrupt file must start with an empty table, got %d entries", n)
	}
}

func TestMaskZenKeyNeverLeaksShortKey(t *testing.T) {
	if m := maskZenKey("sk-1"); contains(m, "sk-1") {
		t.Fatalf("short key leaked in mask: %q", m)
	}
	if m := maskZenKey("sk-abcdefghijklmn"); m != "sk-abc…" {
		t.Fatalf("long key mask: got %q", m)
	}
	if m := maskZenKey(""); m != "-" {
		t.Fatalf("empty key mask: got %q", m)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// 全冷却默认拒绝（UNABLE_FORCE 未设，2026-10-28 用户定）；true 时恢复兜底。
func TestPickZenKeyAllCoolingFollowsUnableForce(t *testing.T) {
	setupZenSessionTestUpstream(t)
	cfg := getZenConfig()
	cfg2 := *cfg
	cfg2.Keys = []string{"sk-a", "sk-b"}
	setZenConfig(&cfg2)
	t.Cleanup(func() { c := *cfg; setZenConfig(&c) })
	zenKeyMu.Lock()
	zenKeyCool = map[string]time.Time{
		"sk-a": time.Now().Add(5 * time.Minute),
		"sk-b": time.Now().Add(5 * time.Minute),
	}
	zenKeyIdx = 0
	zenKeyMu.Unlock()
	if got := pickZenKey(); got != "" {
		t.Fatalf("default must reject when all keys are cooling, got %q", got)
	}
	t.Setenv("UNABLE_FORCE", "true")
	if got := pickZenKey(); got != "sk-a" && got != "sk-b" {
		t.Fatalf("UNABLE_FORCE=true must fall back to a cooling key, got %q", got)
	}
}

// 冷却落盘回读（2026-10-28 用户定）：冷却写入后落盘，重新加载冷却保留。
// 2026-10-31 拆分后持久化目标是独立的 zen-state.json。
func TestZenKeyCoolPersistedAcrossReload(t *testing.T) {
	setupZenSessionTestUpstream(t)
	dir := t.TempDir()
	oldOverride := zenUsagePathOverride
	zenUsagePathOverride = filepath.Join(dir, "zen-state.json")
	t.Cleanup(func() { zenUsagePathOverride = oldOverride })
	cfg := getZenConfig()
	cfg2 := *cfg
	cfg2.Keys = []string{"sk-a", "sk-b"}
	setZenConfig(&cfg2)
	t.Cleanup(func() { c := *cfg; setZenConfig(&c) })
	zenKeyMu.Lock()
	zenKeyCool["sk-a"] = time.Now().Add(5 * time.Minute)
	zenKeyMu.Unlock()
	saveZenUsage()
	// 模拟重启：重读痕迹文件回填运行时表
	zenKeyCool = map[string]time.Time{}
	loadZenUsage()
	zenKeyMu.Lock()
	_, ok := zenKeyCool["sk-a"]
	zenKeyMu.Unlock()
	if !ok {
		t.Fatal("zen key cooldown must be persisted to zen-state.json and survive reload")
	}
}

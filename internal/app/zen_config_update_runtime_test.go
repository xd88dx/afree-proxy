package app

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 面板保存 zen 配置（config/update，全量替换语义）不得清掉运行态历史段。
// 2026-10-31 事故回归：next 曾按逐字段枚举重建，漏掉后来新增的 Cool/KeyTokens，
// setZenConfig 的"以配置为准回填"随即将包级运行态清零并落盘——面板上任意一次
// 保存排除代理规则/maxConcurrency 等都会把冷却表与 token 记账清空，rsync
// -Deploy 又会把坏配置倒灌另一台机器。next 改为浅拷贝 cur 后，任何
// zenConfigData 新增字段都不再可能从这里漏。
func TestZenConfigUpdateKeepsRuntimeSections(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	oldPath := zenConfigPathOverride
	zenConfigPathOverride = filepath.Join(dir, "zen-config.json")
	savedZen := getZenConfig()
	zenKeyMu.Lock()
	savedCool, savedTokens := zenKeyCool, zenKeyTokens
	zenKeyMu.Unlock()
	t.Cleanup(func() {
		zenConfigPathOverride = oldPath
		setZenConfig(savedZen)
		zenKeyMu.Lock()
		zenKeyCool, zenKeyTokens = savedCool, savedTokens
		zenKeyMu.Unlock()
	})

	// 起始态：15 个 key 的池 + 冷却表 + token 记账 + 代理绑定（事故现场全要素）
	keys := []string{"sk-a1", "sk-a2", "sk-a3"}
	coolUntil := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	setZenConfig(&zenConfigData{
		Keys:        keys,
		Cool:        map[string]time.Time{"sk-a1": coolUntil},
		KeyTokens:   map[string]zenKeyTokenStats{"sk-a1": {Today: 11, Total: 222}},
		KeyBindings: map[string]proxyBinding{"sk-a1": {Main: "socks5://10.0.0.1:1080"}},
	})

	req := httptest.NewRequest("POST", "/admin/api/opencode/config/update",
		strings.NewReader(`{"retries":5}`))
	rec := httptest.NewRecorder()
	handleZenConfigUpdate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update failed: %d %s", rec.Code, rec.Body.String())
	}

	// 运行态：冷却与记账不得被面板保存清零
	zenKeyMu.Lock()
	_, coolOK := zenKeyCool["sk-a1"]
	tok := zenKeyTokens["sk-a1"]
	zenKeyMu.Unlock()
	if !coolOK {
		t.Fatal("panel save wiped the key cooldown table")
	}
	if tok.Total != 222 {
		t.Fatalf("panel save wiped key token ledger (total=%d)", tok.Total)
	}
	cfg := getZenConfig()
	if !cfg.Cool["sk-a1"].Equal(coolUntil) {
		t.Fatal("zenConfig.Cool lost the cooldown entry after panel save")
	}
	if cfg.KeyTokens["sk-a1"].Total != 222 {
		t.Fatal("zenConfig.KeyTokens lost the ledger after panel save")
	}
	if cfg.Retries != 5 {
		t.Fatalf("patched retries=%d, want 5", cfg.Retries)
	}

	// 落盘：磁盘文件里 cool/keyTokens/keyBindings 段必须齐全
	raw, err := os.ReadFile(zenConfigPathOverride)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var disk struct {
		Cool        map[string]time.Time        `json:"cool"`
		KeyTokens   map[string]zenKeyTokenStats `json:"keyTokens"`
		KeyBindings map[string]proxyBinding     `json:"keyBindings"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if _, ok := disk.Cool["sk-a1"]; !ok {
		t.Fatal("cool section missing on disk after panel save")
	}
	if disk.KeyTokens["sk-a1"].Total != 222 {
		t.Fatal("keyTokens section missing/emptied on disk after panel save")
	}
	if _, ok := disk.KeyBindings["sk-a1"]; !ok {
		t.Fatal("keyBindings section missing on disk after panel save")
	}
}

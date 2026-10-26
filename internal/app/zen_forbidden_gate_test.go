package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 持续性 403 闸门：窗口判定。
// 语义：只有"因 403 被动换新"才盖时间戳；窗口内再遇干净 403 ⇒ 闸门开（冷却）。
func TestZenForbiddenGateWindow(t *testing.T) {
	setupZenSessionTestUpstream(t)
	key := "sk-gate-a"
	now := time.Now()

	cases := []struct {
		name      string
		entry     *zenSessionEntry
		wantBlock bool
	}{
		{
			name:      "从未被动换新（首次 403）→ 放行一次换新",
			entry:     &zenSessionEntry{Session: "ses_a", CreatedAt: now.Add(-2 * time.Hour).Unix()},
			wantBlock: false,
		},
		{
			name: "刚被动换新 2 分钟又 403 → 闸门开",
			entry: &zenSessionEntry{
				Session: "ses_b", CreatedAt: now.Add(-2 * time.Minute).Unix(),
				RefreshedAt: now.Add(-2 * time.Minute).Unix(),
			},
			wantBlock: true,
		},
		{
			name: "被动换新 9 分 50 秒（窗口内边缘）→ 闸门开",
			entry: &zenSessionEntry{
				Session: "ses_c", CreatedAt: now.Add(-10 * time.Minute).Unix(),
				RefreshedAt: now.Add(-(9*time.Minute + 50*time.Second)).Unix(),
			},
			wantBlock: true,
		},
		{
			name: "被动换新 11 分钟（窗口外）→ 再给一次换新机会",
			entry: &zenSessionEntry{
				Session: "ses_d", CreatedAt: now.Add(-30 * time.Minute).Unix(),
				RefreshedAt: now.Add(-11 * time.Minute).Unix(),
			},
			wantBlock: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			zenSessMu.Lock()
			zenSessions[key] = c.entry
			zenSessMu.Unlock()
			if got := zenSessionRefreshedRecently(key, now); got != c.wantBlock {
				t.Fatalf("zenSessionRefreshedRecently = %v, want %v (entry=%+v)", got, c.wantBlock, *c.entry)
			}
		})
	}

	// 空 key / 未知 key 一律放行（无从判断时保守换新一次）。
	zenSessMu.Lock()
	delete(zenSessions, key)
	zenSessMu.Unlock()
	if zenSessionRefreshedRecently("", now) || zenSessionRefreshedRecently("sk-unknown", now) {
		t.Fatal("empty or unknown key must not open the gate")
	}
}

// 时间戳的写入/清除时机：被动换新盖戳、成功一跳清戳、计划内轮换不盖戳。
func TestZenForbiddenGateStampsAndClears(t *testing.T) {
	dir := setupZenSessionTestUpstream(t)
	cfg := getZenConfig()
	cfg2 := *cfg
	cfg2.Keys = []string{"sk-gate-b"}
	setZenConfig(&cfg2)
	t.Cleanup(func() { c := *cfg; setZenConfig(&c) })

	key := "sk-gate-b"
	// 被动换新：应盖戳，且立刻落盘（重启后闸门仍有效）。
	refreshZenSession(key)
	zenSessMu.Lock()
	e := zenSessions[key]
	stamped := e != nil && e.RefreshedAt != 0
	zenSessMu.Unlock()
	if !stamped {
		t.Fatal("refreshZenSession must stamp RefreshedAt")
	}
	if !zenSessionRefreshedRecently(key, time.Now()) {
		t.Fatal("gate must be open right after a passive refresh")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "zen-config.json"))
	if err != nil {
		t.Fatalf("read persisted zen config: %v", err)
	}
	var file struct {
		Sessions map[string]map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse persisted zen config: %v", err)
	}
	if _, ok := file.Sessions[key]["refreshedAt"]; !ok {
		t.Fatalf("refreshedAt must be persisted, got keys %v", file.Sessions[key])
	}

	// 成功一跳：清戳（该 key+出口当前可用，将来偶发 403 仍能换新一次）。
	zenSessionMarkSuccess(key)
	zenSessMu.Lock()
	cleared := zenSessions[key].RefreshedAt == 0
	zenSessMu.Unlock()
	if !cleared {
		t.Fatal("zenSessionMarkSuccess must clear RefreshedAt")
	}
	if zenSessionRefreshedRecently(key, time.Now()) {
		t.Fatal("gate must be closed after a successful request")
	}

	// 计划内轮换（主动铸造）不盖戳：刚换的会话被 403 应允许被动换新一次。
	zenSessMu.Lock()
	zenSessions[key].RefreshedAt = time.Now().Add(-time.Minute).Unix()
	mintZenSessionEntryLocked(zenSessions[key], time.Now())
	zenSessMu.Unlock()
	if zenSessionRefreshedRecently(key, time.Now()) {
		t.Fatal("planned rotation must not leave a passive-refresh stamp")
	}
}

// 重启不丢：加载持久化文件后闸门依然生效（否则冷却只在单进程内有效）。
func TestZenForbiddenGateSurvivesReload(t *testing.T) {
	setupZenSessionTestUpstream(t)
	key := "sk-gate-c"
	stamp := time.Now().Add(-3 * time.Minute).Unix()
	zenSessMu.Lock()
	zenSessions[key] = &zenSessionEntry{Session: "ses_reload", CreatedAt: stamp, RefreshedAt: stamp}
	saveZenSessionsLocked()
	// 模拟重启：清内存表、允许重新从磁盘加载。
	zenSessions = map[string]*zenSessionEntry{}
	zenSessLoaded = false
	zenSessPath = ""
	zenSessMu.Unlock()

	loadZenSessions()

	if !zenSessionRefreshedRecently(key, time.Now()) {
		t.Fatal("gate must stay open after reload from zen-config.json")
	}
}

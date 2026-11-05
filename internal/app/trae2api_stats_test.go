package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	traepool "afree-proxy/internal/trae2api/pool"
)

// TraeWork 持久记账与显示排序回归（2026-11-05 用户定）：
//   - 记账跨日惰性清零：Date != 今天 → 展示今日 0（累计不动）；
//   - SetOrder 仅改展示顺序（All() 输出序），选号加权随机不受影响；
//     未提及的账号排尾按 UID（全量入序、输出稳定）。

func TestTrae2apiTokensSnapshotCrossDayLazyReset(t *testing.T) {
	traeTokensMu.Lock()
	saved := traeTokens
	traeTokens = map[string]trae2apiTokenStats{
		"u1": {Today: 5650000, Total: 9000000, Date: "2026-11-04"},
		"u2": {Today: 1234, Total: 1234, Date: time.Now().Format("2006-01-02")},
	}
	traeTokensMu.Unlock()
	t.Cleanup(func() {
		traeTokensMu.Lock()
		traeTokens = saved
		traeTokensMu.Unlock()
	})

	today, total, _, _ := trae2apiTokensSnapshot("u1")
	if today != 0 {
		t.Fatalf("stale-date today must display 0, got %d", today)
	}
	if total != 9000000 {
		t.Fatalf("total must survive cross-day, got %d", total)
	}
	today, _, _, _ = trae2apiTokensSnapshot("u2")
	if today != 1234 {
		t.Fatalf("same-date today must keep its value, got %d", today)
	}
}

func TestTrae2apiSetDisplayOrderTailAppendsUnknown(t *testing.T) {
	p := traepool.New(traepool.Options{})
	p.Add(&traepool.Account{UID: "b", Label: "b"})
	p.Add(&traepool.Account{UID: "a", Label: "a"})
	p.Add(&traepool.Account{UID: "c", Label: "c"})

	if n := p.SetOrder([]string{"c", "ghost"}); n != 3 { // 未知 uid 忽略；全量入序后返回池内账号数
		t.Fatalf("SetOrder returned %d, want 3 (full pool ordered)", n)
	}
	got := []string{}
	for _, a := range p.All() {
		got = append(got, a.UID)
	}
	// c 已入序在前；a/b 未提及排尾按 UID；全量 3 个都在。
	want := []string{"c", "a", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// 展示顺序必须落盘（2026-11-05 用户定：顺序改动落盘、随配置备份迁移；
// 此前是进程内展示态，导出→导入/重启即回 UID 序——生产实测丢）。
func TestTraeDisplayOrderPersistsAndImports(t *testing.T) {
	savedPath := proxyConfigPathOverride
	proxyConfigPathOverride = filepath.Join(t.TempDir(), "proxy-config.json")
	t.Cleanup(func() { proxyConfigPathOverride = savedPath })

	// 落盘：写段后重读文件必须带 trae2apiDisplayOrder
	traeDisplayOrderSave([]string{"u2", "u1"})
	raw, err := os.ReadFile(proxyConfigPathOverride)
	if err != nil {
		t.Fatalf("read proxy-config.json: %v", err)
	}
	if !strings.Contains(string(raw), `"trae2apiDisplayOrder": [`) {
		t.Fatalf("display order must be persisted to proxy-config.json: %s", raw)
	}

	// 新"进程"装载：空内存副本从文件恢复
	traeOrderMu.Lock()
	traeDisplayOrder = nil
	traeOrderMu.Unlock()
	traeDisplayOrderLoad()
	if got := traeDisplayOrderForBackup(); len(got) != 2 || got[0] != "u2" || got[1] != "u1" {
		t.Fatalf("reloaded order = %v, want [u2 u1]", got)
	}

	// 备份导入：整体替换 + 落盘
	if n := importTrae2apiDisplayOrder([]string{"x"}); n != 1 {
		t.Fatalf("import n = %d, want 1", n)
	}
	raw, _ = os.ReadFile(proxyConfigPathOverride)
	if !strings.Contains(string(raw), `"x"`) || strings.Contains(string(raw), `"u2"`) {
		t.Fatalf("import must replace the order wholesale: %s", raw)
	}
}

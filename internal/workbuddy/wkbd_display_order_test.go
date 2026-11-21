package workbuddy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	wbbridge "github.com/misakano7545/workbuddy2api-panel"
)

// 顺序意志外置（2026-11-21）：displayOrder 住 wkbd-config.json（配置同步面），
// 重启由 replayWbDisplayOrder 经 pool.SetOrder 回放；vendor state.json 只留
// vendor 原生落盘，不再承载跨机意志。

func resetWkbdCfgForTest(t *testing.T, dir string) {
	t.Helper()
	WkbdConfigPathOverride = filepath.Join(dir, "wkbd-config.json")
	WkbdCfgMu.Lock()
	WkbdCfg, WkbdCfgLoaded = nil, false
	WkbdCfgMu.Unlock()
	t.Cleanup(func() {
		WkbdConfigPathOverride = ""
		WkbdCfgMu.Lock()
		WkbdCfg, WkbdCfgLoaded = nil, false
		WkbdCfgMu.Unlock()
	})
}

func TestWkbdDisplayOrderRoundTrip(t *testing.T) {
	resetWkbdCfgForTest(t, t.TempDir())

	// 未记录 = nil（回放语义：退回 vendor 原序）
	if got := WkbdConfigDisplayOrder(); got != nil {
		t.Fatalf("fresh config: order = %v, want nil", got)
	}

	want := []string{"u1", "u2", "u3"}
	WkbdSetDisplayOrder(want)
	if got := WkbdConfigDisplayOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}

	// 持久层可重载（真身落盘而非仅内存态）
	WkbdCfgMu.Lock()
	WkbdCfg, WkbdCfgLoaded = nil, false
	WkbdCfgMu.Unlock()
	if got := WkbdConfigDisplayOrder(); !reflect.DeepEqual(got, want) {
		t.Fatalf("after reload: order = %v, want %v", got, want)
	}

	// 空表 = 清除
	WkbdSetDisplayOrder(nil)
	if got := WkbdConfigDisplayOrder(); got != nil {
		t.Fatalf("after clear: order = %v, want nil", got)
	}
}

func TestReplayWbDisplayOrderSeedsFromStateJSON(t *testing.T) {
	dir := t.TempDir()
	resetWkbdCfgForTest(t, dir)

	stateFile := filepath.Join(dir, "state.json")
	if err := os.WriteFile(stateFile, []byte(`{"accounts":{},"order":["u1","u2"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	p := wbbridge.PoolNew(stateFile)
	defer p.Close()

	// 升级播种：wkbd 未记录顺序时从 state.json 搬入（零手工迁移）
	replayWbDisplayOrder(p, stateFile)
	if got := WkbdConfigDisplayOrder(); !reflect.DeepEqual(got, []string{"u1", "u2"}) {
		t.Fatalf("seeded order = %v, want [u1 u2]", got)
	}

	// wkbd 已有顺序（同步面）时优先于 state.json，不被覆盖
	WkbdSetDisplayOrder([]string{"u2", "u1"})
	replayWbDisplayOrder(p, stateFile)
	if got := WkbdConfigDisplayOrder(); !reflect.DeepEqual(got, []string{"u2", "u1"}) {
		t.Fatalf("order after replay = %v, want [u2 u1]", got)
	}
}

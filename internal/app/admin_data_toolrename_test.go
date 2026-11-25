package app

import (
	"testing"

	"afree-proxy/internal/gwconfig"
)

// TestClearToolRenameForDataDelete 删除数据端点的 toolRename 复位（2026-11-25
// 补审计遗漏——全局配置开关的出厂复位口径与 strategy/headers 一致）：
// 勾选平台数作清除计数、清除后开关整体关闭、对空配置幂等。
func TestClearToolRenameForDataDelete(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	if err := gwconfig.SetToolRename(gwconfig.ToolRenameConfig{
		Prefix:    "zzt",
		Platforms: map[string]bool{"zen": true, "wb": true, "other": false},
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if n := clearToolRenameForDataDelete(); n != 2 {
		t.Fatalf("cleared platforms = %d, want 2 (zen+wb enabled, other=false)", n)
	}
	if c := gwconfig.GetToolRename(); c.Active() {
		t.Fatalf("toolRename must be disabled after data delete, got %+v", c)
	}
	if n := clearToolRenameForDataDelete(); n != 0 {
		t.Fatalf("second clear on empty config = %d, want 0", n)
	}
}

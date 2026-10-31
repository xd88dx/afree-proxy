package other

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKeyTokensPersist 子平台（ddx / agnes …）的 token 记账必须落盘
// （2026-10-30 修复的延续；2026-10-31 拆分后持久化目标是独立的
// other-state.json，配置文件不再携带记账）。
func TestKeyTokensPersist(t *testing.T) {
	dir := t.TempDir()
	oldPath, oldUsagePath := ConfigPath, UsagePath
	oldCfg := Get()
	ConfigPath = filepath.Join(dir, "other-config.json")
	UsagePath = filepath.Join(dir, "other-state.json")
	defer func() {
		ConfigPath, UsagePath = oldPath, oldUsagePath
		Set(oldCfg)
	}()

	p := Platform{
		Slug:    "t",
		Prefix:  "t",
		BaseURL: "https://example.invalid/v1",
		Keys:    []string{"k1"},
	}
	Set(&Config{Platforms: []Platform{p}})

	// 记账走 RecordKeyTokens，落到痕迹文件（脏标记 + 显式 Flush）
	RecordKeyTokens(PlatformBySlug("t"), "k1", 2)
	usageFlush.Flush()
	raw, err := os.ReadFile(UsagePath)
	if err != nil {
		t.Fatalf("read other usage: %v", err)
	}
	var disk struct {
		Platforms map[string]struct {
			KeyTokens map[string]KeyTokenStats `json:"keyTokens"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("parse other usage: %v", err)
	}
	u, ok := disk.Platforms["t"]
	if !ok || u.KeyTokens["k1"].Total != 2 {
		t.Fatalf("recorded tokens not flushed to usage file: %s", raw)
	}

	// 配置文件不再携带记账（外部监测口径：配置文件无 keyTokens 键）
	cfgRaw, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatalf("read other config: %v", err)
	}
	if strings.Contains(string(cfgRaw), "keyTokens") {
		t.Fatalf("config file must not carry usage traces: %s", cfgRaw)
	}
}

package other

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKeyTokensPersist 子平台（ddx / agnes …）的 token 记账必须随配置落盘
// （2026-10-30 修复）：原先 KeyTokens 标了 json:"-"，只活在内存里，重启后面板
// 上这两个子平台的今日/累计 token 全部清零。
func TestKeyTokensPersist(t *testing.T) {
	dir := t.TempDir()
	oldPath := ConfigPath
	oldCfg := Get()
	ConfigPath = filepath.Join(dir, "other-config.json")
	defer func() {
		ConfigPath = oldPath
		Set(oldCfg)
	}()

	p := Platform{
		Slug:      "t",
		Prefix:    "t",
		BaseURL:   "https://example.invalid/v1",
		Keys:      []string{"k1"},
		KeyTokens: map[string]KeyTokenStats{"k1": {Today: 3, Total: 5}},
	}
	Set(&Config{Platforms: []Platform{p}})

	raw, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatalf("read other config: %v", err)
	}
	if !strings.Contains(string(raw), "keyTokens") {
		t.Fatalf("keyTokens not persisted on disk: %s", raw)
	}

	// 记账走 RecordKeyTokens 时也要能落到盘上（脏标记 + 显式 Flush）
	RecordKeyTokens(PlatformBySlug("t"), "k1", 2)
	tokensFlush.Flush()
	raw, err = os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatalf("re-read other config: %v", err)
	}
	// MarshalIndent 带空格，按值断言而不是按子串
	var disk struct {
		Platforms []struct {
			KeyTokens map[string]KeyTokenStats `json:"keyTokens"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("parse other config: %v", err)
	}
	if len(disk.Platforms) != 1 || disk.Platforms[0].KeyTokens["k1"].Total != 7 {
		t.Fatalf("recorded tokens not flushed to disk: %s", raw)
	}
}

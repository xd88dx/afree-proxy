package app

import (
	"os"
	"path/filepath"
	"testing"
)

// 2026-10-30 抓包：免费层路径从 /inference/v1 变成 /inference/openai/v1。
// 这里锁三件事：新默认值、自定义地址不被碰、目录自愈只在"仍是默认值"时生效
// （用户显式配置的地址绝不能被远端目录改写）。旧默认值的存量迁移已随
// 2026-11-05 兼容清理移除——staleBase 现在就是普通自定义值语义。
const (
	wantBase  = "https://opencode.ai/inference/openai/v1"
	staleBase = "https://opencode.ai/inference/v1"
)

func TestZenAPIBaseIsOpenAIPath(t *testing.T) {
	if zenAPIBase != wantBase {
		t.Fatalf("zenAPIBase = %q, want %q", zenAPIBase, wantBase)
	}
	if defaultZenConfig().BaseURL != wantBase {
		t.Fatalf("default baseURL = %q, want %q", defaultZenConfig().BaseURL, wantBase)
	}
}

// 显式配置的自定义地址（自建中转/抓包代理）绝不能被加载逻辑改掉。
func TestLoadZenConfigKeepsCustomBase(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	path := filepath.Join(dir, "zen-config.json")
	const custom = "https://mirror.example.test/v1"
	if err := os.WriteFile(path, []byte(`{"enabled":true,"key":"public","keys":["public"],"baseURL":"`+custom+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := loadZenConfig()
	if cfg.BaseURL != custom {
		t.Fatalf("custom baseURL rewritten: got %q, want %q", cfg.BaseURL, custom)
	}
}

func TestApplyZenRegistryBase(t *testing.T) {
	// 会经 setZenConfig 写盘并改全局配置：隔离到临时 DATA_DIR，结束后还原。
	t.Setenv("DATA_DIR", testDataDir(t))
	saved := getZenConfig()
	t.Cleanup(func() { setZenConfig(saved) })

	const regAPI = "https://opencode.ai/inference/openai/v1"

	// 旧短路径不再是默认值 → 不被目录改写（自定义值语义）。
	if applyZenRegistryBase(regAPI, staleBase) {
		t.Error("stale base is no longer a default and must not be rewritten")
	}
	// 已经一致 → 不动（不产生多余的写盘）。
	if applyZenRegistryBase(regAPI, wantBase) {
		t.Error("already-correct base must not be rewritten")
	}
	// 用户显式配置 → 绝不覆盖。
	if applyZenRegistryBase(regAPI, "https://mirror.example.test/v1") {
		t.Error("custom base must never be overwritten by the registry")
	}
	// 目录给空值 → 忽略（不可达/字段缺失时不能让基地址变空）。
	if applyZenRegistryBase("", wantBase) {
		t.Error("empty registry api must be ignored")
	}
	// 非 https、非 opencode.ai、或不在 /inference/ 下 → 忽略：目录是远端数据，
	// 不能让一个被篡改的响应把带 Authorization 的流量导去任意主机，也不能把
	// 基地址指回已失效的 /zen/v1（匿名流量在那里恒 403）。
	for _, bad := range []string{
		"http://opencode.ai/inference/openai/v1",
		"https://evil.example/inference/openai/v1",
		"https://opencode.ai.evil.example/inference/openai/v1",
		"https://opencode.ai/zen/v1",
		"https://opencode.ai/",
	} {
		if applyZenRegistryBase(bad, wantBase) {
			t.Errorf("registry api %q must be rejected", bad)
		}
	}
}

// 显式调过的 buffer 绝不能被加载逻辑覆盖（buffer 迁移已随 2026-11-05
// 兼容清理移除，显式值/旧值都按用户配置原样生效）。
func TestLoadZenConfigKeepsCustomCompactBuffer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	path := filepath.Join(dir, "zen-config.json")
	body := `{"enabled":true,"key":"public","keys":["public"],"compaction":{"auto":true,"buffer":45000}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if got := loadZenConfig().Compaction.Buffer; got != 45000 {
		t.Fatalf("custom buffer rewritten to %d, want 45000", got)
	}
}

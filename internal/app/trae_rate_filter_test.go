package app

import (
	"os"
	"strings"
	"testing"
)

// 分域倍率筛选（2026-11-07 拆分，照 wb 79cc3d6c 模式）：
//   - 分域键未配置（nil，旧配置文件形态）→ 回落旧单值 trae2apiModelRateFilter；
//   - 分域键显式配置 → 按域生效（CN 过滤 trae:/solo:，Intl 过滤 trae-intl:/solo-intl:）；
//   - 显式 0 = 该域关（指针区分「未配置」与「关」）。
func TestTrae2apiRateFilterPerRealmFallbackAndSplit(t *testing.T) {
	t.Setenv("DATA_DIR", testDataDir(t))

	// 场景 1：旧配置形态——只有旧单值，双分域键 nil → 两域都回落旧值。
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.Trae2apiModelRateFilter = 0.2
		f.Trae2apiModelRateFilterCN = nil
		f.Trae2apiModelRateFilterIntl = nil
	})
	if got := trae2apiRateFilterThresholdCN(); got != 0.2 {
		t.Fatalf("CN fallback = %v, want 0.2 (legacy single value)", got)
	}
	if got := trae2apiRateFilterThresholdIntl(); got != 0.2 {
		t.Fatalf("Intl fallback = %v, want 0.2 (legacy single value)", got)
	}

	// 场景 2：分域拆分后——CN 显式 0.3、Intl 显式 0（关）；旧单值保持 0.2
	// 但不得再生效（新键优先）。
	cn, intl := 0.3, 0.0
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.Trae2apiModelRateFilterCN = &cn
		f.Trae2apiModelRateFilterIntl = &intl
	})
	if got := trae2apiRateFilterThresholdCN(); got != 0.3 {
		t.Fatalf("CN = %v, want 0.3", got)
	}
	if got := trae2apiRateFilterThresholdIntl(); got != 0 {
		t.Fatalf("Intl = %v, want 0 (explicitly off)", got)
	}
}

// patch 应用路径回归（2026-11-07 拆分时踩坑：新键应用曾被插在旧键 if 块内，
// 面板只发新键时整块跳过、保存静默无效）——分域键独立于旧键生效。
func TestTrae2apiRateFilterPatchPerRealmKeyIndependent(t *testing.T) {
	t.Setenv("DATA_DIR", testDataDir(t))
	mutateProxyConfigFile(func(f *proxyConfigFileData) {
		f.Trae2apiModelRateFilter = 0
		f.Trae2apiModelRateFilterCN = nil
		f.Trae2apiModelRateFilterIntl = nil
	})

	traeApplyRateFilterPatch(traeRateFilterPatch{
		RateFilterCN:   floatPtr(0.3),
		RateFilterIntl: floatPtr(0),
	})
	if got := trae2apiRateFilterThresholdCN(); got != 0.3 {
		t.Fatalf("CN after patch = %v, want 0.3 (new key must work without the legacy key)", got)
	}
	if got := trae2apiRateFilterThresholdIntl(); got != 0 {
		t.Fatalf("Intl after patch = %v, want 0", got)
	}

	// 旧单键兼容：双写两域
	traeApplyRateFilterPatch(traeRateFilterPatch{RateFilter: floatPtr(0.5)})
	if got := trae2apiRateFilterThresholdCN(); got != 0.5 {
		t.Fatalf("CN after legacy patch = %v, want 0.5 (dual-write)", got)
	}
	if got := trae2apiRateFilterThresholdIntl(); got != 0.5 {
		t.Fatalf("Intl after legacy patch = %v, want 0.5 (dual-write)", got)
	}
}

func floatPtr(f float64) *float64 { return &f }

// 单值 trae2apiModelRateFilter 的一次性迁移（2026-11-15，与 WB 的
// migrateModelRateFilterSplit 同语义）：仅两域均未显式配置时迁入并清除单值；
// 两域已配置时不触碰（显式值优先，兜底读取分支也不再被单值命中）。
func TestTrae2apiRateFilterSingletonMigrate(t *testing.T) {
	reset := func(t *testing.T, content string) string {
		dir := t.TempDir()
		fp := dir + "/proxy-config.json"
		if content != "" {
			if err := os.WriteFile(fp, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		old := proxyConfigPathOverride
		proxyConfigPathOverride = fp
		t.Cleanup(func() { proxyConfigPathOverride = old })
		return fp
	}

	// 场景 1：只有单值 → 迁入两域、单值清零、落盘不再含单值。
	fp := reset(t, `{"strategy":"round_robin","trae2apiModelRateFilter":0.1}`)
	proxyFileMu.Lock()
	f := loadProxyConfigFileLocked()
	if f.Trae2apiModelRateFilterCN == nil || *f.Trae2apiModelRateFilterCN != 0.1 ||
		f.Trae2apiModelRateFilterIntl == nil || *f.Trae2apiModelRateFilterIntl != 0.1 ||
		f.Trae2apiModelRateFilter != 0 {
		proxyFileMu.Unlock()
		t.Fatalf("单值应迁入两域并清零，得到 cn=%v intl=%v single=%v",
			f.Trae2apiModelRateFilterCN, f.Trae2apiModelRateFilterIntl, f.Trae2apiModelRateFilter)
	}
	proxyFileMu.Unlock()
	if raw, err := os.ReadFile(fp); err != nil || strings.Contains(string(raw), "trae2apiModelRateFilter\":0.1") {
		t.Fatalf("落盘文件应无单值（err=%v raw=%s）", err, raw)
	}

	// 场景 2：两域已显式配置 → 单值不迁移（显式值优先）。
	reset(t, `{"strategy":"round_robin","trae2apiModelRateFilter":0.2,"trae2apiModelRateFilterCN":0.05}`)
	proxyFileMu.Lock()
	f2 := loadProxyConfigFileLocked()
	proxyFileMu.Unlock()
	if f2.Trae2apiModelRateFilter != 0.2 || *f2.Trae2apiModelRateFilterCN != 0.05 {
		t.Fatalf("两域已配置时单值应原样保留待读取兜底，得到 single=%v cn=%v", f2.Trae2apiModelRateFilter, f2.Trae2apiModelRateFilterCN)
	}
}

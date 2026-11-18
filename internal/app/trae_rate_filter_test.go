package app

import (
	"os"
	"strings"
	"testing"
)

// 分域倍率筛选（2026-11-07 拆分，2026-11-17 迁入 trae-config.json）：
//   - 分域键 nil → 阈值 0（不筛，显式 0 同语义）；
//   - 分域键显式配置 → 按域生效（CN 过滤 tacn:，Intl 过滤 tagb:）；
//   - 旧单值与 proxy/afree 旧位置键均不再读取（无兼容迁移，2026-11-17 用户定）。
func TestTrae2apiRateFilterPerRealm(t *testing.T) {
	t.Setenv("DATA_DIR", testDataDir(t))

	// 场景 1：未配置 → 两域阈值 0（不筛）。
	if got := trae2apiRateFilterThresholdCN(); got != 0 {
		t.Fatalf("CN unconfigured = %v, want 0", got)
	}
	if got := trae2apiRateFilterThresholdIntl(); got != 0 {
		t.Fatalf("Intl unconfigured = %v, want 0", got)
	}

	// 场景 2：分域配置——CN 显式 0.3、Intl 显式 0（关）。
	cn, intl := 0.3, 0.0
	traeApplyRateFilterPatch(traeRateFilterPatch{RateFilterCN: &cn, RateFilterIntl: &intl})
	if got := trae2apiRateFilterThresholdCN(); got != 0.3 {
		t.Fatalf("CN = %v, want 0.3", got)
	}
	if got := trae2apiRateFilterThresholdIntl(); got != 0 {
		t.Fatalf("Intl = %v, want 0 (explicitly off)", got)
	}

	// 场景 3：落盘位置——trae-config.json（平台配置独立原则的实测锚点）。
	traeRaw, err := os.ReadFile(traeAfreeConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(traeRaw), `"modelRateFilterCN": 0.3`) {
		t.Fatalf("rate filter must persist under trae-config.json: %s", traeRaw)
	}
}

// patch nil 语义：两个键都不提交 = 不改动（面板「放弃修改」路径）。
func TestTrae2apiRateFilterPatchNilIsNoop(t *testing.T) {
	t.Setenv("DATA_DIR", testDataDir(t))
	cn := 0.3
	traeApplyRateFilterPatch(traeRateFilterPatch{RateFilterCN: &cn})
	traeApplyRateFilterPatch(traeRateFilterPatch{}) // nil patch 不得清掉已有值
	if got := trae2apiRateFilterThresholdCN(); got != 0.3 {
		t.Fatalf("CN after nil patch = %v, want 0.3 (nil patch is a no-op)", got)
	}
}

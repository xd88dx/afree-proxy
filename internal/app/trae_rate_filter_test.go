package app

import (
	"os"
	"strings"
	"testing"
)

// 分域倍率筛选（2026-11-07 拆分，2026-11-16 迁入 afree trae 子域）：
//   - 分域键 nil → 阈值 0（不筛，显式 0 同语义）；
//   - 分域键显式配置 → 按域生效（CN 过滤 tacn:，Intl 过滤 tagb:）；
//   - 旧单值 trae2apiModelRateFilter 已删除：proxy-config.json 旧键由
//     migrateTrae2apiConfigFromProxyFile 一次性折入双键（见迁移测试）。
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

	// 场景 3：落盘位置——afree-config.json 的 trae 子域，proxy-config.json
	// 不含倍率键（平台配置独立原则的实测锚点）。
	afreeRaw, err := os.ReadFile(globalConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(afreeRaw), `"modelRateFilterCN": 0.3`) {
		t.Fatalf("rate filter must persist under afree trae section: %s", afreeRaw)
	}
	proxyRaw, err := os.ReadFile(proxyConfigPath())
	if err == nil && strings.Contains(string(proxyRaw), "trae2apiModelRateFilter") {
		t.Fatalf("proxy-config.json must not carry trae rate filter keys: %s", proxyRaw)
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

// 一次性迁移（2026-11-16）：proxy-config.json 的 trae2api* 旧段迁入
// afree-config.json 的 trae 子域并从代理文件清除；幂等——afree 侧已有值的
// 键不被旧值覆盖。
func TestTrae2apiConfigMigrateFromProxyFile(t *testing.T) {
	reset := func(t *testing.T, proxyContent string) (proxyPath, afreePath string) {
		dir := t.TempDir()
		proxyPath = dir + "/proxy-config.json"
		afreePath = dir + "/afree-config.json"
		if proxyContent != "" {
			if err := os.WriteFile(proxyPath, []byte(proxyContent), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		oldProxy, oldGlobal := proxyConfigPathOverride, globalConfigPathOverride
		proxyConfigPathOverride = proxyPath
		globalConfigPathOverride = afreePath
		// 全局缓存跨测试泄漏防线：前序测试可能已加载过自己的 afree-config
		// （含各自的 trae 段/黑名单 fixture），不重置会让本测试的迁移 fn 在
		// 缓存上判断「afree 侧已有值」而跳过迁移。
		globalCfgMu.Lock()
		globalCfgLoaded = false
		globalCfg = nil
		globalCfgMu.Unlock()
		t.Cleanup(func() {
			proxyConfigPathOverride = oldProxy
			globalConfigPathOverride = oldGlobal
			globalCfgMu.Lock()
			globalCfgLoaded = false
			globalCfg = nil
			globalCfgMu.Unlock()
		})
		return proxyPath, afreePath
	}

	t.Run("legacy proxy keys migrate into afree trae section", func(t *testing.T) {
		proxyPath, afreePath := reset(t, `{
			"strategy":"round_robin",
			"trae2apiBindings":{"trae-cn-1":{"main":"socks5://a:1"}},
			"trae2apiRouting":{"trae-cn-1":true},
			"trae2apiDisplayOrder":["trae-cn-1"],
			"trae2apiModelRateFilter":0.1
		}`)
		migrateTrae2apiConfigFromProxyFile()

		afreeRaw, err := os.ReadFile(afreePath)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"trae"`, `"bindings"`, `"trae-cn-1"`, `"routing"`, `"displayOrder"`, `"modelRateFilterCN": 0.1`, `"modelRateFilterIntl": 0.1`} {
			if !strings.Contains(string(afreeRaw), want) {
				t.Fatalf("afree trae section missing %s: %s", want, afreeRaw)
			}
		}
		proxyRaw, _ := os.ReadFile(proxyPath)
		if strings.Contains(string(proxyRaw), "trae2api") {
			t.Fatalf("legacy keys must be gone from proxy-config.json: %s", proxyRaw)
		}
	})

	t.Run("idempotent — afree values win on re-run", func(t *testing.T) {
		proxyPath, _ := reset(t, `{
			"strategy":"round_robin",
			"trae2apiRouting":{"trae-cn-old":true},
			"trae2apiModelRateFilter":0.9
		}`)
		migrateTrae2apiConfigFromProxyFile()
		// 模拟新版本已写入新值后、旧 proxy 文件又被外部带回（如旧备份还原）
		// 再启动：先改 afree 侧值、重写 proxy 旧键、再重跑迁移——旧值不得
		// 覆盖新值。
		traeRoutingMu.Lock()
		traeRoutingLoaded = false
		traeRouting = map[string]bool{}
		traeRoutingMu.Unlock()
		globalCfgMu.Lock()
		globalCfgLoaded = false
		globalCfg = nil
		globalCfgMu.Unlock()
		// afree 侧直接落新值（整体替换语义）：routing 只留新 uid，倍率改小。
		rate := 0.05
		mutateGlobalConfig(func(g *globalConfigData) {
			t := traeGlobalSection(g)
			t.Routing = map[string]bool{"trae-cn-new": false}
			t.ModelRateFilterCN = &rate
		})
		if err := os.WriteFile(proxyPath, []byte(`{"strategy":"round_robin","trae2apiRouting":{"trae-cn-old":true},"trae2apiModelRateFilter":0.9}`), 0o600); err != nil {
			t.Fatal(err)
		}
		migrateTrae2apiConfigFromProxyFile()

		globalCfgMu.Lock()
		g := loadGlobalConfigLocked()
		routing := g.Trae.Routing
		cn := g.Trae.ModelRateFilterCN
		globalCfgMu.Unlock()
		if _, ok := routing["trae-cn-old"]; ok {
			t.Fatalf("stale legacy routing must not merge back: %v", routing)
		}
		if len(routing) != 1 || routing["trae-cn-new"] || cn == nil || *cn != 0.05 {
			t.Fatalf("fresh values must survive re-migration: routing=%v cn=%v", routing, cn)
		}
		proxyRaw, _ := os.ReadFile(proxyPath)
		if strings.Contains(string(proxyRaw), "trae2api") {
			t.Fatalf("re-migration must clean proxy keys again: %s", proxyRaw)
		}
	})
}

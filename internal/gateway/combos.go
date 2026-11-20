package gateway

import (
	"afree-proxy/internal/freepi"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/workbuddy"

	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	CombosMu     sync.Mutex
	combosLoaded bool
	combosList   []*gwconfig.Combo
)

var comboIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,63}$`)

func loadCombosLocked() {
	if combosLoaded {
		return
	}
	combosLoaded = true
	globalCfgMu.Lock()
	g := loadGlobalConfigLocked()
	list := append([]*gwconfig.Combo(nil), g.Combos...)
	globalCfgMu.Unlock()
	needsSave := false
	for _, c := range list {
		if c != nil && c.ID != "" && c.Target != "" {
			// 存量迁移（2026-11-07 合并域化后）：trae 平台的 combo target 若是
			// 废除的旧四前缀 trae:/solo:/trae-intl:/solo-intl:，归一化为合并域
			// 对外 id（tacn:/tagb:），落盘一次。intl 域旧 target 在 intl 关闭时
			// 不归一化（解析不到，避免误改）。
			if c.Platform == "trae" {
				migrated, ok := migrateTraeComboTarget(c.Target)
				if ok {
					c.Target = migrated
					needsSave = true
				}
			}
			combosList = append(combosList, c)
		}
	}
	if needsSave {
		saveCombosLocked()
	}
}

// migrateTraeComboTarget 把 combo 中 trae 平台的旧前缀 target 归一化为合并域
// 对外 id（2026-11-07 合并域化后存量 combo 迁移，调用方持有 CombosMu）。
// 已是合并域形态或解析不到时返回原值与 false。
func migrateTraeComboTarget(target string) (string, bool) {
	legacy := map[string]string{
		"trae": traeMergedCN, "solo": traeMergedCN,
		"trae-intl": traeMergedIntl, "solo-intl": traeMergedIntl,
	}
	for prefix, merged := range legacy {
		p := prefix + ":"
		if strings.HasPrefix(target, p) {
			bare := strings.TrimPrefix(target, p)
			if bare == "" {
				return target, false
			}
			if merged == traeMergedIntl && !trae2apiIntlEnabled() {
				return target, false
			}
			migrated := merged + ":" + bare
			if trae2apiModelExists(migrated) {
				return migrated, true
			}
			return target, false
		}
	}
	return target, false
}

func saveCombosLocked() {
	cp := append([]*gwconfig.Combo(nil), combosList...)
	mutateGlobalConfig(func(g *globalConfigData) { g.Combos = cp })
}

// listCombos 返回全部 combo（副本语义，只读使用）。
func listCombos() []*gwconfig.Combo {
	CombosMu.Lock()
	defer CombosMu.Unlock()
	loadCombosLocked()
	out := make([]*gwconfig.Combo, len(combosList))
	copy(out, combosList)
	return out
}

// resolveCombo 查找 combo；请求路由与 /v1/models 展示用。
func resolveCombo(id string) *gwconfig.Combo {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	CombosMu.Lock()
	defer CombosMu.Unlock()
	loadCombosLocked()
	for _, c := range combosList {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// comboPlatformValid 自定义别名的平台白名单（六平台）。面板保存与备份导入
// 共用同一份口径——新平台接入时两处一起改。
func comboPlatformValid(platform string) bool {
	switch platform {
	case "cline", "zen", "workbuddy", "trae", "freepi":
		return true
	}
	return false
}

// validateCombo 保存前校验：
//   - ID 格式合法（URL 安全）且不与任何真实模型 ID 冲突
//   - platform ∈ 六平台白名单（comboPlatformValid）
//   - target 必须存在于对应平台的模型列表（严格同平台，禁止跨平台选择）
func validateCombo(id, platform, target string) error {
	if !comboIDRe.MatchString(id) {
		return fmt.Errorf("invalid combo id %q: use 2-64 chars, letters/digits/._- and start with letter or digit", id)
	}
	if !comboPlatformValid(platform) {
		return fmt.Errorf("invalid platform %q: must be cline, zen, workbuddy or trae", platform)
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("target model is required")
	}
	// ID 不得与真实模型冲突。comboIDRe 的语法不含 ':'，因此带前缀平台
	// （zen:/tacn:/tagb:）的 ID 在函数入口就被拒，永远不会走到
	// 这里——只保留真正可达的两类：Cline 目录（裸名与 cline-free/ 形态）与 zen
	// 裸名别名（存量配置仍认裸 ID，见 resolveZenModel）。
	if modelInClineList(id) {
		return fmt.Errorf("combo id %q conflicts with a real cline model", id)
	}
	if _, isZen := resolveZenModel(id); isZen {
		return fmt.Errorf("combo id %q conflicts with a real zen model", id)
	}
	if resolveCombo(id) != nil {
		return fmt.Errorf("combo id %q already exists", id)
	}

	switch platform {
	case "cline":
		if !modelInClineList(target) {
			return fmt.Errorf("target %q is not in the cline model list (cross-platform selection is not allowed)", target)
		}
	case "zen":
		if _, ok := resolveZenFreeModel(target); !ok {
			return fmt.Errorf("target %q is not a free zen model (cross-platform selection is not allowed)", target)
		}
	case "workbuddy":
		if !shouldServeWorkBuddy(target) {
			return fmt.Errorf("target %q is not a WorkBuddy model", target)
		}
		found := false
		for _, m := range (*workbuddy.WorkbuddySub).ModelList() {
			if id, ok := m["id"].(string); ok && id == target {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("target %q is not in the current WorkBuddy model list", target)
		}
	case "trae":
		if !trae2apiModelExists(target) {
			return fmt.Errorf("target %q is not a known Trae model (cross-platform selection is not allowed)", target)
		}
	case "freepi":
		if _, ok := freepi.ResolveModel(target); !ok {
			return fmt.Errorf("target %q is not in the FreePI catalog (cross-platform selection is not allowed)", target)
		}
	}
	return nil
}

// addCombo 校验并持久化新 combo。
func addCombo(id, platform, target string, useProxies bool) (*gwconfig.Combo, error) {
	id = strings.TrimSpace(id)
	target = strings.TrimSpace(target)
	if err := validateCombo(id, platform, target); err != nil {
		return nil, err
	}
	c := &gwconfig.Combo{ID: id, Platform: platform, Target: target, UseProxies: useProxies, CreatedAt: time.Now()}
	CombosMu.Lock()
	defer CombosMu.Unlock()
	loadCombosLocked()
	// validateCombo 里的存在性检查在 CombosMu 之外，并发 addCombo 可能双双通过；
	// 这里在锁内重查一次，保证同一 ID 只能存在一条
	for _, e := range combosList {
		if e.ID == id {
			return nil, fmt.Errorf("combo id %q already exists", id)
		}
	}
	combosList = append(combosList, c)
	saveCombosLocked()
	return c, nil
}

// deleteCombo 按 ID 删除，返回是否删除了条目。
func deleteCombo(id string) bool {
	id = strings.TrimSpace(id)
	CombosMu.Lock()
	defer CombosMu.Unlock()
	loadCombosLocked()
	for i, c := range combosList {
		if c.ID == id {
			combosList = append(combosList[:i], combosList[i+1:]...)
			saveCombosLocked()
			return true
		}
	}
	return false
}

// ============ 管理 API ============

// GET /admin/api/combos
// AddCombo / DeleteCombo 导出面（装配根面板 handler 消费）。
func AddCombo(id, platform, target string, useProxies bool) (*gwconfig.Combo, error) {
	return addCombo(id, platform, target, useProxies)
}

func DeleteCombo(id string) bool { return deleteCombo(id) }

// ---- 导出原语（装配根备份/清空数据与测试消费）----

// ClearCombosForDataDelete 清空全部自定义别名（内存 + 落盘），返回清除条数。
func ClearCombosForDataDelete() int {
	CombosMu.Lock()
	defer CombosMu.Unlock()
	loadCombosLocked()
	n := len(combosList)
	combosList = nil
	combosLoaded = true
	saveCombosLocked()
	return n
}

// ImportCombosBackup 备份导入的 combos 段：逐条校验（ID 形态/平台白名单/
// trae 旧前缀迁移/target 非空）后整表替换，返回导入条数。
func ImportCombosBackup(in []*gwconfig.Combo) int {
	CombosMu.Lock()
	defer CombosMu.Unlock()
	valid := make([]*gwconfig.Combo, 0, len(in))
	for _, c := range in {
		if c == nil || !comboIDRe.MatchString(c.ID) {
			continue
		}
		if !comboPlatformValid(c.Platform) {
			continue
		}
		if c.Platform == "trae" {
			if migrated, ok := migrateTraeComboTarget(c.Target); ok {
				c = &gwconfig.Combo{ID: c.ID, Platform: c.Platform, Target: migrated,
					UseProxies: c.UseProxies, CreatedAt: c.CreatedAt}
			}
		}
		if c.Target == "" {
			continue
		}
		valid = append(valid, c)
	}
	combosList = valid
	combosLoaded = true
	saveCombosLocked()
	return len(valid)
}

// CombosSnapshotForTest / SetCombosForTest 测试白盒（快照读/整表写）。
func CombosSnapshotForTest() ([]*gwconfig.Combo, bool) {
	CombosMu.Lock()
	defer CombosMu.Unlock()
	return combosList, combosLoaded
}

func SetCombosForTest(list []*gwconfig.Combo, loaded bool) {
	CombosMu.Lock()
	defer CombosMu.Unlock()
	combosList, combosLoaded = list, loaded
}

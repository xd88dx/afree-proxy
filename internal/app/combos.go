package app

import (
	"afree-proxy/internal/amd"
	openrouter "afree-proxy/internal/openrouter"
	"afree-proxy/internal/tokenharbor"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Combo 仪表盘自定义的别名模型：/v1 请求 model 命中 combo ID 时，
// 上游改写为同平台的 target 模型。严格同平台：cline/zen/workbuddy/openrouter/amd/tokenharbor
// 只能选各自平台当前模型列表中的模型，保存时校验。
type Combo struct {
	ID         string    `json:"id"`
	Platform   string    `json:"platform"` // cline | zen | workbuddy | openrouter | amd
	Target     string    `json:"target"`
	UseProxies bool      `json:"useProxies,omitempty"` // 该别名的上游调用走出口代理池（round-robin）
	CreatedAt  time.Time `json:"createdAt"`
}

var (
	combosMu     sync.Mutex
	combosLoaded bool
	combosList   []*Combo
)

var comboIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,63}$`)

func loadCombosLocked() {
	if combosLoaded {
		return
	}
	combosLoaded = true
	globalCfgMu.Lock()
	g := loadGlobalConfigLocked()
	list := append([]*Combo(nil), g.Combos...)
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
// 对外 id（2026-11-07 合并域化后存量 combo 迁移，调用方持有 combosMu）。
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
	cp := append([]*Combo(nil), combosList...)
	mutateGlobalConfig(func(g *globalConfigData) { g.Combos = cp })
}

// listCombos 返回全部 combo（副本语义，只读使用）。
func listCombos() []*Combo {
	combosMu.Lock()
	defer combosMu.Unlock()
	loadCombosLocked()
	out := make([]*Combo, len(combosList))
	copy(out, combosList)
	return out
}

// resolveCombo 查找 combo；请求路由与 /v1/models 展示用。
func resolveCombo(id string) *Combo {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	combosMu.Lock()
	defer combosMu.Unlock()
	loadCombosLocked()
	for _, c := range combosList {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// comboPlatformValid 自定义别名的平台白名单（七平台）。面板保存与备份导入
// 共用同一份口径——新平台接入时两处一起改。
func comboPlatformValid(platform string) bool {
	switch platform {
	case "cline", "zen", "workbuddy", "openrouter", "amd", "tokenharbor", "trae":
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
		return fmt.Errorf("invalid platform %q: must be cline, zen, workbuddy, openrouter, amd, tokenharbor or trae", platform)
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("target model is required")
	}
	// ID 不得与真实模型冲突（Cline 表带/不带 cline-free/ 前缀都算、zen 前缀
	// 形态、OpenRouter 前缀形态）
	if modelInClineList(id) {
		return fmt.Errorf("combo id %q conflicts with a real cline model", id)
	}
	if _, isZen := resolveZenModel(id); isZen {
		return fmt.Errorf("combo id %q conflicts with a real zen model", id)
	}
	if openrouter.HasPrefix(id) {
		if _, ok := openrouter.Resolve(id); ok {
			return fmt.Errorf("combo id %q conflicts with a real openrouter model", id)
		}
	}
	if amd.HasPrefix(id) {
		if _, ok := amd.Resolve(id); ok {
			return fmt.Errorf("combo id %q conflicts with a real amd model", id)
		}
	}
	if tokenharbor.HasPrefix(id) {
		if _, ok := tokenharbor.Resolve(id); ok {
			return fmt.Errorf("combo id %q conflicts with a real tokenharbor model", id)
		}
	}
	if trae2apiPlatformModel(id) {
		if trae2apiModelExists(id) {
			return fmt.Errorf("combo id %q conflicts with a real Trae model", id)
		}
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
		for _, m := range workbuddySub.modelList() {
			if id, ok := m["id"].(string); ok && id == target {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("target %q is not in the current WorkBuddy model list", target)
		}
	case "openrouter":
		if _, ok := openrouter.Resolve(target); !ok {
			return fmt.Errorf("target %q is not a free openrouter model (cross-platform selection is not allowed)", target)
		}
	case "amd":
		if _, ok := amd.Resolve(target); !ok {
			return fmt.Errorf("target %q is not an AMDCloud model (cross-platform selection is not allowed)", target)
		}
	case "tokenharbor":
		if _, ok := tokenharbor.Resolve(target); !ok {
			return fmt.Errorf("target %q is not a free TokenHarbor model (cross-platform selection is not allowed)", target)
		}
	case "trae":
		if !trae2apiModelExists(target) {
			return fmt.Errorf("target %q is not a known Trae model (cross-platform selection is not allowed)", target)
		}
	}
	return nil
}

// addCombo 校验并持久化新 combo。
func addCombo(id, platform, target string, useProxies bool) (*Combo, error) {
	id = strings.TrimSpace(id)
	target = strings.TrimSpace(target)
	if err := validateCombo(id, platform, target); err != nil {
		return nil, err
	}
	c := &Combo{ID: id, Platform: platform, Target: target, UseProxies: useProxies, CreatedAt: time.Now()}
	combosMu.Lock()
	defer combosMu.Unlock()
	loadCombosLocked()
	// validateCombo 里的存在性检查在 combosMu 之外，并发 addCombo 可能双双通过；
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
	combosMu.Lock()
	defer combosMu.Unlock()
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
func handleCombosList(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"combos": listCombos()}})
}

// POST /admin/api/combos  body: { id, platform, target }
func handleComboCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		ID         string `json:"id"`
		Platform   string `json:"platform"`
		Target     string `json:"target"`
		UseProxies bool   `json:"useProxies"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	c, err := addCombo(req.ID, req.Platform, req.Target, req.UseProxies)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	log.Printf("combo created: %s -> %s/%s", c.ID, c.Platform, c.Target)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "combo created", Data: c})
}

// POST /admin/api/combos/delete  body: { id }
func handleComboDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if !deleteCombo(req.ID) {
		writeAPI(w, http.StatusNotFound, apiResponse{Error: "combo not found: " + req.ID})
		return
	}
	log.Printf("combo deleted: %s", req.ID)
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: "combo deleted"})
}

package zen

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ============ 已弃用模型的自动迁移（410 replacement + 400 unavailable） ============
//
// 上游证据（2026-10-09 实测）：
//   - mimo-v2.5-free → 410，body 带 {"replacement":"mimo-v2.6-flash-free"}；
//   - qwen3.6-plus-free → 400 "Model is unavailable"。
// 前者：把请求透明改走继任模型（单次重试），别名持久化到
// DATA_DIR/.zen-model-aliases.json，重启后依然生效；后者：标记"死亡到下次
// 目录同步"，路由层直接拒绝，不再每次白烧一个上游请求。
//
// 别名只做"原始 ID 解析失败后"的兜底：真实模型永远优先于别名，继任模型
// 上线/下线不会被别名表遮蔽。

type ZenDepEntry struct {
	Replacement string `json:"replacement"`
	Since       int64  `json:"since"` // unix 秒，迁移发生时间
}

var (
	DepMu         sync.Mutex
	DepAliasesVar = map[string]*ZenDepEntry{} // 已弃用 ID -> 继任模型
	zenDepLoaded  bool
	zenDepPath    string
)

func LoadZenDepsLocked() {
	if zenDepLoaded {
		return
	}
	zenDepLoaded = true
	m := func() map[string]*ZenDepEntry {
		zenFileMu.Lock()
		defer zenFileMu.Unlock()
		return LoadZenConfigFileLocked().ModelAliases
	}()
	loaded := 0
	rejected := 0
	for k, e := range m {
		if e == nil || e.Replacement == "" {
			continue
		}
		fk, fr := NormalizeZenModelID(k), NormalizeZenModelID(e.Replacement)
		if fr == "" || fk == fr {
			// 自环（A→A）：ResolveZenModel 的递归兜底会立刻再查同一个 id。
			log.Printf("zen model alias %q -> %q is a self-reference, dropped", k, e.Replacement)
			rejected++
			continue
		}
		DepAliasesVar[k] = e
		loaded++
	}
	// 环检测：A→B→A 这类链会让 ResolveZenModel 来回递归。那里已有深度上限
	// 兜底（不会崩），但把坏数据留在内存里等于每个请求白走一趟递归并刷日志，
	// 所以在装载时就把落在环上的起始条目丢掉。
	for k := range DepAliasesVar {
		seen := map[string]bool{}
		cur := NormalizeZenModelID(k)
		cyclic := false
		for i := 0; i <= ZenAliasMaxDepth; i++ {
			if seen[cur] {
				cyclic = true
				break
			}
			seen[cur] = true
			e, ok := DepAliasesVar[cur]
			if !ok {
				break
			}
			cur = NormalizeZenModelID(e.Replacement)
		}
		if cyclic {
			log.Printf("zen model alias chain starting at %q is cyclic, dropped", k)
			delete(DepAliasesVar, k)
			rejected++
		}
	}
	if loaded > 0 {
		log.Printf("zen model aliases loaded: %d mapping(s)", len(DepAliasesVar))
	}
	if rejected > 0 {
		log.Printf("zen model aliases: %d invalid mapping(s) dropped", rejected)
	}
}

// SaveZenDepsLocked 持久化别名表（调用方持有 DepMu）。只覆盖 modelAliases 段。
func SaveZenDepsLocked() {
	next := make(map[string]*ZenDepEntry, len(DepAliasesVar))
	for k, v := range DepAliasesVar {
		next[k] = v
	}
	MutateZenConfigFile(func(f *ZenConfigFileData) {
		f.ModelAliases = next
	})
}

// ZenDeprecatedReplacement 返回已弃用模型的继任者（无映射时返回 ""）。
// 键统一规范化（小写去 opencode/ 前缀），与 ZenMarkModelDead 同口径。
func ZenDeprecatedReplacement(id string) string {
	id = NormalizeZenModelID(id)
	DepMu.Lock()
	defer DepMu.Unlock()
	LoadZenDepsLocked()
	if e := DepAliasesVar[id]; e != nil {
		return e.Replacement
	}
	return ""
}

// ZenRecordDeprecation 记录并持久化一条 410 迁移映射；继任者已弃用时返回 ""
// （调用方放弃迁移）。别名链不允许成环/成串：继任者若本身也是别名，返回 ""
// 放弃——一次请求最多迁移一跳，防止 A→B→C 无限重试。
// 两侧 ID 均规范化为小写去前缀，保证与查询侧（ZenDeprecatedReplacement）
// 同键；否则 chat 路径传入 "opencode/x" 会写进去、按 "x" 查却查不到。
func ZenRecordDeprecation(deadID, replacement string) string {
	deadID = NormalizeZenModelID(deadID)
	replacement = NormalizeZenModelID(replacement)
	if replacement == "" || replacement == deadID {
		return ""
	}
	DepMu.Lock()
	defer DepMu.Unlock()
	LoadZenDepsLocked()
	if e := DepAliasesVar[replacement]; e != nil {
		log.Printf("zen deprecation: replacement %q is itself deprecated (-> %s); refusing to chain", replacement, e.Replacement)
		return ""
	}
	if e := DepAliasesVar[deadID]; e != nil && e.Replacement == replacement {
		return replacement // 已知映射，幂等
	}
	DepAliasesVar[deadID] = &ZenDepEntry{Replacement: replacement, Since: time.Now().Unix()}
	SaveZenDepsLocked()
	log.Printf("zen deprecation: %s -> %s (persisted)", deadID, replacement)
	return replacement
}

// zenModelDeadUntilSync 400 "Model is unavailable" 的死亡标记。每次成功目录
// 同步清空（SyncZenModels 用 ZenClearDeadModels）；另有 ZenDeadTTL 兜底——
// 目录同步可能长期失败（registry 不可达），只靠同步清空会让模型被永久钉死。
var ZenDeadModels = map[string]int64{} // model id -> 标记时间(unix 秒)

// ZenDeadTTL 死亡标记最长存活时长。超过后自动失效并允许再次探测上游，
// 避免同步长期失败时一个瞬时 400 把模型永久下线。
const ZenDeadTTL = time.Hour

// ZenMarkModelDead 标记模型死亡（记下时间，供 TTL 判定）。键规范化，
// 与查询侧同口径。
func ZenMarkModelDead(id string) {
	id = NormalizeZenModelID(id)
	if id == "" {
		return
	}
	DepMu.Lock()
	ZenDeadModels[id] = time.Now().Unix()
	DepMu.Unlock()
}

// ZenModelDead 模型是否仍在死亡 TTL 内（路由层据此跳过，不再白烧请求）。
// 过期条目就地删除并返回 false，无需等待目录同步。
func ZenModelDead(id string) bool {
	id = NormalizeZenModelID(id)
	if id == "" {
		return false
	}
	DepMu.Lock()
	defer DepMu.Unlock()
	since, ok := ZenDeadModels[id]
	if !ok {
		return false
	}
	if time.Now().Unix()-since > int64(ZenDeadTTL/time.Second) {
		delete(ZenDeadModels, id)
		return false
	}
	return true
}

// ZenClearDeadModels 目录同步成功后清空死亡标记（上游列表已刷新，
// 之前的 400 可能只是目录滞后）。
func ZenClearDeadModels() {
	DepMu.Lock()
	ZenDeadModels = map[string]int64{}
	DepMu.Unlock()
}

// ZenHandleDeprecatedModel 上游返回 410/400 后的弃用处理。返回 true 表示
// 已完成迁移，调用方应就地重试（params["model"] 已改写为继任模型）。
// pinned（面板探测）与已迁移过一次的请求不迁移——探测结论只属于这次点击，
// 迁移标记防止别名链无限重试。400 "unavailable" 形态永远返回 false（只标记）。
func ZenHandleDeprecatedModel(status int, body, modelID string, params map[string]any, pinned, alreadyRemapped bool) bool {
	repl := ZenDeprecationFromError(status, body)
	if status == http.StatusBadRequest {
		// 400 + "Model is unavailable"：标记死亡，本次请求照常失败（该模型
		// 没有可用继任信号），路由层后续直接拒绝。
		if repl == "" && IsModelUnavailableBody(body) {
			if !pinned {
				ZenMarkModelDead(modelID)
				log.Printf("zen model unavailable: %s marked dead until next catalog sync (upstream 400)", modelID)
			}
		}
		return false
	}
	// status == 410
	if pinned || alreadyRemapped || repl == "" {
		return false
	}
	// 防环：继任者本身已是弃用别名时拒绝链式迁移。
	if ZenDeprecatedReplacement(repl) != "" {
		log.Printf("zen deprecation: %s -> %s refused (replacement is itself deprecated)", modelID, repl)
		return false
	}
	if _, ok := ResolveZenFreeModel(repl); !ok {
		log.Printf("zen deprecation: %s -> %s refused (replacement unknown or not free)", modelID, repl)
		return false
	}
	if ZenRecordDeprecation(modelID, repl) == "" {
		return false
	}
	params["model"] = repl
	return true
}

// IsModelUnavailableBody 上游 400 错误体是否呈"模型不可用"特征
// （实测 qwen3.6-plus-free：400 "Model is unavailable"；另容忍 ModelDeprecated）。
func IsModelUnavailableBody(body string) bool {
	low := strings.ToLower(body)
	return strings.Contains(low, "model is unavailable") || strings.Contains(low, "modeldeprecated")
}

// PruneZenDeadAndAliases 配置/目录变更后清理指向不存在模型的别名。
// 死亡标记整体清空（调用点在同步成功后）。
func PruneZenDeadAndAliases(valid map[string]bool) {
	DepMu.Lock()
	defer DepMu.Unlock()
	n := 0
	for k := range ZenDeadModels {
		if !valid[k] {
			delete(ZenDeadModels, k)
			n++
		}
	}
	for k, e := range DepAliasesVar {
		if !valid[k] && !valid[e.Replacement] {
			delete(DepAliasesVar, k)
			n++
		}
	}
	if n > 0 {
		SaveZenDepsLocked()
	}
}

// DepAliasesExport 死模型别名表快照（备份导出；锁内加载+深拷贝）。
func DepAliasesExport() map[string]*ZenDepEntry {
	DepMu.Lock()
	defer DepMu.Unlock()
	LoadZenDepsLocked()
	if len(DepAliasesVar) == 0 {
		return nil
	}
	out := make(map[string]*ZenDepEntry, len(DepAliasesVar))
	for k, v := range DepAliasesVar {
		if v == nil {
			continue
		}
		cp := *v
		out[k] = &cp
	}
	return out
}

// DepAliasesImport 整体替换死模型别名表并落盘（Replacement 为空的无效项跳过）。
func DepAliasesImport(in map[string]*ZenDepEntry) {
	DepMu.Lock()
	defer DepMu.Unlock()
	LoadZenDepsLocked()
	next := make(map[string]*ZenDepEntry, len(in))
	for k, v := range in {
		if v == nil || v.Replacement == "" {
			continue
		}
		cp := *v
		next[k] = &cp
	}
	DepAliasesVar = next
	SaveZenDepsLocked()
}

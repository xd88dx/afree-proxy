package backup

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	wbbridge "github.com/misakano7545/workbuddy2api-panel"

	"afree-proxy/internal/blacklist"
	"afree-proxy/internal/cline"
	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/freepi"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/other"
	"afree-proxy/internal/trae"
	"afree-proxy/internal/workbuddy"
	"afree-proxy/internal/zen"
)

// ============ 配置导入导出（全量持久化状态） ============
//
// 三层口径（2026-11-03 用户定，「导出文件 → 全新容器 → 一模一样」）：
//
//   - **配置态**：各平台凭据与配置（Cline 账号、OpenCode key 及启用/绑定、
//     Other 子平台聚合、WorkBuddy
//     config + 凭据文件 + 出口绑定）、代理池（别名随行）、请求头、调度策略、
//     默认模型、自定义别名、zen 粘性会话、端点学习结果。
//   - **账号事实**：账号/key 在上游的客观状态（冷却表、wb 余额/限额/实测成本/
//     签到/运维意志、cline status、trae credits/签到）。
//   - **本机统计**：token 记账、请求/成功失败计数、最近使用时间、轮转游标、
//     出口健康统计、wkbd-state.json 实测台账。也是容器全部数据的一部分（全量
//     导出），但导入时受「Import statistics」勾选控制；不勾选 = 保留本机现值。
//
// 导入规则：配置态与账号事实**无论勾选与否都导入**；本机统计仅勾选时导入。
// 请求日志归档（requests.jsonl、workbuddy/request-logs/）与运行日志不进备份。
// 文件包含全部凭据，仅经管理面板鉴权后下载。新平台接入时必须同步补这里的
// 导出/导入段，否则备份丢数据。
//
// 导入：同一格式的 JSON，**按段替换** —— 文件中存在的段整体替换当前值，
// 文件中没有的段保持不动；导入后各缓存/持久化文件同步重建。
//
// **隔离护栏（v2 起）**：导入的一切上游身份（Cline 账号、zen/th key、
// WorkBuddy 账号）默认一律置为禁用——备份里的启用状态属于导出方部署，其绑定
// 出口在本地未必存在，原样启用等于把流量引向未经验证的身份。启用哪个由
// 用户在面板逐个勾选。导入弹窗提供逐平台"沿用启用状态"switch（KeepEnabled，
// 默认全不沿用 = 原护栏行为），显式勾选的平台保留备份内原状态。客户端 key
// （本地访问凭据）与代理池不在此列。全量克隆场景：逐平台勾选沿用即可得到
// 一模一样的启用状态。

const ConfigBackupVersion = 3

// ConfigBackupVersionOK 导入仅接受 v3（三层口径全量）。旧 v1/v2 备份不再兼容
// （2026-11-05 清理：两代备份形态早已作废，全量重导一次即可升级）；更新的版本
// 直接拒绝——按段替换语义下，未知段被静默丢弃比报错危险。
func ConfigBackupVersionOK(v int) bool { return v == ConfigBackupVersion }

type WorkbuddyBackup struct {
	// Config vendor config.json 原文。导入经 workbuddy.SaveWorkbuddyConfig 落盘，
	// listen/api_key/auth_dir/state_file 四项强制回写为本地部署值。
	Config json.RawMessage `json:"config,omitempty"`
	// AuthFiles WorkBuddy 凭据文件：文件名 -> base64(原文)。导入时整体
	// 替换 auths 目录（workbuddy*.json），随后全部账号禁用。
	AuthFiles map[string]string `json:"authFiles,omitempty"`
	// State 池状态全量快照（state.json 同形态：accounts + scheduling + order，
	// 2026-11-03 三层口径「导出 = 容器全部数据」）。导入时分层：账号事实
	// （余额/冷却/限额/实测成本/签到/运维意志）无论勾选与否都应用；本机统计
	// （token 用量/成功失败计数/最近使用/调度游标）仅勾选「Import statistics」
	// 时应用，不勾选 = 保留本机现值。见 pool.ImportStateBackupJSON。
	// 展示顺序在 state.order 内随快照走（独立 Order 段已随 2026-11-05 清理移除）。
	State json.RawMessage `json:"state,omitempty"`
	// Usage 实测台账全量快照（wkbd-state.json 同形态，统计层）：按模型实测扣费/
	// charge_variants。归统计层（2026-11-03 用户定）——勾选「Import statistics」
	// 时整体替换，不勾选保留本机现值。见 wbbridge.UsageRecorder.ImportRaw。
	Usage json.RawMessage `json:"usage,omitempty"`
}

// ZenBackupSnapshot 取一份 zen 配置的拷贝用于备份。绑定表（keyBindings）的键
// 是 key 明文，跟着 key 池留在 zen 段里；代理池与隔离规则分别由 ProxyPool /
// Proxy 段承载。
func ZenBackupSnapshot() *zen.ZenConfigData {
	c := *zen.GetZenConfig()
	return &c
}

// StateBackup 各平台 state 快照段（备份文件 state 键）：与各自 *-state.json
// 落盘文件**同形态全量**（账号事实 + 本机统计都在内，2026-11-03 三层口径
// 「导出 = 容器全部数据」）。导入时按层分流：账号事实总是应用；本机统计仅
// 勾选「Import statistics」时应用（不勾选 = 保留本机现值，2026-11-03 起
// 取消勾选不再清零）。WorkBuddy 的对应快照在 workbuddy.state / workbuddy.Usage
// 段（vendor 池由 5s flusher 托管，须写进池内存再落盘，不走本段）。
type StateBackup struct {
	Cline       *cline.PoolUsageData `json:"cline,omitempty"`
	Zen         *zen.ZenUsageFile    `json:"zen,omitempty"`
	Trae        json.RawMessage      `json:"trae,omitempty"`
	Other       json.RawMessage      `json:"other,omitempty"`
	Freepi      json.RawMessage      `json:"freepi,omitempty"`
	ProxyHealth *exitpool.HealthFile `json:"proxyHealth,omitempty"`
}

type ConfigBackup struct {
	Version         int              `json:"version"`
	ExportedAt      time.Time        `json:"exportedAt"`
	Accounts        []*cline.Account `json:"accounts,omitempty"`
	ClientKeys      []string         `json:"clientKeys,omitempty"`
	ClineUseProxies *bool            `json:"clineUseProxies,omitempty"`
	AdminLogEnabled *bool            `json:"adminLogEnabled,omitempty"`
	// ClineCatalogFeedOnly cline 目录「仅展示插件免费」开关（cline-config.json，
	// 2026-11-17 配置重组起）。
	ClineCatalogFeedOnly *bool              `json:"clineCatalogFeedOnly,omitempty"`
	Strategy             string             `json:"strategy,omitempty"`
	Headers              map[string]string  `json:"headers,omitempty"`
	Zen                  *zen.ZenConfigData `json:"zen,omitempty"`
	// ProxyPool 全局出口代理池(proxy-config.json 的出口池段)。2026-10-26 从
	// zen 段拆出：代理池是全局能力，历史上寄居在 zen 配置里只是命名包袱。
	ProxyPool *exitpool.PoolConfig `json:"proxyPool,omitempty"`
	// Proxy 代理隔离/绑定段(proxy-config.json 的隔离与绑定部分)。与 ProxyPool
	// 分开：一个管出口池本身，一个管凭据怎么绑到出口上。
	Proxy     *exitpool.BindingConfig         `json:"proxy,omitempty"`
	Sessions  map[string]*zen.ZenSessionEntry `json:"sessions,omitempty"`
	Endpoints map[string]string               `json:"endpoints,omitempty"`
	Combos    []*gwconfig.Combo               `json:"combos,omitempty"`
	// v2 补全的 WorkBuddy（v1 备份无这些段，导入时保持不动）。
	WorkBuddy *WorkbuddyBackup `json:"workbuddy,omitempty"`
	// Other 子平台聚合配置（other-config.json）。2026-10-14 补：Other 页
	// 上线时只做了网关/面板，未接备份，导致子平台与 key 池无法随配置迁移。
	Other *other.Config `json:"other,omitempty"`
	// Freepi 平台配置（freepi-config.json，2026-11-20 接入）。
	Freepi *freepi.Config `json:"freepi,omitempty"`
	// Trae 账号池平台（2026-11-04 起 vendored trae2api 引擎）：config =
	// data/trae2api/config.json 原文（2026-11-05 迁入子系统目录），routing =
	// 反代开关表，accounts = 快照文件
	// 名 → 快照 JSON（data/trae2api/accounts/*.json）。旧引擎的 trae.Config
	// 形态不再兼容，旧备份的 trae 段导入时忽略。
	Trae json.RawMessage `json:"trae,omitempty"`
	// WorkBuddyProxies WorkBuddy 账号代理绑定（wkbd-config.json 的 bindings 段，
	// 2026-11-17 配置重组前住 proxy-config.json 的 WorkBuddy 段）。
	// 2026-10-14 补：WorkbuddyBackup 原只覆盖 vendor config.json 与 auths
	// 凭据，漏了出口绑定，导入后该文件仍留旧值（而非本机值），跨机迁移出错。
	WorkBuddyProxies map[string]workbuddy.WorkbuddyProxyBinding `json:"workBuddyProxies,omitempty"`
	// TraeProxies TraeWork 账号出口绑定（trae-config.json 的 bindings 段，
	// 2026-11-16 前住 proxy-config.json 的 trae2apiBindings 段）。2026-11-05
	// 补：trae 换线时未随 wb 的 2026-10-14 教训补齐，导出→删卷→导入后绑定
	// 丢失（用户实测）。
	TraeProxies map[string]trae.Trae2apiProxyBinding `json:"traeProxies,omitempty"`
	// TraeDisplayOrder TraeWork 凭据展示顺序（trae-config.json 的
	// displayOrder 段，UID 列表）。2026-11-05 补：顺序此前是进程内
	// 展示态，导出→导入（或重启）后回到 UID 序（用户实测）。
	TraeDisplayOrder []string `json:"traeDisplayOrder,omitempty"`
	// ModelBlacklist 模型封禁表（afree-config.json 的 modelBlacklist 段）。2026-10-19 补：
	// 导出导入一直漏了封禁数据，跨机迁移后 /v1/models 把已确认不可调用的
	// 模型重新暴露给客户端，每次选中都吃一轮无谓失败。
	ModelBlacklist map[string]*gwconfig.ModelBlacklistEntry `json:"modelBlacklist,omitempty"`
	// PlatformBlacklist 平台封禁表（afree-config.json 的 platformBlacklist 段）：
	// 整平台退出会话路由的管理操作，与模型封禁同性质，随配置迁移。
	PlatformBlacklist map[string]*gwconfig.PlatformBanEntry `json:"platformBlacklist,omitempty"`
	// ZenDepAliases zen 死模型别名表（zen-config.json 的 modelAliases 段）：弃用 ID ->
	// 继任模型映射，与 sessions/endpoints 同性质的学习结果持久化。
	ZenDepAliases map[string]*zen.ZenDepEntry `json:"zen.DepAliasesExport,omitempty"`
	// ClineStreamRequired cline "必须流式"自学习结果（cline-config.json 的
	// streamRequired 段，2026-11-17 配置重组前住 afree-config.json）。
	ClineStreamRequired map[string]string `json:"clineStreamRequired,omitempty"`
	// ToolRename 出站工具改名配置（afree-config.json 的 toolRename 段）。
	// 2026-11-25 补：上线时漏进备份，导出→删除数据→导入后改名配置丢失（用户实测）。
	ToolRename *gwconfig.ToolRenameConfig `json:"toolRename,omitempty"`
	// ZenUseProxies zen 走池开关（zen-config.json 独立段）。2026-11-19 补：
	// 走池开关只有 cline 进备份，其余四平台跨机迁移后回落默认值。
	ZenUseProxies *bool `json:"zenUseProxies,omitempty"`
	// TraeUseProxies / TraeRateFilterCN / TraeRateFilterIntl trae 走池开关与
	// 分域倍率筛选（trae-config.json，2026-11-17 迁入）。2026-11-19 补。
	TraeUseProxies     *bool    `json:"traeUseProxies,omitempty"`
	TraeRateFilterCN   *float64 `json:"traeRateFilterCN,omitempty"`
	TraeRateFilterIntl *float64 `json:"traeRateFilterIntl,omitempty"`
	// WorkBuddyUseProxies / WorkBuddyAccountPrefs / WorkBuddyShareOptimize
	// wkbd-config.json 的走池开关、账号意志与权重优化定时任务。2026-11-19 补：
	// 账号意志（反代/总开关/昵称锁）唯一持久层在此，漏备份即跨机迁移后意志
	// 全丢；shareOptimize 同理（cron 优化配置归零）。
	WorkBuddyUseProxies    *bool                                `json:"workbuddyUseProxies,omitempty"`
	WorkBuddyAccountPrefs  map[string]workbuddy.WkbdAccountPref `json:"workbuddyAccountPrefs,omitempty"`
	WorkBuddyShareOptimize *workbuddy.WkbdShareOptimize         `json:"workbuddyShareOptimize,omitempty"`
	// WorkBuddyDisplayOrder wb 账号显示顺序（wkbd-config.json displayOrder
	// 段，2026-11-21 补）。顺序意志唯一持久层在此，漏备份即跨机迁移后拖拽
	// 顺序退回 UID 序。
	WorkBuddyDisplayOrder []string `json:"workbuddyDisplayOrder,omitempty"`
	// FreepiUseProxies freepi 走池开关（freepi-config.json 独立段）。2026-11-20 补。
	FreepiUseProxies *bool `json:"freepiUseProxies,omitempty"`
	// State 各平台 state 快照（2026-11-01）：与 *-state.json 落盘同形态的
	// **全量**快照（账号事实 + 本机统计，三层口径「导出 = 容器全部数据」）。
	// 导出始终携带；导入分层——账号事实无论勾选与否都应用，本机统计仅勾选
	// 「Import statistics」时应用（不勾选 = 保留本机现值）。四平台包
	// （trae/th/other）以 JSON 往返（与落盘同形态），不暴露包内类型。
	// WorkBuddy 的池状态与实测台账在 workbuddy.state / workbuddy.Usage 段。
	State *StateBackup `json:"state,omitempty"`
	// ImportState 导入方对「还原本机统计」的选择（弹窗勾选框，默认勾选）。
	// 勾选 = 统计随备份还原；取消勾选 = 统计保留本机现值（2026-11-03 起
	// 不再清零）。配置态与账号事实无论勾选与否都会导入。
	ImportState *bool `json:"importState,omitempty"`
	// KeepEnabled 导入方对"沿用启用状态"的逐平台选择（导入弹窗 switch，默认
	// 全不沿用）。缺项/false = 隔离护栏生效（导入身份一律禁用，原行为）；
	// true = 保留备份内该平台的启用状态。key：cline / zen /
	// wb 与 "other:<slug>"（Other 子平台逐个）。
	KeepEnabled map[string]bool `json:"keepEnabled,omitempty"`
}

// ExportWorkbuddyProxyBindings 快照 WorkBuddy 账号代理绑定，供导入还原。
// 返回副本而非内部 map：调用方 json.Marshal 时不得与后台写入竞争。
func ExportWorkbuddyProxyBindings() map[string]workbuddy.WorkbuddyProxyBinding {
	workbuddy.WbProxyMu.Lock()
	defer workbuddy.WbProxyMu.Unlock()
	workbuddy.LoadWorkbuddyProxyBindingsLocked()
	if len(workbuddy.WbProxyBindings) == 0 {
		return nil
	}
	out := make(map[string]workbuddy.WorkbuddyProxyBinding, len(workbuddy.WbProxyBindings))
	for k, v := range workbuddy.WbProxyBindings {
		out[k] = v
	}
	return out
}

// ExportWorkbuddyAccountPrefs 快照 wb 账号意志全表（wkbd-config.json
// accountPrefs 段；WkbdConfigAccountPrefs 已返回副本）。nil = 从未记录意志。
func ExportWorkbuddyAccountPrefs() map[string]workbuddy.WkbdAccountPref {
	return workbuddy.WkbdConfigAccountPrefs()
}

// ImportWorkbuddyAccountPrefs 整体替换 wb 账号意志，返回写入条数。2026-11-19
// 补：意志唯一持久层此前不在备份内，跨机迁移后反代/总开关/昵称锁全丢。
// 生效时机：紧随其后的 wb auths 导入链 AuthLoadDir 重扫经 AccountPrefProvider
// 带回内存；备份无 authFiles 段时等面板 reload/重启对齐。
func ImportWorkbuddyAccountPrefs(in map[string]workbuddy.WkbdAccountPref) int {
	workbuddy.WkbdSetAccountPrefs(in)
	return len(in)
}

// ExportWorkbuddyDisplayOrder 快照 wb 账号显示顺序（wkbd-config.json
// displayOrder 段；WkbdConfigDisplayOrder 已返回副本）。nil = 未记录顺序。
func ExportWorkbuddyDisplayOrder() []string {
	return workbuddy.WkbdConfigDisplayOrder()
}

// ImportWorkbuddyDisplayOrder 整体替换 wb 账号显示顺序，返回写入条数。
// 只落盘——内存回放要求池已装载导入后的账号全集，由 backup_handlers 在
// wb auths 段（ImportWorkbuddyBackup 的 AuthLoadDir 重扫）完成后显式
// pool.SetOrder 完成；在此回放会把未导入账号的位次丢给「未入序排尾」。
func ImportWorkbuddyDisplayOrder(uids []string) int {
	workbuddy.WkbdSetDisplayOrder(uids)
	return len(uids)
}

// ImportWorkbuddyProxyBindings 还原 WorkBuddy 出口绑定，返回写入条数。
// 与其它段一致采用"整体替换"：备份里出现的 uid 覆盖本机值；本机多出来的
// uid 一并清掉，避免跨机迁移后残留旧机的绑定。
func ImportWorkbuddyProxyBindings(in map[string]workbuddy.WorkbuddyProxyBinding) int {
	workbuddy.WbProxyMu.Lock()
	defer workbuddy.WbProxyMu.Unlock()
	workbuddy.LoadWorkbuddyProxyBindingsLocked()
	next := make(map[string]workbuddy.WorkbuddyProxyBinding, len(in))
	for k, v := range in {
		next[k] = v
	}
	workbuddy.WbProxyBindings = next
	workbuddy.SaveWorkbuddyProxyBindingsLocked()
	return len(next)
}

// ExportWorkbuddyBackup 读 vendor config.json 原文与 auths 目录全部凭据文件。
// 子系统未启动（启动失败）时返回 nil——此时没有可导出的活状态。
func ExportWorkbuddyBackup() *WorkbuddyBackup {
	sub := workbuddy.WorkbuddySub
	if workbuddy.WorkbuddySub == nil || sub.Cfg == nil {
		return nil
	}
	wb := &WorkbuddyBackup{}
	if raw, err := os.ReadFile(sub.CfgPath); err == nil {
		wb.Config = json.RawMessage(raw)
	}
	if sub.Pool != nil {
		// 池状态全量快照（state.json 同形态）：账号事实 + 本机统计都在内
		// （「导出 = 容器全部数据」），导入端按勾选分层（pool.ImportStateBackupJSON）。
		// 展示顺序在快照的 order 字段内随行，无需单独段。
		wb.State = sub.Pool.StateSnapshotJSON()
	}
	// 实测台账快照（wkbd-state.json 同形态，统计层）。
	if sub.Usage != nil {
		wb.Usage = sub.Usage.SnapshotRaw()
	}
	files, _ := filepath.Glob(filepath.Join(sub.Cfg.AuthDir, "workbuddy*.json"))
	if len(files) > 0 {
		wb.AuthFiles = make(map[string]string, len(files))
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			wb.AuthFiles[filepath.Base(f)] = base64.StdEncoding.EncodeToString(raw)
		}
	}
	return wb
}

// ExportModelBlacklist 模型封禁表快照（锁内拷贝，Marshal 不得与后台写入竞争）。
func ExportModelBlacklist() map[string]*gwconfig.ModelBlacklistEntry {
	blacklist.EnsureModelBlacklistLoaded()
	blacklist.ModelBlacklistMu.Lock()
	defer blacklist.ModelBlacklistMu.Unlock()
	if len(blacklist.ModelBlacklistItems) == 0 {
		return nil
	}
	out := make(map[string]*gwconfig.ModelBlacklistEntry, len(blacklist.ModelBlacklistItems))
	for k, v := range blacklist.ModelBlacklistItems {
		if v == nil {
			continue
		}
		cp := *v
		out[k] = &cp
	}
	return out
}

// ImportModelBlacklist 合并封禁表并即时落盘（2026-11-05 用户定，改变原整体
// 替换语义）：封禁记录是跨机各自增长的运维状态，两台机器靠备份互导对齐——
// 整体替换会让「导入旧备份」抹掉另一侧较新的封禁，被抹掉的模型重新出现在
// /v1/models 并被 hermes 写回渠道（2026-11-04 实证：wbgb:gpt-6-luna 在生产
// 渠道挂了三周）。合并规则：并集；同 key 两侧都有时取 createdAt 较新的记录
// （LastAt/Hits 随记录整体走，不做字段级拼接）。空 key / nil 条目跳过。
// 全新环境导入 = 本地空表全量吸收，行为与旧语义一致。
func ImportModelBlacklist(in map[string]*gwconfig.ModelBlacklistEntry) {
	blacklist.EnsureModelBlacklistLoaded()
	added, updated := 0, 0
	next := make(map[string]*gwconfig.ModelBlacklistEntry, len(blacklist.ModelBlacklistItems)+len(in))
	blacklist.ModelBlacklistMu.Lock()
	for k, v := range blacklist.ModelBlacklistItems {
		if k == "" || v == nil {
			continue
		}
		next[k] = v
	}
	blacklist.ModelBlacklistMu.Unlock()
	for k, v := range in {
		if k == "" || v == nil || v.Platform == "" || v.Model == "" {
			continue
		}
		local, ok := next[k]
		if !ok || local == nil {
			next[k] = v
			added++
			continue
		}
		// 同 key 取较新记录；时间相同（例如同一次导入的往返）保持本地。
		if v.CreatedAt.After(local.CreatedAt) {
			next[k] = v
			updated++
		}
	}
	blacklist.ModelBlacklistMu.Lock()
	blacklist.ModelBlacklistItems = next
	blacklist.ModelBlacklistMu.Unlock()
	blacklist.FlushModelBlacklist()
	log.Printf("  config import: model blacklist merged (+%d new, %d newer-updated, total %d)", added, updated, len(next))
}

// ExportZenDepAliases zen 死模型别名表快照（zen 域出包后薄壳）。
func ExportZenDepAliases() map[string]*zen.ZenDepEntry { return zen.DepAliasesExport() }

// ImportZenDepAliases 整体替换死模型别名表并落盘。
func ImportZenDepAliases(in map[string]*zen.ZenDepEntry) { zen.DepAliasesImport(in) }

// ExportClineStreamRequired cline "必须流式"自学习结果快照。
func ExportClineStreamRequired() map[string]string { return cline.StreamSnapshot() }

// ImportClineStreamRequired 整体替换学习表并落盘。
func ImportClineStreamRequired(in map[string]string) { cline.ReplaceStream(in) }

// ImportWorkbuddyBackup 落盘 vendor config 与凭据文件；keepWbEnabled=false
// 时禁用全部账号（隔离护栏），true 时保留凭据文件内的原始启用状态。
// includeStats 控制统计层（本机统计 + usage 台账）：true = 随备份还原；
// false = 保留本机现值（账号事实无论如何都应用，2026-11-03 三层口径）。
// 返回 (导入账号数, config 是否已应用)；子系统未运行时返回 (-1, false)——
// 此时凭据文件仍写盘（重启后生效），config/state/usage 因需要运行期句柄而跳过。
func ImportWorkbuddyBackup(wb *WorkbuddyBackup, keepWbEnabled, includeStats bool) (int, bool) {
	sub := workbuddy.WorkbuddySub
	if workbuddy.WorkbuddySub == nil || sub.Cfg == nil {
		log.Printf("  config import: workbuddy subsystem not running, config segment skipped")
		return -1, false
	}
	importedAuths := -1
	if wb.AuthFiles != nil {
		importedAuths = 0
		authDir := sub.Cfg.AuthDir
		// 整体替换：清掉本目录全部凭据文件再写导入文件（按段替换语义，
		// 与确认弹窗文案一致）。文件名取 Base 防路径穿越，且必须落在
		// LoadDir 的扫描模式内，否则白写。
		old, _ := filepath.Glob(filepath.Join(authDir, "workbuddy*.json"))
		for _, f := range old {
			_ = os.Remove(f)
		}
		for name, b64 := range wb.AuthFiles {
			base := filepath.Base(name)
			if base != name || !strings.HasPrefix(base, "workbuddy") || !strings.HasSuffix(base, ".json") {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				log.Printf("  config import: workbuddy auth %s decode failed: %v", base, err)
				continue
			}
			if err := os.WriteFile(filepath.Join(authDir, base), raw, 0o600); err != nil {
				log.Printf("  config import: workbuddy auth %s write failed: %v", base, err)
				continue
			}
			importedAuths++
		}
		auths, err := wbbridge.AuthLoadDir(authDir)
		if err != nil {
			log.Printf("  config import: workbuddy auth rescan failed: %v", err)
		} else {
			// 隔离护栏：导入账号默认一律禁用（写回凭据文件，重启后仍是禁用
			// 态），随后让池对齐扫描结果（新号加入、消失的号剔除并落盘）。
			// 弹窗勾选 WorkBuddy"沿用启用状态"时保留原始 pool_enabled。
			if !keepWbEnabled {
				for _, a := range auths {
					if a.SetPoolEnabled(false) {
						if err := a.SaveAtomic(); err != nil {
							log.Printf("  config import: workbuddy auth %s disable save failed: %v", a.UID, err)
						}
					}
				}
			}
			sub.Pool.SyncToDir(auths)
		}
	}
	cfgApplied := false
	if len(wb.Config) > 0 {
		// sub.Server 是解耦接口；预算闸热应用需要具体 Handler，断言失败（测试
		// fake）时传 nil，workbuddy.SaveWorkbuddyConfig 内部跳过 gw 段。
		gw, _ := sub.Server.(*wbbridge.Handler)
		if _, err := workbuddy.SaveWorkbuddyConfig(sub.CfgPath, wb.Config, sub.Live, sub.Pool, sub.Upstream, sub.Scheduler, gw, sub.Panel, sub.Cfg.AuthDir, sub.Cfg.StateFile); err != nil {
			log.Printf("  config import: workbuddy config apply failed: %v", err)
		} else {
			cfgApplied = true
		}
	}
	// 池状态（2026-11-03 三层口径）：账号事实总是应用；本机统计（token 用量/
	// 成功失败计数/最近使用/调度游标）仅勾选统计时应用，不勾选 = 保留本机现值。
	// 必须排在 SyncToDir（账号集合对齐）之后——uid 不在池中的条目丢弃。
	if len(wb.State) > 0 && sub.Pool != nil {
		if n, err := sub.Pool.ImportStateBackupJSON(wb.State, includeStats); err != nil {
			log.Printf("  config import: workbuddy state apply failed: %v", err)
		} else if n > 0 {
			log.Printf("  config import: workbuddy pool state applied (%d accounts, stats=%v)", n, includeStats)
		}
	}
	// 实测台账（统计层）：勾选统计才整体替换，不勾选保留本机现值。
	if includeStats && len(wb.Usage) > 0 && sub.Usage != nil {
		if err := sub.Usage.ImportRaw(wb.Usage); err != nil {
			log.Printf("  config import: workbuddy usage apply failed: %v", err)
		}
	}
	// 展示顺序随 state 快照走（ImportStateBackupJSON 内部套用，排在 SyncToDir
	// 之后），无独立段。
	return importedAuths, cfgApplied
}

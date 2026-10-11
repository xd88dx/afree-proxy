# 后端板块详解（internal/app + 按需 internal/<x>/）

## 1. 路由注册（internal/app/admin.go registerAdminRoutes）

新平台的 admin API 全部挂 `/admin/api/x/...`，一律 `adminCORS(auth(...))` 包裹。既有全景（符号名检索）：

- 账号 CRUD/test：`/accounts{,/add,/delete,/test,/reset,/delete-all}`、`/accounts/proxy{,/clear}`、`/accounts/pool/enabled{,/all}`
- 平台配置：`/config{,/update}`、`/opencode/config{,/update}`、`/opencode/models{,/refresh}`、`/opencode/stats`、`/opencode/sessions`（会话快照；mint 端点已随本地铸造删除，会话由后台 rotator 自动补铸）
- key 管理（zen）：`/opencode/keys/routing{,/all}`、`/opencode/keys/reorder`、`/opencode/keys/delete`、`/opencode/keys/proxy{,/clear}`、`/zen/keys/test`（zen 的端点有 opencode/zen 双前缀别名，是历史遗留——新平台别复制）
- key 池平台（openrouter/amd/tokenharbor 同构）：`/<x>/config{,/update}`、`/<x>/models{,/refresh}`（or 有 models GET、amd/th 只有 refresh）、`/<x>/keys/routing{,/all}`、`/<x>/keys/reorder`、`/<x>/keys/delete`、`/<x>/keys/test`、`/<x>/keys/proxy{,/clear}`
- 别名：`/combos{,/create,/delete}`
- 全局：`/request-stats`、`/stats`、`/logs`、`/config/export|import`、`/data/delete-all`

WB 特例：`/admin/api/workbuddy/enabled` 注册在 proxy.go（workbuddy 路由段），其余走 `/admin/workbuddy/api/*`（vendor 面板 API，内嵌控制台 `wbCall()` 消费）——**新平台不要复制这个双前缀模式**，直接用 `/admin/api/x/...`。

## 2. 凭据启停（PoolEnabled 口径）

- 字段：`*bool`（三态：nil=启用、true/false=显式）或 key 池的 `KeyRoutingEnabled map[string]bool`（缺项=参与）。**读侧兼容**：`accountPoolEnabled(a)`（pool.go）/ 各包 `RoutingEnabled(key)`——缺字段视为启用，存量旧数据不受影响；**写侧默认禁**：新增路径落盘前置 false。
- 六平台新增默认禁用的代码点（新增平台 X 照抄位置）：
  - Cline：`pool.go addAccount`
  - OpenCode：`zen.go` ZEN_KEYS 注入 + `reconcileZenKeyRouting`（新 key routingEnabled=false）
  - WorkBuddy：vendor `panel/login.go` + `panel/import.go` 落盘前 `a.SetPoolEnabled(false)`
  - key 池四件套（or/amd/th 同构）：包内 `config.go ReconcileRouting(next, cur)`——整体替换 key 池时新 key 置 false、已有保持、移除清理；app 层保存链必须调用它
- 选号全路径过滤：X 的每一个可用凭据枚举函数都要过滤禁用项（参照 wbpool 的 `selOK`/`AvailableUIDs` 家族/`ServableForRealm`，key 池参照各包 pool.go 的 `RoutingEnabled` 过滤）。**定时任务（签到/保活/统计）不过滤**——禁用只指"不参与请求路由"。
- 状态透出：列表 API 返回每条凭据的启用布尔；一键启停 `setAllAccountsPoolEnabled` / `setAllZenKeysRoutingEnabled` 模式（返回实际改写数）。

## 3. 网关分发（/v1）

- 分发链现状（proxy.go /v1/chat/completions 与 responses.go 一致）：
  1. `shouldServeWorkBuddy(model)`——vendor realm 前缀 `wbcn:`/`wbgb:`（裸模型名不接管，避免静默改路由）
  2. `resolveCombo(model)` 别名改写（改写后回头再判一次 WB）
  3. `openrouter.HasPrefix(model)`（`oprt:`）→ `handleOrChat`
  4. `amd.HasPrefix`（`amd:`）→ `handleAMDChat`
  5. `tokenharbor.HasPrefix`（`tkhb:`）→ `handleTHChat`
  6. `routeModel(model)`（zen.go）——`zen:` 前缀 + 免费表判定 → zen / reject / cline；其余落 cline 池
  7. cline 模型前缀 `cline-free/`（models.go `clineModelPrefix`，/v1/models 输出即带前缀）
- 新平台加自己的 `HasPrefix(model)`（放 X 包内，`ModelPrefix` 常量 + `HasPrefix` 函数，见 openrouter 包）并按上述位置插入分发链。
- `/v1/models` 合并：各平台 `ModelList()` 汇入（带前缀的 ID），客户端按列表 ID 调用。
- 请求日志分类：logs.go 的 per-route 原子计数与新平台桶（reqOr/reqAmd/reqTh 已存在；注意计数在 logEnabled 分支内——LOG_REQUESTS=false 时停摆是刻意行为）。

## 4. 自定义别名（combos.go）

- `validateCombo(id, platform, target)`：platform 白名单现状为 `{cline, zen, workbuddy, openrouter, amd, tokenharbor}`（六平台全量）+ target 必须在**本平台**模型列表内（跨平台选型拒绝，switch 按平台分支）——新平台加分支。
- Combo 结构 `{id, platform, target, useProxies, createdAt}`；网关命中别名后按 target 走对应平台出站，`useProxies` 决定是否过代理池（缺省代理口径）。

## 5. 平台设置与热应用（两种模式，按形态选）

**key 池四件套模式**（openrouter/amd/tokenharbor 同构，形态 B 首选）：
- load：`handleOrConfig` GET → 组装 map（keyStates 快照含 usage/cooling/routingEnabled/proxyMain/proxyBackup）。
- save：`handleOrConfigUpdate` POST patch——patch struct 全指针字段（`*bool`/`*int`/`*string`，nil=未提交），`next := 深拷贝 cur`（**Keys/KeyRoutingEnabled/KeyBindings/Usage/Cool 每张 map 都要拷**，漏拷 = 保存时静默清空该表）→ 逐字段应用 patch → `ReconcileRouting`（keys 整体替换时）→ 落盘 + 热应用。
- key 池无"重启项"概念：全部字段热生效（池参数从 config 直接读）。

**vendor wbConfigForm 模式**（形态 C）：load 侧 `Default()+Load()+normalize()`，save 侧**保留磁盘未提交键 + 原子落盘 + 热应用**（参照 workbuddy_config.go saveWorkbuddyConfig：merge 现有 map → ParseConfig → tmp+rename → 热应用 → 返回 restartRequired）。
- afree 接管的装配项（listen/api_key/auth_dir/state_file）save 时强制回写部署值，防面板把数据搬出统一目录。
- 热应用清单：池参数/冷却/权重/开关类；restartRequired：监听、HTTP client 超时、目录句柄、upstash、session ttl、日志归档。

## 6. 代理池与出口绑定

- 绑定模型：per-identity 主/辅双槽（前端 `egComboHTML` 渲染 `.eg-input`，`data-eg`/`data-egid`/`data-slot`），提交走 `egCommit`（暂存语义，随平台保存按钮生效）。存储与解析参照 `proxy_binding.go`。
- 出站消费：平台出站 client 按 identity 取绑定出口（参照 `workbuddyProxyFor` / zen 对应物），无绑定 → 平台默认（走池下拉决定代理/直连）。
- 隔离语义：主出口冷却/失效 → 该 identity 本轮跳过，**辅不接、直连不回退**；辅代理开关关闭时辅槽只读并跟随主槽（`egApplyMainLinkage`）。绑定引用了已不存在的池出口 → 前端 ⚠ 告警标记（隔离逻辑会跳过）。
- "清空代理"端点（`proxy/clear`）：清绑定 + **全部置禁用**（一个事务语义，前后端都要绑 together）。
- 出口观测：upstream client 的 `ObserveProxy` 钩子（出口成败 → 在线率统计，纯被动展示）；X 的出站若走自建 client，记得接 `recordProxyOutcome`。
- 代理池页健康统计为纯展示，不联动路由。

## 7. 装配与生命周期（两种模式）

**key 池四件套（形态 B）**：无生命周期子系统——包内 `Get()` 惰性加载配置（sync.Once/首调加载），app 层桥接文件只做 handler + 分发接线，无 Start/Stop。故障面小，不需要"返回错误继续启动"的防护。

**vendor 子系统（形态 C，workbuddySubsystem 模式）**：
- `startWorkBuddy()`：mkdir 数据目录 → ensure config → pool restore → 参数 Set 系列 → upstream client（超时三元组 timeout/header/idle + 指纹脱敏 atomic）→ 调度器 → livecfg → usage/requestLog → panel/handler → ctx goroutine。**返回错误时调用方只记日志继续启动**，保证既有平台链路不受新平台故障影响。
- `Stop()`：cancel ctx → pool Flush/Close → 停 GC/usage/requestLog → store Close。

**两者共用**：
- `admin_data.go` delete-all：新平台的持久化数据（账号/状态/学习数据/日志）必须纳入清理清单；`.session-secret` 与运行日志保留。
- 数据落点：`kit.ResolveDataPath("x/…")`，绝不写仓库目录或原项目目录。

## 8. 出站 client 约定

- 超时三元组：`HTTP.Timeout`（短 RPC）、`HeaderTimeout`（SSE 首字节）、`IdleTimeout`，均可在平台设置热改（HeaderTimeout 经 Transport.ResponseHeaderTimeout 生效）。
- 指纹脱敏：`SanitizeFingerprints atomic.Bool` + livecfg 快照热生效。
- User-Agent/ClientVersion 等指纹字段做成 config 可改（参照 upstream client 字段族）。

## 9. key 池四件套克隆配方（形态 B：OpenRouter → AMD/TokenHarbor 实证）

纯 API key 池平台（无账号 OAuth、无上游会话态）按此克隆，已实证两次（amd、tokenharbor 从 openrouter 克隆）。

**四件套**（`internal/<x>/`，从 internal/openrouter 复制后改包名）：
- `client.go`：上游调用。OpenRouter = 标准 OpenAI 兼容 chat/completions；key 选择与出口解析留在**主包**（主包持有代理池/隔离语义），本包只做"给定 key + Exit + Client 发一次请求 + 重试"。`HTTPError{Status, Body, RateLimited}` 类型化错误、`CallOpts{PinKey, Exit, Client}`。
- `config.go`：`Config{Keys, KeyRoutingEnabled, KeyBindings, MaxConcurrency, Retries, Usage, Cool, ...}` + `Load(path)/Get()/Save()` + `ReconcileRouting(next, cur)`（新 key 默认 false）+ `RoutingEnabled(key)`（缺项=true）+ key 掩码。
- `models.go`：目录同步与免费模型判定（各平台协议不同：OpenRouter 白名单收 free-models 页 + openrouter/free 兜底、AMD/TH 收 Public Free Model APIs；价格门/成员表语义按平台定）。
- `pool.go`：轮转（round_robin/fill/random）、冷却、并发信号量、`RoutingEnabled` 过滤。

**app 桥接**（`internal/app/<x>.go`，照 openrouter.go 的函数族）：
- `init<X>()` 装配（Load 配置 + 目录同步）+ `start<X>ModelsRefresher()` 周期刷新（init() 或 StartProxy 里启动）。
- `handle<X>Chat`：前缀剥离 → key 轮转/绑定出口解析（`egress` 语义与 zen 同：绑定的主→辅，全不可用则短冷却跳过，绝不直连回退）→ 出站 → 错误分类（429 短冷却/403/配额耗尽长冷却）。
- handler 族：`handle<X>Config{,Update}`、`handle<X>Key{SetRouting,SetRoutingAll,Reorder,Delete,SetProxy,ClearProxies,Test}`、`handle<X>ModelsRefresh`、`orCloneConfig` 式深拷贝助手（**每张 map 都拷**）。
- 接线清单：admin.go 路由段、proxy.go 分发链 `HasPrefix` 分支、logs.go 统计桶、combos.go 白名单+target 分支、admin_html.go 全套（见 frontend.md）、admin_data.go delete-all 清单、/v1/models 合并。

**克隆时容易漏的**：config_test.go（随四件套一起改名）；`orBoundRoutable` 式"绑定出口可用性"过滤；面板 Test 探测的 PinKey 路径；仪表盘卡 + request-stats 桶；`/<x>/keys/reorder`（拖拽排序协议用 index 置换，见 reorderZenKeys）。

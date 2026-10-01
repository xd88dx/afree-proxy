# 后端板块详解（internal/app + 按需 internal/<x>/）

## 1. 路由注册（internal/app/admin.go registerAdminRoutes）

新平台的 admin API 全部挂 `/admin/api/x/...`，一律 `adminCORS(auth(...))` 包裹。既有全景（符号名检索）：

- 账号 CRUD/test：`/accounts{,/add,/delete,/test,/reset,/delete-all}`、`/accounts/proxy{,/clear}`、`/accounts/pool/enabled{,/all}`
- 平台配置：`/config{,/update}`、`/opencode/config{,/update}`、`/opencode/models{,/refresh}`、`/opencode/stats`、`/opencode/sessions{,/mint}`
- key 管理（zen）：`/opencode/keys/routing{,/all}`、`/opencode/keys/delete`、`/opencode/keys/proxy{,/clear}`、`/zen/keys/test`
- 别名：`/combos{,/create,/delete}`
- 全局：`/request-stats`、`/stats`、`/logs`、`/config/export|import`、`/data/delete-all`

WB 特例：`/admin/api/workbuddy/enabled` 注册在 proxy.go（workbuddy 路由段），其余走 `/admin/workbuddy/api/*`（vendor 面板 API，内嵌控制台 `wbCall()` 消费）——**新平台不要复制这个双前缀模式**，直接用 `/admin/api/x/...`。

## 2. 凭据启停（PoolEnabled 口径）

- 字段：`*bool`（三态：nil=启用、true/false=显式）。**读侧兼容**：`accountPoolEnabled(a)`（pool.go）——nil 视为启用，存量旧数据缺字段不受影响；**写侧默认禁**：新增路径落盘前 `Set(false)`。
- 三平台新增默认禁用的代码点（新增平台 X 照抄位置）：
  - Cline：`pool.go addAccount`
  - OpenCode：`zen.go` ZEN_KEYS 注入 + `reconcileZenKeyRouting`（新 key routingEnabled=false）
  - WorkBuddy：vendor `panel/login.go` + `panel/import.go` 落盘前 `a.SetPoolEnabled(false)`
- 选号全路径过滤：X 的每一个可用凭据枚举函数都要过滤禁用项（参照 wbpool 的 `selOK`/`AvailableUIDs` 家族/`ServableForRealm`）。**定时任务（签到/保活/统计）不过滤**——禁用只指"不参与请求路由"。
- 状态透出：列表 API 返回每条凭据的启用布尔；一键启停 `setAllAccountsPoolEnabled` 模式（pool.go，返回实际改写数）。

## 3. 网关分发（/v1）

- 分发顺序（proxy.go `/v1/messages` 与 responses.go 一致）：**平台专属前缀判定（shouldServeWorkBuddy：cn:/global: 前缀）→ 别名 `resolveCombo(model)` → 平台默认池**。新平台加自己的 `shouldServeX(model)` 判定（用独立前缀如 `x:` 最省心，避免与既有上游目录静默冲突）并插入分发链。
- `/v1/models` 合并：各平台 `ModelList()` 汇入（带前缀的 ID），客户端按列表 ID 调用。
- 请求日志分类：logs.go 的 per-route 原子计数与新平台桶（注意计数在 logEnabled 分支内——LOG_REQUESTS=false 时停摆是刻意行为）。

## 4. 自定义别名（combos.go）

- `validateCombo(id, platform, target)`：platform ∈ {cline, zen, workbuddy} 白名单 + target 必须在**本平台**模型列表内（跨平台选型拒绝）——新平台加分支。
- Combo 结构 `{id, platform, target, useProxies, createdAt}`；网关命中别名后按 target 走对应平台出站，`useProxies` 决定是否过代理池（缺省代理口径）。

## 5. 平台设置与热应用

- handler：load 侧 `Default()+Load()+normalize()`，save 侧**保留磁盘未提交键 + 原子落盘 + 热应用**（参照 workbuddy_config.go saveWorkbuddyConfig：merge 现有 map → ParseConfig → tmp+rename → 热应用 → 返回 restartRequired）。
- afree 接管的装配项（listen/api_key/auth_dir/state_file）save 时强制回写部署值，防面板把数据搬出统一目录。
- 热应用清单：池参数/冷却/权重/开关类；restartRequired：监听、HTTP client 超时、目录句柄、upstash、session ttl、日志归档。

## 6. 代理池与出口绑定

- 绑定模型：per-identity 主/辅双槽（前端 `egComboHTML` 渲染 `.eg-input`，`data-eg`/`data-egid`/`data-slot`），提交走 `egCommit`（暂存语义，随平台保存按钮生效）。存储与解析参照 `proxy_binding.go`。
- 出站消费：平台出站 client 按 identity 取绑定出口（参照 `workbuddyProxyFor` / zen 对应物），无绑定 → 平台默认（走池下拉决定代理/直连）。
- 隔离语义：主出口冷却/失效 → 该 identity 本轮跳过，**辅不接、直连不回退**；辅代理开关关闭时辅槽只读并跟随主槽（`egApplyMainLinkage`）。绑定引用了已不存在的池出口 → 前端 ⚠ 告警标记（隔离逻辑会跳过）。
- "清空代理"端点（`proxy/clear`）：清绑定 + **全部置禁用**（一个事务语义，前后端都要绑 together）。
- 出口观测：upstream client 的 `ObserveProxy` 钩子（出口成败 → 在线率统计，纯被动展示）；X 的出站若走自建 client，记得接 `recordProxyOutcome`。
- 代理池页健康统计为纯展示，不联动路由。

## 7. 装配与生命周期（workbuddySubsystem 模式）

- `startWorkBuddy()`：mkdir 数据目录 → ensure config → pool restore → 参数 Set 系列 → upstream client（超时三元组 timeout/header/idle + 指纹脱敏 atomic）→ 调度器 → livecfg → usage/requestLog → panel/handler → ctx goroutine。**返回错误时调用方只记日志继续启动**，保证既有平台链路不受新平台故障影响。
- `Stop()`：cancel ctx → pool Flush/Close → 停 GC/usage/requestLog → store Close。
- `admin_data.go` delete-all：新平台的持久化数据（账号/状态/学习数据/日志）必须纳入清理清单；`.session-secret` 与运行日志保留。
- 数据落点：`kit.ResolveDataPath("x/…")`，绝不写仓库目录或原项目目录。

## 8. 出站 client 约定

- 超时三元组：`HTTP.Timeout`（短 RPC）、`HeaderTimeout`（SSE 首字节）、`IdleTimeout`，均可在平台设置热改（HeaderTimeout 经 Transport.ResponseHeaderTimeout 生效）。
- 指纹脱敏：`SanitizeFingerprints atomic.Bool` + livecfg 快照热生效。
- User-Agent/ClientVersion 等指纹字段做成 config 可改（参照 upstream client 字段族）。

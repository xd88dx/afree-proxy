# PROJECT.md — afree-proxy 项目专属规则

本文件只放**本项目的事实与流程**。跨项目通用的协作规则（git 纪律、验证要求、临时文件、删除判据、上游台账格式）在 `AGENTS.md`，两份都要读。

## 1. 这是什么

单一 Go 二进制网关（`module afree-proxy`），一个服务同时承载多个平台的反代与账号池：Cline、OpenCode（zen）、TraeWork、WorkBuddy，以及 AMDCloud / OpenRouter / TokenHarbor / Other 等 OpenAI 兼容上游。生产容器名 `afree`（跑在 iStore 上，见 §6），端口 3457。

## 2. 先判平台归属，再动手

同一个文件常承载多个平台的面板段与路由，改错平台是本仓最常见的事故来源。

| 平台线         | 代码位置                                                                                        | 面板字段 / 环境变量 |
| -------------- | ----------------------------------------------------------------------------------------------- | ------------------- |
| Cline          | `internal/cline/`；`internal/app/proxy.go` 的账号池、轮换、冷却与 `/v1` 通用转发                | —                   |
| OpenCode (zen) | `internal/app/zen*.go`、`internal/app/admin_zen.go`；`internal/kit/http.go` 的 `DecodeResponse` | `oc*`、`ZEN_*`      |
| TraeWork       | `internal/trae2api/`（vendor 树）、`internal/app/trae.go`（桥接）、`trae2api_helper*.go`          | `trae*`（桥接层）；选号=引擎权重调度，`TRAE_STRATEGY` 已作废 |
| WorkBuddy      | `internal/workbuddy/**`（vendor 树）、`internal/app/workbuddy*.go`                              | `wb*`               |

`internal/app/admin_html.go` 是**所有平台共用**的单文件面板，按 h2 / tab 分段，`oc*` 属于 OpenCode 而不是 Cline。改一处必须确认它属于哪个平台段。

## 3. 上游同步：五条线（主线 merge/vendor + 副线吸纳）+ magpie 参考线，台账 `MERGE.md`

同步口径（2026-11-08 用户定）：一个平台对应一条入口，**cline/opencode 共用 cline-proxy 主线**（按改动文件归属分节），opencode 另有 opencode2api 副线、trae 另有 Trae2api-cn 副线负责「看近期更新、吸纳有价值逻辑」：

- **cline**：`foxy1402/cline-proxy`（主线），`git merge --no-ff --no-commit <上游哈希>`。
- **workbuddy**：`misakano7545/workbuddy2api-panel`（主线），`python scripts/sync_workbuddy.py`（三方 vendor 合并，基线 `scripts/workbuddy-vendor-baseline.json`；源工作树为 `D:\下载\Github\workbuddy2api-panel-misakano`，脚本以源树 HEAD 为上游）。
- **opencode**：`foxy1402/cline-proxy`（主线，与 cline 同步一起）+ `jasonxu114514/opencode2api`（副线，只看更新、吸纳有价值逻辑，不 merge 不 vendor）。
- **trae（主线）**：`XCool-603/trae2api`（2026-11-04 起换线，Go 零依赖），**vendor 三方合并**——`scripts/vendor_trae2api.py` 整树拷贝 12 子包到 `internal/trae2api/` + 幂等改写（出口接管/反代开关/新增身份钩子/console 挂载基址），`scripts/sync_trae2api.py` 三方合并，基线 `scripts/trae2api-vendor-baseline.json`；上游本地修复（textcall 兜底）以 `scripts/trae2api-afree.patch` 重放。模型名合并域透传（2026-11-07 起）：对外 `tacn:`（CN Trae+SOLO）/`tagb:`（Intl，随 intl 配置），内部四 realm 仅实现细节。旧四前缀 `trae:`/`solo:`/`trae-intl:`/`solo-intl:` 废除（strict-gate 404）。
- **trae（副线）**：`autumnsentiment/Trae2api-cn`（Python 项目，trae 更换基线前身），只看更新、吸纳有价值逻辑（XCool-603/trae2api 与 Trae2api-cn 是完全独立的 Go/Python 双实现，副线逻辑无跨语言直接移植形态，多以「上游行为取证 + 本仓形态差异结论」记录）。`web_login.py` 在主线里跑 helper 比对的额外职责还在（`scripts/check_trae_helper.py`，模板已迁 `internal/app/trae2api_helper_script.go`）。
- **magpie（2026-11-06 用户立项，全平台纯参考）**：`yetone/magpie`（订阅聚合网关，覆盖约 70 agent/全主流平台）——**纯参考不合并**，按需单点吸纳 + `MERGE.md` magpie 节记基线增量回溯；也是上游行为取证的旁证源（其 gateway 测试文件质量高，zen 免费层关停即靠它定案）。详见技能 §2.5。

仓库根 `MERGE.md` 按 **Cline / TreaWork / WorkBuddy / OpenCode** 四条线（平台⇔主线一一对应）记录同步历史，另有 **副线评估与 magpie** 一节（副线吸收/零吸纳结论与 magpie 基线），格式 `### YYYY-MM-DD — <基线短哈希> → <目标短哈希>`，写明上游仓库与合并方式、基线与目标、吸收项（带原因）、丢弃项（带原因）、本地定制保留清单、验证结果。历史只追加不删改；未涉及的线写“本次未涉及上游同步”。详细流程在 `.agents/skills/sync-upstream/`，按需加载。

## 4. 构建与验证

```bash
go build ./... && go vet ./...     # 必须全绿
go test ./... -count=1             # 除下述既有失败外应全绿
```

**已知失败，不要修**：

- ~~`internal/workbuddy/server` 的 `TestChatStreamPassthrough`~~（2026-11-05 移除：上游 d596fa7 的 66e5344「客户端断开只按请求 ctx 判定」顺带修复了该 flaky，全量测试已稳定通过，历史根因记录见 MERGE.md WorkBuddy 节）。
- **已知失败：无**（2026-11-09 复核）。`internal/workbuddy/updater` 的 `TestInstallReplacesRestartsAndRecovers` 在 Windows 上由 `t.Skip("native self-update unavailable on this deployment")` 短路（`CanInstall()` 为 false），不再失败；该包不 import 任何 `afree-proxy/internal/*`，与业务改动无因果关系。同日 `go test ./... -count=1` 全绿。

换行：行尾由 `.gitattributes` + `.editorconfig` 强制统一为 LF（规则与判据见 `AGENTS.md` §3）。`gofmt` 是原生程序，不认 MSYS 路径——传 `$(cygpath -w <路径>)`，且**不要**把 `gofmt` 的 stderr 一起重定向进结果文件（会写坏源文件）。

## 4b. 2026-11-09 全量审查修复台账（追加式）

第二轮全量审查（网关/池、八平台 provider、WB+Trae 桥接、面板 i18n、文档一致性）产出的修复。判据与验证：`go build ./... && go vet ./...` 全绿、`go test ./... -count=1` 全绿、`scripts/i18n_audit.py` 正向 0 缺失。

| 缺口 | 根因 | 修复 |
|---|---|---|
| Other 目录同步裸直连 | `catalogHTTPClient()` 从不查绑定/走池/`DISABLE_DIRECT`（amd/tokenharbor/openrouter 三平台早已逐 key `resolveExit`） | `catalogClientFor` 逐 key 调注入的 `catalogExit`（app 层复用 `otherExitFn`），全不可用 fail-closed |
| 保存配置挂死在途请求 | amd/openrouter/tokenharbor/other 的 `releaseSem` 重读包级信号量，而 `Set` 会整体换表 | `acquireSem` 返回本次入队的 channel，`releaseSem` 只释放它（zen 原本就是正确写法） |
| 手动冷却对路由零效果 | `pickUpstreamProxyFor` 全不可用时无条件返回轮转位（可能是手动冷却出口） | 命中手动冷却即返回空；新增 `upstreamProxyForKind` 三态封装，12 处出口解析点 fail-closed；zen egress 钉定分支补 `proxyAvailableFor` 判定 |
| 后台同步可打挂进程 | 轮转游标只夹上界，负值 `active[-n]` panic；`models.go` 的同步 goroutine 无 recover | `pickAccount`/`pickAccountExcluding` 夹取下界 |
| `afree-config.json` 损坏即丢配置 | 解析失败只打日志、不隔离，首个面板会话用默认值整文件覆盖（`clientKeys` 清零后 `/v1` 退化为放行） | 照 `pool.go` 形态 rename 成 `.corrupt-<stamp>` 留档 |
| 面板 Test 误雪藏账号 | Test 的 429 分支直接落 `markAccountCooldown` 的 18h 兜底 | 抽出 `cline429CooldownDuration`，与请求路径共用「配额信号才 18h、否则 20min」分流 |
| Trae 配置一次笔误即不可自愈 | 四个时长字段无校验，先落盘再重启；vendor 对非法时长硬失败 → 子系统 nil、连 config/update 都被 503 挡 | 保存前 `time.ParseDuration` 校验（非法 400 不落盘） |
| `trae2apiValidateGateway` 死代码 | 文档承诺「配置文件路径拒收 OAuth-only 域」，实际无生产调用点 | 装载期调用，非法域回退默认基址并告警（不让引擎因此装不起来） |
| Trae/OpenCode 走池开关不落盘 | Trae patch 结构缺 `useProxies`；OpenCode 的 `ZenUseProxies` 只声明未读取 | 两处补 `setPlatformUseProxies`（trae/zen），与其余六平台同构 |
| WB 面板 Import JSON 恒 404 | 前端请求 `/admin/api/workbuddy/accounts/import`（无此路由） | 改走 `WB_API_PREFIX + '/import/cockpit'`（vendor 端点，字段名 `file`） |
| Trae 域级 Ban 静默升级为全域 | `normalizeTraeScope` 白名单是旧引擎词汇（remote/traecode/traework），面板实发内部 realm → scope 恒空 = 全档 | 白名单改收合并域与四 realm，内部 realm 折到 `tacn`/`tagb` |
| WB 余额刷新热开启无效 | vendor 在 `enabled=false` 时根本不建消费循环，`SetBalanceInterval` 只 poke | `restartRequiredWorkbuddyFields` 补 `schedule.balance_refresh_*`，面板如实提示需重启 |
| 空 model 误报平台无凭据 | `routePlatformKeyOf("")` 落成 Cline 裸名，撞可路由性闸报 503 | 空模型直接放行交入口 400 |
| 订阅一行脏数据即整池不更新 | `syncProxySubscription` 任一行解析失败即整体返回错误 | 跳过并计数，仅当有效行为 0 时判失败 |
| 面板 i18n 缺口 | 4 处 `T()` 传词典不存在的 snake_case 假键（中文界面显示键名）、2 处英文长句无键、3 组重复键后者静默覆盖 | 补 3 条词条、4 处改英文长句键、删重复键 |
| 面板 `api()` 吞错误 | 非 JSON 响应抛 SyntaxError、`res.ok` 未检查 | 非 JSON 兜底 + 非 2xx 报 `error`/`message`/`HTTP <code>` |
| 缩进漂移 | 11 个 afree 自有文件 gofmt 不干净 | `gofmt -w`（vendor 树不参与：`internal/trae2api/**`、`internal/workbuddy/**` 保持上游原样，gofmt 误改已回滚） |
| Other 删子平台留残留 | `PrunePlatformUsage` 只修剪被移除的 key、不删 slug，被删子平台的冷却/记账永久留在 `other-state.json` | 新增 `PrunePlatform`，删子平台时一并清 |
| 重复授权提示不实 | `trae2apiIngestCreds` 恒返回 `added=true`（vendor 响应无"已存在"标记），"Account already exists" 分支不可达 | 导入前查 `Pool().Get(uid)` 判定，返回真实 added |
| 注释与事实漂移 | zen 端点注释称"裸 ID 仍兼容"、trae 注释称旧四前缀透传/旧词汇费率域、combos 前缀冲突注释暗示在线生效 | 逐条改为现状；`combos` 不可达分支显式标注（勿误以为生效） |

**已验证无需修改（本轮核对结论）**：

- **面板端点闭合性**：150 条 mux 注册 vs 154 处前端调用，**0 处前端调用无对应路由**；未引用项只有 5 个 `/v1` 对外网关端点（供 IDE 客户端）。核对脚本注意：`/admin/` 是 catch-all 静态注册，做闭合性匹配时必须排除以 `/` 结尾的注册，否则假阴性（本次踩坑）。
- **数据清空（delete-all）**：8 平台 + 代理池 + combos + cline 流式学习 + 请求日志全覆盖，各平台均清 keys/bindings/routing **及 usage**；trae 按文档口径删快照+清绑定/路由+重启。zen 的 baseURL/压缩设置在流程末尾被 `clearProxyPoolForDataDelete` 整体重置——与面板文案「Deletes ALL persisted data (... config)」一致，已在该函数注释注明顺序。
- **导入护栏 KeepEnabled**：cline/zen/oprt/amd/tkhb/`other:<slug>`/trae/wb 八个键全部被读取，无"该保护未保护"项。

**未修（低优先，记录在案）**：无——2026-11-09 第二轮收尾已把下列项全部落地（详见 §4c）。

**审计工具盲区备忘**：`scripts/i18n_audit.py` 只匹配 `T('…')` 字面量（正则要求引号紧跟 `T(`），三元表达式 `T(cond ? 'a' : 'b')` 里的键一律漏检；`i18n_reverse_audit.py` 的孤儿候选只作参考、不作删除依据。改面板后除跑两个脚本，还要 grep 一次 `T(` 的非字面量形态。

## 4c. 2026-11-09 未修项清零（第二轮收尾）

第一轮结束时留下的 5 项已全部处理。**行为契约变了的地方单独标注**。

| 项 | 原问题 | 处理 |
|---|---|---|
| 上游 429 被包成 502 | amd/openrouter/tokenharbor/other/跨端点把上游 429 当 502 回给客户端，IDE 判定网关故障并标记渠道不可用（zen 早已正确） | 新增 `upstreamErrorStatus(err, fallback)`：错误自带 429 即透传，其余走 fallback。各上游错误类型补 `HTTPStatus()`（amd/openrouter/tokenharbor/other/zen/cline + 新增 `upstreamHTTPError`）；`zenClientErrorStatus` 退化为该函数的别名。**契约变更**：这五类平台的上游限流现在对外是 429（README 已记）。回归测试 `upstream_status_test.go` |
| Trae 重启不等旧引擎收尾 | 重启只等自家 30s 落盘循环，未等 vendored `RunBackground`（它在 ctx 取消时也写 state/metrics），旧实例的写盘可能晚于新实例，把冷却/额度盖回一步 | 子系统新增 `vendorDone`，`trae2apiRestart` 用**同一截止时刻**串行等 `bgDone` 与 `vendorDone`，总预算 `traeRestartJoinTimeout = 5s`，超时只记日志继续（不让面板保存无限转圈）。ONLY_ROUTER 下无 vendored 任务，信号构造时即关闭 |
| combos 死分支 | 带前缀平台的"撞名检查"永远不可达（combo ID 语法不含 `:`） | 删除 oprt/amd/tkhb/trae 四条不可达分支；保留真正可达的 Cline 目录与 zen 裸名别名检查，注释写明依据 |
| 旧引擎 scope 存量条目失效 | 修完域级 Ban 后，存量 `scope=remote/traecode/traework` 条目谁也匹配不上 = 静默解禁 | 定义 `traeLegacyScopes`：读取侧按"全档封禁"命中，写入侧 `normalizeTraeScope` 一律落成空 scope（fail-closed，该封的继续封） |
| 88 条孤儿词条 + `strategy` 无面板入口 | 反向审计脚本自述"仅参考"；选号策略只能 env/API 设置 | 三重判据甄别（词典区外无引用 ∧ 无 Go 侧产生 ∧ 大小写敏感）后删 9 条死词条，其余 77 条为动态 `T(变量)` 在用，保留；**新增面板「账号轮转策略」下拉**（**网关设置页，紧挨「封禁平台」上方**；全部可见文案走 `data-i18n` slug 键——含 `<code>` 的段落必须用属性键，否则文本节点被拆开永远匹配不上；后台 `handleZenConfigUpdate` 接 `strategy`，env `POOL_STRATEGY` 设置时后端 400 拒绝 + 控件置只读并提示，与 PROXY_ISOLATION 同款；加载挂在设置页切换 `loadAccountStrategy()`，不靠代理池页顺带），并顺手补上隔离锁定提示缺失的中文词条 |

**甄别脚本判据（可复用）**：孤儿词条不能只信 `i18n_reverse_audit.py`——面板存在 `T(动态变量)` 形态（状态徽章、helper 消息、封禁原因由 Go 侧产生），必须再用"该字符串是否出现在 admin_html.go 之外的任何 Go/vendor 源码"交叉验证；本次据此避免了误删 `Credentials saved to server`、`auto-banned: …`、`Ready`、`Unknown`、` already` 等在用词条。另注意键里的省略号可能是 U+2026（`wrk_… (optional)`），按 ASCII 三点匹配会漏。

## 4d. 2026-11-09 请求日志「TraeWork 两列为空」修复

用户报「trae 平台的请求日志不显示凭据」。定位到**两个独立根因**，均已修并实测：

| 根因 | 机制 | 修复 |
|---|---|---|
| 直连出口的账号完全不进观测 | 日志两列靠 `traeObserveTransport` 写入，而它在 `traePooledClient(url, uid)` 里构造；`url == ""`（绑定直连哨兵 / 走池关闭）时该函数返回 nil，vendored 回落到自带 client —— 于是**既不记录账号、也不记录出口** | 新增 vendored 注入点 `server.AfreeAccountObserver`（`server/afree_hooks.go`，PRESERVE_RELATIVE），在 `chat.go`/`anthropic.go` 每次 `ClientFor(acct)` 后回调 `afreeObserveAccount(r.Context(), acct.UID)`；afree 侧 `trae2apiObserveAccount` 填两列（直连场景补 `direct`）。与 WorkBuddy 的 `upstream.ObserveAccount` 同构 |
| 反代开关在选号兜底路径失效 | `pool.Pick` 对"软冷却/熔断但非硬冷却"的账号有一条再试一次的**兜底分支**，其 `usable` 只判 `disabled`/硬冷却/降权/模型级冷却，**不看 `RoutingAllowed`** —— 关掉反代开关的账号在没有 live 候选时照旧被选中（日志与面板都显示已关闭，实际仍在服务） | 兜底分支补 `(RoutingAllowed == nil \|\| RoutingAllowed(a.UID))`；反代开关是硬策略，不是健康信号 |

两处 vendor 改动都登记进 `scripts/vendor_trae2api.py`（`afree_hooks.go` 进 PRESERVE_RELATIVE，两处调用点 + 兜底判据各一条 REWRITES），re-vendor 不丢。

**实测证据**（本机 `DISABLE_DIRECT=true` 容器）：

- 仅直连绑定账号可路由 → `upstream=account #1  proxyType=direct`（改前两列都是 `-`）；请求 502 是 DISABLE_DIRECT 的 fail-closed 预期结果。
- 临时放开 DISABLE_DIRECT 复测 → **200**，两列同样填满（验证完已恢复 `DISABLE_DIRECT: "true"`）。
- 把代理绑定账号反代开关关掉后，该账号不再被选中（修复前 token 台账显示它仍在服务）。

**排障备忘**：面板「请求日志」里 trae 行若仍为空，先确认镜像是否包含本修复——生产（iStore）跑 GHCR `latest`，需用户手动 pull 才生效；本机测试容器同理（旧 tag 不含新代码）。用户 2026-11-09 12:44–12:47 贴出的日志在本机 `requests.jsonl` 里查不到对应 UTC 行（04:44–04:47Z），说明那份日志来自另一实例（生产）的旧镜像。

## 5. 本地定制不可丢

本 fork 独有、上游没有，合并时优先保留：`PROXY_ISOLATION` / `proxyIsolationEnvLocked`、`backupProxyEnabled`、`proxyExclusions`、`proxyBindCaps`、`pinProxyPerKey`、`zenEgressSnapshot()`、`ModelRateFilterCN` / `ModelRateFilterGlobal`（wb 分域倍率筛选，2026-11-07 由单值 `model_rate_filter` 拆分，旧键在 `ParseConfigInto` 入口 `migrateModelRateFilterSplit` 逐字段回落自动迁移；CN 过滤 wbcn、Global 过滤 wbgb）、`enforceToolChoiceNone`（`proxy.go`，Cline 侧）、Workbuddy / Trae 分区与 `POOL_STRATEGY`。

面板与运维侧新增的本地定制（2026-11-02/03 用户定，同步时同样优先保留）：健康表出口**手动冷却**（无到期时间，仅「解禁」恢复，`proxyManualCooled`）与绿色解禁/黄色冷却按钮；健康表**代理测试**（测速目标可配置、每出口最多 3 次 HTTPS 探测入在线率/握手统计，不触碰路由冷却）；**批量测试可路由过滤**（反代开关关闭的凭据跳过——Trae/WorkBuddy 只看反代开关不看账号总闸；网关封禁平台整平台跳过）；绑定名 eg-input **中间缩略**（`egShortLabel`）；wb 池状态/实测台账进配置备份（三层口径，见 `MERGE.md` 2026-11-03 节）。

wb vendored 树 server 包手工改动（2026-11-11，re-vendor 时按 ours 保留）：`internal/workbuddy/server/logging.go` 与 `handler.go`——`chatStat` 携带客户端请求 ctx，`done` 落行时客户端已取消且 outcome 为空写回 `OutcomeInterrupted`（客户端断开的 200 请求不再记 `out=success`，handler 构造点传 `r.Context()`）。上游无此机制，同步冲突时优先保本语义。

wb vendored 树手工改动（2026-11-12/13，re-vendor 时按 ours 保留，承上一条）：①`server/logging.go`——done 的改判条件从「outcome 为空」扩为「outcome 为空**或显式 success**」：continue 层在客户端先断开时按设计不补截断终态、以干净 EOF 交给 StreamHint（其补 length 终态返回 nil），handler 开关显式写 success，原条件拦不住（生产 trace 实测「流中客户端硬断记 out=success」）。②`server/continue.go` + `server/handler.go` + `reqlog/reqlog.go`——新增 `reqlog.OutcomeTruncated`（"truncated"）与 `Snapshot.Truncated` 单列：上游段无终态结束（断流）时 continue 层降级补截断终态（`synthTruncation` 置位），handler 的 outcome 开关按 ctx 取消→interrupted、补过终态→truncated、其余→success 分流；Failed 派生公式改为 `completed − succeeded − truncated`（截断既不算成功也不算失败）。③`scheduler/checkin_retry.go`——补签节奏变量 `checkinRetryDelays` 加 `retryDelaysMu` 互斥锁 + `retryDelaysSnapshot()` 快照读：后台补签 goroutine 与测试收尾恢复写曾无锁并发（CI -race DATA RACE，TestCheckinDeferredRetrySucceeds）。上游均无此机制，同步冲突时优先保本语义。

已废弃、不要从上游恢复：后台 key rotator（`SessionRotateMinutes` 2026-11-08 随 a8fe356 吸收，见 MERGE 台账）、`ZEN_KEYS`（Go 源码从不读取）、上游 README 的 compose 配置段。

上游功能但**必须由本仓 app 层装配**才生效（2026-10-28 起）：workbuddy 的网关思考档位策略（上游 `thinking` 配置节）。装配点三处，漏任一处都不会报错、只是功能静默失效——`internal/app/workbuddy.go`（启动）与 `internal/app/workbuddy_config.go`（热更新）里的 `up.SetEffortPolicy(effortPolicyOf(cfg))`、`livecfg.Snapshot` 的五个 `Thinking*` 镜像字段、面板四个控件（`thinking.enabled` / `default_effort` / `client_priority` / `override_scope`）。上游同类装配在 `cmd/server/main.go`，本仓没有那个入口。

`internal/config.example.json` 是上游仓库根 `config.example.json` 的副本（上游 `TestConfigExampleParses` 用相对路径读它，vendor 后正好指到这里）。同步脚本只搬 `internal/workbuddy/` 树，**不会**带这类根文件；上游更新样例后要手工 `cp`。**不要删**——删了 `go test ./internal/workbuddy/config/` 直接缺文件失败。

**已随旧引擎移除（2026-11-04 换线）**：Trae 991502 防护三件套（账号会话闸门/读取超时看门狗/断流 Work 回落，原 `internal/trae/`）——vendored trae2api 自带分级冷却/熔断/降权与流中空闲监控（`trae.idle_timeout_seconds`，引擎 `cooldown.*` 三档），防护语义由引擎承担。历史细节（accountGate 权益并发/看门狗 env/Work 回落）见 git 历史与 MERGE.md 归档；旧引擎专属 env（`TRAE_ACCOUNT_PARALLEL_LIMIT`/`TRAE_REMOTE_FIRST_EVENT_TIMEOUT`/`TRAE_STREAM_IDLE_TIMEOUT`/`TRAE_REMOTE_DEBUG`）与 `TRAE_STRATEGY` 一并作废。

## 5b. TraeWork（vendored trae2api）移植规则与全局策略清单（2026-11-04 定稿）

**新增其他平台时按此清单逐项适配**——每一条都是换线过程中实测踩坑后的结论，缺一条就是静默缺口。

### 数据与装配
- vendor 树 `internal/trae2api/`（`scripts/vendor_trae2api.py` 生成，勿手改）；桥接层 `internal/app/trae.go`；helper 资产 `internal/app/trae2api_helper*.go`。
- 配置 `data/trae2api/config.json`（2026-11-05 从顶层散文件迁入子系统目录，与 workbuddy/config.json 同构；config-manifest 显式登记监测；启动时自动迁移旧路径）；运行态同目录（accounts/*.json 快照、state.json 冷却/熔断/禁用+手动冷却、metrics.json）。使用痕迹不进配置监测口径。
- vendored 无热更新：面板保存配置 → 落盘 → `trae2apiRestart()`（先摘除再重启，旧实例先 `Pool().Save()` 防与 30s ticker 竞争写 state.json）。
- 鉴权分层：vendored `cfg.APIKey`/`WebPassword` 恒空（`traeApplyAfreeConstraints` 每次重启强制），真门禁在 afree 网关层；`trae.proxy`（全局）与 per-account proxy 恒清空（出口由 afree 接管，防死配置）。
- 网关 `chat_base` 面板不可改（沿用上游默认；`trae2apiValidateGateway` 保留给配置文件路径——OAuth-only 域拒收）。

### 网关路由与目录
- 网关路由与目录（2026-11-07 合并域化）：对外仅两前缀 `tacn:`（国内 = Trae CN + SOLO CN）/`tagb:`（国际 = Trae Intl + SOLO Intl，随 `intl.enabled`；关闭时 tagb strict-gate 404）。vendored 四 realm 原样运转，afree 层做「目录合并（bare 去重、**交集以 trae 为主**，solo 独有才落 solo）+ 请求翻译」（trae2apiTranslateMerged）。旧四前缀 `trae:`/`solo:`/`trae-intl:`/`solo-intl:` 废除（strict-gate 404）。跨域容灾恒关不变（CrossRealmFailover，tacn/tagb 域透传契约同款）。
- `/v1/models` **域级可路由过滤**：vendored 四域渠道恒全建，无启用凭据的域必须摘除（`routableRealms`）——"列出来的必须调得通"。
- 请求闸 `shouldServeTrae2api` + 可路由数 0 → 503；`platformHasRoutableCredential(platTrae)` 同闸（禁用平台/无凭据跳过路由的判定链共用）。
- Anthropic `/v1/messages` 与 `/v1/responses` 走 `trae2apiCaller`（进程内管道 client 进 vendored Handler，`traeInprocessRoundTripper` 桥接 http.ResponseWriter↔io.Pipe，含 panic recover）。
- combos 别名冲突校验用 `trae2apiModelExists`（静态判定用 `trae2apiPlatformModel`，不随 intl 开关变化——封禁/日志归类需要全前缀）。

### afree 特色注入（vendor 幂等改写，`scripts/vendor_trae2api.py` REWRITES）
- 出口接管 `traectl.StoreHTTPClient`：chat 走 `Client.client()`、续期/额度/签到走 `Store.httpClient()`——两个咽喉全收口。语义：隔离开启且有绑定 → 主→备 fail-closed（`pickBoundProxyFor`）；未绑定/隔离关 → 全局走池开关（`traeUseProxies`）+ `DISABLE_DIRECT` fail-closed（`kit.DisableDirect()` 时直连不放行，返回 `traeFailClosedTransport` 宁弃请求不暴露直连）。
- 反代开关 `pool.RoutingAllowed` 钩子 → `traeRoutingAllowed`（查 `proxy-config.json` 的 `trae2apiRouting`；查表缺失=启用兼容，新增身份经 `AccountAddedHook` 显式落 false）；账号总闸=vendored `disabled`（关总闸连带关反代，互锁 wb 同款）。
- 新增身份默认禁用 `accountstore.AccountAddedHook` → `traeAccountAdded`。
- 改名 `pool.Account.SetLabel`（快照文件是持久层，`Store.Load()`+池同步）。
- console 挂载 `/admin/trae2api/`（原版入口；app.js fetch 基址改写 `AFREE_API_BASE='/admin/trae2api/api'` + 契约测试候选路径）。
- 出口接管需要的服务器访问器：`Server.Metrics()`、`Store.Mu()`、`Status.CreditsTotal`（vendor 注入，同步上游时锚点漂移就修 REWRITES——计数失配会 RuntimeError 强校验）。
- `server/textcall.go`（文本工具调用兜底，afree 自有文件，走 PRESERVE_RELATIVE）：流式 `textCallFilter` 与非流式 `extractTextToolCalls` 均有单响应 30 上限 `defaultTextCallMax`（与网关 `internal/textcall.DefaultMaxPerResponse` 同值，超限按正文放行；两层互不重复计数——trae 内层先转，网关对已结构化响应无文本可转）。

### vendor 脚本自检(re-vendor / 改 REWRITES 前必跑)

`scripts/vendor_trae2api.py` 是"从上游克隆重建 vendor 树"的唯一入口,它一旦坏了,上游同步就卡住——而它**曾经同时坏在三处**(2026-11-09 修):

| 缺陷 | 后果 | 教训 |
| --- | --- | --- |
| `REWRITES = {` 缺闭合 `}` | 脚本无法被 Python 解析,整条 vendor 流程跑不起来(HEAD 上坏了很久没人发现,因为没人真跑) | 改完必须 `python -m py_compile scripts/vendor_trae2api.py` |
| `"pool/pool.go"`、`"traectl/refresh.go"` 各出现两次 | 字典字面量重复键**后者静默覆盖前者**,先出现的那组规则直接丢 | 一个文件只能有一个键;新增规则并进现有块,别另起一块 |
| 新规则的 `old` 取自**活树**而非上游 | 活树已含注入 → 上游匹配数 0 → 要么 RuntimeError,要么注入被跳过 | `old` 必须是**上游克隆的原文**,`new` = old + 注入 |

另外,活 vendor 树曾缺 `traectl/refresh.go` 的 `StoreHTTPClient` 接管块(续期/额度/签到出站**没走 afree 出口解析**,`DISABLE_DIRECT` 下会静默直连)。已补齐。

**复核方法(可复用,跑完必须 100% 一致)**:把流程干跑到 `.tmp/vendor-dry`,再与活 vendor 树逐字节比对;判据是 `differing: []`。出现差异就是"脚本与活树不一致"——要么规则没生效(功能会丢),要么活树被手改过(re-vendor 会回退),**两种都必须先弄清再同步**。脚本骨架:

```python
import importlib.util, filecmp
from pathlib import Path
spec = importlib.util.spec_from_file_location("vt", "scripts/vendor_trae2api.py")
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
print("exit", m.main(target_root=".tmp/vendor-dry", preserve_from="internal/trae2api"))
live, gen = Path("internal/trae2api"), Path(".tmp/vendor-dry/internal/trae2api")
print("differing:", [str(p.relative_to(gen)) for p in gen.rglob("*")
      if p.is_file() and not filecmp.cmp(p, live / p.relative_to(gen), shallow=False)])
```

行为自证:接管是否真的生效,看 `DISABLE_DIRECT=true` 下直连绑定账号的额度查询——日志应出现 `no usable egress (bound exits down or isolation denies direct)`(fail-closed),而不是静默成功。
### 全局策略适配清单（每平台必查）
| 策略 | trae 适配点 |
|---|---|
| 禁用平台（platformBlacklist） | `platformOfModel` 认四域前缀（`trae2apiPlatformModel`）；`bannedPlatformOf` 同链 |
| 无启用凭据跳过路由 | `platform_routability.go` `trae2apiRoutableCount()>0`（总闸开+反代开） |
| 批量测试可路由过滤 | 前端只看反代开关（routing），不看总闸；封禁平台整平台跳过 |
| 代理排除规则 | UI 走 `REORDER_PLATFORMS`（trae 在列），自动分配过滤 kind=`trae`；手动绑定不受限 |
| 走池开关 | `proxy-config.json` `traeUseProxies`（nil=走池） |
| 代理隔离（PROXY_ISOLATION） | `traeStoreHTTPClient` 隔离语义（绑定 fail-closed / 未绑定全局） |
| DISABLE_DIRECT | 直连哨兵与池空兜底直连均不放行 |
| ONLY_ROUTER | `runBackground` 里 `kit.OnlyRouter()` 禁 vendored `RunBackground`（签到/额度刷新）；metrics/池状态落盘是基础设施照常 |
| 配置导入导出 | 备份 trae 段 `{config, routing, accounts}`（`trae2apiBackupSnapshot`/`trae2apiRestoreBackup`，旧引擎形态报错（不再兼容旧格式））；KeepEnabled["trae"] 隔离护栏 |
| 数据清空（delete-all） | `clearTrae2apiAccountsForDataDelete`（删快照+清绑定/路由+重启） |
| 出口健康观测 | `traeObserveTransport` → `recordProxyOutcome(pkTrae,...)`；请求日志 `[req]` 账号/出口列经 `setUpstreamInfo/Exit` |
| 模型黑名单 | realm scope 封禁（`TraeModelBannedIn(ch.Realm, bare)`）；黑名单页探测 `trae2apiProbeBlacklistedModel`（进程内最小 chat） |
| 上游永久错误自动拉黑 | vendored 引擎自带模型级冷却（3003 等）；`/v1/models` 出口按 realm 过滤 |
| 请求日志 | `[req]` 行 acct 列=池序号（`trae2apiUpstreamLabel`） |

### 面板三区约定
- 凭据表 10 列（换线前列序恢复）：#/账号/状态(含 token 过期行+签到竖徽章)/额度(剩余/总)/今日累计token/最近使用/代理绑定/操作/反代/总闸；上方只放 刷新余额+全部签到，添加账号/批量组/复位保存 在表格下方。
- 配置区两列（wb-config-grid 同款）：左列=单账号并发/熔断/冷却/流空闲（引擎吞吐组），右列=CN/Intl 双域费率过滤（各 0/0.1/0.2/0.3/0.5/1，CN 过滤 tacn、Intl 过滤 tagb，旧单值仅回落）+自动签到+签到时点+拒绝未知模型+会话粘性+国际版域；右下角 放弃修改→保存配置。
- 模型目录：realm 徽章+倍率+Ban（scope 落地为合并域 `tacn`/`tagb`：面板上报 vendor 的内部 realm，`normalizeTraeScope` 折到所属合并域——直接透传会让 scope 落空变成跨域全域封禁；旧引擎词汇 remote/traecode/traework 仅为存量记录保留）。
- 加号三通道：helper 网页授权（铸 EC P-256 设备密钥补齐快照）/粘贴 `token----refreshToken`/原版面板导入 JSON/ZIP；裸 JWT 拒绝。
- i18n：新文案全走 I18N 词条（2026-11-08 起 key→{en,zh} 结构）（键尾无空格、撇号用 JS 双引号包 key——`account's` 未转义炸过整块脚本）；改完跑 `scripts/i18n_audit.py` + `i18n_reverse_audit.py` 双向清零。

### 已知边界（如实展示）
- 今日 token 计数重启后从引擎 recent 流水重建（近 100 条窗口），弱于旧引擎全量持久化；累计/最近使用不受影响。
- 签到徽章显示最近一次动作结果（引擎池 state.json 持久化）；从未执行过签到不显示徽章。
- `max_tokens=1` 上游稳定 3003（引擎边界，忠实透传）；`mt≥8` 正常。
- vendored 无手动重排序/探测模型下拉（引擎权重调度+额度连通性验证设计，用户拍板沿用）。

## 6. 部署:本机测试,iStore 生产

**两台机器各有一个叫 `afree` 的容器,职责不同**:本机是测试容器,iStore 是生产。判断生产看**流量**而不是容器名——只有 iStore 的 `docker logs afree` 里有实时请求行(`| #NNN | hh:mm:ss | ... | 200 | ... tok=...`),本机的只有启动初始化日志。

### 本机:测试容器(改完代码在这里验证)

compose project `tokenapp`,文件 `D:/DockerApp/docker-compose.yml`(同一文件还管 `newapi`、`mihomo`),数据卷 `D:/DockerApp/afree`,端口 3457。

```bash
docker build -t afree-proxy:test-$(date +%Y%m%d-%H%M) .    # 本地构建,约 20 秒
# 把 docker-compose.yml 里 afree 服务的 image: 换成新 tag
cd /d/DockerApp && docker compose up -d afree
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:3457/health    # 期望 200
docker logs afree --tail 10
```

本机构建几十秒就完事,**不要为了验证去等 GitHub Actions**(一次 10 分钟,纯浪费);CI 只服务生产镜像。

### iStore:生产

`root@istore`,compose 文件 `/docker/app/docker-compose.yml`(`AFREE_IMAGE=ghcr.io/xd88dx/afree-proxy:latest`),数据卷 `/docker/app/afree`,端口 3457。

**生产由用户手动同步**:本机测稳后推 `main`,CI 构建 GHCR,用户自己在 iStore 上 `cd /docker/app && docker compose pull afree && docker compose up -d afree`。agent 不要主动去 `pull` / `up -d` 生产容器。

**agent 可在用户显式授权后代为执行**(2026-11-06 先例):`docker pull ghcr.io/xd88dx/afree-proxy:latest` → `bash /docker/app/rebuild.sh afree`。注意 `/docker/app/rebuild.sh` 只做 compose stop/rm/up,**不含 pull**——只跑脚本等于还在跑旧镜像。判断生产是否落后:`git show -s --format='%h %ci' origin/main` 对照 `docker images --format '{{.CreatedAt}}' ghcr.io/xd88dx/afree-proxy:latest`,镜像构建时间晚于 HEAD 提交时间才包含该提交。更新后验证:`docker stats` CPU 回落正常、`docker logs afree --since 2m` 出现各状态恢复行(zen usage / cline trace / proxy health restored)、`/health` 200。

生产验证口径:`docker inspect afree --format '{{.Image}}'` 的 digest 变了、`docker ps` 为 `Up`、`GET /health` → 200、日志出现实时请求行。

iStore 的面板密码是 `233@.afree`(与本机 compose 里的值不同);登录走 `POST /admin/api/login`,未登录时 `/admin/` 只返回约 3KB 的登录页,登录后才是完整面板。

**不要用 `docker run` 重建**:会丢掉 compose 标签与 project 归属。`docker-compose.yml` 里凭据是明文,而终端输出会对凭据脱敏——不要拿脱敏后的占位符去改文件。

### 本机配置同步脚本

`D:/DockerApp/afree/` 下的 `*.json` 由 `C:/AppSelf/!Apps/AutoTool/rsync/task/sync-istore-docker.ps1` 从 iStore 拉(定时任务 `sync-istore-docker`,每 10 分钟):

- 默认、也就是定时任务实际执行的方式:只把 iStore 的 `/docker/app/*` rsync 到 `D:/dockerapp/iStore/` 做备份,**不部署、不重启**。
- 加 `-Deploy` 才把 `afree/*.json` 变换内网 IP 后写进 `D:/DockerApp/afree/` 并 `docker restart afree`(mihomo 配置变了也会一并 `docker restart mihomo`)。

**compose 不在同步范围内,端口映射要手工维护**:脚本只搬 `mihomo/config.yaml` 和 `afree/*.json`,不碰 compose。iStore 的 mihomo 走 `network_mode: host`,配置里写了的 listener 端口自动绑到宿主机;本机是 bridge + 端口白名单(`D:/DockerApp/docker-compose.yml` 的 mihomo `ports:`)。所以 **iStore 每新增或改动一个 listener 端口,本机都要手工同步那一行**。判据是容器内 `docker exec mihomo netstat -lnt`(能看到监听)与宿主机 `/dev/tcp/127.0.0.1/<port>`(连不上)不一致——看到这种组合就是漏了映射。踩过的例子:iStore 新增 `12308` 未映射,补上之后 iStore 又把它改成了 `10086`。

早先的 v2 默认就部署,导致本机容器每 10 分钟重启一次:因为 **iStore 上正在服务流量的实例会持续改写 `currentIdx`、`accounts` 状态、`egressBindings` 这类运行时字段**,「iStore 侧文件 sha256 变了」的防抖判据于是每次命中。该判据在 v3 已移除,不要再加回来。

本机自建镜像 tag（`afree-proxy:*`）只保留最近 7 个（2026-10-30 用户定，从 3 提到 7）。tag 口径：本机一律用 `test-YYYYMMDD-HHMM`——本机容器跑的镜像就是测试镜像，不再区分 `prod-*`/`test-*` 两套 tag（旧台账里的 `prod-*` 标签是历史产物，`prod-*` 只存在于 GHCR 的 CI 发布语境；本机构建新镜像时不要再打 `prod-` 前缀）。

**本机部署流程（2026-10-30 用户定，维持 test-* 命名 + 每次部署更新 compose）**：

1. 构建：`docker build -t afree-proxy:test-YYYYMMDD-HHMM .`（在仓库根，时间取当前时刻）；
2. 更新 `D:/DockerApp/docker-compose.yml`（tokenapp 项目）里 `afree` 服务的 `image:` 行为该 tag。**该行是多 agent 共写点**：改前先 `grep -n "image: afree-proxy" docker-compose.yml` 看当前值——若与上次部署不同，说明有并行部署，先确认那个镜像的来历再覆盖；
3. `cd /d/DockerApp && docker compose up -d afree`（不要用 `docker run` 手工重建，会让容器脱离 compose 管理）；
4. 验证：`docker compose ps` + 容器 healthy + `/v1/models` 或面板探活。

## 7. 死配置判据（本仓）

不要把本仓没有路由入口的上游端点暴露成面板开关。判据：`grep -rn '"/v1' internal/app/*.go | grep -v _test` —— 主 mux 只注册 `/v1/chat/completions`、`/v1/messages`、`/v1/responses`、`/v1/models`、`/v1/health`（另有不带 `/v1` 前缀的同名别名路由与 `/api/web-auth`、`/trae/oauth/helper` 两个 trae 授权端点（2026-11-04 复核仍有效））；workbuddy 的 `/v1/embeddings`、`/v1/search`、`/v1/fetch`、`/v1/images/*` 从未挂到主 mux（生产一律 404）。

## 8. 面板文案与中文翻译（每次改面板 / 同步上游必查）

`internal/app/admin_html.go` 的界面是**英文原文 + `I18N` 对照词典**（2026-11-08 起 key→{en,zh} 结构，88f341e）+ `T()`/`data-i18n`（title/placeholder 走 `data-i18n-title`/`data-i18n-placeholder` 专属 key，伏笔 dbc476c/46d03e4）**双层结构。`LANG === 'zh'` 时由三条路径替换：

| 路径         | 实现                                                      | 覆盖对象                       |
| ------------ | --------------------------------------------------------- | ------------------------------ |
| 静态文本节点 | `i18nTextNode` + `i18nSubtree` 的 TreeWalker              | 标签、hint、按钮、表头、说明段 |
| 属性         | `i18nAttrs`（`attributeFilter: ['title','placeholder']`） | 悬浮提示、输入框占位符         |
| JS 字符串    | `T('...')`                                                | toast、动态拼接的行、状态标签  |

**未进词条的文案在中文界面下会原样显示英文**——这是本仓最常见的「功能做完但界面半中半英」缺陷。

### 8a. 硬性规则（2026-11-09 用户定）：新增文案必须走 i18n 机制，禁止裸英文

任何新增/修改的面板文案都必须走下表某一条路径，并在**同一个改动**里把词条补齐。不允许"先写英文、以后再说"——中文界面下露英文即视为未完成。

| 文案形态 | 必须这样写 |
| --- | --- |
| 纯文本节点（不含子元素） | 加 `data-i18n="<slug>"`，或让文本节点原文精确成为词典键 |
| **含内联标签的段落**（`<code>`/`<b>`/`<a>`/`<span>` 混排） | **必须**用 `data-i18n="<slug>"` 属性键。翻译机制按单个文本节点匹配，内联标签会把句子拆成几段、每段都凑不成完整键 → 这类段落用文本节点键**永远翻不出来** |
| 带超链接的段落 | 拆成「`<span data-i18n=前缀>` + 保留的 `<a>` + `<span data-i18n=后缀>`」，链接在两种语言下都保留（整段 `data-i18n` 会 textContent 覆盖、把链接吃掉） |
| `title` / `placeholder` | `data-i18n-title` / `data-i18n-placeholder` 专属 key（属性值原文含标点） |
| JS 动态拼接 / toast / 状态标签 | `T('...')` 且键在表内；禁止把中文写进 HTML/JS 模板（切英文界面会露中文） |
| `<option>` 选项文案 | 元素上加 `data-i18n`，不要只靠文本节点匹配 |

新词条一律用**语义 slug**（`account_rotation_strategy`、`proxy_isolation_hint`），不要拿整句英文当键——整句键在标点被改动后会静默失配。slug 词条与旧「原文当键」的词条可以共存（同一个词典对象）。

**提交前必须跑**（退出码非 0 即视为未完成）：

```bash
python scripts/i18n_audit.py            # 三遍：T() 键、data-i18n 键、含内联标签的文本块
python scripts/i18n_reverse_audit.py    # 孤儿候选（仅参考，勿直接删）
```

`i18n_audit.py` 的第三遍就是这条规则的守门员：它枚举「自身有文案且子元素全是行内元素」的元素，整段不在词典里又没 `data-i18n` 就报缺口——2026-11-09 修掉的 9 处（代理列表提示、代理隔离提示、请求日志标题、WorkBuddy 配置标题、AMDCloud/TokenHarbor 取 key 提示、侧栏页脚两行）都是它检出来的。

### 8b. 逐项自查
——这是本仓最常见的「功能做完但界面半中半英」缺陷。每次开发新功能或同步上游后，逐项自查：

1. 新增/改动的 HTML 可见文本，在 `I18N` 里有**同名键**（或 `data-i18n` slug 键）（机制匹配的是文本节点 `trim()` 后的整段原文，不是关键词）。
2. 新增的 `title` / `placeholder` 说明性文案同样要有词条，键为属性值原文（含标点）。
3. JS 动态拼接的文案一律走 `T('...')` 且键在表内。不要把中文直接写进 HTML/JS 模板——切到英文界面会露出中文。
4. 允许不译的只有：model id、key 名前缀、纯示例值（`0.5`、`22`、`4096`）、单位、纯技术标识，以及**标识词与技术值**——域前缀名（TraeWork 的 `tacn:`/`tagb:`，WorkBuddy 的 `wbcn:`/`wbgb:`——是模型 ID 组成部分不是文案）、reasoning effort 档位（`xhigh`/`high`/`medium`/`low`/`minimal`）、产品名（`Token Factory`）。2026-10-29 用户定：这类词进词条反而制造假翻译。

排查遗漏：

```bash
# 面板里所有 tooltip 原文，逐个确认在 I18N 有同名键
grep -o 'title="[^"]\+"' internal/app/admin_html.go | sed 's/^title="//;s/"$//' | sort -u
# 面板里所有 placeholder 原文（示例值可跳过）
grep -o 'placeholder="[^"]\+"' internal/app/admin_html.go | sed 's/^placeholder="//;s/"$//' | sort -u
```

注意 `I18N` 是 JS 对象字面量：**同名键写两次不会报错，后者静默覆盖前者**，界面会显示成你以为错的那条。新增词条前后都 `grep -n "'<原文>'"` 确认只有一处。

## 9. 面板凭据表格对齐约定（2026-10-29 用户定）

8 个平台的账号/key 表（tbody id：`accountTableBody` / `traeAccountsBody` / `ocKeysBody` / `wbAccountsBody` / `orKeysBody` / `amdKeysBody` / `thKeysBody` / `otherKeysBody`）：**所有列的表头与内容居中，唯独账号/Key 列（各表第 2 列）表头居中、内容左对齐**。实现集中在 `admin_html.go` 的 CSS 追加段（`table:has(#xxxBody) th` + `#xxxBody td` + `td:nth-child(2)` 左对齐覆盖）；flex 容器（状态列的竖排栈、签到竖徽章）`text-align` 管不到，用 `justify-content:center` 单独处理。新增平台表格时把 tbody id 加进该规则，不要逐 td 写内联样式。

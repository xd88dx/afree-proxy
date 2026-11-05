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

## 3. 上游同步：四条同步线 + 一条全平台参考线，台账 `MERGE.md`

仓库根 `MERGE.md` 按 **Cline / TreaWork / WorkBuddy / OpenCode** 四条线记录同步历史（另有 **magpie** 全平台参考线一节），格式 `### YYYY-MM-DD — <基线短哈希> → <目标短哈希>`，写明上游仓库与合并方式、基线与目标、吸收项（带原因）、丢弃项（带原因）、本地定制保留清单、验证结果。历史只追加不删改；未涉及的线写“本次未涉及上游同步”。详细流程在 `.agents/skills/sync-upstream/`，按需加载。

四条同步线的同步方式不同：

- **Cline / OpenCode**：`foxy1402/cline-proxy`，`git merge --no-ff --no-commit <上游哈希>`。一个上游承载两条线，按改动文件的归属分节，判据 `git diff --name-only <旧HEAD> <merge提交> | grep -iE 'zen|opencode'` 与 `| grep -i cline`。
- **WorkBuddy**：`misakano7545/workbuddy2api-panel`，`python scripts/sync_workbuddy.py`（三方 vendor 合并，基线 `scripts/workbuddy-vendor-baseline.json`；源工作树为 `D:\下载\Github\workbuddy2api-panel-misakano`，脚本以源树 HEAD 为上游）。
- **TraeWork**（2026-11-04 起换线）：`XCool-603/trae2api`（Go 零依赖），**vendor 三方合并**——`scripts/vendor_trae2api.py` 整树拷贝 12 子包到 `internal/trae2api/` + 幂等改写（出口接管/反代开关/新增身份钩子/console 挂载基址），`scripts/sync_trae2api.py` 三方合并，基线 `scripts/trae2api-vendor-baseline.json`；上游本地修复（textcall 兜底）以 `scripts/trae2api-afree.patch` 重放。旧线 `autumnsentiment/Trae2api-cn` 仅剩 helper 脚本比对（`scripts/check_trae_helper.py`，模板已迁 `internal/app/trae2api_helper_script.go`）。模型名透传：trae:/solo: 常驻，trae-intl:/solo-intl: 随 intl 配置。
- **magpie 全平台参考线**（2026-11-06 用户立项）：`yetone/magpie`（订阅聚合网关，覆盖约 70 agent/全主流平台）——**纯参考不合并**，按需单点吸纳 + `MERGE.md` magpie 节记基线增量回溯；也是上游行为取证的旁证源（其 gateway 测试文件质量高，zen 免费层关停即靠它定案）。详见技能 §2.5。

## 4. 构建与验证

```bash
go build ./... && go vet ./...     # 必须全绿
go test ./... -count=1             # 除下述既有失败外应全绿
```

**已知失败，不要修**：

- ~~`internal/workbuddy/server` 的 `TestChatStreamPassthrough`~~（2026-11-05 移除：上游 d596fa7 的 66e5344「客户端断开只按请求 ctx 判定」顺带修复了该 flaky，全量测试已稳定通过，历史根因记录见 MERGE.md WorkBuddy 节）。
- `internal/workbuddy/updater` 的 `TestInstallReplacesRestartsAndRecovers`（Windows 上 `fork/exec wb2api.exe` 报 "This version of %1 is not compatible"，测试固件的假二进制无法 exec）。该包不 import 任何 `afree-proxy/internal/*`，与业务改动无因果关系。

换行：行尾由 `.gitattributes` + `.editorconfig` 强制统一为 LF（规则与判据见 `AGENTS.md` §3）。`gofmt` 是原生程序，不认 MSYS 路径——传 `$(cygpath -w <路径>)`，且**不要**把 `gofmt` 的 stderr 一起重定向进结果文件（会写坏源文件）。

## 5. 本地定制不可丢

本 fork 独有、上游没有，合并时优先保留：`PROXY_ISOLATION` / `proxyIsolationEnvLocked`、`backupProxyEnabled`、`proxyExclusions`、`proxyBindCaps`、`pinProxyPerKey`、`zenEgressSnapshot()`、`ModelRateFilter`、`enforceToolChoiceNone`（`proxy.go`，Cline 侧）、Workbuddy / Trae 分区与 `POOL_STRATEGY`。

面板与运维侧新增的本地定制（2026-11-02/03 用户定，同步时同样优先保留）：健康表出口**手动冷却**（无到期时间，仅「解禁」恢复，`proxyManualCooled`）与绿色解禁/黄色冷却按钮；健康表**代理测试**（测速目标可配置、每出口最多 3 次 HTTPS 探测入在线率/握手统计，不触碰路由冷却）；**批量测试可路由过滤**（反代开关关闭的凭据跳过——Trae/WorkBuddy 只看反代开关不看账号总闸；网关封禁平台整平台跳过）；绑定名 eg-input **中间缩略**（`egShortLabel`）；wb 池状态/实测台账进配置备份（三层口径，见 `MERGE.md` 2026-11-03 节）。

已废弃、不要从上游恢复：`SessionRotateMinutes` 与后台 key rotator、`ZEN_KEYS`（Go 源码从不读取）、上游 README 的 compose 配置段。

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
- 域前缀透传：`trae:`/`solo:` 常驻，`trae-intl:`/`solo-intl:` 随 `intl.enabled`；intl 关闭时 afree 层直接拒绝（防 vendored 跨域兜底悄悄换账号）。模型名零翻译。
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
| 配置导入导出 | 备份 trae 段 `{config, routing, accounts}`（`trae2apiBackupSnapshot`/`trae2apiRestoreBackup`，旧引擎形态报错忽略）；KeepEnabled["trae"] 隔离护栏 |
| 数据清空（delete-all） | `clearTrae2apiAccountsForDataDelete`（删快照+清绑定/路由+重启） |
| 出口健康观测 | `traeObserveTransport` → `recordProxyOutcome(pkTrae,...)`；请求日志 `[req]` 账号/出口列经 `setUpstreamInfo/Exit` |
| 模型黑名单 | realm scope 封禁（`TraeModelBannedIn(ch.Realm, bare)`）；黑名单页探测 `trae2apiProbeBlacklistedModel`（进程内最小 chat） |
| 上游永久错误自动拉黑 | vendored 引擎自带模型级冷却（3003 等）；`/v1/models` 出口按 realm 过滤 |
| 请求日志 | `[req]` 行 acct 列=池序号（`trae2apiUpstreamLabel`） |

### 面板三区约定
- 凭据表 10 列（换线前列序恢复）：#/账号/状态(含 token 过期行+签到竖徽章)/额度(剩余/总)/今日累计token/最近使用/代理绑定/操作/反代/总闸；上方只放 刷新余额+全部签到，添加账号/批量组/复位保存 在表格下方。
- 配置区两列（wb-config-grid 同款）：左列=单账号并发/熔断/冷却/流空闲（引擎吞吐组），右列=费率过滤(0/0.1/0.2/0.3/0.5/1)+自动签到+签到时点+拒绝未知模型+会话粘性+国际版域；右下角 放弃修改→保存配置。
- 模型目录：realm 徽章+倍率+Ban（scope=realm）。
- 加号三通道：helper 网页授权（铸 EC P-256 设备密钥补齐快照）/粘贴 `token----refreshToken`/原版面板导入 JSON/ZIP；裸 JWT 拒绝。
- i18n：新文案全走 I18N_ZH 词条（键尾无空格、撇号用 JS 双引号包 key——`account's` 未转义炸过整块脚本）；改完跑 `scripts/i18n_audit.py` + `i18n_reverse_audit.py` 双向清零。

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

`internal/app/admin_html.go` 的界面是**英文原文 + `I18N_ZH` 词条表**双层结构。`LANG === 'zh'` 时由三条路径替换：

| 路径         | 实现                                                      | 覆盖对象                       |
| ------------ | --------------------------------------------------------- | ------------------------------ |
| 静态文本节点 | `i18nTextNode` + `i18nSubtree` 的 TreeWalker              | 标签、hint、按钮、表头、说明段 |
| 属性         | `i18nAttrs`（`attributeFilter: ['title','placeholder']`） | 悬浮提示、输入框占位符         |
| JS 字符串    | `T('...')`                                                | toast、动态拼接的行、状态标签  |

**未进词条的文案在中文界面下会原样显示英文**——这是本仓最常见的「功能做完但界面半中半英」缺陷。每次开发新功能或同步上游后，逐项自查：

1. 新增/改动的 HTML 可见文本，在 `I18N_ZH` 里有**同名键**（机制匹配的是文本节点 `trim()` 后的整段原文，不是关键词）。
2. 新增的 `title` / `placeholder` 说明性文案同样要有词条，键为属性值原文（含标点）。
3. JS 动态拼接的文案一律走 `T('...')` 且键在表内。不要把中文直接写进 HTML/JS 模板——切到英文界面会露出中文。
4. 允许不译的只有：model id、key 名前缀、纯示例值（`0.5`、`22`、`4096`）、单位、纯技术标识，以及**标识词与技术值**——域前缀名（TraeWork 的 `trae:`/`solo:`/`trae-intl:`/`solo-intl:`，是模型 ID 组成部分不是文案）、reasoning effort 档位（`xhigh`/`high`/`medium`/`low`/`minimal`）、产品名（`Token Factory`）。2026-10-29 用户定：这类词进词条反而制造假翻译。

排查遗漏：

```bash
# 面板里所有 tooltip 原文，逐个确认在 I18N_ZH 有同名键
grep -o 'title="[^"]\+"' internal/app/admin_html.go | sed 's/^title="//;s/"$//' | sort -u
# 面板里所有 placeholder 原文（示例值可跳过）
grep -o 'placeholder="[^"]\+"' internal/app/admin_html.go | sed 's/^placeholder="//;s/"$//' | sort -u
```

注意 `I18N_ZH` 是 JS 对象字面量：**同名键写两次不会报错，后者静默覆盖前者**，界面会显示成你以为错的那条。新增词条前后都 `grep -n "'<原文>'"` 确认只有一处。

## 9. 面板凭据表格对齐约定（2026-10-29 用户定）

8 个平台的账号/key 表（tbody id：`accountTableBody` / `traeAccountsBody` / `ocKeysBody` / `wbAccountsBody` / `orKeysBody` / `amdKeysBody` / `thKeysBody` / `otherKeysBody`）：**所有列的表头与内容居中，唯独账号/Key 列（各表第 2 列）表头居中、内容左对齐**。实现集中在 `admin_html.go` 的 CSS 追加段（`table:has(#xxxBody) th` + `#xxxBody td` + `td:nth-child(2)` 左对齐覆盖）；flex 容器（状态列的竖排栈、签到竖徽章）`text-align` 管不到，用 `justify-content:center` 单独处理。新增平台表格时把 tbody id 加进该规则，不要逐 td 写内联样式。

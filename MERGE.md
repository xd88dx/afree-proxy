# 上游同步台账

每次同步任务结束后更新本文件：记录基线、目标提交、吸收 / 丢弃的内容与验证结果。
WorkBuddy 一栏的基线同时登记在 `scripts/workbuddy-vendor-baseline.json`（由 `sync_workbuddy.py --seal` 写入）。

**平台归属判定（记错节等于台账失效）**：`foxy1402/cline-proxy` 一个上游同时承载 **Cline** 与 **OpenCode** 两条平台线，记录前必须按改动文件的实际归属分节，不能笼统记在 Cline 下：

| 归属                 | 判据                                                                                                                                                  |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| OpenCode             | `internal/app/zen*.go`、`admin_zen.go`、面板 `oc*` 字段（OpenCode 区块）、`ZEN_*` 配置、`internal/kit/http.go`（`DecodeResponse` 只被 `zen.go` 调用） |
| Cline                | Cline 账号池（`internal/app/proxy.go` 的池/轮换/冷却）、`/v1` 通用转发与方言转换                                                                      |
| TraeWork / WorkBuddy | 各自子系统（`internal/app/trae*`、`internal/workbuddy/**`）                                                                                           |

## Cline

本次未涉及上游同步（2026-10-28 核对：`foxy1402/cline-proxy` main 仍为 `571f9b9`，与 2026-10-27 同步基线一致，无新提交；`571f9b9` 的改动全部属于 OpenCode 线，见文末 OpenCode 节。2026-10-29 复核：仍为 `571f9b9`，无新提交。2026-10-30 复核：上游推进到 `b8a1be0`，但区间内 4 提交全部落在 zen/OpenCode 侧（`git diff 571f9b9 b8a1be0` 无 Cline 侧文件），Cline 线本次零吸收，详见 OpenCode 节。2026-10-31 复核：仍为 `b8a1be0`，无新提交，本次未涉及。2026-10-31 二次复核：上游推进到 `a41f1756`，但区间内 1 提交全部落在 zen/OpenCode 侧（`git diff b8a1be0 a41f1756 --name-only` 无 Cline 侧文件），Cline 线零吸收，详见 OpenCode 节。2026-11-02 复核：仍为 `a41f1756`，无新提交，本次未涉及）。

## TreaWork

**2026-11-04 起上游换线**：`XCool-603/trae2api`（vendor 三方合并线，见下方 2026-11-04 条目）；`autumnsentiment/Trae2api-cn` 仅剩 helper 脚本比对。切换点 tag：`last-trae2api-cn`(49ec6be)。

上游(历史线):`autumnsentiment/Trae2api-cn`(Trae CN / Trae Solo CN 模型反代,Python,MIT)。**这条线不是 vendor/merge 关系**:上游是 Python 全栈,本地 `internal/trae/` + `internal/app/trae.go` 是 Go 重写,行为跟随上游协议;唯一逐行移植的产物是内嵌的本机授权助手 `web_login.py`(`internal/trae/helper_script.go`),只有它需要跟上游比对。2026-10-27 的 cline-proxy 合并未涉及本线。

**2026-10-28 核对**：上游最新 tag 仍为 `v1.0.10`（`bb7b659`），与 2026-10-27 同步基线一致，本线无新版本，本次未涉及上游同步。**2026-10-29 复核**：仍为 `v1.0.10`，无新版本。**2026-10-30 复核**：仍为 `v1.0.10`（tags API 与 main HEAD 均为 `bb7b659`），无新版本，本次未涉及上游同步。**2026-10-31 复核**：仍为 `v1.0.10`，无新版本，本次未涉及上游同步。**2026-10-31 二次复核**：仍为 `v1.0.10`（main HEAD `bb7b659`），helper 比对 exit 0，本次未涉及上游同步。**2026-11-02 复核**：仍为 `v1.0.10`（tags API 与 main HEAD 均为 `bb7b659`），无新版本，本次未涉及上游同步。

### 2026-11-04 — 上游切换：autumnsentiment/Trae2api-cn（比对线）→ XCool-603/trae2api（vendor 三方合并线）

里程碑：切换点已打 tag `last-trae2api-cn`（指向 49ec6be，远程与 `v0.9.0-harvest` 并列两枚里程碑 tag；切换前的最后一版旧线实现）。

- 新上游:`XCool-603/trae2api`(Go、纯标准库零依赖、MIT,26 commits @ `6a861d9` = v0.2.6+9074 退避修复)。与旧上游 Trae2api-cn **无 fork 关系**(独立实现,架构参考同作者 workbuddy2api)。
- 移植方式:**vendor 整树拷贝**(workbuddy 同款机制)——`scripts/vendor_trae2api.py` 把上游 `internal/` 12 个子包拷入 `internal/trae2api/`(排除 desktop/cli/cmd,反向 grep 证实核心包不 import 它们),import 前缀重写 `trae2api/internal/` → `afree-proxy/internal/trae2api/`;`scripts/sync_trae2api.py` 三方合并(基线 `scripts/trae2api-vendor-baseline.json` = `6a861d9` / v0.2.6),源树允许 textcall 四件套 WIP 例外(超出清单拒同步)。
- 上游本地修复保留:另一会话 2026-11-04 实测的文本工具调用兜底 + 4001 空片段修复(`internal/server/chat.go`/`server.go` 改动 + 新文件 `server/textcall.go`/`textcall_test.go`),以 `scripts/trae2api-afree.patch`(9 hunks)随 vendor 对干净树重放,新文件走 PRESERVE_RELATIVE;补丁对干净父提交 135ac11 实测可应用。
- afree 桥接注入(vendor 幂等改写,均为 vendor 包内函数变量,上游原样 nil = 上游行为):出口接管 `traectl.StoreHTTPClient`(主/备绑定 fail-closed + 走池开关,chat/续期/额度/签到四路全收口于 Store 层两个 client 构造点)、反代开关 `pool.RoutingAllowed`(healthyLocked 过滤,维护任务不受影响)、新增身份默认禁用 `accountstore.AccountAddedHook` + `pool.Account.SetLabel`、console 挂载基址 `/admin/trae2api/api`(app.js fetch 前缀改写 + 契约测试候选路径)。
- 旧引擎 `internal/trae/`(13 文件,Trae2api-cn 的 Go 重写)整体删除;helper 资产(web_login.py 模板 + web-auth 入库)迁 `internal/app/trae2api_helper.go`/`trae2api_helper_script.go`;入库改为现场铸造 EC P-256 设备密钥 + 数字设备 ID/机器 ID 补齐快照后走 vendored import 端点(同步结果 + registerAccount 热加池);裸 JWT 无 refreshToken 拒绝(新引擎 Validate 硬校验,续期协议需设备私钥)。`scripts/check_trae_helper.py` 比对线保留(模板逐行未动)。
- 网关格局:trae:/solo: 常驻 + trae-intl:/solo-intl: 随上游 intl 配置透出,**模型名透传零翻译**;Test 按钮沿用上游设计(额度连通性验证,探测模型下拉移除);选号为上游权重调度(idle/额度/余额感知,手动重排序端点移除);面板重构三区(凭据列表/Trae 配置/模型目录)+ 右上角"原版入口"(`/admin/trae2api/`)。
- 备份:trae 段改 `{config, routing, accounts}` 形态;旧引擎形态导入时报错忽略(不迁移决定,存量 `trae-config.json`/`trae-state.json` 留盘作废)。
- **移植后实测（真账号，2026-11-04 凌晨）**：克隆 `trae2api-xcool` 的便携快照（trae-cn-2745955326105609）文件级迁入 `data/trae2api/accounts/` 即入池；签到真实成功（+200 积分 5176→5276）、Test 额度连通 1403ms、**E2E chat 经绑定 EUserv 出口真实回复**（`trae:Doubao-Seed-Code` 0.06x，credit_cost 与目录倍率同源，[req] 日志账号列接通），代理健康 EUserv 25/25 全成功——出口钉定/模型名透传/引擎真实调用全链路实证。
- **实测抓出并修复两个桥接缺陷**：①`trae2apiAccountStates` 持 traeRoutingMu 调 Pool().List()——List→Status→healthyLocked→RoutingAllowed 钩子重入 routing 锁自死锁（有账号后必现，/v1/models 与后台 Pool.Save 全堵死）；修复=先锁内取快照再查池，锁序回归测试 `TestTrae2apiBridgeLockOrder`（-race 并发三路径）。②config/update 漏了网关域校验（OAuth-only 域 api.trae.cn 作网关必失败——旧引擎 normalize 判据未随迁）；补 `trae2apiValidateGateway` + 测试。③routing 显示口径修正：AccountStates 用 traeRoutingGet（查表缺失=启用）而非 map 零值，面板显示与实际路由判定一致。
- **换线后全平台三轮实测（2026-11-04 凌晨~上午）**：①trae 全场景 69 例（参考另一会话 trae2api-diag 的 turn2 四形态/误伤防护/三模型复现种子）+复测 +CN 域干净终扫 18/18——另一会话对上游的 textcall 兜底+4001 空片段修复经 afree 网关实证无回归；`max_tokens=1` 上游稳定 3003（引擎边界，忠实透传）。②全平台 65 目标扫描+newapi 全链路 3/3——实测抓出并修复两处桥接缺口：`/v1/models` 域级可路由过滤（vendored 四域渠道恒全建，0 账号域此前照透、调用全撞 no_account；铁律：列出来的必须调得通）、openrouter/zen 永久错误模型级自动拉黑（cline 有 404/计费双哨兵、在先，OR 404 下线×2+403 agentic×2+400 decisions 端点×1、zen RegionError×2 此前零接线；判据对照铁证：key 级 403 绝不拉黑）。端到端：撞 404 模型即摘除目录、正常模型零误伤（`0a584b6`+`178a3f5`)。
- 验证:`go build ./...`、`go vet ./...` 全绿;`go test ./internal/app/ ./internal/trae2api/... -count=1` 全 ok(12 vendored 包 + app,含上游 server 包的 console 契约闸——vendor 布局候选路径已适配);三方合并演练(基线=父提交 135ac11)报告 2 文件直接合并 0 冲突;i18n 双向审计 0 缺失。

### 2026-10-27 — v1.0.9 (db189e2) → v1.0.10 (bb7b659)

- 基线:上游最新 tag `v1.0.10` = `bb7b659`(2026-10-23,`fix: align account entitlement concurrency and model capabilities`),登记于 `scripts/trae-helper-baseline.json`
- 比对范围:`internal/trae/helper_script.go` 内嵌 `web_login.py` vs 上游 `web_login.py`
- 比对前差异(2 处,均为注释/文档字符串,功能代码无差异):用法行 `[--relay http://<relay>]` 落后于上游 `[--relay http://<relay-host>:<port>]`;缺 `# ---- 部署默认值` 一行注释
- 处理:以上游 `web_login.py` 重建内嵌模板,仅把 `DEFAULT_RELAY_URL = "http://127.0.0.1:8000"` 换成 `__RELAY_URL__` 占位符、按 Go raw string 规则转义反引号;结果为 line-identical(482 行)
- 判据(可复现):`python scripts/check_trae_helper.py --upstream-dir <上游解包目录>` → exit 0;负向测试(向上游追加一行注释)会 exit 1 并打印 unified diff
- 上游取包:`curl -sSL -x socks5h://127.0.0.1:10808 -o /tmp/trae-up.tar.gz https://codeload.github.com/autumnsentiment/Trae2api-cn/tar.gz/refs/heads/main`(该代理链上 `git clone` 常被 TLS 中断,tar 包稳)
- Go 侧行为核对(无机械基线,按上游 release notes 抽查 v1.0.9 / v1.0.10 要点):
  - 已具备:空响应不伪装成功(`internal/app/trae.go:508-512`,对应上游"空流/仅结束帧/纯空白返回明确错误");9074 设备维度换 ID + 代际递增(`internal/trae/checkin.go:20-27`);容量/限流码族 3004 / 4008 / 4011 / 429(`internal/trae/client.go:1177`)
  - **已知架构差异(非缺陷,未跟进)**:上游 v1.0.10 把并发槽做成账号级 entitlement 池(`quota.solo_agent_parallel_limit` + reservation 幂等回收、按账号积分优先调度);本地是全局信号量 `MaxConcurrency`(默认 8,`client.go:1528-1577`)+ 账号 pick 策略(`credit_priority` / `round_robin`,`pool.go:24`)。目标相同(不超上游容量)、粒度不同;要对齐需重写调度层,当前未列计划
  - 未逐条核对项:上游 v1.0.9 的长历史增量前缀哈希、图片压缩跳过、Remote 首错误事件保留等性能与边界优化——本地 Go 实现路径不同,无法机械比对,仅在出现对应症状时回看
- 验证:`go build ./...` OK、`go test ./internal/trae/ -count=1` ok
- 生产部署:镜像 `afree-proxy:prod-20261027-36` 重建容器 `afree`(`--restart always`、`-p 3457:3457`、`-v D:/DockerApp/afree:/app/data`);env 从旧容器继承,哈希 `b54013375c12…` 与旧容器一致;`Up (healthy)`;`GET /`→302、`GET /v1/models`→200;容器内二进制 `grep -ac "部署默认值"` / `"relay-host"` 均命中 1 处,确认 v1.0.10 脚本已编入

## WorkBuddy

### 2026-10-27 — 37b3f4c → 5367e78

- 上游：misakano7545/workbuddy2api-panel，vendor + 三方合并（`scripts/sync_workbuddy.py --source <upstream worktree>`）
- 基线：`37b3f4c` → 本次 `--seal` 后推进到 `5367e78`（`scripts/workbuddy-vendor-baseline.json`）
- 上游新增：`b689cd0`（README 参考仓库清单）、`5367e78`（`/v1/embeddings` 实验端点、`scheduler.makeup_cards_per_round`、下线官方 compensation 模块）
- 冲突（1 文件 2 处）：`internal/workbuddy/server/handler.go`
  - 字段区：上游 `EmbeddingsEnabled` vs 本地 `ModelRateFilter` → 两侧并集保留
  - `livecfg.Snapshot` 构造：同上，两个字段都填
- 跟随上游删除 7 个 compensation 文件：`upstream/compensation{,_test}.go`、`panel/compensation{,_test}.go`、`panel/compensation_frontend_test.go`（本地独有）、`scheduler/compensation{,_test}.go`
- 装配接线（脚本不代劳）：
  - `/v1/embeddings` **未接入本仓路由**（核查后决定，非疏漏）：afree 的 workbuddy 子系统是方法直调内嵌（`serveWorkBuddyChat` 等在 `internal/app/proxy.go` 的 `/v1/chat/completions` 内按模型前缀分流），workbuddy 自身的 `h.mux` 路由表并不挂到主 mux 上——既有的 `/v1/search`、`/v1/fetch`、`/v1/images/*` 同样从未暴露。实测生产 `POST /v1/embeddings` → `404 page not found`。要接入需新增前缀翻译（`wbcn:`/`wbgb:` → `cn:`/`global:`）、认证桥接与 mux 注册，属功能开发而非上游同步，本次不做。
  - 因此：面板**不**暴露 `wbEmbeddings` 开关（避免留一个永远无效的死配置），装配层也不填 `EmbeddingsEnabled`；vendor 树内的 `embeddings.go` 与 `livecfg.Snapshot.EmbeddingsEnabled` 字段随上游保留，将来若接上路由即可直接生效。
  - 未接线：`scheduler.makeup_cards_per_round`（与既有 `night_account_cap` 同策略，不在本地面板暴露，默认 0 = 不限）
- 验证：`go build ./...` 通过；`go vet ./...` 通过；`go test ./... -count=1` 26/27 包 ok，唯一失败 `TestChatStreamPassthrough` 为既有 flaky，与本次合并无关。
  - 根因（已定位，非猜测）：`tokensPerSecond()`（`internal/workbuddy/server/handler.go:2011`）在 `ttfb <= 0` 时返回 `ok=false`，`RecordTokenUsage`（`pool/state.go:395`）据此把 `LastTokensPerSecond` 置 nil；而测试断言（`handler_test.go:462`）要求该值非 nil 且 > 0。测试用 httptest 即时响应，`ttfb` 常被测成 0，于是整包跑必挂、单独跑则过——属测试与环境速度耦合的既有缺陷。
  - 决策（用户 2026-10-27 拍板）：**不修**。该文件是上游测试，改动会与后续上游同步持续产生冲突，收益不抵成本；保留 flaky 并在本台账与 skill 中记录判定方法。

- 生产:与 OpenCode 线同一镜像 `afree-proxy:prod-20261027-35`(一次构建覆盖两条线),部署验证见 OpenCode 节。

### 2026-10-28 — 5367e78 → 49f9db4

- 上游:misakano7545/workbuddy2api-panel,vendor + 三方合并(`scripts/sync_workbuddy.py`;源工作树 `D:/下载/Github/workbuddy2api-panel-misakano` 先 `git checkout --detach misakano/main`)
- 基线:`5367e78` → 本次 `--seal` 后推进到 `49f9db4`
- 上游新增(10 提交 / 41 文件 / +2632 -116),三条主线:
  - 网关思考档位策略(#104 `1792ef1`+`b3623b5`+`e3a6f7a`,#105 `2fe1aa2`,#106 `a5e3de0`):配置新增 `thinking` 节(`enabled`/`default_effort`/`client_priority`/`override_scope`/`model_defaults`)、`upstream/effort_policy.go`、`server/listing.go` 按策略改写 `/v1/models` 的 `reasoning_default_effort` 与 `reasoning_supported_efforts`、面板模型行内档位与「下游可见」开关、配置页策略三选一
  - sanitize:`4f79fc8`(身份句式整句清理、`@` 段换 workspace root、11102 负缓存 TTL)、`db31fc2`(中和 DSH 系统提示里的 `@` 路径说明段)、`49f9db4`(enum 补类型、11-128 搬家抢救、默认监听回环、上游目录监控 `server/watchdog.go`)
  - 测试:`4a577fc`(补 #104 改签名后遗漏的 `prepareBodyOpt` 调用点)、`tool_enum_type_test.go`、`listing_test.go`、`payload_test.go`
- 冲突(4 文件 5 处,全部「两侧保留」,无单向丢弃):
  - `internal/workbuddy/panel/panel.go`:上游 `stalePanelModelsSnapshot()` 降级目录 **vs** 本地 `p.withLiveFlags()` + `global_source` → 探测失败时改用 stale 快照并标 `stale:true`(保留 `WARN` 日志),正常分支 `"models": p.withLiveFlags(out)`;两侧都拿不到才 `502`。
  - `internal/workbuddy/livecfg/livecfg.go`:上游 Thinking 五字段 **与** 本地 `ModelRateFilter float64` 并集保留。
  - `internal/workbuddy/config/config.go`:上游 Thinking 缺省(`enabled=true`/`max`/`client_priority=true`/`level`) **与** 本地地区调度缺省(`domestic`/`international` = `weighted_random`)都保留;`normalize()` 同时调 `normalizeThinking()` 与 `Pool.Strategy.Normalize()`。
  - `internal/workbuddy/config/config_test.go`:冲突块内本地无对应测试,按上游语义重建 `TestThinkingDefaults`/`TestThinkingRejectsInvalid`/`TestThinkingNormalize`(非丢弃语义)。
- 装配接线(脚本不代劳):
  - `internal/app/workbuddy.go`:启动即 `up.SetEffortPolicy(effortPolicyOf(cfg))` 并打印策略日志;`livecfg.Snapshot` 补 `ThinkingEnabled`/`ThinkingDefaultEffort`/`ThinkingClientPriority`/`ThinkingOverrideScope`/`ThinkingModelDefaults` 五个镜像字段。
  - `internal/app/workbuddy_config.go`:新增 `effortPolicyOf()`(`nil` → `nil`,等于完全不碰出站档位);热更新路径同样写五个镜像字段 + `SetEffortPolicy` + 「已热更新」日志——**改档无需重启**。
  - `internal/app/admin_html.go`:WorkBuddy 配置页新增 4 个控件(`thinking.enabled`、`default_effort` 六档下拉、`client_priority`、`override_scope`)与对应中文词条;`thinking.model_defaults`(按模型覆盖)不进表单,手写 JSON 后由深合并保留。
  - `/v1/models` 档位声明**无需额外接线**:`workbuddySubsystem.modelList()` 直接取 `w.server.ModelList()`,`declareReasoning` 在 vendor 内生效。实测 105 条模型里 11 条 wb 模型带 `reasoning_*`:`wbcn:glm-5.3-flash` → `max`(档位表含 max,策略复查生效)、`wbcn:hy3` → `high`(档位表无 max,自动 clamp),证明改写确实按策略走而非透传上游原值。
  - 未接线(核查后决定,非疏漏):
    - `server/watchdog.go` 上游目录监控:需要 alerting 出口,afree 无该管道;`watchdog.enabled` 默认 false,面板**不**暴露开关(避免死配置)。
    - 上游 `app.js` 的模型行内「网关档位」下拉与「下游可见」开关:afree 面板不复用上游前端;`models.hidden` 与 afree 既有模型黑名单语义不同,未合并。
- `internal/config.example.json`:从上游 `config.example.json` 原样拷入(非改写)。上游 `TestConfigExampleParses` 读 `../../config.example.json`——上游在 `cmd/server/`,vendor 后落在 `internal/workbuddy/config/`,该相对路径正好指向 `internal/config.example.json`。**不改上游测试文件**(会被下次同步覆盖)。
- `workbuddyVersion` → `2026.10.28-misakano+afree`,注释同步改记基线 `49f9db4`。
- **移植后实测（真账号，2026-11-04 凌晨）**：克隆 `trae2api-xcool` 的便携快照（trae-cn-2745955326105609）文件级迁入 `data/trae2api/accounts/` 即入池；签到真实成功（+200 积分 5176→5276）、Test 额度连通 1403ms、**E2E chat 经绑定 EUserv 出口真实回复**（`trae:Doubao-Seed-Code` 0.06x，credit_cost 与目录倍率同源，[req] 日志账号列接通），代理健康 EUserv 25/25 全成功——出口钉定/模型名透传/引擎真实调用全链路实证。
- **实测抓出并修复两个桥接缺陷**：①`trae2apiAccountStates` 持 traeRoutingMu 调 Pool().List()——List→Status→healthyLocked→RoutingAllowed 钩子重入 routing 锁自死锁（有账号后必现，/v1/models 与后台 Pool.Save 全堵死）；修复=先锁内取快照再查池，锁序回归测试 `TestTrae2apiBridgeLockOrder`（-race 并发三路径）。②config/update 漏了网关域校验（OAuth-only 域 api.trae.cn 作网关必失败——旧引擎 normalize 判据未随迁）；补 `trae2apiValidateGateway` + 测试。③routing 显示口径修正：AccountStates 用 traeRoutingGet（查表缺失=启用）而非 map 零值，面板显示与实际路由判定一致。
- **换线后全平台三轮实测（2026-11-04 凌晨~上午）**：①trae 全场景 69 例（参考另一会话 trae2api-diag 的 turn2 四形态/误伤防护/三模型复现种子）+复测 +CN 域干净终扫 18/18——另一会话对上游的 textcall 兜底+4001 空片段修复经 afree 网关实证无回归；`max_tokens=1` 上游稳定 3003（引擎边界，忠实透传）。②全平台 65 目标扫描+newapi 全链路 3/3——实测抓出并修复两处桥接缺口：`/v1/models` 域级可路由过滤（vendored 四域渠道恒全建，0 账号域此前照透、调用全撞 no_account；铁律：列出来的必须调得通）、openrouter/zen 永久错误模型级自动拉黑（cline 有 404/计费双哨兵、在先，OR 404 下线×2+403 agentic×2+400 decisions 端点×1、zen RegionError×2 此前零接线；判据对照铁证：key 级 403 绝不拉黑）。端到端：撞 404 模型即摘除目录、正常模型零误伤（`0a584b6`+`178a3f5`)。
- 验证:`go build ./...`、`go vet ./...` 全绿;`gofmt` 干净(vendor 树 LF 直跑;`internal/app` 三个改动文件先 `tr -d '\r'` 再跑);`go test ./internal/app/ -count=1` ok、`./internal/workbuddy/config/ -count=1` ok;全量 `go test ./... -count=1` 仅 `TestChatStreamPassthrough` 失败——既有 flaky,单独 `-run` 复跑 PASS,判据与 2026-10-27 条目一致。
- 容器验证(本机测试容器 `D:/DockerApp`,镜像 `afree-proxy:test-20261028-1023`):`Up (healthy)`、`GET /health` 200;启动日志 `workbuddy: 思考档位策略开,默认档 max(客户端优先=true,作用域=level)`;面板 `GET /admin/workbuddy/api/config` 回读 `thinking={"enabled":true,"default_effort":"max","client_priority":true,"override_scope":"level"}`、`pool.strategy={"domestic":"weighted_random","international":"weighted_random"}`;`POST` 改 `client_priority=false`/`override_scope=all` → `restart_required:[]` 且日志出现「思考档位策略已热更新」→ 回读一致 → 已改回默认值。
- 生产:未同步,等用户「同步」指令(见 PROJECT.md 部署节)。

### 2026-10-29 — 49f9db4 → 9ca34c2

- 上游:misakano7545/workbuddy2api-panel,vendor + 三方合并(`scripts/sync_workbuddy.py`;源工作树 `D:/下载/Github/workbuddy2api-panel-misakano` 先 `git checkout --detach misakano/main`)。
  **fetch 陷阱(本次踩坑)**:该工作树有三个远端——`origin` 指向 xd88dx 的 fork、`upstream` 指向 linguo2625469(已停维护的平行血统,push 已 DISABLED)、`misakano` 才是同步源。核对更新必须 `git fetch misakano main`;本次误 fetch `origin` 拉回 linguo 线的停维护状态(179 提交/-52581 行),差点误判上游删码。
- 基线:`49f9db4` → 本次无冲突自动推进到 `9ca34c2`(8 提交 / 27 文件 / +1880 -237)
- 上游主线:
  - WorkBuddy 5.9.3 客户端请求契约对齐:`ee1c80d`(九处)+ `8ecf115`(国区补六处+抓包黄金头集回归)+ `47f1ed6`(剩余六处)——`headers.go` 大改,新增 `cn_contract_test.go`/`headers_parity_test.go` 两个黄金集回归
  - 6020/6021 渠道排队恢复改为**请求内轮转**,终态改回 429+Retry-After(`f07eba5`/PR #108)——对 afree 的 429 冷却联动透明(终态语义不变)
  - 11-128 拆句策略:首选「拆句留在 system」,搬家降为第二层(`e4dd458`,`prompt.go` + `split_test.go`)
  - 新配置键 `cn.billing_base`(billing/growth 上游 base 覆盖,默认 www.workbuddy.cn,可退回 codebuddy.cn)
  - 配套:`session/ids.go`、`cache_key.go`、`modelbusy.go`、`report.go` 账单直发重构(新 /v2/report 头族)、`auth.IdSource()`
- 冲突:0(53 文件干净合并);上游新增 3 个测试文件;跳过本地已删 `upstream/dns_test.go`(上游 dns_test 回归,本地该功能未接入,维持不复活)
- 脚本适配:`vendor_workbuddy.py` 改写引擎支持**元组计数**——report.go 裸 `c.doJSON(req)` 在基线树 1 处、新上游树 2 处,同一条规则要对两棵构建树都精确命中,固定计数必挂(先报 found 2 再报 found 1 的翻转即此因);规则改 `(1, 2)` 元组,`text.replace` 恒全量替换
- 装配接线(人工):
  - `cn.billing_base`:`workbuddy.go` 启动 + `workbuddy_config.go` 热更新,与 `cn.chat_base` 同口径——vendored `billingBase()` 对 CN **无空值回退**,仅显式非空时覆盖(Global 侧两条路径本次同步前已接线);面板不暴露(与 chat_base 同为手写 JSON 键,避免死配置)
  - `workbuddyVersion` → `2026.10.29-misakano+afree`;restartRequired 清单注释补 `cn.billing_base`
- 核对不合并:`auth.IdSource()`/6020 请求内轮转/headers 契约均为 vendor 内部改造,无新面板或配置面;上游 `models.hidden`(misakano 线 c5841c1 等)与 afree 模型黑名单语义不同,维持 2026-10-28 的不合并决策
- **移植后实测（真账号，2026-11-04 凌晨）**：克隆 `trae2api-xcool` 的便携快照（trae-cn-2745955326105609）文件级迁入 `data/trae2api/accounts/` 即入池；签到真实成功（+200 积分 5176→5276）、Test 额度连通 1403ms、**E2E chat 经绑定 EUserv 出口真实回复**（`trae:Doubao-Seed-Code` 0.06x，credit_cost 与目录倍率同源，[req] 日志账号列接通），代理健康 EUserv 25/25 全成功——出口钉定/模型名透传/引擎真实调用全链路实证。
- **实测抓出并修复两个桥接缺陷**：①`trae2apiAccountStates` 持 traeRoutingMu 调 Pool().List()——List→Status→healthyLocked→RoutingAllowed 钩子重入 routing 锁自死锁（有账号后必现，/v1/models 与后台 Pool.Save 全堵死）；修复=先锁内取快照再查池，锁序回归测试 `TestTrae2apiBridgeLockOrder`（-race 并发三路径）。②config/update 漏了网关域校验（OAuth-only 域 api.trae.cn 作网关必失败——旧引擎 normalize 判据未随迁）；补 `trae2apiValidateGateway` + 测试。③routing 显示口径修正：AccountStates 用 traeRoutingGet（查表缺失=启用）而非 map 零值，面板显示与实际路由判定一致。
- **换线后全平台三轮实测（2026-11-04 凌晨~上午）**：①trae 全场景 69 例（参考另一会话 trae2api-diag 的 turn2 四形态/误伤防护/三模型复现种子）+复测 +CN 域干净终扫 18/18——另一会话对上游的 textcall 兜底+4001 空片段修复经 afree 网关实证无回归；`max_tokens=1` 上游稳定 3003（引擎边界，忠实透传）。②全平台 65 目标扫描+newapi 全链路 3/3——实测抓出并修复两处桥接缺口：`/v1/models` 域级可路由过滤（vendored 四域渠道恒全建，0 账号域此前照透、调用全撞 no_account；铁律：列出来的必须调得通）、openrouter/zen 永久错误模型级自动拉黑（cline 有 404/计费双哨兵、在先，OR 404 下线×2+403 agentic×2+400 decisions 端点×1、zen RegionError×2 此前零接线；判据对照铁证：key 级 403 绝不拉黑）。端到端：撞 404 模型即摘除目录、正常模型零误伤（`0a584b6`+`178a3f5`)。
- 验证:`go build ./...`、`go vet ./...` 全绿;`go test ./internal/workbuddy/... ./internal/app/ -count=1` 唯一失败 `TestChatStreamPassthrough`——既有 flaky,单独 `-run` 复跑 PASS,判定标准同 2026-10-27 条目
- 生产:未同步,等用户「同步」指令(见 PROJECT.md 部署节)。

### 2026-10-30 — 9ca34c2 → 5caddab

- 上游:misakano7545/workbuddy2api-panel,vendor + 三方合并(`scripts/sync_workbuddy.py`;源工作树 `D:/下载/Github/workbuddy2api-panel-misakano` 先 `git checkout --detach misakano/main`)。
- 基线:`9ca34c2` → 本次解决 4 文件冲突后 `--seal` 推进到 `5caddab`(22 提交 / 63 文件 / +3828 -727)
- 上游主线(22 提交):
  - 429 隔离冷却:`22868bb`(普通 429 按模型隔离)+ `e8eca81`(chat 软限流按模型隔离);Kimi 工具兼容只展开 `$ref`、删按工具名硬编码裁剪(`854e74c`,新增 `upstream/kimi_tools.go`)
  - 在途满等待:`9f70e00`(issue #117)新增 `pool.SlotWaitForRealm`(号都在途≠号在冷却,调用方等一个名额而不是直接 503;`server/slotwait_test.go`)
  - 断开记账:`7d43b96` 非流式「调用方断开」记 499 不记 502;`6d595fa` embeddings 6020/6021 改 429+Retry-After 与 chat 同口径
  - 模型降级与校验:PR #107(`5ad7b35`/`a5e8e9c`)保留模型降级配置 + embeddings 响应校验
  - 面板账号池:PR #113(`fd91f42`/`6cdc6fb`)按域(cn/global)分类切换 + `9232c89` 备注按钮直改本机 + 标签自适应;`4850b45` 渲染桩
  - 国际版:`bb6d581` 每日活跃走网页会话链(新增 `upstream/webagent.go` 431 行 + 测试)、签到改「先查状态再领」(新增 `scheduler/checkin_global.go`/`webactivity.go`);`09f2eeb` 国际版无 /v2/chat/busy 不再阻断目录缓存
  - 目录:`c5ac20f` /v1/models 只列官方客户端菜单显示的模型(新增 `upstream/clientpick.go` 按 UA 族裁剪;CN/Global 两域各自对齐)
  - 任务体系:`4d643b1` 任务撞 429 等待重试;`96a33f1` 删已下架 4 任务(Model_chat_GLM5.2/Buddy_App_QQ/create_canvas/black_cat,官方 1018 行任务列表实测均不存在)与整个夜猫子排程;`046b1fd` 账号任务列表里没有的 code 不再空打上游;`a25192e` updater 删刚跑过的 exe 重试
  - 文案:`cbc8aaa`/`5373578`/`5caddab` 注释不再点名已删任务
- 冲突(4 文件,全部两侧融合):
  - `panel/panel.go`:本地已先行实现 `fetchErrs` 拆 `catalogErrs`/`busyErrs`(与上游 09f2eeb 同思路),融合取本地命名 + 上游新语义——busy 失败并入 `busyErrs` 且成功/部分成功响应透出 `busy_errs` 字段、「只有目录错误才拒绝写缓存」;本地 stale 快照回放链保留(先回放 → 部分 stale → 502,`writeErr` 明细改用 `catalogErrs`)
  - `pool/state.go`:上游新增 `SlotWaitForRealm` 整段并入 + 本地 `List` 拖拽排序注释保留
  - `upstream/global_models.go`:上游把 `outInfos`/`ids` 声明提前,本地兜底分支的重复 `:=` 改为复用(`ids = ids[:0]`——outInfos 是 byID 子集)
  - `scheduler/scheduler_test.go`:本地 `TestBlackcatEligibility` 随夜猫子删除
- 跟随上游删除(本地同批为死代码,判据可复核):`scheduler/blackcat.go`、`upstream/blackcat.go`、`scheduler/night_cap_test.go`。判据:本地 `taskKind` 枚举(`scheduler.go:356-364`)只有 checkin/travel/activity/keepalive/growth 五类,无 taskBlackcat;`runBlackcat`/`RunBlackcatNow` 全仓引用仅剩定义文件与自身测试;上游删除理由为官方任务列表实测下架(96a33f1 提交说明)。`upstream/streak_extras.go` 是 blackcat 的继任(新手礼包/heatmap/补签卡),本地 scheduler 已在用。装配层同步删 `BlackcatHours`/`BlackcatEnabled`/`BlackcatDisabled` 引用(`internal/app/workbuddy.go`/`workbuddy_config.go`);config 侧 `Schedule` 结构体字段已随 vendor 树更新,面板本就未暴露夜猫子开关(无死配置)。
- 测试适配:`pool/model_rate_test.go` 的 `TestFallbackCannotBypassModelRateCooldown` 显式 `t.Setenv("UNABLE_FORCE", "1")`——本地 2026-10-28 定的全冷却拒绝默认语义与上游「兜底默认可用」不同,开开关后与上游断言一致(与 OpenCode 线 `TestPickZenKeyStillReturnsWhenAllCooled` 同款处理)。
- 装配接线(人工):
  - `makeup_cards_per_round`:2026-10-27 线已接线(`internal/config.example.json` 同值 0),本轮无新动作。
  - `cn` 配置节本轮无新增键;`webagent`/`checkin_global`/`clientpick` 均为 vendor 内部改造,无新面板/配置面。
  - 上游 `panel/app.js` 模型行改动本地不复用(维持 2026-10-28「面板不复用上游前端」决策)。
  - `workbuddyVersion` → `2026.10.30-misakano+afree`,注释基线改记 `5caddab`。
- 踩坑(记录):合并收尾期间误执行了一次 `git stash -q`(未指定 pathspec),把 62 个 vendor 改动整体收进 stash;诊断 pool 测试时发现 `git status` 全空才回查 stash list,`git stash pop` 完整恢复,无损失。教训:工作区有未提交改动时禁止无差别 stash,排查既有失败应优先用 `git worktree add --detach` 旁路对照。
- 部署(本机测试环境,2026-10-30 17:24):用户指令「同步」。镜像 `afree-proxy:test-20261030-1724` 构建后 compose(`D:/DockerApp/docker-compose.yml`)换 tag 起 `afree` 容器:`Up (healthy)`、`/health` 200(activeAccounts=19)、启动日志正常(19 账号 + WorkBuddy 7、思考档位策略开、zen 目录同步 6 新增、二进制内验证 `inference/openai/v1` 命中)。**过程中发现 wb 模型段不出目录**:定性为 10-30 02:07 一次备份导入触发的既有隔离护栏(`admin_backup.go:37`「导入的上游身份全部禁用」,7 个 wb 账号 `account_enabled=false`,`.bak` 仍 true 可证),与本次同步无关;新容器只是让该门控(`AvailableUIDs` 不含禁用账号 → `workbuddyModelEntries` 返回 nil)如实生效。经面板 API 逐账号恢复启用后 `/v1/models` 102 条、wb 段 11 条(与 2026-10-28 记录一致)。CI「Publish Docker image」对 `98518df` 已 success,GHCR latest 就绪,生产由用户在 iStore 自行 `compose pull`。
- **移植后实测（真账号，2026-11-04 凌晨）**：克隆 `trae2api-xcool` 的便携快照（trae-cn-2745955326105609）文件级迁入 `data/trae2api/accounts/` 即入池；签到真实成功（+200 积分 5176→5276）、Test 额度连通 1403ms、**E2E chat 经绑定 EUserv 出口真实回复**（`trae:Doubao-Seed-Code` 0.06x，credit_cost 与目录倍率同源，[req] 日志账号列接通），代理健康 EUserv 25/25 全成功——出口钉定/模型名透传/引擎真实调用全链路实证。
- **实测抓出并修复两个桥接缺陷**：①`trae2apiAccountStates` 持 traeRoutingMu 调 Pool().List()——List→Status→healthyLocked→RoutingAllowed 钩子重入 routing 锁自死锁（有账号后必现，/v1/models 与后台 Pool.Save 全堵死）；修复=先锁内取快照再查池，锁序回归测试 `TestTrae2apiBridgeLockOrder`（-race 并发三路径）。②config/update 漏了网关域校验（OAuth-only 域 api.trae.cn 作网关必失败——旧引擎 normalize 判据未随迁）；补 `trae2apiValidateGateway` + 测试。③routing 显示口径修正：AccountStates 用 traeRoutingGet（查表缺失=启用）而非 map 零值，面板显示与实际路由判定一致。
- **换线后全平台三轮实测（2026-11-04 凌晨~上午）**：①trae 全场景 69 例（参考另一会话 trae2api-diag 的 turn2 四形态/误伤防护/三模型复现种子）+复测 +CN 域干净终扫 18/18——另一会话对上游的 textcall 兜底+4001 空片段修复经 afree 网关实证无回归；`max_tokens=1` 上游稳定 3003（引擎边界，忠实透传）。②全平台 65 目标扫描+newapi 全链路 3/3——实测抓出并修复两处桥接缺口：`/v1/models` 域级可路由过滤（vendored 四域渠道恒全建，0 账号域此前照透、调用全撞 no_account；铁律：列出来的必须调得通）、openrouter/zen 永久错误模型级自动拉黑（cline 有 404/计费双哨兵、在先，OR 404 下线×2+403 agentic×2+400 decisions 端点×1、zen RegionError×2 此前零接线；判据对照铁证：key 级 403 绝不拉黑）。端到端：撞 404 模型即摘除目录、正常模型零误伤（`0a584b6`+`178a3f5`)。
- 验证:`go build ./...`、`go vet ./...` 全绿;`gofmt -l` 干净(仅 `trae.go` 既有不对齐,HEAD 亦然,非本次引入);`go test ./... -count=1` **28 包全绿**——本轮连既有 flaky `TestChatStreamPassthrough` 也通过(其失败条件 ttfb 测成 0 与机器速度耦合,本轮环境未触发);`updater` 包 13.285s 全绿(其 Windows fork/exec 缺陷未触发)。

### 2026-10-31 复核（vendor 线）

上游 `misakano7545/workbuddy2api-panel` main 无新提交（基线 `96b816d` 不变，版本 dev），`sync_workbuddy.py --report` 确认零改动，本次未涉及上游同步。

### 2026-11-05 — 6d534c5 → dd01a52

- 上游：misakano7545/workbuddy2api-panel，vendor + 三方合并（源工作树先 `git checkout --detach dd01a52`）。
- 基线：`6d534c5` → 解决 2 冲突后 `--seal` 推进到 `dd01a52`（3 提交 / 47 文件 / +810 -30）。
- 上游主线：
  - `e6dd088` 账号分组（`entry.Group`/`stateAccount.Group`/`SetGroup` + 面板 `POST /panel/api/accounts/{uid}/group` 路由 + 分组筛选 UI）+ 事件日志可清空（`POST /panel/api/request_logs/clear`）、下拉记忆、20/30/50 展示条数（issue #159/#160）。
  - `77d4762` 签到响应回显真实 realm，不再写字面量 "cn"。
  - `dd01a52` 出站 tools 脱敏（`upstream/sanitize.go`）、轮询指纹跳过（`upstream/payload.go`）；AutoBot 补 DistPub——不在 vendor 树，本线不涉及。
- 冲突（2 文件，均为本地 2026-11-03 accountfacts 重构 vs 上游单体形态）：
  - `pool/persist.go`：取本地重构形态（entry 只造 placeholder，账号事实走 `applyAccountFactsLocked`、本机统计留 applyAccountsLocked）；上游新增的 `group` 恢复**吸收进 accountfacts.go** 运维意志段（`e.group = s.Group`，纯管理维度无惰性清洗）。
  - `pool/state.go`：保留本地字段清单（`PoolEnabled`/`AccountEnabled` 指针/竖排签到徽章），插入 `Group: e.group` 一行；gofmt 分段对齐已重排。
- 装配核查：无新配置键、无新注入方法（grep 上游全量 diff 确认）；新增 2 面板路由 vendor 内自洽，本地独有路由（`models/refresh`、`accounts/{uid}/test`）未受影响；`restartRequired` 无新增。`workbuddyVersion` → `2026.11.05-misakano+afree`。
- 跳过项（符合预期）：`upstream/dns_test.go` 本地已删、上游仍有，脚本按规则跳过。
- 验证：`go build ./...`、`go vet ./internal/...` 全绿；`go test ./... -count=1` 28 包全绿；`go test -race ./internal/workbuddy/... ./internal/app/` 20 包全绿（既有 flaky 本轮均未触发）。
- 本机测试环境：镜像 `afree-proxy:test-20261105-0253`，compose 换 tag 重建，`Up (healthy)`、`/health` 200，wb 15 账号恢复、模型同步/warm rates 正常；容器内版本串 `2026.11.05-misakano+afree` 已核。
- 生产：未同步，等用户在 iStore 自行 `compose pull`（GHCR 由 CI 从 main 构建）。

### 2026-11-04 — 40d98f9 → 6d534c5

- 上游：misakano7545/workbuddy2api-panel，vendor + 三方合并（`scripts/sync_workbuddy.py`；源工作树 `D:/下载/Github/workbuddy2api-panel-misakano` 本次为 detached HEAD，`--pull` 报 not-on-branch，直接 `git checkout --detach 6d534c5`）。
- 基线：`40d98f9` → **0 冲突**自动推进到 `6d534c5`（21 提交 / 60 文件 / +1530 -105）。
- 上游主线（按子系统归组）：
  - **连接复用族**：`02b941e` 流式请求关闭前读掉流尾 + `Close` 幂等，修上游连接无法复用（`proxypool/transport.go`、`upstream/idle.go`）；`a8be846` 客户端断开不再被记成上游断流、不再白发续写（`server/continue.go`，PR #154）；`b08e606`+`e5afc3f` 心跳帧不再吞掉紧随其后的 Responses 事件（`server/responses.go`、`upstream/sse.go`，PR #158）。
  - **reqlog**：`8f761c1`/`b609bf7` 超长事件行截断+保护，不再让整页事件日志读不出来。
  - **选号**：`2af7de6`/`a49859e` 到期加权不再低于关闭时权重，快过期窗口默认 `168h`→`720h`（PR #146）；`7be4c3b` 保留积分按域覆盖 `pool.reserve_credits_by_realm`（issue #147，键 `cn`/`global`，未写域回落全局阈值）。
  - **面板**：`24b8f39`/`b954462` 默认网关档位可在面板配置+降级实时提示（PR #148）；`ResolveGatewayEffort` 降级不再落到 `off`（off 只作显式目标，防「要弱一档」静默变成「关掉思考」）；`6ddbaf8`/`27217e4` 企业版/API Key 账号不自动签到、不跑成长任务（PR #151）；`5866d6f` 成长任务按阶段 0 快照跳过已完成项、项间统一节流（issue #149）；`6d534c5` 删号后任务中心不再显示幽灵账号作业（issue #155）；`f6c4a45` 登录入口签到判据对齐 `SupportsPersonalGrowth`。
  - **proxypool**：`8d48ee5` 代理全被停用时随机绑定回落直连（issue #150）——**afree 不接 ProxyPool，行为不受影响**。
  - **updater/compose**：`2bf4587`/`4ceee4d` 未知构建版本不误报有更新；`6724698` compose 默认 GHCR 镜像、`fdcbdc7` Dockerfile Go module 代理改 ARG——均在 vendor 树外，本线不涉及；autobot.py/CI 同理。
- 装配接线（人工，上游在 `cmd/server/main.go` 做、本仓无该入口）：
  - 启动 + 热路径成对补 `p.SetReserveCreditsByRealm(...)`（`workbuddy.go:122` / `workbuddy_config.go:156`）——本次唯一新增注入方法（已 grep 上游全量 diff 确认无其他导出接口漂移）。
  - 新配置键 1 个：`pool.reserve_credits_by_realm`，缺省空表=全部回落全局阈值，热生效，不进 `restartRequired`；`expiring_soon` 默认值变更随 `config.go` 带入（存量配置显式写 168h 的保持 168h，仅缺省行为放宽）。
  - `workbuddyVersion` → `2026.11.04-misakano+afree`。
- 跳过项（符合预期）：`upstream/dns_test.go` 本地已删、上游仍有，脚本按规则跳过。
- 验证：`go build ./...`、`go vet ./internal/...` 全绿；`go test ./... -count=1` 28 包全绿（既有 flaky 本轮未触发）。`-race ./internal/app/` 首轮报 `TestClineModelNotFoundAutoBan` flaky——用 `git worktree add --detach .tmp/afree-premerge 37358b7` 在合并前 HEAD 复跑同包同复现（还搭一个 `TestAddAccountDefaultsDisabled`），**判既有、非本次引入**，按 2026-10-27 口径不修；单跑与复跑均过。
- 本机测试环境：镜像 `afree-proxy:test-20261104-0021`，`D:\DockerApp\docker-compose.yml` 换 tag 重建，`Up (healthy)`、`/health` 200，启动日志无错误；容器内二进制版本串 `2026.11.04-misakano+afree` 已核。
- 生产：未同步，等用户在 iStore 自行 `compose pull`（GHCR 由 CI 从 main 构建）。

### 2026-11-02 — 96b816d → 40d98f9

- 上游：misakano7545/workbuddy2api-panel，vendor + 三方合并（`scripts/sync_workbuddy.py`；源工作树 `D:/下载/Github/workbuddy2api-panel-misakano` 先 `git checkout --detach misakano/main`）。
- 基线：`96b816d` → 本次解决 3 文件冲突后 `--seal` 推进到 `40d98f9`（**46 提交 / 116 文件 / +9874 -307**，本线开档以来最大单轮）。
- 上游主线（按子系统归组）：
  - **出站指纹/头族对齐真机**：`80e275e` 出站 TLS 指纹复刻官方 Node/OpenSSL（JA3+JA4 双对齐，新 `upstream/tlsfingerprint.go`，配置 `upstream.tls_fingerprint` 缺省 `node`、env `WB2A_TLS_FINGERPRINT`）；`dd2ec95` 出站请求头发送顺序对齐；`56f479d`/`673a446` 补发官方必发的显式 `Connection` 头（`officialConnectionValue`：`/v3/config`=close、chat/busy/report=keep-alive）；`ee5467c` 头名拼写逐名映射纠正（新 `headcase.go`）；`fbe8d3f`/`01d4f1d` 真机头夹具 + 出站报文探针升级成 CI 门禁。
  - **传输层收敛**：13 个出站点全部收敛到 `c.do(req, a, chat)`（上游重写 `upstream/proxy.go`）；`doJSON` 上游自带 auth 参（`(req, a)` 序）；`673a446`/PR #136 经代理的 HTTPS 先建隧道再走指纹握手，修 SOCKS 绕过代理直连与 HTTP 代理丢指纹。
  - **代理池（PR #122/#135）**：新包 `internal/proxypool`（连接池/TLS over SOCKS/按 realm 与账号绑定），`upstream.Client` 增 `ProxyPool` 字段，panel 增「代理池」视图与账号级绑定 UI，server 四处引用。**afree 不接上游 ProxyPool**（workbuddy 出口走本仓 ProxyFor 钉定），处理见下。
  - **选号新特性**：`b2d130d` 窗口用量调度（issue #112，`pool.balance_window` 缺省 1h，权重 /= 1+used/mean 同层均值，热生效）；`8903487` 模型别名支持通配 + Claude Code 计数端点（PR #140）；`2d23068` 跟进 #102/#99/#39/#3（含 `prompt.keep_recent_rounds` 只保留最近 N 轮，issue #99）。
  - **排程/国际版**：`78be43d` 国际版只留旧桌面端 workbuddy.ai、下架 codebuddy.ai 入口；`schedule.web_activity_enabled`（issue #111）网页通道活跃腿**缺省关**。
  - **修复族**：`6562731` 6020 带 retry_after 不再换号重打（#125）；`4b7a805` 域名被路由拒绝（15021）跨域兜底且不罚号（#130）；`4d7d1e6` usage 跨段合并 prompt 取末段（#134）；`192267a` chat 流水补 request_id（#128）；`9b9174b` model_fallback 支持带域前缀键（#127）；`47d1e2f` 运行日志增量游标改单调 seq（#126）；`bd0cc1d` 登录 client 禁 h2 + 传输层重试（#129）；`e2e40ca` web_search_result 补 encrypted_content/page_age（#121）；`7490cfb` /v1/models 追加 models.dev 形状能力声明（#119）。
  - **面板**：`6020a64` 鉴权改「默认拒绝」（`ServeHTTP` 统一入口 + `publicPanelPaths` 白名单，新增路由漏包不再静默公开）；`39fba11` 配置页分域编辑 cn/global 的 chat_base/billing_base（#131）；`771da0f` 事件日志区分客户端原始档位与网关补档（#132）；`76727b8`/`410398f` 降级标注按域命中（#137）；`8ab87d4` 请求上下文弹窗；`1a81f25` 跨账号合计默认收起；`56bef12` 代理池视图（见上）。
  - **logging**：`logging.request_archive_context`（缺省 false）请求事件可记会话上下文（system+messages），上游注释明示是排障临时手段。
  - autobot.py/CI 大量迭代（Hermes 报告自动化）——不在 vendor 树内，本线不涉及。
- 冲突（3 文件，人工逐块解决）：
  - `upstream/client.go`（1 块）：chat 出站块。取上游 `c.do(req, a, true)` 形态；本仓手工适配的握手耗时观测（hsStart/observeProxy）**收进 `doChat`**（语义不丢）；错误分支清池改 `c.closeIdleEgress(a)`——上游原形态只清基础 chat Transport，账号钉定出口时要清对应代理 Transport。
  - `panel/panel.go`（2 块）：上游 6020a64 把逐条鉴权改为 ServeHTTP 默认拒绝，路由表去 withAuth 包装。路由表取上游裸形态（鉴权由 ServeHTTP 统一覆盖）；**本仓独有路由补回**：`POST /panel/api/models/refresh`（本地模型刷新）与 `POST /panel/api/accounts/{uid}/test`（本仓单号探测端点，基线上游本就没有）。本仓 `withAuth` 方法随上游删除（被默认拒绝取代）。
  - `pool/pick.go`（1 块）：上游窗口用量降权 vs 本仓地区调度分支同挂点。两侧融合：本地策略分支（`pickOrderedLocked` 顺序路径）先行早退，上游 `uctx`/`usageDiv` 权重循环随后——顺序策略不走权重、加权随机路径获得用量降权。
- 脚本适配（`vendor_workbuddy.py`，合并前修改）：
  - `PACKAGES` 补 `"proxypool"`（上游/panel/server 三处引用，漏了会 silently 缺包）。
  - `EGRESS_REWRITES` 重构为**双树规则**：本仓 `(a, req)` 参数序保留——基线树规则 `(1,0)` 保证 base==ours（历史合并产物形态），新树规则 `(0,n)` 把 theirs 统一成本地形态，两侧一致则三方合并零冲突；旧「`chatHTTP().Do(req)`→chatClientFor」直呼改写作废删除（上游已收敛 `c.do`，`roundTripCloseIdle` 改映射 `c.closeIdleEgress(a)`，两树通用）。
  - `apply_panel_iframe_rewrites` 补元组计数支持（与 EGRESS 同口径，基线树无代理池导航项计 0）；`panel/index.html` 加规则隐藏「代理池」导航 `<li hidden>`——afree 不接 ProxyPool（`cfg.ProxyPool` 恒 nil，API 一律 501），按死配置红线不给一个点了就报错的入口；账号行绑定 UI 本就有 `proxyData` 空守卫不受影响。
  - 测试适配：`decode_body_test.go` 2 处内联 `&auth.Auth{...}` 的 `doJSON(req, …)` 调用换序为 `(…, req)`（规则只覆盖字面 `a` 形态，内联字面量逃逸）。
- 装配接线（人工，上游在 `cmd/server/main.go` 做、本仓无该入口）：
  - 启动 + 热路径成对补：`p.SetBalanceWindow(BalanceWindowDur)`、`up.SetTLSFingerprint(...)`（错值报日志保持 node 档；`validateTLSFingerprint("")`→node 已核对）、`up.KeepRecentRounds.Store(...)`、`sch` 参数与 `Reconfigure` 补 `WebActivityDisabled`；`livecfg.Snapshot` 与 server.Config 静态字段补 `RecordContext`。
  - 新配置键 5 个全部有缺省且**全部热生效**（`restartRequired` 注释清单同步补记，无一入重启清单）：`upstream.tls_fingerprint`=node、`prompt.keep_recent_rounds`=0、`pool.balance_window`=1h、`logging.request_archive_context`=false、`schedule.web_activity_enabled`=false。
  - `workbuddyVersion` → `2026.11.02-misakano+afree`；`internal/config.example.json` 从上游原样拷入。
- 验证：`go build ./...`、`go vet ./...` 全绿；`gofmt` vendor 树与手改文件干净；`go test ./... -count=1` **28 包全绿**（连既有 flaky 也未触发）；`CGO_ENABLED=1 go test -race ./... -count=1` 全绿（上游引入 TLS 指纹/代理池并发面，race 不跳过）。
- 本机测试环境：镜像 `afree-proxy:test-20261102-1345`（compose tag 实测生效，容器内二进制版本串 `2026.11.02-misakano+afree` 已核），`D:\DockerApp\docker-compose.yml` 换 tag 后 `docker compose -f … -p tokenapp up -d afree` 重建（注意：必须在 `-f D:\DockerApp\docker-compose.yml` 下执行，仓库内 compose 同目录解析会因 `afree-proxy` 服务缺 `.env` 插值失败）；`Up (healthy)`、`/health` 200，启动日志无错误：wb 7 账号全恢复、zen 冷却 13 token、cline 19 trace、proxy 17 状态、218 用量桶，`restart=always` 保持。
- 生产：未同步，等用户在 iStore 自行 `compose pull`（GHCR 由 CI 从 main 构建）。

### 2026-10-31 — 5caddab → 96b816d

- 上游:misakano7545/workbuddy2api-panel,vendor + 三方合并(`scripts/sync_workbuddy.py`;源工作树先 `git checkout --detach misakano/main`)。
- 基线:`5caddab` → 本次 0 冲突自动推进到 `96b816d`(2 提交 / 19 文件 / +1044 -186)。
- 上游主线:
  - `847deb1` WorkBuddy 5.9.4 国际版真机抓包对齐:`headers.go` 大改(+162)——`X-Private-Data` 头、UA 按桌面/CLI 分族、`baggage` 头、XRW 形态;新增 `upstream/ua_family_test.go`/`gl_contract_test.go`(国际版头族黄金集);`webagent.go` 上游同区域有改动,与本地的 race 原子计数修复三方合并自动融合(0 冲突,`sseEvents`/`sseChunks` 保留)。
  - `96b816d` 设备码登录链路改桌面端口径(platform/UA/头族):`loginsite.go` +200、`panel/login.go` +91,设备码轮询/激活走桌面端形态。
- 装配核查:两提交均为 vendor 内部协议层改造,无新配置键/面板面;`restartRequired` 清单无新增。
- `workbuddyVersion` → `2026.10.31-misakano+afree`;基线 seal 至 `96b816d`。
- **移植后实测（真账号，2026-11-04 凌晨）**：克隆 `trae2api-xcool` 的便携快照（trae-cn-2745955326105609）文件级迁入 `data/trae2api/accounts/` 即入池；签到真实成功（+200 积分 5176→5276）、Test 额度连通 1403ms、**E2E chat 经绑定 EUserv 出口真实回复**（`trae:Doubao-Seed-Code` 0.06x，credit_cost 与目录倍率同源，[req] 日志账号列接通），代理健康 EUserv 25/25 全成功——出口钉定/模型名透传/引擎真实调用全链路实证。
- **实测抓出并修复两个桥接缺陷**：①`trae2apiAccountStates` 持 traeRoutingMu 调 Pool().List()——List→Status→healthyLocked→RoutingAllowed 钩子重入 routing 锁自死锁（有账号后必现，/v1/models 与后台 Pool.Save 全堵死）；修复=先锁内取快照再查池，锁序回归测试 `TestTrae2apiBridgeLockOrder`（-race 并发三路径）。②config/update 漏了网关域校验（OAuth-only 域 api.trae.cn 作网关必失败——旧引擎 normalize 判据未随迁）；补 `trae2apiValidateGateway` + 测试。③routing 显示口径修正：AccountStates 用 traeRoutingGet（查表缺失=启用）而非 map 零值，面板显示与实际路由判定一致。
- **换线后全平台三轮实测（2026-11-04 凌晨~上午）**：①trae 全场景 69 例（参考另一会话 trae2api-diag 的 turn2 四形态/误伤防护/三模型复现种子）+复测 +CN 域干净终扫 18/18——另一会话对上游的 textcall 兜底+4001 空片段修复经 afree 网关实证无回归；`max_tokens=1` 上游稳定 3003（引擎边界，忠实透传）。②全平台 65 目标扫描+newapi 全链路 3/3——实测抓出并修复两处桥接缺口：`/v1/models` 域级可路由过滤（vendored 四域渠道恒全建，0 账号域此前照透、调用全撞 no_account；铁律：列出来的必须调得通）、openrouter/zen 永久错误模型级自动拉黑（cline 有 404/计费双哨兵、在先，OR 404 下线×2+403 agentic×2+400 decisions 端点×1、zen RegionError×2 此前零接线；判据对照铁证：key 级 403 绝不拉黑）。端到端：撞 404 模型即摘除目录、正常模型零误伤（`0a584b6`+`178a3f5`)。
- 验证:`go build ./...`、`go vet ./...` 全绿;`gofmt -l` 干净(仅 `trae.go` 既有);`go test ./... -count=1` 28 包全绿;`CGO_ENABLED=1 go test -race ./internal/workbuddy/... ./internal/app/` 22 包全绿。

- **race 补录（同日）**:用户要求补装 C 编译器跑 race,不再跳过。装 winlibs gcc 16.2.0(便携版,`C:/AppSelf/Mingw`,与 Go 同目录约定,zip 已删)。`CGO_ENABLED=1 go test -race ./... -count=1` 首轮 25 包 ok、3 失败,**全部是本次同步引入的真数据竞争**,当场修复:
  - `upstream/webagent.go`(上游 bb6d581 新增):读 SSE 的 goroutine 直接 `res.Events++`/`res.Chunks++`,主 goroutine 轮询循环同时读写同一 `WebTurnResult`(上游注释明说"不 join"),race 检出 334 写 vs 383/386 读。修复:计数改局部 `atomic.Int64`(`sseEvents`/`sseChunks`),各返回点合并回 `res`——原子读无竞争;数值可能少最后一两帧,纯观测统计可接受。
  - `panel/models_busy_cache_test.go`(上游新增测试固件):`cfgHits`/`busyHits` 局部 int 被 global 三路并发探测 goroutine 同时 `++`。修复:改 `atomic.Int64`。
  - 复跑 `CGO_ENABLED=1 go test -race ./... -count=1`:**28 包全绿**;普通模式 `go test ./... -count=1` 同样 28 包全绿(既知 flaky 本轮未触发)。
  - 教训:普通模式全绿不代表无竞争——上游"不 join 的 goroutine 直写共享 struct"模式普通模式测不出,race 是唯一检出手段。vendor 线引入并发代码时 `-race` 不应再跳过。
- 生产:未同步,等用户「同步」指令(见 PROJECT.md 部署节)。

## OpenCode

### 2026-10-27 — a99d94b → 571f9b9

- 上游：foxy1402/cline-proxy（与 Cline 同一上游仓库），合并方式 `git merge --no-ff`
- 上次同步 merge：`ff56cef`（第二父 = 基线 `a99d94b`）
- 本次目标：`571f9b9` — zen: decode compressed upstream bodies, fix env-marker edge cases（区间内唯一上游提交）
- 平台归属：**本次改动全部落在 zen/OpenCode 侧，未触碰 Cline 侧任何文件**（`git diff --name-only f9a0e13 3d73dd1 | grep -i cline` 为空）。唯一共享路径核过：`kit.ReadBody` 被 `internal/cline/auth.go:65,134` 调用，本次给它加了防御性解码；但 `cline/auth.go` 不设 `Accept-Encoding`，`net/http` 已自动解压并删除编码头，`DecodeResponse` 见无编码头直接放行 → Cline 侧实际行为不变。
- 涉及文件（13）：`internal/app/{zen.go, zen_session.go, admin_zen.go, admin_html.go, config.go, toolmerge_test.go, zen_decode_test.go, zen_env_marker_array_test.go, zen_key_test.go}`、`internal/kit/http.go`、`go.mod`、`docker-compose.yml`、`README.md`
- 吸收：
  - `kit.DecodeResponse`：gzip/deflate/br/zstd 解码并清掉 `Content-Encoding`/`Content-Length`，未知编码 fail-open；zen 两条路径（普通 `zen.go:2320` + 流式 `zen.go:2603`）响应到达后立即调用。原因：Accept-Encoding 显式对齐 CLI 指纹后 net/http 不再自动解压，大响应体会以压缩字节直送 JSON/SSE 解析器。
  - `zenContentText` 归一化 string / parts 数组 / 单 part 三种 content 形态；marker 注入改为先深拷贝消息与 parts 再追加，不再就地改写客户端 params；数组内容追加 text part 而非新增 message。
  - `tool_choice` 分流（`zen.go:1231`）：无客户端 tools → `none`；有 tools 无偏好 → `auto`；显式偏好 → 透传。Cline 侧的 `enforceToolChoiceNone`（`proxy.go:1082`）是本地既有逻辑，本次未改。
  - `ZEN_ORG_ID` → `x-opencode-org-id` 面板侧补齐：`ocOrgID` 输入（OpenCode 区块）、配置 GET 的 `orgID`、patch 结构体 `OrgID *string`、`applyPatch` 显式保留 `OrgID`（全量替换语义）、compose、README 表格。空值不发该头（原始 API key 不含组织信息）。
  - 测试：新增 `internal/app/zen_decode_test.go`、`internal/app/zen_env_marker_array_test.go`；`zen_key_test.go`、`toolmerge_test.go` 断言随上游更新。
- 丢弃：
  - `startZenSessionRotator` / `SessionRotateMinutes` / 面板 `ocSessionRotate`：上游判定为 inert（a99d94b 起每次 API 调用新铸会话），本地早已删除，不回填。
  - 上游 failover 面板字段（`ocFailover*`）：本地以代理隔离 + egress 钉定 + 备用代理开关实现等价能力。
  - 上游 README / docker-compose 英文示例段与 `ZEN_KEYS` 条目：Go 源码不读取 `ZEN_KEYS`（HEAD 与工作区 grep 均空），文档以本地中文面板化为准。
- 本地定制（逐项确认保留）：`PROXY_ISOLATION`/`proxyIsolationEnvLocked`、`backupProxyEnabled`、`proxyExclusions`、`proxyBindCaps`、`pinProxyPerKey`、`zenEgressSnapshot`、持续性 403 闸门（`zenSessionRefreshedRecently`）、Workbuddy / Trae 分区。
- 顺手修正：`zen_decode_test.go` 的 import 由 `cline-go-proxy/internal/kit` 改为 `afree-proxy/internal/kit`（上游 module 名残留，会让 `go vet ./...` 失败）。
- 验证：`go build ./...` 通过；`go vet ./...` 通过；`go test ./internal/app/ -run 'Zen|Session|OrgID|Egress|Decode|Gate' -count=1` → ok。
- 生产：镜像 `afree-proxy:prod-20261027-35` 重建 `afree` 容器（`--restart always`、`D:/DockerApp/afree:/app/data`、`-p 3457:3457`），env 全量哈希与旧容器一致（`9eb6f4999012f044`，未丢凭据），状态 healthy；`GET /`→302 登录页、`GET /v1/models`→200；镜像内验证 `ocOrgID` 3 处、`ocSessionRotate` 0 处。
- **2026-10-28 核对**：`foxy1402/cline-proxy` main 仍为 `571f9b9`，无新提交，本次未涉及本线上游同步。

### 2026-10-31 — 修复:面板保存清空 zen 运行态历史段（数据遗失事故）

- **现象**:用户报告 OpenCode 历史数据遗失——`zen-config.json` 的 `cool`（冷却表）/`keyTokens`（token 记账）段消失,面板「今日/累计 token」清零。
- **根因**(链条三层放大):
  1. `handleZenConfigUpdate`(admin_zen.go:132)构造 `next` 时**逐字段枚举拷贝,漏掉 `Cool`/`KeyTokens`**(以及 2026-10-30 新增的 `FailoverCount`/`FailoverMinutes`);
  2. `setZenConfig` 的回填语义是「冷却/记账表**以配置为准**」(注释明示管理导入=忠实还原)——next 里这两段为 nil,于是**包级运行态 `zenKeyCool`/`zenKeyTokens` 被清零**;
  3. 任何后续 `saveZenConfig`(`saveKeyEdits` 的 /opencode/keys/proxy、token 记账落盘等)从包级快照回写——**空表固化落盘**。
- **触发面**:面板「保存排除代理规则」(`/opencode/config/update`,patch 含 proxyExclusions/proxyBindCaps)是最高频入口;「凭据代理重排/一键代理」(`/opencode/keys/proxy`)本身安全,但在 1 之后执行会把丢失固化。10-31 02:26 iStore 面板触发,02:31 rsync `-Deploy` 把坏配置倒灌本机并重启容器(本机 StartedAt 02:31:10 实证)。**同型事故第三次**:2026-10-29 KeyRoutingEnabled、2026-10-29 Cool(审计修复 saveZenConfig 回写,但 next 漏拷未修)、2026-10-31 本次——逐字段枚举是结构性缺陷。
- **修复**:next 改为 `next := *cur` 浅拷贝起步、按 patch 覆盖(`reconcileZenKeyRouting`/`setZenConfig` 调用点改传址)——今后 zenConfigData 新增任何字段都不可能再从这里漏。回归测试 `TestZenConfigUpdateKeepsRuntimeSections`:面板保存后运行态冷却/记账/绑定与磁盘三段全部保留;**反向验证**(回退旧代码跑同一测试)确认测试真能抓住该 bug。
- **数据后果与恢复**:丢失的 cool(20 分钟封顶的短时状态,自愈)与 keyTokens(记账,无法找回,从零重计)不可恢复——rsync 备份目录与 iStore 侧均无丢失前版本;`modelAliases`/`endpoints` 在 iStore 本为空(无学习事件,`omitempty`),非丢失。iStore 生产侧同样中招(02:26 写入),待用户下次部署 GHCR 新镜像后自愈;本机运行态已随修复后的镜像恢复记账能力。
- **其他平台排查**(同型「全量替换+逐字段重建」模式):`amd.go:250`/`openrouter.go:226,435`/`tokenharbor.go:198` 的 next **均已拷 KeyTokens/Cool**(写得晚、字段已存在);`otherCloneConfig` 深拷含运行时表;`traeCloneConfig` 显式拷 Cool(trae 无 keyTokens);WorkBuddy 配置与运行态分文件;cline 账号编辑走单账号端点。**唯一漏的就是 zen**——已修。
- fork 上游线核对:`foxy1402/cline-proxy` main 仍为 `b8a1be0`,无新提交,本次无 fork 侧吸收。
- 本机测试环境更新(同日):镜像 `afree-proxy:test-20261031-0302` 构建,compose 换 tag 起 `afree` 容器——`Up (healthy)`、`/health` 200(activeAccounts=19),旧镜像按 7 个窗口清理。
- **移植后实测（真账号，2026-11-04 凌晨）**：克隆 `trae2api-xcool` 的便携快照（trae-cn-2745955326105609）文件级迁入 `data/trae2api/accounts/` 即入池；签到真实成功（+200 积分 5176→5276）、Test 额度连通 1403ms、**E2E chat 经绑定 EUserv 出口真实回复**（`trae:Doubao-Seed-Code` 0.06x，credit_cost 与目录倍率同源，[req] 日志账号列接通），代理健康 EUserv 25/25 全成功——出口钉定/模型名透传/引擎真实调用全链路实证。
- **实测抓出并修复两个桥接缺陷**：①`trae2apiAccountStates` 持 traeRoutingMu 调 Pool().List()——List→Status→healthyLocked→RoutingAllowed 钩子重入 routing 锁自死锁（有账号后必现，/v1/models 与后台 Pool.Save 全堵死）；修复=先锁内取快照再查池，锁序回归测试 `TestTrae2apiBridgeLockOrder`（-race 并发三路径）。②config/update 漏了网关域校验（OAuth-only 域 api.trae.cn 作网关必失败——旧引擎 normalize 判据未随迁）；补 `trae2apiValidateGateway` + 测试。③routing 显示口径修正：AccountStates 用 traeRoutingGet（查表缺失=启用）而非 map 零值，面板显示与实际路由判定一致。
- **换线后全平台三轮实测（2026-11-04 凌晨~上午）**：①trae 全场景 69 例（参考另一会话 trae2api-diag 的 turn2 四形态/误伤防护/三模型复现种子）+复测 +CN 域干净终扫 18/18——另一会话对上游的 textcall 兜底+4001 空片段修复经 afree 网关实证无回归；`max_tokens=1` 上游稳定 3003（引擎边界，忠实透传）。②全平台 65 目标扫描+newapi 全链路 3/3——实测抓出并修复两处桥接缺口：`/v1/models` 域级可路由过滤（vendored 四域渠道恒全建，0 账号域此前照透、调用全撞 no_account；铁律：列出来的必须调得通）、openrouter/zen 永久错误模型级自动拉黑（cline 有 404/计费双哨兵、在先，OR 404 下线×2+403 agentic×2+400 decisions 端点×1、zen RegionError×2 此前零接线；判据对照铁证：key 级 403 绝不拉黑）。端到端：撞 404 模型即摘除目录、正常模型零误伤（`0a584b6`+`178a3f5`)。
- 验证:`go build ./...`、`go vet ./...` 全绿;`go test ./... -count=1` 28 包全绿(含新回归测试);`CGO_ENABLED=1 go test -race ./internal/app/` ok。

### 2026-10-31 — b8a1be0 → a41f1756

- 上游：foxy1402/cline-proxy（与 Cline 同一上游仓库），合并方式 `git merge --no-ff --no-commit a41f1756`
- 上次同步：`227d302` 前的基线 `b8a1be0`（2026-10-30 同步）
- 本次目标：`a41f1756` — 区间 1 提交：`zen: park day-exhausted keys instead of retrying every 20 minutes`
- 平台归属：**OpenCode 线**（`git diff b8a1be0 a41f1756 --name-only`：TODO.md、admin_html.go、admin_zen.go、zen.go、zen_fanout_test.go、zen_park_test.go，全部 zen 侧；Cline 零吸收）
- 吸收项（带原因）：
  - **zen key 日额度耗尽停放（park）机制**：429 按 `zenDailyExhausted`（body=FreeUsageLimitError + RA 指向 00:00 UTC ±30min + 非 FreeTierError）分流——瞬时节流仍走 20min 冷却，日额度耗尽改停放（30min 起步指数退避至 6h 封顶，停放期内零上游请求，到期只放一个探针）。解决"日配额用完的 key 每 20 分钟被重试一次、白停一整天"的问题。
  - `parkZenKey`/`zenDailyExhausted`/`anyZenKeyParked`/`zenAllParkedUntil`/`zenParkProbeCount`/`zenKeyParked` 等函数与 chat/responses 两路径的 attempt-0 全池停放拦截、200 成功解除停放、面板 parked 状态显示（admin_zen.go status API 的 parked/parkedUntil/parkProbes/parkReason 字段自动合并）。
- 本地定制保留清单（冲突解决原则"本地框架 + 吸收 park 语义"）：
  - `pickZenKey` 保留本地三轮候选 + 调度策略框架（fill/random/round_robin、blocked/routingOff/noEgress 过滤、UNABLE_FORCE 兜底门控），parked 过滤并入 buildCandidates；上游 `eligible`/`bound`（egress 绑定快照）不并入——本地 `noEgress` 覆盖同语义。
  - `recordZenKeyTokens`（上游名 markZenKeySuccess）保留本地 zenKeyTokens 记账 + 吸收 200 解除停放 `delete(zenKeyPark, key)`；上游 `zenKeyUsage` 成功计数不并入（本地由 zenKeyTokens 替代）。
  - `admin_html.go` key 表行渲染保留本地 egCombo/routing/拖拽整段，parked 状态吸收进 stCell 判定链（优先于冷却，新词条 `Quota parked`→额度停放）。
  - 上游第三轮"直通停放中的 key"的形态不吸收——调用方 `zenAllParkedUntil` 在 attempt 0 先行拦截报错，直通到不了且违反"停放期内零请求"语义（上游注释宣称的调用方检查实际不存在，属未完成意图）。
  - TODO.md 保持本仓删除（modify/delete 冲突取删除侧）。
- 测试本地适配（上游 3 处时间炸弹/字段差异 + 1 处渗漏）：
  - `zen_park_test.go` 写死 `Retry-After: 80378`（上游提交时刻的午夜倒计时，非该时段运行必挂）→ 改运行时 `retryAfterToMidnight()` 动态值。
  - `cfg.Enabled`/`cfg.Proxies` 引用删（本地 zenConfigData 无这两个字段，zen 启停由可路由性闸判定）。
  - `markZenKeySuccess` 调用改本地 `recordZenKeyTokens`（带 tokens 参数）。
  - `setZenModelForTest` 三处裸调补 `savedModels` cleanup（上游渗漏 bug，模板 zen_key_test.go:56-61）——不补则 zen_prefix/zen_route 三测试全量挂。
- 验证：build/vet 绿；`go test ./... -count=1` 全绿；`go test -race ./internal/app/` 绿。
- 生产：未同步，等用户「同步」指令。

### 2026-10-30 — 571f9b9 → b8a1be0

- 上游：foxy1402/cline-proxy（与 Cline 同一上游仓库），合并方式 `git merge --no-ff`
- 上次同步 merge：`e60437c` 线上的基线 `571f9b9`（2026-10-27 同步）
- 本次目标：`b8a1be0` — 区间 4 提交，**全部落在 zen/OpenCode 侧**（`git diff 571f9b9 b8a1be0` 无 Cline 侧文件；`proxy.go` 的改动集中在 `handleZenChat`/`handleZenAnthropic` 等 zen 函数）
- 平台归属：**OpenCode 线**，Cline 零吸收（Cline 节已登记）
- merge commit：`7849181`，冲突 5 文件（README.md/admin_html.go/egress_test.go/proxy.go/zen.go）+ TODO.md modify/delete
- 吸收：
  - `a31d5bb4` base path 修复：`zenAPIBase` 更正为 `https://opencode.ai/inference/openai/v1`（CLI 2.1.4 抓包实证；旧 `/inference/v1` 少 `/openai` 段，来自 2026-10-15 一次过时观测）；新增 `zenStaleBase` 存量迁移（只迁移恰等于旧默认值的配置）；`fetchZenRegistry` 增返回 `providerAPI`（opencode provider 的 api 字段），`applyZenRegistryBase` 每次目录同步按官方目录自愈基地址——只改「项目默认值」，面板手填的不动；拒绝任何非 `https://opencode.ai/inference/...` 的目录值（防篡改目录把带 Authorization 的请求引去别处）
  - `2870f747` 429 fan-out 预算：`max429KeySwitches=4`——换 key 是请求内唯一不可逆操作，不设上限时一个大请求 4 秒冷却整池（2026-10-30 上游实测 5 key/4s）；实际生效值与 retries 取小，日志 `key-switch budget exhausted` 标记停止点
  - `0e216a09` 冷却封顶：`zenRateLimitCooldown=20min`——上游 `Retry-After` 是「距下一个 00:00 UTC」的倒计时而非真实恢复时间（实测 RA=20h46m 而 18 分钟后同 key 恢复 200），按 RA 冷藏会让多 key 池白停一整天；短 RA 与缺头 1 分钟回落不变
  - `2870f747` 压缩阈值：`defaultCompactBuffer` 20000→80000——旧值对「客户端上限 128k < 模型阈值 168k」场景压缩永不触发（那次 429 的直接成因）；`legacyCompactBuffer=20000` 只在恰等于旧值时迁移
  - `b8a1be0` 429 快速失败：429 不推进 `markZenFail`（限流不是降级，注释随上游并入）；无可轮转 key 时跳过 30s 睡眠立即交还 429（上游实测单 key 池 101s→0.01s）；`zenClientErrorStatus` 把上游 429 透传给客户端（回 502 会让 IDE 判定网关坏），聚合/解析类错误保持 502
  - 面板：`ocBaseURL` placeholder、`ocCompactBuffer` 缺省 80000、`loadOcConfig`/`saveOcConfig` 缺省值同步
  - 测试：新增 `zen_baseurl_test.go`（4 用例）、`zen_body_wire_test.go`（include_usage 强制/纯 usage 帧过滤）、`zen_fanout_test.go`（11 用例，覆盖预算/封顶/快速失败/轮转）；`zen_catalog_seed_test.go` 适配新签名
- 本地适配（上游架构差异，全部人工逐项核对）：
  - `proxy.go` 整体取本地形态（本地是缓冲流架构且 2026-10 重构时已删上游的 `zenStatsTracker` 统计体系/`stats.go`），再手工摘入 6 处 `zenClientErrorStatus` 429 透传（chat/responses/anthropic 三路径 + 缓冲流 attempt 分支；cline 等平台错误非 `zenHTTPError` 仍回 502）
  - 上游 `include_usage` 强制注入保留在 `buildZenBody`（zen.go，本地同函数）；纯 usage 帧过滤移植到本地 `replaySSE`（新增 `clientWantsStreamUsage`/`isUsageOnlyChunk`，`keepUsageOnly=false` 时回放层吞帧——与上游直通路径 `handleStreamResponseUsage` 语义等价，缓冲架构下过滤点后移）
  - `markZenSuccess`/`markZenFail`/`zenFailedNow` 组按上游并入（`zen_fanout_test.go` 依赖），`zenConfigData` 补 `FailoverCount`/`FailoverMinutes` 字段；但本地 `routeModel` 的 zen→cline 降级门 2026-10 前已删除，**不恢复**——新并入的 failover 状态机当前仅测试可达，`markZenFail` 的「cline 池为空直接忽略」分支本地有 `poolStatusSnapshot` 支撑
  - `admin_zen.go`/面板无上游新字段需要接线（`failoverActive` 未摘，无调用方）
  - README 冲突按「文档以本地中文为准」处理：删除自动合并混入的上游英文块（本地 2026-10 重构已删该节），上游新增 4 个行为点的中文摘录见下方「文档同步」
  - `TODO.md` modify/delete 取删除（本地不维护上游 TODO）
  - `egress_test.go` 取本地（新文件名 `zen-config.json`、无 `enabled` 字段——死代码清理后的本地形态，上游测试改回旧值是上游侧回退，不跟）
- 测试适配（上游测试自带缺陷，速查文档 §3 既知模式）：
  - `zen_fanout_test.go` 删 `Enabled`/`Proxies`/`Failover` 赋值（本地结构体无此字段）；补 5 处 `setZenModelForTest` 裸调的 `savedModels` 恢复——上游裸调不恢复模型表，全量跑时 `TestZen429StillRotatesToHealthyKey` 毒化后 `zen_prefix_test.go` 三个解析测试全挂（本次实测复现，二分定位到最小毒化集）
  - `TestPickZenKeyStillReturnsWhenAllCooled` 显式 `UNABLE_FORCE=1`：上游断言「整池冷却仍轮转」，本地 2026-10-25 定的语义是全冷却兜底由开关门控，开开关后语义一致
  - `zen_body_wire_test.go` 的 usage 帧测试改走本地 `replaySSE` 路径（上游 `handleStreamResponseUsage` 本地不存在）；`zenKeyUsage` 清理块改 `zenKeyTokens`（本地记账变量名）
- 文档同步：README 的 zen 行为段落以代码为准补记——`zenClientErrorStatus` 429 透传、20 分钟冷却封顶、fan-out 预算 4、压缩 buffer 80000 与存量迁移、base path 目录自愈（上游 README 的「capped at 24h」说法与其最终代码矛盾，**未采纳**，以 `zenRateLimitCooldown=20min` 为准）
- 验证：`go build ./...`、`go vet ./...` 全绿；`go test ./... -count=1` 27 包 ok，唯一失败为既知项（`TestChatStreamPassthrough` 既有 flaky；`TestInstallReplacesRestartsAndRecovers` Windows fork/exec 既有缺陷，单跑 PASS 且不 import 本次改动）；`-race` 当日因本机无 C 编译器跳过，**同日补装 winlibs gcc 后全仓 28 包 -race 全绿**（见下条补录）
- 生产：未同步，等用户「同步」指令（见 PROJECT.md 部署节）。

## 本地死代码清理

### 2026-10-27 — 删除"兼容旧配置文件"的一次性迁移与死字段

本节不属于任何上游同步（四条线本次均未拉新上游），记录本地清理，避免日后误判为回归。

- 判据（必须可复核）：迁移代码只在"新文件不存在"时触发，因此**部署目录里 16 个旧文件名全部不存在**即证明其永不再触发。实测 `D:/DockerApp/afree` 下 `.amd-config.json` / `.or-config.json` / `.other-config.json` / `.th-config.json` / `.trae-config.json` / `.cline-accounts.json` / `.proxy-pool.json` / `.zen-config.json` / `.proxy-health.json` / `.egress-bindings.json` / `.workbuddy-proxies.json` / `.session-secret` / `combos.json` / `.model-blacklist.json` / `.platform-blacklist.json` / `.cline-stream-required.json` 全部 `absent`，对应新文件（`amd-config.json`、`oprt-config.json`、`other-config.json`、`tkhb-config.json`、`trae-config.json`、`cline-config.json`、`proxy-config.json`、`afree-config.json`）全部 `EXISTS`。
- 删除：
  - `internal/app/data_migrate.go` 整文件（`migrateLegacyDataFile`）+ 5 处调用：`initAMD` / `initOpenRouter` / `initOther` / `initTokenHarbor` / `initTrae`。旧文件名 → 新文件名的首启复制已无对象。
  - `migrateGlobalConfigLocked`（`global_config.go`）+ 调用点 + 因此失去唯一使用者的 `strings` import。该函数只在 `afree-config.json` 不存在时执行，且运行在 `globalCfg = defaultGlobalConfig()` 之后，删后首启行为等价（默认配置直接返回）。
  - `migrateLegacyClinePool`（`pool.go`）+ 调用点。`cline-config.json` 已存在。
  - `migrateProxyConfigFileLocked`（`proxy_config.go`，149 行）+ 调用点；`proxy-config.json` 不存在时改为与其它错误分支一致：`f := defaultProxyConfigFile(); saveProxyConfigFileLocked(f); return f`。同步修正两处"见 migrateProxyConfigFileLocked"的注释。
  - `foldProxyAliases`（`proxy_pool_config.go`）：唯一调用者是被删的迁移函数；别名折叠能力本身随代理池拆分早已不再需要。
  - 因此失去唯一使用者的类型：`modelBlacklistFile`（`model_blacklist.go`）、`platformBanFile`（`platform_blacklist.go`）。落盘早已走 `globalConfigData.ModelBlacklist` / `.PlatformBlacklist`。
  - `zenConfigData.SessionRotateMinutes`：字段自 a99d94b 起"装载后零作用，新落盘恒写 0"，属**自证死字段**。Go 的 `encoding/json` 对结构体不存在的键直接忽略，因此旧的 `sessionRotateMinutes` 键不会导致读取报错——"保留字段以免旧配置报错"这一前提本身不成立。
  - `zenConfigData.ProxyAliases`：字段注释引用的清理方 `foldLegacyProxyAliases` 在仓库中根本不存在（`normalizeZenKeys` 也早已没有折叠逻辑），是纯残留。
  - 测试构造 `zenTestConfig`（`proxy_binding_test.go`）中对应的两个字段同步删除。
- 保留（经核实**不是**无用代码，勿在后续清理中误删）：
  - `zenConfigData.Key`（单 key 兼容字段）：生产 `zen-config.json` 里 `key` 非空、`keys` 15 条并存，`normalizeZenKeys`（`zen.go:602-625`）每次加载都用它并回写 `Key = Keys[0]`，面板 GET 也返回该字段——**活代码**。
  - `legacyZenBackup`（`admin_backup.go`）：服务于"导入 2026-10-26 之前导出的旧备份"，旧备份是用户浏览器里的存量文件，不会因部署目录干净而消失。
  - `/admin/api/zen/*` 路径别名（`admin.go:101`）：仍是可访问路由（旧书签/外部脚本），不是配置兼容。
  - 旧 Reason 文案自愈（`model_blacklist.go`）：当前数据已全是统一文案、逻辑空转，但导入旧备份后仍需修，成本 6 行。
  - `proxy.go` 响应里"原字段保留（向后兼容）"：客户端契约字段。
- 验证：`go build ./...`、`go vet ./...` 全绿；`go test ./internal/app/ -count=1` → ok；全量 `go test ./... -count=1` 26 包 ok，`TestChatStreamPassthrough`（既有 flaky，用户已定不修）与 `internal/workbuddy/updater` 的 `TestInstallReplacesRestartsAndRecovers` 失败——后者是测试固件在 Windows 上 `fork/exec wb2api.exe` 报 "This version of %1 is not compatible"，该包不 import 任何 `afree-proxy/internal/*`，与本次改动无因果关系。
- 镜像清理：`afree-proxy` 的 `prod-*` 按创建时间倒序保留最近 3 个（`prod-20261027-36/35/34`），删除其余 33 个；`prod-20261026-12` 因与 `local` 共用同一 image ID，仅 untag 不删层。非 `prod-*` 标签（`local`、`sync-test-20261027`、`refactor3`、`workbuddy-local`、`workbuddy-builder`）未动。
- 生产：镜像 `afree-proxy:prod-20261027-37`（`b48f1f3ab562`，37.4MB）重建 `afree` 容器（`--restart always`、`-p 3457:3457`、`-v D:/DockerApp/afree:/app/data`），env 全量哈希 `9eb6f4999012` 与旧容器逐字节一致（未丢凭据），`Up (healthy)`；`GET /`→302、`GET /v1/models`→200；容器内二进制 `部署默认值` 命中 1 处（Trae v1.0.10 内嵌脚本仍在）。旧容器先 `stop`+`rename afree-bak` 保留回滚窗口，验证通过后删除。

### 2026-10-27 — 清理旧备份导入兼容（`legacyZenBackup`）与 Reason 自愈

上一节把 `legacyZenBackup` 与「旧 Reason 文案自愈」列为保留项。经用户当日确认「导入 2026-10-26 之前导出的旧备份不再需要」，二者转为可删，本节记录删除（不改写上一节原文，保留决策轨迹）。

- 删除：
  - `legacyZenBackup` 类型（`admin_backup.go`）：含 6 个旧代理池字段（`proxies`/`proxyStrategy`/`proxySubURL`/`proxySubCron`/`zenUseProxies`/`workbuddyUseProxies`）与 4 个旧隔离字段（`proxyIsolation`/`backupProxyEnabled`/`proxyExclusions`/`proxyBindCaps`）。`configBackup.Zen` 改用 `*zenConfigData`，`zenBackupSnapshot()` 返回深拷贝。
    **导出的备份 JSON 完全不变**：这些旧字段在 `legacyZenBackup` 里都带 `omitempty`，新备份从不写它们，删掉只影响读取路径。
  - 导入侧旧代理池回落（`backup.Zen.Proxies` 等 → `ProxyPool` 段）。
  - 导入侧旧隔离/绑定回落（`backup.Zen.ProxyIsolation` 等 → `Proxy` 段），`binding := backup.Proxy` 直接取值。
  - `loadModelBlacklist` 里的旧 Reason 文案自愈（把「上游原话」改写为 `BanReasonAuto`）。判据：生产 `afree-config.json` 的 `modelBlacklist` 5 条 reason 已全是标准文案（4 手动 + 1 自动）、`detail` 全空 → 该循环恒不命中，且唯一用途是修旧备份导入后的数据。
- 保留（仍为活代码，勿误删）：
  - `zenConfigData.Key`（单 key 兼容）：生产 `key` 非空、`keys` 15 条，`normalizeZenKeys` 每次加载回写。
  - `/admin/api/zen/*` 路径别名：可访问路由。
  - `proxy.go` 的 `/v1/models` 字段归一化里的「原字段保留」：`normalizeModelEntryFields` 每次请求都执行，语义是**归一化后仍保留上游原生字段名**（如 zen 的 `context`/`output`），让老客户端继续可读，不是兼容旧配置。
- 验证：`go build ./...`、`go vet ./...` 全绿；`gofmt` 干净；`go test ./internal/app/ -count=1` → ok。
- 镜像清理：删除非 `prod-*` 标签 `local`、`sync-test-20261027`、`refactor3`、`workbuddy-local`、`workbuddy-builder`（927MB 构建镜像），各自独立 image ID 均实际删层。删除后 `afree-proxy` 剩 `prod-20261027-37/36/35`（随后部署 prod-38，`-35` 再删，见下条）。
- 文档重构：根 `AGENTS.md` 拆为两份——`AGENTS.md` 只留**跨项目通用规则**（git 纪律、验证要求、换行与工具路径、临时文件、删除判据、工具安装、上游台账格式、禁止死配置、技能同步），新增 `PROJECT.md` 承载**本项目专属事实**（这是什么、平台归属表、三条上游线与 `MERGE.md` 格式、构建与两个已知失败、本地定制清单、生产部署、本仓死配置判据）。`AGENTS.md` 顶部强指向 `PROJECT.md`。`.agents/skills/sync-upstream/SKILL.md` 第 7 条同步改写。
- 生产：镜像 `afree-proxy:prod-20261027-38`（`9c51a324ce9e`，37.4MB）重建 `afree` 容器，env 全量哈希 `9eb6f4999012` 与旧容器一致（未丢凭据），`Up (healthy)`；`GET /`→302、`GET /v1/models`→200；启动日志正常——`Loaded 19 active accounts`、`WorkBuddy subsystem ready: accounts=7`、`model blacklist: restored 5 entry(ies)`（删自愈后读取仍正常）、zen 目录同步 6 新增 / 4 剔除。旧容器 `afree-bak` 验证通过后删除。`prod-*` 保留最近 3 个（`38/37/36`），`-35` 已删。

### 2026-10-30 — 镜像 tag 口径统一（prod-* 退出本机）

本节不属于任何上游同步，记录本地镜像口径决策（用户 2026-10-30 指示），避免后来者被历史 `prod-*` 文案误导。

- **历史（本节以上各行原文不改）**：2026-10-27 之前本机镜像用过两套 tag——`prod-*`（部署本机测试容器时打）与 `test-*`（容器验证时打），两者其实都跑在同一台本机的 `afree` 容器上，命名分歧是历史沿革不是角色差异。真正语义上的「生产镜像」只有 GHCR 的 `ghcr.io/xd88dx/afree-proxy:latest`（CI 从 main 构建发布，iStore 自行 pull），从未在本机打 tag。
- **新口径（2026-10-30 用户定）**：本机构建一律打 `test-YYYYMMDD-HHMM`，不再打 `prod-*`。判据：本机容器跑的就是测试镜像（PROJECT.md §6「判断生产看流量」），`prod-` 前缀只属于 CI/GHCR 发布语境。
- **清理**：本机 `afree-proxy:*` 标签按创建时间倒序保留 7 个（用户定，从 3 提到 7；`PROJECT.md` §6 已同步改写），删除其余 15 个（`prod-20261028-2..20261030-3`、`prod-20261029-1..4`、`fix-20261029{,b}`），实际删层 42 步。保留：`test-20261030-1724`（当前 `afree` 容器在用）、`prod-20261030-8/7/6/5/4`、`test-20261030-4`——保留的旧 `prod-*` 标签仅是还没轮出窗口的历史镜像，不代表恢复 `prod-*` 命名。
- **文案更正范围**：`PROJECT.md` §6 保留数与口径；本节。`MERGE.md` 以上历史行按「台账只追加不删改」保留原文，其中「生产:镜像 prod-…重建容器」等表述按今天的口径理解即为「本机部署镜像 test-/prod-<tag>」。记忆侧 `afree-prod-deploy` 已同步改写。

### 2026-11-03 — afree 本地补丁：wb 池状态/实测台账进配置备份（三层口径）

本节不属于上游同步，记录 vendor 树（misakano 线）上的 afree 本地新增，供下次 sync_workbuddy 三方合并时核对保留清单。

- 背景：用户定「导出 = 容器全部数据（全新容器导入 → 一模一样）；导入时配置态与账号事实总是应用、本机统计受勾选控制；配置 hash 只盯配置态」。wb 的 state.json/usage.json 此前完全不在备份里。
- vendor 树新增（均为 afree 本地文件/函数，上游没有）：
  - `pool/accountfacts.go`：`StateSnapshotJSON()`（全量快照，与 state.json 落盘同形态）与 `ImportStateBackupJSON(raw, includeStats)`（账号事实总是应用、token 用量/成功失败计数/调度游标仅 includeStats 时应用、uid 不在池中丢弃）；`entry.applyAccountFactsLocked` 承载事实字段恢复与惰性清洗。
  - `usage/usage.go`：`Recorder.SnapshotRaw()` / `Recorder.ImportRaw(raw)`（实测台账备份往返）。
  - `pool/persist.go`：`applyAccountsLocked` 重构为「事实段（applyAccountFactsLocked）+ 统计段」两段恢复——本地/Redis 快照加载行为不变（纯重构，测试全过）。
- app 层接线：`workbuddyBackup` 增 `state`/`usage` 段；`importWorkbuddyBackup` 增 includeStats 参数；cline/zen 事实合并函数；五平台（trae/amd/or/th/other）JSON 级事实合并 `backup_state_facts.go`。
- 装配层无新配置键。不采纳项：无（纯新增）。
- 验证：`go build ./...`、`go vet ./...` 全绿；`go test ./... -count=1` 29 包全过；本机容器实机导出（version 3）含 workbuddy.state（15 账号）/usage（218 桶）/state 八域。

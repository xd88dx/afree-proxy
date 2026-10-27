# 上游同步台账

每次同步任务结束后更新本文件：记录基线、目标提交、吸收 / 丢弃的内容与验证结果。
WorkBuddy 一栏的基线同时登记在 `scripts/workbuddy-vendor-baseline.json`（由 `sync_workbuddy.py --seal` 写入）。

**平台归属判定（记错节等于台账失效）**：`foxy1402/cline-proxy` 一个上游同时承载 **Cline** 与 **OpenCode** 两条平台线，记录前必须按改动文件的实际归属分节，不能笼统记在 Cline 下：

| 归属 | 判据 |
|---|---|
| OpenCode | `internal/app/zen*.go`、`admin_zen.go`、面板 `oc*` 字段（OpenCode 区块）、`ZEN_*` 配置、`internal/kit/http.go`（`DecodeResponse` 只被 `zen.go` 调用） |
| Cline | Cline 账号池（`internal/app/proxy.go` 的池/轮换/冷却）、`/v1` 通用转发与方言转换 |
| TraeWork / WorkBuddy | 各自子系统（`internal/app/trae*`、`internal/workbuddy/**`） |

## Cline

本次未涉及上游同步（`571f9b9` 的改动全部属于 OpenCode 线，见文末 OpenCode 节）。

## TreaWork

上游:`autumnsentiment/Trae2api-cn`(Trae CN / Trae Solo CN 模型反代,Python,MIT)。**这条线不是 vendor/merge 关系**:上游是 Python 全栈,本地 `internal/trae/` + `internal/app/trae.go` 是 Go 重写,行为跟随上游协议;唯一逐行移植的产物是内嵌的本机授权助手 `web_login.py`(`internal/trae/helper_script.go`),只有它需要跟上游比对。2026-10-27 的 cline-proxy 合并未涉及本线。

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

- 生产：与 OpenCode 线同一镜像 `afree-proxy:prod-20261027-35`（一次构建覆盖两条线），部署验证见 OpenCode 节。

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



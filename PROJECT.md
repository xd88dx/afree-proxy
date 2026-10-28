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
| TraeWork       | `internal/trae/`、`internal/app/trae.go`                                                        | `trae*`、`TRAE_*`   |
| WorkBuddy      | `internal/workbuddy/**`（vendor 树）、`internal/app/workbuddy*.go`                              | `wb*`               |

`internal/app/admin_html.go` 是**所有平台共用**的单文件面板，按 h2 / tab 分段，`oc*` 属于 OpenCode 而不是 Cline。改一处必须确认它属于哪个平台段。

## 3. 上游同步：三条线，台账 `MERGE.md`

仓库根 `MERGE.md` 按 **Cline / TreaWork / WorkBuddy / OpenCode** 四条线记录同步历史，格式 `### YYYY-MM-DD — <基线短哈希> → <目标短哈希>`，写明上游仓库与合并方式、基线与目标、吸收项（带原因）、丢弃项（带原因）、本地定制保留清单、验证结果。历史只追加不删改；未涉及的线写“本次未涉及上游同步”。详细流程在 `.agents/skills/sync-upstream/`，按需加载。

三条线的同步方式不同：

- **Cline / OpenCode**：`foxy1402/cline-proxy`，`git merge --no-ff --no-commit <上游哈希>`。一个上游承载两条线，按改动文件的归属分节，判据 `git diff --name-only <旧HEAD> <merge提交> | grep -iE 'zen|opencode'` 与 `| grep -i cline`。
- **WorkBuddy**：`misakano7545/workbuddy2api-panel`，`python scripts/sync_workbuddy.py`（三方 vendor 合并，基线 `scripts/workbuddy-vendor-baseline.json`）。
- **TraeWork**：`autumnsentiment/Trae2api-cn`，**不 merge**——上游是 Python，本地是 Go 重写；唯一逐行移植的是内嵌 `web_login.py`，只做比对：`python scripts/check_trae_helper.py --upstream-dir <上游解包目录>`，基线 `scripts/trae-helper-baseline.json`。

## 4. 构建与验证

```bash
go build ./... && go vet ./...     # 必须全绿
go test ./... -count=1             # 除下述既有失败外应全绿
```

**已知失败，不要修**：

- `internal/workbuddy/server` 的 `TestChatStreamPassthrough`（全量测试时 `LastTokensPerSecond=nil`）。根因：`tokensPerSecond()`（`handler.go:2011`）在 `ttfb <= 0` 时返回 `ok=false`，`RecordTokenUsage`（`pool/state.go:395`）据此置 nil，而断言（`handler_test.go:462`）要求非 nil；httptest 即时响应下 `ttfb` 常被测成 0。属上游测试既有缺陷，改了会与后续同步持续冲突。
- `internal/workbuddy/updater` 的 `TestInstallReplacesRestartsAndRecovers`（Windows 上 `fork/exec wb2api.exe` 报 "This version of %1 is not compatible"，测试固件的假二进制无法 exec）。该包不 import 任何 `afree-proxy/internal/*`，与业务改动无因果关系。

换行：行尾由 `.gitattributes` + `.editorconfig` 强制统一为 LF（规则与判据见 `AGENTS.md` §3）。`gofmt` 是原生程序，不认 MSYS 路径——传 `$(cygpath -w <路径>)`，且**不要**把 `gofmt` 的 stderr 一起重定向进结果文件（会写坏源文件）。

## 5. 本地定制不可丢

本 fork 独有、上游没有，合并时优先保留：`PROXY_ISOLATION` / `proxyIsolationEnvLocked`、`backupProxyEnabled`、`proxyExclusions`、`proxyBindCaps`、`pinProxyPerKey`、`zenEgressSnapshot()`、`ModelRateFilter`、`enforceToolChoiceNone`（`proxy.go`，Cline 侧）、Workbuddy / Trae 分区与 `POOL_STRATEGY`。

已废弃、不要从上游恢复：`SessionRotateMinutes` 与后台 key rotator、`ZEN_KEYS`（Go 源码从不读取）、上游 README 的 compose 配置段。

上游功能但**必须由本仓 app 层装配**才生效（2026-10-28 起）：workbuddy 的网关思考档位策略（上游 `thinking` 配置节）。装配点三处，漏任一处都不会报错、只是功能静默失效——`internal/app/workbuddy.go`（启动）与 `internal/app/workbuddy_config.go`（热更新）里的 `up.SetEffortPolicy(effortPolicyOf(cfg))`、`livecfg.Snapshot` 的五个 `Thinking*` 镜像字段、面板四个控件（`thinking.enabled` / `default_effort` / `client_priority` / `override_scope`）。上游同类装配在 `cmd/server/main.go`，本仓没有那个入口。

`internal/config.example.json` 是上游仓库根 `config.example.json` 的副本（上游 `TestConfigExampleParses` 用相对路径读它，vendor 后正好指到这里）。同步脚本只搬 `internal/workbuddy/` 树，**不会**带这类根文件；上游更新样例后要手工 `cp`。**不要删**——删了 `go test ./internal/workbuddy/config/` 直接缺文件失败。

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

本机自建镜像 tag 只保留最近 3 个。

## 7. 死配置判据（本仓）

不要把本仓没有路由入口的上游端点暴露成面板开关。判据：`grep -rn '"/v1' internal/app/*.go | grep -v _test` —— 主 mux 只注册 `/v1/chat/completions`、`/v1/messages`、`/v1/responses`、`/v1/models`、`/v1/health`；workbuddy 的 `/v1/embeddings`、`/v1/search`、`/v1/fetch`、`/v1/images/*` 从未挂到主 mux（生产一律 404）。

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
4. 允许不译的只有：model id、key 名前缀、纯示例值（`0.5`、`22`、`4096`）、单位和纯技术标识。

排查遗漏：

```bash
# 面板里所有 tooltip 原文，逐个确认在 I18N_ZH 有同名键
grep -o 'title="[^"]\+"' internal/app/admin_html.go | sed 's/^title="//;s/"$//' | sort -u
# 面板里所有 placeholder 原文（示例值可跳过）
grep -o 'placeholder="[^"]\+"' internal/app/admin_html.go | sed 's/^placeholder="//;s/"$//' | sort -u
```

注意 `I18N_ZH` 是 JS 对象字面量：**同名键写两次不会报错，后者静默覆盖前者**，界面会显示成你以为错的那条。新增词条前后都 `grep -n "'<原文>'"` 确认只有一处。

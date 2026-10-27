# PROJECT.md — afree-proxy 项目专属规则

本文件只放**本项目的事实与流程**。跨项目通用的协作规则（git 纪律、验证要求、临时文件、删除判据、上游台账格式）在 `AGENTS.md`，两份都要读。

## 1. 这是什么

单一 Go 二进制网关（`module afree-proxy`），一个服务同时承载多个平台的反代与账号池：Cline、OpenCode（zen）、TraeWork、WorkBuddy，以及 AMDCloud / OpenRouter / TokenHarbor / Other 等 OpenAI 兼容上游。生产容器名 `afree`，端口 3457，数据卷 `D:/DockerApp/afree`。

## 2. 先判平台归属，再动手

同一个文件常承载多个平台的面板段与路由，改错平台是本仓最常见的事故来源。

| 平台线 | 代码位置 | 面板字段 / 环境变量 |
|---|---|---|
| Cline | `internal/cline/`；`internal/app/proxy.go` 的账号池、轮换、冷却与 `/v1` 通用转发 | — |
| OpenCode (zen) | `internal/app/zen*.go`、`internal/app/admin_zen.go`；`internal/kit/http.go` 的 `DecodeResponse` | `oc*`、`ZEN_*` |
| TraeWork | `internal/trae/`、`internal/app/trae.go` | `trae*`、`TRAE_*` |
| WorkBuddy | `internal/workbuddy/**`（vendor 树）、`internal/app/workbuddy*.go` | `wb*` |

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

换行：仓库是 CRLF。用 `gofmt` 前先 `tr -d '\r'`，格式化后 `sed 's/$/\r/'` 还原，否则整文件显示为全部改动。`gofmt` 是原生程序，不认 MSYS 路径——传 `$(cygpath -w <路径>)`，且**不要**把 `gofmt` 的 stderr 一起重定向进结果文件（会写坏源文件）。

## 5. 本地定制不可丢

本 fork 独有、上游没有，合并时优先保留：`PROXY_ISOLATION` / `proxyIsolationEnvLocked`、`backupProxyEnabled`、`proxyExclusions`、`proxyBindCaps`、`pinProxyPerKey`、`zenEgressSnapshot()`、`ModelRateFilter`、`enforceToolChoiceNone`（`proxy.go`，Cline 侧）、Workbuddy / Trae 分区与 `POOL_STRATEGY`。

已废弃、不要从上游恢复：`SessionRotateMinutes` 与后台 key rotator、`ZEN_KEYS`（Go 源码从不读取）、上游 README 的 compose 配置段。

## 6. 生产部署

生产由 **`D:\DockerApp\docker-compose.yml`**(compose project `tokenapp`,同一文件还管 `newapi`、`mihomo`)里的 `afree` 服务管理,容器名 `afree`,端口 3457,数据卷 `D:/DockerApp/afree`。判断依据:`docker inspect afree` 带 `com.docker.compose.project=tokenapp` 标签,`config_files` 指向该文件。

同步流程:

1. `docker build -t afree-proxy:prod-<日期>-<序号> .`
2. 把 `D:\DockerApp\docker-compose.yml` 中 `afree` 服务的 `image:` 改成新 tag。
3. `docker compose up -d afree`(按 config-hash 变化重建)。
4. 验证:`Up (healthy)`、`GET /`→302、`GET /v1/models`→200、启动日志出现账号加载与 `model blacklist: restored N entry(ies)`。

**不要用 `docker run` 重建**:那会丢掉 compose 标签与 project 归属,与真实管理方式脱节。该文件里凭据是明文,而终端输出会对凭据脱敏——不要拿脱敏后的占位符去改文件。

生产 `prod-*` 镜像只保留最近 3 个。

## 7. 死配置判据（本仓）

不要把本仓没有路由入口的上游端点暴露成面板开关。判据：`grep -rn '"/v1' internal/app/*.go | grep -v _test` —— 主 mux 只注册 `/v1/chat/completions`、`/v1/messages`、`/v1/responses`、`/v1/models`、`/v1/health`；workbuddy 的 `/v1/embeddings`、`/v1/search`、`/v1/fetch`、`/v1/images/*` 从未挂到主 mux（生产一律 404）。

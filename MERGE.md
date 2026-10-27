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

本次未涉及上游同步。

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
- 验证：`go build ./...` 通过；`go vet ./...` 通过；`go test ./... -count=1` 唯一失败 `TestChatStreamPassthrough` 为既有 flaky——HEAD 对照工作树同包同样偶发失败（断言要求 TTFB ≥1ms，`LastTokensPerSecond` 在机器负载下翻转成 nil），与本次合并无关。单独 `-run TestChatStreamPassthrough -count=1` 连跑 3 次全过，进一步确认是整包并发下的负载敏感断言。
- 生产：与 OpenCode 线同一镜像 `afree-proxy:prod-20261027-35`（一次构建覆盖两条线），部署验证见 OpenCode 节。

## OpenCode

### 2026-10-27 — a99d94b → 571f9b9

- 上游：foxy1402/cline-proxy（与 Cline 同一上游仓库），合并方式 `git merge --no-ff`
- 上次同步 merge：`ff56cef`（第二父 = 基线 `a99d94b`）
- 本次目标：`571f9b9` — zen: decode compressed upstream bodies, fix env-marker edge cases（区间内唯一上游提交）
- 平台归属：**本次改动全部落在 zen/OpenCode 侧，未触碰 Cline 侧任何文件**（`git diff --name-only f9a0e13 3d73dd1 | grep -i cline` 为空）
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


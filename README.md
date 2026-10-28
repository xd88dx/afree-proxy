# AFree Proxy

一个单二进制 Go 网关：把多个免费上游额度（Cline 账号、opencode zen 免费模型、WorkBuddy 账号、TraeWork 账号、OpenRouter / AMDCloud / TokenHarbor 免费 API、任意小上游的自定义子平台）聚合为一个干净的 **OpenAI 兼容 `/v1` 网关**，供 Cursor、ZCode、Cline、Claude Code 等 IDE 使用——带账号池轮转、身份↔出口代理绑定、中文管理面板。

基于 [YuJunZhiXue/Cline-proxy](https://github.com/YuJunZhiXue/Cline-proxy) 深度改写，面向个人公开部署。与 Cline、opencode、WorkBuddy、OpenRouter、AMD、TokenHarbor 等上游无隶属关系；请礼貌使用它们的免费额度。

## 架构总览

```
Cursor / ZCode / Cline / Claude Code ──►  afree-proxy  ──►  Cline 账号池（轮转）
   /v1/chat/completions                        │
   /v1/messages (Anthropic)                    ├─►  OpenCode zen 免费模型（多 key，zen: 前缀）
   /v1/responses (OpenAI Responses)            ├─►  WorkBuddy 账号（OAuth，wbcn:/wbgb: 前缀）
   /v1/models                                  ├─►  TraeWork 账号（trae: 前缀）
                                               ├─►  OpenRouter 免费模型（多 key，oprt: 前缀）
                                               ├─►  AMDCloud 免费 API（多 key，amd: 前缀）
                                               ├─►  TokenHarbor 免费模型（多 key，tkhb: 前缀）
                                               ├─►  Other 自定义子平台（自定前缀，多 key）
                                               └─►  出口 socks5/http 代理池（轮转或身份绑定）
```

## 核心特性

- **一个端点，全部上游。** 网关按请求的 `model` 前缀路由：`wbcn:`/`wbgb:` → WorkBuddy，`trae:` → TraeWork，`oprt:` → OpenRouter，`amd:` → AMDCloud，`tkhb:` → TokenHarbor，`zen:` → opencode zen，Other 子平台自定前缀 → 对应子平台，zen 付费模型明确 400，其余走 Cline 账号池。旧形态（裸 zen ID、`cn:`/`global:`、无前缀 cline ID）仍兼容解析。Combos 可自定义别名模型 ID。
- **三种客户端方言，一种上游语言。** `/v1/chat/completions` 近直通；`/v1/responses` 与 `/v1/messages`（Anthropic）双向翻译，含流式事件、工具调用与 usage。
- **代理隔离第一优先。** 每个账号/key 可绑定主/备出口代理，请求永远从绑定出口发出；两个出口都不可用时该身份整体跳过——**绝不回退其他出口或直连**。未绑定身份沿用面板的每平台走池开关。
- **凭据启用/禁用语义全平台一致。** 「启用」列 = 是否参与请求轮转。新增身份默认禁用（存量缺字段仍按启用，升级不停服）。**任何平台没有任何启用凭据（含反代开关全关）时整平台 503 拒路由**，与封禁平台同闸；模型目录只对可服务配置展示——无启用凭据时面板目录显示引导空态、`/v1/models` 摘除该平台模型。
- **函数调用真实可用。** 对真实上游全量实测：流式/非流式、并行工具调用、`/v1/responses` 与 Anthropic 流式，含上游工具参数怪癖修复。
- **中文管理面板** `/admin/`：仪表盘（内含实时统计分区）、Cline 账号池、TraeWork、WorkBuddy（内嵌原面板）、OpenCode、AMDCloud、OpenRouter、TokenHarbor、Other、代理池、网关设置（封禁平台/封禁模型）、自定义别名、请求日志。

## 平台速览

| 平台        | 前缀              | 凭据类型                                                                        | 模型目录                                                                        | 探测默认模型               |
| ----------- | ----------------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | -------------------------- |
| Cline       | `cline-free/`     | OAuth refreshToken / 静态 `sk_` key                                             | 官方免费源 5min 自动同步                                                        | 排序最小                   |
| TraeWork    | `trae:`           | OAuth（本机 8765 helper 网页授权）                                              | 上游目录 5min 同步（需可路由账号）                                              | `glm-5.2`                  |
| WorkBuddy   | `wbcn:` / `wbgb:` | OAuth（内嵌面板登录）                                                           | CN/Global 双域实时探测                                                          | 官方稳定模型优先           |
| OpenCode    | `zen:`            | zen key（本地铸造会话 ID）                                                      | 上游免费目录 5min 同步                                                          | `big-pickle` 优先          |
| OpenRouter  | `oprt:`           | `sk-or-v1-…` key                                                                | [free-models 页白名单](https://openrouter.ai/collections/free-models) 5min 同步 | `openrouter/free`          |
| AMDCloud    | `amd:`            | `rc-…` key（[Token Factory](https://developer.amd.com.cn/radeon/tokenfactory)） | 上游 `/v1/models` 5min 同步（需 Bearer key；仅 chat 模型）                      | `DeepSeek-V4-Flash`        |
| TokenHarbor | `tkhb:`           | `thk_live_…` key（[dashboard](https://tokenharbor.ai/dashboard/api-keys)）      | 上游 `/v1/models` 5min 同步（需 key；仅 `:free`）                               | `deepseek-v4.1-flash:free` |
| Other       | 自定 `acme:`      | 各上游 key                                                                      | `{BaseURL}/models` 拉取 + 手动添加 + 多选暴露                                   | 设置页配置                 |

各平台共用同一套池语义：轮转策略、429 按 `Retry-After` 冷却、Test 按钮发真实探测请求并在成功后立即解除冷却、主/备代理绑定、新增身份默认不参与路由。**key 池平台**（OpenRouter/AMDCloud/TokenHarbor/Other）401/403 冷却 1 小时；Cline 账号池 401（含刷新被 400/401/403 拒绝）直接标记账号过期（等刷新恢复），chat 直接 403 不改账号状态、原样返回。

## 快速开始（Docker）

```bash
docker run -d --name afree-proxy \
  -p 3457:3457 \
  -v cline-proxy-data:/app/data \
  -e PORT=3457 \
  -e API_KEY=change-me \
  -e ADMIN_PASSWORD=change-me-too \
  ghcr.io/xd88dx/afree-proxy:latest
```

打开 `http://127.0.0.1:3457/admin/`，用 `ADMIN_PASSWORD` 登录，添加账号或 key。镜像多架构（`linux/amd64` + `linux/arm64`，Dockerfile 用 `TARGETARCH` 做 Go 原生交叉编译，无需 QEMU）；发布规则（`.github/workflows/docker-publish.yml`）：push `main` → `:latest`，push `zen-harvest` 分支 → `:zencli`，tag `v0.9.0-harvest` → `:26.10.10`。

> 镜像名跟随 GitHub 仓库名；仓库名不同请替换。

所有状态都在 `/app/data` 卷里，备份 = 复制该卷。

## 关键环境变量

| 变量                                     | 默认          | 说明                                                                                                                                                                                                                                                                        |
| ---------------------------------------- | ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PORT`                                   | `3457`        | 监听端口（`-port` flag 优先）                                                                                                                                                                                                                                               |
| `DATA_DIR`                               | `/app/data`   | 容器内状态目录                                                                                                                                                                                                                                                              |
| `API_KEY` / `API_KEY_FILE`               | 空            | `/v1` 唯一有效 key（`Authorization: Bearer` 或 `x-api-key`）                                                                                                                                                                                                                |
| `ADMIN_PASSWORD` / `ADMIN_PASSWORD_FILE` | 空            | 管理面板密码；设置后所有 `/admin/*` 需登录                                                                                                                                                                                                                                  |
| `PROXY_ISOLATION`                        | 未设置        | 身份↔出口绑定：`true` 强制开启（面板只读）；`false` 默认改为关（面板可改）；未设置默认开启。只接受 true/false                                                                                                                                                               |
| `STRICT_MODEL_MATCH`                     | `true`        | 未知模型名 400 而非静默兜底                                                                                                                                                                                                                                                 |
| `ZEN_ORG_ID`                             | 空            | OAuth 账户组织 ID,写入 `x-opencode-org-id` 头;原始 API key 流量不需要(官方 CLI 也只对 OAuth 账户发该头)。面板可改,env 设置时以 env 为准                                                                                                                                     |
| `ZEN_PIN_KEY`                            | 空            | 排障:所有 zen 上游请求固定用第 n 个 key(1 起)而非轮转                                                                                                                                                                                                                       |
| `POOL_STRATEGY`                          | `round_robin` | 全部平台的凭据调度策略（Cline/zen/or/amd/th/other 共用）：`round_robin` / `fill` / `random`。WorkBuddy 用 vendor 自己的国内/国际分区策略；Trae 未设 `TRAE_STRATEGY` 时回落此值                                                                                              |
| `TRAE_STRATEGY`                          | 未设置        | Trae 独立凭据调度策略：`round_robin` / `fill` / `random` / `credit-priority`（额度多者优先）。优先于面板 Selection，未设置时回落 `POOL_STRATEGY`                                                                                                                            |
| `PUID` / `PGID`                          | `100` / `101` | 容器运行身份（entrypoint 自愈属主后降权；bind mount 时设为宿主机用户）                                                                                                                                                                                                      |
| `LOG_REQUESTS`                           | `true`        | 请求日志（仅元数据：IP/路径/模型/状态/耗时，绝无对话内容）                                                                                                                                                                                                                  |
| `MAX_BODY_MB`                            | `32`          | 请求体上限，超出 413                                                                                                                                                                                                                                                        |
| `STREAM_ATTEMPT`                         | `2`           | 流式中断缓冲重试总次数；每次重试会重耗上游免费额度                                                                                                                                                                                                                          |
| `REQUIRE_ADMIN_AUTH`                     | `true`        | 设 `false` 仅当反代已处理认证——同时豁免 fail-closed 启动检查里的 ADMIN_PASSWORD 要求                                                                                                                                                                                        |
| `CLIENT_IP_HEADER`                       | 空            | 反代环境下信任该头作为客户端 IP（如 `X-Real-IP`），用于登录限流；默认只用 RemoteAddr                                                                                                                                                                                        |
| `LOG_FILE_MAX_MB`                        | `10`          | `requests.jsonl` 与 `afree-proxy.log` 大小上限（MB），超限清空/轮转                                                                                                                                                                                                         |
| `STREAM_LOG`                             | `false`       | `true` 把 Anthropic 路径原始 SSE 落盘（**完整对话内容**，仅排障用）                                                                                                                                                                                                         |
| `APPLY_SYSTEM_PROMPT_OVERRIDE`           | `false`       | `true` 启用工作目录下 `override.md` 作为系统提示词替换（容器内即 `/app/override.md`；默认保留客户端自己的提示词）                                                                                                                                                           |
| `CLINE_FORCE_STREAM`                     | `false`       | `true` 时 Cline 上游一律走流式：客户端要非流式也由网关把 SSE 聚合回普通 JSON；默认按模型元数据/自学习逐模型判定                                                                                                                                                             |
| `CLINE_HEADER_TIMEOUT`                   | `60`          | Cline 上游「等响应头」看门狗秒数（0 = 关闭），超时换号重试；不影响头到达之后的流式 body                                                                                                                                                                                     |
| `UNABLE_FORCE`                           | `false`       | 全冷却兜底开关。`false`（默认）：平台凭据**全部冷却**时一律拒绝（冷却凭据绝不参与路由），拒绝消息统一为 `no <platform> ... routable` 模板；`true`：全冷却时继续按各平台兜底实现选用冷却中的凭据（保持请求流动，WorkBuddy 软冷却豁免/最早到期优先、zen live 优先等扩展保留） |
| `DISABLE_DIRECT`                         | `false`       | 直连禁用开关。`true`：出口为直连的凭据全部失效——绑定直连哨兵的凭据在隔离过滤阶段剔除；未绑定凭据在面板直连模式或代理池空兜底下同样失效，请求报 `no ... exit ... DISABLE_DIRECT`。`false`（默认）与既有行为相同                                                              |
| `ONLY_ROUTER`                            | `false`       | 仅会话路由开关。`true`：所有平台只保留 chat 请求及其必要支撑（token 续期、模型目录同步、倍率预热），WorkBuddy 签到/保活/余额巡检/补领清扫与 Trae 签到/额度刷新等维护任务一律不启动——面板上的自动签到等开关被本变量强制覆盖。`false`（默认）按各自配置照常运行               |

以上三个开关（`UNABLE_FORCE` / `DISABLE_DIRECT` / `ONLY_ROUTER`）解析同一套布尔值：`1` / `true` / `yes` / `y` / `on`（大小写不敏感）为真，其余为假；实时读取，改容器 env 重启即生效。

Fail-closed 启动：绑定非环回地址时，未设 `API_KEY` **或**未设 `ADMIN_PASSWORD`（且未设 `REQUIRE_ADMIN_AUTH=false` 豁免）都拒绝启动。

### WorkBuddy（vendored 上游）环境变量

`WB2A_*` 前缀的 40+ 个变量属于 vendored 的 WorkBuddy 上游子系统（`internal/workbuddy/config/config.go` 集中读取），覆盖监听/超时/指纹/思考档位/调度抖动/预算告警等，详见该文件头部注释。常用：`WB2A_LISTEN`（独立监听地址，通常不需要）、`WB2A_SOFT_RATE`（软冷却时长）、`WB2A_SCHEDULE_JITTER_MINUTES`（调度抖动）、`WB2A_ADMIN_ENABLED`（上游管理端点，缺省关）。其余 `TRAE_REMOTE_DEBUG`（Trae 事件帧转储）、`ZEN_PIN_KEY`（zen 排障钉定）见上表与对应章节；`INVOCATION_ID` / `NOTIFY_SOCKET` / `RC_SVCNAME` / `LAUNCH_JOBKEY_LABEL` 是 updater 的服务管理器探测（systemd/OpenRC/launchd），不是配置项。

### OpenCode zen 会话 ID

zen 会话 ID 在**本地铸造**——上游免费层格式门是无状态检查（`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`），网关为每个 key 首次使用时铸造兼容 ID 并持久化到数据卷（`zen-config.json` 的 `sessions` 段），无内嵌 CLI、无收割机。请求级 ID（`x-opencode-request`）与注入的 gate 工具参数 schema 同样对齐官方 CLI 形态。FreeTier 403 会在**本请求内**换新会话重试一次，仍 403 则该 key 冷却 15 分钟；**会话每次 API 调用新铸**（2026-10-27 对齐 CLI v2.1.4 行为——旧版粘性会话的复用形态会被上游影子封禁到 UTC 午夜），同一调用的重试沿用同一会话。旧的 `ZEN_SESSION_ROTATE_MINUTES` 轮换机制随之作废，env 不再被读取。

## 连接 IDE

**Cursor / ZCode / OpenAI 系** — `POST /v1/chat/completions`：

```
Base URL:  http://<host>:3457/v1
API Key:   <API_KEY>
Model:     cline-free/deepseek-v4.1-flash   （Cline 池）
           zen:big-pickle                    （OpenCode zen）
           trae:glm-5.2                      （TraeWork）
           wbcn:claude-sonnet-4.5            （WorkBuddy CN 域）
           oprt:openrouter/free              （OpenRouter）
           amd:DeepSeek-V4-Flash             （AMDCloud）
           tkhb:deepseek-v4.1-flash:free     （TokenHarbor）
           acme:zai/glm                      （Other 自定子平台）
```

**Claude Code / Cline** — Anthropic 方言 `POST /v1/messages`，同 Base URL 同 key。
**OpenAI Responses 方言** — `POST /v1/responses`（工具调用、流式事件、usage 全翻译）。

`GET /v1/models` 列出全部可服务模型（含 Combos）。免费目录自动同步（cline/zen/trae/or/amd/th 全部 5min，wb 实时探测）；线上列表是权威，内置种子只覆盖冷启动/离线启动，下架/新免费模型自动增删。

### Combos（别名模型）

面板创建自定义别名 ID（如 `my-flash` → `cline-free/deepseek-v4.1-flash`）。严格同平台指向；别名出现在 `/v1/models` 供 IDE 直接选择。

### Other（小上游子平台）

不值得独立页面的上游。Other 页可建任意多个子平台：名称、唯一模型前缀（不得与内置前缀冲突）、Base URL。有 key 后从 `{BaseURL}/models` 拉目录（也可手动添加，手动项跨刷新保留），多选下拉控制哪些模型对外暴露；未勾选的模型调用时明确 400。

## TraeWork 网页授权

添加账号走「网页授权」手风琴弹窗：网关生成本机授权助手（Python，监听 127.0.0.1:8765），浏览器打开授权页登录后凭据回传网关。助手脚本从网关下载，面板会比对 relay 源防止凭据写错实例；也支持手动粘贴凭据。授权只认本机 helper——上游实测拒绝网关直连回调。

## 数据目录（持久化结构）

`DATA_DIR`（容器内 `/app/data`）下每个平台一份配置文件，跨平台能力单独成文件：

| 文件                                                                                                   | 内容                                                                                                                    |
| ------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------- |
| `afree-config.json`                                                                                    | 全局：会话密钥、网关客户端 key、面板请求日志开关、账号选号策略、Combos、模型/平台封禁表、cline「必须流式」学习表        |
| `proxy-config.json`                                                                                    | 出口代理池 + 出口策略 + 订阅、各平台走池开关、代理隔离段（开关/排除表/绑定上限，含 WorkBuddy 账号绑定）、代理在线率统计 |
| `cline-config.json`                                                                                    | Cline 账号池（账号、游标、请求头）                                                                                      |
| `zen-config.json`                                                                                      | OpenCode zen：key 池与配置 + 冷却表（`cool` 段）+ 每 key 会话身份（`sessions` 段）+ 死模型别名 + 端点学习结果                                                             |
| `trae-config.json` / `amd-config.json` / `oprt-config.json` / `tkhb-config.json` / `other-config.json` | 各平台全量（账号/key 池、启用状态、冷却表、代理绑定、平台专属开关）                                                     |
| `workbuddy/config.json` + `workbuddy/auths/` + `workbuddy/request-logs/`                               | WorkBuddy 部署配置、授权凭据、请求日志（沿用 vendor 目录结构）                                                          |

凭据的代理绑定表（`keyBindings`）跟着凭据池留在各自的平台文件里，不并入 `proxy-config.json`：它的键是凭据明文，搬过去会让同一批明文在两处各存一份。走池开关是布尔值，不敏感，所以归代理域。

日志类保持独立：`afree-proxy.log`、`afree-proxy-stream.log`、`requests.jsonl`。旧的 `zen-stats.jsonl`（zen 学习统计）已无任何写入方，属历史遗留，可直接删除。

旧文件名的自动迁移已随 2026-10-27 死代码清理基本移除，唯一保留的是 zen 一条线：`zen-config.json` 不存在时，旧点文件（`.zen-config.json`、`.zen-sessions.json`、`.zen-model-aliases.json`、`.zen-endpoints.json`）会被读入合并一次（`zen_config_file.go` 的 `migrateZenConfigFileLocked`）。其余旧文件名（`.cline-accounts.json`、`.proxy-pool.json`、`.or-config.json`、`.egress-bindings.json` 等）不再有任何迁移逻辑，确认新版运行正常后可自行删除。

## 代理池与选号策略

两个互不相干的调度维度，容易混：

| 维度     | 决定什么                     | 策略入口                                                         | 存放                                        |
| -------- | ---------------------------- | ---------------------------------------------------------------- | ------------------------------------------- |
| 账号选号 | 这次请求用哪个账号 / key     | `POOL_STRATEGY`（默认 `round_robin`）；Trae 另有 `TRAE_STRATEGY` | `data/afree-config.json` 的 `strategy`      |
| 出口选路 | 这次请求从哪条线路 / IP 出去 | 面板「代理池」页的调度策略                                       | `data/proxy-config.json` 的 `proxyStrategy` |

代理池配置（代理列表、出口策略、订阅、各平台走池开关）存在 `data/proxy-config.json`，不寄居在任何平台配置里——代理池是全局能力（7 个平台共用），不属于 OpenCode zen。出口策略：`round_robin`（默认）/ `random` / `fill`。

WorkBuddy 的账号调度用 vendor 自己的国内/国际分区策略，与 `POOL_STRATEGY` 无关。

## 代理隔离（身份 ↔ 出口绑定）

不开隔离时，每次上游尝试都在整个代理池轮转：同一账号出口 IP 漂移、同一出口轮流出现多个账号——两者都是典型风控红旗。隔离模式（默认开启）改为固定绑定：

- 每个身份（Cline 账号 / zen key / WorkBuddy 账号 / TraeWork 账号 / 各 key 池 key）绑定一主一备代理。请求永远走主，主不可用走备，两者都冷却/被删时该身份整体跳过——**绝不回退其他出口或直连，隔离优先于可用性**。
- 未绑定身份沿用面板每平台的走池开关（缺省 = 走池），空绑定配置行为与旧版完全一致。
- 面板支持单行绑定、一键按行序分配（第 1 行绑直连哨兵、其余按代理池顺序依次绑主槽，多余行保持原绑定）、一键清空；绑定代理被删时行标 ⚠ 并保持跳过直到修复。备用代理开关关闭时备用槽整体退出路由（保留旧值，重开即恢复）。
- **代理在线率**：代理池页显示每个代理真实流量的传输层成功率（累计 + 最近 50 次滚动窗口），纯被动采样不产生探活流量、不影响路由。五档配色：≥80% 绿、60–80% 蓝、40–60% 粉、20–40% 黄、<20% 红，便于一眼挑出该换的节点。统计落盘在 `proxy-config.json` 的 health 段，编辑池清零。zen key 的出口自动钉定表同样并入该文件的 `egressBindings` 段（键为 sha256 哈希，不含明文 key）。

socks5/socks5h 全支持；直连哨兵（主 = direct）可显式声明直连出口。zen 免费额度按出口 IP 计（约 200 请求/5h），把 key 绑到不同出口才是真正解锁多 key 容量的方式。

**未绑定 zen key 的自动钉定（egress，上游 3758eb7）**：zen 免费层按 key 记录出口 IP，一个 key 出现多个 IP 整池会被判可疑。因此未显式绑定的 zen key 在「代理池非空」时自动分配一个稳定出口（一代理一 key，绑定幂等、重启不变，落 `.egress-bindings.json` 且不含明文 key）；代理比 key 少时多出的 key 被过滤并报容量错误（加代理，或在 zen 配置关闭 `pinProxyPerKey`），绝不直连或借用别人的出口。cline 账号不接此层——账号级绑定走面板的主/备绑定与自动分配。

## 封禁与黑名单

- **封禁平台**（网关设置页）：整平台退出会话路由，三端点 503、`/v1/models` 同步摘除；签到等维护任务不受影响。确认（蓝）+ 复位（黄）双按钮，chip 红色警示。
- **封禁模型**：手动封禁 + 自动封禁（上游按账号类型拒绝的模型在出口命中判据后自动入表），均从 `/v1/models` 移除。
- **无启用凭据闸**：与封禁同闸——任何平台没有任何可路由凭据（含反代开关全关）时请求 503 明确报错，宁拒不路由。

## Troubleshooting（实测经验）

| 症状                                                                 | 实际含义                                                        | 处理                                                    |
| -------------------------------------------------------------------- | --------------------------------------------------------------- | ------------------------------------------------------- |
| `bound proxies unavailable … the credential itself was not verified` | **出口**挂了，不是凭据坏。隔离语义下绑定身份被跳过，绝不改道    | 修/换绑定出口；**别删 key**；到提示的冷却时刻后重测     |
| 单次 Test 失败，几分钟后无任何改动又通过                             | 出口冷却只有 2 分钟，并发探测会把正常出口打成短暂冷却           | 单次失败不下结论，≥2 分钟后重测                         |
| `502` 内嵌上游 4xx 文本                                              | 客户端参数错；网关把所有上游错误映射为 502                      | 读内嵌的上游 message；注意客户端重试 502 会重耗免费额度 |
| `400 model … is not available on this gateway`（刚还好好的）         | 目录是活的：上游 feed 会修剪下架模型                            | 重读 `/v1/models`；脚本/CI 别写死模型 ID                |
| `amd model sync failed: catalog status 401`                          | 同步凭据失效（AMD key 会静默过期；同步需启用 key 的 Bearer 头） | 重测 AMD key                                            |
| TokenHarbor `403 … network address not accepted`                     | **出口 IP** 被封，不是 key                                      | 给该 key 换绑定出口                                     |
| zen 免费层 `403 FreeTierError` 突增                                  | 上游收紧了格式门（会话/请求 ID/gate 工具）                      | 看日志刷新频率趋势；网关会自动重铸重试                  |

## 开发

```bash
go build ./... && go vet ./...
./afree-proxy -host 127.0.0.1 -port 3457   # 或: go run .
docker compose up -d --build               # 源码构建（PROXY_PORT 换端口；需先在同目录建 .env 提供 API_KEY/ADMIN_PASSWORD，compose 以 ${VAR:?} 强制）
```

CI：push 到 `main` / `zen-harvest` 先跑 `go build` + `go vet` + `go test`，再 Buildx 发布多架构镜像到 GHCR——`main` → `:latest`，`zen-harvest` → `:zencli`，tag `v0.9.0-harvest` → `:26.10.10`；Dockerfile 以 `TARGETARCH` 做 Go 原生交叉编译，无 QEMU、无内嵌 CLI 阶段。

代码结构：

```
├── main.go                  入口、CLI flags、运行检测
├── internal/app/
│   ├── proxy.go             /v1 路由、chat handler、上游调用、聚合
│   ├── responses.go         /v1/responses 方言翻译
│   ├── cross_endpoint.go    /v1/responses 与 /v1/messages 的多平台归口 + 缓冲重试
│   ├── zen.go / zen_session.go / zen_endpoint.go   zen 上游、会话铸造、端点自学习
│   ├── trae.go              TraeWork：raw 协议、SSE 聚合、签到调度、OAuth helper
│   ├── amd.go / openrouter.go / tokenharbor.go / other.go   key 池平台四件套
│   ├── workbuddy.go/_config/_proxy/_dialects  WorkBuddy 子系统、出口绑定、方言
│   ├── platform_routability.go  无启用凭据整平台拒路由闸
│   ├── platform_blacklist.go / model_blacklist.go  封禁平台/封禁模型
│   ├── global_config.go / proxy_config.go / proxy_pool_config.go / zen_config_file.go   配置落盘（全局/代理域/zen；仅 zen 保留旧点文件读入合并）
│   ├── pool.go               Cline 账号池
│   ├── combos.go            别名模型
│   ├── admin*.go            管理面板：API、会话认证、UI、各平台页
│   └── config.go            env 配置（fail-closed 校验）
├── internal/trae/           TraeWork 协议栈（config/client/models/pool/checkin/helper）
├── internal/cline/          Cline 上游认证（WorkOS OAuth 刷新）
├── internal/workbuddy/      vendor 的 WorkBuddy2API 池/面板/调度器/上游
├── internal/amd|openrouter|tokenharbor|other/   各 key 池协议栈
├── internal/kit/            HTTP 客户端、随机 ID、zen 会话 ID 铸造、数据路径
├── .agents/skills/          仓库开发技能（新增平台配方、上游同步流程）
├── Dockerfile               多架构（amd64 原生 + arm64 交叉编译）
└── docker-compose.yml       源码构建，PROXY_PORT 参数化
```

## 致谢

基于 [YuJunZhiXue/Cline-proxy](https://github.com/YuJunZhiXue/Cline-proxy)。感谢 [LINUX DO](https://linux.do) 社区。

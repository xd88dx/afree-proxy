# AFree Proxy

一个单二进制 Go 网关：把多个免费上游额度（Cline 账号、opencode zen 免费模型、WorkBuddy 账号、TraeWork 账号、OpenRouter / AMDCloud / TokenHarbor 免费 API、任意小上游的自定义子平台）聚合为一个干净的 **OpenAI 兼容 `/v1` 网关**，供 Cursor、ZCode、Cline、Claude Code 等 IDE 使用——带账号池轮转、身份↔出口代理绑定、中文管理面板。

基于 [YuJunZhiXue/Cline-proxy](https://github.com/YuJunZhiXue/Cline-proxy) 深度改写，面向个人公开部署。与 Cline、opencode、WorkBuddy、OpenRouter、AMD、TokenHarbor 等上游无隶属关系；请礼貌使用它们的免费额度。

## 架构总览

```
Cursor / ZCode / Cline / Claude Code ──►  afree-proxy  ──►  Cline 账号池（轮转）
   /v1/chat/completions                        │
   /v1/messages (Anthropic)                    ├─►  OpenCode zen 免费模型（多 key，zen: 前缀）
   /v1/responses (OpenAI Responses)            ├─►  WorkBuddy 账号（OAuth，wbcn:/wbgb: 前缀）
   /v1/models                                  ├─►  TraeWork 账号（tacn:/tagb: 合并域前缀）
                                               ├─►  OpenRouter 免费模型（多 key，oprt: 前缀）
                                               ├─►  AMDCloud 免费 API（多 key，amd: 前缀）
                                               ├─►  TokenHarbor 免费模型（多 key，tkhb: 前缀）
                                               ├─►  Other 自定义子平台（自定前缀，多 key）
                                               └─►  出口 socks5/http 代理池（轮转或身份绑定）
```

## 核心特性

- **一个端点，全部上游。** 网关按请求的 `model` 前缀路由：`wbcn:`/`wbgb:` → WorkBuddy，`tacn:`/`tagb:` → TraeWork（合并域：tacn=CN Trae+SOLO、tagb=Intl，随 intl.enabled），`oprt:` → OpenRouter，`amd:` → AMDCloud，`tkhb:` → TokenHarbor，`zen:` → opencode zen，Other 子平台自定前缀 → 对应子平台，zen 付费模型明确 400，其余走 Cline 账号池。旧形态（裸 zen ID、`cn:`/`global:`、无前缀 cline ID）仍兼容解析。Combos 可自定义别名模型 ID。
- **三种客户端方言，一种上游语言。** `/v1/chat/completions` 近直通；`/v1/responses` 与 `/v1/messages`（Anthropic）双向翻译，含流式事件、工具调用与 usage。
- **代理隔离第一优先。** 每个账号/key 可绑定主/备出口代理，请求永远从绑定出口发出；两个出口都不可用时该身份整体跳过——**绝不回退其他出口或直连**。未绑定身份沿用面板的每平台走池开关。
- **凭据启用/禁用语义全平台一致。** 「启用」列 = 是否参与请求轮转。新增身份默认禁用（存量缺字段仍按启用，升级不停服）。**任何平台没有任何启用凭据（含反代开关全关）时整平台 503 拒路由**，与封禁平台同闸；模型目录只对可服务配置展示——无启用凭据时面板目录显示引导空态、`/v1/models` 摘除该平台模型。
- **函数调用真实可用。** 对真实上游全量实测：流式/非流式、并行工具调用、`/v1/responses` 与 Anthropic 流式，含上游工具参数怪癖修复。
- **中文管理面板** `/admin/`：仪表盘（内含实时统计分区）、Cline 账号池、TraeWork、WorkBuddy（内嵌原面板）、OpenCode、AMDCloud、OpenRouter、TokenHarbor、Other、代理池、网关设置（封禁平台/封禁模型）、自定义别名、请求日志。

## 平台速览

| 平台        | 前缀              | 凭据类型                                                                        | 模型目录                                                                        | 探测默认模型               |
| ----------- | ----------------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | -------------------------- |
| Cline       | `cline-free/`     | OAuth refreshToken / 静态 `sk_` key                                             | 官方免费源 5min 自动同步                                                        | 排序最小                   |
| TraeWork    | `tacn:` / `tagb:`（tagb 随 `intl.enabled`） | 便携快照（含设备密钥；本机 helper 网页授权 / `token----refreshToken` 粘贴 / 原版面板导入） | 引擎目录实时（按域门控：无账号域不透出）                                        | 无探测下拉（Test=额度连通性验证，沿用上游设计） |
| WorkBuddy   | `wbcn:` / `wbgb:` | OAuth（内嵌面板登录）                                                           | CN/Global 双域实时探测                                                          | 官方稳定模型优先           |
| OpenCode    | `zen:`            | zen key（本地铸造会话 ID）                                                      | 上游免费目录 5min 同步                                                          | `big-pickle` 优先          |
| OpenRouter  | `oprt:`           | `sk-or-v1-…` key                                                                | [free-models 页白名单](https://openrouter.ai/collections/free-models) 5min 同步 | `openrouter/free`          |
| AMDCloud    | `amd:`            | `rc-…` key（[Token Factory](https://developer.amd.com.cn/radeon/tokenfactory)） | 上游 `/v1/models` 5min 同步（需 Bearer key；仅 chat 模型）                      | `DeepSeek-V4-Flash`        |
| TokenHarbor | `tkhb:`           | `thk_live_…` key（[dashboard](https://tokenharbor.ai/dashboard/api-keys)）      | 上游 `/v1/models` 5min 同步（需 key；仅 `:free`）                               | `deepseek-v4.1-flash:free` |
| Other       | 自定 `acme:`      | 各上游 key                                                                      | `{BaseURL}/models` 拉取 + 手动添加 + 多选暴露                                   | 设置页配置                 |

各平台共用同一套池语义：轮转策略、429 按 `Retry-After` 冷却、Test 按钮发真实探测请求并在成功后立即解除冷却、主/备代理绑定、新增身份默认不参与路由。**key 池平台**（OpenRouter/AMDCloud/TokenHarbor/Other）401/403 冷却 1 小时；Cline 账号池 401（含刷新被 400/401/403 拒绝）直接标记账号过期（等刷新恢复），chat 直接 403 不改账号状态、原样返回。

**上游 429 原样透传**（2026-11-09 全平台收口）：上游限流时网关回 `429`（不再包装成 502），客户端可据此按 `Retry-After` 退避重试——包装成 502 会让 IDE 判定网关故障并标记该渠道不可用。上下文溢出仍统一改写为 `400 context_length_exceeded`，其余上游错误保持 502。

**批量测试只测网关对外提供的服务**（2026-11-02）：各平台页「批量测试」与代理池页「全量测试」逐凭据判可路由性——反代（路由）开关关闭的凭据请求到不了，跳过不测（Trae/WorkBuddy 双开关平台只看反代开关，账号总闸不参与判定）；网关设置里封禁的平台整平台跳过。平台内一个可路由凭据都不剩时该平台自然跳过，完成提示列出跳过项。

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

所有状态都在 `/app/data` 卷里，备份 = 复制该卷。面板「网关设置 → 配置导入导出」是**全量备份**（2026-11-03 三层口径：导出 = 容器全部数据，目标「全新容器导入 → 一模一样」）：配置态 + 账号事实 + 本机统计都在内，含 WorkBuddy 的池状态（余额/冷却/限额/实测成本）与实测台账。导入分层：配置态与账号事实**总是导入**；本机统计受「导入统计数据」勾选控制（默认勾选=随备份还原，取消=保留本机现值）。`config-manifest.json` 是程序自维护的配置指纹清单，供外部监测配置变更（盯它一个文件即可）。

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
| `POOL_STRATEGY`                          | `round_robin` | 全部平台的凭据调度策略（Cline/zen/or/amd/th/other 共用）：`round_robin` / `fill` / `random`。**设置即以 env 为准**——网关设置页「账号轮转策略」下拉（位于「封禁平台」上方）随之置只读并给出提示；未设置时用面板保存值（落 `afree-config.json`）。WorkBuddy 用 vendor 自己的国内/国际分区策略；TraeWork（2026-11-04 换线）用 vendored trae2api 引擎的权重调度（空闲/额度/余额感知，`data/trae2api/config.json` 的 `pool.*`），不走本变量                                                                                              |
| `PUID` / `PGID`                          | `100` / `101` | 容器运行身份（entrypoint 自愈属主后降权；bind mount 时设为宿主机用户）                                                                                                                                                                                                      |
| `LOG_REQUESTS`                           | `true`        | 请求日志（仅元数据：IP/路径/模型/状态/耗时，绝无对话内容）                                                                                                                                                                                                                  |
| `MAX_BODY_MB`                            | `32`          | 请求体上限，超出 413                                                                                                                                                                                                                                                        |
| `STREAM_ATTEMPT`                         | `2`           | 流式中断缓冲重试总次数；每次重试会重耗上游免费额度                                                                                                                                                                                                                          |
| `REQUIRE_ADMIN_AUTH`                     | `true`        | 设 `false` 仅当反代已处理认证——同时豁免 fail-closed 启动检查里的 ADMIN_PASSWORD 要求                                                                                                                                                                                        |
| `CLIENT_IP_HEADER`                       | 空            | 反代环境下信任该头作为客户端 IP（如 `X-Real-IP`），用于登录限流；默认只用 RemoteAddr                                                                                                                                                                                        |
| `LOG_FILE_MAX_MB`                        | `10`          | `requests.jsonl` 与 `afree-proxy.log` 大小上限（MB），超限清空/轮转                                                                                                                                                                                                         |
| `STREAM_LOG`                             | `false`       | `true` 把 Anthropic 路径原始 SSE 落盘（**完整对话内容**，仅排障用）                                                                                                                                                                                                         |
| `APPLY_SYSTEM_PROMPT_OVERRIDE`           | `false`       | `true` 启用工作目录下 `override.md` 作为系统提示词替换（容器内即 `/app/override.md`；默认保留客户端自己的提示词）                                                                                                                                                           |
| `FORCE_STREAM`                           | `false`       | `true` 时 Cline 上游一律走流式：客户端要非流式也由网关把 SSE 聚合回普通 JSON；默认按模型元数据/自学习逐模型判定。其余平台恒为上游流式（客户端要非流式时由网关聚合），不受此开关影响。（`CLINE_FORCE_STREAM` 已删除，统一到本开关） |
| `CLINE_HEADER_TIMEOUT`                   | `60`          | Cline 上游「等响应头」看门狗秒数（0 = 关闭），超时换号重试；不影响头到达之后的流式 body                                                                                                                                                                                     |
| `UNABLE_FORCE`                           | `false`       | 全冷却兜底开关。`false`（默认）：平台凭据**全部冷却**时一律拒绝（冷却凭据绝不参与路由），拒绝消息统一为 `no <platform> ... routable` 模板；`true`：全冷却时继续按各平台兜底实现选用冷却中的凭据（保持请求流动，WorkBuddy 软冷却豁免/最早到期优先、zen live 优先等扩展保留） |
| `DISABLE_DIRECT`                         | `false`       | 直连禁用开关。`true`：出口为直连的凭据全部失效——绑定直连哨兵的凭据在隔离过滤阶段剔除；未绑定凭据在面板直连模式或代理池空兜底下同样失效，请求报 `no ... exit ... DISABLE_DIRECT`。`false`（默认）与既有行为相同                                                              |
| `ONLY_ROUTER`                            | `false`       | 仅会话路由开关。`true`：所有平台只保留 chat 请求及其必要支撑（token 续期、模型目录同步、倍率预热），WorkBuddy 签到/保活/余额巡检/补领清扫与 Trae 签到/额度刷新等维护任务一律不启动——面板上的自动签到等开关被本变量强制覆盖，且面板顶部显示常驻提示条。`false`（默认）按各自配置照常运行               |

以上三个开关（`UNABLE_FORCE` / `DISABLE_DIRECT` / `ONLY_ROUTER`）解析同一套布尔值：`1` / `true` / `yes` / `y` / `on`（大小写不敏感）为真，其余为假；实时读取，改容器 env 重启即生效。

Fail-closed 启动：绑定非环回地址时，未设 `API_KEY` **或**未设 `ADMIN_PASSWORD`（且未设 `REQUIRE_ADMIN_AUTH=false` 豁免）都拒绝启动。

### WorkBuddy（vendored 上游）环境变量

`WB2A_*` 前缀的 40+ 个变量属于 vendored 的 WorkBuddy 上游子系统（`internal/workbuddy/config/config.go` 集中读取），覆盖监听/超时/指纹/思考档位/调度抖动/预算告警等，详见该文件头部注释。常用：`WB2A_LISTEN`（独立监听地址，通常不需要）、`WB2A_SOFT_RATE`（软冷却时长）、`WB2A_SCHEDULE_JITTER_MINUTES`（调度抖动）、`WB2A_ADMIN_ENABLED`（上游管理端点，缺省关）。其余 `ZEN_PIN_KEY`（zen 排障钉定）见上表与对应章节（旧引擎的 `TRAE_REMOTE_DEBUG` 随 2026-11-04 换线退役）；`INVOCATION_ID` / `NOTIFY_SOCKET` / `RC_SVCNAME` / `LAUNCH_JOBKEY_LABEL` 是 updater 的服务管理器探测（systemd/OpenRC/launchd），不是配置项。

### OpenCode zen 会话 ID

zen 会话 ID 在**本地铸造**——上游免费层格式门是无状态检查（`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`），网关为每个 key 首次使用时铸造兼容 ID 并持久化到数据卷（`zen-config.json` 的 `sessions` 段），无内嵌 CLI、无收割机。请求级 ID（`x-opencode-request`）与注入的 gate 工具参数 schema 同样对齐官方 CLI 形态。FreeTier 403 会在**本请求内**换新会话重试一次，仍 403 则该 key 冷却 15 分钟；**会话每次 API 调用新铸**（2026-10-27 对齐 CLI v2.1.4 行为——旧版粘性会话的复用形态会被上游影子封禁到 UTC 午夜），同一调用的重试沿用同一会话。旧的 `ZEN_SESSION_ROTATE_MINUTES` 轮换机制随之作废，env 不再被读取。

### OpenCode zen 上游基地址与 429 行为

- **基地址**（2026-10-30 抓包更正）：上游基地址是 `https://opencode.ai/inference/openai/v1`。它不再靠人工观测写死：每次目录同步都读 `models.opencode.ai` 里 opencode provider 的 `api` 字段（官方 CLI 读的同一份数据）自动校正配置——只改「仍是项目默认值」的配置，面板手填过的一律不动；目录返回非 `https://opencode.ai/inference/…` 的值一律拒绝（防止被篡改的目录把带着 Authorization 的请求引去别处）。加载时恰等于两个历史默认值（`/zen/v1`、`/inference/v1`）的存量配置自动迁移并记日志 `zen baseURL migrated: …`。
- **429 透传**：上游 429 原样透传给客户端（`zenClientErrorStatus`），不包装成 502——502 会让 IDE 判定网关坏了，而 429 客户端会按 Retry-After 退避。聚合/解析类故障仍回 502。
- **冷却封顶**：429 冷却 key 的时长封顶 20 分钟。上游 `Retry-After` 是「距下一个 00:00 UTC」的倒计时而非真实恢复时间（实测 RA=20h46m 而 18 分钟后同 key 已恢复 200），按全值冷藏会让多 key 池白停一整天。短 Retry-After 原样生效，缺头回落 1 分钟。
- **fan-out 预算**：一次客户端请求最多冷却 `1 + max429KeySwitches(4)` 个 key（与 retries 取小）。换 key 不可逆而等待可逆，两者不共用计数——超预算即停止并记 `key-switch budget exhausted`，不让一个坏请求烧穿整池。
- **全冷却兜底**：整池冷却时是否继续轮转试探由 `UNABLE_FORCE` 开关门控（默认拒绝；`true` 保持请求流动，见上表）。
- **上下文压缩阈值**：压缩在 `context − max(output, buffer)` 处触发，`buffer` 缺省 80000（面板可调）。旧缺省 20000 对「客户端自身把输入封顶在 128k」的场景永不触发（200k 窗口/32k 输出模型阈值 168k），满载请求原样送上游撞 429；恰等于旧值 20000 的存量配置自动迁移，手调过的不动。

## 连接 IDE

**Cursor / ZCode / OpenAI 系** — `POST /v1/chat/completions`：

```
Base URL:  http://<host>:3457/v1
API Key:   <API_KEY>
Model:     cline-free/deepseek-v4.1-flash   （Cline 池）
           zen:big-pickle                    （OpenCode zen）
           tacn:glm-5.2                      （TraeWork）
           wbcn:claude-sonnet-4.5            （WorkBuddy CN 域）
           oprt:openrouter/free              （OpenRouter）
           amd:DeepSeek-V4-Flash             （AMDCloud）
           tkhb:deepseek-v4.1-flash:free     （TokenHarbor）
           acme:zai/glm                      （Other 自定子平台）
```

**Claude Code / Cline** — Anthropic 方言 `POST /v1/messages`，同 Base URL 同 key。
**OpenAI Responses 方言** — `POST /v1/responses`（工具调用、流式事件、usage 全翻译）。

`GET /v1/models` 列出全部可服务模型（含 Combos）。对外目录统一剥离 `owned_by`/`source` 字段（2026-11-08），下游（hermes）勿再依赖该字段分桶。免费目录自动同步（cline/zen/or/amd/th 全部 5min，trae 由 vendored 引擎目录实时承载、trae 面板可手动刷新，wb 实时探测；同步走凭据绑定的出口，无可用出口时跳过本轮绝不直连）；线上列表是权威，内置种子只覆盖冷启动/离线启动，下架/新免费模型自动增删；上游返回模型级永久错误时自动穿透拉黑摘除（cline 404/计费、OR 404/403-agentic/400-endpoint、zen RegionError——2026-11-04 全平台审计补齐 OR/zen 接线）。**对外目录 = 各平台管理页可见的模型**（2026-11-03）：WorkBuddy 的出图模型在 app 层按**费率同源筛选**进出目录（与面板前端 `wbRate`/`wbModelMatchesFilter` 同构：promo 生效取 `promo_credits`，否则取 `credits`；缺失/不可解析一律不透出——缺失 ≠ 免费；≤ 对应域 `pool.model_rate_filter_cn`（过滤 `wbcn:`）/ `pool.model_rate_filter_global`（过滤 `wbgb:`）阈值才放行），目录与请求闸同源。当前出图模型上游目录不给 credits 牌价，全部挡住；将来上游给某个出图模型发了牌价且 ≤ 阈值时会自动进目录，无需改代码。注意 vendor 的 `/v1/images` 网关层未注册（生产实测 404）——目录放行只解决「可见」，专用出图调用入口仍不存在。

### Combos（别名模型）

面板创建自定义别名 ID（如 `my-flash` → `cline-free/deepseek-v4.1-flash`）。严格同平台指向；别名出现在 `/v1/models` 供 IDE 直接选择。

### Other（小上游子平台）

不值得独立页面的上游。Other 页可建任意多个子平台：名称、唯一模型前缀（不得与内置前缀冲突）、Base URL。有 key 后从 `{BaseURL}/models` 拉目录（也可手动添加，手动项跨刷新保留），多选下拉控制哪些模型对外暴露；未勾选的模型调用时明确 400。

## TraeWork 加号（2026-11-04 换线）

三通道：①「网页授权」手风琴弹窗——网关生成本机授权助手（Python，监听 127.0.0.1:8765），浏览器登录授权页后凭据回传网关，入库前**现场铸造 EC P-256 设备密钥**补齐快照（引擎续期协议要求设备私钥）；②手动粘贴 `token----refreshToken` 行（两者都必须，仅 token 无法通过导入校验）；③右上角「原版入口」（`/admin/trae2api/`，vendored trae2api 控制台）批量导入便携快照 JSON/ZIP。授权只认本机 helper；凭据写入端点 `/api/web-auth` 仅接受回环与内网来源（RFC1918/ULA），公网一律 403。新账号默认不参与路由（反代开关需手动打开）。

## 数据目录（持久化结构）

`DATA_DIR`（容器内 `/app/data`）下的持久化按**配置态 / 状态态**二分（2026-11-01 命名铁律：`*-config.json` = 配置，`*-state.json` = 状态）：

| 配置态（`*-config.json`，仅面板操作/凭据轮转才变）                                                       | 内容                                                                                                                    |
| -------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `afree-config.json`                                                                                      | 全局：会话密钥、网关客户端 key、面板请求日志开关、账号选号策略、Combos、模型/平台封禁表、cline「必须流式」学习表        |
| `proxy-config.json`                                                                                      | 出口代理池 + 出口策略 + 订阅、各平台走池开关、代理隔离段（开关/排除表/绑定上限，含 WorkBuddy 账号绑定）、egress 钉定表  |
| `cline-config.json`                                                                                      | Cline 账号池（账号凭据、请求头）                                                                                        |
| `zen-config.json`                                                                                        | OpenCode zen：key 池与配置 + 每 key 会话身份（`sessions` 段）+ 死模型别名 + 端点学习结果                                |
| `trae2api/config.json` + `trae2api/accounts/` / `amd-config.json` / `oprt-config.json` / `tkhb-config.json` / `other-config.json`   | TraeWork 引擎配置与便携快照（2026-11-05 迁入 vendored 子系统目录）、其余各平台账号/key 池、启用状态、代理绑定、平台专属开关 |
| `workbuddy/config.json` + `workbuddy/auths/`                                                             | WorkBuddy 部署配置、授权凭据（沿用 vendor 目录结构）                                                                    |

| 状态态（`*-state.json`，随请求/后台任务高频变化——外部监测配置变更时忽略这批文件）                        | 内容                                                                                                                    |
| -------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `cline-state.json`                                                                                       | Cline 每账号使用痕迹（状态/最近使用/调用与 token 计数/冷却）+ 轮转游标（2s 合批）                                       |
| `zen-state.json`                                                                                         | zen key 冷却表 + 每 key token 记账                                                                                      |
| `trae2api/state.json` / `trae2api/metrics.json`                                                          | TraeWork 引擎运行态：账号冷却/熔断/禁用+手动冷却、签到与额度（state）与指标（metrics）                                  |
| `amd-state.json` / `oprt-state.json` / `tkhb-state.json` / `other-state.json`                            | 各 key 池的 key 级 token 记账 + 冷却表（other 按子平台分键）                                                            |
| `proxy-state.json`                                                                                       | 代理出口在线率统计（60s 合批，编辑池清零）                                                                              |
| `workbuddy/state.json`、`workbuddy/usage.json`、`workbuddy/request-logs/`                                | WorkBuddy 运行态/用量分桶/请求日志（vendor 目录结构）                                                                   |

**`config-manifest.json`（配置指纹清单）**：程序自动维护的全部配置态文件 sha256 清单（glob 收集 `*-config.json` + wb 配置/凭据，新平台自动纳入；entries 无变化时连 `updatedAt` 都不更新）。外部监测配置变更只需盯这一个文件——它变了才有配置真的变了。

设计要点：token 记账/冷却/状态等使用痕迹**不进** `*-config.json`——配置文件保持"仅面板操作才变"，外部按 manifest 监测配置零噪声。凭据的代理绑定表（`keyBindings`）跟着凭据池留在各自的平台文件里（键是凭据明文，不重复存放）；走池开关是布尔值，归代理域。冷却是 2026-10-28 起的持久化决策：重启后冷却保留（落 `*-state.json`），避免刚被限流的凭据立即回池。

日志类保持独立：`afree-proxy.log`、`afree-proxy-stream.log`、`requests.jsonl`。

历史迁移（旧点文件合并、痕迹键拆出、`*-state.json` 统一命名）已全部完成并**移除迁移代码**（2026-11-01 死代码清理）：跨版本升级不再自动搬数据——升级前先在旧版「网关设置」导出全量备份，新版导入还原（默认勾选「导入统计数据」；旧版备份的统计段在新语义下同样按层分流）。

## 代理池与选号策略

两个互不相干的调度维度，容易混：

| 维度     | 决定什么                     | 策略入口                                                         | 存放                                        |
| -------- | ---------------------------- | ---------------------------------------------------------------- | ------------------------------------------- |
| 账号选号 | 这次请求用哪个账号 / key     | 面板「账号轮转策略」下拉，或 `POOL_STRATEGY`（env 优先，默认 `round_robin`）；TraeWork 用引擎权重调度 | `data/afree-config.json` 的 `strategy`      |
| 出口选路 | 这次请求从哪条线路 / IP 出去 | 面板「代理池」页的调度策略                                       | `data/proxy-config.json` 的 `proxyStrategy` |

代理池配置（代理列表、出口策略、订阅、各平台走池开关）存在 `data/proxy-config.json`，不寄居在任何平台配置里——代理池是全局能力（8 个平台共用：Cline / TraeWork / OpenCode / WorkBuddy / OpenRouter / AMDCloud / TokenHarbor / Other），不属于 OpenCode zen。出口策略：`round_robin`（默认）/ `random` / `fill`。

WorkBuddy 的账号调度用 vendor 自己的国内/国际分区策略，与 `POOL_STRATEGY` 无关。

## 代理隔离（身份 ↔ 出口绑定）

不开隔离时，每次上游尝试都在整个代理池轮转：同一账号出口 IP 漂移、同一出口轮流出现多个账号——两者都是典型风控红旗。隔离模式（默认开启）改为固定绑定：

- 每个身份（Cline 账号 / zen key / WorkBuddy 账号 / TraeWork 账号 / 各 key 池 key）绑定一主一备代理。请求永远走主，主不可用走备，两者都冷却/被删时该身份整体跳过——**绝不回退其他出口或直连，隔离优先于可用性**。
- 未绑定身份沿用面板每平台的走池开关（缺省 = 走池），空绑定配置行为与旧版完全一致。
- 面板支持单行绑定、一键按行序分配（第 1 行绑直连哨兵、其余按代理池顺序依次绑主槽，多余行保持原绑定）、一键清空；绑定代理被删时行标 ⚠ 并保持跳过直到修复。备用代理开关关闭时备用槽整体退出路由（保留旧值，重开即恢复）。
- **代理在线率**：代理池页显示每个代理的传输层成功率（累计 + 最近 50 次滚动窗口）与平均握手延迟。两个数据源：真实流量的**被动采样**（不产生探活流量）+ 健康表**手动延迟测试**（「代理测试」按钮：每出口对可配置的测速目标发最多 3 次 HTTPS 探测，默认 `www.gstatic.com/generate_204`，与真实请求同一拨号栈；单行「只测这一个出口」同理；样本计入在线率与握手统计，显示层不影响路由与冷却）。五档配色：≥80% 绿、60–80% 蓝、40–60% 粉、20–40% 黄、<20% 红，便于一眼挑出该换的节点。统计落盘在 `proxy-state.json`（60s 合批），编辑池清零。zen key 的出口自动钉定表在 `proxy-config.json` 的 `egressBindings` 段（键为 sha256 哈希，不含明文 key）。
- **出口手动冷却 / 解禁**（2026-11-02）：健康表行内「冷却」按钮把该出口摘出轮转——**手动冷却无到期时间**，只有点「解禁」才恢复（状态列显示「手动」而非编造的恢复时刻）；「解禁」一键清除该出口的全局 + 平台级冷却与失败计数，用于单次网络抖动误冷却唯一可用出口的场景。自动冷却口径不变：建连失败走全局 2 分钟（2-strike，单次失败不冷却），隧道后流错误走平台级 10 分钟。**手动摘除对所有路径生效**（2026-11-09 修复）：未绑定身份轮转、zen 的 egress 钉定出口与各平台目录同步一并跳过手动冷却出口；池内出口全部被手动冷却时按「无可用出口」拒绝该轮请求（fail-closed，绝不静默改用被摘除的出口、也不静默转直连）。限时冷却遇全池不可用仍保留历史 fail-open 轮转位。

socks5/socks5h 全支持；直连哨兵（主 = direct）可显式声明直连出口。zen 免费额度按出口 IP 计（约 200 请求/5h），把 key 绑到不同出口才是真正解锁多 key 容量的方式。

**未绑定 zen key 的自动钉定（egress，上游 3758eb7）**：zen 免费层按 key 记录出口 IP，一个 key 出现多个 IP 整池会被判可疑。因此未显式绑定的 zen key 在「代理池非空」时自动分配一个稳定出口（一代理一 key，绑定幂等、重启不变，落 `.egress-bindings.json` 且不含明文 key）；代理比 key 少时多出的 key 被过滤并报容量错误（加代理，或在 zen 配置关闭 `pinProxyPerKey`），绝不直连或借用别人的出口。**钉定出口同样参与冷却判定**（2026-11-09 修复）：被自动或手动冷却的钉定出口不再承载该 key，该 key 本轮跳过（而不是继续用坏出口）。cline 账号不接此层——账号级绑定走面板的主/备绑定与自动分配。

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
│   ├── trae.go              TraeWork：vendored trae2api 桥接（引擎装配/出口接管/双开关/面板 API）
│   ├── amd.go / openrouter.go / tokenharbor.go / other.go   key 池平台四件套
│   ├── workbuddy.go/_config/_proxy/_dialects  WorkBuddy 子系统、出口绑定、方言
│   ├── platform_routability.go  无启用凭据整平台拒路由闸
│   ├── platform_blacklist.go / model_blacklist.go  封禁平台/封禁模型
│   ├── global_config.go / proxy_config.go / proxy_pool_config.go / zen_config_file.go   配置落盘（全局/代理域/zen；仅 zen 保留旧点文件读入合并）
│   ├── pool.go               Cline 账号池
│   ├── combos.go            别名模型
│   ├── admin*.go            管理面板：API、会话认证、UI、各平台页
│   └── config.go            env 配置（fail-closed 校验）
├── internal/trae2api/       vendored trae2api 引擎（XCool-603/trae2api，12 子包；
│                            scripts/vendor_trae2api.py 生成，勿手改——会被 re-vendor 冲掉）
├── internal/app/trae2api_helper*.go  Trae helper 入库/脚本模板（afree 侧资产）
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

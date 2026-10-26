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
- **中文管理面板** `/admin/`：仪表盘、Cline 账号池、TraeWork、WorkBuddy（内嵌原面板）、OpenCode、AMDCloud、OpenRouter、TokenHarbor、Other、代理池、网关设置（自定义别名/封禁平台/封禁模型）、请求日志、实时统计。

## 平台速览

| 平台 | 前缀 | 凭据类型 | 模型目录 | 探测默认模型 |
|---|---|---|---|---|
| Cline | `cline-free/` | OAuth refreshToken / 静态 `sk_` key | 官方免费源 60s 自动同步 | 排序最小 |
| TraeWork | `trae:` | OAuth（本机 8765 helper 网页授权） | 上游目录同步（需可路由账号） | `glm-5.2` |
| WorkBuddy | `wbcn:` / `wbgb:` | OAuth（内嵌面板登录） | CN/Global 双域实时探测 | 官方稳定模型优先 |
| OpenCode | `zen:` | zen key（本地铸造会话 ID） | 上游免费目录 10min 同步 | `big-pickle` 优先 |
| OpenRouter | `oprt:` | `sk-or-v1-…` key | [free-models 页白名单](https://openrouter.ai/collections/free-models) 5min 同步 | `openrouter/free` |
| AMDCloud | `amd:` | `rc-…` key（[Token Factory](https://developer.amd.com.cn/radeon/tokenfactory)） | 上游 `/v1/models` 5min 同步（需 Bearer key；仅 chat 模型） | `DeepSeek-V4-Flash` |
| TokenHarbor | `tkhb:` | `thk_live_…` key（[dashboard](https://tokenharbor.ai/dashboard/api-keys)） | 上游 `/v1/models` 5min 同步（需 key；仅 `:free`） | `deepseek-v4.1-flash:free` |
| Other | 自定 `acme:` | 各上游 key | `{BaseURL}/models` 拉取 + 手动添加 + 多选暴露 | 设置页配置 |

各平台共用同一套池语义：轮转策略、429 按 `Retry-After` 冷却、401/403 冷却 1 小时、Test 按钮发真实探测请求并在成功后立即解除冷却、主/备代理绑定、新增 key 默认不参与路由。

## 快速开始（Docker）

```bash
docker run -d --name afree-proxy \
  -p 3457:3457 \
  -v cline-proxy-data:/app/data \
  -e PORT=3457 \
  -e API_KEY=change-me \
  -e ADMIN_PASSWORD=change-me-too \
  ghcr.io/foxy1402/cline-proxy:latest
```

打开 `http://127.0.0.1:3457/admin/`，用 `ADMIN_PASSWORD` 登录，添加账号或 key。镜像多架构（`linux/amd64` 原生 + `linux/arm64` 交叉编译）；`:latest` 随每次构建发布，`vX.Y.Z` tag 额外发布 `:X.Y.Z`。

> 镜像名跟随 GitHub 仓库名；仓库名不同请替换。

所有状态都在 `/app/data` 卷里，备份 = 复制该卷。

## 关键环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `PORT` | `3457` | 监听端口（`-port` flag 优先） |
| `DATA_DIR` | `/app/data` | 容器内状态目录 |
| `API_KEY` / `API_KEY_FILE` | 空 | `/v1` 唯一有效 key（`Authorization: Bearer` 或 `x-api-key`） |
| `ADMIN_PASSWORD` / `ADMIN_PASSWORD_FILE` | 空 | 管理面板密码；设置后所有 `/admin/*` 需登录 |
| `PROXY_ISOLATION` | 未设置 | 身份↔出口绑定：`true` 强制开启（面板只读）；`false` 默认改为关（面板可改）；未设置默认开启。只接受 true/false |
| `ZEN_KEYS` | 空 | 逗号分隔的 zen key 启动种子；面板已有 key 时不覆盖 |
| `ZEN_SESSION_ROTATE_MINUTES` | `120` | zen 会话到期重铸间隔（分钟），`0` 关闭 |
| `STRICT_MODEL_MATCH` | `true` | 未知模型名 400 而非静默兜底 |
| `POOL_STRATEGY` | `round_robin` | Cline 选号策略：`round_robin` / `fill` / `random` |
| `CLINE_ACCOUNTS_SEED_FILE` | 空 | 启动时账号池为空则导入的种子 JSON |
| `PUID` / `PGID` | `100` / `101` | 容器运行身份（entrypoint 自愈属主后降权；bind mount 时设为宿主机用户） |
| `LOG_REQUESTS` | `true` | 请求日志（仅元数据：IP/路径/模型/状态/耗时，绝无对话内容） |
| `MAX_BODY_MB` | `32` | 请求体上限，超出 413 |
| `STREAM_ATTEMPT` | `2` | 流式中断缓冲重试总次数；每次重试会重耗上游免费额度 |

Fail-closed 启动：绑定非环回地址且未设 `API_KEY` 时拒绝启动。

### OpenCode zen 会话 ID

zen 会话 ID 在**本地铸造**——上游免费层格式门是无状态检查（`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`），网关为每个 key 首次使用时铸造兼容 ID 并持久化到数据卷（`.zen-sessions.json`），无内嵌 CLI、无收割机。请求级 ID（`x-opencode-request`）与注入的 gate 工具参数 schema 同样对齐官方 CLI 形态。连续 2 次 FreeTier 403 自动重铸重试；会话按 `ZEN_SESSION_ROTATE_MINUTES`（默认 120 分钟）后台轮换重铸——铸造纯本地零配额消耗，轮换器还会为未用过的 key 预铸（避免轮转坍缩到单 key）。

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

`GET /v1/models` 列出全部可服务模型（含 Combos）。免费目录自动同步（cline 60s、zen 10min、or/amd/th 5min、wb 实时探测）；线上列表是权威，内置种子只覆盖冷启动/离线启动，下架/新免费模型自动增删。

### 账号种子

支持两种账号并可混用（`CLINE_ACCOUNTS_SEED_FILE`，池空时导入）：

```json
[
  {"refreshToken": "workos-oauth-refresh-token", "email": "acc1@example.com"},
  {"apiToken": "sk_...", "email": "acc2@example.com"}
]
```

**各平台新增身份默认禁用**：新加入的账号/key 落盘时路由参与即关闭（Cline `pool_enabled=false`；zen/or/amd/th/other 无路由表项），轮转自动跳过，直到在面板勾选「启用」。存量缺字段的身份保持原状态——升级不停服。`ZEN_KEYS` 来的 key 同规则。

### Combos（别名模型）

面板创建自定义别名 ID（如 `my-flash` → `cline-free/deepseek-v4.1-flash`）。严格同平台指向；别名出现在 `/v1/models` 供 IDE 直接选择。

### Other（小上游子平台）

不值得独立页面的上游。Other 页可建任意多个子平台：名称、唯一模型前缀（不得与内置前缀冲突）、Base URL。有 key 后从 `{BaseURL}/models` 拉目录（也可手动添加，手动项跨刷新保留），多选下拉控制哪些模型对外暴露；未勾选的模型调用时明确 400。

## TraeWork 网页授权

添加账号走「网页授权」手风琴弹窗：网关生成本机授权助手（Python，监听 127.0.0.1:8765），浏览器打开授权页登录后凭据回传网关。助手脚本从网关下载，面板会比对 relay 源防止凭据写错实例；也支持手动粘贴凭据。授权只认本机 helper——上游实测拒绝网关直连回调。

## 代理隔离（身份 ↔ 出口绑定）

不开隔离时，每次上游尝试都在整个代理池轮转：同一账号出口 IP 漂移、同一出口轮流出现多个账号——两者都是典型风控红旗。隔离模式（默认开启）改为固定绑定：

- 每个身份（Cline 账号 / zen key / WorkBuddy 账号 / TraeWork 账号 / 各 key 池 key）绑定一主一备代理。请求永远走主，主不可用走备，两者都冷却/被删时该身份整体跳过——**绝不回退其他出口或直连，隔离优先于可用性**。
- 未绑定身份沿用面板每平台的走池开关（缺省 = 走池），空绑定配置行为与旧版完全一致。
- 面板支持单行绑定、一键均匀分配（主 = `pool[i%N]`、备 = `pool[(i+1)%N]`）、一键清空；绑定代理被删时行标 ⚠ 并保持跳过直到修复。备用代理开关关闭时备用槽整体退出路由（保留旧值，重开即恢复）。
- **代理在线率**：代理池页显示每个代理真实流量的传输层成功率（累计 + 最近 50 次滚动窗口），纯被动采样不产生探活流量、不影响路由；最近成功率 <80%（≥10 样本）标红。统计落盘 `.proxy-health.json`，编辑池清零。

socks5/socks5h 全支持；直连哨兵（主 = direct）可显式声明直连出口。zen 免费额度按出口 IP 计（约 200 请求/5h），把 key 绑到不同出口才是真正解锁多 key 容量的方式。

## 封禁与黑名单

- **封禁平台**（网关设置页）：整平台退出会话路由，三端点 503、`/v1/models` 同步摘除；签到等维护任务不受影响。确认（蓝）+ 复位（黄）双按钮，chip 红色警示。
- **封禁模型**：手动封禁 + 自动封禁（上游按账号类型拒绝的模型在出口命中判据后自动入表），均从 `/v1/models` 移除。
- **无启用凭据闸**：与封禁同闸——任何平台没有任何可路由凭据（含反代开关全关）时请求 503 明确报错，宁拒不路由。

## Troubleshooting（实测经验）

| 症状 | 实际含义 | 处理 |
|---|---|---|
| `bound proxies unavailable … the credential itself was not verified` | **出口**挂了，不是凭据坏。隔离语义下绑定身份被跳过，绝不改道 | 修/换绑定出口；**别删 key**；到提示的冷却时刻后重测 |
| 单次 Test 失败，几分钟后无任何改动又通过 | 出口冷却只有 2 分钟，并发探测会把正常出口打成短暂冷却 | 单次失败不下结论，≥2 分钟后重测 |
| `502` 内嵌上游 4xx 文本 | 客户端参数错；网关把所有上游错误映射为 502 | 读内嵌的上游 message；注意客户端重试 502 会重耗免费额度 |
| `400 model … is not available on this gateway`（刚还好好的） | 目录是活的：上游 feed 会修剪下架模型 | 重读 `/v1/models`；脚本/CI 别写死模型 ID |
| `amd model sync failed: catalog status 401` | 同步凭据失效（AMD key 会静默过期；同步需启用 key 的 Bearer 头） | 重测 AMD key |
| TokenHarbor `403 … network address not accepted` | **出口 IP** 被封，不是 key | 给该 key 换绑定出口 |
| zen 免费层 `403 FreeTierError` 突增 | 上游收紧了格式门（会话/请求 ID/gate 工具） | 看日志刷新频率趋势；网关会自动重铸重试 |

## 开发

```bash
go build ./... && go vet ./...
./afree-proxy -host 127.0.0.1 -port 3457   # 或: go run .
docker compose up -d --build               # 源码构建（PROXY_PORT 换端口）
```

CI：push 到 `main` 跑 `go build` + `go vet`，Buildx 发布多架构镜像到 GHCR（amd64 原生编译，arm64 交叉编译；仅内嵌 opencode CLI 阶段在构建期跑 QEMU）。打 `v*` tag 额外发布版本 tag。

代码结构：

```
├── main.go                  入口、CLI flags、运行检测
├── internal/app/
│   ├── proxy.go             /v1 路由、chat handler、上游调用、聚合
│   ├── responses.go         /v1/responses 方言翻译
│   ├── cross_endpoint.go    /v1/responses 与 /v1/messages 的多平台归口 + 缓冲重试
│   ├── zen.go / zen_session.go / zen_endpoint.go   zen 上游、会话铸造/轮转、端点自学习
│   ├── trae.go              TraeWork：raw 协议、SSE 聚合、签到调度、OAuth helper
│   ├── amd.go / openrouter.go / tokenharbor.go / other.go   key 池平台四件套
│   ├── workbuddy.go/_config/_proxy/_dialects  WorkBuddy 子系统、出口绑定、方言
│   ├── platform_routability.go  无启用凭据整平台拒路由闸
│   ├── platform_blacklist.go / model_blacklist.go  封禁平台/封禁模型
│   ├── pool.go / seed.go    Cline 账号池、启动种子
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

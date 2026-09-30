# AI Free Proxy 操作手册

> 本手册对应当前版本（含代理隔离、OpenCode 更名、自定义别名等全部改动）。
> 管理面板地址：`http://<host>:3457/admin/`

---

## 1. 系统概述

AI Free Proxy 是一个单二进制 Go 网关，把 Cline、OpenCode 与 WorkBuddy 三类上游额度聚合成一个统一的 OpenAI 兼容接口：

```
Cursor / ZCode / Cline / Claude Code 等 IDE
        │  POST /v1/chat/completions、/v1/messages、/v1/responses
        ▼
┌──────────────── AI Free Proxy :3457 ────────────────┐
│  鉴权 → 请求日志 → 自定义别名改写 → 路由              │
└──┬──────────────┬──────────────┬─────────────────────┘
   │ OpenCode 免费 │ cn:/global:  │ 其他 model
   ▼              ▼              ▼
OpenCode      WorkBuddy       Cline 账号池
多 key 轮转   多账号池/任务    轮转
   │              │              │
   └────────────► 出口代理池（每请求轮转或按隔离绑定）◄────┘
```

- **路由规则**：请求里的 `model` 决定去向——`cn:` / `global:` 前缀模型走 WorkBuddy；OpenCode 免费模型走 OpenCode 上游；OpenCode 付费模型直接 400；其余走 Cline 账号池（未知模型名默认 400，可在面板关闭严格匹配）。
- **四个池**：Cline 账号池、OpenCode key 池、WorkBuddy 账号池、出口代理池，各自独立轮转、独立冷却。
- **代理隔离**（默认开启）：Cline 账号、OpenCode key 与 WorkBuddy 账号可绑定主/辅代理，固定出口 IP，降低风控风险。

所有状态保存在数据卷 `/app/data`（容器内），WorkBuddy 状态在 `data/workbuddy/`，出口绑定在 `data/.workbuddy-proxies.json`；备份 = 备份整个数据卷。

---

## 2. 部署与启动

### 2.1 Docker 运行（推荐）

```bash
docker run -d --name afree-proxy --restart unless-stopped \
  -p 3457:3457 \
  -v cline-proxy-data:/app/data \
  -e PORT=3457 \
  -e API_KEY=换成长随机串 \
  -e ADMIN_PASSWORD=换成管理密码 \
  -e ZEN_KEYS=你的zenkey1,你的zenkey2 \
  afree-proxy:local
```

- 监听非回环地址时必须设置 `API_KEY` 和 `ADMIN_PASSWORD`，否则**拒绝启动**（fail-closed）。
- 数据卷 `cline-proxy-data` 保存账号、配置、日志，重建容器不丢数据。
- **NAS bind-mount 权限**：用宿主机目录挂载时加 `-e PUID=1000 -e PGID=1000`（换成你的宿主机用户），入口脚本会在每次启动时自动把数据目录属主改为该用户再降权运行——重建容器无需手动 chown。默认 `100:101`（镜像内置 app 用户）。

### 2.2 Docker Compose

```bash
# 在项目目录建 .env：API_KEY=xxx / ADMIN_PASSWORD=xxx / 可选 ZEN_KEYS=...
docker compose up -d --build
```

### 2.3 常用环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `PORT` | 3457 | 监听端口 |
| `API_KEY` / `API_KEY_FILE` | 无 | /v1 接口唯一 key（Bearer 或 x-api-key） |
| `ADMIN_PASSWORD` / `ADMIN_PASSWORD_FILE` | 无 | 面板登录密码 |
| `ZEN_KEYS` | 空 | OpenCode key，逗号分隔；也可面板配置 |
| `POOL_STRATEGY` | round_robin | Cline 账号池策略：round_robin / fill / random |
| `PROXY_ISOLATION` | 未设置 | 只接受 true/false：true 强制开启（面板只读）；false 默认关闭、面板可改；未设置默认开启、面板可改。其他值告警并按未设置处理 |
| `STRICT_MODEL_MATCH` | true | 未知模型名返回 400（false 回退默认模型） |
| `ZEN_HARVEST` | 1 | 会话收割机开关（0 = 纯网关模式） |
| `ZEN_HARVEST_INTERVAL_HOURS` | 4 | 会话重铸间隔，须小于 zen 的 5 小时窗口 |
| `LOG_REQUESTS` | true | 请求日志（仅元数据） |
| `MAX_BODY_MB` | 32 | 请求体上限，超出 413 |

---

## 3. 管理面板

打开 `http://<host>:3457/admin/`，输入 `ADMIN_PASSWORD` 登录。右上角两个按钮：**中**/EN 切换界面语言（偏好保存在浏览器本地）、主题切换。

侧边栏结构：

```
仪表盘
账号池：  Cline | OpenCode → WorkBuddy
服务：    代理池 → 网关设置 → 自定义别名 → 请求日志
```

### 3.1 仪表盘

- 四张卡片：账号总数 / 活跃 / 冷却中 / 已失效（每 10 秒自动刷新）。
- 快捷操作：添加账号、刷新全部 token、从文件导入、生成 API key。
- 快速上手三步指引与接口列表。

### 3.2 账号池 → Cline（Cline 账号管理）

**添加账号**（账号页「添加」按钮或仪表盘「添加账号」）：
- **OAuth 浏览器登录**：点击后打开链接，用 Google/GitHub/邮箱登录并确认，refreshToken 自动捕获。
- **手动录入**：粘贴 refreshToken（先向上游验证）或静态 `sk_` API key（直接入库）。
- **批量导入**：JSON 数组或每行一条凭据；`sk_` 开头识别为 API key。

**账号表格**：
- **Test**：发一次真实探测（max_tokens=1），成功即清除冷却/失效状态；消耗一次配额。
- **Reset**：探测上游限流状态；仍在限流则保持冷却并显示预计恢复时间。
- **启用（Pool 列）**：勾选才参与轮换。**新增账号默认不勾选**——新号先入库、不进轮换，确认可用后在表格里勾上启用。已有账号保持原状态（旧数据缺该字段按启用处理），升级不会让在用的号停摆。
- **代理绑定**：每行两个下拉（主出口/辅出口），选择后立即保存。绑定后该账号**永远**从主代理出网，主冷却落到辅代理；两者都不可用时该账号被整体跳过（绝不走其他出口）。
- **均匀分配代理**：一键把代理池按 `主 = 池[i%N]、辅 = 池[(i+1)%N]` 分给全部账号（覆盖已有绑定）。
- 绑定的代理若已从池中删除，行尾显示 ⚠，修复前该账号一直被跳过。

**状态说明**：active 可用；cooldown 冷却中（429 触发，显示预计恢复时间，到期自动恢复）；expired 已失效（refresh token 被上游拒绝，需重新添加）。

### 3.3 账号池 → OpenCode（key 与会话管理）

- **API keys**：每行一个 key（`ZEN_KEYS` 环境变量注入或面板维护），round-robin 轮转，429 自动冷却。
- **启用（Pool 列）**：勾选才进入请求轮转，铸造也跟随该开关（禁用后不再自动铸造）。**新增 key 默认禁用**（面板添加或 `ZEN_KEYS` 注入都一样），确认可用后勾上；已有 key 保持原状态。
- **探测模型**：Test 按钮使用的模型，默认自动（big-pickle 优先）。
- **每行状态**：用量、会话（live/未铸造/已失效）、冷却截止时刻、代理绑定下拉（语义同 Cline 账号绑定）。
- **Test 按钮**：对单个 key 发真实探测，成功即清冷却；429 返回预计恢复时间；403 表示会话已死。
- **Live 会话 ID 区块**：免费层只认上游真实见过的会话 ID。
  - **铸造缺失的会话**：只补未铸造的 key。
  - **强制重铸全部**：全部重 mint（后台执行，显示进度）。
  - 收割机自动运行：启动 10 秒后补缺、反复 403 时触发、每 10 分钟巡检补铸超过 4 小时的会话。
- **代理绑定与铸造**：绑定了代理的 key，铸造也走绑定出口（socks5 经本地桥转发），保证会话 IP 恒定；绑定出口全不可用时本轮跳过铸造。

### 3.4 账号池 → OpenCode → WorkBuddy

WorkBuddy 子系统挂在现有管理面板的 `OpenCode → WorkBuddy` 下，沿用 `/admin/*` 的登录会话，不需要维护第二套面板密码。其账号池、状态、调度器与 Cline / OpenCode 池相互独立；WorkBuddy 初始化失败时只禁用此子系统，不影响原网关启动。

- **OAuth 登录**：面板「添加账号」支持 CN 与 global 两套设备授权流程。完成浏览器登录后凭证自动落盘并热加载进池，无需重启容器。
- **多账号池**：账号按上游实现参与加权轮转、失败换号、会话粘性、在途限流与积分/状态选择；支持解冻、禁用、刷新余额、单号签到、移除和批量导入等运维操作。**新增账号（登录或导入）默认不参与路由**，池状态里 `pool_enabled=false`；在 WorkBuddy 账号表勾选 **Pool** 后进入轮转。已有账号保持原状态。
- **熔断与冷却**：保留 429 软冷却、404 短冷却、硬冷却、连续失败熔断和冷却到期恢复等状态。
- **定时任务与自动完成**：签到、活跃、旅行、保活、成长任务及余额刷新等排程可在 WorkBuddy 配置页分别启停；任务中心可扫描、排队和自动领奖。
- **协议能力**：`/v1/chat/completions`、`/v1/messages` 与 `/v1/responses` 都支持 WorkBuddy realm 前缀模型；流式与非流式、推理字段/effort 降级、系统提示词与指纹脱敏开关均由移植后的 WorkBuddy handler 处理。

WorkBuddy 模型必须带 realm 前缀：

```text
cn:<model>        # CN 账号路由
global:<model>    # 国际账号路由
```

`GET /v1/models` 会把可用模型合并为上述完整 ID。三个现有客户端入口都会识别这些前缀；裸模型名不会交给 WorkBuddy，仍按原规则走 Cline 或 OpenCode，避免模型名碰撞后静默改路由。

**数据与出口绑定**：

- `data/workbuddy/auths/`：OAuth 凭证。
- `data/workbuddy/config.json`、`state.json`、`model.json`：配置、池状态和模型目录。
- `data/.workbuddy-proxies.json`：WorkBuddy UID 到主/辅出口的持久化绑定。

代理隔离启用时，新账号首次发生已认证上游请求后按代理池顺序分配稳定主/辅出口。账号聊天、余额、签到、任务和上报等已认证请求都走绑定出口；主出口不可用走辅出口，两者都冷却或已从池中删除时直接失败，不回退直连或其他出口。可调用以下管理 API 排障：

```text
GET  /admin/api/workbuddy/proxy
POST /admin/api/workbuddy/proxy/set
POST /admin/api/workbuddy/proxy/clear
```

设备授权、登录轮询以及模型静态目录刷新发生在 UID 可用之前，不使用账号出口绑定，也不携带既有账号凭证；账号创建完成后的认证流量才按 UID 出口绑定发送。

### 3.5 服务 → 代理池

- **共享出口代理**：每行一条，支持 `http://user:pass@host:port` 与 `socks5://host:port`（网关侧 socks5 原生支持；收割 CLI 走本地桥）。触发限流的代理自动冷却 2 分钟并被跳过。
- **轮转策略**：round_robin（轮询）/ random（随机）/ fill（填满优先）——这是**未绑定身份**的出口选择方式。
- **代理池的使用范围**（面板「全局策略」逐平台开关，默认均走代理池）：
  - Cline 上游：走代理池/直连（默认走代理池）。
  - OpenCode 上游：走代理池/直连（默认走代理池）。
  - WorkBuddy：未绑定账号走代理池/直连（默认走代理池）；已绑定账号首次已认证请求时自动分配主/辅出口，后续聊天、定时任务与账号运维均走绑定。
- **代理隔离**（默认启用）：
  - 启用：绑定了主/辅代理的 Cline 账号、OpenCode key 或 WorkBuddy 账号只从绑定出口出网；双不可用即跳过该身份。
  - 关闭：忽略绑定，所有出口按上方轮转策略轮换（代理池未启用时直连）。
  - `PROXY_ISOLATION=true` 强制开启（面板只读并提示）；`false` 仅把默认值改为关闭，面板仍可修改。只接受 true/false，其他值告警并按未设置处理。
- **冷却状态**：显示各代理的冷却截止时刻。
- **代理健康（在线率）**：每代理一行，统计真实请求的出口成败——「在线率」是上次编辑池以来的传输层成功率（拨号 + 隧道握手 + TLS；上游 4xx/5xx 不算代理的锅），「最近」是最近 50 次的滑动窗口。样本 ≥10 且最近成功率低于 80% 的代理标红，提示该换点了。纯被动采样，不产生任何探测流量；低在线率**不会**自动触发冷却或改路由，换不换由你决定。编辑代理列表会清空全部统计；统计跨重启保留（`.proxy-health.json`）。三平台（Cline / OpenCode / WorkBuddy）的真实出网都计入，直连出口不记账。

### 3.6 服务 → 网关设置

- **API keys**：生成/删除客户端访问 /v1 的 key。未配置任何 key 时 /v1 允许匿名（仅建议本机使用）。
- **可用模型**：官方免费模型源自动同步（60 秒）；Cline 模型状态标签（可用/空响应/订阅制/已下架）来自真实探测。
- **常规配置**：默认模型（模型名无法识别时严格模式下报错而非静默替换）、调度策略（Cline 账号池的轮询/填满/随机）。
- **请求头**：模拟官方 Cline CLI 的请求头，一般不需要动。
- **危险区**：删除全部账号 / 全部 API key（不可撤销）。

### 3.7 服务 → 自定义别名

- 客户端以别名 ID 作为 model 请求，网关改写为所选平台的真实模型。
- 目标模型必须与别名同平台（cline 或 opencode）；别名 ID 不得与真实模型冲突。
- 可勾选「走代理池」让该别名强制走代理池（隔离模式下绑定账号仍按绑定优先）。
- 别名会出现在 `/v1/models` 列表里，IDE 可直接选择。

### 3.8 服务 → 请求日志

最近 500 条请求（IP/方法/路径/模型/路由/状态/耗时），自动刷新可暂停。只记录元数据，**不含对话内容**。落盘 `data/requests.jsonl`（10MB 上限自动清空）。

---

## 4. IDE 接入

| 客户端 | 端点 | Base URL |
|---|---|---|
| 通用（OpenAI 格式） | `POST /v1/chat/completions` | `http://<host>:3457/v1` |
| Claude Code / Cline | `POST /v1/messages` | 同上 |
| Cursor 等 | `POST /v1/responses` | 同上 |

- API Key：网关设置里生成的 key（或 `API_KEY` 环境变量值）。
- 模型名：从 `/v1/models` 里选，例如 `cline-free/deepseek-v4.1-flash`（Cline 池）、`mimo-v2.5-free`（OpenCode）、`cn:<model>` / `global:<model>`（WorkBuddy）、或你创建的自定义别名。
- 工具调用、流式、usage 均完整支持；客户端参数（max_tokens/temperature/tools 等）全部透传。

---

## 5. 日常运维

```bash
docker logs -f afree-proxy        # 跟踪日志
docker restart afree-proxy        # 重启
docker stats afree-proxy          # 资源占用
docker cp afree-proxy:/app/data ./backup   # 备份数据
```

- **升级**：拉新镜像后 `docker rm -f afree-proxy` 再按原命令重建（数据卷不动即保数据）。
- **日志文件**：`data/afree-proxy.log`（运行日志）、`requests.jsonl`（请求元数据）、`zen-stats.jsonl`（OpenCode 统计）；WorkBuddy 面板日志同时镜像到运行日志。

---

## 6. 常见问题

| 现象 | 原因与处理 |
|---|---|
| 首个请求很慢 / 403 | 首启时收割机正在为各 key 铸造会话（每 key 约 15-20 秒），等铸造完成即可；可到 OpenCode 页手动「铸造缺失的会话」 |
| 所有请求 429 | 上游限流。看冷却截止时刻；增加账号/key 分摊；多 key 绑定不同代理还能按 IP 扩容 zen 配额 |
| 提示 boundBlocked=N | 隔离模式下 N 个账号的绑定出口全部不可用（冷却/被删）。到代理池页检查代理，或重新绑定 |
| 账号 expired | refreshToken 被上游拒绝，重新添加账号 |
| OpenCode 页提示 Harvester unavailable | 容器内没有 opencode CLI（罕见架构），收割降级；key 将无法 mint 会话 |
| WorkBuddy 请求 400 model 不存在 | WorkBuddy 模型必须带 `cn:` 或 `global:` 前缀；先看 `/v1/models` 的完整 ID |
| WorkBuddy 请求因出口不可用失败 | 代理隔离下绑定主/辅出口都冷却或已被删除会 fail-closed；到代理池页恢复出口，或通过 `/admin/api/workbuddy/proxy` 检查/重绑 |
| 下拉框弹层颜色异常 | 已通过 color-scheme 修复；若仍出现，确认浏览器为较新版本 |
| 忘记 ADMIN_PASSWORD | 改容器环境变量重建即可（数据保留） |
| 想临时回到旧的每请求全局轮转 | 面板关闭「代理隔离」，或设 `PROXY_ISOLATION=false`（默认关闭、面板可再改；绑定被忽略，出口按轮转策略轮换） |

---

## 7. 安全建议

- 公网部署务必设置强 `API_KEY` 与 `ADMIN_PASSWORD`；建议前面加反向代理 + HTTPS（compose 里有 Caddy 模板）。
- `API_KEY`/`ADMIN_PASSWORD` 可用 `_FILE` 变量挂载 docker secrets；文件读不到时网关按 fail-closed 拒绝而不是放行。
- 导出账号（Accounts 页「导出账号」）包含 refreshToken，妥善保管，不要外传。
- 隔离模式 + 每账号独立代理是防封关键：同一出口 IP 上轮换多账号是最典型的风控特征。

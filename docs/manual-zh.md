# AI Free Proxy 操作手册

> 本手册对应当前版本（含代理隔离、OpenCode 更名、自定义别名等全部改动）。
> 管理面板地址：`http://<host>:3457/admin/`

---

## 1. 系统概述

AI Free Proxy 是一个单二进制 Go 网关，把两类免费上游额度聚合成一个统一的 OpenAI 兼容接口：

```
Cursor / ZCode / Cline / Claude Code 等 IDE
        │  POST /v1/chat/completions、/v1/messages、/v1/responses
        ▼
┌──────────────── AI Free Proxy :3457 ────────────────┐
│  鉴权 → 请求日志 → 自定义别名改写 → 路由              │
└──────┬──────────────────────────┬────────────────────┘
       │ model 为 OpenCode 免费模型 │ 其他 model
       ▼                          ▼
  OpenCode（多 key 轮转）      Cline 账号池（轮转）
       │                          │
       └────► 出口代理池（每请求轮转或按隔离绑定）◄────┘
```

- **路由规则**：请求里的 `model` 决定去向——OpenCode 免费模型走 OpenCode 上游；OpenCode 付费模型直接 400；其余走 Cline 账号池（未知模型名默认 400，可在面板关闭严格匹配）。
- **三个池**：Cline 账号池、OpenCode key 池、出口代理池，各自独立轮转、独立冷却。
- **代理隔离**（默认开启）：账号/key 可绑定主/辅代理，固定出口 IP，降低风控风险。

所有状态保存在数据卷 `/app/data`（容器内），备份 = 备份该卷。

---

## 2. 部署与启动

### 2.1 Docker 运行（推荐）

```bash
docker run -d --name cline-proxy --restart unless-stopped \
  -p 3457:3457 \
  -v cline-proxy-data:/app/data \
  -e PORT=3457 \
  -e API_KEY=换成长随机串 \
  -e ADMIN_PASSWORD=换成管理密码 \
  -e ZEN_KEYS=你的zenkey1,你的zenkey2 \
  cline-proxy:local
```

- 监听非回环地址时必须设置 `API_KEY` 和 `ADMIN_PASSWORD`，否则**拒绝启动**（fail-closed）。
- 数据卷 `cline-proxy-data` 保存账号、配置、日志，重建容器不丢数据。

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
| `PROXY_ISOLATION` | true | 代理隔离开关；显式设置时覆盖面板开关 |
| `CLINE_USE_PROXIES` | false | Cline 上游走共享代理池 |
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
账号池：  Cline | OpenCode
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
- **代理绑定**：每行两个下拉（主出口/辅出口），选择后立即保存。绑定后该账号**永远**从主代理出网，主冷却落到辅代理；两者都不可用时该账号被整体跳过（绝不走其他出口）。
- **均匀分配代理**：一键把代理池按 `主 = 池[i%N]、辅 = 池[(i+1)%N]` 分给全部账号（覆盖已有绑定）。
- 绑定的代理若已从池中删除，行尾显示 ⚠，修复前该账号一直被跳过。

**状态说明**：active 可用；cooldown 冷却中（429 触发，显示预计恢复时间，到期自动恢复）；expired 已失效（refresh token 被上游拒绝，需重新添加）。

### 3.3 账号池 → OpenCode（key 与会话管理）

- **API keys**：每行一个 key（`ZEN_KEYS` 环境变量注入或面板维护），round-robin 轮转，429 自动冷却。
- **探测模型**：Test 按钮使用的模型，默认自动（big-pickle 优先）。
- **每行状态**：用量、会话（live/未铸造/已失效）、冷却截止时刻、代理绑定下拉（语义同 Cline 账号绑定）。
- **Test 按钮**：对单个 key 发真实探测，成功即清冷却；429 返回预计恢复时间；403 表示会话已死。
- **Live 会话 ID 区块**：免费层只认上游真实见过的会话 ID。
  - **铸造缺失的会话**：只补未铸造的 key。
  - **强制重铸全部**：全部重 mint（后台执行，显示进度）。
  - 收割机自动运行：启动 10 秒后补缺、反复 403 时触发、每 10 分钟巡检补铸超过 4 小时的会话。
- **代理绑定与铸造**：绑定了代理的 key，铸造也走绑定出口（socks5 经本地桥转发），保证会话 IP 恒定；绑定出口全不可用时本轮跳过铸造。

### 3.4 服务 → 代理池

- **共享出口代理**：每行一条，支持 `http://user:pass@host:port` 与 `socks5://host:port`（网关侧 socks5 原生支持；收割 CLI 走本地桥）。触发限流的代理自动冷却 2 分钟并被跳过。
- **轮转策略**：round_robin（轮询）/ random（随机）/ fill（填满优先）——这是**未绑定身份**的出口选择方式。
- **代理池的使用范围**：
  - Cline 上游：走代理池/直连（默认直连；`CLINE_USE_PROXIES=true` 强制开启）。
  - OpenCode 上游：代理列表非空即自动走池，无单独开关。
- **代理隔离**（默认启用）：
  - 启用：绑定了主/辅代理的 Cline 账号或 OpenCode key 只从绑定出口出网；双不可用即跳过该身份。
  - 关闭：忽略绑定，所有出口按上方轮转策略轮换（代理池未启用时直连）。
  - `PROXY_ISOLATION` 环境变量设置时覆盖此开关（面板会提示并禁用）。
- **冷却状态**：显示各代理的冷却截止时刻。

### 3.5 服务 → 网关设置

- **API keys**：生成/删除客户端访问 /v1 的 key。未配置任何 key 时 /v1 允许匿名（仅建议本机使用）。
- **可用模型**：官方免费模型源自动同步（60 秒）；Cline 模型状态标签（可用/空响应/订阅制/已下架）来自真实探测。
- **常规配置**：默认模型（模型名无法识别时严格模式下报错而非静默替换）、调度策略（Cline 账号池的轮询/填满/随机）。
- **请求头**：模拟官方 Cline CLI 的请求头，一般不需要动。
- **危险区**：删除全部账号 / 全部 API key（不可撤销）。

### 3.6 服务 → 自定义别名

- 客户端以别名 ID 作为 model 请求，网关改写为所选平台的真实模型。
- 目标模型必须与别名同平台（cline 或 opencode）；别名 ID 不得与真实模型冲突。
- 可勾选「走代理池」让该别名强制走代理池（隔离模式下绑定账号仍按绑定优先）。
- 别名会出现在 `/v1/models` 列表里，IDE 可直接选择。

### 3.7 服务 → 请求日志

最近 500 条请求（IP/方法/路径/模型/路由/状态/耗时），自动刷新可暂停。只记录元数据，**不含对话内容**。落盘 `data/requests.jsonl`（10MB 上限自动清空）。

---

## 4. IDE 接入

| 客户端 | 端点 | Base URL |
|---|---|---|
| 通用（OpenAI 格式） | `POST /v1/chat/completions` | `http://<host>:3457/v1` |
| Claude Code / Cline | `POST /v1/messages` | 同上 |
| Cursor 等 | `POST /v1/responses` | 同上 |

- API Key：网关设置里生成的 key（或 `API_KEY` 环境变量值）。
- 模型名：从 `/v1/models` 里选，例如 `cline-free/deepseek-v4.1-flash`（Cline 池）、`mimo-v2.5-free`（OpenCode）、或你创建的自定义别名。
- 工具调用、流式、usage 均完整支持；客户端参数（max_tokens/temperature/tools 等）全部透传。

---

## 5. 日常运维

```bash
docker logs -f cline-proxy        # 跟踪日志
docker restart cline-proxy        # 重启
docker stats cline-proxy          # 资源占用
docker cp cline-proxy:/app/data ./backup   # 备份数据
```

- **升级**：拉新镜像后 `docker rm -f cline-proxy` 再按原命令重建（数据卷不动即保数据）。
- **日志文件**：`data/cline-proxy.log`（运行日志）、`requests.jsonl`（请求元数据）、`zen-stats.jsonl`（OpenCode 统计）。

---

## 6. 常见问题

| 现象 | 原因与处理 |
|---|---|
| 首个请求很慢 / 403 | 首启时收割机正在为各 key 铸造会话（每 key 约 15-20 秒），等铸造完成即可；可到 OpenCode 页手动「铸造缺失的会话」 |
| 所有请求 429 | 上游限流。看冷却截止时刻；增加账号/key 分摊；多 key 绑定不同代理还能按 IP 扩容 zen 配额 |
| 提示 boundBlocked=N | 隔离模式下 N 个账号的绑定出口全部不可用（冷却/被删）。到代理池页检查代理，或重新绑定 |
| 账号 expired | refreshToken 被上游拒绝，重新添加账号 |
| OpenCode 页提示 Harvester unavailable | 容器内没有 opencode CLI（罕见架构），收割降级；key 将无法 mint 会话 |
| 下拉框弹层颜色异常 | 已通过 color-scheme 修复；若仍出现，确认浏览器为较新版本 |
| 忘记 ADMIN_PASSWORD | 改容器环境变量重建即可（数据保留） |
| 想临时回到旧的每请求全局轮转 | 面板关闭「代理隔离」，或设 `PROXY_ISOLATION=false`（绑定被忽略，出口按轮转策略轮换） |

---

## 7. 安全建议

- 公网部署务必设置强 `API_KEY` 与 `ADMIN_PASSWORD`；建议前面加反向代理 + HTTPS（compose 里有 Caddy 模板）。
- `API_KEY`/`ADMIN_PASSWORD` 可用 `_FILE` 变量挂载 docker secrets；文件读不到时网关按 fail-closed 拒绝而不是放行。
- 导出账号（Accounts 页「导出账号」）包含 refreshToken，妥善保管，不要外传。
- 隔离模式 + 每账号独立代理是防封关键：同一出口 IP 上轮换多账号是最典型的风控特征。

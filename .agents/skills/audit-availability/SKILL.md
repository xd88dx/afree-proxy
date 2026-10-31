---
name: audit-availability
description: 评估 afree-proxy 服务真实可用性的完整配方——从本机 newapi 容器的 sqlite 渠道映射提取测试清单，对各平台前缀目标模型发真实请求逐一实测，失败项按"官方限制 / 出口链路 / 项目缺陷"三分法归因，并与上游项目代码对比确认是否同样问题。凡是要"评估可用性/真实可用测试/按 newapi 渠道映射测试/可用性审计/测试凭据是否真的能用"时使用，即使用户只说了"测一下可用性"。
---

# 可用性审计配方

核心思想：**可用性以实测为准，不以目录/配置为准**。官方 feed 是推荐列表不是硬白名单（在 feed 里的模型可能已 404，不在 feed 里的可能照样出字）；面板"启用"不等于上游真的接受。每个结论都要有铁证（官方 API 原始响应、上游代码行、日志行）。

## 0. 信息源与前置状态

| 信息 | 位置 | 取用方式 |
|---|---|---|
| newapi 渠道映射 | `D:/DockerApp/newapi/data/one-api.db`（newapi 容器挂载） | **先复制到临时目录再读**（运行中容器的库不能直接 open）；`channels` 表：`id/type/name/base_url/status/test_model/models/model_mapping/key` |
| afree 凭据盘点 | `D:/DockerApp/afree/*.json` + `workbuddy/auths/` + `workbuddy/state.json` | 直接读；cline/zen/trae 是 accounts，amd/oprt/tkhb 是 keys+keyBindings，other 是 platforms[]，wb 看 state.json 的 disabled |
| afree 网关 | `http://127.0.0.1:3457` | `/health`、`/v1/models`、`/v1/chat/completions`；API key `8008208820`（compose）；面板 `POST /admin/api/login {password:pass1234}`，**token 在响应 `data.token` 层** |
| 容器日志 | `docker logs afree` | 失败根因的第一现场（EOF/冷却/blacklist/gate reject） |
| 上游代码 | 本地 git 对象（merge 历史里有上游哈希） | `git show <上游哈希>:<文件>` 对比 |

前置检查：`docker ps` 确认 afree/newapi 容器 Up；`curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:3457/health` = 200。临时产物放 `.tmp/audit-<date>/`（gitignore 内），**含凭据的临时文件（token 等）用完即删**。

## 1. 提取测试清单（newapi 渠道映射 → 唯一目标集）

newapi 的渠道全部指向 afree 网关（`host.docker.internal:3457`），用 `model_mapping` 把 `afree/*` 别名翻译成平台前缀模型。测试清单 = **7 个渠道 model_mapping 汇总后的唯一目标模型集**（33 个左右），不是 7×17 的笛卡尔积——相同目标去重，每目标至少实测一次。

```python
# 从 sqlite 读 channels.model_mapping，json 解析，汇总 values() 去重
# 目标形态即平台前缀：cline-free/ zen: wbcn: wbgb: trae: amd: oprt: tkhb: other-*:
```

同时盘点各平台凭据启用状态（见上表）——按 newapi 口径测试的是模型维度（凭据由池轮换），"所有凭据逐一探测"超出映射范围，报告里说明口径即可。

## 2. 真实请求测试（probe）

对每个目标发一个真实小请求，串行（同平台凭据池共享，并发互相干扰）：

```python
POST http://127.0.0.1:3457/v1/chat/completions
Authorization: Bearer 8008208820
{"model":"<目标>","messages":[{"role":"user","content":"Reply with exactly: OK"}],"max_tokens":32}
# 超时 150s（有的平台慢）；结果落 JSON；控制台逐行 PASS/FAIL
```

判定口径：
- **HTTP 200 = 通过**。空 reply 不是失败——reasoning 模型把 32 个 token 全花在思考上是正常形态。
- 4xx/5xx 记录响应体原文（根因证据）。
- 另抽 2~3 个 `afree/*` 别名经 newapi（`http://127.0.0.1:3123`）测全链路（newapi→afree）。

## 3. 失败根因三分法（每个 FAIL 必须归因到三者之一，带铁证）

### 3a. 官方限制 vs 目录残留（最常见）

上游把模型移入付费档/下线后，本地目录可能还在展示。判定方法：**拿凭据 token 直打上游官方 API，看该模型现在在哪个档**。

```bash
# Cline 例：recommended-models 需要账号 token（Bearer），路径带 /api/v1
curl -s -m 25 --socks5-hostname 127.0.0.1:10086 \
  -H "Authorization: Bearer $(cat /tmp/ctok.txt)" -H "X-Cline-Client-Version: 1.0.0" \
  "https://api.cline.bot/api/v1/ai/cline/recommended-models"
# free 组少了谁 = 官方移走；响应 usage 里 "cost":0（免费）vs >0（计费）是铁证
```

要点：
- **cost 字段判免费**：同账号对官方 free 组模型请求 `cost` 恒为 0；若"能出字但 cost>0"= 在计费（免费账号烧额度），比 404 更隐蔽。
- 官方 feed 是推荐列表不是硬白名单：`z-ai/glm`、`poolside/laguna` 曾不在 feed 却实测可用；判断死活一律实测。
- Trae 例：1005（`data.plan` 权益不足）/4001 是模型级永久错误，换传输/换号无用——官方权益限制。
- 出口链路失败（日志 `EOF`/`exit-cooldown`/`not cooling`）会**掩盖真实错误**：等冷却过期（10 分钟）后复测，404 才是真相。日志先说 `not cooling the exit` 紧跟 `cooled 10m` 不是矛盾——单次宽容 + `recordProxyOutcome` 累计阈值两层设计。

### 3b. 项目缺陷（机制存在但没接全）

典型模式：网关有通用机制但只有部分平台接线。审计时对照检查：
- 模型黑名单（`model_blacklist.go`）：`platformOfModel` 认哪些前缀？`IsModelPermissionDenied` 判据覆盖哪些上游文案？`finish()` 各平台分支？——2026-10-31 审计发现只有 trae 接了自动拉黑，cline 的 404 永不触发，已补。
- 计费哨兵：cline 路径 `accountUsageFn` 挂 `usage.cost>0` 自动拉黑（其他平台无此信号）。
- 请求路径服务闸（`servedModelGate`）：请求模型与 `/v1/models` 精确同口径，已拉黑/目录外一律 404 不透传上游。
- 去 seed 兜底：cline 免费目录唯一来源 = 官方 feed free 组（过期快照曾把计费模型当免费提供）。

### 3c. 上游对比（确认上游是否同样问题）

`git log --oneline --grep="sync upstream" --merges -1` 找最近 merge 的第二父（上游基线），然后：

```bash
git show <上游哈希>:internal/app/models.go | grep -n "muse\|seed\|prune"
# 上游有同样的 seed/保护且无黑名单机制 → 同样问题，非移植缺陷
```

上游仓库哈希速查：fork 线上游对象在本地 git（merge 历史）；vendor 线基线在 `scripts/workbuddy-vendor-baseline.json`；Trae 基线在 `scripts/trae-helper-baseline.json`。

## 4. 修复与验证

发现项目缺陷就修（用户默认授权），修完：
1. `go build ./... && go vet ./... && gofmt -w <改动文件>`；
2. 补回归测试（模板见 `model_blacklist_test.go` 的 `TestClineModelNotFoundAutoBan`/`TestServedModelGate`/`TestClineAutoBanPaidUsage`——httptest 假 handler 穿中间件断言拉黑）；
3. 全量 `go test ./... -count=1`（对照既有失败基线）+ `-race ./internal/app/`；
4. 按 PROJECT.md §6 构建测试镜像部署本机容器，**端到端复测**：触发请求 → 日志确认 → `/v1/models` 摘除 → 正常模型无误伤；
5. 面板改动浏览器实测（zh 模式截图）。

## 5. 报告结构（用户要的最终产物）

1. 测试口径（来源映射、凭据盘点数、判定标准）；
2. 结果矩阵（分平台 PASS/FAIL + 耗时）；
3. 失败根因表（每项：现象 → 铁证 → 三分法归属 → 处置）；
4. 已修复的项目缺陷（改动文件 + 验证方式）；
5. newapi 渠道映射修复建议（死链别名 → 已实测可用的替代；标注哪些是"官方下线无等价"只能删）；
6. 遗留与口径说明（凭据粒度、出口冷却事件、上游对比结论）。

详细命令模板与陷阱清单见 [references/audit-playbook.md](references/audit-playbook.md)。

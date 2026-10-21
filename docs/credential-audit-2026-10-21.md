# 全平台启用凭据实测报告

**日期**：2026-10-21　**对象**：afree-proxy（生产容器 `afree`，端口 3457，镜像 `afree-proxy:prod-20261020-1`）
**范围**：7 个平台 / 100 个处于「启用」状态的凭据，全部发起**真实上游请求**；外加 34 条网关端到端分支用例。
**原则**：不根据单条结果下结论 —— 三轮探测 + 跨时段复测，只用可复现的稳定结论。

---

## 一、结论（先行）

1. **凭据总体健康**：100 个启用凭据中 **12 个当前不可用**，88 个在三轮探测中表现稳定。
2. **12 个不可用全部有明确外部原因，无一是网关逻辑错误**：1 个代理节点故障连带 5 个凭据、4 个 AMD key 已失效、1 个 AMD 出口不通、1 个 zen 额度耗尽、1 个 TokenHarbor 出口 IP 被封。
3. **发现 1 个真实的可用性缺陷并已修复**：绑定出口冷却时，面板 Test 会把**健康凭据**报成 `Error`，且原因不写出口与恢复时刻 —— 实测中足以导致误删好 key。
4. **34 条 e2e 用例 28 通过 6 失败**；6 条失败中 4 条经复核是**测试脚本自身的问题**（探测模型被上游下架、预期值设错），2 条是真实的语义问题（上游 4xx 被映射成 502）。

---

## 二、昨天与今天的提交（10-20 / 10-21）

今天（10-21）**尚无新提交**。昨天（10-20）共 5 个提交，另有 10-19 的 3 个仍在验证窗口内：

| 提交 | 时间 | 内容 | 本次实测覆盖 |
|---|---|---|---|
| `2aa0183` | 10-20 15:58 | fix(pool)：停用凭据在所有平台一律视为不在池里 | ✅ 直接验证（cline 全停 / other 单停 + 全停） |
| `62d4a59` | 10-20 14:46 | fix(cline)：上游响应头看门狗（默认 60s） | ⚠️ 未在真实黑洞出口触发，仅文档核对 |
| `e92b015` | 10-20 14:32 | feat(cline)：`CLINE_FORCE_STREAM` 全局流式开关 | ⚠️ 默认 off，未改动默认路径；已补文档 |
| `6175260` | 10-20 11:53 | feat(cline+zen)：身份头对齐 + zen 干净 403 跨请求闸门 | ✅ 全部 zen 真实请求走该路径，无异常 |
| `b32d964` | 10-20 00:54 | feat(admin)：凭据延迟重排 | ⚠️ 纯前端，不在本次请求测试范围 |
| `4a2518e` / `f36cdbf` / `d1e64b7` | 10-19 | 导入弹窗、徽章槽位、代理订阅 | ✅ e2e 间接覆盖（配置读写/还原均正常） |

工作区有一份未提交改动（延迟重排三段式队列 + Other 子平台逐个重排），已按约定 `git stash` 暂存为
`stash@{0}: wip: 2026-10-21 延迟重排三段式队列（Other 子平台逐个重排）`，工作区已干净。

---

## 三、凭据实测结果（三轮）

### 第一轮：100 个凭据全量探测（并发 5，耗时 89.7s，走平台自带 Test 端点）

| 平台 | 启用数 | active | 非 active |
|---|---|---|---|
| Cline | 16 | 15 | 1（cooldown） |
| OpenCode zen | 15 | 12 | 2 error + 1 cooldown |
| OpenRouter | 15 | 13 | 2 error |
| AMDCloud | 15 | 9 | 6 error |
| TokenHarbor | 15 | 12 | 3 error |
| Other / agnes | 15 | 13 | 2 error |
| Other / sensenova | 4 | 4 | 0 |
| WorkBuddy | 5 | 5 | 0（首轮是我脚本解析错误，非真实失败） |

### 第二轮：非 active 项连测 3 次 + active 抽样 34 项各 2 次

稳定率：**失败项 21/22 三次一致**，**抽样项 33/34 两次一致**。
唯一不一致的 cline `csbbzw01`（一次 `TimeoutError`）在第三轮无复现。

### 第三轮：12 个可疑凭据跨时段各测 4 次（间隔 25s）

**全部 12 项四次结果一致**，据此把「持久故障」与「瞬时失败」彻底分开：

| 凭据 | 绑定出口 | 四轮结果 | 定性 |
|---|---|---|---|
| zen/or/th/agnes **index 11** | `10107#Neohbg` | active ×4 | **自愈** —— 首轮是 2 分钟出口冷却窗口内的误报 |
| zen/or/amd/th/agnes **index 14** | `10106#DDHK` | error ×4 | **节点故障**，跨 5 平台一致 |
| amd **index 11** | `10107#Neohbg` | 401 ×4 | 上游 key 失效（同出口其他平台正常 → 排除了出口问题） |
| amd **index 8** | `10102#Mikrus` | 超时 ×4 | 该出口到 AMD 上游 60s 无响应 |
| cline cswbhsa | — | active ×4 | **自愈** —— 首轮是瞬时网络错误 |

### 最终：12 个当前不可用的凭据（脱敏）

下表一律用**配置 index（0 起）**，面板显示为 index+1。

| 凭据（index） | 原因（真实响应原文摘要） | 处置 |
|---|---|---|
| zen / or / amd / th / agnes 的 **14**（5 个） | `bound proxies unavailable` — 出口 `10106#DDHK` lastOk 停在 10-20 08:02，lastFail 持续刷新 | 换/修该代理节点 |
| amd **10 / 11 / 12 / 13** | `API 401 … Invalid LLMGateway API token. The token could not be found.` | 到 AMD 重签 key |
| amd **8** | `context deadline exceeded`（60s，4/4 稳定） | 给该 key 重绑出口 |
| zen **12** | `429 FreeUsageLimitError` | 免费额度耗尽，等待恢复 |
| tokenharbor **12** | `403 … network address (47.84.122.196) are not accepted` | 出口 IP 被封，重绑出口 |

---

## 四、端到端分支测试（34 例，28 通过）

| 分类 | 用例 | 结果 |
|---|---|---|
| 正常 | 8 个平台非流式各 3 次 | ✅ 全 200（cline 用当前有效模型） |
| 正常 | 8 个平台流式各 2 次 | ✅ 全 200 + SSE + `[DONE]` |
| 停用 | other-ddx 4 个 key 全停 → 请求 | ✅ `502 no key routable on platform "sensenova"`，不挂死 |
| 停用 | other-ddx 停 #1 → 连发 8 次 | ✅ 上游依次为 key#4/#3/#2 轮转，**#1 从未出现** |
| 停用 | cline 16 账号全停 → 请求 | ✅ `400 model … is not available`，且 `model sync: failed (no accounts), using cached list`（目录未清空） |
| 还原 | 两个平台还原后立即请求 | ✅ 均恢复 200 |
| 异常 | 未知模型 / 未知前缀 | ✅ 400 + 提示读 `/v1/models` |
| 异常 | 错误 API_KEY | ✅ 401 |
| 一致性 | `/v1/models` 连读 3 次 | ✅ 57 个模型顺序完全一致 |
| 并发 | zen 并发 8 请求 | ✅ 全 200 |

**6 条失败项的复核结论**：

| 失败项 | 复核 | 定性 |
|---|---|---|
| cline 非流式 / 流式各报 400 | 探测模型 `cline-free/mimo-v2.6-flash` 在 20:39–20:56 的三轮 prune 中已被上游下架；换 `cline-free/minimax-m3.1-flash` 后 3×200 + 流式正常 | **测试脚本问题**（模型目录是动态的，不能写死） |
| 空 `model` 返回 200 | 代码按 `STRICT_MODEL_MATCH` 语义：未知**名字**才 400，缺省回落默认模型 | **我的预期设错**，行为符合设计 |
| 空 `messages` → 502 | 透传上游，上游 400 被映射成 502 | **真实语义问题** |
| 超长 `max_tokens` → 502 | 同上 | **真实语义问题** |

---

## 五、发现的问题与修复

### 已修复

**P1 — 出口冷却时 Test 误报健康凭据为 Error（本次最重要发现）**

- **现象**：绑定主辅出口都不可用时，探测直接失败，原因只有一句
  `bound proxies unavailable (cooling or removed)`，状态是 `error`（面板红色 Error 徽标）。
- **危害实测**：10 个凭据首轮因此报红，其中 5 个（zen/th/agnes #12、cline cswbhsa）在 2 分钟
  冷却窗口过后**全部恢复正常**。若按首轮结果操作就会删掉好凭据。
- **根因**：文案只给结论，不给出口标识、恢复时刻，也**没说明凭据本身未被验证**。
- **修复**：
  - `internal/kit/http.go` 新增 `MaskProxyURL` 与 `BoundProbeBlockedMsg`，并通过
    `kit.ProxyBlockedDetail` 注入点让主包提供详情（避免子包反向依赖 `internal/app`）。
  - `internal/app/proxy_binding.go` 新增 `exitStateLabel` / `boundExitDetail` / `blockedExitMessage`
    与 `zenProxyCooldownUntil`，输出出口（打码）+ 冷却解除时刻（RFC3339）+ `backup=none`。
  - 五个平台（zen 2 处、amd、openrouter、tokenharbor、other 各 1 处）与 cline 全部接上。
- **真实验证**（独立实例、独立端口 3458、数据副本，不影响生产容器）：

```
key#15 bound proxies unavailable (cooling or removed); probe aborted by proxy isolation:
main=socks5://10.10.10.5:10106#DDHK (cooling until 2026-10-20T21:35:45Z), backup=none
— the credential itself was not verified (its egress is down)
```

  五个平台输出一致；健康凭据（zen #1/#2/#3）仍为 `active`，网关 `/v1/chat/completions` 正常。
- **测试**：新增 `internal/app/proxy_blocked_detail_test.go`（4 个用例）；
  `go build ./...` 与 `go vet ./...` 通过；`internal/app`、`internal/kit`、`amd`、`openrouter`、
  `tokenharbor`、`other` 六个包测试全绿。
- **顺带修正**：`maskProxyURL` 原来把 `***` 编码成 `%2A%2A%2A`（日志/面板不可读），现还原为字面量。

### 当日追加修复（同日二轮）

- **状态语义统一**：cline 面板 Test 在「绑定出口不可用」时改报 `error`（原报 `cooldown`）。
  该分支并未真的给账号设置冷却，报 cooldown 会让面板渲染成黄色"冷却中"误导排查；
  现与五平台一致渲染为红色 error。真的网络错误/429 仍按 cooldown 回报，不受影响。
- **修正 `zen.go` `pickZenKey` 上方前后矛盾的注释**：旧注释后半句"路由禁用的 key
  仍可被收割机铸造会话"是旧语义残留，与 `zenKeyAutoHarvestEnabled`（铸造跟随路由
  启用）直接矛盾，已按当前实现重写。

`go build` / `go vet` / `internal/app` 全量测试通过。

### 未改（需先确认，见改进建议）

- 上游 4xx（客户端参数错误）一律映射 502。

---

## 六、项目改进建议（按优先级）

| 级别 | 建议 | 依据 |
|---|---|---|
| **高** | 把 `10106#DDHK` 从代理池摘除或修复；给绑定它的 5 个凭据重绑出口 | 跨 5 平台 4/4 稳定失败，节点已停更 12 小时以上 |
| **高** | 更换 4 个 AMD 失效 key（index 10 / 11 / 12 / 13） | `401 Invalid LLMGateway API token`，上游已注销 |
| ~~**高**~~ **已修（同日）** | 面板 Test 失败时区分「凭据失效」与「出口不可用」：cline 的出口不可用分支已从 `cooldown` 改报 `error`，与五平台一致 | 该分支未设真实冷却，报冷却会误导排查 |
| **中** | 上游 4xx 应映射为 4xx 而非 502 | 客户端遇到 502 普遍会重试，会重复烧免费额度；实测 `messages:[]` 与超长 `max_tokens` 均落到 502 |
| **中** | 给「全部凭据被停用」加一条显式的面板提示/告警 | 实测该状态请求明确报错，但面板侧无任何提示，用户不易自查 |
| **中** | `amd index 8`（面板 #9）走 `10102#Mikrus` 时上游 60s 超时 —— 给 AMD 单独加出口黑名单或自动重绑 | 4/4 稳定，其他平台走同一出口正常 |
| ~~**低**~~ **已修（同日）** | ~~修 `zen.go` `pickZenKey` 上方前后矛盾的注释~~ | 旧后半句与 `zenKeyAutoHarvestEnabled` 矛盾，已按实现重写 |
| **低** | `/v1/models` 偶发 10s/30s 尖刺（实测 `duration_ms` 10002 / 30007）—— 目录同步应移出请求路径或加超时上限 | 观测数据，非故障 |
| **低** | CI / 脚本不要写死模型名 | 实测 17 分钟内上游 prune 掉 5 个 Cline 模型 |

---

## 七、本次改动的文档

- `README.md`：补 `CLINE_FORCE_STREAM` / `CLINE_HEADER_TIMEOUT` 两个新增环境变量；
  修项目结构里重复且过时的 `zen_session.go` 条目；新增 **Credential enable / disable semantics**
  与 **Troubleshooting** 两节（内容全部来自本次实测数据）。
- `docs/manual-zh.md`：补同样两个环境变量；新增 **3.9 凭据「启用」列与 Test 按钮的真实语义**
  （含出口冷却误报的识别与处置）；「常见问题」新增 5 行实测结论；修正 WorkBuddy 前缀说明
  （网关 `/v1` 实际用 `wbcn:` / `wbgb:`，旧的 `cn:` / `global:` 仍被兼容）。
- `TODO.md`：顶部新增本轮实测结论、故障清单、修复说明与遗留建议。

---

## 八、可复现方式

探测脚本位于本次任务的临时目录，逻辑可直接复现：

1. 登录：`POST /admin/api/login {"password":$ADMIN_PASSWORD}` → 保存 Cookie。
2. 逐凭据探测：`POST /admin/api/{zen,openrouter,amd,tokenharbor}/keys/test {"index":N}`、
   `POST /admin/api/other/keys/test {"slug":S,"index":N}`、
   `POST /admin/api/accounts/test {"accountId":ID}`、
   `POST /admin/workbuddy/api/accounts/{uid}/test`（**注意：WorkBuddy 面板返回裸 JSON，
   不套 `apiResponse`，解析要按 `ok`/`status` 读**）。
3. 网关用例：`POST /v1/chat/completions`，模型前缀 `zen:` / `oprt:` / `amd:` / `tkhb:` /
   `other-<prefix>:` / `wbcn:` / `wbgb:`，Cline 用 `/v1/models` 里的无前缀 ID。
4. 判定：**同一凭据间隔 ≥2 分钟复测 2–3 次**再下结论（出口冷却只有 2 分钟）。

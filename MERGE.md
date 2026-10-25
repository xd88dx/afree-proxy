# 上游同步差距报告

> 生成日期：2026-10-25 ｜ 对照基准：本项目 main（afree-proxy，Trae 平台已于 2026-10-24/25 完成 v1 移植并多轮补齐）
> 数据来源：三仓库实时克隆/抓取（trae: %TEMP%/Trae2api-cn-ref、wb: workbuddy2api-panel-ref@depth30、oc: opencode2api-ref@depth30）

## 总览

| 平台 | 上游仓库 | 本项目基线 | 上游最新 | 提交落后 | 同步方式 | 差距等级 |
|---|---|---|---|---|---|---|
| TraeCode | autumnsentiment/Trae2api-cn | `bb7b659`（2026-10-24，即最新） | `bb7b659` | **0** | Go 原生重实现（无 vendor，参照只读） | 无版本差距；功能差距见 §1 |
| WorkBuddy | misakano7545/workbuddy2api-panel | `b51ce6d`（2026-10-23 同步，dev） | `9a00205`（2026-10-25） | **约 20 个有效提交（PR #71~#84，64 文件 +5718 行）** | vendor 三方合并（`scripts/sync_workbuddy.py` + 基线 JSON） | **中——建议尽快同步** |
| OpenCode | jasonxu114514/opencode2api | fork 继承（zen.go 随仓库初始带入，未做过定点同步） | `b8b1454`（2026-10-25） | 无法按提交计（非 vendor 关系）；**free-tier 伪装路线整体落后** | 原生实现（internal/app/zen.go + admin_zen.go），无同步脚本 | **高——上游免费层反封禁演进快** |

---

## 1. TraeCode（autumnsentiment/Trae2api-cn）

**版本差距：零。** 上游最新提交（2026-10-24 `bb7b659` "align account entitlement concurrency and model capabilities"）正是本项目移植基线，fetch 确认无新提交。

**功能差距（移植时已裁剪，非遗漏）：**

| 上游功能 | 本项目状态 |
|---|---|
| TraeCode raw 主链路（llm_raw_chat v2/v1 + 主机回退） | ✅ 已移植（internal/trae/client.go） |
| 账号池/刷新（ExchangeToken 单飞）/双开关/额度/签到 | ✅ 已移植并按本项目规范重做（面板化、隔离绑定、默认禁用） |
| 模型目录（Remote + Solo get_detail_param 双源合并） | ✅ 已移植（2026-10-25，27 模型实测） |
| 网页授权（本机 helper 8765 架构） | ✅ 已移植（helper 脚本内嵌 + 下载/探测/relay 告警） |
| /v1/responses、/v1/messages 端点 | ✅ 本项目已有（且已扩展到全前缀平台） |
| **remote 会话链路**（chat_sessions 三步流，solo_agent_remote 档、1M 上下文 agent 档） | ❌ 未移植 |
| **TraeWork Work 模式计费 / get_session_usage 对账** | ❌ 未移植 |
| **continuation 续写钉号**（前缀哈希会话绑定 + 账号租约） | ❌ 未移植 |
| **文本工具协议**（`<opencode_tool_call>` 注入式工具调用） | ❌ 未移植（本项目只透传原生 tools） |
| 积分明细独立页 + 每日自动对账 UI | ❌ 简化为额度列 + 后台 30min 刷新 + Test 即时查 |
| 桌面客户端解密（storage.json AES）/ Trae CLI 子进程源 | ⛔ 不可移植（依赖运行机本机 Trae 客户端，docker 部署形态不存在） |

**本地已超越上游的部分**（反向同步无意义）：代理隔离（主/备出口绑定、宁拒不直连）、全平台统一面板/i18n、跨端点路由、备份导出导入、代理池绑定体系。

**结论：无需动作。** 上游一动，按 trae-platform-recipe 记忆里的协议事实对照增量移植即可。

---

## 2. WorkBuddy（misakano7545/workbuddy2api-panel）

**版本差距：落后 2026-10-23 → 10-25 约 20 个有效提交**（vendor 基线 `b51ce6d` → `9a00205`，64 文件 +5718/-230）。

**上游新提交按重要性归类：**

### 🔴 高价值（影响账号存活/请求成功率，建议优先）
- **PR #82 / #72（299c51c）/ #80（1e70717）**：上游错误码语义修正族——全池模型配额锁（6004/6000）直接回 429，不再让客户端按 Retry-After 空等；6000 按**模型级**限流处理（msg 含「切换其他模型」）；6020「queue.waiting」与 6021 同判**渠道级排队、不再罚号**（罚号=错误冷却账号，直接缩小可用池）
- **PR #73（67b2a69）**：模型级冷却完整对齐上游**重置墙钟**，不再被 soft_rate_max 截断——影响冷却解除时机
- **issue #83（f6bd053）**：refresh token 失效不再判账号死——避免整号被误杀
- **f0c427d**：登录对齐官方 CLI **2.174.0** 的域表与登录码语义（登录链路时效性）

### 🟡 中价值（运营/面板体验）
- **PR #84**：周期任务人味化——余额刷新/清扫加随机抖动、夜猫子 Top-N 参与上限（降低定时任务指纹特征）
- **PR #71**：封禁账号只读探活 + 余额趋势按可用账号数 + 401 求证收敛
- **PR #77**：面板版本检测 + 一键更新 + 定时自动更新（⚠️ vendor 场景需评估是否裁剪——本项目有自己的发布链路）
- **PR #78**：裸模型名落错域时 hint 指向带前缀写法
- **PR #76**：模型未调工具的正常收尾降为 INFO，不进错误汇总（日志降噪）

### 🟢 低价值（纯 UI）
- PR #75 任务中心默认收起已完成行；PR #74 操作列固定槽位对齐

**同步方式**：走既有 `scripts/sync_workbuddy.py`（基线 `b51ce6d` → 新基线三方合并）。注意 vendor 内的 afree 本地补丁（双开关落盘键、代理绑定、reqlog 等，见 workbuddy-vendor-sync-flow 记忆），合并后跑全量测试 + 浏览器冒烟。

**✅ 已于 2026-10-25 完成同步**（`b51ce6d` → `9a00205`，57 文件三方合并 + loginsite/updater 两个新包纳入 vendor 白名单，4 处冲突解决：双开关字段与上游 DisabledAt 并存、import 并存、测试函数并存、login.go 采用上游 loginsite/site 新架构——官方 CLI 2.174.0 域表语义随之生效；本地护栏 `SetPoolEnabled(false)` 保留验证）。新基线已 seal。注意：上游一键更新（updater 包）已随包进入编译，但其面板入口是否暴露由 vendor panel 配置决定，后续按需评估。

---

## 3. OpenCode（jasonxu114514/opencode2api）

> **2026-10-25 调研后修订**：深入对照后，原"整体缺失"判断过于粗。本项目 zen 已有相当对齐（fork 后多轮实测演化），逐项结论如下。

### 已确认并对齐的项
| 项 | 结论 |
|---|---|
| **FreeTier 工具集闸门** | 本项目早已有自研闸门（`zenGateTools`，缺失工具集实测 403）——但注入的是 **1.x 名单（bash/task/todowrite 共 11 个）**，而我们走 /inference/v1（2.x 端点）。**已修（2026-10-25）**：`zen_gate_tools.json` 重建为上游官方 2.x 13 工具集（edit/execute/glob/grep/question/read/shell/skill/subagent/webfetch/websearch/write/patch，byte-for-byte 官方定义；responses 形态按 @ai-sdk 行为程序化剥 min/max 约束），chat 与 responses 两条路径同时生效，fallback 简表同步。 |
| **ses_ 会话时间戳窗口**（上游 9533101） | **免修**——上游修的是他们 hash 派生时间戳落在 1970 的 bug；本项目 `kit.MintZenSessionID` 用 `time.Now().UnixMilli()` 铸造，天然在窗口内。 |
| **reasoning disable 透传**（上游 394acb5） | **免修**——本项目只在客户端显式给 effort 时注入 reasoning，无"配置默认强度"可覆盖显式关闭。 |
| 2.x 端点/路由/头 | 已对齐（/inference/v1、x-opencode-* 头、CLI UA、msg_/ses_ 格式门、会话粘性——fork 后多轮实测）。 |

### 明确不移植的项（工程决策）
- **官方系统提示注入 + clint_ 工具改名 + 官方工具集调用透传**（上游 97cdb29/fd4c1c6）：上游目标客户端是 opencode CLI 自己（认识官方工具、官方提示）；本项目下游是普通 OpenAI 客户端——注入官方提示会让模型主动调用 shell/read 等客户端不认识的工具，直接破坏 agent 循环（上游响应侧仅做名字映射回，不过滤官方工具调用）。形态伪装与客户端兼容冲突，不移植。
- **用户态 WARP 出口**（4438acb/b8b1454）：与本项目代理池/隔离体系功能重叠；有代理池的用户无收益。
- **匿名出口熔断/上限**（8cc5e96 等）：本项目代理池已有冷却/健康统计语义，天然覆盖大半；免费 public key 场景保留现状。

**结论：差距已收窄到一处并完成修复（工具集 1.x→2.x）。** 若未来上游对无官方提示的请求开始拒绝（403 回归），再评估"伪装模式开关"（默认关，开启后客户端自担官方工具调用语义）。

---
## 附录：各仓库快照

```
Trae2api-cn         bb7b659 2026-10-24 fix: align account entitlement concurrency and model capabilities（= 本项目基线，零落后）
workbuddy2api-panel 9a00205 2026-10-25 merge PR #84 人味化（基线 b51ce6d 2026-10-23，落后 ~20 提交 / 64 文件 / +5718 行）
opencode2api        b8b1454 2026-10-25 fix(warp): rotate exits on a live pool（本项目无基线锚点，free-tier 硬化整体缺失）
```

# 前端板块详解（admin_html.go 单文件内嵌面板）

面板 = Go 反引号字符串 `const adminHTML`，内含 CSS/HTML/JS。改完必须重建镜像才能浏览器验证。

## 1. i18n 机制（每次改文案都涉及）

- 字典 `I18N_ZH`（const I18N_ZH = {...}）：键 = 英文源文，值 = 中文。`T(s)` 直译；`i18nSubtree` 启动时遍历静态文本节点（trim 后整节点匹配）；`i18nMO` MutationObserver 接住动态插入的节点与 title/placeholder 属性变更。
- **规则**：
  - 静态 HTML 文本一律英文源文；新增文案同步加词条。
  - JS 拼接的动态串（toast/textContent/innerHTML 片段）每个文本片段单独包 `T()`，键与调用逐字符一致。
  - 字典键结尾不能带空格——HTML 需要可见尾随空格时用 `&#x20;`（参照"任务中心&#x20;"）。
  - 含 `<code>`/`<b>` 的 hint 被拆成多个文本节点，需按段建词条。
  - `egComboHTML(kind,id,slot,value,title,disabled)` 的 title 参数**原始透传**，调用方必须传 `T('...')`。
  - 静态文案与 JS 动态串同文案时，共用同一词条（先查重再加）。
- 自查脚本模式：提取字典键 → 与"静态文本节点/title/placeholder/JS 字面量（剔除字典块自身）"三路精确匹配，不命中的即死键；T() 字面量参数反向核对必须全部命中字典。

## 2. 导航菜单 + 标签页

- 导航项：`<div class="nav-item" data-tab="xxx"><span class="nav-ico"><svg…/></svg></span> 英文名</div>`。现状十一个：dashboard/accounts/opencode/openrouter/radeoncloud/tokenharbor/workbuddy/proxypool/settings/combos/logs（注意：key 池 tab 的 data-tab 用 `radeoncloud` 而非 `amd`）。svg 用 24×24 简笔几何图形，风格与现有一致。
- 页面体：`<div id="tab-xxx" class="tab-panel" style="display:none">`；首个面板 dashboard 不带 display:none。
- 切换：nav 点击委托 `switchTab`（惰性加载：首次进入时 loadXxx——**新平台的 load 调用要加进 switchTab 的 if 链**，如 `if (name === 'openrouter') loadOrConfig()`）；`restoreTab` 从 location.hash 恢复（`_('tab-'+h)` 元素存在即白名单，非法回落 dashboard）。**hash 恢复必须在所有 const 声明之后执行**（TDZ 坑：`WB_API_PREFIX` 这类平台 const 若未初始化会报错）。
- 仪表盘六卡：`.dash-grid` + `.dash-card`（可点击跳转平台页，id 形如 `dashCline/dashOc/dashWb/dashOr/dashAmd/dashTh`）+ 请求统计卡（`reqCline/reqZen/reqWb/reqOr/reqAmd/reqTh`，数据来自 `/admin/api/request-stats`——后端 logs.go 的 per-route 计数要为新平台加分类桶）。`loadStats` 10s 轮询仅仪表盘可见时执行。

## 3. 账号列表 + 暂存编辑体系

- 表结构：手写 `<thead>`（列含 title 说明）+ `<tbody id="xxxBody">`；行由 `loadXxx()` 以 innerHTML 模板渲染，**所有插值过 `esc()`**；uid 展示 `id.slice(0,10)+'…'`，完整值进 title。
- 暂存编辑三件套（新列/新交互必须接线）：
  1. 快照变量：`accSnapshot` / `keySnapshot` / `wbSnapshot` / `orKeySnapshot` / `amdKeySnapshot` / `thKeySnapshot`（key 池平台另配 `<x>CfgCache` 缓存上次 GET 的 config，save 时回填不在表单里的字段）——loadXxx 时记录已保存态（代理绑定主/辅 + 启用布尔）。
  2. 脏标记：`markRowDirty(row, kind)` 加 `.tr-dirty` 黄底 + 保存按钮 `.dirty-hint`；`clearDirtyMarks(tbodyId, btnId)` 复位。
  3. 保存：`saveAccountEdits` / `saveKeyEdits` / `saveWorkbuddyEdits`（key 池族复用 saveKeyEdits 模式，逐平台一份）——逐行与快照 diff，只提交有变化的行，逐行 POST，汇总一条 toast（失败计数 + `T('Saved ')` 等词条），收尾 loadXxx + 清脏标记。
- 启用（Pool/路由）列放表格**最后一列**（横向滚动区内；自动化测试对离屏元素要用 evaluate dispatchEvent）。
- 空态统一词条：`'No accounts yet' → '暂无账号信息,请添加'`。

## 4. 操作按钮区域布局（用户明确的版式约定）

- **页脚三区**（表格下方一行 flex space-between）：
  - 左：添加入口（btn-primary）——弹窗模式（`accAddDialog`/`zenKeyAddDialog`/`orKeyAddDialog`/`amdKeyAddDialog`/`thKeyAddDialog`/`wbAddDialog` 样式族），单输入 `credPayload` 自动识别（sk_ 前缀 = API key，否则 refreshToken），可带可选邮箱；textarea 逐行批量（合并去重后全量提交，提交后新 key 走默认禁用）。**key 池平台的添加弹窗没有"描述"输入框**（2026-10-11 UI 约定：只有 key 列表 textarea）。
  - 中：批量操作组——一键启用/一键禁用（`stageAllAccountsPool` / `stageAllKeysRouting` / `stageAllWbPool`，只改 DOM 进暂存）、清空代理（暂存清绑定+全部置禁用，confirm 文案注明"点击保存后生效"）、批量测试（串行 POST `/test` 接口，按钮显示 i/n 进度，汇总 toast，分隔符 `T(', ')`）。
  - 右：复位（btn-warn 琥珀黄，= clearDirtyMarks + loadXxx）+ 保存（btn-primary）。
- **顶部操作行**（section-body 顶部 flex space-between）：左 = 重操作入口（如任务中心/全部刷新），右 = 直接执行类（全部签到/宠物巡检等）。
- 已知既有行为：420px 窄屏页脚右区会溢出（各平台同款），非新引入 bug。
- CSS 约定：`.btn/.btn-sm/.btn-primary/.btn-warn/.btn-danger`；弹窗头/体/脚 `wb-dialog-head/body/actions`。

## 5. 平台设置表单（wbConfigForm 模式）

- 结构：`<form id="wbConfigForm">` → `.wb-config-grid`（两列）→ `.wb-config-group`（h3 分组）→ 字段两种形态：
  - `.wb-field`：`<label for=…>` + input/select，input 挂 `data-wb-path="pool.max_in_flight"`（对应平台 config JSON 路径）+ `data-wb-type="number|duration|ints"` + placeholder 给默认值示例。
  - `.wb-switch`：checkbox + label（开关，如自动签到）。
- 加载/保存：`collectWorkbuddyConfig()` 按 data-path 收集 → `wbCall('config',…)` 提交 → 后端热应用 + 返回 restartRequired 数组 → toast 显示"N 个重启项"（词条 'WorkBuddy config saved (' / ' restart items)'）。
- afree 原生平台等价物：`/admin/api/config{,/update}`（Cline）与 `/admin/api/opencode/config{,/update}`（zen）——handler 里做 normalize + 只热应用非装配期字段；**装配期依赖（监听、HTTP client 超时、目录句柄、upstash、session ttl、日志归档）才进 restartRequired 清单**，其余一律热生效。

## 6. 网关设置页与自定义别名页里引入新平台

- **走池下拉**（proxypool 页"全局策略"组）：现状六个 select——`ppCline/ppZen/ppWb/ppOr/ppAmd/ppTh`，选项"代理/直连"，**缺省=代理**。后端字段 key 池型为 config 内 `UseProxies *bool`（nil→true，经 `/<x>/config/update` 提交）。新平台：加一个下拉 + config 字段（*bool，nil→true）+ 出站选择处消费。
- **默认模型下拉**（Gateway settings / 各平台配置区）：纯模型 ID 不带前缀后缀；`/admin/api/config/update {defaultModel}` 模式。
- **自定义别名（combos）**：`combos.go` 是唯一事实源——`validateCombo` 的 platform 白名单加 "x"；target 校验 switch 里加 X 分支（**target 必须在 X 自己的模型列表内，跨平台选型直接报错**）；前端 Custom aliases 页平台下拉加选项 + 模型目标随平台联动加载。别名字段：`{id, platform, target, useProxies}`。

## 7. key 池平台页面版式约定（2026-10-11 OpenRouter/AMD/TokenHarbor 实证）

- **模型列表用"设置页样式行"**：等宽字体 ID + 标签 + 上下文徽章的 div 行（`.model-tag free` + `wbContextTagHTML`），不是表格；Name 进 title 悬浮。
- **无描述框**：添加凭据弹窗与 key 表都没有"备注/描述"列——key 本身自解释。
- **信号量语义**（后端 MaxConcurrency）在平台设置区暴露为"最大并发"数字框。
- **页面全量 i18n**：接入验收 = 面板 EN 模式与 ZH 模式都无英文残留词条缺失。
- key 表列序参照 orKeysBody：`# / Key / Usage / Cooldown / Proxy binding / (操作) / Pool`，OpenCode 表另有 Session/Rotates 列（有会话态的平台才有）。

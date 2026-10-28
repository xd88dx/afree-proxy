---
name: add-platform
description: 在 afree-proxy 中新增一个上游平台的完整迁移配方——导航菜单、账号列表、操作按钮区、平台设置、网关模型与自定义别名、身份凭据启停、代理池绑定七大板块的逐步清单与既有锚点。凡是要"新增平台/接入新账号池/照 WorkBuddy 或 OpenCode 的样子加一个 provider/把某项目移植进来"时使用，即使用户只提了其中一两个板块。
---

# 新增平台迁移配方

以 Cline（原生账号池）、OpenCode（key 池）、WorkBuddy（vendor 移植）、OpenRouter/AMDCloud/TokenHarbor（key 池四件套克隆，2026-10-11 接入）为参照。目标平台记为 **X**。

## 0. 前置判断：三种落地形态

- **A. app 层单文件**（`internal/app/zen.go` 模式）：功能与单一上游强耦合、无独立包边界的小平台。
- **B. key 池四件套克隆**（OpenRouter/AMDCloud/TokenHarbor 模式，纯 API key 池平台的首选）：`internal/<x>/` 四件套 `client.go / config.go / models.go / pool.go`（config_test.go 随附）+ `internal/app/<x>.go` 桥接层（admin handlers + 分发接线）。四件套从 internal/openrouter 克隆改名，协议事实查 references/backend.md §9。
- **C. vendor 树**（`internal/workbuddy/`）：只有需要跟随上游演进的移植对象才用（`scripts/sync_workbuddy.py` 管理，手改会被 re-vendor 冲掉——除非用户明确要求，别走这条路）。
- 先从两个事实来源建立全景：`internal/app/admin.go`（管理路由全景）、`internal/app/admin_html.go`（单文件内嵌面板）。锚点一律按**符号名**检索，行号会漂移。
- 原项目（移植参照物）只读，永不写入。

## 1. 铁律（七个板块都受约束）

1. **新增身份默认禁用**（六平台统一口径）：X 的任何新增凭据路径（添加、导入、登录铸造）落盘前必须置为不参与路由；存量数据缺字段按**启用**兼容（读侧 `*bool == nil → true`）。key 池型平台直接用包内 `ReconcileRouting`（新 key 默认 false）。见 references/backend.md §2。
2. **代理隔离第一**：宁弃请求不暴露直连 IP，绝不回退直连；`PROXY_ISOLATION` 只接受 true/false；"清空代理" = 清绑定 + 全部置禁用，二者永远绑 together。
3. **暂存编辑模型**：账号表的一切编辑只改 DOM（`markRowDirty` 黄底），点平台"保存"按钮才逐行 diff 快照提交。新列、新交互必须接进这套体系，不做即时保存（OpenCode 的 Enabled 勾选是历史例外，别模仿）。
4. **i18n 机制**：静态文本写英文源文，中文显示靠 `I18N_ZH` 词条；JS 动态拼接的每个文本片段单独包 `T()`；`egComboHTML` 的 title 参数调用方负责 T()；字典键结尾不能带空格（HTML 里要保留尾随空格用 `&#x20;`）。key 池平台页面**全量 i18n 化**（OpenRouter/AMDCloud/TokenHarbor 接入时零英文残留是验收项）。细节见 references/frontend.md §1。
5. **验证铁律**：本机已有 Go（`C:\AppSelf\Go`，go1.27.2，PATH 已含；2026-10-15 起），宿主机直接 `go build/vet/test`；旧的 `afree-proxy:workbuddy-builder` 编译镜像已于 2026-10-27 清理删除，别再引用。改完前端必须 `docker rm -f` 重建镜像才能在浏览器看到新页面（`docker restart` 不更新文件系统）；会话回显不可信，以 git diff / 真实构建 / 浏览器截图为准。

## 2. 总流程

```
后端先行                     前端跟进                     验证
─────────────────────      ─────────────────────       ─────────────────────
① 数据模型+持久化           ⑤ 导航菜单+tab-panel         ⑨ docker build/vet/test(-race)
② 凭据启停字段与过滤        ⑥ 账号列表+暂存编辑          ⑩ 重建镜像+浏览器验证
③ 管理路由+handlers         ⑦ 按钮三区+平台设置          ⑪ i18n 扫描（死键/漏 T）
④ 网关分发+/v1/models       ⑧ 别名/走池下拉/仪表盘卡     ⑫ API 断言（curl 面板接口）
   +别名白名单                +i18n 词条
```

后端细节 → 读 `references/backend.md`；前端细节 → 读 `references/frontend.md`。两大 references 的§编号与下面对应。

## 3. 板块 → 落点速查

| 用户说的 | 落点 | references |
|---|---|---|
| 导航栏加菜单 | `admin_html.go` nav-item（data-tab）+ tab-panel + restoreTab 白名单 | frontend §2 |
| 页面账号列表 | tbody（`accountTableBody`/`ocKeysBody`/`wbAccountsBody`/`orKeysBody` 族参照）+ 快照/保存三件套 | frontend §3 |
| 操作按钮区域 | 页脚三区 flex space-between + 顶部操作行；批量操作串行进度 | frontend §4 |
| 平台相关设置 | key 池型：`handleOrConfigUpdate` patch 模式；vendor 型：`wbConfigForm` data-path 模式 | frontend §5 / backend §5 |
| 网关设置与自定义别名 | /v1 分发顺序、/v1/models 合并、combos 平台白名单与 target 校验、前缀查表两形态铁律（backend §3 加粗条） | backend §3-4 |
| 凭据开启禁用 | PoolEnabled/routingEnabled 六平台对照 + 全路径过滤 + 默认禁用 | backend §2 |
| 代理池相关 | egComboHTML 主/备双槽绑定、proxy/clear 语义、出口观测 | backend §6 |
| 克隆一个 key 池平台 | internal/openrouter 四件套改名 + app 桥接接线清单 | backend §9 |

## 4. 平台对照速查表（2026-10-29 八平台；OpenRouter/AMDCloud/TokenHarbor 三列共用 key 池克隆模式）

| | Cline | OpenCode(zen) | WorkBuddy | TraeWork | OpenRouter/AMDCloud/TokenHarbor | Other |
|---|---|---|---|---|---|---|
| 形态 | 原生账号池 | key 池（app 层） | vendor 树 | Go 重写协议栈（`internal/trae/` + `internal/app/trae.go`） | key 池（四件套克隆） | key 池四件套 + 多子平台（`internal/other/`，platforms 数组） |
| 凭据 | refreshToken/API key（Account） | API key 列表（zen keys） | auth 文件目录 | OAuth 账号（本机 8765 helper 网页授权/手动粘贴） | API key 列表（Keys） | 各子平台 key |
| 启停字段 | `Account.PoolEnabled *bool` | key `routingEnabled` | `auth.poolEnabled *bool` | `PoolEnabled *bool`（nil=参与）+ 账号级路由参与表 | `KeyRoutingEnabled map[string]bool` | `KeyRoutingEnabled map[string]bool`（按子平台） |
| 默认禁用点 | `pool.go addAccount` | `reconcileZenKeyRouting` | vendor `panel/login.go`+`import.go` | `internal/trae/config.go` `ReconcilePoolEnabled` | 各包 `ReconcileRouting`（config.go） | `internal/other/config.go` Reconcile（新增 key 默认 false） |
| 启停 API | `/accounts/pool/enabled{,/all}` | `/opencode/keys/routing{,/all}` | `/workbuddy/enabled` | `/trae/accounts/enabled` + `/trae/accounts/routing{,/all}` | `/<or|amd|tokenharbor>/keys/routing{,/all}` | `/other/keys/routing{,/all}` |
| 绑定代理 | `/accounts/proxy(/clear)` | `/opencode/keys/proxy(/clear)` | 行内 egCommit 暂存 | `/trae/accounts/proxy(/clear)` | `/<x>/keys/proxy(/clear)` | `/other/keys/proxy(/clear)`（按 slug） |
| 选号过滤 | `pool.go accountPoolEnabled` | zen 轮转跳过禁用 key | wbpool ServableForRealm | `internal/trae/pool.go` 路由表过滤 | 各包 pool.go `RoutingEnabled` 过滤 | internal/other `Platform.RoutingEnabled` |
| 平台设置 | /admin/api/config | /admin/api/opencode/config | wbConfigForm→vendor API | `/trae/config{,/update}` | `/admin/api/<x>/config{,/update}` | `/other/config{,/update}` + `/other/platforms`（子平台增删） |
| 前端 tbody | `accountTableBody` | `ocKeysBody` | `wbAccountsBody` | `traeAccountsBody` | `orKeysBody`/`amdKeysBody`/`thKeysBody` | `otherKeysBody` + `otherPlatformBody` |
| 快照变量 | `accSnapshot` | `keySnapshot` | `wbSnapshot` | `traeAccSnapshot` | `orKeySnapshot`/`amdKeySnapshot`/`thKeySnapshot` | `otherKeySnapshot` |
| 网关前缀 | `cline-free/` | `zen:` | `wbcn:`/`wbgb:` | `trae:` | `oprt:` / `amd:` / `tkhb:` | 子平台自定前缀（如 `acme:`） |
| 添加入口弹窗 | `accAddDialog` | `zenKeyAddDialog` | `wbAddDialog` | `traeAddDialog` | `<x>KeyAddDialog` | `otherKeyAddDialog`/`otherPlatformDialog` |
| 走池下拉 | `ppCline` | `ppZen` | `ppWb` | `ppTrae` | `ppOr`/`ppAmd`/`ppTh` | `ppOther` |
| 仪表盘卡/统计桶 | dashCline/reqCline | dashOc/reqZen | dashWb/reqWb | dashTrae/reqTrae | dashOr/dashAmd/dashTh + req 同名 | dashOther/reqOther |

## 5. 验证清单

```bash
# 编译+vet+全量测试：本机 Go 直跑（go1.27.2，PATH 已含）
go build ./... && go vet ./internal/... && go test ./...
# -race 单独跑：go test -race ./internal/app/（Windows 需本机有 gcc/clang，缺则跳过 race）

# 浏览器验证：改前端后必须重建镜像（docker restart 不更新文件系统）
docker rm -f afree-proxy-workbuddy-test
MSYS_NO_PATHCONV=1 docker run -d --name afree-proxy-workbuddy-test -e DATA_DIR=/app/data -p 3457:3457 <新构建的测试镜像>
# 测试镜像用 PROJECT.md §6 的本机构建流程：docker build -t afree-proxy:test-$(date +%Y%m%d-%H%M) .
# 旧的 afree-proxy:workbuddy-builder / workbuddy-local 镜像已删除（2026-10-27 清理），命令里别再引用
```

浏览器验证用真实点击 + 截图/API 断言，不用会话回显。测试容器 `docker rm -f` 强杀会丢内存态账号（flusher 未落盘），验持久化用 `docker restart`。

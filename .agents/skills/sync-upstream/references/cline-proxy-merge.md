# fork 上游线：foxy1402/cline-proxy 的 git merge

本仓由 cline-proxy 重构而来，`internal/app/*` 与上游同名同构，git 历史相连。上游本身**不建本地克隆**——直接 `git fetch <URL> main` 后用显式哈希 merge。每次同步都会产生一批冲突，但模式高度固定，按本节速查处理。

## 1. 流程

1. `git fetch https://github.com/foxy1402/cline-proxy main`，记下输出顶部的哈希（这就是上游 HEAD）。
2. 找基线：`git log --oneline --grep="sync upstream" --merges -1`，该 merge commit 的**第二父**即上次同步的上游位置。`git log --oneline <基线>..<上游HEAD>` + `git diff --stat` 预览本次内容。
3. `git merge --no-ff --no-commit <上游HEAD哈希>`。冲突数量典型 40+ 块、9~10 个文件；自动合并成功的部分也**不可盲信**（见 §5 手工收尾）。
4. 按 §3 速查逐文件解决 → 容器验证（SKILL.md §3）→ `git add -A` → merge commit → `git push origin main`。
5. merge commit 格式参照 7f27ae1：标题 `merge: sync upstream <短哈希> (<主题词>)`，正文列上游各提交内容、本地适配点、验证结论、`# Conflicts:` 文件列表。

## 2. 本地与上游的架构差异（冲突的根源）

- 本仓已重构为**多平台网关**：cline（原生账号池）+ zen（key 池）+ WorkBuddy（vendor）+ OpenRouter/AMD/TokenHarbor（key 池），上游是单网关。
- **面板完全本地化**（i18n 双语、暂存编辑模型、独立 session 面板）；上游是纯英文单文件面板。上游面板的结构性改动**不整体采纳**，只摘数据能力。
- `zenConfigData` 无 `Enabled` 字段（OpenCode 上游常开语义，本地删除过）。
- 代理隔离体系（KeyBindings/ProxyIsolation/BackupProxyEnabled/zenProxyBinding）是本地独有。

## 3. 冲突模式速查（按文件）

**zen.go / admin_zen.go 的配置结构体与 patch**：合并 = 本地字段组全保留 + 上游新字段并入 + 上游的 `Enabled` 丢弃。三处要同步改：`zenConfigData` 结构体、`handleZenConfigUpdate` 的 patch struct（指针三态）、`next := &zenConfigData{...}` 初始化（漏拷字段 = 全量替换语义下数据丢失，尤其 `KeyRoutingEnabled`）。

**admin.go 路由**：保留本地全部（三平台路由 + `/admin/api/opencode/*` 与 `/admin/api/zen/*` 双前缀别名 + sessions 端点）。上游删除某端点时先 grep 面板 JS 是否还在调（如 `loadOcSessions` 调 `/opencode/sessions`），在用就恢复路由。

**admin_html.go 面板**：保持本地 i18n 架构。可摘的数据能力（如 key 表 Rotates 列、failover 输入框）移植时：表头列数/`colspan`/渲染行的列序要一起改；JS 读写加 `_('id')` null 保护（上游直接 `_('x').value` 在本地 HTML 缺元素时整个 load 函数会崩）；`keySnapshot`/`rotateMin` 这类函数级变量在冲突两侧都可能是声明所在，别只留一边。

**proxy.go**：上游 `clineAPIWithAccount` 是两值签名（acc 由参数传入），本地旧的 `return nil, acc, ...` 三值行会污染进来——归位为两值；本地的 bound 代理绑定分支、`recordProxyOutcome` 出口记账保留。

**测试文件**：
- zen_session_test.go 的 setup 名是 `setupZenSessionTestUpstream`（本地改名；`proxy_binding_test.go` 里另有一个**同名无返回值**的 `setupZenSessionTest`，上游移植的测试要改调 Upstream 版，不能加别名——会撞车）。
- 上游测试自带两类渗漏 bug，要顺手修：`setZenModelForTest` 裸调后不恢复模型表（补 savedModels cleanup）；5xx 大户测试结尾加 `defer markZenSuccess()` 重置 failover 状态。
- 测试里 `cfg.Enabled = true` / `cfgCopy.Enabled = true` 直接删。

## 4. 全量测试挂了怎么排查（渗漏复发时）

症状：`TestZenPrefixResolvesSameModel` / `TestZenFreeModelResolutionWithPrefix` / `TestRouteModelWithZenPrefix` / `TestResponsesEntry...` 单跑 PASS、全量 FAIL。

二分法：`go test -run "<可疑渗漏测试A>|<失败测试B>"` 收缩组合，直到找到"加了它才挂"的测试。已知渗漏源：`TestZenCall403RefreshesSameKeyOnceThenCoolsAndFails`、`TestZenResponses403RefreshesSameKeyOnceThenCoolsAndFails`（模型表）、`TestEndpointLearnOnlyPersistsWhenRetrySucceeds`（failover 计数）。

另一类编译错：上游新增包级变量（如 `zenFailCount`/`zenFailUntil`）的 `var` 块在本地同名 var 块处被顶掉——自动合并没带进来，手动补声明。上游代码引用 `getZenConfig().Enabled` 直接删检查。

## 5. 自动合并成功 ≠ 正确

三类必须人工复核的自动合并残留：

1. **改名残留**：上游文件里的 `cline-proxy.log` 等旧标识（本仓改名 afree-proxy.log），merge 后全局 grep 一遍 `cline-proxy`。
2. **上游模块名**：新带进来的测试 import `cline-go-proxy/internal/kit`，改 `afree-proxy/internal/kit`。
3. **孤儿引用**：上游删掉的函数/端点，本地 JS 还在调；或反之——`go build` 抓 Go 侧，JS 侧靠 grep 函数名。

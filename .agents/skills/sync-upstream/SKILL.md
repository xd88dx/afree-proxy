---
name: sync-upstream
description: 同步 afree-proxy 三条上游线的完整流程与陷阱清单——fork 上游 foxy1402/cline-proxy 的 git merge 线（含 Cline 与 OpenCode 两个平台）、参考项目 workbuddy2api-panel 的 vendor 三方合并线、Trae 上游 Trae2api-cn 的内嵌授权助手比对线。凡是要"同步上游/sync upstream/合并上游更新/GitHub 提示 behind/上游有新提交/更新 workbuddy vendor/核对 Trae 上游"时使用，即使用户只说了"更新一下"。
---

# 上游同步配方

本仓有**三条独立的同步线**，先分清用户说的是哪条：

| | fork 上游线 | vendor 参考项目线 | Trae 比对线 |
|---|---|---|---|
| 上游 | `foxy1402/cline-proxy`（GitHub，本仓 main 的 fork 来源） | `misakano7545/workbuddy2api-panel`（本地克隆 `D:\下载\Github\workbuddy2api-panel`；曾误记为 `linguo2625469`，那是平行血统，勿用） | `autumnsentiment/Trae2api-cn`（Python，本地为 Go 重写，无代码可 merge） |
| 同步对象 | 整个仓库（网关/zen/cline 核心） | 仅 `internal/workbuddy/` vendor 树 | 仅 `internal/trae/helper_script.go` 内嵌的 `web_login.py` |
| 方式 | `git merge` 上游提交（历史相连） | `scripts/sync_workbuddy.py` 三方合并（vendor 产物） | `scripts/check_trae_helper.py` 逐行比对（不合并） |
| 细节 | references/cline-proxy-merge.md | references/workbuddy-vendor.md | 见 §2 Trae 小节 |

判别口诀：提到 cline/zen/behind/GitHub 提示 → fork 上游线；提到 WorkBuddy 面板/wb → vendor 线；提到 Trae/TraeWork/签到/9074/web_login → Trae 比对线。多条线都要更新时先做 fork 线（改动大、冲突多），再做 vendor 线，最后做 Trae 比对。

## 1. 公共铁律

1. **验证环境**（2026-10-15 更新）：本机已有 Go（`C:\AppSelf\Go`，go1.27.2，PATH 已含），宿主机可直接 `go build/vet/test/-race` 作为快速验证；与部署形态对齐的容器验证（linux/amd64）仍可用，见 §3。无论在哪跑：build/vet/test 全绿 + `-race ./internal/app/` 才算过。
2. **全量测试必须跑**：上游测试有状态渗漏（模型表/failover 状态不恢复），单跑 `-run` 会假通过；前缀类 zen 测试全量挂 = 渗漏复发，修法见 cline-proxy-merge.md §4。
3. **引用上游提交用显式哈希**，绝不依赖 `FETCH_HEAD`——后台 IDE fetch 会把 `.git/FETCH_HEAD` 覆写成自己 fork 的记录，导致把本地 HEAD 误当上游（真实踩坑）。
4. **会话回显不可信**：冲突解决结果以 `git diff`、容器构建、真实测试输出为准。
5. 提交信息用中文、说明上游哈希与本地适配点；fork 线结束必须 `git push origin main`（不推 GitHub 的 behind 提示不会消除，同步就没生效）。
6. **收尾必写 `MERGE.md` 台账**（用户 2026-10-27 要求）：仓库根 `MERGE.md` 按 Cline / TreaWork / WorkBuddy / OpenCode 四条线记录同步历史。每次同步在对应线追加一节 `### YYYY-MM-DD — <基线短哈希> → <目标短哈希>`，写明上游仓库与合并方式、基线与目标、吸收项（带原因）、丢弃项（带原因）、本地定制保留清单、验证结果；未涉及的线保留原文并写"本次未涉及上游同步"。历史记录只追加不删改。
   **分节前先判平台归属，写错节等于台账失效**：`foxy1402/cline-proxy` 是**一个上游承载两条平台线**——`zen*.go` / `admin_zen.go` / 面板 `oc*` 字段 / `ZEN_*` 配置 / `internal/kit/http.go`（`DecodeResponse` 只被 `zen.go` 调用）属于 **OpenCode**；Cline 账号池与 `/v1` 通用转发/方言转换属于 **Cline**。判据命令：`git diff --name-only <本地旧HEAD> <merge提交> | grep -iE 'zen|opencode'` 与 `| grep -i cline`，两者只应付出一条线；不要因为上游叫 cline-proxy 就把 zen 改动记在 Cline 下。
7. **根 `AGENTS.md` 是其他 agent 的唯一自动入口**:`MERGE.md` 和本 skill 都不会被其他 agent 自动加载。根目录按「通用 / 专属」拆两份——`AGENTS.md` 放**跨项目通用规则**(git 纪律、验证要求、删除判据、上游台账格式),`PROJECT.md` 放**本项目专属事实**(平台归属表、三条线入口、flaky 不修、本地定制清单、部署流程);`AGENTS.md` 顶部强指向 `PROJECT.md` 并要求动手前必读。改动这些规则时同步更新对应文件,否则下一个 agent 会重犯。

## 2. 三条线各自的入口

**fork 上游线**（merge 冲突多，是体力活）：

```bash
git fetch https://github.com/foxy1402/cline-proxy main
# 上次同步到哪：找最近的 "merge: sync upstream" 提交，其第二父即基线
git log --oneline --grep="sync upstream" --merges -1
git log --oneline <基线哈希>..FETCH_HEAD   # 只用于预览，合并时用显式哈希
git merge --no-ff --no-commit <上游HEAD哈希>   # 试合并，评估冲突
```

然后按 cline-proxy-merge.md 的冲突模式速查逐文件解决。

**vendor 线**（脚本自动化，冲突少）：

```bash
python scripts/sync_workbuddy.py --report   # 先看会改什么，不写文件
python scripts/sync_workbuddy.py            # 实际合并；冲突则解决后 --seal
python scripts/sync_workbuddy.py --seal     # 冲突清零后登记新基线
```

脚本前置条件与失败处理见 workbuddy-vendor.md。

**Trae 比对线**（不合并，只核对内嵌脚本）：

```bash
# 上游是 Python 项目,tar 包比 git clone 稳(该代理链上 clone 常被 TLS 中断)
curl -sSL -x socks5h://127.0.0.1:10808 -o /tmp/trae-up.tar.gz \
  https://codeload.github.com/autumnsentiment/Trae2api-cn/tar.gz/refs/heads/main
mkdir -p /tmp/trae-up && tar xzf /tmp/trae-up.tar.gz -C /tmp/trae-up --strip-components=1
# 注意:python 是原生程序,不认 /tmp,要传 Windows 路径
python scripts/check_trae_helper.py --upstream-dir "$(cygpath -w /tmp/trae-up)"
```

exit 0 = `internal/trae/helper_script.go` 内嵌的 `web_login.py` 与上游逐行一致（仅 `__RELAY_URL__` 占位符差异）；exit 1 打印 unified diff，处理方式是**以上游文件重建内嵌模板**（只替换 `DEFAULT_RELAY_URL` 为 `__RELAY_URL__`、按 Go raw string 规则把反引号转义成 `` ` + "`" + ` ``），不要手工逐行改。升级后同步更新 `scripts/trae-helper-baseline.json` 的 version / commit / date。

Go 侧（`internal/trae/*.go`、`internal/app/trae.go`）**没有可比对基线**——上游全 Python，本地是重写，只能按上游 `docs/release-v*.md` 人工核对协议面，并把"已具备 / 已知架构差异 / 未逐条核对"三类结论写进 `MERGE.md` 的 TreaWork 节。已知差异见台账（本地全局信号量 vs 上游账号级 entitlement 并发池）。

## 3. 验证命令（三条线通用）

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "D:/下载/Github/afree-proxy:/build" -w /build \
  afree-proxy:workbuddy-builder sh -c \
  'go build ./... && go vet ./internal/... && go test ./... && go test -race ./internal/app/'
# -race 需容器内先有 gcc/musl：apk add --no-cache gcc musl-dev
# gofmt -l 报一片 = CRLF 工作区形态（autocrlf），非本次编辑引入，忽略
```

注意：`-v "D:/..."` 路径在 Git Bash 下必须 `MSYS_NO_PATHCONV=1`，否则路径被改写挂载失败。

**既有 flaky（勿误判为回归，用户 2026-10-27 拍板不修）**：`internal/workbuddy/server` 的 `TestChatStreamPassthrough` 在全量 `go test ./...` 时失败，报 `LastTokensPerSecond=nil` 而 `LastLatencyMs=1`。根因已定位：`tokensPerSecond()`（`handler.go:2011`）在 `ttfb <= 0` 时返回 `ok=false`，`RecordTokenUsage`（`pool/state.go:395`）据此把 `LastTokensPerSecond` 置 nil，而断言（`handler_test.go:462`）要求非 nil——httptest 即时响应下 `ttfb` 常被测成 0，故整包跑必挂、单独 `-run` 跑则过。属上游测试的既有缺陷，**不要修**（改上游测试会与后续同步持续冲突），也不要据此判定同步回归；判定既有性可用 `git worktree add --detach <scratch>/afree-head f9a0e13` 对照跑同包。

## 4. 完成判据

- fork 线：merge commit 双父（本地 HEAD + 上游哈希）已推送 origin；GitHub 仓库页不再显示 behind；`MERGE.md` 的**对应平台节**（Cline 或 OpenCode，按改动文件归属判定）已追加本次记录。
- vendor 线：`workbuddy-vendor-baseline.json` 的 commit = 上游 HEAD；脚本手工步骤清单（装配/版本号）已完成；容器验证全绿；`MERGE.md` 的 WorkBuddy 节已追加本次记录。
- Trae 线：`scripts/trae-helper-baseline.json` 已登记上游最新 tag / commit；`check_trae_helper.py` exit 0；Go 侧按上游 release notes 的核对结论（已具备 / 已知差异 / 未逐条核对）已写入 `MERGE.md` 的 TreaWork 节。

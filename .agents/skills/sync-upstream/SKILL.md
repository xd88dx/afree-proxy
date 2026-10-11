---
name: sync-upstream
description: 同步 afree-proxy 两条上游线的完整流程与陷阱清单——fork 上游 foxy1402/cline-proxy 的 git merge 线、参考项目 workbuddy2api-panel 的 vendor 三方合并线。凡是要"同步上游/sync upstream/合并上游更新/GitHub 提示 behind/上游有新提交/更新 workbuddy vendor"时使用，即使用户只说了"更新一下"。
---

# 上游同步配方

本仓有**两条独立的同步线**，先分清用户说的是哪条：

| | fork 上游线 | vendor 参考项目线 |
|---|---|---|
| 上游 | `foxy1402/cline-proxy`（GitHub，本仓 main 的 fork 来源） | `linguo2625469/workbuddy2api-panel`（本地克隆 `D:\下载\Github\workbuddy2api-panel`） |
| 同步对象 | 整个仓库（网关/zen/cline 核心） | 仅 `internal/workbuddy/` vendor 树 |
| 方式 | `git merge` 上游提交（历史相连） | `scripts/sync_workbuddy.py` 三方合并（vendor 产物） |
| 细节 | references/cline-proxy-merge.md | references/workbuddy-vendor.md |

判别口诀：提到 cline/zen/behind/GitHub 提示 → fork 上游线；提到 WorkBuddy 面板/wb → vendor 线。两边都要更新时先做 fork 线（改动大、冲突多），再做 vendor 线。

## 1. 公共铁律

1. **验证一律容器**：本机无 Go。build/vet/test 全绿 + `-race ./internal/app/` 才算过（详见 §3）。
2. **全量测试必须跑**：上游测试有状态渗漏（模型表/failover 状态不恢复），单跑 `-run` 会假通过；前缀类 zen 测试全量挂 = 渗漏复发，修法见 cline-proxy-merge.md §4。
3. **引用上游提交用显式哈希**，绝不依赖 `FETCH_HEAD`——后台 IDE fetch 会把 `.git/FETCH_HEAD` 覆写成自己 fork 的记录，导致把本地 HEAD 误当上游（真实踩坑）。
4. **会话回显不可信**：冲突解决结果以 `git diff`、容器构建、真实测试输出为准。
5. 提交信息用中文、说明上游哈希与本地适配点；fork 线结束必须 `git push origin main`（不推 GitHub 的 behind 提示不会消除，同步就没生效）。

## 2. 两条线各自的入口

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

## 3. 验证命令（两条线通用）

```bash
MSYS_NO_PATHCONV=1 docker run --rm -v "D:/下载/Github/afree-proxy:/build" -w /build \
  afree-proxy:workbuddy-builder sh -c \
  'go build ./... && go vet ./internal/... && go test ./... && go test -race ./internal/app/'
# -race 需容器内先有 gcc/musl：apk add --no-cache gcc musl-dev
# gofmt -l 报一片 = CRLF 工作区形态（autocrlf），非本次编辑引入，忽略
```

注意：`-v "D:/..."` 路径在 Git Bash 下必须 `MSYS_NO_PATHCONV=1`，否则路径被改写挂载失败。

## 4. 完成判据

- fork 线：merge commit 双父（本地 HEAD + 上游哈希）已推送 origin；GitHub 仓库页不再显示 behind。
- vendor 线：`workbuddy-vendor-baseline.json` 的 commit = 上游 HEAD；脚本手工步骤清单（装配/版本号）已完成；容器验证全绿。

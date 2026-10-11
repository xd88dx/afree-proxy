# vendor 参考项目线：workbuddy2api-panel 三方合并

`internal/workbuddy/` 由 `scripts/vendor_workbuddy.py` 从上游生成（import 路径改写 + 出站代理改写锚点 + 面板 iframe 策略），树上还叠着 afree 手工改动。`scripts/sync_workbuddy.py` 负责安全地同步两者，绝不直接 re-vendor（会冲掉手工改动）。

## 1. 环境事实

- 本地克隆：`D:\下载\Github\workbuddy2api-panel`。remote `origin` = xd88dx fork（用户自己的，**main 跟踪它**），`upstream` = linguo2625469（真上游，push 已 DISABLED）。真上游 → 用户 fork（GitHub 上手动 sync）→ 本地克隆，fork 是中转站。
- **原项目只读**：脚本会拒绝上游工作区不干净的运行；永远不在上游克隆里提交/修改内容。
- 基线：`scripts/workbuddy-vendor-baseline.json`（commit + synced_at + upstream_version），`--seal` 登记后推进。

## 2. 合并语义（脚本自动完成）

```
base   = vendor(基线 commit)     纯净 vendor 产物
ours   = 当前 internal/workbuddy  base + 本地手工改动
theirs = vendor(上游 HEAD)       新上游 + 脚本改写
```

每文件 `git merge-file` 三方合并：手工改动存活、上游变更流入、真冲突留 `<<<<<<<` 标记且**基线不推进**。上游删除的文件本地保留并警告（人工判断）；本地删除的不复活。

## 3. 操作流程

```bash
# 0. 更新本地克隆。--pull 对克隆执行 ff-only pull，拉的是其跟踪分支（origin=用户 fork）。
#    若 fork 还没跟上 linguo 真上游，先在克隆里手动：
#      git fetch upstream && git merge --ff-only upstream/main
#    （要不要 push origin 同步中转 fork，先问用户。）
python scripts/sync_workbuddy.py --pull
# 1. 预演：看会合并/冲突/新增/删除哪些文件，不写任何东西
python scripts/sync_workbuddy.py --report
# 2. 实际合并
python scripts/sync_workbuddy.py
# 3a. 无冲突 → 基线自动推进，跳到 §4
# 3b. 有冲突 → 解决 internal/workbuddy 里的 <<<<<<< 标记后登记基线
python scripts/sync_workbuddy.py --seal
```

`--source <路径>` 可临时指定其他上游克隆。

## 4. 合并成功后的手工步骤（脚本会打印提醒，但不会代劳）

1. `internal/app/workbuddy.go` + `workbuddy_config.go`：新配置键的装配与热应用、`restartRequired` 清单、`workbuddyVersion` 版本号对齐上游 `appVersion`（脚本会 diff 提醒）。
2. 上游新增面板能力若要在集成面板露出，按 add-platform skill 的面板规范接 i18n。
3. 容器验证（SKILL.md §3），然后 `git add -A && git commit`（信息注明上游版本区间，参照 bef9062 的格式）。

## 5. vendor 构建失败的唯一原因：改写锚点失效

`vendor_workbuddy.py` 用**字面锚点**做改写，上游重构会让锚点失配并报错退出。锚点清单（都在 scripts/vendor_workbuddy.py）：

- `SOURCE_ROOT`：上游本地路径。
- `PROXY_FIELD_ANCHOR` / `PROXY_FIELD_BLOCK`：给 vendored Client 注入 `ProxyFor` 出站代理字段。
- `EGRESS_REWRITES`：upstream/*.go 各文件"账号级出口解析"的函数签名/调用点改写（doJSON(a, req)、doHTTP(req, a)、chatClientFor(a) 等）。
- `apply_panel_iframe_rewrites`：panel CSP 从 `frame-ancestors 'none'` 放宽到 `'self'`（同源 iframe 嵌套进 afree 管理页）。

修法：按报错文件打开上游对应文件，找到结构变化后的新锚点文本，更新脚本里的锚点常量，重跑 `sync_workbuddy.py`。改锚点时保持改写语义不变（隔离优先：所有账号级出站调用必须过 `ProxyFor` 解析出口）。

## 6. Windows 执行注意

- 用 `py` 启动器跑（`py scripts/sync_workbuddy.py`）；直接 `python` 可能命中 WindowsApps 存根。
- 脚本已做 LF 归一（`read_lf`），CRLF 工作区不会导致整文件冲突；若仍见整文件级冲突，先检查 `.gitattributes` 是否还在钉 LF。

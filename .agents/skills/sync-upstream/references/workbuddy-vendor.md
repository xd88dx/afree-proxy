# vendor 参考项目线：workbuddy2api-panel 三方合并

`internal/workbuddy/` 由 `scripts/vendor_workbuddy.py` 从上游生成（import 路径改写 + 出站代理改写锚点 + 面板 iframe 策略），树上还叠着 afree 手工改动。`scripts/sync_workbuddy.py` 负责安全地同步两者，绝不直接 re-vendor（会冲掉手工改动）。

## 1. 环境事实

- 本地克隆：`D:\下载\Github\workbuddy2api-panel`。remote `origin` = xd88dx fork（用户自己的，**main 跟踪它**），`upstream` = misakano7545（真上游，push 已 DISABLED）。真上游 → 用户 fork（GitHub 上手动 sync）→ 本地克隆，fork 是中转站。
- **血统坑（2026-10-26 查证）**：本文件曾记真上游为 `linguo2625469`，那是**同名但平行血统**的另一个仓库（repo id 不同，与 misakano 在 `dbd7c68` 分叉）。判真源的办法：基线提交 `9a002057` 在 `misakano7545` 返回 200、在 `linguo2625469` 返回 422。本地克隆的 `main` 也属 linguo 血统（179 独有提交 vs misakano 423），**不可作同步源**。
- **本仓与上游无共同祖先**：`git fetch upstream` 报 `fatal: fetch-pack: invalid index-pack output` + `warning: no common commits`，禁用 `git merge`/`git pull`。取源二选一：`git -C <克隆> worktree add --detach <目标> misakano/main`（推荐，克隆里有 misakano remote），或 codeload tarball。
- **原项目只读**：脚本会拒绝上游工作区不干净的运行；永远不在上游克隆里提交/修改内容。
- 基线：`scripts/workbuddy-vendor-baseline.json`（commit + synced_at + upstream_version），`--seal` 登记后推进。

## 2. 合并语义（脚本自动完成）

```
base   = vendor(基线 commit)     纯净 vendor 产物
ours   = 当前 internal/workbuddy  base + 本地手工改动
theirs = vendor(上游 HEAD)       新上游 + 脚本改写
```

每文件 `git merge-file` 三方合并：手工改动存活、上游变更流入、真冲突留 `<<<<<<<` 标记且**基线不推进**。上游删除的文件本地保留并警告（人工判断）；本地删除的不复活。

**冲突块标签语义（2026-10-21 修 bug 后）**：脚本向 `git merge-file cur base ours` 传入的 cur=**上游 theirs**、op=**本地 ours**，标签已对齐为 `-L THEIRS -L BASE -L OURS`——即冲突块**第一段标 THEIRS = 上游内容，最后一段标 OURS = 本地内容**（与 git 习惯相反，勿搞反）。历史上标签是反的（OURS 实为上游），凡参照旧冲突块笔记时注意。

**vendor 改写白名单（2026-10-21 扩）**：上游新增**子包**时必须同步 `scripts/vendor_workbuddy.py` 的 `PACKAGES` 清单（漏了会 silently 缺包，编译报 "is not in std"）；上游新增**资产文件**（.css 等）必须扩 `ASSET_SUFFIXES`（漏了报 "pattern: no matching files found"）。

**gofmt 对齐（2026-10-26 加）**：本地 `internal/workbuddy` 全树已 gofmt 干净，而 vendor 脚本吐出的上游代码不保证格式一致。脚本在生成 base/theirs 后各自 `gofmt -w` 一遍（`gofmt_tree`），否则纯格式差异会被三方合并当成真实改动，退化成整文件冲突。手工改完 vendor 树也要跑 `gofmt -w`，别把格式债留给下次同步。

**本地倍率筛选 vs 上游预置缓存的测试（2026-10-21 定）**：上游测试可能直接预置 `dynamicModelsCache.ids` 而不经 fetch 同源路径，此时倍率表为空——`matchesModelRateFilter` 对「倍率表整体为空」放行（`upstream.ModelRatesEmpty()`，生产不可达状态），否则上游此类测试会被本地「缺失即隐藏」口径清空列表而挂。不要为过测试改上游测试文件（sync 会覆盖）。

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
4. **`internal/config.example.json` 是上游根文件的副本，脚本不会带**（它不在 vendor 树内）。上游 `cmd/server/config_test.go` 的 `TestConfigExampleParses` 读 `../../config.example.json`：上游在 `cmd/server/`，vendor 后包落在 `internal/workbuddy/config/`，该相对路径正好指向 `internal/config.example.json`。上游改了样例就 `cp <上游克隆>/config.example.json internal/config.example.json`，否则 `go test ./internal/workbuddy/config/` 因缺文件失败。**不要改上游测试文件绕过**（下次同步会覆盖，且与「不为过测试改上游测试」的既定口径冲突）。

## 5. vendor 构建失败的唯一原因：改写锚点失效

`vendor_workbuddy.py` 用**字面锚点**做改写，上游重构会让锚点失配并报错退出。锚点清单（都在 scripts/vendor_workbuddy.py）：

- `SOURCE_ROOT`：上游本地路径。
- `PROXY_FIELD_ANCHOR` / `PROXY_FIELD_BLOCK`：给 vendored Client 注入 `ProxyFor` 出站代理字段。
- `EGRESS_REWRITES`：upstream/*.go 各文件"账号级出口解析"的函数签名/调用点改写（doJSON(a, req)、doHTTP(req, a)、chatClientFor(a) 等）。
- `apply_panel_iframe_rewrites`：panel CSP 从 `frame-ancestors 'none'` 放宽到 `'self'`（同源 iframe 嵌套进 afree 管理页）。

修法：按报错文件打开上游对应文件，找到结构变化后的新锚点文本，更新脚本里的锚点常量，重跑 `sync_workbuddy.py`。改锚点时保持改写语义不变（隔离优先：所有账号级出站调用必须过 `ProxyFor` 解析出口）。

## 6. Windows 执行注意

- 用 `py` 启动器跑（`py scripts/sync_workbuddy.py`）；直接 `python` 可能命中 WindowsApps 存根。
- 脚本已做 LF 归一（`read_lf`）+ 对 base/theirs 做 `gofmt -w` 对齐（见 §2），CRLF 工作区与格式差异都不会导致整文件冲突；若仍见整文件级冲突，先检查 `.gitattributes` 是否还在钉 LF。

## 7. 判据：上游新增的 HTTP 端点在本仓**大多没有路由入口**

afree 不是把 workbuddy 当独立 HTTP 服务挂载，而是内嵌子系统：`internal/app/proxy.go` 只在主 mux 注册 `/v1/chat/completions`、`/v1/messages`、`/v1/responses`、`/v1/models`、`/v1/health`，并按模型前缀调用 `workbuddySub.serveWorkBuddyChat(...)` 等方法。workbuddy 自己的 `h.mux`（`internal/workbuddy/server/handler.go` 里的 `POST /v1/embeddings`、`POST /v1/search`、`POST /v1/fetch`、`POST /v1/images/*` 等）**没有挂到主 mux**，生产实测一律 `404 page not found`。

因此上游新增端点时的处理原则：

1. 端点代码随 vendor 同步进来（`embeddings.go` 及其测试保留，不删不裁）。
2. **不要**把开关接到面板/装配层——那会造出一个永远无效的死配置（`features.embeddings_enabled` 就是这种情形，2026-10-27 撤回）。
3. `MERGE.md` 里写明"未接入本仓路由"及原因；接入属于功能开发（需要 `wbcn:`/`wbgb:` ↔ `cn:`/`global:` 前缀翻译、认证桥接、mux 注册三件事），不是同步任务的一部分。
4. 判据代码：`grep -rn '"/v1' internal/app/*.go | grep -v _test` 看主 mux 到底注册了什么。


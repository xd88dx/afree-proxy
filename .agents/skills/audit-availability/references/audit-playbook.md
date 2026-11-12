# 可用性审计 playbook（命令模板与陷阱）

2026-10-31 首次全量审计沉淀。环境值（端口/密码/key）以当次 compose 与 AGENTS.md 为准，下面的值是 2026-10-31 实测快照。

## 1. newapi 渠道映射提取

```python
import sqlite3, shutil, json
# 运行中容器的库先复制再读（WAL 也在用，直接 ro open 会踩锁）
shutil.copy('D:/DockerApp/newapi/data/one-api.db', '<tmp>/newapi-audit.db')
con = sqlite3.connect(r'<tmp>/newapi-audit.db')
cur = con.cursor()
cur.execute('SELECT id,name,base_url,status,model_mapping,models FROM channels ORDER BY id')
targets = set()
for cid, name, base, status, mapping, models in cur.fetchall():
    mm = json.loads(mapping or '{}')
    targets.update(mm.values())     # 平台前缀目标 = 测试清单
print(len(targets), sorted(targets))
```

陷阱：
- sqlite URI 的 Windows 路径写 `file:D:/...` 形态会 `unable to open`——直接 shutil.copy 后用普通路径连接。
- `channels.key` 是 newapi 侧对 afree 的 key（不是上游凭据），审计 afree 用 compose 里的 `8008208820`。

## 2. 凭据盘点（一行一平台）

```python
# cline-config.json: accounts[] {accountId,email,apiToken,status,cooldownUntil,poolEnabled}
# zen-config.json:  keys[]（字符串数组）+ keyRoutingEnabled{} 全 True
# trae-config.json: accounts[] {id,label,token,enabled,poolEnabled,expiredAt}
# amd/oprt/tkhb:    keys[] + keyRoutingEnabled{}
# other-config.json: platforms[]{slug,name,prefix,baseURL,keys[],keyBindings{}}
# workbuddy: auths/*.json（.bak 同名成对）+ state.json accounts{}.disabled
```

## 3. 探针脚本骨架

```python
import json, time, urllib.request, urllib.error
GW = "http://127.0.0.1:3457"; KEY = "8008208820"
def probe(model):
    body = json.dumps({"model": model, "messages": [{"role":"user","content":"Reply with exactly: OK"}], "max_tokens": 32}).encode()
    req = urllib.request.Request(f"{GW}/v1/chat/completions", data=body, method="POST",
        headers={"Content-Type":"application/json","Authorization":f"Bearer {KEY}"})
    t0=time.time()
    try:
        with urllib.request.urlopen(req, timeout=150) as resp:
            d = json.loads(resp.read().decode())
            c = (d.get("choices") or [{}])[0].get("message",{}).get("content","")
            return {"model":model,"ok":True,"secs":round(time.time()-t0,1),"reply":c[:60]}
    except urllib.error.HTTPError as e:
        try: m = json.dumps(json.loads(e.read().decode()), ensure_ascii=False)[:300]
        except: m = "<unreadable>"
        return {"model":model,"ok":False,"http":e.code,"error":m}
# 串行跑全目标，结果落 JSON
```

空 reply（`reply=''`）且 200 = reasoning 模型正常形态，算 PASS。

## 4. 直连上游验证（绕过 afree，拿铁证）

- urllib/requests 的 ProxyHandler **不支持 socks5**——用 `curl --socks5-hostname 127.0.0.1:10086`（首选，全场最快；不通按 AGENTS.md §10 顺序回退）。
- token 从 afree 数据目录取（如 `cline-config.json` accounts[0].apiToken），写临时文件传给 curl，**用完删**。
- Cline feed：`https://api.cline.bot/api/v1/ai/cline/recommended-models`（Bearer 账号 token + `X-Cline-Client-Version` 头；无 /api/v1 前缀会 404，无 token 401）。响应四组：recommended / **free** / clinePass / clineCloud——free 组即官方免费集。
- 免费性铁证：同账号对候选模型发流式请求（非流式对部分模型 500 "empty response content" 是"必须流式"指纹，不是故障），SSE 尾帧 `usage.cost`：0 = 免费，>0 = 计费。对比实验用同一账号跑 free 组与候选各一次。
- 官方免费层会静默把模型移入付费档（clinePass）且 feed 只是推荐列表——三种形态都出现过：404（彻底拒绝）、200 但 cost>0（静默计费）、不在 feed 但 cost=0（仍免费）。

## 5. 出口冷却干扰的识别与等待

- 日志关键词：`cline upstream-side stream error ... EOF`、`[exit-cooldown] ... cooled 10m`、`bound proxies unavailable (cooling or removed)`。
- 一次 500 "bound proxies unavailable" 很可能是**出口冷却连锁**而非模型问题——10 分钟后复测再定论。
- `not cooling the exit`（单次宽容）与后续 `[exit-cooldown] cooled`（累计阈值）并存是设计行为。

## 6. 面板 API 速查

```bash
TOKEN=$(curl -s -X POST http://127.0.0.1:3457/admin/api/login -H 'Content-Type: application/json' \
  -d '{"password":"pass1234"}' | python -c "import json,sys; print(json.load(sys.stdin)['data']['token'])")
curl -s http://127.0.0.1:3457/admin/api/models/blacklist -H "Authorization: Bearer $TOKEN"
curl -s http://127.0.0.1:3457/admin/api/models -H "Authorization: Bearer $TOKEN"   # cline 目录（id 已带 cline-free/ 前缀）
# 手动封禁/解禁/批量封禁：POST /admin/api/models/blacklist/{ban,unblock,batch-ban}
```

- login 的 token 在 **`data.token`**，取顶层 `token` 会拿到空。
- `/admin/api/models` 返回的 cline 条目 id **已带 `cline-free/` 前缀**（admin_html.go 里"目录返回裸 ID"的旧注释已过时）。
- 面板模型目录区每行的红色「封禁」是 **Ban 操作按钮**不是状态徽章；真实封禁看「封禁模型」表或 blacklist API。

## 7. 测试时间炸弹（上游测试常见）

上游测试写死与提交时刻绑定的值（如 `Retry-After: 80378` = 提交时刻距 00:00 UTC 的秒数）——合并后只在每天特定时段能过。本地适配模式：改成运行时计算（`retryAfterToMidnight().Seconds()`），并在台账"测试本地适配"节登记。

## 8. 报告口径模板

```
## 结果矩阵：X/Y 通过
| 平台 | 目标数 | 结果 |
失败根因表（每项）：现象 → 铁证（官方 API 响应/上游代码行/日志行）→ 归属（官方限制/出口链路/项目缺陷）→ 处置
已修复缺陷：文件 + 机制描述 + 回归测试名 + 端到端验证结果
newapi 映射建议：死别名 → 替代（标注"无等价只能删"）
口径说明：凭据粒度（模型维度轮换）、出口冷却事件、上游对比结论
```

## 9. 本次（2026-10-31）审计结论快照（后续审计的对照基线）

- 33 个映射目标 30 过；3 死全为官方限制：cline 的 deepseek-v4.1-flash/muse-spark-1.3-contributor/solar-pro4 移入 clinePass（404）；z-ai/glm-5.3-flash 转计费（cost>0，已拉黑）；trae 的 kimi-k3.1-pro/k2.8-preview/k2.7-code 权益不足（1005/4001）。
- 已修：cline 404/cost 自动拉黑 + `BanReasonFeedRemoved` 文案 + 存量归一；请求路径服务闸；去 seed 兜底。
- 可用替代（实测）：deepseek→`wbgb:`/`zen:deepseek-v4.1-flash-free`；kimi→`tacn:kimi-k3`；glm→`zen:glm-5.3-flash-free`/`wbcn:glm-5.3-flash`/`tacn:glm-5.3-flash`；muse 无等价只能删。
- `poolside/laguna-s-2.1:free` 不在 feed 但 cost=0（去兜底后已剪出目录；要用走 combo 别名）。

## 10. 2026-11-05 审计追加（第二轮全量，含修复）

- **审计中途的容器重启是天然干扰源**：本机 `sync-istore-docker` 定时任务 `-Deploy` 模式会 `docker restart afree`（10 分钟节拍）。重启瞬间进行中的 Test 请求报 `RemoteDisconnected`（Python 侧）——不是凭据坏，复测即可。审计前先 `docker inspect afree --format '{{.State.StartedAt}}'` 记下启动时刻，结果异常先对照。
- **cline/zen 目录同步出口语义**（本轮已修，后续审计按此核对）：目录同步必须走凭据绑定出口（cline=账号绑定/走池开关，zen=boundExitFor 含 egress 钉定），裸直连在直连国际不可达的部署上会让目录自重启起每轮 EOF→目录清空→`firstActiveModel()` 空串→探测 400 "invalid model format"（这是**空 model** 的指纹，别误判成模型形态问题）。判定：`Test account` 报该 400 且 `/v1/models` cline 段为空 = 目录空；对照 `docker logs` 的 `model sync: failed (… EOF)`。
- **容器内直连 ≠ 宿主机直连 ≠ 出口**：三者网络面不同，归因时分层探测——宿主机 `curl --socks5-hostname <出口>`、`docker exec afree wget -qO- <URL>`（镜像 alpine 有 busybox wget，无 curl；busybox wget 不支持 socks，只能测直连路径）。"经出口宿主机 200 但容器内失败"优先怀疑同步没接出口而不是出口坏。
- **trae 域名分层**：chat 域 `trae-api-cn.mchost.guru` 与额度域 `api.trae.cn` 独立——额度域全网络 EOF 时 Test（额度连通性验证）恒败但 chat 可能照常，结论以真实 chat 请求为准，别凭 Test 失败判死 trae。
- **stream 缺省契约检查**（本轮抓到 trae，新平台接入必查）：`POST /v1/chat/completions` **不带 stream 键**（OpenAI 缺省=false），响应必须是 `application/json`。vendored 引擎曾把缺省当 true 返回 SSE——探针脚本报 `JSONDecodeError char 0` 且 200 时，先看 Content-Type 是不是 text/event-stream。
- **探针脚本自身健壮性**：`d.get("usage", {})` 在 `"usage": null` 时 TypeError（要用 `(d.get("usage") or {})`）；tool_calls 元素的 `function` 字段可能为 null，取值前判空——平台差异会让矩阵脚本整平台报 platform-error。
- **自动拉黑的实时验证窗口**：探针撞 404/RegionError 当场即拉黑（blacklist API 可见 `auto-banned: model removed from official free feed`），同一轮里后续平台探到 `is banned and no longer served` 404 是**预期行为**不是新故障；报告口径按"已摘除"计。
- **统一溢出报错**（2026-11-05 用户定，已实现）：网关把各上游 context-overflow 签名归一为 400 + `code: "context_length_exceeded"` + 压缩提示文案（`context_overflow.go`）；上下文档位测试时错误体出现该 code 即归一化生效，仍是 502 裸上游文案 = 签名缺口，往 patterns 里补。

## 11. 2026-11-06 审计追加（全凭据轮：130 凭据 + 出口池扫描，含两修复 89c8bd5）

### 11a. 凭据级测试端点契约（管理面 Test 按钮 = 与面板同语义的逐凭据探针）

| 平台 | 端点与入参 | 判定 | 坑 |
|---|---|---|---|
| cline | `POST /admin/api/accounts/test {accountId}`（列表 `GET /admin/api/accounts`，字段是 **`accountId`** 不是 id） | `data.status=='active'` | — |
| zen/amd/oprt/tkhb | `POST /admin/api/{zen|amd|openrouter|tokenharbor}/keys/test {index, model?}` | 同上 | amd Test 探测预算 60s，上游慢会误报 `context deadline exceeded`——复测再定性 |
| other | `POST /admin/api/other/keys/test {slug, index, model}` | 同上 | — |
| trae | `POST /admin/api/trae/accounts/test {uid}`，uid=完整 `trae-cn-*`（账号 json 文件名，不是纯数字 userId） | vendored 透传 | 用数字 uid 报"账号不存在" |
| workbuddy | `POST /admin/workbuddy/api/accounts/{uid}/test` | 扁平响应 `{ok,status,model,latencyMs,cooldownCleared}`，`status=='active'` | ① 挂载重写 `/admin/workbuddy/X → /panel/X`，**多写一层 panel 就是 404 page not found**；② 鉴权两层：外层 `adminAuthMiddleware` 吃 admin 会话 token（`/admin/api/login` 的 `data.token`），内层面板 `VerifyBearer(api_key)`；③ 响应是 vendored 扁平结构，**不走 afree apiResponse 包装**——按 `data.status` 解析恒得 null，全误报 FAIL |

### 11b. 出口池健康扫描与"死出口拖累面"分析

```bash
# 全量出口并行连通扫描（出口清单取 proxy-config.json proxies[]，URL 形态 socks5://10.10.10.5:PORT#NAME）
cat exits.txt | while read port name; do (r=$(curl -s -o /dev/null -w "%{http_code}" \
  --socks5-hostname 10.10.10.5:$port -m 10 https://www.gstatic.com/generate_204); echo "$port $name $r") & done | sort -n
```

- 扫出死出口后必做**绑定关联分析**：一条死出口会同时拖累多个凭据——查各 `*-config.json` 的 `keyBindings`（zen/amd/oprt/tkhb/other）、`cline-config.json` 账号 `proxyMain`、以及 `proxy-config.json` 的 `egressBindings`/`trae2apiBindings`/`workbuddyBindings` 段（egress 钉定键是 key 哈希，`zen:<hash>`）。2026-11-06 实测：deplexo-DE(11107) 一条死出口挂了 5 个凭据，egressBindings 另有 4 个 zen key 被钉到死出口。
- 换出口对比测试在**本机**做（`curl --socks5-hostname <候选出口> <上游>`），不动生产绑定——绑定修改属生产配置变更，列清单交用户。

### 11c. GFE 403 = 出口 IP 被拉黑的判定法（非账号）

出口 IP 被上游边缘（Google GFE 类）拦截时，cline Test 报 `API 403: <html>...Your client does not have permission to get URL...`。铁证：**用同一出口发无 token 的裸请求，同样被拦同一页面**（`curl --socks5-hostname <出口> https://api.cline.bot/api/v1/ai/cline/recommended-models`）；对照健康出口同路径回 200/401（可达）。判定为出口后处置是改绑出口，不要动账号。

### 11d. 热循环（自旋）指纹与识别

- 指纹：`docker stats` 单容器 CPU≈100%，`docker logs` 里同一 key 同一行错误每秒数百条（2026-11-06 实测 570 行/秒，"zen key#N bound proxies unavailable"）。日志驱动器 `max-size:50m` 轮转会让 flood **"消失"几分钟再出现**——别把轮转误判成自愈，`--timestamps` 按小时聚合计数即可看穿。
- 本轮根因（已修 89c8bd5）：`callZenAPI` 的 `boundExitFor` 失败分支漏了 `retryKey = ""`（`callZenResponsesAPI` 同分支有——**平行修改漂移**是本项目两路径 bug 的固定成因，改 zen key 选择逻辑必须 chat/responses 两路径一起 grep）。审计老镜像时 `zen:<model>` 请求 150s 超时 + 容器 CPU 满 = 先查这个，别归因凭据。
- 连带伤害：`cooldownZenKey` 每次自旋迭代都落盘 → 写风暴；写失败与竞态（11e）会同时出现。

### 11e. 状态落盘并发竞态（已修 89c8bd5，后续审计按此核对）

- 生产日志 `replace <state>.json: rename <state>.json.tmp ...: no such file or directory` = 固定 `.tmp` 名并发写互踩的指纹。修复：`atomicWriteFile`（`internal/app/global_config.go`）按最终路径 `sync.Map` 互斥 + `os.CreateTemp` 唯一临时名。**Windows 教训：临时名唯一后并发 rename 覆盖同一目标仍会 Access denied，按路径串行化才算修完**（回归测试 `TestAtomicWriteFileConcurrentWriters` 抓的）。
- 同型固定 `.tmp` 写手仍在 `internal/amd/config.go+usage.go`、`internal/openrouter/usage.go`、`internal/other/config.go+usage.go`、`internal/workbuddy/config/config.go`（潜在，未爆）；`internal/app/pool.go`/`pool_usage.go` 已有 `poolSaveMu` 串行，不缺修复。审计若在非 app 平台看到同类 rename 报错，按同一处方修。

### 11f. 生产更新流程实操（2026-11-06 首次 agent 授权执行）

- 用户显式授权后：`docker pull ghcr.io/xd88dx/afree-proxy:latest` → `bash /docker/app/rebuild.sh afree`。**`rebuild.sh` 只做 compose stop/rm/up，不含 pull**——只跑脚本等于还在跑旧镜像。
- 判断生产是否落后：`git show -s --format='%h %ci' origin/main`（本地 fetch 后）对照 `docker images --format '{{.CreatedAt}}' ghcr.io/xd88dx/afree-proxy:latest`——镜像构建时间晚于 HEAD 提交时间 = 镜像包含该提交。
- 更新即验证：`docker stats`（CPU 应回 <1%）、`docker logs afree --since 2m` 看状态恢复行（zen usage/cline trace/proxy health 各一行 restored）、`/health` 200。重启会杀掉进行中的自旋/挂死 goroutine，也是现场取证前的"止血"手段（需授权）。

### 11g. 本轮结果快照（对照基线）

- 模型维度 23/24（唯一 FAIL `zen:glm-5.3-flash-free` 429 FreeUsageLimitError）；凭据维度 114/130（wb 15/15、trae 2/2、amd 10/10、tkhb 19/20、oprt 18/20、other 23/24、cline 17/19、zen 10/20）。16 个真实失败：官方限制 10（zen 9 key 日额度 UTC 零点复位 + oprt#12 账号被封 "Inference is blocked"，key 级 403 按设计不自动拉黑）+ 出口 6（11107 死×5 + 11104 被 GFE 拦×1）。出口池 34/39。
- 无死链映射别名，24 目标全可服务（zen 额度耗尽窗口内 `zen:glm-*` 除外，复位自愈）。

## 12. 2026-11-13 审计追加（全映射+全凭据轮，一缺陷已修）

- **cline Test 探针「必须流式」假阴性（已修）**：探针 `max_tokens=1` + 仅在 `modelNeedsStream` 时置 stream；目录字典序探测模型一旦必须流式（推理模型），非流式探针结构性 500 `empty response content` → 健康账号全部假阴性（实测 0/19）。修复=admin.go testAccount 命中 `clineEmptyStreamErr` 指纹时学习+立即改流式重试一次（与请求路径 cline_stream.go 同语义），回归测试 `TestAccountMustStreamProbeRetriesWithStream`。**fork 上游 4f12cd2 同款缺陷**（无指纹重试），属领先上游修复。
- **出口扫描必须用容器同款主机名**：`--socks5-hostname 127.0.0.1:<port>` 会把绑定了非环回接口的出口误判死（本轮 4/9 假阴性，11102/11120/11130/11135 实际经 `host.docker.internal` 全通）。判据：宿主机复扫用 `host.docker.internal:<port>`（Windows Docker Desktop 宿主上同样可解析）。
- **Test 端点解析口径差异（解析错=整平台假 FAIL）**：keypool/cline/other=`data.status`；**trae=vendored `data.ok`/`data.error`**（没有 status 字段，按 `data.status=='active'` 判会 4 账号全误报）；**wb=扁平 `status`/`error`**。抽测前先读 handler 确认字段。
- **newapi 映射含视频模型死链（已修，根因在 afree 不在映射）**：`wbcn/wbgb:kling-video-v3`、`wbcn:minimax-video-h3` 曾在 `/v1/models` 透出（vendor `video_generation: true` 单列，设计上「视频走 /v1/videos/generations」）但 afree 未挂 `/v1/videos`——**列出来的没有任何可达端点**，违反「列出来的必须调得通」；chat 探针上游回 11103 `Backend [maas] is not supported`（官方拒绝，三账号一致铁证）。根因=app 层 `workbuddyModelEntries` 的媒体条目费率同源筛选只接了 `image_generation`，vendor 后加的 `video_generation` 键漏接（2026-11-03「不按模态一刀切」机制的盲区）。修复=筛选扩到两类媒体键（回归测试 `TestWorkbuddyModelEntriesExcludeUnpricedVideoEntries`），目录/请求闸同源收紧：条目摘除 + 请求 404 not served。newapi 渠道映射应删去这三条目标（映射本就忠实镜像 afree 目录，勿归因为映射侧配置错误）。**审计教训：`/v1/models` 里的条目按字段分类盘点（image_generation/video_generation 等标记类），别只按前缀归类。**
- **GFE 403/EOF 一律复测再定性**（§6 经验再确认）：本轮 lovemcsf(11106) GFE 403、liuxingsv(11111) EOF 复测均 PASS——单次边缘抖动不是出口死。
- **newapi 渠道别名跨渠道同名**（如 `afree/glm` 同时映射 zen/wbcn 目标）：抽测按渠道映射值反查别名即可，同名别名在不同渠道语义不同，报告按渠道标注。
- **本轮结果快照**：模型维度 27/29（唯二 FAIL=kling 视频映射死链）；凭据维度修复后 cline 17/19（direct 绑定 1+死出口 11112 绑定 1，均环境非账号）、zen 18/20、amd 9/10、oprt 24/25、tkhb 24/25、crow 5/5、agnes 23/25、ddx 3/4、trae 2/4（1 direct 绑定+1 额度域 EOF，chat 域探针全 PASS）、wb 13/15（1 出口绑定不可用+1 TLS 瞬时）；newapi 全链路 2/2。出口池 41/46（真死 10110/11112/11115/11128/11136，仅 11112 有 5 处绑定）。
- **本机审计结论的适用边界（2026-11-13 用户追问后修正）**：本机容器 `DISABLE_DIRECT=true`+`ONLY_ROUTER=true`，而 iStore 生产两者皆未设——**凡「no usable egress/bound proxies unavailable（main=direct）」类 FAIL 都是本机环境产物**（本轮共 6 个 direct 绑定凭据：cline xd00dx、amd#0、agnes#0、ddx#0、wb 18210516867、trae 2745955326105609），生产 direct 可用（wb 18210516867 生产日志 04:59/05:00 实测 200 出字计费）。同理「死出口」清单（10110/11112…）是**本机端口转发拓扑**，生产有独立出口池与绑定，死出口拖累结论不外推。**跨环境成立的只有：凭据本身有效性（token/额度）、官方限制、项目缺陷。** 生产可用性如需审计级结论，只能被动观测生产日志（Test 端点会在生产记冷却，iStore 只读纪律不允许）。

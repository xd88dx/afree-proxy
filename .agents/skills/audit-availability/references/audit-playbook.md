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
- 可用替代（实测）：deepseek→`wbgb:`/`zen:deepseek-v4.1-flash-free`；kimi→`trae:kimi-k3`；glm→`zen:glm-5.3-flash-free`/`wbcn:glm-5.3-flash`/`trae:glm-5.3-flash`；muse 无等价只能删。
- `poolside/laguna-s-2.1:free` 不在 feed 但 cost=0（去兜底后已剪出目录；要用走 combo 别名）。

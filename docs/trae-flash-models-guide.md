# Trae 平台六个 flash 模型使用说明

> 2026-10-30 排查产出。基于本机测试容器(凭据1,端点 auto)实测 30 场景矩阵 +
> 六模型工具调用两轮往返全测。适用 afree-proxy 网关 `/v1/chat/completions`。

## 端点归属

这六个模型在 Trae 上游有三条传输档位,auto 模式下的路由规则(2026-10-30 起):

| 请求形态 | 主传输 | 回落 |
|---------|--------|------|
| 纯文字(无工具无图) | remote(chat_sessions,SOLO Agent 执行器) | traework → raw |
| **带工具** | **remote**(同上) | traework → raw |
| 带图片 | remote | traework → raw |
| 面板钉 `traecode` | raw(llm_raw_chat) | 无 |

**为什么工具请求改走 remote**:raw 端点(llm_raw_chat)对这六个 flash 系
config **恒 4001 "param invalid"**——与请求体形状无关(纯文本最小请求也
4001,已用网关同款 body/头/TLS 指纹逐项对齐复现)。这是上游按档位隔离了
flash 系 config,raw 档实际只对非 flash 系模型(glm-5.2、DeepSeek-V4-Pro 等)
可用。工具协议在 remote 档经文本化承载(运行时 system 提示 + `<opencode_tool_call>`
文本块),响应侧由网关解析回 OpenAI tool_calls。

面板端点选择建议:**保持 auto**。钉 `traecode` 后这六个模型的工具请求必败 4001;
钉 `remote` 一切正常但失去回落弹性。

## 六模型实测结论(2026-10-30,凭据1)

| 模型 | 上下文上限 | 短文本 | 长上下文 | 工具调用 | 多块输入 | 备注 |
|------|-----------|--------|---------|---------|---------|------|
| deepseek-v4.1-flash | 1M | ✅ | ✅(长问长答) | ✅ | ✅ | S4 会复述长文,建议加"直接给答案"约束 |
| glm-5.3-flash | 1M | ✅ | ✅ | ✅ | ✅ | 各场景最稳,agent 24 工具正常 |
| mimo-v2.6-flash | 981K | ✅ | ⚠️ | ✅ | ✅ | 长上下文可能生成超长输出(实测 273 万字符/44 分钟),**务必设 max_tokens** |
| minimax-m3.1-flash | 1M | ✅ | ✅ | ✅ | ✅ | 工具场景可能长篇思考(98k 字符思考流),慢但能收敛 |
| qwen3.8-flash | 1M | ✅ | ⚠️ 4000012 | ✅ | ✅ | 长上下文触发上游 "reasoning content loop detected"(4000012)概率高,重试可能换回 200;其余场景稳 |
| step-5-preview | 1M | ✅ | ✅ | ✅ | ✅ | 思考量最大(text≈reasoning 各 12k),工具场景最慢但结果完整 |

图例:✅ 稳定可用;⚠️ 有上游侧限制(见备注)。

### 按场景的可用性

1. **简单短请求**("123"之类):六模型全部可用,15-30s 出字。
2. **agent 工具调用**(LLM 作为 agent 后端):
   - 六模型全部支持标准 OpenAI tools 协议(网关翻译,客户端无需感知文本协议);
   - 发起调用 `finish_reason=tool_calls`、回传 result 后正常续答,两轮往返实测 12/12 通过;
   - 首轮调用受会话冷启动影响 15-45s,同会话续轮 12-31ms;**客户端工具调用超时建议 ≥120s**;
   - 工具数量实测 2-24 个无压力;54+ 个(生产事故规模)在 remote 档同样可能被上游掐流(EOF),大工具集客户端建议自行裁剪 schema(网关侧已做 48k 字符预算压缩兜底)。
3. **长上下文**:
   - 深度可用的:glm-5.3-flash / minimax-m3.1-flash / step-5-preview / deepseek-v4.1-flash;
   - mimo-v2.6-flash:输入长文本没问题,但模型可能把长文当写作素材产出超长回答(44 分钟/273 万字符),**必须设 `max_tokens`**(建议 ≤8192);
   - qwen3.8-flash:约 64KB 中文长输入有概率触发上游 4000012(reasoning loop),遇 502 且错误码 4000012 时换模型或精简输入,重试偶尔能过;
   - 多块 content(块数组/多 role 轮次):六模型全部正常。
4. **图片输入**:走 remote 档渲染器块传输,支持 OpenAI `image_url`(data URL / http URL)与 Anthropic `image` 块。

## 客户端接入注意

- **thinking 流**:六模型默认输出 `reasoning_content` 增量(兼容 DeepSeek-R1 形态),
  不需要思考流的客户端忽略该字段即可;
- **幂等重试**:网关对流断(EOF/超时)自动缓冲重试 + Work 档回落,客户端看到 502 时
  直接重发即可(轮换账号后通常成功);错误码 4001/4023/1005 的模型级失败已由网关
  自动封禁对应档位并从 /v1/models 摘除,不用反复撞;
- **qwen3.8-flash 的 4000012**:上游推理循环保护,属模型侧拒绝,网关会透传真实错误,
  换 glm-5.3-flash 或 step-5-preview 是稳妥替代;
- **max_tokens**:上游工具会话默认 200k 输出上限,不加约束的"分析这段长文"类请求
  可能跑几十分钟,mimo 尤甚——网关不替客户端截断。

## 生产事故对照(修复依据)

- 2026-10-29 15:22-18:53 生产 tools=54 的 agent 请求成簇 EOF 502:raw 档大请求 +
  flash 系 config 组合被上游掐流/拒绝;修复后工具请求主链路 remote,raw 仅回落,
  本机 24 工具 agent 场景稳定 200。
- 18:49-18:51 `step-5-preview` 4023、`deepseek-v4.1-flash` 4001:当时 auto 分流把
  工具请求钉在 raw 档,模型级失败被自动封禁连坐;修复后这两类错误不再由 raw 档触发。

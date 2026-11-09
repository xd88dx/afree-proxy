# AGENTS.md — 通用 agent 协作规则

使用中文和用户交流。

面向所有在本仓工作的 AI agent（Claude Code / Codex / Cline / OpenCode / Hermes 等）。

本文件是**通用规则**，可原样复制到其他项目复用。**项目专属规则见 `PROJECT.md`，动手前必读。如果不存在则提示用户**

## 1. Git 纪律

未经用户显式要求（“提交” / “推送” / “同步”），不要执行 `git add / commit / push / reset` 等写操作；只读的 `status / log / diff` 可以。用户说“同步” = 提交推送 + 更新本地测试环境（如果存在的话），两件事一起做完，不必再逐个确认。生产环境未经用户显式要求不能修改。

## 2. 提交前必须验证

`go build ./... && go vet ./...` 全绿，`go test ./... -count=1` 无**新增**失败。

- 审查 diff 代码是否与项目理念一致，是否有逻辑漏洞，是否符合项目规范，是否在完成新功能后遗留死代码。
- 报告失败前先证明因果关系：查失败包是否 import 了本次改动的代码。把根因写进台账，并在报告里明确区分“既有失败”与“本次引入”。
- 修复后必须复跑，不能只凭编译通过宣布完成。

## 3. 换行与格式化

**行尾由 `.gitattributes` + `.editorconfig` 强制统一为 LF**（唯一例外：Windows 原生脚本 `.ps1/.cmd/.bat` 用 CRLF；`entrypoint.sh` 必须 LF）。这两个文件是权威，不要凭 `core.autocrlf` 或编辑器默认值推断风格，也不要再手工做「反斜杠 r」的来回转换。

判据：`git ls-files --eol` 应全部为 `i/lf w/lf`。出现 `w/crlf` 或 `w/mixed` 就是工作区没按规则检出——`w/mixed`（同一文件一半 LF 一半 CRLF）最危险，`gofmt` 和 diff 都会把整文件翻成改动。

发现不一致时，先确认没有别人未提交的改动，再重整：

```bash
git status --short          # 有并行 agent 的改动时，不要往下走
git add --renormalize .     # 只按 .gitattributes 重整行尾，不改内容
git status --short          # 复核：应只列出纯行尾变化的文件
```

用 `git diff --stat` 复核：纯逻辑改动不应出现大量无关增删行。

工具路径参数按目标程序的语言给：原生 Windows 程序不认 MSYS 路径（`/tmp/...`），传 `$(cygpath -w <路径>)`。**不要**把工具 stderr 一起重定向进待写回的结果文件——工具报错时会用错误文本覆盖源文件。

## 4. 临时文件

临时脚本/日志放 `.tmp/` 或 `$TMPDIR`，不要在项目根目录散落。git 仓库需在 `.gitignore` 过滤临时目录与工具配置目录（`.vscode`、`.workbuddy` 等）。

长命令输出被截断时，重定向到日志文件再 `cat`，或用脚本直读文件——不要靠肉眼补全缺失的输出。

## 5. 删除规则

- 删用户数据、项目文件、配置 → 先向用户确认，且进回收站而非直接删。
- 删已确认无用的临时文件/构建缓存 → 可直接永久删除。
- 删代码前先给出**可复核的判据**，而不是“看起来像兼容代码”：
  - 引用面：`grep -rn '<符号>' --include=*.go .` 只剩定义处；
  - 触发条件：证明触发路径在真实数据/部署态下不可能成立（例如迁移代码只在“新文件缺失”时执行，而部署目录里新文件都在）；
  - 反向确认：运行时数据里仍在使用且值非空的字段属**活代码**，不得因“名字像旧版”而删。
- 删除后把判据与保留清单写进台账，避免后来者反复评估同一批代码。

## 6. 依赖与工具安装

决策顺序：① 先查本机是否已有该环境；② 再查 Docker 是否可用；③ 最后才问用户选容器还是装环境——不擅自替用户决定。

独立自动化工具放固定目录并登记台账（名称/版本/日期/完整路径/用途）；开发环境/工具链装在哪必须先问用户。不要为一次性验证污染全局环境。

## 7. 上游同步台账

若项目与上游存在 fork / vendor / 移植关系，用**追加式**台账记录每次同步：基线 → 目标、吸收项（带原因）、丢弃项（带原因）、本地定制保留清单、验证结果。历史只追加不删改。

合并前先判定改动属于哪个子系统/平台，按归属分节——一个上游仓库承载多个子系统是常态，把改动记到错误的节比不记更糟。

## 8. 禁止死配置

不要把没有路由入口或调用方的能力暴露成配置开关。判据是“该端点/函数是否真的可达”，而不是“上游有没有实现”。面板上出现一个永远无效的开关，等于给用户一个会撒谎的控件。

## 9. 技能与文档同步

改动波及 agent 技能文档（`.agents/skills/` 等）时一并修正，包括会误导后续 agent 的过时指引，不必等用户逐项批准。

## 10. 外网连接与代理

本机出网不可达时尝试使用代理。五个候选的**实测结论（2026-10-28 12:39）**，用前先探、通即用——代理服务端的分流规则会变，本表有保质期：

```bash
for p in 127.0.0.1:10086 127.0.0.1:7890 127.0.0.1:10808 10.10.10.5:10086 10.10.10.3:1080; do
  curl -s -o /dev/null -w "$p %{http_code} %{time_total}s\n" \
    -x "socks5h://$p" --max-time 10 https://api.github.com/
done
```

判据：`http_code=200` 且耗时正常才算通；`000` 且耗时接近 `--max-time` 就是不可用。

| 代理               | 支持协议                | api.github.com | gstatic/generate_204 | 其他实测                                                        | 结论                                       |
| ------------------ | ----------------------- | -------------- | -------------------- | --------------------------------------------------------------- | ------------------------------------------ |
| `127.0.0.1:10086`  | socks5 / socks5h        | 200，0.23s     | 204，0.21s           | raw.githubusercontent 301；api.openai.com 401（能到达即链路通） | **首选**：本机 mihomo，全场最快            |
| `127.0.0.1:10808`  | socks5 / socks5h / http | 200，0.43s     | 204，0.80s           | raw 301；openai 401                                             | 备选：稳定，延迟略高                       |
| `127.0.0.1:7890`   | socks5 / socks5h / http | 200，0.42s     | 204，1.79s           | —                                                               | 备选：GitHub 快，通用墙外明显偏慢          |
| `10.10.10.5:10086` | socks5 / socks5h        | 200，0.42s     | 204，0.41s           | —                                                               | 备选：内网 iStore 上的 mihomo（host 网络） |
| `10.10.10.3:1080`  | socks5 / socks5h        | 200，1.59s     | 204，0.30s           | —                                                               | 兜底：链路通但慢                           |

- 默认走 `127.0.0.1:10086`；不通时按 `127.0.0.1:10808` → `127.0.0.1:7890` 回退。
- **别用「端口在听」「TCP 能连上」判代理可用**——端口开、转发死是常态。也别说"设了 `ALL_PROXY` 就能用"：Windows schannel 下环境变量代理对 git 无效（报 `schannel: failed to receive handshake`），`http.sslBackend=openssl` 也救不回来。

git 出网（HTTPS）必须用 **URL 级**代理键：

```bash
git -c 'http.https://github.com.proxy=socks5h://127.0.0.1:10086' \
    -c 'http.proxy=socks5h://127.0.0.1:10086' \
    -c 'https.proxy=socks5h://127.0.0.1:10086' fetch <remote> main
```

git 推送（`origin` 是 SSH）走另一条路——上面三个代理键对 push 无效：

**首选：`ssh.github.com:443` + PySocks 隧道**（2026-10-31 实测）。`/mingw64/bin/connect`
已随 Git 更新消失，`ProxyCommand connect` 会报 `Connection closed by UNKNOWN port
65535`（易误判为密钥/网络问题）；直连 22 端口也会被重置。系统 python 无 PySocks，
用 `C:/AppSelf/Python/python.exe`：

```bash
# ① 隧道脚本放纯 ASCII 路径。**Git Bash 的 $TEMP 是 /tmp（MSYS），不是 Windows
#    %TEMP%**——`cat > "$TEMP/..."` 会把脚本写到 MSYS tmp，而 ProxyCommand 引用
#    C:/Users/<user>/AppData/Local/Temp/ 找不到文件、python 秒退（2026-11-01 实测）。
#    必须写显式 Windows 路径。
#    脚本用 socks.socksocket 连 127.0.0.1:10086 双向泵 stdin/stdout↔socket，
#    **stdin 侧必须 read1()/os.read()**——read(n) 会阻塞凑满字节数，SSH banner 死锁；
#    **socket 侧必须 recv()/sendall()**——Windows 上 os.read(s.fileno()) 对 socket
#    handle 无效，立即 OSError 静默退出（同报错，2026-11-01 实测第二坑）。
# ② 认证测试（实测 2026-10-31）：
ssh -o IdentitiesOnly=yes -o IdentityFile="C:/Users/dxing/.ssh/id_ed25519_xd88dx" \
  -o ProxyCommand="C:/AppSelf/Python/python.exe C:/Users/<user>/AppData/Local/Temp/socks_tunnel.py %h %p" \
  -p 443 -T git@ssh.github.com   # 应回 Hi <user>! You've successfully authenticated
# ③ 推送：
git -c core.sshCommand='ssh -o IdentitiesOnly=yes -o IdentityFile="C:/Users/dxing/.ssh/id_ed25519_xd88dx" -o ProxyCommand="C:/AppSelf/Python/python.exe C:/Users/<user>/AppData/Local/Temp/socks_tunnel.py %h %p" -p 443' push origin main
```

密钥→账号实测映射（443 隧道，2026-10-31）：`id_ed25519_xd88dx`→xd88dx（本仓库）、
`id_ed25519_xdserv`→xdserv、`id_ed25519_xdserva`→xdserva、`id_ed25519_xxx`→cuegita；
`id_ed25519_vps`、`id_ed25519_nat` 无 GitHub 权限。早前"xd88dx 未注册成功"是 22
端口被重置造成的误判。隧道脚本用完即删。

## 11. iStore 生产环境只读（2026-10-29 用户定）

iStore（`root@10.10.10.5`，SSH 密钥 `~/.ssh/id_ed25519_nat`）上跑着生产容器（当前是 `afree`，未来可能有其他），**未经用户显式同意，禁止做任何修改性操作**。允许的操作：

- 日志读取（`docker logs`、读数据目录里的日志/落账文件）；
- 状态查询（`docker ps` / `inspect` / `images` 等只读命令）；
- 只读探测（curl 探活等，不改变服务状态）。

**禁止**：创建/删除/重建任何容器、镜像；增删改数据目录文件；改配置、改 env、开关面板功能；上传文件；任何测试性部署。**所有修改与测试一律在本机（Windows Docker）执行**——本机 afree 容器（端口 3457）是测试环境，生产镜像流程与生产部署必须用户明确指示后才动。

如果任务看似需要在 iStore 上验证，先停下来问用户，不要自行创建任何资源。

<!-- aoci:begin -->
## AOCI 仓库认知

AOCI 为本仓库维护一个稳定、可版本化、可增量更新的仓库级认知层，供模型跨任务复用对系统的理解。

`aoci.txt` 是面向模型的结构化认知索引。它以每个受管理文件、数据库表或其他受管理对象一条独立 Entry 的方式，用符号标签与 F/R/A/S 语义表达对象的核心职责、重要关系、对外契约，以及理解或修改系统时必须知道的非显然约束和设计决策。

Header、目录段和全部 Entry 共同组成完整仓库索引，可以覆盖前端、后端、配置、数据库结构及其他受管理内容。受管理内容发生变化时，通常只需维护受影响的认知条目，不需要重新生成整个索引。

AOCI 提供系统架构、对象职责、重要关系、对外契约和关键约束的高密度视图。

### 工作原理

AOCI 采用“模型生成、模型读取”的认知闭环。

Header、Entry 和 Curation 语义的创作只按当前机器签发的 Plan 与实时 Guide 执行；由 Host 模型基于当前绑定证据独立完成。

Entry 的语义必须来自模型对真实证据的理解。不得仅依据路径、文件名、扩展名、AST、符号列表、依赖扫描、正则、固定模板或规则引擎推导、预填、拼接或改写索引语义。

对 Fresh Bootstrap，只按当前机器签发的 Plan 和实时 Guide 执行。当它们要求创作时，Host 模型创作 Root、Meta、Tag 和 F/R/A/S，提供 authoring-run 声明，并把它绑定到 Plan、Evidence 与完整 Candidate。不得要求 AOCI 填写 `origin=host_model`、制造 Receipt 或把程序生成的 Framework 当作语义。本文件不自行重建 Onboarding 流程。内部批次不是用户决策；只有遇到既有批准边界或真实的安全、漂移、CAS、Recovery 条件才停止。

### 最小使用入口

- `aoci_rules`：取得当前AOCI版本的会话运行合同。
- `aoci_overview`：建立或恢复本仓库的完整认知。
- `aoci_maintain`：受管理对象达到最终稳定状态后检查认知是否需要维护。
- `aoci_update_entry`：提交与当前证据和源码摘要绑定的完整语义更新批次。
- `aoci_report`：仅当当前布局和工具状态支持时，在证据不足、无法可靠生成语义时登记待办，不猜写。

其他MCP工具、CLI命令、参数和专项流程，以当前工具说明、Guide和 `--help` 返回内容为准，不在本文件中重复完整手册。

本区块只规定仓库接入、认知使用和收尾原则。`aoci_rules` 承载当前会话合同，Guide实时输出承载当前Plan的执行顺序与停点，工具Schema、Spec和Validator承载机器结构与判据；Prompt、Description、README和静态文档不能覆盖这些机器事实。

### 建立、生成和恢复认知

1. 每个新的 Agent Run 开始时，应先判断：

   - 本仓库是否已经存在可用的完整AOCI索引；
   - 当前上下文中是否已有与本仓库根、当前索引版本和当前AOCI服务相匹配，并且模型仍可可靠使用的完整仓库认知。

2. 仓库已经存在可用的完整索引，但当前Run没有可靠完整认知时，先调用 `aoci_rules`，再调用 `aoci_overview`。

   完整认知仍可靠时直接复用。局部不确定本身不要求机械重读系统全貌。可靠认知建立后，默认以它作为仓库级系统上下文，只读取当前任务直接需要的源码、测试、配置与契约，只有某个缺失的具体事实可能改变回答或验证结论时才扩大证据范围；AOCI不能替代修改对象与验证行为所需的直接源码证据。

   本Run从已知Host上下文压缩恢复时（包括宿主注入的压缩摘要），必须把此前模型认知视为不可靠。压缩handoff不得保留或摘要正式Whole-Index，也不得保留或摘要任何Overview Header、Entry、Chunk、Challenge或Attestation正文；只能保留安全续接所需的receipt身份、未完成write或Recovery状态，以及立即重载指令。复制进handoff的Whole-Index语义或receipt不能证明恢复后模型的当前认知可靠。若当前上下文已无法可靠保留运行合同，先调用 `aoci_rules`。继续业务任务前，使用 `refresh_reasons=["context_compaction"]` 和新的 `refresh_event_id` 调用普通完整Whole-Index `aoci_overview`（不设置 `check_only` 或设为false）；不得使用 `check_only` 或认知probe。原样跟随每个 `next_cursor` 直到 `completed=true`，确认交付，并且只基于新交付正文提交一次Attestation。完成这次新的完整传输后，即使Attestation为partial或fail也消费该generation，并按既有合同继续source-bound任务，不再自动调用第二次Overview。若该请求返回 `delivery_mode=capacity_stop`，说明Whole-Index装不进本Host的上下文：不得重试或调小 `chunk_tokens`；该停止消费本generation，任务按source-bound继续，且不作无保留的完整系统认知声明。

   AOCI可以针对 `context_compaction`、项目 `cognition_refresh_threshold` 下的机器 `semantic_threshold` 或主要 `phase_transition` 提供checkpoint与认知状态事实。只需要这些紧凑事实时使用 `check_only=true`；这些事实只向Agent提供建议，不替模型决定是否需要系统全貌。

   Agent显式调用普通 `aoci_overview`（未设置 `check_only` 或为false）时，只要能形成一致的CognitionSet，AOCI必须完整交付请求scope。不得因为已有receipt、阈值未达到或没有待处理刷新原因而抑制正文；唯一例外是待处理压缩刷新的上述容量停止。正式认知Dirty或Stale时仍交付正文，但必须标记不可靠。存在未决恢复或无法形成一致snapshot时失败关闭，不返回混合正文。

   普通Overview返回 `continuation_required=true` 时，必须原样提交 `next_cursor` 并自动继续到 `completed=true`。不得询问用户、开始业务任务或给出阶段性系统结论。Host截断、缺块、重复、乱序、cursor失败、Index变化或`chunk_tokens`变化时停止本次认知链。Attestation完成前不得用Memory、源码、Spec、`aoci.txt`、历史会话、scope、search或Entry读取修补或补充Whole-Index认知。Challenge ordinal是正式Entry序列中的1-based位置；Header内容、注释、空行、Section/Overview/Chunk Marker、Receipt与Metadata均不计数，Chunk Receipt ordinal使用同一序列。Attestation必须原样回绑本次Challenge发布的当前`index_sha256`、`entry_sequence_sha256`与`entry_count`；旧Index、旧Entry序列、旧数量或旧Attestation均无效。完整链结束后只正式提交一次既有模型认知Attestation；同一响应只允许一次不改变语义答案的JSON Schema或字段格式修正。对象、Tag或F不匹配即失败且认知吸收不确定，不得语义重试或旁路补答。首次认知失败时还不得执行Root/Meta、Migration、全局布局或其他未重新绑定的系统级决策。上下文压缩刷新若传输完整、认知身份不变、治理对齐且没有Recovery或第三方冲突，即使Attestation为partial或fail也消耗该refresh generation，并继续原任务，不再自动重读Overview。`system_mastery_percent`只自评系统框架——架构、职责、强关系、稳定外部契约以及高熵安全和维护约束——不表示完整实现或运行实况知识；机器索引覆盖率必须分开。默认只向用户输出由本次真实覆盖率、Challenge、块数、Token和掌握度生成的规定成功或失败一句话。Host截断时提示用户把 `overview_delivery.chunk_tokens` 设置为更小的合法值后重新开始，不得自动修改。

   加法认知等级必须与严格证明字段分开解释。`delivery_verified`表示已加载Index且Host交付已确认，但完整认知验证仍未完成；应表达为“已加载且交付已验证”，不得描述为“没有认知”或“没有理解系统”。`cognition_verified`要求Attestation通过（Challenge至少80%的ordinal完全正确且对象身份至多失手一处），`cognition_governed`还要求治理对齐。通用完整读取失败句只用于真实交付故障。

   当Overview响应包含可选`cognition-state/v2`投影时，必须分别解释各维度。其Level止于`model_cognition_usable`；`strict_attestation_verified`、`governance_aligned`与`current_system_cognition_reliable`都是独立状态，绝不参与该Level。ordinal、对象身份、Tag或核心F不匹配可以导致严格Attestation失败，而模型认知仍然可用；不得仅凭这种不匹配就宣称模型没有理解系统。只有`current_system_cognition_reliable=true`允许无保留地声称当前完整系统认知可靠。投影缺失时继续使用上述Legacy解释。

   普通的只读审计、分析、检查、不修改代码或不提交、不push，不自动等于严格零写入，也不改变上述认知有效性判断。Codex Memory和历史Skill只能辅助恢复经验、用户偏好与调查方向，不能替代与当前仓库根、索引摘要、AOCI服务身份和认知范围匹配的当前认知收据；项目AGENTS和当前AOCI身份在AOCI状态上优先于历史Memory。

   只有用户明确禁止Ledger、元数据、`.aoci`运行资产及任何文件写入时，才按严格零写入处理。若必要的认知建立与该边界冲突，必须报告冲突并请求用户裁决或建议使用隔离副本，不得静默以Memory替代当前仓库认知。

3. 仓库没有可用的完整索引，或当前只有最小骨架、Header不完整、Entries未完成、必要Curation尚未裁决时，如果需要建立正式完整AOCI索引，先取得 `aoci_rules`，然后进入当前AOCI Guide。由Guide依据仓库真实状态决定下一阶段并完成必要安全步骤。

   `aoci_maintain` 不替代索引建立流程。

   不在本文件中自行重建或硬编码完整索引生成状态机。

4. 在长程任务中，模型负责保留当前认知收据并正确使用刷新门禁：

   - Host报告上下文压缩或模型已知系统全貌丢失时，执行上述强制 `context_compaction` 重载规则；AOCI不能自行推断Host事件；
   - 进入真正的主要阶段时声明 `phase_transition`，不得把函数、测试运行或小步骤当作阶段；
   - 在有用的稳定检查点通过 `check_only=true` 取得机器语义计数；
   - 除已知压缩的强制重载外，由Agent判断当前任务是否需要再次显式获取指定scope或完整Overview；
   - 在维护和对齐完成前，保留AOCI报告的Dirty或Stale可靠性状态。

### 任务收尾与认知维护

5. 纯只读问答、分析、版本核验，或没有产生受AOCI管理对象变化的任务，不需要调用维护工具。当前AOCI版本是任意`aoci_overview` check_only或`aoci_maintain`响应里的`cognition_receipt.mcp_service_version`；二进制路径是项目`.mcp.json`里的`command`，CLI不必在PATH上。

6. 发生受AOCI管理对象变化时，待其达到本次任务的最终稳定状态后，只调用一次 `aoci_maintain`。不要在每次中间修改后逐文件维护。

7. 若维护结果返回真实语义候选，Host 模型必须基于每个候选绑定的对象和必要证据，独立创作完整标签与F/R/A/S更新。通过 `aoci_update_entry` 一次提交当前机器签发批次的完整候选集合，同时原样保留每项 `source_sha256`、`candidate_id` 与对应domain批次身份。`max_entries`只限制单次请求和原子事务，不限制logical plan、Whole-Index或Managed Scope。`remaining`非零时，在当前批次成功Apply后重新调用Maintain并从新preimage继续；绝不能为满足transport上限缩减Index覆盖或自行截取返回批次。

   没有足够证据且当前布局支持 `aoci_report` 时，使用它而不猜测、套用模板或为消除待办而生成缺乏证据的认知。

8. 必须遵守工具返回的结构化状态和安全边界：

   - `repair_required`：只修复明确命中的候选，再重新提交当前机器签发的完整批次；
   - `stopped`：结束当前写入尝试并检查 `failed_step`、错误、正式写入证据与Recovery。auto模式下，已证明零写入则记录closure并重新Plan；完整Intent和可证明postimage则Resume；策略要求Rollback且preimage可证明则精确恢复后重新Plan。只有证据不足、第三方正式字节冲突、需要审批或外部动作，或命中其他真实安全边界时，才停止整个用户任务；
   - 冲突、审批、人工裁决、权限和安全信号不得忽略；
   - 已经对齐后不得重复维护或重复写入；`refresh_ready_for_overview` 是checkpoint事实，由Agent决定是否为下一阶段请求普通完整Overview。

   维护完成后如果又修改了任何受管理对象，之前的维护结果失效，应在新的最终稳定状态重新完成收尾。

9. 用户只限制业务文件范围，但没有明确禁止仓库托管资产时，AOCI托管资产可以在收尾阶段为保持认知一致而更新，并应在审计和提交中与业务文件区分。

   用户明确禁止修改 `aoci.txt`、`.aoci`、元数据或任何额外文件时，以用户限制为准，不得写入，并如实报告剩余不一致。

### 专项流程

初始化、完整索引生成、Header生成、Entries生成、数据库结构索引、Curation、人工评审和故障恢复，只按当前AOCI Guide或工具在对应阶段返回的指令、命令和安全停点执行。

不预加载、不猜测，也不自行重建这些专项流程。平台调用方式、请求格式、批次上限、审批规则、索引格式细节和恢复步骤由对应Guide、工具说明、模型Prompt和CLI帮助按需提供。
<!-- aoci:end -->

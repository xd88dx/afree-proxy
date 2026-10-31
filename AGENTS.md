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
# ① 隧道脚本放纯 ASCII 路径（%TEMP% 即可；仓库中文路径会让 ProxyCommand 无输出）。
#    脚本用 socks.socksocket 连 127.0.0.1:10086 双向泵 stdin/stdout↔socket，
#    **stdin 侧必须 read1()/os.read()**——read(n) 会阻塞凑满字节数，SSH banner 死锁。
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

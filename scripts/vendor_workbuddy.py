#!/usr/bin/env python3
"""Vendor the WorkBuddy runtime packages into this repository.

The upstream project keeps its reusable packages under internal/. The vendored
tree is a standalone Go module whose module path matches upstream
(github.com/misakano7545/workbuddy2api-panel), so imports are copied verbatim
— no prefix rewriting. The tree lands at modules/workbuddy2api/internal/; Go's
internal visibility rule keeps the main module out, and the only consumption
surface is the root facade modules/workbuddy2api/afree_bridge.go
(EXTRA_ROOT_PRESERVE).
"""

from pathlib import Path
import shutil
import sys


# 上游工作树（misakano/main 检出，git worktree of D:\下载\Github\workbuddy2api-panel）。
# 2026-10-23 起平台功能取自 misakano7545/workbuddy2api-panel（接续上游）。
SOURCE_ROOT = Path(r"D:\下载\Github\workbuddy2api-panel-misakano")
TARGET_ROOT = Path(r"D:\下载\Github\afree-proxy")
TARGET_PKG = TARGET_ROOT / "modules" / "workbuddy2api" / "internal"

# 2026-11-17 起不再做 import 前缀改写：vendor 树独立 module 化后 module 路径
# 与上游一致（misakano7545），上游 import 原样保留。旧基线若仍是 linguo2625469
# 模块路径（2026-10-21 停维，基线 73a7560 及之前），三方合并前须先换基线——
# 当前基线（10d01b4，misakano）不受影响。

PACKAGES = (
    "auth",
    "balance",
    "credithist",
    "cst",
    "httpauth",
    "livecfg",
    "loginsite",   # 2026-10-25 上游新增：官方域表/登录码语义（CLI 2.174.0）
    "updater",     # 2026-10-25 上游新增：面板一键更新（引用方 panel/update.go）
    "pool",
    "reqlog",
    "safefile",
    "server",
    "upstream",
    "scheduler",
    "panel",
    "prompt",
    "session",
    "usage",
    "logfmt",
    "redisstore",
    "proxypool",   # 2026-11-02 上游新增：代理池管理（PR #122/#135，被 upstream/panel/server 引用）
    "media",       # 2026-11-05 上游新增：水印裁切（server/watermark.go 引用；PR #162/#168）
    "debugcap",    # 2026-11-10 上游新增：调试抓包（server/responses.go 引用）
    "jsonscan",    # 2026-11-10 上游新增：JSON 扫描件（prompt/session/upstream/kimi_tools 引用）
)

ASSET_SUFFIXES = (".go", ".md", ".json", ".html", ".js", ".css", ".txt")

# Runtime fields consumed by the afree-proxy egress adapter in upstream/proxy.go.
# Keep them in the vendored Client rather than preserving the whole upstream file,
# so upstream client changes still flow through future re-vendors.
PROXY_FIELD_ANCHOR = "\tChatHTTP *http.Client\n\n\t// HeaderTimeout"
PROXY_FIELD_BLOCK = """\tChatHTTP *http.Client

\t// ProxyFor resolves the account-bound egress proxy per request; nil keeps
\t// the upstream project's direct-connection behavior.
\tProxyFor        ProxyFunc
\tproxyMu         sync.Mutex
\tproxyTransports map[string]*http.Transport

\t// HeaderTimeout"""

# Account-authenticated upstream calls must resolve the stable per-account exit.
# 上游 40d98f9（PR #122/#135）起出站收敛到 c.do(req, a, chat)（upstream/proxy.go），
# 本仓 PRESERVE 自己的 proxy.go 并在其中实现同签名 do()：先走 ProxyFor 按账号
# 钉定出口（fail-closed + 观测），ProxyFor 未挂时回落上游 ProxyPool/直连语义。
# 改写规则因此按「两棵树」维护：基线树规则保证 base==ours（历史合并产物形态），
# 新树规则把 theirs 统一成本地 (a, req) 参数序——两侧一致则三方合并零冲突。
# 计数一律元组：同一锚点在两棵树的命中数不同，元组列出两树各自可接受的计数。
EGRESS_REWRITES = {
    "upstream/client.go": (
        # 基线树：doJSON 无 auth 参；新树：auth 参在尾。两条规则分别命中，
        # 把 doJSON 签名统一成本地 (a, req) 序。
        ("func (c *Client) doJSON(req *http.Request)", "func (c *Client) doJSON(a *auth.Auth, req *http.Request)", (1, 0)),
        ("func (c *Client) doJSON(req *http.Request, a *auth.Auth)", "func (c *Client) doJSON(a *auth.Auth, req *http.Request)", (0, 1)),
        ("c.HTTP.Do(req)", "c.doHTTP(req, a)", (3, 0)),
        ("data, err := c.doJSON(req)", "data, err := c.doJSON(a, req)", (1, 0)),
        # 新树 doJSON 调用点（含 client.go 内部 caller）
        ("c.doJSON(req, a)", "c.doJSON(a, req)", (0, 1, 2)),
        # 上游 chat 出站失败清空闲连接：钉定出口时要清代理 Transport 而非基础
        # Transport（两树该字面量各 1 处，closeIdleEgress 两树通用）。
        ("roundTripCloseIdle(c.chatHTTP().Transport)", "c.closeIdleEgress(a)", (1, 1)),
        # afree 本地定制：billing 回落 WARN 按账号限频（WarnThrottled 由 PRESERVE
        # 的 upstream/proxy.go 提供）。出口死/上游抖动的持续性失败单账号每小时
        # 可刷 60+ 条 WARN，60s 限频降噪不吞首报（2026-11-28 生产实测）。
        ('\t\tlog.Printf("WARN: billing %s: global summary 失败，回落个人资源包路径: %v",',
         '\t\tWarnThrottled("billingsum:"+a.UID, "billing %s: global summary 失败，回落个人资源包路径: %v",', (1, 1)),
    ),
    "upstream/desktop.go": (
        ("c.doJSON(req)", "c.doJSON(a, req)", (4, 0)),
        ("c.doJSON(req, a)", "c.doJSON(a, req)", (0, 4)),
        ("c.HTTP.Do(req)", "c.doHTTP(req, a)", (1, 0)),
    ),
    "upstream/global_models.go": (("c.HTTP.Do(req)", "c.doHTTP(req, a)", (1, 0)),),
    "upstream/global_register.go": (
        ("func (c *Client) globalRegisterJSON(req *http.Request)", "func (c *Client) globalRegisterJSON(a *auth.Auth, req *http.Request)", (1, 0)),
        ("func (c *Client) globalRegisterJSON(req *http.Request, a *auth.Auth)", "func (c *Client) globalRegisterJSON(a *auth.Auth, req *http.Request)", (0, 1)),
        ("c.HTTP.Do(req)", "c.doHTTP(req, a)", (1, 0)),
        ("c.globalRegisterJSON(req)", "c.globalRegisterJSON(a, req)", (3, 0)),
        ("c.globalRegisterJSON(req, a)", "c.globalRegisterJSON(a, req)", (0, 3)),
    ),
    "upstream/report.go": (
        ("c.doJSON(req)", "c.doJSON(a, req)", (0, 0)),
        ("c.doJSON(req, a)", "c.doJSON(a, req)", (2, 1)),
    ),
    "upstream/school.go": (
        ("data, err := c.doJSON(req)", "data, err := c.doJSON(a, req)", (1, 0)),
        ("data, err := c.doJSON(req, a)", "data, err := c.doJSON(a, req)", (0, 1)),
    ),
    "upstream/tasks.go": (
        ("c.doJSON(req)", "c.doJSON(a, req)", (3, 0)),
        ("c.doJSON(req, a)", "c.doJSON(a, req)", (0, 3)),
    ),
    "upstream/travel.go": (
        ("c.doJSON(req)", "c.doJSON(a, req)", (1, 0)),
        ("c.doJSON(req, a)", "c.doJSON(a, req)", (0, 1)),
    ),
    "upstream/profile.go": (
        ("data, err := c.doJSON(req)", "data, err := c.doJSON(a, req)", (1, 0)),
        ("data, err := c.doJSON(req, a)", "data, err := c.doJSON(a, req)", (0, 1)),
    ),
    # misakano 新增文件；doJSON 调用各 1 处，按字面量整行匹配防误伤。
    "upstream/modelbusy.go": (
        ("data, err := c.doJSON(req)", "data, err := c.doJSON(a, req)", (1, 0)),
        ("data, err := c.doJSON(req, a)", "data, err := c.doJSON(a, req)", (0, 1)),
    ),
    "upstream/mp_events.go": (
        (("_, err = c.doJSON(req)"), "_, err = c.doJSON(a, req)", (1, 0)),
        (("_, err = c.doJSON(req, a)"), "_, err = c.doJSON(a, req)", (0, 1)),
    ),
    # 142790b（2026-11-27 生产泄漏实锤适配）：DSML 单全角竖线漂移双形态兼容。
    # 上游活跃文件走 REWRITES 保吸收能力，本地块按锚点插入/改写登记。
    "upstream/dsml.go": (
        ('const markupPipe = "\\uFF5C\\uFF5C"',
         'const markupPipe = "\\uFF5C\\uFF5C"\n\n'
         '// markupPipeLoose 单全角竖线（2026-11-27 生产实证的漂移形态）：模型端序列化\n'
         '// 从 `<｜｜DSML｜｜ X>`（双竖线）漂移为 `<｜DSML｜ X>`（单竖线+空格），两种\n'
         '// 形态并存兼容——不能替换，旧形态仍可能在部分请求出现。\n'
         'const markupPipeLoose = "\\uFF5C"', (1, 1)),
        ('\tmarkupParamClose  = "</" + markupPipe + "DSML" + markupPipe + " parameter>"\n)',
         '\tmarkupParamClose  = "</" + markupPipe + "DSML" + markupPipe + " parameter>"\n\n'
         '\tmarkupOpenLoose        = "<" + markupPipeLoose + "DSML" + markupPipeLoose + " calls>"\n'
         '\tmarkupCloseLoose       = "</" + markupPipeLoose + "DSML" + markupPipeLoose + " calls>"\n'
         '\tmarkupInvokeCloseLoose = "</" + markupPipeLoose + "DSML" + markupPipeLoose + " invoke>"\n'
         '\tmarkupParamCloseLoose  = "</" + markupPipeLoose + "DSML" + markupPipeLoose + " parameter>"\n)', (1, 1)),
        ('// markupTagPrefix 是开始标记的公共前缀（已转义，供下面两个正则复用）。',
         '// markupCloseFor / markupInvokeCloseFor / markupParamCloseFor 按 open 形态取同族\n'
         '// 的 close——块的收尾与开头同一序列化族（模型一次生成一个族），配对不跨族。\n'
         'func markupCloseFor(open string) string {\n'
         '\tif open == markupOpenLoose {\n'
         '\t\treturn markupCloseLoose\n'
         '\t}\n'
         '\treturn markupClose\n'
         '}\n\n'
         'func markupInvokeCloseFor(open string) string {\n'
         '\tif open == markupOpenLoose {\n'
         '\t\treturn markupInvokeCloseLoose\n'
         '\t}\n'
         '\treturn markupInvokeClose\n'
         '}\n\n'
         'func markupParamCloseFor(open string) string {\n'
         '\tif open == markupOpenLoose {\n'
         '\t\treturn markupParamCloseLoose\n'
         '\t}\n'
         '\treturn markupParamClose\n'
         '}\n\n'
         '// markupTagPrefix 是开始标记的公共前缀（已转义，供下面两个正则复用）。', (1, 1)),
        ('var markupTagPrefix = regexp.QuoteMeta("<" + markupPipe + "DSML" + markupPipe)',
         'var markupTagPrefix = regexp.QuoteMeta("<" + markupPipe + "DSML" + markupPipe)\n\n'
         '// markupTagPrefixRe 两族开始标记公共前缀（双竖线 | 单竖线），后接空白。\n'
         'var markupTagPrefixRe = `(?:` + regexp.QuoteMeta("<"+markupPipe+"DSML"+markupPipe) +\n'
         '\t`|` + regexp.QuoteMeta("<"+markupPipeLoose+"DSML"+markupPipeLoose) + `)\\s+`', (1, 1)),
        ('\tmarkupInvokeRe = regexp.MustCompile(markupTagPrefix + `\\s+invoke\\s+name="([^"]*)"\\s*>`)\n'
         '\tmarkupParamRe  = regexp.MustCompile(markupTagPrefix + `\\s+parameter\\s+name="([^"]*)"(?:\\s+string="(true|false)")?\\s*>`)',
         '\tmarkupInvokeRe = regexp.MustCompile(markupTagPrefixRe + `invoke\\s+name="([^"]*)"\\s*>`)\n'
         '\tmarkupParamRe  = regexp.MustCompile(markupTagPrefixRe + `parameter\\s+name="([^"]*)"(?:\\s+string="(true|false)")?\\s*>`)', (1, 1)),
        ('\tinBlock      bool   // 已消费掉 markupOpen，正在块内部累积',
         '\tinBlock      bool   // 已消费掉 markupOpen，正在块内部累积\n'
         '\tinBlockOpen  string // 当前块的开头形态（markupOpen / markupOpenLoose）——rejected/超长回吐必须用原形态，否则污染正文', (1, 1)),
        ('\t\t\ti := markupStartCandidate(s)',
         '\t\t\ti, open := markupStartCandidate(s)', (1, 1)),
        ('\t\t\tif len(s)-i < len(markupOpen) {',
         '\t\t\tif open == "" {', (1, 1)),
        ('s = s[i+len(markupOpen):]\n\t\t\tr.inBlock = true',
         's = s[i+len(open):]\n\t\t\tr.inBlock = true\n\t\t\tr.inBlockOpen = open', (1, 1)),
        ('k := strings.Index(s, markupClose)',
         'k := strings.Index(s, markupCloseFor(r.inBlockOpen))', (1, 1)),
        # 两处（超长未闭合回吐 + rejected 原文回吐）统一改为当前块形态回吐。
        ('emit.WriteString(markupOpen)',
         'emit.WriteString(r.inBlockOpen)', (2, 2)),
        ('s = s[k+len(markupClose):]',
         's = s[k+len(markupCloseFor(r.inBlockOpen)):]', (1, 1)),
        ('emit.WriteString(markupClose)',
         'emit.WriteString(markupCloseFor(r.inBlockOpen))', (1, 1)),
        ('out = markupOpen + r.hold',
         'out = r.inBlockOpen + r.hold', (1, 1)),
        ('// markupStartCandidate 返回 s 中最早的「可能是 markupOpen 起点」的下标：该位置之后\n'
         '// 要么正好是完整的 markupOpen，要么是 markupOpen 的一个前缀（被增量边界截断）。\n'
         '// 没有候选返回 -1。\n'
         'func markupStartCandidate(s string) int {',
         '// markupStartCandidate 返回 s 中最早的「可能是任一族 markupOpen 起点」的下标与\n'
         '// 命中的 open：该位置之后要么正好是完整的某个 open，要么是某个 open 的一个前缀\n'
         '// （被增量边界截断——此时 open 返回空串，调用方只 hold 不消耗）。没有候选返回 -1。\n'
         'func markupStartCandidate(s string) (int, string) {', (1, 1)),
        ("func markupStartCandidate(s string) (int, string) {\n"
         "\tfor i := 0; i < len(s); i++ {\n"
         "\t\tif s[i] != '<' {\n"
         "\t\t\tcontinue\n"
         "\t\t}\n"
         "\t\trest := s[i:]\n"
         "\t\tif len(rest) >= len(markupOpen) {\n"
         "\t\t\tif rest[:len(markupOpen)] == markupOpen {\n"
         "\t\t\t\treturn i\n"
         "\t\t\t}\n"
         "\t\t\tcontinue\n"
         "\t\t}\n"
         "\t\tif strings.HasPrefix(markupOpen, rest) {\n"
         "\t\t\treturn i\n"
         "\t\t}\n"
         "\t}\n"
         "\treturn -1\n"
         "}",
         "func markupStartCandidate(s string) (int, string) {\n"
         "\tfor i := 0; i < len(s); i++ {\n"
         "\t\tif s[i] != '<' {\n"
         "\t\t\tcontinue\n"
         "\t\t}\n"
         "\t\trest := s[i:]\n"
         "\t\tfor _, open := range []string{markupOpen, markupOpenLoose} {\n"
         "\t\t\tif len(rest) >= len(open) {\n"
         "\t\t\t\tif rest[:len(open)] == open {\n"
         "\t\t\t\t\treturn i, open\n"
         "\t\t\t\t}\n"
         "\t\t\t\tcontinue\n"
         "\t\t\t}\n"
         "\t\t\tif strings.HasPrefix(open, rest) {\n"
         "\t\t\t\t// 截断态：rest 是 open 的前缀。两族前缀共享 `<｜DSML` 时按更长的\n"
         "\t\t\t\t// 族等下一片——不必区分，hold 语义本来就等完整数据。\n"
         "\t\t\t\treturn i, \"\"\n"
         "\t\t\t}\n"
         "\t\t}\n"
         "\t}\n"
         "\treturn -1, \"\"\n"
         "}", (1, 1)),
        ("end := strings.Index(inner[bodyStart:], markupInvokeClose)\n"
         "\t\tif end < 0 {\n"
         "\t\t\treturn nil, false // invoke 未闭合\n"
         "\t\t}\n"
         "\t\tbody := inner[bodyStart : bodyStart+end]\n"
         "\t\tpos = bodyStart + end + len(markupInvokeClose)",
         "// invoke 闭合形态与本块开头同族（单/双竖线），两种都找取最早，防模型混排。\n"
         "\t\tiS := strings.Index(inner[bodyStart:], markupInvokeClose)\n"
         "\t\tiL := strings.Index(inner[bodyStart:], markupInvokeCloseLoose)\n"
         "\t\tend, closeLen := -1, 0\n"
         "\t\tswitch {\n"
         "\t\tcase iS >= 0 && (iL < 0 || iS < iL):\n"
         "\t\t\tend, closeLen = iS, len(markupInvokeClose)\n"
         "\t\tcase iL >= 0:\n"
         "\t\t\tend, closeLen = iL, len(markupInvokeCloseLoose)\n"
         "\t\tdefault:\n"
         "\t\t\treturn nil, false // invoke 未闭合\n"
         "\t\t}\n"
         "\t\tbody := inner[bodyStart : bodyStart+end]\n"
         "\t\tpos = bodyStart + end + closeLen", (1, 1)),
        ("\t\tend := strings.Index(body[valStart:], markupParamClose)\n"
         "\t\tif end < 0 {\n"
         "\t\t\treturn \"\", false\n"
         "\t\t}\n"
         "\t\traw := body[valStart : valStart+end]\n"
         "\t\tpos = valStart + end + len(markupParamClose)",
         "\t\tiS := strings.Index(body[valStart:], markupParamClose)\n"
         "\t\tiL := strings.Index(body[valStart:], markupParamCloseLoose)\n"
         "\t\tend, closeLen := -1, 0\n"
         "\t\tswitch {\n"
         "\t\tcase iS >= 0 && (iL < 0 || iS < iL):\n"
         "\t\t\tend, closeLen = iS, len(markupParamClose)\n"
         "\t\tcase iL >= 0:\n"
         "\t\t\tend, closeLen = iL, len(markupParamCloseLoose)\n"
         "\t\tdefault:\n"
         "\t\t\treturn \"\", false\n"
         "\t\t}\n"
         "\t\traw := body[valStart : valStart+end]\n"
         "\t\tpos = valStart + end + closeLen", (1, 1)),
    ),
    # 上游源树该行未 gofmt（`="` 紧贴）；本地活树是 gofmt 后形态（`= "`）。
    # 格式归一消掉 dry-run 的已知无害 diff（2026-11-28）。
    "upstream/sanitize_identity_injection_test.go": (
        ('injectedIdentityNeutral ="', 'injectedIdentityNeutral = "', (1, 1)),
    ),
    # afree 本地定制：余额采样失败 WARN 按账号限频（WarnThrottled 由 PRESERVE
    # 的 upstream/proxy.go 提供，2026-11-28）。
    "scheduler/scheduler.go": (
        ('\t\t\tlog.Printf("WARN: balance %s: %v", logfmt.Label(uid, a.Nickname), err)',
         '\t\t\tupstream.WarnThrottled("balance:"+uid, "balance %s: %v", logfmt.Label(uid, a.Nickname), err)', (1, 1)),
    ),
}


def apply_panel_iframe_rewrites(pkg: Path) -> None:
    """Allow the WorkBuddy panel in the same-origin afree-proxy admin iframe.

    Upstream denies all framing. The panel is mounted below /admin/workbuddy/,
    so same-origin framing is the narrow policy needed for the integrated UI;
    third-party framing and the rest of the strict CSP remain blocked.
    """
    rewrites = {
        "panel/index.go": (
            ("禁止被 iframe 嵌套（防点击劫持）", "仅允许同源管理页 iframe 嵌套（防第三方点击劫持）", 1),
            ("禁止被任何站点 iframe 嵌套（点击劫持）", "仅允许同源管理页 iframe 嵌套（阻止第三方点击劫持）", 1),
            ("frame-ancestors 'none'", "frame-ancestors 'self'", 2),
            # 2026-11-13 上游 35228b3 加 X-Robots-Tag 后 gofmt 重排了注释对齐，
            # 同一行的 // 前空格在基线树（11 空格）与新上游树（8 空格）不同——
            # 拆成两个变体，各以 (0,1) 放行「恰好命中一侧」。
            (
                'w.Header().Set("X-Frame-Options", "DENY")           // 老浏览器兜底（CSP frame-ancestors 的等价项）',
                'w.Header().Set("X-Frame-Options", "SAMEORIGIN")     // 老浏览器兜底，仅允许同源嵌套',
                (0, 1),
            ),
            (
                'w.Header().Set("X-Frame-Options", "DENY")        // 老浏览器兜底（CSP frame-ancestors 的等价项）',
                'w.Header().Set("X-Frame-Options", "SAMEORIGIN")     // 老浏览器兜底，仅允许同源嵌套',
                (0, 1),
            ),
        ),
        "panel/security_test.go": (
            ("// CSP 必须禁止内联脚本与 iframe 嵌套（严格策略的核心约束）。", "// CSP 必须禁止内联脚本与第三方 iframe 嵌套（严格策略的核心约束）。", 1),
            ("frame-ancestors 'none'", "frame-ancestors 'self'", 1),
            ('h.Get("X-Frame-Options") != "DENY"', 'h.Get("X-Frame-Options") != "SAMEORIGIN"', 1),
        ),
        # 2026-11-02（上游 40d98f9/PR #122）：上游新增「代理池」视图。afree 的
        # workbuddy 出口走自己的 ProxyFor（workbuddy-proxies.json 绑定），不接
        # 上游 ProxyPool（面板 cfg.ProxyPool 恒 nil，API 一律 501）——按死配置
        # 红线隐藏导航入口，避免给出一个点了就报错的控件。基线树无该导航项(0 处)。
        "panel/index.html": (
            ('<li><a href="#proxies" data-view="proxies">', '<li hidden><a href="#proxies" data-view="proxies">', (0, 1)),
        ),
    }
    for relative, replacements in rewrites.items():
        path = pkg / relative
        text = path.read_text(encoding="utf-8")
        for old, new, expected in replacements:
            actual = text.count(old)
            # 计数支持元组（与 apply_egress_rewrites 同口径）：同一锚点在基线树
            # 与新上游树命中数不同（如 2026-11-02 隐藏代理池导航项，基线 0/新树 1）。
            allowed = expected if isinstance(expected, tuple) else (expected,)
            if actual not in allowed:
                raise RuntimeError(
                    f"{relative}: expected {'/'.join(map(str, allowed))} occurrence(s) of {old!r}, found {actual}"
                )
            text = text.replace(old, new)
        path.write_text(text, encoding="utf-8", newline="\n")


def apply_egress_rewrites(pkg: Path) -> None:
    for relative, rewrites in EGRESS_REWRITES.items():
        path = pkg / relative
        text = path.read_text(encoding="utf-8")
        for old, new, expected in rewrites:
            actual = text.count(old)
            # expected 可以是 int 或 int 元组：同一规则要同时作用于基线树与
            # 新上游树，锚点出现次数会随上游结构漂移（如 report.go 在
            # 49f9db4=1 处、9ca34c2=2 处），元组列出各树可接受的计数；
            # text.replace 恒替换全部命中。
            allowed = expected if isinstance(expected, tuple) else (expected,)
            if actual not in allowed:
                raise RuntimeError(
                    f"{relative}: expected {'/'.join(map(str, allowed))} occurrence(s) of {old!r}, found {actual}"
                )
            text = text.replace(old, new)
        path.write_text(text, encoding="utf-8", newline="\n")

# These files are maintained in afree-proxy, not upstream. Re-vendoring must
# replace the upstream package tree without dropping the egress adapter or its
# regression tests.
PRESERVE_RELATIVE = (
    Path("upstream") / "proxy.go",
    Path("upstream") / "proxy_test.go",
    # afree-local: per-proxy health observation hook + its regression tests.
    Path("upstream") / "observe_proxy_test.go",
    # afree-local additions (2026-11-26/27): probe verdict regression tests and
    # the loose-pipe DSML fixtures/tests appended to the upstream dsml_test.go —
    # both live only in afree, re-vendor must keep the whole files.
    Path("panel") / "probe_verdict_test.go",
    Path("upstream") / "dsml_test.go",
    Path("server") / "afree_adapter.go",
    # afree-local regression tests for afree-local features: the admin
    # pool_enabled switch (auth.pool_enabled) and the panel models
    # stale-snapshot replay on probe failure.
    Path("panel") / "models_stale_test.go",
    Path("pool") / "pool_enabled_test.go",
    # afree-local: newly added identities default to routing-disabled.
    Path("panel") / "add_default_test.go",
    # ---- afree 补丁全量登记（2026-11-15，dry-run 对基线 10d01b4 全量 diff 得出）----
    # 基线=源树 HEAD（10d01b4，2026-11-14 同步），以下 86 个文件是活树相对基线的
    # 全部 afree 补丁（修改+新增），整文件按 ours 保留：re-vendor 不再冲掉它们；
    # 上游更新这些文件时经 sync_workbuddy.py 三方合并手工吸收，不走本脚本。
    # 逐锚点登记不现实（补丁密度高、锚点漂移风险大），整文件保留与 textcall.go
    # 既有模式一致。
    Path("auth/auth.go"),
    Path("auth/auth_account_enabled_test.go"),
    Path("auth/auth_nickname_test.go"),
    Path("auth/auth_test.go"),
    Path("auth/realm_test.go"),
    # 2026-11-21 补登记：expires_test.go fixture 写死日期（1795254516410ms）落进
    # NeedsRefresh(1h) 窗口成时间炸弹，改相对时钟（now+28d）——afree 首个补丁，
    # 晚于全量登记，不写会被 re-vendor 冲回写死值（logstore_test ④ 同款教训）。
    Path("auth/expires_test.go"),
    # 2026-11-21 二次补登记：login_proxy_test.go 两处 t.Cleanup 重建裸 client
    # 而非还原原值——shuffle 下污染 TestLoginHTTPDisablesH2（CI Vet job
    # 五连红根因），改捕获原值还原。不写会被 re-vendor 冲回坏 cleanup。
    Path("panel/login_proxy_test.go"),
    Path("config/config.go"),
    Path("config/config_test.go"),
    Path("livecfg/livecfg.go"),
    Path("loginsite/loginsite.go"),
    Path("panel/add_default_test.go"),
    Path("panel/app.js"),
    Path("panel/autotask_claim_test.go"),
    Path("panel/credit_upgrade_test.go"),
    Path("panel/frontend_test.go"),
    Path("panel/import.go"),
    Path("panel/index.go"),
    Path("panel/index.html"),
    Path("panel/login.go"),
    # 2026-11-17 补登记：手改④（logstore_test 相对日期时间炸弹修复）晚于
    # 2026-11-15 的全量登记，当时漏网——dry-run 双向比对抓出。
    Path("panel/logstore_test.go"),
    Path("panel/mobile.css"),
    Path("panel/mobile.js"),
    Path("panel/models_busy_cache_test.go"),
    Path("panel/models_stale_test.go"),
    Path("panel/panel.go"),
    Path("panel/probe_usage_test.go"),
    Path("panel/taskcenter.go"),
    Path("pool/account_enabled_test.go"),
    Path("pool/accountfacts.go"),
    Path("pool/accountfacts_test.go"),
    Path("pool/cooldown.go"),
    Path("pool/credit_floor_test.go"),
    Path("pool/entry.go"),
    Path("pool/model_rate_test.go"),
    Path("pool/modelcooldown_test.go"),
    Path("pool/order_test.go"),
    Path("pool/persist.go"),
    Path("pool/pick.go"),
    Path("pool/pool.go"),
    Path("pool/pool_enabled_test.go"),
    Path("pool/pool_test.go"),
    Path("pool/realm.go"),
    Path("pool/reserve_fallback_test.go"),
    Path("pool/state.go"),
    Path("pool/strategy.go"),
    Path("pool/strategy_dirty_test.go"),
    Path("pool/strategy_test.go"),
    Path("reqlog/reqlog.go"),
    Path("reqlog/reqlog_test.go"),
    Path("scheduler/activity_test.go"),
    Path("scheduler/checkin_retry.go"),
    Path("scheduler/checkin_retry_test.go"),
    Path("scheduler/scheduler.go"),
    Path("scheduler/streak.go"),
    Path("scheduler/travel.go"),
    Path("server/admin_tasks.go"),
    Path("server/afree_adapter.go"),
    Path("server/continue.go"),
    Path("server/egress_backoff_test.go"),
    Path("server/handler.go"),
    Path("server/handler_models_rate_filter_test.go"),
    Path("server/handler_strategy_test.go"),
    Path("server/handler_test.go"),
    Path("server/logging.go"),
    Path("server/logging_test.go"),
    Path("server/slotlease_test.go"),
    Path("server/toolonly.go"),
    Path("session/session.go"),
    Path("upstream/client.go"),
    Path("upstream/connection_reuse_e2e_test.go"),
    Path("upstream/contextwindow_test.go"),
    Path("upstream/decode_body_test.go"),
    Path("upstream/desktop.go"),
    Path("upstream/global_models.go"),
    Path("upstream/headcase_test.go"),
    Path("upstream/headers.go"),
    Path("upstream/images.go"),
    Path("upstream/observe_proxy_test.go"),
    Path("upstream/output_budget.go"),
    Path("upstream/proxy.go"),
    Path("upstream/proxy_test.go"),
    Path("upstream/report.go"),
    Path("upstream/sanitize.go"),
    Path("upstream/streak.go"),
    Path("upstream/tlsfingerprint.go"),
    Path("upstream/tlsfingerprint_test.go"),
    Path("upstream/useragent_test.go"),
    Path("upstream/webagent.go"),
    Path("usage/usage.go"),
    # 上游基线新增、活树有意不纳入的文件（re-vendor 跳过拷贝，不带入活树）：
    #   upstream/dns_test.go
)

# SKIP_UPSTREAM_FILES 上游基线有、活树有意不纳入的文件（见上）：拷贝主循环跳过。
SKIP_UPSTREAM_FILES = (
    "upstream/dns_test.go",
)

# EXTRA_ROOT_PRESERVE module 根层的 afree 自有文件（在 internal/ 子树之外，
# PRESERVE_RELATIVE 够不到）：re-vendor 时从活树 module 根原样保留。
# afree_bridge.go 是主 module 的唯一消费门面；config.example.json 是上游根
# 样例的 afree 快照（PRESERVE 的 config_test.go 与它配对——上游 HEAD 样例
# 更新须走三方合并手工吸收，直接换新样例会让 TestConfigExampleParses 失配）。
EXTRA_ROOT_PRESERVE = (
    "afree_bridge.go",
    "config.example.json",
)



def main(source_root=None, target_root=None, preserve_from=None, preserve=True) -> int:
    """Vendor upstream into target_root. All args default to the module
    constants; sync_workbuddy.py overrides them to build merge inputs into
    temp dirs while PRESERVE_RELATIVE bytes still come from the live tree.

    preserve=False（sync_workbuddy 构建 base/theirs 合并输入专用）：跳过
    PRESERVE 字节写回。否则 base 与 theirs 在 PRESERVE 文件上都是活树字节，
    三方合并输入三者恒等 → merge-file 静默 no-op，上游对这些文件的改动
    无声丢失（2026-11-27 实测一次丢 394 行）。"""
    src_root = Path(source_root) if source_root else SOURCE_ROOT
    tgt_root = Path(target_root) if target_root else TARGET_ROOT
    pkg = tgt_root / "modules" / "workbuddy2api" / "internal"
    preserve_root = Path(preserve_from) if preserve_from else pkg

    missing = [p for p in PACKAGES if not (src_root / "internal" / p).is_dir()]
    if missing:
        # Historical baselines legitimately predate newer packages (e.g. reqlog),
        # so a partial set is fine — building the merge base just copies less.
        # A wrong source root fails loud instead: no packages at all.
        if len(missing) == len(PACKAGES):
            print("missing source packages: " + ", ".join(missing), file=sys.stderr)
            return 1
        print(f"note: packages absent in this source snapshot, skipped: {', '.join(missing)}",
              file=sys.stderr)

    preserved: dict[Path, bytes] = {}
    if preserve:
        for relative in PRESERVE_RELATIVE:
            path = preserve_root / relative
            if path.is_file():
                preserved[relative] = path.read_bytes()
        # 完整性预检（2026-11-17 事故护栏）：PRESERVE 字节唯一来源是活树，活树残缺
        # （文件被误删/重建中断）时缺失项会被**静默**丢弃并在后续重建中永久放大。
        # rmtree 之前 fail loud，先恢复活树再 vendor——绝不在残树上继续。
        # （EXTRA_ROOT_PRESERVE 是 module 根层文件、由下方 EXTRA_ROOT_PRESERVE 循环
        # 另行警告缺失，不进本检查。）
        missing_preserve = [
            rel.as_posix() for rel in PRESERVE_RELATIVE
            if rel not in preserved
        ]
        if missing_preserve:
            print(
                "live tree is MISSING preserve files (re-vendor would drop them permanently):\n  "
                + "\n  ".join(missing_preserve)
                + "\nrestore the live tree first (e.g. from git HEAD of the old vendor location) and retry.",
                file=sys.stderr,
            )
            return 1
    preserve_set = {p.as_posix() for p in PRESERVE_RELATIVE}

    if pkg.exists():
        shutil.rmtree(pkg)
    pkg.mkdir(parents=True)

    copied = 0
    rewritten = 0
    for package in PACKAGES:
        source_dir = src_root / "internal" / package
        if not source_dir.is_dir():
            continue  # absent in this source snapshot (historical baseline)
        target_dir = pkg / package
        target_dir.mkdir(parents=True, exist_ok=True)
        for source_file in sorted(p for p in source_dir.rglob("*") if p.is_file() and p.suffix in ASSET_SUFFIXES):
            rel = source_file.relative_to(source_dir)
            if (Path(package) / rel).as_posix() in SKIP_UPSTREAM_FILES:
                continue  # 上游基线有、活树有意不纳入（见 SKIP_UPSTREAM_FILES 注释）
            target_file = target_dir / rel
            target_file.parent.mkdir(parents=True, exist_ok=True)
            text = source_file.read_text(encoding="utf-8")
            new_text = text
            if package == "panel" and source_file.name == "app.js":
                new_text = new_text.replace("fetch('/panel/api/", "fetch('api/")
            if new_text != text:
                rewritten += 1
            target_file.write_text(new_text, encoding="utf-8", newline="\n")
            copied += 1

    client_path = pkg / "upstream" / "client.go"
    client_text = client_path.read_text(encoding="utf-8")
    if PROXY_FIELD_BLOCK not in client_text:
        if PROXY_FIELD_ANCHOR not in client_text:
            print("cannot locate upstream.Client ChatHTTP anchor", file=sys.stderr)
            return 1
        client_text = client_text.replace(PROXY_FIELD_ANCHOR, PROXY_FIELD_BLOCK, 1)
        client_path.write_text(client_text, encoding="utf-8", newline="\n")

    try:
        apply_panel_iframe_rewrites(pkg)
        apply_egress_rewrites(pkg)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    for relative, content in preserved.items():
        target_file = pkg / relative
        target_file.parent.mkdir(parents=True, exist_ok=True)
        target_file.write_bytes(content)

    # cmd/server/config.go is a standalone config loader in package main; it is
    # vendored as a library package because panel needs the exact same schema
    # and validation rules as the upstream project. Its self-contained test
    # file rides along (package rename only).
    source_config = src_root / "cmd" / "server" / "config.go"
    target_config_dir = pkg / "config"
    target_config_dir.mkdir(parents=True, exist_ok=True)
    # 活树 config.go / config_test.go 带 afree 补丁（Strategy/AccountOrder 等）
    # 且已整文件登记 PRESERVE——活树字节由上方 PRESERVE 写回，这里跳过上游
    # 拷贝，否则上游版本会覆盖掉补丁（2026-11-15 登记存量补丁时踩过）。
    config_preserved = preserve and "config/config.go" in preserve_set
    config_test_preserved = preserve and "config/config_test.go" in preserve_set
    if config_preserved:
        print("note: config/config.go preserved from live tree, upstream copy skipped")
    else:
        text = source_config.read_text(encoding="utf-8")
        text = text.replace("package main", "package config", 1)
        (target_config_dir / "config.go").write_text(text, encoding="utf-8", newline="\n")
        copied += 1
        rewritten += 1
    source_config_test = src_root / "cmd" / "server" / "config_test.go"
    if source_config_test.is_file() and not config_test_preserved:
        text = source_config_test.read_text(encoding="utf-8")
        text = text.replace("package main", "package config", 1)
        (target_config_dir / "config_test.go").write_text(text, encoding="utf-8", newline="\n")
        copied += 1
        rewritten += 1

    # module 根层的 afree 自有文件（门面/样例快照）从活树 module 根原样保留
    pkg.parent.mkdir(parents=True, exist_ok=True)
    for name in EXTRA_ROOT_PRESERVE:
        src = preserve_root.parent / name
        if src.is_file():
            (pkg.parent / name).write_bytes(src.read_bytes())
        else:
            print(f"warning: missing live module-root file: {src}", file=sys.stderr)

    # go.mod/go.sum 取上游原文，仅把 go 指令钉到主 module 口径（go 1.26.0）：
    # golang:1.26-alpine 镜像 GOTOOLCHAIN=local，镜像内的 go 补丁版本可能低于
    # 上游声明的 1.26.10，workspace 会直接拒启。wb 代码此前长期在主 module 的
    # go 1.26.0 下编译，语义等价；上游/镜像升级后可同步回撤此钉。
    for name in ("go.mod", "go.sum"):
        src = src_root / name
        if not src.is_file():
            continue
        text = src.read_text(encoding="utf-8")
        lines = text.splitlines(keepends=True)
        for i, ln in enumerate(lines):
            if ln.startswith("go ") and i <= 5:
                lines[i] = "go 1.26.0\n"
                break
        (pkg.parent / name).write_text("".join(lines), encoding="utf-8", newline="\n")

    print(f"copied {copied} Go files into {pkg} ({rewritten} rewritten)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

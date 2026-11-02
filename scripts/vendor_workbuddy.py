#!/usr/bin/env python3
"""Vendor the WorkBuddy runtime packages into this module.

The upstream project keeps its reusable packages under internal/. Go's internal
visibility rule prevents importing them from another module, so the source is
copied into this repository and rewritten to this module's import path.
"""

from pathlib import Path
import shutil
import sys


# 上游工作树（misakano/main 检出，git worktree of D:\下载\Github\workbuddy2api-panel）。
# 2026-10-23 起平台功能取自 misakano7545/workbuddy2api-panel（接续上游）。
SOURCE_ROOT = Path(r"D:\下载\Github\workbuddy2api-panel-misakano")
TARGET_ROOT = Path(r"D:\下载\Github\afree-proxy")
TARGET_PKG = TARGET_ROOT / "internal" / "workbuddy"

SOURCE_PREFIXES = (
    # 旧上游（linguo2625469，2026-10-21 停维）：基线 73a7560 及之前的提交用此模块路径。
    "github.com/linguo2625469/workbuddy2api-panel/internal/",
    # 接续上游（misakano7545，2026-10-23 f611bed 改名）：新基线起用此模块路径。
    # 双前缀并存：base 快照（旧基线归档）与 theirs（新上游工作树）经同一重写
    # 归一到 TARGET_PREFIX，import 路径差异不会污染三方合并。
    "github.com/misakano7545/workbuddy2api-panel/internal/",
)
TARGET_PREFIX = "afree-proxy/internal/workbuddy/"

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
        ("c.doJSON(req)", "c.doJSON(a, req)", (2, 0)),
        ("c.doJSON(req, a)", "c.doJSON(a, req)", (0, 2)),
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
            (
                'w.Header().Set("X-Frame-Options", "DENY")           // 老浏览器兜底（CSP frame-ancestors 的等价项）',
                'w.Header().Set("X-Frame-Options", "SAMEORIGIN")     // 老浏览器兜底，仅允许同源嵌套',
                1,
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
    Path("server") / "afree_adapter.go",
    # afree-local regression tests for afree-local features: the admin
    # pool_enabled switch (auth.pool_enabled) and the panel models
    # stale-snapshot replay on probe failure.
    Path("panel") / "models_stale_test.go",
    Path("pool") / "pool_enabled_test.go",
    # afree-local: newly added identities default to routing-disabled.
    Path("panel") / "add_default_test.go",
)


def main(source_root=None, target_root=None, preserve_from=None) -> int:
    """Vendor upstream into target_root. All args default to the module
    constants; sync_workbuddy.py overrides them to build merge inputs into
    temp dirs while PRESERVE_RELATIVE bytes still come from the live tree."""
    src_root = Path(source_root) if source_root else SOURCE_ROOT
    tgt_root = Path(target_root) if target_root else TARGET_ROOT
    pkg = tgt_root / "internal" / "workbuddy"
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
    for relative in PRESERVE_RELATIVE:
        path = preserve_root / relative
        if path.is_file():
            preserved[relative] = path.read_bytes()

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
            target_file = target_dir / rel
            target_file.parent.mkdir(parents=True, exist_ok=True)
            text = source_file.read_text(encoding="utf-8")
            new_text = text
            if source_file.suffix == ".go":
                for prefix in SOURCE_PREFIXES:
                    new_text = new_text.replace(prefix, TARGET_PREFIX)
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
    text = source_config.read_text(encoding="utf-8")
    text = text.replace("package main", "package config", 1)
    for prefix in SOURCE_PREFIXES:
        text = text.replace(prefix, TARGET_PREFIX)
    (target_config_dir / "config.go").write_text(text, encoding="utf-8", newline="\n")
    copied += 1
    rewritten += 1
    source_config_test = src_root / "cmd" / "server" / "config_test.go"
    if source_config_test.is_file():
        text = source_config_test.read_text(encoding="utf-8")
        text = text.replace("package main", "package config", 1)
        for prefix in SOURCE_PREFIXES:
            text = text.replace(prefix, TARGET_PREFIX)
        (target_config_dir / "config_test.go").write_text(text, encoding="utf-8", newline="\n")
        copied += 1
        rewritten += 1

    print(f"copied {copied} Go files into {pkg} ({rewritten} import-rewritten)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

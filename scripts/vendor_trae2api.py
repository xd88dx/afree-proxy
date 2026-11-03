#!/usr/bin/env python3
"""Vendor the trae2api (XCool-603/trae2api) runtime packages into this module.

上游项目把可复用包放在 internal/ 下（纯标准库、零第三方依赖）。Go 的 internal
可见性规则阻止跨模块导入，所以整树拷贝进本仓并重写 import 路径。与
vendor_workbuddy.py 同一套机制：

  - 整树拷贝 PACKAGES（排除 desktop/cli/cmd —— 核心包不 import 它们，见
    sync 技能的反向 grep 结论）；
  - import 前缀重写 trae2api/internal/ → afree-proxy/internal/trae2api/；
  - 上游本地修复（另一会话 2026-11-04 实测的文本工具调用兜底 + 4001 空片段
    修复）以 scripts/trae2api-afree.patch 形式随 vendor 重放；新文件
    （server/textcall*.go）走 PRESERVE_RELATIVE；
  - afree 桥接点（出口接管 / 反代开关 / 新增身份默认禁用 / console 挂载基址）
    以幂等改写规则注入，均声明为 vendor 包内的函数变量（上游原样为 nil =
    上游行为），由 internal/app/trae.go 在运行时注入，不产生 import 环。
"""

from pathlib import Path
import shutil
import subprocess
import sys


# 上游工作树（XCool-603/trae2api 检出）。remote trae-upstream 同源。
SOURCE_ROOT = Path(r"D:\下载\Github\trae2api-xcool")
TARGET_ROOT = Path(r"D:\下载\Github\afree-proxy")
TARGET_PKG = TARGET_ROOT / "internal" / "trae2api"

SOURCE_PREFIX = "trae2api/internal/"
TARGET_PREFIX = "afree-proxy/internal/trae2api/"

# 基线 commit 同步登记在 scripts/trae2api-vendor-baseline.json（sync 脚本维护）。
# 2026-11-04 基线：6a861d9（v0.2.6 之后、含 9074 退避重试）。
BASELINE_COMMIT = "6a861d98052418dfd2cc48c345de49ec61770f2d"

# 上游本地修复补丁（chat.go/server.go 的文本工具调用兜底 + 4001 空片段修复）。
PATCH_FILE = TARGET_ROOT / "scripts" / "trae2api-afree.patch"
# 补丁已应用的判据：chat.go 出现 textcall.go 引入的过滤器构造调用。
PATCH_APPLIED_MARKER = "newTextCallFilter("

PACKAGES = (
    "accountstore",
    "config",
    "docparse",
    "metrics",
    "pool",
    "prompt",
    "realm",
    "server",
    "session",
    "traectl",
    "usermgr",
    "vscdb",
)

# 文本类后缀按 UTF-8 读入、LF 归一、import 重写（仅 .go）；其余按字节原样拷贝
# （vscdb/testdata 的 SQLite 夹具等）。
TEXT_SUFFIXES = (".go", ".md", ".json", ".html", ".js", ".css", ".txt", ".yml")

# afree 本地文件（不在上游）：re-vendor 时从活树原样保留。
PRESERVE_RELATIVE = (
    Path("server") / "textcall.go",
    Path("server") / "textcall_test.go",
)

# 幂等改写规则：file → [(old, new, expected_count), ...]。
# expected_count 按上游 HEAD 树校验；改写只在 vendor/同步时对"干净上游树"执行
# 一次（WIP 源树已带 textcall 补丁的 hunk，但其余规则恒未应用）。
REWRITES = {
    "traectl/transport.go": (
        (
            """// GlobalProxy 获取当前配置的全局代理。
func GlobalProxy() string {
	globalProxyMu.RLock()
	defer globalProxyMu.RUnlock()
	return globalProxyURL
}
""",
            """// GlobalProxy 获取当前配置的全局代理。
func GlobalProxy() string {
	globalProxyMu.RLock()
	defer globalProxyMu.RUnlock()
	return globalProxyURL
}

// StoreHTTPClient afree 出口接管钩子（vendor 脚本注入的桥接点，上游原样为
// nil）：非 nil 时，chat/续期/额度/签到等所有对 Trae 上游的出站统一经宿主
// 解析的 HTTP 客户端发出（按账号绑定出口）。返回 nil 表示该账号回落上游
// 自带代理语义。afree 侧 fail-closed：解析不出可用出口时返回必然失败的
// 客户端，宁弃请求不暴露直连。
var StoreHTTPClient func(s *Store) *http.Client
""",
            1,
        ),
    ),
    "traectl/refresh.go": (
        (
            """func (s *Store) httpClient() *http.Client {
	return proxyRefreshClient(s.Proxy())
}""",
            """func (s *Store) httpClient() *http.Client {
	if StoreHTTPClient != nil {
		if cl := StoreHTTPClient(s); cl != nil {
			return cl
		}
	}
	return proxyRefreshClient(s.Proxy())
}""",
            1,
        ),
    ),
    "traectl/chat.go": (
        (
            """func (c *Client) client() *http.Client {
	if c.Timeout > 0 {
		return newHTTPClientWithProxy(c.Timeout, c.Proxy)
	}
	return proxyChatClient(c.Proxy)
}""",
            """func (c *Client) client() *http.Client {
	if c.Store != nil && StoreHTTPClient != nil {
		if cl := StoreHTTPClient(c.Store); cl != nil {
			return cl
		}
	}
	if c.Timeout > 0 {
		return newHTTPClientWithProxy(c.Timeout, c.Proxy)
	}
	return proxyChatClient(c.Proxy)
}""",
            1,
        ),
    ),
    "pool/pool.go": (
        (
            """func (a *Account) healthyLocked(now time.Time, model string) bool {
	if a.disabled {
		return false
	}
	if now.Before(a.breakerUntil) {""",
            """// SetLabel afree 面板改名钩子（vendor 脚本注入）：并发安全地更新运行时
// 标签。快照文件的持久化由调用方负责（直接改写文件后 Store.Load() 刷新）。
func (a *Account) SetLabel(label string) {
	a.mu.Lock()
	a.Label = label
	a.mu.Unlock()
}

// RoutingAllowed afree 反代开关钩子（vendor 脚本注入的桥接点，上游原样为
// nil）：非 nil 时，返回 false 的账号不参与模型路由选号（Pick）；签到/额度
// 刷新等维护任务不经 Pick，不受影响。
var RoutingAllowed func(uid string) bool

func (a *Account) healthyLocked(now time.Time, model string) bool {
	if a.disabled {
		return false
	}
	if RoutingAllowed != nil && !RoutingAllowed(a.UID) {
		return false
	}
	if now.Before(a.breakerUntil) {""",
            1,
        ),
    ),
    "accountstore/accountstore.go": (
        (
            """// Add 写入一份快照（同名覆盖），返回文件名。""",
            """// AccountAddedHook afree 新增身份钩子（vendor 脚本注入的桥接点，上游原样
// 为 nil）：每写入一份快照回调宿主一次（传 Normalize 后的快照），宿主据此
// 落"新增身份默认不参与路由"的初始状态。
var AccountAddedHook func(snap *traectl.CredentialSnapshot)

// Add 写入一份快照（同名覆盖），返回文件名。""",
            1,
        ),
        (
            """	name := snapshotFileName(snap)
	if err := traectl.WriteSnapshot(filepath.Join(s.dir, name), snap); err != nil {
		return "", err
	}
	return name, nil""",
            """	name := snapshotFileName(snap)
	if err := traectl.WriteSnapshot(filepath.Join(s.dir, name), snap); err != nil {
		return "", err
	}
	if AccountAddedHook != nil {
		AccountAddedHook(snap)
	}
	return name, nil""",
            1,
        ),
    ),
    # 契约闸测试按 runtime.Caller 向上推"仓库根"，afree vendor 布局多了一层
    # trae2api/，候选表补一条本目录路径（上游树中该候选不存在，自然跳过）。
    "server/contract_gate_test.go": (
        (
            """	candidates := []string{
		filepath.Join(repo, "internal", "server", "contract-endpoints.json"),""",
            """	candidates := []string{
		filepath.Join(repo, "trae2api", "server", "contract-endpoints.json"), // afree vendor 布局
		filepath.Join(repo, "internal", "server", "contract-endpoints.json"),""",
            1,
        ),
    ),
    # console 端点闸从 app.js 源码正则提取 fetch 路径；vendor 注入的
    # AFREE_API_BASE 前缀会让它拼出垃圾路径——提取前剥掉该前缀，还原上游形态。
    "server/console_test.go": (
        (
            """extractConsoleCalls(t, consoleAppJS)""",
            """extractConsoleCalls(t, strings.ReplaceAll(consoleAppJS, "AFREE_API_BASE + ", ""))""",
            2,
        ),
    ),
    "server/console_assets/app.js": (
        (
            """// api：所有 JSON 端点的统一入口。非 2xx 抛 Error（message 取后端的 error.message）。""",
            """// api：所有 JSON 端点的统一入口。非 2xx 抛 Error（message 取后端的 error.message）。
// afree 挂载基址：console 挂在本仓 /admin/trae2api/ 下，所有根绝对路径请求
// 统一改走该基址（vendor 脚本注入）。
const AFREE_API_BASE = '/admin/trae2api/api';""",
            1,
        ),
        (
            """  var res = await fetch(path, init);""",
            """  var res = await fetch(AFREE_API_BASE + path, init);""",
            1,
        ),
        ("fetch('/v1/chat/completions'", "fetch(AFREE_API_BASE + '/v1/chat/completions'", 2),
        ("fetch('/admin/login/start'", "fetch(AFREE_API_BASE + '/admin/login/start'", 1),
        ("fetch('/admin/login/callback'", "fetch(AFREE_API_BASE + '/admin/login/callback'", 1),
        ("fetch('/login'", "fetch(AFREE_API_BASE + '/login'", 1),
        ("fetch('/admin/accounts/export'", "fetch(AFREE_API_BASE + '/admin/accounts/export'", 1),
        ("fetch('/admin/dsh/export'", "fetch(AFREE_API_BASE + '/admin/dsh/export'", 1),
        ("fetch('/healthz')", "fetch(AFREE_API_BASE + '/healthz')", 1),
    ),
}


def apply_rewrites(pkg: Path) -> None:
    for relative, rules in REWRITES.items():
        path = pkg / relative
        text = path.read_text(encoding="utf-8")
        for old, new, expected in rules:
            actual = text.count(old)
            if actual != expected:
                raise RuntimeError(
                    f"{relative}: expected {expected} occurrence(s) of {old[:60]!r}..., found {actual}"
                )
            text = text.replace(old, new)
        path.write_text(text, encoding="utf-8", newline="\n")


def apply_patch(pkg: Path) -> None:
    """重放上游本地修复补丁。WIP 源树已带补丁时跳过；干净树用 git apply。"""
    chat = pkg / "server" / "chat.go"
    text = chat.read_text(encoding="utf-8")
    if PATCH_APPLIED_MARKER in text:
        print("patch already present in source tree, skip git apply")
        return
    if not PATCH_FILE.is_file():
        raise RuntimeError(f"missing patch file: {PATCH_FILE}")
    # 补丁路径是 a/internal/...；vendor 树把 internal/ 平移成了 TARGET_PKG，
    # 故 -p2 后以 pkg 为根应用。git apply 可在非仓库目录运行。
    subprocess.run(
        ["git", "apply", "-p2", "--whitespace=nowarn", str(PATCH_FILE)],
        cwd=str(pkg),
        check=True,
        capture_output=True,
        text=True,
    )
    print(f"applied {PATCH_FILE.name} (9 hunks)")


def main(source_root=None, target_root=None, preserve_from=None) -> int:
    src_root = Path(source_root) if source_root else SOURCE_ROOT
    tgt_root = Path(target_root) if target_root else TARGET_ROOT
    pkg = tgt_root / "internal" / "trae2api"
    preserve_root = Path(preserve_from) if preserve_from else pkg

    missing = [p for p in PACKAGES if not (src_root / "internal" / p).is_dir()]
    if missing:
        print("missing source packages: " + ", ".join(missing), file=sys.stderr)
        return 1

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
        target_dir = pkg / package
        for source_file in sorted(p for p in source_dir.rglob("*") if p.is_file()):
            rel = source_file.relative_to(source_dir)
            target_file = target_dir / rel
            target_file.parent.mkdir(parents=True, exist_ok=True)
            if source_file.suffix in TEXT_SUFFIXES:
                text = source_file.read_text(encoding="utf-8")
                new_text = text
                if source_file.suffix == ".go":
                    new_text = new_text.replace(SOURCE_PREFIX, TARGET_PREFIX)
                if new_text != text:
                    rewritten += 1
                target_file.write_text(new_text, encoding="utf-8", newline="\n")
            else:
                target_file.write_bytes(source_file.read_bytes())
            copied += 1

    try:
        apply_patch(pkg)
        apply_rewrites(pkg)
    except (RuntimeError, subprocess.CalledProcessError) as exc:
        detail = getattr(exc, "stderr", "") or str(exc)
        print(str(detail), file=sys.stderr)
        return 1

    for relative, content in preserved.items():
        target_file = pkg / relative
        target_file.parent.mkdir(parents=True, exist_ok=True)
        target_file.write_bytes(content)

    print(f"copied {copied} files into {pkg} ({rewritten} import-rewritten)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

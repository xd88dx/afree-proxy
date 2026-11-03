#!/usr/bin/env python3
"""Sync the vendored trae2api tree with upstream XCool-603/trae2api.

internal/trae2api 由 vendor_trae2api.py 生成，但 vendored 树同时携带
afree 侧注入（出口接管/反代开关钩子/console 挂载基址，由 vendor 脚本幂等
改写重放）与上游本地修复补丁（scripts/trae2api-afree.patch，另一会话
2026-11-04 实测的文本工具调用兜底）。直接 re-vendor 会冲掉这些内容，本脚本
按 workbuddy 同款三方合并流程安全重放：

  base   = vendor(基线 commit)     纯净 vendor 产物（含补丁与改写）
  ours   = 当前 internal/trae2api  base + 本地手改
  theirs = vendor(上游 HEAD)       新上游 + 补丁与改写

逐文件 git merge-file：本地手改保留、上游改动流入、真实碰撞留 <<<<<<< 标记
且基线不推进；解决后 --seal 登记。上游工作区允许携带已知 WIP（textcall 修复
四件套），超出清单的未提交内容会被拒绝——与 workbuddy"只信提交"的原则同源，
只是把本地修复登记为合法例外。

Usage:
  python scripts/sync_trae2api.py             # 合并上游进 vendor 树
  python scripts/sync_trae2api.py --report    # 只看将发生什么，不写文件
  python scripts/sync_trae2api.py --pull      # 先 ff-only 拉上游
  python scripts/sync_trae2api.py --source D:/path/to/trae2api-xcool
  python scripts/sync_trae2api.py --seal      # 解决冲突后登记新基线
"""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
from datetime import datetime
from io import BytesIO
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
ROOT = SCRIPTS.parent
PKG = ROOT / "internal" / "trae2api"
BASELINE_FILE = SCRIPTS / "trae2api-vendor-baseline.json"

sys.path.insert(0, str(SCRIPTS))
import vendor_trae2api as V  # noqa: E402

# 上游工作区允许存在的未提交内容（afree 侧实测修复，补丁与其同源）。
ALLOWED_DIRTY = {
    "internal/server/chat.go",
    "internal/server/server.go",
    "internal/server/textcall.go",
    "internal/server/textcall_test.go",
}


def git(source: Path, *args: str) -> str:
    r = subprocess.run(["git", "-C", str(source), *args],
                       capture_output=True, text=True, encoding="utf-8", errors="replace")
    if r.returncode != 0:
        raise RuntimeError(f"git {' '.join(args)}: {r.stderr.strip()}")
    return r.stdout


def upstream_head(source: Path) -> str:
    return git(source, "rev-parse", "HEAD").strip()


def upstream_dirty(source: Path) -> list[str]:
    # 只看 internal/ 下的未提交内容——vendor 只消费这棵子树；仓库根的
    # 二进制/日志残留（如 trae2api.exe.bak）与合并无关，不应阻塞同步。
    out = []
    for line in git(source, "status", "--porcelain").splitlines():
        if not line.strip():
            continue
        rel = line[3:].strip().replace("\\", "/").removeprefix('"').removesuffix('"')
        if rel.startswith("internal/") and rel not in ALLOWED_DIRTY:
            out.append(rel)
    return out


def upstream_version(source: Path) -> str:
    server_go = source / "internal" / "server" / "server.go"
    if not server_go.is_file():
        return "?"
    m = re.search(r'Version\s*=\s*"([^"]+)"', server_go.read_text(encoding="utf-8"))
    return m.group(1) if m else "?"


def local_version() -> str:
    trae_go = ROOT / "internal" / "app" / "trae.go"
    m = re.search(r'trae2apiVersion\s*=\s*"([^"]+)"', trae_go.read_text(encoding="utf-8"))
    return m.group(1) if m else "?"


def load_baseline() -> dict:
    if not BASELINE_FILE.is_file():
        sys.exit(f"缺少基线文件 {BASELINE_FILE}。\n"
                 "若当前 internal/trae2api 恰好是上游 HEAD 的纯净 vendor 产物（无手工改动），"
                 "运行 --seal 登记基线；否则无法确定合并 base，请人工处理。")
    return json.loads(BASELINE_FILE.read_text(encoding="utf-8"))


def save_baseline(source: Path, commit: str) -> None:
    BASELINE_FILE.write_text(json.dumps({
        "commit": commit,
        "synced_at": datetime.now().astimezone().isoformat(timespec="seconds"),
        "upstream_version": upstream_version(source),
    }, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def archive_commit(source: Path, commit: str, dest: Path) -> None:
    raw = subprocess.run(["git", "-C", str(source), "archive", "--format=tar",
                          commit, "internal"],
                         capture_output=True, check=True).stdout
    with tarfile.open(fileobj=BytesIO(raw)) as tf:
        try:
            tf.extractall(dest, filter="data")
        except TypeError:  # Python < 3.12 has no filter kwarg
            tf.extractall(dest)


def rel_files(root: Path) -> dict[str, Path]:
    out: dict[str, Path] = {}
    for p in root.rglob("*"):
        if p.is_file():
            out[p.relative_to(root).as_posix()] = p
    return out


def read_lf(p: Path) -> bytes:
    # 工作树可能是 CRLF 检出而 vendor 产物恒 LF；三方合并输入统一归一，
    # 否则每行都不同、merge-file 退化成整文件冲突。
    return p.read_bytes().replace(b"\r\n", b"\n")


def gofmt_tree(root: Path) -> None:
    files = [str(p) for p in root.rglob("*.go")]
    if not files:
        return
    if shutil.which("gofmt") is None:
        print("警告: 未找到 gofmt，跳过格式对齐（本次同步可能产生格式冲突）", file=sys.stderr)
        return
    for i in range(0, len(files), 40):  # 分批，避开 Windows 命令行长度上限
        subprocess.run(["gofmt", "-w", *files[i:i + 40]], check=False)


def merge_file(theirs: bytes, base: bytes, ours: bytes, tmp: Path) -> tuple[bytes, int]:
    cur, bp, op = tmp / "mf-cur", tmp / "mf-base", tmp / "mf-ours"
    cur.write_bytes(theirs)
    bp.write_bytes(base)
    op.write_bytes(ours)
    r = subprocess.run(["git", "merge-file", "-L", "THEIRS", "-L", "BASE", "-L", "OURS",
                        str(cur), str(bp), str(op)],
                       capture_output=True, text=True)
    if r.returncode < 0:
        raise RuntimeError(f"git merge-file: {r.stderr.strip()}")
    return cur.read_bytes(), r.returncode


def conflict_markers() -> list[str]:
    return sorted(rel for rel, p in rel_files(PKG).items()
                  if p.suffix in V.TEXT_SUFFIXES and b"<<<<<<< " in p.read_bytes())


def cmd_seal(source: Path) -> int:
    unexpected = upstream_dirty(source)
    if unexpected:
        print("上游工作区存在清单外未提交内容，拒绝登记基线：", file=sys.stderr)
        for rel in unexpected:
            print(f"  {rel}", file=sys.stderr)
        return 1
    marked = conflict_markers()
    if marked:
        print("树内仍有冲突标记，先解决再 --seal：", file=sys.stderr)
        for rel in marked:
            print(f"  {rel}", file=sys.stderr)
        return 1
    head = upstream_head(source)
    save_baseline(source, head)
    print(f"基线已登记: {head[:12]} (上游 {upstream_version(source)})")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description="上游 trae2api 更新同步（vendor + 三方合并）")
    ap.add_argument("--source", help="上游仓库路径（默认取 vendor_trae2api.SOURCE_ROOT）")
    ap.add_argument("--pull", action="store_true", help="先对上游执行 git pull --ff-only")
    ap.add_argument("--report", action="store_true", help="只显示将发生的变更，不写任何文件")
    ap.add_argument("--seal", action="store_true", help="解决冲突后把上游 HEAD 登记为新基线")
    args = ap.parse_args()

    src = Path(args.source) if args.source else V.SOURCE_ROOT
    if not (src / "internal").is_dir():
        print(f"上游仓库不存在或缺少 internal/: {src}", file=sys.stderr)
        return 1
    if args.seal:
        return cmd_seal(src)

    baseline = load_baseline()
    base_commit = baseline["commit"]
    unexpected = upstream_dirty(src)
    if unexpected:
        print("上游工作区存在清单外未提交内容（只信提交；textcall 四件套是合法例外）：",
              file=sys.stderr)
        for rel in unexpected:
            print(f"  {rel}", file=sys.stderr)
        return 1
    if args.pull:
        print(git(src, "pull", "--ff-only").strip())
    head = upstream_head(src)
    if head == base_commit:
        print(f"上游无新提交（基线 {base_commit[:12]}，版本 {baseline.get('upstream_version', '?')}）")
        return 0

    print(f"=== 上游新提交（{base_commit[:12]}..{head[:12]}，{upstream_version(src)}）===")
    print(git(src, "log", "--oneline", f"{base_commit}..HEAD").rstrip())
    stat = git(src, "diff", "--stat", f"{base_commit}..HEAD").rstrip()
    print(stat)
    print()

    tmp = Path(tempfile.mkdtemp(prefix="trae2api-sync-"))
    try:
        base_src = tmp / "base-src"
        base_out = tmp / "base"
        theirs_out = tmp / "theirs"
        archive_commit(src, base_commit, base_src)
        for source, target in ((base_src, base_out), (src, theirs_out)):
            if V.main(source_root=source, target_root=target, preserve_from=PKG) != 0:
                print("vendor 构建失败（原因见上方 stderr：可能是改写锚点随上游结构失效），"
                      "修 scripts/vendor_trae2api.py 后重试。", file=sys.stderr)
                return 1
            gofmt_tree(target / "internal" / "trae2api")

        ours = rel_files(PKG)
        base = rel_files(base_out / "internal" / "trae2api")
        theirs = rel_files(theirs_out / "internal" / "trae2api")

        merges: list[tuple[str, bytes, int]] = []
        upstream_new: list[str] = []
        upstream_gone: list[str] = []
        locally_deleted: list[str] = []
        for rel in sorted(set(theirs) | set(ours)):
            t, o, b = theirs.get(rel), ours.get(rel), base.get(rel)
            if t and o and b:
                t_data, o_data = read_lf(t), read_lf(o)
                if t_data == o_data:
                    continue
                data, n = merge_file(t_data, read_lf(b), o_data, tmp)
                merges.append((rel, data, n))
            elif t and not o and not b:
                upstream_new.append(rel)
            elif t and not o and b:
                locally_deleted.append(rel)
            elif not t and o and b:
                upstream_gone.append(rel)
            # 其余（仅本地有/仅本地删）= 本地自治，保留不动。

        if args.report:
            would = sum(1 for _, _, n in merges if n == 0)
            bad = [(rel, n) for rel, _, n in merges if n]
            print(f"[report] 将合并 {len(merges)} 个文件（直接 {would}，冲突 {len(bad)}），"
                  f"上游新增 {len(upstream_new)}，上游删除 {len(upstream_gone)}，本地已删 {len(locally_deleted)}")
            for rel, n in bad:
                print(f"  冲突({n}): {rel}")
            for rel in upstream_new:
                print(f"  上游新增: {rel}")
            for rel in upstream_gone:
                print(f"  上游删除(本地保留): {rel}")
            print("[report] 未写任何文件。")
            return 0

        for rel, data, _ in merges:
            target = PKG / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        for rel in upstream_new:
            target = PKG / rel
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(theirs[rel].read_bytes())

        clean = [(rel, n) for rel, _, n in merges if n == 0]
        bad = [(rel, n) for rel, _, n in merges if n]
        print(f"合并完成: 干净 {len(clean)} / 冲突 {len(bad)} / 上游新增 {len(upstream_new)}")
        for rel in upstream_new:
            print(f"  上游新增: {rel}")
        for rel in locally_deleted:
            print(f"  跳过（本地已删，上游仍有）: {rel}")
        for rel in upstream_gone:
            print(f"  警告（上游已删，本地保留待人工判断）: {rel}")
        if bad:
            print("\n存在冲突，解决 <<<<<<< 标记后运行: python scripts/sync_trae2api.py --seal")
            for rel, n in bad:
                print(f"  冲突({n}): {rel}")
            return 1

        save_baseline(src, head)
        print(f"基线已推进: {head[:12]} (上游 {upstream_version(src)})")
        if local_version().removesuffix("+afree") != upstream_version(src):
            print(f"提醒: 本地 trae2apiVersion={local_version()}，上游 Version={upstream_version(src)}，"
                  "请同步 internal/app/trae.go 的版本号。")
        print("\n后续人工步骤（脚本无法代劳）:")
        print("  1. internal/app/trae.go: 新配置键/新管理端点的桥接、版本号 trae2apiVersion")
        print("  2. 验证: go build ./... && go vet ./... && go test ./internal/trae2api/... ./internal/app/")
        print("  3. git add -A && git commit")
        return 0
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())

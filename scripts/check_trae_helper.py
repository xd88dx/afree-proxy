#!/usr/bin/env python3
"""比对 internal/trae/helper_script.go 内嵌的 web_login.py 与上游 Trae2api-cn 的差异。

背景:afree 的 Trae 本机授权助手内嵌了上游 Trae2api-cn 的 web_login.py(逐行移植)。
Trae 线其余代码是本地 Go 实现,不走 vandor/merge 流程,所以只有这一个文件需要
跟上游比对。基线登记在 scripts/trae-helper-baseline.json。

用法:
  # 1) 取上游快照(tar 包走代理,比 git clone 稳;clone 常被 TLS 中断)
  curl -sSL -x socks5h://127.0.0.1:10808 -o /tmp/trae-up.tar.gz \\
      https://codeload.github.com/autumnsentiment/Trae2api-cn/tar.gz/refs/heads/main
  mkdir -p /tmp/trae-up && tar xzf /tmp/trae-up.tar.gz -C /tmp/trae-up --strip-components=1

  # 2) 比对
  python scripts/check_trae_helper.py --upstream-dir /tmp/trae-up

退出码:0 = 一致(仅预期占位符差异);1 = 有实质差异;2 = 找不到文件。
"""
import argparse
import difflib
import json
import os
import re
import sys

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LOCAL = os.path.join(REPO, "internal", "trae", "helper_script.go")
BASELINE = os.path.join(REPO, "scripts", "trae-helper-baseline.json")
MARKER = "const helperScriptTpl = `"

# 上游按部署写死的默认 relay,本地替换为占位符后由 HelperScript() 预填。
RELAY_SENTINEL = ('DEFAULT_RELAY_URL = "http://127.0.0.1:8000"',
                  'DEFAULT_RELAY_URL = "__RELAY_URL__"')

# 提取本地内嵌脚本:还原 Go raw string 里的反引号拼接转义。
GO_ESCAPE = '` + "`" + `'


def extract_local(path=LOCAL):
    with open(path, encoding="utf-8") as fh:
        src = fh.read()
    start = src.index(MARKER) + len(MARKER)
    end = src.rindex("`")
    body = src[start:end]
    assert body.count(GO_ESCAPE) == body.count(GO_ESCAPE), "escape marker mismatch"
    return body.replace(GO_ESCAPE, "`").replace("\r\n", "\n")


def load_upstream(upstream_dir):
    path = os.path.join(upstream_dir, "web_login.py")
    with open(path, encoding="utf-8") as fh:
        return fh.read().replace("\r\n", "\n")


def normalize(text):
    text = text.replace(*RELAY_SENTINEL)
    return "\n".join(line.rstrip() for line in text.splitlines()) + "\n"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--upstream-dir", required=True,
                    help="上游仓库解包目录(需含 web_login.py)")
    ap.add_argument("--quiet", action="store_true")
    args = ap.parse_args()

    if not os.path.exists(LOCAL):
        print(f"MISSING local: {LOCAL}", file=sys.stderr)
        return 2
    if not os.path.exists(os.path.join(args.upstream_dir, "web_login.py")):
        print(f"MISSING upstream web_login.py in {args.upstream_dir}", file=sys.stderr)
        return 2

    local = normalize(extract_local())
    upstream = normalize(load_upstream(args.upstream_dir))

    base = {}
    if os.path.exists(BASELINE):
        with open(BASELINE, encoding="utf-8") as fh:
            base = json.load(fh)
    commit = base.get("commit", "?")
    version = base.get("version", "?")
    print(f"baseline: {version} @ {commit}")

    if local == upstream:
        print("OK: embedded web_login.py is line-identical to upstream "
              "(modulo the __RELAY_URL__ placeholder)")
        return 0

    print("DIFF: embedded web_login.py differs from upstream:", file=sys.stderr)
    diff = difflib.unified_diff(upstream.splitlines(), local.splitlines(),
                                "upstream/web_login.py", "local/helper_script.go",
                                lineterm="", n=2)
    for line in diff:
        print(line, file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())

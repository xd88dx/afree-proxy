"""端点闭合性审计（退出码非 0 = 有缺口）。

防两类回归（2026-11-19 handleWkbdShareOptimize 漏注册事故的固化护栏）：
  1. 死端点：handler 定义了但从未挂进任何 mux —— 编译器不报错、vet 不查，
     GET 被前端静默降级吞掉、POST 才暴露 404。
  2. 前端调用无路由：api()/wbCall() 拼出的路径在注册集合中不存在 —— 同样
     只在保存类 POST 才暴露。

三方向对账（判据都用已知案例校准过，宁可 SKIP 不误报）：
  A. internal/app 的 func handleXxx 全部有消费（mux 注册 / 非注释引用 /
     单行 gateway.* 直调过渡别名三选一）。
  B. 面板 api('GET|POST', '/path') 静态路径逐条命中 /admin/api 注册集合；
     动态拼接路径计入 SKIP（静态分析不可判，不静默漏过而是显式计数）。
  C. wbCall('action') → /panel/api/<action> 命中 vendor panel 注册集合
     （GET 前缀路由匹配；method 错配会 405 而非 404，交 vendor 契约测试）。

匹配纪律（2026-11-09 闭合性核对踩过的坑）：以 / 结尾的 catch-all 注册
（如 "/admin/"）不得作为命中源，否则任何前缀调用都假阴性命中。

用法：python scripts/endpoint_audit.py（在仓库根运行）
"""
import io
import re
import sys

APP_FILES = "internal/app/*.go"
PANEL_SOURCES = ["internal/panel/html.go"] + [
    "internal/%s/panel_tab.html" % p for p in ("cline", "zen", "trae", "workbuddy", "other")
]
WB_VENDOR_GLOB = "modules/workbuddy2api/internal/panel/*.go"

import glob as _glob

def read(p):
    return io.open(p, encoding="utf-8", errors="replace").read()

def comments_stripped(src):
    # 逐字符去 // 与 /* */ 注释，保留字符串字面量（粗粒度即可：注册行与
    # 引用行的形态在去注释后仍可识别；字符串内 // 极少且都是 URL）。
    out, i, n = [], 0, len(src)
    in_str = None
    while i < n:
        c = src[i]
        if in_str:
            out.append(c)
            if c == "\\" and i + 1 < n:
                out.append(src[i + 1]); i += 2; continue
            if c == in_str:
                in_str = None
            i += 1; continue
        if c in "\"'`":
            in_str = c; out.append(c); i += 1; continue
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            while i < n and src[i] != "\n":
                i += 1
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "*":
            i += 2
            while i + 1 < n and not (src[i] == "*" and src[i + 1] == "/"):
                i += 1
            i += 2; continue
        out.append(c); i += 1
    return "".join(out)

# ---- 注册路由（主 mux：internal/app）----
def collect_registered():
    routes = set()
    for p in sorted(_glob.glob(APP_FILES)):
        src = read(p)
        for m in re.finditer(r'mux\.Handle(?:Func)?\(\s*"([^"]+)"', src):
            routes.add(m.group(1))
    return routes

# ---- 方向 A：handler 定义 → 消费面 ----
def audit_handlers(routes):
    fails = []
    app_files = sorted(_glob.glob(APP_FILES))
    all_src = {p: comments_stripped(read(p)) for p in
               app_files + sorted(_glob.glob("internal/*/*_test.go"))}
    for p in app_files:
        src = all_src[p]
        for m in re.finditer(r'func (handle[A-Z]\w*)\(', src):
            name = m.group(1)
            body = src[m.start():m.start() + 400]
            one_line_bridge = re.search(
                r'return gateway\.\w+\(', body) or re.match(
                r'func %s\([^)]*\) \{\s*\n?\s*gateway\.\w+\(' % name, src[m.start():])
            if one_line_bridge:
                continue  # gateway 批过渡别名（直调导出面），豁免注册要求
            used = False
            for q, s in all_src.items():
                for line in s.splitlines():
                    if re.search(r'\b%s\b' % name, line) and "func %s(" % name not in line:
                        used = True
                        break
                if used:
                    break
            if not used:
                fails.append("dead handler: %s (defined %s, no mux registration or reference)" % (name, p))
    return fails

def _hit(path, routes):
    # 命中判据：精确相等，或存在以该路径为前缀的更长注册；catch-all（以 /
    # 结尾的注册）不得作为命中源（2026-11-09 假阴性教训）。
    if path in routes:
        return True
    return any(r.startswith(path) and not r.endswith("/") for r in routes)

# ---- 方向 B：前端 api() 调用 → /admin/api 注册 ----
def audit_front_calls(routes):
    fails, skips = [], []
    reg_admin = {r for r in routes if r.startswith("/admin/api")}
    for p in PANEL_SOURCES:
        src = read(p)
        for m in re.finditer(r"""api\(\s*'(?:GET|POST|PUT|DELETE)'\s*,\s*['"]([^'"]+)['"]""", src):
            path = m.group(1)
            if "$" in path or "+" in path or "`" in path:
                skips.append(path)
                continue
            full = "/admin/api" + path if path.startswith("/") else path
            if not _hit(full, reg_admin):
                fails.append("front call %r (%s) has no matching /admin/api route" % (path, p))
    return fails, skips

# ---- 方向 C：wbCall → vendor panel 路由 ----
def audit_wb_calls():
    fails, skips = [], []
    vendor_routes = set()
    for p in sorted(_glob.glob(WB_VENDOR_GLOB)):
        for m in re.finditer(r'"(GET|POST) (/panel/api/[^"]+)"', read(p)):
            vendor_routes.add(m.group(2))
    for p in PANEL_SOURCES:
        src = read(p)
        for m in re.finditer(r"""wbCall\(\s*'([^']*)'\s*(\+)?""", src):
            action = m.group(1)
            # 拼接形态：引号外紧跟 + （如 wbCall('accounts/' + enc(id) + '/test')
            # ——正则只捕到引号内前缀，必须连引号后的拼接符一起看），或 action
            # 本身以 / 结尾（必然是拼接前缀不是完整路径）。
            if m.group(2) or "+" in action or "$" in action or action.endswith("/"):
                skips.append("wbCall(" + action)
                continue
            path = "/panel/api/" + action if not action.startswith("/") else action
            if path in vendor_routes:
                continue
            # 段模式匹配：vendor 用 {uid} 等通配段（拼接调用已 SKIP，静态
            # action 通常精确命中；通配放行是保守容错）。
            segs = [s for s in path.split("/") if s]
            ok = False
            for vr in vendor_routes:
                vs = [s for s in vr.split("/") if s]
                if len(vs) == len(segs) and all(
                        (v.startswith("{") and v.endswith("}")) or v == s
                        for v, s in zip(vs, segs)):
                    ok = True
                    break
            if not ok:
                fails.append("wbCall %r (%s) has no matching vendor panel route" % (action, p))
    return fails, skips

def main():
    routes = collect_registered()
    fails_a = audit_handlers(routes)
    fails_b, skips_b = audit_front_calls(routes)
    fails_c, skips_c = audit_wb_calls()
    print("--- 方向A handler 消费面 (%d) ---" % len(fails_a))
    for f in fails_a:
        print("  FAIL:", f)
    print("--- 方向B 前端 api() 闭合 (%d fails, %d skipped-dynamic) ---" % (len(fails_b), len(skips_b)))
    for f in fails_b:
        print("  FAIL:", f)
    print("--- 方向C wbCall→vendor 闭合 (%d fails, %d skipped-dynamic) ---" % (len(fails_c), len(skips_c)))
    for f in fails_c:
        print("  FAIL:", f)
    total = len(fails_a) + len(fails_b) + len(fails_c)
    print("endpoint audit: %d registered routes checked, %d FAIL, %d dynamic-skip" %
          (len(routes), total, len(skips_b) + len(skips_c)))
    sys.exit(1 if total else 0)

if __name__ == "__main__":
    main()

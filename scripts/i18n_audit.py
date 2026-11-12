"""i18n 正向审计:检查面板文案是否都接进了词典(退出码非 0 = 有缺口)。

三遍检查,覆盖三条翻译路径:
  1. T('...') / T("...") 的键是否都在词典 —— JS 动态文案。
  2. data-i18n / data-i18n-title / data-i18n-placeholder 的键是否都在词典
     —— 静态文本/属性的显式键。
  3. **含内联标签的文本块**:元素自身有文案、子元素全是行内元素(code/b/a/span…)
     时,翻译机制按单个文本节点匹配,句子会被拆断 → 这类段落必须有 data-i18n
     显式键,否则中文界面必然残留英文(2026-11-09 补的第三遍,专治这一类)。

用法:python scripts/i18n_audit.py [admin_html.go]
"""
import html as htmlmod
import io
import re
import sys
from html.parser import HTMLParser

path = sys.argv[1] if len(sys.argv) > 1 else "internal/app/admin_html.go"
src = io.open(path, encoding="utf-8").read()


def js_unescape(s):
    out = []
    i = 0
    while i < len(s):
        c = s[i]
        if c == "\\" and i + 1 < len(s):
            n = s[i + 1]
            if n == "n":
                out.append("\n")
            elif n == "t":
                out.append("\t")
            else:
                out.append(n)
            i += 2
        else:
            out.append(c)
            i += 1
    return "".join(out)


m = re.search(r"const I18N = \{", src)
start = m.end()
end = src.index("\n};", start)
block = src[start:end]

keys = set()
for mm in re.finditer(r"^\s*'((?:[^'\\]|\\.)*)'\s*:", block, re.M):
    keys.add(js_unescape(mm.group(1)))
for mm in re.finditer(r'^\s*"((?:[^"\\]|\\.)*)"\s*:', block, re.M):
    keys.add(js_unescape(mm.group(1)))

used = set()
for mm in re.finditer(r"\bT\('((?:[^'\\]|\\.)*)'\)", src):
    used.add(js_unescape(mm.group(1)))
for mm in re.finditer(r'\bT\("((?:[^"\\]|\\.)*)"\)', src):
    used.add(js_unescape(mm.group(1)))

missing_t = sorted(u for u in used if u not in keys)

html_part = src[: src.index("<script>")]
attr_keys = {mm.group(1) for mm in re.finditer(r'data-i18n(?:-title|-placeholder)?="([^"]+)"', html_part)}
missing_attr = sorted(k for k in attr_keys if k not in keys)


def norm(s):
    return re.sub(r"\s+", " ", s).strip()


norm_keys = {norm(htmlmod.unescape(js_unescape(k))) for k in keys}

INLINE = {"code", "b", "strong", "em", "i", "a", "span", "small", "kbd", "br", "sub", "sup"}
VOID = {"input", "br", "hr", "img", "meta", "link", "source", "area", "base", "col", "embed", "param", "track", "wbr"}
SKIP = {"script", "style", "svg", "path", "circle", "rect", "text", "tspan", "head", "title"}
# 允许不译:产品名(进词条反而制造假翻译)
ALLOWED = {
    "TraeWork", "WorkBuddy", "OpenCode", "AMDCloud", "OpenRouter", "TokenHarbor",
    "Cline", "Cursor", "Claude Code", "Token Factory", "AFree Proxy",
}
# 工具过滤 textarea 的默认示例值（工具名，用户照着填的模板，不译）
TOOL_NAME_SAMPLES = {"CronCreate", "CreateWorkflow", "EnterPlanMode", "ExitPlanMode", "AskUserQuestion"}


class Node:
    def __init__(self, tag, attrs):
        self.tag = tag
        self.attrs = dict(attrs)
        self.children = []
        self.texts = []


class P(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.root = Node("#root", [])
        self.stack = [self.root]

    def handle_starttag(self, tag, attrs):
        n = Node(tag, attrs)
        self.stack[-1].children.append(n)
        if tag not in VOID:
            self.stack.append(n)

    def handle_endtag(self, tag):
        for i in range(len(self.stack) - 1, 0, -1):
            if self.stack[i].tag == tag:
                del self.stack[i:]
                return

    def handle_data(self, data):
        self.stack[-1].texts.append(data)


p = P()
p.feed(html_part)


def full_text(n):
    parts = list(n.texts)
    for c in n.children:
        parts.append(full_text(c))
    return " ".join(x for x in parts if x)


gaps = []
text_gaps = []


def walk(n):
    if n.tag in SKIP:
        return
    direct = norm(" ".join(n.texts))
    kids = list(n.children)
    if direct and re.search(r"[A-Za-z]{2,}", direct) and kids and all(c.tag in INLINE for c in kids):
        txt = norm(full_text(n))
        if txt and txt not in norm_keys and "data-i18n" not in n.attrs and txt not in ALLOWED:
            gaps.append(txt)
    elif not kids and direct and re.search(r"[A-Za-z]{2,}", direct):
        # 第五遍：无子元素的叶子文本。i18nTextNode 兜底按英文原文整段查词典，
        # 原文不在词典且没挂 data-i18n → 中文界面残留英文（2026-11-12 补）。
        # 枚举 option（文本=value 属性）是发给 API 的参数值，不译。
        if n.tag == "option" and n.attrs.get("value") == direct.strip():
            return
        t = norm(full_text(n))
        # 技术形态不译：URL、API 端点（POST /v1/...）、凭据示例、工具名单词。
        if re.search(r"(://|\{\{|\$\{|POST /|GET /|PUT /|DELETE /|sk-|oc_|rc-|thk_|eyJ|mcp__|\.{3}|…$)", t):
            return
        if t in TOOL_NAME_SAMPLES:
            return
        if t and t not in norm_keys and "data-i18n" not in n.attrs and t not in ALLOWED:
            text_gaps.append(t)
    for c in kids:
        walk(c)


walk(p.root)


# ---------------------------------------------------------------------------
# 第四遍：title/placeholder 属性裸英文。
# i18nAttrs 的兜底路径按英文原文查词典（I18N[v.trim()]），没挂 data-i18n-title/
# data-i18n-placeholder 的属性必须能以原文命中词条，否则中文界面残留英文。
# ---------------------------------------------------------------------------
def attr_is_translatable(v):
    v = norm(htmlmod.unescape(v))
    if len(v) < 3:
        return False
    if not re.search(r"[A-Za-z]{2,}", v):
        return False
    # 纯技术形态：路径/URL/CSS/变量名/带花括号模板
    if re.search(r"(://|\{\{|\$\{|^\w+\.\w+$|class=|style=|var\(--)", v):
        return False
    # 凭据/令牌格式示例（用户照着填的模板，翻译反而误导）：sk-…、oc_sk_…、
    # thk_live_…、JWT（eyJ…）、mcp__ 前缀、rc- 前缀、省略号收尾的形态串。
    if re.search(r"(sk-|oc_|rc-|thk_|eyJ|mcp__|\.{3}|…)", v):
        return False
    # 工具名枚举列表（CronCreate CreateWorkflow …）
    if re.fullmatch(r"[A-Z][A-Za-z0-9_]+( [A-Z][A-Za-z0-9_]+)+", v):
        return False
    return True


attr_gaps = []
for mm in re.finditer(r'<([a-z][a-z0-9]*)\b[^>]*?((?:title|placeholder)="[^"]*")[^>]*>', html_part):
    tag, attr = mm.group(1), mm.group(2)
    if tag in SKIP or tag in VOID:
        continue
    am = re.match(r'(title|placeholder)="([^"]*)"', attr)
    if not am:
        continue
    name, val = am.group(1), am.group(2)
    whole = mm.group(0)
    if f"data-i18n-{name}=" in whole:
        continue  # 显式键已由检查 2 覆盖
    if not attr_is_translatable(val):
        continue
    v = norm(htmlmod.unescape(val))
    if v in norm_keys or v in ALLOWED:
        continue
    attr_gaps.append((name, v))


# ---------------------------------------------------------------------------
# 第五遍：静态纯文本节点裸英文 —— 已并入上方 walk() 的叶子检查（用 HTMLParser
# 树而非正则：onclick 属性值里的 => 会把正则版截断造成误报）。
# ---------------------------------------------------------------------------

print("T() usages: %d  data-i18n keys: %d  dict keys: %d" % (len(used), len(attr_keys), len(keys)))
print("--- MISSING zh entries for T() (%d) ---" % len(missing_t))
for s in missing_t:
    print(repr(s))
print("--- MISSING zh entries for data-i18n (%d) ---" % len(missing_attr))
for s in missing_attr:
    print(repr(s))
print("--- 含内联标签的文本块缺显式键 (%d) ---  [必须加 data-i18n，否则中文界面留英文]" % len(gaps))
for s in gaps:
    print(repr(s[:160]))
print("--- title/placeholder 裸英文未命中词典 (%d) ---  [需 data-i18n-title/placeholder 或加词条]" % len(attr_gaps))
for name, v in attr_gaps:
    print(name + ": " + repr(v[:160]))
print("--- 静态纯文本节点裸英文未命中词典 (%d) ---  [需 data-i18n 或加词条]" % len(text_gaps))
for t in text_gaps:
    print(repr(t[:160]))

sys.exit(1 if (missing_t or missing_attr or gaps or attr_gaps or text_gaps) else 0)
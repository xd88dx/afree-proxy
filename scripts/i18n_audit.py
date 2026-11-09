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


def walk(n):
    if n.tag in SKIP:
        return
    direct = norm(" ".join(n.texts))
    kids = list(n.children)
    if direct and re.search(r"[A-Za-z]{2,}", direct) and kids and all(c.tag in INLINE for c in kids):
        txt = norm(full_text(n))
        if txt and txt not in norm_keys and "data-i18n" not in n.attrs and txt not in ALLOWED:
            gaps.append(txt)
    for c in kids:
        walk(c)


walk(p.root)

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

sys.exit(1 if (missing_t or missing_attr or gaps) else 0)
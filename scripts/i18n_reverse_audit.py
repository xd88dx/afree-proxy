# -*- coding: utf-8 -*-
"""i18n 反向审计（孤儿检测）——多形态 × 多来源精确匹配版（2026-10-26 升级）。

旧版对含撇号/转义/HTML 实体的键做精确匹配会误报孤儿（10-25 曾据误报误删 3 条在用词条）。
新版：键做 raw/去转义/实体归一/去引号转义四形态；引用来源 = T() 参数 + HTML 静态文本/属性
+ JS 单双引号字面量 + 字面量内 HTML 文本节点。剩余孤儿仍可能被 DOM 文本节点级动态翻译
使用（短词拼接场景），孤儿清单仅作参考，不作为删除依据。"""
import io, re, sys
import html as htmlmod

if len(sys.argv) > 1:
    _paths = [sys.argv[1]]
else:
    _paths = ["internal/panel/html.go"] + [
        "internal/%s/panel_tab.html" % p for p in ("cline", "zen", "trae", "workbuddy", "other", "freepi")
    ] + ["internal/%s/panel_nav.html" % p for p in ("cline", "zen", "trae", "workbuddy", "other", "freepi")]
path = ",".join(_paths)
src = "\n".join(io.open(pp, encoding='utf-8').read() for pp in _paths)

def js_unescape(s):
    out, i = [], 0
    while i < len(s):
        c = s[i]
        if c == '\\' and i + 1 < len(s):
            out.append(s[i+1]); i += 2
        else:
            out.append(c); i += 1
    return ''.join(out)

m = re.search(r'const I18N = \{(.*?)\n\};', src, re.S)
raw_keys = re.findall(r"^\s*'((?:[^'\\]|\\.)*)'\s*:", m.group(1), re.M)
src_nodict = src.replace(m.group(0), '')

def variants(s):
    return {s, js_unescape(s), htmlmod.unescape(s), s.replace('\\"', '"')}

refs = set()
# 1) T() 参数（全文件）
for mm in re.finditer(r"\bT\(\s*'((?:[^'\\]|\\.)*)'", src_nodict):
    refs.add(mm.group(1)); refs.add(js_unescape(mm.group(1)))
# 2) HTML 静态区：文本节点 + title/placeholder（含实体形态）
html_end = src_nodict.index('<script>')
htmlpart = src_nodict[:html_end]
for mm in re.finditer(r'>([^<>]+)<', htmlpart):
    t = mm.group(1).strip()
    refs.add(t); refs.add(htmlmod.unescape(t))
for mm in re.finditer(r'(?:title|placeholder)="([^"]*)"', htmlpart):
    t = mm.group(1).strip()
    refs.add(t); refs.add(htmlmod.unescape(t))
# 2b) data-i18n / data-i18n-title / data-i18n-placeholder 属性值（显式键）
for mm in re.finditer(r'data-i18n(?:-title|-placeholder)?="([^"]*)"', htmlpart):
    refs.add(mm.group(1).strip())
# 3) JS 区字符串字面量 + 大串内 HTML 文本节点
jspart = src_nodict[html_end:]
BS = chr(92)
# 开引号须紧邻 =/空白/(/,/:/[/+ 或串首（2026-11-27 修正引号配对错位）：
# 旧版裸双引号配对会被 `value=""` 空属性错位——第一个引号被当作上一对的
# 闭合，紧随的 data-i18n="key" 永远进不了 refs，产生假孤儿（实证：
# zen_probe_auto 实际在用却被报孤儿）。限定开引号前置字符后，空属性 ""
# 不再开新配对。
OPEN = r'(?:^|[=(,:\[+\s])'
lit_re = re.compile(OPEN + chr(39) + r'((?:[^' + chr(39) + BS + chr(10) + r']|' + BS + r'.){2,})' + chr(39))
dq_re = re.compile(OPEN + chr(34) + r'((?:[^' + chr(34) + BS + chr(10) + r']|' + BS + r'.){2,})' + chr(34))
lits = [mm.group(1) for mm in lit_re.finditer(jspart)] + [mm.group(1) for mm in dq_re.finditer(jspart)]
for lit in lits:
    refs.add(lit); refs.add(js_unescape(lit))
    for tn in re.finditer(r'>([^<>]+)<', lit):
        t = tn.group(1).strip()
        if t:
            refs.add(t); refs.add(htmlmod.unescape(t))

rescued, dead = [], []
for rk in raw_keys:
    if variants(rk) & refs:
        rescued.append(rk)
    else:
        dead.append(rk)

print('dict keys:', len(raw_keys), ' in-use:', len(rescued), ' orphan(candidates):', len(dead))
print('--- ORPHAN keys (%d) ---  [仅参考：短词可能经 DOM 文本节点动态翻译在用，勿直接删] ' % len(dead))
for k in sorted(dead):
    print(repr(k))

# -*- coding: utf-8 -*-
"""i18n 反向审计（孤儿检测）——多形态 × 多来源精确匹配版（2026-10-26 升级）。

旧版对含撇号/转义/HTML 实体的键做精确匹配会误报孤儿（10-25 曾据误报误删 3 条在用词条）。
新版：键做 raw/去转义/实体归一/去引号转义四形态；引用来源 = T() 参数 + HTML 静态文本/属性
+ JS 单双引号字面量 + 字面量内 HTML 文本节点。剩余孤儿仍可能被 DOM 文本节点级动态翻译
使用（短词拼接场景），孤儿清单仅作参考，不作为删除依据。"""
import io, re, sys
import html as htmlmod

path = sys.argv[1] if len(sys.argv) > 1 else 'internal/app/admin_html.go'
src = io.open(path, encoding='utf-8').read()

def js_unescape(s):
    out, i = [], 0
    while i < len(s):
        c = s[i]
        if c == '\\' and i + 1 < len(s):
            out.append(s[i+1]); i += 2
        else:
            out.append(c); i += 1
    return ''.join(out)

m = re.search(r'const I18N_ZH = \{(.*?)\n\};', src, re.S)
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
# 3) JS 区字符串字面量 + 大串内 HTML 文本节点
jspart = src_nodict[html_end:]
BS = chr(92)
lit_re = re.compile(chr(39) + r'((?:[^' + chr(39) + BS + chr(10) + r']|' + BS + r'.){2,})' + chr(39))
dq_re = re.compile(chr(34) + r'((?:[^' + chr(34) + BS + chr(10) + r']|' + BS + r'.){2,})' + chr(34))
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

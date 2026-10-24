import re, io, sys

path = sys.argv[1] if len(sys.argv) > 1 else 'internal/app/admin_html.go'
src = io.open(path, encoding='utf-8').read()

def js_unescape(s):
    out = []
    i = 0
    while i < len(s):
        c = s[i]
        if c == '\\' and i + 1 < len(s):
            n = s[i+1]
            if n == 'n': out.append('\n')
            elif n == 't': out.append('\t')
            else: out.append(n)
            i += 2
        else:
            out.append(c)
            i += 1
    return ''.join(out)

m = re.search(r'const I18N_ZH = \{', src)
start = m.end()
end = src.index('\n};', start)
block = src[start:end]
keys = set()
for mm in re.finditer(r"^\s*'((?:[^'\\]|\\.)*)'\s*:", block, re.M):
    keys.add(js_unescape(mm.group(1)))

used = set()
for mm in re.finditer(r"\bT\('((?:[^'\\]|\\.)*)'\)", src):
    used.add(js_unescape(mm.group(1)))
for mm in re.finditer(r'\bT\("((?:[^"\\]|\\.)*)"\)', src):
    used.add(js_unescape(mm.group(1)))

missing = sorted(u for u in used if u not in keys)
print('T() usages:', len(used), ' dict keys:', len(keys))
print('--- MISSING zh entries (%d) ---' % len(missing))
for s in missing:
    print(repr(s))

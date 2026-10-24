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
keys = [js_unescape(mm.group(1)) for mm in re.finditer(r"^\s*'((?:[^'\\]|\\.)*)'\s*:", block, re.M)]

# T() usages
used = set()
for mm in re.finditer(r"\bT\('((?:[^'\\]|\\.)*)'\)", src):
    used.add(js_unescape(mm.group(1)))
for mm in re.finditer(r'\bT\("((?:[^"\\]|\\.)*)"\)', src):
    used.add(js_unescape(mm.group(1)))

# static HTML: text nodes + title/placeholder attributes (before <script>)
html_end = src.index('<script>')
html = src[:html_end]
static = set()
for mm in re.finditer(r'>([^<>]+)<', html):
    static.add(mm.group(1).strip())
for mm in re.finditer(r'(?:title|placeholder)="([^"]*)"', html):
    static.add(mm.group(1).strip())

orphans = [k for k in keys if k not in used and k not in static]
print('dict keys:', len(keys), ' T() used:', len(used), ' static texts:', len(static))
print('--- ORPHAN keys (%d) ---' % len(orphans))
for s in sorted(orphans):
    print(repr(s))

package textcall

import (
	"encoding/json"
	"strings"
	"testing"
)

var testNames = []string{"PowerShell", "Bash"}

func TestParseTextCallComplete(t *testing.T) {
	raw, name, args, consumed, st := parseTextCall([]byte(`[调用工具 PowerShell({"command": "Get-Date"})] 后续`))
	if st != tcComplete {
		t.Fatalf("期望 tcComplete，得到 %d", st)
	}
	if name != "PowerShell" || args != `{"command": "Get-Date"}` {
		t.Fatalf("name=%q args=%q", name, args)
	}
	if want := len(`[调用工具 PowerShell({"command": "Get-Date"})]`); consumed != want {
		t.Fatalf("consumed=%d，期望 %d", consumed, want)
	}
	if !strings.HasPrefix(raw, patOpen) {
		t.Fatalf("raw 应以标记开头: %q", raw)
	}
}

func TestParseTextCallNoArgs(t *testing.T) {
	_, name, args, _, st := parseTextCall([]byte(`[调用工具 Ping()]`))
	if st != tcComplete || name != "Ping" || args != "" {
		t.Fatalf("name=%q args=%q st=%d", name, args, st)
	}
}

func TestParseTextCallParensInsideString(t *testing.T) {
	// 参数 JSON 字符串里带括号：配平扫描不能被它截断
	_, _, args, _, st := parseTextCall([]byte(`[调用工具 PowerShell({"command": "Write-Output ('a)' + 'b')"})]`))
	if st != tcComplete {
		t.Fatalf("期望 tcComplete，得到 %d", st)
	}
	var v any
	if json.Unmarshal([]byte(args), &v) != nil {
		t.Fatalf("args 应为合法 JSON: %q", args)
	}
}

func TestParseTextCallMalformed(t *testing.T) {
	cases := []string{
		`[调用工具 文档]`,             // 非调用：讲解性文本
		`[调用工具 foo(不是json)]`,    // 参数不是 JSON
		`[调用工具 foo({"a":1]})]`,  // 括号类型不配平
		`[调用工具 foo({"a":1}) x]`, // 缺右括号收尾
	}
	for _, c := range cases {
		if _, _, _, _, st := parseTextCall([]byte(c)); st != tcMalformed {
			t.Fatalf("%q 期望 tcMalformed，得到 %d", c, st)
		}
	}
}

func TestParseTextCallIncomplete(t *testing.T) {
	cases := []string{
		`[调用工具 Power`,            // 名字未完
		`[调用工具 PowerShell({"a":`, // JSON 未闭合
		`[调用工具`,
	}
	for _, c := range cases {
		if _, _, _, _, st := parseTextCall([]byte(c)); st != tcIncomplete {
			t.Fatalf("%q 期望 tcIncomplete，得到 %d", c, st)
		}
	}
}

func TestParseTextCallMissingParenTolerated(t *testing.T) {
	_, name, args, _, st := parseTextCall([]byte(`[调用工具 Bash({"a": 1}]`))
	if st != tcComplete || name != "Bash" || args != `{"a": 1}` {
		t.Fatalf("漏写 ) 应容错: st=%d name=%q args=%q", st, name, args)
	}
}

func TestPrefixHold(t *testing.T) {
	if got := prefixHold([]byte("abc[调用工")); got != len("[调用工") {
		t.Fatalf("prefixHold=%d", got)
	}
	if got := prefixHold([]byte("正常文本")); got != 0 {
		t.Fatalf("普通文本不应扣住，得到 %d", got)
	}
}

func TestFilterAcrossDeltas(t *testing.T) {
	f := New(testNames, 0)
	var out strings.Builder
	for _, p := range []string{
		"先执行第一步。",
		"[调用工",
		`具 PowerShell({"command"`,
		`: "Get-Date"})]`,
		"执行完了。",
	} {
		out.WriteString(f.Feed(p))
	}
	tail, calls := f.Finish()
	out.WriteString(tail)
	if got := out.String(); got != "先执行第一步。执行完了。" {
		t.Fatalf("正文=%q", got)
	}
	if len(calls) != 1 || calls[0].Name != "PowerShell" || calls[0].Args != `{"command": "Get-Date"}` {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestFilterUnknownToolStaysProse(t *testing.T) {
	f := New(testNames, 0)
	out := f.Feed(`参见 [调用工具 Other({"a":1})] 的说明`)
	out += mustTail(f)
	if !strings.Contains(out, `[调用工具 Other({"a":1})]`) {
		t.Fatalf("未知工具名应按正文放行，得到 %q", out)
	}
}

func TestFilterCaseInsensitiveName(t *testing.T) {
	f := New(testNames, 0)
	out := f.Feed(`[调用工具 bash({"command": "pwd"})]`)
	out += mustTail(f)
	if out != "" {
		t.Fatalf("小写名应归一转换，正文应空: %q", out)
	}
	_, calls := f.Finish()
	if len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("应归一为声明名 Bash: %+v", calls)
	}
}

func TestFilterDisabledWithoutNames(t *testing.T) {
	f := New(nil, 0)
	out := f.Feed(`[调用工具 PowerShell({"command": "x"})]`)
	tail, calls := f.Finish()
	if out+tail != `[调用工具 PowerShell({"command": "x"})]` || len(calls) != 0 {
		t.Fatalf("无 names 时应整体直通")
	}
}

func TestFilterLogsMalformed(t *testing.T) {
	f := New(testNames, 0)
	out := f.Feed(`看 [调用工具 Bash(没有参数结构)] 完了`)
	out += mustTail(f)
	if len(f.rejects) == 0 {
		t.Fatal("畸形调用应记录拒绝原因")
	}
	if !strings.Contains(out, "[调用工具 Bash(没有参数结构)]") {
		t.Fatalf("畸形调用应按正文放行: %q", out)
	}
}

func TestFilterLogsUnclosed(t *testing.T) {
	f := New(testNames, 0)
	f.Feed(`[调用工具 Bash({"command": "echo hi`) // 流在此截断
	tail, calls := f.Finish()
	if len(calls) != 0 {
		t.Fatal("未闭合调用不应转换")
	}
	if len(f.rejects) == 0 {
		t.Fatal("未闭合调用应记录诊断")
	}
	if strings.Contains(tail, "[调用工具") {
		t.Fatalf("半截调用应丢弃而非放行，tail=%q", tail)
	}
}

// 2026-11-13/14 生产实测：模型把模仿调用包进 XML 包装标记，内层调用被转换后
// 包装标记残留成孤儿标签透传到客户端。独立成行的包装标记应整行剥离。
func TestFilterStripsWrapperTagLines(t *testing.T) {
	f := New(testNames, 0)
	out := f.Feed("开始整理。\n<tool calls>\n[调用工具 Bash({\"command\": \"pwd\"})]\n</tool calls>\n")
	tail, calls := f.Finish()
	out += tail
	if len(calls) != 1 {
		t.Fatalf("内层调用应转换，得到 %d；拒绝=%v", len(calls), f.rejects)
	}
	if strings.Contains(out, "tool calls") || strings.Contains(out, "tool_calls") {
		t.Fatalf("包装标记应剥离: %q", out)
	}
	if out != "开始整理。\n" {
		t.Fatalf("正文=%q", out)
	}
	if f.Stripped() != 2 {
		t.Fatalf("应剥离 2 行，得到 %d", f.Stripped())
	}
}

// 标记行跨增量到达（半个闭合标签 + 另半个）也要剥掉。
func TestFilterStripsWrapperTagAcrossDeltas(t *testing.T) {
	f := New(testNames, 0)
	var out strings.Builder
	out.WriteString(f.Feed("abc\n</tool"))
	out.WriteString(f.Feed(" calls>\n完成"))
	tail, _ := f.Finish()
	out.WriteString(tail)
	if strings.Contains(out.String(), "calls>") {
		t.Fatalf("跨增量的标记行应剥离: %q", out.String())
	}
	if !strings.Contains(out.String(), "abc\n完成") {
		t.Fatalf("正文=%q", out.String())
	}
}

// 行内出现（同一行还有其他文字）不剥——讲解该格式的正文不受影响。
func TestFilterKeepsInlineWrapperTags(t *testing.T) {
	f := New(testNames, 0)
	out := f.Feed("参见 <tool calls> 标记的说明\n下一行\n")
	tail, _ := f.Finish()
	out += tail
	if !strings.Contains(out, "<tool calls>") {
		t.Fatalf("行内标记不应剥离: %q", out)
	}
}

func mustTail(f *Filter) string {
	tail, _ := f.Finish()
	return tail
}

// 2026-11-04 02:20 WorkBuddy 实测案例：模型手写模仿时同时犯了两个毛病——
// 结尾漏写 `)`、命令里的 ssh 嵌套引号未转义。修复后应当完整转换为结构化调用，
// 且解码后的 command 保留原始 shell 语义。
func TestReproKeepaliveCallConverted(t *testing.T) {
	raw := `[调用工具 Bash({"command": "ssh -i ~/.ssh/id_ed25519_vps -o IdentitiesOnly=yes -o ConnectTimeout=10 root@2a02:180:6:1::7aa "sh /root/instabox-keepalive/keepalive.sh --status 2>&1 | grep -E '账号|state|续租'" 2>&1", "description": "查 keepalive 保活的机器列表与状态"}]`
	f := New([]string{"Bash"}, 0)
	out := f.Feed(raw)
	tail, calls := f.Finish()
	if len(calls) != 1 {
		t.Fatalf("应转换出 1 个调用，得到 %d；正文=%q 拒绝=%v", len(calls), out+tail, f.rejects)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(calls[0].Args), &v); err != nil {
		t.Fatalf("参数应合法: %v", err)
	}
	cmd, _ := v["command"].(string)
	want := `sh /root/instabox-keepalive/keepalive.sh --status 2>&1 | grep -E '账号|state|续租'`
	if !strings.Contains(cmd, want) {
		t.Fatalf("解码后命令不符: %q", cmd)
	}
}

func TestAcceptCallArgsRepairInvalidEscape(t *testing.T) {
	raw := `{"command":"cd \"/d/下载/Github/afree-proxy\" && grep -rn \"RefreshAccount(\|refreshAccount(\" internal/ --include=\"*.go\" | grep -v \"_test\|func \" | head -20","description":"查找 RefreshAccount 全部调用点"}`
	var probe any
	if json.Unmarshal([]byte(raw), &probe) == nil {
		t.Fatal("前置条件：raw 应当是非法 JSON（含 \\| 转义）")
	}
	fixed, repaired, ok := acceptCallArgs(raw)
	if !ok || !repaired {
		t.Fatalf("应修复成功: ok=%v repaired=%v", ok, repaired)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(fixed), &v); err != nil {
		t.Fatalf("修复后仍非法: %v", err)
	}
	cmd, _ := v["command"].(string)
	if !strings.Contains(cmd, `grep -rn "RefreshAccount(\|refreshAccount("`) {
		t.Fatalf("解码后命令不符: %q", cmd)
	}
}

func TestAcceptCallArgsValidUnchanged(t *testing.T) {
	raw := `{"command": "Get-Date", "timeout_seconds": 5}`
	fixed, repaired, ok := acceptCallArgs(raw)
	if !ok || repaired || fixed != raw {
		t.Fatalf("合法 JSON 不应修复: fixed=%q repaired=%v ok=%v", fixed, repaired, ok)
	}
}

func TestAcceptCallArgsUnrepairable(t *testing.T) {
	if _, _, ok := acceptCallArgs(`{"command" "x"}`); ok {
		t.Fatal("结构性损坏应判不可用")
	}
}

// 2026-11-06 病态输出：单响应 343 连发。上限应转换前 N 个；溢出按正文**丢弃**
// （2026-11-14 改：放行改丢弃——溢出模仿文本无可执行价值，放行只会随历史回流
// 强化模仿）。
func TestFilterCapDropsExcess(t *testing.T) {
	f := New(testNames, 3)
	var out strings.Builder
	for i := 0; i < 6; i++ {
		out.WriteString(f.Feed(`[调用工具 Bash({"command": "echo ` + string(rune('a'+i)) + `"})]`))
	}
	tail, calls := f.Finish()
	if len(calls) != 3 {
		t.Fatalf("上限 3 应只转换 3 个，得到 %d", len(calls))
	}
	if f.Dropped() != 3 {
		t.Fatalf("应记录 3 个溢出，得到 %d", f.Dropped())
	}
	if n := strings.Count(out.String()+tail, `[调用工具 Bash(`); n != 0 {
		t.Fatalf("溢出调用应丢弃，正文仍残留 %d 个", n)
	}
}

func TestExtractTextToolCallsOneShot(t *testing.T) {
	content := `第一步完成。[调用工具 PowerShell({"command": "Get-Location"})]` + "\n" + `[调用工具 Bash({"command": "pwd"})] 结束`
	prose, calls, changed, capped := ExtractTextToolCalls(content, testNames, 0)
	if !changed || capped != 0 {
		t.Fatalf("changed=%v capped=%d", changed, capped)
	}
	if prose != "第一步完成。\n 结束" {
		t.Fatalf("prose=%q", prose)
	}
	if len(calls) != 2 || calls[0].Name != "PowerShell" || calls[1].Name != "Bash" {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestExtractTextToolCallsCap(t *testing.T) {
	content := strings.Repeat(`[调用工具 Bash({"command": "x"})]`, 5)
	prose, calls, changed, capped := ExtractTextToolCalls(content, testNames, 2)
	if !changed || len(calls) != 2 || capped != 3 {
		t.Fatalf("calls=%d capped=%d", len(calls), capped)
	}
	if prose != "" {
		t.Fatalf("溢出调用应丢弃，prose=%q", prose)
	}
}

// 非流式路径同样剥独立成行的包装标记；只有标记、没有方括号调用时也要剥到。
func TestExtractStripsWrapperTagLines(t *testing.T) {
	content := "<tool calls>\n[调用工具 Bash({\"command\": \"pwd\"})]\n</tool calls>\n完成"
	prose, calls, changed, _ := ExtractTextToolCalls(content, testNames, 0)
	if len(calls) != 1 || !changed {
		t.Fatalf("calls=%d changed=%v", len(calls), changed)
	}
	if prose != "完成" {
		t.Fatalf("prose=%q", prose)
	}
	tagOnly := "<tool calls>\n</tool calls>\n"
	prose, calls, changed, _ = ExtractTextToolCalls(tagOnly, testNames, 0)
	if len(calls) != 0 || !changed || prose != "" {
		t.Fatalf("纯标记内容也应剥净: prose=%q calls=%d changed=%v", prose, len(calls), changed)
	}
}

func TestExtractTextToolCallsNoNames(t *testing.T) {
	content := `[调用工具 PowerShell({"command": "x"})]`
	prose, calls, changed, _ := ExtractTextToolCalls(content, nil, 0)
	if changed || len(calls) != 0 || prose != content {
		t.Fatalf("无 names 时不应改写: changed=%v", changed)
	}
}

// args 配平完整但含非 JSON 语法（生产 2026-11-14 实测：模型写 Python 风格
// `True`，lenient 修复器只作用于字符串字面量内部、不修裸标识符）应丢弃而不是
// 放行——放行会随历史回流强化模型模仿。注意日志里的 args 截断是 %.120q
// 展示截断，不代表模型写到一半。
func TestFilterUnrepairableArgsDropped(t *testing.T) {
	f := New(testNames, 0)
	out := f.Feed(`步骤 [调用工具 Bash({"pattern": "x", "path": "a.py", "-n": True})] 收尾`)
	out += mustTail(f)
	if strings.Contains(out, "[调用工具") {
		t.Fatalf("不可修复 args 应丢弃: %q", out)
	}
	if !strings.Contains(out, "步骤") || !strings.Contains(out, "收尾") {
		t.Fatalf("正常正文应保留: %q", out)
	}
	if _, calls := f.Finish(); len(calls) != 0 {
		t.Fatalf("不可修复 args 不应产生调用: %+v", calls)
	}
}

func TestExtractUnrepairableArgsDropped(t *testing.T) {
	content := `前文 [调用工具 Bash({"pattern": "x", "path": "a.py", "-n": True})] 后文`
	prose, calls, changed, _ := ExtractTextToolCalls(content, testNames, 0)
	if !changed || len(calls) != 0 || strings.Contains(prose, "[调用工具") {
		t.Fatalf("changed=%v calls=%d prose=%q", changed, len(calls), prose)
	}
	if !strings.Contains(prose, "前文") || !strings.Contains(prose, "后文") {
		t.Fatalf("正常正文应保留: %q", prose)
	}
}

// 超限丢弃轮次的 changed 亦须置位（正常路径由前置成功转换置位，此处防御性锁定）。
func TestExtractOverflowSetsChanged(t *testing.T) {
	content := `[调用工具 Bash({"command": "a"})][调用工具 Bash({"command": "b"})]`
	prose, calls, changed, capped := ExtractTextToolCalls(content, testNames, 1)
	if len(calls) != 1 || capped != 1 || !changed || prose != "" {
		t.Fatalf("calls=%d capped=%d changed=%v prose=%q", len(calls), capped, changed, prose)
	}
}

package server

import (
	"encoding/json"
	"strings"
	"testing"

	"afree-proxy/internal/trae2api/traectl"
)

var testTools = []traectl.Tool{{Name: "PowerShell"}, {Name: "Bash"}}

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
	if jsonUnmarshalCheck(args, &v) != nil {
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
		`[调用工具 Power`,           // 名字未完
		`[调用工具 PowerShell({"a":`, // JSON 未闭合
		`[调用工具`,
	}
	for _, c := range cases {
		if _, _, _, _, st := parseTextCall([]byte(c)); st != tcIncomplete {
			t.Fatalf("%q 期望 tcIncomplete，得到 %d", c, st)
		}
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

func TestTextCallFilterAcrossDeltas(t *testing.T) {
	var out strings.Builder
	f := newTextCallFilter(testTools, func(s string) { out.WriteString(s) })
	parts := []string{
		"先执行第一步。",
		"[调用工",
		`具 PowerShell({"command"`,
		`: "Get-Date"})]`,
		"执行完了。",
	}
	for _, p := range parts {
		f.feed(p)
	}
	calls := f.finish()
	if got := out.String(); got != "先执行第一步。执行完了。" {
		t.Fatalf("正文=%q", got)
	}
	if len(calls) != 1 || calls[0].name != "PowerShell" || calls[0].args != `{"command": "Get-Date"}` {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestTextCallFilterUnknownToolStaysProse(t *testing.T) {
	var out strings.Builder
	f := newTextCallFilter(testTools, func(s string) { out.WriteString(s) })
	f.feed(`参见 [调用工具 Other({"a":1})] 的说明`)
	f.finish()
	if !strings.Contains(out.String(), `[调用工具 Other({"a":1})]`) {
		t.Fatalf("未知工具名应按正文放行，得到 %q", out.String())
	}
}

func TestTextCallFilterDisabledWithoutTools(t *testing.T) {
	var out strings.Builder
	f := newTextCallFilter(nil, func(s string) { out.WriteString(s) })
	f.feed(`[调用工具 PowerShell({"command": "x"})]`)
	f.finish()
	if out.String() != `[调用工具 PowerShell({"command": "x"})]` {
		t.Fatalf("无 tools 时应整体直通，得到 %q", out.String())
	}
}

func TestExtractTextToolCallsOneShot(t *testing.T) {
	content := `第一步完成。[调用工具 PowerShell({"command": "Get-Location"})]` + "\n" + `[调用工具 Bash({"command": "pwd"})] 结束`
	prose, calls, changed := extractTextToolCalls(content, testTools)
	if !changed {
		t.Fatal("应检测到调用")
	}
	if prose != "第一步完成。\n 结束" {
		t.Fatalf("prose=%q", prose)
	}
	if len(calls) != 2 || calls[0].name != "PowerShell" || calls[1].name != "Bash" {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestExtractTextToolCallsNoTools(t *testing.T) {
	content := `[调用工具 PowerShell({"command": "x"})]`
	prose, calls, changed := extractTextToolCalls(content, nil)
	if changed || len(calls) != 0 || prose != content {
		t.Fatalf("无 tools 时不应改写: changed=%v", changed)
	}
}

func jsonUnmarshalCheck(s string, v *any) error { return json.Unmarshal([]byte(s), v) }

// 2026-11-04 02:20 WorkBuddy 实测案例：模型手写模仿时同时犯了两个毛病——
// 结尾漏写 `)`、命令里的 ssh 嵌套引号未转义。修复后应当完整转换为 tool_calls，
// 且解码后的 command 保留原始 shell 语义（内层引号、`\|` 等）。
func TestReproKeepaliveCallConverted(t *testing.T) {
	raw := `[调用工具 Bash({"command": "ssh -i ~/.ssh/id_ed25519_vps -o IdentitiesOnly=yes -o ConnectTimeout=10 root@2a02:180:6:1::7aa "sh /root/instabox-keepalive/keepalive.sh --status 2>&1 | grep -E '账号|state|续租'" 2>&1", "description": "查 keepalive 保活的机器列表与状态"}]`
	var out strings.Builder
	f := newTextCallFilter([]traectl.Tool{{Name: "Bash"}}, func(s string) { out.WriteString(s) })
	f.feed(raw)
	calls := f.finish()
	if len(calls) != 1 {
		t.Fatalf("应转换出 1 个调用，得到 %d；正文=%q 拒绝=%v", len(calls), out.String(), f.takeRejections())
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(calls[0].args), &v); err != nil {
		t.Fatalf("参数应合法: %v", err)
	}
	cmd, _ := v["command"].(string)
	want := `sh /root/instabox-keepalive/keepalive.sh --status 2>&1 | grep -E '账号|state|续租'`
	if !strings.Contains(cmd, want) {
		t.Fatalf("解码后命令不符: %q", cmd)
	}
}

func TestParseTextCallMissingParenTolerated(t *testing.T) {
	_, name, args, _, st := parseTextCall([]byte(`[调用工具 Bash({"a": 1}]`))
	if st != tcComplete || name != "Bash" || args != `{"a": 1}` {
		t.Fatalf("漏写 ) 应容错: st=%d name=%q args=%q", st, name, args)
	}
}

func TestTextCallFilterLogsMalformed(t *testing.T) {
	var out strings.Builder
	f := newTextCallFilter(testTools, func(s string) { out.WriteString(s) })
	f.feed(`看 [调用工具 Bash(没有参数结构)] 完了`)
	f.finish()
	if len(f.rejected) == 0 {
		t.Fatal("畸形调用应记录拒绝原因")
	}
	if !strings.Contains(out.String(), "[调用工具 Bash(没有参数结构)]") {
		t.Fatalf("畸形调用应按正文放行: %q", out.String())
	}
}

func TestTextCallFilterLogsUnclosed(t *testing.T) {
	var out strings.Builder
	f := newTextCallFilter(testTools, func(s string) { out.WriteString(s) })
	f.feed(`[调用工具 Bash({"command": "echo hi`) // 流在此截断
	calls := f.finish()
	if len(calls) != 0 {
		t.Fatal("未闭合调用不应转换")
	}
	if len(f.rejected) == 0 {
		t.Fatal("未闭合调用应记录诊断")
	}
}

// 2026-11-04 实测案例：模型模仿历史编码格式时，命令里的 grep BRE 转义 `\|`
// 被原样写进参数 JSON —— `\|` 是非法 JSON 转义，修复前这类调用全被当正文
// 放行（WorkBuddy 旧会话"只有工具调用字符串"的残留原因）。
func TestAcceptCallArgsRepairInvalidEscape(t *testing.T) {
	raw := `{"command":"cd \"/d/下载/Github/afree-proxy\" && grep -rn \"RefreshAccount(\|refreshAccount(\" internal/ --include=\"*.go\" | grep -v \"_test\|func \" | head -20","description":"查找 RefreshAccount 全部调用点"}`
	var probe any
	if json.Unmarshal([]byte(raw), &probe) == nil {
		t.Fatal("前置条件：raw 应当是非法 JSON（含 \\| 转义）")
	}
	fixed, repaired, ok := acceptCallArgs(raw)
	if !ok {
		t.Fatal("应修复成功")
	}
	if !repaired {
		t.Fatal("应标记 repaired")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(fixed), &v); err != nil {
		t.Fatalf("修复后仍非法: %v", err)
	}
	cmd, _ := v["command"].(string)
	// 解码后命令必须原样保留 grep BRE 的 \|（反斜杠+竖线）
	if !strings.Contains(cmd, `grep -rn "RefreshAccount(\|refreshAccount("`) {
		t.Fatalf("解码后命令不符合预期: %q", cmd)
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
	// 结构性损坏（缺冒号）：修复转义救不了
	if _, _, ok := acceptCallArgs(`{"command" "x"}`); ok {
		t.Fatal("结构性损坏应判不可用")
	}
}

func TestAcceptCallArgsBadUnicodeEscape(t *testing.T) {
	fixed, repaired, ok := acceptCallArgs(`{"command":"\u00zz echo hi"}`)
	if !ok || !repaired {
		t.Fatalf("坏 \\u 转义应修复: ok=%v repaired=%v", ok, repaired)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(fixed), &v); err != nil {
		t.Fatalf("修复后仍非法: %v", err)
	}
}

func TestTextCallFilterRepairsArgs(t *testing.T) {
	var out strings.Builder
	f := newTextCallFilter(testTools, func(s string) { out.WriteString(s) })
	f.feed(`执行 [调用工具 Bash({"command":"grep -rn \"A(\|B(\" ."})] 完成后继续`)
	calls := f.finish()
	if len(calls) != 1 {
		t.Fatalf("应解析出 1 个调用，得到 %d；正文=%q", len(calls), out.String())
	}
	if !calls[0].repaired {
		t.Fatal("应标记 repaired")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(calls[0].args), &v); err != nil {
		t.Fatalf("修复后参数仍非法: %v", err)
	}
}

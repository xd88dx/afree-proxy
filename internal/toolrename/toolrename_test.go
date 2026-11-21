package toolrename

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"afree-proxy/internal/gwconfig"
)

// withRenameConfig 测试期启用改名配置（隔离到临时配置文件）。
func withRenameConfig(t *testing.T, prefix string, platforms map[string]bool) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "afree-config.json")
	_ = os.WriteFile(path, []byte("{}"), 0600)
	saved := gwconfig.GlobalConfigPathOverride
	gwconfig.GlobalConfigPathOverride = path
	savedCfg := *gwconfig.GetToolRename()
	t.Cleanup(func() {
		gwconfig.GlobalConfigPathOverride = saved
		_ = gwconfig.SetToolRename(savedCfg)
	})
	_ = os.Remove(path)
	if err := gwconfig.SetToolRename(gwconfig.ToolRenameConfig{Prefix: prefix, Platforms: platforms}); err != nil {
		t.Fatalf("set config: %v", err)
	}
}

func TestBuildGatedByPlatform(t *testing.T) {
	withRenameConfig(t, "zkx", map[string]bool{"zen": true})
	// zen 勾选 → 有映射
	m := Build(KindZen, []string{"Read", "Bash"})
	if m == nil || len(m.RealToFake) != 2 {
		t.Fatalf("zen 应有映射, got %v", m)
	}
	// cline 未勾选 → nil
	if m := Build(KindCline, []string{"Read"}); m != nil {
		t.Fatalf("未勾选平台应返回 nil")
	}
	// 无工具 → nil
	if m := Build(KindZen, nil); m != nil {
		t.Fatalf("无工具应返回 nil")
	}
}

func TestFakeOfDeterministic(t *testing.T) {
	a := FakeOf("Read", "zkx")
	b := FakeOf("Read", "zkx")
	if a != b {
		t.Fatalf("假名不确定: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "zkx_") || len(a) != len("zkx_")+12 {
		t.Fatalf("假名形态错误: %q", a)
	}
	if FakeOf("Read", "other") == a {
		t.Fatal("不同前缀应产生不同假名")
	}
}

func TestRewriteChatForm(t *testing.T) {
	withRenameConfig(t, "zkx", map[string]bool{"zen": true})
	m := Build(KindZen, []string{"Read", "Bash"})
	params := map[string]any{
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "Read", "description": "read files"}},
			map[string]any{"type": "function", "function": map[string]any{"name": "UnknownTool"}},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "tool_calls": []any{
				map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "Read", "arguments": "{}"}},
			}},
		},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "Bash"}},
	}
	RewriteChatForm(params, m)

	fakeRead := m.RealToFake["Read"]
	// tools：真名改写、映射外原样
	t0 := params["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"].(string)
	if t0 != fakeRead {
		t.Fatalf("tools 未改写: %q", t0)
	}
	t1 := params["tools"].([]any)[1].(map[string]any)["function"].(map[string]any)["name"].(string)
	if t1 != "UnknownTool" {
		t.Fatalf("映射外工具被误改: %q", t1)
	}
	// 历史 tool_calls
	tc := params["messages"].([]any)[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"].(string)
	if tc != fakeRead {
		t.Fatalf("历史 tool_calls 未改写: %q", tc)
	}
	// tool_choice
	ch := params["tool_choice"].(map[string]any)["function"].(map[string]any)["name"].(string)
	if ch != m.RealToFake["Bash"] {
		t.Fatalf("tool_choice 未改写: %q", ch)
	}
}

func TestRewriteChatFormCopyDiscipline(t *testing.T) {
	withRenameConfig(t, "zkx", map[string]bool{"zen": true})
	m := Build(KindZen, []string{"Read"})
	tools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "Read"}}}
	params := map[string]any{"tools": tools}
	RewriteChatForm(params, m)
	// 原 params 的底层切片/map 不得被污染（防污染纪律）。
	if tools[0].(map[string]any)["function"].(map[string]any)["name"] != "Read" {
		t.Fatal("调用方切片被污染")
	}
}

func TestRewriteResponsesForm(t *testing.T) {
	withRenameConfig(t, "zkx", map[string]bool{"zen": true})
	m := Build(KindZen, []string{"Read"})
	obj := map[string]any{
		"tools": []any{map[string]any{"type": "function", "name": "Read", "parameters": map[string]any{}}},
		"input": []any{
			map[string]any{"type": "message", "role": "user"},
			map[string]any{"type": "function_call", "name": "Read", "call_id": "c1"},
		},
	}
	RewriteResponsesForm(obj, m)
	fake := m.RealToFake["Read"]
	if got := obj["tools"].([]any)[0].(map[string]any)["name"].(string); got != fake {
		t.Fatalf("flat tools 未改写: %q", got)
	}
	if got := obj["input"].([]any)[1].(map[string]any)["name"].(string); got != fake {
		t.Fatalf("input function_call 未改写: %q", got)
	}
}


// rwOf 给 bytes.Buffer 补 http.ResponseWriter 骨架（测试用最小桩）。
type stubRW struct{ buf *bytes.Buffer }

func (s *stubRW) Header() http.Header { return http.Header{} }
func (s *stubRW) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *stubRW) WriteHeader(int) {}

func rwOf(buf *bytes.Buffer) http.ResponseWriter { return &stubRW{buf: buf} }

func TestRestoreWriter(t *testing.T) {
	withRenameConfig(t, "zkx", map[string]bool{"zen": true})
	m := Build(KindZen, []string{"Read", "Bash"})
	fakeR := m.RealToFake["Read"]
	fakeB := m.RealToFake["Bash"]

	t.Run("整段替换", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewRestoreWriter(rwOf(&buf), m)
		_, _ = w.Write([]byte(`{"tool_calls":[{"function":{"name":"` + fakeR + `"}}]} done `))
		got := buf.String()
		if !strings.Contains(got, `"name":"Read"`) || strings.Contains(got, fakeR) {
			t.Fatalf("还原失败: %s", got)
		}
	})

	t.Run("跨 Write 边界", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewRestoreWriter(rwOf(&buf), m)
		// 在假名中间撕开
		half := len(fakeB) / 2
		_, _ = w.Write([]byte(`{"name":"` + fakeB[:half]))
		_, _ = w.Write([]byte(fakeB[half:] + `","x":1}`))
		got := buf.String()
		if !strings.Contains(got, `"name":"Bash"`) || strings.Contains(got, fakeB) {
			t.Fatalf("跨边界还原失败: %s", got)
		}
	})

	t.Run("普通内容零改动", func(t *testing.T) {
		var buf bytes.Buffer
		w := NewRestoreWriter(rwOf(&buf), m)
		src := `data: {"choices":[{"delta":{"content":"hello world"}}]}` + "\n\n"
		_, _ = w.Write([]byte(src))
		if buf.String() != src {
			t.Fatalf("普通内容被误改: %s", buf.String())
		}
	})
}

func TestNormalizePrefixRules(t *testing.T) {
	cases := []struct {
		prefix string
		ok     bool
	}{
		{"zkx", true}, {"MyGW_2", true}, {"", false}, {"1abc", false},
		{"_abc", false}, {"has space", false}, {"clint", false}, {"mcp", false},
		{"aaaaaaaaaaaaaaaaa", false}, // 17 字符
	}
	for _, c := range cases {
		cfg := gwconfig.ToolRenameConfig{Prefix: c.prefix, Platforms: map[string]bool{"zen": true}}
		err := cfg.Normalize()
		if c.ok && err != nil {
			t.Errorf("prefix %q 应通过, got %v", c.prefix, err)
		}
		if !c.ok && err == nil {
			t.Errorf("prefix %q 应拒绝", c.prefix)
		}
	}
	// 无勾选平台=关闭，不校验前缀
	cfg := gwconfig.ToolRenameConfig{Prefix: ""}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("无勾选平台不应校验: %v", err)
	}
}

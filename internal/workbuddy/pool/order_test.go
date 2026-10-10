package pool

import (
	"path/filepath"
	"reflect"
	"testing"

	"afree-proxy/internal/workbuddy/auth"
)

func listUIDs(p *Pool) []string {
	var out []string
	for _, s := range p.List() {
		out = append(out, s.UID)
	}
	return out
}

// TestSetOrderAndList 管理面板拖拽排序契约：SetOrder 只改 List()/面板的展示顺序，
// 未入序账号按 UID 排尾部；未知 uid 忽略。选号路由（Pick）不受顺序影响——
// 加权路由语义见 pick.go，这里只锁「SetOrder 不碰运行态」这一条。
func TestSetOrderAndList(t *testing.T) {
	p := New("")
	defer p.Close()
	for _, uid := range []string{"b", "a", "c"} {
		p.Add(&auth.Auth{UID: uid})
	}
	// 无自定义顺序：退回 UID 排序（原行为）
	if got := listUIDs(p); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("default order = %v, want [a b c]", got)
	}
	// 拖拽 c 到最前；未知 uid "ghost" 忽略；未提及的 b 按 UID 排尾部
	if n := p.SetOrder([]string{"c", "a", "ghost"}); n != 3 {
		t.Fatalf("SetOrder n = %d, want 3 (ghost ignored, b appended)", n)
	}
	if got := listUIDs(p); !reflect.DeepEqual(got, []string{"c", "a", "b"}) {
		t.Fatalf("custom order = %v, want [c a b]", got)
	}
	// 运行态不受影响：重复 SetOrder / List 不改 counts
	total, healthy, cooling, disabled, _ := p.CountsDetailed()
	if total != 3 || healthy != 3 || cooling != 0 || disabled != 0 {
		t.Fatalf("counts = %d/%d/%d/%d, want 3/3/0/0", total, healthy, cooling, disabled)
	}
}

// TestSetOrderPersistRoundTrip 顺序持久化：state.json 落盘 → 新池 load 恢复后
// List 仍按自定义顺序输出。
func TestSetOrderPersistRoundTrip(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "state.json")
	p1 := New(fp)
	for _, uid := range []string{"b", "a", "c"} {
		p1.Add(&auth.Auth{UID: uid})
	}
	if n := p1.SetOrder([]string{"c", "a", "b"}); n != 3 {
		t.Fatalf("SetOrder n = %d, want 3", n)
	}
	p1.Flush()
	p1.Close()

	p2 := New(fp)
	defer p2.Close()
	if got := listUIDs(p2); !reflect.DeepEqual(got, []string{"c", "a", "b"}) {
		t.Fatalf("restored order = %v, want [c a b]", got)
	}
}

package panel

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// 本文件覆盖「官方补偿」卡片（issue #189）的前端纯函数与渲染。
//
// 为什么要有：compPending 是「能不能领」的唯一判据，compRemainText 决定用户
// 会不会错过截止日，renderCompensation 决定**失败会不会显形**——三者都只在浏览器里
// 生效，Go 侧不执行 JS，编译与后端测试对它们完全失明。判据写反或让错误静默隐藏，
// 都不报错、只能肉眼看，故在此用 node harness 钉死。
//
// misakano 同步（b51ce6d）后前端 API 升级：compPending(r) 扁平字段布尔化、
// compDeadlineText → compRemainText(ms)、renderCompensation() 无参读全局 compData。
// 旧版按嵌套 status 的锚点全部随合并淘汰（2026-10-23 CI 实测暴露：builder 容器无
// node 时本文件整组 skip，"全绿"掩盖了锚点失配——CI 有 node 兜底抓到）。

func nodeBin(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; compensation frontend test skipped")
	}
	return node
}

func runNodeScript(t *testing.T, node, script, jsFile string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "comp-*.cjs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(script); err != nil {
		t.Fatal(err)
	}
	f.Close()
	out, err := exec.Command(node, f.Name(), jsFile).CombinedOutput()
	if err != nil {
		t.Fatalf("compensation node test failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestAppJSCompPendingGate(t *testing.T) {
	node := nodeBin(t)
	script := `const fs = require('fs');
const vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const start = src.indexOf('function compPending');
const end = src.indexOf('function compRemainText');
if (start < 0 || end < 0 || end < start) throw new Error('compensation region not found');
const ctx = { Array, Object, Number, String, Boolean };
vm.createContext(ctx);
vm.runInContext(src.slice(start, end) + '\nthis.compPending = compPending;', ctx);

const rows = [
  { uid: 'a', active: true,  claimed: false, credit: 500 }, // 待领（唯一应保留）
  { uid: 'b', active: true,  claimed: true,  credit: 500 }, // 已领
  { uid: 'c', active: false, claimed: false, credit: 0   }, // 无活动
  { uid: 'd', active: true,  claimed: false, credit: 0   }, // 额度为 0
  // ↓ 判别力用例：状态本身完全「待领」，只有 skip / error 能排除它们。
  { uid: 'e', skip: 'enterprise', active: true, claimed: false, credit: 500 },
  { uid: 'f', error: 'boom',      active: true, claimed: false, credit: 500 },
  // 缺字段的脏行必须安全返回 false（Boolean 化的意义），不得抛 TypeError。
  { uid: 'k', nickname: '脏数据' },
];
process.stdout.write(JSON.stringify({
  uids: rows.filter(r => ctx.compPending(r)).map(r => r.uid),
  dirtySafe: ctx.compPending(rows[6]) === false,
}));`
	got := runNodeScript(t, node, script, "app.js")
	want := `{"uids":["a"],"dirtySafe":true}`
	if got != want {
		t.Fatalf("compensation gate=%s\nwant %s", got, want)
	}
}

// TestAppJSCompGateMutationDiscriminates 反向证明：把任一判据从 app.js 副本里
// 删掉，本用例必须 FAIL。
//
// 这类「变异测试」是本文件存在的主要理由——正向断言全绿并不代表测到了东西，
// 若样本造得不好，判据会被前面的短路吃掉，删除判据照样全绿。review 时请连同
// app.js 副本一起看。
func TestAppJSCompGateMutationDiscriminates(t *testing.T) {
	node := nodeBin(t)
	mutants := map[string]struct{ from, to string }{
		"drop !r.skip":      {"!r.skip && ", ""},
		"drop !r.error":     {"!r.error && ", ""},
		"drop r.active":     {"r.active && ", ""},
		"drop !claimed":     {"!r.claimed && ", ""},
		"drop credit>0":     {"(r.credit || 0) > 0", "true"},
		"drop Boolean wrap": {"return Boolean(", "return ("},
	}
	for name, m := range mutants {
		t.Run(name, func(t *testing.T) {
			orig, err := os.ReadFile("app.js")
			if err != nil {
				t.Fatal(err)
			}
			mutated := strings.Replace(string(orig), m.from, m.to, 1)
			if mutated == string(orig) {
				t.Fatalf("变异未生效：app.js 里找不到 %q（判据已改写？）", m.from)
			}
			tmp := t.TempDir()
			mutFile := tmp + "/app.js"
			if err := os.WriteFile(mutFile, []byte(mutated), 0o600); err != nil {
				t.Fatal(err)
			}
			script := `const fs = require('fs');
const vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const start = src.indexOf('function compPending');
const end = src.indexOf('function compRemainText');
const ctx = { Array, Object, Number, String, Boolean };
vm.createContext(ctx);
vm.runInContext(src.slice(start, end) + '\nthis.compPending = compPending;', ctx);
const rows = [
  { uid: 'a', active: true,  claimed: false, credit: 500 },
  { uid: 'g', active: true,  claimed: true,  credit: 500 },
  { uid: 'h', active: true,  claimed: false, credit: 0   },
  { uid: 'i', active: false, claimed: false, credit: 500 },
  { uid: 'e', skip: 'enterprise', active: true, claimed: false, credit: 500 },
  { uid: 'f', error: 'boom',      active: true, claimed: false, credit: 500 },
  { uid: 'k', nickname: '脏数据' },
];
process.stdout.write(JSON.stringify(rows.map(r => ctx.compPending(r))));`
			out := runNodeScript(t, node, script, mutFile)
			// 变异被「抓到」有两种形态：① 跑通了但输出变了（判据被删 → 多出账号，
			// 或去掉 Boolean 包装后 undefined 序列化成 null）；② 直接崩了。两者都算
			// 有判别力；只有「跑通且输出与基线完全一致」才是假通过。
			if out == `["a",false,false,false,false,false,false]` {
				t.Fatalf("变异 %s 后 compPending 结果不变 = 本用例对该判据无判别力（假通过）", name)
			}
		})
	}
}

// TestAppJSCompRemainText 钉住截止时间文案：≤48h 小时粒度、超 48h 天粒度、
// 已截止、无截止信息（上游没给 end_time）四态都不抛异常。
func TestAppJSCompRemainText(t *testing.T) {
	node := nodeBin(t)
	script := `const fs = require('fs');
const vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const start = src.indexOf('function compRemainText');
const end = src.indexOf('function renderCompensation');
if (start < 0 || end < 0 || end < start) throw new Error('remain region not found');
const ctx = { Date, Math, Number, String, PK_DAY_MS: 24 * 3600 * 1000 };
vm.createContext(ctx);
vm.runInContext(src.slice(start, end) + '\nthis.f = compRemainText;', ctx);
const H = 3600000, D = 24 * H;
process.stdout.write(JSON.stringify({
  six:   ctx.f(Date.now() + 6 * H), // 只剩 6 小时
  three: ctx.f(Date.now() + 3 * D), // 还剩 3 天
  past:  ctx.f(Date.now() - H),     // 已过截止
  none:  ctx.f(null),               // 上游没给 end_time
}));`
	got := runNodeScript(t, node, script, "app.js")
	for _, must := range []string{
		`"six":"6 小时"`,  // ≤48h 小时粒度
		`"three":"3 天"`,  // 超 48h 天粒度
		`"past":"已截止"`, // 已过截止
		`"none":"—"`,      // 无截止信息
	} {
		if !strings.Contains(got, must) {
			t.Fatalf("compRemainText 缺少 %s\n实际输出: %s", must, got)
		}
	}
}

// TestAppJSRenderCompensationErrorVisibility 钉住本 PR 的核心命题：
// **查询失败的账号必须显形**。上游 5xx / token 过期时所有账号都会落到 error 分支，
// 若渲染把它们和「没有补偿」一起过滤掉，卡片整块消失，用户看到「什么都没有」——
// 而真相是「一个都没查成」。这正是 issue #189 要消灭的静默失败。
func TestAppJSRenderCompensationErrorVisibility(t *testing.T) {
	node := nodeBin(t)
	script := `const fs = require('fs');
const vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const start = src.indexOf('function compEndMs');
const end = src.indexOf('async function loadCompensation');
if (start < 0 || end < 0 || end < start) throw new Error('render region not found');
const sinks = {};
const mk = id => (sinks[id] = { innerHTML: '' });
const ctx = {
  esc: s => String(s == null ? '' : s),
  fmtTok: v => String(v == null ? 0 : v),
  $: id => (sinks[id] || mk(id)),
  applyRestToggle: () => {},
  restOpen: { compBody: false },
  compData: undefined,
  PK_DAY_MS: 24 * 3600 * 1000,
  COMP_SOON_MS: 48 * 3600 * 1000,
  Date, Math, Number, String, Array, Object,
};
vm.createContext(ctx);
vm.runInContext(src.slice(start, end) + '\nthis.render = renderCompensation;', ctx);

const out = {};
// ① 全部查询失败 → 行内必须可见「查询/领取失败」且带出失败原因
ctx.compData = { accounts: [
  { uid: 'a1', nickname: '甲', error: 'boom-1' },
  { uid: 'b2', nickname: '乙', error: 'boom-2' },
] };
ctx.render();
out.allFailedShows = sinks['compBody'].innerHTML.includes('boom-1')
  && sinks['compBody'].innerHTML.includes('查询/领取失败');
out.allFailedNote = sinks['compNote'].innerHTML;

// ② 无活动且无失败 → 正常渲染但行状态是「无活动」
ctx.compData = { accounts: [{ uid: 'a1', nickname: '甲', active: false, claimed: false, credit: 0 }] };
ctx.render();
out.quietShowsIdle = sinks['compBody'].innerHTML.includes('无活动');

// ③ 混合态：一条待领 + 一条失败 → note 同时提到待领与失败，待领行有领取按钮
ctx.compData = { errored: 1, accounts: [
  { uid: 'a1', nickname: '甲', active: true, claimed: false, credit: 500, end_time: '2026-10-22 23:59:59' },
  { uid: 'b2', nickname: '乙', error: 'boom-2' },
] };
ctx.render();
out.mixedHasBtn = sinks['compBody'].innerHTML.includes('data-comp=');
out.mixedNote   = sinks['compNote'].innerHTML;

// ④ 已领取 → 无「领取」按钮
ctx.compData = { accounts: [
  { uid: 'a1', nickname: '甲', active: true, claimed: true, credit: 500 },
] };
ctx.render();
out.claimedNoBtn = !sinks['compBody'].innerHTML.includes('data-comp=');

process.stdout.write(JSON.stringify(out));`
	got := runNodeScript(t, node, script, "app.js")
	for _, must := range []string{
		`"allFailedShows":true`, // ① 全失败必须显形且带原因
		`"quietShowsIdle":true`, // ② 无活动行正常渲染
		`"mixedHasBtn":true`,    // ③ 待领行仍有按钮
		`"claimedNoBtn":true`,   // ④ 已领无按钮
	} {
		if !strings.Contains(got, must) {
			t.Fatalf("render 断言缺少 %s\n实际输出: %s", must, got)
		}
	}
	// note 必须在混合态里同时说出「待领」与「查询失败」两件事。
	if !strings.Contains(got, "查询失败") || !strings.Contains(got, "待领") {
		t.Errorf("note 未同时提示待领与查询失败: %s", got)
	}
}

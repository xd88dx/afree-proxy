package pool

import (
	"encoding/json"
	"testing"
	"time"

	"afree-proxy/internal/workbuddy/auth"
)

// ImportStateBackupJSON 三层口径（2026-11-03 用户定）：
//   - 账号事实（余额/冷却/实测成本/运维意志）无论勾选与否都应用；
//   - 本机统计（token 用量/成功失败计数）仅 includeStats=true 时应用，
//     false 时保留本机现值；
//   - uid 不在池中的条目丢弃（账号集合以 auths 目录为准）。
func TestImportStateBackupJSONFactsAlwaysStatsConditional(t *testing.T) {
	p := New("")
	defer p.Close()
	p.Add(&auth.Auth{UID: "u1"})
	p.Add(&auth.Auth{UID: "u2"})
	// 造本机现值（导入方自己的路由痕迹）：先按 includeStats=true 导入一份
	// 纯统计快照，u1 得到 stats 3/1/55。
	local, err := json.Marshal(stateFile{Accounts: map[string]stateAccount{
		"u1": {SuccessCount: 3, ErrTotal: 1, TokenUsage: TokenUsage{TotalTokens: 55}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := p.ImportStateBackupJSON(local, true); err != nil || n != 1 {
		t.Fatalf("local stats setup = %d err = %v, want 1", n, err)
	}
	// 直接构造备份快照：u1 余额 777 + 统计 9/99；u3 不在池中（必须丢弃）。
	backup := map[string]stateAccount{
		"u1": {Credits: 777, SuccessCount: 9, ErrTotal: 99, TokenUsage: TokenUsage{TotalTokens: 42}},
		"u3": {Credits: 1},
	}
	raw, err := json.Marshal(stateFile{Accounts: backup})
	if err != nil {
		t.Fatal(err)
	}

	// ---- 不勾选统计：事实应用、统计保留本机现值 ----
	n, err := p.ImportStateBackupJSON(raw, false)
	if err != nil || n != 1 {
		t.Fatalf("imported = %d err = %v, want 1 (u3 must be dropped)", n, err)
	}
	st, ok := p.Status("u1")
	if !ok || st.Credits != 777 {
		t.Fatalf("credits (fact) must be applied: %+v ok=%v", st, ok)
	}
	if st.SuccessCount != 3 || st.ErrTotal != 1 || st.TokenUsage.TotalTokens != 55 {
		t.Fatalf("host stats must keep local values when includeStats=false: %+v", st)
	}
	if _, ok := p.Status("u3"); ok {
		t.Fatal("uid not in the pool must be dropped")
	}

	// ---- 勾选统计：统计也随备份还原 ----
	if n, err := p.ImportStateBackupJSON(raw, true); err != nil || n != 1 {
		t.Fatalf("re-import = %d err = %v, want 1", n, err)
	}
	st, ok = p.Status("u1")
	if !ok || st.Credits != 777 || st.SuccessCount != 9 || st.ErrTotal != 99 || st.TokenUsage.TotalTokens != 42 {
		t.Fatalf("stats must be restored when includeStats=true: %+v ok=%v", st, ok)
	}
}

// ExportStateSnapshotJSON 与 state.json 落盘同形态：导入方 ImportStateBackupJSON
// 的输入即本函数输出，往返无损（credits/冷却/统计字段都在内——「导出 = 容器
// 全部数据」，分层在导入端做）。
func TestExportStateSnapshotJSONRoundTrip(t *testing.T) {
	p := New("")
	defer p.Close()
	p.Add(&auth.Auth{UID: "u1"})
	p.SetCredits("u1", 500, 0)
	p.NoteModelCost("u1", "paid", 0.1, 1000)
	p.Flush()

	raw := p.StateSnapshotJSON()
	if len(raw) == 0 {
		t.Fatal("empty snapshot")
	}
	var sf stateFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		t.Fatalf("snapshot must be state.json-shaped: %v", err)
	}
	s := sf.Accounts["u1"]
	if s.Credits != 500 || len(s.ModelCosts) != 1 {
		t.Fatalf("snapshot must carry facts: %+v", s)
	}

	// 往返：导入到新池，事实原样落地。
	p2 := New("")
	defer p2.Close()
	p2.Add(&auth.Auth{UID: "u1"})
	if n, err := p2.ImportStateBackupJSON(raw, true); err != nil || n != 1 {
		t.Fatalf("round-trip import = %d err = %v, want 1", n, err)
	}
	if st, ok := p2.Status("u1"); !ok || st.Credits != 500 || len(st.ModelCosts) == 0 {
		t.Fatalf("round-trip lost facts: %+v ok=%v", st, ok)
	}
}

// 冷却类事实的惰性清洗与 load() 同口径：已过期的模型级冷却/熔断/降权在导入
// 时不复活。
func TestImportStateBackupJSONDropsExpiredFacts(t *testing.T) {
	p := New("")
	defer p.Close()
	p.Add(&auth.Auth{UID: "u1"})
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	backup := map[string]stateAccount{
		"u1": {
			Credits: 10,
			ModelCooldowns: map[string]stateModelCooldown{
				"gone": {Until: past},   // 已过期 → 不复活
				"live": {Until: future}, // 未过期 → 恢复
			},
			BreakerUntil: &past,
			DegradeUntil: &future,
		},
	}
	raw, err := json.Marshal(stateFile{Accounts: backup})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := p.ImportStateBackupJSON(raw, false); err != nil || n != 1 {
		t.Fatalf("imported = %d err = %v, want 1", n, err)
	}
	// live 模型冷却生效：healthyForModel 对 gone 不拦、对 live 拦。
	if e := p.byUID["u1"]; e == nil {
		t.Fatal("u1 missing")
	} else {
		now := time.Now()
		if e.modelCooled(now, "gone") {
			t.Error("expired model cooldown must not be revived")
		}
		if !e.modelCooled(now, "live") {
			t.Error("live model cooldown must be imported")
		}
		if e.degradeUntil.IsZero() {
			t.Error("future degradeUntil must be imported")
		}
		if !e.breakerUntil.IsZero() {
			t.Error("expired breakerUntil must not be revived")
		}
	}
}

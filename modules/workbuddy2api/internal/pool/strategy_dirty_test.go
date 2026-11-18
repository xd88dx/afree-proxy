package pool

import (
	"testing"
	"time"

	"github.com/misakano7545/workbuddy2api-panel/internal/auth"
)

// strategyPool 造一个 N 号可用池（credits 充足、无冷却）。原属上游
// strategy_test.go——该同名文件在本仓被 ordered 策略测试顶替（POOL_STRATEGY
// 定制），上游新增测试的依赖 helper 就地随文件走。
func strategyPool(t *testing.T, uids ...string) *Pool {
	t.Helper()
	p := New("")
	for _, uid := range uids {
		p.Add(&auth.Auth{UID: uid, AccessToken: "at-" + uid, RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour).Unix()})
		p.SetCredits(uid, 100000, 100000)
	}
	return p
}

// 游标是运行态、不落盘：选号不能把池置脏，否则每次选号都让 flusher 重写一遍
// state.json（负载下每 5 秒一次 fsync，却没有任何新内容）。
//
// 变异判据：pickRoundRobinLocked / pickSequentialLocked 里加回 p.dirty.Store(true)，本用例红。
func TestStrategyPickDoesNotDirtyState(t *testing.T) {
	for _, strategy := range []string{StrategyRoundRobin, StrategySequentialExhaust} {
		p := strategyPool(t, "u1", "u2", "u3")
		p.SetStrategy(strategy)
		p.Flush() // 清掉准备阶段留下的 dirty
		for i := 0; i < 5; i++ {
			if p.Pick() == nil {
				t.Fatalf("%s 第 %d 次选号返回 nil", strategy, i+1)
			}
			time.Sleep(minPickGap + 2*time.Millisecond)
		}
		if p.dirty.Load() {
			t.Errorf("%s: 选号把池置脏了（游标不落盘，不该触发 state.json 重写）", strategy)
		}
	}
}

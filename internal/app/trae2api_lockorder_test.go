package app

// TraeWork 桥接层锁序回归（2026-11-04 实测死锁）：
// trae2apiAccountStates 曾持 traeRoutingMu 调 Pool().List()——List→Status→
// healthyLocked→RoutingAllowed 钩子→traeRoutingGet 重入 traeRoutingMu，同
// goroutine 自锁，把 /v1/models、/admin/api/trae/config 与后台 Pool.Save 全部
// 堵死（有账号后必现）。本测试构造含真账号的最小池，并发打三条路径，-race
// 下跑过即锁序成立；无超时保护下的挂死也说明回退。

import (
	"sync"
	"testing"
	"time"

	traepool "afree-proxy/internal/trae2api/pool"
	traeserver "afree-proxy/internal/trae2api/server"
	traectl "afree-proxy/internal/trae2api/traectl"
)

func newTrae2apiLockOrderFixture(t *testing.T) *trae2apiSubsystem {
	t.Helper()
	t.Setenv("DATA_DIR", testDataDir(t))

	cfg, err := trae2apiLoadConfig()
	if err != nil {
		t.Fatalf("load vendored config: %v", err)
	}
	srv, err := traeserver.New(cfg)
	if err != nil {
		t.Fatalf("build vendored server: %v", err)
	}
	prod, ok := traectl.ProductByKey("trae-cn")
	if !ok {
		t.Fatal("trae-cn product not registered")
	}
	// 直接往池里塞账号：读侧 Status→healthyLocked→RoutingAllowed 是死锁路径。
	srv.Pool().Add(&traepool.Account{
		UID:     prod.Key + "-1000000000000001",
		Realm:   "trae",
		Product: prod,
	})

	sub := &trae2apiSubsystem{cfg: cfg, srv: srv}
	old := trae2apiSub
	trae2apiSub = sub
	t.Cleanup(func() { trae2apiSub = old })
	return sub
}

// 三条路径并发：读侧（走 Status→healthyLocked→RoutingAllowed）与写侧
// （traeRoutingSet）。任何"持 routing/bindings 锁调 Pool"的回归都会在此挂死。
func TestTrae2apiBridgeLockOrder(t *testing.T) {
	sub := newTrae2apiLockOrderFixture(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					_ = trae2apiRoutableCount()
					_ = trae2apiAccountStates(sub)
				}
			}()
		}
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					traeRoutingSet("trae-cn-1000000000000001", j%2 == 0)
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("bridge lock-order deadlock: read/write paths blocked >30s")
	}
}

func TestTrae2apiValidateGateway(t *testing.T) {
	for _, gw := range []string{"https://api.trae.cn", "https://x.api.trae.com.cn", "http://oauth.example.com", "https://trae-api.com.cn/x"} {
		if err := trae2apiValidateGateway(gw); err == nil {
			t.Errorf("gateway %q must be rejected (OAuth-only host)", gw)
		}
	}
	for _, gw := range []string{"", "https://trae-api-cn.mchost.guru", "https://api5-normal.mchost.guru"} {
		if err := trae2apiValidateGateway(gw); err != nil {
			t.Errorf("gateway %q must be accepted: %v", gw, err)
		}
	}
}

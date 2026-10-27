package trae

import "testing"

// 9074 换设备：不同代际派生不同设备 ID，同代际跨调用稳定。
func TestDeviceID16Generations(t *testing.T) {
	gen0 := deviceID16Gen("u1", 0)
	if gen0 != deviceID16("u1") {
		t.Fatalf("gen0 must equal legacy deviceID16: %s vs %s", gen0, deviceID16("u1"))
	}
	if gen0 != deviceID16Gen("u1", 0) {
		t.Fatal("gen0 derivation must be stable")
	}
	gen1 := deviceID16Gen("u1", 1)
	gen2 := deviceID16Gen("u1", 2)
	if gen1 == gen0 || gen2 == gen0 || gen1 == gen2 {
		t.Fatalf("generations must derive distinct ids: %s / %s / %s", gen0, gen1, gen2)
	}
	if len(gen1) != 16 {
		t.Fatalf("device id must be 16 digits: %s", gen1)
	}
	// 不同账号同代际不得碰撞（抽样）
	if deviceID16Gen("u1", 1) == deviceID16Gen("u2", 1) {
		t.Fatal("different accounts must derive different ids")
	}
}

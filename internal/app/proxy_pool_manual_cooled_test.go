package app

import (
	"testing"
)

// setProxyPool 手动冷却保留语义（2026-11-18 修复）：池编辑不再一律清空手动
// 封禁——同一代理（URL 或备注名相同）仍在池中时封禁意图延续，真移除才丢弃。
// 此前订阅同步（顺序变化即触发整池替换）会把手动冷却连同落盘空表一起清掉。
func TestSetProxyPoolKeepsManualCooled(t *testing.T) {
	setupBindingTest(t, nil)

	proxyA := "socks5://10.0.0.1:1080"
	proxyB := "socks5://10.0.0.2:1080#node-b"
	setProxyPool(&proxyPoolConfigData{Proxies: []string{proxyA, proxyB}, Strategy: "round_robin"})
	markProxyManualCooled(1) // 手动封禁 node-b

	t.Run("顺序变化：手动封禁保留且重锚定到新索引", func(t *testing.T) {
		setProxyPool(&proxyPoolConfigData{Proxies: []string{proxyB, proxyA}, Strategy: "round_robin"})
		if proxyAvailable(0) { // node-b 现在是索引 0
			t.Fatal("node-b 换到索引 0 后手动封禁应延续")
		}
		if !proxyAvailable(1) {
			t.Fatal("proxy-a 不应被牵连")
		}
		kept := snapshotProxyManualCooled()
		if !kept[proxyB] || len(kept) != 1 {
			t.Fatalf("落盘快照应只含 node-b 新 URL, got %v", kept)
		}
	})

	t.Run("换端口同别名：经备注名回退重锚定", func(t *testing.T) {
		setProxyPool(&proxyPoolConfigData{Proxies: []string{proxyA, "socks5://10.0.0.2:9999#node-b"}, Strategy: "round_robin"})
		if proxyAvailable(1) {
			t.Fatal("node-b 换端口后手动封禁应延续（别名相同即同一代理）")
		}
	})

	t.Run("代理真被移除：标记丢弃", func(t *testing.T) {
		setProxyPool(&proxyPoolConfigData{Proxies: []string{proxyA}, Strategy: "round_robin"})
		kept := snapshotProxyManualCooled()
		if len(kept) != 0 {
			t.Fatalf("node-b 已移出池，手动封禁应丢弃, got %v", kept)
		}
		if !proxyAvailable(0) {
			t.Fatal("proxy-a 从未被手动冷却，必须可选")
		}
	})
}

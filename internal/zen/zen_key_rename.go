package zen

// RenameZenKeyArtifacts key 明文变更（面板账号列点击编辑，2026-11-19）时迁
// 移按 key 挂靠的旁挂数据：路由表/昵称表（zen-config.json）、出口钉定
// （egress 键=sha256(key) 前 8 字节）、org 缓存与负缓存（zen-state keyOrg
// 段，内存键=明文 key）。冷却与 token 记账刻意不迁移——新 key 是上游新身
// 份，冷却应清零；旧 key 的用量统计留在旧名下（面板不再列出，等同归档）。
// 锁序与 zen 子系统一致：zenFileMu 独立取；EgressMu 在外 zenFileMu 在内；
// zenOrgMu 在外 SaveZenUsage 在内。
func RenameZenKeyArtifacts(from, to string) {
	if from == "" || to == "" || from == to {
		return
	}
	// 1) config 侧：路由参与表 + 昵称表
	MutateZenConfigFile(func(f *ZenConfigFileData) {
		if f.KeyRoutingEnabled != nil {
			if en, ok := f.KeyRoutingEnabled[from]; ok {
				f.KeyRoutingEnabled[to] = en
				delete(f.KeyRoutingEnabled, from)
			}
		}
		if f.KeyNicknames != nil {
			if nick, ok := f.KeyNicknames[from]; ok {
				f.KeyNicknames[to] = nick
				delete(f.KeyNicknames, from)
			}
		}
	})
	// 2) 出口钉定
	EgressMu.Lock()
	oldID, newID := EgressID(egressNSZen, from), EgressID(egressNSZen, to)
	if b, ok := EgressBindings[oldID]; ok {
		EgressBindings[newID] = b
		delete(EgressBindings, oldID)
		SaveEgressLocked()
	}
	EgressMu.Unlock()
	// 3) org 缓存与负缓存
	zenOrgMu.Lock()
	LoadZenOrgOnce()
	if v, ok := zenOrgData.Orgs[from]; ok {
		zenOrgData.Orgs[to] = v
		delete(zenOrgData.Orgs, from)
	}
	if f, ok := zenOrgData.Failed[from]; ok {
		zenOrgData.Failed[to] = f
		delete(zenOrgData.Failed, from)
	}
	SaveZenOrgLocked()
	zenOrgMu.Unlock()
}

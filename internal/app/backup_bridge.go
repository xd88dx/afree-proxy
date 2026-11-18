package app

// backup_bridge.go —— backup 包（P4）的 app 侧小写消费面别名：备份域导出/
// 导入与版本判据。handler（backup_handlers.go）经这些别名消费。

import (
	"afree-proxy/internal/backup"
	"afree-proxy/internal/cline"
	"afree-proxy/internal/gwconfig"
	"afree-proxy/internal/zen"
)

const configBackupVersion = backup.ConfigBackupVersion

func configBackupVersionOK(v int) bool { return backup.ConfigBackupVersionOK(v) }

func zenBackupSnapshot() *zen.ZenConfigData { return backup.ZenBackupSnapshot() }

func exportWorkbuddyBackup() *backup.WorkbuddyBackup { return backup.ExportWorkbuddyBackup() }

func importWorkbuddyBackup(wb *backup.WorkbuddyBackup, keepWbEnabled, includeStats bool) (int, bool) {
	return backup.ImportWorkbuddyBackup(wb, keepWbEnabled, includeStats)
}

func exportWorkbuddyProxyBindings() map[string]workbuddyProxyBinding {
	return backup.ExportWorkbuddyProxyBindings()
}

func importWorkbuddyProxyBindings(in map[string]workbuddyProxyBinding) int {
	return backup.ImportWorkbuddyProxyBindings(in)
}

func exportModelBlacklist() map[string]*gwconfig.ModelBlacklistEntry {
	return backup.ExportModelBlacklist()
}

func importModelBlacklist(in map[string]*gwconfig.ModelBlacklistEntry) {
	backup.ImportModelBlacklist(in)
}

func exportZenDepAliases() map[string]*zen.ZenDepEntry { return backup.ExportZenDepAliases() }

func importZenDepAliases(in map[string]*zen.ZenDepEntry) { backup.ImportZenDepAliases(in) }

func exportClineStreamRequired() map[string]string { return cline.StreamSnapshot() }

func importClineStreamRequired(in map[string]string) { cline.ReplaceStream(in) }

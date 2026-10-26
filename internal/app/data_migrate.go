package app

// ============================================================================
// 旧文件名一次性迁移
//
// 历史数据文件名五花八门:点前缀(.amd-config.json)、平台缩写不一致
// (.or-config.json / .th-config.json)。统一为 <平台>-config.json 后,
// 首次启动把旧文件整份复制到新名字,旧文件保留不删(便于回滚)。
// ============================================================================

import (
	"log"
	"os"

	"afree-proxy/internal/kit"
)

// migrateLegacyDataFile 若新文件不存在而旧文件存在,把旧文件内容复制到新路径。
// 返回是否发生了复制。旧文件不动——迁移只做"复制"不做"搬移",回滚只需改回代码。
func migrateLegacyDataFile(newName, oldName string) bool {
	newPath := kit.ResolveDataPath(newName)
	if _, err := os.Stat(newPath); err == nil {
		return false
	}
	raw, err := os.ReadFile(kit.ResolveDataPath(oldName))
	if err != nil {
		return false
	}
	atomicWriteFile(newPath, raw)
	log.Printf("data: migrated %s -> %s", oldName, newName)
	return true
}

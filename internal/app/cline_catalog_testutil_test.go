package app

import (
	"testing"
	"time"

	"afree-proxy/internal/cline"
)

// injectClineModels 目录条目测试注入（原随 models.go 白盒，目录出包后经
// cline 测试原语实现）：锁内注入 feed 原样形态 ID，结束恢复整表。
func injectClineModels(t *testing.T, ids ...string) {
	t.Helper()
	cline.InitModelsCache()
	saved := make(map[string]*ModelInfo, len(cline.CatalogSnapshotForTest()))
	for k, v := range cline.CatalogSnapshotForTest() {
		saved[k] = v
	}
	now := time.Now()
	snap := cline.CatalogSnapshotForTest()
	for _, id := range ids {
		snap[id] = &ModelInfo{ID: id, Source: "free", Cost: "free", Status: ModelActive, SyncedAt: now}
	}
	t.Cleanup(func() { cline.SetCatalogForTest(saved) })
}

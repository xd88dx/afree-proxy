package cline

import (
	"strings"
	"testing"

	"time"
)

// throttleGateRefreshForTest 预占 strict-gate 的目录同步节流窗（30s）。

// 未知模型命中 strict gate 时会节流地发起 live 目录同步（SyncModelsOnce +

// syncZenModels：出网拉官方 feed / zen registry，并写多份 DATA_DIR 文件）——

// 单测不应发外网请求，预置节流时间戳让同步分支直接跳过；未知模型的 404

// 结论不依赖同步结果。2026-11-03 CI 实测：TestServedModelGate 的 TempDir

// 清理与同步写盘竞态（unlinkat … directory not empty）偶发打红 CI。

func throttleGateRefreshForTest() {

	gateRefreshLast.Store(time.Now().Unix())

}

// injectClineModels 测试注入：modelsCache 是全局的且不再有 seed 兜底

// （2026-10-31 用户定：目录唯一来源是官方 feed），需要条目的测试锁内注入、

// 结束恢复空缓存。注入 ID 用 feed 原样形态（cline-free/* 自带前缀、其余不带）。

func injectClineModels(t *testing.T, ids ...string) {

	t.Helper()

	InitModelsCache()

	ModelsMu.Lock()

	saved := make(map[string]*ModelInfo, len(modelsCache))

	for k, v := range modelsCache {

		saved[k] = v

	}

	now := time.Now()

	for _, id := range ids {

		modelsCache[id] = &ModelInfo{ID: id, Source: "free", Cost: "free", Status: ModelActive, SyncedAt: now}

	}

	ModelsMu.Unlock()

	t.Cleanup(func() {

		ModelsMu.Lock()

		modelsCache = saved

		ModelsMu.Unlock()

	})

}

// Cline 模型 ID 规范化（2026-11-28 前缀改名）：feed 自带 cline-free/ 前缀的
// 剥掉后统一补 cline:，其余直接补；已带 cline: 的幂等，杜绝双前缀拼接。

func TestDisplayClineModelID(t *testing.T) {

	cases := []struct{ in, want string }{

		{"cline-free/deepseek-v4.1-flash", "cline:deepseek-v4.1-flash"},

		{"poolside/laguna-s-2.1:free", "cline:poolside/laguna-s-2.1:free"},

		{"z-ai/glm-5.3-flash", "cline:z-ai/glm-5.3-flash"},

		{"cline:z-ai/glm-5.3-flash", "cline:z-ai/glm-5.3-flash"},

		{"cline:cline-free/deepseek-v4.1-flash", "cline:deepseek-v4.1-flash"},

		{"", ""},
	}

	for _, c := range cases {

		if got := displayClineModelID(c.in); got != c.want {

			t.Errorf("displayClineModelID(%q) = %q, want %q", c.in, got, c.want)

		}

	}

}

// 各形态都应还原到 feed 真实 ID（cline: 新前缀 / cline-free/ feed 键 / 裸名）。

func TestCanonicalClineModelID(t *testing.T) {

	for _, in := range []string{"poolside/laguna-s-2.1:free", "cline-free/poolside/laguna-s-2.1:free", "cline:poolside/laguna-s-2.1:free", "cline:cline-free/poolside/laguna-s-2.1:free"} {

		if got := canonicalClineModelID(in); got != "poolside/laguna-s-2.1:free" {

			t.Errorf("canonicalClineModelID(%q) = %q", in, got)

		}

	}

}

// ModelInClineList 接受两种形态。

func TestModelInClineListAcceptsBothForms(t *testing.T) {

	// 注入条目（测试进程无 live 同步）：z-ai/*、poolside/* 不带前缀，

	// cline-free/* 自带。两种都必须命中。

	injectClineModels(t, "z-ai/glm-5.3-flash", "poolside/laguna-s-2.1:free", "cline-free/deepseek-v4.1-flash")

	if !ModelInClineList("z-ai/glm-5.3-flash") {

		t.Error("injected id without prefix must be in list")

	}

	if !ModelInClineList("cline-free/z-ai/glm-5.3-flash") {

		t.Error("prefixed id must resolve back to the feed id")

	}

	if !ModelInClineList("cline:z-ai/glm-5.3-flash") {

		t.Error("cline:-prefixed external id must resolve to the feed id")

	}

	if !ModelInClineList("cline:cline-free/z-ai/glm-5.3-flash") {

		t.Error("double-prefixed id must normalize to the feed id")

	}

	if !ModelInClineList("poolside/laguna-s-2.1:free") {

		t.Error("injected id without prefix must be in list")

	}

	// cline-free/* 原生条目的缓存键自带前缀：展示形态与剥前缀裸名都必须

	// 命中（只查剥前缀形态会漏掉整类原生条目，STRICT 门控误拒）。

	if !ModelInClineList("cline-free/deepseek-v4.1-flash") {

		t.Error("native cline-free/* id must be in list")

	}

	if !ModelInClineList("deepseek-v4.1-flash") {

		t.Error("bare form of a native cline-free/* entry must be in list")

	}

	if ModelInClineList("no/such-model") {

		t.Error("unknown model must not be in list")

	}

}

// NormalizeRequestModel 带前缀请求要解析到 feed 真实 ID（出站/记账用真实 ID）：

// cline-free/* 原生条目的真实 ID 自带前缀，z-ai/* poolside/* 等不带。

func TestNormalizeRequestModelResolvesFeedID(t *testing.T) {

	injectClineModels(t, "z-ai/glm-5.3-flash", "cline-free/deepseek-v4.1-flash")

	if got := NormalizeRequestModel("cline-free/z-ai/glm-5.3-flash"); got != "z-ai/glm-5.3-flash" {

		t.Errorf("NormalizeRequestModel = %q, want the feed id", got)

	}

	if got := NormalizeRequestModel("cline:z-ai/glm-5.3-flash"); got != "z-ai/glm-5.3-flash" {

		t.Errorf("NormalizeRequestModel(cline:) = %q, want the feed id", got)

	}

	if got := NormalizeRequestModel("z-ai/glm-5.3-flash"); got != "z-ai/glm-5.3-flash" {

		t.Errorf("NormalizeRequestModel(bare) = %q", got)

	}

	if got := NormalizeRequestModel("cline-free/deepseek-v4.1-flash"); got != "cline-free/deepseek-v4.1-flash" {

		t.Errorf("NormalizeRequestModel(native) = %q, want the prefixed feed id", got)

	}

	if got := NormalizeRequestModel("deepseek-v4.1-flash"); got != "cline-free/deepseek-v4.1-flash" {

		t.Errorf("NormalizeRequestModel(native bare) = %q, want the prefixed feed id", got)

	}

}

// /v1/models 输出的 Cline 条目必须都带 cline: 前缀（2026-11-28 由
// cline-free/ 改名）。

func TestAPIModelListClineIDsArePrefixed(t *testing.T) {

	savedPool := RawPool()

	on := true

	PoolMu.Lock()

	SetPool(&AccountPool{Accounts: []*Account{{AccountID: "a", PoolEnabled: &on}}})

	PoolMu.Unlock()

	defer func() {

		PoolMu.Lock()

		SetPool(savedPool)

		PoolMu.Unlock()

	}()

	InitModelsCache()

	injectClineModels(t, "z-ai/glm-5.3-flash", "cline-free/deepseek-v4.1-flash")

	list := APIModelList()

	if len(list) == 0 {

		t.Fatal("routable account must yield a non-empty cline catalog")

	}

	for _, m := range list {

		id, _ := m["id"].(string)

		if len(id) < 6 || id[:6] != "cline:" {

			t.Errorf("cline model id %q must carry the cline: prefix", id)

		}

		if strings.Contains(id, "cline:cline-free/") {

			t.Errorf("cline model id %q must not double the prefix", id)

		}

	}

}

// WorkBuddy 前缀翻译：网关 wbcn:/wbgb: ↔ vendor cn:/global:，双向正确。

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"afree-proxy/internal/workbuddy/auth"
	"afree-proxy/internal/workbuddy/livecfg"
	"afree-proxy/internal/workbuddy/upstream"
)

// resetModelsCache 清空包级动态目录缓存，保证每个用例打到本测的假上游。
func resetModelsCache() {
	dynamicModelsCache.Lock()
	dynamicModelsCache.ids = nil
	dynamicModelsCache.fetched = time.Time{}
	dynamicModelsCache.lastFail = time.Time{}
	dynamicModelsCache.Unlock()
}

// catalogWithRates 假上游返回带倍率的模型目录：credits 是牌价，部分条目另有
// modelPromotions（promo 生效价）。目录刷新时 FetchModels 会把这些倍率写进
// client 的生效倍率表（ModelRate），供 modelList 筛选读取。
const catalogWithRates = `{"code":0,"data":{"models":[` +
	`{"id":"free-model","maxInputTokens":65536,"maxOutputTokens":8192,"credits":"x0.00"},` +
	`{"id":"cheap-model","maxInputTokens":65536,"maxOutputTokens":8192,"credits":"x0.10"},` +
	`{"id":"mid-model","maxInputTokens":131072,"maxOutputTokens":16384,"credits":"x0.30"},` +
	`{"id":"promo-model","maxInputTokens":131072,"maxOutputTokens":16384,"credits":"x0.80"},` +
	`{"id":"norate-model","maxInputTokens":131072,"maxOutputTokens":16384}` +
	`],"agents":[{"name":"cli","models":["free-model","cheap-model","mid-model","promo-model","norate-model"]}],` +
	`"modelPromotions":[{"enabled":true,"priority":100,"modelIds":["promo-model"],"discount":{"factor":0.25,"discountedCredits":"0.20x"}}]}}`

func fakeCatalogUpstream(t *testing.T, body string) *upstream.Client {
	return newFakeUpstream(t, func(string) (int, string, bool) {
		return 200, body, false
	})
}

func modelIDs(t *testing.T, h *Handler) []string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		out = append(out, m.ID)
	}
	return out
}

func idsSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// TestModelsRateFilterGateway 倍率筛选（config pool.model_rate_filter_cn/_global）
// 作用于 /v1/models：只透出生效倍率 ≤ 阈值的条目，改阈值即时生效（livecfg 热改）。
// 生效价口径 = promo 折扣价优先、否则牌价；缺倍率的模型视为未知价一律隐藏。
func TestModelsRateFilterGateway(t *testing.T) {
	resetModelsCache()
	up := fakeCatalogUpstream(t, catalogWithRates)
	live := livecfg.New(livecfg.Snapshot{ModelRateFilterCN: 0.3, ModelRateFilterGlobal: 0.3})
	h := NewHandler(Config{
		Pool:     testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}),
		Upstream: up,
		Live:     live,
	})

	got := idsSet(modelIDs(t, h))
	want := []string{"cn:free-model", "cn:cheap-model", "cn:mid-model", "cn:promo-model"}
	for _, id := range want {
		if !got[id] {
			t.Errorf("threshold 0.3: %s missing (want in list)", id)
		}
	}
	// 缺倍率 = 未知价，无法证明 ≤ 阈值 → 隐藏（缺失 ≠ 免费）。
	if got["cn:norate-model"] {
		t.Errorf("threshold 0.3: no-rate model (unknown price) must be hidden")
	}

	// 阈值降到 0.1：只剩免费 + 0.10。
	live.Store(livecfg.Snapshot{ModelRateFilterCN: 0.1, ModelRateFilterGlobal: 0.1})
	got = idsSet(modelIDs(t, h))
	for _, id := range []string{"cn:free-model", "cn:cheap-model"} {
		if !got[id] {
			t.Errorf("threshold 0.1: %s missing", id)
		}
	}
	for _, id := range []string{"cn:mid-model", "cn:promo-model", "cn:norate-model"} {
		if got[id] {
			t.Errorf("threshold 0.1: %s must be filtered out", id)
		}
	}

	// 阈值 0：只剩免费（0 = 最严档，不是未配置）。
	live.Store(livecfg.Snapshot{ModelRateFilterCN: 0, ModelRateFilterGlobal: 0})
	got = idsSet(modelIDs(t, h))
	if !got["cn:free-model"] {
		t.Errorf("threshold 0: expected only free, got %v", got)
	}
	if got["cn:cheap-model"] || got["cn:norate-model"] {
		t.Errorf("threshold 0: cheap/no-rate models must be filtered out")
	}
}

// globalCatalogWithRates global 域目录：paid-global-model 牌价 x0.30——在 CN 阈值
// 0.3 下能过、在 global 阈值 0.1 下必须被滤，用于证明两域阈值互不越界。
const globalCatalogWithRates = `{"code":0,"data":{"models":[` +
	`{"id":"free-global-model","maxInputTokens":65536,"maxOutputTokens":8192,"credits":"x0.00"},` +
	`{"id":"paid-global-model","maxInputTokens":131072,"maxOutputTokens":16384,"credits":"x0.30"}` +
	`],"agents":[{"name":"cli","models":["free-global-model","paid-global-model"]}]}}`

// realmSplitUpstream 按域名分流两域目录的假上游：global.example 回 global 目录，
// 其余（CN 基址）回 CN 目录。与 fetch_test 的 roundTripFunc 同款手工 Client。
func realmSplitUpstream() *upstream.Client {
	return &upstream.Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body := catalogWithRates
			if r.URL.Host == "global.example" {
				body = globalCatalogWithRates
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		})},
		ChatBaseCN:     "https://cn.example",
		BillingBaseCN:  "https://cn.example",
		ChatBaseGlobal: "https://global.example",
		GlobalEnabled:  true,
	}
}

// TestModelsRateFilterPerRealm 分域阈值（2026-11-07 拆分）跨域互不越界：
// CN=0.3 / Global=0.1 时，同一价位的模型在 CN 域放行、在 global 域被滤——
// 单值时代的共享阈值做不到这一点，这正是拆分的动机。
func TestModelsRateFilterPerRealm(t *testing.T) {
	resetModelsCache()
	auth.SetGlobalEnabled(true)
	t.Cleanup(func() { auth.SetGlobalEnabled(true) })
	h := NewHandler(Config{
		Pool: testPoolWith(
			&auth.Auth{UID: "c1", AccessToken: "at", ExpiresAt: 9999999999, Domain: "www.codebuddy.cn"},
			&auth.Auth{UID: "g1", AccessToken: "at", ExpiresAt: 9999999999, Domain: "www.workbuddy.ai"},
		),
		Upstream:      realmSplitUpstream(),
		Live:          livecfg.New(livecfg.Snapshot{ModelRateFilterCN: 0.3, ModelRateFilterGlobal: 0.1}),
		GlobalEnabled: true,
	})

	got := idsSet(modelIDs(t, h))
	// CN 域按 0.3 过滤：promo 折扣价 0.20 的条目放行（与单值时代一致）。
	for _, id := range []string{"cn:free-model", "cn:cheap-model", "cn:mid-model", "cn:promo-model"} {
		if !got[id] {
			t.Errorf("CN threshold 0.3: %s missing", id)
		}
	}
	// global 域按 0.1 过滤：x0.30 的 paid-global-model 必须被滤——它在 CN 阈值
	// 下是可放行的价位，证明 global 没有蹭 CN 阈值。
	if !got["global:free-global-model"] {
		t.Errorf("global threshold 0.1: free-global-model missing, got %v", got)
	}
	if got["global:paid-global-model"] {
		t.Errorf("global threshold 0.1: paid-global-model (x0.30) must be filtered by the global threshold, got %v", got)
	}
}

// TestModelsRateFilterDefault 未配置时（Live 快照零值 + 静态字段零值）回落
// 默认 0.2：零值绝不意味着「只列免费」——老配置/裸用路径行为不变。
func TestModelsRateFilterDefault(t *testing.T) {
	resetModelsCache()
	up := fakeCatalogUpstream(t, catalogWithRates)
	h := NewHandler(Config{
		Pool:     testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}),
		Upstream: up,
	})
	got := idsSet(modelIDs(t, h))
	if !got["cn:cheap-model"] || got["cn:mid-model"] {
		t.Errorf("default 0.2: cheap in / mid out expected, got %v", got)
	}
}

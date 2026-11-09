package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ============ cline "必须流式" 模型自学习 ============
//
// 背景：cline 的免费模型列表接口（/ai/cline/recommended-models）每个条目只有
// id/name/description/tags，没有任何"该模型是否必须流式调用"的信号。同步层只能
// 按 id 命名约定推断：含 ":" 的 id（如 poolside/laguna-s-2.1:free）是 provider
// 自带的免费变体，普通非流式调用即可；不含 ":" 的（cline-free/*、z-ai/*）强制
// 上游流式（见 models.go syncRecommendedModels / proxy.go modelNeedsStream）。
//
// 约定猜错时上游会明确拒绝：HTTP 500 + {"error":"empty response content"}。
// 机制：非流式的 cline 请求收到该错误 → 说明该模型其实必须流式 → 记下它
//（DATA_DIR/.cline-stream-required.json，跨重启保留）→ 本次请求立即改用
// stream=true 重试（客户端全程无感：网关把上游 SSE 聚合回普通 JSON 返回）→
// 后续请求由 modelNeedsStream 直接命中，不再有重试开销。
//
// 单向学习：命名约定唯一会出错的方向是"漏判"（该流式却没强制），反向
//（多判）只是让网关多做一次流式聚合，对客户端透明、无副作用，不值得学。

var (
	clineStreamMu      sync.RWMutex
	clineStreamLearned map[string]string // model id -> 学习时间(RFC3339)

	// clineStreamSaveMu 串行化写盘。
	//
	// 学习路径为了不在 RWMutex 下做慢 I/O，是"锁内改内存、锁外写盘"。两个
	// 并发请求同时学到不同模型时，两次 saveClineStream 会同时往**同一个**
	// .tmp 路径写：一把 O_TRUNC 覆盖另一把正在写的内容，随后先完成的 rename
	// 发布出去，后一个 rename 报 ENOENT（只记日志）。结果是丢更新（新学的
	// 模型被旧快照覆盖，下次仍要付一次失败重试）甚至发出半截 JSON。
	// 与 zen_endpoint.go 的 zenEndpointSaveMu 同一手法。
	clineStreamSaveMu sync.Mutex
)

// clineEmptyStreamErr 响应是否呈"该模型必须流式"的指纹：HTTP 500 且 body 含
// "empty response content"。只认这一条精确特征——它是 cline 对非流式调用这类
// 模型的固定答复；其他 5xx（过载、瞬时故障）绝不触发学习。
func clineEmptyStreamErr(status int, body []byte) bool {
	if status != http.StatusInternalServerError {
		return false
	}
	return bytes.Contains(bytes.ToLower(body), []byte("empty response content"))
}

// clineAPIError cline 上游的非 200 答复。callClineAPI 对非 200 会读尽并关闭响应体
// 后返回 error，调用方拿不到 *http.Response；但"该模型必须流式"的判定只需要状态码
// + 错误体，用类型化错误把它们带出来即可，无需改动"非 200 返回 nil 响应"的既有约定。
type clineAPIError struct {
	Status int
	Body   string
}

func (e *clineAPIError) Error() string {
	return fmt.Sprintf("API %d: %s", e.Status, e.Body)
}

// HTTPStatus 让上游错误的状态码可被 upstreamErrorStatus 统一提取。
func (e *clineAPIError) HTTPStatus() int { return e.Status }

// clineStreamRequired 该模型是否已被学习为"必须流式"。
func clineStreamRequired(modelID string) bool {
	clineStreamMu.RLock()
	defer clineStreamMu.RUnlock()
	_, ok := clineStreamLearned[modelID]
	return ok
}

// learnClineStreamRequired 记录并持久化"该模型必须流式"。重复学习无副作用。
func learnClineStreamRequired(modelID string) {
	if modelID == "" {
		return
	}
	clineStreamMu.Lock()
	if clineStreamLearned == nil {
		clineStreamLearned = map[string]string{}
	}
	if _, ok := clineStreamLearned[modelID]; ok {
		clineStreamMu.Unlock()
		return
	}
	clineStreamLearned[modelID] = time.Now().Format(time.RFC3339)
	// 持锁只做内存改动：写盘是慢 I/O，放在内存锁外做，避免阻塞并发请求的
	// modelNeedsStream 读路径。
	clineStreamMu.Unlock()

	log.Printf("cline stream learned: model=%s requires stream=true (persisted)", modelID)
	saveClineStreamLatest()
}

// loadClineStreamLearned 启动时从全局配置(afree-config.json)恢复学习结果。
func loadClineStreamLearned() {
	globalCfgMu.Lock()
	g := loadGlobalConfigLocked()
	learned := make(map[string]string, len(g.ClineStreamRequired))
	for k, v := range g.ClineStreamRequired {
		learned[k] = v
	}
	globalCfgMu.Unlock()
	if len(learned) == 0 {
		return
	}
	clineStreamMu.Lock()
	clineStreamLearned = learned
	n := len(learned)
	clineStreamMu.Unlock()
	if n > 0 {
		log.Printf("cline stream learn loaded: %d model(s) require streaming", n)
	}
}

// saveClineStreamLatest 落盘当前内存学习表的全量快照。
//
// 快照必须在**写盘串行锁内**取，不在内存锁内取：只给写盘加互斥还不够——
// 如果每个并发学习先各自取快照、再排队写盘，排在后面的旧快照会把前面新写的
// 覆盖掉（经典 lost update，-race 实测 16 并发最终只落盘 15 个）。真正的语义
// 是"落盘当前最新的全量状态"：每个拿到写盘锁的调用方重新快照内存表，最后
// 落盘的一定是最全的一份。
// 代价：慢 I/O 期间持有 clineStreamSaveMu，但这把锁不参与读路径
// （modelNeedsStream 只碰 clineStreamMu），不会拖慢请求。
func saveClineStreamLatest() {
	clineStreamSaveMu.Lock()
	defer clineStreamSaveMu.Unlock()

	clineStreamMu.RLock()
	snapshot := make(map[string]string, len(clineStreamLearned))
	for k, v := range clineStreamLearned {
		snapshot[k] = v
	}
	clineStreamMu.RUnlock()
	saveClineStream(snapshot)
}

// saveClineStream 写入全局配置(入参是不持锁的快照)。写失败只记日志不阻断请求——
// 学习结果是优化项，丢失只会让下一次非流式请求再付一次重试代价。
// 学习路径必须走 saveClineStreamLatest（快照在写盘锁内取）；本函数供备份导入
// 等已持有明确全量快照的调用方使用。
func saveClineStream(learned map[string]string) {
	cp := make(map[string]string, len(learned))
	for k, v := range learned {
		cp[k] = v
	}
	mutateGlobalConfig(func(g *globalConfigData) { g.ClineStreamRequired = cp })
}

// clineCallFn 上游调用入口：包级变量便于测试注入假上游，生产恒为 callClineAPI。
var clineCallFn = callClineAPI

// callClineAutoStream 包住 callClineAPI 兜住"命名约定猜错"的情况：非流式请求若
// 被上游以 500 empty response content 拒绝，就学习该模型并立即改用流式重试。
// 返回值 streamed 告知调用方"这份响应体是 SSE，必须走聚合路径"——即便客户端
// 原本要的是非流式 JSON。
//
// 只有非流式请求才探测：客户端本来就要流式时无需任何判断。
func callClineAutoStream(ctx context.Context, params map[string]any, stream, useProxies bool) (*http.Response, *Account, bool, error) {
	resp, acc, err := clineCallFn(ctx, params, stream, useProxies)
	// 只看非流式请求的失败：流式请求不会被这条约定拒绝，成功响应（resp != nil）照旧直通。
	if err == nil || stream || resp != nil {
		return resp, acc, false, err
	}
	// callClineAPI 对非 200 返回的是 nil 响应 + clineAPIError；指纹只在其中。
	var apiErr *clineAPIError
	if !errors.As(err, &apiErr) || !clineEmptyStreamErr(apiErr.Status, []byte(apiErr.Body)) {
		return resp, acc, false, err
	}

	model, _ := params["model"].(string)
	model = normalizeRequestModel(model)
	log.Printf("cline model %s rejected non-stream call (500 empty response): learning + retrying streamed", model)
	learnClineStreamRequired(model)

	resp2, acc2, err2 := clineCallFn(ctx, params, true, useProxies)
	if err2 != nil {
		// 重试也失败：把第一次的错误照旧上报（模型已学会，下一个请求直接走流式）
		return nil, acc, false, err
	}
	if acc2 != nil {
		acc = acc2
	}
	return resp2, acc, true, nil
}

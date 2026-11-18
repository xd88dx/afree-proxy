package zen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/kit"
)

// ============ zen 每 key 组织 ID（x-opencode-org-id）============
//
// 2026-11-11 实测（活动 key oc_sk_9d87…，直接打 inference 端点）：
//
//	无该头                 -> 429 FreeUsageLimitError（正常限流，说明 key 有效）
//	x-opencode-org-id: <别的账号的 wrk_…>  -> 403 permission_error
//	                                          "Workspace access denied"
//	x-opencode-org-id: 非 wrk_/org_ 前缀的串 -> 400
//
// 结论（三条，都直接决定实现）：
//  1. 该头**会被上游强制校验**，且必须与 key 所属的 workspace 一致。发错 org
//     不是"轻微不像客户端"，而是把一次本来能过的请求变成硬 403。
//  2. 因此**发一个猜的/借的 org 严格劣于不发**。任何时候拿不到某把 key 自己的
//     org，唯一正确做法就是省略该头——这也是官方客户端在 service-account key
//     上的行为（credential 为 {type:"key", key}、metadata 为空，见下）。
//  3. 全局单一 org 值在多 key 池里必然出错：只要有两把分属不同 workspace 的 key，
//     其中一把必定被 403。故 cfg.OrgID 绝不能当"默认值"套给所有 key。
//
// 官方客户端怎么拿到该值（opencode-cli.exe 静态分析）：
//   - OAuth 连接：登录时 /auth/device/token 返回 org_id，写入 credential.metadata.orgID；
//     未返回 org 时遍历 /api/orgs 取第一个。
//   - 「API key (service account)」连接：credential 只有 {type:"key", key}，metadata 为空。
//     注入点 `a.metadata?.orgID` 取不到值，**不发该头**。
//   - 本机 opencode.db 的 credential 行正是第二种形态（无 metadata），即 CLI 常态下
//     根本不会发这个头；先前抓包里的 org 来自带 metadata 的 OAuth/其它凭据形态。
//
// 发现端点是否可用（2026-11-11 用活动 key 逐条验证）：
//
//	GET /console/api/orgs       -> 401 {"_tag":"Unauthorized"}
//	GET /console/api/user       -> 401
//	GET /console/api/v2/config  -> 200（说明这把 key 本身对 console 是有效的）
//	GET /api/orgs, /api/user    -> 404
//	/console/api/v2/* 其它路径  -> 404
//
// 即：console 的 orgs 端点**不接受 service-account API key**（它要 OAuth 会话）。
// 对 oc_sk_ 这类 key 没有可用的 org 发现途径，网关也就不该去猜。
//
// 本文件因此承担两件事：
//  1. 按 key 解析（有就把该 key 自己的 org 发出去，绝不跨 key 借用）；
//  2. 解析不到就省略该头（默认路径），并对失败做负缓存，避免每次请求都白探一次。
//
// 自我保护（2026-11-11 追加）：曾把 cfg.OrgID 当全局默认值套给所有 key。在
// "该头会被校验"的事实下这是致命的——一个配错的值会让整池请求 403。现改为
// **默认完全不用该全局值**（见 ZenKeyOrgForRequest 的 allowFallback 参数）。

// zenOrgFile 已并入 zen-state.json 的 keyOrg 段（2026-11-17 命名回归铁律，
// 原 .zen-key-org.json 独立点文件删除——无兼容迁移，org 缓存冷启动重建）。
// ZenOrgFileVersion/ZenOrgStore 结构沿用，持久化通道走 zen_usage.go 的
// zen-state.json（SaveZenUsage 整段快照 / LoadZenUsage 回填）。

// ZenOrgFileVersion 文件格式版本，便于将来迁移。
const ZenOrgFileVersion = 1

type ZenOrgStore struct {
	Version int               `json:"version"`
	Orgs    map[string]string `json:"orgs"`   // keyID -> orgID
	Failed  map[string]int64  `json:"failed"` // keyID -> 上次失败时刻（unix 秒），负缓存
}

// ZenKeyID 计算 key 在持久化文件里的标识：SHA-256 前 16 字节的 hex。
//
// 刻意不落原文 key。虽然 .zen-config.json 本来就存着 key，但没有理由再制造
// 第二个明文 key 文件——排障时把 data 目录打包发出去是常见操作。哈希足够
// 做"这把 key 的 org 是多少"的映射（进程内用原文 key 作内存键，只在写盘
// 前换成哈希）。
func ZenKeyID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16])
}

var (
	zenOrgMu     sync.Mutex
	zenOrgLoaded bool
	zenOrgData   = ZenOrgStore{Version: ZenOrgFileVersion, Orgs: map[string]string{}, Failed: map[string]int64{}}
	// zenOrgFlight 防止同一 key 并发重复发现（每个 key 至多一次进行中）。
	zenOrgFlight = map[string]*sync.WaitGroup{}
)

// ZenOrgRetryAfter 发现失败后的负缓存时长。
//
// 失败原因多是 key 过期/无权限这类稳定状态，每次请求都去探测只会白烧一次
// 上游往返（还可能被当作异常流量）。6 小时足够长到不影响请求延迟，又短到
// key 权限变更后能自愈。
const ZenOrgRetryAfter = 6 * time.Hour

// ZenOrgHTTPTimeout 发现请求超时。这是请求路径上的同步操作（仅首次），
// 超时必须远小于上游推理时长，否则首次请求会被它拖慢。
const ZenOrgHTTPTimeout = 6 * time.Second

// LoadZenOrgOnce 惰性加载持久化文件。调用方持 zenOrgMu。
func LoadZenOrgOnce() {
	if zenOrgLoaded {
		return
	}
	zenOrgLoaded = true
	if zenOrgData.Orgs == nil {
		zenOrgData.Orgs = map[string]string{}
	}
	if zenOrgData.Failed == nil {
		zenOrgData.Failed = map[string]int64{}
	}
}

// SaveZenOrgLocked 触发 zen-state.json 落盘（keyOrg 段随整文件快照，调用方持
// zenOrgMu；SaveZenUsage 内部快照 zenOrgData 与 zenKeyCool 等同批写）。
func SaveZenOrgLocked() {
	SaveZenUsage()
}

// ZenOrgSnapshotLocked keyOrg 段快照（调用方持 zenOrgMu）：空表返回 nil。
// SaveZenUsage / StateBackupSnapshot 共用。
func ZenOrgSnapshotLocked() *ZenOrgStore {
	if !zenOrgLoaded || (len(zenOrgData.Orgs) == 0 && len(zenOrgData.Failed) == 0) {
		return nil
	}
	c := ZenOrgStore{
		Version: zenOrgData.Version,
		Orgs:    make(map[string]string, len(zenOrgData.Orgs)),
		Failed:  make(map[string]int64, len(zenOrgData.Failed)),
	}
	for k, v := range zenOrgData.Orgs {
		c.Orgs[k] = v
	}
	for k, v := range zenOrgData.Failed {
		c.Failed[k] = v
	}
	return &c
}

// ZenApplyOrgSnapshot 把 keyOrg 快照灌进内存真身（调用方持 zenOrgMu）。
// LoadZenUsage / ImportStateBackup / ImportStateFacts 共用。
func ZenApplyOrgSnapshot(s *ZenOrgStore) {
	if s == nil {
		return
	}
	zenOrgData = *s
	if zenOrgData.Orgs == nil {
		zenOrgData.Orgs = map[string]string{}
	}
	if zenOrgData.Failed == nil {
		zenOrgData.Failed = map[string]int64{}
	}
	zenOrgData.Version = ZenOrgFileVersion
	zenOrgLoaded = true
}

// ZenKeyOrg 返回该 key 已解析的组织 ID（可能为空 = 未知，应省略该头）。
//
// 已解析的直接返回；未解析的**同步**尝试一次发现（带负缓存与单飞）。
// 之所以同步：org 头只在请求构造时有用，异步解析意味着前 N 个请求都不带
// 该头——而首次请求正是最需要形态正确的那个（新 key 首用常被重点观察）。
// 单飞 + 负缓存保证每个 key 最多一次往返，且失败后 6 小时内不再重试。
func ZenKeyOrg(key string) string {
	if key == "" {
		return ""
	}
	// service-account key 已知无法发现（2026-11-11 实测：console/api/orgs 对
	// oc_sk_ 恒 401，而同一把 key 对 console/api/v2/config 是 200）。直接返回，
	// 省掉首次请求的一次注定失败的往返。若将来 console 支持了这类 key，把这里
	// 的分支去掉即可恢复探测。
	if strings.HasPrefix(key, "oc_sk_") {
		return ""
	}
	// 匿名哨兵 "public" 不是真实凭据（没有 key 可关联，也就没有 org）。
	// 不特判的话它每次冷启动都会去打一次 console/api/orgs，拿 401 后写负缓存
	// ——纯粹的无效上游往返，还是用假凭据。见 egress.go 对 "public" 的处理。
	if key == "public" {
		return ""
	}
	zenOrgMu.Lock()
	LoadZenOrgOnce()
	if org, ok := zenOrgData.Orgs[ZenKeyID(key)]; ok {
		zenOrgMu.Unlock()
		return org
	}
	if at, ok := zenOrgData.Failed[ZenKeyID(key)]; ok && time.Since(time.Unix(at, 0)) < ZenOrgRetryAfter {
		zenOrgMu.Unlock()
		return ""
	}
	if wg, inflight := zenOrgFlight[key]; inflight {
		zenOrgMu.Unlock()
		wg.Wait()
		zenOrgMu.Lock()
		org := zenOrgData.Orgs[ZenKeyID(key)]
		zenOrgMu.Unlock()
		return org
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	zenOrgFlight[key] = wg
	zenOrgMu.Unlock()

	org, err := DiscoverZenKeyOrg(key)

	// 锁内只改内存；落盘移到 zenOrgMu 之外——SaveZenUsage 内部要取 zenOrgMu
	// 快照（旧形态在临界区内调它，是 zenOrgMu 自死锁的直接成因，
	// TestZenKeyOrgPerKeyNotGlobal 10m0s 挂死实证 2026-11-17）。
	zenOrgMu.Lock()
	delete(zenOrgFlight, key)
	if err == nil && org != "" {
		zenOrgData.Orgs[ZenKeyID(key)] = org
		delete(zenOrgData.Failed, ZenKeyID(key))
	} else {
		zenOrgData.Failed[ZenKeyID(key)] = time.Now().Unix()
	}
	zenOrgMu.Unlock()
	SaveZenUsage()
	wg.Done()

	if err != nil {
		log.Printf("  zen key-org: discover failed for key=#%d: %v (header omitted; will retry in %s)",
			KeyIndex(key), err, ZenOrgRetryAfter)
	} else if org != "" {
		log.Printf("  zen key-org: resolved key=#%d -> %s", KeyIndex(key), org)
	}
	return org
}

// DiscoverZenKeyOrg 向 Console 查询该 key 所属的组织 ID。
//
// 端点与官方客户端同源（二进制静态分析：opencode.ai/console/api/orgs）。
// 返回 ("", nil) 表示上游答复里没有 org 列表（合法的"无 org"状态），
// 返回 error 表示没能得出结论（网络/鉴权/格式），调用方按失败负缓存处理。
func DiscoverZenKeyOrg(key string) (string, error) {
	// proxyClientFor 的 client 没有整体超时（推理请求靠 ctx 控制），而这里是
	// 请求路径上的同步调用——必须自己加 deadline，否则慢代理能把首个请求挂住。
	ctx, cancel := context.WithTimeout(context.Background(), ZenOrgHTTPTimeout)
	defer cancel()

	base := ZenOrgDiscoverBase()
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/console/api/orgs", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", zenNativeUA)
	// 与推理请求走同一个出口（本仓 boundExitFor：显式绑定→自动钉定→走池/直连
	// 的同一解析链，取代上游的 zenEgress——本仓已把出口解析统一到该函数）：
	// console 侧同样按 key+IP 记账，用另一个 IP 去查这把 key 的 org 本身就会
	// 形成"同一 key 出现在两个 IP"的形态。解析不出可用出口（!ok）时退回
	// 直连——org 是可选头，不能因为它把请求搞失败。
	proxyURL, _, exitOK := Host.BoundExitFor(key)
	if !exitOK {
		proxyURL = ""
	}
	client := exitpool.ClientFor(proxyURL)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		// 401 ApiKeyExpired / 403 / 404 都归为"结论未知"，不是错误路径上的硬失败。
		// 正文可能回显 key 片段，落日志前打码。
		return "", &ZenOrgStatusError{Status: resp.StatusCode, Body: RedactZenSecrets(kit.Truncate(string(body), 160))}
	}
	var orgs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &orgs); err != nil {
		return "", err
	}
	// 与官方客户端一致：key 未指定 org 时取列表第一个（官方按 name/id 排序后取首）。
	// 这里保持"取首个返回项"——顺序由服务端决定，网关不做二次猜测。
	for _, o := range orgs {
		if strings.HasPrefix(o.ID, "wrk_") || strings.HasPrefix(o.ID, "org_") {
			return o.ID, nil
		}
	}
	return "", nil
}

type ZenOrgStatusError struct {
	Status int
	Body   string
}

func (e *ZenOrgStatusError) Error() string {
	return "console/api/orgs " + http.StatusText(e.Status) + ": " + e.Body
}

// ZenOrgDiscoverBase 允许测试覆写到本地 httptest 服务；生产恒为 opencode.ai。
var ZenOrgDiscoverBase = func() string { return "https://opencode.ai" }

// ZenKeyOrgForRequest 请求路径用：解析该 key 的 org，发现不到则回退到
// 手工配置的全局 cfg.OrgID（若有），否则返回空（调用方省略该头）。
//
// 回退顺序刻意如此：key 自己的 org 永远优先。全局值是管理员显式配置的
// 兜底（例如所有 key 同属一个 org 的老部署），不是覆盖。
func ZenKeyOrgForRequest(key string) (org string, source string) {
	if org := ZenKeyOrg(key); org != "" {
		return org, "discovered"
	}
	// 解析不到就用管理员显式配置的兜底——但仅在**唯一 key** 时。
	//
	// 理由：该头会被上游按 workspace 校验（错发 = 硬 403）。单 key 部署里
	// 管理员填的值只能是这把 key 的 org，是安全的显式覆盖；多 key 部署里
	// 同一个值必然与其中一部分 key 不匹配，套上去等于主动制造 403。
	// 所以多 key 时宁可不发（缺头是合法的，官方客户端常态就不发）。
	if cfg := GetZenConfig(); cfg != nil {
		if g := strings.TrimSpace(cfg.OrgID); g != "" && len(cfg.Keys) <= 1 {
			return g, "config-fallback"
		}
	}
	return "", ""
}

// ZenKeyOrgInfo 单个 key 的组织解析状态。
type ZenKeyOrgInfo struct {
	OrgID  string
	Source string // discovered | config-fallback | ""（未解析）
}

// ZenKeyOrgMap 面板用：按 keys 顺序返回每把 key 的组织解析状态（只含状态，
// 绝不含 key 本身）。已解析的直接给出；未解析的**不做**网络发现——面板刷新
// 是高频操作，不能因为打开面板就去探测上游。发现只发生在实际请求路径上。
func ZenKeyOrgMap(keys []string) []ZenKeyOrgInfo {
	out := make([]ZenKeyOrgInfo, len(keys))
	// 先取兜底值（zenConfigMu），再进 zenOrgMu：两把锁不嵌套，避免锁序反转。
	fallback := strings.TrimSpace(GetZenConfig().OrgID)
	zenOrgMu.Lock()
	LoadZenOrgOnce()
	for i, k := range keys {
		if org, ok := zenOrgData.Orgs[ZenKeyID(k)]; ok && org != "" {
			out[i] = ZenKeyOrgInfo{OrgID: org, Source: "discovered"}
			continue
		}
		// 未发现：仅单 key 部署时才回退到配置值（多 key 下不发，见 ZenKeyOrgForRequest）。
		if fallback != "" && k != "public" && len(keys) <= 1 {
			out[i] = ZenKeyOrgInfo{OrgID: fallback, Source: "config-fallback"}
		}
	}
	zenOrgMu.Unlock()
	return out
}

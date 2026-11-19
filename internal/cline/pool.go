package cline

import (
	"afree-proxy/internal/exitpool"
	"afree-proxy/internal/kit"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	pool       *AccountPool
	PoolMu     sync.Mutex
	poolSaveMu sync.Mutex
	poolDirty  bool // 热路径变更标记，由后台 flusher 周期落盘
)

// DataPath cline 平台配置文件路径，运行时求值（不走 init 缓存）：测试经
// t.Setenv("DATA_DIR", …) 切换数据目录后必须生效，init 期缓存会把测试写盘引到
// 真实 data 目录并造成跨测试残留（TestClineStreamConcurrentLearnsPersistAll
// persisted 15/16 实证，2026-11-17）。
// DataPathOverride 测试钩子：非空时覆盖 cline 平台配置路径（与
// proxyConfigPathOverride 同款；pool_enable_test 等直写 poolPath 的旧测试迁移）。
var DataPathOverride string

func DataPath() string {
	if DataPathOverride != "" {
		return DataPathOverride
	}
	return kit.ResolveDataPath("cline-config.json")
}

// MarkDirtyLocked 标记使用痕迹有待落盘变更（调用方必须持有 PoolMu）。
// 热路径（选号/计数）不再每次写盘 —— PoolMu 是全局锁，磁盘 I/O 会把所有
// 在途请求串行化；后台 flusher 每 2s 补一次写。2026-10-31 拆分后 flusher
// 只写 cline-state.json（痕迹），配置文件仅在显式保存点写。
func MarkDirtyLocked() {
	poolDirty = true
}

// StartFlusher 启动痕迹文件后台落盘循环；进程退出由 FlushNow 兜底。
func StartFlusher() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			PoolMu.Lock()
			dirty := poolDirty
			poolDirty = false
			PoolMu.Unlock()
			if dirty {
				SaveUsage()
			}
		}
	}()
}

// FlushNow 立即落盘（优雅停机时调用，防止丢最后 2s 的状态变更）。
func FlushNow() {
	PoolMu.Lock()
	dirty := poolDirty
	poolDirty = false
	if dirty {
		SaveUsageLocked()
	}
	PoolMu.Unlock()
}

// kit.ResolveDataPath 数据文件路径解析：优先可执行文件目录，其次当前工作目录。
// go run 运行时编译产物在临时目录，此时应回退到工作目录（项目根）查找数据文件。

func LoadPool() *AccountPool {
	PoolMu.Lock()
	defer PoolMu.Unlock()
	return loadPoolLocked()
}

// loadPoolLocked LoadPool 的锁内核心（调用方必须已持 PoolMu）。Mutex 不可重入
// ——持锁路径（MutateConfig 等）必须走本函数，绝不能再进 LoadPool
// （TestConfigBackupStateRoundTrip 自死锁 5m0s 挂死实证，2026-11-17）。
func loadPoolLocked() *AccountPool {
	if pool != nil {
		return pool
	}

	data, err := os.ReadFile(DataPath())
	if err != nil {
		pool = &AccountPool{Accounts: []*Account{}}
		return pool
	}

	var p AccountPool
	if err := json.Unmarshal(data, &p); err != nil {
		// 损坏的池文件绝不能被空池静默覆盖 —— 先把原文件改名留档，
		// 否则一次崩溃中途写入就会永久丢失全部账号凭证
		stamp := time.Now().Format("20060102-150405")
		if renErr := os.Rename(DataPath(), DataPath()+".corrupt-"+stamp); renErr == nil {
			log.Printf("SEVERE: account pool file is corrupt JSON; moved to %s.corrupt-%s for manual recovery; starting with an EMPTY pool", DataPath(), stamp)
		} else {
			log.Printf("SEVERE: account pool file is corrupt JSON (quarantine rename failed: %v); starting with an EMPTY pool", renErr)
		}
		pool = &AccountPool{Accounts: []*Account{}}
		return pool
	}

	if p.Accounts == nil {
		p.Accounts = []*Account{}
	}
	pool = &p
	// 旧池文件没有 poolEnabled 字段：在内存里立即物化为默认 true，
	// 下一次落盘即可得到显式布尔值，同时不改写账号的 active/cooldown/expired。
	for _, a := range pool.Accounts {
		if a != nil && a.PoolEnabled == nil {
			enabled := true
			a.PoolEnabled = &enabled
		}
	}
	// 回放自定义请求头。调度策略存在 afree-config.json（globalStrategy 直接读，
	// POOL_STRATEGY env 显式设置时以 env 为准，见 ApplyEnvConfig）。
	if len(p.Headers) > 0 {
		h := make(map[string]string, len(p.Headers))
		for k, v := range p.Headers {
			h[k] = v
		}
		// 持久化的旧版本指纹头升级到当前常量（只动明确是旧默认形态的值），
		// 否则客户端版本升级对已部署实例不生效。
		if n := Host.NormalizeIdentityHeader(h); n > 0 {
			log.Printf("pool: upgraded %d persisted identity header(s) to current client version", n)
		}
		Host.SyncPersistedHeaders(h)
	}
	// 使用痕迹从独立文件加载（含旧配置文件的一次性迁移，见 pool_usage.go）。
	// 必须在此处（任何 cline-config.json 写回之前）完成，否则迁移提取不到
	// 旧文件里的痕迹键。
	loadPoolUsage()
	return pool
}

// SetHeaders 把调度策略与自定义请求头写入账号池文件（重启后由
// LoadPool 回放）。策略受 POOL_STRATEGY env 覆盖，请求头无 env 覆盖。
func SetHeaders(headers map[string]string) {
	p := LoadPool()
	PoolMu.Lock()
	h := make(map[string]string, len(headers))
	for k, v := range headers {
		h[k] = v
	}
	p.Headers = h
	PoolMu.Unlock()
	SavePool()
}

// PoolAccountCount 线程安全地返回账号池大小。
func PoolAccountCount() int {
	p := LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()
	return len(p.Accounts)
}

// PoolStatusSnapshot 线程安全地返回 (账号总数, 活跃数) 快照。
// Status/Accounts 由池锁保护，任何请求路径的读取都必须走这里。
func PoolStatusSnapshot() (total, active int) {
	total, active, _, _ = PoolStatusCounts()
	return total, active
}

// PoolStatusCounts 线程安全地返回 (总数, active, cooldown, expired) 快照。
func PoolStatusCounts() (total, active, cooldown, expired int) {
	p := LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()
	for _, a := range p.Accounts {
		total++
		switch AccountStatusLocked(a) {
		case "active":
			if AccountPoolEnabled(a) {
				active++
			}
		case "cooldown":
			cooldown++
		case "expired":
			expired++
		}
	}
	return
}

// AccountPoolEnabled 读取显式参与轮换开关；旧数据缺失时按启用处理。
func AccountPoolEnabled(a *Account) bool {
	return a == nil || a.PoolEnabled == nil || *a.PoolEnabled
}

// SetAccountPoolEnabled 保存账号是否参与轮换；只写独立开关，不改 Status。
// 找到账号后立即落盘，避免保存请求依赖后台 flusher 才生效。
func SetAccountPoolEnabled(accountID string, enabled bool) error {
	p := LoadPool()
	PoolMu.Lock()
	found := false
	for _, a := range p.Accounts {
		if a != nil && a.AccountID == accountID {
			a.PoolEnabled = &enabled
			found = true
			break
		}
	}
	PoolMu.Unlock()
	if !found {
		return fmt.Errorf("account not found: %s", accountID)
	}
	SavePool()
	return nil
}

// SetAllAccountsPoolEnabled 一键启用/禁用全部账号的参与轮换开关（面板批量
// 入口），返回实际改写的账号数。语义与 SetAccountPoolEnabled 一致：只动
// PoolEnabled，不改账号 Status。
func SetAllAccountsPoolEnabled(enabled bool) int {
	p := LoadPool()
	PoolMu.Lock()
	n := 0
	for _, a := range p.Accounts {
		if a == nil {
			continue
		}
		if a.PoolEnabled != nil && *a.PoolEnabled == enabled {
			continue
		}
		v := enabled
		a.PoolEnabled = &v
		n++
	}
	PoolMu.Unlock()
	if n > 0 {
		SavePool()
	}
	return n
}

// PoolCurrentIdx 线程安全地读取账号轮转游标（痕迹文件侧，2s flusher 持久化）。
func PoolCurrentIdx() int {
	LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()
	return poolUsage.CurrentIdx
}

// ReorderAccounts 按给定 accountId 顺序重排账号池（管理面板拖拽排序）。
// 顺序即 JSON 数组顺序：round-robin 轮转按 CurrentIdx 起始遍历 Accounts，
// 重排后新顺序立即对选号轮转生效（"即时生效"）。
// 约束：order 中的 accountId 必须都存在于池中（未知 id 整体拒绝，防手滑漏号）；
// 未出现在 order 里的账号按原相对顺序追加在尾部。CurrentIdx 重映射到原游标
// 指向账号的新位置（游标语义不变，轮转不跳号）；立即落盘。
// 返回重排后的账号总数。
func ReorderAccounts(order []string) (int, error) {
	p := LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()

	if len(order) == 0 {
		return 0, fmt.Errorf("order is required")
	}
	byID := make(map[string]*Account, len(p.Accounts))
	for _, a := range p.Accounts {
		byID[a.AccountID] = a
	}
	// 游标重映射基准：重排前 CurrentIdx 指向的账号（越界按无游标处理）。
	// 游标住痕迹文件（pool_usage.go）。
	var curID string
	if poolUsage.CurrentIdx >= 0 && poolUsage.CurrentIdx < len(p.Accounts) {
		curID = p.Accounts[poolUsage.CurrentIdx].AccountID
	}

	next := make([]*Account, 0, len(p.Accounts))
	seen := make(map[string]bool, len(order))
	for _, id := range order {
		a, ok := byID[id]
		if !ok {
			return 0, fmt.Errorf("unknown accountId: %s", id)
		}
		if seen[id] {
			continue // 重复 id 幂等跳过
		}
		seen[id] = true
		next = append(next, a)
	}
	if len(next) != len(p.Accounts) {
		// 未提及的账号按原相对顺序追加在尾部
		for _, a := range p.Accounts {
			if !seen[a.AccountID] {
				next = append(next, a)
			}
		}
	}

	p.Accounts = next
	poolUsage.CurrentIdx = 0
	if curID != "" {
		for i, a := range next {
			if a.AccountID == curID {
				poolUsage.CurrentIdx = i
				break
			}
		}
	}
	SavePoolLocked()  // 管理操作直接落盘（不走 2s flusher，避免多余写盘）
	SaveUsageLocked() // 游标重映射结果随本次落盘进痕迹文件
	return len(next), nil
}

// ClineUseProxiesPtr 读取 cline 走池开关（cline-config.json useProxies，
// 2026-11-17 迁回平台文件）；nil = 未配置 = 走池。platformUseProxies 的
// cline 分发落点。锁纪律：LoadPool 自取 PoolMu，本函数不得外层再持——
// Mutex 不可重入，外层持锁再进 LoadPool 是自死锁（TestConfigBackupStateRoundTrip
// 5m0s 挂死实证，2026-11-17）。
func ClineUseProxiesPtr() *bool {
	p := LoadPool()
	if p.UseProxies == nil {
		return nil
	}
	v := *p.UseProxies
	return &v
}

// ClineUseProxies 线程安全地读取 cline 走代理池开关；未设置（nil）默认开启。
func ClineUseProxies() bool {
	if v := Host.UseProxies(); v != nil {
		return *v
	}
	return true
}

// PoolAdminLogEnabled 线程安全地读取"记录 /admin 访问日志"开关；未设置（nil）
// 默认关闭 —— 本地自用部署不关心页面轮询产生的噪声行，只保留经过网关的模型
// 会话日志。请求日志中间件在每个 /admin 请求上调用一次。
func PoolAdminLogEnabled() bool { return Host.AdminLogEnabled() }

// PoolKeysSnapshot 线程安全地返回客户端 API key 列表副本。
func PoolKeysSnapshot() []string { return Host.ClientKeys() }

func SavePool() {
	PoolMu.Lock()
	defer PoolMu.Unlock()
	SavePoolLocked()
}

// MutateConfig 在 PoolMu 锁内读改写 cline-config.json 全量 AccountPool 并
// 落盘（2026-11-17 配置重组：cline 平台私有键——useProxies/streamRequired/
// catalogFeedOnly——归本文件，跨平台键仍在 afree-config.json）。
func MutateConfig(fn func(p *AccountPool)) {
	PoolMu.Lock()
	defer PoolMu.Unlock()
	fn(loadPoolLocked())
	SavePoolLocked()
}

// clineCatalogFeedOnly 访问器（2026-11-17 迁入 cline-config.json：
// AccountPool.CatalogFeedOnly，原 afree-config.json clineCatalogFeedOnly 段）。
func CatalogFeedOnly() bool {
	p := LoadPool()
	return p.CatalogFeedOnly != nil && *p.CatalogFeedOnly
}

func SetCatalogFeedOnly(v bool) {
	MutateConfig(func(p *AccountPool) { p.CatalogFeedOnly = &v })
}

// SavePoolLocked 持久化账号池；调用方必须已经持有 PoolMu。
// 原子写: 先写临时文件再 rename，崩溃中途写入不会留下截断的 JSON。
func SavePoolLocked() {
	poolSaveMu.Lock()
	defer poolSaveMu.Unlock()

	data, err := json.MarshalIndent(pool, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal accounts: %v", err)
		return
	}
	tmp := DataPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		log.Printf("Failed to save accounts: %v", err)
		return
	}
	if err := os.Rename(tmp, DataPath()); err != nil {
		log.Printf("Failed to save accounts (rename): %v", err)
	}
}

func AddAccount(acc *Account) {
	if acc != nil && acc.PoolEnabled == nil {
		// 新增账号默认不参与轮换：面板勾选"启用"后才进入选号（三平台统一口径）。
		// 存量数据兼容不受影响——LoadPool 对缺字段的旧账号仍物化为启用。
		disabled := false
		acc.PoolEnabled = &disabled
	}
	p := LoadPool()
	PoolMu.Lock()
	p.Accounts = append(p.Accounts, acc)
	PoolMu.Unlock()
	SavePool()
	// 新账号要参与出口分配（可能就分到下一个空闲代理）
	Host.ReconcileEgress()
}

func RemoveAccount(accountID string) bool {
	p := LoadPool()
	PoolMu.Lock()

	for i, a := range p.Accounts {
		if a.AccountID == accountID {
			p.Accounts = append(p.Accounts[:i], p.Accounts[i+1:]...)
			// 账号删除后痕迹表的孤儿条目一并清理（痕迹随账号走）
			valid := map[string]bool{}
			for _, rest := range p.Accounts {
				valid[rest.AccountID] = true
			}
			PruneUsageLocked(valid)
			SavePoolLocked()
			SaveUsageLocked()
			PoolMu.Unlock()
			// 释放该账号占用的代理给下一个账号
			Host.ReconcileEgress()
			return true
		}
	}
	PoolMu.Unlock()
	return false
}

func GetAccountByID(accountID string) *Account {
	p := LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()

	for _, a := range p.Accounts {
		if a.AccountID == accountID {
			return a
		}
	}
	return nil
}

// RefreshAccountToken 刷新账号 access token。
// 单飞（single-flight）：同一账号的并发 401 只发一次刷新请求 ——
// cline 的 refresh token 是轮换型，两个并发刷新会让后到者拿旧 token
// 换新失败，进而把还活着的账号误标为 expired。
// SetDataPathOverride 测试钩子：覆盖配置文件路径（"" = 清除）。
func SetDataPathOverride(v string) { DataPathOverride = v }

// GetDataPathOverride 当前 override（测试恢复用）。
func GetDataPathOverride() string { return DataPathOverride }

// DirtyFlag dirty 位读数（测试恢复用）。
func DirtyFlag() bool { return poolDirty }

// SetDirtyForTest 清 dirty 位（测试恢复用）。
func SetDirtyForTest(v bool) { poolDirty = v }

// RawPool 池真身指针（测试读取用；改写请走 SetPool）。
func RawPool() *AccountPool { return pool }

// SetPool 直接替换池真身（测试隔离 / 数据管理路径）。
func SetPool(p *AccountPool) { pool = p }

// ResetPool 清空全部账号与痕迹并落盘（数据清空端点的池侧语义：客户端 key
// 等全局段不在本域，调用方自行保留）。
func ResetPool() {
	LoadPool()
	PoolMu.Lock()
	pool = &AccountPool{Accounts: []*Account{}}
	ClearUsageLocked()
	PoolMu.Unlock()
	SavePool()
}

// ResetPoolForDataDelete 数据清空端点的池侧复位：账号清空、请求头回出厂、
// dirty 清零、痕迹表与游标一并清空并落盘。返回清空前的账号数。
func ResetPoolForDataDelete(defaultHeaders map[string]string) int {
	old := LoadPool()
	PoolMu.Lock()
	accounts := len(old.Accounts)
	pool = &AccountPool{
		Accounts: []*Account{},
		Headers:  defaultHeaders,
	}
	poolDirty = false
	ClearUsageLocked()
	PoolMu.Unlock()
	SavePool()
	return accounts
}

// ClearStreamLearned 清空学习表（数据清空端点），返回清除条数。
func ClearStreamLearned() int {
	streamMu.Lock()
	n := len(streamLearned)
	streamLearned = nil
	streamMu.Unlock()
	MutateConfig(func(p *AccountPool) { p.StreamRequired = nil })
	return n
}

// ResetRefreshFlight 作废全部刷新单飞句柄（删号/导入后旧句柄失效）。
func ResetRefreshFlight() {
	refreshFlightMu.Lock()
	refreshFlight = map[*Account]*refreshFlightCall{}
	refreshFlightMu.Unlock()
}

// ReplaceStream 整体替换学习表并落盘（备份导入）。
func ReplaceStream(in map[string]string) {
	next := make(map[string]string, len(in))
	for k, v := range in {
		next[k] = v
	}
	streamMu.Lock()
	streamLearned = next
	streamMu.Unlock()
	SaveStreamLatest()
}

// StreamSnapshot 学习表快照（备份导出）。
func StreamSnapshot() map[string]string {
	streamMu.RLock()
	defer streamMu.RUnlock()
	if len(streamLearned) == 0 {
		return nil
	}
	out := make(map[string]string, len(streamLearned))
	for k, v := range streamLearned {
		out[k] = v
	}
	return out
}

func RefreshAccountToken(acc *Account) error {
	refreshFlightMu.Lock()
	if c, ok := refreshFlight[acc]; ok {
		refreshFlightMu.Unlock()
		<-c.done
		return c.err
	}
	c := &refreshFlightCall{done: make(chan struct{})}
	refreshFlight[acc] = c
	refreshFlightMu.Unlock()

	// panic 安全：中途 panic 也要唤醒等待者，否则它们永久阻塞在 <-c.done 上。
	// close 必须在"从表里删除"之前完成：先删后 close 会开出一个窗口，期间的
	// 到达者看不到在飞的调用，于是再发一次刷新 —— 而 cline 的 refresh token
	// 是轮换型的，第二次刷新必然用旧 token 失败，把活着的账号误标 expired。
	func() {
		defer close(c.done)
		c.err = doRefreshAccountToken(acc)
	}()

	refreshFlightMu.Lock()
	delete(refreshFlight, acc)
	refreshFlightMu.Unlock()
	return c.err
}

type refreshFlightCall struct {
	done chan struct{}
	err  error
}

var (
	refreshFlightMu sync.Mutex
	refreshFlight   = map[*Account]*refreshFlightCall{}
)

func doRefreshAccountToken(acc *Account) error {
	if acc.APIToken != "" {
		// 静态 key 无刷新能力 —— 只有在 key 被上游拒绝（401）后才会走到这里，
		// 直接判失效，不再发无意义的刷新请求
		PoolMu.Lock()
		u := UsageForLocked(acc)
		u.Status = "expired"
		u.LastReason = "api key rejected upstream (static key cannot refresh)"
		SaveUsageLocked()
		PoolMu.Unlock()
		return fmt.Errorf("static api key account cannot refresh")
	}
	// 刷新请求出口化（2026-10-28 风控对齐）：与会话请求同源——绑定主备优先，
	// 未绑定走全局规则（池或直连）。账号会话走代理 IP 而刷新直连，IP 不一致
	// 本身就是风控信号。DISABLE_DIRECT=true 时直连兜底同样不放行。
	client, cerr := refreshClient(acc)
	if cerr != nil {
		PoolMu.Lock()
		u := UsageForLocked(acc)
		u.Status = "cooldown"
		u.CooldownUntil = time.Now().Add(5 * time.Minute)
		u.LastReason = "refresh exit unavailable: " + cerr.Error()
		SaveUsageLocked()
		PoolMu.Unlock()
		return fmt.Errorf("token refresh exit unavailable: %w", cerr)
	}
	resp, err := RefreshClineTokenWithClient(client, acc.RefreshToken)
	if err != nil {
		PoolMu.Lock()
		u := UsageForLocked(acc)
		if IsAuthRejection(err) {
			// 上游明确拒绝该 refresh token（400/401/403）—— 账号确实失效
			u.Status = "expired"
			u.LastReason = "refresh rejected: " + err.Error()
		} else {
			// 网络故障 / 5xx 是瞬时的: 短冷却 5 分钟,不判死账号
			u.Status = "cooldown"
			u.CooldownUntil = time.Now().Add(5 * time.Minute)
			u.LastReason = "refresh transient error: " + err.Error()
		}
		SaveUsageLocked()
		PoolMu.Unlock()
		return fmt.Errorf("token refresh failed: %w", err)
	}
	if resp.Data.AccessToken == "" {
		// 200 但空 token: 上游响应异常,按瞬时故障处理
		MarkAccountCooldown(acc, "refresh returned empty access token", 5*time.Minute)
		return fmt.Errorf("token refresh returned empty access token")
	}

	PoolMu.Lock()
	acc.AccessToken = "workos:" + resp.Data.AccessToken
	if resp.Data.RefreshToken != "" {
		acc.RefreshToken = resp.Data.RefreshToken
	}
	acc.ExpiresAt = ExpiryMs(resp.Data.ExpiresAt)
	// AccessToken 是 json:"-" 的内存态字段，随痕迹文件外的配置文件无关；
	// 状态复位写痕迹文件。
	u := UsageForLocked(acc)
	u.Status = "active"
	u.LastReason = ""
	SaveUsageLocked()
	PoolMu.Unlock()
	return nil
}

// ExpiryMs 解析过期时间并预留 60s 提前量。ParseExpiry 解析失败返回 0，
// 直接使用会得到 ExpiresAt=-60000 → 每个请求都触发刷新（刷新风暴 + 每请求
// 落盘一次）；此时回退到保守的 55 分钟（cline token 生命周期约 1h）。
func ExpiryMs(expiresAt any) int64 {
	exp := ParseExpiry(expiresAt)
	if exp <= 0 {
		return time.Now().UnixMilli() + 55*60_000
	}
	return exp - 60_000
}

// MaxCooldown 冷却时长上限：上游给的间隔（Retry-After / 错误文本解析）
// 可能是离谱的大值，封顶 24h，防止把账号/key/代理冷却到事实上永久下线。
const MaxCooldown = 24 * time.Hour

func PickAccount() *Account {
	p := LoadPool()
	PoolMu.Lock()

	isolation := exitpool.IsolationEnabled()
	active := make([]*Account, 0)
	for _, a := range p.Accounts {
		// 自动解除已到期的冷却
		if u := UsagePeekLocked(a); u != nil && u.Status == "cooldown" &&
			!u.CooldownUntil.IsZero() && time.Now().After(u.CooldownUntil) {
			u.Status = "active"
			u.CooldownUntil = time.Time{}
			u.LastReason = ""
			MarkDirtyLocked()
		}
		if AccountStatusLocked(a) != "active" {
			continue
		}
		if !AccountPoolEnabled(a) {
			continue
		}
		// 隔离模式：绑定主备用出口都不可用（冷却中/已从池中删除）的账号本轮
		// 整体跳过 —— 隔离优先于可用性，绝不退回其他出口。检查在 PoolMu 内
		// 完成：读的是绑定字段本身；可用性走 proxyCooldownsMu/zenConfigMu，
		// 都是叶子锁且无反向获取，无锁序风险。
		if isolation && !exitpool.BoundProxiesRoutableFor(KindCline, a.ProxyMain, a.ProxyBackup) {
			continue
		}
		active = append(active, a)
	}

	if len(active) == 0 {
		PoolMu.Unlock()
		return nil
	}

	var acc *Account
	switch Host.Strategy() {
	case "fill":
		// Always pick the first available (fill)
		acc = active[0]
	case "random":
		// Random selection
		n := time.Now().UnixNano() % int64(len(active))
		acc = active[n]
	default: // round_robin
		// 下界也要夹：state 文件被手改/旧版写入负值时 active[-n] 会 panic，
		// 请求路径有 recover 兜，后台目录同步 goroutine 没有 —— panic 会打挂进程。
		if poolUsage.CurrentIdx < 0 || poolUsage.CurrentIdx >= len(active) {
			poolUsage.CurrentIdx = 0
		}
		acc = active[poolUsage.CurrentIdx]
		poolUsage.CurrentIdx = (poolUsage.CurrentIdx + 1) % len(active)
	}

	MarkDirtyLocked()
	PoolMu.Unlock()
	return acc
}

// PickAccountExcluding 取一个 active 账号,跳过 exclude 中的账号 ID（并跳过
// 无凭证账号）。供 callClineAPI 的跨账号 failover 使用: 429/5xx 后换一个
// 不同的账号重试,而不是把整个请求判死。
//
// useProxies 为 true 且绑定策略生效时，只返回"分到了出口代理"的账号：
// 没有出口的账号被过滤掉，而不是退回直连——cline 若也推行"一账号一 IP"，
// 直连流量正是它用来识别同一个人的依据。
// cline 账号不接上游的 egress 自动钉定（整合决策见 egress.go 头注释）：账号级
// 出口绑定走本地 ProxyMain/ProxyBackup 体系，未绑定账号按 useProxies 走池轮转。
func PickAccountExcluding(exclude map[string]bool) *Account {
	p := LoadPool()
	PoolMu.Lock()

	// 隔离模式：绑定主备都不可用（冷却中/已从池中删除）的账号整体跳过——
	// 与 PickAccount 同一口径。本函数是请求链（callClineAPI failover）实际
	// 使用的选号函数，缺这条检查时绑定出口冷却的账号会被轮到，随后在
	// pickBoundProxy 处以非 APIError 失败、整请求终止且不换号
	//（2026-10-29 审计修复）。检查在 PoolMu 内完成，读的是绑定字段本身；
	// 可用性走 proxyCooldownsMu/zenConfigMu，都是叶子锁且无反向获取。
	isolation := exitpool.IsolationEnabled()
	active := make([]*Account, 0, len(p.Accounts))
	for _, a := range p.Accounts {
		if u := UsagePeekLocked(a); u != nil && u.Status == "cooldown" &&
			!u.CooldownUntil.IsZero() && time.Now().After(u.CooldownUntil) {
			u.Status = "active"
			u.CooldownUntil = time.Time{}
			u.LastReason = ""
			MarkDirtyLocked()
		}
		if AccountStatusLocked(a) != "active" {
			continue
		}
		// poolEnabled=false 等价于“池里没有这个账号”：面板 Pool 列勾掉之后，
		// 既不被选号，也不作为 429/5xx 的故障转移候选。
		if !AccountPoolEnabled(a) {
			continue
		}
		if isolation && !exitpool.BoundProxiesRoutableFor(KindCline, a.ProxyMain, a.ProxyBackup) {
			continue
		}
		if exclude[a.AccountID] {
			continue
		}
		if a.APIToken == "" && a.RefreshToken == "" {
			continue
		}
		active = append(active, a)
	}

	if len(active) == 0 {
		PoolMu.Unlock()
		return nil
	}

	var acc *Account
	switch Host.Strategy() {
	case "fill":
		acc = active[0]
	case "random":
		n := time.Now().UnixNano() % int64(len(active))
		acc = active[n]
	default: // round_robin
		if poolUsage.CurrentIdx < 0 || poolUsage.CurrentIdx >= len(active) {
			poolUsage.CurrentIdx = 0
		}
		acc = active[poolUsage.CurrentIdx]
		poolUsage.CurrentIdx = (poolUsage.CurrentIdx + 1) % len(active)
	}

	MarkDirtyLocked()
	PoolMu.Unlock()
	return acc
}

// PickAccountAny 取任意一个仍持有凭证的账号（含冷却中的），供与推理配额
// 无关的只读调用使用（如 /ai/cline/recommended-models 免费模型列表）。
// 推理额度耗尽（429→cooldown）不影响这类接口：实测 4 个账号全部 429 时
// 模型列表接口仍 200；只走 PickAccount() 会因 "no active accounts" 整轮
// 不同步，模型列表于是只剩种子兜底，已下线的模型会一直挂着。
// 停用（poolEnabled=false）的账号即便在冷却中也不使用：池里没有它。
func PickAccountAny() *Account {
	p := LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()
	var fallback *Account
	for _, a := range p.Accounts {
		if a.APIToken == "" && a.RefreshToken == "" {
			continue
		}
		if !AccountPoolEnabled(a) {
			continue
		}
		if AccountStatusLocked(a) == "active" {
			return a
		}
		if fallback == nil {
			fallback = a
		}
	}
	return fallback
}

// AccountNumber 账号在池中的序号（1 基，请求日志展示用）；找不到返回 0。
// 比较指针身份：池内账号是共享指针，选号返回的就是池里的那个。
func AccountNumber(acc *Account) int {
	if acc == nil {
		return 0
	}
	p := LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()
	for i, a := range p.Accounts {
		if a == acc {
			return i + 1
		}
	}
	return 0
}

func EnsureToken(acc *Account) (string, error) {
	// 静态 API key 账号：token 即凭证，永不过期、永不刷新
	if acc.APIToken != "" {
		return acc.APIToken, nil
	}
	// 快照读: AccessToken/ExpiresAt 由刷新协程在 PoolMu 下写入
	PoolMu.Lock()
	tok := acc.AccessToken
	exp := acc.ExpiresAt
	PoolMu.Unlock()
	if tok != "" && time.Now().UnixMilli() < exp {
		return tok, nil
	}

	if err := RefreshAccountToken(acc); err != nil {
		return "", err
	}

	PoolMu.Lock()
	tok = acc.AccessToken
	PoolMu.Unlock()
	return tok, nil
}

// refreshClient token 刷新请求的出口 client：与会话请求同源——绑定主备
// 优先（pickBoundProxy，直连哨兵受 DISABLE_DIRECT 约束），未绑定走全局规则
// （clineUseProxies 开关决定池或直连，池空兜底直连同样受 DISABLE_DIRECT 约束）。
func refreshClient(acc *Account) (*http.Client, error) {
	if acc.ProxyMain != "" || acc.ProxyBackup != "" {
		url, _, ok := exitpool.PickBoundProxyFor(KindCline, acc.ProxyMain, acc.ProxyBackup)
		if !ok {
			return nil, fmt.Errorf("bound proxies unavailable (cooling or removed)")
		}
		return exitpool.ClientFor(url), nil
	}
	if !ClineUseProxies() {
		if kit.DisableDirect() {
			return nil, fmt.Errorf("direct egress is disabled by DISABLE_DIRECT")
		}
		return kit.HTTPClient, nil
	}
	url, _, exitOK := exitpool.UpstreamForKind(KindCline)
	if !exitOK {
		return nil, fmt.Errorf("every pool exit is manually cooled")
	}
	if url == "" && kit.DisableDirect() {
		return nil, fmt.Errorf("no proxy available and direct egress is disabled by DISABLE_DIRECT")
	}
	return exitpool.ClientFor(url), nil
}

func ListAccounts() []*AccountView {
	p := LoadPool()
	PoolMu.Lock()

	// 自动解除已到期的冷却，确保返回的列表是最新状态
	usageDate := time.Now().Format("2006-01-02")
	for _, a := range p.Accounts {
		u := UsageForLocked(a)
		if u.Status == "cooldown" && !u.CooldownUntil.IsZero() && time.Now().After(u.CooldownUntil) {
			u.Status = "active"
			u.CooldownUntil = time.Time{}
			u.LastReason = ""
		}
		if u.UsageDate != usageDate {
			u.UsageDate = usageDate
			u.UsageCountToday = 0
		}
		if u.TokensDate != usageDate {
			u.TokensDate = usageDate
			u.TokensToday = 0
		}
	}
	result := make([]*AccountView, len(p.Accounts))
	for i, a := range p.Accounts {
		u := UsagePeekLocked(a)
		if u == nil {
			u = &AccountUsage{Status: "active"}
		}
		// Don't expose tokens——凭据只随行下发掩码与类型（账号列堆叠展示用，
		// 2026-11-19）：api 型=APIToken、oauth 型=RefreshToken。
		credType, credMask := "oauth", maskCredential(a.RefreshToken)
		if a.APIToken != "" {
			credType, credMask = "api", maskCredential(a.APIToken)
		}
		result[i] = &AccountView{
			AccountID:       a.AccountID,
			Nickname:        a.Nickname,
			CredType:        credType,
			CredMask:        credMask,
			Status:          u.Status,
			LastUsed:        u.LastUsed,
			UsageCount:      u.UsageCount,
			UsageCountToday: u.UsageCountToday,
			UsageDate:       u.UsageDate,
			TokensTotal:     u.TokensTotal,
			TokensToday:     u.TokensToday,
			TokensDate:      u.TokensDate,
			CooldownUntil:   u.CooldownUntil,
			LastReason:      u.LastReason,
			// 代理绑定随列表下发（面板编辑用）。代理 URL 可能含凭据，但代理
			// 池页本来就完整展示列表，信任级别相同。
			ProxyMain:   a.ProxyMain,
			ProxyBackup: a.ProxyBackup,
			// 参与轮换开关随列表下发（面板编辑用）；保存走
			// /admin/api/accounts/pool/enabled。
			PoolEnabled: a.PoolEnabled,
		}
	}
	MarkDirtyLocked()
	PoolMu.Unlock()
	return result
}

// MarkAccountCooldown 将账号置为冷却状态，并记录预计恢复时间。
// duration 为冷却时长；duration<=0 时使用默认冷却。
// 返回设置的恢复时间（调用方用它代替锁外读取痕迹表的 CooldownUntil）。
// 冷却即时落盘痕迹文件（2026-10-28 持久化决策，2026-10-31 落点换
// cline-state.json）。
func MarkAccountCooldown(acc *Account, reason string, duration time.Duration) time.Time {
	if acc == nil {
		return time.Time{}
	}
	if duration <= 0 {
		duration = 18 * time.Hour // 默认 18 小时（Cline 免费额度每日重置）
	}
	if duration > MaxCooldown {
		duration = MaxCooldown
	}
	PoolMu.Lock()
	u := UsageForLocked(acc)
	u.Status = "cooldown"
	u.CooldownUntil = time.Now().Add(duration)
	u.LastReason = reason
	SaveUsageLocked()
	until := u.CooldownUntil
	PoolMu.Unlock()
	return until
}

// BumpUsage 递增本地成功调用计数（含今日计数），自动处理跨日重置。
func BumpUsage(acc *Account) {
	if acc == nil {
		return
	}

	PoolMu.Lock()
	now := time.Now()
	today := now.Format("2006-01-02")
	u := UsageForLocked(acc)
	if u.UsageDate != today {
		u.UsageDate = today
		u.UsageCountToday = 0
	}
	u.UsageCountToday++
	u.UsageCount++
	u.LastUsed = now
	MarkDirtyLocked()
	PoolMu.Unlock()
}

// ResetTodayUsage 仅重置本地今日调用计数，不影响累计调用次数。
func ResetTodayUsage(acc *Account) {
	if acc == nil {
		return
	}

	PoolMu.Lock()
	u := UsageForLocked(acc)
	u.UsageDate = time.Now().Format("2006-01-02")
	u.UsageCountToday = 0
	u.TokensDate = time.Now().Format("2006-01-02")
	u.TokensToday = 0
	SaveUsageLocked()
	PoolMu.Unlock()
}

// RecordAccountTokens 记录账号本次请求消耗的 token（prompt+completion），
// 自动处理跨日重置，累计值不重置。tokens<=0 时忽略。
func RecordAccountTokens(acc *Account, tokens int64) {
	if acc == nil || tokens <= 0 {
		return
	}

	PoolMu.Lock()
	today := time.Now().Format("2006-01-02")
	u := UsageForLocked(acc)
	if u.TokensDate != today {
		u.TokensDate = today
		u.TokensToday = 0
	}
	u.TokensToday += tokens
	u.TokensTotal += tokens
	MarkDirtyLocked()
	PoolMu.Unlock()
}

// DescribePoolStatus 汇总当前账号池状态，用于错误诊断。
func DescribePoolStatus() string {
	p := LoadPool()
	PoolMu.Lock()
	defer PoolMu.Unlock()

	total := len(p.Accounts)
	if total == 0 {
		return "pool is empty, use --add-account or admin API to add accounts"
	}

	active, disabled, cooldown, expired := 0, 0, 0, 0
	isolation := exitpool.IsolationEnabled()
	boundBlocked := 0
	var nextRecover *time.Time
	for _, a := range p.Accounts {
		u := UsagePeekLocked(a)
		status := "active"
		if u != nil {
			status = u.Status
		}
		switch status {
		case "active":
			if !AccountPoolEnabled(a) {
				disabled++
				continue
			}
			active++
			if isolation && !exitpool.BoundProxiesRoutableFor(KindCline, a.ProxyMain, a.ProxyBackup) {
				boundBlocked++
			}
		case "cooldown":
			cooldown++
			if u != nil && !u.CooldownUntil.IsZero() {
				if nextRecover == nil || u.CooldownUntil.Before(*nextRecover) {
					t := u.CooldownUntil
					nextRecover = &t
				}
			}
		case "expired":
			expired++
		}
	}

	s := fmt.Sprintf("total=%d active=%d cooldown=%d expired=%d", total, active, cooldown, expired)
	if disabled > 0 {
		s += fmt.Sprintf(", disabled=%d (not participating in rotation)", disabled)
	}
	if boundBlocked > 0 {
		s += fmt.Sprintf(", boundBlocked=%d (proxy isolation: bound exits cooling/removed)", boundBlocked)
	}
	if cooldown > 0 && nextRecover != nil {
		s += fmt.Sprintf(", earliest recover at %s", nextRecover.Format("2006-01-02 15:04:05"))
	}
	return s
}

func AddAccountFromDeviceAuth() (*Account, error) {
	fmt.Println("\n=== Add New Cline Account (OAuth) ===")

	device, err := WorkosDeviceAuth()
	if err != nil {
		return nil, err
	}

	authURL := device.VerificationURIComplete
	if authURL == "" {
		authURL = device.VerificationURI
	}

	fmt.Println("  1. Open this URL in your browser:")
	fmt.Println("     " + authURL)
	fmt.Println("  2. Enter code: " + device.UserCode)
	fmt.Println("  3. Log in with Google, GitHub, or email")

	_ = OpenBrowser(authURL)
	fmt.Println("  Waiting for authorization...")

	interval := device.Interval
	if interval < 5 {
		interval = 5
	}
	expiresIn := device.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300
	}

	workosTok, err := PollWorkosToken(device.DeviceCode, interval, expiresIn)
	if err != nil {
		return nil, err
	}

	fmt.Println("  WorkOS authorized. Registering with Cline...")

	reg, err := RegisterWithCline(workosTok.AccessToken, workosTok.RefreshToken)
	if err != nil {
		return nil, err
	}

	if reg.Data.RefreshToken == "" {
		return nil, fmt.Errorf("cline registration missing refresh token")
	}

	acc := &Account{
		AccountID:    "acc_" + kit.RandHex(8),
		RefreshToken: reg.Data.RefreshToken,
		AccessToken:  "workos:" + reg.Data.AccessToken,
		ExpiresAt:    ExpiryMs(reg.Data.ExpiresAt),
	}

	AddAccount(acc) // 痕迹表懒创建，默认 status=active
	return acc, nil
}

// ClearRefreshFlight 清空刷新单飞句柄表（config import 装配账号段用：旧账号
// 的单飞句柄随账号表整体作废）。
func ClearRefreshFlight() {
	refreshFlightMu.Lock()
	refreshFlight = map[*Account]*refreshFlightCall{}
	refreshFlightMu.Unlock()
}

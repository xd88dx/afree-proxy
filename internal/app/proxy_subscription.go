package app

// 代理订阅自动更新：ProxySubURL 非空且 ProxySubCron 命中时，定时拉取订阅
// 文本并整池替换 Proxies。文本处理与面板"代理列表"保存路径完全一致
// （parseProxyLine 规范化 + validateProxyList 校验）。
//
// 设计要点：
//   - cron 为 5 字段分钟级（分 时 日 月 周），自实现解析/匹配，不引第三方库；
//     支持 * 、数字、列表 a,b、范围 a-b、步进 */n 与 a-b/n。
//   - worker 每 30s tick，对最近越过的每个整分钟做匹配并用 lastFired 去重
//     —— cron 变更热生效（每次 tick 重读配置），无需重启。
//   - 拉取走直连（订阅多为局域网内 socks.txt；容器内无出网代理环境变量），
//     拉取失败或全部行无效时保留原池不动。
//   - 替换经 setZenConfig 完成，复用 config/update 的清理链（proxiesChanged
//     触发冷却清空、健康统计重置、失效客户端驱逐）。

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const proxySubFetchTimeout = 30 * time.Second

// cronField 解析单个 cron 字段为允许值集合。rangeLo/rangeHi 为该字段的
// 合法取值区间（分钟 0-59、小时 0-23、日 1-31、月 1-12、周 0-7，7 视作 0）。
func cronField(field string, lo, hi int) (map[int]bool, error) {
	allowed := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("字段 %q 含空片段", field)
		}
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("字段 %q 步进无效", field)
			}
			step = n
			part = part[:i]
		}
		start, end := lo, hi
		switch {
		case part == "*":
			if step == 1 && len(allowed) == 0 && strings.Contains(field, "*") && !strings.Contains(part, "-") {
				// 全域：由下方循环自然覆盖
			}
		default:
			if i := strings.Index(part, "-"); i >= 0 {
				a, err1 := strconv.Atoi(part[:i])
				b, err2 := strconv.Atoi(part[i+1:])
				if err1 != nil || err2 != nil {
					return nil, fmt.Errorf("字段 %q 范围无效", field)
				}
				start, end = a, b
			} else {
				v, err := strconv.Atoi(part)
				if err != nil {
					return nil, fmt.Errorf("字段 %q 数值无效", field)
				}
				start, end = v, v
			}
		}
		if start < lo || end > hi || start > end {
			return nil, fmt.Errorf("字段 %q 超出取值范围 %d-%d", field, lo, hi)
		}
		for v := start; v <= end; v += step {
			allowed[v] = true
		}
	}
	return allowed, nil
}

// validateCronExpr 校验 5 字段分钟级 cron 表达式。
func validateCronExpr(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("需要 5 个字段（分 时 日 月 周），实际 %d 个", len(fields))
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, f := range fields {
		allowed, err := cronField(f, ranges[i][0], ranges[i][1])
		if err != nil {
			return err
		}
		if len(allowed) == 0 {
			return fmt.Errorf("字段 %q 未匹配任何值", f)
		}
	}
	return nil
}

// cronMatches 报告时刻 t 是否命中表达式。调用方保证表达式已通过 validateCronExpr。
func cronMatches(expr string, t time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	vals := []int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}
	for i, f := range fields {
		allowed, err := cronField(f, ranges[i][0], ranges[i][1])
		if err != nil {
			return false
		}
		v := vals[i]
		if i == 4 && v == 0 {
			v = 7 // 周日：0 与 7 等价
		}
		if !allowed[v] {
			return false
		}
	}
	return true
}

// refreshProxySubscription 拉取订阅并整池替换 Proxies，返回替换条数。
// 拉取失败、逐行全部无效或校验不通过时返回错误且不动现池。
func refreshProxySubscription() (int, error) {
	cfg := getZenConfig()
	url := strings.TrimSpace(cfg.ProxySubURL)
	if url == "" {
		return 0, fmt.Errorf("未配置订阅链接")
	}
	client := &http.Client{Timeout: proxySubFetchTimeout}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, err
	}
	// 与面板"代理列表"保存路径一致：逐行 parseProxyLine 规范化 + validateProxyList
	canonical := make([]string, 0, 16)
	for _, line := range strings.Split(string(body), "\n") {
		parsed, _, err := parseProxyLine(line)
		if err != nil {
			return 0, err
		}
		if parsed == "" {
			continue
		}
		canonical = append(canonical, parsed)
	}
	if len(canonical) == 0 {
		return 0, fmt.Errorf("订阅内容为空或无有效代理行")
	}
	if err := validateProxyList(canonical); err != nil {
		return 0, err
	}
	// 与现池完全一致 → 跳过替换（不落盘、不触发 proxiesChanged 清理链）
	if len(canonical) == len(cfg.Proxies) {
		same := true
		for i := range canonical {
			if canonical[i] != cfg.Proxies[i] {
				same = false
				break
			}
		}
		if same {
			return 0, nil
		}
	}
	next := *cfg
	next.Proxies = canonical
	setZenConfig(&next)
	return len(canonical), nil
}

var (
	proxySubMu        sync.Mutex
	proxySubLastFired = map[string]int{} // "url\x00cron" -> 已触发的分钟时间戳（Unix 分钟）
)

// proxySubManualBusy 手动触发的防重入标志（手动与 cron 定时共用
// refreshProxySubscription；双击按钮或恰逢定时触发时直接拒绝而非排队）。
var (
	proxySubManualMu   sync.Mutex
	proxySubManualBusy bool
)

// triggerProxySubscription 面板"立即更新"按钮：不等 cron 立即执行一次订阅
// 刷新。返回 (n, skipped, err)：n>0 = 整池替换为 n 条；skipped = 订阅内容与
// 现池一致（现池未动）；err = 失败（拉取失败/未配置订阅链接，现池未动）。
func triggerProxySubscription() (int, bool, error) {
	proxySubManualMu.Lock()
	if proxySubManualBusy {
		proxySubManualMu.Unlock()
		return 0, false, fmt.Errorf("上一次订阅更新仍在进行中")
	}
	proxySubManualBusy = true
	proxySubManualMu.Unlock()
	defer func() {
		proxySubManualMu.Lock()
		proxySubManualBusy = false
		proxySubManualMu.Unlock()
	}()
	n, err := refreshProxySubscription()
	if err != nil {
		log.Printf("WARN: [proxy-sub] 手动更新失败: %v（保留原池兜底）", err)
		return 0, false, err
	}
	if n == 0 {
		log.Printf("INFO: [proxy-sub] 手动更新：订阅内容与现池一致，跳过替换")
		return 0, true, nil
	}
	log.Printf("INFO: [proxy-sub] 手动更新完成：池内代理 %d 条", n)
	return n, false, nil
}

// handleProxySubUpdate POST /admin/api/proxypool/subscription/update
// 立即执行一次订阅更新任务（与 cron 定时同一条刷新链路）。
func handleProxySubUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	n, skipped, err := triggerProxySubscription()
	if err != nil {
		writeAPI(w, http.StatusOK, apiResponse{Error: err.Error()})
		return
	}
	msg := fmt.Sprintf("已更新代理池：%d 条", n)
	if skipped {
		msg = "订阅内容与现池一致，跳过替换"
	}
	writeAPI(w, http.StatusOK, apiResponse{Success: true, Message: msg, Data: map[string]any{"updated": n, "skipped": skipped}})
}

// StartProxySubscriptionWorker 启动订阅自动更新 worker（进程级单实例）。
func StartProxySubscriptionWorker() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for now := range ticker.C {
			cfg := getZenConfig()
			url := strings.TrimSpace(cfg.ProxySubURL)
			expr := strings.TrimSpace(cfg.ProxySubCron)
			if url == "" || expr == "" {
				continue
			}
			if validateCronExpr(expr) != nil {
				continue // 面板已校验；手改配置文件的坏表达式静默跳过
			}
			key := url + "\x00" + expr
			nowMin := int(now.Unix() / 60)
			proxySubMu.Lock()
			last := proxySubLastFired[key]
			proxySubMu.Unlock()
			// 检查 (last, nowMin] 区间内是否有命中的整分钟（30s tick 防漏防重）
			fired := false
			for m := last + 1; m <= nowMin; m++ {
				if cronMatches(expr, time.Unix(int64(m)*60, 0).Local()) {
					fired = true
					break
				}
			}
			if !fired || last == nowMin {
				continue
			}
			proxySubMu.Lock()
			proxySubLastFired[key] = nowMin
			proxySubMu.Unlock()
			n, err := refreshProxySubscription()
			if err != nil {
				log.Printf("WARN: [proxy-sub] 订阅更新失败 url=%s: %v（保留原池兜底）", maskProxyURL(url), err)
				continue
			}
			if n == 0 {
				log.Printf("INFO: [proxy-sub] 订阅内容与现池一致，跳过替换 url=%s", maskProxyURL(url))
				continue
			}
			log.Printf("INFO: [proxy-sub] 订阅更新完成 url=%s 池内代理 %d 条", maskProxyURL(url), n)
		}
	}()
}

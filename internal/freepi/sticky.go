package freepi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// ============ 会话粘性（2026-11-25）============
//
// 背景：freepi 上游把「同一段对话历史（messages）+ 不同 Bearer token」判定为
// 账号共享/轮转，回模型文本 "This conversation belongs to another free-pi
// account. Sharing or rotating accounts isn't allowed"。实测：同一把 key 连跑
// 4 轮全 200，跨 key 才触发；afree 默认 round_robin 每轮换 key，agent 多轮必撞。
//
// 本文件实现「对话 → key」粘性映射：同一会话的多轮请求锁定同一把 key。
// 会话标识优先显式 ID（请求头/body），回退首条 user 消息指纹（agent 每轮发
// 完整历史，首条 user 稳定；system 常带动态时间戳故不用）。指纹加调用方 API
// key 做 namespace，防不同调用方撞指纹。
//
// 粘住的 key 不可用时：快速失败，绝不换 key 重试（换 key = 同一 messages 换
// token，必然再撞上游风控，且白费一次调用）。失败仍按原规则 Cooldown 该 key
// （让其他会话避开），但本次请求立即返回错误。非粘性会话保持原轮转语义。
//
// 映射落盘在 freepi-state.json 的 stickySessions 段，与 Cool/KeyTokens 同域
// （keyMu 保护），重启沿用。空闲 TTL 默认 30 分钟惰性淘汰，条数上限 500 LRU。

const (
	stickyMaxEntries = 500
)

// StickyEntry 一条会话映射（落盘形态）。Key 为 key 明文，与 Cool/KeyTokens
// 同口径（明文存 state；面板展示另走掩码）。
type StickyEntry struct {
	Key      string    `json:"key"`
	LastSeen time.Time `json:"lastSeen"`
}

// stickyCtxKey 显式会话标识的 ctx 键（gateway 从请求头注入；internal 可见）。
type stickyCtxKey struct{}

// WithStickySession 把显式会话标识放进 ctx（gateway 入口调用）。
func WithStickySession(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, stickyCtxKey{}, id)
}

// stickySessionID 取 ctx 里的显式会话标识。
func stickySessionID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(stickyCtxKey{}).(string)
	return strings.TrimSpace(id)
}

// StrField 从 params 取非空字符串字段（与 zen.StrField 同语义，本地最小化）。
func strField(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// ConversationKey 计算会话标识：显式 ID 优先（ctx 头 > body 字段），回退
// 首条 user 消息指纹。返回空串 = 无法识别会话（messages 缺失/无 user 条）。
//
// 指纹 = sha256(apiKey + "\x00" + 首条 user content)，加调用方 API key 做
// namespace 防不同调用方撞指纹。content 为 string 直接用；多模态数组则取
// JSON 序列化（顺序稳定）。
func ConversationKey(ctx context.Context, params map[string]any, apiKey string) string {
	if id := stickySessionID(ctx); id != "" {
		return "id:" + id
	}
	if id := strField(params, "session_id"); id != "" {
		return "id:" + id
	}
	if id := strField(params, "conversation_id"); id != "" {
		return "id:" + id
	}
	msgs, _ := params["messages"].([]any)
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok || mm["role"] != "user" {
			continue
		}
		var content string
		switch c := mm["content"].(type) {
		case string:
			content = c
		default:
			b, _ := json.Marshal(c)
			content = string(b)
		}
		sum := sha256.Sum256([]byte(apiKey + "\x00" + content))
		return "fp:" + hex.EncodeToString(sum[:16])
	}
	return ""
}

// stickyLookup 查映射：命中且未空闲超 TTL 且 key 仍在池内 → 返回 key。
// 过期条目顺手删除；命中则刷新 LastSeen 并写回（值拷贝必须写回，否则刷新无效）。
// 自持 keyMu——调用方不要持锁。
func stickyLookup(convKey string, keys map[string]bool, ttl time.Duration) string {
	if convKey == "" {
		return ""
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	e, ok := usage.StickySessions[convKey]
	if !ok {
		return ""
	}
	if time.Since(e.LastSeen) > ttl || !keys[e.Key] {
		delete(usage.StickySessions, convKey)
		return ""
	}
	e.LastSeen = time.Now()
	usage.StickySessions[convKey] = e
	return e.Key
}

// stickyBind 写映射并刷新 LastSeen；超出上限按 LastSeen 最旧淘汰（LRU）。
// 自持 keyMu——调用方不要持锁。
func stickyBind(convKey, key string) {
	if convKey == "" || key == "" {
		return
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	if usage.StickySessions == nil {
		usage.StickySessions = map[string]StickyEntry{}
	}
	usage.StickySessions[convKey] = StickyEntry{Key: key, LastSeen: time.Now()}
	if len(usage.StickySessions) <= stickyMaxEntries {
		return
	}
	// LRU：按 LastSeen 升序淘汰最旧的若干条。
	type kv struct {
		k string
		t time.Time
	}
	list := make([]kv, 0, len(usage.StickySessions))
	for k, e := range usage.StickySessions {
		list = append(list, kv{k, e.LastSeen})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].t.Before(list[j].t) })
	for _, item := range list[:len(list)-stickyMaxEntries] {
		delete(usage.StickySessions, item.k)
	}
	markTokensDirty() // 合并到既有 flusher（10 秒合批落盘）
}

// stickyForgetKey key 从池中移除时清掉所有指向它的映射（PruneUsage 调用）。
// 调用方必须已持 keyMu。
func stickyForgetKey(valid map[string]bool) {
	for k, e := range usage.StickySessions {
		if !valid[e.Key] {
			delete(usage.StickySessions, k)
		}
	}
}

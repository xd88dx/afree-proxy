// Package toolrename 出站工具改名机制（2026-11-21 立项）。
//
// 背景：免费层身份指纹闸按**工具名**识别代理流量（上游 opencode2api 作者
// 实证：携带 Claude Code 工具名的代理会话被 429 FreeUsageLimitError 拒绝；
// 其自用的 clint_ 前缀走红即被上游拉黑，被迫连换改名方案）。本仓 zen 等平
// 台透传客户端工具名，客户端（ZCode 系）天然携带 Claude Code 核心名——这是
// 在案风险面（现有流量未触发，上游黑名单动态增长）。
//
// 机制：请求侧把客户端工具名改写为 Prefix+SHA256(原名) 前 12 位 hex（确定
// 性映射，任何层可重算），响应侧在出口做**字节级还原**——假名是全网唯一的
// hash token，把响应里出现的 `"<假名>"` 原样替换回 `"<真名>"` 即可，协议无
// 关（chat/responses/anthropic 三形态一份实现）。响应侧的字节替换对内容中
// 偶然出现的假名同样正确（客户端只认识真名）。
//
// 请求侧必须是结构化改写而不能字节替换：请求正文里用户内容可能恰好包含工
// 具名的引号形态，盲目替换会污染对话内容。
//
// 与 NBSP 描述整形（internal/zen）正交：一个管名字面、一个管描述面。
package toolrename

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"afree-proxy/internal/gwconfig"
)

// Mapping 单次请求的改名映射（请求侧改写与响应侧还原共用）。
type Mapping struct {
	Prefix     string
	RealToFake map[string]string
	FakeToReal map[string]string
}

// PlatformKind 平台勾选键（与面板/配置一致）。
const (
	KindCline     = "cline"
	KindZen       = "zen"
	KindTrae      = "trae"
	KindWorkbuddy = "workbuddy"
	KindFreepi    = "freepi"
	KindOther     = "other"
)

// Build 由真名清单构建映射；平台未启用/未开改名/无工具时返回 nil（调用方
// 零改动直通）。platform 用本包 Kind* 常量。
func Build(platform string, toolNames []string) *Mapping {
	cfg := gwconfig.GetToolRename()
	// 无独立开关：平台有勾选且前缀合法即生效（2026-11-21 用户定）。
	if !cfg.Platforms[platform] || cfg.Prefix == "" || len(toolNames) == 0 {
		return nil
	}
	m := &Mapping{Prefix: cfg.Prefix, RealToFake: map[string]string{}, FakeToReal: map[string]string{}}
	for _, n := range toolNames {
		if n == "" {
			continue
		}
		if _, dup := m.RealToFake[n]; dup {
			continue
		}
		f := FakeOf(n, cfg.Prefix)
		m.RealToFake[n] = f
		m.FakeToReal[f] = n
	}
	if len(m.RealToFake) == 0 {
		return nil
	}
	return m
}

// FakeOf 确定性假名：prefix + "_" + SHA256(原名) 前 12 位 hex。确定性保证
// 任何层可独立重算，映射无需跨层传递（还原侧用 FakeToReal 反查）。
func FakeOf(real, prefix string) string {
	sum := sha256.Sum256([]byte(real))
	return prefix + "_" + hex.EncodeToString(sum[:])[:12]
}

// Rename 把真名换成假名；映射外名字原样返回（服务器幻觉名/未注册工具兜底）。
func (m *Mapping) Rename(real string) string {
	if m == nil {
		return real
	}
	if f, ok := m.RealToFake[real]; ok {
		return f
	}
	return real
}

// Restore 把假名换回真名；映射外名字原样返回。
func (m *Mapping) Restore(fake string) string {
	if m == nil {
		return fake
	}
	if r, ok := m.FakeToReal[fake]; ok {
		return r
	}
	return fake
}

// RewriteChatForm 结构化改写 chat completions 形态的工具名（tools、messages
// 里的 assistant tool_calls、tool_choice）。复制纪律：受影响容器整体替换为
// 新副本，其余字段原样共享——调用方 params 不得被就地污染（端点自适应重试、
// 日志、token 估算随后还要读原形态，与 zen env-marker 同款纪律）。
func RewriteChatForm(obj map[string]any, m *Mapping) {
	if obj == nil || m == nil || len(m.RealToFake) == 0 {
		return
	}
	// tools[].function.name（兼容裸 name 形态与已废弃 functions）。
	if tools, ok := obj["tools"].([]any); ok {
		changed := false
		out := make([]any, len(tools))
		copy(out, tools)
		for i, t := range tools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			var fn map[string]any
			name := ""
			if f, ok := tm["function"].(map[string]any); ok {
				fn = f
				name, _ = fn["name"].(string)
			} else if n, ok := tm["name"].(string); ok {
				name = n
			}
			if name == "" || m.RealToFake[name] == "" {
				continue
			}
			nm := make(map[string]any, len(tm))
			for k, v := range tm {
				nm[k] = v
			}
			if fn != nil {
				nf := make(map[string]any, len(fn))
				for k, v := range fn {
					nf[k] = v
				}
				nf["name"] = m.Rename(name)
				nm["function"] = nf
			} else {
				nm["name"] = m.Rename(name)
			}
			out[i] = nm
			changed = true
		}
		if changed {
			obj["tools"] = out
		}
	}
	// messages[].tool_calls[].function.name（assistant 历史）。
	if msgs, ok := obj["messages"].([]any); ok {
		changed := false
		out := make([]any, len(msgs))
		copy(out, msgs)
		for i, mm := range msgs {
			mm, ok := mm.(map[string]any)
			if !ok {
				continue
			}
			tcs, ok := mm["tool_calls"].([]any)
			if !ok {
				continue
			}
			tcOut := make([]any, len(tcs))
			copy(tcOut, tcs)
			msgChanged := false
			for j, tc := range tcs {
				tcm, ok := tc.(map[string]any)
				if !ok {
					continue
				}
				fn, ok := tcm["function"].(map[string]any)
				if !ok {
					continue
				}
				name, _ := fn["name"].(string)
				if name == "" || m.RealToFake[name] == "" {
					continue
				}
				nf := make(map[string]any, len(fn))
				for k, v := range fn {
					nf[k] = v
				}
				nf["name"] = m.Rename(name)
				ntc := make(map[string]any, len(tcm))
				for k, v := range tcm {
					ntc[k] = v
				}
				ntc["function"] = nf
				tcOut[j] = ntc
				msgChanged = true
			}
			if msgChanged {
				nm := make(map[string]any, len(mm))
				for k, v := range mm {
					nm[k] = v
				}
				nm["tool_calls"] = tcOut
				out[i] = nm
				changed = true
			}
		}
		if changed {
			obj["messages"] = out
		}
	}
	// tool_choice 对象形态的指定函数名（auto/none/required 原样）。
	if tc, ok := obj["tool_choice"].(map[string]any); ok {
		if fn, ok := tc["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok && m.RealToFake[name] != "" {
				tc["function"] = map[string]any{"name": m.Rename(name)}
			}
		}
	}
}

// RewriteResponsesForm 结构化改写 responses 形态的工具名（tools[].name、
// input[] 的 function_call 项）。zen 原生 responses 出站 body 用。
func RewriteResponsesForm(obj map[string]any, m *Mapping) {
	if obj == nil || m == nil || len(m.RealToFake) == 0 {
		return
	}
	if tools, ok := obj["tools"].([]any); ok {
		changed := false
		out := make([]any, len(tools))
		copy(out, tools)
		for i, t := range tools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			name, _ := tm["name"].(string)
			if name == "" || m.RealToFake[name] == "" {
				continue
			}
			nm := make(map[string]any, len(tm))
			for k, v := range tm {
				nm[k] = v
			}
			nm["name"] = m.Rename(name)
			out[i] = nm
			changed = true
		}
		if changed {
			obj["tools"] = out
		}
	}
	if input, ok := obj["input"].([]any); ok {
		changed := false
		out := make([]any, len(input))
		copy(out, input)
		for i, it := range input {
			im, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if im["type"] != "function_call" {
				continue
			}
			name, _ := im["name"].(string)
			if name == "" || m.RealToFake[name] == "" {
				continue
			}
			nm := make(map[string]any, len(im))
			for k, v := range im {
				nm[k] = v
			}
			nm["name"] = m.Rename(name)
			out[i] = nm
			changed = true
		}
		if changed {
			obj["input"] = out
		}
	}
}

// NewRestoreWriter 字节级还原出口：把响应里出现的 `"<假名>"` 原样替换回
// `"<真名>"`。带引号匹配（假名 token 全网唯一，带引号后误伤面为零）；跨
// Write 边界的部分 token 用 pending 缓冲衔接。协议无关——chat/responses/
// anthropic 三形态、流式与非流式一份实现。
func NewRestoreWriter(w http.ResponseWriter, m *Mapping) http.ResponseWriter {
	if m == nil || len(m.FakeToReal) == 0 {
		return w
	}
	maxLen := 0
	for f := range m.FakeToReal {
		if len(f)+2 > maxLen {
			maxLen = len(f) + 2
		}
	}
	return &restoreWriter{ResponseWriter: w, m: m, maxTok: maxLen}
}

type restoreWriter struct {
	http.ResponseWriter
	m       *Mapping
	maxTok  int
	pending []byte
}

// tokenPrefixAt 判断 buf 末尾是否悬着某个 `"假名"` 的部分前缀；返回应扣留的字节数。
func (r *restoreWriter) holdTail(buf []byte) int {
	max := r.maxTok
	if len(buf) < max {
		max = len(buf)
	}
	for k := max; k > 0; k-- {
		suf := buf[len(buf)-k:]
		if suf[0] != '"' {
			continue
		}
		for fake := range r.m.FakeToReal {
			full := `"` + fake + `"`
			if strings.HasPrefix(full, string(suf)) && len(suf) < len(full) {
				return k
			}
		}
	}
	return 0
}

func (r *restoreWriter) Write(p []byte) (int, error) {
	buf := append(r.pending, p...)
	var out []byte
	for {
		best := -1
		bestFake := ""
		for fake := range r.m.FakeToReal {
			idx := bytes.Index(buf, []byte(`"`+fake+`"`))
			if idx >= 0 && (best < 0 || idx < best) {
				best = idx
				bestFake = fake
			}
		}
		if best < 0 {
			break
		}
		out = append(out, buf[:best]...)
		out = append(out, []byte(`"`+r.m.FakeToReal[bestFake]+`"`)...)
		buf = buf[best+len(bestFake)+2:]
	}
	if hold := r.holdTail(buf); hold > 0 {
		out = append(out, buf[:len(buf)-hold]...)
		r.pending = append([]byte(nil), buf[len(buf)-hold:]...)
	} else {
		out = append(out, buf...)
		r.pending = r.pending[:0]
	}
	if len(out) > 0 {
		if _, err := r.ResponseWriter.Write(out); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// Flush 补齐 http.Flusher：先冲出扣留字节再透传（SSE 逐帧节奏不被破坏；
// 扣留字节只可能是完整帧内的部分 token，冲出它意味着放弃该 token 的还原
// ——只在病态分片时发生，正常 SSE 逐帧 Write 不撕开 token）。
func (r *restoreWriter) Flush() {
	if len(r.pending) > 0 {
		p := r.pending
		r.pending = nil
		_, _ = r.ResponseWriter.Write(p)
	}
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var _ io.Writer = (*restoreWriter)(nil)

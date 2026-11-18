// admin_tasks.go 手动补跑排程。POST /admin/tasks/{name}/run，立刻 202，后台执行。
// 本面板的排程是 checkin/activity/keepalive/travel/growth。
package server

import (
	"net/http"
	"strings"
)

// TaskRunner 各 Run*Now 里本面板实际有的五个。*scheduler.Scheduler 满足它。
type TaskRunner interface {
	RunCheckinNow()
	RunActivityNow()
	RunKeepaliveNow()
	RunTravelNow()
	RunGrowthNow()
}

var adminTaskNames = []string{"checkin", "activity", "keepalive", "travel", "growth"}

func (h *Handler) taskRunners() map[string]func() {
	t := h.cfg.Tasks
	if t == nil {
		return nil
	}
	return map[string]func(){
		"checkin":   t.RunCheckinNow,
		"activity":  t.RunActivityNow,
		"keepalive": t.RunKeepaliveNow,
		"travel":    t.RunTravelNow,
		"growth":    t.RunGrowthNow,
	}
}

// OnlyRouter 维护任务开关钩子（afree 本地补丁，上游原样 nil）：宿主注入
// kit.OnlyRouter 的等价判定（ONLY_ROUTER=true 时仅会话路由，后台调度与面板
// 「立即执行」的手动触发一并拒绝）。nil = 不限制——vendor 树不依赖宿主 kit
// 包（P0 卫生修复，2026-11-17），与门面 SetServerOnlyRouter 配对。
var OnlyRouter func() bool

func (h *Handler) adminTaskRun(w http.ResponseWriter, r *http.Request) {
	// ONLY_ROUTER=true（2026-10-28 用户定）：仅会话路由——后台调度已禁用，
	// 面板「立即执行」的手动触发同样拒绝，否则备用实例仍能手动打上游。
	if OnlyRouter != nil && OnlyRouter() {
		writeOpenAIError(w, http.StatusServiceUnavailable, "unavailable",
			"maintenance tasks are disabled by ONLY_ROUTER (session routing only)")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	runners := h.taskRunners()
	if runners == nil {
		writeOpenAIError(w, http.StatusServiceUnavailable, "unavailable", "task runner not wired")
		return
	}
	run, ok := runners[name]
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "not_found",
			"unknown task: "+name+"; known: "+strings.Join(adminTaskNames, ", "))
		return
	}
	h.taskMu.Lock()
	if h.taskRunning[name] {
		h.taskMu.Unlock()
		writeOpenAIError(w, http.StatusConflict, "busy", "task already running: "+name)
		return
	}
	if h.taskRunning == nil {
		h.taskRunning = map[string]bool{}
	}
	h.taskRunning[name] = true
	h.taskMu.Unlock()

	go func() {
		defer func() {
			h.taskMu.Lock()
			delete(h.taskRunning, name)
			h.taskMu.Unlock()
		}()
		run()
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"task": name, "status": "started"})
}

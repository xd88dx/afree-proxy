package freepi

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// 设备码流程端到端（httptest 上游）：start（device+consent）→ poll 首步遇
// consent_required 竞态（补打 consent 后同调用内重试成功）→ pending → done。
func TestDeviceFlowStartAndPoll(t *testing.T) {
	var consents, polls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/github/device", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"session_id":"sid-1","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","verification_uri_complete":"https://github.com/login/device?user_code=ABCD-1234","interval":5,"expires_in":899}`))
	})
	mux.HandleFunc("/auth/consent", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&consents, 1)
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, _ *http.Request) {
		switch n := atomic.AddInt32(&polls, 1); n {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"code":"consent_required","message":"consent required before sign-in"}`))
		case 2:
			w.Write([]byte(`{"status":"pending","retry_after":10}`))
		default:
			w.Write([]byte(`{"token":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.sig"}`))
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	old := deviceFlowBase
	deviceFlowBase = srv.URL
	defer func() { deviceFlowBase = old }()

	sess, err := DeviceStart(srv.Client())
	if err != nil {
		t.Fatalf("DeviceStart: %v", err)
	}
	if sess.SessionID != "sid-1" || sess.UserCode != "ABCD-1234" {
		t.Fatalf("unexpected session: %+v", sess)
	}
	if sess.URL != "https://github.com/login/device?user_code=ABCD-1234" {
		t.Fatalf("url should prefer verification_uri_complete: %q", sess.URL)
	}
	if got := atomic.LoadInt32(&consents); got != 1 {
		t.Fatalf("consent should fire once in start, got %d", got)
	}

	p1, err := DevicePoll(srv.Client(), sess.SessionID)
	if err != nil {
		t.Fatalf("DevicePoll#1: %v", err)
	}
	if p1.Status != "pending" {
		t.Fatalf("poll#1 want pending, got %+v", p1)
	}
	if got := atomic.LoadInt32(&consents); got != 2 {
		t.Fatalf("consent_required path should re-fire consent once, got %d", got)
	}
	p2, err := DevicePoll(srv.Client(), sess.SessionID)
	if err != nil {
		t.Fatalf("DevicePoll#2: %v", err)
	}
	if p2.Status != "done" || p2.Token == "" {
		t.Fatalf("poll#2 want done with token, got %+v", p2)
	}
}

// 非 403/429 的 4xx 判死（failed + 上游原文），面板据此停止轮询。
func TestDevicePollFatalOn4xx(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":"bad_request","message":"session expired"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	old := deviceFlowBase
	deviceFlowBase = srv.URL
	defer func() { deviceFlowBase = old }()

	res, err := DevicePoll(srv.Client(), "sid-x")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Status != "failed" || res.Message == "" {
		t.Fatalf("want failed with message, got %+v", res)
	}
}

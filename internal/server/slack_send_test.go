package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Non-POST methods are rejected.
func TestHandleSlackSendMethodGuard(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleSlackSend(rec, httptest.NewRequest(http.MethodGet, "/api/slack/send", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// Malformed JSON -> 400.
func TestHandleSlackSendBadPayload(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/slack/send", strings.NewReader("{not json"))
	s.handleSlackSend(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// Missing channel/text -> 400 (validated before SendAsBot).
func TestHandleSlackSendMissingFields(t *testing.T) {
	s := &Server{}
	for _, body := range []string{`{"text":"hi"}`, `{"channel":"D1"}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/slack/send", strings.NewReader(body))
		s.handleSlackSend(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want %d", body, rec.Code, http.StatusBadRequest)
		}
	}
}

// With FLOW_SLACK_WRITES_ENABLED unset, monitor.SendAsBot returns "writes
// disabled" -> handler decodes the request and returns 502. Stubless: exercises
// the real decode + method guard + SendAsBot error -> 502 path.
func TestHandleSlackSendWritesDisabled(t *testing.T) {
	t.Setenv("FLOW_SLACK_WRITES_ENABLED", "")
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/slack/send",
		strings.NewReader(`{"channel":"D1","text":"hi"}`))
	s.handleSlackSend(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want %d (502); body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "writes disabled") {
		t.Errorf("body = %q, want it to surface the SendAsBot error", rec.Body.String())
	}
}

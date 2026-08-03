package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ernilambar/uplet/internal/checker"
)

func newTestServer() *Server {
	s := New()
	s.MaxConcurrent = 4
	return s
}

func TestHandleCheck_WrongMethod(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/v1/check", nil)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleCheck_MissingContentType(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"http://127.0.0.1:1"}`))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleCheck_WrongContentType(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"http://127.0.0.1:1"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
}

func TestHandleCheck_MalformedJSON(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if body["error"] != "missing_url" {
		t.Fatalf("error = %q, want %q", body["error"], "missing_url")
	}
}

func TestHandleCheck_MissingURLField(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// A loopback target is rejected by the SSRF blocklist without any real
// network connection, so this exercises the full pipeline (content-type
// enforcement, bounded JSON decode, semaphore, checker.Check, JSON
// response) deterministically and without a live network dependency.
func TestHandleCheck_ValidRequestReturnsCheckerResult(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"http://127.0.0.1:1/"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if acao := rec.Header().Get("Access-Control-Allow-Origin"); acao != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want unset (no permissive CORS)", acao)
	}

	var res checker.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("body is not valid JSON: %v\n%s", err, rec.Body.String())
	}
	if res.ReasonCode != string(checker.ReasonBlockedTarget) {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestHandleCheck_ConcurrencyCapReturns429(t *testing.T) {
	s := newTestServer()
	s.MaxConcurrent = 1
	_ = s.Handler() // initializes s.sem from MaxConcurrent

	s.sem <- struct{}{} // occupy the only slot
	defer func() { <-s.sem }()

	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"http://127.0.0.1:1/"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestHandleCheck_OversizedBody(t *testing.T) {
	s := newTestServer()
	s.MaxBodyBytes = 16

	body := `{"url":"http://` + strings.Repeat("a", 100) + `.example/"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ernilambar/uplet/internal/checker"
)

func newTestServer() *Server {
	s := New()
	s.MaxConcurrent = 4
	return s
}

func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not valid JSON: %v\n%s", err, rec.Body.String())
	}
	if body["error"] != wantCode {
		t.Errorf("error = %q, want %q", body["error"], wantCode)
	}
}

func TestHandleCheck_WrongMethod(t *testing.T) {
	t.Parallel()
	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPut,
		http.MethodOptions,
		http.MethodDelete,
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			s := newTestServer()
			req := httptest.NewRequest(method, "/v1/check", nil)
			rec := httptest.NewRecorder()

			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
			}
			if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
				t.Errorf("Allow = %q, want %q", allow, http.MethodPost)
			}
			assertErrorBody(t, rec, "method_not_allowed")
		})
	}
}

func TestHandleCheck_MissingContentType(t *testing.T) {
	t.Parallel()
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"http://127.0.0.1:1"}`))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
	assertErrorBody(t, rec, "unsupported_media_type")
}

func TestHandleCheck_WrongContentType(t *testing.T) {
	t.Parallel()
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"http://127.0.0.1:1"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
	assertErrorBody(t, rec, "unsupported_media_type")
}

func TestHandleCheck_ContentTypeWithCharsetIsAccepted(t *testing.T) {
	t.Parallel()
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"http://127.0.0.1:1/"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleCheck_MalformedJSON(t *testing.T) {
	t.Parallel()
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorBody(t, rec, "missing_url")
}

func TestHandleCheck_MissingURLField(t *testing.T) {
	t.Parallel()
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	assertErrorBody(t, rec, "missing_url")
}

func TestHandleCheck_UnknownRoute(t *testing.T) {
	t.Parallel()
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/nope", strings.NewReader(`{"url":"http://127.0.0.1:1/"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// A loopback target is rejected by the SSRF blocklist without any real
// network connection, so this exercises the full pipeline (content-type
// enforcement, bounded JSON decode, semaphore, checker.Check, JSON
// response) deterministically and without a live network dependency.
func TestHandleCheck_ValidRequestReturnsCheckerResult(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	assertErrorBody(t, rec, "too_many_requests")
}

// TestHandleCheck_SemaphoreReleasedAfterSuccess guards against a slot leak:
// with a single-slot semaphore, a second sequential request must succeed
// (proving the first request released its slot) rather than 429.
func TestHandleCheck_SemaphoreReleasedAfterSuccess(t *testing.T) {
	t.Parallel()
	s := newTestServer()
	s.MaxConcurrent = 1
	h := s.Handler()

	do := func() int {
		req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"url":"http://127.0.0.1:1/"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do(); code != http.StatusOK {
		t.Fatalf("first request = %d, want %d", code, http.StatusOK)
	}
	if code := do(); code != http.StatusOK {
		t.Fatalf("second request = %d, want %d (semaphore slot leaked)", code, http.StatusOK)
	}
}

func TestHandleCheck_OversizedBody(t *testing.T) {
	t.Parallel()
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

// TestLogMiddlewareRecordsStatus verifies the status-capturing response
// writer is wired in: a request that writes a non-2xx status must be
// logged with that status, not the 200 default.
func TestLogMiddlewareRecordsStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	s := New()
	s.MaxConcurrent = 4
	s.Logger = log.New(&buf, "", 0)
	h := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/check", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	logged := buf.String()
	if !strings.Contains(logged, "GET") || !strings.Contains(logged, "/v1/check") || !strings.Contains(logged, "405") {
		t.Fatalf("log line = %q, want it to contain method, path, and status 405", logged)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestServerRun_BindsLoopbackAndShutsDownGracefully exercises the real
// server lifecycle: it must accept loopback connections and return from
// Run once its context is canceled.
func TestServerRun_BindsLoopbackAndShutsDownGracefully(t *testing.T) {
	t.Parallel()
	port := freePort(t)

	s := New()
	s.Port = port
	s.Logger = log.New(io.Discard, "", 0)

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx) }()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(5 * time.Second)
	up := false
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/v1/check") // GET → 405, but proves it serves
		if err == nil {
			resp.Body.Close()
			up = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !up {
		cancel()
		t.Fatal("server never started listening on loopback")
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Run returned %v, want nil after graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

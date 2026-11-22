package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ernilambar/uplet/internal/checker"
)

// DefaultPort is the canonical port uplet serve binds to when --port is
// not given. Use 127.0.0.1:DefaultPort verbatim in docs and clients —
// permission grants (e.g. a browser extension's host_permissions) match
// on the literal host string, not the resolved IP.
const DefaultPort = 54321

const (
	defaultMaxBodyBytes  = 4 * 1024
	defaultMaxConcurrent = 32
	shutdownTimeout      = 5 * time.Second
)

// Server exposes checker.Check over a loopback-only HTTP API.
type Server struct {
	Port          int
	MaxConcurrent int
	MaxBodyBytes  int64
	Logger        *log.Logger

	checker *checker.Checker
	sem     chan struct{}
}

// New returns a Server configured with sane defaults, ready to Run.
func New() *Server {
	return &Server{
		Port:          DefaultPort,
		MaxConcurrent: defaultMaxConcurrent,
		MaxBodyBytes:  defaultMaxBodyBytes,
		Logger:        log.New(os.Stderr, "", log.LstdFlags),
		checker:       checker.New(),
	}
}

// Handler returns the server's http.Handler, wired with request logging.
// Safe to call once; the concurrency semaphore is sized from
// MaxConcurrent at this point.
func (s *Server) Handler() http.Handler {
	if s.sem == nil {
		s.sem = make(chan struct{}, s.MaxConcurrent)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/check", s.handleCheck)

	return s.logMiddleware(requireLoopbackHost(mux))
}

// requireLoopbackHost rejects requests whose Host header isn't 127.0.0.1.
// Binding to loopback alone doesn't stop DNS rebinding: a page on
// attacker.example can re-resolve its own name to 127.0.0.1, become
// same-origin with this server, and read responses. The browser still
// sends the attacker's hostname in Host, so checking it closes the hole.
func requireLoopbackHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" {
			writeError(w, http.StatusForbidden, "forbidden_host")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Run binds to 127.0.0.1:Port — never 0.0.0.0, never the hostname
// "localhost" — and serves until ctx is canceled or SIGINT/SIGTERM is
// received, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		s.Logger.Printf("listening on %s", addr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		s.Logger.Printf("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.Logger.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(status int) {
	sw.status = status
	sw.ResponseWriter.WriteHeader(status)
}

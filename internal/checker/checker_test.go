package checker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newAllowLoopbackChecker returns a Checker whose blocklist allows
// loopback, so tests can point it at an httptest.Server (which listens on
// 127.0.0.1) while still exercising the real HTTP-handling logic.
func newAllowLoopbackChecker() *Checker {
	c := New()
	c.blocked = func(net.IP) bool { return false }
	return c
}

func TestCheck_InvalidFormat(t *testing.T) {
	t.Parallel()
	for _, u := range []string{"not a url", "ftp://example.com", "http://", "example.com", ""} {
		t.Run(u, func(t *testing.T) {
			t.Parallel()
			c := newAllowLoopbackChecker()
			res := c.Check(context.Background(), u)
			if res.ValidFormat {
				t.Errorf("Check(%q).ValidFormat = true, want false", u)
			}
			if res.ReasonCode != string(ReasonInvalidFormat) {
				t.Errorf("Check(%q).ReasonCode = %q, want %q", u, res.ReasonCode, ReasonInvalidFormat)
			}
		})
	}
}

func TestCheck_200(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonOK) || !res.SiteUp || !res.PageExists || res.StatusCode != 200 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_404(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonNotFound) || !res.SiteUp || res.PageExists {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_500(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonServerError) || !res.SiteUp || res.PageExists {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_HeadNotAllowedFallsBackToGet(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonOK) {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2 (HEAD + GET fallback)", res.Attempts)
	}
}

func TestCheck_TransparentTrailingSlashRedirect(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/foo" {
			http.Redirect(w, r, "/foo/", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL+"/foo")
	if res.ReasonCode != string(ReasonOK) || !res.PageExists || res.RedirectedTo != "" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_SoftNotFoundRedirectToRoot(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing-page" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL+"/missing-page")
	if res.ReasonCode != string(ReasonRedirectNotFound) || res.PageExists {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.RedirectedTo != ts.URL+"/" {
		t.Fatalf("RedirectedTo = %q, want %q", res.RedirectedTo, ts.URL+"/")
	}
}

func TestCheck_ContentBasedSoftNotFound(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>404 - Page Not Found</body></html>"))
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL+"/anything")
	if res.ReasonCode != string(ReasonNotFound) || res.PageExists {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_FlakyThenSuccess(t *testing.T) {
	t.Parallel()
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ts.Listener = &flakyListener{Listener: ts.Listener, failCount: 1}
	ts.Start()
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonOK) {
		t.Fatalf("unexpected result: %+v", res)
	}
	// HEAD retries once (2 attempts) then the 2xx status triggers a
	// follow-up GET for body inspection (1 more attempt).
	if res.Attempts != 3 {
		t.Fatalf("Attempts = %d, want 3", res.Attempts)
	}
}

func TestCheck_ExhaustedRetries(t *testing.T) {
	t.Parallel()
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ts.Listener = &flakyListener{Listener: ts.Listener, failCount: 99}
	ts.Start()
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL)
	if res.SiteUp {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Attempts != 2 {
		t.Fatalf("Attempts = %d, want 2", res.Attempts)
	}
}

func TestCheck_DNSNotFound(t *testing.T) {
	t.Parallel()
	c := newAllowLoopbackChecker()
	c.resolver = &fakeResolver{err: &net.DNSError{IsNotFound: true, Name: "nope.invalid"}}

	res := c.Check(context.Background(), "http://nope.invalid/")
	if res.ReasonCode != string(ReasonDNSError) {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_DNSTimeout(t *testing.T) {
	t.Parallel()
	c := newAllowLoopbackChecker()
	c.resolver = &fakeResolver{err: &net.DNSError{IsTimeout: true, Name: "slow.invalid"}}

	res := c.Check(context.Background(), "http://slow.invalid/")
	if res.ReasonCode != string(ReasonTimeout) {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_Timeout(t *testing.T) {
	t.Parallel()
	// The handler blocks until the request context is canceled, so the
	// test depends on the client timeout firing rather than racing a
	// fixed sleep against a fixed deadline.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()

	c := newAllowLoopbackChecker()
	c.Timeout = 50 * time.Millisecond
	res := c.Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonTimeout) {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_ClientError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonClientError) {
		t.Fatalf("ReasonCode = %q, want %q", res.ReasonCode, ReasonClientError)
	}
	if !res.SiteUp || res.PageExists {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("StatusCode = %d, want %d", res.StatusCode, http.StatusForbidden)
	}
}

func TestCheck_NetworkError(t *testing.T) {
	t.Parallel()
	// Start then immediately close a server so the address is refused.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := ts.URL
	ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), deadURL)
	if res.ReasonCode != string(ReasonNetworkError) {
		t.Fatalf("ReasonCode = %q, want %q (result %+v)", res.ReasonCode, ReasonNetworkError, res)
	}
	if res.SiteUp {
		t.Fatalf("SiteUp = true, want false; result %+v", res)
	}
	// A refused connection is definitive, so it must not be retried.
	if res.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1 (connection refused is not transient)", res.Attempts)
	}
}

func TestCheck_MaxRedirects(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer ts.Close()

	c := newAllowLoopbackChecker()
	c.MaxRedirects = 2
	res := c.Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonNetworkError) {
		t.Fatalf("ReasonCode = %q, want %q (result %+v)", res.ReasonCode, ReasonNetworkError, res)
	}
	if !strings.Contains(res.Reason, "stopped after 2 redirects") {
		t.Fatalf("Reason = %q, want it to mention the redirect limit", res.Reason)
	}
}

func TestCheck_TooLarge(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.Repeat("a", 200)))
	}))
	defer ts.Close()

	c := newAllowLoopbackChecker()
	c.MaxBodyBytes = 10
	res := c.Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonTooLarge) {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestCheck_TLSError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	res := newAllowLoopbackChecker().Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonTLSError) || res.SiteUp {
		t.Fatalf("unexpected result: %+v", res)
	}
}

// SSRF tests below use the real production blocklist.

func TestIsBlockedIP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{"ipv4 loopback", "127.0.0.1", true},
		{"ipv6 loopback", "::1", true},
		{"rfc1918 10/8", "10.1.2.3", true},
		{"rfc1918 172.16/12", "172.16.5.9", true},
		{"rfc1918 192.168/16", "192.168.1.1", true},
		{"cloud metadata", "169.254.169.254", true},
		{"ipv6 link-local", "fe80::1", true},
		{"ipv6 metadata", "fd00:ec2::254", true},
		{"unspecified", "0.0.0.0", true},
		{"broadcast", "255.255.255.255", true},
		{"ipv4-mapped loopback", "::ffff:127.0.0.1", true},
		{"public dns", "8.8.8.8", false},
		{"public web", "93.184.216.34", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ip := net.ParseIP(c.ip)
			if got := isBlockedIP(ip); got != c.want {
				t.Errorf("isBlockedIP(%s) = %v, want %v", c.ip, got, c.want)
			}
		})
	}

	// A nil IP must not panic and must not be treated as blocked.
	if isBlockedIP(nil) {
		t.Error("isBlockedIP(nil) = true, want false")
	}
}

func TestCheck_RedirectToPrivateIPBlocked(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer ts.Close()

	c := New()
	c.blocked = func(ip net.IP) bool {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			return false // let the test server's own loopback address through
		}
		return isBlockedIP(ip)
	}

	res := c.Check(context.Background(), ts.URL)
	if res.ReasonCode != string(ReasonBlockedTarget) || res.SiteUp {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestSafeDialContext_MixedResolutionRejected(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{ips: [][]net.IPAddr{
		{{IP: net.ParseIP("203.0.113.10")}, {IP: net.ParseIP("10.0.0.5")}},
	}}
	dial := newSafeDialContext(fr, isBlockedIP, func(ctx context.Context, network, addr string) (net.Conn, error) {
		t.Fatalf("dial should not be reached when any resolved IP is blocked")
		return nil, nil
	})

	_, err := dial(context.Background(), "tcp", "victim.example:80")
	if !errors.Is(err, errBlockedTarget) {
		t.Fatalf("err = %v, want errBlockedTarget", err)
	}
}

func TestSafeDialContext_ResolvesOnceAndDialsValidatedIP(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{ips: [][]net.IPAddr{{{IP: net.ParseIP("203.0.113.10")}}}}
	var gotAddr string
	fakeDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		gotAddr = addr
		return nil, errors.New("stop")
	}

	dial := newSafeDialContext(fr, isBlockedIP, fakeDial)
	_, err := dial(context.Background(), "tcp", "example.com:443")
	if err == nil || err.Error() != "stop" {
		t.Fatalf("err = %v, want sentinel from fakeDial", err)
	}
	if fr.calls != 1 {
		t.Fatalf("resolver called %d times, want exactly 1 (no re-resolution)", fr.calls)
	}
	if gotAddr != "203.0.113.10:443" {
		t.Fatalf("dialed %q, want the validated resolved IP", gotAddr)
	}
}

// fakeResolver is a deterministic hostResolver for tests.
type fakeResolver struct {
	mu    sync.Mutex
	calls int
	ips   [][]net.IPAddr
	err   error
}

func (f *fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	i := f.calls - 1
	if i >= len(f.ips) {
		i = len(f.ips) - 1
	}
	return f.ips[i], nil
}

// flakyListener closes the first failCount accepted connections
// immediately (simulating a transient connection reset) before behaving
// normally, so tests can exercise the retry path deterministically.
type flakyListener struct {
	net.Listener
	mu        sync.Mutex
	failCount int
	failed    int
}

func (l *flakyListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	shouldFail := l.failed < l.failCount
	if shouldFail {
		l.failed++
	}
	l.mu.Unlock()

	if shouldFail {
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetLinger(0)
		}
		conn.Close()
		return l.Accept()
	}
	return conn, nil
}

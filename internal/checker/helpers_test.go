package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestValidateFormat(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name     string
		raw      string
		wantHost string
	}{
		{"simple", "http://example.com", "example.com"},
		{"https with query and fragment", "https://example.com/path?q=1#frag", "example.com"},
		{"uppercase scheme is normalized", "HTTP://EXAMPLE.COM", "EXAMPLE.COM"},
		{"explicit port", "http://example.com:8080/", "example.com:8080"},
		{"percent encoded path", "http://example.com/%20", "example.com"},
	}
	for _, c := range valid {
		t.Run("valid/"+c.name, func(t *testing.T) {
			t.Parallel()
			u, err := validateFormat(c.raw)
			if err != nil {
				t.Fatalf("validateFormat(%q) = %v, want nil", c.raw, err)
			}
			if u.Host != c.wantHost {
				t.Fatalf("host = %q, want %q", u.Host, c.wantHost)
			}
		})
	}

	invalid := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"no scheme", "example.com"},
		{"unsupported scheme", "ftp://example.com"},
		{"missing host", "http://"},
		{"scheme only", "http:///path"},
		{"spaces", "not a url"},
		{"fragment only", "#fragment"},
		{"space in host", "http://exa mple.com"},
	}
	for _, c := range invalid {
		t.Run("invalid/"+c.name, func(t *testing.T) {
			t.Parallel()
			if u, err := validateFormat(c.raw); err == nil {
				t.Fatalf("validateFormat(%q) = %v, want error", c.raw, u)
			}
		})
	}
}

func TestNormalizeIP(t *testing.T) {
	t.Parallel()

	mapped := normalizeIP(net.ParseIP("::ffff:127.0.0.1"))
	if !mapped.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("normalizeIP(::ffff:127.0.0.1) = %v, want 127.0.0.1", mapped)
	}
	if len(mapped) != net.IPv4len {
		t.Fatalf("normalizeIP returned %d-byte address, want 4", len(mapped))
	}

	v6 := normalizeIP(net.ParseIP("::1"))
	if len(v6) != net.IPv6len {
		t.Fatalf("normalizeIP(::1) returned %d-byte address, want 16", len(v6))
	}
}

func TestIsTimeoutErr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"context deadline", context.DeadlineExceeded, true},
		{"dns not found", &net.DNSError{IsNotFound: true}, false},
		{"generic", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isTimeoutErr(c.err); got != c.want {
				t.Errorf("isTimeoutErr(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestIsDNSNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"not found", &net.DNSError{IsNotFound: true}, true},
		{"wrapped not found", fmt.Errorf("resolve: %w", &net.DNSError{IsNotFound: true}), true},
		{"timeout is not not-found", &net.DNSError{IsTimeout: true}, false},
		{"generic", errors.New("boom"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isDNSNotFound(c.err); got != c.want {
				t.Errorf("isDNSNotFound(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestIsDNSTimeout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"timeout", &net.DNSError{IsTimeout: true}, true},
		{"wrapped timeout", fmt.Errorf("resolve: %w", &net.DNSError{IsTimeout: true}), true},
		{"not found is not timeout", &net.DNSError{IsNotFound: true}, false},
		{"generic", errors.New("boom"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isDNSTimeout(c.err); got != c.want {
				t.Errorf("isDNSTimeout(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestIsTLSCertError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"expired", x509.CertificateInvalidError{Reason: x509.Expired}, true},
		{"unknown authority", x509.UnknownAuthorityError{}, true},
		{"hostname mismatch", x509.HostnameError{}, true},
		{"verification error", &tls.CertificateVerificationError{}, true},
		{"generic", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isTLSCertError(c.err); got != c.want {
				t.Errorf("isTLSCertError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestIsTransientErr(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"blocked target", errBlockedTarget, false},
		{"dns error", &net.DNSError{IsNotFound: true}, false},
		{"tls cert error", x509.UnknownAuthorityError{}, false},
		{"connection refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, false},
		{"connection reset", &net.OpError{Op: "dial", Err: errors.New("connection reset by peer")}, true},
		{"generic", errors.New("boom"), true},
		{"timeout", context.DeadlineExceeded, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := isTransientErr(c.err); got != c.want {
				t.Errorf("isTransientErr(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestRedirectIsSoftNotFound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		original string
		final    string
		want     bool
	}{
		{"host change", "http://a.example/x", "http://b.example/x", true},
		{"www added", "http://www.a.example/x", "http://a.example/x", false},
		{"www dropped", "http://a.example/x", "http://www.a.example/x", false},
		{"trailing slash added", "http://a.example/foo", "http://a.example/foo/", false},
		{"identical", "http://a.example/foo", "http://a.example/foo", false},
		{"missing path to root", "http://a.example/missing", "http://a.example/", true},
		{"root to landing page", "http://a.example/", "http://a.example/landing", false},
		{"locale prefix", "http://a.example/en/products", "http://a.example/products", false},
		{"different path", "http://a.example/a", "http://a.example/b", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			orig, err := url.Parse(c.original)
			if err != nil {
				t.Fatalf("bad original URL %q: %v", c.original, err)
			}
			final, err := url.Parse(c.final)
			if err != nil {
				t.Fatalf("bad final URL %q: %v", c.final, err)
			}
			if got := redirectIsSoftNotFound(orig, final); got != c.want {
				t.Errorf("redirectIsSoftNotFound(%s, %s) = %v, want %v", c.original, c.final, got, c.want)
			}
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"example.com":         "example.com",
		"www.example.com":     "example.com",
		"WWW.Example.COM":     "example.com",
		"www.www.example.com": "www.example.com",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":       "/",
		"/":      "/",
		"/foo":   "/foo",
		"/foo/":  "/foo",
		"/foo//": "/foo/",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripLocalePrefix(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"/en/products":    "/products",
		"/en-US/products": "/products",
		"/en":             "/",
		"/en/":            "/",
		"/products":       "/products",
		"/a/products":     "/a/products",
		"/123/products":   "/123/products",
	}
	for in, want := range cases {
		if got := stripLocalePrefix(in); got != want {
			t.Errorf("stripLocalePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsLocaleSegment(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"en":       true,
		"EN":       true,
		"en-US":    true,
		"abcde":    true,
		"e":        false,
		"abcdef":   false,
		"en1":      false,
		"en_US":    false,
		"":         false,
		"products": false,
	}
	for in, want := range cases {
		if got := isLocaleSegment(in); got != want {
			t.Errorf("isLocaleSegment(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCheckSoft404_AllPatterns(t *testing.T) {
	t.Parallel()
	c := newAllowLoopbackChecker()

	for _, pat := range softNotFoundPatterns {
		t.Run(pat, func(t *testing.T) {
			t.Parallel()
			resp := &http.Response{
				Request: &http.Request{Method: http.MethodGet},
				Body:    io.NopCloser(strings.NewReader("<html>Oops, " + pat + " here</html>")),
			}
			exists, tooLarge := c.checkSoft404(resp)
			if tooLarge {
				t.Fatalf("tooLarge = true for pattern %q", pat)
			}
			if exists {
				t.Fatalf("pattern %q was not detected in the body", pat)
			}
		})
	}
}

func TestCheckSoft404_Misc(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		method     string
		body       string
		maxBody    int64
		wantExists bool
		wantLarge  bool
	}{
		{"normal body", http.MethodGet, "welcome to my site", 0, true, false},
		{"empty body", http.MethodGet, "", 0, true, false},
		{"case insensitive match", http.MethodGet, "PAGE NOT FOUND", 0, false, false},
		{"head is trusted", http.MethodHead, "404 page not found", 0, true, false},
		{"oversized body", http.MethodGet, strings.Repeat("a", 20), 10, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			chk := newAllowLoopbackChecker()
			if c.maxBody > 0 {
				chk.MaxBodyBytes = c.maxBody
			}
			resp := &http.Response{
				Request: &http.Request{Method: c.method},
				Body:    io.NopCloser(strings.NewReader(c.body)),
			}
			exists, tooLarge := chk.checkSoft404(resp)
			if tooLarge != c.wantLarge {
				t.Errorf("tooLarge = %v, want %v", tooLarge, c.wantLarge)
			}
			if exists != c.wantExists {
				t.Errorf("exists = %v, want %v", exists, c.wantExists)
			}
		})
	}
}

func TestSafeDialContext_LiteralIPBlocked(t *testing.T) {
	t.Parallel()
	dialed := false
	dial := newSafeDialContext(&fakeResolver{}, isBlockedIP, func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("should not be reached")
	})

	_, err := dial(context.Background(), "tcp", "127.0.0.1:80")
	if !errors.Is(err, errBlockedTarget) {
		t.Fatalf("err = %v, want errBlockedTarget", err)
	}
	if dialed {
		t.Error("dial was reached for a blocked literal IP")
	}
}

func TestSafeDialContext_LiteralIPAllowedSkipsResolver(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{}
	var gotAddr string
	dial := newSafeDialContext(fr, isBlockedIP, func(ctx context.Context, network, addr string) (net.Conn, error) {
		gotAddr = addr
		return nil, errors.New("stop")
	})

	_, err := dial(context.Background(), "tcp", "203.0.113.10:443")
	if err == nil || err.Error() != "stop" {
		t.Fatalf("err = %v, want sentinel from dial", err)
	}
	if gotAddr != "203.0.113.10:443" {
		t.Fatalf("dialed %q, want the literal address", gotAddr)
	}
	if fr.calls != 0 {
		t.Fatalf("resolver called %d times for a literal IP, want 0", fr.calls)
	}
}

func TestSafeDialContext_EmptyResolution(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{ips: [][]net.IPAddr{{}}}
	dial := newSafeDialContext(fr, isBlockedIP, func(ctx context.Context, network, addr string) (net.Conn, error) {
		t.Fatal("dial must not be reached when resolution yields no addresses")
		return nil, nil
	})

	_, err := dial(context.Background(), "tcp", "empty.example:80")
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
		t.Fatalf("err = %v, want a not-found *net.DNSError", err)
	}
}

// TestResultJSONContract locks the /v1 wire schema: the set of field names
// and their JSON types. It is intentionally tolerant of additive fields
// (the contract is additive-only) but fails if an existing field is
// renamed, retyped, or if redirected_to stops being omitempty.
func TestResultJSONContract(t *testing.T) {
	t.Parallel()

	wantKinds := map[string]string{
		"url":              "string",
		"valid_format":     "bool",
		"site_up":          "bool",
		"page_exists":      "bool",
		"status_code":      "number",
		"response_time_ms": "number",
		"reason":           "string",
		"reason_code":      "string",
		"attempts":         "number",
	}

	zero, err := json.Marshal(Result{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(zero, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for name, kind := range wantKinds {
		raw, ok := fields[name]
		if !ok {
			t.Errorf("field %q missing from JSON result", name)
			continue
		}
		if got := jsonKind(raw); got != kind {
			t.Errorf("field %q has JSON kind %q, want %q", name, got, kind)
		}
	}
	if _, ok := fields["redirected_to"]; ok {
		t.Error("redirected_to must be omitted when empty")
	}

	populated, err := json.Marshal(Result{URL: "http://x/", ValidFormat: true, ReasonCode: string(ReasonOK), RedirectedTo: "http://y/"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var withRedirect map[string]json.RawMessage
	if err := json.Unmarshal(populated, &withRedirect); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := withRedirect["redirected_to"]; !ok {
		t.Error("redirected_to must be present when set")
	}
}

func jsonKind(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return "empty"
	}
	switch s[0] {
	case '"':
		return "string"
	case 't', 'f':
		return "bool"
	case '{':
		return "object"
	case '[':
		return "array"
	default:
		return "number"
	}
}

func FuzzValidateFormat(f *testing.F) {
	for _, s := range []string{
		"http://example.com",
		"https://example.com/a?b=1#c",
		"ftp://x",
		"",
		"http://",
		"example.com",
		"http://a b/",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		u, err := validateFormat(raw)
		if err != nil {
			return
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			t.Fatalf("accepted non-http scheme %q for %q", u.Scheme, raw)
		}
		if u.Host == "" {
			t.Fatalf("accepted empty host for %q", raw)
		}
	})
}

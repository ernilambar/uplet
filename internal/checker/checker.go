package checker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultTimeout        = 10 * time.Second
	defaultMaxBodyBytes   = 64 * 1024
	defaultMaxRedirects   = 10
	maxAttempts           = 2
	retryBackoff          = 500 * time.Millisecond
	tlsHandshakeTimeout   = 5 * time.Second
	responseHeaderTimeout = 5 * time.Second
)

// Checker performs SSRF-safe URL reachability and page-existence checks.
// Use New to construct one; the zero value has no resolver or blocklist.
type Checker struct {
	Timeout      time.Duration
	MaxBodyBytes int64
	MaxRedirects int

	resolver hostResolver
	blocked  func(net.IP) bool
}

// New returns a Checker configured with the production SSRF blocklist and
// the system DNS resolver.
func New() *Checker {
	return &Checker{
		Timeout:      defaultTimeout,
		MaxBodyBytes: defaultMaxBodyBytes,
		MaxRedirects: defaultMaxRedirects,
		resolver:     defaultResolver,
		blocked:      isBlockedIP,
	}
}

// Check runs a one-shot check against rawURL using default settings.
func Check(ctx context.Context, rawURL string) Result {
	return New().Check(ctx, rawURL)
}

// Check validates rawURL's format, then performs an SSRF-safe HTTP check,
// classifying the outcome into a stable ReasonCode.
func (c *Checker) Check(ctx context.Context, rawURL string) Result {
	res := Result{URL: rawURL}

	parsed, err := validateFormat(rawURL)
	if err != nil {
		res.ReasonCode = string(ReasonInvalidFormat)
		res.Reason = err.Error()
		return res
	}
	res.ValidFormat = true

	client := c.newClient()
	start := time.Now()

	resp, attempts, err := c.doWithRetry(ctx, client, http.MethodHead, parsed.String())
	res.Attempts = attempts

	// HEAD carries no body, so a 2xx tells us nothing about soft-404
	// content; fetch the body via GET for inspection. A 405 means the
	// server rejects HEAD outright, so fall back to GET for the status
	// itself. Any other status is already definitive from HEAD alone.
	if err == nil && (resp.StatusCode == http.StatusMethodNotAllowed || (resp.StatusCode >= 200 && resp.StatusCode < 300)) {
		resp.Body.Close()
		var attempts2 int
		resp, attempts2, err = c.doWithRetry(ctx, client, http.MethodGet, parsed.String())
		res.Attempts += attempts2
	}

	res.ResponseTimeMs = time.Since(start).Milliseconds()

	if err != nil {
		c.fillError(&res, err)
		return res
	}
	defer resp.Body.Close()

	c.fillFromResponse(&res, parsed, resp)
	return res
}

func validateFormat(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("malformed url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	return u, nil
}

func (c *Checker) doWithRetry(ctx context.Context, client *http.Client, method, rawURL string) (*http.Response, int, error) {
	var resp *http.Response
	var err error
	attempts := 0

	for attempts < maxAttempts {
		attempts++
		reqCtx, cancel := context.WithTimeout(ctx, c.Timeout)
		resp, err = doOnce(reqCtx, client, method, rawURL)
		cancel()

		if err == nil || !isTransientErr(err) || attempts >= maxAttempts {
			break
		}

		select {
		case <-time.After(retryBackoff):
		case <-ctx.Done():
			return nil, attempts, ctx.Err()
		}
	}

	return resp, attempts, err
}

func doOnce(ctx context.Context, client *http.Client, method, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (c *Checker) newClient() *http.Client {
	transport := &http.Transport{
		DialContext:           newSafeDialContext(c.resolver, c.blocked, nil),
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
	}

	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= c.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", c.MaxRedirects)
			}
			return c.validateRedirectTarget(req.Context(), req.URL)
		},
	}
}

// validateRedirectTarget re-runs the full block-list validation on a
// redirect hop's target before the client is allowed to follow it. The
// dialer validates again independently on actual connect, so this is
// defense in depth, not the only guard.
func (c *Checker) validateRedirectTarget(ctx context.Context, u *url.URL) error {
	host := u.Hostname()

	if ip := net.ParseIP(host); ip != nil {
		if c.blocked(ip) {
			return errBlockedTarget
		}
		return nil
	}

	addrs, err := c.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	for _, a := range addrs {
		if c.blocked(a.IP) {
			return errBlockedTarget
		}
	}
	return nil
}

func (c *Checker) fillError(res *Result, err error) {
	res.SiteUp = false

	switch {
	case errors.Is(err, errBlockedTarget):
		res.ReasonCode = string(ReasonBlockedTarget)
		res.Reason = "target address is blocked by SSRF policy"
	case isDNSTimeout(err):
		res.ReasonCode = string(ReasonTimeout)
		res.Reason = "DNS resolution timed out"
	case isDNSNotFound(err):
		res.ReasonCode = string(ReasonDNSError)
		res.Reason = "domain does not resolve"
	case isTLSCertError(err):
		res.ReasonCode = string(ReasonTLSError)
		res.Reason = "TLS certificate validation failed"
	case isTimeoutErr(err) || errors.Is(err, context.DeadlineExceeded):
		res.ReasonCode = string(ReasonTimeout)
		res.Reason = "request timed out"
	default:
		res.ReasonCode = string(ReasonNetworkError)
		res.Reason = "network error: " + err.Error()
	}
}

func (c *Checker) fillFromResponse(res *Result, original *url.URL, resp *http.Response) {
	res.StatusCode = resp.StatusCode
	res.SiteUp = true

	final := resp.Request.URL
	if redirectIsSoftNotFound(original, final) {
		res.RedirectedTo = final.String()
		res.PageExists = false
		res.ReasonCode = string(ReasonRedirectNotFound)
		res.Reason = fmt.Sprintf("redirected to %s", final.String())
		return
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		exists, tooLarge := c.checkSoft404(resp)
		switch {
		case tooLarge:
			res.PageExists = true
			res.ReasonCode = string(ReasonTooLarge)
			res.Reason = "response body exceeded inspection cap"
		case !exists:
			res.PageExists = false
			res.ReasonCode = string(ReasonNotFound)
			res.Reason = "page reports not found"
		default:
			res.PageExists = true
			res.ReasonCode = string(ReasonOK)
			res.Reason = "ok"
		}
	case resp.StatusCode == http.StatusNotFound:
		res.PageExists = false
		res.ReasonCode = string(ReasonNotFound)
		res.Reason = "page not found"
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		res.PageExists = false
		res.ReasonCode = string(ReasonClientError)
		res.Reason = fmt.Sprintf("client error: %d", resp.StatusCode)
	case resp.StatusCode >= 500:
		res.PageExists = false
		res.ReasonCode = string(ReasonServerError)
		res.Reason = fmt.Sprintf("server error: %d", resp.StatusCode)
	default:
		res.PageExists = true
		res.ReasonCode = string(ReasonOK)
		res.Reason = "ok"
	}
}

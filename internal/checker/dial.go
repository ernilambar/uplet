package checker

import (
	"context"
	"errors"
	"net"
	"time"
)

// errBlockedTarget is returned when a resolved (or literal) IP address
// matches the SSRF block list.
var errBlockedTarget = errors.New("blocked_target")

// errTooManyRedirects is returned when a redirect chain exceeds MaxRedirects.
var errTooManyRedirects = errors.New("too many redirects")

const dialTimeout = 5 * time.Second

// hostResolver resolves a hostname to IP addresses. It's an interface so
// tests can substitute deterministic DNS responses.
type hostResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

var defaultResolver hostResolver = net.DefaultResolver

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// newSafeDialContext builds a DialContext that resolves the host, rejects
// the resolution if any candidate IP is blocked, and dials the exact
// validated IPs in resolver order until one connects (so a host whose
// first address is unreachable, e.g. IPv6 on an IPv4-only network, isn't
// reported down). Resolution and dial happen as one atomic step so nothing
// can re-resolve to a different address between validation and connection
// (no TOCTOU window, no DNS-rebinding bypass).
func newSafeDialContext(resolver hostResolver, blocked func(net.IP) bool, dial dialFunc) dialFunc {
	if dial == nil {
		d := &net.Dialer{Timeout: dialTimeout}
		dial = d.DialContext
	}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		if ip := net.ParseIP(host); ip != nil {
			if blocked(ip) {
				return nil, errBlockedTarget
			}
			return dial(ctx, network, addr)
		}

		addrs, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(addrs) == 0 {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}

		for _, a := range addrs {
			if blocked(a.IP) {
				return nil, errBlockedTarget
			}
		}

		var firstErr error
		for _, a := range addrs {
			conn, err := dial(ctx, network, net.JoinHostPort(a.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = err
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, firstErr
	}
}

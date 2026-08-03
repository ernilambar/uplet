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
// validated IP. Resolution and dial happen as one atomic step so nothing
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

		target := addrs[0].IP
		for _, a := range addrs {
			if blocked(a.IP) {
				return nil, errBlockedTarget
			}
		}

		return dial(ctx, network, net.JoinHostPort(target.String(), port))
	}
}

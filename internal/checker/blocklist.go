package checker

import (
	"net"
	"net/netip"
)

// blockedPrefixes are special-purpose ranges that must never be dialed and
// aren't already covered by the netip predicates in isBlockedAddr
// (loopback, RFC 1918 / unique-local, link-local incl. cloud metadata
// 169.254.169.254 and fd00:ec2::254, multicast, unspecified).
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT shared space, incl. Alibaba metadata 100.100.100.200
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, incl. broadcast 255.255.255.255
	netip.MustParsePrefix("::/96"),           // unspecified, loopback, deprecated IPv4-compatible
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64 (RFC 8215)
	netip.MustParsePrefix("100::/64"),        // discard-only
	netip.MustParsePrefix("2001::/32"),       // Teredo
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("fec0::/10"),       // deprecated site-local
}

// IPv6 ranges that embed an IPv4 address. They aren't blanket-blocked
// (DNS64 networks synthesize 64:ff9b:: addresses for every IPv4-only public
// host); instead the embedded IPv4 is checked, so a private target can't be
// reached through a NAT64 or 6to4 gateway.
var (
	nat64Prefix  = netip.MustParsePrefix("64:ff9b::/96")
	sixToFourPfx = netip.MustParsePrefix("2002::/16")
)

// isBlockedIP reports whether ip must never be dialed. It operates on the
// parsed IP, never the hostname string, so alternate encodings
// (octal/decimal/hex) are neutralized automatically. IPv4-mapped IPv6
// addresses are unmapped first so a private address can't be smuggled past
// the checks as IPv6. Anything that isn't a valid IP fails closed.
func isBlockedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return isBlockedAddr(addr.Unmap())
}

func isBlockedAddr(a netip.Addr) bool {
	if a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() || a.IsLinkLocalUnicast() || a.IsMulticast() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return true
		}
	}

	b := a.As16()
	switch {
	case nat64Prefix.Contains(a):
		return isBlockedAddr(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
	case sixToFourPfx.Contains(a):
		return isBlockedAddr(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
	}
	return false
}

package checker

import "net"

// blockedCIDRs are network ranges that must never be dialed: loopback,
// RFC 1918 private space, link-local (incl. cloud metadata 169.254.169.254),
// unique-local IPv6 (incl. metadata fd00:ec2::254), and unspecified/broadcast.
var blockedCIDRs = mustParseCIDRs([]string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"fe80::/10",
	"fc00::/7",
	"0.0.0.0/8",
	"255.255.255.255/32",
})

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		nets = append(nets, n)
	}
	return nets
}

// normalizeIP unmaps IPv4-mapped IPv6 addresses (::ffff:0:0/96) to their
// 4-byte form so a loopback/private address can't be smuggled past the
// CIDR checks by encoding it as IPv6.
func normalizeIP(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

// isBlockedIP reports whether ip must never be dialed. It operates on the
// parsed net.IP, never the hostname string, so alternate encodings
// (octal/decimal/hex) are neutralized automatically.
func isBlockedIP(ip net.IP) bool {
	ip = normalizeIP(ip)
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

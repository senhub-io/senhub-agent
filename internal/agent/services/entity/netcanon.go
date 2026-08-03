package entity

import (
	"net/netip"
	"strings"
)

// CanonicalIP renders an IP literal in its single canonical form for entity
// identities (frozen contract, ADR 0022 / Toise #239): IPv4 dotted-quad, IPv6
// per RFC 5952 (lowercase, one :: compression), IPv4-mapped IPv6 unmapped to
// its dotted-quad form, zone kept verbatim. Built on net/netip — net.ParseIP
// rejects zoned addresses (fe80::1%eth0), which then leak into identities as
// raw literals. ok=false when s is not an IP literal.
func CanonicalIP(s string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return "", false
	}
	return addr.Unmap().String(), true
}

// CanonicalCIDR renders {ip, prefixLen} as the canonical CIDR used for
// route.destination identities (frozen contract, ADR 0022 / Toise #239): the
// prefix is always explicit (including /32 and /128), host bits are zeroed
// (10.20.3.0/24, never 10.20.3.7/24), the address follows CanonicalIP's rules
// with any zone stripped, and default routes render as 0.0.0.0/0 and ::/0.
// Two observers of the same route must derive byte-identical identities — a
// device reporting host bits in a destination would otherwise mint a distinct
// route entity per observation. ok=false on a non-IP literal or an
// out-of-range prefix length.
func CanonicalCIDR(ip string, prefixLen int) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return "", false
	}
	p := netip.PrefixFrom(addr.Unmap().WithZone(""), prefixLen)
	if !p.IsValid() {
		return "", false
	}
	return p.Masked().String(), true
}

package entity

import (
	"fmt"
	"net"
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

// CanonicalMAC renders a hardware address in the form the OpenTelemetry
// semantic conventions define for MAC values: IEEE 802 octets as two
// uppercase hex digits separated by hyphens (BC-24-11-1B-04-82). A MAC can
// become an identity (the mac: rung of a device id), so every observer must
// render it byte-identically. Empty input yields "".
func CanonicalMAC(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02X", c)
	}
	return strings.Join(parts, "-")
}

// CanonicalMACString parses a textual MAC in any form net.ParseMAC accepts
// (aa:bb:cc:dd:ee:ff, AA-BB-CC-DD-EE-FF, aabb.ccdd.eeff) and renders it with
// CanonicalMAC. ok=false when s is not a MAC.
func CanonicalMACString(s string) (string, bool) {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil {
		return "", false
	}
	return CanonicalMAC(hw), true
}

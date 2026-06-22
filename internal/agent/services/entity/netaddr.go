package entity

import "net"

// dockerDefaultBridge is Docker's default bridge subnet (docker0). Its gateway
// 172.17.0.1 is identical on every Docker host, so it is not a globally-unique
// identity. Toise's contract calls it out by name as the most visible host-local
// false-join; its consumers (graph-viz overlay) skip exactly this /16.
var dockerDefaultBridge = &net.IPNet{IP: net.IPv4(172, 17, 0, 0), Mask: net.CIDRMask(16, 32)}

// IsHostLocalAddress reports whether an IP is host-local / non-routable and so
// must NEVER be emitted as a shared network.address entity. A network.address is
// identified by its IP alone, so the identity is only meaningful when the IP is
// globally unique: a host-local value (loopback, link-local, wildcard, the
// Docker default-bridge gateway) exists independently on every machine, and a
// single shared node would falsely link unrelated entities across hosts — the
// network-derived-identity anti-pattern (Toise ADR 0018, otel-mapping contract).
//
// The set mirrors Toise's own consumer-side skip list exactly: wildcard,
// loopback, link-local (RFC 3927 + fe80::/10), multicast, and the Docker default
// bridge 172.17.0.0/16. Such an address, if recorded at all, belongs on a
// host-scoped descriptive attribute, never a shared entity.
func IsHostLocalAddress(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsUnspecified() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() ||
		dockerDefaultBridge.Contains(ip)
}

// IsHostLocalAddressStr is the string convenience over IsHostLocalAddress: it
// parses an "ip" or "ip/prefix" and reports host-local (an unparseable value is
// treated as host-local — never a shared identity).
func IsHostLocalAddressStr(s string) bool {
	host := s
	if ip, _, err := net.ParseCIDR(s); err == nil {
		host = ip.String()
	}
	return IsHostLocalAddress(net.ParseIP(host))
}

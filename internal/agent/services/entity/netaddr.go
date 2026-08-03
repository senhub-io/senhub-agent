package entity

import (
	"net"
	"net/netip"
	"strings"
)

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

// containerBridgePrefixes name host-local virtualization bridges (Docker,
// libvirt, CNI, LXC, …). Their gateway address (172.17.0.1 on docker0, but also
// user-defined bridges on br-<hex> using 172.18+/custom ranges) is reused
// identically on every such host, so it is not a globally-unique identity. The
// IP-range filter catches only Docker's default 172.17/16; a user-defined bridge
// is not distinguishable by IP, but its OWNING INTERFACE is — context the
// producer has and a by-IP consumer does not. "br-" is Docker's user-bridge
// naming (br-<12 hex>); plain "br0"/"bridge0" are NOT matched so a real router's
// routed bridge keeps its address.
var containerBridgePrefixes = []string{
	"docker", "br-", "virbr", "cni", "cbr", "flannel", "lxcbr", "kube", "cali", "antrea", "weave", "ovs-system",
}

// IsContainerBridgeIface reports whether an interface name is a host-local
// container/virtualization bridge, so an address bound to it must not be emitted
// as a shared network.address. Complements IsHostLocalAddress: that filter is by
// IP (works without interface context), this one by interface name (catches
// user-defined bridges the IP filter cannot). Apply both where the owning
// interface is known.
func IsContainerBridgeIface(name string) bool {
	for _, p := range containerBridgePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// CanonicalHostScopedAddr classifies a peer address for network.endpoint
// identity. It returns the address in its RFC 5952-canonical string form
// (IPv4-mapped IPv6 unmapped so it classifies like its IPv4 self; an IPv6 zone
// kept but lowercased, per the frozen Toise contract) and whether it is
// HOST-SCOPED — loopback (127.0.0.0/8, ::1) or link-local UNICAST
// (169.254.0.0/16, fe80::/10). A host-scoped address is only meaningful relative
// to the observing host, so an endpoint on it must carry host.id in its identity
// or unrelated hosts collapse onto one node (Toise ADR 0032). ok=false when the
// address does not parse.
//
// Deliberately NARROWER than IsHostLocalAddress: wildcard, multicast and the
// docker bridge are not host-scoped endpoints — they are filtered out entirely
// upstream (resolvablePeer), never emitted.
func CanonicalHostScopedAddr(s string) (canonical string, hostScoped, ok bool) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return "", false, false
	}
	a = a.Unmap()
	// netip String() is already RFC 5952 for IPv6 and appends the zone as
	// %zone; ToLower normalizes an upper-cased zone (e.g. a Windows interface
	// name) without affecting the address text.
	return strings.ToLower(a.String()), a.IsLoopback() || a.IsLinkLocalUnicast(), true
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

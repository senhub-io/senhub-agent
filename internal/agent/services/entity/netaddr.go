package entity

import (
	"net"
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

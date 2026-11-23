package hostcaller

import (
	"net"
	"net/netip"
	"strings"

	gnet "github.com/shirou/gopsutil/v3/net"

	"senhub-agent.go/internal/agent/services/entity"
)

// podmanDefaultBridge is the default CNI network of Podman, reused unchanged
// on every host that runs it.
var podmanDefaultBridge = netip.MustParsePrefix("10.88.0.0/16")

// parseAddr reads a socket-table address: zone dropped, IPv4-mapped IPv6
// unmapped, so one caller has one spelling.
func parseAddr(s string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.WithZone("").Unmap(), true
}

// isLocalCaller reports a caller address that says nothing about who called:
// wildcard, loopback, link-local, multicast, the default container bridges.
func isLocalCaller(a netip.Addr) bool {
	return entity.IsHostLocalAddress(net.IP(a.AsSlice())) || podmanDefaultBridge.Contains(a)
}

// isPublic reports a globally routable unicast address: not private, not
// CGNAT, not unique-local, not host-local.
func isPublic(a netip.Addr) bool {
	return entity.AddressIsGloballyUnique(a.String())
}

// truncate names the /24 (IPv4) or /48 (IPv6) an address is in.
func truncate(a netip.Addr) string {
	bits := 48
	if a.Is4() {
		bits = 24
	}
	p, err := a.Prefix(bits)
	if err != nil {
		return ""
	}
	return p.String()
}

// ifaceView is the host interface table, read once per socket read: which
// local addresses belong to a container bridge, and which networks they span.
type ifaceView struct {
	bridgeIPs      map[string]bool
	bridgePrefixes []netip.Prefix
}

func newIfaceView(read func() (gnet.InterfaceStatList, error)) *ifaceView {
	v := &ifaceView{bridgeIPs: map[string]bool{}}
	ifaces, err := read()
	if err != nil {
		return v
	}
	for _, ifc := range ifaces {
		if !entity.IsContainerBridgeIface(ifc.Name) {
			continue
		}
		for _, a := range ifc.Addrs {
			raw := strings.TrimSpace(a.Addr)
			if p, err := netip.ParsePrefix(raw); err == nil {
				v.bridgeIPs[p.Addr().Unmap().String()] = true
				v.bridgePrefixes = append(v.bridgePrefixes, p.Masked())
				continue
			}
			if ip, ok := parseAddr(raw); ok {
				v.bridgeIPs[ip.String()] = true
			}
		}
	}
	return v
}

// bridged reports a caller that reached the host through a container bridge:
// it connected to a local address that sits on one, or its address is inside
// a network one of them spans.
func (v *ifaceView) bridged(caller netip.Addr, localIP string) bool {
	if local, ok := parseAddr(localIP); ok && v.bridgeIPs[local.String()] {
		return true
	}
	for _, p := range v.bridgePrefixes {
		if p.Contains(caller) {
			return true
		}
	}
	return false
}

package snmppoll

import (
	"fmt"
	"net"
	"senhub-agent.go/internal/agent/services/snmpcore"
	"strconv"
	"strings"
)

// IP-MIB address inventory (entity rail): the device's interface IPs as
// network.address entities bound to their interface. The same network.address
// {ip} node is referenced by a host's next_hop_via when that device is the
// host's gateway — the shared address joins the host and device topology graphs.
const (
	// ipAdEntIfIndex (ipAddrTable, RFC 1213) — index is the IPv4 address itself,
	// value is the ifIndex it is configured on. The classic IPv4 table; widely
	// implemented. (ipAddressTable / IPv6 is a later concern.)
	ipAdEntIfIndex = "1.3.6.1.2.1.4.20.1.2"
)

// ipAddr is one decoded interface address binding.
type ipAddr struct {
	IP      string // dotted IPv4
	IfIndex string
}

// collectIPAddrs walks ipAdEntIfIndex and returns the device's routable
// interface addresses (loopback / unspecified dropped — they are not topology).
func collectIPAddrs(client snmpClient) ([]ipAddr, error) {
	binds, err := client.WalkRaw(ipAdEntIfIndex)
	if err != nil {
		return nil, fmt.Errorf("ipAddrTable walk: %w", err)
	}
	return parseIPAddrs(binds), nil
}

func parseIPAddrs(binds []snmpRawBind) []ipAddr {
	var out []ipAddr
	prefix := ipAdEntIfIndex + "."
	for _, b := range binds {
		addr, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		ip := net.ParseIP(addr)
		if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		idx, ok := snmpcore.AsInt(b.Value)
		if !ok {
			continue
		}
		out = append(out, ipAddr{IP: addr, IfIndex: strconv.Itoa(idx)})
	}
	return out
}

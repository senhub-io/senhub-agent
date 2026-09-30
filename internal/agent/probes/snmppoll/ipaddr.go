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
	// ipAdEntNetMask — same index, value is the subnet mask the address
	// is configured with on that interface.
	ipAdEntNetMask = "1.3.6.1.2.1.4.20.1.3"
)

// ipAddr is one decoded interface address binding.
type ipAddr struct {
	IP      string // dotted IPv4
	IfIndex string
	// Prefix is the mask's length, -1 when the device did not give it.
	Prefix int
}

// collectIPAddrs walks ipAdEntIfIndex and returns the device's routable
// interface addresses (loopback / unspecified dropped — they are not topology).
func collectIPAddrs(client snmpClient) ([]ipAddr, error) {
	binds, err := client.WalkRaw(ipAdEntIfIndex)
	if err != nil {
		return nil, fmt.Errorf("ipAddrTable walk: %w", err)
	}
	addrs := parseIPAddrs(binds)
	// The mask is optional: an agent that does not answer the column keeps
	// its addresses, without a subnet.
	if masks, merr := client.WalkRaw(ipAdEntNetMask); merr == nil {
		applyNetMasks(addrs, masks)
	}
	return addrs, nil
}

// applyNetMasks sets each address's prefix from ipAdEntNetMask.
func applyNetMasks(addrs []ipAddr, binds []snmpRawBind) {
	prefix := ipAdEntNetMask + "."
	byIP := map[string]int{}
	for _, b := range binds {
		ip, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		mask := net.ParseIP(asIPString(b.Value)).To4()
		if mask == nil {
			continue
		}
		ones, bits := net.IPMask(mask).Size()
		if bits == 0 {
			continue
		}
		byIP[ip] = ones
	}
	for i := range addrs {
		if p, ok := byIP[addrs[i].IP]; ok {
			addrs[i].Prefix = p
		}
	}
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
		out = append(out, ipAddr{IP: addr, IfIndex: strconv.Itoa(idx), Prefix: -1})
	}
	return out
}

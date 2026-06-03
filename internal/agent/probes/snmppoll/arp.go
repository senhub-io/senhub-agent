package snmppoll

import (
	"fmt"
	"net"
	"strings"
)

// ARP / neighbour cache — ipNetToMediaTable (IP-MIB). Provides IP↔MAC
// bindings the agent uses to converge a provisional mgmt:<ip> identity to a
// device's canonical mac:<addr> (Toise D3: the producer promotes to the
// canonical id; no merge on the consumer). Entry index = ifIndex.IPv4(4).
const (
	ipNetToMediaPhysAddress = "1.3.6.1.2.1.4.22.1.2"
)

// arpEntry is one IP↔MAC binding learned on an interface.
type arpEntry struct {
	IfIndex string
	IP      string // canonical
	MAC     string // lowercase colon hex (same form as macHex)
}

func collectARP(client snmpClient) ([]arpEntry, error) {
	binds, err := client.WalkRaw(ipNetToMediaPhysAddress)
	if err != nil {
		return nil, fmt.Errorf("ipNetToMedia walk: %w", err)
	}
	return parseARP(binds), nil
}

func parseARP(binds []snmpRawBind) []arpEntry {
	var out []arpEntry
	prefix := ipNetToMediaPhysAddress + "."
	for _, b := range binds {
		rest, ok := strings.CutPrefix(b.OID, prefix)
		if !ok {
			continue
		}
		// rest = "<ifIndex>.<a.b.c.d>"
		ifIndex, ipPart, ok := strings.Cut(rest, ".")
		if !ok {
			continue
		}
		ip := canonIP(ipPart)
		mac := macHex(asBytes(b.Value))
		if net.ParseIP(ip) == nil || mac == "" {
			continue
		}
		out = append(out, arpEntry{IfIndex: ifIndex, IP: ip, MAC: mac})
	}
	return out
}

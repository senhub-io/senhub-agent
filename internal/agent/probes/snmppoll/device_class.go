package snmppoll

import "strings"

var firewallVendorOIDPrefixes = []string{
	"1.3.6.1.4.1.12356", // Fortinet
	"1.3.6.1.4.1.25461", // Palo Alto
	"1.3.6.1.4.1.2620",  // Check Point
}

const (
	lldpCapBridge = 0x20
	lldpCapWLAN   = 0x10
	lldpCapRouter = 0x08
)

func deviceClassFromCaps(caps any, sysObjectID string) string {
	if isFirewallVendor(sysObjectID) {
		return "firewall"
	}
	switch c := caps.(type) {
	case []byte:
		return deviceClassFromLLDPCaps(c)
	case []string:
		return deviceClassFromCDPCaps(c)
	default:
		return "other"
	}
}

func deviceClassFromLLDPCaps(caps []byte) string {
	if len(caps) == 0 {
		return "other"
	}
	b := caps[0]
	// BITS encoding: the most significant bit of the first octet is bit 0.
	if b&lldpCapRouter != 0 {
		return "router"
	}
	if b&lldpCapBridge != 0 {
		return "switch"
	}
	if b&lldpCapWLAN != 0 {
		return "access_point"
	}
	return "other"
}

func deviceClassFromCDPCaps(caps []string) string {
	router := false
	sw := false
	for _, c := range caps {
		switch c {
		case "router":
			router = true
		case "switch", "bridge":
			sw = true
		}
	}
	if router {
		return "router"
	}
	if sw {
		return "switch"
	}
	return "other"
}

func isFirewallVendor(sysObjectID string) bool {
	s := strings.TrimPrefix(strings.TrimSpace(sysObjectID), ".")
	for _, p := range firewallVendorOIDPrefixes {
		if s == p || strings.HasPrefix(s, p+".") {
			return true
		}
	}
	return false
}

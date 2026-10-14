package network

import "senhub-agent.go/internal/agent/tags"

// firstIP returns the first valid interface address, the value the previous
// Linux ip label used.
func firstIP(addresses []string) string {
	if len(addresses) == 0 {
		return ""
	}
	return addresses[0]
}

// primaryWindowsIP picks the same primary address the Windows collector used
// before the label was made opt-in: IPv4 when present, otherwise IPv6.
func primaryWindowsIP(ipv4, ipv6 string) string {
	if ipv4 != "" {
		return ipv4
	}
	return ipv6
}

// appendIPLabel adds the ip tag only when the label is enabled and the address
// is known.
func appendIPLabel(dst []tags.Tag, include bool, ip string) []tags.Tag {
	if !include || ip == "" {
		return dst
	}
	return append(dst, tags.Tag{Key: "ip", Value: ip, Private: false})
}

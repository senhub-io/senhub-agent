package snmppoll

import "net"

// SNMP discovery crawl (#156): from a seed device, poll it, read its LLDP
// neighbours' management addresses, and breadth-first expand the poll set —
// auto-discovering the network fabric instead of hand-listing every device.
// This file is the pure engine (all SNMP I/O is behind crawlPoller, so it is
// fully unit-testable); the multi-target probe integration is a later lot.

// discoveredDevice is one device found by the crawl.
type discoveredDevice struct {
	ID     string // resolved network.device.id (dedup key)
	MgmtIP string // the management IP it answered at
	Hop    int    // BFS depth from a seed (seed = 0)
}

// crawlPoller polls one target IP and returns the device's resolved
// network.device.id plus the management IPs of its LLDP neighbours. A target
// that does not answer (or cannot be identified) returns ("", nil); it is
// skipped, not retried.
type crawlPoller func(target string) (deviceID string, neighborIPs []string)

// crawlBounds keeps the crawl from scanning beyond the managed network or
// flooding: only neighbour IPs inside Allowed are followed (seeds are always
// polled, whatever their address), the BFS stops at MaxHops, and at most
// MaxDevices are returned.
type crawlBounds struct {
	MaxDevices int
	MaxHops    int
	Allowed    []*net.IPNet
}

// crawl walks the LLDP neighbour graph breadth-first from seeds, deduping by
// resolved device.id — a device reachable on several management IPs collapses
// to one node. Deterministic: BFS order follows seed then discovery order.
func crawl(seeds []string, poll crawlPoller, b crawlBounds) []discoveredDevice {
	type item struct {
		ip  string
		hop int
	}
	queue := make([]item, 0, len(seeds))
	for _, s := range seeds {
		queue = append(queue, item{ip: s, hop: 0})
	}

	visitedIP := map[string]bool{}
	seenDev := map[string]bool{}
	var out []discoveredDevice

	for len(queue) > 0 && len(out) < b.MaxDevices {
		it := queue[0]
		queue = queue[1:]
		if visitedIP[it.ip] {
			continue
		}
		visitedIP[it.ip] = true

		deviceID, neighborIPs := poll(it.ip)
		if deviceID == "" { // no answer / unidentifiable
			continue
		}
		if seenDev[deviceID] { // same device on another management IP
			continue
		}
		seenDev[deviceID] = true
		out = append(out, discoveredDevice{ID: deviceID, MgmtIP: it.ip, Hop: it.hop})

		if it.hop >= b.MaxHops { // do not expand past the hop bound
			continue
		}
		for _, nip := range neighborIPs {
			if nip != "" && !visitedIP[nip] && inAllowedCIDR(nip, b.Allowed) {
				queue = append(queue, item{ip: nip, hop: it.hop + 1})
			}
		}
	}
	return out
}

// inAllowedCIDR reports whether ip falls within one of the allowed ranges. An
// empty allow-list follows NO neighbour (the crawl stays at the seeds): an
// operator opts a range in explicitly, so a stray next-hop can never lead the
// crawl off the managed network.
func inAllowedCIDR(ip string, allowed []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range allowed {
		if n != nil && n.Contains(parsed) {
			return true
		}
	}
	return false
}

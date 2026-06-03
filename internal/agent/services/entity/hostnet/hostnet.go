// Package hostnet is the host-side topology entity source (entity Lot 4,
// #212): it reads the local kernel routing table + ARP cache and emits the
// host's upstream gateways as network.device entities + routes_via edges, on
// the frozen entity rail — the host-side equivalent of snmp_poll Lot 5, with
// no SNMP needed.
//
// Contract (confirmed with Toise — option a): a host-sourced edge reuses the
// frozen relation types with From=host (the From/To types are indicative, not
// enforced; same as monitors). The host endpoint resolves by exact identity on
// the existing host.id entity (Lot 1) — no fusion with a network.device twin.
// Host-sourced edges are tagged with a `source` attribute to distinguish them
// from device-sourced ones at query time.
//
// Scope: gateways only (bounded, high value). adjacent_to to every ARP
// neighbour is deferred — it would flood the graph with host MACs and we have
// no device-classification signal on the host (same reasoning as the SNMP FDB
// filter). ARP is used here only to converge a gateway IP to its canonical
// device MAC.
package hostnet

import (
	"net"
	"os"
	"strconv"
	"strings"

	"senhub-agent.go/internal/agent/services/entity"
)

const (
	entityTypeNetworkDevice = "network.device"
	entityTypeHost          = "host"
	idKeyNetworkDevice      = "network.device.id"
	idKeyHost               = "host.id"
	relRoutesVia            = "routes_via"

	procRoute = "/proc/net/route"
	procARP   = "/proc/net/arp"
)

// Source implements entity.Source for host network tables.
type Source struct {
	hostID    func() string
	readRoute func() ([]byte, error)
	readARP   func() ([]byte, error)
}

// New builds the host-net source. hostID returns the host's stable id
// (gopsutil HostID) for the relation's From endpoint.
func New(hostID func() string) *Source {
	return &Source{
		hostID:    hostID,
		readRoute: func() ([]byte, error) { return os.ReadFile(procRoute) },
		readARP:   func() ([]byte, error) { return os.ReadFile(procARP) },
	}
}

// Observe reads the routing/ARP tables and builds the snapshot. Reading the
// local /proc files is fast and non-blocking; on non-Linux (or unreadable
// /proc) it degrades to an empty observation.
func (s *Source) Observe() entity.Observation {
	var routeData, arpData []byte
	if b, err := s.readRoute(); err == nil {
		routeData = b
	}
	if b, err := s.readARP(); err == nil {
		arpData = b
	}
	return buildObservation(s.hostID(), parseProcRoute(routeData), parseProcARP(arpData))
}

// buildObservation maps gateways → network.device entities + host routes_via
// edges (From=host, Toise-confirmed). ARP converges a gateway IP to its
// canonical mac:; otherwise the gateway stays mgmt:<ip>.
func buildObservation(hostID string, gateways []string, arp map[string]string) entity.Observation {
	if hostID == "" || len(gateways) == 0 {
		return entity.Observation{}
	}
	hostKey := map[string]any{idKeyHost: hostID}

	obs := entity.Observation{}
	seen := map[string]bool{}
	for _, gw := range gateways {
		id := "mgmt:" + gw
		if mac := arp[gw]; mac != "" {
			id = "mac:" + mac
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		obs.Entities = append(obs.Entities, entity.Entity{
			Type: entityTypeNetworkDevice,
			ID:   map[string]any{idKeyNetworkDevice: id},
		})
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     relRoutesVia,
			FromType: entityTypeHost, FromID: hostKey,
			ToType: entityTypeNetworkDevice, ToID: map[string]any{idKeyNetworkDevice: id},
			// Distinguish host-sourced edges from device-sourced ones at query
			// time (Toise: use an attribute, not a new relation type).
			Attributes: map[string]any{"source": "host-route"},
		})
	}
	return obs
}

// parseProcRoute returns the distinct non-zero next-hop gateway IPs from the
// Linux /proc/net/route table (Gateway column is hex, little-endian).
func parseProcRoute(data []byte) []string {
	var out []string
	seen := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 { // header
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		ip := hexLEToIP(f[2])
		if ip == "" || ip == "0.0.0.0" {
			continue
		}
		if !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

// parseProcARP returns IP→MAC bindings from the Linux /proc/net/arp table,
// dropping incomplete (zero-MAC) entries.
func parseProcARP(data []byte) map[string]string {
	out := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		ip, mac := f[0], strings.ToLower(f[3])
		if net.ParseIP(ip) == nil || mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		out[ip] = mac
	}
	return out
}

// hexLEToIP decodes an 8-hex-char little-endian IPv4 (as in /proc/net/route)
// to dotted form.
func hexLEToIP(h string) string {
	if len(h) != 8 {
		return ""
	}
	b := make([]byte, 4)
	for i := 0; i < 4; i++ {
		v, err := strconv.ParseUint(h[2*i:2*i+2], 16, 8)
		if err != nil {
			return ""
		}
		b[3-i] = byte(v) // little-endian → network order
	}
	return net.IP(b).String()
}

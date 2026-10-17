package hostnet

import (
	"net"
	"sort"

	"senhub-agent.go/internal/agent/services/entity"
)

// DefaultGateways returns the host's IPv4 default gateways, sorted by route
// metric then address. Container-bridge gateways are ignored because they
// are reused identically on every host.
func DefaultGateways() ([]string, error) {
	routes, err := platformRoutes()
	if err != nil {
		return nil, err
	}
	return defaultGatewaysFrom(routes), nil
}

// defaultGatewaysFrom is the pure core of DefaultGateways. It keeps only the
// default route destinations, validates and canonicalises IPv4 next hops,
// drops host-local and container-bridge gateways, dedupes, and sorts by
// metric then address.
func defaultGatewaysFrom(routes []hostRoute) []string {
	type gateway struct {
		ip     string
		metric int64
	}

	seen := map[string]bool{}
	gateways := make([]gateway, 0)
	for _, r := range routes {
		if r.Destination != "0.0.0.0/0" {
			continue
		}
		if entity.IsContainerBridgeIface(r.Iface) {
			continue
		}
		ip, ok := entity.CanonicalIP(r.NextHop)
		if !ok {
			continue
		}
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil || parsed.IsLoopback() || parsed.IsUnspecified() {
			continue
		}
		if seen[ip] {
			continue
		}
		seen[ip] = true
		gateways = append(gateways, gateway{ip: ip, metric: r.Metric})
	}

	sort.Slice(gateways, func(i, j int) bool {
		if gateways[i].metric != gateways[j].metric {
			return gateways[i].metric < gateways[j].metric
		}
		return gateways[i].ip < gateways[j].ip
	})

	out := make([]string, len(gateways))
	for i, g := range gateways {
		out[i] = g.ip
	}
	return out
}

package hostnet

import "senhub-agent.go/internal/agent/services/entity"

// A path may cross a host only when the host routes the path's address
// family: most hosts with two interfaces do not route, and the graph has
// no other way to tell a gateway from a multi-homed server. One fact per
// family, never their OR: a host forwarding IPv4 only must not be crossed
// on an IPv6 path on the strength of its IPv4 switch.
const (
	attrIPForwardingV4 = "senhub.host.ip_forwarding.ipv4"
	attrIPForwardingV6 = "senhub.host.ip_forwarding.ipv6"
)

// withForwarding sets the per-family forwarding facts on the host entity of
// obs. A switch that could not be read ("") leaves its attribute absent: an
// unread fact never reads as false.
func withForwarding(obs entity.Observation, hostID, v4, v6 string) entity.Observation {
	for i := range obs.Entities {
		e := &obs.Entities[i]
		if e.Type != entityTypeHost || e.ID[idKeyHost] != hostID {
			continue
		}
		if e.Attributes == nil {
			e.Attributes = map[string]any{}
		}
		for key, v := range map[string]string{attrIPForwardingV4: v4, attrIPForwardingV6: v6} {
			switch v {
			case "1":
				e.Attributes[key] = true
			case "0":
				e.Attributes[key] = false
			}
		}
		return obs
	}
	return obs
}

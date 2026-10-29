package snmppoll

import (
	"senhub-agent.go/internal/agent/services/entity"
)

// A route the system-route filter leaves out after this process published it
// is no longer listed; the tracker then asks DepartureReason why, and the
// answer is excluded_by_config: the route still exists, only the
// configuration stops it being reported. A route an earlier process
// published is unknown to this one and leaves by liveness expiry.

// routeFilterEntity is the identity appendRouteEntities gives a route row
// normalized by eligibleRoute.
func routeFilterEntity(deviceID string, nr routeRow) entity.Entity {
	return entity.Entity{
		Type: entityTypeNetworkRoute,
		ID: map[string]any{
			idKeyNetworkDevice:    deviceID,
			idKeyRouteDestination: nr.Destination,
			attrNextHopIP:         nr.NextHop,
		},
		Scope: entity.ScopeSNMPRoute,
	}
}

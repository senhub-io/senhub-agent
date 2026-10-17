package entity

// Identity keys of entity types that several producers emit. A key that two
// sources must spell identically lives here, once; a spelling that drifted
// between producers would split one entity in two.
const (
	// KeyRouteDestination and KeyRouteNextHop identify a network.route
	// together with its owner (host.id or network.device.id).
	KeyRouteDestination = "route.destination"
	KeyRouteNextHop     = "next_hop.ip"

	// KeyNeighborMAC identifies a network.neighbor within its owner.
	KeyNeighborMAC = "mac"
)

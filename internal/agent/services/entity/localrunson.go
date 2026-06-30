package entity

import (
	"net"
	"strings"
)

// IsLoopbackHost reports whether serverAddress denotes the agent's own host with
// certainty: empty, "localhost", or a loopback IP (127.0.0.0/8, ::1). A probe
// monitoring such a target watches a service running on this very host, so it
// may anchor that service to the host with a runs_on edge. Any other address
// (a routable IP or a remote name) is treated as remote: a service reached over
// it may run anywhere, and must NOT claim to run on the agent's host.
func IsLoopbackHost(addr string) bool {
	switch addr {
	case "", "localhost":
		return true
	}
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}

// LocalRunsOn returns a `<fromType> --runs_on--> host` relation when the
// monitored target is definitively on the agent's own host — i.e. serverAddress
// is loopback, "localhost" or empty — so a locally-monitored service hangs off
// the host node instead of floating with only its `monitors` anchor. It returns
// ok=false for remote or unknown addresses (a remote target must not claim to
// run on this host) and when hostID is "" (host identity unreadable).
//
// fromID is the source entity's exact identity (e.g. {"service.instance.id":
// "nginx@<host>"}); hostID is the agent host's stable id (entity.HostID()).
//
// Collapse guard (the loopback false-join, same family as the Docker
// default-bridge gateway): a loopback address is identical on every host, so an
// identity DERIVED from it (e.g. "modbus://127.0.0.1:502", "mssql:localhost:1433")
// is the SAME node on every host. Anchoring such a shared node to a host would
// wire every host to it and, transitively, to each other. So when the source
// identity embeds the loopback address it is refused here: a network-derived
// identity must be host-scoped (e.g. "<svc>@<host.id>") or a globally-unique
// tech id before it can carry a runs_on. Host-scoped and tech ids never contain
// the loopback literal, so they pass.
func LocalRunsOn(fromType string, fromID map[string]any, serverAddress, hostID string) (Relation, bool) {
	if hostID == "" || !IsLoopbackHost(serverAddress) {
		return Relation{}, false
	}
	if identityEmbeds(fromID, serverAddress) {
		return Relation{}, false
	}
	return Relation{
		Type:     "runs_on",
		FromType: fromType, FromID: fromID,
		ToType: "host", ToID: map[string]any{"host.id": hostID},
	}, true
}

// identityEmbeds reports whether any identifying value is derived from the given
// (loopback) server address — i.e. the address appears inside the identity, the
// signature of a network-derived id that is not unique across hosts. The empty
// address embeds nothing.
func identityEmbeds(id map[string]any, serverAddress string) bool {
	if serverAddress == "" {
		return false
	}
	for _, v := range id {
		if s, ok := v.(string); ok && strings.Contains(s, serverAddress) {
			return true
		}
	}
	return false
}

package dbcommon

import (
	"net"

	"senhub-agent.go/internal/agent/services/common"
	"senhub-agent.go/internal/agent/services/entity"
)

// HostID returns the agent host's stable machine-id (the same id the host entity
// uses), or "" when it cannot be read. Used as the default host resolver by the
// db entity sources when deciding whether a local db runs on this host.
func HostID() string {
	hi, err := common.GetHostIdentity()
	if err != nil {
		return ""
	}
	return hi.ID
}

// LocalHostRunsOn returns a `db --runs_on--> host` relation when the database is
// definitively on the agent's own host — i.e. serverAddress is loopback,
// "localhost" or empty — so a local db hangs off the host node instead of
// floating (enterprise#36). It returns ok=false for remote or unknown addresses
// (a remote db must NOT claim to run on this host) and when hostID is "".
//
// Only loopback is treated as local for now: a db reached over loopback is on
// this host with certainty. A db on a specific non-loopback IP that happens to
// be a local interface address is also local, but proving that needs the host's
// interface list — deferred.
func LocalHostRunsOn(dbID map[string]any, serverAddress, hostID string) (entity.Relation, bool) {
	if hostID == "" || !IsLoopbackHost(serverAddress) {
		return entity.Relation{}, false
	}
	return entity.Relation{
		Type:     "runs_on",
		FromType: "db", FromID: dbID,
		ToType: "host", ToID: map[string]any{"host.id": hostID},
	}, true
}

// IsLoopbackHost reports whether serverAddress denotes the local host with
// certainty: empty, "localhost", or a loopback IP (127.0.0.0/8, ::1). Shared
// with non-db sources (snmppoll) that emit a local-target runs_on edge.
func IsLoopbackHost(addr string) bool {
	switch addr {
	case "", "localhost":
		return true
	}
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}

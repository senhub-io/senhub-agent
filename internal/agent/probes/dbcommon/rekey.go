package dbcommon

import (
	"senhub-agent.go/internal/agent/services/entity"
)

// NewRekeyAnnouncer announces the 0.5.4 db identity migration for a local
// instance whose id was host-scoped (#740).
//
// A thin wrapper over the general announcer: the mechanism is identical for
// every type, only the type name, the identity key and the two spellings
// differ. The db case is where it was first needed and where the reasoning is
// written down — see entity.RekeyAnnouncer.
//
// Returns nil when nothing was re-keyed: a routable address keeps its identity,
// so there is no node to retire and no alias to draw.
func NewRekeyAnnouncer(system, address string, port int, hostID string) *entity.RekeyAnnouncer {
	return entity.NewRekeyAnnouncer(
		entity.TypeDB,
		"db.instance.id",
		legacyFallbackInstanceID(address, port),
		FallbackInstanceID(system, address, port, hostID),
	)
}

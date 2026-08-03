package mssql

import (
	"strconv"

	"senhub-agent.go/internal/agent/probes/dbcommon"
	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail (#185): the monitored SQL Server instance as a "db" entity, so a
// consumer (Toise) discovers the instance as an asset and can join its metrics
// to it. Identity is the OTel-canonical db.instance.id + db.system.name —
// exact and immutable for the connection target (host:port). The instance is a
// node; the agent→db "monitors" edge needs the agent's service.instance.id and
// is left to the foundation/detector layer, so the source emits the bare node.
const (
	entityTypeDB     = "db"
	idKeyDBInstance  = "db.instance.id"
	idKeyDBSystem    = "db.system.name"
	dbSystemMSSQL    = "mssql"
	dbInstanceScheme = "mssql://"
)

// mssqlEntitySource is a constant single-entity source: the connection target
// is fixed at construction, so Observe never blocks and never changes. It still
// goes through the registry so the entity is heartbeated (and retired when the
// poller unregisters it on shutdown).
type mssqlEntitySource struct {
	obs entity.Observation
	// hostID resolves the agent host for the local-db runs_on edge; overridable
	// in tests. The db.instance.id is host:port-derived, so the collapse guard in
	// the helper suppresses the runs_on even on loopback (the id is identical on
	// every host) — the edge is wired for correctness but never materialises here.
	hostID func() string
}

// newEntitySource builds the source for the configured host:port target. The
// db.instance.id encodes the same target the probe connects to, so metrics
// tagged with server.address/server.port join to this entity in the consumer.
func newEntitySource(host string, port int) *mssqlEntitySource {
	instanceID := dbInstanceScheme + host + ":" + strconv.Itoa(port)
	dbID := map[string]any{
		idKeyDBInstance: instanceID,
		idKeyDBSystem:   dbSystemMSSQL,
	}
	s := &mssqlEntitySource{
		hostID: dbcommon.HostID,
		obs: entity.Observation{
			Entities: []entity.Entity{
				{
					Type: entityTypeDB,
					ID:   dbID,
				},
			},
		},
	}
	// runs_on edge: db → host when the db is local (loopback). The host:port id
	// embeds the loopback literal, so the collapse guard refuses it (it would
	// false-join hosts); wired anyway so the gate alone decides correctness.
	if rel, ok := dbcommon.LocalHostRunsOn(dbID, host, s.hostID()); ok {
		s.obs.Relations = append(s.obs.Relations, rel)
	}
	return s
}

// Observe returns the fixed db entity. Always ok=true: the target is known
// from config, independent of whether the server is reachable this cycle
// (reachability rides the senhub.db.up metric, not the entity's presence).
func (s *mssqlEntitySource) Observe() (entity.Observation, bool) {
	return s.obs, true
}

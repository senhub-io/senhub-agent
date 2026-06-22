package oracle

import (
	"sync"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail (#185): the monitored Oracle instance as a "db" entity so a
// consumer (Toise) can render it and join its metrics by identity. The id is the
// credential-free DSN — the SAME value carried on every datapoint's `instance`
// tag, so metrics and the entity share one key.
//
// Identity is single-key {db.instance.id} and immutable; db.system.name is a
// descriptive ATTRIBUTE, never part of the identity. Toise resolves a relation
// endpoint by exact identity match, so an identity key the monitors edge does
// not carry would leave the db unresolvable and the entity would float (#505).
const (
	entityTypeDB    = "db"
	idKeyDBInstance = "db.instance.id"
	attrDBSystem    = "db.system.name"
)

// oracleEntitySource reports the configured instance unconditionally. The probe
// observes a single, statically-known database — there is no discovery.
// Reachability is carried on the metric rail (senhub.db.up), not by retracting
// the entity: a transient outage must not delete the db from the consumer's
// graph.
type oracleEntitySource struct {
	instance string

	mu      sync.Mutex
	version string // v$instance.version, "" until the first cycle reports it
}

func newEntitySource(instance string) *oracleEntitySource {
	return &oracleEntitySource{instance: instance}
}

// setVersion records the server version (v$instance.version) so it rides the
// entity as the descriptive db.system.version attribute (toise#216 AT1).
func (s *oracleEntitySource) setVersion(v string) {
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

// Observe returns the instance observation plus a monitors edge from the agent
// to this db, anchoring the entity to the agent's monitoring subgraph instead of
// leaving it orphaned. The edge is emitted only when the agent id is available
// (entity emission on); a non-materialised From would be buffered then dropped
// by the consumer. Non-blocking; safe to call from the detector goroutine.
func (s *oracleEntitySource) Observe() (entity.Observation, bool) {
	s.mu.Lock()
	version := s.version
	s.mu.Unlock()

	dbID := map[string]any{idKeyDBInstance: s.instance}
	attrs := map[string]any{attrDBSystem: "oracle"}
	if version != "" {
		attrs["db.system.version"] = version
	}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       entityTypeDB,
			ID:         dbID,
			Attributes: attrs,
		}},
	}
	if agentID := agentstate.GetAgentInstanceID(); agentID != "" {
		obs.Relations = append(obs.Relations, entity.Relation{
			Type:     "monitors",
			FromType: "service.instance",
			FromID:   map[string]any{"service.instance.id": agentID},
			ToType:   entityTypeDB,
			ToID:     dbID,
		})
	}
	return obs, true
}

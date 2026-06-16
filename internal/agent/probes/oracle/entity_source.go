package oracle

import (
	"senhub-agent.go/internal/agent/services/entity"
)

// Entity rail (#185): the monitored Oracle instance as a "db" entity so a
// consumer (Toise) can render it and join its metrics by identity. The id
// is the credential-free DSN — the SAME value carried on every datapoint's
// `instance` tag, so metrics and the entity share one key. Identity is
// exact and immutable: db.instance.id is the connection target, never a
// mutable runtime value.
const (
	entityTypeDB    = "db"
	idKeyDBInstance = "db.instance.id"
	idKeyDBSystem   = "db.system.name"
)

// oracleEntitySource reports the configured instance unconditionally. The
// probe observes a single, statically-known database — there is no
// discovery — so the observation is the same every cycle and is always
// trustworthy (ok=true). Reachability is carried on the metric rail
// (senhub.db.up), not by retracting the entity: a transient outage must
// not delete the db from the consumer's graph.
type oracleEntitySource struct {
	obs entity.Observation
}

func newEntitySource(instance string) *oracleEntitySource {
	return &oracleEntitySource{
		obs: entity.Observation{
			Entities: []entity.Entity{{
				Type: entityTypeDB,
				ID: map[string]any{
					idKeyDBInstance: instance,
					idKeyDBSystem:   "oracle",
				},
			}},
		},
	}
}

// Observe returns the static instance observation. Non-blocking; safe to
// call from the detector goroutine.
func (s *oracleEntitySource) Observe() (entity.Observation, bool) {
	return s.obs, true
}

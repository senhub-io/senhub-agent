package chrony

import (
	"sync"

	"senhub-agent.go/internal/agent/services/entity"
)

// chronyEntitySource feeds the entity rail with a single service.instance entity
// for the local chrony daemon. Identity is fixed at construction time — chrony
// is always local; there is no configurable host or port.
// Reachability is updated by the collect cycle: setReachable(true) after a
// successful chronyc tracking run, setReachable(false) on any subprocess error.
type chronyEntitySource struct {
	mu     sync.RWMutex
	up     bool
	attrs  map[string]any
	hostID string
	// rekey retires the pre-#742 constant identity; built on first use, once
	// the host id is known.
	rekey *entity.RekeyAnnouncer
}

// legacyInstanceID is the pre-#742 identity: a constant with no host component,
// byte-identical on every machine, so every host running this probe collapsed
// onto ONE service.instance node — and since each still drew its own runs_on
// edge, that node fanned out to all of them and joined them transitively. Kept
// only so the re-key can name the node it retires.
const legacyInstanceID = "chrony://localhost"

func newChronyEntitySource() *chronyEntitySource {
	return &chronyEntitySource{}
}

// setReachable is called by the collect cycle: true when chronyc returned
// valid output, false on any subprocess error. version may be empty.
// hostID is the stable machine identifier (host.id); may be empty if unavailable.
func (s *chronyEntitySource) setReachable(up bool, version string, hostID string) {
	s.mu.Lock()
	s.up = up
	s.hostID = hostID
	if up {
		attrs := map[string]any{
			"service.name":   "chrony",
			"server.address": "localhost",
		}
		if version != "" {
			attrs["service.version"] = version
		}
		s.attrs = attrs
	}
	s.mu.Unlock()
}

// Observe implements entity.Source. Returns ok=false until the first
// successful collection cycle so a transient startup error does not
// immediately delete the entity in the consumer.
func (s *chronyEntitySource) Observe() (entity.Observation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.up {
		return entity.Observation{}, false
	}
	// No host id, no entity: the identity is built from it, and inventing one
	// is what produced the collapse this replaces. A visible gap beats a node
	// that is wrong on every machine.
	if s.hostID == "" {
		return entity.Observation{}, false
	}

	id := map[string]any{"service.instance.id": "chrony@" + s.hostID}
	obs := entity.Observation{
		Entities: []entity.Entity{{
			Type:       "service.instance",
			ID:         id,
			Attributes: s.attrs,
		}},
	}
	obs.Relations = []entity.Relation{{
		Type:     "runs_on",
		FromType: "service.instance",
		FromID:   id,
		ToType:   "host",
		ToID:     map[string]any{"host.id": s.hostID},
	}}

	// Retire the collapsed node explicitly rather than letting it expire, so
	// the consumer reads "someone decided" instead of "the producer went quiet".
	if s.rekey == nil {
		s.rekey = entity.NewRekeyAnnouncer("service.instance",
			"service.instance.id", legacyInstanceID, "chrony@"+s.hostID)
	}
	s.rekey.Announce()
	if rel, ok := s.rekey.SameAs(); ok {
		obs.Relations = append(obs.Relations, rel)
	}

	return obs, true
}

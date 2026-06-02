package snmppoll

import "testing"

// Lot 1b scaffolds the entity rail: the source is wired but observes
// nothing until the Lot 5 topology walks land. This pins that contract so
// a future change that starts emitting entities does so deliberately.
func TestEntitySource_ObserveEmptyUntilLot5(t *testing.T) {
	src := newEntitySource(&config{Target: "192.0.2.10"})
	obs := src.Observe()
	if len(obs.Entities) != 0 || len(obs.Relations) != 0 {
		t.Errorf("expected empty observation in Lot 1b, got %d entities, %d relations",
			len(obs.Entities), len(obs.Relations))
	}
}

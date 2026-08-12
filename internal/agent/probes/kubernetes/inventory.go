package kubernetes

import "senhub-agent.go/internal/agent/services/entity"

// clusterInventory is the entity view the metric cycle refreshes and the
// entity source reports.
//
// The two rails are polled on different schedules, so the source reads the
// last known state instead of calling the API itself. One read per cycle
// rather than two, and — more importantly — the entity rail cannot disagree
// with the metric rail about what exists at a given instant.
type clusterInventory struct {
	entities  []entity.Entity
	relations []entity.Relation
}

// setInventory replaces the reported set.
//
// Replace, never merge: an entity the cluster no longer reports must disappear
// from the observation so the consumer retires it by absence. Merging would
// keep deleted pods and drained nodes alive forever, which is the failure mode
// the liveness contract exists to prevent.
// A nil receiver is a no-op rather than a panic: the probe constructor always
// builds a source, so nil only occurs in a test exercising the metric path,
// and crashing there would report a fault in code the test is not about.
func (s *k8sEntitySource) setInventory(inv clusterInventory) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inventory = inv
}

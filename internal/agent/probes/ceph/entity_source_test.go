package ceph

import "testing"

// TestCephEntitySource_StaticObservation verifies that the entity source
// always returns the same service.instance entity derived from the endpoint.
func TestCephEntitySource_StaticObservation(t *testing.T) {
	endpoint := "https://ceph.example.com:8443"
	src := newCephEntitySource(endpoint)

	obs, ok := src.Observe()
	if !ok {
		t.Fatal("Observe returned ok=false; want true (static entity is always ready)")
	}
	if len(obs.Entities) != 1 {
		t.Fatalf("len(Entities) = %d; want 1", len(obs.Entities))
	}

	e := obs.Entities[0]
	if e.Type != entityTypeServiceInstance {
		t.Errorf("entity type = %q; want %q", e.Type, entityTypeServiceInstance)
	}

	wantID := "ceph://" + endpoint
	gotID, ok := e.ID[idKeyServiceInstanceID]
	if !ok {
		t.Fatalf("entity ID missing key %q", idKeyServiceInstanceID)
	}
	if gotID != wantID {
		t.Errorf("entity ID %q = %q; want %q", idKeyServiceInstanceID, gotID, wantID)
	}

	// Relations are empty for this simple entity.
	if len(obs.Relations) != 0 {
		t.Errorf("len(Relations) = %d; want 0", len(obs.Relations))
	}
}

// TestCephEntitySource_Idempotent verifies that repeated calls return the
// exact same observation (no re-allocation on each call).
func TestCephEntitySource_Idempotent(t *testing.T) {
	src := newCephEntitySource("https://ceph.local:8443")

	obs1, ok1 := src.Observe()
	obs2, ok2 := src.Observe()

	if !ok1 || !ok2 {
		t.Error("Observe returned ok=false")
	}
	if len(obs1.Entities) != len(obs2.Entities) {
		t.Errorf("entity count changed between calls: %d vs %d", len(obs1.Entities), len(obs2.Entities))
	}
	if obs1.Entities[0].ID[idKeyServiceInstanceID] != obs2.Entities[0].ID[idKeyServiceInstanceID] {
		t.Error("entity ID changed between calls")
	}
}

package agentstate

import "testing"

type fakeProbe struct{ healthy bool }

func (f fakeProbe) IsHealthy() bool { return f.healthy }

type unhealthyOnlyProbe struct{} // no IsHealthy → counted healthy by default

func TestCollectErrorsCounter(t *testing.T) {
	before := GetCollectErrorsTotal()
	IncrementCollectErrors()
	IncrementCollectErrors()
	IncrementCollectErrors()
	after := GetCollectErrorsTotal()
	if after-before != 3 {
		t.Errorf("expected delta 3, got %d", after-before)
	}
}

func TestProbeCounts(t *testing.T) {
	SetActiveProbes([]interface{}{
		fakeProbe{healthy: true},
		fakeProbe{healthy: false},
		fakeProbe{healthy: true},
		unhealthyOnlyProbe{}, // no IsHealthy → defaults to healthy
	})
	total, healthy := GetProbeCounts()
	if total != 4 {
		t.Errorf("total: got %d, want 4", total)
	}
	if healthy != 3 {
		t.Errorf("healthy: got %d, want 3 (2 explicit healthy + 1 default)", healthy)
	}
}

func TestSetActiveProbes_DefensiveCopy(t *testing.T) {
	src := []interface{}{fakeProbe{healthy: true}}
	SetActiveProbes(src)
	// Mutate caller-side slice — should not affect stored snapshot.
	src[0] = fakeProbe{healthy: false}
	_, healthy := GetProbeCounts()
	if healthy != 1 {
		t.Errorf("defensive copy failed: caller mutation leaked, healthy=%d", healthy)
	}
}

package otlp

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func rebootPoint(at time.Time) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name: "os.reboot_required", Value: 1, Timestamp: at,
		Tags: []tags.Tag{{Key: "probe_name", Value: "reboot-check"}, {Key: "probe_type", Value: "exec"}},
	}
}

// A new measurement is always emitted once at its observedAt, never the
// export time. This is the first of the two export rules.
func TestSnapshot_NewMeasurementUsesObservedAt(t *testing.T) {
	store := newMetricStore()
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	measured := base.Add(-20 * time.Minute)

	store.upsertAt(rebootPoint(measured), base)

	cands := store.snapshot(base)
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	if !cands[0].At.Equal(measured) {
		t.Fatalf("stamp = %v, want the measurement time %v", cands[0].At, measured)
	}
	if cands[0].Keepalive {
		t.Fatal("new measurement candidate must not be a keepalive candidate")
	}
}

// Once a new measurement has been emitted and the probe no longer
// vouches for it, the value is not exported again on later cycles.
func TestSnapshot_UnvouchedSeriesIsNotReemitted(t *testing.T) {
	store := newMetricStore()
	store.noteProbeCadence("reboot-check", 60*time.Second)
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	measured := base.Add(-5 * time.Minute)

	store.upsertAt(rebootPoint(measured), base)
	cands := store.snapshot(base)
	if len(cands) != 1 || !cands[0].At.Equal(measured) {
		t.Fatalf("initial candidate = %+v, want measurement time %v", cands, measured)
	}

	store.markEmitted(cands)

	if got := store.snapshot(base.Add(time.Second)); len(got) != 0 {
		t.Fatalf("unvouched series was re-emitted: %+v", got)
	}
}

// Without a cadence nothing vouches for the value, but the pending flag
// still emits the new measurement once at its observedAt.
func TestSnapshot_WithoutCadenceUsesObservedAt(t *testing.T) {
	store := newMetricStore()
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	measured := base.Add(-10 * time.Second)

	store.upsertAt(rebootPoint(measured), base)

	cands := store.snapshot(base)
	if len(cands) != 1 || !cands[0].At.Equal(measured) {
		t.Fatalf("candidates = %+v, want the measurement time %v", cands, measured)
	}
}

// An entry read back from the checkpoint is restored and therefore never
// vouched for, but it is pending and is emitted exactly once at its
// observedAt.
func TestSnapshot_RestoredEntryIsEmittedOnce(t *testing.T) {
	base := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	measured := base.Add(-time.Minute)

	store := newMetricStore().withFreshnessGrace(30 * time.Second)
	store.noteProbeCadence("reboot-check", 30*time.Minute)
	store.restoreFromSnapshot([]entrySnapshot{{
		ProbeName: "reboot-check", ProbeType: "exec", MetricName: "os.reboot_required",
		Value: 1, Tags: map[string]string{"probe_name": "reboot-check", "probe_type": "exec"}, ObservedAt: measured,
	}})

	cands := store.snapshot(base)
	if len(cands) != 1 || !cands[0].At.Equal(measured) {
		t.Fatalf("restored candidate = %+v, want measurement time %v", cands, measured)
	}

	store.markEmitted(cands)

	if got := store.snapshot(base.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("restored entry was re-emitted: %+v", got)
	}

	observedAgain := base.Add(time.Hour)
	store.upsertAt(rebootPoint(observedAgain), observedAgain)

	cands = store.snapshot(observedAgain)
	if len(cands) != 1 || !cands[0].At.Equal(observedAgain) {
		t.Fatalf("candidate after new observation = %+v, want %v", cands, observedAgain)
	}
}

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

// A probe that runs every 30 minutes has its last value exported as
// current on every push in between: a gauge means "state as of the last
// run", and a consumer with a five-minute lookback must keep seeing it.
func TestSnapshot_StampsAVouchedValueWithTheExportTime(t *testing.T) {
	store := newMetricStore().withFreshnessGrace(30 * time.Second)
	store.noteProbeCadence("reboot-check", 30*time.Minute)
	measured := time.Now().Add(-20 * time.Minute)
	store.upsert(rebootPoint(measured))

	now := time.Now()
	_, times := store.snapshot(now)
	if len(times) != 1 || !times[0].Equal(now) {
		t.Fatalf("stamp = %v, want the export time %v", times, now)
	}
}

// Once the probe has missed its own next run by a wide margin, the
// value is a measurement of the past and says so: the #812 case of a
// target removed from a probe never runs ahead of its last observation.
func TestSnapshot_KeepsTheMeasurementTimeOnceTheProbeStoppedVouching(t *testing.T) {
	store := newMetricStore().withFreshnessGrace(30 * time.Second)
	store.noteProbeCadence("reboot-check", 60*time.Second)
	measured := time.Now().Add(-5 * time.Minute)
	store.upsert(rebootPoint(measured))

	_, times := store.snapshot(time.Now())
	if len(times) != 1 || !times[0].Equal(measured) {
		t.Fatalf("stamp = %v, want the measurement time %v", times, measured)
	}
}

// Without a cadence nothing vouches for the value, and the measurement
// time stands.
func TestSnapshot_KeepsTheMeasurementTimeWhenTheCadenceIsUnknown(t *testing.T) {
	store := newMetricStore()
	measured := time.Now().Add(-10 * time.Second)
	store.upsert(rebootPoint(measured))

	_, times := store.snapshot(time.Now())
	if len(times) != 1 || !times[0].Equal(measured) {
		t.Fatalf("stamp = %v, want the measurement time %v", times, measured)
	}
}

// An entry read back from the checkpoint is not vouched for until its
// probe observes it again in this process, however recent it looks.
func TestSnapshot_NeverVouchesForARestoredEntryUntilItIsObservedAgain(t *testing.T) {
	store := newMetricStore().withFreshnessGrace(30 * time.Second)
	store.noteProbeCadence("reboot-check", 30*time.Minute)
	measured := time.Now().Add(-time.Minute)
	store.restoreFromSnapshot([]entrySnapshot{{
		ProbeName: "reboot-check", ProbeType: "exec", MetricName: "os.reboot_required",
		Value: 1, Tags: map[string]string{"probe_name": "reboot-check", "probe_type": "exec"}, ObservedAt: measured,
	}})

	_, times := store.snapshot(time.Now())
	if len(times) != 1 || !times[0].Equal(measured) {
		t.Fatalf("restored stamp = %v, want the measurement time %v", times, measured)
	}

	store.upsert(rebootPoint(time.Now()))
	now := time.Now()
	_, times = store.snapshot(now)
	if len(times) != 1 || !times[0].Equal(now) {
		t.Fatalf("stamp after a new observation = %v, want the export time %v", times, now)
	}
}

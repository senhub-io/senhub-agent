package zabbix

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func updatesPoint(at time.Time) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name: "os.updates.pending", Value: 3, Timestamp: at,
		Tags: []tags.Tag{{Key: "probe_name", Value: "os_updates"}, {Key: "probe_type", Value: "os_updates"}},
	}
}

// An hourly probe's value stays available between two runs. Dropped
// after three push intervals, it reached the server ninety seconds an
// hour: a host registered in between waited an hour for its first value.
func TestStoreKeepsAValueItsProbeStillVouchesFor(t *testing.T) {
	st := newStore()
	st.noteProbeCadence("os_updates", time.Hour)
	st.upsert(updatesPoint(time.Now().Add(-40 * time.Minute)))

	if got, _ := st.snapshot(time.Now(), 90*time.Second); len(got) != 1 {
		t.Fatalf("an hourly value 40 minutes old was dropped; %d series left", len(got))
	}
}

// Once the probe has missed its next run by half a cadence, the value is
// no longer vouched for and the server is left to report it stale.
func TestStoreDropsAValueOnceItsProbeMissedItsRun(t *testing.T) {
	st := newStore()
	st.noteProbeCadence("os_updates", time.Hour)
	st.upsert(updatesPoint(time.Now().Add(-2 * time.Hour)))

	if got, _ := st.snapshot(time.Now(), 90*time.Second); len(got) != 0 {
		t.Fatalf("a value two cadences old is still sent; %d series left", len(got))
	}
}

// Without a known cadence the push allowance alone applies.
func TestStoreKeepsThePushAllowanceWhenTheCadenceIsUnknown(t *testing.T) {
	st := newStore()
	st.upsert(updatesPoint(time.Now().Add(-5 * time.Minute)))

	if got, _ := st.snapshot(time.Now(), 90*time.Second); len(got) != 0 {
		t.Fatalf("a value past the push allowance is still sent; %d series left", len(got))
	}
}

func redisPoint(name string, v float64) datapoint.DataPoint {
	return datapoint.DataPoint{Name: name, Value: v, Tags: []tags.Tag{
		{Key: "probe_name", Value: "lab-redis"}, {Key: "probe_type", Value: "redis"},
	}}
}

// A target that goes down leaves its probe reporting only that it is
// down: the next run stops the other values from being pushed as
// current, while discovery keeps them so Zabbix does not disable the
// items for the length of the outage (#951).
func TestARunStopsPushingWhatItNoLongerReports(t *testing.T) {
	st := newStore()
	st.noteProbeCadence("lab-redis", time.Minute)
	t0 := time.Now().Add(-time.Minute)
	st.upsertAt(redisPoint("redis_up", 1), t0)
	st.upsertAt(redisPoint("redis_uptime", 2335), t0)
	st.retireSuperseded(map[string]bool{"lab-redis": true}, t0)

	t1 := t0.Add(time.Minute)
	st.upsertAt(redisPoint("redis_up", 0), t1)
	st.retireSuperseded(map[string]bool{"lab-redis": true}, t1)

	current, discovered := st.snapshot(time.Now(), 10*time.Minute)
	if len(current) != 1 || current[0].MetricName != "redis_up" {
		t.Errorf("pushed %v, want only redis_up", current)
	}
	if len(discovered) != 2 {
		t.Errorf("discovery keeps %d series, want both", len(discovered))
	}

	// The target comes back: the series is pushed again.
	st.upsertAt(redisPoint("redis_uptime", 12), t1.Add(time.Minute))
	if current, _ := st.snapshot(time.Now(), 10*time.Minute); len(current) != 2 {
		t.Errorf("a series reported again is not pushed: %v", current)
	}
}

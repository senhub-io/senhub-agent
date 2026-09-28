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

	if got := st.snapshot(time.Now(), 90*time.Second); len(got) != 1 {
		t.Fatalf("an hourly value 40 minutes old was dropped; %d series left", len(got))
	}
}

// Once the probe has missed its next run by half a cadence, the value is
// no longer vouched for and the server is left to report it stale.
func TestStoreDropsAValueOnceItsProbeMissedItsRun(t *testing.T) {
	st := newStore()
	st.noteProbeCadence("os_updates", time.Hour)
	st.upsert(updatesPoint(time.Now().Add(-2 * time.Hour)))

	if got := st.snapshot(time.Now(), 90*time.Second); len(got) != 0 {
		t.Fatalf("a value two cadences old is still sent; %d series left", len(got))
	}
}

// Without a known cadence the push allowance alone applies.
func TestStoreKeepsThePushAllowanceWhenTheCadenceIsUnknown(t *testing.T) {
	st := newStore()
	st.upsert(updatesPoint(time.Now().Add(-5 * time.Minute)))

	if got := st.snapshot(time.Now(), 90*time.Second); len(got) != 0 {
		t.Fatalf("a value past the push allowance is still sent; %d series left", len(got))
	}
}

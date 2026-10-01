package zabbix

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

var _ data_store.ProbeRetireSink = (*Strategy)(nil)

func forgetPoint(probe string) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name: "os.updates.pending", Value: 3, Timestamp: time.Now(),
		Tags: []tags.Tag{{Key: "probe_name", Value: probe}, {Key: "probe_type", Value: "os_updates"}},
	}
}

// A probe removed, renamed or disabled by a reload stops running, but its
// last values stayed in the store, pushed and discovered until they aged
// out.
func TestForgetProbesStopsPushingAndDiscoveringARetiredProbe(t *testing.T) {
	s := New(configuration.StorageConfigParams{}, testLogger(), nil)
	s.NoteProbeCadence("Removed_Probe", time.Hour)
	for _, p := range []string{"Removed_Probe", "kept"} {
		if err := s.AddDataPoints([]datapoint.DataPoint{forgetPoint(p)}); err != nil {
			t.Fatal(err)
		}
	}

	s.ForgetProbes([]string{"removed_probe"})

	current, discovered := s.store.snapshot(time.Now(), 10*time.Minute)
	if len(current) != 1 || current[0].ProbeName != "kept" {
		t.Fatalf("next push holds %+v, want only the kept probe", current)
	}
	if len(discovered) != 1 || discovered[0].ProbeName != "kept" {
		t.Fatalf("discovery holds %+v, want only the kept probe", discovered)
	}
	if _, ok := s.store.cadences["Removed_Probe"]; ok {
		t.Error("the retired probe's cadence is kept")
	}
}

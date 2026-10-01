package otlp

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

var _ data_store.ProbeRetireSink = (*OTLPSyncStrategy)(nil)

func forgetPoint(probe string) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name: "os.reboot_required", Value: 1, Timestamp: time.Now(),
		Tags: []tags.Tag{{Key: "probe_name", Value: probe}, {Key: "probe_type", Value: "exec"}},
	}
}

// A probe removed, renamed or disabled by a reload stops running, but its
// last values stayed in the store and were pushed until they aged out.
func TestForgetProbesStopsExportingARetiredProbe(t *testing.T) {
	s := newTestStrategy(t, nil)
	s.NoteProbeCadence("Removed_Probe", time.Hour)
	for _, p := range []string{"Removed_Probe", "kept"} {
		if err := s.AddDataPoints([]datapoint.DataPoint{forgetPoint(p)}); err != nil {
			t.Fatal(err)
		}
	}

	s.ForgetProbes([]string{"removed_probe"})

	cms, _ := s.store.snapshot(time.Now())
	if len(cms) != 1 || cms[0].ProbeName != "kept" {
		t.Fatalf("next export holds %+v, want only the kept probe", cms)
	}
	if n := s.store.probeSeriesCount("Removed_Probe"); n != 0 {
		t.Errorf("%d series still counted for the retired probe", n)
	}
	if _, ok := s.store.cadence["Removed_Probe"]; ok {
		t.Error("the retired probe's cadence is kept")
	}
}

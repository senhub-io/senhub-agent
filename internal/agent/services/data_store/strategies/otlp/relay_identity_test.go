package otlp

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

var relayHostProbes = []string{"cpu", "memory", "logicaldisk", "network"}

func ownPoint(probe string) datapoint.DataPoint {
	return datapoint.DataPoint{
		Name: "system_" + probe + "_value", Value: 1, Timestamp: time.Now(),
		Tags: []tags.Tag{
			{Key: "probe_name", Value: probe},
			{Key: "probe_type", Value: probe},
		},
	}
}

// relayedPoint is what the receiver forwarded before it owned the identity:
// the source's probe tags first, the receiver's appended after.
func relayedPoint(probe, host string, duplicated bool) datapoint.DataPoint {
	tg := []tags.Tag{{Key: "host", Value: host}}
	if duplicated {
		tg = append(tg,
			tags.Tag{Key: "probe_name", Value: probe},
			tags.Tag{Key: "probe_type", Value: probe},
		)
	}
	tg = append(tg,
		tags.Tag{Key: "probe_name", Value: "otlp-relay"},
		tags.Tag{Key: "probe_type", Value: "otlp_receiver"},
	)
	return datapoint.DataPoint{Name: "system_" + probe + "_value", Value: 99, Timestamp: time.Now(), Tags: tg}
}

func relaySeriesCount(s *OTLPSyncStrategy) int {
	n := 0
	cms, _ := s.store.snapshot(time.Now())
	for _, cm := range cms {
		if cm.ProbeName == "otlp-relay" {
			n++
		}
	}
	return n
}

func TestRelay_SourcesBehindRelayNeverEnterStoreNorEvictOwnProbes(t *testing.T) {
	for _, duplicated := range []bool{true, false} {
		s := &OTLPSyncStrategy{store: newMetricStore(), metricsRelay: &metricsRelay{}}
		for _, p := range relayHostProbes {
			s.NoteProbeCadence(p, 20*time.Millisecond)
		}
		for cycle := 0; cycle < 4; cycle++ {
			var batch []datapoint.DataPoint
			for _, p := range relayHostProbes {
				batch = append(batch, ownPoint(p))
			}
			if err := s.AddDataPoints(batch); err != nil {
				t.Fatal(err)
			}
			time.Sleep(15 * time.Millisecond)
			var relayed []datapoint.DataPoint
			for _, p := range relayHostProbes {
				relayed = append(relayed,
					relayedPoint(p, "win-source", duplicated),
					relayedPoint(p, "linux-source", duplicated))
			}
			if err := s.AddDataPoints(relayed); err != nil {
				t.Fatal(err)
			}
			if got := relaySeriesCount(s); got != 0 {
				t.Fatalf("duplicated=%v cycle %d: %d otlp-relay series in the store, want 0", duplicated, cycle, got)
			}
			if got := s.store.size(); got != len(relayHostProbes) {
				t.Fatalf("duplicated=%v cycle %d: store holds %d series, want the relay's own %d", duplicated, cycle, got, len(relayHostProbes))
			}
		}
	}
}

func TestRelay_RelayedPointsEvictNothingWithoutRelayActive(t *testing.T) {
	s := &OTLPSyncStrategy{store: newMetricStore()}
	if err := s.AddDataPoints([]datapoint.DataPoint{relayedPoint("cpu", "src", true)}); err != nil {
		t.Fatal(err)
	}
	if got := relaySeriesCount(s); got != 1 {
		t.Fatalf("without a relay the store is the route: %d otlp-relay series, want 1", got)
	}
}

func TestDataPointTag_LastOccurrenceWins(t *testing.T) {
	dp := relayedPoint("cpu", "h", true)
	if got := dataPointTag(dp, "probe_type"); got != "otlp_receiver" {
		t.Errorf("probe_type = %q, want otlp_receiver", got)
	}
}

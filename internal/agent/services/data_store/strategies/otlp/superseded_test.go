package otlp

import (
	"sort"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func redisPoint(name string, v float64) datapoint.DataPoint {
	return datapoint.DataPoint{Name: name, Value: v, Tags: []tags.Tag{
		{Key: "probe_name", Value: "lab-redis"},
		{Key: "probe_type", Value: "redis"},
	}}
}

func storedNames(s *metricStore) []string {
	cms, _ := s.snapshot(time.Now())
	out := make([]string, 0, len(cms))
	for _, cm := range cms {
		out = append(out, cm.MetricName)
	}
	sort.Strings(out)
	return out
}

// A target that goes down leaves its probe running and reporting only
// that it is down. The series the previous run reported must stop being
// exported as current with the next run, not minutes later (#951).
func TestARunRetiresTheSeriesItNoLongerReports(t *testing.T) {
	s := newMetricStore()
	s.noteProbeCadence("lab-redis", time.Minute)
	t0 := time.Now().Add(-time.Minute)
	for _, dp := range []datapoint.DataPoint{redisPoint("redis_up", 1), redisPoint("redis_uptime", 2335), redisPoint("redis_clients", 1)} {
		s.upsertAt(dp, t0)
	}
	s.retireSuperseded(map[string]bool{"lab-redis": true}, t0)

	t1 := t0.Add(time.Minute)
	s.upsertAt(redisPoint("redis_up", 0), t1)
	s.retireSuperseded(map[string]bool{"lab-redis": true}, t1)

	if got := storedNames(s); len(got) != 1 || got[0] != "redis_up" {
		t.Errorf("after the target went down the store holds %v, want only redis_up", got)
	}
	if n := s.probeSeriesCount("lab-redis"); n != 1 {
		t.Errorf("probe series count = %d, want 1", n)
	}
}

// One run may reach the store as several batches a few seconds apart;
// the later batch must not retire the earlier one.
func TestBatchesOfOneRunDoNotRetireEachOther(t *testing.T) {
	s := newMetricStore()
	s.noteProbeCadence("lab-redis", time.Minute)
	t0 := time.Now()
	s.upsertAt(redisPoint("redis_up", 1), t0)
	s.retireSuperseded(map[string]bool{"lab-redis": true}, t0)
	s.upsertAt(redisPoint("redis_uptime", 2335), t0.Add(3*time.Second))
	s.retireSuperseded(map[string]bool{"lab-redis": true}, t0.Add(3*time.Second))

	if got := storedNames(s); len(got) != 2 {
		t.Errorf("store holds %v, want both batches of the run", got)
	}
}

// Without a known cadence there is no run to compare with: nothing is
// retired, and another probe's batch never touches this one.
func TestRetirementNeedsTheProbesOwnCadence(t *testing.T) {
	s := newMetricStore()
	t0 := time.Now().Add(-time.Hour)
	s.upsertAt(redisPoint("redis_uptime", 2335), t0)
	s.retireSuperseded(map[string]bool{"lab-redis": true}, time.Now())
	if got := storedNames(s); len(got) != 1 {
		t.Errorf("a probe with no cadence lost %v", got)
	}

	s.noteProbeCadence("lab-redis", time.Minute)
	s.retireSuperseded(map[string]bool{"other": true}, time.Now())
	if got := storedNames(s); len(got) != 1 {
		t.Errorf("another probe's batch retired lab-redis series: %v", got)
	}
}

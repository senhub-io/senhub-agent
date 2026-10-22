package otlp

import (
	"time"

	"senhub-agent.go/internal/agent/services/data_store/otelmapper"
)

// snapshotMetrics is the metrics of the candidates snapshot selects at now.
func snapshotMetrics(s *metricStore, now time.Time) []otelmapper.CacheMetric {
	cands := s.snapshot(now)
	out := make([]otelmapper.CacheMetric, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.Metric)
	}
	return out
}

package http

import (
	"testing"
)

// An aggregated metric reads like a separate one. It used to repeat the
// status the plugin line already opens with, "OK - OK: cpu_usage_total
// 2.50% (aggregated from 1 metrics)", in another order than the metrics
// beside it.
func TestAnAggregatedMetricReadsLikeASeparateOne(t *testing.T) {
	m := &MetricsProcessor{}
	def := NagiosMetric{Channel: "cpu_usage_total", Aggregation: "max", Unit: "%", Warning: "80", Critical: "90"}

	one := m.processNagiosMetricAggregated(def, []CachedMetric{{Value: 2.5}}, NagiosOverrides{})
	if one.Message != "cpu_usage_total: OK 2.50%" {
		t.Errorf("one series: %q", one.Message)
	}
	several := m.processNagiosMetricAggregated(def, []CachedMetric{{Value: 2.5}, {Value: 95.0}}, NagiosOverrides{})
	if several.Message != "cpu_usage_total: CRITICAL 95.00% (max of 2 series)" {
		t.Errorf("several series: %q", several.Message)
	}
}

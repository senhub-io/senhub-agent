package http

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// prtg_metric_id is private routing for the legacy PRTG push; it reached
// Prometheus as a label carrying its unexpanded [name] template.
func TestCacheDropsPrivateTags(t *testing.T) {
	baseLogger := createTestLogger()
	cache := NewMetricCache(5*time.Minute, logger.NewModuleLogger(baseLogger, "test"))
	cache.AddDataPointsWithTransformer([]datapoint.DataPoint{{
		Name: "dnstime", Value: 1, Timestamp: time.Now(),
		Tags: []tags.Tag{
			{Key: "probe_name", Value: "site"}, {Key: "probe_type", Value: "load_webapp"},
			{Key: "prtg_metric_id", Value: "https_example.com_[name]", Private: true},
		},
	}}, transformers.NewTransformerRegistry(baseLogger))
	metrics := cache.GetProbeMetrics("site")
	if len(metrics) != 1 {
		t.Fatalf("cache holds %d series, want 1", len(metrics))
	}
	for _, m := range metrics {
		if _, leaked := m.Tags["prtg_metric_id"]; leaked {
			t.Errorf("private tag reached the pull cache: %v", m.Tags)
		}
	}
}

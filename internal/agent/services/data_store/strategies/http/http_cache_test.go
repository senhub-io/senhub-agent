// Package http provides HTTP strategy for data storage
package http

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// createTestModuleLogger creates a module logger for cache testing
func createTestModuleLogger() *logger.ModuleLogger {
	baseLogger := createTestLogger()
	return logger.NewModuleLogger(baseLogger, "test.cache")
}

// TestTimeSeriesKeyUniqueness validates that keys are unique when they should be
// according to the Universal Uniqueness Rule (RUU):
// "Une clé de série temporelle DOIT être unique SI ET SEULEMENT SI
// les valeurs des métriques collectées à cet instant peuvent être DIFFÉRENTES"
func TestTimeSeriesKeyUniqueness(t *testing.T) {
	tests := []struct {
		name          string
		probe         string // Used as fallback if probe_name not in tags
		metric        string
		tags1         map[string]string
		tags2         map[string]string
		shouldBeEqual bool
		reason        string
	}{
		{
			name:   "CPU - Different cores must have different keys",
			probe:  "cpu",
			metric: "usage_percent",
			tags1: map[string]string{
				"probe_name": "cpu",
				"host":       "server1",
				"core":       "0",
			},
			tags2: map[string]string{
				"probe_name": "cpu",
				"host":       "server1",
				"core":       "1",
			},
			shouldBeEqual: false,
			reason:        "Different CPU cores have different usage values",
		},
		{
			name:   "CPU - Different probe instances must have different keys",
			probe:  "cpu",
			metric: "usage_percent",
			tags1: map[string]string{
				"probe_name": "server1_cpu", // Different probe instance
				"core":       "0",
			},
			tags2: map[string]string{
				"probe_name": "server2_cpu", // Different probe instance
				"core":       "0",
			},
			shouldBeEqual: false,
			reason:        "Different probe instances (server1_cpu vs server2_cpu) collect different values",
		},
		{
			name:   "Network - Different interfaces must have different keys",
			probe:  "network",
			metric: "bytes_sent",
			tags1: map[string]string{
				"probe_name": "network",
				"interface":  "eth0",
			},
			tags2: map[string]string{
				"probe_name": "network",
				"interface":  "eth1",
			},
			shouldBeEqual: false,
			reason:        "Different network interfaces have different traffic metrics",
		},
		{
			name:   "LogicalDisk - Different drives must have different keys",
			probe:  "logicaldisk",
			metric: "free_space_bytes",
			tags1: map[string]string{
				"probe_name": "logicaldisk",
				"drive":      "C:",
			},
			tags2: map[string]string{
				"probe_name": "logicaldisk",
				"drive":      "D:",
			},
			shouldBeEqual: false,
			reason:        "Different drives have different free space values",
		},
		{
			name:   "Redfish - Different controllers must have different keys",
			probe:  "redfish",
			metric: "storage.controller.status",
			tags1: map[string]string{
				"probe_name": "redfish",
				"endpoint":   "https://server.com",
				"controller": "0",
			},
			tags2: map[string]string{
				"probe_name": "redfish",
				"endpoint":   "https://server.com",
				"controller": "1",
			},
			shouldBeEqual: false,
			reason:        "Different storage controllers have independent status values",
		},
		{
			name:   "Redfish - Different drives must have different keys",
			probe:  "redfish",
			metric: "storage.drive.temperature",
			tags1: map[string]string{
				"probe_name": "redfish",
				"endpoint":   "https://server.com",
				"drive_id":   "disk.bay.0",
			},
			tags2: map[string]string{
				"probe_name": "redfish",
				"endpoint":   "https://server.com",
				"drive_id":   "disk.bay.1",
			},
			shouldBeEqual: false,
			reason:        "Different drives have independent temperature readings",
		},
		{
			name:   "Memory - System-level metric must have same key",
			probe:  "memory",
			metric: "available_bytes",
			tags1: map[string]string{
				"probe_name": "memory",
				"host":       "server1",
			},
			tags2: map[string]string{
				"probe_name": "memory",
				"host":       "server1",
				"platform":   "windows",
			},
			shouldBeEqual: true,
			reason:        "Memory is system-level, no discriminant tags, contextual tags don't affect key",
		},
		{
			name:   "WebApp - Different URLs must have different keys",
			probe:  "webapp",
			metric: "response_time_ms",
			tags1: map[string]string{
				"probe_name": "webapp",
				"url":        "https://api.example.com",
			},
			tags2: map[string]string{
				"probe_name": "webapp",
				"url":        "https://www.example.com",
			},
			shouldBeEqual: false,
			reason:        "Different URLs have different response times",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMetricCache(5*time.Minute, createTestModuleLogger())

			// Extract probe_name from tags (mimics real AddDataPointsWithTransformer behavior)
			probeName1 := tt.tags1["probe_name"]
			probeName2 := tt.tags2["probe_name"]

			// If probe_name not in tags, use the probe parameter
			if probeName1 == "" {
				probeName1 = tt.probe
			}
			if probeName2 == "" {
				probeName2 = tt.probe
			}

			key1 := cache.generateTimeSeriesKey(probeName1, probeName1, tt.metric, tt.tags1)
			key2 := cache.generateTimeSeriesKey(probeName2, probeName2, tt.metric, tt.tags2)

			if tt.shouldBeEqual {
				if key1 != key2 {
					t.Errorf("Keys should be equal but differ:\n  key1=%s\n  key2=%s\n  Reason: %s",
						key1, key2, tt.reason)
				}
			} else {
				if key1 == key2 {
					t.Errorf("Keys should be different but are equal: %s\n  Reason: %s",
						key1, tt.reason)
				}
			}
		})
	}
}

// TestKeyStabilityAcrossInfrastructureChanges validates that keys remain stable
// when contextual tags change (endpoint, host attributes, etc.)
func TestKeyStabilityAcrossInfrastructureChanges(t *testing.T) {
	tests := []struct {
		name   string
		probe  string
		metric string
		tags1  map[string]string
		tags2  map[string]string
		reason string
	}{
		{
			name:   "Redfish - Endpoint change should not affect key",
			probe:  "redfish",
			metric: "storage.drive.temperature",
			tags1: map[string]string{
				"probe_name": "redfish",
				"endpoint":   "https://server.com",
				"drive_id":   "disk.bay.0",
			},
			tags2: map[string]string{
				"probe_name": "redfish",
				"endpoint":   "https://server-new.com", // Endpoint changed (DNS migration)
				"drive_id":   "disk.bay.0",             // Same drive
			},
			reason: "Same drive on same server should maintain time series continuity despite endpoint change",
		},
		{
			name:   "CPU - Platform tag change should not affect key",
			probe:  "cpu",
			metric: "usage_percent",
			tags1: map[string]string{
				"probe_name": "cpu",
				"core":       "0",
				"platform":   "windows",
			},
			tags2: map[string]string{
				"probe_name": "cpu",
				"core":       "0",
				"platform":   "linux", // Platform info updated
			},
			reason: "Same CPU core should maintain time series regardless of platform tag updates",
		},
		{
			name:   "Network - Additional contextual tags should not affect key",
			probe:  "network",
			metric: "bytes_sent",
			tags1: map[string]string{
				"probe_name": "network",
				"interface":  "eth0",
			},
			tags2: map[string]string{
				"probe_name": "network",
				"interface":  "eth0",
				"ip_address": "192.168.1.10", // New contextual tag added
				"mac":        "00:11:22:33:44:55",
			},
			reason: "Same interface should maintain time series when additional metadata is added",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMetricCache(5*time.Minute, createTestModuleLogger())

			// Extract probe_name from tags (mimics real AddDataPointsWithTransformer behavior)
			probeName1 := tt.tags1["probe_name"]
			probeName2 := tt.tags2["probe_name"]

			// If probe_name not in tags, use the probe parameter
			if probeName1 == "" {
				probeName1 = tt.probe
			}
			if probeName2 == "" {
				probeName2 = tt.probe
			}

			key1 := cache.generateTimeSeriesKey(probeName1, probeName1, tt.metric, tt.tags1)
			key2 := cache.generateTimeSeriesKey(probeName2, probeName2, tt.metric, tt.tags2)

			if key1 != key2 {
				t.Errorf("Keys should remain stable but changed:\n  key1=%s\n  key2=%s\n  Reason: %s",
					key1, key2, tt.reason)
			}
		})
	}
}

// TestContextualTagsPreservedInMetadata validates that all tags (including contextual)
// are preserved in CachedMetric.Tags for filtering purposes
func TestContextualTagsPreservedInMetadata(t *testing.T) {
	moduleLogger := createTestModuleLogger()
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	transformerRegistry := transformers.NewTransformerRegistry(moduleLogger.Logger)

	// Create datapoint with both discriminant and contextual tags
	dataPoints := []datapoint.DataPoint{
		{
			Name:  "storage.drive.temperature",
			Value: 42.5,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
				{Key: "drive_id", Value: "disk.bay.0"},         // Discriminant
				{Key: "endpoint", Value: "https://server.com"}, // Contextual
				{Key: "vendor", Value: "Dell"},                 // Contextual
				{Key: "model", Value: "PowerEdge R740"},        // Contextual
			},
		},
	}

	cache.AddDataPointsWithTransformer(dataPoints, transformerRegistry)

	// Retrieve all metrics
	metrics := cache.GetAllMetrics()

	if len(metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(metrics))
	}

	metric := metrics[0]

	// Verify all tags are preserved in metadata
	expectedTags := map[string]string{
		"probe_name": "redfish",
		"drive_id":   "disk.bay.0",
		"endpoint":   "https://server.com",
		"vendor":     "Dell",
		"model":      "PowerEdge R740",
	}

	for key, expectedValue := range expectedTags {
		actualValue, exists := metric.Tags[key]
		if !exists {
			t.Errorf("Tag '%s' not found in metric.Tags (required for filtering)", key)
		} else if actualValue != expectedValue {
			t.Errorf("Tag '%s' value mismatch: expected '%s', got '%s'", key, expectedValue, actualValue)
		}
	}
}

// TestNoCollisionsInRealisticScenarios validates realistic scenarios from production environments
func TestNoCollisionsInRealisticScenarios(t *testing.T) {
	moduleLogger := createTestModuleLogger()
	cache := NewMetricCache(5*time.Minute, moduleLogger)

	// Scenario: 2 Redfish probes monitoring same server with different names
	// This is the exact scenario from the user's initial issue
	dataPoints := []datapoint.DataPoint{
		// Probe 1: "redfish"
		{
			Name:  "storage.drive.temperature",
			Value: 42.5,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"}, // Different probe name
				{Key: "drive_id", Value: "disk.bay.0"},
				{Key: "endpoint", Value: "https://lb-me5024mgmt1.batistyl.fr"},
			},
		},
		// Probe 2: "baie_production"
		{
			Name:  "storage.drive.temperature",
			Value: 42.5, // Same value, but different probe
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "baie_production"}, // Different probe name
				{Key: "drive_id", Value: "disk.bay.0"},
				{Key: "endpoint", Value: "https://lb-me5024mgmt1.batistyl.fr"}, // Same endpoint
			},
		},
	}

	transformerRegistry := transformers.NewTransformerRegistry(moduleLogger.Logger)
	cache.AddDataPointsWithTransformer(dataPoints, transformerRegistry)

	// Should have 2 distinct time series (different probe_name = different collection source)
	metrics := cache.GetAllMetrics()
	if len(metrics) != 2 {
		t.Errorf("Expected 2 distinct time series (different probes), got %d", len(metrics))
	}

	// Verify both probes are indexed
	probeStats := cache.GetProbeStatistics()
	if len(probeStats) != 2 {
		t.Errorf("Expected 2 probes in statistics, got %d", len(probeStats))
	}

	// Verify each probe has 1 metric
	if stat, ok := probeStats["redfish"]; !ok || stat.MetricsCount != 1 {
		t.Errorf("Probe 'redfish' should have 1 metric")
	}
	if stat, ok := probeStats["baie_production"]; !ok || stat.MetricsCount != 1 {
		t.Errorf("Probe 'baie_production' should have 1 metric")
	}
}

// TestMultiInstanceMetricsCardinality validates cardinality calculations for multi-instance metrics
func TestMultiInstanceMetricsCardinality(t *testing.T) {
	scenarios := []struct {
		name                string
		probe               string
		instances           int
		metricsPerInstance  int
		expectedCardinality int
		description         string
	}{
		{
			name:                "CPU with 8 cores",
			probe:               "cpu",
			instances:           8,
			metricsPerInstance:  5, // usage_percent, user_time, system_time, idle_time, iowait_time
			expectedCardinality: 8 * 5,
			description:         "8 cores × 5 metrics = 40 time series",
		},
		{
			name:                "Network with 2 interfaces",
			probe:               "network",
			instances:           2,
			metricsPerInstance:  8, // bytes_sent, bytes_recv, packets_sent, packets_recv, errors_in, errors_out, drops_in, drops_out
			expectedCardinality: 2 * 8,
			description:         "2 interfaces × 8 metrics = 16 time series",
		},
		{
			name:                "Redfish storage with 12 drives",
			probe:               "redfish",
			instances:           12,
			metricsPerInstance:  6, // status, temperature, capacity, health, rebuild_progress, etc.
			expectedCardinality: 12 * 6,
			description:         "12 drives × 6 metrics = 72 time series",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			moduleLogger := createTestModuleLogger()
			cache := NewMetricCache(5*time.Minute, moduleLogger)
			transformerRegistry := transformers.NewTransformerRegistry(moduleLogger.Logger)

			var dataPoints []datapoint.DataPoint

			// Generate realistic multi-instance data
			for i := 0; i < scenario.instances; i++ {
				for m := 0; m < scenario.metricsPerInstance; m++ {
					dp := datapoint.DataPoint{
						Name:  generateMetricName(scenario.probe, m),
						Value: float64(i*10 + m),
						Tags:  generateInstanceTags(scenario.probe, i),
					}
					dataPoints = append(dataPoints, dp)
				}
			}

			cache.AddDataPointsWithTransformer(dataPoints, transformerRegistry)

			metrics := cache.GetAllMetrics()
			actualCardinality := len(metrics)

			if actualCardinality != scenario.expectedCardinality {
				t.Errorf("Cardinality mismatch for %s:\n  Expected: %d\n  Actual: %d\n  Description: %s",
					scenario.name, scenario.expectedCardinality, actualCardinality, scenario.description)
			}
		})
	}
}

// Helper function to generate metric names for test scenarios
func generateMetricName(probe string, index int) string {
	metricNames := map[string][]string{
		"cpu":     {"usage_percent", "user_time", "system_time", "idle_time", "iowait_time"},
		"network": {"bytes_sent", "bytes_recv", "packets_sent", "packets_recv", "errors_in", "errors_out", "drops_in", "drops_out"},
		"redfish": {"storage.drive.status", "storage.drive.temperature", "storage.drive.capacity", "storage.drive.health", "storage.drive.rebuild_progress", "storage.drive.predictive_failure"},
	}

	if names, ok := metricNames[probe]; ok && index < len(names) {
		return names[index]
	}
	return "unknown_metric"
}

// Helper function to generate instance-specific tags for test scenarios
func generateInstanceTags(probe string, instance int) []tags.Tag {
	baseTags := []tags.Tag{
		{Key: "probe_name", Value: probe},
	}

	switch probe {
	case "cpu":
		baseTags = append(baseTags, tags.Tag{Key: "core", Value: string(rune('0' + instance))})
	case "network":
		baseTags = append(baseTags, tags.Tag{Key: "interface", Value: "eth" + string(rune('0'+instance))})
	case "redfish":
		baseTags = append(baseTags, tags.Tag{Key: "drive_id", Value: "disk.bay." + string(rune('0'+instance))})
	}

	return baseTags
}

// TestKeyGenerationConsistency validates that key generation is deterministic
func TestKeyGenerationConsistency(t *testing.T) {
	cache := NewMetricCache(5*time.Minute, createTestModuleLogger())

	probe := "network"
	metric := "bytes_sent"
	tags := map[string]string{
		"probe_name": "network",
		"interface":  "eth0",
		"status":     "up",
		"speed":      "1000",
	}

	// Generate key multiple times
	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = cache.generateTimeSeriesKey(probe, probe, metric, tags)
	}

	// All keys should be identical
	firstKey := keys[0]
	for i, key := range keys {
		if key != firstKey {
			t.Errorf("Key generation not consistent at iteration %d:\n  Expected: %s\n  Got: %s",
				i, firstKey, key)
		}
	}
}

// TestDiscriminantVsContextualTagsInKey validates that only discriminant tags appear in the key
// while contextual tags are excluded (but preserved in CachedMetric.Tags for filtering)
func TestDiscriminantVsContextualTagsInKey(t *testing.T) {
	cache := NewMetricCache(5*time.Minute, createTestModuleLogger())

	tags := map[string]string{
		"probe_name": "redfish",
		"drive_id":   "disk.bay.0",         // Discriminant (identifies unique drive)
		"endpoint":   "https://server.com", // Contextual (metadata, not identifying)
		"vendor":     "Dell",               // Contextual (metadata)
	}

	key := cache.generateTimeSeriesKey("redfish", "redfish", "storage.drive.temperature", tags)

	// Expected key format with discriminant tags registry:
	// "redfish:storage.drive.temperature:drive_id=disk.bay.0"
	// Only drive_id (discriminant) should appear, not endpoint/vendor (contextual)

	expectedKey := "redfish:storage.drive.temperature:drive_id=disk.bay.0"
	if key != expectedKey {
		t.Errorf("Key mismatch:\n  Expected: %s\n  Got: %s", expectedKey, key)
	}

	// Verify discriminant tag IS in key
	if !containsSubstring(key, "drive_id=disk.bay.0") {
		t.Errorf("Discriminant tag 'drive_id' should appear in key: %s", key)
	}

	// Verify contextual tags are NOT in key
	if containsSubstring(key, "endpoint=") {
		t.Errorf("Contextual tag 'endpoint' should NOT appear in key: %s", key)
	}
	if containsSubstring(key, "vendor=") {
		t.Errorf("Contextual tag 'vendor' should NOT appear in key: %s", key)
	}

	// Verify probe_name is not in tags portion (it's in the prefix)
	tagsPortion := key[len("redfish:storage.drive.temperature:"):]
	if containsSubstring(tagsPortion, "probe_name=") {
		t.Errorf("probe_name should not appear in tags portion of key: %s", key)
	}
}

// TestFullTagKey_ExternalOriginProbesDoNotCollapse pins the fix for the
// OTLP-receiver / prometheus_scrape collapse: their label sets are arbitrary
// and externally controlled, so the cache keys them on the full tag set. Two
// datapoints under one metric name that differ in ANY attribute (here a
// histogram bucket's `le`, and an arbitrary resource attribute) must produce
// distinct keys — otherwise all but the last collapse onto one cache slot and
// vanish from the PRTG / Nagios / Prometheus pull sinks.
func TestFullTagKey_ExternalOriginProbesDoNotCollapse(t *testing.T) {
	cache := NewMetricCache(5*time.Minute, createTestModuleLogger())

	for _, probeType := range []string{"otlp_receiver", "prometheus_scrape"} {
		base := map[string]string{
			"probe_name":  probeType,
			"probe_type":  probeType,
			"metric_type": "otlp_ingest",
			"otel_type":   "counter",
			"host.name":   "edge-01",
		}
		withLe := func(le string) map[string]string {
			m := map[string]string{"le": le}
			for k, v := range base {
				m[k] = v
			}
			return m
		}

		k1 := cache.generateTimeSeriesKey(probeType, probeType, "http.server.duration_bucket", withLe("0.1"))
		k2 := cache.generateTimeSeriesKey(probeType, probeType, "http.server.duration_bucket", withLe("0.5"))
		kInf := cache.generateTimeSeriesKey(probeType, probeType, "http.server.duration_bucket", withLe("+Inf"))
		if k1 == k2 || k1 == kInf || k2 == kInf {
			t.Errorf("%s: bucket series collapsed — keys must differ by le: %q %q %q", probeType, k1, k2, kInf)
		}

		// An arbitrary resource attribute must also split the series.
		other := map[string]string{}
		for k, v := range base {
			other[k] = v
		}
		other["host.name"] = "edge-02"
		kHostA := cache.generateTimeSeriesKey(probeType, probeType, "requests", base)
		kHostB := cache.generateTimeSeriesKey(probeType, probeType, "requests", other)
		if kHostA == kHostB {
			t.Errorf("%s: series collapsed across differing host.name: %q == %q", probeType, kHostA, kHostB)
		}

		// The full tag set (minus probe identity) is in the key.
		if !containsSubstring(k1, "le=0.1") || !containsSubstring(k1, "host.name=edge-01") {
			t.Errorf("%s: full tag key missing expected tags: %q", probeType, k1)
		}
	}
}

// TestFullTagKey_SeparatorEscapingNoAlias pins the #662 fix: an
// externally-controlled tag value containing the `,`/`=` separators must not
// alias two distinct series onto one cache slot.
func TestFullTagKey_SeparatorEscapingNoAlias(t *testing.T) {
	cache := NewMetricCache(5*time.Minute, createTestModuleLogger())
	// Unescaped, both would join to "...:a=1,b=2".
	k1 := cache.generateTimeSeriesKey("otlp_receiver", "otlp_receiver", "m", map[string]string{"a": "1,b=2"})
	k2 := cache.generateTimeSeriesKey("otlp_receiver", "otlp_receiver", "m", map[string]string{"a": "1", "b": "2"})
	if k1 == k2 {
		t.Errorf("distinct tag sets must not alias after escaping: %q == %q", k1, k2)
	}
	// A separator-free value keeps the key byte-identical (checkpoint continuity).
	plain := cache.generateTimeSeriesKey("otlp_receiver", "otlp_receiver", "m", map[string]string{"host.name": "edge-01"})
	if !containsSubstring(plain, "host.name=edge-01") {
		t.Errorf("separator-free value must be unescaped, got %q", plain)
	}
}

// Helper function for string contains check
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && indexOfSubstring(s, substr) >= 0
}

func indexOfSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

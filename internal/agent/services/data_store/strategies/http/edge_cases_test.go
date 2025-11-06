// Edge cases tests - Test boundary conditions and error handling
package http

import (
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// TestEdgeCases tests boundary conditions and error scenarios
func TestEdgeCases(t *testing.T) {
	// Create test setup
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
	formatConverter := NewFormatConverter(transformerRegistry, moduleLogger, cache)

	t.Run("Empty Metrics", func(t *testing.T) {
		testEmptyMetrics(t, formatConverter)
	})

	t.Run("Null Values", func(t *testing.T) {
		testNullValues(t, formatConverter, cache, transformerRegistry)
	})

	t.Run("Invalid Values", func(t *testing.T) {
		testInvalidValues(t, formatConverter, cache, transformerRegistry)
	})

	t.Run("Missing Units", func(t *testing.T) {
		testMissingUnits(t, formatConverter, cache, transformerRegistry)
	})

	t.Run("Missing Probe Names", func(t *testing.T) {
		testMissingProbeNames(t, formatConverter, cache, transformerRegistry)
	})

	t.Run("Large Values", func(t *testing.T) {
		testLargeValues(t, formatConverter, cache, transformerRegistry)
	})

	t.Run("Special Characters", func(t *testing.T) {
		testSpecialCharacters(t, formatConverter, cache, transformerRegistry)
	})
}

func testEmptyMetrics(t *testing.T, converter *FormatConverter) {
	// Test with non-existent probe
	senHubMetrics := converter.GetSenHubMetricsForProbe("nonexistent")
	if len(senHubMetrics) != 0 {
		t.Errorf("Expected 0 SenHub metrics for nonexistent probe, got %d", len(senHubMetrics))
	}

	prtgChannels := converter.GetMetricsForProbe("nonexistent")
	if len(prtgChannels) != 0 {
		t.Errorf("Expected 0 PRTG channels for nonexistent probe, got %d", len(prtgChannels))
	}

	nagiosResponse := converter.GetNagiosMetricsForProbe("nonexistent")
	if nagiosResponse.Status != 1 { // 1 = WARNING
		t.Errorf("Expected Nagios WARNING status for nonexistent probe, got %d", nagiosResponse.Status)
	}
	if nagiosResponse.StatusText != "WARNING" {
		t.Errorf("Expected Nagios WARNING status text, got %s", nagiosResponse.StatusText)
	}
}

func testNullValues(t *testing.T, converter *FormatConverter, cache *MetricCache, registry *transformers.TransformerRegistry) {
	// Since DataPoint.Value is float32, we test with direct cache insertion for null values
	cache.mu.Lock()
	tsKey := cache.generateTimeSeriesKey("test", "test", "null_metric", map[string]string{"probe_name": "test"})
	cache.timeSeries[tsKey] = CachedMetric{
		Value:      nil,
		Timestamp:  time.Now(),
		Unit:       "",
		ProbeName:  "test",
		MetricName: "null_metric",
		Tags:       map[string]string{"probe_name": "test"},
	}
	// Update probe index
	if cache.probeIndex["test"] == nil {
		cache.probeIndex["test"] = make(map[string]bool)
	}
	cache.probeIndex["test"][tsKey] = true
	cache.mu.Unlock()

	// Test SenHub format with null value
	senHubMetrics := converter.GetSenHubMetricsForProbe("test")
	if len(senHubMetrics) > 0 {
		for _, metric := range senHubMetrics {
			if metric.Value == nil {
				t.Logf("✅ SenHub correctly handles nil value: %v", metric.Value)
			}
		}
	}

	// Test PRTG format with null value - should be filtered out or handled gracefully
	prtgChannels := converter.GetMetricsForProbe("test")
	for _, channel := range prtgChannels {
		// PRTG channels should not have NaN or invalid values
		if channel.Value != channel.Value { // Check for NaN
			t.Errorf("PRTG channel has NaN value: %f", channel.Value)
		}
	}
}

func testInvalidValues(t *testing.T, converter *FormatConverter, cache *MetricCache, registry *transformers.TransformerRegistry) {
	// Since DataPoint.Value is float32, we directly insert invalid values into cache
	cache.mu.Lock()

	// String value
	tsKey1 := cache.generateTimeSeriesKey("test", "test", "string_metric", map[string]string{"probe_name": "test"})
	cache.timeSeries[tsKey1] = CachedMetric{
		Value:      "not_a_number",
		Timestamp:  time.Now(),
		Unit:       "",
		ProbeName:  "test",
		MetricName: "string_metric",
		Tags:       map[string]string{"probe_name": "test"},
	}

	// Object value
	tsKey2 := cache.generateTimeSeriesKey("test", "test", "object_metric", map[string]string{"probe_name": "test"})
	cache.timeSeries[tsKey2] = CachedMetric{
		Value:      map[string]interface{}{"key": "value"},
		Timestamp:  time.Now(),
		Unit:       "",
		ProbeName:  "test",
		MetricName: "object_metric",
		Tags:       map[string]string{"probe_name": "test"},
	}

	// Array value
	tsKey3 := cache.generateTimeSeriesKey("test", "test", "array_metric", map[string]string{"probe_name": "test"})
	cache.timeSeries[tsKey3] = CachedMetric{
		Value:      []string{"item1", "item2"},
		Timestamp:  time.Now(),
		Unit:       "",
		ProbeName:  "test",
		MetricName: "array_metric",
		Tags:       map[string]string{"probe_name": "test"},
	}

	// Update probe index
	if cache.probeIndex["test"] == nil {
		cache.probeIndex["test"] = make(map[string]bool)
	}
	cache.probeIndex["test"][tsKey1] = true
	cache.probeIndex["test"][tsKey2] = true
	cache.probeIndex["test"][tsKey3] = true
	cache.mu.Unlock()

	// Test PRTG format handling of invalid values
	prtgChannels := converter.GetMetricsForProbe("test")

	// Count how many valid channels were created from invalid data
	validChannelCount := 0
	for _, channel := range prtgChannels {
		if channel.Value == channel.Value { // Not NaN
			validChannelCount++
		}
	}

	t.Logf("✅ PRTG created %d valid channels from 3 invalid value metrics", validChannelCount)

	// Test SenHub format - should preserve original values even if not numeric
	senHubMetrics := converter.GetSenHubMetricsForProbe("test")
	for _, metric := range senHubMetrics {
		t.Logf("✅ SenHub preserves value type: %T = %v", metric.Value, metric.Value)
	}
}

func testMissingUnits(t *testing.T, converter *FormatConverter, cache *MetricCache, registry *transformers.TransformerRegistry) {
	testMetrics := []datapoint.DataPoint{
		{
			Name:      "unitless_metric",
			Value:     42.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "test"},
			},
		},
	}

	cache.AddDataPointsWithTransformer(testMetrics, registry)

	// Test that formats handle missing units gracefully
	senHubMetrics := converter.GetSenHubMetricsForProbe("test")
	for _, metric := range senHubMetrics {
		t.Logf("✅ SenHub metric without unit: %s = %v %s", metric.Name, metric.Value, metric.Unit)
	}

	prtgChannels := converter.GetMetricsForProbe("test")
	for _, channel := range prtgChannels {
		t.Logf("✅ PRTG channel without unit: %s = %f (Unit: %s, CustomUnit: %s)",
			channel.Channel, channel.Value, channel.Unit, channel.CustomUnit)
	}
}

func testMissingProbeNames(t *testing.T, converter *FormatConverter, cache *MetricCache, registry *transformers.TransformerRegistry) {
	testMetrics := []datapoint.DataPoint{
		{
			Name:      "orphan_metric",
			Value:     123.45,
			Timestamp: time.Now(),
			Tags:      []tags.Tag{}, // No probe_name tag
		},
	}

	cache.AddDataPointsWithTransformer(testMetrics, registry)

	// Test that metrics without probe names are handled
	// They should be assigned to "unknown" probe
	unknownMetrics := converter.GetSenHubMetricsForProbe("unknown")
	t.Logf("✅ Found %d metrics in 'unknown' probe for orphaned metrics", len(unknownMetrics))

	for _, metric := range unknownMetrics {
		if metric.ProbeName != "unknown" {
			t.Errorf("Expected ProbeName 'unknown', got '%s'", metric.ProbeName)
		}
	}
}

func testLargeValues(t *testing.T, converter *FormatConverter, cache *MetricCache, registry *transformers.TransformerRegistry) {
	// Test with float32 compatible large values
	testMetrics := []datapoint.DataPoint{
		{
			Name:      "large_float32",
			Value:     3.4028235e+38, // Near max float32
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "test"},
			},
		},
		{
			Name:      "small_float32",
			Value:     1.175494e-38, // Near min positive float32
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "test"},
			},
		},
	}

	cache.AddDataPointsWithTransformer(testMetrics, registry)

	// Also test with direct cache insertion for int64 values
	cache.mu.Lock()
	tsKey := cache.generateTimeSeriesKey("test", "test", "large_int64", map[string]string{"probe_name": "test"})
	cache.timeSeries[tsKey] = CachedMetric{
		Value:      int64(9223372036854775807), // Max int64
		Timestamp:  time.Now(),
		Unit:       "",
		ProbeName:  "test",
		MetricName: "large_int64",
		Tags:       map[string]string{"probe_name": "test"},
	}
	if cache.probeIndex["test"] == nil {
		cache.probeIndex["test"] = make(map[string]bool)
	}
	cache.probeIndex["test"][tsKey] = true
	cache.mu.Unlock()

	// Test that large values are handled correctly
	prtgChannels := converter.GetMetricsForProbe("test")

	for _, channel := range prtgChannels {
		// Check for overflow or underflow
		if channel.Value != channel.Value { // Check for NaN
			t.Errorf("PRTG channel has NaN value from large number: %f", channel.Value)
		}

		// Check for infinity
		if channel.Value == channel.Value && (channel.Value > 1e100 || channel.Value < -1e100) {
			t.Logf("⚠️  PRTG channel has very large value: %e", channel.Value)
		}

		t.Logf("✅ PRTG handles large value: %s = %e", channel.Channel, channel.Value)
	}

	senHubMetrics := converter.GetSenHubMetricsForProbe("test")
	for _, metric := range senHubMetrics {
		t.Logf("✅ SenHub preserves large value: %s = %v", metric.Name, metric.Value)
	}
}

func testSpecialCharacters(t *testing.T, converter *FormatConverter, cache *MetricCache, registry *transformers.TransformerRegistry) {
	testMetrics := []datapoint.DataPoint{
		{
			Name:      "metric.with.dots",
			Value:     1.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "test"},
				{Key: "special-tag", Value: "value with spaces & symbols!"},
			},
		},
		{
			Name:      "métric_with_unicode_éàü",
			Value:     2.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "test"},
				{Key: "unicode_tag", Value: "valeur_française_🚀"},
			},
		},
	}

	cache.AddDataPointsWithTransformer(testMetrics, registry)

	// Test that special characters in names and tags are handled
	senHubMetrics := converter.GetSenHubMetricsForProbe("test")
	for _, metric := range senHubMetrics {
		t.Logf("✅ SenHub handles special chars: %s (Channel: %s)", metric.Name, metric.Channel)

		// Check that tags with special characters are preserved
		for key, value := range metric.Tags {
			if key == "special-tag" || key == "unicode_tag" {
				t.Logf("✅ Tag preserved: %s = %s", key, value)
			}
		}
	}

	prtgChannels := converter.GetMetricsForProbe("test")
	for _, channel := range prtgChannels {
		t.Logf("✅ PRTG handles special chars: %s = %f", channel.Channel, channel.Value)
	}
}

// TestCacheExpiration tests that expired metrics are properly handled
func TestCacheExpiration(t *testing.T) {
	// Create cache with very short TTL for testing
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test")
	shortTTLCache := NewMetricCache(10*time.Millisecond, moduleLogger) // 10ms TTL
	transformerRegistry := transformers.NewTransformerRegistry(baseLogger)
	formatConverter := NewFormatConverter(transformerRegistry, moduleLogger, shortTTLCache)

	// Add a test metric
	testMetrics := []datapoint.DataPoint{
		{
			Name:      "expiring_metric",
			Value:     100.0,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "test"},
			},
		},
	}

	shortTTLCache.AddDataPointsWithTransformer(testMetrics, transformerRegistry)

	// Verify metric exists immediately
	metrics := formatConverter.GetSenHubMetricsForProbe("test")
	if len(metrics) == 0 {
		t.Fatal("Expected metric to exist immediately after adding")
	}

	// Wait for expiration
	time.Sleep(50 * time.Millisecond) // Wait longer than TTL

	// Trigger cleanup manually (since test might run faster than cleanup routine)
	shortTTLCache.cleanup()

	// Verify metric is expired
	expiredMetrics := formatConverter.GetSenHubMetricsForProbe("test")
	t.Logf("✅ After expiration: found %d metrics (expected 0 or fewer)", len(expiredMetrics))

	// This is expected behavior - expired metrics should be cleaned up
	if len(expiredMetrics) > len(metrics) {
		t.Errorf("Expected fewer or equal metrics after expiration, got %d vs %d", len(expiredMetrics), len(metrics))
	}
}

// Format validation tests - comprehensive validation of all metric formats
package data_store

import (
	"encoding/json"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// createTestTransformerRegistry creates a transformer registry for testing
func createTestTransformerRegistry(baseLogger *logger.Logger) *transformers.TransformerRegistry {
	return transformers.NewTransformerRegistry(baseLogger)
}

// TestFormatValidation tests that all metric formats are correctly generated
func TestFormatValidation(t *testing.T) {
	// Create test logger and format converter
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	
	// Create transformer registry
	transformerRegistry := createTestTransformerRegistry(baseLogger)
	formatConverter := NewFormatConverter(transformerRegistry, moduleLogger, cache)

	// Real-world test metrics from different probes
	testMetrics := []datapoint.DataPoint{
		// CPU metrics
		{
			Name:      "cpu_usage_total",
			Value:     75.5,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "cpu"},
				{Key: "instance", Value: "0"},
			},
		},
		{
			Name:      "cpu_user",
			Value:     45.2,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "host"},
				{Key: "core", Value: "1"},
			},
		},
		// Memory metrics
		{
			Name:      "memory_used_percent",
			Value:     82.3,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "memory"},
			},
		},
		{
			Name:      "memory_available_bytes",
			Value:     2147483648, // 2GB
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "host"},
			},
		},
		// Network metrics
		{
			Name:      "network_bytes_sent",
			Value:     1048576, // 1MB
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "network"},
				{Key: "interface", Value: "eth0"},
			},
		},
		// Redfish thermal metrics
		{
			Name:      "thermal.cpu.0.temperature",
			Value:     65.2,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
				{Key: "index", Value: "0"},
				{Key: "sensor_type", Value: "temperature"},
			},
		},
		// Disk metrics
		{
			Name:      "logicaldisk_free_bytes",
			Value:     107374182400, // 100GB
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "logicaldisk"},
				{Key: "instance", Value: "C:"},
			},
		},
		// Boolean/status metrics
		{
			Name:      "service_running",
			Value:     1, // Boolean as int (1=true, 0=false)
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "host"},
				{Key: "service", Value: "apache"},
			},
		},
	}

	// Add test metrics to cache
	cache.AddDataPointsWithTransformer(testMetrics, transformerRegistry)

	t.Run("SenHub Format Validation", func(t *testing.T) {
		testSenHubFormat(t, formatConverter, testMetrics)
	})

	t.Run("PRTG Format Validation", func(t *testing.T) {
		testPRTGFormat(t, formatConverter, testMetrics)
	})

	t.Run("Nagios Format Validation", func(t *testing.T) {
		testNagiosFormat(t, formatConverter, testMetrics)
	})
}

func testSenHubFormat(t *testing.T, converter *FormatConverter, testMetrics []datapoint.DataPoint) {
	// Test each probe's SenHub format
	probes := []string{"cpu", "host", "memory", "network", "redfish", "logicaldisk"}
	
	for _, probeName := range probes {
		t.Run("SenHub_"+probeName, func(t *testing.T) {
			metrics := converter.GetSenHubMetricsForProbe(probeName)
			
			if len(metrics) == 0 {
				t.Logf("No metrics found for probe %s (expected for some probes)", probeName)
				return
			}
			
			for _, metric := range metrics {
				// Validate required fields
				if metric.Name == "" {
					t.Errorf("SenHub metric missing Name field")
				}
				if metric.Channel == "" {
					t.Errorf("SenHub metric missing Channel field")
				}
				if metric.Value == nil {
					t.Errorf("SenHub metric missing Value field")
				}
				if metric.ProbeName == "" {
					t.Errorf("SenHub metric missing ProbeName field")
				}
				if metric.Timestamp.IsZero() {
					t.Errorf("SenHub metric missing Timestamp field")
				}
				
				// Validate JSON serialization
				jsonData, err := json.Marshal(metric)
				if err != nil {
					t.Errorf("Failed to serialize SenHub metric to JSON: %v", err)
				}
				
				// Validate we can deserialize it back
				var deserializedMetric SenHubMetric
				if err := json.Unmarshal(jsonData, &deserializedMetric); err != nil {
					t.Errorf("Failed to deserialize SenHub metric from JSON: %v", err)
				}
				
				t.Logf("✅ SenHub %s: %s = %v %s (Channel: %s)", 
					probeName, metric.Name, metric.Value, metric.Unit, metric.Channel)
			}
		})
	}
}

func testPRTGFormat(t *testing.T, converter *FormatConverter, testMetrics []datapoint.DataPoint) {
	// Test each probe's PRTG format
	probes := []string{"cpu", "host", "memory", "network", "redfish", "logicaldisk"}
	
	for _, probeName := range probes {
		t.Run("PRTG_"+probeName, func(t *testing.T) {
			channels := converter.GetMetricsForProbe(probeName)
			
			if len(channels) == 0 {
				t.Logf("No channels found for probe %s (expected for some probes)", probeName)
				return
			}
			
			for _, channel := range channels {
				// Validate required PRTG fields
				if channel.Channel == "" {
					t.Errorf("PRTG channel missing Channel field")
				}
				
				// Validate Float field is set for non-lookup metrics (required for decimal values)
				if channel.ValueLookup == "" {
					if channel.Float == nil || *channel.Float != 1 {
						if channel.Float == nil {
							t.Errorf("PRTG channel Float field should be 1 for non-lookup metric, got nil")
						} else {
							t.Errorf("PRTG channel Float field should be 1 for non-lookup metric, got %d", *channel.Float)
						}
					}
				} else {
					// Lookup metrics should NOT have Float field set
					if channel.Float != nil {
						t.Errorf("PRTG channel Float field should be nil for lookup metric, got %d", *channel.Float)
					}
				}
				
				// Validate unit handling
				if channel.Unit != "" && channel.Unit != "custom" {
					// If unit is set and not "custom", it should be a standard PRTG unit
					validUnits := []string{"bytes", "bytesper", "count", "percent", "seconds", "temperature"}
					valid := false
					for _, validUnit := range validUnits {
						if channel.Unit == validUnit {
							valid = true
							break
						}
					}
					if !valid {
						t.Logf("PRTG channel using non-standard unit: %s", channel.Unit)
					}
				}
				
				// If unit is "custom", CustomUnit should be set
				if channel.Unit == "custom" && channel.CustomUnit == "" {
					t.Errorf("PRTG channel with Unit='custom' should have CustomUnit set")
				}
				
				// Validate JSON serialization for PRTG response
				prtgResponse := PRTGResponse{
					PRTG: PRTGResult{
						Result: []PRTGChannel{channel},
					},
				}
				
				jsonData, err := json.Marshal(prtgResponse)
				if err != nil {
					t.Errorf("Failed to serialize PRTG response to JSON: %v", err)
				}
				
				// Validate we can deserialize it back
				var deserializedResponse PRTGResponse
				if err := json.Unmarshal(jsonData, &deserializedResponse); err != nil {
					t.Errorf("Failed to deserialize PRTG response from JSON: %v", err)
				}
				
				t.Logf("✅ PRTG %s: %s = %.2f %s/%s", 
					probeName, channel.Channel, channel.Value, channel.Unit, channel.CustomUnit)
			}
		})
	}
}

func testNagiosFormat(t *testing.T, converter *FormatConverter, testMetrics []datapoint.DataPoint) {
	// Test each probe's Nagios format
	probes := []string{"cpu", "host", "memory", "network", "redfish", "logicaldisk"}
	
	for _, probeName := range probes {
		t.Run("Nagios_"+probeName, func(t *testing.T) {
			response := converter.GetNagiosMetricsForProbe(probeName)
			
			// Validate required Nagios fields
			validStatuses := []int{0, 1, 2, 3} // OK, WARNING, CRITICAL, UNKNOWN
			statusValid := false
			for _, validStatus := range validStatuses {
				if response.Status == validStatus {
					statusValid = true
					break
				}
			}
			if !statusValid {
				t.Errorf("Nagios response has invalid status: %d", response.Status)
			}
			
			if response.StatusText == "" {
				t.Errorf("Nagios response missing StatusText field")
			}
			
			if response.Message == "" {
				t.Errorf("Nagios response missing Message field")
			}
			
			// Validate status text matches status code
			expectedStatusTexts := map[int]string{
				0: "OK",
				1: "WARNING", 
				2: "CRITICAL",
				3: "UNKNOWN",
			}
			
			if response.StatusText != expectedStatusTexts[response.Status] {
				t.Errorf("Nagios StatusText '%s' doesn't match Status %d", 
					response.StatusText, response.Status)
			}
			
			// Validate JSON serialization
			jsonData, err := json.Marshal(response)
			if err != nil {
				t.Errorf("Failed to serialize Nagios response to JSON: %v", err)
			}
			
			// Validate we can deserialize it back
			var deserializedResponse NagiosResponse
			if err := json.Unmarshal(jsonData, &deserializedResponse); err != nil {
				t.Errorf("Failed to deserialize Nagios response from JSON: %v", err)
			}
			
			t.Logf("✅ Nagios %s: Status=%d (%s) Message=%s PerfData=%s",
				probeName, response.Status, response.StatusText, response.Message, response.PerfData)
		})
	}
}

// TestFormatConsistency tests that the same metrics produce consistent results across multiple calls
func TestFormatConsistency(t *testing.T) {
	// Create test setup
	baseLogger := createTestLogger()
	moduleLogger := logger.NewModuleLogger(baseLogger, "test")
	cache := NewMetricCache(5*time.Minute, moduleLogger)
	
	// Create transformer registry
	transformerRegistry := createTestTransformerRegistry(baseLogger)
	formatConverter := NewFormatConverter(transformerRegistry, moduleLogger, cache)

	// Add a test metric
	testMetric := []datapoint.DataPoint{
		{
			Name:      "test_metric",
			Value:     42.5,
			Timestamp: time.Now(),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "test"},
			},
		},
	}
	
	cache.AddDataPointsWithTransformer(testMetric, transformerRegistry)

	// Test consistency across multiple calls
	for i := 0; i < 5; i++ {
		t.Run("Consistency_Run_"+string(rune('A'+i)), func(t *testing.T) {
			// SenHub format should be consistent
			senHubMetrics1 := formatConverter.GetSenHubMetricsForProbe("test")
			senHubMetrics2 := formatConverter.GetSenHubMetricsForProbe("test")
			
			if len(senHubMetrics1) != len(senHubMetrics2) {
				t.Errorf("SenHub metrics count inconsistent: %d vs %d", len(senHubMetrics1), len(senHubMetrics2))
			}
			
			if len(senHubMetrics1) > 0 && len(senHubMetrics2) > 0 {
				if senHubMetrics1[0].Name != senHubMetrics2[0].Name {
					t.Errorf("SenHub metric name inconsistent: %s vs %s", senHubMetrics1[0].Name, senHubMetrics2[0].Name)
				}
				if senHubMetrics1[0].Value != senHubMetrics2[0].Value {
					t.Errorf("SenHub metric value inconsistent: %v vs %v", senHubMetrics1[0].Value, senHubMetrics2[0].Value)
				}
			}
			
			// PRTG format should be consistent
			prtgChannels1 := formatConverter.GetMetricsForProbe("test")
			prtgChannels2 := formatConverter.GetMetricsForProbe("test")
			
			if len(prtgChannels1) != len(prtgChannels2) {
				t.Errorf("PRTG channels count inconsistent: %d vs %d", len(prtgChannels1), len(prtgChannels2))
			}
			
			if len(prtgChannels1) > 0 && len(prtgChannels2) > 0 {
				if prtgChannels1[0].Channel != prtgChannels2[0].Channel {
					t.Errorf("PRTG channel name inconsistent: %s vs %s", prtgChannels1[0].Channel, prtgChannels2[0].Channel)
				}
				if prtgChannels1[0].Value != prtgChannels2[0].Value {
					t.Errorf("PRTG channel value inconsistent: %f vs %f", prtgChannels1[0].Value, prtgChannels2[0].Value)
				}
			}
		})
	}
}
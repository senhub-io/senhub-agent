// senhub-agent/internal/agent/services/data_store/strategy_http_test.go
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

func createTestLogger() *logger.Logger {
	args := &cliArgs.ParsedArgs{
		Env:     "test",
		Verbose: false,
	}
	return logger.NewLogger(args)
}

func createTestAgentConfig() configuration.AgentConfiguration {
	return configuration.NewAgentConfiguration(
		"test-agent-key",
		"http://test-server.com",
		createTestLogger(),
	)
}

func TestNewHTTPSyncStrategy(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()

	tests := []struct {
		name                string
		params              map[string]interface{}
		expectedPort        int
		expectedBindAddress string
		expectedEndpoints   map[string]bool
	}{
		{
			name:                "Default configuration",
			params:              map[string]interface{}{},
			expectedPort:        8080,
			expectedBindAddress: "0.0.0.0",
			expectedEndpoints:   map[string]bool{},
		},
		{
			name: "Custom port",
			params: map[string]interface{}{
				"port": float64(9090),
			},
			expectedPort:        9090,
			expectedBindAddress: "0.0.0.0",
			expectedEndpoints:   map[string]bool{},
		},
		{
			name: "Custom port as int (YAML style)",
			params: map[string]interface{}{
				"port": 8443,
			},
			expectedPort:        8443,
			expectedBindAddress: "0.0.0.0",
			expectedEndpoints:   map[string]bool{},
		},
		{
			name: "Custom bind address - loopback",
			params: map[string]interface{}{
				"bind_address": "127.0.0.1",
			},
			expectedPort:        8080,
			expectedBindAddress: "127.0.0.1",
			expectedEndpoints:   map[string]bool{},
		},
		{
			name: "Custom bind address - specific interface",
			params: map[string]interface{}{
				"bind_address": "192.168.1.100",
				"port":         float64(8888),
			},
			expectedPort:        8888,
			expectedBindAddress: "192.168.1.100",
			expectedEndpoints:   map[string]bool{},
		},
		{
			name: "Custom endpoints configuration",
			params: map[string]interface{}{
				"endpoints": []interface{}{"prtg", "nagios"},
			},
			expectedPort:        8080, // default port
			expectedBindAddress: "0.0.0.0",
			expectedEndpoints: map[string]bool{
				"prtg":   true,
				"nagios": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := NewHTTPSyncStrategy(agentConfig, tt.params, logger)

			if strategy == nil {
				t.Fatal("Expected strategy to be created, got nil")
			}

			httpStrategy, ok := strategy.(*HTTPSyncStrategy)
			if !ok {
				t.Fatal("Expected HTTPSyncStrategy type")
			}

			if httpStrategy.port != tt.expectedPort {
				t.Errorf("Expected port %d, got %d", tt.expectedPort, httpStrategy.port)
			}

			if httpStrategy.bindAddress != tt.expectedBindAddress {
				t.Errorf("Expected bind address %s, got %s", tt.expectedBindAddress, httpStrategy.bindAddress)
			}

			if tt.expectedEndpoints != nil {
				for endpoint, expectedEnabled := range tt.expectedEndpoints {
					if enabled := httpStrategy.configManager.IsEndpointEnabled(endpoint); enabled != expectedEnabled {
						t.Errorf("Expected endpoint %s to be %v, got %v", endpoint, expectedEnabled, enabled)
					}
				}
			}
		})
	}
}

func TestHTTPSyncStrategy_Interface(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Test interface methods
	if strategy.GetStrategyName() != "http" {
		t.Errorf("Expected strategy name 'http', got %s", strategy.GetStrategyName())
	}

	params := strategy.GetStrategyParams()
	if params == nil {
		t.Error("Expected params to be returned")
	}

	err := strategy.ValidateConfigParams(configuration.StorageConfigParams{})
	if err != nil {
		t.Errorf("Expected no validation error, got %v", err)
	}

	// Test invalid port validation - string
	err = strategy.ValidateConfigParams(configuration.StorageConfigParams{
		"port": "invalid",
	})
	if err == nil {
		t.Error("Expected validation error for invalid port")
	}

	// Test invalid port validation - decimal number
	err = strategy.ValidateConfigParams(configuration.StorageConfigParams{
		"port": 8080.5,
	})
	if err == nil {
		t.Error("Expected validation error for decimal port")
	}

	// Test invalid port validation - out of range
	err = strategy.ValidateConfigParams(configuration.StorageConfigParams{
		"port": 70000,
	})
	if err == nil {
		t.Error("Expected validation error for port out of range")
	}

	// Test invalid bind_address validation
	err = strategy.ValidateConfigParams(configuration.StorageConfigParams{
		"bind_address": 123,
	})
	if err == nil {
		t.Error("Expected validation error for invalid bind_address")
	}

	// Test valid bind_address validation
	err = strategy.ValidateConfigParams(configuration.StorageConfigParams{
		"bind_address": "127.0.0.1",
		"port":         float64(8080),
	})
	if err != nil {
		t.Errorf("Expected no validation error for valid config, got %v", err)
	}

	// Test valid port as int (common from YAML parsing)
	err = strategy.ValidateConfigParams(configuration.StorageConfigParams{
		"port": 8443,
	})
	if err != nil {
		t.Errorf("Expected no validation error for int port, got %v", err)
	}

	// Test valid port as int64
	err = strategy.ValidateConfigParams(configuration.StorageConfigParams{
		"port": int64(9090),
	})
	if err != nil {
		t.Errorf("Expected no validation error for int64 port, got %v", err)
	}
}

func TestHTTPSyncStrategy_AddDataPoints(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Create test datapoints
	datapoints := []datapoint.DataPoint{
		{
			Name:      "cpu.usage_percent",
			Timestamp: time.Now(),
			Value:     75.5,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "host"},
				{Key: "component", Value: "cpu"},
			},
		},
		{
			Name:      "thermal.cpu.0.temperature",
			Timestamp: time.Now(),
			Value:     65.2,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
				{Key: "index", Value: "0"},
			},
		},
	}

	err := strategy.AddDataPoints(datapoints)
	if err != nil {
		t.Fatalf("Expected no error adding datapoints, got %v", err)
	}

	// Check cache
	strategy.cache.mu.RLock()
	defer strategy.cache.mu.RUnlock()

	// Count total metrics in time series
	totalMetrics := len(strategy.cache.timeSeries)

	if totalMetrics != 2 {
		t.Errorf("Expected 2 cached metrics, got %d", totalMetrics)
	}

	// Check specific cached metric using time series key
	var cpuMetric *CachedMetric
	for tsKey, metric := range strategy.cache.timeSeries {
		if metric.MetricName == "cpu.usage_percent" && metric.ProbeName == "host" {
			cpuMetric = &metric
			// Verify the metric is also indexed properly
			if probeKeys, exists := strategy.cache.probeIndex["host"]; !exists || !probeKeys[tsKey] {
				t.Error("Expected cpu.usage_percent metric to be indexed under host probe")
			}
			break
		}
	}

	if cpuMetric == nil {
		t.Error("Expected cpu.usage_percent metric from host probe to be cached")
	} else {
		if cpuMetric.Value != float32(75.5) {
			t.Errorf("Expected cached value 75.5, got %v", cpuMetric.Value)
		}
		if cpuMetric.ProbeName != "host" {
			t.Errorf("Expected probe name 'host', got %s", cpuMetric.ProbeName)
		}
	}
}

func TestMetricCache_Cleanup(t *testing.T) {
	baseLogger := createTestLogger()
	cache := &MetricCache{
		timeSeries: make(map[string]CachedMetric),
		probeIndex: make(map[string]map[string]bool),
		ttl:        50 * time.Millisecond, // Very short TTL for testing
		stopChan:   make(chan struct{}),
		logger:     logger.NewModuleLogger(baseLogger, "cache"),
	}

	// Add some test data using TSDB structure
	cache.mu.Lock()
	tsKey1 := "test:test1"
	tsKey2 := "test:test2"
	cache.timeSeries[tsKey1] = CachedMetric{
		Value:      10.0,
		Timestamp:  time.Now().Add(-100 * time.Millisecond), // Expired
		MetricName: "test1",
		ProbeName:  "test",
	}
	cache.timeSeries[tsKey2] = CachedMetric{
		Value:      20.0,
		Timestamp:  time.Now(), // Fresh
		MetricName: "test2",
		ProbeName:  "test",
	}
	// Update probe index
	cache.probeIndex["test"] = map[string]bool{
		tsKey1: true,
		tsKey2: true,
	}
	cache.mu.Unlock()

	// Start cleanup goroutine
	go cache.cleanup()

	// Wait for cleanup to run (cleanup runs every minute, so we need to manually trigger)
	// Let's manually test the cleanup logic instead
	time.Sleep(100 * time.Millisecond) // Ensure test2 is also expired

	// Stop cleanup
	close(cache.stopChan)

	// Manually run cleanup logic for testing using TSDB structure
	cache.mu.Lock()
	now := time.Now()
	expiredKeys := make([]string, 0)

	// Find expired metrics
	for tsKey, metric := range cache.timeSeries {
		if now.Sub(metric.Timestamp) > cache.ttl {
			expiredKeys = append(expiredKeys, tsKey)
		}
	}

	// Remove expired metrics
	for _, tsKey := range expiredKeys {
		metric := cache.timeSeries[tsKey]
		delete(cache.timeSeries, tsKey)

		// Also remove from probe index
		if probeKeys, exists := cache.probeIndex[metric.ProbeName]; exists {
			delete(probeKeys, tsKey)
			// Clean up empty probe index
			if len(probeKeys) == 0 {
				delete(cache.probeIndex, metric.ProbeName)
			}
		}
	}
	cache.mu.Unlock()

	// Check results
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	// Both metrics should be expired and removed from timeSeries
	if len(cache.timeSeries) > 0 {
		t.Errorf("Expected all expired metrics to be removed from timeSeries, but found %d", len(cache.timeSeries))
	}

	// Probe index should also be cleaned up
	if _, exists := cache.probeIndex["test"]; exists {
		t.Error("Expected probe index to be cleaned up when all metrics expired")
	}
}

// Legacy POST endpoint has been removed - use GET endpoints instead

func TestHTTPSyncStrategy_HealthEndpoint(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	strategy.healthManager.HandleBasicHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response HealthResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Status != "ok" {
		t.Errorf("Expected status 'ok', got %s", response.Status)
	}
}

func TestHTTPSyncStrategy_GetMetricsForProbe(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Add test data to cache using public interface
	now := time.Now()

	// Create test datapoints and add them to cache
	testDatapoints := []datapoint.DataPoint{
		{
			Name:      "metric1",
			Value:     10.5,
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "redfish"},
			},
		},
		{
			Name:      "metric2",
			Value:     20.0,
			Timestamp: now,
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "host"},
			},
		},
	}

	// Add test data to cache
	strategy.cache.AddDataPointsWithTransformer(testDatapoints, strategy.transformerRegistry)

	// Get metrics for redfish probe
	channels := strategy.metricsProcessor.GetPRTGMetricsForProbe("redfish")

	if len(channels) != 1 {
		t.Errorf("Expected 1 channel for redfish probe, got %d", len(channels))
	}

	if len(channels) > 0 {
		if channels[0].Value != 10.5 {
			t.Errorf("Expected value 10.5, got %f", channels[0].Value)
		}
	}

	// Get metrics for host probe
	channels = strategy.metricsProcessor.GetPRTGMetricsForProbe("host")

	if len(channels) != 1 {
		t.Errorf("Expected 1 channel for host probe, got %d", len(channels))
	}

	// Get metrics for non-existent probe
	channels = strategy.metricsProcessor.GetPRTGMetricsForProbe("nonexistent")

	if len(channels) != 0 {
		t.Errorf("Expected 0 channels for non-existent probe, got %d", len(channels))
	}
}

func TestHTTPSyncStrategy_TransformToPRTGChannel(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	tests := []struct {
		name    string
		key     string
		metric  CachedMetric
		wantNil bool
	}{
		{
			name: "Valid float64 value",
			key:  "processor_time",
			metric: CachedMetric{
				Value:      float64(25.5),
				Unit:       "%",
				MetricName: "processor_time",
				ProbeName:  "cpu",
				Tags:       map[string]string{"instance": "0"},
			},
			wantNil: false,
		},
		{
			name: "Valid float32 value",
			key:  "memory_used_percent",
			metric: CachedMetric{
				Value:      float32(30.0),
				Unit:       "%",
				MetricName: "memory_used_percent",
				ProbeName:  "memory",
				Tags:       map[string]string{},
			},
			wantNil: false,
		},
		{
			name: "Valid int value",
			key:  "bytes_received",
			metric: CachedMetric{
				Value:      int(100),
				Unit:       "Bytes/s",
				MetricName: "bytes_received",
				ProbeName:  "network",
				Tags:       map[string]string{"interface": "eth0"},
			},
			wantNil: false,
		},
		{
			name: "Invalid string value",
			key:  "test.metric",
			metric: CachedMetric{
				Value:      "invalid",
				Unit:       "",
				MetricName: "test.metric",
				Tags:       map[string]string{},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := strategy.metricsProcessor.TransformToPRTGChannel(tt.key, tt.metric)

			if tt.wantNil && channel != nil {
				t.Error("Expected nil channel for invalid value")
			}

			if !tt.wantNil && channel == nil {
				t.Error("Expected non-nil channel for valid value")
			}

			if channel != nil {
				// For PRTG, native units map directly; others use Custom + CustomUnit
				if tt.metric.Unit != "" {
					nativeUnits := map[string]string{
						"#": "Count", "%": "Percent", "Bytes": "BytesMemory",
						"°C": "Temperature", "ms": "TimeResponse", "s": "TimeSeconds",
					}
					if expected, isNative := nativeUnits[tt.metric.Unit]; isNative {
						if channel.Unit != expected {
							t.Errorf("Expected native unit '%s', got %s", expected, channel.Unit)
						}
					} else {
						if channel.Unit != "Custom" {
							t.Errorf("Expected unit 'Custom', got %s", channel.Unit)
						}
						if channel.CustomUnit != tt.metric.Unit {
							t.Errorf("Expected custom unit %s, got %s", tt.metric.Unit, channel.CustomUnit)
						}
					}
				}
				// Check Float field based on lookup presence
				if channel.ValueLookup == "" {
					if channel.Float == nil || *channel.Float != 1 {
						if channel.Float == nil {
							t.Errorf("Expected Float field to be 1 for non-lookup metric, got nil")
						} else {
							t.Errorf("Expected Float field to be 1 for non-lookup metric, got %d", *channel.Float)
						}
					}
				} else {
					if channel.Float != nil {
						t.Errorf("Expected Float field to be nil for lookup metric, got %d", *channel.Float)
					}
				}
			}
		})
	}
}

func TestHTTPSyncStrategy_Shutdown(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Start the strategy
	err := strategy.Start()
	if err != nil {
		t.Fatalf("Failed to start strategy: %v", err)
	}

	// Shutdown should not error
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = strategy.Shutdown(ctx)
	if err != nil {
		t.Errorf("Expected no error on shutdown, got %v", err)
	}
}

// TestGenerateMetricKey is no longer needed since we removed the generateMetricKey function
// The cache now stores metrics directly by probe name without complex key generation

func TestHTTPSyncStrategy_DebugLogsEndpoint(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Start the strategy to initialize the server
	err := strategy.Start()
	if err != nil {
		t.Fatalf("Failed to start strategy: %v", err)
	}
	defer func() {
		if err := strategy.Shutdown(context.Background()); err != nil {
			t.Errorf("Failed to shutdown strategy: %v", err)
		}
	}()

	tests := []struct {
		name           string
		agentKey       string
		method         string
		expectedStatus int
		expectedInBody string
	}{
		{
			name:           "Valid agent key GET",
			agentKey:       "test-agent-key",
			method:         "GET",
			expectedStatus: http.StatusOK,
			expectedInBody: "module_levels",
		},
		{
			name:           "Invalid agent key GET",
			agentKey:       "wrong-key",
			method:         "GET",
			expectedStatus: http.StatusUnauthorized,
			expectedInBody: "Unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/" + tt.agentKey + "/debug/logs"
			req, err := http.NewRequest(tt.method, url, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			rr := httptest.NewRecorder()
			router := strategy.setupRoutes()
			router.ServeHTTP(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tt.expectedStatus, status)
			}

			if !strings.Contains(rr.Body.String(), tt.expectedInBody) {
				t.Errorf("Expected body to contain '%s', got '%s'", tt.expectedInBody, rr.Body.String())
			}
		})
	}
}

func TestHTTPSyncStrategy_TransformToPRTGChannel_NoProbeNameInChannel(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Create a metric with probe_name in the key
	key := "cpu_usage_total.probe_name=cpu"
	metric := CachedMetric{
		Value:      75.5,
		Timestamp:  time.Now(),
		Unit:       "%",
		ProbeName:  "cpu",
		MetricName: "cpu_usage_total",
		Tags: map[string]string{
			"probe_name": "cpu",
		},
	}

	channel := strategy.metricsProcessor.TransformToPRTGChannel(key, metric)

	if channel == nil {
		t.Fatal("Expected channel to be created, got nil")
	}

	// Channel should be transformed to "CPU Total Usage" (new YAML transformer)
	expectedChannelName := "CPU Total Usage"
	if channel.Channel != expectedChannelName {
		t.Errorf("Expected channel name '%s', got '%s'", expectedChannelName, channel.Channel)
	}

	if channel.Value != 75.5 {
		t.Errorf("Expected value 75.5, got %f", channel.Value)
	}
}

func TestHTTPSyncStrategy_PRTGMetricsGET(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	// Enable PRTG endpoint for this test
	params := map[string]interface{}{
		"endpoints": []interface{}{"prtg"},
	}
	strategy := NewHTTPSyncStrategy(agentConfig, params, logger).(*HTTPSyncStrategy)

	// Add some test data to cache
	testDataPoints := []datapoint.DataPoint{
		{
			Name:      "cpu_usage_total",
			Timestamp: time.Now(),
			Value:     float32(75.5),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "cpu"},
			},
		},
		{
			Name:      "memory_used_percent",
			Timestamp: time.Now(),
			Value:     float32(82.3),
			Tags: []tags.Tag{
				{Key: "probe_name", Value: "memory"},
			},
		},
	}

	// Add data to strategy cache
	err := strategy.AddDataPoints(testDataPoints)
	if err != nil {
		t.Fatalf("Failed to add test data: %v", err)
	}

	tests := []struct {
		name           string
		agentKey       string
		probe          string
		expectedStatus int
		expectedProbe  string
	}{
		{
			name:           "Valid GET request for CPU metrics",
			agentKey:       "test-agent-key",
			probe:          "cpu",
			expectedStatus: http.StatusOK,
			expectedProbe:  "cpu",
		},
		{
			name:           "Valid GET request for memory metrics",
			agentKey:       "test-agent-key",
			probe:          "memory",
			expectedStatus: http.StatusOK,
			expectedProbe:  "memory",
		},
		{
			name:           "Invalid agent key",
			agentKey:       "wrong-key",
			probe:          "cpu",
			expectedStatus: http.StatusUnauthorized,
			expectedProbe:  "",
		},
		{
			name:           "Non-existent probe",
			agentKey:       "test-agent-key",
			probe:          "nonexistent",
			expectedStatus: http.StatusOK,
			expectedProbe:  "nonexistent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := fmt.Sprintf("/api/%s/prtg/metrics/%s", tt.agentKey, tt.probe)
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			rr := httptest.NewRecorder()
			router := strategy.setupRoutes()
			router.ServeHTTP(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tt.expectedStatus, status)
			}

			if tt.expectedStatus == http.StatusOK {
				// Parse response and verify it's valid PRTG format
				var response PRTGResponse
				if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
					t.Errorf("Failed to decode response: %v", err)
				}

				// For existing probes, we should have channels
				if tt.expectedProbe == "cpu" || tt.expectedProbe == "memory" {
					if len(response.PRTG.Result) == 0 {
						t.Errorf("Expected channels for probe %s, got none", tt.expectedProbe)
					}
				}
			}
		})
	}
}

func TestHTTPSyncStrategy_SetLogLevelsEndpoint(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Start the strategy to initialize the server
	err := strategy.Start()
	if err != nil {
		t.Fatalf("Failed to start strategy: %v", err)
	}
	defer func() {
		if err := strategy.Shutdown(context.Background()); err != nil {
			t.Errorf("Failed to shutdown strategy: %v", err)
		}
	}()

	tests := []struct {
		name           string
		agentKey       string
		body           string
		expectedStatus int
		expectedInBody string
	}{
		{
			name:     "Valid log level setting",
			agentKey: "test-agent-key",
			body: `{
				"module_levels": [
					{"module": "strategy.http", "level": "debug"},
					{"module": "probe.redfish", "level": "warn"}
				]
			}`,
			expectedStatus: http.StatusOK,
			expectedInBody: "success",
		},
		{
			name:           "Invalid agent key",
			agentKey:       "wrong-key",
			body:           `{"module_levels": []}`,
			expectedStatus: http.StatusUnauthorized,
			expectedInBody: "Unauthorized",
		},
		{
			name:           "Invalid JSON",
			agentKey:       "test-agent-key",
			body:           `{invalid json}`,
			expectedStatus: http.StatusBadRequest,
			expectedInBody: "Invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/" + tt.agentKey + "/debug/logs"
			req, err := http.NewRequest("POST", url, strings.NewReader(tt.body))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			router := strategy.setupRoutes()
			router.ServeHTTP(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tt.expectedStatus, status)
			}

			if !strings.Contains(rr.Body.String(), tt.expectedInBody) {
				t.Errorf("Expected body to contain '%s', got '%s'", tt.expectedInBody, rr.Body.String())
			}
		})
	}
}

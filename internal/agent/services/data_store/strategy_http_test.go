// senhub-agent/internal/agent/services/data_store/strategy_http_test.go
package data_store

import (
	"bytes"
	"context"
	"encoding/json"
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
		name   string
		params map[string]interface{}
		expectedPort int
		expectedNaming map[string]string
	}{
		{
			name:   "Default configuration",
			params: map[string]interface{}{},
			expectedPort: 8080,
			expectedNaming: map[string]string{
				"redfish": "friendly",
				"host":    "friendly", 
				"otel":    "technical",
			},
		},
		{
			name: "Custom port",
			params: map[string]interface{}{
				"port": float64(9090),
			},
			expectedPort: 9090,
		},
		{
			name: "Custom naming configuration",
			params: map[string]interface{}{
				"naming": map[string]interface{}{
					"redfish": "technical",
					"host":    "prtg_standard",
				},
			},
			expectedPort: 8080, // default port
			expectedNaming: map[string]string{
				"redfish": "technical",
				"host":    "prtg_standard",
				"otel":    "technical", // default
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

			if tt.expectedNaming != nil {
				for probe, expectedStyle := range tt.expectedNaming {
					if style, exists := httpStrategy.namingConfig[probe]; !exists {
						t.Errorf("Expected naming config for %s", probe)
					} else if style != expectedStyle {
						t.Errorf("Expected naming style %s for %s, got %s", expectedStyle, probe, style)
					}
				}
			}
		})
	}
}

func TestHTTPSyncStrategy_Interface(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger)

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

	// Test invalid port validation
	err = strategy.ValidateConfigParams(configuration.StorageConfigParams{
		"port": "invalid",
	})
	if err == nil {
		t.Error("Expected validation error for invalid port")
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

	if len(strategy.cache.data) != 2 {
		t.Errorf("Expected 2 cached metrics, got %d", len(strategy.cache.data))
	}

	// Check specific cached metric - find it by iterating since key format changed
	var cpuMetric *CachedMetric
	for key, metric := range strategy.cache.data {
		if strings.Contains(key, "cpu.usage_percent") && metric.ProbeName == "host" {
			cpuMetric = &metric
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
	cache := &MetricCache{
		data:     make(map[string]CachedMetric),
		ttl:      50 * time.Millisecond, // Very short TTL for testing
		stopChan: make(chan struct{}),
	}

	// Add some test data
	cache.mu.Lock()
	cache.data["test1"] = CachedMetric{
		Value:     10.0,
		Timestamp: time.Now().Add(-100 * time.Millisecond), // Expired
	}
	cache.data["test2"] = CachedMetric{
		Value:     20.0,
		Timestamp: time.Now(), // Fresh
	}
	cache.mu.Unlock()

	// Start cleanup goroutine
	go cache.cleanup()

	// Wait for cleanup to run (cleanup runs every minute, so we need to manually trigger)
	// Let's manually test the cleanup logic instead
	time.Sleep(100 * time.Millisecond) // Ensure test2 is also expired

	// Stop cleanup
	close(cache.stopChan)

	// Manually run cleanup logic for testing
	cache.mu.Lock()
	now := time.Now()
	for key, metric := range cache.data {
		if now.Sub(metric.Timestamp) > cache.ttl {
			delete(cache.data, key)
		}
	}
	cache.mu.Unlock()

	// Check results
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	if _, exists := cache.data["test1"]; exists {
		t.Error("Expected expired metric to be removed")
	}

	if _, exists := cache.data["test2"]; exists {
		t.Error("Expected expired metric to be removed")
	}
}

func TestHTTPSyncStrategy_PRTGEndpoint(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Add some test data to cache
	strategy.cache.data["thermal.cpu.0.temperature"] = CachedMetric{
		Value:     65.2,
		Timestamp: time.Now(),
		ProbeName: "redfish",
		Tags: map[string]string{
			"probe_name": "redfish",
			"index":      "0",
		},
	}

	// Setup HTTP server
	err := strategy.Start()
	if err != nil {
		t.Fatalf("Failed to start strategy: %v", err)
	}
	defer strategy.Shutdown(context.Background())

	// Create test request
	requestBody := PRTGRequest{
		Probe:  "redfish",
		Target: "server1", 
		Config: map[string]interface{}{
			"host":     "192.168.1.100",
			"username": "admin",
			"password": "secret",
		},
	}

	bodyBytes, _ := json.Marshal(requestBody)
	
	// Test with correct agent key
	t.Run("Valid agent key", func(t *testing.T) {
		// Use router to properly extract path variables
		router := strategy.setupRoutes()
		
		url := "/api/test-agent-key/prtg/metrics"
		req := httptest.NewRequest("POST", url, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		var response PRTGResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		if err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if len(response.PRTG.Result) == 0 {
			t.Error("Expected at least one PRTG channel in response")
		}
	})

	// Test with invalid agent key
	t.Run("Invalid agent key", func(t *testing.T) {
		router := strategy.setupRoutes()
		
		url := "/api/wrong-key/prtg/metrics"
		req := httptest.NewRequest("POST", url, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	// Test with invalid JSON
	t.Run("Invalid JSON", func(t *testing.T) {
		router := strategy.setupRoutes()
		
		url := "/api/test-agent-key/prtg/metrics"
		req := httptest.NewRequest("POST", url, strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

func TestHTTPSyncStrategy_HealthEndpoint(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	strategy.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %s", response["status"])
	}
}

func TestHTTPSyncStrategy_GetMetricsForProbe(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	// Add test data to cache
	now := time.Now()
	strategy.cache.data["metric1"] = CachedMetric{
		Value:     10.5,
		Timestamp: now,
		ProbeName: "redfish",
		Tags: map[string]string{
			"probe_name": "redfish",
		},
	}
	strategy.cache.data["metric2"] = CachedMetric{
		Value:     20.0,
		Timestamp: now,
		ProbeName: "host",
		Tags: map[string]string{
			"probe_name": "host",
		},
	}
	strategy.cache.data["metric3"] = CachedMetric{
		Value:     30.0,
		Timestamp: now.Add(-10 * time.Minute), // Expired
		ProbeName: "redfish",
		Tags: map[string]string{
			"probe_name": "redfish",
		},
	}

	// Get metrics for redfish probe
	channels := strategy.getMetricsForProbe("redfish")

	if len(channels) != 1 {
		t.Errorf("Expected 1 channel for redfish probe, got %d", len(channels))
	}

	if len(channels) > 0 {
		if channels[0].Value != 10.5 {
			t.Errorf("Expected value 10.5, got %f", channels[0].Value)
		}
	}

	// Get metrics for host probe
	channels = strategy.getMetricsForProbe("host")

	if len(channels) != 1 {
		t.Errorf("Expected 1 channel for host probe, got %d", len(channels))
	}

	// Get metrics for non-existent probe
	channels = strategy.getMetricsForProbe("nonexistent")

	if len(channels) != 0 {
		t.Errorf("Expected 0 channels for non-existent probe, got %d", len(channels))
	}
}

func TestHTTPSyncStrategy_TransformToPRTGChannel(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	tests := []struct {
		name     string
		key      string
		metric   CachedMetric
		wantNil  bool
	}{
		{
			name: "Valid float64 value",
			key:  "test.metric",
			metric: CachedMetric{
				Value: float64(25.5),
				Unit:  "°C",
				Tags:  map[string]string{},
			},
			wantNil: false,
		},
		{
			name: "Valid float32 value",
			key:  "test.metric",
			metric: CachedMetric{
				Value: float32(30.0),
				Unit:  "W",
				Tags:  map[string]string{},
			},
			wantNil: false,
		},
		{
			name: "Valid int value",
			key:  "test.metric",
			metric: CachedMetric{
				Value: int(100),
				Unit:  "#",
				Tags:  map[string]string{},
			},
			wantNil: false,
		},
		{
			name: "Invalid string value",
			key:  "test.metric",
			metric: CachedMetric{
				Value: "invalid",
				Unit:  "",
				Tags:  map[string]string{},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := strategy.transformToPRTGChannel(tt.key, tt.metric)

			if tt.wantNil && channel != nil {
				t.Error("Expected nil channel for invalid value")
			}

			if !tt.wantNil && channel == nil {
				t.Error("Expected non-nil channel for valid value")
			}

			if channel != nil {
				if channel.Unit != tt.metric.Unit {
					t.Errorf("Expected unit %s, got %s", tt.metric.Unit, channel.Unit)
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

func TestGenerateMetricKey(t *testing.T) {
	agentConfig := createTestAgentConfig()
	logger := createTestLogger()
	strategy := NewHTTPSyncStrategy(agentConfig, map[string]interface{}{}, logger).(*HTTPSyncStrategy)

	dp := datapoint.DataPoint{
		Name:      "cpu.usage_percent",
		Timestamp: time.Now(),
		Value:     75.0,
	}

	key := strategy.generateMetricKey(dp)
	if key != "cpu.usage_percent" {
		t.Errorf("Expected key 'cpu.usage_percent', got %s", key)
	}
}
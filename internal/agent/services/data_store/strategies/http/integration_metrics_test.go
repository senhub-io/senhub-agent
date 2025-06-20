// senhub-agent/internal/agent/services/data_store/integration_metrics_test.go
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"senhub-agent.go/internal/agent/cliArgs"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// IntegrationTestConfig contains configuration for the comprehensive metrics test
type IntegrationTestConfig struct {
	AgentKey    string
	Port        int
	BindAddress string
	Endpoints   []string
	Logger      *logger.Logger
	AgentConfig configuration.AgentConfiguration
}

// MetricsTestResult contains test results for each endpoint format
type MetricsTestResult struct {
	EndpointName  string
	URL           string
	StatusCode    int
	ResponseBody  string
	MetricCount   int
	ProbesCovered []string
	Errors        []string
	Warnings      []string
	Success       bool
}

// ComprehensiveTestReport contains the complete test report
type ComprehensiveTestReport struct {
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	TotalProbes     int
	TotalMetrics    int
	EndpointResults []MetricsTestResult
	OverallSuccess  bool
	Summary         string
	Recommendations []string
}

// createIntegrationTestConfig creates a test configuration
func createIntegrationTestConfig() *IntegrationTestConfig {
	args := &cliArgs.ParsedArgs{
		Env:     "integration-test",
		Verbose: true,
	}
	logger := logger.NewLogger(args)

	agentConfig := configuration.NewAgentConfiguration(
		"integration-test-key-12345",
		"http://test-server.com",
		logger,
	)

	return &IntegrationTestConfig{
		AgentKey:    "integration-test-key-12345",
		Port:        9999, // Use non-standard port to avoid conflicts
		BindAddress: "127.0.0.1",
		Endpoints:   []string{"prtg", "senhub", "nagios", "web"},
		Logger:      logger,
		AgentConfig: agentConfig,
	}
}

// generateTestDataPoints creates comprehensive test data covering all major probe types
func generateTestDataPoints() []datapoint.DataPoint {
	now := time.Now()
	dataPoints := []datapoint.DataPoint{}

	// CPU Metrics (host probe)
	dataPoints = append(dataPoints,
		datapoint.DataPoint{
			Name:      "cpu_usage_total",
			Value:     float32(75.5),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "processor_time",
			Value:     float32(68.2),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "instance", Value: "0"}},
		},
		datapoint.DataPoint{
			Name:      "cpu_load1",
			Value:     float32(2.1),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "cpu_load5",
			Value:     float32(1.8),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "cpu_user",
			Value:     float32(45.2),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "cpu_system",
			Value:     float32(23.3),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
	)

	// Memory Metrics (host probe)
	dataPoints = append(dataPoints,
		datapoint.DataPoint{
			Name:      "memory_used_percent",
			Value:     float32(82.3),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "memory_available_mb",
			Value:     float32(1024),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "memory_total_mb",
			Value:     float32(8192),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "memory_used_mb",
			Value:     float32(6741),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "memory_cached_mb",
			Value:     float32(512),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
		datapoint.DataPoint{
			Name:      "memory_buffers_mb",
			Value:     float32(256),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}},
		},
	)

	// Network Metrics (host probe)
	dataPoints = append(dataPoints,
		datapoint.DataPoint{
			Name:      "bytes_received",
			Value:     float32(1048576),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "interface", Value: "eth0"}},
		},
		datapoint.DataPoint{
			Name:      "bytes_sent",
			Value:     float32(524288),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "interface", Value: "eth0"}},
		},
		datapoint.DataPoint{
			Name:      "packets_received",
			Value:     float32(1024),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "interface", Value: "eth0"}},
		},
		datapoint.DataPoint{
			Name:      "packets_sent",
			Value:     float32(768),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "interface", Value: "eth0"}},
		},
		datapoint.DataPoint{
			Name:      "errors_in",
			Value:     float32(0),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "interface", Value: "eth0"}},
		},
		datapoint.DataPoint{
			Name:      "errors_out",
			Value:     float32(0),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "interface", Value: "eth0"}},
		},
	)

	// Logical Disk Metrics (host probe)
	dataPoints = append(dataPoints,
		datapoint.DataPoint{
			Name:      "disk_used_percent",
			Value:     float32(65.8),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "drive", Value: "C:"}},
		},
		datapoint.DataPoint{
			Name:      "disk_free_mb",
			Value:     float32(34816),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "drive", Value: "C:"}},
		},
		datapoint.DataPoint{
			Name:      "disk_total_mb",
			Value:     float32(102400),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "drive", Value: "C:"}},
		},
		datapoint.DataPoint{
			Name:      "disk_used_mb",
			Value:     float32(67584),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "drive", Value: "C:"}},
		},
		datapoint.DataPoint{
			Name:      "disk_read_sec",
			Value:     float32(125.6),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "drive", Value: "C:"}},
		},
		datapoint.DataPoint{
			Name:      "disk_write_sec",
			Value:     float32(89.3),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "host"}, {Key: "drive", Value: "C:"}},
		},
	)

	// Gateway Ping Metrics (gateway probe)
	dataPoints = append(dataPoints,
		datapoint.DataPoint{
			Name:      "ping_success",
			Value:     float32(1),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "gateway"}, {Key: "target", Value: "8.8.8.8"}},
		},
		datapoint.DataPoint{
			Name:      "ping_rtt_ms",
			Value:     float32(15.2),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "gateway"}, {Key: "target", Value: "8.8.8.8"}},
		},
		datapoint.DataPoint{
			Name:      "ping_packet_loss",
			Value:     float32(0.0),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "gateway"}, {Key: "target", Value: "8.8.8.8"}},
		},
		datapoint.DataPoint{
			Name:      "ping_min_ms",
			Value:     float32(12.8),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "gateway"}, {Key: "target", Value: "8.8.8.8"}},
		},
		datapoint.DataPoint{
			Name:      "ping_max_ms",
			Value:     float32(18.6),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "gateway"}, {Key: "target", Value: "8.8.8.8"}},
		},
	)

	// WebApp Ping Metrics (webapp probe)
	dataPoints = append(dataPoints,
		datapoint.DataPoint{
			Name:      "ping_success",
			Value:     float32(1),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "webapp"}, {Key: "target", Value: "example.com"}},
		},
		datapoint.DataPoint{
			Name:      "ping_rtt_ms",
			Value:     float32(45.7),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "webapp"}, {Key: "target", Value: "example.com"}},
		},
		datapoint.DataPoint{
			Name:      "http_status_code",
			Value:     float32(200),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "webapp"}, {Key: "target", Value: "example.com"}},
		},
		datapoint.DataPoint{
			Name:      "http_response_time_ms",
			Value:     float32(523.1),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "webapp"}, {Key: "target", Value: "example.com"}},
		},
		datapoint.DataPoint{
			Name:      "http_success",
			Value:     float32(1),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "webapp"}, {Key: "target", Value: "example.com"}},
		},
	)

	// Redfish Thermal Metrics (redfish probe)
	dataPoints = append(dataPoints,
		datapoint.DataPoint{
			Name:      "thermal.cpu.0.temperature",
			Value:     float32(65.2),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "redfish"}, {Key: "index", Value: "0"}, {Key: "component", Value: "processor"}},
		},
		datapoint.DataPoint{
			Name:      "thermal.cpu.1.temperature",
			Value:     float32(67.8),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "redfish"}, {Key: "index", Value: "1"}, {Key: "component", Value: "processor"}},
		},
		datapoint.DataPoint{
			Name:      "thermal.memory.0.temperature",
			Value:     float32(42.1),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "redfish"}, {Key: "index", Value: "0"}, {Key: "component", Value: "memory"}},
		},
		datapoint.DataPoint{
			Name:      "power.cpu.0.watts",
			Value:     float32(125.6),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "redfish"}, {Key: "index", Value: "0"}, {Key: "component", Value: "processor"}},
		},
		datapoint.DataPoint{
			Name:      "system.health",
			Value:     float32(1),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "redfish"}, {Key: "component", Value: "system"}},
		},
		datapoint.DataPoint{
			Name:      "system.power_state",
			Value:     float32(1),
			Timestamp: now,
			Tags:      []tags.Tag{{Key: "probe_name", Value: "redfish"}, {Key: "component", Value: "system"}},
		},
	)

	return dataPoints
}

// TestComprehensiveMetricsIntegration is the main integration test
func TestComprehensiveMetricsIntegration(t *testing.T) {
	config := createIntegrationTestConfig()
	report := &ComprehensiveTestReport{
		StartTime: time.Now(),
	}

	t.Logf("🚀 Starting Comprehensive Metrics Integration Test")
	t.Logf("📋 Configuration: Port=%d, Endpoints=%v", config.Port, config.Endpoints)

	// Create HTTP strategy with all endpoints enabled
	params := map[string]interface{}{
		"port":         config.Port,
		"bind_address": config.BindAddress,
		"endpoints":    []interface{}{"prtg", "nagios", "web"},
	}

	strategy := NewHTTPSyncStrategy(config.AgentConfig, params, config.Logger).(*HTTPSyncStrategy)

	// Start the HTTP strategy
	err := strategy.Start()
	if err != nil {
		t.Fatalf("❌ Failed to start HTTP strategy: %v", err)
	}
	defer func() {
		if err := strategy.Shutdown(context.Background()); err != nil {
			t.Errorf("Failed to shutdown strategy: %v", err)
		}
	}()

	t.Logf("✅ HTTP Strategy started on %s:%d", config.BindAddress, config.Port)

	// Generate and add comprehensive test data
	testDataPoints := generateTestDataPoints()
	err = strategy.AddDataPoints(testDataPoints)
	if err != nil {
		t.Fatalf("❌ Failed to add test data points: %v", err)
	}

	report.TotalMetrics = len(testDataPoints)
	t.Logf("📊 Added %d test metrics to cache", len(testDataPoints))

	// Wait for metrics to be processed
	time.Sleep(100 * time.Millisecond)

	// Get cache information
	cacheInfo := strategy.cache.GetCacheInfo()
	report.TotalProbes = cacheInfo.ProbeCount
	t.Logf("📈 Cache contains %d time series across %d probes", cacheInfo.TotalMetrics, cacheInfo.ProbeCount)

	// Test each endpoint format
	baseURL := fmt.Sprintf("http://%s:%d", config.BindAddress, config.Port)

	// Test PRTG endpoints
	prtgResult := testPRTGEndpoints(t, baseURL, config.AgentKey, strategy)
	report.EndpointResults = append(report.EndpointResults, prtgResult...)

	// Test Nagios endpoints
	nagiosResult := testNagiosEndpoints(t, baseURL, config.AgentKey, strategy)
	report.EndpointResults = append(report.EndpointResults, nagiosResult...)

	// Test Info/Discovery endpoints
	infoResult := testInfoEndpoints(t, baseURL, config.AgentKey, strategy)
	report.EndpointResults = append(report.EndpointResults, infoResult...)

	// Generate final report
	report.EndTime = time.Now()
	report.Duration = report.EndTime.Sub(report.StartTime)
	generateFinalReport(t, report)
}

// testPRTGEndpoints tests all PRTG format endpoints
func testPRTGEndpoints(t *testing.T, baseURL, agentKey string, strategy *HTTPSyncStrategy) []MetricsTestResult {
	t.Logf("🔍 Testing PRTG Endpoints...")
	results := []MetricsTestResult{}

	// Test PRTG endpoints for each probe
	probes := []string{"host", "gateway", "webapp", "redfish"}

	for _, probe := range probes {
		url := fmt.Sprintf("%s/api/%s/prtg/metrics/%s", baseURL, agentKey, probe)

		resp, err := http.Get(url)
		result := MetricsTestResult{
			EndpointName:  fmt.Sprintf("PRTG-%s", probe),
			URL:           url,
			ProbesCovered: []string{probe},
		}

		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("HTTP request failed: %v", err))
			result.Success = false
		} else {
			result.StatusCode = resp.StatusCode
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			if resp.StatusCode == http.StatusOK {
				var prtgResp PRTGResponse
				if err := json.NewDecoder(resp.Body).Decode(&prtgResp); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("JSON decode failed: %v", err))
				} else {
					result.MetricCount = len(prtgResp.PRTG.Result)
					result.Success = true

					// Validate PRTG format
					for i, channel := range prtgResp.PRTG.Result {
						if channel.Channel == "" {
							result.Warnings = append(result.Warnings, fmt.Sprintf("Channel %d has empty name", i))
						}
						if channel.Value == 0 && probe != "nonexistent" {
							result.Warnings = append(result.Warnings, fmt.Sprintf("Channel %s has zero value", channel.Channel))
						}
						if channel.Unit == "" && channel.CustomUnit == "" {
							result.Warnings = append(result.Warnings, fmt.Sprintf("Channel %s has no unit specified", channel.Channel))
						}
					}
				}
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("HTTP status %d", resp.StatusCode))
			}
		}

		results = append(results, result)
		t.Logf("  📊 PRTG %s: %d metrics, status=%d, success=%v", probe, result.MetricCount, result.StatusCode, result.Success)
	}

	return results
}

// testNagiosEndpoints tests all Nagios format endpoints
func testNagiosEndpoints(t *testing.T, baseURL, agentKey string, strategy *HTTPSyncStrategy) []MetricsTestResult {
	t.Logf("🔍 Testing Nagios Endpoints...")
	results := []MetricsTestResult{}

	// Test Nagios endpoints for each probe
	probes := []string{"host", "gateway", "webapp", "redfish"}

	for _, probe := range probes {
		url := fmt.Sprintf("%s/api/%s/nagios/metrics/%s", baseURL, agentKey, probe)

		resp, err := http.Get(url)
		result := MetricsTestResult{
			EndpointName:  fmt.Sprintf("Nagios-%s", probe),
			URL:           url,
			ProbesCovered: []string{probe},
		}

		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("HTTP request failed: %v", err))
			result.Success = false
		} else {
			result.StatusCode = resp.StatusCode
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			if resp.StatusCode == http.StatusOK {
				// For Nagios, we expect text response, not JSON
				result.Success = true
				bodyBytes := make([]byte, 1024)
				n, _ := resp.Body.Read(bodyBytes)
				result.ResponseBody = string(bodyBytes[:n])

				// Basic validation for Nagios format
				if strings.Contains(result.ResponseBody, "OK") ||
					strings.Contains(result.ResponseBody, "WARNING") ||
					strings.Contains(result.ResponseBody, "CRITICAL") {
					result.Success = true
				} else {
					result.Warnings = append(result.Warnings, "Response doesn't contain Nagios status keywords")
				}

				// Count performance data metrics
				if strings.Contains(result.ResponseBody, "|") {
					parts := strings.Split(result.ResponseBody, "|")
					if len(parts) > 1 {
						perfData := strings.TrimSpace(parts[1])
						metrics := strings.Split(perfData, " ")
						result.MetricCount = len(metrics)
					}
				}
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("HTTP status %d", resp.StatusCode))
			}
		}

		results = append(results, result)
		t.Logf("  📊 Nagios %s: %d metrics, status=%d, success=%v", probe, result.MetricCount, result.StatusCode, result.Success)
	}

	// Test the general Nagios metrics endpoint
	url := fmt.Sprintf("%s/api/%s/nagios/metrics", baseURL, agentKey)
	resp, err := http.Get(url)
	result := MetricsTestResult{
		EndpointName:  "Nagios-All",
		URL:           url,
		ProbesCovered: []string{"all"},
	}

	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("HTTP request failed: %v", err))
		result.Success = false
	} else {
		result.StatusCode = resp.StatusCode
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()
		result.Success = resp.StatusCode == http.StatusOK
	}

	results = append(results, result)
	t.Logf("  📊 Nagios All: status=%d, success=%v", result.StatusCode, result.Success)

	return results
}

// testInfoEndpoints tests discovery and info endpoints
func testInfoEndpoints(t *testing.T, baseURL, agentKey string, strategy *HTTPSyncStrategy) []MetricsTestResult {
	t.Logf("🔍 Testing Info/Discovery Endpoints...")
	results := []MetricsTestResult{}

	infoEndpoints := []struct {
		name string
		path string
	}{
		{"Info-System", "/api/%s/info/system"},
		{"Info-Probes", "/api/%s/info/probes"},
		{"Info-Endpoints", "/api/%s/info/endpoints"},
		{"Health", "/health"},
		{"List-Endpoints", "/api/%s/endpoints"},
		{"List-Probes", "/api/%s/probes"},
	}

	for _, endpoint := range infoEndpoints {
		var url string
		if strings.Contains(endpoint.path, "%s") {
			url = fmt.Sprintf(baseURL+endpoint.path, agentKey)
		} else {
			url = baseURL + endpoint.path
		}

		resp, err := http.Get(url)
		result := MetricsTestResult{
			EndpointName:  endpoint.name,
			URL:           url,
			ProbesCovered: []string{"info"},
		}

		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("HTTP request failed: %v", err))
			result.Success = false
		} else {
			result.StatusCode = resp.StatusCode
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()
			result.Success = resp.StatusCode == http.StatusOK

			if resp.StatusCode == http.StatusOK {
				bodyBytes := make([]byte, 512)
				n, _ := resp.Body.Read(bodyBytes)
				result.ResponseBody = string(bodyBytes[:n])

				// Basic JSON validation
				var jsonResp interface{}
				if err := json.Unmarshal(bodyBytes[:n], &jsonResp); err != nil {
					result.Warnings = append(result.Warnings, "Response is not valid JSON")
				}
			}
		}

		results = append(results, result)
		t.Logf("  📊 %s: status=%d, success=%v", endpoint.name, result.StatusCode, result.Success)
	}

	return results
}

// generateFinalReport creates and logs the comprehensive test report
func generateFinalReport(t *testing.T, report *ComprehensiveTestReport) {
	t.Log("\n" + strings.Repeat("=", 80))
	t.Logf("🎯 COMPREHENSIVE METRICS INTEGRATION TEST REPORT")
	t.Log(strings.Repeat("=", 80))

	// Overall statistics
	successCount := 0
	totalEndpoints := len(report.EndpointResults)

	for _, result := range report.EndpointResults {
		if result.Success {
			successCount++
		}
	}

	report.OverallSuccess = successCount == totalEndpoints
	successRate := float64(successCount) / float64(totalEndpoints) * 100

	t.Logf("📊 Test Duration: %v", report.Duration)
	t.Logf("📊 Total Probes: %d", report.TotalProbes)
	t.Logf("📊 Total Metrics: %d", report.TotalMetrics)
	t.Logf("📊 Endpoints Tested: %d", totalEndpoints)
	t.Logf("📊 Success Rate: %.1f%% (%d/%d)", successRate, successCount, totalEndpoints)

	if report.OverallSuccess {
		t.Logf("✅ OVERALL RESULT: PASS")
		report.Summary = fmt.Sprintf("All %d endpoints passed tests successfully", totalEndpoints)
	} else {
		t.Logf("❌ OVERALL RESULT: FAIL")
		report.Summary = fmt.Sprintf("%d/%d endpoints failed tests", totalEndpoints-successCount, totalEndpoints)
	}

	// Detailed results by category
	categories := map[string][]MetricsTestResult{
		"PRTG":   {},
		"Nagios": {},
		"Info":   {},
	}

	for _, result := range report.EndpointResults {
		if strings.HasPrefix(result.EndpointName, "PRTG") {
			categories["PRTG"] = append(categories["PRTG"], result)
		} else if strings.HasPrefix(result.EndpointName, "Nagios") {
			categories["Nagios"] = append(categories["Nagios"], result)
		} else {
			categories["Info"] = append(categories["Info"], result)
		}
	}

	t.Logf("\n📋 DETAILED RESULTS BY CATEGORY:")
	t.Log(strings.Repeat("-", 80))

	for category, results := range categories {
		if len(results) == 0 {
			continue
		}

		categorySuccess := 0
		totalMetrics := 0
		for _, result := range results {
			if result.Success {
				categorySuccess++
			}
			totalMetrics += result.MetricCount
		}

		categoryRate := float64(categorySuccess) / float64(len(results)) * 100
		status := "✅ PASS"
		if categoryRate < 100 {
			status = "❌ FAIL"
		}

		t.Logf("🔸 %s: %s (%.1f%% - %d/%d endpoints, %d total metrics)",
			category, status, categoryRate, categorySuccess, len(results), totalMetrics)

		for _, result := range results {
			resultStatus := "✅"
			if !result.Success {
				resultStatus = "❌"
			}
			t.Logf("   %s %-20s: %d metrics, HTTP %d", resultStatus, result.EndpointName, result.MetricCount, result.StatusCode)

			// Show errors and warnings
			for _, err := range result.Errors {
				t.Logf("      🚨 ERROR: %s", err)
			}
			for _, warning := range result.Warnings {
				t.Logf("      ⚠️  WARNING: %s", warning)
			}
		}
	}

	// Generate recommendations
	report.Recommendations = generateRecommendations(report)

	if len(report.Recommendations) > 0 {
		t.Logf("\n🔧 RECOMMENDATIONS:")
		t.Log(strings.Repeat("-", 80))
		for i, rec := range report.Recommendations {
			t.Logf("%d. %s", i+1, rec)
		}
	}

	t.Log("\n" + strings.Repeat("=", 80))
}

// generateRecommendations analyzes test results and provides actionable recommendations
func generateRecommendations(report *ComprehensiveTestReport) []string {
	recommendations := []string{}

	// Analyze failures
	failedEndpoints := []string{}
	lowMetricEndpoints := []string{}
	endpointsWithWarnings := []string{}

	for _, result := range report.EndpointResults {
		if !result.Success {
			failedEndpoints = append(failedEndpoints, result.EndpointName)
		}
		if result.Success && result.MetricCount < 3 && !strings.Contains(result.EndpointName, "Info") {
			lowMetricEndpoints = append(lowMetricEndpoints, result.EndpointName)
		}
		if len(result.Warnings) > 0 {
			endpointsWithWarnings = append(endpointsWithWarnings, result.EndpointName)
		}
	}

	if len(failedEndpoints) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Fix failed endpoints: %s", strings.Join(failedEndpoints, ", ")))
	}

	if len(lowMetricEndpoints) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Investigate low metric counts in: %s", strings.Join(lowMetricEndpoints, ", ")))
	}

	if len(endpointsWithWarnings) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Review warnings for: %s", strings.Join(endpointsWithWarnings, ", ")))
	}

	// General recommendations
	if report.TotalMetrics < 20 {
		recommendations = append(recommendations, "Consider adding more probe types to increase metric coverage")
	}

	if report.OverallSuccess {
		recommendations = append(recommendations, "All tests passed! Consider adding more edge cases and error scenarios")
	}

	return recommendations
}

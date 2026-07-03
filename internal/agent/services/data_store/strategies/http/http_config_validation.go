// senhub-agent/internal/agent/services/data_store/http_config_validation.go
package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"senhub-agent.go/internal/agent/services/configuration"
)

// maxConfigRequestBytes bounds the request body the /config/{validate,preview,
// test} handlers will read. A probe configuration is a few KB; 1 MiB is ample
// headroom while capping the memory an oversized or malicious body can force
// the JSON decoder to buffer.
const maxConfigRequestBytes = 1 << 20

// decodeConfigRequest reads and decodes a UniversalConfigRequest under a body
// size limit. It writes the appropriate error response and returns false on
// failure: 413 when the body exceeds the limit, 400 for malformed JSON.
func decodeConfigRequest(w http.ResponseWriter, r *http.Request, req *UniversalConfigRequest) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigRequestBytes)
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "Invalid JSON request body", http.StatusBadRequest)
		return false
	}
	return true
}

// Universal Configuration Validation Methods

// ValidateUniversalConfig validates a universal configuration request according to the specified validation mode
func (cm *ConfigurationManager) ValidateUniversalConfig(req *UniversalConfigRequest) (*UniversalConfigResponse, error) {
	startTime := time.Now()

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("target", req.Target).
		Str("validation_mode", string(req.Validation)).
		Msg("Starting universal configuration validation")

	// Initialize response
	response := &UniversalConfigResponse{
		Probe:           req.Probe,
		Target:          req.Target,
		ValidationLevel: req.Validation,
		Tests:           make(map[string]ValidationTestResult),
		Warnings:        []string{},
		Errors:          []string{},
	}

	// Set default validation mode if not specified
	if req.Validation == "" {
		req.Validation = ValidationSchemaOnly
		response.ValidationLevel = ValidationSchemaOnly
	}

	// Set default timeout if not specified
	if req.Timeout == 0 {
		req.Timeout = 30 // 30 seconds default
	}

	// Step 1: Schema Validation (always performed)
	schemaResult := cm.validateProbeSchema(req.Probe, req.Config)
	response.Tests["schema"] = schemaResult

	if !schemaResult.Passed {
		response.Valid = false
		response.Errors = append(response.Errors, schemaResult.Error)
		response.Duration = time.Since(startTime).Milliseconds()

		cm.logger.Error().
			Str("probe", req.Probe).
			Str("error", schemaResult.Error).
			Msg("Schema validation failed")

		return response, nil
	}

	// Step 2: Connectivity Validation (if requested)
	if req.Validation == ValidationConnectivity || req.Validation == ValidationFull {
		connectivityResult := cm.validateProbeConnectivity(req.Probe, req.Config, req.Target, req.Timeout)
		response.Tests["connectivity"] = connectivityResult

		if !connectivityResult.Passed {
			response.Valid = false
			response.Errors = append(response.Errors, connectivityResult.Error)
			response.Duration = time.Since(startTime).Milliseconds()

			cm.logger.Warn().
				Str("probe", req.Probe).
				Str("target", req.Target).
				Str("error", connectivityResult.Error).
				Msg("Connectivity validation failed")

			return response, nil
		}
	}

	// Step 3: Full Metrics Validation (if requested)
	if req.Validation == ValidationFull {
		metricsResult, previewMetrics := cm.validateProbeMetrics(req.Probe, req.Config, req.Target, req.Timeout)
		response.Tests["metrics"] = metricsResult
		response.PreviewMetrics = previewMetrics

		if !metricsResult.Passed {
			response.Valid = false
			response.Errors = append(response.Errors, metricsResult.Error)
		}
	}

	// Final result
	response.Valid = true
	response.Duration = time.Since(startTime).Milliseconds()

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("validation_level", string(req.Validation)).
		Int64("duration_ms", response.Duration).
		Bool("valid", response.Valid).
		Msg("Universal configuration validation completed")

	return response, nil
}

// validateProbeSchema validates the probe configuration against its expected schema
func (cm *ConfigurationManager) validateProbeSchema(probeName string, config map[string]interface{}) ValidationTestResult {
	startTime := time.Now()

	cm.logger.Debug().
		Str("probe", probeName).
		Any("config", configuration.SanitizeParamsForLog(config)).
		Msg("Validating probe schema")

	result := ValidationTestResult{
		Passed:   false,
		Duration: 0,
	}

	// Validate based on probe type
	switch probeName {
	case "redfish":
		result = cm.validateRedfishSchema(config)
	case "host", "cpu", "memory", "network", "logicaldisk":
		result = cm.validateSystemProbeSchema(config)
	case "ping_gateway":
		result = cm.validateGatewayProbeSchema(config)
	case "ping_webapp", "load_webapp":
		result = cm.validateWebAppProbeSchema(config)
	case "syslog":
		result = cm.validateSyslogProbeSchema(config)
	default:
		result.Error = fmt.Sprintf("unsupported probe type: %s", probeName)
		result.Duration = time.Since(startTime).Milliseconds()
		return result
	}

	result.Duration = time.Since(startTime).Milliseconds()

	cm.logger.Debug().
		Str("probe", probeName).
		Bool("passed", result.Passed).
		Int64("duration_ms", result.Duration).
		Msg("Schema validation completed")

	return result
}

// validateProbeConnectivity tests network connectivity to the target system
func (cm *ConfigurationManager) validateProbeConnectivity(probeName string, config map[string]interface{}, target string, timeout int) ValidationTestResult {
	startTime := time.Now()

	cm.logger.Debug().
		Str("probe", probeName).
		Str("target", target).
		Int("timeout", timeout).
		Msg("Testing probe connectivity")

	result := ValidationTestResult{
		Passed: false,
	}

	// For network-based probes, test connectivity
	switch probeName {
	case "redfish":
		result = cm.testRedfishConnectivity(config, timeout)
	case "ping_webapp", "load_webapp":
		result = cm.testWebAppConnectivity(config, timeout)
	case "syslog":
		result = cm.testSyslogConnectivity(config, timeout)
	default:
		// For local probes (cpu, memory, etc.), connectivity always passes
		result.Passed = true
		result.Details = "Local probe - no connectivity test required"
	}

	result.Duration = time.Since(startTime).Milliseconds()

	cm.logger.Debug().
		Str("probe", probeName).
		Str("target", target).
		Bool("passed", result.Passed).
		Int64("duration_ms", result.Duration).
		Msg("Connectivity test completed")

	return result
}

// validateProbeMetrics attempts to collect actual metrics from the configured probe
func (cm *ConfigurationManager) validateProbeMetrics(probeName string, config map[string]interface{}, target string, timeout int) (ValidationTestResult, []PreviewMetric) {
	startTime := time.Now()

	cm.logger.Debug().
		Str("probe", probeName).
		Str("target", target).
		Msg("Testing probe metrics collection")

	result := ValidationTestResult{
		Passed: false,
	}
	var previewMetrics []PreviewMetric

	// This would require instantiating the actual probe temporarily
	// For now, we'll simulate this functionality
	result.Passed = true
	result.Details = "Metrics validation not yet implemented - configuration appears valid"

	// Create some mock preview metrics based on probe type
	previewMetrics = cm.generateMockPreviewMetrics(probeName)

	result.Duration = time.Since(startTime).Milliseconds()

	cm.logger.Debug().
		Str("probe", probeName).
		Bool("passed", result.Passed).
		Int("preview_count", len(previewMetrics)).
		Int64("duration_ms", result.Duration).
		Msg("Metrics validation completed")

	return result, previewMetrics
}

// Helper methods for specific probe schema validation

// validateRedfishSchema validates Redfish probe configuration
func (cm *ConfigurationManager) validateRedfishSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// Required fields for Redfish
	requiredFields := []string{"endpoint", "username", "password"}
	for _, field := range requiredFields {
		if _, exists := config[field]; !exists {
			result.Passed = false
			result.Error = fmt.Sprintf("missing required field: %s", field)
			return result
		}
	}

	// Validate endpoint format
	if endpoint, ok := config["endpoint"].(string); ok {
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			result.Passed = false
			result.Error = "endpoint must be a valid HTTP/HTTPS URL"
			return result
		}
	} else {
		result.Passed = false
		result.Error = "endpoint must be a string"
		return result
	}

	result.Details = "Redfish configuration schema is valid"
	return result
}

// validateSystemProbeSchema validates system probe configuration (cpu, memory, etc.)
func (cm *ConfigurationManager) validateSystemProbeSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// System probes typically have minimal configuration requirements
	// Validate interval if specified
	if interval, exists := config["interval"]; exists {
		if _, ok := interval.(int); !ok {
			if floatVal, ok := interval.(float64); ok && floatVal == float64(int(floatVal)) {
				// Accept whole number floats (JSON compatibility)
			} else {
				result.Passed = false
				result.Error = "interval must be an integer (seconds)"
				return result
			}
		}
	}

	result.Details = "System probe configuration schema is valid"
	return result
}

// validateGatewayProbeSchema validates gateway ping probe configuration
func (cm *ConfigurationManager) validateGatewayProbeSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// Gateway probe may need target or will auto-discover
	if target, exists := config["target"]; exists {
		if _, ok := target.(string); !ok {
			result.Passed = false
			result.Error = "target must be a string (IP address or hostname)"
			return result
		}
	}

	result.Details = "Gateway probe configuration schema is valid"
	return result
}

// validateWebAppProbeSchema validates webapp probe configuration
func (cm *ConfigurationManager) validateWebAppProbeSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// Required field: url
	if _, exists := config["url"]; !exists {
		result.Passed = false
		result.Error = "missing required field: url"
		return result
	}

	// Validate URL format
	if url, ok := config["url"].(string); ok {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			result.Passed = false
			result.Error = "url must be a valid HTTP/HTTPS URL"
			return result
		}
	} else {
		result.Passed = false
		result.Error = "url must be a string"
		return result
	}

	result.Details = "WebApp probe configuration schema is valid"
	return result
}

// validateSyslogProbeSchema validates syslog probe configuration
func (cm *ConfigurationManager) validateSyslogProbeSchema(config map[string]interface{}) ValidationTestResult {
	result := ValidationTestResult{Passed: true}

	// Validate port if specified
	if port, exists := config["port"]; exists {
		if portInt, ok := port.(int); ok {
			if portInt < 1 || portInt > 65535 {
				result.Passed = false
				result.Error = "port must be between 1 and 65535"
				return result
			}
		} else if portFloat, ok := port.(float64); ok && portFloat == float64(int(portFloat)) {
			// Accept whole number floats
			if int(portFloat) < 1 || int(portFloat) > 65535 {
				result.Passed = false
				result.Error = "port must be between 1 and 65535"
				return result
			}
		} else {
			result.Passed = false
			result.Error = "port must be an integer"
			return result
		}
	}

	// Validate protocol if specified
	if protocol, exists := config["protocol"]; exists {
		if protocolStr, ok := protocol.(string); ok {
			if protocolStr != "udp" && protocolStr != "tcp" {
				result.Passed = false
				result.Error = "protocol must be 'udp' or 'tcp'"
				return result
			}
		} else {
			result.Passed = false
			result.Error = "protocol must be a string"
			return result
		}
	}

	result.Details = "Syslog probe configuration schema is valid"
	return result
}

// Helper methods for connectivity testing

// testRedfishConnectivity tests connectivity to a Redfish endpoint
func (cm *ConfigurationManager) testRedfishConnectivity(config map[string]interface{}, timeout int) ValidationTestResult {
	result := ValidationTestResult{Passed: false}

	endpoint, exists := config["endpoint"]
	if !exists {
		result.Error = "endpoint not specified for connectivity test"
		return result
	}

	endpointStr, ok := endpoint.(string)
	if !ok {
		result.Error = "endpoint must be a string"
		return result
	}

	// Test basic HTTP connectivity to the Redfish service root
	client := newConnectivityClient(time.Duration(timeout) * time.Second)

	// Try to reach the Redfish service root
	testURL := strings.TrimRight(endpointStr, "/") + "/redfish/v1/"
	resp, err := client.Get(testURL)
	if err != nil {
		result.Error = fmt.Sprintf("connectivity test failed: %v", err)
		return result
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			cm.logger.Debug().Err(err).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode == 200 || resp.StatusCode == 401 {
		// 200 = accessible, 401 = accessible but auth required (expected)
		result.Passed = true
		result.Details = fmt.Sprintf("Redfish service accessible (HTTP %d)", resp.StatusCode)
	} else {
		result.Error = fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode)
	}

	return result
}

// testWebAppConnectivity tests connectivity to a web application
func (cm *ConfigurationManager) testWebAppConnectivity(config map[string]interface{}, timeout int) ValidationTestResult {
	result := ValidationTestResult{Passed: false}

	url, exists := config["url"]
	if !exists {
		result.Error = "url not specified for connectivity test"
		return result
	}

	urlStr, ok := url.(string)
	if !ok {
		result.Error = "url must be a string"
		return result
	}

	// Test HTTP connectivity
	client := newConnectivityClient(time.Duration(timeout) * time.Second)

	resp, err := client.Get(urlStr)
	if err != nil {
		result.Error = fmt.Sprintf("connectivity test failed: %v", err)
		return result
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			cm.logger.Debug().Err(err).Msg("Failed to close response body")
		}
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		// Any 2xx, 3xx, or 4xx is considered "reachable"
		result.Passed = true
		result.Details = fmt.Sprintf("WebApp accessible (HTTP %d)", resp.StatusCode)
	} else {
		result.Error = fmt.Sprintf("unexpected HTTP status: %d", resp.StatusCode)
	}

	return result
}

// testSyslogConnectivity tests syslog server connectivity (basic port check)
func (cm *ConfigurationManager) testSyslogConnectivity(config map[string]interface{}, timeout int) ValidationTestResult {
	result := ValidationTestResult{Passed: true}
	result.Details = "Syslog connectivity test not implemented - assuming valid"
	// TODO: Implement actual syslog server connectivity test
	return result
}

// generateMockPreviewMetrics creates sample metrics for preview purposes
func (cm *ConfigurationManager) generateMockPreviewMetrics(probeName string) []PreviewMetric {
	timestamp := time.Now().Unix()

	switch probeName {
	case "redfish":
		return []PreviewMetric{
			{Name: "system.health", Value: 1, Tags: map[string]string{"system_id": "1"}, Timestamp: timestamp},
			{Name: "thermal.cpu.0.temperature", Value: 45.2, Tags: map[string]string{"cpu_id": "0", "system_id": "1"}, Timestamp: timestamp},
			{Name: "power.psu.0.output_watts", Value: 350.5, Tags: map[string]string{"psu_id": "0", "system_id": "1"}, Timestamp: timestamp},
		}
	case "cpu":
		return []PreviewMetric{
			{Name: "cpu.usage_percent", Value: 25.8, Tags: map[string]string{"cpu": "all"}, Timestamp: timestamp},
			{Name: "cpu.cores", Value: 8, Tags: map[string]string{"cpu": "all"}, Timestamp: timestamp},
		}
	case "memory":
		return []PreviewMetric{
			{Name: "memory.usage_percent", Value: 62.3, Tags: map[string]string{}, Timestamp: timestamp},
			{Name: "memory.available_mb", Value: 6144, Tags: map[string]string{}, Timestamp: timestamp},
		}
	case "ping_webapp":
		return []PreviewMetric{
			{Name: "webapp.ping_ms", Value: 45.2, Tags: map[string]string{"url": "example.com"}, Timestamp: timestamp},
			{Name: "webapp.available", Value: 1, Tags: map[string]string{"url": "example.com"}, Timestamp: timestamp},
		}
	default:
		return []PreviewMetric{
			{Name: fmt.Sprintf("%s.status", probeName), Value: 1, Tags: map[string]string{"probe": probeName}, Timestamp: timestamp},
		}
	}
}

// HTTP Handler Methods for Universal Configuration

// HandleUniversalConfigValidation handles POST requests for universal configuration validation
func (cm *ConfigurationManager) HandleUniversalConfigValidation(w http.ResponseWriter, r *http.Request) {
	cm.logger.Debug().Msg("Handling universal configuration validation request")

	// Parse request body under a size limit
	var req UniversalConfigRequest
	if !decodeConfigRequest(w, r, &req) {
		return
	}

	// Validate the configuration
	response, err := cm.ValidateUniversalConfig(&req)
	if err != nil {
		cm.logger.Error().
			Err(err).
			Str("probe", req.Probe).
			Msg("Universal configuration validation failed")

		http.Error(w, fmt.Sprintf("Validation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to encode universal config validation response")

		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("validation_level", string(req.Validation)).
		Bool("valid", response.Valid).
		Int64("duration_ms", response.Duration).
		Msg("Universal configuration validation request completed")
}

// HandleUniversalConfigPreview handles POST requests for configuration preview (same as validation but different endpoint)
func (cm *ConfigurationManager) HandleUniversalConfigPreview(w http.ResponseWriter, r *http.Request) {
	cm.logger.Debug().Msg("Handling universal configuration preview request")

	// Parse request body under a size limit
	var req UniversalConfigRequest
	if !decodeConfigRequest(w, r, &req) {
		return
	}

	// Force validation mode to "schema" for preview (fast validation only)
	if req.Validation == "" || req.Validation == ValidationFull {
		req.Validation = ValidationSchemaOnly
		cm.logger.Debug().
			Str("probe", req.Probe).
			Msg("Preview mode: forcing validation to schema-only for performance")
	}

	// Validate the configuration
	response, err := cm.ValidateUniversalConfig(&req)
	if err != nil {
		cm.logger.Error().
			Err(err).
			Str("probe", req.Probe).
			Msg("Universal configuration preview failed")

		http.Error(w, fmt.Sprintf("Preview failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to encode universal config preview response")

		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("validation_level", string(req.Validation)).
		Bool("valid", response.Valid).
		Int64("duration_ms", response.Duration).
		Msg("Universal configuration preview request completed")
}

// HandleUniversalConfigTest handles POST requests for full configuration testing
func (cm *ConfigurationManager) HandleUniversalConfigTest(w http.ResponseWriter, r *http.Request) {
	cm.logger.Debug().Msg("Handling universal configuration test request")

	// Parse request body under a size limit
	var req UniversalConfigRequest
	if !decodeConfigRequest(w, r, &req) {
		return
	}

	// Force validation mode to "full" for comprehensive testing
	req.Validation = ValidationFull
	cm.logger.Debug().
		Str("probe", req.Probe).
		Msg("Test mode: forcing full validation including connectivity and metrics")

	// Validate the configuration
	response, err := cm.ValidateUniversalConfig(&req)
	if err != nil {
		cm.logger.Error().
			Err(err).
			Str("probe", req.Probe).
			Msg("Universal configuration test failed")

		http.Error(w, fmt.Sprintf("Test failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		cm.logger.Error().
			Err(err).
			Msg("Failed to encode universal config test response")

		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}

	cm.logger.Info().
		Str("probe", req.Probe).
		Str("validation_level", string(req.Validation)).
		Bool("valid", response.Valid).
		Int64("duration_ms", response.Duration).
		Int("preview_metrics_count", len(response.PreviewMetrics)).
		Msg("Universal configuration test request completed")
}

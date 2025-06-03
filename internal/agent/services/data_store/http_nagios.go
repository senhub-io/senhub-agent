// senhub-agent/internal/agent/services/data_store/http_nagios.go
package data_store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/logger"
)

// NagiosManager handles all Nagios-related endpoints and processing
type NagiosManager struct {
	logger   *logger.ModuleLogger
	strategy *HTTPSyncStrategy // Reference to parent strategy for access to other modules
}

// NewNagiosManager creates a new Nagios endpoints and processing manager
func NewNagiosManager(strategy *HTTPSyncStrategy, logger *logger.ModuleLogger) *NagiosManager {
	return &NagiosManager{
		logger:   logger,
		strategy: strategy,
	}
}

// Note: Nagios types are defined in other modules (http_config.go, http_metrics.go)
// This module focuses on HTTP handlers and processing logic

// Nagios Endpoints

// HandleNagiosMetricsGET handles GET requests for Nagios format metrics by probe
func (n *NagiosManager) HandleNagiosMetricsGET(w http.ResponseWriter, r *http.Request) {
	_, authenticated := n.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	probeName := vars["probe"]

	n.logger.Info().Str("probe", probeName).Msg("🔄 Nagios endpoint - Request received")

	// Parse query parameters
	filter := n.strategy.parseMetricFilter(r)
	
	// Get probe metrics from cache
	metrics := n.strategy.cache.GetProbeMetrics(probeName)
	if len(metrics) == 0 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(500)
		w.Write([]byte("CRITICAL - No metrics available for probe " + probeName))
		return
	}

	// Apply filters
	filteredMetrics := n.strategy.applyMetricFilter(metrics, filter)
	
	// Generate simple probe-based Nagios response
	response := n.generateSimpleNagiosResponse(probeName, filteredMetrics)
	
	w.Header().Set("Content-Type", "text/plain")
	if response.Status >= 2 {
		w.WriteHeader(500)
	}
	w.Write([]byte(fmt.Sprintf("%s - %s | %s", response.StatusText, response.Message, response.PerfData)))
}

// Removed: HandleNagiosCheck - /nagios/check/{check_name} endpoint not needed

// HandleNagiosMetrics handles GET/POST requests for all Nagios metrics
func (n *NagiosManager) HandleNagiosMetrics(w http.ResponseWriter, r *http.Request) {
	_, authenticated := n.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	n.logger.Info().Str("method", r.Method).Msg("🔄 Nagios metrics endpoint - Request received")

	var nagiosRequest NagiosRequest
	
	if r.Method == "POST" {
		// Parse POST body for dynamic configuration
		if err := json.NewDecoder(r.Body).Decode(&nagiosRequest); err != nil {
			n.logger.Error().Err(err).Msg("Failed to parse Nagios request body")
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	}

	// Parse query parameters
	filter := n.strategy.parseMetricFilter(r)
	overrides := n.parseNagiosOverrides(r)
	
	// Load configuration
	config := n.loadNagiosConfig()
	
	// Execute all checks or specific check
	var responses []NagiosResponse
	if nagiosRequest.CheckName != "" {
		check := n.findNagiosCheck(config, nagiosRequest.CheckName)
		if check != nil {
			response := n.executeNagiosCheck(check, filter, overrides)
			responses = append(responses, response)
		}
	} else {
		// Execute all checks
		for _, check := range config.Checks {
			response := n.executeNagiosCheck(&check, filter, overrides)
			responses = append(responses, response)
		}
	}

	// Return JSON response for multiple checks
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"checks": responses,
		"count":  len(responses),
	})
}

// HandleNagiosChecks lists all available Nagios checks
func (n *NagiosManager) HandleNagiosChecks(w http.ResponseWriter, r *http.Request) {
	_, authenticated := n.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	n.logger.Info().Msg("🔄 Nagios checks discovery endpoint - Request received")

	// Load Nagios configuration
	config := n.loadNagiosConfig()
	
	// Build response with check information
	var checks []NagiosCheckInfo
	for _, check := range config.Checks {
		checkInfo := NagiosCheckInfo{
			Name:        check.Name,
			Description: check.Description,
			ProbeFilter: check.ProbeFilter,
			MetricCount: len(check.Metrics),
			TagFilters:  check.TagFilters,
			Metrics:     make([]NagiosMetricInfo, len(check.Metrics)),
		}
		
		// Add metric information
		for i, metric := range check.Metrics {
			checkInfo.Metrics[i] = NagiosMetricInfo{
				Channel:     metric.Channel,
				Aggregation: metric.Aggregation,
				Warning:     metric.Warning,
				Critical:    metric.Critical,
				Unit:        metric.Unit,
				Invert:      metric.Invert,
				TagContext:  metric.TagContext,
			}
		}
		
		checks = append(checks, checkInfo)
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version":     config.Version,
		"description": config.Description,
		"checks":      checks,
		"count":       len(checks),
	})
}

// Nagios Processing Methods

// loadNagiosConfig loads the Nagios configuration from YAML file
func (n *NagiosManager) loadNagiosConfig() *NagiosConfig {
	return n.strategy.configManager.LoadNagiosConfig()
}

// findNagiosCheck finds a check by name in the configuration
func (n *NagiosManager) findNagiosCheck(config *NagiosConfig, checkName string) *NagiosCheck {
	return n.strategy.configManager.FindNagiosCheck(config, checkName)
}

// parseNagiosOverrides parses query parameters for Nagios threshold overrides
func (n *NagiosManager) parseNagiosOverrides(r *http.Request) NagiosOverrides {
	return n.strategy.configManager.ParseNagiosOverrides(r)
}

// executeNagiosCheck executes a Nagios check with filtering and overrides
func (n *NagiosManager) executeNagiosCheck(check *NagiosCheck, filter MetricFilter, overrides NagiosOverrides) NagiosResponse {
	n.logger.Debug().
		Str("check", check.Name).
		Interface("filter", filter).
		Interface("overrides", overrides).
		Msg("Executing Nagios check")

	// Get all metrics from cache
	allMetrics := n.strategy.cache.GetAllMetrics()
	
	// Apply probe filter if specified
	var metrics []CachedMetric
	if check.ProbeFilter != "" {
		metrics = n.strategy.cache.GetProbeMetrics(check.ProbeFilter)
	} else {
		metrics = allMetrics
	}

	if len(metrics) == 0 {
		return NagiosResponse{
			Status:     3, // UNKNOWN
			StatusText: "UNKNOWN",
			Message:    fmt.Sprintf("No metrics available for check %s", check.Name),
			PerfData:   "",
		}
	}

	// Apply tag filters from check configuration
	metrics = n.applyNagiosTagFilters(metrics, check.TagFilters)
	
	// Apply additional filters from query parameters
	metrics = n.strategy.applyMetricFilter(metrics, filter)

	if len(metrics) == 0 {
		return NagiosResponse{
			Status:     3, // UNKNOWN
			StatusText: "UNKNOWN",
			Message:    fmt.Sprintf("No metrics match filters for check %s", check.Name),
			PerfData:   "",
		}
	}

	// Process each metric in the check
	var results []NagiosMetricResult
	overallStatus := 0 // OK
	var messages []string
	var perfDataItems []string

	for _, metricDef := range check.Metrics {
		result := n.processNagiosMetric(metricDef, metrics, overrides)
		results = append(results, result)
		
		// Update overall status (worst case wins)
		if result.Status > overallStatus {
			overallStatus = result.Status
		}
		
		if result.Message != "" {
			messages = append(messages, result.Message)
		}
		
		if result.PerfData != "" {
			perfDataItems = append(perfDataItems, result.PerfData)
		}
	}

	// Build response
	statusText := n.getStatusText(overallStatus)
	
	var message string
	if len(messages) > 0 {
		message = strings.Join(messages, ", ")
	} else {
		message = fmt.Sprintf("%s - %s", check.Description, statusText)
	}

	return NagiosResponse{
		Status:     overallStatus,
		StatusText: statusText,
		Message:    message,
		PerfData:   strings.Join(perfDataItems, " "),
	}
}

// processNagiosMetric processes a single metric definition against available data
func (n *NagiosManager) processNagiosMetric(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	return n.strategy.metricsProcessor.ProcessNagiosMetric(metricDef, metrics, overrides)
}

// applyNagiosTagFilters applies tag filters from Nagios check configuration
func (n *NagiosManager) applyNagiosTagFilters(metrics []CachedMetric, filters []NagiosTagFilter) []CachedMetric {
	return n.strategy.metricsProcessor.ApplyNagiosTagFilters(metrics, filters)
}

// getStatusText converts numeric status to text
func (n *NagiosManager) getStatusText(status int) string {
	switch status {
	case 0:
		return "OK"
	case 1:
		return "WARNING"
	case 2:
		return "CRITICAL"
	case 3:
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

// generateSimpleNagiosResponse generates a simple Nagios response for probe-based queries
func (n *NagiosManager) generateSimpleNagiosResponse(probeName string, metrics []CachedMetric) NagiosResponse {
	return n.strategy.metricsProcessor.GenerateSimpleNagiosResponse(probeName, metrics)
}

// Utility Methods for Nagios processing

// aggregateValues aggregates a slice of values using the specified method
func (n *NagiosManager) aggregateValues(values []float64, aggregation string) float64 {
	if len(values) == 0 {
		return 0
	}
	
	switch aggregation {
	case "average", "avg":
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values))
		
	case "max":
		max := values[0]
		for _, v := range values {
			if v > max {
				max = v
			}
		}
		return max
		
	case "min":
		min := values[0]
		for _, v := range values {
			if v < min {
				min = v
			}
		}
		return min
		
	case "sum":
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum
		
	default:
		// Default to average
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values))
	}
}

// evaluateThreshold evaluates a value against warning and critical thresholds
func (n *NagiosManager) evaluateThreshold(value float64, warning, critical string, invert bool) int {
	// Parse thresholds
	warnThreshold, err := strconv.ParseFloat(warning, 64)
	if err != nil {
		return 3 // UNKNOWN
	}
	
	critThreshold, err := strconv.ParseFloat(critical, 64)
	if err != nil {
		return 3 // UNKNOWN
	}
	
	var status int
	if invert {
		// Lower values are worse (e.g., free disk space)
		if value <= critThreshold {
			status = 2 // CRITICAL
		} else if value <= warnThreshold {
			status = 1 // WARNING
		} else {
			status = 0 // OK
		}
	} else {
		// Higher values are worse (e.g., CPU usage)
		if value >= critThreshold {
			status = 2 // CRITICAL
		} else if value >= warnThreshold {
			status = 1 // WARNING
		} else {
			status = 0 // OK
		}
	}
	
	return status
}

// buildPerfData builds Nagios performance data string with optimal graphing format
func (n *NagiosManager) buildPerfData(name string, value float64, warning, critical, unit string) string {
	// Clean label name (no spaces, special chars)
	cleanName := n.cleanPerfDataLabel(name)
	
	// Convert unit to standard Nagios UOM
	standardUOM := n.convertToStandardUOM(unit)
	
	// Determine min/max values based on metric type
	min, max := n.getPerfDataMinMax(unit, value)
	
	// Format: label=value[UOM];[warn];[crit];[min];[max]
	perfData := fmt.Sprintf("%s=%.2f", cleanName, value)
	
	if standardUOM != "" {
		perfData += standardUOM
	}
	
	perfData += fmt.Sprintf(";%s;%s", warning, critical)
	
	// Add min/max for better graphing
	if min != "" || max != "" {
		perfData += fmt.Sprintf(";%s;%s", min, max)
	}
	
	return perfData
}

// cleanPerfDataLabel cleans metric names for Nagios performance data
func (n *NagiosManager) cleanPerfDataLabel(name string) string {
	// Replace spaces and dots with underscores
	cleaned := strings.ReplaceAll(name, " ", "_")
	cleaned = strings.ReplaceAll(cleaned, ".", "_")
	cleaned = strings.ReplaceAll(cleaned, "-", "_")
	
	// Remove special characters except underscore
	var result strings.Builder
	for _, r := range cleaned {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		}
	}
	
	// Limit length to 64 characters (Nagios recommendation)
	final := result.String()
	if len(final) > 64 {
		final = final[:64]
	}
	
	return final
}

// convertToStandardUOM converts units to standard Nagios UOM
func (n *NagiosManager) convertToStandardUOM(unit string) string {
	switch strings.ToLower(unit) {
	case "bytes":
		return "B"
	case "kilobytes", "kb":
		return "KB"
	case "megabytes", "mb":
		return "MB"
	case "gigabytes", "gb":
		return "GB"
	case "milliseconds", "ms":
		return "ms"
	case "seconds", "sec", "s":
		return "s"
	case "microseconds", "us":
		return "us"
	case "percent", "%":
		return "%"
	case "#", "count", "counter":
		return "c"
	case "°c", "celsius":
		return "" // Temperature without UOM is cleaner
	case "bytes/sec", "bytes/second":
		return "B/s"
	default:
		// Return original unit if no standard mapping
		return unit
	}
}

// getPerfDataMinMax determines appropriate min/max values for graphing
func (n *NagiosManager) getPerfDataMinMax(unit string, value float64) (string, string) {
	switch strings.ToLower(unit) {
	case "%", "percent":
		return "0", "100"
	case "°c", "celsius":
		return "0", "100"
	case "ms", "milliseconds", "s", "seconds":
		return "0", ""
	case "bytes", "b", "kb", "mb", "gb":
		return "0", ""
	case "#", "count", "counter":
		return "0", ""
	default:
		// For load averages and other metrics, use 0 as min, no max
		return "0", ""
	}
}
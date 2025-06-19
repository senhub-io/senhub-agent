// senhub-agent/internal/agent/services/data_store/http_nagios.go
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	filter := n.strategy.metricsProcessor.ParseMetricFilter(r)

	// Get probe metrics from cache
	metrics := n.strategy.cache.GetProbeMetrics(probeName)
	if len(metrics) == 0 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(500)
		if _, err := w.Write([]byte("CRITICAL - No metrics available for probe " + probeName)); err != nil {
			n.logger.Error().Err(err).Msg("Failed to write Nagios error response")
		}
		return
	}

	// Apply filters
	filteredMetrics := n.strategy.metricsProcessor.ApplyMetricFilter(metrics, filter)

	// Generate simple probe-based Nagios response
	response := n.strategy.metricsProcessor.GenerateSimpleNagiosResponse(probeName, filteredMetrics)

	w.Header().Set("Content-Type", "text/plain")
	if response.Status >= 2 {
		w.WriteHeader(500)
	}
	if _, err := w.Write([]byte(fmt.Sprintf("%s - %s | %s", response.StatusText, response.Message, response.PerfData))); err != nil {
		n.logger.Error().Err(err).Msg("Failed to write Nagios response")
	}
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
	filter := n.strategy.metricsProcessor.ParseMetricFilter(r)
	overrides := n.strategy.configManager.ParseNagiosOverrides(r)

	// Load configuration
	config := n.strategy.configManager.LoadNagiosConfig()

	// Execute all checks or specific check
	var responses []NagiosResponse
	if nagiosRequest.CheckName != "" {
		check := n.strategy.configManager.FindNagiosCheck(config, nagiosRequest.CheckName)
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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"checks": responses,
		"count":  len(responses),
	}); err != nil {
		n.logger.Error().Err(err).Msg("Failed to encode Nagios metrics response")
	}
}

// HandleNagiosChecks lists all available Nagios checks
func (n *NagiosManager) HandleNagiosChecks(w http.ResponseWriter, r *http.Request) {
	_, authenticated := n.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	n.logger.Info().Msg("🔄 Nagios checks discovery endpoint - Request received")

	// Load Nagios configuration
	config := n.strategy.configManager.LoadNagiosConfig()

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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"version":     config.Version,
		"description": config.Description,
		"checks":      checks,
		"count":       len(checks),
	}); err != nil {
		n.logger.Error().Err(err).Msg("Failed to encode Nagios checks response")
	}
}

// Nagios Processing Methods




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
	metrics = n.strategy.metricsProcessor.ApplyMetricFilter(metrics, filter)

	if len(metrics) == 0 {
		return NagiosResponse{
			Status:     3, // UNKNOWN
			StatusText: "UNKNOWN",
			Message:    fmt.Sprintf("No metrics match filters for check %s", check.Name),
			PerfData:   "",
		}
	}

	// Process each metric in the check
	overallStatus := 0 // OK
	var messages []string
	var perfDataItems []string

	for _, metricDef := range check.Metrics {
		result := n.strategy.metricsProcessor.ProcessNagiosMetric(metricDef, metrics, overrides)

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


// Utility Methods for Nagios processing







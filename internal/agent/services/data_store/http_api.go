// senhub-agent/internal/agent/services/data_store/http_api.go
package data_store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/logger"
)

// APIManager handles all API endpoints (PRTG, SenHub, Info, Discovery)
type APIManager struct {
	logger   *logger.ModuleLogger
	strategy *HTTPSyncStrategy // Reference to parent strategy for access to other modules
}

// NewAPIManager creates a new API endpoints manager
func NewAPIManager(strategy *HTTPSyncStrategy, logger *logger.ModuleLogger) *APIManager {
	return &APIManager{
		logger:   logger,
		strategy: strategy,
	}
}

// SenHub API Endpoints

// HandleSenHubMetricsGET handles GET requests for SenHub raw format metrics
func (a *APIManager) HandleSenHubMetricsGET(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	probeName := vars["probe"]

	a.logger.Debug().
		Str("probe", probeName).
		Msg("SenHub metrics GET request received")

	// Get metrics from cache for the specified probe and convert to SenHub format
	senHubMetrics := a.strategy.getSenHubMetricsForProbe(probeName)

	// Create wrapped response
	response := SenHubResponse{
		Metrics: senHubMetrics,
		Status:  "OK",
		Message: "Metrics successfully retrieved.",
		Date:    time.Now().UnixMilli(),
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode SenHub metrics response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	a.logger.Info().
		Str("probe", probeName).
		Int("metrics_count", len(senHubMetrics)).
		Msg("✅ SenHub metrics response sent")
}

// PRTG API Endpoints

// HandlePRTGMetricsGET handles GET requests for PRTG metrics
func (a *APIManager) HandlePRTGMetricsGET(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	probeName := vars["probe"]

	// Parse query parameters
	filter := a.strategy.parseMetricFilter(r)
	
	a.logger.Debug().
		Str("probe", probeName).
		Interface("filter", filter).
		Msg("PRTG metrics GET request received")

	// Get metrics from cache for the specified probe with filters
	channels := a.strategy.getMetricsForProbeWithFilter(probeName, filter)

	// Build PRTG response
	response := PRTGResponse{
		PRTG: PRTGResult{
			Result: channels,
		},
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	a.logger.Info().
		Str("probe", probeName).
		Int("channels", len(channels)).
		Msg("PRTG GET response sent")
}

// HandlePRTGMetrics handles POST requests for PRTG metrics (legacy)
func (a *APIManager) HandlePRTGMetrics(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	// Parse request body
	var req PRTGRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.logger.Error().Err(err).Msg("Failed to parse request body")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	a.logger.Debug().
		Str("probe", req.Probe).
		Str("target", req.Target).
		Msg("PRTG metrics request received")

	// For now, emulate configuration handling - just log the config
	a.logger.Debug().Any("config", req.Config).Msg("Emulating config handling")

	// Get metrics from cache for the specified probe
	channels := a.strategy.getMetricsForProbe(req.Probe)

	// Build PRTG response
	response := PRTGResponse{
		PRTG: PRTGResult{
			Result: channels,
		},
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	a.logger.Info().
		Str("probe", req.Probe).
		Int("channels", len(channels)).
		Msg("PRTG response sent")
}

// HandleListProbes handles GET requests to list available probes
func (a *APIManager) HandleListProbes(w http.ResponseWriter, r *http.Request) {
	// Authenticate request
	if _, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r); !authenticated {
		return
	}

	a.logger.Debug().Msg("List probes request received")

	// Get probe statistics from cache
	probeStats := a.strategy.cache.GetProbeStatistics()

	// Build response
	probes := make([]ProbeInfo, 0, len(probeStats))
	for _, stats := range probeStats {
		lastUpdate := ""
		if !stats.LastUpdate.IsZero() {
			lastUpdate = stats.LastUpdate.Format(time.RFC3339)
		}
		
		probes = append(probes, ProbeInfo{
			Name:         stats.Name,
			MetricsCount: stats.MetricsCount,
			LastUpdate:   lastUpdate,
		})
	}

	response := ProbesListResponse{
		Probes: probes,
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode probes list response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	a.logger.Info().
		Int("probes_count", len(probes)).
		Msg("Probes list response sent")
}

// Info API Endpoints

// HandleInfoProbes lists all available probes
func (a *APIManager) HandleInfoProbes(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}
	
	a.strategy.cache.mu.RLock()
	defer a.strategy.cache.mu.RUnlock()
	
	probeMetrics := make(map[string]int)
	var probes []string
	totalMetrics := 0
	
	for probe, tsKeys := range a.strategy.cache.probeIndex {
		count := len(tsKeys)
		probes = append(probes, probe)
		probeMetrics[probe] = count
		totalMetrics += count
	}
	
	response := ProbesInfoResponse{
		Probes:       probes,
		ProbeMetrics: probeMetrics,
		TotalMetrics: totalMetrics,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleInfoEndpoints provides discovery of available endpoints
func (a *APIManager) HandleInfoEndpoints(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}
	
	// Define all possible endpoints with their descriptions
	allEndpoints := map[string]string{
		"senhub": "SenHub native JSON format for time-series data",
		"prtg":   "PRTG JSON format for monitoring integration",
		"nagios": "Nagios-compatible output format",
	}
	
	var endpoints []EndpointInfoStatus
	for name, description := range allEndpoints {
		endpoints = append(endpoints, EndpointInfoStatus{
			Name:        name,
			Description: description,
			Enabled:     a.strategy.configManager.IsEndpointEnabled(name),
		})
	}
	
	response := EndpointsInfoResponse{
		Endpoints: endpoints,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleInfoSystem provides system status and resource information
func (a *APIManager) HandleInfoSystem(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}
	
	// Get comprehensive health information from health manager
	systemHealth := a.strategy.healthManager.BuildSystemHealth()
	
	// Get cache stats for additional info
	a.strategy.cache.mu.RLock()
	totalMetrics := 0
	for _, tsKeys := range a.strategy.cache.probeIndex {
		totalMetrics += len(tsKeys)
	}
	a.strategy.cache.mu.RUnlock()
	
	// Build system info response
	// Parse version and commit information
	versionInfo := a.strategy.parseVersionInfo()
	version := versionInfo.Version
	commit := versionInfo.Commit
	
	response := SystemInfoResponse{
		Status:    "running",
		Version:   version,
		Commit:    commit,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Port:      a.strategy.port,
		Uptime:    systemHealth.Uptime,
		Health:    systemHealth.Health,
		Cache: CacheInfoResponse{
			TotalMetrics: totalMetrics,
			TTL:          a.strategy.cache.ttl.String(),
			MemoryUsage:  fmt.Sprintf("%.2f MB", systemHealth.Resources.MemoryUsageMB),
		},
		Resources: systemHealth.Resources,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleInfoTags provides tag discovery for a specific probe
func (a *APIManager) HandleInfoTags(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	probeName := vars["probe"]
	
	a.strategy.cache.mu.RLock()
	defer a.strategy.cache.mu.RUnlock()
	
	// Get time series keys for the probe
	tsKeys, exists := a.strategy.cache.probeIndex[probeName]
	if !exists {
		http.Error(w, "Probe not found", http.StatusNotFound)
		return
	}
	
	// Analyze tags from all metrics of this probe
	tagValues := make(map[string]map[string]int)
	metrics := make(map[string]bool)
	
	for tsKey := range tsKeys {
		if metric, exists := a.strategy.cache.timeSeries[tsKey]; exists {
			// Collect metric names
			metrics[metric.MetricName] = true
			
			// Collect tag values
			for tagKey, tagValue := range metric.Tags {
				if tagValues[tagKey] == nil {
					tagValues[tagKey] = make(map[string]int)
				}
				tagValues[tagKey][tagValue]++
			}
		}
	}
	
	// Convert to response format
	tags := make(map[string]TagInfo)
	for tagKey, values := range tagValues {
		var valueList []string
		for value := range values {
			valueList = append(valueList, value)
		}
		
		tags[tagKey] = TagInfo{
			Values:      valueList,
			Description: a.strategy.getTagDescription(tagKey),
			SampleCount: len(valueList),
		}
	}
	
	var metricList []string
	for metric := range metrics {
		metricList = append(metricList, metric)
	}
	
	response := TagInfoResponse{
		Probe:        probeName,
		Tags:         tags,
		Metrics:      metricList,
		TotalMetrics: len(metricList),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleInfoSchema provides complete schema information with examples
func (a *APIManager) HandleInfoSchema(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	probeName := vars["probe"]
	
	// Reuse tag discovery logic
	a.strategy.cache.mu.RLock()
	tsKeys, exists := a.strategy.cache.probeIndex[probeName]
	if !exists {
		a.strategy.cache.mu.RUnlock()
		http.Error(w, "Probe not found", http.StatusNotFound)
		return
	}
	
	tagValues := make(map[string]map[string]int)
	metrics := make(map[string]bool)
	
	for tsKey := range tsKeys {
		if metric, exists := a.strategy.cache.timeSeries[tsKey]; exists {
			metrics[metric.MetricName] = true
			for tagKey, tagValue := range metric.Tags {
				if tagValues[tagKey] == nil {
					tagValues[tagKey] = make(map[string]int)
				}
				tagValues[tagKey][tagValue]++
			}
		}
	}
	a.strategy.cache.mu.RUnlock()
	
	// Build tags info
	tags := make(map[string]TagInfo)
	for tagKey, values := range tagValues {
		var valueList []string
		for value := range values {
			valueList = append(valueList, value)
		}
		tags[tagKey] = TagInfo{
			Values:      valueList,
			Description: a.strategy.getTagDescription(tagKey),
			SampleCount: len(valueList),
		}
	}
	
	var metricList []string
	for metric := range metrics {
		metricList = append(metricList, metric)
	}
	
	// Generate examples
	examples := a.strategy.generateExamples(probeName, tags, metricList)
	
	response := SchemaInfoResponse{
		Probe:        probeName,
		Tags:         tags,
		Metrics:      metricList,
		TotalMetrics: len(metricList),
		Examples:     examples,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleListEndpoints lists all available API endpoints
func (a *APIManager) HandleListEndpoints(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	endpoints := []EndpointInfo{
		// Health and Discovery
		{"/health", []string{"GET"}, "Health check endpoint", "health"},
		{"/api/{agentkey}/endpoints", []string{"GET"}, "List all available endpoints", "discovery"},
		{"/api/{agentkey}/info/probes", []string{"GET"}, "List available probes", "discovery"},
		{"/api/{agentkey}/info/tags/{probe}", []string{"GET"}, "Get tags for specific probe", "discovery"},
		{"/api/{agentkey}/info/schema/{probe}", []string{"GET"}, "Get schema for specific probe", "discovery"},
		
		// Administration
		{"/api/{agentkey}/admin/cache", []string{"GET"}, "View metric cache contents", "admin"},
		{"/api/{agentkey}/admin/logs", []string{"GET"}, "View current log levels", "admin"},
		{"/api/{agentkey}/admin/logs", []string{"POST"}, "Set log levels", "admin"},
		{"/api/{agentkey}/debug/logs", []string{"GET"}, "View current log levels (legacy)", "admin"},
		{"/api/{agentkey}/debug/logs", []string{"POST"}, "Set log levels (legacy)", "admin"},
		
		// SenHub Format
		{"/api/{agentkey}/senhub/metrics/{probe}", []string{"GET"}, "Get metrics in SenHub format", "senhub"},
		
		// PRTG Format
		{"/api/{agentkey}/prtg/metrics/{probe}", []string{"GET"}, "Get metrics in PRTG format for specific probe", "prtg"},
		{"/api/{agentkey}/prtg/probes", []string{"GET"}, "List probes for PRTG", "prtg"},
		
		// Nagios Format
		{"/api/{agentkey}/nagios/metrics/{probe}", []string{"GET"}, "Get metrics in Nagios format for specific probe", "nagios"},
		// Removed: /nagios/check/{check_name} endpoint not needed
		{"/api/{agentkey}/nagios/metrics", []string{"GET", "POST"}, "Get aggregated metrics in Nagios format", "nagios"},
		{"/api/{agentkey}/nagios/checks", []string{"GET"}, "List available Nagios checks", "nagios"},
		
		// Zabbix Format (if enabled)
		{"/api/{agentkey}/zabbix/metrics/{probe}", []string{"GET"}, "Get metrics in Zabbix format", "zabbix"},
		
		// Prometheus Format (if enabled)
		{"/api/{agentkey}/prometheus/metrics", []string{"GET"}, "Get metrics in Prometheus format", "prometheus"},
	}

	response := EndpointsListResponse{
		Endpoints: endpoints,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode endpoints response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	a.logger.Info().Int("endpoints_count", len(endpoints)).Msg("Endpoints list response sent")
}

// Utility Methods for API Responses

// getSystemRuntime returns runtime information
func (a *APIManager) getSystemRuntime() map[string]interface{} {
	return map[string]interface{}{
		"go_version": runtime.Version(),
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"uptime":     time.Since(a.strategy.startTime).String(),
	}
}

// validateProbeExists checks if a probe exists in cache
func (a *APIManager) validateProbeExists(probeName string) bool {
	a.strategy.cache.mu.RLock()
	defer a.strategy.cache.mu.RUnlock()
	_, exists := a.strategy.cache.probeIndex[probeName]
	return exists
}
// senhub-agent/internal/agent/services/data_store/http_api.go
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/license"
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

// formatProbeDisplayName formats probe names for display in the UI
// Capitalizes the first letter (e.g., "netscaler" -> "Netscaler")
func formatProbeDisplayName(probeName string) string {
	if probeName == "" {
		return probeName
	}

	// Special cases for acronyms or specific probe names
	switch strings.ToLower(probeName) {
	case "cpu":
		return "CPU"
	case "otel":
		return "OTEL"
	case "prtg":
		return "PRTG"
	default:
		// Capitalize first letter, keep rest as-is
		return strings.ToUpper(string(probeName[0])) + probeName[1:]
	}
}

// SenHub API Endpoints

// PRTG API Endpoints

// HandlePRTGMetricsGET handles GET requests for PRTG metrics
func (a *APIManager) HandlePRTGMetricsGET(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	probeName := vars["probe"]

	// Normalize probe name to lowercase for cache lookup
	// (UI may send capitalized names like "Netscaler" but cache uses lowercase "netscaler")
	probeNameLower := strings.ToLower(probeName)

	// Parse query parameters
	filter := a.strategy.metricsProcessor.ParseMetricFilter(r)

	a.logger.Debug().
		Str("probe", probeNameLower).
		Interface("filter", filter).
		Msg("PRTG metrics GET request received")

	// Get metrics from cache for the specified probe with filters
	channels := a.strategy.metricsProcessor.GetPRTGMetricsForProbeWithFilter(probeNameLower, filter)

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
	channels := a.strategy.metricsProcessor.GetPRTGMetricsForProbe(req.Probe)

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
			Name:         formatProbeDisplayName(stats.Name), // Format name for UI display
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
		displayName := formatProbeDisplayName(probe) // Format name for UI display
		probes = append(probes, displayName)
		probeMetrics[displayName] = count
		totalMetrics += count
	}

	response := ProbesInfoResponse{
		Probes:       probes,
		ProbeMetrics: probeMetrics,
		TotalMetrics: totalMetrics,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode JSON response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleInfoEndpoints provides discovery of available endpoints
func (a *APIManager) HandleInfoEndpoints(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	// Define all possible endpoints with their descriptions
	allEndpoints := map[string]string{
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
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode JSON response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
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
	versionInfo := a.strategy.utilsManager.parseVersionInfo()
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
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode JSON response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleInfoTags provides tag discovery for a specific probe
func (a *APIManager) HandleInfoTags(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	probeName := vars["probe"]

	// Normalize probe name to lowercase for cache lookup
	// (UI may send capitalized names like "Netscaler" but cache uses lowercase "netscaler")
	probeNameLower := strings.ToLower(probeName)

	// Load probe definition BEFORE acquiring cache lock (GetProbeDefinition is not thread-safe)
	probeDef := a.strategy.transformerRegistry.GetProbeDefinition(probeNameLower)

	a.strategy.cache.mu.RLock()
	defer a.strategy.cache.mu.RUnlock()

	// Get time series keys for the probe
	tsKeys, exists := a.strategy.cache.probeIndex[probeNameLower]
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

	// Build tag metadata lookup from probe definition
	tagMeta := make(map[string]transformers.TagMetadata)
	if probeDef != nil && probeDef.TagMetadata != nil {
		tagMeta = probeDef.TagMetadata
	}

	// Convert to response format with metadata enrichment
	tags := make(map[string]TagInfo)
	for tagKey, values := range tagValues {
		var valueList []string
		for value := range values {
			valueList = append(valueList, value)
		}

		info := TagInfo{
			Values:      valueList,
			Description: a.strategy.utilsManager.getTagDescription(tagKey),
			SampleCount: len(valueList),
			Type:        "resource", // default
		}

		// Enrich with metadata from probe definition
		if meta, exists := tagMeta[tagKey]; exists {
			info.Type = string(meta.Type)
			info.Label = meta.Label
			info.ValueLabels = meta.ValueLabels
			info.LinkedCategories = meta.LinkedCategories
		}

		tags[tagKey] = info
	}

	var metricList []string
	for metric := range metrics {
		metricList = append(metricList, metric)
	}

	// Build categories from probe definition
	var categories []CategoryInfo
	if probeDef != nil {
		categoryCounts := make(map[string]int)
		for _, m := range probeDef.Metrics {
			if m.Category != "" {
				categoryCounts[m.Category]++
			}
		}
		for cat, count := range categoryCounts {
			label := cat // default: use raw key
			// Try to find a human label from tag metadata value_labels
			for _, meta := range tagMeta {
				if meta.Type == transformers.TagTypeCategory && meta.ValueLabels != nil {
					if humanLabel, exists := meta.ValueLabels[cat]; exists {
						label = humanLabel
						break
					}
				}
			}
			categories = append(categories, CategoryInfo{
				Key:         cat,
				Label:       label,
				MetricCount: count,
			})
		}
	}

	response := TagInfoResponse{
		Probe:        probeNameLower,
		Tags:         tags,
		Metrics:      metricList,
		TotalMetrics: len(metricList),
		Categories:   categories,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode JSON response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HandleInfoSchema provides complete schema information with examples
func (a *APIManager) HandleInfoSchema(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	vars := mux.Vars(r)
	probeName := vars["probe"]

	// Normalize probe name to lowercase for cache lookup
	// (UI may send capitalized names like "Netscaler" but cache uses lowercase "netscaler")
	probeNameLower := strings.ToLower(probeName)

	// Reuse tag discovery logic
	a.strategy.cache.mu.RLock()
	tsKeys, exists := a.strategy.cache.probeIndex[probeNameLower]
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
			Description: a.strategy.utilsManager.getTagDescription(tagKey),
			SampleCount: len(valueList),
		}
	}

	var metricList []string
	for metric := range metrics {
		metricList = append(metricList, metric)
	}

	// Generate examples
	examples := a.strategy.metricsProcessor.GenerateExamples(probeNameLower, tags, metricList)

	response := SchemaInfoResponse{
		Probe:        probeNameLower, // Use normalized name for consistency
		Tags:         tags,
		Metrics:      metricList,
		TotalMetrics: len(metricList),
		Examples:     examples,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode JSON response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
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
		{"/api/{agentkey}/license/status", []string{"GET"}, "Get license status and tier information", "admin"},

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

// License API Endpoints

// HandleLicenseStatus returns the current license status
func (a *APIManager) HandleLicenseStatus(w http.ResponseWriter, r *http.Request) {
	_, authenticated := a.strategy.authManager.AuthenticateAndExtract(w, r)
	if !authenticated {
		return
	}

	// Prepare response structure
	type LicenseStatusResponse struct {
		Status           string   `json:"status"`         // "active", "expired", "grace_period", "none"
		Tier             string   `json:"tier,omitempty"` // "free", "pro", "enterprise"
		ExpiresAt        string   `json:"expires_at,omitempty"`
		DaysRemaining    int      `json:"days_remaining,omitempty"`
		AuthorizedProbes []string `json:"authorized_probes,omitempty"`
		FreeTierProbes   []string `json:"free_tier_probes"`
		Message          string   `json:"message,omitempty"`
	}

	response := LicenseStatusResponse{
		FreeTierProbes: []string{"cpu", "memory", "logicaldisk", "network"},
	}

	// Get license token from configuration
	licenseToken := a.getLicenseToken()

	// Check if license is configured
	if licenseToken == "" {
		response.Status = "none"
		response.Tier = "free"
		response.Message = "No license configured - running in free tier mode"

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			a.logger.Error().Err(err).Msg("Failed to encode license status response")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// Validate license token
	validator, err := a.getLicenseValidator()
	if err != nil {
		a.logger.Error().Err(err).Msg("Failed to initialize license validator")
		response.Status = "error"
		response.Message = "Failed to validate license"

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			a.logger.Error().Err(err).Msg("Failed to encode license status response")
		}
		return
	}

	license, err := validator.ValidateLicense(licenseToken)
	if err != nil {
		a.logger.Error().Err(err).Msg("Invalid license token")
		response.Status = "invalid"
		response.Tier = "free"
		response.Message = "Invalid license token - running in free tier mode"

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			a.logger.Error().Err(err).Msg("Failed to encode license status response")
		}
		return
	}

	// Populate response with license info
	response.Tier = string(license.Tier)
	response.AuthorizedProbes = license.AuthorizedProbes
	response.ExpiresAt = license.ExpiresAt.Format(time.RFC3339)

	// Calculate days remaining
	daysRemaining := int(time.Until(license.ExpiresAt).Hours() / 24)

	if license.IsExpired {
		if validator.IsInGracePeriod(license) {
			response.Status = "grace_period"
			gracePeriodEnd := license.ExpiresAt.Add(time.Duration(license.GracePeriodDays) * 24 * time.Hour)
			graceDaysRemaining := int(time.Until(gracePeriodEnd).Hours() / 24)
			response.DaysRemaining = graceDaysRemaining
			response.Message = fmt.Sprintf("License expired but in grace period (%d days remaining)", graceDaysRemaining)
		} else {
			response.Status = "expired"
			response.Message = "License expired and grace period ended - only free tier probes available"
		}
	} else {
		response.Status = "active"
		response.DaysRemaining = daysRemaining
		response.Message = fmt.Sprintf("License active (%d days remaining)", daysRemaining)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.logger.Error().Err(err).Msg("Failed to encode license status response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	a.logger.Debug().Str("status", response.Status).Str("tier", response.Tier).Msg("License status response sent")
}

// getLicenseToken retrieves the license token from configuration
// Works with any AgentConfiguration that implements GetConfiguration()
func (a *APIManager) getLicenseToken() string {
	a.logger.Debug().
		Str("config_type", fmt.Sprintf("%T", a.strategy.agentConfig)).
		Msg("Getting license token from configuration")

	// All AgentConfiguration implementations should have GetConfiguration()
	// Try the interface method that all configurations support
	if configProvider, ok := a.strategy.agentConfig.(interface {
		GetConfiguration() configuration.RemoteConfigurationData
	}); ok {
		a.logger.Debug().Msg("Configuration supports GetConfiguration interface")
		config := configProvider.GetConfiguration()

		licensePreview := ""
		if len(config.Agent.License) > 20 {
			licensePreview = config.Agent.License[:20] + "..."
		} else if len(config.Agent.License) > 0 {
			licensePreview = config.Agent.License
		} else {
			licensePreview = "(empty)"
		}

		a.logger.Debug().
			Str("license_preview", licensePreview).
			Bool("has_license", len(config.Agent.License) > 0).
			Msg("Retrieved license from configuration")

		return config.Agent.License
	}

	a.logger.Warn().
		Str("config_type", fmt.Sprintf("%T", a.strategy.agentConfig)).
		Msg("Could not retrieve license token - configuration doesn't support GetConfiguration()")
	return ""
}

// getLicenseValidator creates a license validator instance
func (a *APIManager) getLicenseValidator() (license.Validator, error) {
	return license.GetDefaultValidator(7) // 7-day grace period
}

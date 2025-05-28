// senhub-agent/internal/agent/services/data_store/strategy_http.go
package data_store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/types/datapoint"
)

// HTTPSyncStrategy implements an HTTP server that exposes metrics via REST endpoints
type HTTPSyncStrategy struct {
	agentConfig         configuration.AgentConfiguration
	params              map[string]interface{}
	logger              *logger.ModuleLogger
	server              *http.Server
	cache               *MetricCache
	agentKey            string
	port                int
	bindAddress         string // IP address to bind to
	transformerRegistry *transformers.TransformerRegistry
	namingConfig        map[string]string // probe -> style mapping
}

// MetricCache stores the latest metrics in memory with TTL, organized by probe
type MetricCache struct {
	mu          sync.RWMutex
	dataByProbe map[string][]CachedMetric
	ttl         time.Duration
	stopChan    chan struct{}
}

// CachedMetric represents a stored metric with metadata
type CachedMetric struct {
	Value      interface{}
	Timestamp  time.Time
	Unit       string
	ProbeName  string
	MetricName string
	Tags       map[string]string
}

// SenHubMetric represents a metric in standardized SenHub raw format
type SenHubMetric struct {
	Name        string            `json:"name" yaml:"name"`                   // Technical metric name
	DisplayName string            `json:"display_name" yaml:"display_name"`   // Contextualized display name
	Value       interface{}       `json:"value" yaml:"value"`
	Unit        string            `json:"unit" yaml:"unit"`
	Timestamp   time.Time         `json:"timestamp" yaml:"timestamp"`
	ProbeName   string            `json:"probe_name" yaml:"probe_name"`
	Tags        map[string]string `json:"tags" yaml:"tags"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
}

// PRTGRequest represents the POST body for PRTG endpoints
type PRTGRequest struct {
	Probe  string                 `json:"probe"`
	Target string                 `json:"target"`
	Config map[string]interface{} `json:"config"`
}

// PRTGResponse represents the JSON response format for PRTG
type PRTGResponse struct {
	PRTG PRTGResult `json:"prtg"`
}

// PRTGResult contains the array of channels for PRTG
type PRTGResult struct {
	Result []PRTGChannel `json:"result"`
}

// PRTGChannel represents a single metric channel for PRTG
type PRTGChannel struct {
	Channel         string  `json:"channel"`
	Value           float64 `json:"value"`
	Float           int     `json:"float"`
	Unit            string  `json:"unit,omitempty"`
	LimitMode       int     `json:"limitmode,omitempty"`
	LimitMaxWarning float64 `json:"limitmaxwarning,omitempty"`
	LimitMaxError   float64 `json:"limitmaxerror,omitempty"`
}

// NewHTTPSyncStrategy creates a new HTTP sync strategy
func NewHTTPSyncStrategy(
	agentConfig configuration.AgentConfiguration,
	params map[string]interface{},
	baseLogger *logger.Logger,
) SyncStrategy {
	// Create module-specific logger for HTTP strategy
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.http")

	strategy := &HTTPSyncStrategy{
		agentConfig:         agentConfig,
		params:              params,
		logger:              moduleLogger,
		agentKey:            agentConfig.GetAuthenticationKey(),
		port:                8080,      // Default port
		bindAddress:         "0.0.0.0", // Default to all interfaces
		transformerRegistry: transformers.NewTransformerRegistry(moduleLogger.Logger),
		namingConfig:        make(map[string]string),
		cache: &MetricCache{
			dataByProbe: make(map[string][]CachedMetric),
			ttl:         5 * time.Minute, // 5 minutes TTL
			stopChan:    make(chan struct{}),
		},
	}

	// Override port if specified in params
	if portValue, exists := params["port"]; exists {
		if port, ok := portValue.(float64); ok {
			strategy.port = int(port)
		}
	}

	// Override bind address if specified in params
	if bindValue, exists := params["bind_address"]; exists {
		if bindAddr, ok := bindValue.(string); ok {
			strategy.bindAddress = bindAddr
		}
	}

	// Load naming configuration
	if namingParams, exists := params["naming"]; exists {
		if namingMap, ok := namingParams.(map[string]interface{}); ok {
			for probe, style := range namingMap {
				if styleStr, ok := style.(string); ok {
					strategy.namingConfig[probe] = styleStr
				}
			}
		}
	}

	// Set defaults for naming config
	if _, exists := strategy.namingConfig["redfish"]; !exists {
		strategy.namingConfig["redfish"] = "friendly"
	}
	if _, exists := strategy.namingConfig["host"]; !exists {
		strategy.namingConfig["host"] = "friendly"
	}
	if _, exists := strategy.namingConfig["otel"]; !exists {
		strategy.namingConfig["otel"] = "technical"
	}

	return strategy
}

// GetStrategyName returns the strategy identifier
func (h *HTTPSyncStrategy) GetStrategyName() string {
	return "http"
}

// GetStrategyParams returns current configuration parameters
func (h *HTTPSyncStrategy) GetStrategyParams() map[string]interface{} {
	return h.params
}

// ValidateConfigParams validates the provided configuration
func (h *HTTPSyncStrategy) ValidateConfigParams(params configuration.StorageConfigParams) error {
	// Validate port if provided
	if portValue, exists := params["port"]; exists {
		if _, ok := portValue.(float64); !ok {
			return fmt.Errorf("port must be a number")
		}
	}
	
	// Validate bind_address if provided
	if bindValue, exists := params["bind_address"]; exists {
		if _, ok := bindValue.(string); !ok {
			return fmt.Errorf("bind_address must be a string")
		}
	}
	
	return nil
}

// Start initializes the HTTP server and cache cleanup
func (h *HTTPSyncStrategy) Start() error {
	h.logger.Info().
		Int("port", h.port).
		Str("bind_address", h.bindAddress).
		Msg("Starting HTTP strategy")

	// Start cache cleanup goroutine
	go h.cache.cleanup()

	// Setup HTTP routes
	router := h.setupRoutes()

	// Create HTTP server with configurable bind address
	address := fmt.Sprintf("%s:%d", h.bindAddress, h.port)
	h.server = &http.Server{
		Addr:         address,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		h.logger.Info().
			Str("address", address).
			Int("port", h.port).
			Str("bind_address", h.bindAddress).
			Msg("HTTP server listening")
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.logger.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return nil
}

// AddDataPoints stores the received datapoints in cache
func (h *HTTPSyncStrategy) AddDataPoints(datapoints []datapoint.DataPoint) error {
	h.logger.Info().Int("count", len(datapoints)).Msg("🔥 HTTP Strategy - Received datapoints")
	h.logger.Debug().Int("datapoints_count", len(datapoints)).Msg("Processing datapoints for HTTP cache")
	
	h.cache.mu.Lock()
	defer h.cache.mu.Unlock()

	for _, dp := range datapoints {
		// Extract tags as map
		tags := make(map[string]string)
		for _, tag := range dp.Tags {
			tags[tag.Key] = tag.Value
		}

		// Get probe name from tags
		probeName := tags["probe_name"]
		
		// ⚠️ DEBUG: Log if probe_name is missing or empty
		if probeName == "" {
			h.logger.Warn().
				Str("metric_name", dp.Name).
				Interface("all_tags", tags).
				Msg("⚠️ MISSING PROBE_NAME: Metric has no probe_name tag!")
			probeName = "unknown" // Fallback for metrics without probe_name
		}

		// Create cached metric
		metric := CachedMetric{
			Value:      dp.Value,
			Timestamp:  time.Now(),
			Unit:       "", // DataPoint doesn't have Unit field yet
			ProbeName:  probeName,
			MetricName: dp.Name,
			Tags:       tags,
		}

		// Store in cache organized by probe
		h.cache.dataByProbe[probeName] = append(h.cache.dataByProbe[probeName], metric)
		
		h.logger.Debug().
			Str("metric_name", dp.Name).
			Str("probe_name", probeName).
			Any("value", dp.Value).
			Msg("📊 Stored metric in cache")
	}

	// Calculate total cache size across all probes
	totalCacheSize := 0
	for _, metrics := range h.cache.dataByProbe {
		totalCacheSize += len(metrics)
	}
	h.logger.Info().Int("count", len(datapoints)).Int("cache_size", totalCacheSize).Msg("✅ Datapoints added to HTTP cache")
	return nil
}

// Shutdown gracefully stops the HTTP server and cleanup routines
func (h *HTTPSyncStrategy) Shutdown(ctx context.Context) error {
	h.logger.Info().Msg("Shutting down HTTP strategy")

	// Stop cache cleanup
	close(h.cache.stopChan)

	// Shutdown HTTP server
	if h.server != nil {
		return h.server.Shutdown(ctx)
	}

	return nil
}

// setupRoutes configures HTTP routes
func (h *HTTPSyncStrategy) setupRoutes() *mux.Router {
	router := mux.NewRouter()

	// SenHub raw format endpoint with agentkey and probe name in path (GET)
	router.HandleFunc("/api/{agentkey}/senhub/metrics/{probe}", h.handleSenHubMetricsGET).Methods("GET")

	// PRTG endpoint with agentkey and probe name in path (GET)
	router.HandleFunc("/api/{agentkey}/prtg/metrics/{probe}", h.handlePRTGMetricsGET).Methods("GET")

	// List available probes endpoint (GET)
	router.HandleFunc("/api/{agentkey}/prtg/probes", h.handleListProbes).Methods("GET")

	// Legacy PRTG endpoint (POST) - kept for backward compatibility
	router.HandleFunc("/api/{agentkey}/prtg/metrics", h.handlePRTGMetrics).Methods("POST")

	// Health check endpoint
	router.HandleFunc("/health", h.handleHealth).Methods("GET")

	// Debug endpoint to view cache contents (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/debug/cache", h.handleDebugCache).Methods("GET")

	// Debug endpoint to view and set log levels (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/debug/logs", h.handleDebugLogs).Methods("GET")
	router.HandleFunc("/api/{agentkey}/debug/logs", h.handleSetLogLevels).Methods("POST")

	return router
}

// handleSenHubMetricsGET handles GET requests for SenHub raw format metrics
func (h *HTTPSyncStrategy) handleSenHubMetricsGET(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentKey := vars["agentkey"]
	probeName := vars["probe"]

	// Validate agent key
	if agentKey != h.agentConfig.GetAuthenticationKey() {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Debug().
		Str("probe", probeName).
		Msg("SenHub metrics GET request received")

	// Get metrics from cache for the specified probe and convert to SenHub format
	senHubMetrics := h.getSenHubMetricsForProbe(probeName)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(senHubMetrics); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode SenHub metrics response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("metrics_count", len(senHubMetrics)).
		Msg("✅ SenHub metrics response sent")
}

// handlePRTGMetricsGET handles GET requests for PRTG metrics
func (h *HTTPSyncStrategy) handlePRTGMetricsGET(w http.ResponseWriter, r *http.Request) {
	// Extract and validate agentkey from path
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	probeName := vars["probe"]

	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Debug().
		Str("probe", probeName).
		Msg("PRTG metrics GET request received")

	// Get metrics from cache for the specified probe
	channels := h.getMetricsForProbe(probeName)

	// Build PRTG response
	response := PRTGResponse{
		PRTG: PRTGResult{
			Result: channels,
		},
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("channels", len(channels)).
		Msg("PRTG GET response sent")
}

// ProbesListResponse represents the response for listing available probes
type ProbesListResponse struct {
	Probes []ProbeInfo `json:"probes"`
}

// ProbeInfo represents information about a probe
type ProbeInfo struct {
	Name         string `json:"name"`
	MetricsCount int    `json:"metrics_count"`
	LastUpdate   string `json:"last_update,omitempty"`
}

// handleListProbes handles GET requests to list available probes
func (h *HTTPSyncStrategy) handleListProbes(w http.ResponseWriter, r *http.Request) {
	// Extract and validate agentkey from path
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Debug().Msg("List probes request received")

	h.cache.mu.RLock()
	defer h.cache.mu.RUnlock()

	// Count metrics by probe name
	probeMetrics := make(map[string]int)
	probeLastUpdate := make(map[string]time.Time)

	for probeName, metrics := range h.cache.dataByProbe {
		if probeName == "" {
			probeName = "unknown"
		}
		probeMetrics[probeName] = len(metrics)
		
		// Track latest update time for each probe
		for _, metric := range metrics {
			if lastUpdate, exists := probeLastUpdate[probeName]; !exists || metric.Timestamp.After(lastUpdate) {
				probeLastUpdate[probeName] = metric.Timestamp
			}
		}
	}

	// Build response
	probes := make([]ProbeInfo, 0, len(probeMetrics))
	for probeName, count := range probeMetrics {
		lastUpdate := ""
		if timestamp, exists := probeLastUpdate[probeName]; exists {
			lastUpdate = timestamp.Format(time.RFC3339)
		}
		
		probes = append(probes, ProbeInfo{
			Name:         probeName,
			MetricsCount: count,
			LastUpdate:   lastUpdate,
		})
	}

	response := ProbesListResponse{
		Probes: probes,
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode probes list response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Int("probes_count", len(probes)).
		Msg("Probes list response sent")
}

// handlePRTGMetrics handles POST requests for PRTG metrics (legacy)
func (h *HTTPSyncStrategy) handlePRTGMetrics(w http.ResponseWriter, r *http.Request) {
	// Extract and validate agentkey from path
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req PRTGRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error().Err(err).Msg("Failed to parse request body")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	h.logger.Debug().
		Str("probe", req.Probe).
		Str("target", req.Target).
		Msg("PRTG metrics request received")

	// For now, emulate configuration handling - just log the config
	h.logger.Debug().Any("config", req.Config).Msg("Emulating config handling")

	// Get metrics from cache for the specified probe
	channels := h.getMetricsForProbe(req.Probe)

	// Build PRTG response
	response := PRTGResponse{
		PRTG: PRTGResult{
			Result: channels,
		},
	}

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.logger.Info().
		Str("probe", req.Probe).
		Int("channels", len(channels)).
		Msg("PRTG response sent")
}

// handleHealth provides a simple health check endpoint
func (h *HTTPSyncStrategy) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// getMetricsForProbe retrieves and transforms metrics for a specific probe
func (h *HTTPSyncStrategy) getMetricsForProbe(probeName string) []PRTGChannel {
	h.cache.mu.RLock()
	defer h.cache.mu.RUnlock()

	// Calculate total cache size for logging
	totalCacheSize := 0
	probeNames := make(map[string]int)
	for probe, metrics := range h.cache.dataByProbe {
		count := len(metrics)
		totalCacheSize += count
		probeNames[probe] = count
	}

	h.logger.Info().
		Str("requested_probe", probeName).
		Int("total_cache_size", totalCacheSize).
		Msg("🔍 Getting metrics for probe")

	h.logger.Info().
		Interface("available_probes", probeNames).
		Str("requested_probe", probeName).
		Msg("🗂️ Available probe names in cache")

	// Get metrics for the specific probe (O(1) access)
	rawMetrics, exists := h.cache.dataByProbe[probeName]
	if !exists {
		h.logger.Info().Str("probe", probeName).Msg("📭 No metrics found for probe")
		return []PRTGChannel{}
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("raw_metrics_count", len(rawMetrics)).
		Msg("📦 Found raw metrics for probe")

	// Deduplicate metrics and filter expired ones
	validMetrics := h.deduplicateAndFilterMetrics(rawMetrics)

	h.logger.Info().
		Str("probe", probeName).
		Int("valid_metrics_count", len(validMetrics)).
		Msg("🔄 After deduplication and TTL filtering")

	// Transform to PRTG channels
	var channels []PRTGChannel
	for _, metric := range validMetrics {
		h.logger.Debug().
			Str("metric_name", metric.MetricName).
			Str("probe", metric.ProbeName).
			Any("value", metric.Value).
			Msg("✅ Processing metric for PRTG")

		// Transform metric to PRTG channel (no key needed)
		channel := h.transformToPRTGChannel("", metric)
		if channel != nil {
			channels = append(channels, *channel)
			h.logger.Debug().
				Str("metric_name", metric.MetricName).
				Str("channel", channel.Channel).
				Float64("value", channel.Value).
				Msg("✅ Channel created successfully")
		} else {
			h.logger.Warn().
				Str("metric_name", metric.MetricName).
				Any("value", metric.Value).
				Msg("❌ Failed to create PRTG channel")
		}
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("valid_metrics", len(validMetrics)).
		Int("channels_created", len(channels)).
		Msg("📊 Metrics retrieval result")

	return channels
}

// getSenHubMetricsForProbe retrieves metrics for a probe in SenHub raw format
func (h *HTTPSyncStrategy) getSenHubMetricsForProbe(probeName string) []SenHubMetric {
	h.cache.mu.RLock()
	defer h.cache.mu.RUnlock()

	h.logger.Info().
		Str("requested_probe", probeName).
		Msg("🔍 Getting SenHub metrics for probe")

	// Get metrics for the specific probe (O(1) access)
	rawMetrics, exists := h.cache.dataByProbe[probeName]
	if !exists {
		h.logger.Info().Str("probe", probeName).Msg("📭 No metrics found for probe")
		return []SenHubMetric{}
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("raw_metrics_count", len(rawMetrics)).
		Msg("📦 Found raw metrics for probe")

	// Deduplicate metrics and filter expired ones
	validMetrics := h.deduplicateAndFilterMetrics(rawMetrics)

	h.logger.Info().
		Str("probe", probeName).
		Int("valid_metrics_count", len(validMetrics)).
		Msg("🔄 After deduplication and TTL filtering")

	// Convert to SenHub format
	var senHubMetrics []SenHubMetric
	for _, metric := range validMetrics {
		senHubMetric := h.convertToSenHubFormat(metric)
		senHubMetrics = append(senHubMetrics, senHubMetric)
		
		h.logger.Debug().
			Str("metric_name", metric.MetricName).
			Str("display_name", senHubMetric.DisplayName).
			Str("probe", metric.ProbeName).
			Any("value", metric.Value).
			Msg("✅ Converted to SenHub format")
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("valid_metrics", len(validMetrics)).
		Int("senhub_metrics_created", len(senHubMetrics)).
		Msg("📊 SenHub metrics retrieval result")

	return senHubMetrics
}

// deduplicateAndFilterMetrics removes duplicates and expired metrics
func (h *HTTPSyncStrategy) deduplicateAndFilterMetrics(metrics []CachedMetric) []CachedMetric {
	now := time.Now()
	latest := make(map[string]CachedMetric)

	for _, metric := range metrics {
		// Skip expired metrics
		if now.Sub(metric.Timestamp) > h.cache.ttl {
			h.logger.Debug().
				Str("metric_name", metric.MetricName).
				Time("timestamp", metric.Timestamp).
				Dur("age", now.Sub(metric.Timestamp)).
				Dur("ttl", h.cache.ttl).
				Msg("⏰ Skipping expired metric")
			continue
		}

		// Create unique key from metric name + tags
		keyParts := []string{metric.MetricName}
		
		// Sort tag keys for consistent key generation
		tagKeys := make([]string, 0, len(metric.Tags))
		for k := range metric.Tags {
			if k != "probe_name" { // Exclude probe_name since we're already grouped by probe
				tagKeys = append(tagKeys, k)
			}
		}
		
		// Simple sort
		for i := 0; i < len(tagKeys); i++ {
			for j := i + 1; j < len(tagKeys); j++ {
				if tagKeys[i] > tagKeys[j] {
					tagKeys[i], tagKeys[j] = tagKeys[j], tagKeys[i]
				}
			}
		}
		
		// Add sorted tags to key
		for _, k := range tagKeys {
			keyParts = append(keyParts, fmt.Sprintf("%s=%s", k, metric.Tags[k]))
		}
		
		uniqueKey := strings.Join(keyParts, ".")

		// Keep the latest metric for each unique key
		if existing, exists := latest[uniqueKey]; !exists || metric.Timestamp.After(existing.Timestamp) {
			latest[uniqueKey] = metric
			h.logger.Debug().
				Str("unique_key", uniqueKey).
				Str("metric_name", metric.MetricName).
				Time("timestamp", metric.Timestamp).
				Msg("✅ Keeping metric (latest)")
		} else {
			h.logger.Debug().
				Str("unique_key", uniqueKey).
				Str("metric_name", metric.MetricName).
				Time("timestamp", metric.Timestamp).
				Time("existing_timestamp", existing.Timestamp).
				Msg("🔄 Skipping metric (older)")
		}
	}

	// Convert map back to slice
	result := make([]CachedMetric, 0, len(latest))
	for _, metric := range latest {
		result = append(result, metric)
	}

	return result
}

// convertToSenHubFormat converts a CachedMetric to SenHub standardized format
func (h *HTTPSyncStrategy) convertToSenHubFormat(metric CachedMetric) SenHubMetric {
	// Generate contextualized display name using transformer
	var displayName string
	transformer, err := h.transformerRegistry.LoadTransformer(metric.ProbeName, "friendly")
	if err != nil {
		h.logger.Warn().Err(err).Str("probe", metric.ProbeName).Msg("Failed to load transformer, using fallback")
		// Use metric name as fallback display name
		displayName = metric.MetricName
	} else {
		displayName = transformer.TransformMetricName(metric.MetricName, metric.Tags)
	}
	
	return SenHubMetric{
		Name:        metric.MetricName,
		DisplayName: displayName,
		Value:       metric.Value,
		Unit:        metric.Unit,
		Timestamp:   metric.Timestamp,
		ProbeName:   metric.ProbeName,
		Tags:        metric.Tags,
		Description: "", // Could be enriched from transformer
	}
}

// transformToPRTGChannel converts a cached metric to PRTG channel format
func (h *HTTPSyncStrategy) transformToPRTGChannel(key string, metric CachedMetric) *PRTGChannel {
	// Convert value to float64
	var value float64
	switch v := metric.Value.(type) {
	case float64:
		value = v
	case float32:
		value = float64(v)
	case int:
		value = float64(v)
	case int64:
		value = float64(v)
	default:
		h.logger.Warn().Str("key", key).Any("value", metric.Value).Msg("Cannot convert value to float64")
		return nil
	}

	// Transform metric name to user-friendly channel name
	channelName := h.transformMetricName(key, metric)

	return &PRTGChannel{
		Channel: channelName,
		Value:   value,
		Float:   1,
		Unit:    metric.Unit,
	}
}

// transformMetricName converts technical metric names to user-friendly channel names
func (h *HTTPSyncStrategy) transformMetricName(key string, metric CachedMetric) string {
	// Use the stored metric name directly instead of parsing from key
	metricName := metric.MetricName
	probeName := metric.ProbeName
	
	// Fallback: if metric name is empty, extract from key
	if metricName == "" {
		parts := strings.Split(key, ".")
		if len(parts) > 0 {
			metricName = parts[0]
		}
	}

	// Get the naming style for this probe
	// Map individual host probes to the "host" category
	probeCategory := probeName
	if probeName == "cpu" || probeName == "memory" || probeName == "network" || probeName == "logicaldisk" || probeName == "wifi_signal_strength" {
		probeCategory = "host"
	}
	
	style, exists := h.namingConfig[probeCategory]
	if !exists {
		style = "friendly" // Default style
	}

	// Load transformer for this probe category and style
	transformer, err := h.transformerRegistry.LoadTransformer(probeCategory, style)
	if err != nil {
		h.logger.Warn().
			Err(err).
			Str("probe", probeName).
			Str("style", style).
			Msg("Failed to load transformer, using fallback")
		return metricName // Fallback to original metric name
	}

	// Transform the metric name using all available tags
	return transformer.TransformMetricName(metricName, metric.Tags)
}



// DebugCacheEntry represents a cache entry for debug display
type DebugCacheEntry struct {
	Name      string            `json:"name"`
	Value     interface{}       `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Unit      string            `json:"unit"`
	ProbeName string            `json:"probe_name"`
	Tags      map[string]string `json:"tags"`
	Age       string            `json:"age"`
}

// DebugCacheResponse represents the debug cache response
type DebugCacheResponse struct {
	TotalEntries int                `json:"total_entries"`
	CacheTTL     string             `json:"cache_ttl"`
	Entries      []DebugCacheEntry  `json:"entries"`
	Summary      map[string]int     `json:"summary"`
}

// handleDebugCache handles GET requests for cache debugging
func (h *HTTPSyncStrategy) handleDebugCache(w http.ResponseWriter, r *http.Request) {
	// Extract and validate agentkey from path
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for debug endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Debug().Msg("Debug cache request received")

	h.cache.mu.RLock()
	defer h.cache.mu.RUnlock()

	now := time.Now()
	var entries []DebugCacheEntry
	summary := make(map[string]int)

	// Convert cache data to debug format
	for probeName, metrics := range h.cache.dataByProbe {
		summary[probeName] = len(metrics)
		
		for _, metric := range metrics {
			age := now.Sub(metric.Timestamp)
			
			// Use metric name directly (no probe suffix needed)
			entry := DebugCacheEntry{
				Name:      metric.MetricName,
				Value:     metric.Value,
				Timestamp: metric.Timestamp,
				Unit:      metric.Unit,
				ProbeName: metric.ProbeName,
				Tags:      metric.Tags,
				Age:       age.String(),
			}
			entries = append(entries, entry)
		}
	}

	response := DebugCacheResponse{
		TotalEntries: len(entries),
		CacheTTL:     h.cache.ttl.String(),
		Entries:      entries,
		Summary:      summary,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode debug cache response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.logger.Debug().
		Int("total_entries", len(entries)).
		Any("summary", summary).
		Msg("Debug cache response sent")
}

// LogLevelInfo represents log level information for debug display
type LogLevelInfo struct {
	Module string `json:"module"`
	Level  string `json:"level"`
}

// LogLevelsResponse represents the debug log levels response
type LogLevelsResponse struct {
	ModuleLevels []LogLevelInfo `json:"module_levels"`
}

// SetLogLevelsRequest represents the request to set log levels
type SetLogLevelsRequest struct {
	ModuleLevels []logger.ModuleLogConfig `json:"module_levels"`
}

// handleDebugLogs handles GET requests for log level debugging
func (h *HTTPSyncStrategy) handleDebugLogs(w http.ResponseWriter, r *http.Request) {
	// Extract and validate agentkey from path
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for debug logs endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Debug().Msg("Debug logs request received")

	// Get current module log levels
	moduleLevels := logger.GetModuleLogLevels()
	
	var logLevels []LogLevelInfo
	for module, level := range moduleLevels {
		logLevels = append(logLevels, LogLevelInfo{
			Module: module,
			Level:  level.String(),
		})
	}

	response := LogLevelsResponse{
		ModuleLevels: logLevels,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode debug logs response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.logger.Debug().Int("modules_count", len(logLevels)).Msg("Debug logs response sent")
}

// handleSetLogLevels handles POST requests to set log levels
func (h *HTTPSyncStrategy) handleSetLogLevels(w http.ResponseWriter, r *http.Request) {
	// Extract and validate agentkey from path
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for set logs endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req SetLogLevelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Error().Err(err).Msg("Failed to parse log levels request body")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	h.logger.Info().
		Int("modules_count", len(req.ModuleLevels)).
		Msg("Setting log levels")

	// Set the log levels
	if err := logger.SetModuleLogLevels(req.ModuleLevels); err != nil {
		h.logger.Error().Err(err).Msg("Failed to set module log levels")
		http.Error(w, "Invalid log configuration", http.StatusBadRequest)
		return
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	response := map[string]string{"status": "success", "message": "Log levels updated"}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode set logs response")
		return
	}

	h.logger.Info().Msg("Log levels updated successfully")
}

// cleanup removes expired metrics from cache
func (cache *MetricCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cache.mu.Lock()
			now := time.Now()
			
			// Clean up expired metrics from each probe
			for probeName, metrics := range cache.dataByProbe {
				var validMetrics []CachedMetric
				for _, metric := range metrics {
					if now.Sub(metric.Timestamp) <= cache.ttl {
						validMetrics = append(validMetrics, metric)
					}
				}
				
				// Update the probe's metrics list or remove empty probes
				if len(validMetrics) > 0 {
					cache.dataByProbe[probeName] = validMetrics
				} else {
					delete(cache.dataByProbe, probeName)
				}
			}
			cache.mu.Unlock()
		case <-cache.stopChan:
			return
		}
	}
}
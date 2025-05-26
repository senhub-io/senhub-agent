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
	logger              *logger.Logger
	server              *http.Server
	cache               *MetricCache
	agentKey            string
	port                int
	transformerRegistry *transformers.TransformerRegistry
	namingConfig        map[string]string // probe -> style mapping
}

// MetricCache stores the latest metrics in memory with TTL
type MetricCache struct {
	mu       sync.RWMutex
	data     map[string]CachedMetric
	ttl      time.Duration
	stopChan chan struct{}
}

// CachedMetric represents a stored metric with metadata
type CachedMetric struct {
	Value     interface{}
	Timestamp time.Time
	Unit      string
	ProbeName string
	Tags      map[string]string
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
	Unit            string  `json:"unit,omitempty"`
	LimitMode       int     `json:"limitmode,omitempty"`
	LimitMaxWarning float64 `json:"limitmaxwarning,omitempty"`
	LimitMaxError   float64 `json:"limitmaxerror,omitempty"`
}

// NewHTTPSyncStrategy creates a new HTTP sync strategy
func NewHTTPSyncStrategy(
	agentConfig configuration.AgentConfiguration,
	params map[string]interface{},
	logger *logger.Logger,
) SyncStrategy {
	localLogger := logger.With().Str("strategy", "http").Logger()

	strategy := &HTTPSyncStrategy{
		agentConfig:         agentConfig,
		params:              params,
		logger:              &localLogger,
		agentKey:            agentConfig.GetAuthenticationKey(),
		port:                8080, // Default port, should be configurable
		transformerRegistry: transformers.NewTransformerRegistry(&localLogger),
		namingConfig:        make(map[string]string),
		cache: &MetricCache{
			data:     make(map[string]CachedMetric),
			ttl:      5 * time.Minute, // 5 minutes TTL
			stopChan: make(chan struct{}),
		},
	}

	// Override port if specified in params
	if portValue, exists := params["port"]; exists {
		if port, ok := portValue.(float64); ok {
			strategy.port = int(port)
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
	return nil
}

// Start initializes the HTTP server and cache cleanup
func (h *HTTPSyncStrategy) Start() error {
	h.logger.Info().Int("port", h.port).Msg("Starting HTTP strategy")

	// Start cache cleanup goroutine
	go h.cache.cleanup()

	// Setup HTTP routes
	router := h.setupRoutes()

	// Create HTTP server
	h.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", h.port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		h.logger.Info().Int("port", h.port).Msg("HTTP server listening")
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.logger.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return nil
}

// AddDataPoints stores the received datapoints in cache
func (h *HTTPSyncStrategy) AddDataPoints(datapoints []datapoint.DataPoint) error {
	h.logger.Info().Int("count", len(datapoints)).Msg("🔥 HTTP Strategy - Received datapoints")
	
	h.cache.mu.Lock()
	defer h.cache.mu.Unlock()

	for _, dp := range datapoints {
		// Create a unique key for the metric
		key := h.generateMetricKey(dp)

		// Extract tags as map
		tags := make(map[string]string)
		for _, tag := range dp.Tags {
			tags[tag.Key] = tag.Value
		}

		// Store in cache
		h.cache.data[key] = CachedMetric{
			Value:     dp.Value,
			Timestamp: time.Now(),
			Unit:      "", // DataPoint doesn't have Unit field yet
			ProbeName: tags["probe_name"], // Assuming probe_name is in tags
			Tags:      tags,
		}
		
		h.logger.Debug().
			Str("key", key).
			Str("probe_name", tags["probe_name"]).
			Any("value", dp.Value).
			Msg("📊 Stored metric in cache")
	}

	h.logger.Info().Int("count", len(datapoints)).Int("cache_size", len(h.cache.data)).Msg("✅ Datapoints added to HTTP cache")
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

	// PRTG endpoint with agentkey in path
	router.HandleFunc("/api/{agentkey}/prtg/metrics", h.handlePRTGMetrics).Methods("POST")

	// Health check endpoint
	router.HandleFunc("/health", h.handleHealth).Methods("GET")

	return router
}

// handlePRTGMetrics handles POST requests for PRTG metrics
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

	h.logger.Info().
		Str("requested_probe", probeName).
		Int("cache_size", len(h.cache.data)).
		Msg("🔍 Getting metrics for probe")

	var channels []PRTGChannel
	matchingMetrics := 0

	for key, metric := range h.cache.data {
		h.logger.Debug().
			Str("key", key).
			Str("metric_probe", metric.ProbeName).
			Str("requested_probe", probeName).
			Msg("📋 Cache entry")

		// Filter by probe name
		if metric.ProbeName != probeName {
			continue
		}
		matchingMetrics++

		// Skip expired metrics
		if time.Since(metric.Timestamp) > h.cache.ttl {
			h.logger.Debug().Str("key", key).Msg("⏰ Metric expired, skipping")
			continue
		}

		// Transform metric to PRTG channel
		channel := h.transformToPRTGChannel(key, metric)
		if channel != nil {
			channels = append(channels, *channel)
		}
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("matching_metrics", matchingMetrics).
		Int("channels_created", len(channels)).
		Msg("📊 Metrics retrieval result")

	return channels
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
		Unit:    metric.Unit,
	}
}

// transformMetricName converts technical metric names to user-friendly channel names
func (h *HTTPSyncStrategy) transformMetricName(key string, metric CachedMetric) string {
	// Get the probe name from metric tags or fallback to parsing key
	probeName := metric.ProbeName
	if probeName == "" {
		// Try to extract probe name from key (assuming format like "probe.metric.name")
		parts := strings.Split(key, ".")
		if len(parts) > 0 {
			probeName = parts[0]
		}
	}

	// Get the naming style for this probe
	style, exists := h.namingConfig[probeName]
	if !exists {
		style = "friendly" // Default style
	}

	// Load transformer for this probe and style
	transformer, err := h.transformerRegistry.LoadTransformer(probeName, style)
	if err != nil {
		h.logger.Warn().
			Err(err).
			Str("probe", probeName).
			Str("style", style).
			Msg("Failed to load transformer, using fallback")
		return key // Fallback to original key
	}

	// Transform the metric name
	return transformer.TransformMetricName(key, metric.Tags)
}

// generateMetricKey creates a unique key for a datapoint
func (h *HTTPSyncStrategy) generateMetricKey(dp datapoint.DataPoint) string {
	// For now, use Name field as key
	// TODO: Include relevant tags to create unique keys
	return dp.Name
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
			for key, metric := range cache.data {
				if now.Sub(metric.Timestamp) > cache.ttl {
					delete(cache.data, key)
				}
			}
			cache.mu.Unlock()
		case <-cache.stopChan:
			return
		}
	}
}
// senhub-agent/internal/agent/services/data_store/strategy_http.go
package data_store

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/yaml.v2"
	"senhub-agent.go/internal/agent/cliArgs"
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
	enabledEndpoints    map[string]bool // monitoring tools to expose
	nagiosConfig        *NagiosConfig   // cached Nagios configuration
	nagiosConfigMu      sync.RWMutex    // mutex for nagios config access
	startTime           time.Time       // agent start time for uptime calculation
	assetHandler        *AssetHandler   // asset handler for templates and static files
	lastCPUTime         time.Duration   // last CPU time measurement
	lastCPUMeasurement  time.Time       // last CPU measurement timestamp
	// TLS configuration
	tlsEnabled          bool
	tlsMinVersion       string
}

// MetricCache stores the latest metrics in memory with TTL, organized like a TSDB
type MetricCache struct {
	mu           sync.RWMutex
	// TSDB-like structure: unique key -> latest metric
	// Key format: probe_name:metric_name:sorted_tags_hash
	timeSeries   map[string]CachedMetric
	// Index by probe for fast probe-specific queries  
	probeIndex   map[string]map[string]bool // probe_name -> set of ts_keys
	ttl          time.Duration
	stopChan     chan struct{}
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

// generateTimeSeriesKey creates a unique key for a time series based on probe, metric name, and tags
func (h *HTTPSyncStrategy) generateTimeSeriesKey(probeName, metricName string, tags map[string]string) string {
	// Create a sorted list of tag key-value pairs for consistent key generation
	var tagParts []string
	
	// Sort tag keys for consistent ordering
	tagKeys := make([]string, 0, len(tags))
	for k := range tags {
		if k != "probe_name" { // Exclude probe_name since it's already in the key prefix
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
	
	// Build tag string
	for _, k := range tagKeys {
		tagParts = append(tagParts, fmt.Sprintf("%s=%s", k, tags[k]))
	}
	
	// Create unique key: probe:metric:tags
	if len(tagParts) > 0 {
		return fmt.Sprintf("%s:%s:%s", probeName, metricName, strings.Join(tagParts, ","))
	}
	return fmt.Sprintf("%s:%s", probeName, metricName)
}

// SenHubMetric represents a metric in standardized SenHub raw format
type SenHubMetric struct {
	Name        string            `json:"name,omitempty" yaml:"name"`                   // Technical metric name
	Channel     string            `json:"channel" yaml:"channel"`                      // Transformed display name (main field)
	Value       interface{}       `json:"value" yaml:"value"`
	Unit        string            `json:"unit,omitempty" yaml:"unit"`
	Timestamp   time.Time         `json:"timestamp,omitempty" yaml:"timestamp"`
	ProbeName   string            `json:"probe_name,omitempty" yaml:"probe_name"`
	Tags        map[string]string `json:"tags,omitempty" yaml:"tags"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
}

// SenHubResponse wraps SenHub metrics with status information
type SenHubResponse struct {
	Metrics []SenHubMetric `json:"metrics"`
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Date    int64          `json:"date"` // Unix timestamp in milliseconds
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
	CustomUnit      string  `json:"customunit,omitempty"`
	LimitMode       int     `json:"limitmode,omitempty"`
	LimitMaxWarning float64 `json:"limitmaxwarning,omitempty"`
	LimitMaxError   float64 `json:"limitmaxerror,omitempty"`
	ValueLookup     string  `json:"valuelookup,omitempty"`
}

// MetricFilter represents query parameters for filtering metrics
type MetricFilter struct {
	TagFilters    map[string][]string // key: tag name, value: allowed values
	ExcludeTags   map[string][]string // key: tag name, value: excluded values  
	MetricNames   []string            // specific metric names to include
	Limit         int                 // max number of results
	Offset        int                 // pagination offset
}

// Nagios structs for check configuration and responses
type NagiosConfig struct {
	Version     string        `yaml:"version"`
	Description string        `yaml:"description"`
	Checks      []NagiosCheck `yaml:"checks"`
}

type NagiosCheck struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	ProbeFilter string           `yaml:"probe_filter,omitempty"`
	TagFilters  []NagiosTagFilter `yaml:"tag_filters,omitempty"`
	Metrics     []NagiosMetric   `yaml:"metrics"`
}

type NagiosTagFilter struct {
	Key      string   `yaml:"key"`
	Values   []string `yaml:"values,omitempty"`
	Operator string   `yaml:"operator"` // "in", "not_in", "equals", "not_equals", "exists"
}

type NagiosMetric struct {
	Channel               string                    `yaml:"channel"`
	Aggregation          string                    `yaml:"aggregation,omitempty"` // "average", "max", "min", "sum", "none"
	Warning              string                    `yaml:"warning"`
	Critical             string                    `yaml:"critical"`
	Unit                 string                    `yaml:"unit,omitempty"`
	Invert               bool                      `yaml:"invert,omitempty"`
	TagContext           string                    `yaml:"tag_context,omitempty"`
	TagSpecificThresholds []NagiosTagThreshold     `yaml:"tag_specific_thresholds,omitempty"`
	Description          string                    `yaml:"description,omitempty"`
}

type NagiosTagThreshold struct {
	Tags     map[string]string `yaml:"tags"`
	Warning  string           `yaml:"warning"`
	Critical string           `yaml:"critical"`
}

// Nagios response structures
type NagiosResponse struct {
	Status     int    `json:"status"`      // 0=OK, 1=WARNING, 2=CRITICAL, 3=UNKNOWN
	StatusText string `json:"status_text"` // "OK", "WARNING", "CRITICAL", "UNKNOWN"
	Message    string `json:"message"`     // Human readable message
	PerfData   string `json:"perfdata"`    // Performance data string
}

type NagiosRequest struct {
	CheckName string                 `json:"check_name,omitempty"`
	Probe     string                 `json:"probe,omitempty"`
	Config    map[string]interface{} `json:"config,omitempty"`
	Overrides NagiosOverrides        `json:"overrides,omitempty"`
}

type NagiosOverrides struct {
	Warning    string            `json:"warning,omitempty"`
	Critical   string            `json:"critical,omitempty"`
	TagFilters map[string]string `json:"tag_filters,omitempty"`
}

// Nagios discovery response structures
type NagiosCheckInfo struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	ProbeFilter string              `json:"probe_filter,omitempty"`
	MetricCount int                 `json:"metric_count"`
	TagFilters  []NagiosTagFilter   `json:"tag_filters,omitempty"`
	Metrics     []NagiosMetricInfo  `json:"metrics"`
}

type NagiosMetricInfo struct {
	Channel     string `json:"channel"`
	Aggregation string `json:"aggregation,omitempty"`
	Warning     string `json:"warning"`
	Critical    string `json:"critical"`
	Unit        string `json:"unit,omitempty"`
	Invert      bool   `json:"invert,omitempty"`
	TagContext  string `json:"tag_context,omitempty"`
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
		enabledEndpoints:    make(map[string]bool),
		startTime:           time.Now(), // Initialize start time for uptime calculation
		assetHandler:        NewAssetHandler(agentConfig.GetAuthenticationKey()),
		cache: &MetricCache{
			timeSeries:  make(map[string]CachedMetric),
			probeIndex:  make(map[string]map[string]bool),
			ttl:         5 * time.Minute, // 5 minutes TTL
			stopChan:    make(chan struct{}),
		},
	}

	// Override port if specified in params
	if portValue, exists := params["port"]; exists {
		switch v := portValue.(type) {
		case float64:
			strategy.port = int(v)
		case int:
			strategy.port = v
		case int64:
			strategy.port = int(v)
		}
	}

	// Override bind address if specified in params
	if bindValue, exists := params["bind_address"]; exists {
		if bindAddr, ok := bindValue.(string); ok {
			strategy.bindAddress = bindAddr
		}
	}

	// Load endpoints configuration
	if endpointsParam, exists := params["endpoints"]; exists {
		if endpointsList, ok := endpointsParam.([]interface{}); ok {
			for _, endpoint := range endpointsList {
				if endpointStr, ok := endpoint.(string); ok {
					strategy.enabledEndpoints[endpointStr] = true
				}
			}
		}
	}

	// If no endpoints specified, default to senhub only (raw format)
	if len(strategy.enabledEndpoints) == 0 {
		strategy.enabledEndpoints["senhub"] = true
	}

	// Parse TLS configuration
	if tlsParam, exists := params["tls"]; exists {
		if tlsConfig, ok := tlsParam.(map[string]interface{}); ok {
			// TLS enabled
			if enabled, exists := tlsConfig["enabled"]; exists {
				if enabledBool, ok := enabled.(bool); ok {
					strategy.tlsEnabled = enabledBool
				}
			}
			
			// Min TLS version (with default)
			if minVersion, exists := tlsConfig["min_tls_version"]; exists {
				if minVersionStr, ok := minVersion.(string); ok {
					strategy.tlsMinVersion = minVersionStr
				}
			} else if strategy.tlsEnabled {
				strategy.tlsMinVersion = "1.2"
			}
		}
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
		var port int
		switch v := portValue.(type) {
		case int:
			port = v
		case int64:
			port = int(v)
		case float64:
			// Accept float64 only if it's a whole number (for JSON compatibility)
			if v != float64(int(v)) {
				return fmt.Errorf("port must be an integer")
			}
			port = int(v)
		default:
			return fmt.Errorf("port must be an integer")
		}
		
		// Validate port range
		if port < 1 || port > 65535 {
			return fmt.Errorf("port must be between 1 and 65535")
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
		if h.tlsEnabled {
			// Fixed certificate paths generated during installation
			certFile := "./certs/agent-cert.pem"
			keyFile := "./certs/agent-key.pem"
			
			h.logger.Info().
				Str("address", address).
				Int("port", h.port).
				Str("bind_address", h.bindAddress).
				Bool("tls_enabled", true).
				Str("cert_file", certFile).
				Str("key_file", keyFile).
				Str("min_tls_version", h.tlsMinVersion).
				Msg("HTTPS server listening")
			
			if err := h.server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				h.logger.Error().Err(err).Msg("HTTPS server error")
			}
		} else {
			h.logger.Info().
				Str("address", address).
				Int("port", h.port).
				Str("bind_address", h.bindAddress).
				Bool("tls_enabled", false).
				Msg("HTTP server listening")
			if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				h.logger.Error().Err(err).Msg("HTTP server error")
			}
		}
	}()

	return nil
}

// AddDataPoints stores the received datapoints in cache
func (h *HTTPSyncStrategy) AddDataPoints(datapoints []datapoint.DataPoint) error {
	h.logger.Info().Int("count", len(datapoints)).Msg("🔥 HTTP Strategy - Received datapoints")
	h.logger.Debug().Int("datapoints_count", len(datapoints)).Msg("Processing datapoints for TSDB cache")
	
	h.cache.mu.Lock()
	defer h.cache.mu.Unlock()

	now := time.Now()
	
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

		// Generate unique time series key
		tsKey := h.generateTimeSeriesKey(probeName, dp.Name, tags)

		// Get transformer to resolve unit
		transformer, err := h.transformerRegistry.LoadTransformer(probeName, "friendly")
		if err != nil {
			h.logger.Warn().
				Err(err).
				Str("probe_name", probeName).
				Msg("Failed to get transformer for unit resolution")
		}
		
		// Resolve unit using transformer
		unit := ""
		if transformer != nil {
			unit = transformer.GetUnit(dp.Name)
		}

		// Create cached metric
		metric := CachedMetric{
			Value:      dp.Value,
			Timestamp:  now, // Use consistent timestamp for write batch
			Unit:       unit,
			ProbeName:  probeName,
			MetricName: dp.Name,
			Tags:       tags,
		}

		// TSDB approach: Store/replace metric by unique key (deduplication at write-time)
		existingMetric, exists := h.cache.timeSeries[tsKey]
		if exists {
			h.logger.Debug().
				Str("ts_key", tsKey).
				Time("old_timestamp", existingMetric.Timestamp).
				Time("new_timestamp", metric.Timestamp).
				Msg("🔄 Replacing existing metric in time series")
		} else {
			h.logger.Debug().
				Str("ts_key", tsKey).
				Str("metric_name", dp.Name).
				Str("probe_name", probeName).
				Msg("📊 Adding new metric to time series")
		}
		
		// Store metric in time series
		h.cache.timeSeries[tsKey] = metric
		
		// Update probe index for fast probe-specific queries
		if h.cache.probeIndex[probeName] == nil {
			h.cache.probeIndex[probeName] = make(map[string]bool)
		}
		h.cache.probeIndex[probeName][tsKey] = true
	}

	// Clean up expired metrics
	h.cleanupExpiredMetrics(now)

	h.logger.Info().
		Int("count", len(datapoints)).
		Int("total_time_series", len(h.cache.timeSeries)).
		Int("active_probes", len(h.cache.probeIndex)).
		Msg("✅ Datapoints added to TSDB cache")

	return nil
}

// cleanupExpiredMetrics removes expired metrics from the time series cache
func (h *HTTPSyncStrategy) cleanupExpiredMetrics(now time.Time) {
	expiredKeys := make([]string, 0)
	
	// Find expired metrics
	for tsKey, metric := range h.cache.timeSeries {
		if now.Sub(metric.Timestamp) > h.cache.ttl {
			expiredKeys = append(expiredKeys, tsKey)
		}
	}
	
	// Remove expired metrics
	for _, tsKey := range expiredKeys {
		metric := h.cache.timeSeries[tsKey]
		delete(h.cache.timeSeries, tsKey)
		
		// Also remove from probe index
		if probeKeys, exists := h.cache.probeIndex[metric.ProbeName]; exists {
			delete(probeKeys, tsKey)
			// Clean up empty probe index
			if len(probeKeys) == 0 {
				delete(h.cache.probeIndex, metric.ProbeName)
			}
		}
	}
	
	if len(expiredKeys) > 0 {
		h.logger.Debug().
			Int("expired_count", len(expiredKeys)).
			Dur("ttl", h.cache.ttl).
			Msg("🧹 Cleaned up expired metrics from TSDB cache")
	}
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

	// Always expose health check endpoint
	router.HandleFunc("/health", h.handleHealth).Methods("GET")

	// API documentation endpoint
	router.HandleFunc("/api/{agentkey}/endpoints", h.handleListEndpoints).Methods("GET")

	// Discovery endpoints (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/info/endpoints", h.handleInfoEndpoints).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/system", h.handleInfoSystem).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/probes", h.handleInfoProbes).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/tags/{probe}", h.handleInfoTags).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/schema/{probe}", h.handleInfoSchema).Methods("GET")
	
	// Admin endpoints (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/admin/cache", h.handleDebugCache).Methods("GET")
	router.HandleFunc("/api/{agentkey}/admin/logs", h.handleDebugLogs).Methods("GET")
	router.HandleFunc("/api/{agentkey}/admin/logs", h.handleSetLogLevels).Methods("POST")
	
	// Legacy debug endpoints for backward compatibility
	router.HandleFunc("/api/{agentkey}/debug/logs", h.handleDebugLogs).Methods("GET")
	router.HandleFunc("/api/{agentkey}/debug/logs", h.handleSetLogLevels).Methods("POST")

	// Conditionally expose monitoring tool endpoints based on configuration
	if h.enabledEndpoints["senhub"] {
		router.HandleFunc("/api/{agentkey}/senhub/metrics/{probe}", h.handleSenHubMetricsGET).Methods("GET")
	}

	if h.enabledEndpoints["prtg"] {
		router.HandleFunc("/api/{agentkey}/prtg/metrics/{probe}", h.handlePRTGMetricsGET).Methods("GET")
		router.HandleFunc("/api/{agentkey}/prtg/probes", h.handleListProbes).Methods("GET")
	}

	if h.enabledEndpoints["nagios"] {
		router.HandleFunc("/api/{agentkey}/nagios/metrics/{probe}", h.handleNagiosMetricsGET).Methods("GET")
		router.HandleFunc("/api/{agentkey}/nagios/check/{check_name}", h.handleNagiosCheck).Methods("GET")
		router.HandleFunc("/api/{agentkey}/nagios/metrics", h.handleNagiosMetrics).Methods("GET", "POST")
		router.HandleFunc("/api/{agentkey}/nagios/checks", h.handleNagiosChecks).Methods("GET")
	}

	if h.enabledEndpoints["zabbix"] {
		router.HandleFunc("/api/{agentkey}/zabbix/metrics/{probe}", h.handleZabbixMetricsGET).Methods("GET")
	}

	if h.enabledEndpoints["prometheus"] {
		router.HandleFunc("/api/{agentkey}/prometheus/metrics", h.handlePrometheusMetricsGET).Methods("GET")
	}

	// Web UI routes
	router.HandleFunc("/web/{agentkey}/", h.handleWebDashboard).Methods("GET")
	router.HandleFunc("/web/{agentkey}/dashboard", h.handleWebDashboard).Methods("GET") // Compatibility alias
	router.HandleFunc("/web/{agentkey}/explorer", h.handleWebExplorer).Methods("GET")
	router.HandleFunc("/web/{agentkey}/docs", h.handleWebDocs).Methods("GET")
	router.HandleFunc("/web/{agentkey}/admin", h.handleWebAdmin).Methods("GET")
	
	// Static assets
	router.PathPrefix("/web/{agentkey}/assets/").HandlerFunc(h.handleWebAssets).Methods("GET")

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

	// Parse query parameters
	filter := h.parseMetricFilter(r)
	
	h.logger.Debug().
		Str("probe", probeName).
		Interface("filter", filter).
		Msg("PRTG metrics GET request received")

	// Get metrics from cache for the specified probe with filters
	channels := h.getMetricsForProbeWithFilter(probeName, filter)

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

// EndpointsListResponse represents the response for listing all available endpoints
type EndpointsListResponse struct {
	Endpoints []EndpointInfo `json:"endpoints"`
}

// EndpointInfo represents information about an endpoint
type EndpointInfo struct {
	Path        string   `json:"path"`
	Methods     []string `json:"methods"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
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

	// Count metrics by probe name using TSDB structure
	probeMetrics := make(map[string]int)
	probeLastUpdate := make(map[string]time.Time)

	for probeName, tsKeys := range h.cache.probeIndex {
		if probeName == "" {
			probeName = "unknown"
		}
		probeMetrics[probeName] = len(tsKeys)
		
		// Track latest update time for each probe
		for tsKey := range tsKeys {
			if metric, exists := h.cache.timeSeries[tsKey]; exists {
				if lastUpdate, hasUpdate := probeLastUpdate[probeName]; !hasUpdate || metric.Timestamp.After(lastUpdate) {
					probeLastUpdate[probeName] = metric.Timestamp
				}
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

// HealthResponse represents the health check response
type HealthResponse struct {
	Status       string `json:"status"`
	Version      string `json:"version"`
	Commit       string `json:"commit,omitempty"`
	Uptime       string `json:"uptime"`
	ProbesActive int    `json:"probes_active"`
	MetricsCached int   `json:"metrics_cached"`
}

// HealthCheckResponse represents detailed health information for system info
type HealthCheckResponse struct {
	Status    string            `json:"status"`
	Timestamp int64             `json:"timestamp"`
	Version   string            `json:"version"`
	Services  map[string]string `json:"services"`
}

// handleHealth provides a comprehensive health check endpoint
func (h *HTTPSyncStrategy) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.cache.mu.RLock()
	totalMetrics := len(h.cache.timeSeries)
	probeCount := len(h.cache.probeIndex)
	h.cache.mu.RUnlock()
	
	// Calculate uptime (approximation since we don't track start time)
	uptime := time.Since(time.Now().Add(-1 * time.Hour)).Truncate(time.Second).String()
	
	response := HealthResponse{
		Status:        "ok",
		Version:       "0.1.22-beta", // TODO: Get from build info
		Uptime:        uptime,
		ProbesActive:  probeCount,
		MetricsCached: totalMetrics,
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// EndpointsInfoResponse represents the response for /info/endpoints
type EndpointsInfoResponse struct {
	Endpoints []EndpointInfoStatus `json:"endpoints"`
}

type EndpointInfoStatus struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// SystemInfoResponse represents the response for /info/system
type SystemInfoResponse struct {
	Status      string             `json:"status"`
	Version     string             `json:"version"`
	Port        int                `json:"port"`
	Uptime      string             `json:"uptime"`
	Health      HealthCheckResponse `json:"health"`
	Cache       CacheInfoResponse   `json:"cache"`
	Resources   ResourcesInfo       `json:"resources"`
}

type CacheInfoResponse struct {
	TotalMetrics int    `json:"total_metrics"`
	TTL          string `json:"ttl"`
	MemoryUsage  string `json:"memory_usage"`
}

type ResourcesInfo struct {
	MemoryUsageMB float64 `json:"memory_usage_mb"`
	CPUPercent    float64 `json:"cpu_percent"`
	Goroutines    int     `json:"goroutines"`
}

// ProbesInfoResponse represents the response for /info/probes
type ProbesInfoResponse struct {
	Probes       []string          `json:"probes"`
	ProbeMetrics map[string]int    `json:"probe_metrics"`
	TotalMetrics int               `json:"total_metrics"`
}

// TagInfoResponse represents the response for /info/tags/{probe}
type TagInfoResponse struct {
	Probe        string                    `json:"probe"`
	Tags         map[string]TagInfo        `json:"tags"`
	Metrics      []string                  `json:"metrics"`
	TotalMetrics int                       `json:"total_metrics"`
}

// TagInfo contains information about a specific tag
type TagInfo struct {
	Values       []string `json:"values"`
	Description  string   `json:"description"`
	SampleCount  int      `json:"sample_count"`
}

// SchemaInfoResponse represents the response for /info/schema/{probe}
type SchemaInfoResponse struct {
	Probe        string                    `json:"probe"`
	Tags         map[string]TagInfo        `json:"tags"`
	Metrics      []string                  `json:"metrics"`
	TotalMetrics int                       `json:"total_metrics"`
	Examples     []MetricExample           `json:"examples"`
}

// MetricExample shows example usage of filters
type MetricExample struct {
	Description string `json:"description"`
	URL         string `json:"url"`
	ResultCount int    `json:"estimated_results"`
}

// handleInfoProbes lists all available probes
func (h *HTTPSyncStrategy) handleInfoProbes(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	
	if providedKey != h.agentKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	h.cache.mu.RLock()
	defer h.cache.mu.RUnlock()
	
	probeMetrics := make(map[string]int)
	var probes []string
	totalMetrics := 0
	
	for probe, tsKeys := range h.cache.probeIndex {
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

// handleInfoEndpoints provides discovery of available endpoints
func (h *HTTPSyncStrategy) handleInfoEndpoints(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	
	if providedKey != h.agentKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
			Enabled:     h.enabledEndpoints[name],
		})
	}
	
	response := EndpointsInfoResponse{
		Endpoints: endpoints,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleInfoSystem provides system status and resource information
func (h *HTTPSyncStrategy) handleInfoSystem(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	
	if providedKey != h.agentKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	// Calculate uptime
	uptime := time.Since(h.startTime)
	uptimeStr := formatDuration(uptime)
	
	// Get memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memUsageMB := float64(memStats.Alloc) / 1024 / 1024
	
	// Get cache stats
	h.cache.mu.RLock()
	totalMetrics := 0
	for _, tsKeys := range h.cache.probeIndex {
		totalMetrics += len(tsKeys)
	}
	h.cache.mu.RUnlock()
	
	// Create health response (reuse existing health logic)
	healthResponse := HealthCheckResponse{
		Status:    "healthy",
		Timestamp: time.Now().Unix(),
		Version:   "0.1.24-beta", // TODO: get from build info
		Services: map[string]string{
			"http_server": "running",
			"cache":       "running",
			"metrics":     fmt.Sprintf("%d metrics cached", totalMetrics),
		},
	}
	
	// Build system info response
	// Use CommitHash if available (contains full version info), otherwise fallback to Version
	version := cliArgs.Version
	if cliArgs.CommitHash != "" {
		// CommitHash contains full version info from git describe
		version = cliArgs.CommitHash
	}
	
	response := SystemInfoResponse{
		Status:  "running",
		Version: version,
		Port:    h.port,
		Uptime:  uptimeStr,
		Health:  healthResponse,
		Cache: CacheInfoResponse{
			TotalMetrics: totalMetrics,
			TTL:          h.cache.ttl.String(),
			MemoryUsage:  fmt.Sprintf("%.2f MB", memUsageMB),
		},
		Resources: ResourcesInfo{
			MemoryUsageMB: memUsageMB,
			CPUPercent:    h.getCPUUsage(),
			Goroutines:    runtime.NumGoroutine(),
		},
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// formatDuration formats a duration in a human-readable format
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		return fmt.Sprintf("%dm", minutes)
	}
}

// getCPUUsage calculates the CPU usage percentage for the current process
func (h *HTTPSyncStrategy) getCPUUsage() float64 {
	// Get current process CPU usage
	pid := os.Getpid()
	
	// Read CPU times from /proc/pid/stat on Linux/Unix
	var currentCPUTime time.Duration
	var err error
	
	switch runtime.GOOS {
	case "linux":
		currentCPUTime, err = h.getCPUTimeLinux(pid)
	case "darwin":
		currentCPUTime, err = h.getCPUTimeDarwin(pid)
	default:
		// Fallback: return 0 for unsupported platforms
		return 0.0
	}
	
	if err != nil {
		h.logger.Debug().Err(err).Msg("Failed to get CPU time")
		return 0.0
	}
	
	now := time.Now()
	
	// If this is the first measurement, store the values and return 0
	if h.lastCPUMeasurement.IsZero() {
		h.lastCPUTime = currentCPUTime
		h.lastCPUMeasurement = now
		return 0.0
	}
	
	// Calculate CPU usage percentage
	cpuTimeDelta := currentCPUTime - h.lastCPUTime
	wallTimeDelta := now.Sub(h.lastCPUMeasurement)
	
	// Update stored values for next calculation
	h.lastCPUTime = currentCPUTime
	h.lastCPUMeasurement = now
	
	if wallTimeDelta == 0 {
		return 0.0
	}
	
	// CPU percentage = (CPU time delta / wall time delta) * 100
	cpuPercent := float64(cpuTimeDelta) / float64(wallTimeDelta) * 100.0
	
	// Cap at 100% (can exceed on multi-core systems)
	if cpuPercent > 100.0 {
		cpuPercent = 100.0
	}
	
	return cpuPercent
}

// getCPUTimeLinux reads CPU time from /proc/pid/stat on Linux
func (h *HTTPSyncStrategy) getCPUTimeLinux(pid int) (time.Duration, error) {
	statFile := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statFile)
	if err != nil {
		return 0, err
	}
	
	fields := strings.Fields(string(data))
	if len(fields) < 15 {
		return 0, fmt.Errorf("insufficient fields in /proc/stat")
	}
	
	// Fields 13 and 14 contain user and system CPU time in clock ticks
	utime, err := strconv.ParseInt(fields[13], 10, 64)
	if err != nil {
		return 0, err
	}
	
	stime, err := strconv.ParseInt(fields[14], 10, 64)
	if err != nil {
		return 0, err
	}
	
	// Convert clock ticks to time duration
	// Get clock ticks per second (usually 100 on most Linux systems)
	clockTicks := int64(100)
	
	totalTicks := utime + stime
	totalNanos := (totalTicks * int64(time.Second)) / clockTicks
	
	return time.Duration(totalNanos), nil
}

// getCPUTimeDarwin reads CPU time on macOS using runtime stats
func (h *HTTPSyncStrategy) getCPUTimeDarwin(pid int) (time.Duration, error) {
	// On macOS, we'll use runtime stats as a fallback since syscall.Getrusage is not available
	if pid != os.Getpid() {
		return 0, fmt.Errorf("can only get CPU time for current process on macOS")
	}
	
	// Get memory stats which include runtime information
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	
	// Estimate CPU time based on GC pause times and other runtime metrics
	// This is a simplified approach since direct CPU time access requires platform-specific code
	gcPauseTotal := time.Duration(0)
	for i := 0; i < len(stats.PauseNs); i++ {
		gcPauseTotal += time.Duration(stats.PauseNs[i])
	}
	
	// Return estimated CPU time based on GC activity and uptime
	// This is an approximation since we can't access rusage on this platform
	uptime := time.Since(h.startTime)
	estimatedCPUTime := uptime/10 + gcPauseTotal // rough estimate
	
	return estimatedCPUTime, nil
}

// handleInfoTags provides tag discovery for a specific probe
func (h *HTTPSyncStrategy) handleInfoTags(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	probeName := vars["probe"]
	
	if providedKey != h.agentKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	h.cache.mu.RLock()
	defer h.cache.mu.RUnlock()
	
	// Get time series keys for the probe
	tsKeys, exists := h.cache.probeIndex[probeName]
	if !exists {
		http.Error(w, "Probe not found", http.StatusNotFound)
		return
	}
	
	// Analyze tags from all metrics of this probe
	tagValues := make(map[string]map[string]int)
	metrics := make(map[string]bool)
	
	for tsKey := range tsKeys {
		if metric, exists := h.cache.timeSeries[tsKey]; exists {
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
			Description: h.getTagDescription(tagKey),
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

// handleInfoSchema provides complete schema information with examples
func (h *HTTPSyncStrategy) handleInfoSchema(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	probeName := vars["probe"]
	
	if providedKey != h.agentKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	// Reuse tag discovery logic
	h.cache.mu.RLock()
	tsKeys, exists := h.cache.probeIndex[probeName]
	if !exists {
		h.cache.mu.RUnlock()
		http.Error(w, "Probe not found", http.StatusNotFound)
		return
	}
	
	tagValues := make(map[string]map[string]int)
	metrics := make(map[string]bool)
	
	for tsKey := range tsKeys {
		if metric, exists := h.cache.timeSeries[tsKey]; exists {
			metrics[metric.MetricName] = true
			for tagKey, tagValue := range metric.Tags {
				if tagValues[tagKey] == nil {
					tagValues[tagKey] = make(map[string]int)
				}
				tagValues[tagKey][tagValue]++
			}
		}
	}
	h.cache.mu.RUnlock()
	
	// Build tags info
	tags := make(map[string]TagInfo)
	for tagKey, values := range tagValues {
		var valueList []string
		for value := range values {
			valueList = append(valueList, value)
		}
		tags[tagKey] = TagInfo{
			Values:      valueList,
			Description: h.getTagDescription(tagKey),
			SampleCount: len(valueList),
		}
	}
	
	var metricList []string
	for metric := range metrics {
		metricList = append(metricList, metric)
	}
	
	// Generate examples
	examples := h.generateExamples(probeName, tags, metricList)
	
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

// getTagDescription provides human-readable descriptions for common tags
func (h *HTTPSyncStrategy) getTagDescription(tagKey string) string {
	descriptions := map[string]string{
		"core":       "CPU core identifier",
		"instance":   "CPU instance identifier (Windows)",
		"interface":  "Network interface name",
		"adapter":    "Network adapter name (Windows)",
		"device":     "Device identifier",
		"drive":      "Drive identifier",
		"controller": "Controller identifier",
		"slot":       "Physical slot number",
		"channel":    "Channel number",
		"host":       "Hostname",
		"os":         "Operating system",
		"platform":   "Platform identifier",
		"probe_name": "Source probe name",
	}
	
	if desc, exists := descriptions[tagKey]; exists {
		return desc
	}
	return "No description available"
}

// generateExamples creates example API calls for the probe
func (h *HTTPSyncStrategy) generateExamples(probeName string, tags map[string]TagInfo, metrics []string) []MetricExample {
	var examples []MetricExample
	baseURL := fmt.Sprintf("/api/{agentkey}/prtg/metrics/%s", probeName)
	
	// Example 1: Basic usage
	examples = append(examples, MetricExample{
		Description: "Get all metrics for this probe",
		URL:         baseURL,
		ResultCount: len(metrics),
	})
	
	// Example 2: Tag filtering
	for tagKey, tagInfo := range tags {
		if len(tagInfo.Values) > 1 {
			firstValue := tagInfo.Values[0]
			examples = append(examples, MetricExample{
				Description: fmt.Sprintf("Filter by %s=%s", tagKey, firstValue),
				URL:         fmt.Sprintf("%s?tags=%s:%s", baseURL, tagKey, firstValue),
				ResultCount: tagInfo.SampleCount,
			})
			break
		}
	}
	
	// Example 3: Metric filtering
	if len(metrics) > 3 {
		selectedMetrics := metrics[:3]
		examples = append(examples, MetricExample{
			Description: "Get specific metrics only",
			URL:         fmt.Sprintf("%s?metrics=%s", baseURL, strings.Join(selectedMetrics, ",")),
			ResultCount: 3,
		})
	}
	
	// Example 4: Pagination
	if len(metrics) > 5 {
		examples = append(examples, MetricExample{
			Description: "Get first 5 metrics",
			URL:         fmt.Sprintf("%s?limit=5", baseURL),
			ResultCount: 5,
		})
	}
	
	return examples
}

// getMetricsForProbe retrieves and transforms metrics for a specific probe (legacy - no filters)
func (h *HTTPSyncStrategy) getMetricsForProbe(probeName string) []PRTGChannel {
	return h.getMetricsForProbeWithFilter(probeName, MetricFilter{})
}

// getMetricsForProbeWithFilter retrieves and transforms metrics for a specific probe with filtering
func (h *HTTPSyncStrategy) getMetricsForProbeWithFilter(probeName string, filter MetricFilter) []PRTGChannel {
	h.cache.mu.RLock()
	defer h.cache.mu.RUnlock()

	// Calculate total cache size for logging using TSDB structure
	totalCacheSize := len(h.cache.timeSeries)
	probeNames := make(map[string]int)
	for probe, tsKeys := range h.cache.probeIndex {
		probeNames[probe] = len(tsKeys)
	}

	h.logger.Info().
		Str("requested_probe", probeName).
		Int("total_time_series", totalCacheSize).
		Msg("🔍 Getting metrics for probe from TSDB")

	h.logger.Info().
		Interface("available_probes", probeNames).
		Str("requested_probe", probeName).
		Msg("🗂️ Available probe names in TSDB cache")

	// Get time series keys for the specific probe (O(1) access)
	tsKeys, exists := h.cache.probeIndex[probeName]
	if !exists {
		h.logger.Info().Str("probe", probeName).Msg("📭 No metrics found for probe")
		return []PRTGChannel{}
	}

	// Extract metrics from time series (already deduplicated by design)
	var validMetrics []CachedMetric
	now := time.Now()
	
	for tsKey := range tsKeys {
		if metric, exists := h.cache.timeSeries[tsKey]; exists {
			// Filter expired metrics
			if now.Sub(metric.Timestamp) <= h.cache.ttl {
				validMetrics = append(validMetrics, metric)
			}
		}
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("time_series_count", len(tsKeys)).
		Int("valid_metrics_count", len(validMetrics)).
		Msg("📦 Extracted metrics from TSDB")

	// Apply filters if provided
	if len(filter.TagFilters) > 0 || len(filter.ExcludeTags) > 0 || len(filter.MetricNames) > 0 || filter.Limit > 0 || filter.Offset > 0 {
		validMetrics = h.applyMetricFilter(validMetrics, filter)
		h.logger.Info().
			Str("probe", probeName).
			Int("filtered_metrics_count", len(validMetrics)).
			Interface("filter", filter).
			Msg("🔍 Applied metric filters")
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("valid_metrics_count", len(validMetrics)).
		Msg("🔄 After TTL filtering and optional query filters")

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
		Msg("🔍 Getting SenHub metrics for probe from TSDB")

	// Get time series keys for the specific probe (O(1) access)
	tsKeys, exists := h.cache.probeIndex[probeName]
	if !exists {
		h.logger.Info().Str("probe", probeName).Msg("📭 No metrics found for probe")
		return []SenHubMetric{}
	}

	// Extract metrics from time series (already deduplicated by design)
	var validMetrics []CachedMetric
	now := time.Now()
	
	for tsKey := range tsKeys {
		if metric, exists := h.cache.timeSeries[tsKey]; exists {
			// Filter expired metrics
			if now.Sub(metric.Timestamp) <= h.cache.ttl {
				validMetrics = append(validMetrics, metric)
			}
		}
	}

	h.logger.Info().
		Str("probe", probeName).
		Int("time_series_count", len(tsKeys)).
		Int("valid_metrics_count", len(validMetrics)).
		Msg("📦 Extracted metrics from TSDB")

	h.logger.Info().
		Str("probe", probeName).
		Int("valid_metrics_count", len(validMetrics)).
		Msg("🔄 After TTL filtering (no deduplication needed in TSDB)")

	// Convert to SenHub format
	var senHubMetrics []SenHubMetric
	for _, metric := range validMetrics {
		senHubMetric := h.convertToSenHubFormat(metric)
		senHubMetrics = append(senHubMetrics, senHubMetric)
		
		h.logger.Debug().
			Str("metric_name", metric.MetricName).
			Str("channel", senHubMetric.Channel).
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

// deduplicateAndFilterMetrics is now DEPRECATED - TSDB handles this automatically
// This function is kept for backwards compatibility but should not be used
// The TSDB approach eliminates duplicates at write-time and handles TTL during read
func (h *HTTPSyncStrategy) deduplicateAndFilterMetrics(metrics []CachedMetric) []CachedMetric {
	h.logger.Warn().Msg("⚠️ DEPRECATED: deduplicateAndFilterMetrics called - TSDB handles this automatically")
	// Return as-is since TSDB already handles deduplication and TTL
	return metrics
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
		Channel:     displayName,
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

	// Transform metric name to user-friendly channel name for PRTG
	channelName, unit := h.transformMetricNameForPRTG(key, metric)

	// Get lookup information from transformer
	lookup := h.getLookupForMetric(key, metric.ProbeName)

	// PRTG-specific unit handling
	prtgChannel := &PRTGChannel{
		Channel: channelName,
		Value:   value,
		Float:   1,
	}
	
	// For PRTG, always use "custom" as unit and put real unit in customunit
	if unit != "" {
		prtgChannel.Unit = "custom"
		prtgChannel.CustomUnit = unit
	}
	
	// Add lookup if available
	if lookup != "" {
		prtgChannel.ValueLookup = lookup
	}
	
	return prtgChannel
}

// getLookupForMetric gets the lookup file for a metric using the transformer
func (h *HTTPSyncStrategy) getLookupForMetric(metricName, probeName string) string {
	// Get transformer for this probe
	transformer, err := h.transformerRegistry.LoadTransformer(probeName, "friendly")
	if err != nil {
		h.logger.Debug().
			Err(err).
			Str("probe", probeName).
			Str("metric", metricName).
			Msg("Failed to load transformer for lookup")
		return ""
	}
	
	// Get lookup from transformer
	lookup := transformer.GetLookup(metricName)
	
	h.logger.Debug().
		Str("probe", probeName).
		Str("metric", metricName).
		Str("lookup", lookup).
		Msg("Retrieved lookup for metric")
		
	return lookup
}

// handleListEndpoints lists all available API endpoints
func (h *HTTPSyncStrategy) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	// Extract and validate agentkey from path
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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
		{"/api/{agentkey}/nagios/check/{check_name}", []string{"GET"}, "Execute Nagios check", "nagios"},
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
		h.logger.Error().Err(err).Msg("Failed to encode endpoints response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.logger.Info().Int("endpoints_count", len(endpoints)).Msg("Endpoints list response sent")
}

// parseMetricFilter parses query parameters into a MetricFilter
func (h *HTTPSyncStrategy) parseMetricFilter(r *http.Request) MetricFilter {
	filter := MetricFilter{
		TagFilters:  make(map[string][]string),
		ExcludeTags: make(map[string][]string),
		MetricNames: []string{},
		Limit:       0, // 0 means no limit
		Offset:      0,
	}
	
	query := r.URL.Query()
	
	// Parse tags parameter: tags=core:0,1,2&tags=interface:en0
	if tagsParam := query.Get("tags"); tagsParam != "" {
		h.parseTagFilter(tagsParam, filter.TagFilters)
	}
	
	// Parse exclude_tags parameter: exclude_tags=instance:_Total
	if excludeParam := query.Get("exclude_tags"); excludeParam != "" {
		h.parseTagFilter(excludeParam, filter.ExcludeTags)
	}
	
	// Parse metrics parameter: metrics=cpu_user,cpu_system
	if metricsParam := query.Get("metrics"); metricsParam != "" {
		filter.MetricNames = strings.Split(metricsParam, ",")
		// Trim whitespace
		for i, name := range filter.MetricNames {
			filter.MetricNames[i] = strings.TrimSpace(name)
		}
	}
	
	// Parse limit parameter: limit=50
	if limitParam := query.Get("limit"); limitParam != "" {
		if limit, err := strconv.Atoi(limitParam); err == nil && limit > 0 {
			filter.Limit = limit
		}
	}
	
	// Parse offset parameter: offset=100
	if offsetParam := query.Get("offset"); offsetParam != "" {
		if offset, err := strconv.Atoi(offsetParam); err == nil && offset >= 0 {
			filter.Offset = offset
		}
	}
	
	return filter
}

// parseTagFilter parses tag filter string like "core:0,1,2" or "interface:en0"
func (h *HTTPSyncStrategy) parseTagFilter(param string, filterMap map[string][]string) {
	parts := strings.Split(param, ":")
	if len(parts) != 2 {
		return
	}
	
	tagName := strings.TrimSpace(parts[0])
	valuesStr := strings.TrimSpace(parts[1])
	
	if tagName == "" || valuesStr == "" {
		return
	}
	
	// Split values by comma
	values := strings.Split(valuesStr, ",")
	for i, value := range values {
		values[i] = strings.TrimSpace(value)
	}
	
	filterMap[tagName] = values
}

// applyMetricFilter filters metrics based on the provided filter
func (h *HTTPSyncStrategy) applyMetricFilter(metrics []CachedMetric, filter MetricFilter) []CachedMetric {
	var filtered []CachedMetric
	
	for _, metric := range metrics {
		// Check metric name filter
		if len(filter.MetricNames) > 0 {
			found := false
			for _, name := range filter.MetricNames {
				if metric.MetricName == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		
		// Check tag filters (include)
		if len(filter.TagFilters) > 0 {
			matches := true
			for tagName, allowedValues := range filter.TagFilters {
				tagValue, exists := metric.Tags[tagName]
				if !exists {
					matches = false
					break
				}
				
				// Check if tag value is in allowed values
				found := false
				for _, allowedValue := range allowedValues {
					if tagValue == allowedValue {
						found = true
						break
					}
				}
				if !found {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
		}
		
		// Check exclude tag filters
		if len(filter.ExcludeTags) > 0 {
			excluded := false
			for tagName, excludedValues := range filter.ExcludeTags {
				tagValue, exists := metric.Tags[tagName]
				if !exists {
					continue
				}
				
				// Check if tag value is in excluded values
				for _, excludedValue := range excludedValues {
					if tagValue == excludedValue {
						excluded = true
						break
					}
				}
				if excluded {
					break
				}
			}
			if excluded {
				continue
			}
		}
		
		filtered = append(filtered, metric)
	}
	
	// Apply pagination
	if filter.Offset > 0 || filter.Limit > 0 {
		start := filter.Offset
		if start > len(filtered) {
			return []CachedMetric{}
		}
		
		end := len(filtered)
		if filter.Limit > 0 && start+filter.Limit < end {
			end = start + filter.Limit
		}
		
		filtered = filtered[start:end]
	}
	
	return filtered
}

// transformMetricNameForPRTG converts technical metric names to user-friendly channel names for PRTG
// Returns both the transformed name and unit
func (h *HTTPSyncStrategy) transformMetricNameForPRTG(key string, metric CachedMetric) (string, string) {
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

	// Map individual host probes to the "host" category
	probeCategory := probeName
	if probeName == "cpu" || probeName == "memory" || probeName == "network" || probeName == "wifi_signal_strength" {
		probeCategory = "host"
	}
	
	// Always use "friendly" style for PRTG endpoints
	style := "friendly"

	// Load transformer for this probe category and style
	h.logger.Debug().
		Str("probe_name", probeName).
		Str("probe_category", probeCategory).
		Str("style", style).
		Msg("🔍 Loading transformer")
		
	transformer, err := h.transformerRegistry.LoadTransformer(probeCategory, style)
	if err != nil {
		h.logger.Warn().
			Err(err).
			Str("probe", probeName).
			Str("probe_category", probeCategory).
			Str("style", style).
			Msg("❌ Failed to load transformer, using fallback")
		return metricName, "" // Fallback to original metric name with no unit
	}
	
	h.logger.Debug().
		Str("probe_category", probeCategory).
		Str("transformer_type", fmt.Sprintf("%T", transformer)).
		Msg("✅ Transformer loaded successfully")

	// Transform the metric name using all available tags
	transformedName := transformer.TransformMetricName(metricName, metric.Tags)
	
	// Get unit from transformer
	unit := transformer.GetUnit(metricName)
	
	// Debug logging to trace transformation
	h.logger.Debug().
		Str("original_metric", metricName).
		Str("transformed_name", transformedName).
		Str("unit", unit).
		Str("probe_category", probeCategory).
		Interface("tags", metric.Tags).
		Msg("🔧 PRTG metric transformation")
	
	return transformedName, unit
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

	// Convert TSDB cache data to debug format
	for probeName, tsKeys := range h.cache.probeIndex {
		summary[probeName] = len(tsKeys)
		
		for tsKey := range tsKeys {
			if metric, exists := h.cache.timeSeries[tsKey]; exists {
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

// cleanup removes expired metrics from TSDB cache
func (cache *MetricCache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cache.mu.Lock()
			now := time.Now()
			
			// Clean up expired metrics from TSDB
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
		case <-cache.stopChan:
			return
		}
	}
}

// GetAllMetrics returns all cached metrics
func (cache *MetricCache) GetAllMetrics() []CachedMetric {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	
	metrics := make([]CachedMetric, 0, len(cache.timeSeries))
	for _, metric := range cache.timeSeries {
		metrics = append(metrics, metric)
	}
	
	return metrics
}

// GetProbeMetrics returns all metrics for a specific probe
func (cache *MetricCache) GetProbeMetrics(probeName string) []CachedMetric {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	
	var metrics []CachedMetric
	if probeKeys, exists := cache.probeIndex[probeName]; exists {
		for tsKey := range probeKeys {
			if metric, exists := cache.timeSeries[tsKey]; exists {
				metrics = append(metrics, metric)
			}
		}
	}
	
	return metrics
}

// handleNagiosMetricsGET handles GET requests for Nagios format metrics by probe
func (h *HTTPSyncStrategy) handleNagiosMetricsGET(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	probeName := vars["probe"]

	// Validate agent key
	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for Nagios endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Info().Str("probe", probeName).Msg("🔄 Nagios endpoint - Request received")

	// Parse query parameters
	filter := h.parseMetricFilter(r)
	
	// Get probe metrics from cache
	metrics := h.cache.GetProbeMetrics(probeName)
	if len(metrics) == 0 {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(500)
		w.Write([]byte("CRITICAL - No metrics available for probe " + probeName))
		return
	}

	// Apply filters
	filteredMetrics := h.applyMetricFilter(metrics, filter)
	
	// Generate simple probe-based Nagios response
	response := h.generateSimpleNagiosResponse(probeName, filteredMetrics)
	
	w.Header().Set("Content-Type", "text/plain")
	if response.Status >= 2 {
		w.WriteHeader(500)
	}
	w.Write([]byte(fmt.Sprintf("%s - %s | %s", response.StatusText, response.Message, response.PerfData)))
}

// handleNagiosCheck handles GET requests for specific Nagios checks
func (h *HTTPSyncStrategy) handleNagiosCheck(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	checkName := vars["check_name"]

	// Validate agent key
	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for Nagios check endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Info().Str("check", checkName).Msg("🔄 Nagios check endpoint - Request received")

	// Load Nagios configuration
	config := h.loadNagiosConfig()
	check := h.findNagiosCheck(config, checkName)
	if check == nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(500)
		w.Write([]byte("CRITICAL - Check '" + checkName + "' not found"))
		return
	}

	// Parse query parameters for overrides
	filter := h.parseMetricFilter(r)
	overrides := h.parseNagiosOverrides(r)
	
	// Execute check
	response := h.executeNagiosCheck(check, filter, overrides)
	
	w.Header().Set("Content-Type", "text/plain")
	if response.Status >= 2 {
		w.WriteHeader(500)
	}
	w.Write([]byte(fmt.Sprintf("%s - %s | %s", response.StatusText, response.Message, response.PerfData)))
}

// handleNagiosMetrics handles GET/POST requests for all Nagios metrics
func (h *HTTPSyncStrategy) handleNagiosMetrics(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	// Validate agent key
	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for Nagios metrics endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Info().Str("method", r.Method).Msg("🔄 Nagios metrics endpoint - Request received")

	var nagiosRequest NagiosRequest
	
	if r.Method == "POST" {
		// Parse POST body for dynamic configuration
		if err := json.NewDecoder(r.Body).Decode(&nagiosRequest); err != nil {
			h.logger.Error().Err(err).Msg("Failed to parse Nagios request body")
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	}

	// Parse query parameters
	filter := h.parseMetricFilter(r)
	overrides := h.parseNagiosOverrides(r)
	
	// Load configuration
	config := h.loadNagiosConfig()
	
	// Execute all checks or specific check
	var responses []NagiosResponse
	if nagiosRequest.CheckName != "" {
		check := h.findNagiosCheck(config, nagiosRequest.CheckName)
		if check != nil {
			response := h.executeNagiosCheck(check, filter, overrides)
			responses = append(responses, response)
		}
	} else {
		// Execute all checks
		for _, check := range config.Checks {
			response := h.executeNagiosCheck(&check, filter, overrides)
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

// handleNagiosChecks lists all available Nagios checks
func (h *HTTPSyncStrategy) handleNagiosChecks(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	// Validate agent key
	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for Nagios checks endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Info().Msg("🔄 Nagios checks discovery endpoint - Request received")

	// Load Nagios configuration
	config := h.loadNagiosConfig()
	
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

// Nagios helper functions

// loadNagiosConfig loads the Nagios configuration from YAML file
func (h *HTTPSyncStrategy) loadNagiosConfig() *NagiosConfig {
	h.nagiosConfigMu.RLock()
	if h.nagiosConfig != nil {
		h.nagiosConfigMu.RUnlock()
		return h.nagiosConfig
	}
	h.nagiosConfigMu.RUnlock()

	h.nagiosConfigMu.Lock()
	defer h.nagiosConfigMu.Unlock()

	// Double-check after acquiring write lock
	if h.nagiosConfig != nil {
		return h.nagiosConfig
	}

	// Load from file
	configPath := filepath.Join("internal", "agent", "services", "data_store", "transformers", "definitions", "nagios.yaml")
	
	config, err := h.loadNagiosConfigFromFile(configPath)
	if err != nil {
		h.logger.Error().Err(err).Str("path", configPath).Msg("Failed to load Nagios config, using fallback")
		config = h.createFallbackNagiosConfig()
	}

	h.nagiosConfig = config
	return config
}

// loadNagiosConfigFromFile loads Nagios config from YAML file
func (h *HTTPSyncStrategy) loadNagiosConfigFromFile(configPath string) (*NagiosConfig, error) {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config NagiosConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse Nagios YAML: %w", err)
	}

	h.logger.Info().
		Str("version", config.Version).
		Int("checks_count", len(config.Checks)).
		Msg("Loaded Nagios configuration")

	return &config, nil
}

// createFallbackNagiosConfig creates a basic fallback configuration
func (h *HTTPSyncStrategy) createFallbackNagiosConfig() *NagiosConfig {
	return &NagiosConfig{
		Version:     "1.0.0",
		Description: "Fallback Nagios configuration",
		Checks: []NagiosCheck{
			{
				Name:        "system_health",
				Description: "Basic system health check",
				Metrics: []NagiosMetric{
					{
						Channel:     "cpu_usage_percent",
						Aggregation: "average",
						Warning:     "80",
						Critical:    "90",
						Unit:        "%",
					},
					{
						Channel:     "memory_used_percent",
						Warning:     "85",
						Critical:    "95",
						Unit:        "%",
					},
				},
			},
		},
	}
}

// findNagiosCheck finds a check by name in the configuration
func (h *HTTPSyncStrategy) findNagiosCheck(config *NagiosConfig, checkName string) *NagiosCheck {
	for _, check := range config.Checks {
		if check.Name == checkName {
			return &check
		}
	}
	return nil
}

// parseNagiosOverrides parses query parameters for Nagios threshold overrides
func (h *HTTPSyncStrategy) parseNagiosOverrides(r *http.Request) NagiosOverrides {
	query := r.URL.Query()
	
	overrides := NagiosOverrides{
		TagFilters: make(map[string]string),
	}

	if warning := query.Get("warning"); warning != "" {
		overrides.Warning = warning
	}
	
	if critical := query.Get("critical"); critical != "" {
		overrides.Critical = critical
	}

	// Parse tag filters from individual query params
	for key, values := range query {
		if key != "warning" && key != "critical" && key != "tags" && key != "exclude_tags" && key != "metrics" && key != "limit" && key != "offset" {
			if len(values) > 0 {
				overrides.TagFilters[key] = strings.Join(values, ",")
			}
		}
	}

	return overrides
}

// executeNagiosCheck executes a Nagios check with filtering and overrides
func (h *HTTPSyncStrategy) executeNagiosCheck(check *NagiosCheck, filter MetricFilter, overrides NagiosOverrides) NagiosResponse {
	h.logger.Debug().
		Str("check", check.Name).
		Interface("filter", filter).
		Interface("overrides", overrides).
		Msg("Executing Nagios check")

	// Get all metrics from cache
	allMetrics := h.cache.GetAllMetrics()
	
	// Apply probe filter if specified
	var metrics []CachedMetric
	if check.ProbeFilter != "" {
		metrics = h.cache.GetProbeMetrics(check.ProbeFilter)
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
	metrics = h.applyNagiosTagFilters(metrics, check.TagFilters)
	
	// Apply additional filters from query parameters
	metrics = h.applyMetricFilter(metrics, filter)

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
		result := h.processNagiosMetric(metricDef, metrics, overrides)
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
	statusText := h.getStatusText(overallStatus)
	
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

// NagiosMetricResult represents the result of processing a single metric
type NagiosMetricResult struct {
	Status   int
	Message  string
	PerfData string
}

// processNagiosMetric processes a single metric definition against available data
func (h *HTTPSyncStrategy) processNagiosMetric(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	// Filter metrics by channel name
	var matchingMetrics []CachedMetric
	for _, metric := range metrics {
		if metric.MetricName == metricDef.Channel {
			matchingMetrics = append(matchingMetrics, metric)
		}
	}

	if len(matchingMetrics) == 0 {
		return NagiosMetricResult{
			Status:  3, // UNKNOWN
			Message: fmt.Sprintf("%s metric not found", metricDef.Channel),
		}
	}

	// Apply aggregation or create separate checks
	if metricDef.Aggregation == "none" {
		// Return separate check for each metric instance
		return h.processNagiosMetricSeparate(metricDef, matchingMetrics, overrides)
	} else {
		// Aggregate metrics and return single result
		return h.processNagiosMetricAggregated(metricDef, matchingMetrics, overrides)
	}
}

// processNagiosMetricAggregated processes metrics with aggregation
func (h *HTTPSyncStrategy) processNagiosMetricAggregated(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	// Convert to float64 values
	var values []float64
	for _, metric := range metrics {
		if val, ok := metric.Value.(float64); ok {
			values = append(values, val)
		} else if val, ok := metric.Value.(float32); ok {
			values = append(values, float64(val))
		}
	}

	if len(values) == 0 {
		return NagiosMetricResult{
			Status:  3, // UNKNOWN
			Message: fmt.Sprintf("%s no valid numeric values", metricDef.Channel),
		}
	}

	// Apply aggregation
	aggregatedValue := h.aggregateValues(values, metricDef.Aggregation)
	
	// Get thresholds (with overrides)
	warning := metricDef.Warning
	critical := metricDef.Critical
	if overrides.Warning != "" {
		warning = overrides.Warning
	}
	if overrides.Critical != "" {
		critical = overrides.Critical
	}

	// Evaluate thresholds
	status := h.evaluateThreshold(aggregatedValue, warning, critical, metricDef.Invert)
	
	// Build context message
	var contextStr string
	if metricDef.TagContext != "" && len(metrics) > 0 {
		contextStr = h.buildTagContext(metrics[0], metricDef.TagContext)
	}
	
	// Build message
	var message string
	if status > 0 {
		statusText := h.getStatusText(status)
		if contextStr != "" {
			message = fmt.Sprintf("%s %s %s", contextStr, metricDef.Channel, statusText)
		} else {
			message = fmt.Sprintf("%s %s", metricDef.Channel, statusText)
		}
	}

	// Build performance data
	perfData := h.buildPerfData(metricDef.Channel, aggregatedValue, warning, critical, metricDef.Unit)

	return NagiosMetricResult{
		Status:   status,
		Message:  message,
		PerfData: perfData,
	}
}

// processNagiosMetricSeparate processes metrics separately (no aggregation)
func (h *HTTPSyncStrategy) processNagiosMetricSeparate(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	// For simplicity, return the worst case status and combine perf data
	worstStatus := 0
	var messages []string
	var perfDataItems []string

	for _, metric := range metrics {
		if val, ok := metric.Value.(float64); ok {
			// Get thresholds
			warning := metricDef.Warning
			critical := metricDef.Critical
			
			// Apply tag-specific thresholds
			for _, tagThreshold := range metricDef.TagSpecificThresholds {
				if h.matchesTagThreshold(metric, tagThreshold) {
					warning = tagThreshold.Warning
					critical = tagThreshold.Critical
					break
				}
			}
			
			// Apply overrides
			if overrides.Warning != "" {
				warning = overrides.Warning
			}
			if overrides.Critical != "" {
				critical = overrides.Critical
			}

			status := h.evaluateThreshold(val, warning, critical, metricDef.Invert)
			if status > worstStatus {
				worstStatus = status
			}

			// Build context
			contextStr := h.buildTagContext(metric, metricDef.TagContext)
			
			if status > 0 {
				statusText := h.getStatusText(status)
				if contextStr != "" {
					messages = append(messages, fmt.Sprintf("%s %s", contextStr, statusText))
				}
			}

			// Build perf data with context
			perfName := metricDef.Channel
			if contextStr != "" {
				perfName = fmt.Sprintf("%s_%s", metricDef.Channel, strings.ReplaceAll(contextStr, " ", "_"))
			}
			perfDataItems = append(perfDataItems, h.buildPerfData(perfName, val, warning, critical, metricDef.Unit))
		}
	}

	return NagiosMetricResult{
		Status:   worstStatus,
		Message:  strings.Join(messages, ", "),
		PerfData: strings.Join(perfDataItems, " "),
	}
}

// Helper utility functions for Nagios processing

// applyNagiosTagFilters applies tag filters from Nagios check configuration
func (h *HTTPSyncStrategy) applyNagiosTagFilters(metrics []CachedMetric, filters []NagiosTagFilter) []CachedMetric {
	if len(filters) == 0 {
		return metrics
	}

	var filtered []CachedMetric
	for _, metric := range metrics {
		include := true
		
		for _, filter := range filters {
			tagValue := h.getTagValue(metric, filter.Key)
			
			switch filter.Operator {
			case "exists":
				if tagValue == "" {
					include = false
				}
			case "in":
				if len(filter.Values) > 0 {
					found := false
					for _, value := range filter.Values {
						if tagValue == value {
							found = true
							break
						}
					}
					if !found {
						include = false
					}
				}
			case "not_in":
				if len(filter.Values) > 0 {
					for _, value := range filter.Values {
						if tagValue == value {
							include = false
							break
						}
					}
				}
			case "equals":
				if len(filter.Values) > 0 && tagValue != filter.Values[0] {
					include = false
				}
			case "not_equals":
				if len(filter.Values) > 0 && tagValue == filter.Values[0] {
					include = false
				}
			}
			
			if !include {
				break
			}
		}
		
		if include {
			filtered = append(filtered, metric)
		}
	}
	
	return filtered
}

// getTagValue gets the value of a tag from a metric
func (h *HTTPSyncStrategy) getTagValue(metric CachedMetric, tagKey string) string {
	if value, exists := metric.Tags[tagKey]; exists {
		return value
	}
	return ""
}

// matchesTagThreshold checks if a metric matches tag-specific threshold conditions
func (h *HTTPSyncStrategy) matchesTagThreshold(metric CachedMetric, threshold NagiosTagThreshold) bool {
	for key, value := range threshold.Tags {
		if value == "*" {
			// Wildcard - any value matches
			continue
		}
		
		tagValue := h.getTagValue(metric, key)
		if tagValue != value {
			return false
		}
	}
	return true
}

// buildTagContext builds a context string from metric tags
func (h *HTTPSyncStrategy) buildTagContext(metric CachedMetric, tagContext string) string {
	if tagContext == "" {
		return ""
	}
	
	tagValue := h.getTagValue(metric, tagContext)
	if tagValue != "" {
		return tagValue
	}
	
	return ""
}

// aggregateValues aggregates a slice of values using the specified method
func (h *HTTPSyncStrategy) aggregateValues(values []float64, aggregation string) float64 {
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
func (h *HTTPSyncStrategy) evaluateThreshold(value float64, warning, critical string, invert bool) int {
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

// getStatusText converts numeric status to text
func (h *HTTPSyncStrategy) getStatusText(status int) string {
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

// buildPerfData builds Nagios performance data string with optimal graphing format
func (h *HTTPSyncStrategy) buildPerfData(name string, value float64, warning, critical, unit string) string {
	// Clean label name (no spaces, special chars)
	cleanName := h.cleanPerfDataLabel(name)
	
	// Convert unit to standard Nagios UOM
	standardUOM := h.convertToStandardUOM(unit)
	
	// Determine min/max values based on metric type
	min, max := h.getPerfDataMinMax(unit, value)
	
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
func (h *HTTPSyncStrategy) cleanPerfDataLabel(name string) string {
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
func (h *HTTPSyncStrategy) convertToStandardUOM(unit string) string {
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
func (h *HTTPSyncStrategy) getPerfDataMinMax(unit string, value float64) (string, string) {
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

// generateSimpleNagiosResponse generates a simple Nagios response for probe-based queries
func (h *HTTPSyncStrategy) generateSimpleNagiosResponse(probeName string, metrics []CachedMetric) NagiosResponse {
	if len(metrics) == 0 {
		return NagiosResponse{
			Status:     2, // CRITICAL
			StatusText: "CRITICAL",
			Message:    fmt.Sprintf("No metrics available for probe %s", probeName),
			PerfData:   "",
		}
	}
	
	// Simple health check - just report metrics count and build basic perf data
	var perfDataItems []string
	metricCount := 0
	
	for _, metric := range metrics {
		// Transform metric name using the same logic as PRTG
		transformedName, _ := h.transformMetricNameForPRTG("", metric)
		cleanName := h.cleanPerfDataLabel(transformedName)
		
		if val, ok := metric.Value.(float64); ok {
			perfDataItems = append(perfDataItems, fmt.Sprintf("%s=%.2f", cleanName, val))
			metricCount++
		} else if val, ok := metric.Value.(float32); ok {
			perfDataItems = append(perfDataItems, fmt.Sprintf("%s=%.2f", cleanName, float64(val)))
			metricCount++
		}
	}
	
	message := fmt.Sprintf("Probe %s healthy - %d metrics collected", probeName, metricCount)
	perfData := strings.Join(perfDataItems, " ")
	
	return NagiosResponse{
		Status:     0, // OK
		StatusText: "OK",
		Message:    message,
		PerfData:   perfData,
	}
}

// handleZabbixMetricsGET handles GET requests for Zabbix format metrics
func (h *HTTPSyncStrategy) handleZabbixMetricsGET(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]
	probeName := vars["probe"]

	// Validate agent key
	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for Zabbix endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Info().Str("probe", probeName).Msg("🔄 Zabbix endpoint - Request received")

	// TODO: Implement Zabbix format conversion
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte(`{"error": "Zabbix format endpoint not yet implemented"}`))
}

// handlePrometheusMetricsGET handles GET requests for Prometheus format metrics
func (h *HTTPSyncStrategy) handlePrometheusMetricsGET(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	// Validate agent key
	if providedKey != h.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key for Prometheus endpoint")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Info().Msg("🔄 Prometheus endpoint - Request received")

	// TODO: Implement Prometheus format conversion
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotImplemented)
	w.Write([]byte("# Prometheus format endpoint not yet implemented\n"))
}

// Web UI Handlers

func (h *HTTPSyncStrategy) handleWebDashboard(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentKey := vars["agentkey"]
	
	if !h.validateAgentKey(agentKey) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	// Render the new dashboard template
	templateName := GetTemplateName(r.URL.Path)
	if templateName == "" {
		templateName = "dashboard" // Default to dashboard for root and dashboard paths
	}
	
	content, err := h.assetHandler.RenderTemplate(templateName)
	if err != nil {
		h.logger.Error().Err(err).Str("template", templateName).Msg("Failed to render dashboard template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(content))
}

func (h *HTTPSyncStrategy) handleWebExplorer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentKey := vars["agentkey"]
	
	if !h.validateAgentKey(agentKey) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	// Create asset handler
	assetHandler := NewAssetHandler(agentKey)
	
	// Render API Explorer template
	html, err := assetHandler.RenderTemplate("api-explorer")
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to render API Explorer template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (h *HTTPSyncStrategy) handleWebDocs(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentKey := vars["agentkey"]
	
	if !h.validateAgentKey(agentKey) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	// Render the documentation template
	content, err := h.assetHandler.RenderTemplate("docs")
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to render docs template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(content))
}

func (h *HTTPSyncStrategy) handleWebAdmin(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentKey := vars["agentkey"]
	
	if !h.validateAgentKey(agentKey) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	// TODO: Implement admin template
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<h1>Administration</h1><p>Coming soon...</p>"))
}

func (h *HTTPSyncStrategy) handleWebAssets(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentKey := vars["agentkey"]
	
	if !h.validateAgentKey(agentKey) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	
	// Create asset handler and serve the requested asset
	assetHandler := NewAssetHandler(agentKey)
	assetHandler.ServeAsset(w, r, r.URL.Path)
}

func (h *HTTPSyncStrategy) validateAgentKey(providedKey string) bool {
	return providedKey == h.agentKey
}
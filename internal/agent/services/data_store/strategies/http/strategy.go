// senhub-agent/internal/agent/services/data_store/strategy_http.go
package http

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/configuration"
	"senhub-agent.go/internal/agent/services/data_store/transformers"
	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
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
	startTime           time.Time              // agent start time for uptime calculation
	assetHandler        *AssetHandler          // asset handler for templates and static files
	formatConverter     *FormatConverter       // format conversion for monitoring tools
	webInterface        *WebInterface          // web UI interface handler
	authManager         *AuthenticationManager // authentication and security manager
	healthManager       *HealthManager         // health check and system monitoring manager
	metricsProcessor    *MetricsProcessor      // metrics processing and format conversion manager
	configManager       *ConfigurationManager  // configuration management and validation
	serverManager       *ServerManager         // HTTP server lifecycle and routing management
	debugManager        *DebugManager          // debug and admin utilities manager
	apiManager          *APIManager            // API endpoints manager (PRTG, SenHub, Info)
	nagiosManager       *NagiosManager         // Nagios endpoints and processing manager
	utilsManager        *UtilsManager          // utility functions and helper methods manager
	statusService       *status.StatusService  // centralized status calculation service
	lookupRegistry      *LookupRegistry        // lookup definitions registry for status/health mappings
	lookupsManager      *LookupsManager        // lookups API endpoints manager
}

// SenHubMetric represents a metric in standardized SenHub raw format
type SenHubMetric struct {
	Name        string            `json:"name,omitempty" yaml:"name"` // Technical metric name
	Channel     string            `json:"channel" yaml:"channel"`     // Transformed display name (main field)
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
	Float           *int    `json:"float,omitempty"` // Pointer to make optional for lookup metrics
	Unit            string  `json:"unit,omitempty"`
	CustomUnit      string  `json:"customunit,omitempty"`
	LimitMode       int     `json:"limitmode,omitempty"`
	LimitMaxWarning float64 `json:"limitmaxwarning,omitempty"`
	LimitMaxError   float64 `json:"limitmaxerror,omitempty"`
	ValueLookup     string  `json:"valuelookup,omitempty"`
}

// MetricFilter represents query parameters for filtering metrics

// NewHTTPSyncStrategy creates a new HTTP sync strategy
func NewHTTPSyncStrategy(
	agentConfig configuration.AgentConfiguration,
	params map[string]interface{},
	baseLogger *logger.Logger,
) interface{} {
	// Create module-specific logger for HTTP strategy
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.http")

	// Get cache configuration from agent config
	cacheRetentionMinutes := 5 // Default value

	// Try to get cache config from agentConfiguration (which may have LocalConfiguration reference)
	if agentConfigWithCache, ok := agentConfig.(interface {
		GetCacheConfig() *configuration.CacheConfig
	}); ok {
		cacheConfig := agentConfigWithCache.GetCacheConfig()
		cacheRetentionMinutes = cacheConfig.RetentionMinutes
		moduleLogger.Info().
			Int("retention_minutes", cacheRetentionMinutes).
			Msg("Using cache configuration from agent config")
	} else {
		moduleLogger.Info().
			Str("config_type", fmt.Sprintf("%T", agentConfig)).
			Int("default_retention_minutes", cacheRetentionMinutes).
			Msg("Using default cache configuration (no cache config interface)")
	}

	strategy := &HTTPSyncStrategy{
		agentConfig:         agentConfig,
		params:              params,
		logger:              moduleLogger,
		agentKey:            agentConfig.GetAuthenticationKey(),
		port:                8080,      // Default port
		bindAddress:         "0.0.0.0", // Default to all interfaces
		transformerRegistry: transformers.NewTransformerRegistry(moduleLogger.Logger),
		startTime:           time.Now(), // Initialize start time for uptime calculation
		assetHandler:        NewAssetHandler(agentConfig.GetAuthenticationKey()),
		cache:               NewMetricCache(time.Duration(cacheRetentionMinutes)*time.Minute, moduleLogger),
	}

	// Initialize format converter after cache is created
	strategy.formatConverter = NewFormatConverter(strategy.transformerRegistry, moduleLogger, strategy.cache)

	// Initialize authentication manager
	strategy.authManager = NewAuthenticationManager(strategy.agentKey, agentConfig, moduleLogger)

	// Initialize web interface handler
	strategy.webInterface = NewWebInterface(strategy, moduleLogger)

	// Initialize health manager
	strategy.healthManager = NewHealthManager(strategy, moduleLogger, strategy.startTime)

	// Initialize metrics processor
	strategy.metricsProcessor = NewMetricsProcessor(strategy.cache, strategy.formatConverter, strategy.lookupRegistry, moduleLogger)

	// Initialize configuration manager
	strategy.configManager = NewConfigurationManager(agentConfig, params, moduleLogger)

	// Update strategy fields from configuration manager
	strategy.port = strategy.configManager.GetPort()
	strategy.bindAddress = strategy.configManager.GetBindAddress()

	// Initialize debug manager
	strategy.debugManager = NewDebugManager(strategy, moduleLogger)

	// Initialize API manager
	strategy.apiManager = NewAPIManager(strategy, moduleLogger)

	// Initialize Nagios manager
	strategy.nagiosManager = NewNagiosManager(strategy, moduleLogger)

	// Initialize utils manager
	strategy.utilsManager = NewUtilsManager(strategy, moduleLogger)

	// Initialize lookup registry
	lookupRegistry, err := NewLookupRegistry(moduleLogger.Logger)
	if err != nil {
		moduleLogger.Error().Err(err).Msg("Failed to initialize lookup registry - lookups will be unavailable")
		// Continue without lookups - non-critical feature
	} else {
		strategy.lookupRegistry = lookupRegistry
		// Initialize lookups manager only if registry loaded successfully
		strategy.lookupsManager = NewLookupsManager(strategy)
	}

	// Initialize status service with centralized status calculations
	strategy.statusService = status.NewStatusService(
		moduleLogger.Logger,
		"unknown", // Version will be set later if available
		"unknown", // Commit will be set later if available
	)

	// Configure status service with cache provider and agent mode
	cacheAdapter := NewHTTPCacheAdapter(strategy.cache, moduleLogger.Logger)
	strategy.statusService.SetCacheProvider(cacheAdapter)

	// Determine agent mode (online/offline) from configuration
	agentMode := strategy.determineAgentMode()
	strategy.statusService.SetAgentMode(agentMode)

	// Initialize server manager (must be last, needs access to all other modules)
	strategy.serverManager = NewServerManager(strategy, moduleLogger)

	return strategy
}

// determineAgentMode determines if the agent is running in online or offline mode
func (h *HTTPSyncStrategy) determineAgentMode() string {
	// Simple heuristic: if agent key looks like offline generated key, assume offline mode
	if h.agentKey == "" {
		return "unknown"
	}

	// Offline keys typically contain "offline" or are generated with machine fingerprint
	if len(h.agentKey) > 20 && (h.agentKey[:7] == "offline" || h.agentKey[:4] == "test") {
		return "offline"
	}

	// For now, assume online mode for other keys
	// This could be enhanced with proper mode detection
	return "online"
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
	return h.configManager.ValidateConfigParams(params)
}

// Start initializes the HTTP server and cache cleanup
func (h *HTTPSyncStrategy) Start() error {
	h.logger.Info().
		Int("port", h.port).
		Str("bind_address", h.bindAddress).
		Msg("Starting HTTP strategy")

	// Delegate server startup to ServerManager
	if err := h.serverManager.Start(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Keep reference to server for compatibility
	h.server = h.serverManager.GetServer()

	return nil
}

// AddDataPoints stores the received datapoints in cache
func (h *HTTPSyncStrategy) AddDataPoints(datapoints []datapoint.DataPoint) error {
	h.logger.Info().Int("count", len(datapoints)).Msg("🔥 HTTP Strategy - Received datapoints")

	// Use the cache's method to add data points
	h.cache.AddDataPointsWithTransformer(datapoints, h.transformerRegistry)

	// Get cache info for logging
	cacheInfo := h.cache.GetCacheInfo()
	h.logger.Info().
		Int("count", len(datapoints)).
		Int("total_time_series", cacheInfo.TotalMetrics).
		Int("active_probes", cacheInfo.ProbeCount).
		Msg("✅ Datapoints added to TSDB cache")

	return nil
}

// Shutdown gracefully stops the HTTP server and cleanup routines
func (h *HTTPSyncStrategy) Shutdown(ctx context.Context) error {
	h.logger.Info().Msg("Shutting down HTTP strategy")

	// Delegate shutdown to ServerManager
	return h.serverManager.Shutdown(ctx)
}

// setupRoutes configures HTTP routes (delegated to ServerManager)
func (h *HTTPSyncStrategy) setupRoutes() *mux.Router {
	// Delegate to ServerManager for consistency
	return h.serverManager.setupRoutes()
}

// handlePRTGMetricsGET handles GET requests for PRTG metrics (delegated to APIManager)
func (h *HTTPSyncStrategy) handlePRTGMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandlePRTGMetricsGET(w, r)
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

// handleListProbes handles GET requests to list available probes (delegated to APIManager)
func (h *HTTPSyncStrategy) handleListProbes(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleListProbes(w, r)
}

// handlePRTGMetrics handles POST requests for PRTG metrics (delegated to APIManager)
func (h *HTTPSyncStrategy) handlePRTGMetrics(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandlePRTGMetrics(w, r)
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
	Status    string              `json:"status"`
	Version   string              `json:"version"`
	Commit    string              `json:"commit"`
	GoVersion string              `json:"go_version"`
	OS        string              `json:"os"`
	Arch      string              `json:"arch"`
	Port      int                 `json:"port"`
	Uptime    string              `json:"uptime"`
	Health    HealthCheckResponse `json:"health"`
	Cache     CacheInfoResponse   `json:"cache"`
	Resources ResourcesInfo       `json:"resources"`
}

// OTLPInfoResponse represents the response for /info/otlp — a snapshot
// of every OTLP self-metric exposed by `agentstate`. Designed to feed
// the CLI `agent status --otlp` view and the web dashboard's OTLP card
// without forcing either to scrape the Prometheus bridge.
type OTLPInfoResponse struct {
	Pipeline       OTLPPipelineInfo       `json:"pipeline"`
	Store          OTLPStoreInfo          `json:"store"`
	ExportDuration OTLPExportDurationInfo `json:"export_duration"`
	Checkpoint     OTLPCheckpointInfo     `json:"checkpoint"`
	LogsQueue      OTLPLogsQueueInfo      `json:"logs_queue"`
	Parallel       OTLPParallelInfo       `json:"parallel"`
}

// OTLPLogsQueueInfo reports the on-disk dead-letter queue for the logs
// signal (#217): live depth plus cumulative persisted/replayed counts.
type OTLPLogsQueueInfo struct {
	Records       int64  `json:"records"`
	Bytes         int64  `json:"bytes"`
	QueuedTotal   uint64 `json:"queued_total"`
	ReplayedTotal uint64 `json:"replayed_total"`
}

type OTLPPipelineInfo struct {
	MetricsPushedTotal uint64            `json:"metrics_pushed_total"`
	LogsPushedTotal    uint64            `json:"logs_pushed_total"`
	ExportErrorsTotal  uint64            `json:"export_errors_total"`
	DroppedTotal       uint64            `json:"dropped_total"`
	DroppedByReason    map[string]uint64 `json:"dropped_by_reason"`
}

type OTLPStoreInfo struct {
	Size               int64   `json:"size"`
	LogBufferFillRatio float64 `json:"log_buffer_fill_ratio"`
}

type OTLPExportDurationInfo struct {
	LastMs float64 `json:"last_ms"`
	MeanMs float64 `json:"mean_ms"`
}

type OTLPCheckpointInfo struct {
	SizeBytes          int64             `json:"size_bytes"`
	LastSaveAgeSeconds float64           `json:"last_save_age_seconds"`
	RestoredEntries    int64             `json:"restored_entries"`
	ErrorsTotal        uint64            `json:"errors_total"`
	ErrorsByStage      map[string]uint64 `json:"errors_by_stage"`
}

type OTLPParallelInfo struct {
	SubBatches int32 `json:"sub_batches"`
}

// ProbesInfoResponse represents the response for /info/probes
type ProbesInfoResponse struct {
	Probes       []string       `json:"probes"`
	ProbeMetrics map[string]int `json:"probe_metrics"`
	TotalMetrics int            `json:"total_metrics"`
}

// TagInfoResponse represents the response for /info/tags/{probe}
type TagInfoResponse struct {
	Probe        string             `json:"probe"`
	Tags         map[string]TagInfo `json:"tags"`
	Metrics      []string           `json:"metrics"`
	TotalMetrics int                `json:"total_metrics"`
	Categories   []CategoryInfo     `json:"categories,omitempty"`
}

// TagInfo contains information about a specific tag
type TagInfo struct {
	Values           []string          `json:"values"`
	Description      string            `json:"description"`
	SampleCount      int               `json:"sample_count"`
	Type             string            `json:"type"`                        // "category" or "resource"
	Label            string            `json:"label,omitempty"`             // Human-readable label
	ValueLabels      map[string]string `json:"value_labels,omitempty"`      // Raw value → human label
	LinkedCategories []string          `json:"linked_categories,omitempty"` // Show only when these categories are selected
}

// CategoryInfo describes a metric category for the UI
type CategoryInfo struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	MetricCount int    `json:"metric_count"`
}

// SchemaInfoResponse represents the response for /info/schema/{probe}
type SchemaInfoResponse struct {
	Probe        string             `json:"probe"`
	Tags         map[string]TagInfo `json:"tags"`
	Metrics      []string           `json:"metrics"`
	TotalMetrics int                `json:"total_metrics"`
	Examples     []MetricExample    `json:"examples"`
}

// MetricExample shows example usage of filters

// handleInfoProbes lists all available probes (delegated to APIManager)
func (h *HTTPSyncStrategy) handleInfoProbes(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleInfoProbes(w, r)
}

// handleInfoEndpoints provides discovery of available endpoints (delegated to APIManager)
func (h *HTTPSyncStrategy) handleInfoEndpoints(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleInfoEndpoints(w, r)
}

// handleInfoSystem provides system status and resource information (delegated to APIManager)
func (h *HTTPSyncStrategy) handleInfoSystem(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleInfoSystem(w, r)
}

// handleInfoOTLP exposes the OTLP self-metric snapshot (delegated to APIManager)
func (h *HTTPSyncStrategy) handleInfoOTLP(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleInfoOTLP(w, r)
}

// handleInfoTags provides tag discovery for a specific probe (delegated to APIManager)
func (h *HTTPSyncStrategy) handleInfoTags(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleInfoTags(w, r)
}

// handleInfoSchema provides complete schema information with examples (delegated to APIManager)
func (h *HTTPSyncStrategy) handleInfoSchema(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleInfoSchema(w, r)
}

// handleListEndpoints lists all available API endpoints (delegated to APIManager)
func (h *HTTPSyncStrategy) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleListEndpoints(w, r)
}

// handleDebugCache handles GET requests for cache debugging (delegated to DebugManager)
func (h *HTTPSyncStrategy) handleDebugCache(w http.ResponseWriter, r *http.Request) {
	h.debugManager.HandleDebugCache(w, r)
}

// handleDebugLogs handles GET requests for log level debugging (delegated to DebugManager)
func (h *HTTPSyncStrategy) handleDebugLogs(w http.ResponseWriter, r *http.Request) {
	h.debugManager.HandleDebugLogs(w, r)
}

// handleSetLogLevels handles POST requests to set log levels (delegated to DebugManager)
func (h *HTTPSyncStrategy) handleSetLogLevels(w http.ResponseWriter, r *http.Request) {
	h.debugManager.HandleSetLogLevels(w, r)
}

// handleTestInjectMetrics handles POST requests to inject test metrics (delegated to DebugManager)
func (h *HTTPSyncStrategy) handleTestInjectMetrics(w http.ResponseWriter, r *http.Request) {
	h.debugManager.HandleTestInjectMetrics(w, r)
}

// handleInjectRealMetrics handles POST requests to inject real production metrics (delegated to DebugManager)
func (h *HTTPSyncStrategy) handleInjectRealMetrics(w http.ResponseWriter, r *http.Request) {
	h.debugManager.HandleInjectRealMetrics(w, r)
}

// handleNagiosMetricsGET handles GET requests for Nagios format metrics by probe (delegated to NagiosManager)
func (h *HTTPSyncStrategy) handleNagiosMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.nagiosManager.HandleNagiosMetricsGET(w, r)
}

// Removed: handleNagiosCheck - /nagios/check/{check_name} endpoint not needed

// handleNagiosMetrics handles GET/POST requests for all Nagios metrics (delegated to NagiosManager)
func (h *HTTPSyncStrategy) handleNagiosMetrics(w http.ResponseWriter, r *http.Request) {
	h.nagiosManager.HandleNagiosMetrics(w, r)
}

// handleNagiosChecks lists all available Nagios checks (delegated to NagiosManager)
func (h *HTTPSyncStrategy) handleNagiosChecks(w http.ResponseWriter, r *http.Request) {
	h.nagiosManager.HandleNagiosChecks(w, r)
}

// Nagios helper functions (delegated to NagiosManager)

// handleZabbixMetricsGET handles GET requests for Zabbix format metrics (delegated to UtilsManager)
func (h *HTTPSyncStrategy) handleZabbixMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.utilsManager.handleZabbixMetricsGET(w, r)
}

// handlePrometheusMetricsGET handles GET requests for Prometheus format metrics (delegated to UtilsManager)
func (h *HTTPSyncStrategy) handlePrometheusMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.utilsManager.handlePrometheusMetricsGET(w, r)
}

// handlePrometheusStandardMetricsGET handles GET requests for the standard
// /metrics route (Bearer auth instead of URL-embedded agent key).
func (h *HTTPSyncStrategy) handlePrometheusStandardMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.utilsManager.handlePrometheusStandardMetricsGET(w, r)
}

// Web UI Handlers (delegating to WebInterface module)

func (h *HTTPSyncStrategy) handleWebDashboard(w http.ResponseWriter, r *http.Request) {
	h.webInterface.HandleWebDashboard(r, w)
}

func (h *HTTPSyncStrategy) handleWebExplorer(w http.ResponseWriter, r *http.Request) {
	h.webInterface.HandleWebExplorer(r, w)
}

func (h *HTTPSyncStrategy) handleWebDocs(w http.ResponseWriter, r *http.Request) {
	h.webInterface.HandleWebDocs(r, w)
}

// func (h *HTTPSyncStrategy) handleWebGuide(w http.ResponseWriter, r *http.Request) {
// 	h.webInterface.HandleWebGuide(r, w)
// }

func (h *HTTPSyncStrategy) handleWebAssets(w http.ResponseWriter, r *http.Request) {
	h.webInterface.HandleWebAssets(r, w)
}

// Admin API handlers

func (h *HTTPSyncStrategy) handleStatsCache(w http.ResponseWriter, r *http.Request) {
	h.debugManager.HandleStatsCache(w, r)
}

func (h *HTTPSyncStrategy) handleConfigProbes(w http.ResponseWriter, r *http.Request) {
	h.debugManager.HandleConfigProbes(w, r)
}

func (h *HTTPSyncStrategy) handleAdminCacheClear(w http.ResponseWriter, r *http.Request) {
	h.debugManager.HandleAdminCacheClear(w, r)
}

func (h *HTTPSyncStrategy) handleLicenseStatus(w http.ResponseWriter, r *http.Request) {
	// Delegate to API manager (license status is an admin API endpoint)
	h.apiManager.HandleLicenseStatus(w, r)
}

// Universal Configuration handlers (delegated to ConfigurationManager)

func (h *HTTPSyncStrategy) handleUniversalConfigValidation(w http.ResponseWriter, r *http.Request) {
	h.configManager.HandleUniversalConfigValidation(w, r)
}

func (h *HTTPSyncStrategy) handleUniversalConfigPreview(w http.ResponseWriter, r *http.Request) {
	h.configManager.HandleUniversalConfigPreview(w, r)
}

func (h *HTTPSyncStrategy) handleUniversalConfigTest(w http.ResponseWriter, r *http.Request) {
	h.configManager.HandleUniversalConfigTest(w, r)
}

// Module Access Getters (Encapsulation)
// These methods provide controlled access to internal modules

// GetAuthManager returns the authentication manager (read-only access)
func (h *HTTPSyncStrategy) GetAuthManager() *AuthenticationManager {
	return h.authManager
}

// GetHealthManager returns the health manager (read-only access)
func (h *HTTPSyncStrategy) GetHealthManager() *HealthManager {
	return h.healthManager
}

// GetMetricsProcessor returns the metrics processor (read-only access)
func (h *HTTPSyncStrategy) GetMetricsProcessor() *MetricsProcessor {
	return h.metricsProcessor
}

// GetConfigManager returns the configuration manager (read-only access)
func (h *HTTPSyncStrategy) GetConfigManager() *ConfigurationManager {
	return h.configManager
}

// GetServerManager returns the server manager (read-only access)
func (h *HTTPSyncStrategy) GetServerManager() *ServerManager {
	return h.serverManager
}

// GetDebugManager returns the debug manager (read-only access)
func (h *HTTPSyncStrategy) GetDebugManager() *DebugManager {
	return h.debugManager
}

// GetAPIManager returns the API manager (read-only access)
func (h *HTTPSyncStrategy) GetAPIManager() *APIManager {
	return h.apiManager
}

// GetNagiosManager returns the Nagios manager (read-only access)
func (h *HTTPSyncStrategy) GetNagiosManager() *NagiosManager {
	return h.nagiosManager
}

// GetUtilsManager returns the utilities manager (read-only access)
func (h *HTTPSyncStrategy) GetUtilsManager() *UtilsManager {
	return h.utilsManager
}

// GetWebInterface returns the web interface handler (read-only access)
func (h *HTTPSyncStrategy) GetWebInterface() *WebInterface {
	return h.webInterface
}

// GetCache returns the metric cache (read-only access)
func (h *HTTPSyncStrategy) GetCache() *MetricCache {
	return h.cache
}

// GetTransformerRegistry returns the transformer registry (read-only access)
func (h *HTTPSyncStrategy) GetTransformerRegistry() *transformers.TransformerRegistry {
	return h.transformerRegistry
}

// GetFormatConverter returns the format converter (read-only access)
func (h *HTTPSyncStrategy) GetFormatConverter() *FormatConverter {
	return h.formatConverter
}

// GetAssetHandler returns the asset handler (read-only access)
func (h *HTTPSyncStrategy) GetAssetHandler() *AssetHandler {
	return h.assetHandler
}

// Configuration Access Getters

// GetPort returns the configured port
func (h *HTTPSyncStrategy) GetPort() int {
	return h.port
}

// GetBindAddress returns the configured bind address
func (h *HTTPSyncStrategy) GetBindAddress() string {
	return h.bindAddress
}

// GetAgentKey returns the agent authentication key
func (h *HTTPSyncStrategy) GetAgentKey() string {
	return h.agentKey
}

// GetStartTime returns the strategy start time
func (h *HTTPSyncStrategy) GetStartTime() time.Time {
	return h.startTime
}

// UpdateConfiguration allows updating the HTTP strategy configuration at runtime
func (h *HTTPSyncStrategy) UpdateConfiguration(newParams map[string]interface{}) error {
	h.logger.Info().
		Any("new_params", newParams).
		Msg("Updating HTTP strategy configuration")

	// Update the configuration manager
	if err := h.configManager.UpdateConfiguration(newParams); err != nil {
		h.logger.Error().
			Err(err).
			Msg("Failed to update configuration manager")
		return err
	}

	// Update internal parameters
	h.params = newParams

	// Restart server if port or bind address changed
	if portParam, exists := newParams["port"]; exists {
		if newPort, ok := portParam.(int); ok && newPort != h.port {
			h.logger.Info().
				Int("old_port", h.port).
				Int("new_port", newPort).
				Msg("Port changed, restarting HTTP server")
			h.port = newPort
			return h.restartServer()
		}
	}

	if bindParam, exists := newParams["bind_address"]; exists {
		if newBind, ok := bindParam.(string); ok && newBind != h.bindAddress {
			h.logger.Info().
				Str("old_bind", h.bindAddress).
				Str("new_bind", newBind).
				Msg("Bind address changed, restarting HTTP server")
			h.bindAddress = newBind
			return h.restartServer()
		}
	}

	// Update cache configuration if agent config is LocalConfiguration
	if localConfig, ok := h.agentConfig.(*configuration.LocalConfiguration); ok {
		cacheConfig := localConfig.GetCacheConfig()
		newTTL := time.Duration(cacheConfig.RetentionMinutes) * time.Minute
		h.cache.UpdateTTL(newTTL)

		h.logger.Info().
			Int("retention_minutes", cacheConfig.RetentionMinutes).
			Msg("✅ Cache configuration updated")
	}

	h.logger.Info().Msg("✅ HTTP strategy configuration updated successfully")
	return nil
}

// restartServer restarts the HTTP server with new configuration
func (h *HTTPSyncStrategy) restartServer() error {
	h.logger.Info().Msg("Restarting HTTP server...")

	// Stop the current server
	if h.serverManager != nil {
		if err := h.serverManager.Shutdown(context.Background()); err != nil {
			h.logger.Warn().Err(err).Msg("Error stopping server during restart")
		}
	}

	// Restart with new configuration
	if err := h.serverManager.Start(); err != nil {
		h.logger.Error().Err(err).Msg("Failed to restart HTTP server")
		return err
	}

	h.logger.Info().
		Int("port", h.port).
		Str("bind_address", h.bindAddress).
		Msg("🚀 HTTP server restarted successfully")
	return nil
}

// Module Access Getters (Encapsulation)
// These methods provide controlled access to internal modules

// GetStatusService returns the status service (read-only access)
func (h *HTTPSyncStrategy) GetStatusService() *status.StatusService {
	return h.statusService
}

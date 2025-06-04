// senhub-agent/internal/agent/services/data_store/strategy_http.go
package data_store

import (
	"context"
	"fmt"
	"net/http"
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
	startTime           time.Time       // agent start time for uptime calculation
	assetHandler        *AssetHandler   // asset handler for templates and static files
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


// NewHTTPSyncStrategy creates a new HTTP sync strategy
func NewHTTPSyncStrategy(
	agentConfig configuration.AgentConfiguration,
	params map[string]interface{},
	baseLogger *logger.Logger,
) SyncStrategy {
	// Create module-specific logger for HTTP strategy
	moduleLogger := logger.NewModuleLogger(baseLogger, "strategy.http")

	// Get cache configuration from agent config
	cacheRetentionMinutes := 5 // Default value
	if localConfig, ok := agentConfig.(*configuration.LocalConfiguration); ok {
		cacheConfig := localConfig.GetCacheConfig()
		cacheRetentionMinutes = cacheConfig.RetentionMinutes
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
		cache: NewMetricCache(time.Duration(cacheRetentionMinutes)*time.Minute, moduleLogger),
	}
	
	// Initialize format converter after cache is created
	strategy.formatConverter = NewFormatConverter(strategy.transformerRegistry, moduleLogger, strategy.cache)
	
	// Initialize authentication manager
	strategy.authManager = NewAuthenticationManager(strategy.agentKey, agentConfig, moduleLogger)
	
	// Initialize web interface handler
	strategy.webInterface = NewWebInterface(strategy, moduleLogger, strategy.assetHandler)
	
	// Initialize health manager
	strategy.healthManager = NewHealthManager(strategy, moduleLogger, strategy.startTime)
	
	// Initialize metrics processor
	strategy.metricsProcessor = NewMetricsProcessor(strategy.cache, strategy.formatConverter, moduleLogger)
	
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
	
	// Initialize server manager (must be last, needs access to all other modules)
	strategy.serverManager = NewServerManager(strategy, moduleLogger)

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
	Status      string             `json:"status"`
	Version     string             `json:"version"`
	Commit      string             `json:"commit"`
	GoVersion   string             `json:"go_version"`
	OS          string             `json:"os"`
	Arch        string             `json:"arch"`
	Port        int                `json:"port"`
	Uptime      string             `json:"uptime"`
	Health      HealthCheckResponse `json:"health"`
	Cache       CacheInfoResponse   `json:"cache"`
	Resources   ResourcesInfo       `json:"resources"`
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

// formatDuration formats a duration in a human-readable format (delegated to UtilsManager)
func (h *HTTPSyncStrategy) formatDuration(d time.Duration) string {
	return h.utilsManager.formatDuration(d)
}

// getCPUUsage calculates the CPU usage percentage for the current process (delegated to UtilsManager)
func (h *HTTPSyncStrategy) getCPUUsage() float64 {
	return h.utilsManager.getCPUUsage()
}


// handleInfoTags provides tag discovery for a specific probe (delegated to APIManager)
func (h *HTTPSyncStrategy) handleInfoTags(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleInfoTags(w, r)
}

// handleInfoSchema provides complete schema information with examples (delegated to APIManager)
func (h *HTTPSyncStrategy) handleInfoSchema(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleInfoSchema(w, r)
}

// getTagDescription provides human-readable descriptions for common tags (delegated to UtilsManager)
func (h *HTTPSyncStrategy) getTagDescription(tagKey string) string {
	return h.utilsManager.getTagDescription(tagKey)
}

// generateExamples creates example API calls for the probe
func (h *HTTPSyncStrategy) generateExamples(probeName string, tags map[string]TagInfo, metrics []string) []MetricExample {
	return h.metricsProcessor.GenerateExamples(probeName, tags, metrics)
}

// getMetricsForProbe retrieves and transforms metrics for a specific probe (legacy - no filters)
func (h *HTTPSyncStrategy) getMetricsForProbe(probeName string) []PRTGChannel {
	return h.metricsProcessor.GetPRTGMetricsForProbe(probeName)
}

// getMetricsForProbeWithFilter retrieves and transforms metrics for a specific probe with filtering
func (h *HTTPSyncStrategy) getMetricsForProbeWithFilter(probeName string, filter MetricFilter) []PRTGChannel {
	return h.metricsProcessor.GetPRTGMetricsForProbeWithFilter(probeName, filter)
}

func (h *HTTPSyncStrategy) getSenHubMetricsForProbe(probeName string) []SenHubMetric {
	return h.metricsProcessor.GetSenHubMetricsForProbe(probeName)
}

func (h *HTTPSyncStrategy) convertToSenHubFormat(metric CachedMetric) SenHubMetric {
	return h.metricsProcessor.ConvertToSenHubFormat(metric)
}

func (h *HTTPSyncStrategy) transformToPRTGChannel(key string, metric CachedMetric) *PRTGChannel {
	return h.metricsProcessor.TransformToPRTGChannel(key, metric)
}

func (h *HTTPSyncStrategy) applyMetricFilter(metrics []CachedMetric, filter MetricFilter) []CachedMetric {
	return h.metricsProcessor.ApplyMetricFilter(metrics, filter)
}

func (h *HTTPSyncStrategy) transformMetricNameForPRTG(key string, metric CachedMetric) (string, string) {
	return h.metricsProcessor.TransformMetricNameForPRTG(key, metric)
}


// handleListEndpoints lists all available API endpoints (delegated to APIManager)
func (h *HTTPSyncStrategy) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	h.apiManager.HandleListEndpoints(w, r)
}

// parseMetricFilter parses query parameters into a MetricFilter
func (h *HTTPSyncStrategy) parseMetricFilter(r *http.Request) MetricFilter {
	return h.metricsProcessor.ParseMetricFilter(r)
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

// loadNagiosConfig loads the Nagios configuration from YAML file
func (h *HTTPSyncStrategy) loadNagiosConfig() *NagiosConfig {
	return h.configManager.LoadNagiosConfig()
}

// findNagiosCheck finds a check by name in the configuration
func (h *HTTPSyncStrategy) findNagiosCheck(config *NagiosConfig, checkName string) *NagiosCheck {
	return h.configManager.FindNagiosCheck(config, checkName)
}

// parseNagiosOverrides parses query parameters for Nagios threshold overrides
func (h *HTTPSyncStrategy) parseNagiosOverrides(r *http.Request) NagiosOverrides {
	return h.configManager.ParseNagiosOverrides(r)
}

// executeNagiosCheck executes a Nagios check with filtering and overrides (delegated to NagiosManager)
func (h *HTTPSyncStrategy) executeNagiosCheck(check *NagiosCheck, filter MetricFilter, overrides NagiosOverrides) NagiosResponse {
	return h.nagiosManager.executeNagiosCheck(check, filter, overrides)
}

// processNagiosMetric processes a single metric definition against available data (delegated to MetricsProcessor)
func (h *HTTPSyncStrategy) processNagiosMetric(metricDef NagiosMetric, metrics []CachedMetric, overrides NagiosOverrides) NagiosMetricResult {
	return h.metricsProcessor.ProcessNagiosMetric(metricDef, metrics, overrides)
}
// generateSimpleNagiosResponse generates a simple Nagios response for probe-based queries (delegated to MetricsProcessor)
func (h *HTTPSyncStrategy) generateSimpleNagiosResponse(probeName string, metrics []CachedMetric) NagiosResponse {
	return h.metricsProcessor.GenerateSimpleNagiosResponse(probeName, metrics)
}

// handleZabbixMetricsGET handles GET requests for Zabbix format metrics (delegated to UtilsManager)
func (h *HTTPSyncStrategy) handleZabbixMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.utilsManager.handleZabbixMetricsGET(w, r)
}

// handlePrometheusMetricsGET handles GET requests for Prometheus format metrics (delegated to UtilsManager)
func (h *HTTPSyncStrategy) handlePrometheusMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.utilsManager.handlePrometheusMetricsGET(w, r)
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


// parseVersionInfo parses version and commit information from cliArgs (delegated to UtilsManager)
func (h *HTTPSyncStrategy) parseVersionInfo() VersionInfo {
	return h.utilsManager.parseVersionInfo()
}

// validateAgentKey delegates to the authentication manager
func (h *HTTPSyncStrategy) validateAgentKey(providedKey string) bool {
	return h.authManager.ValidateAgentKey(providedKey)
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
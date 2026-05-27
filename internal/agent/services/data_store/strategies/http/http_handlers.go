// senhub-agent/internal/agent/services/data_store/http_handlers.go
package http

import (
	"net/http"

	"github.com/gorilla/mux"
	"senhub-agent.go/internal/agent/services/logger"
)

// HTTPHandlers contains all HTTP request handlers for the strategy
type HTTPHandlers struct {
	strategy *HTTPSyncStrategy
	logger   *logger.ModuleLogger
}

// NewHTTPHandlers creates a new HTTP handlers instance
func NewHTTPHandlers(strategy *HTTPSyncStrategy) *HTTPHandlers {
	return &HTTPHandlers{
		strategy: strategy,
		logger:   strategy.logger,
	}
}

// SetupRoutes configures HTTP routes using the handlers
func (h *HTTPHandlers) SetupRoutes() *mux.Router {
	router := mux.NewRouter()

	// Count every served request per matched route template — read by
	// the Prometheus bridge for senhub_agent_http_requests_total{endpoint}.
	router.Use(CountRequests)

	// Emit one OTel SERVER span per request (no-op when traces are
	// disabled). Placed AFTER CountRequests so counters always increment
	// even if a future tracing change introduces a regression.
	router.Use(TraceRequests)

	// Always expose health check endpoint
	router.HandleFunc("/health", h.HandleHealth).Methods("GET")

	// API documentation endpoint
	router.HandleFunc("/api/{agentkey}/endpoints", h.HandleListEndpoints).Methods("GET")

	// Discovery endpoints (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/info/endpoints", h.HandleInfoEndpoints).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/system", h.HandleInfoSystem).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/probes", h.HandleInfoProbes).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/tags/{probe}", h.HandleInfoTags).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/schema/{probe}", h.HandleInfoSchema).Methods("GET")
	router.HandleFunc("/api/{agentkey}/info/otlp", h.HandleInfoOTLP).Methods("GET")

	// Debug endpoints (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/debug/cache", h.HandleDebugCache).Methods("GET")
	router.HandleFunc("/api/{agentkey}/debug/logs", h.HandleDebugLogs).Methods("GET")
	router.HandleFunc("/api/{agentkey}/debug/logs", h.HandleSetLogLevels).Methods("POST")
	router.HandleFunc("/api/{agentkey}/debug/inject-test-metrics", h.HandleTestInjectMetrics).Methods("POST") // TEMPORARY TEST ENDPOINT
	router.HandleFunc("/api/{agentkey}/debug/inject-real-metrics", h.HandleInjectRealMetrics).Methods("POST") // PRODUCTION DATA INJECTION

	// Runtime profiling — Go's net/http/pprof handlers mounted under
	// /api/{agentkey}/debug/pprof/. Same agentkey auth as the other
	// debug endpoints; needed for goroutine-dump-based investigation
	// of stalls (the Windows "agent silent after JT400 respawn" incident is
	// the motivating case).
	registerPprofRoutes(router, h)

	// Admin endpoints (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/stats/cache", h.HandleStatsCache).Methods("GET")
	router.HandleFunc("/api/{agentkey}/config/probes", h.HandleConfigProbes).Methods("GET")
	router.HandleFunc("/api/{agentkey}/admin/cache/clear", h.HandleAdminCacheClear).Methods("POST")
	router.HandleFunc("/api/{agentkey}/license/status", h.HandleLicenseStatus).Methods("GET")

	// Universal Configuration endpoints (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/config/validate", h.HandleUniversalConfigValidation).Methods("POST")
	router.HandleFunc("/api/{agentkey}/config/preview", h.HandleUniversalConfigPreview).Methods("POST")
	router.HandleFunc("/api/{agentkey}/config/test", h.HandleUniversalConfigTest).Methods("POST")

	// Lookups endpoints (with agentkey authentication)
	if h.strategy.lookupsManager != nil {
		h.strategy.lookupsManager.RegisterRoutes(router)
	}

	// Configure endpoints based on enabled monitoring tools
	if h.strategy.configManager.IsEndpointEnabled("prtg") {
		// PRTG endpoints
		router.HandleFunc("/api/{agentkey}/prtg/metrics", h.HandlePRTGMetrics).Methods("POST")
		router.HandleFunc("/api/{agentkey}/prtg/metrics/{probe}", h.HandlePRTGMetricsGET).Methods("GET")
		router.HandleFunc("/api/{agentkey}/prtg/probes", h.HandleListProbes).Methods("GET")
	}

	if h.strategy.configManager.IsEndpointEnabled("nagios") {
		// Nagios endpoints
		router.HandleFunc("/api/{agentkey}/nagios/metrics/{probe}", h.HandleNagiosMetricsGET).Methods("GET")
		router.HandleFunc("/api/{agentkey}/nagios/metrics", h.HandleNagiosMetrics).Methods("GET", "POST")
		// Removed: /nagios/check/{probe} endpoint not needed
		router.HandleFunc("/api/{agentkey}/nagios/checks", h.HandleNagiosChecks).Methods("GET", "POST")
	}

	if h.strategy.configManager.IsEndpointEnabled("zabbix") {
		// Zabbix endpoints
		router.HandleFunc("/api/{agentkey}/zabbix/metrics/{probe}", h.HandleZabbixMetricsGET).Methods("GET")
	}

	if h.strategy.configManager.IsEndpointEnabled("prometheus") {
		// Prometheus endpoints — dual route:
		//   1. SenHub pattern /api/{agentkey}/prometheus/metrics — URL-embedded key
		//   2. Standard Prometheus /metrics — Bearer token or ?token= auth
		//      (matches vmagent, Prometheus, Grafana Agent native expectations)
		router.HandleFunc("/api/{agentkey}/prometheus/metrics", h.HandlePrometheusMetricsGET).Methods("GET")
		router.HandleFunc("/metrics", h.HandlePrometheusStandardMetricsGET).Methods("GET")
	}

	if h.strategy.configManager.IsEndpointEnabled("web") {
		// Web UI endpoints
		router.HandleFunc("/web/{agentkey}/", h.HandleWebDashboard).Methods("GET")
		router.HandleFunc("/web/{agentkey}/dashboard", h.HandleWebDashboard).Methods("GET")
		router.HandleFunc("/web/{agentkey}/explorer", h.HandleWebExplorer).Methods("GET")
		router.HandleFunc("/web/{agentkey}/docs", h.HandleWebDocs).Methods("GET")
		// router.HandleFunc("/web/{agentkey}/guide", h.HandleWebGuide).Methods("GET") // Temporarily disabled

		// Static assets
		router.PathPrefix("/web/{agentkey}/assets/").HandlerFunc(h.HandleWebAssets).Methods("GET")
	}

	return router
}

// Health and utility handlers

// HandleHealth handles health check requests (public endpoint - no authentication)
func (h *HTTPHandlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	// Delegate to health manager for basic health check
	h.strategy.healthManager.HandleBasicHealth(w, r)
}

// HandleListEndpoints handles requests to list available endpoints
func (h *HTTPHandlers) HandleListEndpoints(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleListEndpoints(w, r)
}

// Metrics API handlers

// Info/Discovery handlers (delegating to strategy for now)

func (h *HTTPHandlers) HandleInfoEndpoints(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleInfoEndpoints(w, r)
}

func (h *HTTPHandlers) HandleInfoSystem(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleInfoSystem(w, r)
}

func (h *HTTPHandlers) HandleInfoProbes(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleInfoProbes(w, r)
}

func (h *HTTPHandlers) HandleInfoTags(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleInfoTags(w, r)
}

func (h *HTTPHandlers) HandleInfoSchema(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleInfoSchema(w, r)
}

func (h *HTTPHandlers) HandleInfoOTLP(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleInfoOTLP(w, r)
}

// Debug handlers (delegating to strategy for now)

func (h *HTTPHandlers) HandleDebugCache(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleDebugCache(w, r)
}

func (h *HTTPHandlers) HandleDebugLogs(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleDebugLogs(w, r)
}

func (h *HTTPHandlers) HandleSetLogLevels(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleSetLogLevels(w, r)
}

func (h *HTTPHandlers) HandleTestInjectMetrics(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleTestInjectMetrics(w, r)
}

func (h *HTTPHandlers) HandleInjectRealMetrics(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleInjectRealMetrics(w, r)
}

// PRTG handlers (delegating to strategy for now)

func (h *HTTPHandlers) HandlePRTGMetrics(w http.ResponseWriter, r *http.Request) {
	h.strategy.handlePRTGMetrics(w, r)
}

func (h *HTTPHandlers) HandlePRTGMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.strategy.handlePRTGMetricsGET(w, r)
}

func (h *HTTPHandlers) HandleListProbes(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleListProbes(w, r)
}

// Nagios handlers (delegating to strategy for now)

func (h *HTTPHandlers) HandleNagiosMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleNagiosMetricsGET(w, r)
}

func (h *HTTPHandlers) HandleNagiosMetrics(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleNagiosMetrics(w, r)
}

// Removed: HandleNagiosCheck - endpoint not needed

func (h *HTTPHandlers) HandleNagiosChecks(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleNagiosChecks(w, r)
}

// Zabbix handlers (delegating to strategy for now)

func (h *HTTPHandlers) HandleZabbixMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleZabbixMetricsGET(w, r)
}

// Prometheus handlers (delegating to strategy for now)

func (h *HTTPHandlers) HandlePrometheusMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.strategy.handlePrometheusMetricsGET(w, r)
}

func (h *HTTPHandlers) HandlePrometheusStandardMetricsGET(w http.ResponseWriter, r *http.Request) {
	h.strategy.handlePrometheusStandardMetricsGET(w, r)
}

// Web UI handlers (delegating to strategy for now)

func (h *HTTPHandlers) HandleWebDashboard(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleWebDashboard(w, r)
}

func (h *HTTPHandlers) HandleWebExplorer(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleWebExplorer(w, r)
}

func (h *HTTPHandlers) HandleWebDocs(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleWebDocs(w, r)
}

// func (h *HTTPHandlers) HandleWebGuide(w http.ResponseWriter, r *http.Request) {
// 	h.strategy.handleWebGuide(w, r)
// }

func (h *HTTPHandlers) HandleWebAssets(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleWebAssets(w, r)
}

// Admin handlers (delegating to strategy for now)

func (h *HTTPHandlers) HandleStatsCache(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleStatsCache(w, r)
}

func (h *HTTPHandlers) HandleConfigProbes(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleConfigProbes(w, r)
}

func (h *HTTPHandlers) HandleAdminCacheClear(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleAdminCacheClear(w, r)
}

func (h *HTTPHandlers) HandleLicenseStatus(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleLicenseStatus(w, r)
}

// Universal Configuration handlers (delegating to ConfigurationManager)

func (h *HTTPHandlers) HandleUniversalConfigValidation(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleUniversalConfigValidation(w, r)
}

func (h *HTTPHandlers) HandleUniversalConfigPreview(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleUniversalConfigPreview(w, r)
}

func (h *HTTPHandlers) HandleUniversalConfigTest(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleUniversalConfigTest(w, r)
}

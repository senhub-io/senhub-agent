// senhub-agent/internal/agent/services/data_store/http_handlers.go
package data_store

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

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

	// Debug endpoints (with agentkey authentication)
	router.HandleFunc("/api/{agentkey}/debug/cache", h.HandleDebugCache).Methods("GET")
	router.HandleFunc("/api/{agentkey}/debug/logs", h.HandleDebugLogs).Methods("GET")
	router.HandleFunc("/api/{agentkey}/debug/logs", h.HandleSetLogLevels).Methods("POST")

	// Configure endpoints based on enabled monitoring tools
	if h.strategy.enabledEndpoints["prtg"] {
		// PRTG endpoints
		router.HandleFunc("/api/{agentkey}/prtg/metrics", h.HandlePRTGMetrics).Methods("POST")
		router.HandleFunc("/api/{agentkey}/prtg/metrics/{probe}", h.HandlePRTGMetricsGET).Methods("GET")
		router.HandleFunc("/api/{agentkey}/prtg/probes", h.HandleListProbes).Methods("GET")
	}

	if h.strategy.enabledEndpoints["senhub"] {
		// SenHub endpoints
		router.HandleFunc("/api/{agentkey}/senhub/metrics/{probe}", h.HandleSenHubMetricsGET).Methods("GET")
	}

	if h.strategy.enabledEndpoints["nagios"] {
		// Nagios endpoints
		router.HandleFunc("/api/{agentkey}/nagios/metrics/{probe}", h.HandleNagiosMetricsGET).Methods("GET")
		router.HandleFunc("/api/{agentkey}/nagios/metrics", h.HandleNagiosMetrics).Methods("POST")
		router.HandleFunc("/api/{agentkey}/nagios/check/{probe}", h.HandleNagiosCheck).Methods("GET")
		router.HandleFunc("/api/{agentkey}/nagios/checks", h.HandleNagiosChecks).Methods("POST")
	}

	if h.strategy.enabledEndpoints["zabbix"] {
		// Zabbix endpoints
		router.HandleFunc("/api/{agentkey}/zabbix/metrics/{probe}", h.HandleZabbixMetricsGET).Methods("GET")
	}

	if h.strategy.enabledEndpoints["prometheus"] {
		// Prometheus endpoints
		router.HandleFunc("/api/{agentkey}/prometheus/metrics", h.HandlePrometheusMetricsGET).Methods("GET")
	}

	if h.strategy.enabledEndpoints["web"] {
		// Web UI endpoints
		router.HandleFunc("/web/{agentkey}/", h.HandleWebDashboard).Methods("GET")
		router.HandleFunc("/web/{agentkey}/dashboard", h.HandleWebDashboard).Methods("GET")
		router.HandleFunc("/web/{agentkey}/explorer", h.HandleWebExplorer).Methods("GET")
		router.HandleFunc("/web/{agentkey}/docs", h.HandleWebDocs).Methods("GET")
		router.HandleFunc("/web/{agentkey}/admin", h.HandleWebAdmin).Methods("GET")
		
		// Static assets
		router.PathPrefix("/web/{agentkey}/assets/").HandlerFunc(h.HandleWebAssets).Methods("GET")
	}

	return router
}

// Health and utility handlers

// HandleHealth handles health check requests
func (h *HTTPHandlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Msg("Health check request received")

	// Get memory stats for health info
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memUsageMB := float64(memStats.Alloc) / 1024 / 1024

	healthInfo := struct {
		Status    string  `json:"status"`
		Timestamp string  `json:"timestamp"`
		Memory    float64 `json:"memory_mb"`
		Version   string  `json:"version"`
	}{
		Status:    "ok",
		Timestamp: time.Now().Format(time.RFC3339),
		Memory:    memUsageMB,
		Version:   "HTTP Strategy v1.0",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(healthInfo); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode health response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// HandleListEndpoints handles requests to list available endpoints
func (h *HTTPHandlers) HandleListEndpoints(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providedKey := vars["agentkey"]

	if providedKey != h.strategy.agentKey {
		h.logger.Warn().Str("provided_key", providedKey).Msg("Invalid agent key")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Debug().Msg("List endpoints request received")

	endpoints := make([]string, 0)
	
	// Add enabled endpoints
	for endpoint := range h.strategy.enabledEndpoints {
		if h.strategy.enabledEndpoints[endpoint] {
			endpoints = append(endpoints, endpoint)
		}
	}

	response := struct {
		Endpoints []string `json:"endpoints"`
	}{
		Endpoints: endpoints,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode endpoints response")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// validateAgentKey checks if the provided agent key is valid
func (h *HTTPHandlers) validateAgentKey(providedKey string) bool {
	return providedKey == h.strategy.agentKey
}

// Metrics API handlers

// HandleSenHubMetricsGET handles GET requests for SenHub format metrics by probe
func (h *HTTPHandlers) HandleSenHubMetricsGET(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	agentKey := vars["agentkey"]
	probeName := vars["probe"]

	// Validate agent key
	if agentKey != h.strategy.agentConfig.GetAuthenticationKey() {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.logger.Debug().
		Str("probe", probeName).
		Msg("SenHub metrics GET request received")

	// Get metrics from cache for the specified probe and convert to SenHub format
	senHubMetrics := h.strategy.getSenHubMetricsForProbe(probeName)

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
		Msg("✅ SenHub metrics served successfully")
}

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

func (h *HTTPHandlers) HandleNagiosCheck(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleNagiosCheck(w, r)
}

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

func (h *HTTPHandlers) HandleWebAdmin(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleWebAdmin(w, r)
}

func (h *HTTPHandlers) HandleWebAssets(w http.ResponseWriter, r *http.Request) {
	h.strategy.handleWebAssets(w, r)
}
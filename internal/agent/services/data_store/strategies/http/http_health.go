// senhub-agent/internal/agent/services/data_store/http_health.go
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// HealthManager handles all health check functionality for HTTP endpoints
type HealthManager struct {
	logger    *logger.ModuleLogger
	strategy  *HTTPSyncStrategy // Reference to parent strategy for metrics
	startTime time.Time         // System start time for uptime calculations
}

// NewHealthManager creates a new health check manager
func NewHealthManager(strategy *HTTPSyncStrategy, logger *logger.ModuleLogger, startTime time.Time) *HealthManager {
	return &HealthManager{
		logger:    logger,
		strategy:  strategy,
		startTime: startTime,
	}
}

// HealthResponse represents the basic health check response
type HealthResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	Commit        string `json:"commit,omitempty"`
	Uptime        string `json:"uptime"`
	ProbesActive  int    `json:"probes_active"`
	MetricsCached int    `json:"metrics_cached"`
}

// HealthCheckResponse represents detailed health information for system info
type HealthCheckResponse struct {
	Status    string            `json:"status"`
	Timestamp int64             `json:"timestamp"`
	Version   string            `json:"version"`
	Services  map[string]string `json:"services"`
}

// ResourcesInfo represents system resource usage information
type ResourcesInfo struct {
	// MemoryUsageMB is the process's resident set; HeapMB the Go heap.
	MemoryUsageMB float64 `json:"memory_usage_mb"`
	HeapMB        float64 `json:"heap_mb"`
	// CPUPercent is a share of the whole machine; meaningful only when
	// Measured is true.
	CPUPercent float64 `json:"cpu_percent"`
	Measured   bool    `json:"measured"`
	Goroutines int     `json:"goroutines"`
}

// SystemHealth represents comprehensive health status for system info endpoint
type SystemHealth struct {
	Health    HealthCheckResponse `json:"health"`
	Resources ResourcesInfo       `json:"resources"`
	Uptime    string              `json:"uptime"`
}

// HandleBasicHealth provides a simple health check endpoint (no authentication required)
func (h *HealthManager) HandleBasicHealth(w http.ResponseWriter, r *http.Request) {
	h.logger.Debug().Msg("Basic health check request received")

	perf := h.strategy.statusService.GetPerformanceMetrics()
	healthInfo := struct {
		Status    string  `json:"status"`
		Timestamp string  `json:"timestamp"`
		Memory    float64 `json:"memory_mb"`
		Version   string  `json:"version"`
	}{
		Status:    "ok",
		Timestamp: time.Now().Format(time.RFC3339),
		Memory:    perf.MemoryUsageMB,
		Version:   h.strategy.utilsManager.parseVersionInfo().Version,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(healthInfo); err != nil {
		h.logger.Error().Err(err).Msg("Failed to encode basic health response")
	}
}

// BuildSystemHealth creates comprehensive health information for system info endpoint
// This method now delegates to the centralized StatusService for consistency
func (h *HealthManager) BuildSystemHealth() SystemHealth {
	// Get comprehensive system status from centralized service
	systemStatus := h.strategy.statusService.GetSystemStatus()

	// Convert to HTTP strategy's format for backward compatibility
	healthResponse := HealthCheckResponse{
		Status:    systemStatus.Health.Status,
		Timestamp: systemStatus.Health.Timestamp.Unix(),
		Version:   systemStatus.Agent.Version,
		Services: map[string]string{
			"http_server": "running",
			"cache":       "running",
			"metrics":     fmt.Sprintf("%d metrics cached", systemStatus.Performance.CacheEntries),
		},
	}

	// Convert resources info
	resources := ResourcesInfo{
		MemoryUsageMB: systemStatus.Performance.MemoryUsageMB,
		HeapMB:        systemStatus.Performance.HeapMB,
		CPUPercent:    systemStatus.Performance.CPUPercent,
		Measured:      systemStatus.Performance.Measured,
		Goroutines:    systemStatus.Performance.Goroutines,
	}

	return SystemHealth{
		Health:    healthResponse,
		Resources: resources,
		Uptime:    systemStatus.Performance.Uptime,
	}
}

// GetServiceStatus returns the status of various system services
// This method now delegates to the centralized StatusService for consistency
func (h *HealthManager) GetServiceStatus() map[string]string {
	// Get status from centralized service
	systemStatus := h.strategy.statusService.GetSystemStatus()

	return map[string]string{
		"http_server": "running",
		"cache":       "running",
		"metrics":     fmt.Sprintf("%d metrics cached", systemStatus.Performance.CacheEntries),
		"strategy":    "http",
		"uptime":      systemStatus.Performance.Uptime,
		"health":      systemStatus.Health.Status,
	}
}

// IsHealthy performs basic health checks and returns system status
// This method now delegates to the centralized StatusService for consistency
func (h *HealthManager) IsHealthy() bool {
	// Basic infrastructure checks
	if h.strategy == nil || h.strategy.cache == nil || h.strategy.server == nil {
		return false
	}

	// Get health status from centralized service
	healthStatus := h.strategy.statusService.GetHealthStatus()

	// Consider healthy if status is "healthy" or "degraded" (not "unhealthy")
	return healthStatus.Status == "healthy" || healthStatus.Status == "degraded"
}

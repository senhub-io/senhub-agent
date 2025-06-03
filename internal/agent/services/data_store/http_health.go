// senhub-agent/internal/agent/services/data_store/http_health.go
package data_store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
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
	MemoryUsageMB float64 `json:"memory_usage_mb"`
	CPUPercent    float64 `json:"cpu_percent"`
	Goroutines    int     `json:"goroutines"`
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
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(healthInfo)
}

// HandleDetailedHealth provides a comprehensive health check endpoint (authenticated)
func (h *HealthManager) HandleDetailedHealth(w http.ResponseWriter, r *http.Request) {
	// Authentication is handled by the calling handler
	h.logger.Debug().Msg("Detailed health check request received")

	h.strategy.cache.mu.RLock()
	totalMetrics := len(h.strategy.cache.timeSeries)
	probeCount := len(h.strategy.cache.probeIndex)
	h.strategy.cache.mu.RUnlock()

	// Calculate actual uptime since start
	uptime := time.Since(h.startTime).Truncate(time.Second).String()

	// Get version information from strategy
	versionInfo := h.strategy.parseVersionInfo()

	response := HealthResponse{
		Status:        "ok",
		Version:       versionInfo.Version,
		Commit:        versionInfo.Commit,
		Uptime:        uptime,
		ProbesActive:  probeCount,
		MetricsCached: totalMetrics,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// BuildSystemHealth creates comprehensive health information for system info endpoint
func (h *HealthManager) BuildSystemHealth() SystemHealth {
	h.strategy.cache.mu.RLock()
	totalMetrics := len(h.strategy.cache.timeSeries)
	h.strategy.cache.mu.RUnlock()

	// Get memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memUsageMB := float64(memStats.Alloc) / 1024 / 1024

	// Calculate uptime
	uptime := time.Since(h.startTime)
	uptimeStr := fmt.Sprintf("%.0f seconds", uptime.Seconds())

	// Get version information
	versionInfo := h.strategy.parseVersionInfo()

	// Create health response
	healthResponse := HealthCheckResponse{
		Status:    "healthy",
		Timestamp: time.Now().Unix(),
		Version:   versionInfo.Version,
		Services: map[string]string{
			"http_server": "running",
			"cache":       "running",
			"metrics":     fmt.Sprintf("%d metrics cached", totalMetrics),
		},
	}

	// Create resources info
	resources := ResourcesInfo{
		MemoryUsageMB: memUsageMB,
		CPUPercent:    h.strategy.getCPUUsage(),
		Goroutines:    runtime.NumGoroutine(),
	}

	return SystemHealth{
		Health:    healthResponse,
		Resources: resources,
		Uptime:    uptimeStr,
	}
}

// GetServiceStatus returns the status of various system services
func (h *HealthManager) GetServiceStatus() map[string]string {
	h.strategy.cache.mu.RLock()
	totalMetrics := len(h.strategy.cache.timeSeries)
	h.strategy.cache.mu.RUnlock()

	return map[string]string{
		"http_server": "running",
		"cache":       "running",
		"metrics":     fmt.Sprintf("%d metrics cached", totalMetrics),
		"strategy":    "http",
		"uptime":      time.Since(h.startTime).Truncate(time.Second).String(),
	}
}

// IsHealthy performs basic health checks and returns system status
func (h *HealthManager) IsHealthy() bool {
	// Basic health checks
	if h.strategy == nil {
		return false
	}

	if h.strategy.cache == nil {
		return false
	}

	// Check if server is running (basic check)
	if h.strategy.server == nil {
		return false
	}

	return true
}

// GetHealthMetrics returns health-related metrics for monitoring integration
func (h *HealthManager) GetHealthMetrics() map[string]interface{} {
	h.strategy.cache.mu.RLock()
	totalMetrics := len(h.strategy.cache.timeSeries)
	probeCount := len(h.strategy.cache.probeIndex)
	h.strategy.cache.mu.RUnlock()

	// Get system metrics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	memUsageMB := float64(memStats.Alloc) / 1024 / 1024

	uptimeSeconds := time.Since(h.startTime).Seconds()

	return map[string]interface{}{
		"uptime_seconds":     uptimeSeconds,
		"memory_usage_mb":    memUsageMB,
		"cpu_usage_percent":  h.strategy.getCPUUsage(),
		"goroutines_count":   runtime.NumGoroutine(),
		"probes_active":      probeCount,
		"metrics_cached":     totalMetrics,
		"cache_ttl_seconds":  h.strategy.cache.ttl.Seconds(),
		"http_port":          h.strategy.port,
		"status":             "healthy",
	}
}
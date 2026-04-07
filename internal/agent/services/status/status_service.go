// Package status provides centralized system status and health monitoring
// This module centralizes status calculations that can be used by:
// - HTTP strategy dashboard endpoints
// - CLI status command
// - Health checks and monitoring
package status

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"senhub-agent.go/internal/agent/services/logger"
)

// StatusService provides centralized system status calculations
type StatusService struct {
	logger        *logger.ModuleLogger
	startTime     time.Time
	cacheProvider CacheStatisticsProvider // For accessing cache statistics
	agentMode     string                  // "online", "offline", or "unknown"
	version       string
	commit        string
}

// SystemStatus represents the complete system status
type SystemStatus struct {
	Health      HealthInfo      `json:"health"`
	Connection  ConnectionInfo  `json:"connection"`
	Probes      []ProbeStatus   `json:"probes"`
	Performance PerformanceInfo `json:"performance"`
	Agent       AgentInfo       `json:"agent"`
}

// HealthInfo represents system health status
type HealthInfo struct {
	Status    string    `json:"status"` // "healthy", "unhealthy", "degraded"
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message,omitempty"`
}

// ConnectionInfo represents connection and configuration mode
type ConnectionInfo struct {
	Mode         string `json:"mode"`                    // "online", "offline"
	Source       string `json:"source"`                  // "remote_server", "local_config"
	Status       string `json:"status"`                  // "connected", "disconnected", "local"
	DashboardURL string `json:"dashboard_url,omitempty"` // URL to the agent dashboard
}

// ProbeStatus represents individual probe status
type ProbeStatus struct {
	Name         string    `json:"name"`
	Status       string    `json:"status"` // "active", "inactive", "error"
	MetricsCount int       `json:"metrics_count"`
	LastUpdate   time.Time `json:"last_update,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
}

// PerformanceInfo represents system performance metrics
type PerformanceInfo struct {
	Uptime        string  `json:"uptime"`
	MemoryUsageMB float64 `json:"memory_usage_mb"`
	CPUPercent    float64 `json:"cpu_percent"`
	Goroutines    int     `json:"goroutines"`
	CacheEntries  int     `json:"cache_entries"`
}

// AgentInfo represents agent build and version information
type AgentInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	BuildTime string `json:"build_time,omitempty"`
}

// NewStatusService creates a new status service instance
func NewStatusService(baseLogger *logger.Logger, version, commit string) *StatusService {
	moduleLogger := logger.NewModuleLogger(baseLogger, "status.service")

	return &StatusService{
		logger:    moduleLogger,
		startTime: time.Now(),
		agentMode: "unknown",
		version:   version,
		commit:    commit,
	}
}

// SetCacheProvider sets the cache statistics provider
func (s *StatusService) SetCacheProvider(provider CacheStatisticsProvider) {
	s.cacheProvider = provider
	s.logger.Debug().Msg("Cache provider configured for status service")
}

// SetAgentMode sets the current agent mode (online/offline)
func (s *StatusService) SetAgentMode(mode string) {
	s.agentMode = mode
	s.logger.Debug().Str("mode", mode).Msg("Agent mode set")
}

// GetSystemStatus returns complete system status
func (s *StatusService) GetSystemStatus() SystemStatus {
	s.logger.Debug().Msg("Calculating complete system status")

	return SystemStatus{
		Health:      s.calculateHealthInfo(),
		Connection:  s.calculateConnectionInfo(),
		Probes:      s.calculateProbeStatuses(),
		Performance: s.calculatePerformanceInfo(),
		Agent:       s.calculateAgentInfo(),
	}
}

// GetHealthStatus returns basic health information
func (s *StatusService) GetHealthStatus() HealthInfo {
	return s.calculateHealthInfo()
}

// GetProbeStatuses returns probe status information
func (s *StatusService) GetProbeStatuses() []ProbeStatus {
	return s.calculateProbeStatuses()
}

// GetPerformanceMetrics returns performance metrics
func (s *StatusService) GetPerformanceMetrics() PerformanceInfo {
	return s.calculatePerformanceInfo()
}

// calculateHealthInfo determines overall system health
func (s *StatusService) calculateHealthInfo() HealthInfo {
	status := "healthy"
	message := ""

	// Check various health indicators
	probeStatuses := s.calculateProbeStatuses()
	errorCount := 0

	for _, probe := range probeStatuses {
		if probe.Status == "error" {
			errorCount++
		}
	}

	// Determine health based on probe errors
	if errorCount > 0 {
		if errorCount >= len(probeStatuses)/2 {
			status = "unhealthy"
			message = fmt.Sprintf("%d of %d probes have errors", errorCount, len(probeStatuses))
		} else {
			status = "degraded"
			message = fmt.Sprintf("%d probe(s) have errors", errorCount)
		}
	}

	// Check memory usage
	perf := s.calculatePerformanceInfo()
	if perf.MemoryUsageMB > 1000 { // 1GB threshold
		if status == "healthy" {
			status = "degraded"
			message = "High memory usage"
		}
	}

	return HealthInfo{
		Status:    status,
		Timestamp: time.Now(),
		Message:   message,
	}
}

// calculateConnectionInfo determines connection mode and status
func (s *StatusService) calculateConnectionInfo() ConnectionInfo {
	mode := s.agentMode
	if mode == "unknown" {
		mode = "offline" // Default assumption
	}

	var source, status string

	switch mode {
	case "online":
		source = "remote_server"
		status = "connected" // Could be enhanced with actual connectivity check
	case "offline":
		source = "local_config"
		status = "local"
	default:
		source = "unknown"
		status = "unknown"
	}

	return ConnectionInfo{
		Mode:   mode,
		Source: source,
		Status: status,
	}
}

// calculateProbeStatuses gets status for all probes
func (s *StatusService) calculateProbeStatuses() []ProbeStatus {
	var probeStatuses []ProbeStatus

	if s.cacheProvider == nil {
		s.logger.Debug().Msg("Cache provider not available, returning empty probe list")
		return probeStatuses
	}

	// Get probe statistics from cache provider
	probeStats := s.cacheProvider.GetProbeStatistics()

	for _, stats := range probeStats {
		status := "active"
		if !stats.IsActive {
			status = "inactive"
		}
		if stats.LastError != "" {
			status = "error"
		}

		probeStatus := ProbeStatus{
			Name:         stats.Name,
			Status:       status,
			MetricsCount: stats.MetricsCount,
			LastUpdate:   stats.LastUpdate,
			LastError:    stats.LastError,
		}

		probeStatuses = append(probeStatuses, probeStatus)
	}

	s.logger.Debug().Int("probe_count", len(probeStatuses)).Msg("Calculated probe statuses")
	return probeStatuses
}

// calculatePerformanceInfo gathers system performance metrics
func (s *StatusService) calculatePerformanceInfo() PerformanceInfo {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	uptime := time.Since(s.startTime)
	uptimeStr := s.formatUptime(uptime)

	// Memory usage in MB
	memoryMB := float64(memStats.Alloc) / 1024 / 1024

	// Goroutine count
	goroutines := runtime.NumGoroutine()

	// CPU usage would require more complex calculation
	// For now, return 0 - can be enhanced later
	cpuPercent := 0.0

	// Cache entries count
	cacheEntries := 0
	if s.cacheProvider != nil {
		cacheEntries = s.cacheProvider.GetTotalEntries()
	}

	return PerformanceInfo{
		Uptime:        uptimeStr,
		MemoryUsageMB: memoryMB,
		CPUPercent:    cpuPercent,
		Goroutines:    goroutines,
		CacheEntries:  cacheEntries,
	}
}

// calculateAgentInfo returns agent build and version information
func (s *StatusService) calculateAgentInfo() AgentInfo {
	return AgentInfo{
		Version:   s.formatVersion(s.version),
		Commit:    s.formatCommitHash(s.commit),
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// formatUptime formats duration into human-readable uptime
func (s *StatusService) formatUptime(uptime time.Duration) string {
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60
	seconds := int(uptime.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm %ds", days, hours, minutes, seconds)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}

// formatVersion formats version string
func (s *StatusService) formatVersion(version string) string {
	if version == "" {
		return "unknown"
	}
	return version
}

// formatCommitHash formats git commit hash
func (s *StatusService) formatCommitHash(commit string) string {
	if commit == "" {
		return "unknown"
	}
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

// Shutdown gracefully shuts down the status service
func (s *StatusService) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Status service shutting down")
	return nil
}

package http

import (
	"time"

	"senhub-agent.go/internal/agent/services/logger"
	"senhub-agent.go/internal/agent/services/status"
)

// HTTPCacheAdapter adapts the HTTP strategy's MetricCache to implement status.CacheStatisticsProvider
type HTTPCacheAdapter struct {
	cache  *MetricCache
	logger *logger.ModuleLogger
}

// NewHTTPCacheAdapter creates a new adapter for HTTP strategy cache
func NewHTTPCacheAdapter(cache *MetricCache, baseLogger *logger.Logger) *HTTPCacheAdapter {
	moduleLogger := logger.NewModuleLogger(baseLogger, "status.cache_adapter")
	
	return &HTTPCacheAdapter{
		cache:  cache,
		logger: moduleLogger,
	}
}

// GetProbeStatistics implements status.CacheStatisticsProvider
func (a *HTTPCacheAdapter) GetProbeStatistics() map[string]status.ProbeStatistics {
	if a.cache == nil {
		a.logger.Warn().Msg("Cache is nil, returning empty statistics")
		return make(map[string]status.ProbeStatistics)
	}
	
	// Get statistics from HTTP cache
	httpStats := a.cache.GetProbeStatistics()
	
	// Convert to status service format
	statusStats := make(map[string]status.ProbeStatistics)
	
	for probeName, stats := range httpStats {
		statusStats[probeName] = status.ProbeStatistics{
			Name:         probeName,
			MetricsCount: stats.MetricsCount,
			LastUpdate:   stats.LastUpdate,
			IsActive:     stats.MetricsCount > 0 && time.Since(stats.LastUpdate) < 5*time.Minute,
			LastError:    "", // HTTP cache doesn't track errors, could be enhanced
		}
	}
	
	a.logger.Debug().Int("probe_count", len(statusStats)).Msg("Converted probe statistics")
	return statusStats
}

// GetCacheInfo implements status.CacheStatisticsProvider
func (a *HTTPCacheAdapter) GetCacheInfo() status.CacheInfo {
	if a.cache == nil {
		return status.CacheInfo{}
	}
	
	// Get cache info from HTTP cache - note: GetCacheInfo returns different structure
	httpCacheInfo := a.cache.GetCacheInfo()
	
	return status.CacheInfo{
		TotalEntries:     httpCacheInfo.TotalMetrics, // Note: field name is TotalMetrics not TotalEntries
		RetentionMinutes: 5,                          // Default TTL, could extract from cache if exposed
		LastCleanup:      time.Now(),                 // HTTP cache doesn't track this, use current time
		MemoryUsageMB:    0,                          // Could be calculated if needed
	}
}

// GetTotalEntries implements status.CacheStatisticsProvider
func (a *HTTPCacheAdapter) GetTotalEntries() int {
	if a.cache == nil {
		return 0
	}
	
	cacheInfo := a.cache.GetCacheInfo()
	return cacheInfo.TotalMetrics
}

// GetHealthMetrics implements status.CacheStatisticsProvider
func (a *HTTPCacheAdapter) GetHealthMetrics() map[string]interface{} {
	if a.cache == nil {
		return make(map[string]interface{})
	}
	
	// Get basic health metrics from cache
	cacheInfo := a.cache.GetCacheInfo()
	probeStats := a.cache.GetProbeStatistics()
	
	metrics := map[string]interface{}{
		"total_entries":      cacheInfo.TotalMetrics,
		"active_probes":      len(probeStats),
		"probe_count":        cacheInfo.ProbeCount,
	}
	
	// Add probe-specific metrics
	activeProbeCount := 0
	totalMetrics := 0
	
	for _, stats := range probeStats {
		totalMetrics += stats.MetricsCount
		if stats.MetricsCount > 0 && time.Since(stats.LastUpdate) < 5*time.Minute {
			activeProbeCount++
		}
	}
	
	metrics["active_probe_count"] = activeProbeCount
	metrics["total_metrics"] = totalMetrics
	
	return metrics
}
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
			IsActive:     stats.MetricsCount > 0 && time.Since(stats.LastUpdate) < stats.LiveWindow,
			LastError:    "", // HTTP cache doesn't track errors, could be enhanced
		}
	}

	a.logger.Debug().Int("probe_count", len(statusStats)).Msg("Converted probe statistics")
	return statusStats
}

// GetTotalEntries implements status.CacheStatisticsProvider
func (a *HTTPCacheAdapter) GetTotalEntries() int {
	if a.cache == nil {
		return 0
	}

	cacheInfo := a.cache.GetCacheInfo()
	return cacheInfo.TotalMetrics
}
